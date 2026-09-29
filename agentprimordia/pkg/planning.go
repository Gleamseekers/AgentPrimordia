// Stability: Experimental — v3.0.0 新增前沿能力，API 可能随使用场景演进而调整。
package ap

import (
	"github.com/Gleamseekers/AgentPrimordia/internal/agent/planning"
)

// Planner 定义任务规划和分解接口
type Planner = planning.Planner

// SubTask 表示一个子任务
type SubTask = planning.SubTask

// Plan 表示执行计划
type Plan = planning.Plan

// TaskStatus 任务状态
type TaskStatus = planning.TaskStatus

// LLMPlanner 使用 LLM 进行任务规划
type LLMPlanner = planning.LLMPlanner

const (
	// TaskPending 任务等待中
	TaskPending = planning.TaskPending
	// TaskRunning 任务运行中
	TaskRunning = planning.TaskRunning
	// TaskCompleted 任务已完成
	TaskCompleted = planning.TaskCompleted
	// TaskFailed 任务失败
	TaskFailed = planning.TaskFailed
)

var (
	// NewLLMPlanner 创建 LLMPlanner 实例
	NewLLMPlanner = planning.NewLLMPlanner
	// NewEnhancedPlanner 创建增强规划器（高风险动作审批门 + 计划状态机）
	NewEnhancedPlanner = planning.NewEnhancedPlanner
	// NewManagedPlan 创建受管计划（状态机驱动：pending→active→blocked→completed/failed）
	NewManagedPlan = planning.NewManagedPlan
)

// EnhancedPlanner 增强规划器（高风险动作需审批）
type EnhancedPlanner = planning.EnhancedPlanner

// ManagedPlan 受管计划（状态机 + 转换审计）
type ManagedPlan = planning.ManagedPlan

// PlanState 计划状态
type PlanState = planning.PlanState

const (
	// PlanStatePending 计划待启动
	PlanStatePending = planning.PlanStatePending
	// PlanStateActive 计划执行中
	PlanStateActive = planning.PlanStateActive
	// PlanStateBlocked 计划阻塞（可恢复）
	PlanStateBlocked = planning.PlanStateBlocked
	// PlanStateCompleted 计划完成
	PlanStateCompleted = planning.PlanStateCompleted
	// PlanStateFailed 计划失败
	PlanStateFailed = planning.PlanStateFailed
)
