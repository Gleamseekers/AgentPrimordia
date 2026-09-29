// Stability: Experimental — v3.3.0 新增长期自治能力，API 可能随使用场景演进而调整。
package ap

import (
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/autonomy"
)

// --- 核心类型导出 ---

// AgentGoal 持久化目标：自治执行的核心载体
type AgentGoal = autonomy.AgentGoal

// GoalConfig 目标创建配置
type GoalConfig = autonomy.GoalConfig

// GoalState 目标状态
type GoalState = autonomy.GoalState

// GoalPlan 目标执行计划
type GoalPlan = autonomy.GoalPlan

// PlanStep 计划中的单个执行步骤
type PlanStep = autonomy.PlanStep

// AutonomyRuntime 自治运行时
type AutonomyRuntime = autonomy.AutonomyRuntime

// RuntimeConfig 自治运行时配置
type RuntimeConfig = autonomy.RuntimeConfig

// StepExecutor 步骤执行器接口
type StepExecutor = autonomy.StepExecutor

// Checkpoint 自治目标检查点快照
type Checkpoint = autonomy.Checkpoint

// MonitorConfig 监控器配置
type MonitorConfig = autonomy.MonitorConfig

// Monitor 自治监控器：停滞检测 + 进度追踪 + 异常上报
type Monitor = autonomy.Monitor

// Alert 异常告警
type Alert = autonomy.Alert

// AlertLevel 异常分级
type AlertLevel = autonomy.AlertLevel

// --- 状态常量导出 ---

const (
	// GoalCreated 目标已创建
	GoalCreated = autonomy.GoalCreated
	// GoalPlanned 目标已规划
	GoalPlanned = autonomy.GoalPlanned
	// GoalExecuting 正在执行
	GoalExecuting = autonomy.GoalExecuting
	// GoalValidated 已校验
	GoalValidated = autonomy.GoalValidated
	// GoalDone 已完成
	GoalDone = autonomy.GoalDone
	// GoalFailed 已失败
	GoalFailed = autonomy.GoalFailed
)

// --- 优先级常量导出 ---

const (
	// PriorityLow 低优先级
	PriorityLow = autonomy.PriorityLow
	// PriorityNormal 普通优先级
	PriorityNormal = autonomy.PriorityNormal
	// PriorityHigh 高优先级
	PriorityHigh = autonomy.PriorityHigh
	// PriorityCritical 紧急优先级
	PriorityCritical = autonomy.PriorityCritical
)

// --- 构造器导出 ---

var (
	// NewAgentGoal 创建新的自治目标
	NewAgentGoal = autonomy.NewAgentGoal
	// NewGoalPlan 创建执行计划
	NewGoalPlan = autonomy.NewGoalPlan
	// NewAutonomyRuntime 创建自治运行时
	NewAutonomyRuntime = autonomy.NewAutonomyRuntime
	// NewGoalExecutor 创建目标执行器
	NewGoalExecutor = autonomy.NewGoalExecutor
)

// --- 集成接口导出 ---

// RAGRetriever RAG 知识检索接口
type RAGRetriever = autonomy.RAGRetriever

// PoolDispatcher 多目标并发调度接口
type PoolDispatcher = autonomy.PoolDispatcher

// ClusterSync 分布式状态同步接口
type ClusterSync = autonomy.ClusterSync

// GoalMetrics 目标级指标记录接口
type GoalMetrics = autonomy.GoalMetrics

// StepGuardrail 步骤级护栏检查接口
type StepGuardrail = autonomy.StepGuardrail

// MemoryStore 记忆存储接口
type AutonomyMemoryStore = autonomy.MemoryStore

// CheckpointStore 检查点存储接口
type AutonomyCheckpointStore = autonomy.CheckpointStore
