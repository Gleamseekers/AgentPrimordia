package agent

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools"
)

// ===== 流式 Mock LLM =====

// streamMockLLM 支持逐 token 流式输出的 Mock
type streamMockLLM struct {
	chunks       []string
	toolCalls    []llm.FunctionCall
	toolContent  string
	finalResp    string
	streamErr    error
	completeErr  error
	toolCallUsed bool
}

func (m *streamMockLLM) Complete(_ context.Context, _ *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if m.completeErr != nil {
		return nil, m.completeErr
	}
	content := m.finalResp
	if content == "" {
		content = "fallback response"
	}
	return &llm.CompletionResponse{
		ID:      "stream-mock-complete",
		Content: content,
		Role:    "assistant",
		Usage:   llm.Usage{PromptTokens: 5, CompletionTokens: 10},
	}, nil
}

func (m *streamMockLLM) Stream(_ context.Context, _ *llm.CompletionRequest) (<-chan llm.Chunk, error) {
	if m.streamErr != nil {
		return nil, m.streamErr
	}
	ch := make(chan llm.Chunk, len(m.chunks)+1)
	go func() {
		defer close(ch)
		for _, c := range m.chunks {
			ch <- llm.Chunk{Content: c}
		}
		ch <- llm.Chunk{Done: true, Usage: &llm.Usage{PromptTokens: 5, CompletionTokens: len(m.chunks)}}
	}()
	return ch, nil
}

func (m *streamMockLLM) CallTools(_ context.Context, _ *llm.ToolCallRequest) (*llm.ToolCallResponse, error) {
	if m.toolCallUsed {
		return &llm.ToolCallResponse{
			Content:   m.finalResp,
			ToolCalls: nil,
			Usage:     llm.Usage{PromptTokens: 10, CompletionTokens: 20},
		}, nil
	}
	m.toolCallUsed = true
	resp := &llm.ToolCallResponse{
		Content:   m.toolContent,
		ToolCalls: m.toolCalls,
		Usage:     llm.Usage{PromptTokens: 10, CompletionTokens: 20},
	}
	return resp, nil
}

func (m *streamMockLLM) Embeddings(_ context.Context, _ []string) ([][]float32, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *streamMockLLM) Info() llm.ModelInfo {
	return llm.ModelInfo{
		Name:              "stream-test-mock",
		Provider:          "mock",
		MaxContext:        4096,
		SupportsTools:     true,
		SupportsStreaming: true,
	}
}

// ===== StreamRun 测试 =====

func TestReActAgent_StreamRun_BasicCompletion(t *testing.T) {
	mock := &streamMockLLM{
		chunks:    []string{"Hello", " world", "!"},
		finalResp: "Hello world!",
	}

	agent, err := NewAgent("stream-basic", "", mock, WithMaxTurns(10))
	if err != nil {
		t.Fatalf("NewAgent() error = %v", err)
	}
	agent = agent.WithToolkit(tools.NewRegistry())

	ch, err := agent.StreamRun(context.Background(), UserMessage("Hi"))
	if err != nil {
		t.Fatalf("StreamRun() error = %v", err)
	}

	var tokens []string
	var gotComplete bool
	var gotThought bool
	for evt := range ch {
		switch evt.Type {
		case StreamEventToken:
			tokens = append(tokens, evt.Content)
		case StreamEventComplete:
			gotComplete = true
			if resp, ok := evt.Data.(*Response); ok {
				if resp.Content != "Hello world!" {
					t.Errorf("complete event content = %q, want %q", resp.Content, "Hello world!")
				}
				if resp.Metrics.TotalTurns != 1 {
					t.Errorf("TotalTurns = %d, want 1", resp.Metrics.TotalTurns)
				}
			}
		case StreamEventThought:
			gotThought = true
		}
	}

	if !gotComplete {
		t.Error("expected StreamEventComplete event")
	}
	if len(tokens) < 2 {
		t.Errorf("expected at least 2 token events, got %d", len(tokens))
	}
	if gotThought {
		t.Error("unexpected StreamEventThought for basic completion without tools")
	}
}

func TestReActAgent_StreamRun_WithToolCall(t *testing.T) {
	mock := &streamMockLLM{
		chunks: []string{"Let me", " check", " that"},
		toolCalls: []llm.FunctionCall{
			{ID: "call_1", Name: "get_time", Arguments: "{}"},
		},
		toolContent: "",
		finalResp:   "The current time is 12:00 PM.",
	}

	registry := tools.NewRegistry()
	_ = registry.Register(&mockTimeTool{name: "get_time"})

	agent, err := NewAgent("stream-tool", "", mock, WithMaxTurns(10))
	if err != nil {
		t.Fatalf("NewAgent() error = %v", err)
	}
	agent = agent.WithToolkit(registry)

	ch, err := agent.StreamRun(context.Background(), UserMessage("What time?"))
	if err != nil {
		t.Fatalf("StreamRun() error = %v", err)
	}

	var toolCallEvents int
	var toolResultEvents int
	var gotComplete bool
	for evt := range ch {
		switch evt.Type {
		case StreamEventToolCall:
			toolCallEvents++
		case StreamEventToolResult:
			toolResultEvents++
		case StreamEventComplete:
			gotComplete = true
		}
	}

	if toolCallEvents != 1 {
		t.Errorf("tool call events = %d, want 1", toolCallEvents)
	}
	if toolResultEvents != 1 {
		t.Errorf("tool result events = %d, want 1", toolResultEvents)
	}
	if !gotComplete {
		t.Error("expected StreamEventComplete event")
	}
}

func TestReActAgent_StreamRun_MaxTurnsExceeded(t *testing.T) {
	loopMock := &streamLoopLLM{maxToolCalls: 5}

	registry := tools.NewRegistry()
	_ = registry.Register(&mockTool{name: "loop_tool", response: "more data"})

	agent, err := NewAgent("stream-max-turns", "", loopMock, WithMaxTurns(3))
	if err != nil {
		t.Fatalf("NewAgent() error = %v", err)
	}
	agent = agent.WithToolkit(registry)

	ch, err := agent.StreamRun(context.Background(), UserMessage("Loop"))
	if err != nil {
		t.Fatalf("StreamRun() error = %v", err)
	}

	var gotError bool
	for evt := range ch {
		if evt.Type == StreamEventError {
			gotError = true
		}
	}

	if !gotError {
		t.Error("expected StreamEventError for max turns exceeded")
	}
}

func TestReActAgent_StreamRun_ContextCancelled(t *testing.T) {
	slowMock := &streamMockLLM{
		chunks:    []string{"slow"},
		finalResp: "slow response",
	}

	agent, err := NewAgent("stream-cancel", "", slowMock, WithMaxTurns(10))
	if err != nil {
		t.Fatalf("NewAgent() error = %v", err)
	}
	agent = agent.WithToolkit(tools.NewRegistry())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ch, err := agent.StreamRun(ctx, UserMessage("test"))
	if err != nil {
		t.Fatalf("StreamRun() error = %v", err)
	}

	for evt := range ch {
		if evt.Type == StreamEventError {
			return
		}
	}
}

func TestReActAgent_StreamRun_StreamErrorFallback(t *testing.T) {
	mock := &streamMockLLM{
		streamErr:   errors.New("stream not available"),
		finalResp:   "Fallback response",
		completeErr: nil,
	}

	agent, err := NewAgent("stream-fallback", "", mock, WithMaxTurns(10))
	if err != nil {
		t.Fatalf("NewAgent() error = %v", err)
	}
	agent = agent.WithToolkit(tools.NewRegistry())

	ch, err := agent.StreamRun(context.Background(), UserMessage("test"))
	if err != nil {
		t.Fatalf("StreamRun() error = %v", err)
	}

	var gotComplete bool
	for evt := range ch {
		if evt.Type == StreamEventComplete {
			gotComplete = true
			if resp, ok := evt.Data.(*Response); ok {
				if resp.Content != "Fallback response" {
					t.Errorf("fallback content = %q, want %q", resp.Content, "Fallback response")
				}
			}
		}
	}

	if !gotComplete {
		t.Error("expected StreamEventComplete after fallback")
	}
}

func TestReActAgent_StreamRun_WithMetrics(t *testing.T) {
	mock := &streamMockLLM{
		chunks:    []string{"Hello"},
		finalResp: "Hello",
	}

	recorder := &mockMetricsRecorder{}

	agent, err := NewAgent("stream-metrics", "", mock, WithMaxTurns(10))
	if err != nil {
		t.Fatalf("NewAgent() error = %v", err)
	}
	agent = agent.WithToolkit(tools.NewRegistry()).WithMetrics(recorder)

	ch, err := agent.StreamRun(context.Background(), UserMessage("test"))
	if err != nil {
		t.Fatalf("StreamRun() error = %v", err)
	}

	for range ch {
	}

	if recorder.activeAgents != 0 {
		t.Errorf("activeAgents = %d, want 0 after completion", recorder.activeAgents)
	}
	if recorder.llmCalls == 0 {
		t.Error("expected at least 1 LLM call recorded")
	}
	if recorder.turns == 0 {
		t.Error("expected at least 1 turn recorded")
	}
}

func TestReActAgent_StreamRun_WithEventPublisher(t *testing.T) {
	mock := &streamMockLLM{
		chunks:    []string{"Hello"},
		finalResp: "Hello",
	}

	pub := &mockEventPublisher{}

	agent, err := NewAgent("stream-events", "", mock, WithMaxTurns(10))
	if err != nil {
		t.Fatalf("NewAgent() error = %v", err)
	}
	agent = agent.WithToolkit(tools.NewRegistry()).WithEvents(pub)

	ch, err := agent.StreamRun(context.Background(), UserMessage("test"))
	if err != nil {
		t.Fatalf("StreamRun() error = %v", err)
	}

	for range ch {
	}

	if len(pub.events) == 0 {
		t.Error("expected events to be published")
	}

	hasStart := false
	hasStop := false
	for _, e := range pub.events {
		if e.eventType == EventAgentStart {
			hasStart = true
		}
		if e.eventType == EventAgentStop {
			hasStop = true
		}
	}
	if !hasStart {
		t.Error("expected agent.start event")
	}
	if !hasStop {
		t.Error("expected agent.stop event")
	}
}

// ===== 辅助 Mock 类型 =====

type mockMetricsRecorder struct {
	llmCalls     int
	toolCalls    int
	turns        int
	activeAgents int
}

func (m *mockMetricsRecorder) RecordLLMCall(_ time.Duration, _ error) {
	m.llmCalls++
}

func (m *mockMetricsRecorder) RecordToolCall(_ time.Duration, _ error) {
	m.toolCalls++
}

func (m *mockMetricsRecorder) RecordTurn(_ time.Duration) {
	m.turns++
}

func (m *mockMetricsRecorder) IncActiveAgents() {
	m.activeAgents++
}

func (m *mockMetricsRecorder) DecActiveAgents() {
	m.activeAgents--
}

func (m *mockMetricsRecorder) RecordTokenUsage(_ string, _, _ int) {}

type mockPublishedEvent struct {
	eventType string
	source    string
	payload   any
}

type mockEventPublisher struct {
	events []mockPublishedEvent
}

func (m *mockEventPublisher) PublishAsync(eventType string, source string, payload any) error {
	m.events = append(m.events, mockPublishedEvent{
		eventType: eventType,
		source:    source,
		payload:   payload,
	})
	return nil
}

// streamLoopLLM 持续返回工具调用的 Mock，用于测试 MaxTurns
type streamLoopLLM struct {
	maxToolCalls int
	callCount    int
}

func (m *streamLoopLLM) Complete(_ context.Context, _ *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return &llm.CompletionResponse{
		ID:      "loop-complete",
		Content: "loop response",
		Role:    "assistant",
		Usage:   llm.Usage{PromptTokens: 5, CompletionTokens: 10},
	}, nil
}

func (m *streamLoopLLM) Stream(_ context.Context, _ *llm.CompletionRequest) (<-chan llm.Chunk, error) {
	ch := make(chan llm.Chunk, 2)
	go func() {
		defer close(ch)
		ch <- llm.Chunk{Content: "thinking"}
		ch <- llm.Chunk{Done: true, Usage: &llm.Usage{PromptTokens: 5, CompletionTokens: 1}}
	}()
	return ch, nil
}

func (m *streamLoopLLM) CallTools(_ context.Context, _ *llm.ToolCallRequest) (*llm.ToolCallResponse, error) {
	m.callCount++
	if m.callCount > m.maxToolCalls {
		return &llm.ToolCallResponse{
			Content:   "done",
			ToolCalls: nil,
			Usage:     llm.Usage{},
		}, nil
	}
	return &llm.ToolCallResponse{
		Content: "",
		ToolCalls: []llm.FunctionCall{
			{ID: fmt.Sprintf("call_%d", m.callCount), Name: "loop_tool", Arguments: "{}"},
		},
		Usage: llm.Usage{},
	}, nil
}

func (m *streamLoopLLM) Embeddings(_ context.Context, _ []string) ([][]float32, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *streamLoopLLM) Info() llm.ModelInfo {
	return llm.ModelInfo{
		Name:              "loop-mock",
		Provider:          "mock",
		MaxContext:        4096,
		SupportsTools:     true,
		SupportsStreaming: true,
	}
}
