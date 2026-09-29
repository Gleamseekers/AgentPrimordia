// visualize.go — workflow 子包的可视化类型别名，保持向后兼容
//
// 工作流可视化方法（ToMermaid, ToDot 等）已随 WorkflowExecution 类型
// 迁移到 workflow 子包，父包通过类型别名自动继承这些方法。
// 本文件仅保留 VisualizeConfig 类型别名和 DefaultVisualizeConfig 函数委托。
package agent

import (
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/workflow"
)

// VisualizeConfig 可视化配置
type VisualizeConfig = workflow.VisualizeConfig

// DefaultVisualizeConfig 返回默认可视化配置
func DefaultVisualizeConfig() VisualizeConfig {
	return workflow.DefaultVisualizeConfig()
}
