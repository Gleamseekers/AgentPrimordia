// gate.go — 公钥钉扎 + 验签门（fail-closed）。
//
// 语义复刻 cmd/ap/tool_forge.go 的 pinnedArtifactVerifier：签名/公钥必填 →
// 公钥必须命中钉扎集合 → ed25519 验签 over SHA-256(工件)。本文件在示例侧
// 重写一份，因为 pinnedArtifactVerifier 是 cmd/ap 包内的未导出类型，
// 示例无法引用（AGENTS.md §4.2：示例只经 pkg/ 公共 API 与核心交互）。
//
// fail-closed 的关键性质：钉扎集合为空时没有任何公钥可信——一切工件被拒。
// 这与 internal/tools/intelligence 的 RegisteringCreator 语义一致（未注入
// verifier 时拒绝一切）。
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	ap "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg"
)

// pinnedGate 钉扎公钥集合上的工件验签门。
type pinnedGate struct {
	// pinned 以公钥原始字节为键（支持轮换窗口多键并存）。
	pinned map[string]bool
}

// newPinnedGate 构造钉扎集合。空集合 = 拒绝一切（fail-closed），
// 与 ToolForgeConfig.PinnedKeys 的「显式钉扎是启用前提」同义。
func newPinnedGate(keys ...[]byte) *pinnedGate {
	pinned := make(map[string]bool, len(keys))
	for _, k := range keys {
		if len(k) == 0 {
			continue
		}
		pinned[string(k)] = true
	}
	return &pinnedGate{pinned: pinned}
}

// count 返回钉扎的公钥数量（输出用）。
func (g *pinnedGate) count() int { return len(g.pinned) }

// Verify 验签门：签名/公钥必填 → 公钥命中钉扎集合 → ed25519 验签。
//
// 三个拒绝分支（对应 demo 的 [4/7][5/7] 与篡改情节）：
//   - 缺签名/公钥：签名前置，未签名工件不得进入注册表（A6）；
//   - 公钥未钉扎：签名有效但签名方不是受信方——agent 不得自铸密钥自签入库；
//   - 验签失败：工件被篡改（任改一字节即失败，A6 表驱动负样本同型）。
func (g *pinnedGate) Verify(artifact, signature, publicKey []byte) error {
	if len(signature) == 0 || len(publicKey) == 0 {
		return fmt.Errorf("工件缺少签名/公钥（INV-0 签名前置，A6）")
	}
	if len(g.pinned) == 0 {
		return fmt.Errorf("钉扎集合为空：无任何受信公钥，拒绝一切（fail-closed）")
	}
	if !g.pinned[string(publicKey)] {
		return fmt.Errorf("签名公钥未在钉扎集合中（指纹 %s）", fingerprint(publicKey))
	}
	digest := sha256.Sum256(artifact)
	if err := ap.WASMVerifySignature(artifact, signature, publicKey); err != nil {
		return fmt.Errorf("验签失败（sha256=%s）: %w", hex.EncodeToString(digest[:8]), err)
	}
	return nil
}

// fingerprint 公钥指纹（SHA-256(公钥) 前 4 字节十六进制，与
// wasm.KeyFingerprint 同口径；经 pkg 公共 API 计算）。
func fingerprint(publicKey []byte) string { return ap.WASMKeyFingerprint(publicKey) }

// sha256Hex 工件摘要的十六进制（前 8 字节，供输出对账）。
func sha256Hex(artifact []byte) string {
	digest := sha256.Sum256(artifact)
	return hex.EncodeToString(digest[:8])
}
