// wasm-tool-supply-chain — 「开发者自带 WASM 工件」的安全供应链可运行 Demo。
//
// 场景（对应 AGENTS.md §2.3 INV-0 与提案-code层沙箱受控释放.md §2.2 的 A4/A6/A7）：
//
//	开发者（本 demo 代码）手工组装 WASM 字节
//	  → ed25519 签名（over SHA-256(工件)）
//	  → 公钥钉扎（fail-closed：空集合 = 拒绝一切）
//	  → 验签门注册（未签名 / 公钥未钉扎 / 非 WASM / 工件被篡改：四类拒绝）
//	  → 注册进 tools.Registry（ReActAgent toolkit 调用路径）
//	  → agent 在 wazero 沙箱中真实调用该工具并取回结果
//
// 运行方式（任意工作目录，约 1 秒）：
//
//	./run.sh
//
// 或（在 monorepo 根目录）：
//
//	go run ./agentprimordia/ecosystem/examples/wasm-tool-supply-chain
//
// 诚实边界（先读这段再信输出）：
//
//   - 工具生成器无生产实现。internal/tools/lifecycle 的全部 Generator 实现
//     均为测试替身，cmd/ap/tool_forge.go 的注释亦自认「生成质量（WASM 化）
//     属研究面」。本 demo 的 WASM 工件由「开发者」（即本 demo 代码）手工
//     字节组装提供——定位是**安全供应链**，不是 agent 自主写工具。
//   - agent 的「决定调用哪个工具」由零凭据脚本化 provider 给出（不发网络
//     请求、不需要 API key）；工具执行走框架生产路径（详见 agent.go 注释）。
//   - ed25519 密钥对每次运行随机生成（读系统熵），故公钥指纹/签名十六进制
//     每次不同；其余输出字节级确定。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	ap "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg"
)

// 演示用的工具身份与参数（固定，保证输出可断言）。
const (
	toolName    = "wasm-checksum-tool"
	toolVersion = "1.0.0"
	toolArgs    = `{"expression":"1+1"}`
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "wasm-tool-supply-chain: %v\n", err)
		os.Exit(1)
	}
}

// run 执行全链路演示。任一步骤的预期结果不成立即返回错误（非零退出），
// 不打印「假装成功」的结论。
func run() error {
	ctx := context.Background()

	printHeader()

	// ============================================================
	// [1/7] 手工组装最小有效 WASM 工具模块
	// ============================================================
	step(1, 7, "手工组装最小有效 WASM 工具模块（字节级，无外部工具链）")
	artifact := buildWASMToolModule()
	fmt.Printf("  范式出处: agentprimordia/wasm/runtime_e2e_test.go validTestWASM()\n")
	fmt.Printf("  大小: %d 字节 | sha256(前8字节): %s\n", len(artifact), sha256Hex(artifact))
	fmt.Printf("  导出: %s / %s / %s / %s\n", exportMemoryName, exportAllocName, exportExecute, exportFreeName)
	fmt.Printf("  调用协议: %s(ptr,len) -> (ret_ptr,ret_len) + %s + %s\n",
		exportExecute, exportAllocName, exportMemoryName)
	fmt.Printf("  行为: 对宿主写入线性内存的参数逐字节求和(mod 256)，两位小写十六进制写回载荷\n")

	// ============================================================
	// [2/7] 开发者签名：ed25519 over SHA-256(工件)
	// ============================================================
	step(2, 7, "开发者签名（ed25519 over SHA-256(工件)）")
	priv, pub, err := ap.WASMGenerateKeyPair()
	if err != nil {
		return fmt.Errorf("生成密钥对失败: %w", err)
	}
	sig, _, err := ap.WASMSignWASM(artifact, priv)
	if err != nil {
		return fmt.Errorf("签名工件失败: %w", err)
	}
	fmt.Printf("  算法: Ed25519（公钥 %d 字节 / 签名 %d 字节 / 哈希 SHA-256）\n", len(pub), len(sig))
	fmt.Printf("  公钥指纹: %s（SHA-256(公钥) 前 4 字节十六进制）\n", ap.WASMKeyFingerprint(pub))
	fmt.Printf("  签名(前16字节): %s...\n", shortHex(sig, 16))
	if err := ap.WASMVerifySignature(artifact, sig, pub); err != nil {
		return fmt.Errorf("原始工件验签应通过: %w", err)
	}
	fmt.Printf("  验签 · 原始工件: 通过\n")
	tampered := tamperArtifact(artifact)
	tamperErr := ap.WASMVerifySignature(tampered, sig, pub)
	if tamperErr == nil {
		return fmt.Errorf("篡改 1 字节后验签应失败（安全回归）")
	}
	fmt.Printf("  验签 · 篡改 1 字节(偏移 %d): 拒绝（%v）\n", tamperOffset(artifact), tamperErr)

	// ============================================================
	// [3/7] 公钥钉扎（fail-closed）
	// ============================================================
	step(3, 7, "公钥钉扎（fail-closed：空钉扎集合 = 拒绝一切）")
	emptyGate := newPinnedGate()
	fmt.Printf("  空钉扎集合: %d 个受信公钥 → %v\n",
		emptyGate.count(), errText(emptyGate.Verify(artifact, sig, pub)))
	gate := newPinnedGate(pub)
	fmt.Printf("  钉扎集合: %d 个受信公钥（指纹 %s）\n", gate.count(), ap.WASMKeyFingerprint(pub))
	if err := gate.Verify(artifact, sig, pub); err != nil {
		return fmt.Errorf("已签名且已钉扎的工件应通过验签门: %w", err)
	}
	fmt.Printf("  验签门 · 签名有效 + 公钥已钉扎: 通过\n")

	// ============================================================
	// [4/7] 拒绝分支 A：未签名工件
	// ============================================================
	step(4, 7, "拒绝分支 A · 未签名工件（A6 签名前置）")
	if err := registerUnsigned(ctx, artifact); err != nil {
		fmt.Printf("  ✗ 拒绝: %v\n", err)
	} else {
		return fmt.Errorf("未签名工件竟然注册成功（安全回归）")
	}

	// ============================================================
	// [5/7] 拒绝分支 B：签名公钥未钉扎
	// ============================================================
	step(5, 7, "拒绝分支 B · 签名公钥未钉扎")
	roguePriv, roguePub, err := ap.WASMGenerateKeyPair()
	if err != nil {
		return fmt.Errorf("生成非受信密钥对失败: %w", err)
	}
	rogueSig, _, err := ap.WASMSignWASM(artifact, roguePriv)
	if err != nil {
		return fmt.Errorf("非受信方签名失败: %w", err)
	}
	if err := gate.Verify(artifact, rogueSig, roguePub); err != nil {
		fmt.Printf("  ✗ 拒绝: %v\n", err)
		fmt.Printf("    （签名本身有效——只是签名方指纹 %s 不在钉扎集合中）\n", ap.WASMKeyFingerprint(roguePub))
	} else {
		return fmt.Errorf("未钉扎公钥的签名竟然通过验签门（安全回归）")
	}

	// ============================================================
	// [6/7] 拒绝分支 C：非 WASM 工件
	// ============================================================
	step(6, 7, "拒绝分支 C · 非 WASM 工件（shell 脚本，已签名且公钥已钉扎）")
	if err := registerNonWASM(ctx, gate, priv, pub); err != nil {
		fmt.Printf("  ✗ 拒绝: %v\n", err)
	} else {
		return fmt.Errorf("非 WASM 工件竟然注册成功（安全回归）")
	}
	fmt.Printf("    （验签门已通过——签名对 shell 脚本同样有效；拦下它的是沙箱编译，宿主零执行）\n")

	// ============================================================
	// [7/7] 篡改工件被拒 + 验签门放行 → 注册 → 沙箱执行
	// ============================================================
	step(7, 7, "篡改工件被拒 + 验签门放行 → 注册 → agent 在沙箱中真实调用")
	if err := gate.Verify(tampered, sig, pub); err != nil {
		fmt.Printf("  ✗ 篡改工件被验签门拒绝: %v\n", err)
	} else {
		return fmt.Errorf("篡改工件竟然通过验签门（安全回归）")
	}
	// 佐证：同一份被篡改的字节仍是合法 WASM（沙箱能编译）——安全判据是签名，
	// 不是字节形态。用独立探针沙箱证明，不污染后面的成功路径。
	if err := probeTamperedCompiles(tampered); err != nil {
		return fmt.Errorf("探针沙箱验证失败: %w", err)
	}

	if err := runSuccessPath(ctx, gate, artifact, sig, pub); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("=== 全链路通过：签名 → 钉扎 → 注册 → 沙箱执行 ===")
	return nil
}

// runSuccessPath 验签门放行后的成功分支：注册 → AsTool 桥 → tools.Registry →
// agent 运行 → 取回结果并与宿主侧独立复算对账。
func runSuccessPath(ctx context.Context, gate *pinnedGate, artifact, sig, pub []byte) error {
	if err := gate.Verify(artifact, sig, pub); err != nil {
		return fmt.Errorf("原始工件应通过验签门: %w", err)
	}
	fmt.Printf("  ✓ 验签门放行: 签名有效 + 公钥已钉扎 + 工件 sha256=%s\n", sha256Hex(artifact))

	sandbox := newSandbox()
	defer func() {
		if err := sandbox.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "警告: 关闭沙箱失败: %v\n", err)
		}
	}()
	adapter := ap.NewWASMToolAdapter(sandbox)

	meta := toolMetadata(sig, pub)
	if err := adapter.RegisterTool(ctx, meta, artifact); err != nil {
		return fmt.Errorf("注册 WASM 工具失败: %w", err)
	}
	fmt.Printf("  ✓ 注册成功: name=%s version=%s execute_func=%s\n", meta.Name, meta.Version, meta.ExecuteFunc)

	// 沙箱事实（数字取自 wasm.DefaultSandboxConfig，不以印象为准）
	cfg := ap.DefaultWASMSandboxConfig()
	fmt.Printf("  ✓ 沙箱上限: 内存 %d 页(%dKB) / 执行超时 %v / 导入白名单 %v\n",
		cfg.MaxMemoryPages, cfg.MaxMemoryPages*64, cfg.MaxExecutionTime, cfg.AllowedImports)

	// AsTool 桥 → tools.Registry（ReActAgent toolkit 调用路径）
	tool, err := adapter.AsTool(toolName)
	if err != nil {
		return fmt.Errorf("AsTool 失败: %w", err)
	}
	registry := ap.NewToolRegistry()
	if err := registry.Register(tool); err != nil {
		return fmt.Errorf("注册进 tools.Registry 失败: %w", err)
	}
	fmt.Printf("  ✓ 注册进 tools.Registry（AsTool 桥，ReActAgent toolkit 调用路径）\n")

	// 导入段审计（A7）：枚举已加载模块的导入函数
	for _, mod := range sandbox.ListModules() {
		imports, err := sandbox.ImportedFunctions(mod)
		if err != nil {
			return fmt.Errorf("枚举 %s 导入段失败: %w", mod, err)
		}
		fmt.Printf("  ✓ 导入段审计(A7): %s 导入 %d 项宿主函数 %v\n", mod, len(imports), imports)
	}

	// ① agent 运行：证明运行时真的发起了这次工具调用
	provider := newScriptedProvider(toolName, json.RawMessage(toolArgs))
	resp, err := runAgentWithWASMTool(toolName, registry, provider, "请用已注册的 WASM 工具计算 1+1")
	if err != nil {
		return fmt.Errorf("agent 运行失败: %w", err)
	}
	if resp.Metrics.TotalTools < 1 {
		return fmt.Errorf("agent 未发起工具调用（TotalTools=%d）", resp.Metrics.TotalTools)
	}
	fmt.Printf("  ✓ agent 运行（零凭据脚本化 provider，无网络调用）: tool_calls=%d turns=%d\n",
		resp.Metrics.TotalTools, resp.Metrics.TotalTurns)

	// ② 取回结果：同一 tools.Tool 经注册表直接执行，打印沙箱输出
	registered, ok := registry.Get(toolName)
	if !ok {
		return fmt.Errorf("tools.Registry 中找不到 %s", toolName)
	}
	result, err := registered.Execute(ctx, json.RawMessage(toolArgs))
	if err != nil {
		return fmt.Errorf("经注册表执行 WASM 工具失败: %w", err)
	}
	if result.IsError {
		return fmt.Errorf("WASM 工具执行报错: %s", result.Content)
	}
	fmt.Printf("  ✓ 沙箱执行结果: %s\n", result.Content)

	// ③ 独立复算对账：宿主侧重算同一校验和，与沙箱内计算逐位比对
	input, err := toolInputJSON(toolName, json.RawMessage(toolArgs))
	if err != nil {
		return fmt.Errorf("构造对账输入失败: %w", err)
	}
	want := inputChecksumHex(input)
	if got := checksumIn(result.Content); got != want {
		return fmt.Errorf("校验和对账失败: 沙箱=%q 宿主复算=%q", got, want)
	}
	fmt.Printf("  ✓ 独立复算对账: 沙箱内计算与宿主复算一致（input-checksum=%s，输入 %d 字节）\n",
		want, len(input))
	return nil
}

// newSandbox 构造 wazero 沙箱（pkg: ap.NewWASMSandbox + ap.DefaultWASMSandboxConfig）。
func newSandbox() *ap.WASMSandbox {
	return ap.NewWASMSandbox(ap.DefaultWASMSandboxConfig())
}

// toolMetadata 构造 WASM 工具元数据（含签名/公钥，A6 签名前置的必填项）。
func toolMetadata(sig, pub []byte) ap.WASMToolMetadata {
	return ap.WASMToolMetadata{
		Name:        toolName,
		Description: "WASM 校验和演示工具（沙箱内计算，结果经线性内存回传）",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"expression":{"type":"string"}}}`),
		ExecuteFunc: exportExecute,
		Version:     toolVersion,
		Signature:   sig,
		PublicKey:   pub,
	}
}

// ===== 输出辅助 =====

// printHeader 打印 demo 定位与诚实边界。
func printHeader() {
	fmt.Println("=== WASM 工具安全供应链 Demo（v7.7 副线）===")
	fmt.Println()
	fmt.Println("定位（先读这段）：本 demo 演示「开发者自带 WASM 工件」的安全供应链——")
	fmt.Println("开发者（本 demo 代码）手工组装 WASM 字节 → ed25519 签名 → 公钥钉扎 →")
	fmt.Println("验签门注册 → wazero 沙箱内执行。")
	fmt.Println("诚实边界：工具生成器无生产实现（internal/tools/lifecycle 的全部 Generator")
	fmt.Println("实现均为测试替身，生成质量属研究面）——本 demo 不是「agent 自己写工具」。")
	fmt.Println()
}

// step 打印步骤标题。
func step(n, total int, title string) {
	fmt.Printf("[%d/%d] %s\n", n, total, title)
}

// errText 取错误文本（nil 时返回 <nil>）。
func errText(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

// shortHex 取字节的十六进制前 n 字节。
func shortHex(b []byte, n int) string {
	if len(b) < n {
		n = len(b)
	}
	return fmt.Sprintf("%x", b[:n])
}

// checksumIn 从工具输出内容中提取 input-checksum=XX 的值（无则返回空串）。
func checksumIn(content string) string {
	const key = "input-checksum="
	idx := indexOf(content, key)
	if idx < 0 {
		return ""
	}
	rest := content[idx+len(key):]
	if len(rest) < 2 {
		return ""
	}
	return rest[:2]
}

// indexOf 子串查找（strings.Index 的薄封装，避免为一个小函数引入 import 噪音）。
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
