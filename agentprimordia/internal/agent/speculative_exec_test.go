// speculative_exec_test.go — G2-2 投机执行单元测试
package agent

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm"
)

// ===== ToolResultPredictor 测试 =====

func TestPredictor_BasicHitMiss(t *testing.T) {
	p := NewToolResultPredictor(10)
	result := &SpeculativeToolResult{Content: "cached", ToolCallID: "tc1"}

	// 首次查询：miss
	if _, ok := p.Predict("search", "args1"); ok {
		t.Error("expected miss on first query")
	}

	// Record 后：hit
	p.Record("search", "args1", result)
	r, ok := p.Predict("search", "args1")
	if !ok {
		t.Fatal("expected hit after Record")
	}
	if r.Content != "cached" {
		t.Errorf("expected cached content, got %q", r.Content)
	}
}

func TestPredictor_ArgsHashDifferentiates(t *testing.T) {
	p := NewToolResultPredictor(10)
	r1 := &SpeculativeToolResult{Content: "result1"}
	r2 := &SpeculativeToolResult{Content: "result2"}

	p.Record("search", "args1", r1)
	p.Record("search", "args2", r2)

	r, _ := p.Predict("search", "args1")
	if r.Content != "result1" {
		t.Errorf("args1 lookup got %q", r.Content)
	}
	r, _ = p.Predict("search", "args2")
	if r.Content != "result2" {
		t.Errorf("args2 lookup got %q", r.Content)
	}
}

func TestPredictor_ToolNameDifferentiates(t *testing.T) {
	p := NewToolResultPredictor(10)
	p.Record("search", "x", &SpeculativeToolResult{Content: "s"})
	p.Record("calc", "x", &SpeculativeToolResult{Content: "c"})

	r, _ := p.Predict("search", "x")
	if r.Content != "s" {
		t.Errorf("search got %q", r.Content)
	}
	r, _ = p.Predict("calc", "x")
	if r.Content != "c" {
		t.Errorf("calc got %q", r.Content)
	}
}

func TestPredictor_LRUEviction(t *testing.T) {
	p := NewToolResultPredictor(3)
	for i := 0; i < 5; i++ {
		p.Record("tool", string(rune('a'+i)), &SpeculativeToolResult{Content: string(rune('a' + i))})
	}
	stats := p.Stats()
	if stats.Size != 3 {
		t.Errorf("expected size 3, got %d", stats.Size)
	}
	// 最早的两个（a, b）应该被淘汰
	if _, ok := p.Predict("tool", "a"); ok {
		t.Error("expected 'a' to be evicted")
	}
	if _, ok := p.Predict("tool", "e"); !ok {
		t.Error("expected 'e' to be present")
	}
}

func TestPredictor_LRUTouch(t *testing.T) {
	p := NewToolResultPredictor(3)
	p.Record("tool", "a", &SpeculativeToolResult{Content: "a"})
	p.Record("tool", "b", &SpeculativeToolResult{Content: "b"})
	p.Record("tool", "c", &SpeculativeToolResult{Content: "c"})
	// 触摸 a
	_, _ = p.Predict("tool", "a")
	// 再 record d → b 应被淘汰（a 是最新的）
	p.Record("tool", "d", &SpeculativeToolResult{Content: "d"})
	if _, ok := p.Predict("tool", "b"); ok {
		t.Error("expected 'b' to be evicted (a was touched)")
	}
	if _, ok := p.Predict("tool", "a"); !ok {
		t.Error("expected 'a' to remain (touched)")
	}
}

func TestPredictor_HitRate(t *testing.T) {
	p := NewToolResultPredictor(10)
	p.Record("tool", "x", &SpeculativeToolResult{Content: "ok"})
	// 3 hits
	_, _ = p.Predict("tool", "x")
	_, _ = p.Predict("tool", "x")
	_, _ = p.Predict("tool", "x")
	// 2 misses
	_, _ = p.Predict("tool", "y")
	_, _ = p.Predict("tool", "z")
	stats := p.Stats()
	if stats.Hits != 3 {
		t.Errorf("expected 3 hits, got %d", stats.Hits)
	}
	if stats.Misses != 2 {
		t.Errorf("expected 2 misses, got %d", stats.Misses)
	}
	if stats.HitRate < 0.59 || stats.HitRate > 0.61 {
		t.Errorf("expected hit rate 0.6, got %f", stats.HitRate)
	}
}

func TestPredictor_Clear(t *testing.T) {
	p := NewToolResultPredictor(10)
	p.Record("tool", "x", &SpeculativeToolResult{Content: "ok"})
	_, _ = p.Predict("tool", "x") // hit
	p.Clear()
	if _, ok := p.Predict("tool", "x"); ok {
		t.Error("expected miss after Clear")
	}
	if p.Stats().Hits != 0 {
		t.Error("expected hits reset to 0")
	}
}

func TestPredictor_NilSafe(t *testing.T) {
	var p *ToolResultPredictor
	// 不应 panic
	if _, ok := p.Predict("t", "a"); ok {
		t.Error("nil predictor should return false")
	}
	p.Record("t", "a", &SpeculativeToolResult{Content: "x"})
	if p.Stats().Size != 0 {
		t.Error("nil predictor stats should be empty")
	}
}

// ===== SpeculativeExecutor 测试 =====

// slowProvider 模拟一个慢速 LLM Provider
type slowProvider struct {
	delay time.Duration
	resp  string
}

func (s *slowProvider) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	select {
	case <-time.After(s.delay):
		return &llm.CompletionResponse{Content: s.resp}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (s *slowProvider) Stream(ctx context.Context, req *llm.CompletionRequest) (<-chan llm.Chunk, error) {
	ch := make(chan llm.Chunk, 1)
	ch <- llm.Chunk{Content: s.resp, Done: true}
	close(ch)
	return ch, nil
}
func (s *slowProvider) CallTools(ctx context.Context, req *llm.ToolCallRequest) (*llm.ToolCallResponse, error) {
	return &llm.ToolCallResponse{}, nil
}
func (s *slowProvider) Info() llm.ModelInfo { return llm.ModelInfo{Name: "slow", Provider: "mock"} }

func TestSpeculativeExecutor_AllHit(t *testing.T) {
	p := NewToolResultPredictor(10)
	p.Record("search", "q1", &SpeculativeToolResult{Content: "cached1", ToolCallID: "tc1"})
	p.Record("search", "q2", &SpeculativeToolResult{Content: "cached2", ToolCallID: "tc2"})

	exec := NewSpeculativeExecutor(nil, p, 0.1, 100*time.Millisecond)
	calls := []SpeculativeToolCall{
		{ID: "tc1", Name: "search", Args: "q1"},
		{ID: "tc2", Name: "search", Args: "q2"},
	}
	var executed atomic.Int32
	execFn := func(ctx context.Context, tc SpeculativeToolCall) (*SpeculativeToolResult, error) {
		executed.Add(1)
		return nil, nil
	}

	res, err := exec.ExecuteWithSpeculation(context.Background(), calls, execFn, nil)
	if err != nil {
		t.Fatalf("ExecuteWithSpeculation failed: %v", err)
	}
	if executed.Load() != 0 {
		t.Errorf("expected 0 actual executions (all hit), got %d", executed.Load())
	}
	if res.PredictionHits != 2 {
		t.Errorf("expected 2 hits, got %d", res.PredictionHits)
	}
	if len(res.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(res.Results))
	}
	if res.Results[0].Content != "cached1" {
		t.Errorf("expected cached1, got %q", res.Results[0].Content)
	}
}

func TestSpeculativeExecutor_AllMiss(t *testing.T) {
	p := NewToolResultPredictor(10)
	exec := NewSpeculativeExecutor(nil, p, 0.1, 100*time.Millisecond)
	calls := []SpeculativeToolCall{
		{ID: "tc1", Name: "search", Args: "q1"},
		{ID: "tc2", Name: "search", Args: "q2"},
	}
	var executed atomic.Int32
	execFn := func(ctx context.Context, tc SpeculativeToolCall) (*SpeculativeToolResult, error) {
		executed.Add(1)
		return &SpeculativeToolResult{
			ToolCallID: tc.ID,
			Content:    "result-" + tc.Args,
		}, nil
	}

	res, err := exec.ExecuteWithSpeculation(context.Background(), calls, execFn, nil)
	if err != nil {
		t.Fatalf("ExecuteWithSpeculation failed: %v", err)
	}
	if executed.Load() != 2 {
		t.Errorf("expected 2 actual executions (all miss), got %d", executed.Load())
	}
	if res.PredictionHits != 0 {
		t.Errorf("expected 0 hits, got %d", res.PredictionHits)
	}
	// Record 后应可命中
	_, ok := p.Predict("search", "q1")
	if !ok {
		t.Error("expected q1 to be recorded after miss")
	}
}

func TestSpeculativeExecutor_PartialHit(t *testing.T) {
	p := NewToolResultPredictor(10)
	p.Record("search", "cached", &SpeculativeToolResult{Content: "hit", ToolCallID: "tc1"})

	exec := NewSpeculativeExecutor(nil, p, 0.1, 100*time.Millisecond)
	calls := []SpeculativeToolCall{
		{ID: "tc1", Name: "search", Args: "cached"},
		{ID: "tc2", Name: "search", Args: "fresh"},
	}
	var executed atomic.Int32
	execFn := func(ctx context.Context, tc SpeculativeToolCall) (*SpeculativeToolResult, error) {
		executed.Add(1)
		return &SpeculativeToolResult{Content: "exec-" + tc.Args}, nil
	}

	res, err := exec.ExecuteWithSpeculation(context.Background(), calls, execFn, nil)
	if err != nil {
		t.Fatalf("ExecuteWithSpeculation failed: %v", err)
	}
	if executed.Load() != 1 {
		t.Errorf("expected 1 execution (1 hit, 1 miss), got %d", executed.Load())
	}
	if res.PredictionHits != 1 {
		t.Errorf("expected 1 hit, got %d", res.PredictionHits)
	}
	if res.Results[0].Content != "hit" {
		t.Errorf("expected cached content for tc1, got %q", res.Results[0].Content)
	}
	if res.Results[1].Content != "exec-fresh" {
		t.Errorf("expected exec result for tc2, got %q", res.Results[1].Content)
	}
}

func TestSpeculativeExecutor_EmptyList(t *testing.T) {
	p := NewToolResultPredictor(10)
	exec := NewSpeculativeExecutor(nil, p, 0.1, 100*time.Millisecond)
	res, err := exec.ExecuteWithSpeculation(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("ExecuteWithSpeculation failed: %v", err)
	}
	if !res.Skipped {
		t.Error("expected Skipped=true for empty list")
	}
}

func TestSpeculativeExecutor_ExecutionError(t *testing.T) {
	p := NewToolResultPredictor(10)
	exec := NewSpeculativeExecutor(nil, p, 0.1, 100*time.Millisecond)
	calls := []SpeculativeToolCall{{ID: "tc1", Name: "failing", Args: "x"}}
	wantErr := errors.New("execution failed")
	execFn := func(ctx context.Context, tc SpeculativeToolCall) (*SpeculativeToolResult, error) {
		return &SpeculativeToolResult{Content: "error", IsError: true}, wantErr
	}
	res, err := exec.ExecuteWithSpeculation(context.Background(), calls, execFn, nil)
	// spec 层的 ExecuteWithSpeculation 应吞掉 execFn 返回的 err（result 仍记录）
	if err != nil {
		t.Fatalf("ExecuteWithSpeculation should swallow tool errors: %v", err)
	}
	if res.Results[0].IsError != true {
		t.Error("expected IsError=true on error result")
	}
}

func TestSpeculativeExecutor_ContextCancel(t *testing.T) {
	p := NewToolResultPredictor(10)
	exec := NewSpeculativeExecutor(nil, p, 0.1, 100*time.Millisecond)
	calls := []SpeculativeToolCall{{ID: "tc1", Name: "long", Args: "x"}}
	execFn := func(ctx context.Context, tc SpeculativeToolCall) (*SpeculativeToolResult, error) {
		select {
		case <-time.After(1 * time.Second):
			return &SpeculativeToolResult{Content: "ok"}, nil
		case <-ctx.Done():
			return &SpeculativeToolResult{Content: "canceled", IsError: true}, ctx.Err()
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	res, err := exec.ExecuteWithSpeculation(ctx, calls, execFn, nil)
	if err != nil {
		t.Fatalf("ExecuteWithSpeculation failed: %v", err)
	}
	if res.Results[0].IsError != true {
		t.Error("expected IsError=true after context cancel")
	}
}

func TestSpeculativeExecutor_SpeculativeLLMHit(t *testing.T) {
	p := NewToolResultPredictor(10)
	provider := &slowProvider{delay: 5 * time.Millisecond, resp: "speculative answer"}
	exec := NewSpeculativeExecutor(provider, p, 0.1, 1*time.Second)
	calls := []SpeculativeToolCall{{ID: "tc1", Name: "search", Args: "x"}}
	execFn := func(ctx context.Context, tc SpeculativeToolCall) (*SpeculativeToolResult, error) {
		// 工具执行慢于 LLM 投机调用 → 投机命中
		time.Sleep(50 * time.Millisecond)
		return &SpeculativeToolResult{Content: "tool result"}, nil
	}
	specMsgs := []llm.ChatMessage{{Role: "user", Content: "predict next"}}

	res, err := exec.ExecuteWithSpeculation(context.Background(), calls, execFn, specMsgs)
	if err != nil {
		t.Fatalf("ExecuteWithSpeculation failed: %v", err)
	}
	if !res.SpeculativeLLMHit {
		t.Error("expected speculative LLM hit (LLM faster than tool)")
	}
	if res.SpeculativeLLMResp == nil || res.SpeculativeLLMResp.Content != "speculative answer" {
		t.Errorf("expected speculative resp, got %+v", res.SpeculativeLLMResp)
	}
}

func TestSpeculativeExecutor_SpeculativeLLMNoProvider(t *testing.T) {
	p := NewToolResultPredictor(10)
	exec := NewSpeculativeExecutor(nil, p, 0.1, 100*time.Millisecond) // provider=nil
	calls := []SpeculativeToolCall{{ID: "tc1", Name: "search", Args: "x"}}
	execFn := func(ctx context.Context, tc SpeculativeToolCall) (*SpeculativeToolResult, error) {
		return &SpeculativeToolResult{Content: "ok"}, nil
	}
	res, err := exec.ExecuteWithSpeculation(context.Background(), calls, execFn, []llm.ChatMessage{{Role: "user"}})
	if err != nil {
		t.Fatalf("ExecuteWithSpeculation failed: %v", err)
	}
	if res.SpeculativeLLMHit {
		t.Error("expected no speculative hit when provider is nil")
	}
}

func TestSpeculativeExecutor_StatsAccumulate(t *testing.T) {
	p := NewToolResultPredictor(10)
	p.Record("tool", "hit", &SpeculativeToolResult{Content: "x"})
	exec := NewSpeculativeExecutor(&slowProvider{delay: 1 * time.Millisecond, resp: "r"}, p, 0.1, 1*time.Second)
	execFn := func(ctx context.Context, tc SpeculativeToolCall) (*SpeculativeToolResult, error) {
		return &SpeculativeToolResult{Content: "exec"}, nil
	}
	calls := []SpeculativeToolCall{
		{ID: "1", Name: "tool", Args: "hit"},
		{ID: "2", Name: "tool", Args: "miss"},
	}
	_, err := exec.ExecuteWithSpeculation(context.Background(), calls, execFn, []llm.ChatMessage{{Role: "user"}})
	if err != nil {
		t.Fatalf("failed: %v", err)
	}
	stats := exec.Stats()
	if stats.PredictionsAttempted != 2 {
		t.Errorf("expected 2 attempted, got %d", stats.PredictionsAttempted)
	}
	if stats.PredictionHits != 1 {
		t.Errorf("expected 1 hit, got %d", stats.PredictionHits)
	}
	if stats.SpeculativeLLMCalls != 1 {
		t.Errorf("expected 1 LLM call, got %d", stats.SpeculativeLLMCalls)
	}
}
