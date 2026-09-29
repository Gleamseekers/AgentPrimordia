//go:build e2e
// +build e2e

// harness_e2e_test.go — harness 混沌 E2E（v3.5-5）
//
// 在真实 harness 基准集（v3.5-1 benchmark_cases.json）上跑混沌对比：
// 基线 vs 注入故障，量化成功率下降。
//
// 运行方式：
//
//	go test -tags=e2e -run TestE2E_HarnessChaos -v ./internal/chaos/
package chaos

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/eval"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
)

// compliantMockProvider 对任何基准用例都返回包含全部 Requires 片段的输出
// （基线必然通过），用于在真实基准集上量化故障注入带来的成功率下降。
// 通过系统消息中的 case=<id> 识别用例。
type compliantMockProvider struct {
	// byID: 用例 ID → 返回内容（由测试从基准集构建）
	byID map[string]string
}

func (m *compliantMockProvider) Complete(_ context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	content := "compliance output"
	if len(req.Messages) > 0 {
		sys := req.Messages[0].Content
		if i := strings.Index(sys, "case="); i >= 0 {
			id := strings.TrimSpace(sys[i+5:])
			if j := strings.IndexAny(id, " \t）\n"); j >= 0 {
				id = id[:j]
			}
			if out, ok := m.byID[id]; ok {
				content = out
			}
		}
	}
	return &llm.CompletionResponse{
		ID:      "e2e-mock",
		Model:   req.Model,
		Content: content,
		Role:    "assistant",
		Usage:   llm.Usage{PromptTokens: 5, CompletionTokens: 5, TotalTokens: 10},
	}, nil
}

func (m *compliantMockProvider) Stream(ctx context.Context, req *llm.CompletionRequest) (<-chan llm.Chunk, error) {
	ch := make(chan llm.Chunk, 1)
	go func() {
		resp, err := m.Complete(ctx, req)
		if err != nil {
			close(ch)
			return
		}
		ch <- llm.Chunk{Content: resp.Content, Done: true}
		close(ch)
	}()
	return ch, nil
}

func (m *compliantMockProvider) CallTools(_ context.Context, _ *llm.ToolCallRequest) (*llm.ToolCallResponse, error) {
	return &llm.ToolCallResponse{Content: ""}, nil
}

func (m *compliantMockProvider) Info() llm.ModelInfo {
	return llm.ModelInfo{Name: "e2e-mock", Provider: "mock", MaxContext: 8192}
}

// buildCompliantResponses 为用例集构建"必然通过"的响应映射：
// 每个用例 → 其 Requires 拼接（编码任务）或 Expected 关键词（非编码任务）。
func buildCompliantResponses(cases []eval.EvalCase) map[string]string {
	out := make(map[string]string, len(cases))
	for _, c := range cases {
		content := c.Expected
		if len(c.Requires) > 0 {
			content = strings.Join(c.Requires, " ")
		}
		out[c.ID] = content
	}
	return out
}

// TestE2E_HarnessChaos 在真实基准集子集上量化注入故障下的成功率下降。
// 故障注入 50%（每 2 次调用失败 1 次），验证下降量可量化且为正。
func TestE2E_HarnessChaos(t *testing.T) {
	cases := eval.MustBenchmarkCases()
	if len(cases) < 18 {
		t.Fatalf("基准集用例 < 18，无法执行")
	}
	// 取前 18 条：覆盖 plan(6)+implement go(10)+implement ts(前2)，含双线
	cases = cases[:18]

	base := &compliantMockProvider{byID: buildCompliantResponses(cases)}
	fault := NewFaultInjectingProvider(base, nil, 0.5)

	cfg := HarnessChaosConfig{
		Cases:            cases,
		BaselineProvider: base,
		FaultProvider:    fault,
		Timeout:          10 * time.Second,
	}

	report, err := RunHarnessChaos(context.Background(), cfg)
	if err != nil {
		t.Fatalf("RunHarnessChaos failed: %v", err)
	}

	if report.Total != 18 {
		t.Errorf("Total = %d, want 18", report.Total)
	}
	if report.BaselinePassRate != 1.0 {
		t.Errorf("BaselinePassRate = %f, want 1.0", report.BaselinePassRate)
	}
	if report.FaultPassRate >= report.BaselinePassRate {
		t.Errorf("故障后成功率应下降: fault=%f baseline=%f", report.FaultPassRate, report.BaselinePassRate)
	}
	if report.InjectedFailures == 0 {
		t.Error("应注入故障")
	}
	if report.Degradation <= 0 {
		t.Errorf("Degradation = %f, 应 > 0（成功率下降可量化）", report.Degradation)
	}

	t.Logf("harness 混沌结果: baseline=%.3f fault=%.3f degradation=%.3f (%.1f%%), injected=%d",
		report.BaselinePassRate, report.FaultPassRate, report.Degradation, report.DegradationPct, report.InjectedFailures)

	// 报告中应区分两条线上的用例（go/ts）
	langs := map[string]bool{}
	for _, c := range cases {
		if c.Lang != "" {
			langs[c.Lang] = true
		}
	}
	if !langs["go"] || !langs["ts"] {
		t.Errorf("基准集应双线覆盖, langs=%v", langs)
	}
}

// TestE2E_HarnessChaos_PhaseCoverage 验证 harness 混沌覆盖各阶段用例。
func TestE2E_HarnessChaos_PhaseCoverage(t *testing.T) {
	cases := eval.MustBenchmarkCases()
	phases := map[string]int{}
	for _, c := range cases {
		phases[c.HarnessPhase]++
	}
	for _, p := range []string{
		eval.PhasePlan, eval.PhaseImplement, eval.PhaseTest,
		eval.PhaseReview, eval.PhaseRelease, eval.PhaseGuard,
	} {
		if phases[p] == 0 {
			t.Errorf("基准集缺少阶段 %s", p)
		}
	}
	_ = strings.TrimSpace
}
