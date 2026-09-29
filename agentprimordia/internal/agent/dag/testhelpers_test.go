package dag

import (
	"context"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/core"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/lifecycle"
)

// mockAgentForOrch 用于测试的模拟 Agent
type mockAgentForOrch struct {
	name   string
	output string
}

func (m *mockAgentForOrch) Run(_ context.Context, _ core.Message) (*core.Response, error) {
	return &core.Response{Content: m.output}, nil
}

func (m *mockAgentForOrch) StreamRun(_ context.Context, _ core.Message) (<-chan core.StreamEvent, error) {
	ch := make(chan core.StreamEvent, 1)
	ch <- core.StreamEvent{Type: core.StreamEventComplete, Content: m.output}
	close(ch)
	return ch, nil
}

func (m *mockAgentForOrch) Stop() {}
func (m *mockAgentForOrch) Stats() core.AgentStats {
	return core.AgentStats{Status: lifecycle.StatusIdle}
}
func (m *mockAgentForOrch) Name() string { return m.name }
