// Package planning 提供审批门——策略驱动的高风险动作审批
package planning

import (
	"context"
	"fmt"
	"sync"
)

// PolicyApprovalGate 基于策略的审批门
// 维护需要审批的高风险动作列表，通过 channel 实现异步审批等待
type PolicyApprovalGate struct {
	mu              sync.Mutex
	highRiskActions map[string]bool          // 需要审批的动作集合
	pending         map[string]chan struct{} // 等待审批的动作 → 通知 channel
}

// NewPolicyApprovalGate 创建策略审批门
// highRiskActions 为需要审批的高风险动作列表
func NewPolicyApprovalGate(highRiskActions []string) *PolicyApprovalGate {
	actions := make(map[string]bool, len(highRiskActions))
	for _, a := range highRiskActions {
		actions[a] = true
	}
	return &PolicyApprovalGate{
		highRiskActions: actions,
		pending:         make(map[string]chan struct{}),
	}
}

// RequiresApproval 判断指定动作是否需要审批
func (g *PolicyApprovalGate) RequiresApproval(_ context.Context, action string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.highRiskActions[action]
}

// RequestApproval 提交审批请求，创建等待 channel
// 如果动作不在高风险列表中，直接返回 nil（无需审批）
// 如果已有待审批请求，返回错误防止重复提交
func (g *PolicyApprovalGate) RequestApproval(_ context.Context, action, reason string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.highRiskActions[action] {
		return nil // 不需要审批的动作直接放行
	}

	if _, exists := g.pending[action]; exists {
		return fmt.Errorf("action %q already has a pending approval request", action)
	}

	// 创建审批等待 channel，记录审批原因
	ch := make(chan struct{})
	g.pending[action] = ch

	// 审批原因通过 channel 的上下文传递（此处简化，仅创建通道）
	// 实际使用中 reason 可通过日志或事件总线记录
	_ = reason

	return nil
}

// WaitApproval 阻塞等待指定动作的审批通过
// 如果动作无需审批或没有待审批请求，立即返回
// context 取消时返回 context 错误
func (g *PolicyApprovalGate) WaitApproval(ctx context.Context, action string) error {
	g.mu.Lock()
	ch, exists := g.pending[action]
	g.mu.Unlock()

	if !exists {
		return nil // 无待审批请求，直接放行
	}

	select {
	case <-ch:
		// 审批通过，清理 pending 记录
		g.mu.Lock()
		delete(g.pending, action)
		g.mu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Approve 批准指定动作（供审批方调用）
// 如果该动作有待审批请求，通知等待方
func (g *PolicyApprovalGate) Approve(action string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	ch, exists := g.pending[action]
	if !exists {
		return false
	}

	close(ch)
	delete(g.pending, action)
	return true
}

// PendingActions 返回当前待审批的动作列表
func (g *PolicyApprovalGate) PendingActions() []string {
	g.mu.Lock()
	defer g.mu.Unlock()

	actions := make([]string, 0, len(g.pending))
	for a := range g.pending {
		actions = append(actions, a)
	}
	return actions
}

// 确保 PolicyApprovalGate 实现 ApprovalGate 接口
var _ ApprovalGate = (*PolicyApprovalGate)(nil)
