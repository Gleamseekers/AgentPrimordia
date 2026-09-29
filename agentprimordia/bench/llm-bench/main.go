// main.go — AgentPrimordia 真实 LLM 跑分工具（v3.5-2）
//
// 对真实 harness 基准集（60 条编码任务）跑真实 LLM Provider，
// 产出含 成功率/成本/耗时/恢复率 的基准报告，并作为版本门禁：
//   - 通过率 ≥ max(基线, 阈值) 才达标（分数只升不降）
//   - --update-baseline 将本次报告写入基线文件（首次运行或发版后更新）
//
// 用法：
//
//	export OPENAI_API_KEY=sk-xxx
//	go run ./bench/llm-bench --provider openai --model gpt-4o-mini --out report.json
//	go run ./bench/llm-bench --provider deepseek --model deepseek-chat --update-baseline
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/eval"
	"github.com/Gleamseekers/AgentPrimordia/internal/llm"
)

func main() {
	var (
		provider       = flag.String("provider", "openai", "LLM Provider: openai/anthropic/deepseek/qwen/gemini/glm")
		model          = flag.String("model", "gpt-4o-mini", "被测模型名")
		apiKey         = flag.String("api-key", "", "API Key（默认从环境变量读取）")
		baseURL        = flag.String("base-url", "", "自定义 Base URL（OpenAI 兼容网关，如 https://api.deepseek.com/v1；默认按 provider 官方端点）")
		out            = flag.String("out", "llm-bench-report.json", "报告输出路径")
		baseline       = flag.String("baseline", "bench/llm-bench/baseline.json", "基线文件路径")
		updateBaseline = flag.Bool("update-baseline", false, "将本次报告写入基线文件")
		threshold      = flag.Float64("threshold", 0.8, "版本门禁通过率下限 [0,1]")
		retries        = flag.Int("retries", 1, "失败后重试次数（测量恢复率）")
		limit          = flag.Int("limit", 0, "仅运行前 N 条用例（0 = 全部）")
		version        = flag.String("version", "dev", "被测框架版本号")
		timeout        = flag.Duration("timeout", 60*time.Second, "单次 LLM 调用超时")
		ability        = flag.String("ability", "coding", "跑分能力: coding（默认）| autonomy | skills")
		allowFail      = flag.Bool("allow-fail", false, "无 API Key 时产出 0 分基线报告（计划约定：失败记 0 分不门禁）")
	)
	flag.Parse()

	if *threshold < 0 || *threshold > 1 {
		fmt.Fprintf(os.Stderr, "ERROR: --threshold 必须在 [0,1]，got %f\n", *threshold)
		os.Exit(2)
	}
	if *retries < 0 {
		fmt.Fprintf(os.Stderr, "ERROR: --retries 不能为负\n")
		os.Exit(2)
	}

	key := *apiKey
	if key == "" {
		key = envKeyForProvider(*provider)
	}
	if key == "" && !*allowFail {
		fmt.Fprintf(os.Stderr, "ERROR: %s API Key 未配置（用 --api-key 或 %s 环境变量）\n", *provider, envKeyName(*provider))
		os.Exit(2)
	}
	noKey := key == ""

	var prov llm.Provider
	var err error
	if !noKey {
		prov, err = newProvider(*provider, key, *model, *baseURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: 创建 Provider 失败: %v\n", err)
			os.Exit(2)
		}
	}

	cases := eval.MustBenchmarkCases()
	if *limit > 0 && *limit < len(cases) {
		cases = cases[:*limit]
	}

	agent := eval.NewLLMBenchAgent(prov, *model, eval.WithLLMTimeout(*timeout))
	baselineRate := loadBaseline(*baseline, *model)

	cfg := eval.LLMBenchConfig{
		Version:      *version,
		Model:        *model,
		ProviderName: *provider,
		Retries:      *retries,
		Timeout:      *timeout,
		Threshold:    *threshold,
		Baseline:     baselineRate,
	}

	fmt.Printf("==> AgentPrimordia LLM 跑分\n")
	fmt.Printf("    Provider: %s\n    Model:    %s\n", *provider, *model)
	fmt.Printf("    Ability:  %s\n", *ability)
	fmt.Printf("    Cases:    %d\n    Retries:  %d\n", len(cases), *retries)
	fmt.Printf("    Baseline: %.4f (gate >= %.4f)\n\n", baselineRate, maxOf(*threshold, baselineRate))

	// 按能力选择案例集
	caseCount := len(cases)
	switch *ability {
	case "autonomy":
		acCases := abilityGoalCases(*limit)
		cases = nil
		caseCount = len(acCases)
		_ = acCases
	case "skills":
		skCases := abilitySkillCases(*limit)
		cases = nil
		caseCount = len(skCases)
		_ = skCases
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	start := time.Now()
	var res *eval.LLMBenchResult
	if noKey {
		// v4.1 计划约定：失败记 0 分不门禁，但须产出首份基线报告
		fmt.Printf("    ⚠ 无 API Key：按计划产出 0 分基线报告（provider=unavailable）\n\n")
		res = zeroRateReport(cfg, *ability, caseCount)
	} else {
		switch *ability {
		case "coding":
			res, err = eval.RunLLMBench(ctx, cfg, agent, cases)
		case "autonomy":
			res, err = eval.RunAutonomyGoalBench(ctx, cfg, agent, abilityGoalCases(*limit))
		case "skills":
			res, err = eval.RunSkillAcquisitionBench(ctx, cfg, agent, abilitySkillCases(*limit))
		default:
			fmt.Fprintf(os.Stderr, "ERROR: 未知能力 %q（支持 coding|autonomy|skills）\n", *ability)
			os.Exit(2)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: 跑分失败: %v\n", err)
			os.Exit(2)
		}
	}

	printReport(res, time.Since(start))

	// 写报告
	if err := writeJSON(*out, res); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: 写报告失败: %v\n", err)
		os.Exit(2)
	}
	fmt.Printf("\n报告已写入 %s\n", *out)

	// 基线更新或门禁判定
	if *updateBaseline {
		if err := writeBaseline(*baseline, res); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: 更新基线失败: %v\n", err)
			os.Exit(2)
		}
		fmt.Printf("基线已更新: %s (pass_rate=%.4f)\n", *baseline, res.PassRate)
		return
	}

	if noKey {
		fmt.Printf("ℹ 0 分基线模式：不执行门禁判定（计划约定：失败记 0 分不门禁）\n")
		return
	}
	if !res.MeetsGate {
		fmt.Printf("❌ 门禁未达标：pass_rate=%.4f < gate=%.4f（分数只升不降）\n", res.PassRate, maxOf(*threshold, baselineRate))
		os.Exit(1)
	}
	fmt.Printf("✅ 门禁通过：pass_rate=%.4f >= gate=%.4f\n", res.PassRate, maxOf(*threshold, baselineRate))
}

func maxOf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// newProvider 按名称构造 Provider。baseURL 非空时覆盖默认端点（OpenAI 兼容网关）。
func newProvider(name, apiKey, model, baseURL string) (llm.Provider, error) {
	cfg := llm.Config{APIKey: apiKey, Model: model, BaseURL: baseURL}
	switch name {
	case "openai":
		return llm.NewOpenAIProvider(cfg)
	case "anthropic":
		return llm.NewAnthropicProvider(cfg)
	case "deepseek":
		return llm.NewDeepSeekProvider(cfg)
	case "qwen":
		return llm.NewQwenProvider(cfg)
	case "gemini":
		return llm.NewGeminiProvider(cfg)
	case "glm":
		return llm.NewGLMProvider(cfg)
	default:
		return nil, fmt.Errorf("未知 Provider %q", name)
	}
}

func envKeyForProvider(name string) string {
	return os.Getenv(envKeyName(name))
}

func envKeyName(name string) string {
	switch name {
	case "openai":
		return "OPENAI_API_KEY"
	case "anthropic":
		return "ANTHROPIC_API_KEY"
	case "deepseek":
		return "DEEPSEEK_API_KEY"
	case "qwen":
		return "QWEN_API_KEY"
	case "gemini":
		return "GEMINI_API_KEY"
	case "glm":
		return "GLM_API_KEY"
	default:
		return upperASCII(name) + "_API_KEY"
	}
}

func upperASCII(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'a' && b[i] <= 'z' {
			b[i] -= 32
		}
	}
	return string(b)
}

// loadBaseline 读取基线文件中的 pass_rate；文件不存在或模型不符时返回 0。
func loadBaseline(path, model string) float64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var b struct {
		Model    string  `json:"model"`
		PassRate float64 `json:"pass_rate"`
	}
	if err := json.Unmarshal(data, &b); err != nil {
		return 0
	}
	if b.Model != "" && b.Model != model {
		fmt.Printf("::warning::基线模型 %s 与本次模型 %s 不一致，按无基线处理\n", b.Model, model)
		return 0
	}
	return b.PassRate
}

// writeBaseline 将本次报告写为基线文件。
func writeBaseline(path string, res *eval.LLMBenchResult) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b := struct {
		Version      string  `json:"version"`
		Model        string  `json:"model"`
		Provider     string  `json:"provider"`
		PassRate     float64 `json:"pass_rate"`
		CostUSD      float64 `json:"cost_usd"`
		RecoveryRate float64 `json:"recovery_rate"`
		Updated      string  `json:"updated"`
	}{
		Version:      res.Version,
		Model:        res.Model,
		Provider:     res.Provider,
		PassRate:     res.PassRate,
		CostUSD:      res.CostUSD,
		RecoveryRate: res.RecoveryRate,
		Updated:      res.Generated,
	}
	return writeJSON(path, b)
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func printReport(res *eval.LLMBenchResult, elapsed time.Duration) {
	fmt.Println("==> 跑分结果")
	fmt.Printf("    Version:        %s\n", res.Version)
	fmt.Printf("    Total:          %d\n", res.Total)
	fmt.Printf("    Passed:         %d\n", res.Passed)
	fmt.Printf("    Failed:         %d\n", res.Failed)
	fmt.Printf("    PassRate:       %.4f\n", res.PassRate)
	fmt.Printf("    RecoveryRate:   %.4f\n", res.RecoveryRate)
	fmt.Printf("    Cost(USD):      $%.4f\n", res.CostUSD)
	fmt.Printf("    Tokens:         %d (prompt %d / completion %d)\n", res.TotalTokens, res.PromptTokens, res.CompletionTokens)
	fmt.Printf("    Latency:        %d ms total / %d ms avg\n", res.LatencyMs, res.AvgLatencyMs)
	fmt.Printf("    Wall:           %s\n", elapsed.Round(time.Millisecond))
}

// zeroRateReport 无 Key 时的 0 分基线报告（provider=unavailable，每用例记错误）。
func zeroRateReport(cfg eval.LLMBenchConfig, ability string, total int) *eval.LLMBenchResult {
	res := &eval.LLMBenchResult{
		Version:   cfg.Version,
		Model:     cfg.Model,
		Provider:  "unavailable",
		Total:     total,
		Failed:    total,
		PassRate:  0,
		Threshold: cfg.Threshold,
		Baseline:  cfg.Baseline,
		Generated: time.Now().UTC().Format(time.RFC3339),
		Cases:     make([]eval.LLMBenchCaseResult, 0, total),
	}
	for i := range total {
		res.Cases = append(res.Cases, eval.LLMBenchCaseResult{
			CaseID: "unavailable-" + itoa(i),
			Phase:  ability,
			Error:  "API Key 未配置（--allow-fail 0 分基线模式）",
		})
	}
	return res
}

// abilityGoalCases autonomy 能力跑分案例（目标 + 必达阶段）。
func abilityGoalCases(limit int) []eval.AutonomyGoalCase {
	cases := []eval.AutonomyGoalCase{
		{ID: "goal-1", Goal: "监控数据异常并自动修复", Required: []string{"采集", "修复", "验证"}},
		{ID: "goal-2", Goal: "每日生成销售汇总报告并发送", Required: []string{"汇总", "报告"}},
		{ID: "goal-3", Goal: "分析用户反馈并输出改进建议", Required: []string{"分析", "建议"}},
	}
	if limit > 0 && limit < len(cases) {
		cases = cases[:limit]
	}
	return cases
}

// abilitySkillCases skills 能力跑分案例（任务 + 工具轨迹）。
func abilitySkillCases(limit int) []eval.SkillAcquisitionCase {
	cases := []eval.SkillAcquisitionCase{
		{ID: "skill-1", Task: "数据库异常修复", ToolCalls: []string{"query_anomaly", "fix_data", "verify_fix"}, MinSteps: 3},
		{ID: "skill-2", Task: "日志归档", ToolCalls: []string{"scan_logs", "archive"}, MinSteps: 2},
		{ID: "skill-3", Task: "模型评估报告", ToolCalls: []string{"run_eval", "collect_metrics", "render_report"}, MinSteps: 2},
	}
	if limit > 0 && limit < len(cases) {
		cases = cases[:limit]
	}
	return cases
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [8]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}
