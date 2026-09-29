// Stability: Stable — 生命周期钩子（20+ HookPoint）。
package ap

import (
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent"
)

// HookPoint 标识钩子的挂载点，对应 Agent 生命周期的各个阶段
type HookPoint = agent.HookPoint

// HookContext 是钩子执行时的上下文，包含 Agent ID、会话 ID、轮次、消息、响应等信息
type HookContext = agent.HookContext

// HookFunc 是钩子处理函数类型，接收上下文并返回错误
type HookFunc = agent.HookFunc

// HookManager 管理所有钩子的注册和触发，支持优先级排序
type HookManager = agent.HookManager

// Hook 是一个注册的钩子，包含挂载点、处理函数和优先级
type Hook = agent.Hook

// Hooks 是 HookManager 的指针别名，用于在配置中传递钩子管理器
type Hooks = agent.Hooks

const (
	// HookBeforeRun 在 Agent 运行前触发
	HookBeforeRun = agent.HookBeforeRun
	// HookAfterRun 在 Agent 运行后触发
	HookAfterRun = agent.HookAfterRun
	// HookBeforeTurn 在每轮推理前触发
	HookBeforeTurn = agent.HookBeforeTurn
	// HookAfterTurn 在每轮推理后触发
	HookAfterTurn = agent.HookAfterTurn
	// HookBeforeLLM 在调用 LLM 前触发
	HookBeforeLLM = agent.HookBeforeLLM
	// HookAfterLLM 在 LLM 响应后触发
	HookAfterLLM = agent.HookAfterLLM
	// HookBeforeTool 在tool执行前触发
	HookBeforeTool = agent.HookBeforeTool
	// HookAfterTool 在tool执行后触发
	HookAfterTool = agent.HookAfterTool
	// HookOnError 在发生错误时触发
	HookOnError = agent.HookOnError
	// HookOnComplete 在 Agent 完成时触发
	HookOnComplete = agent.HookOnComplete
	// HookBeforeRAG 在 RAG 检索前触发
	HookBeforeRAG = agent.HookBeforeRAG
	// HookAfterRAG 在 RAG 检索后触发
	HookAfterRAG = agent.HookAfterRAG
	// HookBeforePipelineStep 在 Pipeline 步骤执行前触发
	HookBeforePipelineStep = agent.HookBeforePipelineStep
	// HookAfterPipelineStep 在 Pipeline 步骤执行后触发
	HookAfterPipelineStep = agent.HookAfterPipelineStep
	// HookBeforeHandoff 在 Agent 交接前触发
	HookBeforeHandoff = agent.HookBeforeHandoff
	// HookAfterHandoff 在 Agent 交接后触发
	HookAfterHandoff = agent.HookAfterHandoff
	// HookBeforeParallelAgent 在并行 Agent 执行前触发
	HookBeforeParallelAgent = agent.HookBeforeParallelAgent
	// HookAfterParallelAgent 在并行 Agent 执行后触发
	HookAfterParallelAgent = agent.HookAfterParallelAgent
)

type HookPhase = agent.HookPhase

const (
	PhaseValidation     = agent.PhaseValidation
	PhasePreProcessing  = agent.PhasePreProcessing
	PhaseExecution      = agent.PhaseExecution
	PhasePostProcessing = agent.PhasePostProcessing
)

// NewHookManager 创建钩子管理器实例
var NewHookManager = agent.NewHookManager
