package eval

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/internal/llm"
)

// mockBenchProvider 实现 llm.Provider，用于真实跑分逻辑的单元测试。
// 按调用顺序返回预设内容；可通过 failCalls 指定哪些调用返回错误。
type mockBenchProvider struct {
	mu        sync.Mutex
	responses []string
	failCalls map[int]error // 第 N 次调用（从 1 起）返回错误
	callCount int
	usage     llm.Usage
}

func (m *mockBenchProvider) Complete(_ context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount++
	if err, ok := m.failCalls[m.callCount]; ok {
		return nil, err
	}
	idx := m.callCount - 1
	content := "default"
	if idx < len(m.responses) {
		content = m.responses[idx]
	}
	m.usage.PromptTokens += 100
	m.usage.CompletionTokens += 200
	m.usage.TotalTokens += 300
	// 返回本次调用的独立用量（真实 Provider 每次返回单次用量）
	return &llm.CompletionResponse{
		ID:      "mock-bench-id",
		Model:   req.Model,
		Content: content,
		Role:    "assistant",
		Usage:   llm.Usage{PromptTokens: 100, CompletionTokens: 200, TotalTokens: 300},
	}, nil
}

func (m *mockBenchProvider) Stream(ctx context.Context, req *llm.CompletionRequest) (<-chan llm.Chunk, error) {
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

func (m *mockBenchProvider) CallTools(_ context.Context, _ *llm.ToolCallRequest) (*llm.ToolCallResponse, error) {
	return &llm.ToolCallResponse{Content: ""}, nil
}

func (m *mockBenchProvider) Info() llm.ModelInfo {
	return llm.ModelInfo{Name: "mock-bench", Provider: "mock", MaxContext: 8192}
}

// benchmarkFixture 构造两条用例：一条需要完整代码，一条关键词即可。
func benchmarkFixture() []EvalCase {
	return []EvalCase{
		{
			ID:           "impl_go_fibonacci",
			Name:         "实现斐波那契",
			Category:     CategoryCoding,
			HarnessPhase: PhaseImplement,
			Lang:         LangGo,
			Input:        "用 Go 实现 func Fibonacci(n int) int",
			Expected:     "Fibonacci",
			Threshold:    0.8,
			Requires:     []string{"func Fibonacci(", "if n < 0", "return"},
		},
		{
			ID:           "chat_greeting",
			Name:         "基础问候",
			Category:     CategoryChat,
			HarnessPhase: PhasePlan,
			Lang:         LangGeneric,
			Input:        "Hello!",
			Expected:     "Hello",
			Threshold:    0.5,
			Requires:     []string{"Hello"},
		},
	}
}

const goodFib = "func Fibonacci(n int) int { if n < 0 { return -1 }; if n < 2 { return n }; return Fibonacci(n-1)+Fibonacci(n-2) }"

// TestLLMBenchAgent_Run 验证 Agent 累计用量与调用次数。
func TestLLMBenchAgent_Run(t *testing.T) {
	p := &mockBenchProvider{responses: []string{"hello", "world"}}
	a := NewLLMBenchAgent(p, "mock-model")

	out, err := a.Run(context.Background(), "Hello!")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if out != "hello" {
		t.Errorf("output = %q, want hello", out)
	}
	if a.CallCount() != 1 {
		t.Errorf("CallCount = %d, want 1", a.CallCount())
	}
	u := a.Usage()
	if u.PromptTokens != 100 || u.CompletionTokens != 200 || u.TotalTokens != 300 {
		t.Errorf("usage = %+v, want 100/200/300", u)
	}
}

// TestLLMBenchAgent_Error 验证 Provider 错误透传。
func TestLLMBenchAgent_Error(t *testing.T) {
	wantErr := errors.New("provider down")
	p := &mockBenchProvider{failCalls: map[int]error{1: wantErr}}
	a := NewLLMBenchAgent(p, "mock-model")

	_, err := a.Run(context.Background(), "x")
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
}

// TestRunLLMBench_Pass 验证全通过场景：通过率/成本/耗时/门禁。
func TestRunLLMBench_Pass(t *testing.T) {
	p := &mockBenchProvider{responses: []string{goodFib, "Hello there!"}}
	a := NewLLMBenchAgent(p, "mock-model")
	cfg := LLMBenchConfig{Version: "v3.5.0", Model: "mock-model", ProviderName: "mock", Threshold: 0.8}

	res, err := RunLLMBench(context.Background(), cfg, a, benchmarkFixture())
	if err != nil {
		t.Fatalf("RunLLMBench failed: %v", err)
	}
	if res.Total != 2 || res.Passed != 2 || res.Failed != 0 {
		t.Errorf("total/passed/failed = %d/%d/%d, want 2/2/0", res.Total, res.Passed, res.Failed)
	}
	if res.PassRate != 1.0 {
		t.Errorf("PassRate = %f, want 1.0", res.PassRate)
	}
	if res.TotalTokens != 600 {
		t.Errorf("TotalTokens = %d, want 600", res.TotalTokens)
	}
	if res.LatencyMs < 0 {
		t.Error("LatencyMs 不应为负数")
	}
	if res.AvgLatencyMs < 0 {
		t.Error("AvgLatencyMs 不应为负数")
	}
	if !res.MeetsGate {
		t.Error("PassRate 1.0 应通过门禁")
	}
	if res.Generated == "" {
		t.Error("Generated 时间戳为空")
	}
}

// TestRunLLMBench_Recovery 验证恢复率：首轮失败、重试成功计入 Recovered。
func TestRunLLMBench_Recovery(t *testing.T) {
	// 第一个用例：首轮给出缺 "if n < 0" 的代码 → 失败；第二轮给出完整代码 → 恢复
	// 第二个用例：首轮给出 "Hi"（不含 Hello）→ 失败；第二轮给出 "Hello!" → 恢复
	p := &mockBenchProvider{responses: []string{
		"func Fibonacci(n int) int { return n }", // 缺 if n < 0 → 失败
		goodFib,                                  // 恢复
		"Hi there!",                              // 缺 Hello → 失败
		"Hello there!",                           // 恢复
	}}
	a := NewLLMBenchAgent(p, "mock-model")
	cfg := LLMBenchConfig{Version: "v3.5.0", Model: "mock-model", ProviderName: "mock", Retries: 1, Threshold: 0.0}

	res, err := RunLLMBench(context.Background(), cfg, a, benchmarkFixture())
	if err != nil {
		t.Fatalf("RunLLMBench failed: %v", err)
	}
	if res.Passed != 2 {
		t.Errorf("Passed = %d, want 2（重试恢复后全部通过）", res.Passed)
	}
	if res.RecoveryRate != 1.0 {
		t.Errorf("RecoveryRate = %f, want 1.0（两例均恢复）", res.RecoveryRate)
	}
	for _, cr := range res.Cases {
		if !cr.Recovered {
			t.Errorf("case %s 应标记 Recovered", cr.CaseID)
		}
		if cr.Attempts != 2 {
			t.Errorf("case %s Attempts = %d, want 2", cr.CaseID, cr.Attempts)
		}
	}
}

// TestRunLLMBench_Unrecovered 验证恢复率非 1：存在无法恢复的用例。
func TestRunLLMBench_Unrecovered(t *testing.T) {
	// 第一个用例始终缺 "if n < 0"，重试也失败 → 无法恢复
	p := &mockBenchProvider{responses: []string{
		"func Fibonacci(n int) int { return n }",
		"func Fibonacci(n int) int { return n }",
		"Hello there!",
	}}
	a := NewLLMBenchAgent(p, "mock-model")
	cfg := LLMBenchConfig{Version: "v3.5.0", Model: "mock-model", ProviderName: "mock", Retries: 1, Threshold: 0.0}

	res, err := RunLLMBench(context.Background(), cfg, a, benchmarkFixture())
	if err != nil {
		t.Fatalf("RunLLMBench failed: %v", err)
	}
	if res.Passed != 1 || res.Failed != 1 {
		t.Errorf("Passed/Failed = %d/%d, want 1/1", res.Passed, res.Failed)
	}
	// 失败 1 例且未恢复 → 恢复率 0
	if res.RecoveryRate != 0 {
		t.Errorf("RecoveryRate = %f, want 0", res.RecoveryRate)
	}
}

// TestRunLLMBench_Gate 验证门禁：通过率低于基线判失败。
func TestRunLLMBench_Gate(t *testing.T) {
	p := &mockBenchProvider{responses: []string{"bad code", "Hi there!"}}
	a := NewLLMBenchAgent(p, "mock-model")
	// 全失败：通过率 0；基线 0.5 → 不达标
	cfg := LLMBenchConfig{Version: "v3.5.0", Model: "mock-model", ProviderName: "mock", Baseline: 0.5, Threshold: 0.0}
	res, err := RunLLMBench(context.Background(), cfg, a, benchmarkFixture())
	if err != nil {
		t.Fatalf("RunLLMBench failed: %v", err)
	}
	if res.PassRate != 0 {
		t.Errorf("PassRate = %f, want 0", res.PassRate)
	}
	if res.MeetsGate {
		t.Error("通过率低于基线不应达标")
	}
}

// TestRunLLMBench_ProviderError 验证 Provider 错误计入失败并记录 Error。
func TestRunLLMBench_ProviderError(t *testing.T) {
	p := &mockBenchProvider{failCalls: map[int]error{1: errors.New("timeout"), 2: errors.New("timeout")}}
	a := NewLLMBenchAgent(p, "mock-model")
	cfg := LLMBenchConfig{Version: "v3.5.0", Model: "mock-model", ProviderName: "mock", Retries: 0, Threshold: 0.0}

	res, err := RunLLMBench(context.Background(), cfg, a, benchmarkFixture())
	if err != nil {
		t.Fatalf("RunLLMBench failed: %v", err)
	}
	if res.Failed != 2 {
		t.Errorf("Failed = %d, want 2", res.Failed)
	}
	for _, cr := range res.Cases {
		if !strings.Contains(cr.Error, "timeout") {
			t.Errorf("case %s 应记录 timeout 错误, got %q", cr.CaseID, cr.Error)
		}
	}
}
