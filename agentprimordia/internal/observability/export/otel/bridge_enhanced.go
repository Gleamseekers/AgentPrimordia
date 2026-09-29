package otel

import (
	"context"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent"
)

// BridgeConfig 桥接器配置
type BridgeConfig struct {
	ServiceName    string
	ServiceVersion string
}

// EnhancedBridge 增强版 OTel 桥接器
// 集成 Tracing、Metrics 和 Baggage 传播能力
type EnhancedBridge struct {
	tracer  *agent.LoggingTracer
	metrics *MetricExporter
}

// NewEnhancedBridge 创建增强版桥接器
func NewEnhancedBridge(cfg BridgeConfig) *EnhancedBridge {
	return &EnhancedBridge{
		tracer:  agent.NewLoggingTracer(),
		metrics: NewMetricExporter(),
	}
}

// StartSpanWithMetrics 启动带指标的 Span
func (b *EnhancedBridge) StartSpanWithMetrics(ctx context.Context, name string, attrs map[string]string) (context.Context, SpanBridge) {
	// 使用内建 OTelBridge 创建真实 Span
	bridge := NewOTelBridge()
	spanBridge := bridge.StartSpan(name)

	// 设置属性
	for k, v := range attrs {
		spanBridge.SetAttribute(k, v)
	}

	// 如果 context 中有 Baggage，将 Baggage 条目作为 Span 属性注入
	if baggage := Extract(ctx); baggage != nil {
		for _, key := range baggage.Keys() {
			if item, ok := baggage.Get(key); ok {
				spanBridge.SetAttribute("baggage."+key, item.Value)
			}
		}
	}

	return ctx, spanBridge
}

// FinishSpanWithMetrics 结束 Span 并记录指标
func (b *EnhancedBridge) FinishSpanWithMetrics(span SpanBridge, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus("error", err.Error())
	} else {
		span.SetStatus("ok", "")
	}
	span.End()

	// 记录 span 完成指标
	otelSpan, ok := span.(*otelSpan)
	if !ok {
		return
	}

	duration := otelSpan.Duration()
	b.metrics.RecordCounter("ap_spans_total", 1, map[string]string{
		"name":   otelSpan.Name(),
		"status": statusString(err),
	})
	b.metrics.RecordHistogram("ap_span_duration_ms", float64(duration.Milliseconds()),
		map[string]string{"name": otelSpan.Name()},
		[]float64{1, 5, 10, 50, 100, 500, 1000, 5000},
	)
}

// RecordLLMMetrics 记录 LLM 调用指标
func (b *EnhancedBridge) RecordLLMMetrics(provider, model string, duration time.Duration, tokens int, err error) {
	labels := map[string]string{
		"provider": provider,
		"model":    model,
	}

	// 调用计数
	b.metrics.RecordCounter("ap_llm_calls", 1, labels)

	// 耗时直方图
	b.metrics.RecordHistogram("ap_llm_duration_ms", float64(duration.Milliseconds()), labels,
		[]float64{10, 50, 100, 500, 1000, 5000})

	// Token 计数
	if tokens > 0 {
		b.metrics.RecordCounter("ap_llm_tokens", float64(tokens), labels)
	}

	// 错误计数
	if err != nil {
		b.metrics.RecordCounter("ap_llm_errors", 1, labels)
	}
}

// RecordToolMetrics 记录tool调用指标
func (b *EnhancedBridge) RecordToolMetrics(tool string, duration time.Duration, err error) {
	labels := map[string]string{
		"tool": tool,
	}

	// 调用计数
	b.metrics.RecordCounter("ap_tool_calls", 1, labels)

	// 耗时直方图
	b.metrics.RecordHistogram("ap_tool_duration_ms", float64(duration.Milliseconds()), labels,
		[]float64{1, 5, 10, 50, 100, 500, 1000})

	// 错误计数
	if err != nil {
		b.metrics.RecordCounter("ap_tool_errors", 1, labels)
	}
}

// Metrics 返回底层指标导出器（用于直接访问导出功能）
func (b *EnhancedBridge) Metrics() *MetricExporter {
	return b.metrics
}

// statusString 根据是否有错误返回状态字符串
func statusString(err error) string {
	if err != nil {
		return "error"
	}
	return "ok"
}
