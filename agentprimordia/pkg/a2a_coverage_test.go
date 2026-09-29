package ap

import (
	"context"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/a2a"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/events"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/memory"
	"log/slog"
)

// ===== A2A AgentCard tests =====

func TestNewA2AAgentCard(t *testing.T) {
	card := NewA2AAgentCard("agent-1", "TestAgent")
	if card == nil {
		t.Fatal("NewA2AAgentCard returned nil")
	}
}

// ===== A2A TaskManager tests =====

func TestNewA2ATaskManager(t *testing.T) {
	tm := NewA2ATaskManager()
	if tm == nil {
		t.Fatal("NewA2ATaskManager returned nil")
	}
}

// ===== A2A Part tests =====

func TestNewA2ATextPart(t *testing.T) {
	p := NewA2ATextPart("hello")
	if p.Text != "hello" {
		t.Errorf("Text = %q, want hello", p.Text)
	}
	if p.Type() != "text" {
		t.Errorf("Type = %q, want text", p.Type())
	}
}

func TestNewA2AFilePartFromURI(t *testing.T) {
	p := NewA2AFilePartFromURI("http://example.com/file.txt", "text/plain")
	if p.FileURI == nil {
		t.Fatal("FileURI is nil")
	}
	if p.FileURI.URI != "http://example.com/file.txt" {
		t.Errorf("URI = %q", p.FileURI.URI)
	}
	if p.Type() != "file" {
		t.Errorf("Type = %q, want file", p.Type())
	}
}

func TestNewA2ADataPart(t *testing.T) {
	p := NewA2ADataPart([]byte(`{"key":"value"}`))
	if p.Type() != "data" {
		t.Errorf("Type = %q, want data", p.Type())
	}
	if len(p.Data) == 0 {
		t.Error("Data should not be empty")
	}
}

// ===== A2A Authenticator tests =====

func TestNewA2ANoopAuthenticator(t *testing.T) {
	auth := NewA2ANoopAuthenticator()
	if auth == nil {
		t.Fatal("NewA2ANoopAuthenticator returned nil")
	}
}

func TestNewA2AAPIKeyAuthenticator(t *testing.T) {
	keys := map[string]string{"key1": "client1"}
	auth := NewA2AAPIKeyAuthenticator(keys, "X-API-Key")
	if auth == nil {
		t.Fatal("NewA2AAPIKeyAuthenticator returned nil")
	}
}

func TestNewA2ABearerTokenAuthenticator(t *testing.T) {
	validate := func(token string) (*a2a.Principal, error) {
		return &a2a.Principal{ID: "user1"}, nil
	}
	auth := NewA2ABearerTokenAuthenticator(validate)
	if auth == nil {
		t.Fatal("NewA2ABearerTokenAuthenticator returned nil")
	}
}

// ===== A2A Discovery tests =====

func TestNewA2ALocalDiscovery(t *testing.T) {
	d := NewA2ALocalDiscovery()
	if d == nil {
		t.Fatal("NewA2ALocalDiscovery returned nil")
	}
}

// ===== A2A MessageBridge tests =====

func TestNewA2AMessageBridge(t *testing.T) {
	mb := NewA2AMessageBridge()
	if mb == nil {
		t.Fatal("NewA2AMessageBridge returned nil")
	}
}

// ===== A2A Service tests =====

func TestNewA2AService(t *testing.T) {
	tm := NewA2ATaskManager()
	card := NewA2AAgentCard("agent-1", "TestAgent")
	service := NewA2AService(card, tm)
	if service == nil {
		t.Fatal("NewA2AService returned nil")
	}
}

// ===== A2A gRPC Server tests =====

func TestNewA2AGRPCServer(t *testing.T) {
	tm := NewA2ATaskManager()
	card := NewA2AAgentCard("agent-1", "TestAgent")
	service := NewA2AService(card, tm)
	server := NewA2AGRPCServer(service)
	if server == nil {
		t.Fatal("NewA2AGRPCServer returned nil")
	}
}

func TestNewA2AGRPCServerWithService(t *testing.T) {
	tm := NewA2ATaskManager()
	card := NewA2AAgentCard("agent-1", "TestAgent")
	service := NewA2AService(card, tm)
	gserver := NewA2AGRPCServerWithService(service)
	if gserver == nil {
		t.Fatal("NewA2AGRPCServerWithService returned nil")
	}
}

// ===== A2A gRPC options tests =====

func TestWithGRPCAuth(t *testing.T) {
	authFn := func(ctx context.Context) (*a2a.Principal, error) {
		return &a2a.Principal{ID: "user1"}, nil
	}
	opt := WithGRPCAuth(authFn)
	if opt == nil {
		t.Fatal("WithGRPCAuth returned nil")
	}
}

func TestWithGRPCLogger(t *testing.T) {
	logger := slog.Default()
	opt := WithGRPCLogger(logger)
	if opt == nil {
		t.Fatal("WithGRPCLogger returned nil")
	}
}

// ===== EventBusAdapter tests =====

func TestNewEventBusAdapter(t *testing.T) {
	bus := events.NewBus(16)
	adapter := NewEventBusAdapter(bus)
	if adapter == nil {
		t.Fatal("NewEventBusAdapter returned nil")
	}

	// Test PublishAsync
	err := adapter.PublishAsync("test.event", "test-source", map[string]string{"key": "value"})
	if err != nil {
		t.Fatalf("PublishAsync failed: %v", err)
	}
}

// ===== SummarizerLLMAdapter tests =====

func TestNewSummarizerLLMAdapter(t *testing.T) {
	mockLLM := &integrationMockLLM{response: "summary result"}
	adapter := NewSummarizerLLMAdapter(mockLLM)
	if adapter == nil {
		t.Fatal("NewSummarizerLLMAdapter returned nil")
	}

	ctx := context.Background()
	messages := []memory.ChatMessageForSummary{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there"},
	}

	result, err := adapter.Complete(ctx, messages, "gpt-4")
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}
	if result != "summary result" {
		t.Errorf("Complete result = %q, want %q", result, "summary result")
	}
}

func TestNewSummarizerLLMAdapter_EmptyModel(t *testing.T) {
	mockLLM := &integrationMockLLM{response: "ok"}
	adapter := NewSummarizerLLMAdapter(mockLLM)

	ctx := context.Background()
	messages := []memory.ChatMessageForSummary{
		{Role: "user", Content: "test"},
	}

	result, err := adapter.Complete(ctx, messages, "")
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}
	if result != "ok" {
		t.Errorf("result = %q, want ok", result)
	}
}

func TestNewSummarizerLLMAdapter_Error(t *testing.T) {
	mockLLM := &errorMockLLM{}
	adapter := NewSummarizerLLMAdapter(mockLLM)

	ctx := context.Background()
	messages := []memory.ChatMessageForSummary{
		{Role: "user", Content: "test"},
	}

	_, err := adapter.Complete(ctx, messages, "gpt-4")
	if err == nil {
		t.Fatal("expected error from failing LLM")
	}
}

// errorMockLLM is a mock LLM that always fails
type errorMockLLM struct{}

func (m *errorMockLLM) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return nil, errLLMFailure
}

func (m *errorMockLLM) Stream(ctx context.Context, req *llm.CompletionRequest) (<-chan llm.Chunk, error) {
	return nil, errLLMFailure
}

func (m *errorMockLLM) CallTools(ctx context.Context, req *llm.ToolCallRequest) (*llm.ToolCallResponse, error) {
	return nil, errLLMFailure
}

func (m *errorMockLLM) Embeddings(ctx context.Context, texts []string) ([][]float32, error) {
	return nil, errLLMFailure
}

func (m *errorMockLLM) Info() llm.ModelInfo {
	return llm.ModelInfo{Name: "error-mock"}
}

var errLLMFailure = &llmFailureError{}

type llmFailureError struct{}

func (e *llmFailureError) Error() string { return "LLM failure" }

// ===== EmbeddingAdapter error path =====

// nonEmbedderMock implements llm.Provider but NOT llm.Embedder
type nonEmbedderMock struct{}

func (m *nonEmbedderMock) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return &llm.CompletionResponse{Content: "ok"}, nil
}

func (m *nonEmbedderMock) Stream(ctx context.Context, req *llm.CompletionRequest) (<-chan llm.Chunk, error) {
	return nil, nil
}

func (m *nonEmbedderMock) CallTools(ctx context.Context, req *llm.ToolCallRequest) (*llm.ToolCallResponse, error) {
	return nil, nil
}

func (m *nonEmbedderMock) Info() llm.ModelInfo {
	return llm.ModelInfo{Name: "non-embedder"}
}

func TestEmbeddingAdapter_Embed_NotSupported(t *testing.T) {
	// nonEmbedderMock does not implement llm.Embedder
	adapter := NewEmbeddingAdapter(&nonEmbedderMock{}, 128)

	_, err := adapter.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error for non-embedder provider")
	}
}
