package collaboration

import (
	"context"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/core"
)

// mockCoreAgent 模拟 core.Agent 实现
type mockCoreAgent struct {
	name     string
	response *core.Response
	err      error
}

func (m *mockCoreAgent) Run(_ context.Context, _ core.Message) (*core.Response, error) {
	return m.response, m.err
}

func (m *mockCoreAgent) StreamRun(_ context.Context, _ core.Message) (<-chan core.StreamEvent, error) {
	return nil, nil
}

func (m *mockCoreAgent) Stop() {}

func (m *mockCoreAgent) Stats() core.AgentStats {
	return core.AgentStats{}
}

func (m *mockCoreAgent) Name() string {
	return m.name
}

func TestCoreAgentAdapter_Name(t *testing.T) {
	mock := &mockCoreAgent{name: "test-agent"}
	adapter := NewCoreAgentAdapter(mock)

	if got := adapter.Name(); got != "test-agent" {
		t.Errorf("Name() = %q, want %q", got, "test-agent")
	}
}

func TestCoreAgentAdapter_Run_Success(t *testing.T) {
	mock := &mockCoreAgent{
		name: "test-agent",
		response: &core.Response{
			Content: "hello from core",
		},
	}
	adapter := NewCoreAgentAdapter(mock)

	msg := Message{Role: "user", Content: "hi"}
	got, err := adapter.Run(context.Background(), msg)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got.Role != "assistant" {
		t.Errorf("Role = %q, want %q", got.Role, "assistant")
	}
	if got.Content != "hello from core" {
		t.Errorf("Content = %q, want %q", got.Content, "hello from core")
	}
}

func TestCoreAgentAdapter_Run_Error(t *testing.T) {
	mock := &mockCoreAgent{
		name: "test-agent",
		err:  context.DeadlineExceeded,
	}
	adapter := NewCoreAgentAdapter(mock)

	msg := Message{Role: "user", Content: "hi"}
	_, err := adapter.Run(context.Background(), msg)
	if err == nil {
		t.Fatal("Run() expected error, got nil")
	}
}

func TestCoreAgentAdapter_Unwrap(t *testing.T) {
	mock := &mockCoreAgent{name: "test-agent"}
	adapter := NewCoreAgentAdapter(mock)

	if got := adapter.Unwrap(); got != mock {
		t.Error("Unwrap() did not return the inner agent")
	}
}
