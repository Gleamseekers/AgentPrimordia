// reject.go — 四类拒绝分支的实证（未签名 / 公钥未钉扎 / 非 WASM / 工件被篡改）。
//
// 每个分支都真实调用框架生产入口（wasm.WASMToolAdapter.RegisterTool 或本
// demo 的钉扎验签门），打印框架返回的真实错误文本，不用自造文案冒充。
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"

	ap "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg"
)

// shellArtifact 是被故意当作「非 WASM 工件」的 shell 脚本（拒绝分支 C）。
// 语义同 cmd/ap/tool_forge.go 注释：「非 WASM 工件（如 shell 脚本）：沙箱
// 加载失败即拒绝执行——宿主零执行」。
const shellArtifact = "#!/bin/sh\necho demo\n"

// tamperMarker 载荷内容里的定位串，用于确定篡改偏移（翻转其首字节）。
const tamperMarker = "wasm-tool-exec ok"

// tamperIndex 返回被篡改的字节偏移（载荷内容首字节）。
func tamperIndex(artifact []byte) int {
	return bytes.Index(artifact, []byte(tamperMarker))
}

// tamperArtifact 返回篡改 1 字节后的工件副本（不改动原工件）。
//
// 篡改点在 data 段的载荷文本内：模块结构未被破坏，因此它仍是合法 WASM
// （沙箱能编译）——但签名不再匹配，验签门必须拒绝。
func tamperArtifact(artifact []byte) []byte {
	out := append([]byte(nil), artifact...)
	idx := tamperIndex(out)
	if idx < 0 {
		// 理论上不可达（模块由本包组装，标记串必然存在）；真出现则原样返回，
		// 由调用方的验签负对照暴露问题，不静默「篡改成功」。
		return out
	}
	out[idx] ^= 0x01
	return out
}

// tamperOffset 返回篡改偏移（输出用；-1 表示未找到标记串）。
func tamperOffset(artifact []byte) int { return tamperIndex(artifact) }

// registerUnsigned 拒绝分支 A：未签名工件直接送进注册入口。
//
// 走的是框架生产路径：wasm.WASMToolAdapter.RegisterTool 在验签失败时
// 于编译/加载之前返回错误（A6 签名前置）。
func registerUnsigned(ctx context.Context, artifact []byte) error {
	sandbox := newSandbox()
	defer func() { _ = sandbox.Close() }()
	adapter := ap.NewWASMToolAdapter(sandbox)

	// 故意不带 Signature/PublicKey。
	meta := ap.WASMToolMetadata{
		Name:        "unsigned-tool",
		Description: "未签名工件（应被拒绝）",
		ExecuteFunc: exportExecute,
		Version:     toolVersion,
	}
	return adapter.RegisterTool(ctx, meta, artifact)
}

// registerNonWASM 拒绝分支 C：非 WASM 工件（shell 脚本）。
//
// 该工件已用受信私钥签名、公钥已在钉扎集合中——验签门会放行；把它拦下的
// 是沙箱编译（不是合法 WASM 模块）。这正体现了两道门的分工：
// 签名门管「谁造的」，沙箱门管「这是什么」。
func registerNonWASM(ctx context.Context, gate *pinnedGate, priv ed25519.PrivateKey, pub ed25519.PublicKey) error {
	artifact := []byte(shellArtifact)
	sig, _, err := ap.WASMSignWASM(artifact, priv)
	if err != nil {
		return fmt.Errorf("签名 shell 工件失败: %w", err)
	}
	if err := gate.Verify(artifact, sig, pub); err != nil {
		// 非预期：签名/钉扎都没问题却被验签门拦下——如实报告，不冒充分支 C。
		return fmt.Errorf("非 WASM 工件竟未通过验签门（分支 C 前提不成立）: %w", err)
	}

	sandbox := newSandbox()
	defer func() { _ = sandbox.Close() }()
	adapter := ap.NewWASMToolAdapter(sandbox)

	meta := ap.WASMToolMetadata{
		Name:        "shell-tool",
		Description: "非 WASM 工件（应被沙箱拦下）",
		Parameters:  json.RawMessage(`{"type":"object"}`),
		ExecuteFunc: exportExecute,
		Version:     toolVersion,
		Signature:   sig,
		PublicKey:   pub,
	}
	return adapter.RegisterTool(ctx, meta, artifact)
}

// probeTamperedCompiles 佐证：被篡改的工件仍是合法 WASM（沙箱能编译）。
//
// 用独立探针沙箱验证，避免污染成功路径的沙箱实例。结论用于说明「拒绝篡改
// 工件的判据是签名，不是字节形态」。
func probeTamperedCompiles(tampered []byte) error {
	sandbox := newSandbox()
	defer func() { _ = sandbox.Close() }()
	if err := sandbox.Load("tampered-probe", tampered); err != nil {
		fmt.Printf("  · 篡改工件在沙箱中的编译结果: 失败（%v）\n", err)
		return nil
	}
	fmt.Printf("  · 篡改工件在沙箱中仍可编译（仍是合法 WASM）——拒绝它的不是形态，是签名\n")
	return nil
}
