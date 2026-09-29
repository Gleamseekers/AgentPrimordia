package debugger

import (
	"github.com/Gleamseekers/AgentPrimordia/internal/agent"
	"context"
	"sync"
	"time"
)

// Inspector 提供类似LangSmith的调试和追踪功能
type Inspector struct {
	mu       sync.RWMutex
	traces   []*TraceSpan
	sessions map[string]*SessionTrace
	maxSpans int
}

// TraceSpan 表示一个执行跨度（类似OpenTelemetry Span）
type TraceSpan struct {
	ID         string                 `json:"id"`
	ParentID   string                 `json:"parent_id,omitempty"`
	TraceID    string                 `json:"trace_id"`
	SessionID  string                 `json:"session_id"`
	Name       string                 `json:"name"`
	Kind       string                 `json:"kind"`   // agent, llm, tool, memory
	Status     string                 `json:"status"` // started, completed, failed
	StartTime  time.Time              `json:"start_time"`
	EndTime    time.Time              `json:"end_time,omitempty"`
	Duration   time.Duration          `json:"duration,omitempty"`
	Attributes map[string]interface{} `json:"attributes,omitempty"`
	Events     []SpanEvent            `json:"events,omitempty"`
	Error      string                 `json:"error,omitempty"`

	// Token统计
	PromptTokens     int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
	TotalTokens      int `json:"total_tokens,omitempty"`
}

// SpanEvent 表示Span内的事件
type SpanEvent struct {
	Name       string                 `json:"name"`
	Timestamp  time.Time              `json:"timestamp"`
	Attributes map[string]interface{} `json:"attributes,omitempty"`
}

// SessionTrace 表示一个会话的完整追踪
type SessionTrace struct {
	SessionID  string       `json:"session_id"`
	AgentName  string       `json:"agent_name"`
	StartTime  time.Time    `json:"start_time"`
	EndTime    time.Time    `json:"end_time,omitempty"`
	Spans      []*TraceSpan `json:"spans"`
	TotalTurns int          `json:"total_turns"`
	TotalCost  float64      `json:"total_cost"` // 估算成本
}

// NewInspector 创建Inspector实例
func NewInspector(maxSpans int) *Inspector {
	if maxSpans <= 0 {
		maxSpans = 10000
	}
	return &Inspector{
		traces:   make([]*TraceSpan, 0, maxSpans),
		sessions: make(map[string]*SessionTrace),
		maxSpans: maxSpans,
	}
}

// StartSpan 开始一个新的Span
func (i *Inspector) StartSpan(ctx context.Context, name, kind, sessionID string) (*TraceSpan, context.Context) {
	i.mu.Lock()
	defer i.mu.Unlock()

	span := &TraceSpan{
		ID:         generateID(),
		TraceID:    generateID(),
		SessionID:  sessionID,
		Name:       name,
		Kind:       kind,
		Status:     "started",
		StartTime:  time.Now(),
		Attributes: make(map[string]interface{}),
		Events:     make([]SpanEvent, 0),
	}

	// 检查是否有父Span
	if parentSpan := SpanFromContext(ctx); parentSpan != nil {
		span.ParentID = parentSpan.ID
		span.TraceID = parentSpan.TraceID
	}

	// 存储Span
	i.traces = append(i.traces, span)
	if len(i.traces) > i.maxSpans {
		i.traces = i.traces[1:]
	}

	// 更新Session追踪
	if sessionID != "" {
		if _, exists := i.sessions[sessionID]; !exists {
			i.sessions[sessionID] = &SessionTrace{
				SessionID: sessionID,
				StartTime: time.Now(),
				Spans:     make([]*TraceSpan, 0),
			}
		}
		i.sessions[sessionID].Spans = append(i.sessions[sessionID].Spans, span)
	}

	// 将Span注入context
	ctx = context.WithValue(ctx, spanKey{}, span)

	return span, ctx
}

// EndSpan 结束一个Span
func (i *Inspector) EndSpan(span *TraceSpan, err error) {
	if span == nil {
		return
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	span.EndTime = time.Now()
	span.Duration = span.EndTime.Sub(span.StartTime)

	if err != nil {
		span.Status = "failed"
		span.Error = err.Error()
	} else {
		span.Status = "completed"
	}
}

// AddEvent 向Span添加事件
func (i *Inspector) AddEvent(span *TraceSpan, name string, attrs map[string]interface{}) {
	if span == nil {
		return
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	event := SpanEvent{
		Name:       name,
		Timestamp:  time.Now(),
		Attributes: attrs,
	}
	span.Events = append(span.Events, event)
}

// SetAttribute 设置Span属性
func (i *Inspector) SetAttribute(span *TraceSpan, key string, value interface{}) {
	if span == nil {
		return
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	span.Attributes[key] = value
}

// GetTraces 获取所有追踪数据
func (i *Inspector) GetTraces() []*TraceSpan {
	i.mu.RLock()
	defer i.mu.RUnlock()

	result := make([]*TraceSpan, len(i.traces))
	copy(result, i.traces)
	return result
}

// GetSessionTrace 获取指定会话的追踪
func (i *Inspector) GetSessionTrace(sessionID string) *SessionTrace {
	i.mu.RLock()
	defer i.mu.RUnlock()

	return i.sessions[sessionID]
}

// GetAllSessions 获取所有会话
func (i *Inspector) GetAllSessions() []*SessionTrace {
	i.mu.RLock()
	defer i.mu.RUnlock()

	result := make([]*SessionTrace, 0, len(i.sessions))
	for _, session := range i.sessions {
		result = append(result, session)
	}
	return result
}

// GetStats 获取统计信息
func (i *Inspector) GetStats() *InspectorStats {
	i.mu.RLock()
	defer i.mu.RUnlock()

	stats := &InspectorStats{
		TotalSpans:    len(i.traces),
		TotalSessions: len(i.sessions),
		SpanByKind:    make(map[string]int),
		SpanByStatus:  make(map[string]int),
	}

	var totalTokens int
	var totalDuration time.Duration

	for _, span := range i.traces {
		stats.SpanByKind[span.Kind]++
		stats.SpanByStatus[span.Status]++
		totalTokens += span.TotalTokens
		totalDuration += span.Duration
	}

	stats.TotalTokens = totalTokens
	stats.AvgDuration = totalDuration / time.Duration(max(len(i.traces), 1))

	return stats
}

// InspectorStats Inspector统计信息
type InspectorStats struct {
	TotalSpans    int            `json:"total_spans"`
	TotalSessions int            `json:"total_sessions"`
	TotalTokens   int            `json:"total_tokens"`
	AvgDuration   time.Duration  `json:"avg_duration"`
	SpanByKind    map[string]int `json:"span_by_kind"`
	SpanByStatus  map[string]int `json:"span_by_status"`
}

// Context key for span
type spanKey struct{}

// SpanFromContext 从context中提取Span
func SpanFromContext(ctx context.Context) *TraceSpan {
	span, _ := ctx.Value(spanKey{}).(*TraceSpan)
	return span
}

// generateID 生成简单的ID
func generateID() string {
	return agent.NewRequestID()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
