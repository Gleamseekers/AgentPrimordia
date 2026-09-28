package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// defaultMaxPluginDownloadBytes 插件下载大小默认上限（256MB）。
// MarketInterface.Download 返回全量字节（无法流式限读），故在落盘前
// 强制长度上限——语义等价于 io.LimitReader 的防护（拒超限工件落盘）。
const defaultMaxPluginDownloadBytes int64 = 256 * 1024 * 1024

// PluginVerifierFunc cosign 同款验签函数（payload / 签名 base64 / 公钥 PEM）。
// 返回 nil = 验签通过。
//
// 分层约束（与 lifecycle/trust.go 同款）：internal/tools 不得反向 import
// 横向支撑包 marketplace（marketplace 消费 tools）——故验签算法由组装根
// （cmd/ 或测试）绑定 marketplace.VerifyCosignSignature。
type PluginVerifierFunc func(payload []byte, signatureB64, publicKeyPEM string) error

// PluginInstaller 插件安装器
type PluginInstaller struct {
	installDir      string
	market          MarketInterface
	loader          *PluginLoader
	verify          PluginVerifierFunc // 验签算法注入（nil = 拒绝一切安装）
	pinnedKeys      []string           // 钉扎签名者公钥（空 = 不钉扎）
	maxDownloadSize int64              // 下载大小上限（字节）
}

// NewPluginInstaller 创建插件安装器
func NewPluginInstaller(installDir string, market MarketInterface, loader *PluginLoader) (*PluginInstaller, error) {
	if installDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		installDir = filepath.Join(home, ".agentprimordia", "installed")
	}

	if err := os.MkdirAll(installDir, 0755); err != nil {
		return nil, fmt.Errorf("tools: cannot create install dir: %w", err)
	}

	return &PluginInstaller{
		installDir:      installDir,
		market:          market,
		loader:          loader,
		maxDownloadSize: defaultMaxPluginDownloadBytes,
	}, nil
}

// WithVerifier 注入验签算法（组装根绑定 marketplace.VerifyCosignSignature）。
// 未注入时 InstallFromMarket 一律拒绝（fail-closed）——宁可装不上，
// 不可装未经验签的工件。
func (pi *PluginInstaller) WithVerifier(verify PluginVerifierFunc) *PluginInstaller {
	pi.verify = verify
	return pi
}

// WithPinnedKeys 设置钉扎的签名者公钥集合（轮换窗口可多把）。
// 设置后 manifest.PublicKey 必须命中其中之一，否则拒绝安装。
func (pi *PluginInstaller) WithPinnedKeys(keys ...string) *PluginInstaller {
	pi.pinnedKeys = append([]string(nil), keys...)
	return pi
}

// WithMaxDownloadSize 设置下载大小上限（字节；<=0 恢复默认值）。
func (pi *PluginInstaller) WithMaxDownloadSize(n int64) *PluginInstaller {
	if n <= 0 {
		n = defaultMaxPluginDownloadBytes
	}
	pi.maxDownloadSize = n
	return pi
}

// sanitizePluginSegment 净化插件名/版本号：必须是单个安全的路径段。
// 拒绝空值、含路径分隔符、"."、".." 及一切穿越形态——
// 防 name="../../x" 经 filepath.Join 越出 installDir（P1 修复）。
func sanitizePluginSegment(kind, s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("tools: %s 为空", kind)
	}
	if strings.ContainsAny(s, `/\`) {
		return "", fmt.Errorf("tools: %s %q 含路径分隔符，拒绝", kind, s)
	}
	if s == "." || s == ".." || strings.Contains(s, "..") {
		return "", fmt.Errorf("tools: %s %q 含路径穿越形态，拒绝", kind, s)
	}
	// 双保险：Base 必须原样返回（否则说明有非常规路径成分）
	if filepath.Base(s) != s {
		return "", fmt.Errorf("tools: %s %q 不是单段安全名，拒绝", kind, s)
	}
	return s, nil
}

// InstallFromMarket 从市场安装插件。
//
// 安全顺序（P1 加固，全部先验后写）：
//  1. name/version 净化（单段安全名，拒穿越）；
//  2. 下载 + 大小上限（超限拒绝，等价 io.LimitReader 防护语义）；
//  3. 取 manifest（在任何落盘动作之前）；
//  4. checksum 校验（不符即拒，且不落盘）；
//  5. 签名校验（缺签/未配验签器/验签失败/签名者未钉扎 → 拒绝，且不落盘）；
//  6. 全部通过后才写工件（0644）与 manifest（0644）。
func (pi *PluginInstaller) InstallFromMarket(ctx context.Context, name, version string) error {
	// 1. 净化 name / version
	safeName, err := sanitizePluginSegment("插件名", name)
	if err != nil {
		return err
	}
	safeVersion, err := sanitizePluginSegment("版本号", version)
	if err != nil {
		return err
	}

	// 2. 下载（带大小上限）
	data, err := pi.market.Download(ctx, name, version)
	if err != nil {
		return fmt.Errorf("tools: download failed: %w", err)
	}
	if int64(len(data)) > pi.maxDownloadSize {
		return fmt.Errorf("tools: 插件 %s@%s 下载大小 %d 字节超过上限 %d，拒绝安装",
			name, version, len(data), pi.maxDownloadSize)
	}

	// 3. 取 manifest（先于一切落盘动作）
	manifest, err := pi.market.GetManifest(name)
	if err != nil {
		return fmt.Errorf("tools: get manifest: %w", err)
	}

	// 4. checksum 校验（先验后写）
	if manifest.Checksum != "" {
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != manifest.Checksum {
			return fmt.Errorf("tools: checksum verification failed for %s@%s", name, version)
		}
	}

	// 5. 签名校验（缺签即拒；未注入验签器即拒；验签失败即拒）
	if manifest.Signature == "" || manifest.PublicKey == "" {
		return fmt.Errorf("tools: 插件 %s@%s 的 manifest 无签名/公钥，拒绝安装"+
			"（P1：市场发布必须携带 cosign 签名；未签名工件不得落地）", name, version)
	}
	if len(pi.pinnedKeys) > 0 {
		pinned := false
		for _, k := range pi.pinnedKeys {
			if k == manifest.PublicKey {
				pinned = true
				break
			}
		}
		if !pinned {
			return fmt.Errorf("tools: 插件 %s@%s 签名者公钥不在钉扎集合内，拒绝安装", name, version)
		}
	}
	if pi.verify == nil {
		return fmt.Errorf("tools: 未配置验签器（WithVerifier 绑定 marketplace.VerifyCosignSignature），"+
			"拒绝安装 %s@%s", name, version)
	}
	if err := pi.verify(data, manifest.Signature, manifest.PublicKey); err != nil {
		return fmt.Errorf("tools: 签名校验失败，拒绝安装 %s@%s: %w", name, version, err)
	}

	// 6. 全部通过：落盘（工件 0644 数据文件）
	installPath := filepath.Join(pi.installDir, safeName)
	if err := os.MkdirAll(installPath, 0755); err != nil {
		return fmt.Errorf("tools: create install path: %w", err)
	}

	pluginFile := filepath.Join(installPath, safeVersion+".tar.gz")
	if err := os.WriteFile(pluginFile, data, 0644); err != nil {
		return fmt.Errorf("tools: write plugin file: %w", err)
	}

	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("tools: marshal manifest: %w", err)
	}
	manifestFile := filepath.Join(installPath, "manifest.json")
	if err := os.WriteFile(manifestFile, manifestData, 0644); err != nil {
		return fmt.Errorf("tools: write manifest: %w", err)
	}

	return nil
}

// VerifyChecksum 校验已安装插件的完整性
func (pi *PluginInstaller) VerifyChecksum(name, version string) error {
	safeName, err := sanitizePluginSegment("插件名", name)
	if err != nil {
		return err
	}
	safeVersion, err := sanitizePluginSegment("版本号", version)
	if err != nil {
		return err
	}
	installPath := filepath.Join(pi.installDir, safeName)
	manifestFile := filepath.Join(installPath, "manifest.json")

	data, err := os.ReadFile(manifestFile)
	if err != nil {
		return fmt.Errorf("tools: read manifest: %w", err)
	}

	var manifest PluginManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("tools: unmarshal manifest: %w", err)
	}

	if manifest.Checksum == "" {
		return nil
	}

	pluginFile := filepath.Join(installPath, safeVersion+".tar.gz")
	fileData, err := os.ReadFile(pluginFile)
	if err != nil {
		return fmt.Errorf("tools: read plugin file: %w", err)
	}

	sum := sha256.Sum256(fileData)
	if hex.EncodeToString(sum[:]) != manifest.Checksum {
		return fmt.Errorf("tools: checksum mismatch for %s@%s", name, version)
	}

	return nil
}

// Uninstall 卸载已安装的插件
func (pi *PluginInstaller) Uninstall(name string) error {
	safeName, err := sanitizePluginSegment("插件名", name)
	if err != nil {
		return err
	}
	installPath := filepath.Join(pi.installDir, safeName)
	if _, err := os.Stat(installPath); os.IsNotExist(err) {
		return fmt.Errorf("tools: plugin %q not installed", name)
	}
	return os.RemoveAll(installPath)
}

// ListInstalled 列出已安装的插件
func (pi *PluginInstaller) ListInstalled() ([]string, error) {
	entries, err := os.ReadDir(pi.installDir)
	if err != nil {
		return nil, fmt.Errorf("tools: read install dir: %w", err)
	}

	var plugins []string
	for _, entry := range entries {
		if entry.IsDir() {
			plugins = append(plugins, entry.Name())
		}
	}
	return plugins, nil
}

// InstallDir 返回安装目录
func (pi *PluginInstaller) InstallDir() string {
	return pi.installDir
}
