package cost

import (
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm"
	"math"
	"sync"
	"testing"
	"time"
)

func TestCostTracker_Record(t *testing.T) {
	pricing := map[string]llm.ModelPricing{
		"test-model": {
			Model:                "test-model",
			Provider:             "test",
			PromptPricePer1M:     5.0,
			CompletionPricePer1M: 15.0,
		},
	}
	tracker := NewCostTracker(pricing, nil)

	err := tracker.Record("test-model", "sess1", "agent1", llm.Usage{
		PromptTokens:     1000,
		CompletionTokens: 500,
		TotalTokens:      1500,
	})
	if err != nil {
		t.Fatalf("Record error: %v", err)
	}

	summary := tracker.Summary()
	if summary.CallCount != 1 {
		t.Errorf("CallCount = %d, want 1", summary.CallCount)
	}
	if summary.TotalPromptTokens != 1000 {
		t.Errorf("TotalPromptTokens = %d, want 1000", summary.TotalPromptTokens)
	}
	if summary.TotalCompTokens != 500 {
		t.Errorf("TotalCompTokens = %d, want 500", summary.TotalCompTokens)
	}
	if summary.TotalTokens != 1500 {
		t.Errorf("TotalTokens = %d, want 1500", summary.TotalTokens)
	}

	expectedCost := 5.0*1000/1_000_000 + 15.0*500/1_000_000
	if math.Abs(summary.TotalCostUSD-expectedCost) > 1e-9 {
		t.Errorf("TotalCostUSD = %v, want %v", summary.TotalCostUSD, expectedCost)
	}
}

func TestCostTracker_RecordMultiple(t *testing.T) {
	pricing := map[string]llm.ModelPricing{
		"model-a": {Model: "model-a", PromptPricePer1M: 3.0, CompletionPricePer1M: 6.0},
		"model-b": {Model: "model-b", PromptPricePer1M: 1.0, CompletionPricePer1M: 2.0},
	}
	tracker := NewCostTracker(pricing, nil)

	_ = tracker.Record("model-a", "s1", "a1", llm.Usage{PromptTokens: 2000, CompletionTokens: 1000, TotalTokens: 3000})
	_ = tracker.Record("model-b", "s1", "a1", llm.Usage{PromptTokens: 500, CompletionTokens: 200, TotalTokens: 700})

	summary := tracker.Summary()
	if summary.CallCount != 2 {
		t.Errorf("CallCount = %d, want 2", summary.CallCount)
	}
	if summary.TotalPromptTokens != 2500 {
		t.Errorf("TotalPromptTokens = %d, want 2500", summary.TotalPromptTokens)
	}
	if summary.TotalCompTokens != 1200 {
		t.Errorf("TotalCompTokens = %d, want 1200", summary.TotalCompTokens)
	}

	modelA, ok := summary.ByModel["model-a"]
	if !ok {
		t.Fatal("ByModel should contain model-a")
	}
	if modelA.Calls != 1 {
		t.Errorf("model-a Calls = %d, want 1", modelA.Calls)
	}

	modelB, ok := summary.ByModel["model-b"]
	if !ok {
		t.Fatal("ByModel should contain model-b")
	}
	if modelB.Calls != 1 {
		t.Errorf("model-b Calls = %d, want 1", modelB.Calls)
	}
}

func TestCostTracker_Summary(t *testing.T) {
	pricing := map[string]llm.ModelPricing{
		"test-model": {Model: "test-model", PromptPricePer1M: 10.0, CompletionPricePer1M: 30.0},
	}
	tracker := NewCostTracker(pricing, nil)

	_ = tracker.Record("test-model", "s1", "a1", llm.Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150})
	_ = tracker.Record("test-model", "s1", "a1", llm.Usage{PromptTokens: 200, CompletionTokens: 100, TotalTokens: 300})

	summary := tracker.Summary()
	if summary.CallCount != 2 {
		t.Errorf("CallCount = %d, want 2", summary.CallCount)
	}
	if summary.TotalPromptTokens != 300 {
		t.Errorf("TotalPromptTokens = %d, want 300", summary.TotalPromptTokens)
	}
	if summary.TotalCompTokens != 150 {
		t.Errorf("TotalCompTokens = %d, want 150", summary.TotalCompTokens)
	}
}

func TestCostTracker_BudgetExceed(t *testing.T) {
	pricing := map[string]llm.ModelPricing{
		"test-model": {Model: "test-model", PromptPricePer1M: 1000.0, CompletionPricePer1M: 3000.0},
	}
	budget := &BudgetConfig{
		MaxTotalCostUSD: 0.001,
	}
	tracker := NewCostTracker(pricing, budget)

	if tracker.CheckBudget() {
		t.Error("CheckBudget should be false before any calls")
	}

	_ = tracker.Record("test-model", "s1", "a1", llm.Usage{PromptTokens: 1000, CompletionTokens: 500, TotalTokens: 1500})

	if !tracker.CheckBudget() {
		t.Error("CheckBudget should be true after exceeding budget")
	}
}

func TestCostTracker_BudgetNotExceed(t *testing.T) {
	pricing := map[string]llm.ModelPricing{
		"test-model": {Model: "test-model", PromptPricePer1M: 1.0, CompletionPricePer1M: 2.0},
	}
	budget := &BudgetConfig{
		MaxTotalCostUSD: 1.0,
	}
	tracker := NewCostTracker(pricing, budget)

	_ = tracker.Record("test-model", "s1", "a1", llm.Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150})

	if tracker.CheckBudget() {
		t.Error("CheckBudget should be false when within budget")
	}
}

func TestCostTracker_BudgetCallback(t *testing.T) {
	pricing := map[string]llm.ModelPricing{
		"test-model": {Model: "test-model", PromptPricePer1M: 1000.0, CompletionPricePer1M: 3000.0},
	}

	var callbackSummary *CostSummary
	budget := &BudgetConfig{
		MaxTotalCostUSD: 0.001,
		OnBudgetExceed: func(s *CostSummary) {
			callbackSummary = s
		},
	}
	tracker := NewCostTracker(pricing, budget)

	_ = tracker.Record("test-model", "s1", "a1", llm.Usage{PromptTokens: 1000, CompletionTokens: 500, TotalTokens: 1500})

	if callbackSummary == nil {
		t.Error("OnBudgetExceed callback should have been called")
	}
	if callbackSummary.CallCount != 1 {
		t.Errorf("callback CallCount = %d, want 1", callbackSummary.CallCount)
	}
}

func TestCostTracker_Reset(t *testing.T) {
	pricing := map[string]llm.ModelPricing{
		"test-model": {Model: "test-model", PromptPricePer1M: 5.0, CompletionPricePer1M: 15.0},
	}
	tracker := NewCostTracker(pricing, nil)

	_ = tracker.Record("test-model", "s1", "a1", llm.Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150})
	tracker.Reset()

	summary := tracker.Summary()
	if summary.CallCount != 0 {
		t.Errorf("CallCount after reset = %d, want 0", summary.CallCount)
	}
	if summary.TotalCostUSD != 0 {
		t.Errorf("TotalCostUSD after reset = %v, want 0", summary.TotalCostUSD)
	}
}

func TestCostTracker_ConcurrentRecord(t *testing.T) {
	pricing := map[string]llm.ModelPricing{
		"test-model": {Model: "test-model", PromptPricePer1M: 5.0, CompletionPricePer1M: 15.0},
	}
	tracker := NewCostTracker(pricing, nil)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = tracker.Record("test-model", "s1", "a1", llm.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15})
		}()
	}
	wg.Wait()

	summary := tracker.Summary()
	if summary.CallCount != 100 {
		t.Errorf("CallCount = %d, want 100", summary.CallCount)
	}
	if summary.TotalPromptTokens != 1000 {
		t.Errorf("TotalPromptTokens = %d, want 1000", summary.TotalPromptTokens)
	}
}

func TestCostTracker_UnknownModel(t *testing.T) {
	pricing := map[string]llm.ModelPricing{
		"known-model": {Model: "known-model", PromptPricePer1M: 5.0, CompletionPricePer1M: 15.0},
	}
	tracker := NewCostTracker(pricing, nil)

	err := tracker.Record("unknown-model", "s1", "a1", llm.Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150})
	if err != nil {
		t.Fatalf("Record should not error for unknown model: %v", err)
	}

	summary := tracker.Summary()
	if summary.CallCount != 1 {
		t.Errorf("CallCount = %d, want 1", summary.CallCount)
	}
	if summary.TotalCostUSD != 0 {
		t.Errorf("TotalCostUSD for unknown model should be 0, got %v", summary.TotalCostUSD)
	}
	if summary.TotalPromptTokens != 100 {
		t.Errorf("TotalPromptTokens should still be tracked, got %d", summary.TotalPromptTokens)
	}
}

func TestCostRecord_Fields(t *testing.T) {
	now := time.Now()
	record := CostRecord{
		Model:            "gpt-4o",
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		CostUSD:          0.001,
		Timestamp:        now,
		SessionID:        "sess1",
		AgentName:        "agent1",
	}
	if record.Model != "gpt-4o" {
		t.Error("Model field mismatch")
	}
	if record.Timestamp != now {
		t.Error("Timestamp field mismatch")
	}
}

func TestBudgetConfig_NilBudget(t *testing.T) {
	pricing := map[string]llm.ModelPricing{
		"test-model": {Model: "test-model", PromptPricePer1M: 5.0, CompletionPricePer1M: 15.0},
	}
	tracker := NewCostTracker(pricing, nil)

	_ = tracker.Record("test-model", "s1", "a1", llm.Usage{PromptTokens: 100000, CompletionTokens: 50000, TotalTokens: 150000})

	if tracker.CheckBudget() {
		t.Error("CheckBudget should always be false when no budget config")
	}
}

// TestCostTracker_RecordCheckBudgetConsistency 验证 Record 在预算检查前已完成原子累加更新。
func TestCostTracker_RecordCheckBudgetConsistency(t *testing.T) {
	pricing := map[string]llm.ModelPricing{
		"expensive": {Model: "expensive", PromptPricePer1M: 1e9, CompletionPricePer1M: 1e9},
	}
	budget := &BudgetConfig{MaxTotalCostUSD: 0.001}
	tracker := NewCostTracker(pricing, budget)

	var observedFalse bool
	SetRecordBudgetCheckHook(func() {
		// 此时 records 已追加，原子累加应已更新，CheckBudget 必须返回 true
		if !tracker.CheckBudget() {
			observedFalse = true
		}
	})
	defer SetRecordBudgetCheckHook(nil)

	_ = tracker.Record("expensive", "s1", "a1", llm.Usage{PromptTokens: 1000, CompletionTokens: 1000, TotalTokens: 2000})

	if observedFalse {
		t.Error("在预算检查前，CheckBudget 读取到了未更新的原子累加值，返回 false")
	}
	if !tracker.CheckBudget() {
		t.Error("Record 完成后 CheckBudget 应为 true")
	}
}

func TestCostTracker_TokenBudget(t *testing.T) {
	pricing := map[string]llm.ModelPricing{
		"test-model": {Model: "test-model", PromptPricePer1M: 1.0, CompletionPricePer1M: 2.0},
	}
	budget := &BudgetConfig{
		MaxTokensPerSession: 200,
	}
	tracker := NewCostTracker(pricing, budget)

	_ = tracker.Record("test-model", "s1", "a1", llm.Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150})
	if tracker.CheckBudget() {
		t.Error("should not exceed token budget yet")
	}

	_ = tracker.Record("test-model", "s1", "a1", llm.Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150})
	if !tracker.CheckBudget() {
		t.Error("should exceed token budget now (300 > 200)")
	}
}
