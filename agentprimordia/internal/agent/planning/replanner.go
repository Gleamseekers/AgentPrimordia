// Package planning 提供动态重规划器——LLM 驱动的plan调整
package planning

import (
	"context"
	"fmt"
	"strings"

	"github.com/Gleamseekers/AgentPrimordia/internal/llm"
)

// LLMReplanner 使用 LLM 判断是否需要重规划并生成新计划
type LLMReplanner struct {
	provider llm.Provider
}

// NewLLMReplanner 创建 LLMReplanner 实例
func NewLLMReplanner(provider llm.Provider) *LLMReplanner {
	return &LLMReplanner{provider: provider}
}

// ShouldReplan 判断当前计划是否需要重新规划
func (r *LLMReplanner) ShouldReplan(ctx context.Context, plan *Plan, observation string) (bool, string) {
	var statusLines []string
	for _, st := range plan.SubTasks {
		statusLines = append(statusLines, fmt.Sprintf("  %s: %s", st.ID, st.Status))
	}
	prompt := fmt.Sprintf(`你是一个任务规划器。当前计划状态：
目标: %s
子任务状态:
%s
最新观察: %s
请判断是否需要重新规划。只回答"是，<原因>"或"否，<原因>"。`, plan.Goal, strings.Join(statusLines, "\n"), observation)

	resp, err := r.provider.Complete(ctx, &llm.CompletionRequest{
		Messages:    []llm.ChatMessage{{Role: "user", Content: prompt}},
		MaxTokens:   100,
		Temperature: llm.Float64Ptr(0),
	})
	if err != nil {
		return false, "replanner error: " + err.Error()
	}
	content := strings.TrimSpace(resp.Content)
	if strings.HasPrefix(content, "是") {
		return true, content
	}
	return false, content
}

// Replan 根据失败原因重新生成计划
func (r *LLMReplanner) Replan(ctx context.Context, plan *Plan, reason string) (*Plan, error) {
	var statusLines []string
	for _, st := range plan.SubTasks {
		statusLines = append(statusLines, fmt.Sprintf("  %s [%s]: %s (结果: %s)", st.ID, st.Status, st.Description, st.Result))
	}
	prompt := fmt.Sprintf(`原计划失败，需要重新规划。
原目标: %s
原计划:
%s
失败原因: %s
请输出新的子任务分解，JSON 数组格式：[{"id":"s1","description":"...","depends_on":[]}]`,
		plan.Goal, strings.Join(statusLines, "\n"), reason)

	resp, err := r.provider.Complete(ctx, &llm.CompletionRequest{
		Messages:    []llm.ChatMessage{{Role: "user", Content: prompt}},
		MaxTokens:   500,
		Temperature: llm.Float64Ptr(0),
	})
	if err != nil {
		return nil, fmt.Errorf("replan LLM call failed: %w", err)
	}

	subTasks, err := parseSubTasksFromResponse(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("replan parse failed: %w", err)
	}
	return &Plan{Goal: plan.Goal, SubTasks: subTasks}, nil
}

// 确保 LLMReplanner 实现 Replanner 接口
var _ Replanner = (*LLMReplanner)(nil)
