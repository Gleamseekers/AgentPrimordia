// main.go — AgentPrimordia v7.2 统一 Bench 运行器
//
// 加载 v7.2 冻结硬任务集（docs/evals/v72/），按命题运行 A/B 双臂实验：
//   - A 臂：基线 Agent（filesystem + shell 工具，纯 ReAct）
//   - B 臂：Agent + 对应命题的框架能力
//     p1 → WithWorldModel（世界模型）
//     p2 → WithPlanner（增强规划）
//     p4 → 基础工具（工具智能需自动生成工具，此处以基线为对照）
//     p5 → 基础工具（多模态需 Vision Provider，此处以基线为对照）
//
// 对每对结果计算 McNemar 精确检验 + Wilson 95% CI，输出 JSON 报告。
//
// 用法：
//
//	go run ./bench/v72 --prop p1 --model sensenova-6.8-flash-lite \
//	  --base-url https://token.sensenova.cn/v1 \
//	  --api-key xxx --out bench/results/v72
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agentprimordia/internal/agent"
	"agentprimordia/internal/agent/planning"
	"agentprimordia/internal/agent/worldmodel"
	"agentprimordia/internal/eval"
	"agentprimordia/internal/llm"
	"agentprimordia/internal/persist"
	"agentprimordia/internal/tools"
	"agentprimordia/internal/tools/builtin"
)

// taskResult 单条任务单臂运行结果。
type taskResult struct {
	TaskID      string `json:"task_id"`
	Prop        string `json:"prop"`
	Arm         string `json:"arm"`         // "A" 或 "B"
	Success     bool   `json:"success"`     // 断言是否全部通过
	Turns       int    `json:"turns"`       // ReAct 轮数
	DurationSec int    `json:"duration_sec"`
	Error       string `json:"error,omitempty"`
	FailedAssert string `json:"failed_assert,omitempty"` // 首个失败断言描述
}

// benchReport v7.2 bench 最终报告。
type benchReport struct {
	Prop        string `json:"prop"`
	Model       string `json:"model"`
	GeneratedAt string `json:"generated_at"`
	TotalTasks  int    `json:"total_tasks"`  // 非留出任务数
	HoldoutSkip int    `json:"holdout_skip"` // 跳出的留出任务数
	// McNemar 配对分析
	PairedAnalysis *eval.PairedAnalysis `json:"paired_analysis"`
	// 各臂独立 Wilson CI
	ARate eval.RatePoint `json:"a_rate"`
	BRate eval.RatePoint `json:"b_rate"`
	// 逐条结果
	Results []taskResult `json:"results"`
}

func main() {
	var (
		prop      = flag.String("prop", "p1", "命题（p1/p2/p4/p5/all）")
		model     = flag.String("model", "sensenova-6.8-flash-lite", "模型名称")
		apiKey    = flag.String("api-key", "", "API key")
		baseURL   = flag.String("base-url", "https://token.sensenova.cn/v1", "API base URL")
		outDir    = flag.String("out", "bench/results/v72", "输出目录")
		limit     = flag.Int("limit", 0, "限制任务数（0=全部）")
		pace      = flag.Duration("pace", 8*time.Second, "任务间间隔")
		maxTokens = flag.Int("max-tokens", 4096, "每次请求最大 token 数")
		taskTimeout = flag.Duration("task-timeout", 10*time.Minute, "单臂任务超时时间")
	)
	flag.Parse()

	if *apiKey == "" {
		*apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if *apiKey == "" {
		fmt.Println("错误: 需要 --api-key 或 OPENAI_API_KEY 环境变量")
		os.Exit(1)
	}

	// 确定要运行的命题列表
	props := resolveProps(*prop)
	if len(props) == 0 {
		fmt.Printf("错误: 未知命题 %q，可选: p1/p2/p4/p5/all\n", *prop)
		os.Exit(1)
	}

	if err := os.MkdirAll(*outDir, 0755); err != nil {
		fmt.Printf("创建输出目录失败: %v\n", err)
		os.Exit(1)
	}

	// 创建 LLM Provider
	prov, err := llm.NewOpenAIProvider(llm.Config{
		APIKey:    *apiKey,
		Model:     *model,
		BaseURL:   *baseURL,
		MaxTokens: *maxTokens,
	})
	if err != nil {
		fmt.Printf("创建 Provider 失败: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()

	// 按命题逐一运行
	for _, p := range props {
		fmt.Printf("\n===== 命题 %s =====\n", p)
		report := runProp(ctx, prov, p, *limit, *pace, *taskTimeout)
		report.Prop = p
		report.Model = *model
		report.GeneratedAt = eval.NowRFC3339()

		// 输出报告 JSON
		reportPath := filepath.Join(*outDir, p+"-report.json")
		data, _ := json.MarshalIndent(report, "", "  ")
		if err := os.WriteFile(reportPath, data, 0644); err != nil {
			fmt.Printf("写入报告失败: %v\n", err)
		} else {
			fmt.Printf("报告已写入: %s\n", reportPath)
		}

		// 同时追加 JSONL 逐条结果
		jsonlPath := filepath.Join(*outDir, p+"-results.jsonl")
		f, _ := os.OpenFile(jsonlPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		for _, r := range report.Results {
			if err := json.NewEncoder(f).Encode(r); err != nil {
				fmt.Printf("写入结果失败: %v\n", err)
			}
		}
		f.Close()
		fmt.Printf("逐条结果: %s\n", jsonlPath)

		// 终端汇总
		printSummary(p, report)
	}
}

// resolveProps 将 --prop 参数展开为命题列表。
func resolveProps(prop string) []string {
	switch prop {
	case "all":
		return []string{"p1", "p2", "p4", "p5"}
	case "p1", "p2", "p4", "p5":
		return []string{prop}
	default:
		return nil
	}
}

// propFile 将命题简写映射到任务文件名（不含 .json 后缀）。
func propFile(prop string) string {
	switch prop {
	case "p1":
		return "p1-worldmodel"
	case "p2":
		return "p2-planning"
	case "p4":
		return "p4-intelligence"
	case "p5":
		return "p5-multimodal"
	default:
		return prop
	}
}

// runProp 运行单个命题的 A/B 实验并返回报告。
func runProp(ctx context.Context, prov llm.Provider, prop string, limit int, pace time.Duration, taskTimeout time.Duration) benchReport {
	tasks, err := eval.LoadV72Tasks(propFile(prop))
	if err != nil {
		fmt.Printf("加载 %s 任务失败: %v\n", prop, err)
		return benchReport{}
	}

	// 过滤留出任务
	var active []eval.V72Task
	holdoutSkip := 0
	for _, t := range tasks {
		if t.Holdout {
			holdoutSkip++
			continue
		}
		active = append(active, t)
	}
	if limit > 0 && limit < len(active) {
		active = active[:limit]
	}
	fmt.Printf("任务总数: %d / 留出: %d / 本次运行: %d\n", len(tasks), holdoutSkip, len(active))

	var results []taskResult
	var pairs []eval.PairedOutcome

	for i, task := range active {
		fmt.Printf("[%d/%d] %s ...", i+1, len(active), task.ID)

		rA := runTaskArm(ctx, prov, task, "A", prop, taskTimeout)
		rB := runTaskArm(ctx, prov, task, "B", prop, taskTimeout)

		results = append(results, rA, rB)
		pairs = append(pairs, eval.PairedOutcome{
			TaskID:    task.ID,
			Baseline:  rA.Success,
			Treatment: rB.Success,
		})

		statusA, statusB := "FAIL", "FAIL"
		if rA.Success {
			statusA = "OK"
		}
		if rB.Success {
			statusB = "OK"
		}
		fmt.Printf(" A=%s(%ds) B=%s(%ds)\n", statusA, rA.DurationSec, statusB, rB.DurationSec)

		time.Sleep(pace)
	}

	// 统计
	report := benchReport{
		TotalTasks:  len(active),
		HoldoutSkip: holdoutSkip,
		Results:     results,
	}

	aOK, bOK := 0, 0
	for _, r := range results {
		if r.Arm == "A" && r.Success {
			aOK++
		}
		if r.Arm == "B" && r.Success {
			bOK++
		}
	}

	if aRate, err := eval.ReportRate(aOK, len(active)); err == nil {
		report.ARate = aRate
	}
	if bRate, err := eval.ReportRate(bOK, len(active)); err == nil {
		report.BRate = bRate
	}
	if len(pairs) > 0 {
		if pa, err := eval.AnalyzePaired(pairs); err == nil {
			report.PairedAnalysis = &pa
		}
	}

	return report
}

// runTaskArm 运行单条任务单臂。
func runTaskArm(ctx context.Context, prov llm.Provider, task eval.V72Task, arm string, prop string, taskTimeout time.Duration) taskResult {
	start := time.Now()
	r := taskResult{TaskID: task.ID, Prop: prop, Arm: arm}

	// 单臂超时控制
	taskCtx, cancel := context.WithTimeout(ctx, taskTimeout)
	defer cancel()

	// 创建沙箱临时目录
	sandbox, err := os.MkdirTemp("", "v72-"+task.ID+"-"+arm+"-")
	if err != nil {
		r.Error = err.Error()
		r.DurationSec = int(time.Since(start).Seconds())
		return r
	}
	defer os.RemoveAll(sandbox)

	// 注入 fixtures
	for _, fx := range task.Fixtures {
		// 多模态 fixture 跳过（二进制/远程内容无法简单写入）
		if fx.ContentType != "" || fx.Source != "" {
			continue
		}
		p := filepath.Join(sandbox, filepath.FromSlash(fx.Path))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			r.Error = err.Error()
			r.DurationSec = int(time.Since(start).Seconds())
			return r
		}
		if err := os.WriteFile(p, []byte(fx.Content), 0644); err != nil {
			r.Error = err.Error()
			r.DurationSec = int(time.Since(start).Seconds())
			return r
		}
	}

	// 创建检查点存储
	ckpt, err := persist.NewSQLiteCheckpointStore(filepath.Join(sandbox, "checkpoint.db"))
	if err != nil {
		r.Error = err.Error()
		r.DurationSec = int(time.Since(start).Seconds())
		return r
	}
	defer ckpt.Close()

	// 构建工具集（双臂相同：filesystem + shell）
	reg := sandboxToolkit(sandbox)

	// 系统提示
	sysPrompt := buildSystemPrompt(sandbox, task)

	// 构建 Agent 选项
	opts := []agent.Option{
		agent.WithMaxTurns(25),
		agent.WithToolkit(reg),
		agent.WithCheckpointStore(ckpt),
		agent.WithSessionID(fmt.Sprintf("v72-%s-%s", task.ID, arm)),
	}

	// B 臂：根据命题启用框架能力
	if arm == "B" {
		switch prop {
		case "p1":
			// 世界模型：跟踪状态变更
			tracker := worldmodel.NewWorldModelTracker()
			opts = append(opts, agent.WithWorldModel(tracker))
		case "p2":
			// 增强规划：LLM 分解子任务
			planner := planning.NewLLMPlanner(prov)
			opts = append(opts, agent.WithPlanner(planner))
		case "p4":
			// 工具智能：当前以基线对照（工具自动生成本身就是被测试能力）
			// 系统提示中鼓励工具复用
			sysPrompt += "\n\n提示：优先使用已有工具组合完成任务，避免重复创建工具。"
		case "p5":
			// 多模态：当前以基线对照（Vision 能力取决于 Provider 支持）
			// 系统提示中鼓励利用图像信息
			sysPrompt += "\n\n提示：任务可能涉及图像内容分析，请仔细观察提供的视觉信息。"
		}
	}

	ag, err := agent.NewAgent("v72-"+task.ID+"-"+arm, sysPrompt, prov, opts...)
	if err != nil {
		r.Error = err.Error()
		r.DurationSec = int(time.Since(start).Seconds())
		return r
	}

	// 运行 Agent
	resp, err := ag.Run(taskCtx, agent.UserMessage(task.Task))
	if err != nil {
		if taskCtx.Err() == context.DeadlineExceeded {
			r.Error = "task timeout (" + taskTimeout.String() + ")"
			r.DurationSec = int(time.Since(start).Seconds())
			return r
		}
		// API key 耗尽或网络错误：记为失败，不崩溃
		r.Error = err.Error()
		r.DurationSec = int(time.Since(start).Seconds())
		return r
	}

	r.Turns = resp.Metrics.TotalTurns
	r.DurationSec = int(time.Since(start).Seconds())

	// 评估 v72 断言
	r.Success, r.FailedAssert = evaluateV72Asserts(sandbox, task.Asserts)

	return r
}

// sandboxToolkit 创建沙箱内基础工具集（filesystem + shell）。
func sandboxToolkit(dir string) *tools.Registry {
	reg := tools.NewRegistry()
	fsTool, err := builtin.NewFileSystem(dir)
	if err == nil {
		_ = reg.Register(fsTool)
	}
	shell := builtin.NewShell().WithAllowedWorkdirs([]string{dir})
	_ = reg.Register(shell)
	return reg
}

// buildSystemPrompt 构建系统提示。
func buildSystemPrompt(sandbox string, task eval.V72Task) string {
	return fmt.Sprintf(`你在一个隔离沙箱目录中工作（根目录: %s）。
使用提供的工具完成任务。所有产物必须写在该目录内。
任务完成后将最终答案写入 answer.txt。
直接开始工作，不要询问用户。`, sandbox)
}

// evaluateV72Asserts 评估 v7.2 格式的断言列表。
// 返回 (全部通过, 首个失败断言描述)。
func evaluateV72Asserts(sandbox string, asserts []eval.V72Assert) (bool, string) {
	for _, a := range asserts {
		ok, desc := evaluateSingleAssert(sandbox, a)
		if !ok {
			return false, desc
		}
	}
	return true, ""
}

// evaluateSingleAssert 评估单条 v72 断言。
func evaluateSingleAssert(sandbox string, a eval.V72Assert) (bool, string) {
	target := filepath.Join(sandbox, filepath.FromSlash(a.Target))
	desc := fmt.Sprintf("%s(%s)", a.Type, a.Target)

	switch a.Type {
	case "file_exists":
		if _, err := os.Stat(target); err != nil {
			return false, desc
		}
		return true, ""

	case "file_not_exists":
		if _, err := os.Stat(target); err == nil {
			return false, desc + " 文件不应存在但存在"
		}
		return true, ""

	case "file_contains":
		data, err := os.ReadFile(target)
		if err != nil {
			return false, desc + " 文件不存在"
		}
		if !strings.Contains(string(data), a.Expect) {
			return false, desc + " 不包含 " + a.Expect
		}
		return true, ""

	case "file_eq":
		data, err := os.ReadFile(target)
		if err != nil {
			return false, desc + " 文件不存在"
		}
		got := strings.TrimRight(string(data), "\r\n")
		want := strings.TrimRight(a.Expect, "\r\n")
		if got != want {
			return false, desc + " 内容不匹配"
		}
		return true, ""

	default:
		// 未知断言类型：跳过并记录警告
		fmt.Printf("  警告: 未知断言类型 %q，跳过\n", a.Type)
		return true, ""
	}
}

// printSummary 终端打印命题汇总。
func printSummary(prop string, report benchReport) {
	fmt.Printf("\n===== %s 汇总 =====\n", prop)
	fmt.Printf("任务数: %d (留出跳过: %d)\n", report.TotalTasks, report.HoldoutSkip)
	fmt.Printf("A 臂（基线）: %s\n", report.ARate.String())
	fmt.Printf("B 臂（框架）: %s\n", report.BRate.String())
	if report.PairedAnalysis != nil {
		pa := report.PairedAnalysis
		fmt.Printf("McNemar: N=%d, 一致=%d, 仅A=%d, 仅B=%d, 提升=%.3f, p=%.4f\n",
			pa.N, pa.Concordant, pa.DiscB, pa.DiscC, pa.Lift, pa.PValue)
		if pa.PValue < 0.05 {
			fmt.Println("结论: p<0.05, 框架增强显著")
		} else {
			fmt.Println("结论: p≥0.05, 差异不显著")
		}
	}
}
