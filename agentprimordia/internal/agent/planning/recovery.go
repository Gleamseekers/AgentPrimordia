// Package planning 提供失败恢复策略——死路检测与替代路径生成
package planning

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/Gleamseekers/AgentPrimordia/internal/llm"
)

// DeadlockDetector 死路检测器——连续失败超过阈值即判定死路
type DeadlockDetector struct {
	mu              sync.Mutex
	threshold       int           // 连续失败阈值
	consecutiveFail map[string]int // 子任务 ID → 连续失败次数
}

// NewDeadlockDetector 创建死路检测器
func NewDeadlockDetector(threshold int) *DeadlockDetector {
	if threshold <= 0 {
		threshold = 3
	}
	return &DeadlockDetector{
		threshold:       threshold,
		consecutiveFail: make(map[string]int),
	}
}

// RecordFailure 记录子任务失败，返回是否达到死路阈值
func (d *DeadlockDetector) RecordFailure(subTaskID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.consecutiveFail[subTaskID]++
	return d.consecutiveFail[subTaskID] >= d.threshold
}

// RecordSuccess 记录子任务成功，重置该子任务的连续失败计数
func (d *DeadlockDetector) RecordSuccess(subTaskID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.consecutiveFail, subTaskID)
}

// DetectDeadlock 检测指定子任务是否处于死路状态
func (d *DeadlockDetector) DetectDeadlock(_ context.Context, _ *Plan, failedSubTask string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.consecutiveFail[failedSubTask] >= d.threshold
}

// Reset 重置所有计数器
func (d *DeadlockDetector) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.consecutiveFail = make(map[string]int)
}

// LLMRecoveryStrategy 使用 LLM 生成替代恢复路径
type LLMRecoveryStrategy struct {
	provider  llm.Provider
	detector  *DeadlockDetector
}

// NewLLMRecoveryStrategy 创建 LLM 恢复策略
func NewLLMRecoveryStrategy(provider llm.Provider, detector *DeadlockDetector) *LLMRecoveryStrategy {
	return &LLMRecoveryStrategy{
		provider: provider,
		detector: detector,
	}
}

// DetectDeadlock 委托给内部死路检测器
func (s *LLMRecoveryStrategy) DetectDeadlock(ctx context.Context, plan *Plan, failedSubTask string) bool {
	return s.detector.DetectDeadlock(ctx, plan, failedSubTask)
}

// Recover 通过 LLM 生成替代子任务来绕过失败
func (s *LLMRecoveryStrategy) Recover(ctx context.Context, plan *Plan, failedSubTask string) (*Plan, error) {
	var statusLines []string
	for _, st := range plan.SubTasks {
		statusLines = append(statusLines, fmt.Sprintf("  %s [%s]: %s (结果: %s)", st.ID, st.Status, st.Description, st.Result))
	}
	prompt := fmt.Sprintf(`计划执行遇到死路，需要生成替代方案。
目标: %s
当前计划状态:
%s
失败子任务: %s
请输出替代子任务分解，JSON 数组格式：[{"id":"s1","description":"...","depends_on":[]}]
替代方案应绕过失败的子任务，尝试不同的方法达成相同目标。`,
		plan.Goal, strings.Join(statusLines, "\n"), failedSubTask)

	resp, err := s.provider.Complete(ctx, &llm.CompletionRequest{
		Messages:    []llm.ChatMessage{{Role: "user", Content: prompt}},
		MaxTokens:   500,
		Temperature: llm.Float64Ptr(0),
	})
	if err != nil {
		return nil, fmt.Errorf("recovery LLM call failed: %w", err)
	}

	subTasks, err := parseSubTasksFromResponse(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("recovery parse failed: %w", err)
	}
	return &Plan{Goal: plan.Goal, SubTasks: subTasks}, nil
}

// 确保 LLMRecoveryStrategy 实现 RecoveryStrategy 接口
var _ RecoveryStrategy = (*LLMRecoveryStrategy)(nil)
