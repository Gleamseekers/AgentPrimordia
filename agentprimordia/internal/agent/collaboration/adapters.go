package collaboration

import (
	"context"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent/core"
)

// CoreAgentAdapter 将 core.Agent 适配为 collaboration.Agent 接口。
// 这使得任何标准 Agent 实现（如 ReActAgent）可以直接参与协作模式，
// 无需用户手动编写类型转换代码。
//
// 用法:
//
//	reactAgent := agent.NewReActAgent(...)
//	collabAgent := collaboration.NewCoreAgentAdapter(reactAgent)
//	gc := collaboration.NewGroupChat(collabAgent, ...)
type CoreAgentAdapter struct {
	inner core.Agent
}

// 编译期接口满足检查
var _ Agent = (*CoreAgentAdapter)(nil)

// NewCoreAgentAdapter 创建适配器，将 core.Agent 包装为 collaboration.Agent。
func NewCoreAgentAdapter(inner core.Agent) *CoreAgentAdapter {
	return &CoreAgentAdapter{inner: inner}
}

// Name 返回底层 Agent 的名称。
func (a *CoreAgentAdapter) Name() string {
	return a.inner.Name()
}

// Run 将 collaboration.Message 转换为 core.Message，调用底层 Agent，
// 再将 core.Response 转换回 collaboration.Message。
func (a *CoreAgentAdapter) Run(ctx context.Context, msg Message) (Message, error) {
	coreMsg := core.Message{
		Role:    core.Role(msg.Role),
		Content: msg.Content,
	}

	resp, err := a.inner.Run(ctx, coreMsg)
	if err != nil {
		return Message{}, err
	}

	out := Message{
		Role:    "assistant",
		Content: resp.Content,
	}
	return out, nil
}

// Unwrap 返回底层 core.Agent，供需要访问完整 Agent 能力的场景使用。
func (a *CoreAgentAdapter) Unwrap() core.Agent {
	return a.inner
}
