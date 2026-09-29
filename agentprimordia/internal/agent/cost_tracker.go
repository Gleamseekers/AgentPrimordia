// cost_tracker.go — cost 子包的类型别名，保持向后兼容
package agent

import (
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/cost"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
)

// CostRecord 单次成本记录
// 类型别名保持向后兼容
type CostRecord = cost.CostRecord

// BudgetConfig 预算配置
// 类型别名保持向后兼容
type BudgetConfig = cost.BudgetConfig

// CostSummary 成本汇总
// 类型别名保持向后兼容
type CostSummary = cost.CostSummary

// ModelCost 单模型成本
// 类型别名保持向后兼容
type ModelCost = cost.ModelCost

// CostTracker 成本追踪器
// 类型别名保持向后兼容
type CostTracker = cost.CostTracker

// NewCostTracker 创建成本追踪器
// 委托到 cost 子包，保持向后兼容
func NewCostTracker(pricing map[string]llm.ModelPricing, budget *BudgetConfig) *CostTracker {
	return cost.NewCostTracker(pricing, budget)
}
