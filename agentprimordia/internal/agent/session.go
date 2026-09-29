package agent

import (
	"github.com/Gleamseekers/AgentPrimordia/internal/agent/session"
	"github.com/Gleamseekers/AgentPrimordia/internal/memory"
	"context"
)

// Session 多轮对话会话
type Session = session.Session

// SessionOption 会话选项
type SessionOption = session.SessionOption

// SessWithID 设置会话 ID 选项
var SessWithID = session.SessWithID

// agentAdapter 将 agent.Agent 适配为 session.Agent 接口，
// 在不同包的 Message / Response 类型之间进行转换。
type agentAdapter struct {
	a Agent
}

func (w *agentAdapter) Run(ctx context.Context, msg session.Message) (*session.Response, error) {
	in := Message{
		Role:     Role(msg.Role),
		Content:  msg.Content,
		Metadata: Metadata{SessionID: msg.Metadata.SessionID},
	}
	resp, err := w.a.Run(ctx, in)
	if err != nil {
		return nil, err
	}
	return &session.Response{Content: resp.Content}, nil
}

// NewSession 创建新的会话；内部将 Agent 包装为 session.Agent 适配器。
func NewSession(a Agent, mem memory.Memory, opts ...SessionOption) *Session {
	return session.NewSession(&agentAdapter{a: a}, mem, opts...)
}

// SessionAgent 会话 Agent 接口（向后兼容）。
type SessionAgent interface {
	Run(ctx context.Context, msg Message) (*Response, error)
}
