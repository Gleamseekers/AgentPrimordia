package agent

import (
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/autonomy"
)

// AutonomyCapable 标识 Agent 具备长期自治执行能力。
// 引擎通过此接口发现 Agent 是否配置了自治运行时，
// 从而在目标驱动模式下启用自主规划、执行、校验、再计划循环。
type AutonomyCapable interface {
	GetAutonomyRuntime() *autonomy.AutonomyRuntime
}

// AutonomyConfig 自治能力配置
type AutonomyConfig struct {
	// Runtime 自治运行时实例（由外部装配后注入）
	Runtime *autonomy.AutonomyRuntime
}

// WithAutonomy 注入自治能力（链式注入模式）
func WithAutonomy(cfg AutonomyConfig) Option {
	return func(c *AgentConfig) { c.Autonomy = cfg }
}
