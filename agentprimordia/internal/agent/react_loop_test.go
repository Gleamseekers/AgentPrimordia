package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools"
)

func TestReActAgent_SimpleCompletion(t *testing.T) {
	t.Parallel()
	mockLLM := llm.NewMockLLM(t).WithResponse("Hello! I can help you.")

	registry := tools.NewRegistry()

	agent, err := NewAgent("test-agent", "", mockLLM, WithMaxTurns(10), WithToolkit(registry))
	if err != nil {
		t.Fatalf("unexpected error creating agent: %v", err)
	}

	resp, err := agent.Run(context.Background(), UserMessage("Hi there"))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("response should not have error: %v", resp.Error)
	}
	if resp.Content != "Hello! I can help you." {
		t.Errorf("expected 'Hello! I can help you.', got '%s'", resp.Content)
	}
	if resp.Metrics.TotalTurns != 1 {
		t.Errorf("expected 1 turn, got %d", resp.Metrics.TotalTurns)
	}
}

func TestReActAgent_SingleToolCall(t *testing.T) {
	t.Parallel()
	mockLLM := llm.NewMockLLM(t).
		WithToolResponse([]llm.FunctionCall{
			{ID: "call_1", Name: "get_time", Arguments: "{}"},
		}).
		WithResponse("The current time is 12:00 PM.")

	registry := tools.NewRegistry()
	_ = registry.Register(&mockTimeTool{name: "get_time"})

	agent, err := NewAgent("tool-agent", "", mockLLM, WithMaxTurns(10), WithToolkit(registry))
	if err != nil {
		t.Fatalf("unexpected error creating agent: %v", err)
	}

	resp, err := agent.Run(context.Background(), UserMessage("What time is it?"))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "The current time is 12:00 PM." {
		t.Errorf("unexpected response: %s", resp.Content)
	}
	if resp.Metrics.TotalTurns != 2 {
		t.Errorf("expected 2 turns, got %d", resp.Metrics.TotalTurns)
	}
	if resp.Metrics.TotalTools != 1 {
		t.Errorf("expected 1 tool call, got %d", resp.Metrics.TotalTools)
	}
}

func TestReActAgent_MultipleToolCalls(t *testing.T) {
	t.Parallel()
	mockLLM := llm.NewMockLLM(t).
		WithToolResponse([]llm.FunctionCall{
			{ID: "call_1", Name: "search", Arguments: `{"q":"golang"}`},
			{ID: "call_2", Name: "search", Arguments: `{"q":"rust"}`},
			{ID: "call_3", Name: "search", Arguments: `{"q":"python"}`},
		}).
		WithResponse("I've searched for all three languages.")

	registry := tools.NewRegistry()
	_ = registry.Register(&mockSearchTool{name: "search"})

	agent, err := NewAgent("multi-tool-agent", "", mockLLM, WithMaxTurns(20), WithToolkit(registry))
	if err != nil {
		t.Fatalf("unexpected error creating agent: %v", err)
	}

	resp, err := agent.Run(context.Background(), UserMessage("Compare Go, Rust, and Python"))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Metrics.TotalTools != 3 {
		t.Errorf("expected 3 tool calls, got %d", resp.Metrics.TotalTools)
	}
}

func TestReActAgent_MaxTurnsExceeded(t *testing.T) {
	t.Parallel()
	mockLLM := llm.NewMockLLM(t)
	for i := 0; i < 15; i++ {
		mockLLM.WithToolResponse([]llm.FunctionCall{
			{ID: "call_x", Name: "loop_tool", Arguments: "{}"},
		})
	}

	registry := tools.NewRegistry()
	_ = registry.Register(&mockTool{name: "loop_tool", response: "more data needed"})

	agent, err := NewAgent("loop-agent", "", mockLLM, WithMaxTurns(5), WithToolkit(registry))
	if err != nil {
		t.Fatalf("unexpected error creating agent: %v", err)
	}

	resp, err := agent.Run(context.Background(), UserMessage("Keep going"))

	if err != ErrMaxTurnsExceeded {
		t.Errorf("expected ErrMaxTurnsExceeded, got: %v", err)
	}
	if resp == nil {
		t.Fatal("response should not be nil even on error")
	}
	if resp.Error != ErrMaxTurnsExceeded {
		t.Errorf("response error should be ErrMaxTurnsExceeded")
	}
}

func TestReActAgent_LLMError(t *testing.T) {
	t.Parallel()
	mockLLM := llm.NewMockLLM(t).WithError(errors.New("API rate limited"))

	agent, err := NewAgent("error-agent", "", mockLLM, WithMaxTurns(10), WithToolkit(tools.NewRegistry()))
	if err != nil {
		t.Fatalf("unexpected error creating agent: %v", err)
	}

	resp, err := agent.Run(context.Background(), UserMessage("test"))

	if err == nil {
		t.Error("expected error, got nil")
	}
	if resp.Error == nil {
		t.Error("response should contain error")
	}
}

func TestReActAgent_ContextCanceled(t *testing.T) {
	t.Parallel()
	mockLLM := llm.NewMockLLM(t).WithResponse("slow response").WithDelay(2 * time.Second)

	agent, err := NewAgent("cancel-agent", "", mockLLM, WithMaxTurns(10), WithToolkit(tools.NewRegistry()))
	if err != nil {
		t.Fatalf("unexpected error creating agent: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err = agent.Run(ctx, UserMessage("test"))

	if err == nil {
		t.Error("expected context canceled error")
	}
}

func TestReActAgent_Hooks_Fired(t *testing.T) {
	t.Parallel()
	var reasoningCalled bool
	var toolUseCalled bool
	var completeCalled bool

	mockLLM := llm.NewMockLLM(t).
		WithToolResponse([]llm.FunctionCall{
			{ID: "call_1", Name: "echo", Arguments: `{"msg":"hello"}`},
		}).
		WithResponse("Done!")

	registry := tools.NewRegistry()
	_ = registry.Register(&mockEchoTool{name: "echo"})

	hooks := NewHookManager()
	hooks.Register(HookAfterLLM, func(ctx context.Context, hctx *HookContext) error {
		reasoningCalled = true
		return nil
	})
	hooks.Register(HookBeforeTool, func(ctx context.Context, hctx *HookContext) error {
		toolUseCalled = true
		return nil
	})
	hooks.Register(HookOnComplete, func(ctx context.Context, hctx *HookContext) error {
		completeCalled = true
		return nil
	})

	agent, err := NewAgent("hooks-agent", "", mockLLM, WithMaxTurns(10), WithToolkit(registry), WithHooks(hooks))
	if err != nil {
		t.Fatalf("unexpected error creating agent: %v", err)
	}

	_, err = agent.Run(context.Background(), UserMessage("test with hooks"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reasoningCalled {
		t.Error("OnReasoning hook should have been called")
	}
	if !toolUseCalled {
		t.Error("OnToolUse hook should have been called")
	}
	if !completeCalled {
		t.Error("OnComplete hook should have been called")
	}
}

func TestReActAgent_NoToolkit(t *testing.T) {
	t.Parallel()
	mockLLM := llm.NewMockLLM(t).WithResponse("Simple answer")

	agent, err := NewAgent("no-toolkit-agent", "", mockLLM, WithMaxTurns(10))
	if err != nil {
		t.Fatalf("unexpected error creating agent: %v", err)
	}

	resp, err := agent.Run(context.Background(), UserMessage("Simple question"))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Simple answer" {
		t.Errorf("unexpected content: %s", resp.Content)
	}
}

func TestReActAgent_Stats(t *testing.T) {
	t.Parallel()
	mockLLM := llm.NewMockLLM(t).WithResponse("Stats test")

	agent, err := NewAgent("stats-agent", "", mockLLM, WithMaxTurns(10), WithToolkit(tools.NewRegistry()))
	if err != nil {
		t.Fatalf("unexpected error creating agent: %v", err)
	}

	_, err = agent.Run(context.Background(), UserMessage("test"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stats := agent.Stats()
	if stats.Status != StatusCompleted {
		t.Errorf("expected StatusCompleted, got %s", stats.Status)
	}
	if stats.CurrentTurn != 1 {
		t.Errorf("expected CurrentTurn 1, got %d", stats.CurrentTurn)
	}
}

// ===== Mock Tools for Testing =====

type mockTimeTool struct {
	name string
}

func (m *mockTimeTool) Name() string        { return m.name }
func (m *mockTimeTool) Description() string { return "Get current time" }
func (m *mockTimeTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"required":[]}`)
}
func (m *mockTimeTool) Execute(ctx context.Context, args json.RawMessage) (*tools.Result, error) {
	return tools.NewResult(`{"time":"12:00 PM"}`), nil
}

type mockSearchTool struct {
	name string
}

func (m *mockSearchTool) Name() string        { return m.name }
func (m *mockSearchTool) Description() string { return "Search the web" }
func (m *mockSearchTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}`)
}
func (m *mockSearchTool) Execute(ctx context.Context, args json.RawMessage) (*tools.Result, error) {
	return tools.NewResult("search results"), nil
}

type mockEchoTool struct {
	name string
}

func (m *mockEchoTool) Name() string        { return m.name }
func (m *mockEchoTool) Description() string { return "Echo back input" }
func (m *mockEchoTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"msg":{"type":"string"}},"required":["msg"]}`)
}
func (m *mockEchoTool) Execute(ctx context.Context, args json.RawMessage) (*tools.Result, error) {
	return tools.NewResult("echoed"), nil
}

type mockTool struct {
	name     string
	response string
}

func (m *mockTool) Name() string        { return m.name }
func (m *mockTool) Description() string { return "A mock tool" }
func (m *mockTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"required":[]}`)
}
func (m *mockTool) Execute(ctx context.Context, args json.RawMessage) (*tools.Result, error) {
	return tools.NewResult(m.response), nil
}

func TestReActAgent_GracefulShutdown(t *testing.T) {
	t.Parallel()
	mockLLM := llm.NewMockLLM(t)
	mockLLM.WithToolResponse([]llm.FunctionCall{
		{ID: "tc1", Name: "echo", Arguments: `{"msg":"hi"}`},
	})
	mockLLM.WithResponse("final answer")

	registry := tools.NewRegistry()
	_ = registry.Register(&mockEchoTool{name: "echo"})

	agent, err := NewAgent("graceful-agent", "", mockLLM, WithMaxTurns(10), WithToolkit(registry))
	if err != nil {
		t.Fatalf("unexpected error creating agent: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = agent.Run(context.Background(), UserMessage("test"))
	}()

	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = agent.GracefulShutdown(ctx)
	if err != nil {
		t.Errorf("graceful shutdown should succeed, got %v", err)
	}

	<-done

	stats := agent.Stats()
	if stats.Status != StatusCancelled && stats.Status != StatusCompleted {
		t.Errorf("expected cancelled or completed status, got %s", stats.Status)
	}
}

func TestReActAgent_GracefulShutdown_TimeoutFallback(t *testing.T) {
	t.Parallel()
	mockLLM := llm.NewMockLLM(t).WithResponse("done").WithDelay(200 * time.Millisecond)

	agent, err := NewAgent("graceful-timeout-agent", "", mockLLM, WithMaxTurns(10), WithToolkit(tools.NewRegistry()))
	if err != nil {
		t.Fatalf("unexpected error creating agent: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = agent.Run(context.Background(), UserMessage("test"))
	}()

	time.Sleep(20 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err = agent.GracefulShutdown(ctx)
	if err == nil {
		t.Error("expected timeout error from graceful shutdown")
	}

	<-done
}

func TestReActAgent_WithCache(t *testing.T) {
	t.Parallel()
	mockLLM := llm.NewMockLLM(t).WithResponse("cached answer")

	cache := llm.NewFingerprintCache(100, 0)

	agent, err := NewAgent("cache-agent", "", mockLLM, WithMaxTurns(10), WithToolkit(tools.NewRegistry()), WithCache(cache))
	if err != nil {
		t.Fatalf("unexpected error creating agent: %v", err)
	}

	resp, err := agent.Run(context.Background(), UserMessage("what is Go?"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "cached answer" {
		t.Errorf("Content = %q", resp.Content)
	}

	stats := cache.Stats(context.Background())
	if stats.EntryCount != 1 {
		t.Errorf("EntryCount = %d, want 1", stats.EntryCount)
	}
}

func TestReActAgent_RequestID_AutoGenerated(t *testing.T) {
	t.Parallel()
	mockLLM := llm.NewMockLLM(t).WithResponse("Hello!")
	agent, err := NewAgent("reqid-agent", "", mockLLM, WithMaxTurns(10), WithToolkit(tools.NewRegistry()))
	if err != nil {
		t.Fatalf("unexpected error creating agent: %v", err)
	}

	resp, err := agent.Run(context.Background(), UserMessage("Hi"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.RequestID == "" {
		t.Error("RequestID should be auto-generated when not provided in context")
	}
	if len(resp.RequestID) != 32 {
		t.Errorf("RequestID length = %d, want 32 (16 bytes hex)", len(resp.RequestID))
	}

	// Stats 也应该包含 RequestID
	stats := agent.Stats()
	if stats.RequestID != resp.RequestID {
		t.Errorf("Stats.RequestID = %q, want %q", stats.RequestID, resp.RequestID)
	}
}

func TestReActAgent_RequestID_FromContext(t *testing.T) {
	t.Parallel()
	mockLLM := llm.NewMockLLM(t).WithResponse("Hello!")
	agent, err := NewAgent("reqid-ctx-agent", "", mockLLM, WithMaxTurns(10), WithToolkit(tools.NewRegistry()))
	if err != nil {
		t.Fatalf("unexpected error creating agent: %v", err)
	}

	// 通过 context 传入自定义请求 ID
	ctx := WithRequestID(context.Background(), "custom-req-123")
	resp, err := agent.Run(ctx, UserMessage("Hi"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.RequestID != "custom-req-123" {
		t.Errorf("RequestID = %q, want custom-req-123", resp.RequestID)
	}
}

func TestNewRequestID_Uniqueness(t *testing.T) {
	t.Parallel()
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := NewRequestID()
		if ids[id] {
			t.Fatalf("duplicate RequestID generated: %s", id)
		}
		ids[id] = true
	}
}

func TestRequestIDFromCtx_Empty(t *testing.T) {
	t.Parallel()
	id := RequestIDFromCtx(context.Background())
	if id != "" {
		t.Errorf("RequestIDFromCtx of empty context = %q, want empty", id)
	}
}
