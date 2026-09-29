package orchestration

import "github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent"

// Re-exports of the visualization types and methods now living in internal/agent.
// These aliases keep the orchestration package as the public entry point for
// workflow visualization, consistent with the Phase 3 implementation plan.

type VisualizeConfig = agent.VisualizeConfig

// DefaultVisualizeConfig 返回默认可视化配置
func DefaultVisualizeConfig() VisualizeConfig {
	return agent.DefaultVisualizeConfig()
}
