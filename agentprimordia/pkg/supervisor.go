// Stability: Experimental — v3.0.0 新增前沿能力，API 可能随使用场景演进而调整。
package ap

import (
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/orchestration"
)

// ===== Orchestration Tracing 编排追踪 =====

// OrchestrationTracer 编排层追踪器接口
// Deprecated: 仓内 0 消费者的预留 API（编排追踪管线），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type OrchestrationTracer = orchestration.Tracer

// OrchestrationTracingConfig 编排追踪配置
// Deprecated: 仓内 0 消费者的预留 API（编排追踪管线），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type OrchestrationTracingConfig = orchestration.TracingConfig

// OrchestrationTracingStepExecutor 自动追踪的 StepExecutor 装饰器
// Deprecated: 仓内 0 消费者的预留 API（编排追踪管线），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type OrchestrationTracingStepExecutor = orchestration.TracingStepExecutor

// OrchestrationTracingPipeline 自动追踪的 Pipeline 装饰器
// Deprecated: 仓内 0 消费者的预留 API（编排追踪管线），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type OrchestrationTracingPipeline = orchestration.TracingPipeline

var (
	// NewOrchestrationNoopTracer 创建不记录任何 span 的 tracer（默认）
	// Deprecated: 仓内 0 消费者的预留 API（编排追踪管线），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewOrchestrationNoopTracer = orchestration.NewNoopTracer
	// NewOrchestrationTracingStepExecutor 创建追踪 StepExecutor 装饰器
	// Deprecated: 仓内 0 消费者的预留 API（编排追踪管线），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewOrchestrationTracingStepExecutor = orchestration.NewTracingStepExecutor
	// NewOrchestrationTracingPipeline 创建追踪 Pipeline 装饰器
	// Deprecated: 仓内 0 消费者的预留 API（编排追踪管线），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewOrchestrationTracingPipeline = orchestration.NewTracingPipeline
	// OrchestrationWithTracer 构造启用追踪的 TracingConfig
	// Deprecated: 仓内 0 消费者的预留 API（编排追踪管线），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	OrchestrationWithTracer = orchestration.WithTracer
	// OrchestrationFromAgentTracer 把 agent.Tracer 适配为 orchestration.Tracer
	// Deprecated: 仓内 0 消费者的预留 API（编排追踪管线），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	OrchestrationFromAgentTracer = orchestration.FromAgentTracer
	// OrchestrationDefaultTracingConfig 返回关闭追踪的默认配置
	// Deprecated: 仓内 0 消费者的预留 API（编排追踪管线），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	OrchestrationDefaultTracingConfig = orchestration.DefaultTracingConfig
	// OrchestrationConfigureTracing 在 Orchestrator 上启用追踪（接口预留）
	// Deprecated: 仓内 0 消费者的预留 API（编排追踪管线），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	OrchestrationConfigureTracing = orchestration.ConfigureOrchestratorTracing
)
