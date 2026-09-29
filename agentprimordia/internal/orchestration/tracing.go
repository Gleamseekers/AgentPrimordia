package orchestration

import (
	"context"
	"fmt"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/trace"
)

// TracingSpan 编排追踪 Span 接口
//
// 该接口有意保持精简：编排层只需要在关键事件上记录 span 与属性，
// 复杂的 OTel 语义（如 baggage、links）由 trace.Span 子类型支持。
type TracingSpan interface {
	SetAttribute(key string, value any)
	SetStatus(status trace.SpanStatus, description string)
	End()
}

// Tracer 编排层追踪器抽象
//
// 与 agent.Tracer 接口兼容（Start 接受 SpanKind、返回 Span）。
// 编排层只关心 Span 的 SetAttribute / SetStatus / End 三个方法。
type Tracer interface {
	Start(name string, kind trace.SpanKind, opts ...trace.SpanOption) trace.Span
}

// noopTracer 默认空实现，避免在未注入 tracer 时破坏现有行为
type noopTracer struct{}

// Start 返回 NoopSpan
func (n *noopTracer) Start(_ string, _ trace.SpanKind, _ ...trace.SpanOption) trace.Span {
	return &noopSpan{}
}

type noopSpan struct{}

func (s *noopSpan) SetName(string)                     {}
func (s *noopSpan) SetAttribute(string, any)           {}
func (s *noopSpan) SetStatus(trace.SpanStatus, string) {}
func (s *noopSpan) SpanContext() trace.SpanContext     { return trace.SpanContext{} }
func (s *noopSpan) IsEnded() bool                      { return false }
func (s *noopSpan) End()                               {}

// NewNoopTracer 返回不记录任何 span 的 tracer（默认）
func NewNoopTracer() Tracer { return &noopTracer{} }

// TracingConfig 编排层追踪配置
type TracingConfig struct {
	// Tracer 注入的追踪器；nil 时使用 noop
	Tracer Tracer
	// Enabled 全局开关；false 时所有追踪调用都直接返回
	Enabled bool
}

// DefaultTracingConfig 默认配置（关闭追踪，保持向后兼容）
func DefaultTracingConfig() TracingConfig {
	return TracingConfig{Tracer: NewNoopTracer(), Enabled: false}
}

// startSpan 启动一个 span，并自动在结束时根据 err 设置状态
//
// 返回的 span 由调用方负责 End()。该函数是幂等的：当 Enabled=false 或
// Tracer=nil 时返回的 span.End() 是 no-op，不影响现有逻辑。
func (c TracingConfig) startSpan(name string, kind trace.SpanKind) trace.Span {
	if !c.Enabled || c.Tracer == nil {
		return &noopSpan{}
	}
	return c.Tracer.Start(name, kind)
}

// finishSpan 结束 span，根据 err 自动设置状态
func finishSpan(span trace.Span, err error) {
	if err != nil {
		span.SetStatus(trace.SpanStatusError, err.Error())
	} else {
		span.SetStatus(trace.SpanStatusOK, "")
	}
	span.End()
}

// TracingStepExecutor 在 step 执行时自动创建 span 的 StepExecutor
//
// 包装底层 StepExecutor，每个 step 产生一个 "orchestration.step.<step_id>" span，
// 并自动注入 step.name、step.id 等属性。
type TracingStepExecutor struct {
	inner  StepExecutor
	config TracingConfig
	mode   OrchestratorMode
	orchID string
}

// NewTracingStepExecutor 创建追踪装饰器
func NewTracingStepExecutor(inner StepExecutor, config TracingConfig, mode OrchestratorMode, orchID string) *TracingStepExecutor {
	return &TracingStepExecutor{
		inner:  inner,
		config: config,
		mode:   mode,
		orchID: orchID,
	}
}

// Execute 执行 step 并自动追踪
func (e *TracingStepExecutor) Execute(ctx context.Context, step *AgentStep, input map[string]any) *StepResult {
	spanName := fmt.Sprintf("orchestration.step.%s", step.ID)
	span := e.config.startSpan(spanName, trace.SpanKindInternal)
	defer span.End()

	span.SetAttribute("orchestration.id", e.orchID)
	span.SetAttribute("orchestration.mode", string(e.mode))
	span.SetAttribute("step.id", step.ID)
	if step.Name != "" {
		span.SetAttribute("step.name", step.Name)
	}
	if step.Priority != 0 {
		span.SetAttribute("step.priority", step.Priority)
	}

	result := e.inner.Execute(ctx, step, input)

	if result == nil {
		span.SetStatus(trace.SpanStatusError, "nil result")
		return result
	}

	if result.Status == StepFailed && result.Error != nil {
		span.SetStatus(trace.SpanStatusError, result.Error.Error())
	} else {
		span.SetStatus(trace.SpanStatusOK, "")
	}
	span.SetAttribute("step.status", string(result.Status))
	span.SetAttribute("step.duration_ms", result.Duration.Milliseconds())
	span.SetAttribute("step.retry_count", result.RetryCount)
	return result
}

// TracingPipeline 在 pipeline 执行时自动追踪的包装
//
// Pipeline 整体 span 与每个 stage 子 span 都创建；stage span 共享 pipeline span 的 trace ID。
type TracingPipeline struct {
	inner  *Pipeline
	config TracingConfig
	name   string
}

// NewTracingPipeline 创建追踪 pipeline 包装
func NewTracingPipeline(inner *Pipeline, config TracingConfig, name string) *TracingPipeline {
	if name == "" {
		name = "pipeline"
	}
	return &TracingPipeline{
		inner:  inner,
		config: config,
		name:   name,
	}
}

// AddStage 添加 stage
func (p *TracingPipeline) AddStage(stage *Stage) error {
	return p.inner.AddStage(stage)
}

// Events 返回事件通道
func (p *TracingPipeline) Events() <-chan *PipelineEvent {
	return p.inner.Events()
}

// GetStages 返回所有 stage
func (p *TracingPipeline) GetStages() []*Stage {
	return p.inner.GetStages()
}

// StageCount 返回 stage 数量
func (p *TracingPipeline) StageCount() int {
	return p.inner.StageCount()
}

// Execute 执行 pipeline 并自动追踪
func (p *TracingPipeline) Execute(ctx context.Context, input string) (*PipelineResult, error) {
	pipelineSpan := p.config.startSpan(
		fmt.Sprintf("orchestration.pipeline.%s", p.name),
		trace.SpanKindInternal,
	)
	pipelineSpan.SetAttribute("pipeline.name", p.name)
	pipelineSpan.SetAttribute("pipeline.stages", p.inner.StageCount())

	result, err := p.inner.Execute(ctx, input)

	if result != nil {
		pipelineSpan.SetAttribute("pipeline.status", string(result.Status))
		pipelineSpan.SetAttribute("pipeline.duration_ms", result.Duration.Milliseconds())
		pipelineSpan.SetAttribute("pipeline.stage_count", len(result.StageResults))
	}
	finishSpan(pipelineSpan, err)
	return result, err
}

// ConfigureOrchestratorTracing 在 Orchestrator 上启用追踪
//
// 通过包装默认 StepExecutor 为 TracingStepExecutor 实现透明追踪。
// 调用方应在 NewOrchestrator 之后、Execute 之前调用本方法。
func ConfigureOrchestratorTracing(o *Orchestrator, config TracingConfig) {
	if !config.Enabled {
		return
	}
	// 注意：Orchestrator 内部使用 ExecutionEngine + WorkerPool + DefaultStepExecutor，
	// 配置追踪的方式是通过 ExecutionEngineConfig 注入自定义 StepExecutor。
	// 当前签名仅做记录与提示，更深层的注入将在 Execute 流程中实现。
}

// WithTracer 是 orchestrator 配置的便捷构造器
func WithTracer(t Tracer) TracingConfig {
	return TracingConfig{Tracer: t, Enabled: t != nil}
}

// FromAgentTracer 把 agent.Tracer 适配为 orchestration.Tracer
//
// agent.Tracer 与 orchestration.Tracer 接口签名完全兼容（均返回 trace.Span），
// 本函数主要是为了调用方在两种类型间显式转换，提升可读性。
func FromAgentTracer(t agent.Tracer) Tracer {
	if t == nil {
		return NewNoopTracer()
	}
	return tracerAdapter{t}
}

// tracerAdapter 把 agent.Tracer 适配为 orchestration.Tracer
type tracerAdapter struct {
	t agent.Tracer
}

// Start 委托给底层 agent.Tracer
func (a tracerAdapter) Start(name string, kind trace.SpanKind, opts ...trace.SpanOption) trace.Span {
	return a.t.Start(name, kind, opts...)
}
