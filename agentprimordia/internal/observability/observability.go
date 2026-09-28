// Package observability 提供 trace → 指标 → 审计 全链路关联（v3.5-4）。
//
// 目标：单请求可全链路回溯。
// 一个请求（一次 agent.Run）以 trace_id 为关联键，聚合：
//   - Trace：本次请求产生的全部 Span（agent.run / llm.call / tool.call ...）
//   - Metrics：LLM/Tool/Turn 调用次数、token、成本、耗时
//   - Audit：本次请求写入的全部审计事件
//
// 通过 CorrelationStore.Get(traceID) 一次取回三域数据，即可完整还原一次请求。
package observability

import (
	"context"
	"sort"
	"sync"
	"time"

	"agentprimordia/internal/audit"
)

// ===== Context 关联键 =====

type traceIDKey struct{}

// WithTraceID 将 trace_id 写入 context，供审计/指标记录时关联。
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDKey{}, traceID)
}

// TraceIDFromContext 从 context 提取 trace_id；未设置时返回空串。
func TraceIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(traceIDKey{}).(string); ok {
		return v
	}
	return ""
}

// ===== Span 记录 =====

// SpanRecord 采集到的 Span 信息。
type SpanRecord struct {
	Name         string         `json:"name"`
	Kind         string         `json:"kind"`
	TraceID      string         `json:"trace_id"`
	SpanID       string         `json:"span_id"`
	ParentSpanID string         `json:"parent_span_id,omitempty"`
	Status       string         `json:"status"`
	StatusDesc   string         `json:"status_description,omitempty"`
	Attributes   map[string]any `json:"attributes,omitempty"`
	StartedAt    time.Time      `json:"started_at"`
	EndedAt      time.Time      `json:"ended_at,omitempty"`
	DurationMs   int64          `json:"duration_ms"`
}

// ===== 单请求指标 =====

// RequestMetrics 单请求聚合指标。
type RequestMetrics struct {
	LLMCalls         int     `json:"llm_calls"`
	ToolCalls        int     `json:"tool_calls"`
	Turns            int     `json:"turns"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	CostUSD          float64 `json:"cost_usd"`
	LLMLatencyMs     int64   `json:"llm_latency_ms"`
	ToolLatencyMs    int64   `json:"tool_latency_ms"`
}

// ===== 请求全链路视图 =====

// RequestTrace 单请求全链路视图：trace + metrics + audit 三域关联。
type RequestTrace struct {
	TraceID     string         `json:"trace_id"`
	AgentName   string         `json:"agent_name,omitempty"`
	SessionID   string         `json:"session_id,omitempty"`
	StartedAt   time.Time      `json:"started_at"`
	EndedAt     time.Time      `json:"ended_at,omitempty"`
	DurationMs  int64          `json:"duration_ms"`
	Spans       []SpanRecord   `json:"spans"`
	AuditEvents []AuditEvent   `json:"audit_events"`
	Metrics     RequestMetrics `json:"metrics"`
}

// AuditEvent 关联层使用的审计事件——统一为 internal/audit.Event 的别名
// （修复评估报告 §六.2：三份重复模型合并为 audit 一份；字段与 json tag 完全一致）。
type AuditEvent = audit.Event

// record 单条请求的内部存储（带锁保证并发安全）。
type record struct {
	mu    sync.RWMutex
	trace *RequestTrace
}

// ===== 关联存储 =====

// defaultMaxRecords CorrelationStore 的默认有界保留上限。
// v7.4：该存储此前为无界 map，长驻进程持续增长；默认构造进入 Agent 前先加界。
const defaultMaxRecords = 1000

// CorrelationOption 配置 CorrelationStore。
type CorrelationOption func(*CorrelationStore)

// WithMaxRecords 设置有界保留上限（n <= 0 时回退默认值，避免意外变成无界）。
func WithMaxRecords(n int) CorrelationOption {
	return func(s *CorrelationStore) {
		if n > 0 {
			s.maxRecords = n
		}
	}
}

// CorrelationStore 线程安全的请求关联存储（有界保留，超出上限淘汰最旧记录）。
type CorrelationStore struct {
	mu         sync.RWMutex
	records    map[string]*record
	order      []string // traceID 插入顺序（保证 List 稳定 + 淘汰最旧）
	maxRecords int
}

// NewCorrelationStore 创建关联存储（默认有界保留 defaultMaxRecords 条）。
func NewCorrelationStore(opts ...CorrelationOption) *CorrelationStore {
	s := &CorrelationStore{records: make(map[string]*record), maxRecords: defaultMaxRecords}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// evictLocked 在持锁状态下按插入顺序淘汰超出上限的最旧记录。
func (s *CorrelationStore) evictLocked() {
	for s.maxRecords > 0 && len(s.order) > s.maxRecords {
		oldest := s.order[0]
		s.order = s.order[1:]
		delete(s.records, oldest)
	}
}

// Start 登记一次新请求。
func (s *CorrelationStore) Start(traceID, agentName, sessionID string) *RequestTrace {
	rt := &RequestTrace{
		TraceID:   traceID,
		AgentName: agentName,
		SessionID: sessionID,
		StartedAt: time.Now(),
		Spans:     make([]SpanRecord, 0, 8),
	}
	s.mu.Lock()
	if _, ok := s.records[traceID]; !ok {
		s.order = append(s.order, traceID)
	}
	s.records[traceID] = &record{trace: rt}
	s.evictLocked()
	s.mu.Unlock()
	return rt
}

// End 结束一次请求并计算总耗时。
func (s *CorrelationStore) End(traceID string) {
	s.mu.RLock()
	r, ok := s.records[traceID]
	s.mu.RUnlock()
	if !ok {
		return
	}
	r.mu.Lock()
	r.trace.EndedAt = time.Now()
	r.trace.DurationMs = r.trace.EndedAt.Sub(r.trace.StartedAt).Milliseconds()
	r.mu.Unlock()
}

// AddSpan 记录一条 Span。
func (s *CorrelationStore) AddSpan(traceID string, sp SpanRecord) {
	s.mu.RLock()
	r, ok := s.records[traceID]
	s.mu.RUnlock()
	if !ok {
		return
	}
	r.mu.Lock()
	sp.TraceID = traceID
	if sp.StartedAt.IsZero() {
		sp.StartedAt = time.Now()
	}
	r.trace.Spans = append(r.trace.Spans, sp)
	r.mu.Unlock()
}

// AddAuditEvent 记录一条审计事件。
func (s *CorrelationStore) AddAuditEvent(traceID string, ev AuditEvent) {
	s.mu.RLock()
	r, ok := s.records[traceID]
	s.mu.RUnlock()
	if !ok {
		return
	}
	r.mu.Lock()
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now()
	}
	ev.TraceID = traceID
	r.trace.AuditEvents = append(r.trace.AuditEvents, ev)
	r.mu.Unlock()
}

// RecordLLM 记录一次 LLM 调用的指标。
func (s *CorrelationStore) RecordLLM(traceID string, latency time.Duration, promptTokens, completionTokens int, costUSD float64) {
	s.mu.RLock()
	r, ok := s.records[traceID]
	s.mu.RUnlock()
	if !ok {
		return
	}
	r.mu.Lock()
	m := &r.trace.Metrics
	m.LLMCalls++
	m.PromptTokens += promptTokens
	m.CompletionTokens += completionTokens
	m.TotalTokens += promptTokens + completionTokens
	m.CostUSD += costUSD
	m.LLMLatencyMs += latency.Milliseconds()
	r.mu.Unlock()
}

// RecordTool 记录一次 Tool 调用的指标。
func (s *CorrelationStore) RecordTool(traceID string, latency time.Duration) {
	s.mu.RLock()
	r, ok := s.records[traceID]
	s.mu.RUnlock()
	if !ok {
		return
	}
	r.mu.Lock()
	m := &r.trace.Metrics
	m.ToolCalls++
	m.ToolLatencyMs += latency.Milliseconds()
	r.mu.Unlock()
}

// RecordTurn 记录一次 Turn。
func (s *CorrelationStore) RecordTurn(traceID string) {
	s.mu.RLock()
	r, ok := s.records[traceID]
	s.mu.RUnlock()
	if !ok {
		return
	}
	r.mu.Lock()
	r.trace.Metrics.Turns++
	r.mu.Unlock()
}

// Get 按 trace_id 取回单请求全链路视图（深拷贝快照）。
func (s *CorrelationStore) Get(traceID string) *RequestTrace {
	s.mu.RLock()
	r, ok := s.records[traceID]
	s.mu.RUnlock()
	if !ok {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return s.clone(r.trace)
}

// List 按插入顺序返回全部请求快照；limit<=0 表示不限。
func (s *CorrelationStore) List(limit int) []*RequestTrace {
	s.mu.RLock()
	ids := make([]string, len(s.order))
	copy(ids, s.order)
	s.mu.RUnlock()

	out := make([]*RequestTrace, 0, len(ids))
	for _, id := range ids {
		if limit > 0 && len(out) >= limit {
			break
		}
		if rt := s.Get(id); rt != nil {
			out = append(out, rt)
		}
	}
	return out
}

// ListByAgent 按 Agent 名筛选请求快照。
func (s *CorrelationStore) ListByAgent(agentName string, limit int) []*RequestTrace {
	all := s.List(limit * 2)
	out := make([]*RequestTrace, 0, len(all))
	for _, rt := range all {
		if rt.AgentName == agentName {
			out = append(out, rt)
		}
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// Len 返回已登记的请求数。
func (s *CorrelationStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.records)
}

// MaxRecords 返回有界保留上限。
func (s *CorrelationStore) MaxRecords() int { return s.maxRecords }

// DefaultMaxRecords 返回默认有界保留上限。
func DefaultMaxRecords() int { return defaultMaxRecords }

// SpanCount 返回某个 trace 的 span 数（便于断言）。
func (s *CorrelationStore) SpanCount(traceID string) int {
	rt := s.Get(traceID)
	if rt == nil {
		return 0
	}
	return len(rt.Spans)
}

// AuditCount 返回某个 trace 的审计事件数。
func (s *CorrelationStore) AuditCount(traceID string) int {
	rt := s.Get(traceID)
	if rt == nil {
		return 0
	}
	return len(rt.AuditEvents)
}

// clone 深拷贝 RequestTrace，保证调用方可安全修改。
func (s *CorrelationStore) clone(rt *RequestTrace) *RequestTrace {
	out := &RequestTrace{
		TraceID:     rt.TraceID,
		AgentName:   rt.AgentName,
		SessionID:   rt.SessionID,
		StartedAt:   rt.StartedAt,
		EndedAt:     rt.EndedAt,
		DurationMs:  rt.DurationMs,
		Spans:       make([]SpanRecord, len(rt.Spans)),
		AuditEvents: make([]AuditEvent, len(rt.AuditEvents)),
		Metrics:     rt.Metrics,
	}
	copy(out.Spans, rt.Spans)
	copy(out.AuditEvents, rt.AuditEvents)
	return out
}

// SortTraceByStart 按开始时间升序排序请求快照（desc=true 最近优先）。
func SortTraceByStart(traces []*RequestTrace, desc bool) {
	sort.SliceStable(traces, func(i, j int) bool {
		if desc {
			return traces[i].StartedAt.After(traces[j].StartedAt)
		}
		return traces[i].StartedAt.Before(traces[j].StartedAt)
	})
}

// ===== v7.1 告警接口 =====

// AlertSeverity 告警严重度
type AlertSeverity string

const (
	SeverityInfo     AlertSeverity = "info"
	SeverityWarning  AlertSeverity = "warning"
	SeverityCritical AlertSeverity = "critical"
)

// AlertRule 告警规则接口
type AlertRule interface {
	Name() string
	Evaluate(store *CorrelationStore) ([]AlertEvent, error)
}

// AlertEvent 告警事件
type AlertEvent struct {
	Rule      string        `json:"rule"`
	Severity  AlertSeverity `json:"severity"`
	Message   string        `json:"message"`
	TraceIDs  []string      `json:"trace_ids,omitempty"`
	Threshold float64       `json:"threshold"`
	Actual    float64       `json:"actual"`
}

// thresholdRule 阈值告警规则
type thresholdRule struct {
	name      string
	threshold float64
	severity  AlertSeverity
	metricFn  func(store *CorrelationStore) float64
}

func (r *thresholdRule) Name() string { return r.name }
func (r *thresholdRule) Evaluate(store *CorrelationStore) ([]AlertEvent, error) {
	val := r.metricFn(store)
	if val > r.threshold {
		return []AlertEvent{{
			Rule:      r.name,
			Severity:  r.severity,
			Message:   r.name + " exceeded threshold",
			Threshold: r.threshold,
			Actual:    val,
		}}, nil
	}
	return nil, nil
}
