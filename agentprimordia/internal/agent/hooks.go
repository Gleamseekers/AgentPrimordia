// hooks.go — hooks 子包的类型别名，保持向后兼容
package agent

import (
	"context"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/hooks"
)

// ===== 类型别名 =====

// HookPoint 钩子触发点
type HookPoint = hooks.HookPoint

// HookContext 钩子执行上下文
type HookContext = hooks.HookContext

// HookFunc 钩子函数类型
type HookFunc = hooks.HookFunc

// Hooks 是 HookManager 指针的类型别名
type Hooks = hooks.Hooks

// HookPhase 钩子执行阶段
type HookPhase = hooks.HookPhase

// Hook 钩子定义
type Hook = hooks.Hook

// HookCondition 钩子条件
type HookCondition = hooks.HookCondition

// HookStats 钩子执行统计
type HookStats = hooks.HookStats

// HookManager 钩子管理器
type HookManager = hooks.HookManager

// HookMiddleware 中间件接口
type HookMiddleware = hooks.HookMiddleware

// HookMiddlewareFunc 函数式中间件
type HookMiddlewareFunc = hooks.HookMiddlewareFunc

// ===== 常量别名 =====

const (
	HookBeforeRun           = hooks.HookBeforeRun
	HookAfterRun            = hooks.HookAfterRun
	HookBeforeTurn          = hooks.HookBeforeTurn
	HookAfterTurn           = hooks.HookAfterTurn
	HookBeforeLLM           = hooks.HookBeforeLLM
	HookAfterLLM            = hooks.HookAfterLLM
	HookBeforeTool          = hooks.HookBeforeTool
	HookAfterTool           = hooks.HookAfterTool
	HookOnError             = hooks.HookOnError
	HookOnComplete          = hooks.HookOnComplete
	HookBeforeRAG           = hooks.HookBeforeRAG
	HookAfterRAG            = hooks.HookAfterRAG
	HookBeforePipelineStep  = hooks.HookBeforePipelineStep
	HookAfterPipelineStep   = hooks.HookAfterPipelineStep
	HookBeforeHandoff       = hooks.HookBeforeHandoff
	HookAfterHandoff        = hooks.HookAfterHandoff
	HookBeforeParallelAgent = hooks.HookBeforeParallelAgent
	HookAfterParallelAgent  = hooks.HookAfterParallelAgent
	HookBeforeDAGNode       = hooks.HookBeforeDAGNode
	HookAfterDAGNode        = hooks.HookAfterDAGNode
)

const (
	HookOnStream            = hooks.HookOnStream
	HookOnStreamStart       = hooks.HookOnStreamStart
	HookOnStreamEnd         = hooks.HookOnStreamEnd
	HookBeforeMemoryRead    = hooks.HookBeforeMemoryRead
	HookAfterMemoryRead     = hooks.HookAfterMemoryRead
	HookBeforeMemoryWrite   = hooks.HookBeforeMemoryWrite
	HookAfterMemoryWrite    = hooks.HookAfterMemoryWrite
	HookContextWindowUpdate = hooks.HookContextWindowUpdate
	HookContextWindowFull   = hooks.HookContextWindowFull
	HookBeforeToolParse     = hooks.HookBeforeToolParse
	HookAfterToolParse      = hooks.HookAfterToolParse
	HookOnMetricsCollect    = hooks.HookOnMetricsCollect
	HookBeforeShutdown      = hooks.HookBeforeShutdown
	HookAfterShutdown       = hooks.HookAfterShutdown
	HookOnStateChange       = hooks.HookOnStateChange
)

const (
	PhaseValidation     = hooks.PhaseValidation
	PhasePreProcessing  = hooks.PhasePreProcessing
	PhaseExecution      = hooks.PhaseExecution
	PhasePostProcessing = hooks.PhasePostProcessing
)

// ===== 函数委托 =====

// AcquireHookContext 从池中获取一个已重置的 HookContext。
func AcquireHookContext() *HookContext {
	return hooks.AcquireHookContext()
}

// ReleaseHookContext 归还 HookContext 到池中。
func ReleaseHookContext(hctx *HookContext) {
	hooks.ReleaseHookContext(hctx)
}

// NewHookManager 创建钩子管理器
func NewHookManager() *HookManager {
	return hooks.NewHookManager()
}

// Always 总是返回 true
func Always(ctx context.Context, hctx *HookContext) bool {
	return hooks.Always(ctx, hctx)
}

// OnTurn 在指定 turn 号时触发
func OnTurn(turnNum int) HookCondition {
	return hooks.OnTurn(turnNum)
}

// OnTurnsGreater 在 turn 大于指定值时触发
func OnTurnsGreater(threshold int) HookCondition {
	return hooks.OnTurnsGreater(threshold)
}

// OnError 当有错误时触发
func OnError() HookCondition {
	return hooks.OnError()
}

// OnMetadataKey 当 Metadata 包含指定 key 时触发
func OnMetadataKey(key string) HookCondition {
	return hooks.OnMetadataKey(key)
}

// OnStateTransition 指定状态转换时触发
func OnStateTransition(from, to string) HookCondition {
	return hooks.OnStateTransition(from, to)
}

// AllHookPoints 返回所有定义的钩子点常量
func AllHookPoints() []HookPoint {
	return hooks.AllHookPoints()
}

// HookPointCategory 钩子点分类
func HookPointCategory(p HookPoint) string {
	return hooks.HookPointCategory(p)
}

// LoggingMiddleware 日志中间件
func LoggingMiddleware() *HookMiddlewareFunc {
	return hooks.LoggingMiddleware()
}

// MetricsCollectionMiddleware 指标收集中间件
func MetricsCollectionMiddleware(stats *HookStats) *HookMiddlewareFunc {
	return hooks.MetricsCollectionMiddleware(stats)
}

// TimeoutMiddleware 超时中间件
func TimeoutMiddleware(timeout time.Duration) *HookMiddlewareFunc {
	return hooks.TimeoutMiddleware(timeout)
}

// ErrorRecoveryMiddleware 错误恢复中间件
func ErrorRecoveryMiddleware(onError func(HookPoint, error)) *HookMiddlewareFunc {
	return hooks.ErrorRecoveryMiddleware(onError)
}
