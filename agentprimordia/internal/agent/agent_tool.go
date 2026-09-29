package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/tools"
)

// AgentToolConfig AgentTool 配置
type AgentToolConfig struct {
	// Description tool描述，默认自动生成
	Description string
	// ParamSchema 自定义输入参数 JSON Schema
	ParamSchema json.RawMessage
	// MaxSubTurns 子 Agent 最大轮数，默认 10
	MaxSubTurns int
	// PassContext 是否将父 Agent 上下文传递给子 Agent
	PassContext bool
}

// AgentTool 将 Agent 适配为 Tool 接口
// 使一个 Agent 可以在 ReAct Loop 中作为tool调用另一个 Agent
type AgentTool struct {
	agent       Agent
	config      AgentToolConfig
	paramSchema json.RawMessage
}

// defaultParamSchema 默认参数 Schema
var defaultParamSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"input": {
			"type": "string",
			"description": "传递给子 Agent 的输入文本"
		}
	},
	"required": ["input"]
}`)

// NewAgentTool 创建 Agent-as-Tool 适配器
func NewAgentTool(agent Agent, opts ...AgentToolConfig) *AgentTool {
	cfg := AgentToolConfig{
		MaxSubTurns: 10,
	}
	if len(opts) > 0 {
		cfg = opts[0]
	}

	paramSchema := defaultParamSchema
	if len(cfg.ParamSchema) > 0 {
		paramSchema = cfg.ParamSchema
	}

	return &AgentTool{
		agent:       agent,
		config:      cfg,
		paramSchema: paramSchema,
	}
}

// Name 实现 Tool 接口
func (t *AgentTool) Name() string {
	return "agent_" + t.agent.Name()
}

// Description 实现 Tool 接口
func (t *AgentTool) Description() string {
	if t.config.Description != "" {
		return t.config.Description
	}
	return fmt.Sprintf("委托子 Agent [%s] 执行任务", t.agent.Name())
}

// Parameters 实现 Tool 接口
func (t *AgentTool) Parameters() json.RawMessage {
	return t.paramSchema
}

// agentToolArgs tool调用参数
type agentToolArgs struct {
	Input string `json:"input"`
}

// Execute 实现 Tool 接口 — 调用子 Agent 并返回结果
func (t *AgentTool) Execute(ctx context.Context, args json.RawMessage) (*tools.Result, error) {
	var parsed agentToolArgs
	if err := json.Unmarshal(args, &parsed); err != nil {
		return tools.NewErrorResult(fmt.Sprintf("failed to parse arguments: %v", err)), fmt.Errorf("failed to parse arguments: %w", err)
	}

	if parsed.Input == "" {
		return tools.NewErrorResult("missing required parameter 'input'"), fmt.Errorf("missing required parameter 'input'")
	}

	msg := UserMessage(parsed.Input)
	resp, err := t.agent.Run(ctx, msg)
	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("子 Agent [%s] 执行失败: %v", t.agent.Name(), err)), err
	}

	return tools.NewResult(resp.Content), nil
}
