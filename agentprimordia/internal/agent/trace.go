package agent

import (
	"github.com/Gleamseekers/AgentPrimordia/internal/agent/trace"
)

// SpanKind Span 类型
// 类型别名保持向后兼容
type SpanKind = trace.SpanKind

// SpanStatus Span 状态
// 类型别名保持向后兼容
type SpanStatus = trace.SpanStatus

// SpanContext Span 上下文，用于跨服务传播
// 类型别名保持向后兼容
type SpanContext = trace.SpanContext

// Span 追踪 Span 接口
// 类型别名保持向后兼容
type Span = trace.Span

// SpanConfig Span 创建配置
// 类型别名保持向后兼容
type SpanConfig = trace.SpanConfig

// SpanOption Span 创建选项
// 类型别名保持向后兼容
type SpanOption = trace.SpanOption

// NoopSpan 空操作 Span，用于未启用追踪时
// 类型别名保持向后兼容
type NoopSpan = trace.NoopSpan

// LoggingSpan 日志 Span 实现
// 类型别名保持向后兼容
type LoggingSpan = trace.LoggingSpan

// LoggingTracer 日志追踪器
// 类型别名保持向后兼容
type LoggingTracer = trace.LoggingTracer

// Span 类型常量
const (
	SpanKindInternal = trace.SpanKindInternal
	SpanKindClient   = trace.SpanKindClient
	SpanKindServer   = trace.SpanKindServer
)

// Span 状态常量
const (
	SpanStatusOK    = trace.SpanStatusOK
	SpanStatusError = trace.SpanStatusError
)

// FromW3CTraceParent 从 W3C traceparent 字符串解析 SpanContext
func FromW3CTraceParent(s string) (SpanContext, error) {
	return trace.FromW3CTraceParent(s)
}

// WithParent 设置父 Span 上下文
func WithParent(parent SpanContext) SpanOption {
	return trace.WithParent(parent)
}

// WithAttributes 设置 Span 属性
func WithAttributes(attrs map[string]any) SpanOption {
	return trace.WithAttributes(attrs)
}

// NewLoggingTracer 创建日志追踪器
func NewLoggingTracer() *LoggingTracer {
	return trace.NewLoggingTracer()
}
