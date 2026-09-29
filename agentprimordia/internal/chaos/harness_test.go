package chaos

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/eval"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
)

// chaosMockProvider 确定性 mock Provider：按内容关键词返回完整代码或关键词。
type chaosMockProvider struct {
	mu    sync.Mutex
	calls int
}

// Complete 按输入关键词返回可判分的内容（满足 eval.CodeConstructEvaluator 的 requires）。
func (m *chaosMockProvider) Complete(_ context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	m.mu.Lock()
	m.calls++
	m.mu.Unlock()

	content := "default"
	if len(req.Messages) > 0 {
		input := req.Messages[len(req.Messages)-1].Content
		switch {
		case containsAll(input, "Fibonacci", "负数"):
			content = "func Fibonacci(n int) int { if n < 0 { return -1 }; if n < 2 { return n }; return Fibonacci(n-1)+Fibonacci(n-2) }"
		case containsAll(input, "IsPalindrome", "回文"):
			content = "func IsPalindrome(s string) bool { r := []rune(s); for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 { if r[i] != r[j] { return false } }; return true }"
		case containsAll(input, "LRU", "缓存"):
			content = "type LRUCache struct { m map[any]any; mu sync.Mutex }; func (c *LRUCache) Get(k any) any { return c.m[k] }; func (c *LRUCache) Put(k, v any) { c.m[k] = v }"
		case containsAll(input, "Hello"):
			content = "Hello there!"
		}
	}
	return &llm.CompletionResponse{
		ID:      "chaos-mock",
		Model:   req.Model,
		Content: content,
		Role:    "assistant",
		Usage:   llm.Usage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30},
	}, nil
}

func (m *chaosMockProvider) Stream(ctx context.Context, req *llm.CompletionRequest) (<-chan llm.Chunk, error) {
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

func (m *chaosMockProvider) CallTools(_ context.Context, _ *llm.ToolCallRequest) (*llm.ToolCallResponse, error) {
	return &llm.ToolCallResponse{Content: ""}, nil
}

func (m *chaosMockProvider) Info() llm.ModelInfo {
	return llm.ModelInfo{Name: "chaos-mock", Provider: "mock", MaxContext: 8192}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		found := false
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// harnessChaosCases 构造 4 条确定性用例（2 条可通过 + 2 条可通过）。
func harnessChaosCases() []eval.EvalCase {
	return []eval.EvalCase{
		{
			ID: "impl_go_fibonacci", Name: "fib", Category: eval.CategoryCoding,
			HarnessPhase: eval.PhaseImplement, Lang: eval.LangGo,
			Input: "用 Go 实现 Fibonacci，负数返回 -1", Expected: "Fibonacci",
			Threshold: 0.8, Requires: []string{"func Fibonacci(", "if n < 0", "return"},
		},
		{
			ID: "impl_go_palindrome", Name: "pal", Category: eval.CategoryCoding,
			HarnessPhase: eval.PhaseImplement, Lang: eval.LangGo,
			Input: "用 Go 实现 IsPalindrome 回文判断", Expected: "IsPalindrome",
			Threshold: 0.8, Requires: []string{"func IsPalindrome(", "for ", "return"},
		},
		{
			ID: "impl_go_lru", Name: "lru", Category: eval.CategoryCoding,
			HarnessPhase: eval.PhaseImplement, Lang: eval.LangGo,
			Input: "实现并发安全 LRU 缓存", Expected: "LRUCache",
			Threshold: 0.8, Requires: []string{"type LRUCache struct", "Get(", "Put("},
		},
		{
			ID: "chat_greeting", Name: "greet", Category: eval.CategoryChat,
			HarnessPhase: eval.PhasePlan, Lang: eval.LangGeneric,
			Input: "Hello!", Expected: "Hello",
			Threshold: 0.5, Requires: []string{"Hello"},
		},
	}
}

// TestHarnessChaos_DegradationQuantified 验证注入故障后成功率下降可量化。
func TestHarnessChaos_DegradationQuantified(t *testing.T) {
	base := &chaosMockProvider{}
	cases := harnessChaosCases()

	// 故障 Provider：前 2 次调用失败（对应前 2 条用例失败）
	fault := NewFaultInjectingProvider(base, []int{1, 2}, 0)

	cfg := HarnessChaosConfig{
		Cases:            cases,
		BaselineProvider: base,
		FaultProvider:    fault,
		Timeout:          5 * time.Second,
	}

	report, err := RunHarnessChaos(context.Background(), cfg)
	if err != nil {
		t.Fatalf("RunHarnessChaos failed: %v", err)
	}

	// 基线全过
	if report.Total != 4 {
		t.Errorf("Total = %d, want 4", report.Total)
	}
	if report.BaselinePassRate != 1.0 {
		t.Errorf("BaselinePassRate = %f, want 1.0", report.BaselinePassRate)
	}
	// 注入 2 个故障 → 2 条失败
	if report.FaultPassRate != 0.5 {
		t.Errorf("FaultPassRate = %f, want 0.5", report.FaultPassRate)
	}
	if report.InjectedFailures != 2 {
		t.Errorf("InjectedFailures = %d, want 2", report.InjectedFailures)
	}
	// 下降可量化
	if report.Degradation != 0.5 {
		t.Errorf("Degradation = %f, want 0.5", report.Degradation)
	}
	if report.DegradationPct != 50.0 {
		t.Errorf("DegradationPct = %f, want 50.0", report.DegradationPct)
	}
}

// TestHarnessChaos_NoFaults 验证无故障时下降为 0。
func TestHarnessChaos_NoFaults(t *testing.T) {
	base := &chaosMockProvider{}
	fault := NewFaultInjectingProvider(base, nil, 0)

	report, err := RunHarnessChaos(context.Background(), HarnessChaosConfig{
		Cases:            harnessChaosCases(),
		BaselineProvider: base,
		FaultProvider:    fault,
	})
	if err != nil {
		t.Fatalf("RunHarnessChaos failed: %v", err)
	}
	if report.Degradation != 0 {
		t.Errorf("Degradation = %f, want 0", report.Degradation)
	}
	if report.InjectedFailures != 0 {
		t.Errorf("InjectedFailures = %d, want 0", report.InjectedFailures)
	}
}

// TestHarnessChaos_AllFaults 验证全部失败时下降 100%。
func TestHarnessChaos_AllFaults(t *testing.T) {
	base := &chaosMockProvider{}
	// 前 4 次调用全部失败
	fault := NewFaultInjectingProvider(base, []int{1, 2, 3, 4}, 0)

	report, err := RunHarnessChaos(context.Background(), HarnessChaosConfig{
		Cases:            harnessChaosCases(),
		BaselineProvider: base,
		FaultProvider:    fault,
	})
	if err != nil {
		t.Fatalf("RunHarnessChaos failed: %v", err)
	}
	if report.FaultPassRate != 0 {
		t.Errorf("FaultPassRate = %f, want 0", report.FaultPassRate)
	}
	if report.DegradationPct != 100.0 {
		t.Errorf("DegradationPct = %f, want 100.0", report.DegradationPct)
	}
}

// TestHarnessChaos_EmptyCases 验证无用例报错。
func TestHarnessChaos_EmptyCases(t *testing.T) {
	base := &chaosMockProvider{}
	_, err := RunHarnessChaos(context.Background(), HarnessChaosConfig{
		Cases:            nil,
		BaselineProvider: base,
		FaultProvider:    NewFaultInjectingProvider(base, nil, 0),
	})
	if err == nil {
		t.Error("空用例应返回错误")
	}
}

// TestFaultInjectingProvider_Probability 验证概率故障模式。
func TestFaultInjectingProvider_Probability(t *testing.T) {
	base := &chaosMockProvider{}
	fault := NewFaultInjectingProvider(base, nil, 0.5)
	ctx := context.Background()

	failed := 0
	for i := 0; i < 10; i++ {
		_, err := fault.Complete(ctx, &llm.CompletionRequest{Messages: []llm.ChatMessage{{Role: "user", Content: "Hello!"}}})
		if err != nil {
			failed++
		}
	}
	if failed != 5 {
		t.Errorf("rate=0.5 时 10 次应失败 5 次, got %d", failed)
	}
	if fault.InjectedFailures() != 5 {
		t.Errorf("InjectedFailures = %d, want 5", fault.InjectedFailures())
	}
}

// TestHarnessChaos_ReportJSON 验证报告可序列化。
func TestHarnessChaos_ReportJSON(t *testing.T) {
	base := &chaosMockProvider{}
	report, err := RunHarnessChaos(context.Background(), HarnessChaosConfig{
		Cases:            harnessChaosCases(),
		BaselineProvider: base,
		FaultProvider:    NewFaultInjectingProvider(base, []int{1}, 0),
	})
	if err != nil {
		t.Fatalf("RunHarnessChaos failed: %v", err)
	}
	data, _ := jsonMarshal(report)
	for _, field := range []string{"baseline_pass_rate", "fault_pass_rate", "degradation", "injected_failures"} {
		if !containsAll(string(data), field) {
			t.Errorf("报告 JSON 缺少字段 %s", field)
		}
	}
}

func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}
