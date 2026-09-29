// workflow.go — workflow 子包的类型别名，保持向后兼容
//
// 本文件从 internal/agent 父包的 workflow 实现（原 workflow.go +
// workflow_engine.go + workflow_evaluator.go + workflow_executor.go +
// workflow_lifecycle.go）提取为独立子包后，通过类型别名和函数委托保持
// 父包 API 不变。
package agent

import (
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/workflow"
)

// ===== 类型别名 =====

// WorkflowType 工作流类型
type WorkflowType = workflow.WorkflowType

// WorkflowStatus 工作流状态
type WorkflowStatus = workflow.WorkflowStatus

// WfRetryPolicy 工作流重试策略
type WfRetryPolicy = workflow.WfRetryPolicy

// WorkflowConfig 工作流配置
type WorkflowConfig = workflow.WorkflowConfig

// ErrorHandling 错误处理策略
type ErrorHandling = workflow.ErrorHandling

// WorkflowNode 工作流节点
type WorkflowNode = workflow.WorkflowNode

// NodeType 节点类型
type NodeType = workflow.NodeType

// NodeConfig 节点配置
type NodeConfig = workflow.NodeConfig

// NodeCondition 节点执行条件
type NodeCondition = workflow.NodeCondition

// Transition 转换/边
type Transition = workflow.Transition

// TransitionCondition 转换条件
type TransitionCondition = workflow.TransitionCondition

// WorkflowExecution 工作流执行实例
type WorkflowExecution = workflow.WorkflowExecution

// ExecutionRecord 执行记录
type ExecutionRecord = workflow.ExecutionRecord

// NodeExecutionStatus 节点执行状态
type NodeExecutionStatus = workflow.NodeExecutionStatus

// WorkflowResult 工作流结果
type WorkflowResult = workflow.WorkflowResult

// WorkflowMetrics 工作流指标
type WorkflowMetrics = workflow.WorkflowMetrics

// WorkflowEvent 工作流事件
type WorkflowEvent = workflow.WorkflowEvent

// ===== 常量别名 =====

const (
	// LinearWorkflow 线性工作流
	LinearWorkflow = workflow.LinearWorkflow
	// ConditionalWorkflow 条件分支工作流
	ConditionalWorkflow = workflow.ConditionalWorkflow
	// LoopWorkflow 循环工作流
	LoopWorkflow = workflow.LoopWorkflow
	// ParallelForkJoin 并行分叉合并工作流
	ParallelForkJoin = workflow.ParallelForkJoin
	// StateMachine 状态机工作流
	StateMachine = workflow.StateMachine
)

const (
	WfStatusPending   = workflow.WfStatusPending
	WfStatusRunning   = workflow.WfStatusRunning
	WfStatusPaused    = workflow.WfStatusPaused
	WfStatusCompleted = workflow.WfStatusCompleted
	WfStatusFailed    = workflow.WfStatusFailed
	WfStatusCancelled = workflow.WfStatusCancelled
)

const (
	TaskNode      = workflow.TaskNode
	ConditionNode = workflow.ConditionNode
	ParallelNode  = workflow.ParallelNode
	LoopStartNode = workflow.LoopStartNode
	LoopEndNode   = workflow.LoopEndNode
	FallbackNode  = workflow.FallbackNode
	SubWfNode     = workflow.SubWfNode
)

const (
	NodePending   = workflow.NodePending
	NodeRunning   = workflow.NodeRunning
	NodeCompleted = workflow.NodeCompleted
	NodeSkipped   = workflow.NodeSkipped
	NodeFailed    = workflow.NodeFailed
)

// ===== 函数委托 =====

// NewWorkflowExecution 创建新的工作流执行实例
func NewWorkflowExecution(config WorkflowConfig) *WorkflowExecution {
	return workflow.NewWorkflowExecution(config)
}
