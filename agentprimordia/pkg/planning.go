// Stability: Experimental — v3.0.0 新增前沿能力，API 可能随使用场景演进而调整。
// v7.6：NewEnhancedPlanner 附带规划护栏入口（见下方证据锚点与使用警告）。
package ap

import (
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/planning"
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
	//
	// ⚠ 证据锚点（v7.2 A/B 实测，docs/v7.2-验收实验报告.md §2.1 / §2.2 / §5.1）：
	// 其递归分解策略在 sensenova-6.8-flash-lite 上 **反向显著 -47.8pp**
	// （A 臂 52.2% 12/23 vs B 臂 4.3% 1/23，McNemar p=0.001，11:0）——
	// 规划器为每个任务生成多层 plan（6 层 × 4-9 subtasks），每层 subtask 又触发
	// 新的递归分解，规划本身消耗大量 turns/token，与 ReAct 短程任务
	// （25 turns 上限）根本冲突：**规划开销 > 规划收益**，多数 B 臂任务触发
	// 600s 超时后仍全军覆没。
	//
	// 使用警告（未加护栏时）：
	//   - 规划消耗 turns 必须 ≤ 总 turns 上限的 20%；
	//   - 递归分解深度必须 ≤ 2 层；
	//   - 超限即停止规划、退回直接执行。
	// 需要护栏请改用 NewEnhancedPlannerWithBudget（阈值默认即上述护栏值，
	// 可用 WithPlannerTurnRatio / WithMaxPlanDepth 收紧）。
	// Deprecated: v7.2 A/B 实证反向显著（-47.8pp, p=0.001；A 臂 52.2% 12/23 vs B 臂 4.3% 1/23，McNemar p=0.001，锚点 docs/v7.2-验收实验报告.md §2.1/§2.2/§5.1）。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewEnhancedPlanner = planning.NewEnhancedPlanner
	// NewManagedPlan 创建受管计划（状态机驱动：pending→active→blocked→completed/failed）
	NewManagedPlan = planning.NewManagedPlan
)

// EnhancedPlanner 增强规划器（高风险动作需审批）
// Deprecated: v7.2 A/B 实证反向显著（-47.8pp, p=0.001；A 臂 52.2% 12/23 vs B 臂 4.3% 1/23，McNemar p=0.001，锚点 docs/v7.2-验收实验报告.md §2.1/§2.2/§5.1）。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
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

// ===== v7.6 规划护栏（D6：v7.2 反向显著的修复项）=====

// PlannerGuard 规划护栏（线程安全）：turns 预算 + 递归深度双重守门。
// 超限即熔断——停止规划、退回直接执行。
type PlannerGuard = planning.PlannerGuard

// PlannerBudget 规划护栏阈值配置（默认值即护栏值：20% / 2 层）。
type PlannerBudget = planning.PlannerBudget

// ErrPlanBudgetExceeded 规划 turns 预算耗尽的哨兵错误（errors.Is 可识别）。
// 调用方收到它时应停止规划并退回直接执行。
var ErrPlanBudgetExceeded = planning.ErrPlanBudgetExceeded

// ErrPlanDepthExceeded 递归分解深度超限的哨兵错误（errors.Is 可识别）。
// 调用方收到它时应把该子任务当作原子任务直接执行。
var ErrPlanDepthExceeded = planning.ErrPlanDepthExceeded

var (
	// NewEnhancedPlannerWithBudget 创建带规划护栏的增强规划器。
	// totalTurns 为任务级总 turns 上限（如 AgentConfig.MaxTurns），≤0 时按默认
	// 任务预算 25 turns 计（锚点：v7.2 报告的 ReAct 25 turns 上限）。
	// 阈值默认即护栏值：规划 turns ≤ 总 turns 的 20%、递归分解深度 ≤ 2 层；
	// 可用 WithPlannerTurnRatio / WithMaxPlanDepth 收紧。
	// Deprecated: v7.2 A/B 实证反向显著（-47.8pp, p=0.001；A 臂 52.2% 12/23 vs B 臂 4.3% 1/23，McNemar p=0.001，锚点 docs/v7.2-验收实验报告.md §2.1/§2.2/§5.1）。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewEnhancedPlannerWithBudget = planning.NewEnhancedPlannerWithBudget
	// WithPlannerTurnRatio 设置规划 turns 占比上限（如 0.1 = 10%）。
	WithPlannerTurnRatio = planning.WithPlannerTurnRatio
	// WithMaxPlanDepth 设置递归分解最大层数（如 1 = 只允许一层分解）。
	WithMaxPlanDepth = planning.WithMaxDecomposeDepth
	// DefaultPlannerBudget 返回护栏默认值（20% / 2 层）。
	DefaultPlannerBudget = planning.DefaultPlannerBudget
)
