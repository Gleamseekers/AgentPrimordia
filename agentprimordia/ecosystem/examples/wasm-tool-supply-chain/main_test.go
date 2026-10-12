// main_test.go — wasm-tool-supply-chain demo 的确定性单测。
//
// 覆盖：模块字节稳定性、沙箱真实执行与校验和对账、三个拒绝分支 + 篡改分支、
// 空钉扎 fail-closed、agent 经 toolkit 真实调用 WASM 工具。
// 全部用例零外部依赖（无网络、无凭据、无时钟断言）。
package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	ap "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg"
)

// newTestSandboxAdapter 构造独立沙箱 + 适配器（每个用例一份，避免互相污染）。
func newTestSandboxAdapter(t *testing.T) (*ap.WASMSandbox, *ap.WASMToolAdapter) {
	t.Helper()
	sandbox := newSandbox()
	t.Cleanup(func() { _ = sandbox.Close() })
	return sandbox, ap.NewWASMToolAdapter(sandbox)
}

// signForTest 用新生成的 ed25519 密钥对签名工件，返回 (sig, pub)。
func signForTest(t *testing.T, artifact []byte) ([]byte, []byte) {
	t.Helper()
	priv, _, err := ap.WASMGenerateKeyPair()
	if err != nil {
		t.Fatalf("生成密钥对失败: %v", err)
	}
	sig, pub, err := ap.WASMSignWASM(artifact, priv)
	if err != nil {
		t.Fatalf("签名工件失败: %v", err)
	}
	return sig, pub
}

// TestWASMModuleBytesDeterministic 模块字节级确定：两次组装结果完全一致，
// 且 magic/version 正确（这是「无外部工具链」的可验证性质）。
func TestWASMModuleBytesDeterministic(t *testing.T) {
	a := buildWASMToolModule()
	b := buildWASMToolModule()
	if len(a) != len(b) {
		t.Fatalf("两次组装长度不同: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("字节 %d 不一致: %02x vs %02x", i, a[i], b[i])
		}
	}
	want := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	for i := range want {
		if a[i] != want[i] {
			t.Fatalf("magic/version 字节 %d = %02x, want %02x", i, a[i], want[i])
		}
	}
	// 载荷占位符必须存在且位于 payload 内（写回偏移的前提）。
	if idx := indexPlaceholder(); idx < 0 || idx+1 >= len(payloadTemplate) {
		t.Fatalf("占位符索引异常: %d (payload 长度 %d)", idx, len(payloadTemplate))
	}
}

// TestWASMModuleLoadsIntoSandbox 模块是合法 WASM：可被沙箱编译（A7 前置）。
func TestWASMModuleLoadsIntoSandbox(t *testing.T) {
	sandbox, _ := newTestSandboxAdapter(t)
	if err := sandbox.Load("probe", buildWASMToolModule()); err != nil {
		t.Fatalf("沙箱应能编译本模块: %v", err)
	}
}

// TestWASMToolExecutesAndReconciles 沙箱内真实执行 + 校验和独立复算对账。
//
// 这是「参数真的经过了 WASM 线性内存往返」的证据：宿主写参数 → 沙箱内逐字节
// 求和 → 写回载荷 → 宿主读回；宿主侧用 Go 复算同一值，逐位比对。
func TestWASMToolExecutesAndReconciles(t *testing.T) {
	ctx := context.Background()
	artifact := buildWASMToolModule()
	sig, pub := signForTest(t, artifact)

	_, adapter := newTestSandboxAdapter(t)
	if err := adapter.RegisterTool(ctx, toolMetadata(sig, pub), artifact); err != nil {
		t.Fatalf("注册应成功: %v", err)
	}

	res, err := adapter.ExecuteTool(ctx, toolName, json.RawMessage(toolArgs))
	if err != nil {
		t.Fatalf("ExecuteTool 失败: %v", err)
	}
	if res.IsError {
		t.Fatalf("执行不应报错: %s", res.Content)
	}
	got := checksumIn(res.Content)
	input, err := toolInputJSON(toolName, json.RawMessage(toolArgs))
	if err != nil {
		t.Fatalf("构造对账输入失败: %v", err)
	}
	want := inputChecksumHex(input)
	if got != want {
		t.Fatalf("校验和对账失败: 沙箱=%q 宿主复算=%q (content=%q)", got, want, res.Content)
	}
	if !strings.Contains(res.Content, "wasm-tool-exec ok") {
		t.Fatalf("执行结果缺少预期内容: %q", res.Content)
	}
}

// TestRejectUnsignedArtifact 拒绝分支 A：未签名工件在编译/加载前被拒（A6）。
func TestRejectUnsignedArtifact(t *testing.T) {
	ctx := context.Background()
	_, adapter := newTestSandboxAdapter(t)
	err := adapter.RegisterTool(ctx, ap.WASMToolMetadata{
		Name:        "unsigned-tool",
		ExecuteFunc: exportExecute,
		Version:     toolVersion,
	}, buildWASMToolModule())
	if err == nil {
		t.Fatal("未签名工件应被拒绝")
	}
	if !strings.Contains(err.Error(), "缺少签名或公钥") {
		t.Fatalf("拒绝原因不符合 A6 签名前置: %v", err)
	}
}

// TestRejectUnpinnedKey 拒绝分支 B：签名有效但公钥未钉扎 → 验签门拒绝。
func TestRejectUnpinnedKey(t *testing.T) {
	artifact := buildWASMToolModule()
	sig, pub := signForTest(t, artifact)
	// 钉扎的是另一把密钥。
	_, otherPub := signForTest(t, artifact)

	gate := newPinnedGate(otherPub)
	if err := gate.Verify(artifact, sig, pub); err == nil {
		t.Fatal("未钉扎公钥的签名应被拒绝")
	} else if !strings.Contains(err.Error(), "未在钉扎集合中") {
		t.Fatalf("拒绝原因不符合钉扎语义: %v", err)
	}
}

// TestRejectNonWASMArtifact 拒绝分支 C：非 WASM 工件（已签名且公钥已钉扎）
// 被沙箱编译拦下——签名门管「谁造的」，沙箱门管「这是什么」。
func TestRejectNonWASMArtifact(t *testing.T) {
	ctx := context.Background()
	artifact := []byte(shellArtifact)
	priv, pub, err := ap.WASMGenerateKeyPair()
	if err != nil {
		t.Fatalf("生成密钥对失败: %v", err)
	}
	sig, _, err := ap.WASMSignWASM(artifact, priv)
	if err != nil {
		t.Fatalf("签名失败: %v", err)
	}
	gate := newPinnedGate(pub)
	if err := gate.Verify(artifact, sig, pub); err != nil {
		t.Fatalf("已签名且已钉扎的工件应通过验签门（分支 C 前提）: %v", err)
	}

	_, adapter := newTestSandboxAdapter(t)
	err = adapter.RegisterTool(ctx, ap.WASMToolMetadata{
		Name:        "shell-tool",
		ExecuteFunc: exportExecute,
		Version:     toolVersion,
		Signature:   sig,
		PublicKey:   pub,
	}, artifact)
	if err == nil {
		t.Fatal("非 WASM 工件应被拒绝")
	}
	if !strings.Contains(err.Error(), "load module") {
		t.Fatalf("拒绝应发生在沙箱加载阶段: %v", err)
	}
}

// TestRejectTamperedArtifact 篡改 1 字节 → 验签失败（A6 负样本同型）。
func TestRejectTamperedArtifact(t *testing.T) {
	artifact := buildWASMToolModule()
	sig, pub := signForTest(t, artifact)
	gate := newPinnedGate(pub)

	if err := gate.Verify(artifact, sig, pub); err != nil {
		t.Fatalf("原始工件应通过验签门: %v", err)
	}
	tampered := tamperArtifact(artifact)
	if err := gate.Verify(tampered, sig, pub); err == nil {
		t.Fatal("篡改工件应被验签门拒绝")
	} else if !strings.Contains(err.Error(), "验签失败") {
		t.Fatalf("拒绝原因应为验签失败: %v", err)
	}
	// 直接调 pkg 验签 API 亦应失败（双重证据）。
	if err := ap.WASMVerifySignature(tampered, sig, pub); err == nil {
		t.Fatal("篡改工件经 pkg 验签 API 亦应失败")
	}
	// 原工件不得被篡改函数改动（副本语义）。
	if err := gate.Verify(artifact, sig, pub); err != nil {
		t.Fatalf("原工件应仍能通过验签（tamperArtifact 不得改动入参）: %v", err)
	}
}

// TestTamperedArtifactStillCompiles 佐证：被篡改的工件仍是合法 WASM——
// 拒绝它的判据是签名，不是字节形态。
func TestTamperedArtifactStillCompiles(t *testing.T) {
	sandbox, _ := newTestSandboxAdapter(t)
	if err := sandbox.Load("tampered-probe", tamperArtifact(buildWASMToolModule())); err != nil {
		t.Fatalf("篡改工件应仍可编译（篡改点在数据段）: %v", err)
	}
}

// TestEmptyPinningRejectsEverything fail-closed：空钉扎集合拒绝一切。
func TestEmptyPinningRejectsEverything(t *testing.T) {
	artifact := buildWASMToolModule()
	sig, pub := signForTest(t, artifact)
	if err := newPinnedGate().Verify(artifact, sig, pub); err == nil {
		t.Fatal("空钉扎集合应拒绝一切")
	}
}

// TestGateAcceptsSignedPinnedArtifact 验签门放行：签名有效 + 公钥已钉扎。
func TestGateAcceptsSignedPinnedArtifact(t *testing.T) {
	artifact := buildWASMToolModule()
	sig, pub := signForTest(t, artifact)
	if err := newPinnedGate(pub).Verify(artifact, sig, pub); err != nil {
		t.Fatalf("应通过验签门: %v", err)
	}
}

// TestAgentInvokesWASMTool agent 经 toolkit 真实调用 WASM 工具。
//
// 走的是框架生产路径：ReActAgent → tools.Registry → tools.Tool.Execute →
// WASMToolAdapter → Sandbox.ExecuteWithMemory（wazero）。
func TestAgentInvokesWASMTool(t *testing.T) {
	ctx := context.Background()
	artifact := buildWASMToolModule()
	sig, pub := signForTest(t, artifact)

	_, adapter := newTestSandboxAdapter(t)
	if err := adapter.RegisterTool(ctx, toolMetadata(sig, pub), artifact); err != nil {
		t.Fatalf("注册应成功: %v", err)
	}
	tool, err := adapter.AsTool(toolName)
	if err != nil {
		t.Fatalf("AsTool 失败: %v", err)
	}
	registry := ap.NewToolRegistry()
	if err := registry.Register(tool); err != nil {
		t.Fatalf("注册进 tools.Registry 失败: %v", err)
	}

	provider := newScriptedProvider(toolName, json.RawMessage(toolArgs))
	resp, err := runAgentWithWASMTool(toolName, registry, provider, "请用已注册的 WASM 工具计算 1+1")
	if err != nil {
		t.Fatalf("agent 运行失败: %v", err)
	}
	if resp.Metrics.TotalTools < 1 {
		t.Fatalf("agent 应至少发起一次工具调用, got %d", resp.Metrics.TotalTools)
	}
	if provider.toolCallCount() < 1 {
		t.Fatalf("provider 应至少被调用一次, got %d", provider.toolCallCount())
	}

	// 工具结果可经注册表取回，且校验和与宿主复算一致。
	registered, ok := registry.Get(toolName)
	if !ok {
		t.Fatal("tools.Registry 中找不到已注册工具")
	}
	result, err := registered.Execute(ctx, json.RawMessage(toolArgs))
	if err != nil {
		t.Fatalf("经注册表执行失败: %v", err)
	}
	if result.IsError {
		t.Fatalf("工具执行报错: %s", result.Content)
	}
	input, err := toolInputJSON(toolName, json.RawMessage(toolArgs))
	if err != nil {
		t.Fatalf("构造对账输入失败: %v", err)
	}
	if got, want := checksumIn(result.Content), inputChecksumHex(input); got != want {
		t.Fatalf("校验和对账失败: 沙箱=%q 宿主复算=%q", got, want)
	}
}

// TestScriptedProviderContract 脚本化 provider 的接口契约与轮次行为。
func TestScriptedProviderContract(t *testing.T) {
	p := newScriptedProvider(toolName, json.RawMessage(toolArgs))
	var _ ap.Provider = p

	first, err := p.CallTools(context.Background(), &ap.ToolCallRequest{})
	if err != nil {
		t.Fatalf("首次 CallTools 失败: %v", err)
	}
	if len(first.ToolCalls) != 1 || first.ToolCalls[0].Name != toolName {
		t.Fatalf("首次 CallTools 应返回一次 %s 调用, got %+v", toolName, first.ToolCalls)
	}
	second, err := p.CallTools(context.Background(), &ap.ToolCallRequest{})
	if err != nil {
		t.Fatalf("二次 CallTools 失败: %v", err)
	}
	if len(second.ToolCalls) != 0 {
		t.Fatalf("二次 CallTools 应返回空调用集（收尾）, got %+v", second.ToolCalls)
	}
	if p.Info().Name == "" {
		t.Fatal("Info().Name 不应为空")
	}
}
