package session

import (
	"context"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent/core"
)

// CoreAgentAdapter 将 core.Agent 适配为 session.Agent 接口。
// session.Agent 使用简化的 Message/Response 类型，
// 此适配器自动完成 core.Message ↔ session.Message 的类型转换。
//
// 用法:
//
//	reactAgent := agent.NewReActAgent(...)
//	sessAgent := session.NewCoreAgentAdapter(reactAgent)
//	sess := session.NewSession(sessAgent, mem)
type CoreAgentAdapter struct {
	inner core.Agent
}

// 编译期接口满足检查
var _ Agent = (*CoreAgentAdapter)(nil)

// NewCoreAgentAdapter 创建适配器，将 core.Agent 包装为 session.Agent。
func NewCoreAgentAdapter(inner core.Agent) *CoreAgentAdapter {
	return &CoreAgentAdapter{inner: inner}
}

// Run 将 session.Message 转换为 core.Message，调用底层 Agent，
// 再将 core.Response 转换为 session.Response。
func (a *CoreAgentAdapter) Run(ctx context.Context, msg Message) (*Response, error) {
	coreMsg := core.Message{
		Role:    core.Role(msg.Role),
		Content: msg.Content,
	}
	if msg.Metadata.SessionID != "" {
		coreMsg.Metadata.SessionID = msg.Metadata.SessionID
	}

	resp, err := a.inner.Run(ctx, coreMsg)
	if err != nil {
		return nil, err
	}

	return &Response{Content: resp.Content}, nil
}

// Unwrap 返回底层 core.Agent。
func (a *CoreAgentAdapter) Unwrap() core.Agent {
	return a.inner
}
