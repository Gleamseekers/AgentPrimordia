// plugin_installer_security_test.go — 插件安装器安全加固测试（P1 批次）
//
// 攻击面（修复前实证）：
//  1. name/version 未净化即 filepath.Join——"../../x" 越出 installDir；
//  2. 先 os.WriteFile 后验 checksum——校验失败时恶意工件已落盘；
//  3. 无签名验证——市场 manifest 可被篡改指向任意工件；
//  4. 下载无大小上限——恶意市场可耗尽磁盘。
package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// okVerifier 总是通过的验签桩（模拟组装根绑定 marketplace.VerifyCosignSignature）。
func okVerifier(_ []byte, _, _ string) error { return nil }

// badVerifier 总是失败的验签桩。
func badVerifier(_ []byte, _, _ string) error { return errors.New("signature verification failed") }

// signedManifest 构造带签名信息的 manifest。
func signedManifest(name, version string, checksum string) *PluginManifest {
	return &PluginManifest{
		Name:      name,
		Version:   version,
		Category:  "data",
		Checksum:  checksum,
		Signature: "c2lnbmF0dXJl", // base64("signature")
		PublicKey: "PUBKEY",
	}
}

// publishPlugin 向市场发布一个带工件与 manifest 的插件。
func publishPlugin(t *testing.T, market *FileBasedMarket, marketDir, name, version string, data []byte, manifest *PluginManifest) {
	t.Helper()
	pluginDir := filepath.Join(marketDir, name)
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("mkdir plugin dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, version+".tar.gz"), data, 0644); err != nil {
		t.Fatalf("write plugin file: %v", err)
	}
	if err := market.Publish(manifest); err != nil {
		t.Fatalf("publish: %v", err)
	}
}

func checksumHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// === 1. 路径穿越 ===

func TestPluginInstaller_PathTraversalNameRejected(t *testing.T) {
	dir := t.TempDir()
	marketDir := t.TempDir()
	market, _ := NewFileBasedMarket(marketDir)
	loader := NewPluginLoader(NewRegistry())
	installer, _ := NewPluginInstaller(dir, market, loader)
	installer.WithVerifier(okVerifier)

	err := installer.InstallFromMarket(context.Background(), "../../evil", "1.0.0")
	if err == nil {
		t.Fatal("name 含路径穿越应被拒绝")
	}
	// installDir 之外不得出现任何写入
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("installDir 应保持为空, 实际: %v", entries)
	}
}

func TestPluginInstaller_PathTraversalVersionRejected(t *testing.T) {
	dir := t.TempDir()
	marketDir := t.TempDir()
	market, _ := NewFileBasedMarket(marketDir)
	loader := NewPluginLoader(NewRegistry())
	installer, _ := NewPluginInstaller(dir, market, loader)
	installer.WithVerifier(okVerifier)

	err := installer.InstallFromMarket(context.Background(), "ok-plugin", "../../1.0.0")
	if err == nil {
		t.Fatal("version 含路径穿越应被拒绝")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("installDir 应保持为空, 实际: %v", entries)
	}
}

func TestPluginInstaller_UninstallTraversalRejected(t *testing.T) {
	dir := t.TempDir()
	marketDir := t.TempDir()
	market, _ := NewFileBasedMarket(marketDir)
	loader := NewPluginLoader(NewRegistry())
	installer, _ := NewPluginInstaller(dir, market, loader)

	if err := installer.Uninstall("../../etc"); err == nil {
		t.Fatal("Uninstall 路径穿越应被拒绝")
	}
}

func TestPluginInstaller_VerifyChecksumTraversalRejected(t *testing.T) {
	dir := t.TempDir()
	marketDir := t.TempDir()
	market, _ := NewFileBasedMarket(marketDir)
	loader := NewPluginLoader(NewRegistry())
	installer, _ := NewPluginInstaller(dir, market, loader)

	if err := installer.VerifyChecksum("../../etc", "1.0.0"); err == nil {
		t.Fatal("VerifyChecksum 路径穿越应被拒绝")
	}
}

// === 2. 先验后写 ===

func TestPluginInstaller_ChecksumMismatchNoWrite(t *testing.T) {
	dir := t.TempDir()
	marketDir := t.TempDir()
	market, _ := NewFileBasedMarket(marketDir)
	loader := NewPluginLoader(NewRegistry())
	installer, _ := NewPluginInstaller(dir, market, loader)
	installer.WithVerifier(okVerifier)

	data := []byte("tampered payload")
	// manifest 声明的 checksum 与工件不符
	publishPlugin(t, market, marketDir, "bad-sum", "1.0.0", data,
		signedManifest("bad-sum", "1.0.0", checksumHex([]byte("different"))))

	err := installer.InstallFromMarket(context.Background(), "bad-sum", "1.0.0")
	if err == nil {
		t.Fatal("checksum 不符应拒绝安装")
	}
	// 先验后写：任何文件都不得落盘
	if _, statErr := os.Stat(filepath.Join(dir, "bad-sum")); !os.IsNotExist(statErr) {
		t.Fatal("checksum 校验失败时不得写入任何文件（先验后写）")
	}
}

// === 3. 签名验证 ===

func TestPluginInstaller_UnsignedManifestRejected(t *testing.T) {
	dir := t.TempDir()
	marketDir := t.TempDir()
	market, _ := NewFileBasedMarket(marketDir)
	loader := NewPluginLoader(NewRegistry())
	installer, _ := NewPluginInstaller(dir, market, loader)
	installer.WithVerifier(okVerifier)

	data := []byte("unsigned plugin")
	m := signedManifest("no-sig", "1.0.0", checksumHex(data))
	m.Signature = ""
	m.PublicKey = ""
	publishPlugin(t, market, marketDir, "no-sig", "1.0.0", data, m)

	err := installer.InstallFromMarket(context.Background(), "no-sig", "1.0.0")
	if err == nil {
		t.Fatal("无签名的 manifest 必须拒绝安装")
	}
	if !strings.Contains(err.Error(), "签名") {
		t.Errorf("错误应说明缺签名原因, 实际: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "no-sig")); !os.IsNotExist(statErr) {
		t.Fatal("验签失败时不得写入任何文件")
	}
}

func TestPluginInstaller_SignatureVerifiedSuccess(t *testing.T) {
	dir := t.TempDir()
	marketDir := t.TempDir()
	market, _ := NewFileBasedMarket(marketDir)
	loader := NewPluginLoader(NewRegistry())
	installer, _ := NewPluginInstaller(dir, market, loader)
	installer.WithVerifier(okVerifier)

	data := []byte("signed plugin")
	publishPlugin(t, market, marketDir, "signed", "1.0.0", data,
		signedManifest("signed", "1.0.0", checksumHex(data)))

	if err := installer.InstallFromMarket(context.Background(), "signed", "1.0.0"); err != nil {
		t.Fatalf("签名+checksum 齐全应安装成功: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "signed", "1.0.0.tar.gz")); err != nil {
		t.Fatalf("插件文件应落盘: %v", err)
	}
}

func TestPluginInstaller_SignatureMismatchRejected(t *testing.T) {
	dir := t.TempDir()
	marketDir := t.TempDir()
	market, _ := NewFileBasedMarket(marketDir)
	loader := NewPluginLoader(NewRegistry())
	installer, _ := NewPluginInstaller(dir, market, loader)
	installer.WithVerifier(badVerifier)

	data := []byte("forged plugin")
	publishPlugin(t, market, marketDir, "forged", "1.0.0", data,
		signedManifest("forged", "1.0.0", checksumHex(data)))

	err := installer.InstallFromMarket(context.Background(), "forged", "1.0.0")
	if err == nil {
		t.Fatal("验签失败必须拒绝安装")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "forged")); !os.IsNotExist(statErr) {
		t.Fatal("验签失败时不得写入任何文件")
	}
}

func TestPluginInstaller_NoVerifierConfiguredRejected(t *testing.T) {
	dir := t.TempDir()
	marketDir := t.TempDir()
	market, _ := NewFileBasedMarket(marketDir)
	loader := NewPluginLoader(NewRegistry())
	installer, _ := NewPluginInstaller(dir, market, loader)
	// 未注入验签器

	data := []byte("plugin without verifier")
	publishPlugin(t, market, marketDir, "noverify", "1.0.0", data,
		signedManifest("noverify", "1.0.0", checksumHex(data)))

	err := installer.InstallFromMarket(context.Background(), "noverify", "1.0.0")
	if err == nil {
		t.Fatal("未配置验签器时必须拒绝安装（fail-closed）")
	}
}

func TestPluginInstaller_PinnedKeyEnforced(t *testing.T) {
	dir := t.TempDir()
	marketDir := t.TempDir()
	market, _ := NewFileBasedMarket(marketDir)
	loader := NewPluginLoader(NewRegistry())
	installer, _ := NewPluginInstaller(dir, market, loader)
	installer.WithVerifier(okVerifier)
	installer.WithPinnedKeys("TRUSTED-KEY")

	data := []byte("plugin from untrusted signer")
	m := signedManifest("untrusted", "1.0.0", checksumHex(data))
	m.PublicKey = "ATTACKER-KEY"
	publishPlugin(t, market, marketDir, "untrusted", "1.0.0", data, m)

	err := installer.InstallFromMarket(context.Background(), "untrusted", "1.0.0")
	if err == nil {
		t.Fatal("签名者公钥不在钉扎集合内必须拒绝安装")
	}
}

// === 4. 下载大小上限 ===

func TestPluginInstaller_OversizedDownloadRejected(t *testing.T) {
	dir := t.TempDir()
	marketDir := t.TempDir()
	market, _ := NewFileBasedMarket(marketDir)
	loader := NewPluginLoader(NewRegistry())
	installer, _ := NewPluginInstaller(dir, market, loader)
	installer.WithVerifier(okVerifier)
	installer.WithMaxDownloadSize(1024) // 测试用小上限

	// 构造超限工件（2KB > 1KB 上限）
	data := make([]byte, 2048)
	for i := range data {
		data[i] = 'x'
	}
	publishPlugin(t, market, marketDir, "huge", "1.0.0", data,
		signedManifest("huge", "1.0.0", checksumHex(data)))

	err := installer.InstallFromMarket(context.Background(), "huge", "1.0.0")
	if err == nil {
		t.Fatal("超大下载必须拒绝")
	}
	if !strings.Contains(err.Error(), "大小") && !strings.Contains(err.Error(), "超过") {
		t.Errorf("错误应说明大小超限, 实际: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "huge")); !os.IsNotExist(statErr) {
		t.Fatal("超大下载拒绝时不得写入任何文件")
	}
}
