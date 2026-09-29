package agent

import (
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/realtime"
)

// RealtimeCapable 标识 Agent 具备多模态实时交互能力。
// 引擎通过此接口发现 Agent 是否配置了实时会话编排器，
// 从而启用语音/视觉实时双向流交互。
type RealtimeCapable interface {
	GetRealtimeHub() *realtime.RealtimeHub
}

// RealtimeConfig 实时交互能力配置
type RealtimeConfig struct {
	// Hub 实时会话编排器
	Hub *realtime.RealtimeHub
}

// WithRealtime 注入实时交互能力（链式注入模式）
func WithRealtime(cfg RealtimeConfig) Option {
	return func(c *AgentConfig) { c.Realtime = cfg }
}
