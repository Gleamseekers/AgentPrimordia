// Package planning 提供任务分解和计划生成能力
package planning

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm"
)

// Planner 定义任务规划和分解接口
type Planner interface {
	// Decompose 将复杂任务分解为子任务列表
	Decompose(ctx context.Context, task string) ([]SubTask, error)
	// GeneratePlan 生成执行计划（包含依赖关系）
	GeneratePlan(ctx context.Context, task string) (*Plan, error)
}

// SubTask 表示一个子任务
type SubTask struct {
	ID          string     `json:"id"`
	Description string     `json:"description"`
	DependsOn   []string   `json:"depends_on"`
	Status      TaskStatus `json:"status"`
	Result      string     `json:"result,omitempty"`
}

// Plan 表示执行计划
type Plan struct {
	Goal      string    `json:"goal"`
	SubTasks  []SubTask `json:"subtasks"`
	CreatedAt time.Time `json:"created_at"`
}

// TaskStatus 任务状态
type TaskStatus string

const (
	TaskPending   TaskStatus = "pending"
	TaskRunning   TaskStatus = "running"
	TaskCompleted TaskStatus = "completed"
	TaskFailed    TaskStatus = "failed"
)

// LLMPlanner 使用 LLM 进行任务规划
type LLMPlanner struct {
	provider llm.Provider
}

// NewLLMPlanner 创建 LLMPlanner 实例
func NewLLMPlanner(provider llm.Provider) *LLMPlanner {
	return &LLMPlanner{
		provider: provider,
	}
}

// Decompose 将任务分解为子任务
func (p *LLMPlanner) Decompose(ctx context.Context, task string) ([]SubTask, error) {
	prompt := fmt.Sprintf(`将以下任务分解为可执行的子任务列表。
任务：%s

请以 JSON 数组格式返回，每个子任务包含：
- id: 任务标识
- description: 任务描述
- depends_on: 依赖的任务 ID 列表（可为空数组）

示例格式：
[
  {"id": "1", "description": "第一步", "depends_on": []},
  {"id": "2", "description": "第二步", "depends_on": ["1"]}
]

请只返回 JSON，不要其他内容。`, task)

	req := &llm.CompletionRequest{
		Messages: []llm.ChatMessage{
			{Role: "user", Content: prompt},
		},
	}

	response, err := p.provider.Complete(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("LLM complete failed: %w", err)
	}

	var subtasks []SubTask
	if err := json.Unmarshal([]byte(response.Content), &subtasks); err != nil {
		return nil, fmt.Errorf("parse subtasks failed: %w", err)
	}

	// 设置默认状态
	for i := range subtasks {
		if subtasks[i].Status == "" {
			subtasks[i].Status = TaskPending
		}
	}

	return subtasks, nil
}

// GeneratePlan 生成执行计划
func (p *LLMPlanner) GeneratePlan(ctx context.Context, task string) (*Plan, error) {
	subtasks, err := p.Decompose(ctx, task)
	if err != nil {
		return nil, err
	}

	return &Plan{
		Goal:      task,
		SubTasks:  subtasks,
		CreatedAt: time.Now(),
	}, nil
}

// parseSubTasksFromResponse 从 LLM 响应中解析子任务 JSON 数组
// 支持纯 JSON 和 markdown 代码块包裹的 JSON
func parseSubTasksFromResponse(content string) ([]SubTask, error) {
	// 尝试直接解析
	var subtasks []SubTask
	if err := json.Unmarshal([]byte(content), &subtasks); err == nil {
		for i := range subtasks {
			if subtasks[i].Status == "" {
				subtasks[i].Status = TaskPending
			}
		}
		return subtasks, nil
	}

	// 尝试从 markdown 代码块中提取 JSON
	start := strings.Index(content, "[")
	end := strings.LastIndex(content, "]")
	if start >= 0 && end > start {
		jsonStr := content[start : end+1]
		if err := json.Unmarshal([]byte(jsonStr), &subtasks); err == nil {
			for i := range subtasks {
				if subtasks[i].Status == "" {
					subtasks[i].Status = TaskPending
				}
			}
			return subtasks, nil
		}
	}

	return nil, fmt.Errorf("无法从 LLM 响应中解析子任务 JSON: %s", content)
}

// ===== v7.1 规划增强接口 =====

// Decomposer 子目标分解器
type Decomposer interface {
	Decompose(ctx context.Context, goal string, context []string) (*Plan, error)
}

// Replanner 动态重规划器
type Replanner interface {
	ShouldReplan(ctx context.Context, plan *Plan, observation string) (bool, string)
	Replan(ctx context.Context, plan *Plan, reason string) (*Plan, error)
}

// RecoveryStrategy 失败恢复策略
type RecoveryStrategy interface {
	DetectDeadlock(ctx context.Context, plan *Plan, failedSubTask string) bool
	Recover(ctx context.Context, plan *Plan, failedSubTask string) (*Plan, error)
}

// ApprovalGate 审批门
type ApprovalGate interface {
	RequiresApproval(ctx context.Context, action string) bool
	RequestApproval(ctx context.Context, action, reason string) error
	WaitApproval(ctx context.Context, action string) error
}

// PlanState 计划状态
type PlanState string

const (
	PlanStatePending   PlanState = "pending"
	PlanStateActive    PlanState = "active"
	PlanStateBlocked   PlanState = "blocked"
	PlanStateCompleted PlanState = "completed"
	PlanStateFailed    PlanState = "failed"
)

// ManagedPlan 带状态机的计划
type ManagedPlan struct {
	Plan      *Plan
	State     PlanState
	History   []PlanTransition
	CreatedAt time.Time
	UpdatedAt time.Time
}

// PlanTransition 状态转换记录
type PlanTransition struct {
	From PlanState `json:"from"`
	To   PlanState `json:"to"`
	At   time.Time `json:"at"`
	Why  string    `json:"why"`
}
