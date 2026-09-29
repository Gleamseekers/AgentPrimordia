package orchestration

import (
	"context"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent"
)

// noopAgent 是一个空实现 Agent，仅用于可视化测试
type noopAgent struct {
	name string
}

func newNoopAgent(name string) *noopAgent {
	return &noopAgent{name: name}
}

func (a *noopAgent) Run(_ context.Context, _ agent.Message) (*agent.Response, error) {
	return &agent.Response{Content: "noop"}, nil
}

func (a *noopAgent) StreamRun(_ context.Context, _ agent.Message) (<-chan agent.StreamEvent, error) {
	ch := make(chan agent.StreamEvent)
	close(ch)
	return ch, nil
}

func (a *noopAgent) Stop() {}

func (a *noopAgent) Stats() agent.AgentStats {
	return agent.AgentStats{}
}

func (a *noopAgent) Name() string {
	return a.name
}
