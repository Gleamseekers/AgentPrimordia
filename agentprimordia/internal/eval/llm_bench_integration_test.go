//go:build integration
// +build integration

// 真实 LLM 跑分集成测试（v3.5-2）。
//
// 需要 OPENAI_API_KEY（或 LLM_BENCH_PROVIDER / LLM_BENCH_API_KEY 指定其他 Provider，
// LLM_BENCH_BASE_URL 指定 OpenAI 兼容网关）。
// 默认仅跑 3 条用例以控制成本；设置 LLM_BENCH_FULL=1 时跑完整 60 条基准集。
package eval

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/llm"
)

func newBenchProviderFromEnv(t *testing.T) llm.Provider {
	t.Helper()
	providerName := os.Getenv("LLM_BENCH_PROVIDER")
	if providerName == "" {
		providerName = "openai"
	}
	key := os.Getenv("LLM_BENCH_API_KEY")
	if key == "" {
		key = os.Getenv("OPENAI_API_KEY")
	}
	if key == "" {
		t.Skip("LLM_BENCH_API_KEY / OPENAI_API_KEY 未配置，跳过真实跑分集成测试")
	}

	model := os.Getenv("LLM_BENCH_MODEL")
	if model == "" {
		model = "gpt-4o-mini"
	}

	var (
		prov llm.Provider
		err  error
	)
	// v6.0.1：支持 LLM_BENCH_BASE_URL 指定 OpenAI 兼容网关（默认 provider 官方端点）
	cfg := llm.Config{APIKey: key, Model: model, BaseURL: os.Getenv("LLM_BENCH_BASE_URL")}
	switch providerName {
	case "openai":
		prov, err = llm.NewOpenAIProvider(cfg)
	case "deepseek":
		prov, err = llm.NewDeepSeekProvider(cfg)
	case "qwen":
		prov, err = llm.NewQwenProvider(cfg)
	default:
		t.Skipf("不支持的 Provider %q", providerName)
	}
	if err != nil {
		t.Fatalf("创建 Provider 失败: %v", err)
	}
	return prov
}

// TestIntegration_LLMBench 真实 LLM 跑分（成功率/成本/耗时/恢复率）。
func TestIntegration_LLMBench(t *testing.T) {
	prov := newBenchProviderFromEnv(t)
	cases := MustBenchmarkCases()

	// 控制成本：默认 3 条，LLM_BENCH_FULL=1 时全量
	if full, _ := strconv.Atoi(os.Getenv("LLM_BENCH_FULL")); full != 1 {
		cases = cases[:3]
	}

	agent := NewLLMBenchAgent(prov, os.Getenv("LLM_BENCH_MODEL"), WithLLMTimeout(60*time.Second))
	cfg := LLMBenchConfig{
		Version:      "integration",
		Model:        os.Getenv("LLM_BENCH_MODEL"),
		ProviderName: os.Getenv("LLM_BENCH_PROVIDER"),
		Retries:      1,
		Threshold:    0,
		Baseline:     0,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	res, err := RunLLMBench(ctx, cfg, agent, cases)
	if err != nil {
		t.Fatalf("RunLLMBench failed: %v", err)
	}

	if res.Total != len(cases) {
		t.Errorf("Total = %d, want %d", res.Total, len(cases))
	}
	if res.PassRate <= 0 || res.PassRate > 1 {
		t.Errorf("PassRate = %f, 超出 (0,1]", res.PassRate)
	}
	if res.CostUSD < 0 {
		t.Errorf("CostUSD = %f, 不应为负", res.CostUSD)
	}
	if res.TotalTokens <= 0 {
		t.Errorf("TotalTokens = %d, 真实调用应产生 token", res.TotalTokens)
	}
	if res.RecoveryRate < 0 || res.RecoveryRate > 1 {
		t.Errorf("RecoveryRate = %f, 超出 [0,1]", res.RecoveryRate)
	}

	t.Logf("真实跑分: total=%d passed=%d pass_rate=%.3f cost=$%.4f tokens=%d recovery=%.3f",
		res.Total, res.Passed, res.PassRate, res.CostUSD, res.TotalTokens, res.RecoveryRate)
}
