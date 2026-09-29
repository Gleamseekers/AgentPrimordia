package testutil

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
)

func TestNewMockProvider_WithResponses(t *testing.T) {
	mp := NewMockProvider("hello", "world")

	resp1, err := mp.Complete(context.Background(), &llm.CompletionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp1.Content != "hello" {
		t.Errorf("expected 'hello', got '%s'", resp1.Content)
	}
	if resp1.Role != "assistant" {
		t.Errorf("expected role 'assistant', got '%s'", resp1.Role)
	}

	resp2, err := mp.Complete(context.Background(), &llm.CompletionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp2.Content != "world" {
		t.Errorf("expected 'world', got '%s'", resp2.Content)
	}
}

func TestMockProvider_Complete_DefaultResponse(t *testing.T) {
	mp := NewMockProvider() // 无预设响应

	resp, err := mp.Complete(context.Background(), &llm.CompletionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "This is a default mock response." {
		t.Errorf("unexpected content: %s", resp.Content)
	}
}

func TestMockProvider_CallTools_ReturnsPresetCalls(t *testing.T) {
	mp := NewMockProvider()
	mp.WithToolCalls(
		llm.FunctionCall{ID: "call-1", Name: "search", Arguments: `{"q":"test"}`},
		llm.FunctionCall{ID: "call-2", Name: "calc", Arguments: `{"expr":"1+1"}`},
	)

	resp, err := mp.CallTools(context.Background(), &llm.ToolCallRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "search" {
		t.Errorf("expected tool 'search', got '%s'", resp.ToolCalls[0].Name)
	}
	if resp.ToolCalls[1].Name != "calc" {
		t.Errorf("expected tool 'calc', got '%s'", resp.ToolCalls[1].Name)
	}
}

func TestMockProvider_CallTools_EmptyByDefault(t *testing.T) {
	mp := NewMockProvider()

	resp, err := mp.CallTools(context.Background(), &llm.ToolCallRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.ToolCalls) != 0 {
		t.Errorf("expected empty tool calls, got %d", len(resp.ToolCalls))
	}
}

func TestMockProvider_CallCount_TracksInvocations(t *testing.T) {
	mp := NewMockProvider("a", "b", "c")

	if mp.CallCount() != 0 {
		t.Errorf("expected 0 calls, got %d", mp.CallCount())
	}

	_, _ = mp.Complete(context.Background(), &llm.CompletionRequest{})
	if mp.CallCount() != 1 {
		t.Errorf("expected 1 call, got %d", mp.CallCount())
	}

	_, _ = mp.CallTools(context.Background(), &llm.ToolCallRequest{})
	if mp.CallCount() != 2 {
		t.Errorf("expected 2 calls, got %d", mp.CallCount())
	}

	_, _ = mp.Complete(context.Background(), &llm.CompletionRequest{})
	_, _ = mp.Complete(context.Background(), &llm.CompletionRequest{})
	if mp.CallCount() != 4 {
		t.Errorf("expected 4 calls, got %d", mp.CallCount())
	}
}

func TestMockProvider_WithError(t *testing.T) {
	expectedErr := errors.New("injected error")
	mp := NewMockProvider("hello").WithError(expectedErr)

	_, err := mp.Complete(context.Background(), &llm.CompletionRequest{})
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected injected error, got %v", err)
	}

	_, err = mp.CallTools(context.Background(), &llm.ToolCallRequest{})
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected injected error for CallTools, got %v", err)
	}
}

func TestMockProvider_WithDelay(t *testing.T) {
	mp := NewMockProvider("hello").WithDelay(50 * time.Millisecond)

	start := time.Now()
	resp, err := mp.Complete(context.Background(), &llm.CompletionRequest{})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "hello" {
		t.Errorf("expected 'hello', got '%s'", resp.Content)
	}
	if elapsed < 50*time.Millisecond {
		t.Errorf("expected at least 50ms delay, got %v", elapsed)
	}
}

func TestMockProvider_WithDelay_ContextCanceled(t *testing.T) {
	mp := NewMockProvider("hello").WithDelay(5 * time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := mp.Complete(ctx, &llm.CompletionRequest{})
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

func TestMockProvider_Stream(t *testing.T) {
	mp := NewMockProvider("streaming response")

	ch, err := mp.Stream(context.Background(), &llm.CompletionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	chunk, ok := <-ch
	if !ok {
		t.Fatal("expected chunk, got closed channel")
	}
	if chunk.Content != "streaming response" {
		t.Errorf("expected 'streaming response', got '%s'", chunk.Content)
	}
	if !chunk.Done {
		t.Error("expected Done=true")
	}

	// channel should be closed after the single chunk
	_, ok = <-ch
	if ok {
		t.Error("expected channel to be closed")
	}
}

func TestMockProvider_Embeddings(t *testing.T) {
	mp := NewMockProvider()

	texts := []string{"hello", "world", "test"}
	embeddings, err := mp.Embeddings(context.Background(), texts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(embeddings) != 3 {
		t.Errorf("expected 3 embeddings, got %d", len(embeddings))
	}
	for i, emb := range embeddings {
		if len(emb) != 16 {
			t.Errorf("embedding[%d]: expected length 16, got %d", i, len(emb))
		}
	}
}

func TestMockProvider_Info(t *testing.T) {
	mp := NewMockProvider()
	info := mp.Info()

	if info.Name != "mock-model" {
		t.Errorf("expected 'mock-model', got '%s'", info.Name)
	}
	if info.Provider != "mock" {
		t.Errorf("expected 'mock', got '%s'", info.Provider)
	}
	if !info.SupportsTools {
		t.Error("expected SupportsTools=true")
	}
	if !info.SupportsStreaming {
		t.Error("expected SupportsStreaming=true")
	}
}

func TestNewTestAgent_CreatesWorkingAgent(t *testing.T) {
	a := NewTestAgent(TestAgentConfig{
		Name:         "test-bot",
		SystemPrompt: "you are a test bot",
		Responses:    []string{"I am a test agent."},
		MaxTurns:     5,
	})

	if a.Name() != "test-bot" {
		t.Errorf("expected name 'test-bot', got '%s'", a.Name())
	}

	ctx := context.Background()
	resp, err := a.Run(ctx, agent.UserMessage("hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "I am a test agent." {
		t.Errorf("expected 'I am a test agent.', got '%s'", resp.Content)
	}
}

func TestNewTestAgent_Defaults(t *testing.T) {
	a := NewTestAgent(TestAgentConfig{})

	if a.Name() != "test-agent" {
		t.Errorf("expected default name 'test-agent', got '%s'", a.Name())
	}

	ctx := context.Background()
	resp, err := a.Run(ctx, agent.UserMessage("hi"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "This is a test response." {
		t.Errorf("expected default response, got '%s'", resp.Content)
	}
}

func TestNewTestAgent_MultipleResponses(t *testing.T) {
	a := NewTestAgent(TestAgentConfig{
		Name:      "multi-bot",
		MaxTurns:  1,
		Responses: []string{"first"},
	})

	ctx := context.Background()
	resp, err := a.Run(ctx, agent.UserMessage("hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "first" {
		t.Errorf("expected 'first', got '%s'", resp.Content)
	}
}

func TestMockProvider_ImplementsInterfaces(t *testing.T) {
	// 编译期已通过 ensure var 验证；运行时确认非 nil
	mp := NewMockProvider("test")

	var p llm.Provider = mp
	_ = p

	var e llm.Embedder = mp
	_ = e
}

func TestMockProvider_Stream_Error(t *testing.T) {
	mp := NewMockProvider("hello").WithError(errors.New("stream error"))

	ch, err := mp.Stream(context.Background(), &llm.CompletionRequest{})
	if err != nil {
		t.Fatalf("Stream should not return init error: %v", err)
	}

	_, ok := <-ch
	if ok {
		t.Error("expected closed channel on stream error (no chunks)")
	}
}
