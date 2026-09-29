package ap

import (
	"math"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
)

// TestWrapCostTracker_Summary 验证 CostTrackerSource.Summary 把
// agent.CostSummary 转换成 metrics.CostSourceSummary
func TestWrapCostTracker_Summary(t *testing.T) {
	pricing := map[string]llm.ModelPricing{
		"gpt-4": {
			Model:                "gpt-4",
			Provider:             "openai",
			PromptPricePer1M:     30.0,
			CompletionPricePer1M: 60.0,
		},
	}
	tracker := agent.NewCostTracker(pricing, nil)
	if err := tracker.Record("gpt-4", "sess-1", "agent-1", llm.Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}); err != nil {
		t.Fatalf("Record failed: %v", err)
	}

	src := WrapCostTracker(tracker)
	summary := src.Summary()

	mc, ok := summary.ByModel["gpt-4"]
	if !ok {
		t.Fatal("expected ByModel[gpt-4]")
	}
	// 价格表是 per-1M tokens：
	//   prompt = 30 * 100 / 1_000_000 = 0.003
	//   completion = 60 * 50 / 1_000_000 = 0.003
	//   total = 0.006
	expectedCost := 30.0*100.0/1_000_000.0 + 60.0*50.0/1_000_000.0
	if math.Abs(mc.CostUSD-expectedCost) > 1e-9 {
		t.Errorf("expected cost=%v, got %v", expectedCost, mc.CostUSD)
	}
	if mc.Calls != 1 {
		t.Errorf("expected calls=1, got %d", mc.Calls)
	}
	if mc.Tokens != 150 {
		t.Errorf("expected tokens=150, got %d", mc.Tokens)
	}
}

// TestWrapCostTracker_Records 验证 CostTrackerSource.Records 转换
func TestWrapCostTracker_Records(t *testing.T) {
	pricing := map[string]llm.ModelPricing{
		"claude-3-5-sonnet-20241022": {
			Model:                "claude-3-5-sonnet-20241022",
			Provider:             "anthropic",
			PromptPricePer1M:     3.0,
			CompletionPricePer1M: 15.0,
		},
	}
	tracker := agent.NewCostTracker(pricing, nil)
	if err := tracker.Record("claude-3-5-sonnet-20241022", "sess-1", "agent-2", llm.Usage{
		PromptTokens:     10,
		CompletionTokens: 20,
		TotalTokens:      30,
	}); err != nil {
		t.Fatalf("Record failed: %v", err)
	}

	src := WrapCostTracker(tracker)
	records := src.Records()

	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	rec := records[0]
	if rec.Model != "claude-3-5-sonnet-20241022" {
		t.Errorf("expected model=claude-3-5-sonnet-20241022, got %q", rec.Model)
	}
	if rec.AgentName != "agent-2" {
		t.Errorf("expected agent_name=agent-2, got %q", rec.AgentName)
	}
	if rec.PromptTokens != 10 || rec.CompletionTokens != 20 || rec.TotalTokens != 30 {
		t.Errorf("unexpected tokens: prompt=%d, completion=%d, total=%d",
			rec.PromptTokens, rec.CompletionTokens, rec.TotalTokens)
	}
}

// TestWrapCostTracker_NilTracker 测试 nil 指针的 Wrap
func TestWrapCostTracker_NilTracker(t *testing.T) {
	src := WrapCostTracker(nil)
	if src == nil {
		t.Fatal("expected non-nil source even with nil tracker")
	}
	if src.Tracker != nil {
		t.Error("expected nil tracker")
	}
}
