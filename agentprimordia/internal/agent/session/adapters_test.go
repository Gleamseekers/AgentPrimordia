package session

import (
	"context"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent/core"
)

// mockCoreAgent 模拟 core.Agent 实现
type mockCoreAgent struct {
	response *core.Response
	err      error
	lastMsg  core.Message
}

func (m *mockCoreAgent) Run(_ context.Context, msg core.Message) (*core.Response, error) {
	m.lastMsg = msg
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
		response: &core.Response{Content: "session reply"},
	}
	adapter := NewCoreAgentAdapter(mock)

	msg := UserMessage("hello")
	msg.Metadata.SessionID = "sess_123"

	resp, err := adapter.Run(context.Background(), msg)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if resp.Content != "session reply" {
		t.Errorf("Content = %q, want %q", resp.Content, "session reply")
	}
	// 验证 SessionID 传递
	if mock.lastMsg.Metadata.SessionID != "sess_123" {
		t.Errorf("SessionID = %q, want %q", mock.lastMsg.Metadata.SessionID, "sess_123")
	}
}

func TestCoreAgentAdapter_Run_Error(t *testing.T) {
	mock := &mockCoreAgent{err: context.DeadlineExceeded}
	adapter := NewCoreAgentAdapter(mock)

	_, err := adapter.Run(context.Background(), UserMessage("test"))
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
