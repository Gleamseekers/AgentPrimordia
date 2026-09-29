// tool-intelligence 示例：v7.1 统一工具智能系统演示
//
// 演示内容：
//   - ToolIntelligence 统一入口的构造与组装
//   - InMemoryProfiler 工具性能画像
//   - DataDrivenTuner 数据驱动参数调优
//   - HistorySelector 基于成功率的工具选择
//   - TraceGapDetector 失败轨迹缺口检测
//   - LifecycleCreator 自动工具生成
//   - ap.ToolCatalog + ap.TaskMatcher 工具目录与任务匹配
//   - IntelligenceHook 桥接 ReAct 循环
//   - INV-0 合规注册：RegisteringCreator 验签门 + wasm 沙箱执行通道
//     （生产装配范例见 cmd/ap/tool_forge.go）
//
// 运行方式：go run ./ecosystem/examples/tool-intelligence/
package main

import (
	"sort"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	ap "github.com/Gleamseekers/AgentPrimordia/pkg"
)

func main() {
	fmt.Println("=== AgentPrimordia v7.1 统一工具智能系统演示 ===")
	fmt.Println()

	ctx := context.Background()

	// ============================================================
	// 第一步：构造各子组件
	// ============================================================

	// 1. 性能画像器：记录每次工具调用的成功率、延迟、token 统计
	profiler := ap.NewInMemoryProfiler()

	// 2. 数据驱动调优器：基于画像提出参数调整建议（低成功率→重试，高延迟→增大超时）
	tuner := ap.NewDataDrivenTuner()

	// 3. 历史选择器：根据历史成功率从候选工具中选最优
	selector := ap.NewHistorySelector()

	// 4. 缺口检测器：分析失败轨迹，按错误模式聚类发现缺失工具
	detector := ap.NewTraceGapDetector()

	// 5. 工具生成器：根据缺口候选自动生成 shell 脚本工具
	creator := ap.NewLifecycleCreator()

	// 6. 统一入口：组装所有子组件
	ti := ap.NewToolIntelligence(detector, creator, profiler, tuner, selector)
	_ = ti // 统一入口已就绪，后续各组件单独演示以便输出清晰

	// 7. 复用层：工具目录 + 任务匹配器
	catalog := ap.NewToolCatalog()
	matcher := ap.NewTaskMatcher()

	// 8. 智能 Hook：桥接 ReAct 循环，工具调用后画像记录，轮次结束后缺口检测
	hook := ap.NewIntelligenceHook(profiler, detector, creator)

	// 9. DemoProvider：本示例不发起真实 API 调用，仅用于展示 LLM 上下文可用
	// （MockLLM 构造器签名依赖 *testing.T，不适合公共 API 导出，故用无需
	// API key 的 DemoProvider 等价演示）
	mockLLM := ap.NewDemoProvider()
	_ = mockLLM

	fmt.Println("✓ 所有组件构造完成")
	fmt.Println("  - Profiler: InMemoryProfiler")
	fmt.Println("  - Tuner: DataDrivenTuner (成功率阈值=70%, 延迟阈值=5s)")
	fmt.Println("  - Selector: HistorySelector")
	fmt.Println("  - Detector: TraceGapDetector")
	fmt.Println("  - Creator: LifecycleCreator")
	fmt.Println("  - Hook: IntelligenceHook")
	fmt.Println("  - Catalog: reuse.ToolCatalog")
	fmt.Println("  - Matcher: reuse.TaskMatcher")
	fmt.Println()

	// ============================================================
	// 第二步：注册工具到目录，演示任务匹配
	// ============================================================

	fmt.Println("--- 工具目录与任务匹配 ---")
	catalog.Register(ap.ToolEntry{
		ID: "file_search", Name: "file_search",
		Description: "在文件系统中搜索匹配模式的文件", Domain: "filesystem",
	})
	catalog.Register(ap.ToolEntry{
		ID: "http_get", Name: "http_get",
		Description: "发送 HTTP GET 请求并返回响应内容", Domain: "network",
	})
	catalog.Register(ap.ToolEntry{
		ID: "csv_parse", Name: "csv_parse",
		Description: "解析 CSV 格式数据并提取字段", Domain: "data",
	})

	tools := catalog.List()
	fmt.Printf("  已注册工具: %d 个\n", len(tools))

	// 任务匹配：从目录中选择最匹配任务的工具
	task := "解析 CSV 数据并提取关键字段"
	best := matcher.Match(task, tools)
	fmt.Printf("  任务: %q\n", task)
	fmt.Printf("  最佳匹配: %s (%s)\n", best.Name, best.Description)
	fmt.Println()

	// ============================================================
	// 第三步：模拟工具调用，演示性能画像
	// ============================================================

	fmt.Println("--- 性能画像 ---")

	// 模拟 file_read 工具的多次调用（混合成功与失败）
	simulateToolUsage(ctx, profiler, "file_read", []toolCall{
		{success: true, duration: 50 * time.Millisecond, tokens: 100},
		{success: true, duration: 80 * time.Millisecond, tokens: 120},
		{success: false, duration: 200 * time.Millisecond, tokens: 50},
		{success: true, duration: 60 * time.Millisecond, tokens: 110},
		{success: true, duration: 70 * time.Millisecond, tokens: 90},
	})

	// 模拟 slow_api 工具（高延迟场景）
	simulateToolUsage(ctx, profiler, "slow_api", []toolCall{
		{success: true, duration: 3 * time.Second, tokens: 500},
		{success: true, duration: 4 * time.Second, tokens: 600},
		{success: true, duration: 6 * time.Second, tokens: 550},
	})

	// 查看画像
	fileProfile, _ := profiler.Profile(ctx, "file_read")
	fmt.Printf("  file_read: 调用 %d 次 | 成功率 %.0f%% | 平均延迟 %v | P95 %v\n",
		fileProfile.TotalCalls, fileProfile.SuccessRate*100,
		fileProfile.AvgDuration.Round(time.Millisecond), fileProfile.P95Duration.Round(time.Millisecond))

	apiProfile, _ := profiler.Profile(ctx, "slow_api")
	fmt.Printf("  slow_api:  调用 %d 次 | 成功率 %.0f%% | 平均延迟 %v | P95 %v\n",
		apiProfile.TotalCalls, apiProfile.SuccessRate*100,
		apiProfile.AvgDuration.Round(time.Millisecond), apiProfile.P95Duration.Round(time.Millisecond))
	fmt.Println()

	// ============================================================
	// 第四步：数据驱动调优建议
	// ============================================================

	fmt.Println("--- 调优建议 ---")

	// file_read 成功率 80% > 阈值 70%，延迟正常 → 无需调优
	sug1, _ := tuner.SuggestTuning(ctx, "file_read", fileProfile)
	if sug1 != nil {
		fmt.Printf("  file_read: 建议 %s=%s (原因: %s)\n", sug1.Parameter, sug1.SuggestedVal, sug1.Reason)
	} else {
		fmt.Println("  file_read: 表现良好，无需调优")
	}

	// slow_api 延迟超限 → 建议增大超时
	sug2, _ := tuner.SuggestTuning(ctx, "slow_api", apiProfile)
	if sug2 != nil {
		fmt.Printf("  slow_api:  建议 %s=%s (置信度 %.0f%%, 原因: %s)\n",
			sug2.Parameter, sug2.SuggestedVal, sug2.Confidence*100, sug2.Reason)
	} else {
		fmt.Println("  slow_api:  表现良好，无需调优")
	}
	fmt.Println()

	// ============================================================
	// 第五步：基于历史成功率的工具选择
	// ============================================================

	fmt.Println("--- 工具选择 ---")

	// 记录候选工具的历史调用结果
	_ = selector.RecordOutcome(ctx, "bash_exec", true)
	_ = selector.RecordOutcome(ctx, "bash_exec", true)
	_ = selector.RecordOutcome(ctx, "bash_exec", false)
	_ = selector.RecordOutcome(ctx, "shell_cmd", true)
	_ = selector.RecordOutcome(ctx, "shell_cmd", false)
	_ = selector.RecordOutcome(ctx, "shell_cmd", false)
	_ = selector.RecordOutcome(ctx, "python_exec", true)
	_ = selector.RecordOutcome(ctx, "python_exec", true)

	// 从候选中选择最优
	candidates := []string{"bash_exec", "shell_cmd", "python_exec"}
	chosen, _ := selector.Select(ctx, "执行系统命令", candidates)
	fmt.Printf("  候选: %v\n", candidates)
	fmt.Printf("  选中: %s（历史成功率最高）\n", chosen)
	fmt.Println()

	// ============================================================
	// 第六步：缺口检测与自动工具生成
	// ============================================================

	fmt.Println("--- 缺口检测与工具生成 ---")

	// 模拟包含多种失败模式的调用轨迹
	trace := []ap.ToolCallRecord{
		{ToolName: "file_read", Args: "/data/report.csv", Error: "parse error: invalid CSV format",
			Duration: 10 * time.Millisecond, Success: false, Timestamp: time.Now()},
		{ToolName: "file_read", Args: "/data/log.txt", Error: "parse error: unsupported log format",
			Duration: 8 * time.Millisecond, Success: false, Timestamp: time.Now()},
		{ToolName: "web_fetch", Args: "https://api.example.com", Error: "connection refused",
			Duration: 5 * time.Second, Success: false, Timestamp: time.Now()},
		{ToolName: "file_read", Args: "/etc/config", Error: "permission denied",
			Duration: 2 * time.Millisecond, Success: false, Timestamp: time.Now()},
		{ToolName: "data_proc", Args: "input.json", Result: "ok",
			Duration: 100 * time.Millisecond, Success: true, Timestamp: time.Now()},
	}

	// 检测缺口
	gaps, _ := detector.Detect(ctx, trace)
	fmt.Printf("  轨迹长度: %d 条记录 | 检测到缺口: %d 个\n", len(trace), len(gaps))
	for _, gap := range gaps {
		fmt.Printf("  - 缺口: %s (出现 %d 次, 样本错误: %s)\n", gap.Key, gap.Count, gap.SampleError)
	}

	// 自动为每个缺口生成工具
	fmt.Println()
	fmt.Println("  自动工具生成:")
	for _, gap := range gaps {
		artifact, err := creator.Create(ctx, gap)
		if err != nil {
			fmt.Printf("  - %s: 生成失败 (%v)\n", gap.Key, err)
			continue
		}
		fmt.Printf("  - %s → %s (SHA256: %s...)\n",
			gap.Key, artifact.ID, artifact.ArtifactSHA[:12])
	}
	fmt.Println()

	// ============================================================
	// 第七步：IntelligenceHook 桥接演示
	// ============================================================

	fmt.Println("--- IntelligenceHook 桥接 ---")

	// 模拟 ReAct 循环中的工具调用
	hook.AfterToolCall(ctx, "file_read", "/data.csv", "", fmt.Errorf("parse error: invalid format"), 15*time.Millisecond)
	hook.AfterToolCall(ctx, "file_read", "/data.json", "ok", nil, 20*time.Millisecond)
	hook.AfterToolCall(ctx, "web_fetch", "https://example.com", "", fmt.Errorf("connection refused"), 3*time.Second)

	fmt.Printf("  当前轨迹长度: %d\n", hook.TraceLength())

	// 模拟轮次结束：触发缺口检测 + 自动工具生成
	fmt.Println("  轮次结束 → 触发缺口检测...")
	hook.OnTurnEnd(ctx)
	fmt.Println("  ✓ Hook 已完成轮次收尾（缺口已检测并尝试自动创建工具）")
	fmt.Printf("  轮次后轨迹长度: %d（已清空，等待下一轮）\n", hook.TraceLength())
	fmt.Println()

	// ============================================================
	// 第八步：INV-0 合规注册演示（RegisteringCreator 验签门）
	//
	// agent 生成工具的唯一合法链路：验签（ed25519 + 钉扎公钥）→ 注册
	// （0644 数据落盘）→ 执行（仅 wasm 沙箱）。未签名工件一律拒绝；
	// 宿主进程零写入可执行代码、零加载 agent 生成代码。
	// ============================================================

	fmt.Println("--- INV-0 合规注册（RegisteringCreator）---")

	reg := ap.NewToolRegistry()
	// 工件落盘用临时目录，避免污染当前工作目录（AGENTS.md §5 纪律）。
	forgeDir, dirErr := os.MkdirTemp("", "ap-tool-forge-*")
	if dirErr != nil {
		fmt.Printf("  ✗ 创建临时目录失败: %v\n", dirErr)
		forgeDir = "."
	} else {
		defer os.RemoveAll(forgeDir)
	}
	priv, pub, keyErr := ap.WASMGenerateKeyPair()
	if keyErr != nil {
		fmt.Printf("  ✗ 生成密钥对失败: %v\n", keyErr)
	} else {
		// 受信生成方：工件 + ed25519 签名（生产上由签名服务/CI 完成）
		scriptArtifact := []byte("#!/bin/sh\necho demo\n")
		sum := sha256.Sum256(scriptArtifact)
		sig, _, signErr := ap.WASMSignWASM(scriptArtifact, priv)
		if signErr != nil {
			fmt.Printf("  ✗ 签名失败: %v\n", signErr)
		}

		// 场景 1：未签名工件 → 拒绝注册（fail-closed）
		unsigned := &ap.ToolArtifact{
			ID: "demo-unsigned", Name: "demo_unsigned", Description: "未签名演示",
			ArtifactSHA: hex.EncodeToString(sum[:]), Artifact: scriptArtifact,
		}
		refuseCreator := ap.NewRegisteringCreator(&demoArtifactCreator{art: unsigned}, reg, forgeDir)
		if _, err := refuseCreator.Create(ctx, ap.GapCandidate{Key: "demo_unsigned"}); err != nil {
			fmt.Println("  ✓ 未签名工件被拒绝注册（fail-closed，符合 INV-0）")
		} else {
			fmt.Println("  ✗ 未签名工件竟然注册成功（安全回归！）")
		}

		// 场景 2：已签名工件 → 注册放行；执行通道仅 wasm 沙箱
		signed := &ap.ToolArtifact{
			ID: "demo-signed", Name: "demo_signed", Description: "已签名演示",
			ArtifactSHA: hex.EncodeToString(sum[:]), Artifact: scriptArtifact,
			Signature:   sig, PublicKey: pub,
		}
		acceptCreator := ap.NewRegisteringCreator(&demoArtifactCreator{art: signed}, reg, forgeDir).
			WithVerifier(&demoPinnedVerifier{pub: pub})
		if _, err := acceptCreator.Create(ctx, ap.GapCandidate{Key: "demo_signed"}); err != nil {
			fmt.Printf("  ✗ 已签名工件注册失败: %v\n", err)
		} else if _, ok := reg.Get("demo_signed"); ok {
			fmt.Println("  ✓ 已签名工件注册成功（验签门放行）")
			fmt.Println("  ✓ 执行通道：未注入 wasm executor 时拒绝宿主执行")
			fmt.Println("    （生产装配：cmd/ap/tool_forge.go 绑定 WASMToolAdapter）")
		}
	}
	fmt.Println()

	// ============================================================
	// 汇总
	// ============================================================

	allProfiles, _ := profiler.AllProfiles(ctx)
	fmt.Println("--- 系统状态汇总 ---")
	fmt.Printf("  Profiler 追踪工具数: %d\n", len(allProfiles))
	// AllProfiles 返回 map；示例层按键排序输出，保证演示确定可复现。
	profileNames := make([]string, 0, len(allProfiles))
	for name := range allProfiles {
		profileNames = append(profileNames, name)
	}
	sort.Strings(profileNames)
	for _, name := range profileNames {
		p := allProfiles[name]
		fmt.Printf("    %s: %d 次调用, 成功率 %.0f%%\n", name, p.TotalCalls, p.SuccessRate*100)
	}
	fmt.Printf("  Selector 统计工具数: 3 (bash_exec, shell_cmd, python_exec)\n")
	fmt.Printf("  Catalog 注册工具数: %d\n", len(catalog.List()))
	fmt.Println()
	fmt.Println("=== 演示完成 ===")
}

// toolCall 模拟调用参数
type toolCall struct {
	success  bool
	duration time.Duration
	tokens   int
}

// simulateToolUsage 模拟工具调用并记录到 profiler
func simulateToolUsage(ctx context.Context, profiler ap.ToolProfiler, name string, calls []toolCall) {
	for _, c := range calls {
		_ = profiler.Record(ctx, ap.IntelligenceToolUsageRecord{
			ToolName: name,
			Success:  c.success,
			Duration: c.duration,
			Tokens:   c.tokens,
		})
	}
}

// demoArtifactCreator 返回预设工件的演示生成器。
type demoArtifactCreator struct{ art *ap.ToolArtifact }

func (c *demoArtifactCreator) Create(_ context.Context, _ ap.GapCandidate) (*ap.ToolArtifact, error) {
	return c.art, nil
}

// demoPinnedVerifier 演示用钉扎公钥验签器（生产见 cmd/ap/tool_forge.go 的
// pinnedArtifactVerifier：多钥钉扎 + 指纹披露）。
type demoPinnedVerifier struct{ pub []byte }

func (v *demoPinnedVerifier) VerifyArtifact(art *ap.ToolArtifact) error {
	if len(art.Signature) == 0 || len(art.PublicKey) == 0 {
		return fmt.Errorf("工件缺少签名/公钥")
	}
	if string(art.PublicKey) != string(v.pub) {
		return fmt.Errorf("签名公钥未钉扎")
	}
	return ap.WASMVerifySignature(art.Artifact, art.Signature, art.PublicKey)
}
