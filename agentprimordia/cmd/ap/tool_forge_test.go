// tool_forge_test.go — 工具锻造装配根全链路回归（INV-0 生产通道）。
//
// 固防的四条语义：
//  1. 未签名工件 → 拒绝注册（fail-closed）；
//  2. 签名公钥未钉扎 → 拒绝注册；
//  3. 已签名非 WASM 工件（如 shell 脚本）→ 可注册但执行被沙箱加载拒绝
//     （宿主零执行，生成质量属研究面）；
//  4. 已签名 WASM 工件（公钥钉扎）→ 沙箱内真实执行。
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools/intelligence"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/wasm"
)

// stubCreator 测试用基础生成器：返回预设工件。
type stubCreator struct {
	art *intelligence.ToolArtifact
	err error
}

func (c *stubCreator) Create(_ context.Context, _ intelligence.GapCandidate) (*intelligence.ToolArtifact, error) {
	if c.err != nil {
		return nil, c.err
	}
	return c.art, nil
}

// makeArtifact 构造带哈希锚定的工件（签名由调用方按用例决定是否填充）。
func makeArtifact(t *testing.T, name string, artifact []byte) *intelligence.ToolArtifact {
	t.Helper()
	sum := sha256.Sum256(artifact)
	return &intelligence.ToolArtifact{
		ID:          "auto-" + name,
		Name:        name,
		Description: "test artifact " + name,
		ArtifactSHA: hex.EncodeToString(sum[:]),
		Artifact:    artifact,
	}
}

// signArtifact 用私钥签名工件并填回 Signature/PublicKey。
func signArtifact(t *testing.T, art *intelligence.ToolArtifact, priv ed25519.PrivateKey) {
	t.Helper()
	sig, pub, err := wasm.SignWASM(art.Artifact, priv)
	if err != nil {
		t.Fatalf("SignWASM: %v", err)
	}
	art.Signature = sig
	art.PublicKey = pub
}

// genKey 生成 ed25519 密钥对（raw bytes 形式便于钉扎集合传递）。
func genKey(t *testing.T) (priv ed25519.PrivateKey, pub ed25519.PublicKey) {
	t.Helper()
	p, q, err := wasm.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	return p, q
}

// validTestWASM 最小可执行 WASM 模块（tool_execute/alloc/free 导出），
// 与 wasm 包 e2e 测试同构（magic + type/function/memory/export/code 段）。
func validTestWASM() []byte {
	b := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	b = append(b, 0x01, 0x12,
		0x03,
		0x60, 0x02, 0x7f, 0x7f, 0x02, 0x7f, 0x7f,
		0x60, 0x01, 0x7f, 0x01, 0x7f,
		0x60, 0x02, 0x7f, 0x7f, 0x00,
	)
	b = append(b, 0x03, 0x04, 0x03, 0x01, 0x00, 0x02)
	b = append(b, 0x05, 0x03, 0x01, 0x00, 0x01)
	b = append(b, 0x07, 0x28, 0x04,
		0x06, 'm', 'e', 'm', 'o', 'r', 'y', 0x02, 0x00,
		0x05, 'a', 'l', 'l', 'o', 'c', 0x00, 0x00,
		0x0c, 't', 'o', 'o', 'l', '_', 'e', 'x', 'e', 'c', 'u', 't', 'e', 0x00, 0x01,
		0x04, 'f', 'r', 'e', 'e', 0x00, 0x02,
	)
	b = append(b, 0x0a, 0x10, 0x03,
		0x04, 0x00, 0x41, 0x10, 0x0b,
		0x06, 0x00, 0x41, 0x00, 0x41, 0x02, 0x0b,
		0x02, 0x00, 0x0b,
	)
	return b
}

// TestToolForge_UnsignedRefused 未签名工件拒绝注册。
func TestToolForge_UnsignedRefused(t *testing.T) {
	reg := tools.NewRegistry()
	_, pub := genKey(t)
	art := makeArtifact(t, "unsigned_tool", []byte("#!/bin/sh\necho hi\n"))

	forge, err := NewToolForge(ToolForgeConfig{
		Registry: reg, Dir: t.TempDir(), PinnedKeys: [][]byte{pub}, BaseCreator: &stubCreator{art: art},
	})
	if err != nil {
		t.Fatalf("NewToolForge: %v", err)
	}
	defer forge.Close()

	if _, err := forge.Creator().Create(context.Background(), intelligence.GapCandidate{Key: "unsigned_tool"}); err == nil {
		t.Fatal("未签名工件应被拒绝注册")
	}
	if _, ok := reg.Get("unsigned_tool"); ok {
		t.Fatal("未签名工件不得出现在注册表")
	}
}

// TestToolForge_UnpinnedKeyRefused 签名公钥未钉扎 → 拒绝注册。
func TestToolForge_UnpinnedKeyRefused(t *testing.T) {
	reg := tools.NewRegistry()
	_, pub := genKey(t)
	otherPriv, _ := genKey(t) // 签名用另一把未钉扎的钥
	art := makeArtifact(t, "unpinned_tool", []byte("data"))
	signArtifact(t, art, otherPriv)

	forge, err := NewToolForge(ToolForgeConfig{
		Registry: reg, Dir: t.TempDir(), PinnedKeys: [][]byte{pub}, BaseCreator: &stubCreator{art: art},
	})
	if err != nil {
		t.Fatalf("NewToolForge: %v", err)
	}
	defer forge.Close()

	if _, err := forge.Creator().Create(context.Background(), intelligence.GapCandidate{Key: "unpinned_tool"}); err == nil {
		t.Fatal("未钉扎公钥的签名工件应被拒绝注册")
	}
}

// TestToolForge_SignedNonWASM_RegisterButExecuteRefused
// 已签名非 WASM 工件（shell 脚本）：注册放行，执行被沙箱加载拒绝。
func TestToolForge_SignedNonWASM_RegisterButExecuteRefused(t *testing.T) {
	reg := tools.NewRegistry()
	priv, pub := genKey(t)
	art := makeArtifact(t, "script_tool", []byte("#!/bin/sh\necho pwned\n"))
	signArtifact(t, art, priv)

	forge, err := NewToolForge(ToolForgeConfig{
		Registry: reg, Dir: t.TempDir(), PinnedKeys: [][]byte{pub}, BaseCreator: &stubCreator{art: art},
	})
	if err != nil {
		t.Fatalf("NewToolForge: %v", err)
	}
	defer forge.Close()

	if _, err := forge.Creator().Create(context.Background(), intelligence.GapCandidate{Key: "script_tool"}); err != nil {
		t.Fatalf("已签名工件应注册成功: %v", err)
	}
	tool, ok := reg.Get("script_tool")
	if !ok {
		t.Fatal("已签名工件应出现在注册表")
	}
	// 执行必须被拒绝（非 WASM，沙箱加载失败），且宿主编译/执行零发生。
	res, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	if err == nil && (res == nil || !res.IsError) {
		t.Fatal("非 WASM 工件执行应被沙箱拒绝")
	}
}

// TestToolForge_SignedWASM_Executes 已签名 WASM 工件（公钥钉扎）沙箱内真实执行。
func TestToolForge_SignedWASM_Executes(t *testing.T) {
	reg := tools.NewRegistry()
	priv, pub := genKey(t)
	art := makeArtifact(t, "wasm_tool", validTestWASM())
	signArtifact(t, art, priv)

	forge, err := NewToolForge(ToolForgeConfig{
		Registry: reg, Dir: t.TempDir(), PinnedKeys: [][]byte{pub}, BaseCreator: &stubCreator{art: art},
	})
	if err != nil {
		t.Fatalf("NewToolForge: %v", err)
	}
	defer forge.Close()

	if _, err := forge.Creator().Create(context.Background(), intelligence.GapCandidate{Key: "wasm_tool"}); err != nil {
		t.Fatalf("已签名 WASM 工件应注册成功: %v", err)
	}
	tool, ok := reg.Get("wasm_tool")
	if !ok {
		t.Fatal("wasm_tool 应出现在注册表")
	}
	res, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("WASM 沙箱执行不应返回 error: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("WASM 沙箱执行应成功: %+v", res)
	}
}

// TestToolForge_ConfigValidation 配置校验（fail-closed）。
func TestToolForge_ConfigValidation(t *testing.T) {
	_, pub := genKey(t)
	base := &stubCreator{art: makeArtifact(t, "x", []byte("x"))}
	cases := []struct {
		name string
		cfg  ToolForgeConfig
	}{
		{"nil registry", ToolForgeConfig{Dir: t.TempDir(), PinnedKeys: [][]byte{pub}, BaseCreator: base}},
		{"empty dir", ToolForgeConfig{Registry: tools.NewRegistry(), PinnedKeys: [][]byte{pub}, BaseCreator: base}},
		{"nil creator", ToolForgeConfig{Registry: tools.NewRegistry(), Dir: t.TempDir(), PinnedKeys: [][]byte{pub}}},
		{"empty pinned key", ToolForgeConfig{Registry: tools.NewRegistry(), Dir: t.TempDir(), PinnedKeys: [][]byte{{}}, BaseCreator: base}},
	}
	for _, tc := range cases {
		if _, err := NewToolForge(tc.cfg); err == nil {
			t.Errorf("%s: 预期配置错误", tc.name)
		}
	}
}

// TestToolForge_ToolIntelligenceConfig 装配产物可直接注入 ReActConfig。
func TestToolForge_ToolIntelligenceConfig(t *testing.T) {
	_, pub := genKey(t)
	forge, err := NewToolForge(ToolForgeConfig{
		Registry: tools.NewRegistry(), Dir: t.TempDir(), PinnedKeys: [][]byte{pub},
		BaseCreator: &stubCreator{art: makeArtifact(t, "y", []byte("y"))},
	})
	if err != nil {
		t.Fatalf("NewToolForge: %v", err)
	}
	defer forge.Close()

	tic := forge.ToolIntelligenceConfig(nil, nil)
	if tic == nil || tic.Creator == nil {
		t.Fatal("ToolIntelligenceConfig 应携带门控后的 Creator")
	}
}
