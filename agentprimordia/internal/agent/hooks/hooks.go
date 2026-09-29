// Package hooks 提供 Agent 钩子管理器
package hooks

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/core"
)

// hookContextPool 复用 HookContext 实例。
var hookContextPool = sync.Pool{
	New: func() any {
		return &HookContext{}
	},
}

// AcquireHookContext 从池中获取一个已重置的 HookContext。
func AcquireHookContext() *HookContext {
	hctx := hookContextPool.Get().(*HookContext)
	hctx.Reset()
	return hctx
}

// ReleaseHookContext 归还 HookContext 到池中。
func ReleaseHookContext(hctx *HookContext) {
	if hctx == nil {
		return
	}
	hctx.Reset()
	hookContextPool.Put(hctx)
}

// Reset 清空 HookContext 的所有字段。
func (h *HookContext) Reset() {
	if h == nil {
		return
	}
	h.AgentID = ""
	h.RequestID = ""
	h.SessionID = ""
	h.Point = ""
	h.Turn = 0
	h.Message = nil
	h.Response = nil
	h.ToolCall = nil
	h.ToolResult = nil
	h.Error = nil
	if h.Metadata != nil {
		for k := range h.Metadata {
			delete(h.Metadata, k)
		}
	}
	h.StreamChunk = nil
	h.Duration = 0
	h.OldState = ""
	h.NewState = ""
	h.Reason = ""
	h.MemoryQuery = ""
	h.MemoryResult = nil
	h.ContextWindowUsage = 0
	h.ContextWindowLimit = 0
}

// HookPoint 钩子触发点
type HookPoint string

// 钩子点常量
const (
	HookBeforeRun           HookPoint = "before_run"
	HookAfterRun            HookPoint = "after_run"
	HookBeforeTurn          HookPoint = "before_turn"
	HookAfterTurn           HookPoint = "after_turn"
	HookBeforeLLM           HookPoint = "before_llm"
	HookAfterLLM            HookPoint = "after_llm"
	HookBeforeTool          HookPoint = "before_tool"
	HookAfterTool           HookPoint = "after_tool"
	HookOnError             HookPoint = "on_error"
	HookOnComplete          HookPoint = "on_complete"
	HookBeforeRAG           HookPoint = "before_rag"
	HookAfterRAG            HookPoint = "after_rag"
	HookBeforePipelineStep  HookPoint = "before_pipeline_step"
	HookAfterPipelineStep   HookPoint = "after_pipeline_step"
	HookBeforeHandoff       HookPoint = "before_handoff"
	HookAfterHandoff        HookPoint = "after_handoff"
	HookBeforeParallelAgent HookPoint = "before_parallel_agent"
	HookAfterParallelAgent  HookPoint = "after_parallel_agent"
	HookBeforeDAGNode       HookPoint = "before_dag_node"
	HookAfterDAGNode        HookPoint = "after_dag_node"
	HookOnStream            HookPoint = "on_stream"
	HookOnStreamStart       HookPoint = "on_stream_start"
	HookOnStreamEnd         HookPoint = "on_stream_end"
	HookBeforeMemoryRead    HookPoint = "before_memory_read"
	HookAfterMemoryRead     HookPoint = "after_memory_read"
	HookBeforeMemoryWrite   HookPoint = "before_memory_write"
	HookAfterMemoryWrite    HookPoint = "after_memory_write"
	HookContextWindowUpdate HookPoint = "context_window_update"
	HookContextWindowFull   HookPoint = "context_window_full"
	HookBeforeToolParse     HookPoint = "before_tool_parse"
	HookAfterToolParse      HookPoint = "after_tool_parse"
	HookOnMetricsCollect    HookPoint = "on_metrics_collect"
	HookBeforeShutdown      HookPoint = "before_shutdown"
	HookAfterShutdown       HookPoint = "after_shutdown"
	HookOnStateChange       HookPoint = "on_state_change"
)

// HookContext 钩子执行上下文
type HookContext struct {
	AgentID    string
	RequestID  string
	SessionID  string
	Point      HookPoint
	Turn       int
	Message    *core.Message
	Response   *core.Response
	ToolCall   *core.ToolCall
	ToolResult *core.ToolResult
	Error      error
	Metadata   map[string]any

	StreamChunk *core.StreamEvent
	Duration    time.Duration

	OldState string
	NewState string
	Reason   string

	MemoryQuery  string
	MemoryResult any

	ContextWindowUsage float64
	ContextWindowLimit int
}

// HookFunc 钩子函数类型
type HookFunc func(ctx context.Context, hctx *HookContext) error

// Hooks 是 HookManager 指针的类型别名
type Hooks = *HookManager

// HookPhase 钩子执行阶段
type HookPhase int

const (
	PhaseValidation HookPhase = iota
	PhasePreProcessing
	PhaseExecution
	PhasePostProcessing
)

// Hook 钩子定义
type Hook struct {
	Point     HookPoint
	Func      HookFunc
	Priority  int
	Condition HookCondition
	ID        string
	Phase     HookPhase
}

// HookCondition 钩子条件
type HookCondition func(ctx context.Context, hctx *HookContext) bool

// Always 总是返回 true
func Always(_ context.Context, _ *HookContext) bool { return true }

// OnTurn 在指定 turn 号时触发
func OnTurn(turnNum int) HookCondition {
	return func(_ context.Context, hctx *HookContext) bool {
		return hctx.Turn == turnNum
	}
}

// OnTurnsGreater 在 turn 大于指定值时触发
func OnTurnsGreater(threshold int) HookCondition {
	return func(_ context.Context, hctx *HookContext) bool {
		return hctx.Turn > threshold
	}
}

// OnError 当有错误时触发
func OnError() HookCondition {
	return func(_ context.Context, hctx *HookContext) bool {
		return hctx.Error != nil
	}
}

// OnMetadataKey 当 Metadata 包含指定 key 时触发
func OnMetadataKey(key string) HookCondition {
	return func(_ context.Context, hctx *HookContext) bool {
		if hctx.Metadata == nil {
			return false
		}
		_, ok := hctx.Metadata[key]
		return ok
	}
}

// OnStateTransition 指定状态转换时触发
func OnStateTransition(from, to string) HookCondition {
	return func(_ context.Context, hctx *HookContext) bool {
		return hctx.OldState == from && hctx.NewState == to
	}
}

const hookPointCount = 64

// HookStats 钩子执行统计
type HookStats struct {
	TotalFired   int64
	TotalErrors  int64
	ByPoint      []atomic.Int64
	ByPointError []atomic.Int64
	nameIndex    map[HookPoint]int
}

// NewHookStats 创建钩子执行统计器
func NewHookStats() *HookStats {
	idx := make(map[HookPoint]int, hookPointCount)
	all := []HookPoint{
		HookBeforeRun, HookAfterRun, HookBeforeTurn, HookAfterTurn,
		HookBeforeLLM, HookAfterLLM, HookBeforeTool, HookAfterTool,
		HookOnError, HookOnComplete, HookBeforeRAG, HookAfterRAG,
		HookBeforePipelineStep, HookAfterPipelineStep,
		HookBeforeHandoff, HookAfterHandoff,
		HookBeforeParallelAgent, HookAfterParallelAgent,
		HookBeforeDAGNode, HookAfterDAGNode,
		HookOnStream, HookOnStreamStart, HookOnStreamEnd,
		HookBeforeMemoryRead, HookAfterMemoryRead, HookBeforeMemoryWrite, HookAfterMemoryWrite,
		HookContextWindowUpdate, HookContextWindowFull,
		HookBeforeToolParse, HookAfterToolParse,
		HookOnMetricsCollect, HookBeforeShutdown, HookAfterShutdown, HookOnStateChange,
	}
	for i, p := range all {
		idx[p] = i
	}
	return &HookStats{
		ByPoint:      make([]atomic.Int64, hookPointCount),
		ByPointError: make([]atomic.Int64, hookPointCount),
		nameIndex:    idx,
	}
}

func (s *HookStats) Record(point HookPoint, err error) {
	atomic.AddInt64(&s.TotalFired, 1)
	if idx, ok := s.nameIndex[point]; ok && idx < hookPointCount {
		s.ByPoint[idx].Add(1)
		if err != nil {
			atomic.AddInt64(&s.TotalErrors, 1)
			s.ByPointError[idx].Add(1)
		}
	}
}

func (s *HookStats) Snapshot() map[string]interface{} {
	result := map[string]interface{}{
		"total_fired":  atomic.LoadInt64(&s.TotalFired),
		"total_errors": atomic.LoadInt64(&s.TotalErrors),
		"by_point":     make(map[string]int64, len(s.nameIndex)),
		"by_errors":    make(map[string]int64, len(s.nameIndex)),
	}
	for p, idx := range s.nameIndex {
		if v := s.ByPoint[idx].Load(); v > 0 {
			result["by_point"].(map[string]int64)[string(p)] = v
		}
		if v := s.ByPointError[idx].Load(); v > 0 {
			result["by_errors"].(map[string]int64)[string(p)] = v
		}
	}
	return result
}

// HookManager 钩子管理器
type HookManager struct {
	mu             sync.RWMutex
	hooks          map[HookPoint][]Hook
	stats          *HookStats
	middleware     []HookMiddleware
	hooksSnap      atomic.Pointer[map[HookPoint][]Hook]
	middlewareSnap atomic.Pointer[[]HookMiddleware]
}

// HookMiddleware 中间件接口
type HookMiddleware interface {
	Before(ctx context.Context, hctx *HookContext) error
	After(ctx context.Context, hctx *HookContext, err error) error
}

// HookMiddlewareFunc 函数式中间件
type HookMiddlewareFunc struct {
	BeforeFn func(ctx context.Context, hctx *HookContext) error
	AfterFn  func(ctx context.Context, hctx *HookContext, err error) error
}

func (m *HookMiddlewareFunc) Before(ctx context.Context, hctx *HookContext) error {
	if m.BeforeFn != nil {
		return m.BeforeFn(ctx, hctx)
	}
	return nil
}

func (m *HookMiddlewareFunc) After(ctx context.Context, hctx *HookContext, err error) error {
	if m.AfterFn != nil {
		return m.AfterFn(ctx, hctx, err)
	}
	return err
}

// NewHookManager 创建钩子管理器
func NewHookManager() *HookManager {
	return &HookManager{
		hooks:      make(map[HookPoint][]Hook),
		stats:      NewHookStats(),
		middleware: make([]HookMiddleware, 0),
	}
}

func (m *HookManager) Register(point HookPoint, fn HookFunc) {
	m.RegisterWithPriority(point, fn, 0)
}

func (m *HookManager) RegisterWithPriority(point HookPoint, fn HookFunc, priority int) {
	m.RegisterConditional(point, fn, priority, Always, "")
}

func (m *HookManager) RegisterConditional(point HookPoint, fn HookFunc, priority int, condition HookCondition, id string) {
	m.RegisterConditionalInPhase(PhaseExecution, point, fn, priority, condition, id)
}

func (m *HookManager) RegisterInPhase(phase HookPhase, point HookPoint, fn HookFunc) {
	m.RegisterConditionalInPhase(phase, point, fn, 0, Always, "")
}

func (m *HookManager) refreshSnapshots() {
	newHooks := make(map[HookPoint][]Hook, len(m.hooks))
	for p, hs := range m.hooks {
		newHooks[p] = append([]Hook(nil), hs...)
	}
	m.hooksSnap.Store(&newHooks)

	newMid := append([]HookMiddleware(nil), m.middleware...)
	m.middlewareSnap.Store(&newMid)
}

func (m *HookManager) RegisterConditionalInPhase(phase HookPhase, point HookPoint, fn HookFunc, priority int, condition HookCondition, id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	hook := Hook{
		Point:     point,
		Func:      fn,
		Priority:  priority,
		Condition: condition,
		ID:        id,
		Phase:     phase,
	}
	m.hooks[point] = append(m.hooks[point], hook)

	for i := len(m.hooks[point]) - 1; i > 0; i-- {
		if m.hooks[point][i].Priority < m.hooks[point][i-1].Priority {
			m.hooks[point][i], m.hooks[point][i-1] = m.hooks[point][i-1], m.hooks[point][i]
		} else {
			break
		}
	}
	m.refreshSnapshots()
}

func (m *HookManager) Use(middleware HookMiddleware) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.middleware = append(m.middleware, middleware)
	m.refreshSnapshots()
}

func (m *HookManager) Fire(ctx context.Context, hctx *HookContext) error {
	var hooks []Hook
	if snap := m.hooksSnap.Load(); snap != nil {
		hooks = (*snap)[hctx.Point]
	} else {
		m.mu.RLock()
		hooks = append([]Hook(nil), m.hooks[hctx.Point]...)
		m.mu.RUnlock()
	}

	var mids []HookMiddleware
	if snap := m.middlewareSnap.Load(); snap != nil {
		mids = *snap
	} else {
		m.mu.RLock()
		mids = append([]HookMiddleware(nil), m.middleware...)
		m.mu.RUnlock()
	}

	var execErr error

	for _, mid := range mids {
		if err := mid.Before(ctx, hctx); err != nil {
			m.stats.Record(hctx.Point, err)
			return err
		}
	}

	for _, phase := range phaseOrder {
		for _, hook := range hooks {
			if hook.Phase != phase {
				continue
			}
			if hook.Condition == nil || hook.Condition(ctx, hctx) {
				if err := hook.Func(ctx, hctx); err != nil {
					execErr = err
					m.stats.Record(hctx.Point, err)
					break
				}
			}
		}
		if execErr != nil {
			break
		}
	}

	if execErr == nil {
		m.stats.Record(hctx.Point, nil)
	}

	for i := len(mids) - 1; i >= 0; i-- {
		afterErr := mids[i].After(ctx, hctx, execErr)
		if afterErr != nil {
			if afterErr == errRecovered {
				execErr = nil
			} else {
				execErr = afterErr
			}
		}
	}

	return execErr
}

func (m *HookManager) Remove(point HookPoint) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.hooks, point)
	m.refreshSnapshots()
}

func (m *HookManager) RemoveByID(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for point, hooks := range m.hooks {
		filtered := hooks[:0]
		for _, h := range hooks {
			if h.ID != id {
				filtered = append(filtered, h)
			}
		}
		m.hooks[point] = filtered
	}
	m.refreshSnapshots()
}

func (m *HookManager) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hooks = make(map[HookPoint][]Hook)
	m.middleware = nil
	m.refreshSnapshots()
}

func (m *HookManager) Count(point HookPoint) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.hooks[point])
}

func (m *HookManager) TotalCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	total := 0
	for _, hooks := range m.hooks {
		total += len(hooks)
	}
	return total
}

func (m *HookManager) Stats() *HookStats {
	return m.stats
}

func (m *HookManager) RegisteredPoints() []HookPoint {
	m.mu.RLock()
	defer m.mu.RUnlock()
	points := make([]HookPoint, 0, len(m.hooks))
	for p := range m.hooks {
		points = append(points, p)
	}
	return points
}

// ===== 便捷方法 =====

func (m *HookManager) OnReasoning(ctx context.Context, thought *core.Thought) {
	hctx := AcquireHookContext()
	defer ReleaseHookContext(hctx)
	hctx.Point = HookAfterLLM
	_ = m.Fire(ctx, hctx)
}

func (m *HookManager) OnComplete(ctx context.Context, resp *core.Response) {
	hctx := AcquireHookContext()
	defer ReleaseHookContext(hctx)
	hctx.Point = HookOnComplete
	hctx.Response = resp
	_ = m.Fire(ctx, hctx)
}

func (m *HookManager) OnTurnComplete(ctx context.Context, turn int, thought *core.Thought) {
	hctx := AcquireHookContext()
	defer ReleaseHookContext(hctx)
	hctx.Point = HookAfterTurn
	hctx.Turn = turn
	_ = m.Fire(ctx, hctx)
}

func (m *HookManager) OnToolUse(ctx context.Context, tc *core.ToolCall) {
	hctx := AcquireHookContext()
	defer ReleaseHookContext(hctx)
	hctx.Point = HookBeforeTool
	hctx.ToolCall = tc
	_ = m.Fire(ctx, hctx)
}

func (m *HookManager) OnToolResult(ctx context.Context, result *core.ToolResult) {
	hctx := AcquireHookContext()
	defer ReleaseHookContext(hctx)
	hctx.Point = HookAfterTool
	hctx.ToolResult = result
	_ = m.Fire(ctx, hctx)
}

func (m *HookManager) OnError(ctx context.Context, err error) {
	hctx := AcquireHookContext()
	defer ReleaseHookContext(hctx)
	hctx.Point = HookOnError
	hctx.Error = err
	_ = m.Fire(ctx, hctx)
}

func (m *HookManager) OnStream(ctx context.Context, event *core.StreamEvent) {
	hctx := AcquireHookContext()
	defer ReleaseHookContext(hctx)
	hctx.Point = HookOnStream
	hctx.StreamChunk = event
	_ = m.Fire(ctx, hctx)
}

func (m *HookManager) OnStreamStart(ctx context.Context) {
	hctx := AcquireHookContext()
	defer ReleaseHookContext(hctx)
	hctx.Point = HookOnStreamStart
	_ = m.Fire(ctx, hctx)
}

func (m *HookManager) OnStreamEnd(ctx context.Context, duration time.Duration) {
	hctx := AcquireHookContext()
	defer ReleaseHookContext(hctx)
	hctx.Point = HookOnStreamEnd
	hctx.Duration = duration
	_ = m.Fire(ctx, hctx)
}

func (m *HookManager) OnStateChange(ctx context.Context, agentID, oldState, newState, reason string) {
	hctx := AcquireHookContext()
	defer ReleaseHookContext(hctx)
	hctx.Point = HookOnStateChange
	hctx.AgentID = agentID
	hctx.OldState = oldState
	hctx.NewState = newState
	hctx.Reason = reason
	_ = m.Fire(ctx, hctx)
}

func (m *HookManager) OnMemoryRead(ctx context.Context, query string, result any) {
	hctx := AcquireHookContext()
	defer ReleaseHookContext(hctx)
	hctx.Point = HookAfterMemoryRead
	hctx.MemoryQuery = query
	hctx.MemoryResult = result
	_ = m.Fire(ctx, hctx)
}

func (m *HookManager) OnMemoryWrite(ctx context.Context, sessionID string) {
	hctx := AcquireHookContext()
	defer ReleaseHookContext(hctx)
	hctx.Point = HookAfterMemoryWrite
	hctx.SessionID = sessionID
	_ = m.Fire(ctx, hctx)
}

func (m *HookManager) OnContextWindowUpdate(ctx context.Context, usage float64, limit int) {
	hctx := AcquireHookContext()
	defer ReleaseHookContext(hctx)
	hctx.Point = HookContextWindowUpdate
	hctx.ContextWindowUsage = usage
	hctx.ContextWindowLimit = limit
	_ = m.Fire(ctx, hctx)
}

func (m *HookManager) OnShutdown(ctx context.Context) {
	hctx := AcquireHookContext()
	defer ReleaseHookContext(hctx)
	hctx.Point = HookBeforeShutdown
	_ = m.Fire(ctx, hctx)
}

func (m *HookManager) OnShutdownComplete(ctx context.Context) {
	hctx := AcquireHookContext()
	defer ReleaseHookContext(hctx)
	hctx.Point = HookAfterShutdown
	_ = m.Fire(ctx, hctx)
}

// AllHookPoints 返回所有定义的钩子点常量
func AllHookPoints() []HookPoint {
	return []HookPoint{
		HookBeforeRun, HookAfterRun,
		HookBeforeTurn, HookAfterTurn,
		HookBeforeLLM, HookAfterLLM,
		HookBeforeTool, HookAfterTool,
		HookOnError, HookOnComplete,
		HookBeforeRAG, HookAfterRAG,
		HookBeforePipelineStep, HookAfterPipelineStep,
		HookBeforeHandoff, HookAfterHandoff,
		HookBeforeParallelAgent, HookAfterParallelAgent,
		HookBeforeDAGNode, HookAfterDAGNode,
		HookOnStream, HookOnStreamStart, HookOnStreamEnd,
		HookBeforeMemoryRead, HookAfterMemoryRead,
		HookBeforeMemoryWrite, HookAfterMemoryWrite,
		HookContextWindowUpdate, HookContextWindowFull,
		HookBeforeToolParse, HookAfterToolParse,
		HookOnMetricsCollect,
		HookBeforeShutdown, HookAfterShutdown,
		HookOnStateChange,
	}
}

// HookPointCategory 钩子点分类
func HookPointCategory(p HookPoint) string {
	switch p {
	case HookBeforeRun, HookAfterRun, HookBeforeShutdown, HookAfterShutdown:
		return "lifecycle"
	case HookBeforeTurn, HookAfterTurn, HookOnComplete, HookOnError, HookOnStateChange:
		return "execution"
	case HookBeforeLLM, HookAfterLLM:
		return "llm"
	case HookBeforeTool, HookAfterTool, HookBeforeToolParse, HookAfterToolParse:
		return "tool"
	case HookBeforeRAG, HookAfterRAG, HookBeforeMemoryRead, HookAfterMemoryRead,
		HookBeforeMemoryWrite, HookAfterMemoryWrite:
		return "memory"
	case HookOnStream, HookOnStreamStart, HookOnStreamEnd:
		return "stream"
	case HookContextWindowUpdate, HookContextWindowFull:
		return "context"
	case HookBeforePipelineStep, HookAfterPipelineStep, HookBeforeHandoff, HookAfterHandoff,
		HookBeforeParallelAgent, HookAfterParallelAgent, HookBeforeDAGNode, HookAfterDAGNode:
		return "orchestration"
	case HookOnMetricsCollect:
		return "observability"
	default:
		return "unknown"
	}
}

// LoggingMiddleware 日志中间件
func LoggingMiddleware() *HookMiddlewareFunc {
	return &HookMiddlewareFunc{
		BeforeFn: func(_ context.Context, hctx *HookContext) error { return nil },
		AfterFn:  func(_ context.Context, hctx *HookContext, err error) error { return err },
	}
}

// MetricsCollectionMiddleware 指标收集中间件
func MetricsCollectionMiddleware(stats *HookStats) *HookMiddlewareFunc {
	return &HookMiddlewareFunc{
		BeforeFn: func(_ context.Context, _ *HookContext) error { return nil },
		AfterFn: func(_ context.Context, hctx *HookContext, err error) error {
			if stats != nil {
				stats.Record(hctx.Point, err)
			}
			return err
		},
	}
}

// TimeoutMiddleware 超时中间件
func TimeoutMiddleware(timeout time.Duration) *HookMiddlewareFunc {
	return &HookMiddlewareFunc{
		BeforeFn: func(ctx context.Context, hctx *HookContext) error { return nil },
		AfterFn: func(ctx context.Context, _ *HookContext, err error) error {
			select {
			case <-ctx.Done():
				return fmt.Errorf("hook execution cancelled")
			default:
				return err
			}
		},
	}
}

// errRecovered sentinel error
var errRecovered = errors.New("hook error recovered")

// phaseOrder Hook 执行阶段顺序
var phaseOrder = []HookPhase{PhaseValidation, PhasePreProcessing, PhaseExecution, PhasePostProcessing}

// ErrorRecoveryMiddleware 错误恢复中间件
func ErrorRecoveryMiddleware(onError func(HookPoint, error)) *HookMiddlewareFunc {
	return &HookMiddlewareFunc{
		BeforeFn: func(_ context.Context, _ *HookContext) error { return nil },
		AfterFn: func(ctx context.Context, hctx *HookContext, err error) error {
			if err != nil && onError != nil {
				onError(hctx.Point, err)
			}
			if err != nil {
				return errRecovered
			}
			return nil
		},
	}
}
