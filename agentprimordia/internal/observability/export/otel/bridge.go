package otel

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent"
)

// BridgeEnabled 标识 OTel SDK 桥接是否启用（统一实现，始终启用）
const BridgeEnabled = true

// OTelBridge OTel 兼容桥接，使用内建实现无需外部 SDK
// 生成真实 trace/span ID，支持父子关系，可通过 OTLPExporter 导出
type OTelBridge struct {
	mu     sync.RWMutex
	spans  []*otelSpan
	tracer *agent.LoggingTracer
}

// NewOTelBridge 创建 OTel 兼容桥接
func NewOTelBridge() *OTelBridge {
	return &OTelBridge{
		spans:  make([]*otelSpan, 0),
		tracer: agent.NewLoggingTracer(),
	}
}

// StartSpan 创建带 W3C Trace Context 的真实 Span
func (b *OTelBridge) StartSpan(name string) SpanBridge {
	traceID := generateHexID(16) // 128-bit
	spanID := generateHexID(8)   // 64-bit

	span := &otelSpan{
		name:      name,
		traceID:   traceID,
		spanID:    spanID,
		startTime: time.Now(),
		bridge:    b,
	}

	b.mu.Lock()
	b.spans = append(b.spans, span)
	b.mu.Unlock()

	return span
}

// StartSpanWithParent 创建子 Span
func (b *OTelBridge) StartSpanWithParent(name string, parent SpanBridge) SpanBridge {
	traceID := generateHexID(16)
	parentSpanID := ""
	if p, ok := parent.(*otelSpan); ok {
		traceID = p.traceID
		parentSpanID = p.spanID
	}

	span := &otelSpan{
		name:         name,
		traceID:      traceID,
		spanID:       generateHexID(8),
		parentSpanID: parentSpanID,
		startTime:    time.Now(),
		bridge:       b,
	}

	b.mu.Lock()
	b.spans = append(b.spans, span)
	b.mu.Unlock()

	return span
}

// Shutdown 关闭桥接
func (b *OTelBridge) Shutdown() error {
	b.mu.Lock()
	b.spans = nil
	b.mu.Unlock()
	return nil
}

// FlushSpans 返回所有未导出的 Span（用于 OTLPExporter）
func (b *OTelBridge) FlushSpans() []*otelSpan {
	b.mu.Lock()
	spans := b.spans
	b.spans = make([]*otelSpan, 0)
	b.mu.Unlock()
	return spans
}

// SpanCount 返回当前活跃 Span 数量
func (b *OTelBridge) SpanCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.spans)
}

// ===== SpanBridge 接口 =====

// SpanBridge Span 桥接接口
type SpanBridge interface {
	SetAttribute(key string, value any)
	End()
	RecordError(err error)
	SetStatus(code string, description string)
	AddEvent(name string, attrs map[string]any)
	SpanContext() map[string]string
}

// ===== otelSpan 实现 =====

type otelSpan struct {
	name         string
	traceID      string
	spanID       string
	parentSpanID string
	startTime    time.Time
	duration     time.Duration
	ended        bool

	attrs  map[string]any
	status string
	errors []string
	events []spanEvent

	bridge *OTelBridge
	mu     sync.RWMutex
}

type spanEvent struct {
	name      string
	attrs     map[string]any
	timestamp time.Time
}

func (s *otelSpan) SetAttribute(key string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.attrs == nil {
		s.attrs = make(map[string]any)
	}
	s.attrs[key] = value
}

func (s *otelSpan) End() {
	s.mu.Lock()
	if !s.ended {
		s.duration = time.Since(s.startTime)
		s.ended = true
	}
	s.mu.Unlock()

	// 同时通过 LoggingTracer 记录（用于导出）
	if s.bridge != nil && s.bridge.tracer != nil {
		ls := s.bridge.tracer.Start(s.name, agent.SpanKindInternal)
		ls.SetAttribute("trace_id", s.traceID)
		ls.SetAttribute("span_id", s.spanID)
		if s.parentSpanID != "" {
			ls.SetAttribute("parent_span_id", s.parentSpanID)
		}
		s.mu.RLock()
		for k, v := range s.attrs {
			ls.SetAttribute(k, v)
		}
		if s.status != "" {
			ls.SetStatus(agent.SpanStatus(s.status), "")
		}
		for _, e := range s.errors {
			ls.SetAttribute("error", e)
		}
		s.mu.RUnlock()
		ls.End()
	}
}

func (s *otelSpan) RecordError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errors = append(s.errors, err.Error())
}

func (s *otelSpan) SetStatus(code string, description string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = code
	if description != "" {
		if s.attrs == nil {
			s.attrs = make(map[string]any)
		}
		s.attrs["status_description"] = description
	}
}

func (s *otelSpan) AddEvent(name string, attrs map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, spanEvent{
		name:      name,
		attrs:     attrs,
		timestamp: time.Now(),
	})
}

func (s *otelSpan) SpanContext() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ctx := map[string]string{
		"trace_id": s.traceID,
		"span_id":  s.spanID,
	}
	if s.parentSpanID != "" {
		ctx["parent_span_id"] = s.parentSpanID
	}
	return ctx
}

// ===== 导出辅助方法 =====

// Name 返回 Span 名称
func (s *otelSpan) Name() string { return s.name }

// TraceID 返回 trace ID
func (s *otelSpan) TraceID() string { return s.traceID }

// SpanID 返回 span ID
func (s *otelSpan) SpanID() string { return s.spanID }

// ParentSpanID 返回父 span ID
func (s *otelSpan) ParentSpanID() string { return s.parentSpanID }

// Duration 返回 Span 持续时间
func (s *otelSpan) Duration() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.duration
}

// StartTime 返回 Span 开始时间
func (s *otelSpan) StartTime() time.Time { return s.startTime }

// Attributes 返回所有属性
func (s *otelSpan) Attributes() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]any, len(s.attrs))
	for k, v := range s.attrs {
		result[k] = v
	}
	return result
}

// Errors 返回所有错误
func (s *otelSpan) Errors() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]string, len(s.errors))
	copy(result, s.errors)
	return result
}

// Events 返回所有事件
func (s *otelSpan) Events() []spanEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]spanEvent, len(s.events))
	copy(result, s.events)
	return result
}

// IsEnded 返回 Span 是否已结束
func (s *otelSpan) IsEnded() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ended
}

// W3CTraceParent 生成 W3C Trace Context traceparent 头
func (s *otelSpan) W3CTraceParent() string {
	flags := "00"
	if s.IsEnded() {
		flags = "01"
	}
	return fmt.Sprintf("00-%s-%s-%s", s.traceID, s.spanID, flags)
}

// ===== tool函数 =====

func generateHexID(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
