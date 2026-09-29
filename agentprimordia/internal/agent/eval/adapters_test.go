package eval

import (
	"context"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/core"
)

// mockCoreAgent 模拟 core.Agent 实现
type mockCoreAgent struct {
	response *core.Response
	err      error
}

func (m *mockCoreAgent) Run(_ context.Context, _ core.Message) (*core.Response, error) {
	return m.response, m.err
}

func (m *mockCoreAgent) StreamRun(_ context.Context, _ core.Message) (<-chan core.StreamEvent, error) {
	return nil, nil
}

func (m *mockCoreAgent) Stop()                  {}
func (m *mockCoreAgent) Stats() core.AgentStats { return core.AgentStats{} }
func (m *mockCoreAgent) Name() string           { return "mock" }

func TestCoreAgentAdapter_Run_Success(t *testing.T) {
	mock := &mockCoreAgent{
		response: &core.Response{
			Content: "eval result",
			ToolCalls: []core.ToolCall{
				{ID: "1", Name: "search", Args: `{"q":"test"}`},
			},
		},
	}
	adapter := NewCoreAgentAdapter(mock)

	resp, err := adapter.Run(context.Background(), "what is 2+2?")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if resp.Content != "eval result" {
		t.Errorf("Content = %q, want %q", resp.Content, "eval result")
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "search" {
		t.Errorf("ToolCalls[0].Name = %q, want %q", resp.ToolCalls[0].Name, "search")
	}
}

func TestCoreAgentAdapter_Run_Error(t *testing.T) {
	mock := &mockCoreAgent{err: context.Canceled}
	adapter := NewCoreAgentAdapter(mock)

	_, err := adapter.Run(context.Background(), "test")
	if err == nil {
		t.Fatal("Run() expected error, got nil")
	}
}

func TestCoreAgentAdapter_Unwrap(t *testing.T) {
	mock := &mockCoreAgent{}
	adapter := NewCoreAgentAdapter(mock)
	if adapter.Unwrap() != mock {
		t.Error("Unwrap() did not return inner agent")
	}
}
