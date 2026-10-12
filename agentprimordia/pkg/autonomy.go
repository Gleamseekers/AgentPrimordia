// Stability: Experimental — v3.3.0 新增长期自治能力，API 可能随使用场景演进而调整。
package ap

import (
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/autonomy"
)

// --- 核心类型导出 ---

// AgentGoal 持久化目标：自治执行的核心载体
// Deprecated: 无外部采用证据的周边能力（自主目标执行），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type AgentGoal = autonomy.AgentGoal

// GoalConfig 目标创建配置
// Deprecated: 无外部采用证据的周边能力（自主目标执行），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type GoalConfig = autonomy.GoalConfig

// GoalState 目标状态
// Deprecated: 无外部采用证据的周边能力（自主目标执行），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type GoalState = autonomy.GoalState

// GoalPlan 目标执行计划
// Deprecated: 无外部采用证据的周边能力（自主目标执行），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type GoalPlan = autonomy.GoalPlan

// PlanStep 计划中的单个执行步骤
// Deprecated: 无外部采用证据的周边能力（自主目标执行），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type PlanStep = autonomy.PlanStep

// AutonomyRuntime 自治运行时
// Deprecated: 无外部采用证据的周边能力（自主目标执行），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type AutonomyRuntime = autonomy.AutonomyRuntime

// RuntimeConfig 自治运行时配置
// Deprecated: 无外部采用证据的周边能力（自主目标执行），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type RuntimeConfig = autonomy.RuntimeConfig

// StepExecutor 步骤执行器接口
// Deprecated: 无外部采用证据的周边能力（自主目标执行），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type StepExecutor = autonomy.StepExecutor

// Checkpoint 自治目标检查点快照
// Deprecated: 无外部采用证据的周边能力（自主目标执行），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type Checkpoint = autonomy.Checkpoint

// MonitorConfig 监控器配置
// Deprecated: 无外部采用证据的周边能力（自主目标执行），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type MonitorConfig = autonomy.MonitorConfig

// Monitor 自治监控器：停滞检测 + 进度追踪 + 异常上报
// Deprecated: 无外部采用证据的周边能力（自主目标执行），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type Monitor = autonomy.Monitor

// Alert 异常告警
// Deprecated: 无外部采用证据的周边能力（自主目标执行），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type Alert = autonomy.Alert

// AlertLevel 异常分级
// Deprecated: 无外部采用证据的周边能力（自主目标执行），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type AlertLevel = autonomy.AlertLevel

// --- 状态常量导出 ---

const (
	// GoalCreated 目标已创建
	// Deprecated: 无外部采用证据的周边能力（自主目标执行），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	GoalCreated = autonomy.GoalCreated
	// GoalPlanned 目标已规划
	// Deprecated: 无外部采用证据的周边能力（自主目标执行），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	GoalPlanned = autonomy.GoalPlanned
	// GoalExecuting 正在执行
	// Deprecated: 无外部采用证据的周边能力（自主目标执行），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	GoalExecuting = autonomy.GoalExecuting
	// GoalValidated 已校验
	// Deprecated: 无外部采用证据的周边能力（自主目标执行），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	GoalValidated = autonomy.GoalValidated
	// GoalDone 已完成
	// Deprecated: 无外部采用证据的周边能力（自主目标执行），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	GoalDone = autonomy.GoalDone
	// GoalFailed 已失败
	// Deprecated: 无外部采用证据的周边能力（自主目标执行），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	GoalFailed = autonomy.GoalFailed
)

// --- 优先级常量导出 ---

const (
	// PriorityLow 低优先级
	// Deprecated: 无外部采用证据的周边能力（自主目标执行），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	PriorityLow = autonomy.PriorityLow
	// PriorityNormal 普通优先级
	// Deprecated: 无外部采用证据的周边能力（自主目标执行），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	PriorityNormal = autonomy.PriorityNormal
	// PriorityHigh 高优先级
	// Deprecated: 无外部采用证据的周边能力（自主目标执行），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	PriorityHigh = autonomy.PriorityHigh
	// PriorityCritical 紧急优先级
	// Deprecated: 无外部采用证据的周边能力（自主目标执行），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	PriorityCritical = autonomy.PriorityCritical
)

// --- 构造器导出 ---

var (
	// NewAgentGoal 创建新的自治目标
	// Deprecated: 无外部采用证据的周边能力（自主目标执行），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewAgentGoal = autonomy.NewAgentGoal
	// NewGoalPlan 创建执行计划
	// Deprecated: 无外部采用证据的周边能力（自主目标执行），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewGoalPlan = autonomy.NewGoalPlan
	// NewAutonomyRuntime 创建自治运行时
	// Deprecated: 无外部采用证据的周边能力（自主目标执行），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewAutonomyRuntime = autonomy.NewAutonomyRuntime
	// NewGoalExecutor 创建目标执行器
	// Deprecated: 无外部采用证据的周边能力（自主目标执行），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewGoalExecutor = autonomy.NewGoalExecutor
)

// --- 集成接口导出 ---

// RAGRetriever RAG 知识检索接口
// Deprecated: 无外部采用证据的周边能力（自主目标执行），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type RAGRetriever = autonomy.RAGRetriever

// PoolDispatcher 多目标并发调度接口
// Deprecated: 无外部采用证据的周边能力（自主目标执行），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type PoolDispatcher = autonomy.PoolDispatcher

// ClusterSync 分布式状态同步接口
// Deprecated: 无外部采用证据的周边能力（自主目标执行），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type ClusterSync = autonomy.ClusterSync

// GoalMetrics 目标级指标记录接口
// Deprecated: 无外部采用证据的周边能力（自主目标执行），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type GoalMetrics = autonomy.GoalMetrics

// StepGuardrail 步骤级护栏检查接口
// Deprecated: 无外部采用证据的周边能力（自主目标执行），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type StepGuardrail = autonomy.StepGuardrail

// MemoryStore 记忆存储接口
// Deprecated: 无外部采用证据的周边能力（自主目标执行），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type AutonomyMemoryStore = autonomy.MemoryStore

// CheckpointStore 检查点存储接口
// Deprecated: 无外部采用证据的周边能力（自主目标执行），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type AutonomyCheckpointStore = autonomy.CheckpointStore
