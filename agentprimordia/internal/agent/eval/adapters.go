package eval

import (
	"context"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent/core"
)

// CoreAgentAdapter 将 core.Agent 适配为 eval.Agent 接口。
// eval.Agent 接受纯字符串输入并返回 eval.Response，
// 此适配器自动完成 core.Message ↔ string 的类型转换。
//
// 用法:
//
//	reactAgent := agent.NewReActAgent(...)
//	evalAgent := eval.NewCoreAgentAdapter(reactAgent)
//	result, _ := runner.RunSuite(ctx, evalAgent, cases)
type CoreAgentAdapter struct {
	inner core.Agent
}

// 编译期接口满足检查
var _ Agent = (*CoreAgentAdapter)(nil)

// NewCoreAgentAdapter 创建适配器，将 core.Agent 包装为 eval.Agent。
func NewCoreAgentAdapter(inner core.Agent) *CoreAgentAdapter {
	return &CoreAgentAdapter{inner: inner}
}

// Run 将字符串输入转换为 core.Message，调用底层 Agent，
// 再将 core.Response 转换为 eval.Response。
func (a *CoreAgentAdapter) Run(ctx context.Context, input string) (*Response, error) {
	coreMsg := core.Message{
		Role:    core.RoleUser,
		Content: input,
	}

	resp, err := a.inner.Run(ctx, coreMsg)
	if err != nil {
		return nil, err
	}

	out := &Response{
		Content: resp.Content,
	}
	// 转换tool调用
	for _, tc := range resp.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, ToolCall{
			Name: tc.Name,
			Args: tc.Args,
		})
	}
	return out, nil
}

// Unwrap 返回底层 core.Agent。
func (a *CoreAgentAdapter) Unwrap() core.Agent {
	return a.inner
}
