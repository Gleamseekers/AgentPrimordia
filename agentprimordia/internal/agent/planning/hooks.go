// hooks.go — 增强规划器统一入口：组合 Base+Replanner+Recovery+Deadlock+Approval
package planning

import (
	"context"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm"
)

// EnhancedPlanner 组合所有规划增强能力的统一规划器
type EnhancedPlanner struct {
	Base      *LLMPlanner
	Replanner *LLMReplanner
	Recovery  *LLMRecoveryStrategy
	Deadlock  *DeadlockDetector
	Approval  *PolicyApprovalGate
}

// NewEnhancedPlanner 创建增强规划器
// provider 用于驱动 LLM 调用（Base/Replanner/Recovery 共用）
// approvalActions 为需要审批的高风险动作列表
func NewEnhancedPlanner(provider llm.Provider, approvalActions []string) *EnhancedPlanner {
	detector := NewDeadlockDetector(3)
	return &EnhancedPlanner{
		Base:      NewLLMPlanner(provider),
		Replanner: NewLLMReplanner(provider),
		Recovery:  NewLLMRecoveryStrategy(provider, detector),
		Deadlock:  detector,
		Approval:  NewPolicyApprovalGate(approvalActions),
	}
}

// GeneratePlan 实现 Planner 接口（委托给 Base）
func (ep *EnhancedPlanner) GeneratePlan(ctx context.Context, task string) (*Plan, error) {
	return ep.Base.GeneratePlan(ctx, task)
}

// Decompose 实现 Planner 接口（委托给 Base）
func (ep *EnhancedPlanner) Decompose(ctx context.Context, task string) ([]SubTask, error) {
	return ep.Base.Decompose(ctx, task)
}

// 编译期断言：EnhancedPlanner 实现 Planner 接口
var _ Planner = (*EnhancedPlanner)(nil)
