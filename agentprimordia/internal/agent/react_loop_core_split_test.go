// react_loop_core_split_test.go — runLoop / runLoopTurn 阶段拆分单元测试
//
// 覆盖从 runLoop 与 runLoopTurn 抽出的命名单元（行为保持重构的回归门）：
//
//	runLoop:     tryPlanExecution / resolveMaxTurns / checkTurnPreconditions /
//	             startTurnSpan / turnMetrics / maxTurnsExceededOutcome
//	runLoopTurn: resolveToolDefinitions / invokeLLM / recordLLMUsageAndAudit /
//	             finishTurnBookkeeping / completeWithoutToolCalls /
//	             gracefulShutdownOutcome
//
// 端到端语义（完整 ReAct 循环）由 react_loop_test.go / p1_span_leak_test.go 等
// 既有测试覆盖；本文件只锁定各阶段单元的输入输出契约，防止拆分引入行为偏差。
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/planning"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/tools"
)

// ===== 测试替身（split 前缀避免与包内其它测试文件重名） =====

// splitAuditLogger 内存审计日志器：记录事件供断言。
type splitAuditLogger struct {
	mu     sync.Mutex
	events []AuditEvent
}

func (l *splitAuditLogger) Log(_ context.Context, e AuditEvent) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, e)
	return nil
}

func (l *splitAuditLogger) countAction(action string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, e := range l.events {
		if e.Action == action {
			n++
		}
	}
	return n
}

func (l *splitAuditLogger) firstByAction(action string) (AuditEvent, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, e := range l.events {
		if e.Action == action {
			return e, true
		}
	}
	return AuditEvent{}, false
}

// splitMetricsRecorder 指标记录替身：同时实现 MetricsRecorder 与
// LabeledMetricsRecorder，记录各类调用次数供断言。
type splitMetricsRecorder struct {
	mu             sync.Mutex
	llmCalls       int
	toolCalls      int
	turns          int
	turnsWithAgent int
	tokenUsages    int
}

func (m *splitMetricsRecorder) RecordLLMCall(time.Duration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.llmCalls++
}

func (m *splitMetricsRecorder) RecordToolCall(time.Duration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.toolCalls++
}

func (m *splitMetricsRecorder) RecordTurn(time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.turns++
}

func (m *splitMetricsRecorder) RecordTokenUsage(string, int, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokenUsages++
}

func (m *splitMetricsRecorder) IncActiveAgents() {}
func (m *splitMetricsRecorder) DecActiveAgents() {}

func (m *splitMetricsRecorder) RecordLLMCallWithLabels(time.Duration, error, string, string) {}
func (m *splitMetricsRecorder) RecordToolCallWithLabels(time.Duration, error, string)        {}
func (m *splitMetricsRecorder) RecordTurnWithAgent(time.Duration, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.turnsWithAgent++
}

func (m *splitMetricsRecorder) snapshot() (turns, turnsWithAgent, tokenUsages int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.turns, m.turnsWithAgent, m.tokenUsages
}

// splitEventPublisher 事件发布替身：记录事件类型序列。
type splitEventPublisher struct {
	mu     sync.Mutex
	events []string
}

func (p *splitEventPublisher) PublishAsync(eventType, _ string, _ any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, eventType)
	return nil
}

func (p *splitEventPublisher) has(eventType string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, e := range p.events {
		if e == eventType {
			return true
		}
	}
	return false
}

// splitHookLog 记录触发过的 HookPoint（含 OnComplete 的响应引用）。
type splitHookLog struct {
	mu     sync.Mutex
	points []HookPoint
	resp   *Response
	err    error
}

func (h *splitHookLog) record(point HookPoint) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.points = append(h.points, point)
}

func (h *splitHookLog) has(point HookPoint) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, p := range h.points {
		if p == point {
			return true
		}
	}
	return false
}

// newSplitHooks 构造注册了关键触发点的 HookManager。
func newSplitHooks() (*HookManager, *splitHookLog) {
	log := &splitHookLog{}
	m := NewHookManager()
	m.Register(HookAfterTurn, func(context.Context, *HookContext) error {
		log.record(HookAfterTurn)
		return nil
	})
	m.Register(HookOnComplete, func(_ context.Context, hctx *HookContext) error {
		log.record(HookOnComplete)
		log.resp = hctx.Response
		return nil
	})
	m.Register(HookOnError, func(_ context.Context, hctx *HookContext) error {
		log.record(HookOnError)
		log.err = hctx.Error
		return nil
	})
	return m, log
}

// splitStubPlanner planning.Planner 桩：返回预设计划或错误。
type splitStubPlanner struct {
	plan *planning.Plan
	err  error
}

func (p *splitStubPlanner) Decompose(context.Context, string) ([]planning.SubTask, error) {
	if p.err != nil {
		return nil, p.err
	}
	return p.plan.SubTasks, nil
}

func (p *splitStubPlanner) GeneratePlan(context.Context, string) (*planning.Plan, error) {
	return p.plan, p.err
}

// splitNoopTool 无副作用占位工具（用于工具定义解析测试）。
type splitNoopTool struct{}

func (splitNoopTool) Name() string        { return "split_noop" }
func (splitNoopTool) Description() string { return "noop" }
func (splitNoopTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (splitNoopTool) Execute(context.Context, json.RawMessage) (*tools.Result, error) {
	return tools.NewResult("ok"), nil
}

// newSplitAgent 构造最小 ReActAgent（MockLLM，无外部依赖）。
func newSplitAgent(t *testing.T) *ReActAgent {
	t.Helper()
	a := newReActAgent(ReActConfig{
		Name:     "split-test",
		Model:    llm.NewMockLLM(t).WithResponse("hello"),
		MaxTurns: 10,
	})
	// 单元测试不走 reactLoopEngine，手动补上 startTime，避免Metrics.Duration
	// 出现以起止时间零点为基准的巨大值（不影响断言，仅让日志可读）。
	a.startTime = time.Now()
	return a
}

// ===== runLoop 阶段单元 =====

// TestTryPlanExecution_NotPlanned 未进入 plan 路径的各情形：
// planner 未配置 / 自愈降级跳过 / 计划生成失败 / 单子任务计划。
func TestTryPlanExecution_NotPlanned(t *testing.T) {
	history := []Message{{Role: RoleUser, Content: "任务"}}

	cases := []struct {
		name    string
		agent   func(a *ReActAgent)
		cfg     loopConfig
		startAt int
	}{
		{
			name:    "planner 未配置",
			agent:   func(a *ReActAgent) {},
			cfg:     loopConfig{},
			startAt: 0,
		},
		{
			name: "自愈降级路径跳过 plan 分支",
			agent: func(a *ReActAgent) {
				a.capCache = &capabilityCache{planner: &splitStubPlanner{plan: multiSubTaskPlan()}}
			},
			cfg:     loopConfig{skipPlan: true},
			startAt: 0,
		},
		{
			name: "非首轮不触发",
			agent: func(a *ReActAgent) {
				a.capCache = &capabilityCache{planner: &splitStubPlanner{plan: multiSubTaskPlan()}}
			},
			cfg:     loopConfig{},
			startAt: 1,
		},
		{
			name: "计划生成失败",
			agent: func(a *ReActAgent) {
				a.capCache = &capabilityCache{planner: &splitStubPlanner{err: errors.New("boom")}}
			},
			cfg:     loopConfig{},
			startAt: 0,
		},
		{
			name: "单子任务计划",
			agent: func(a *ReActAgent) {
				a.capCache = &capabilityCache{planner: &splitStubPlanner{plan: &planning.Plan{
					Goal:      "g",
					SubTasks:  []planning.SubTask{{ID: "1", Description: "d"}},
					CreatedAt: time.Now(),
				}}}
			},
			cfg:     loopConfig{},
			startAt: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newSplitAgent(t)
			tc.agent(a)
			resp, err, planned := a.tryPlanExecution(context.Background(), history, tc.startAt, tc.cfg)
			if planned {
				t.Errorf("planned = true, want false（resp=%+v err=%v）", resp, err)
			}
			if resp != nil || err != nil {
				t.Errorf("未进入 plan 路径应返回 (nil, nil)，实际 (%+v, %v)", resp, err)
			}
		})
	}
}

// TestTryPlanExecution_Planned 多子任务计划进入 DAG 执行路径：
// 注入 subtaskExecutor 后 planned=true 并原样透传执行结果。
func TestTryPlanExecution_Planned(t *testing.T) {
	a := newSplitAgent(t)
	a.capCache = &capabilityCache{planner: &splitStubPlanner{plan: multiSubTaskPlan()}}
	a.subtaskExecutor = func(context.Context, planning.SubTask, []Message, loopConfig) (*Response, error) {
		return &Response{Content: "子任务完成"}, nil
	}

	history := []Message{{Role: RoleUser, Content: "复杂任务"}}
	resp, err, planned := a.tryPlanExecution(context.Background(), history, 0, loopConfig{})
	if !planned {
		t.Fatalf("planned = false, want true")
	}
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if resp == nil || resp.Content != "子任务完成" {
		t.Errorf("resp = %+v, want Content=子任务完成", resp)
	}
}

// multiSubTaskPlan 构造含两个无依赖子任务的计划。
func multiSubTaskPlan() *planning.Plan {
	return &planning.Plan{
		Goal: "g",
		SubTasks: []planning.SubTask{
			{ID: "1", Description: "步骤一"},
			{ID: "2", Description: "步骤二"},
		},
		CreatedAt: time.Now(),
	}
}

// TestResolveMaxTurns 子任务轮次预算覆盖语义（v7.3-P2fix）。
func TestResolveMaxTurns(t *testing.T) {
	a := newSplitAgent(t) // MaxTurns = 10
	cases := []struct {
		name            string
		subtaskMaxTurns int
		want            int
	}{
		{"未设置子任务预算用全局值", 0, 10},
		{"子任务预算更大不覆盖", 15, 10},
		{"子任务预算更小则覆盖", 4, 4},
		{"相等不覆盖", 10, 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := a.resolveMaxTurns(loopConfig{subtaskMaxTurns: tc.subtaskMaxTurns}); got != tc.want {
				t.Errorf("resolveMaxTurns(%d) = %d, want %d", tc.subtaskMaxTurns, got, tc.want)
			}
		})
	}
}

// TestCheckTurnPreconditions 轮次前置检查：正常放行 / 已停止 / ctx 取消。
func TestCheckTurnPreconditions(t *testing.T) {
	t.Run("正常放行", func(t *testing.T) {
		a := newSplitAgent(t)
		if err := a.checkTurnPreconditions(context.Background(), loopConfig{}); err != nil {
			t.Errorf("期望 nil，实际 %v", err)
		}
	})

	t.Run("lifecycle 已停止", func(t *testing.T) {
		a := newSplitAgent(t)
		a.lifecycle.Stop()
		err := a.checkTurnPreconditions(context.Background(), loopConfig{})
		if !errors.Is(err, ErrAgentStopped) {
			t.Errorf("期望 ErrAgentStopped，实际 %v", err)
		}
		if got := a.lifecycle.Status(); got != StatusCancelled {
			t.Errorf("status = %v, want %v", got, StatusCancelled)
		}
	})

	t.Run("ctx 已取消", func(t *testing.T) {
		a := newSplitAgent(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := a.checkTurnPreconditions(ctx, loopConfig{})
		if !errors.Is(err, context.Canceled) {
			t.Errorf("期望 context.Canceled，实际 %v", err)
		}
		if got := a.lifecycle.Status(); got != StatusCancelled {
			t.Errorf("status = %v, want %v", got, StatusCancelled)
		}
	})
}

// TestStartTurnSpan turn span 创建：tracer 未配置返回 NoopSpan；
// 已配置时按 "turn.<n>" 命名创建且可正常闭合。
func TestStartTurnSpan(t *testing.T) {
	t.Run("tracer 未配置", func(t *testing.T) {
		a := newSplitAgent(t)
		span := a.startTurnSpan(nil, 3, nil)
		if _, ok := span.(*NoopSpan); !ok {
			t.Errorf("期望 *NoopSpan，实际 %T", span)
		}
	})

	t.Run("tracer 已配置", func(t *testing.T) {
		a := newSplitAgent(t)
		tr := &recordingTracer{}
		span := a.startTurnSpan(tr, 2, nil)
		if _, ok := span.(*recordingSpan); !ok {
			t.Fatalf("期望 *recordingSpan，实际 %T", span)
		}
		if total, turns := tr.countNames("turn."); total != 1 || turns != 1 {
			t.Errorf("span 统计 = (%d, %d), want (1, 1)", total, turns)
		}
		span.End()
		if !span.IsEnded() {
			t.Error("End() 后 span 应已闭合")
		}
	})
}

// TestTurnMetrics 指标快照字段映射。
func TestTurnMetrics(t *testing.T) {
	got := turnMetrics(3, 5, 2*time.Second, 300*time.Millisecond, 400*time.Millisecond)
	want := Metrics{
		TotalTurns:  3,
		TotalTools:  5,
		Duration:    2 * time.Second,
		LLMLatency:  300 * time.Millisecond,
		ToolLatency: 400 * time.Millisecond,
	}
	if got != want {
		t.Errorf("turnMetrics = %+v, want %+v", got, want)
	}
}

// TestMaxTurnsExceededOutcome 超轮次终态：错误/指标/状态/hook/流事件。
func TestMaxTurnsExceededOutcome(t *testing.T) {
	a := newSplitAgent(t) // MaxTurns = 10
	hooks, hookLog := newSplitHooks()
	a.hooks = hooks

	streamCh := make(chan StreamEvent, 4)
	cfg := loopConfig{
		requestID: "req-max",
		stream:    true,
		streamCh:  streamCh,
		streamCtx: context.Background(),
	}

	resp, err := a.maxTurnsExceededOutcome(cfg, 5, time.Second, 2*time.Second)
	if !errors.Is(err, ErrMaxTurnsExceeded) {
		t.Fatalf("err = %v, want ErrMaxTurnsExceeded", err)
	}
	if resp == nil || !errors.Is(resp.Error, ErrMaxTurnsExceeded) {
		t.Errorf("resp.Error = %v, want ErrMaxTurnsExceeded", resp)
	}
	if resp.RequestID != "req-max" {
		t.Errorf("RequestID = %q, want req-max", resp.RequestID)
	}
	if resp.Metrics.TotalTurns != 10 || resp.Metrics.TotalTools != 5 {
		t.Errorf("Metrics = %+v, want TotalTurns=10 TotalTools=5", resp.Metrics)
	}
	if resp.Metrics.LLMLatency != time.Second || resp.Metrics.ToolLatency != 2*time.Second {
		t.Errorf("Metrics 延迟字段 = %+v, want 1s/2s", resp.Metrics)
	}
	if got := a.lifecycle.Status(); got != StatusFailed {
		t.Errorf("status = %v, want %v", got, StatusFailed)
	}
	if !hookLog.has(HookOnError) {
		t.Error("HookOnError 未触发")
	} else if !errors.Is(hookLog.err, ErrMaxTurnsExceeded) {
		t.Errorf("HookOnError 携带错误 = %v, want ErrMaxTurnsExceeded", hookLog.err)
	}
	select {
	case ev := <-streamCh:
		if ev.Type != StreamEventError || ev.Content != ErrMaxTurnsExceeded.Error() {
			t.Errorf("流事件 = %+v, want StreamEventError/ErrMaxTurnsExceeded", ev)
		}
	default:
		t.Error("未推送错误流事件")
	}
}

// ===== runLoopTurn 阶段单元 =====

// TestResolveToolDefinitions 工具定义解析：缓存优先 / toolkit 回退 / 空路径。
func TestResolveToolDefinitions(t *testing.T) {
	t.Run("capCache 预转换结果优先", func(t *testing.T) {
		a := newSplitAgent(t)
		cached := []llm.ToolDefinition{{Function: llm.FunctionDefinition{Name: "cached"}}}
		a.capCache = &capabilityCache{toolDefinitions: cached}
		got := a.resolveToolDefinitions()
		if len(got) != 1 || got[0].Function.Name != "cached" {
			t.Errorf("resolveToolDefinitions = %+v, want 缓存结果", got)
		}
	})

	t.Run("回退到 capCache.toolkit 现转换", func(t *testing.T) {
		a := newSplitAgent(t)
		reg := tools.NewRegistry()
		if err := reg.Register(splitNoopTool{}); err != nil {
			t.Fatalf("Register: %v", err)
		}
		a.capCache = &capabilityCache{toolkit: reg}
		got := a.resolveToolDefinitions()
		if len(got) != 1 || got[0].Function.Name != "split_noop" {
			t.Errorf("resolveToolDefinitions = %+v, want 1 个 split_noop 定义", got)
		}
	})

	t.Run("无 toolkit 返回空", func(t *testing.T) {
		a := newSplitAgent(t)
		if got := a.resolveToolDefinitions(); len(got) != 0 {
			t.Errorf("resolveToolDefinitions = %+v, want 空", got)
		}
	})
}

// TestInvokeLLM_Sync 非流式路径：成功返回思考与耗时；错误路径 llmSpan 仍闭合。
func TestInvokeLLM_Sync(t *testing.T) {
	t.Run("成功", func(t *testing.T) {
		a := newSplitAgent(t)
		tr := &recordingTracer{}
		thought, latency, err := a.invokeLLM(context.Background(), loopConfig{},
			nil, nil, tr, &NoopSpan{}, 0)
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if thought.Content != "hello" {
			t.Errorf("thought.Content = %q, want hello", thought.Content)
		}
		if latency <= 0 {
			t.Errorf("latency = %v, want > 0", latency)
		}
		assertNoSpanLeak(t, tr)
		if total, llmCalls := tr.countNames("llm.call"); total != 1 || llmCalls != 1 {
			t.Errorf("span 统计 = (%d, %d), want (1, 1)", total, llmCalls)
		}
	})

	t.Run("LLM 错误", func(t *testing.T) {
		a := newReActAgent(ReActConfig{
			Name:     "split-err",
			Model:    llm.NewMockLLM(t).WithError(errors.New("boom")),
			MaxTurns: 3,
		})
		tr := &recordingTracer{}
		_, _, err := a.invokeLLM(context.Background(), loopConfig{},
			nil, nil, tr, &NoopSpan{}, 0)
		if err == nil || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("err = %v, want 包含 boom", err)
		}
		// P1-1 回归：错误路径 llmSpan 必须闭合
		assertNoSpanLeak(t, tr)
	})
}

// TestInvokeLLM_Stream 流式路径：正常产出与空思考包装错误。
func TestInvokeLLM_Stream(t *testing.T) {
	t.Run("正常产出", func(t *testing.T) {
		a := newReActAgent(ReActConfig{
			Name:     "split-stream",
			Model:    llm.NewMockLLM(t).WithResponse("streamed"),
			MaxTurns: 3,
		})
		thought, _, err := a.invokeLLM(context.Background(), loopConfig{stream: true},
			nil, nil, nil, &NoopSpan{}, 0)
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if thought.Content != "streamed" {
			t.Errorf("thought.Content = %q, want streamed", thought.Content)
		}
	})

	t.Run("空思考包装为错误", func(t *testing.T) {
		a := newReActAgent(ReActConfig{
			Name:     "split-stream-err",
			Model:    llm.NewMockLLM(t).WithError(errors.New("upstream down")),
			MaxTurns: 3,
		})
		_, _, err := a.invokeLLM(context.Background(), loopConfig{stream: true},
			nil, nil, nil, &NoopSpan{}, 0)
		if err == nil || !strings.Contains(err.Error(), "stream reasoning failed") {
			t.Fatalf("err = %v, want 包装后的 stream reasoning failed", err)
		}
	})
}

// TestInvokeLLM_PublishesLLMCallEvent 存在订阅者时推送 LLMCall 事件。
func TestInvokeLLM_PublishesLLMCallEvent(t *testing.T) {
	a := newSplitAgent(t)
	pub := &splitEventPublisher{}
	a.capCache = &capabilityCache{eventPublisher: pub}

	if _, _, err := a.invokeLLM(context.Background(), loopConfig{},
		nil, nil, nil, &NoopSpan{}, 1); err != nil {
		t.Fatalf("invokeLLM: %v", err)
	}
	if !pub.has(EventLLMCall) {
		t.Error("LLMCall 事件未推送")
	}
}

// TestRecordLLMUsageAndAudit LLM 调用后置：用量记账 + LLMCall 审计事件。
func TestRecordLLMUsageAndAudit(t *testing.T) {
	a := newSplitAgent(t)
	audit := &splitAuditLogger{}
	rec := &splitMetricsRecorder{}
	a.WithMetrics(rec) // 经能力包装使 getMetricsRecorder 可见（原地修改后 self 即包装器）
	a.capCache = &capabilityCache{auditLogger: audit, model: "mock-model"}

	thought := Thought{
		Content: "hello",
		Usage:   llm.Usage{PromptTokens: 10, CompletionTokens: 1, TotalTokens: 11},
	}
	a.recordLLMUsageAndAudit(context.Background(), 3, thought, 1500*time.Microsecond)

	if n := audit.countAction(auditActionLLMCall); n != 1 {
		t.Fatalf("LLMCall 审计事件数 = %d, want 1", n)
	}
	ev, ok := audit.firstByAction(auditActionLLMCall)
	if !ok {
		t.Fatal("未找到 LLMCall 审计事件")
	}
	if ev.Actor != "split-test" || ev.Resource != "mock-model" || ev.Result != auditResultSuccess {
		t.Errorf("审计事件基础字段 = %+v", ev)
	}
	if ev.Details["turn"] != 3 {
		t.Errorf("Details[turn] = %v, want 3", ev.Details["turn"])
	}
	if ev.Details["prompt_tokens"] != 10 || ev.Details["completion_tokens"] != 1 {
		t.Errorf("Details token 字段 = %v / %v, want 10 / 1",
			ev.Details["prompt_tokens"], ev.Details["completion_tokens"])
	}
	if _, _, tokenUsages := rec.snapshot(); tokenUsages != 1 {
		t.Errorf("RecordTokenUsage 调用次数 = %d, want 1", tokenUsages)
	}
}

// TestFinishTurnBookkeeping turn 收尾记账：hook / 耗时 / 事件，及 needTiming 门控。
func TestFinishTurnBookkeeping(t *testing.T) {
	a := newSplitAgent(t)
	hooks, hookLog := newSplitHooks()
	a.hooks = hooks
	rec := &splitMetricsRecorder{}
	pub := &splitEventPublisher{}
	a.capCache = &capabilityCache{labeledRecorder: rec, eventPublisher: pub}

	// needTiming=true：记录耗时 + 推送事件
	a.finishTurnBookkeeping(2, true, time.Now().Add(-time.Millisecond))
	if !hookLog.has(HookAfterTurn) {
		t.Error("HookAfterTurn 未触发")
	}
	if _, turnsWithAgent, _ := rec.snapshot(); turnsWithAgent != 1 {
		t.Errorf("RecordTurnWithAgent 调用次数 = %d, want 1", turnsWithAgent)
	}
	if !pub.has(EventTurnEnd) {
		t.Error("TurnEnd 事件未推送")
	}

	// needTiming=false：不记录耗时
	a.finishTurnBookkeeping(3, false, time.Time{})
	if _, turnsWithAgent, _ := rec.snapshot(); turnsWithAgent != 1 {
		t.Errorf("needTiming=false 时 RecordTurnWithAgent 次数 = %d, want 保持 1", turnsWithAgent)
	}
}

// TestCompleteWithoutToolCalls 无 tool 调用的完成路径：响应/终态/审计/hook/流事件。
func TestCompleteWithoutToolCalls(t *testing.T) {
	a := newSplitAgent(t)
	hooks, hookLog := newSplitHooks()
	a.hooks = hooks
	audit := &splitAuditLogger{}
	rec := &splitMetricsRecorder{}
	a.capCache = &capabilityCache{auditLogger: audit, labeledRecorder: rec}

	history := []Message{{Role: RoleUser, Content: "问题"}}
	cfg := loopConfig{requestID: "req-done"}
	resp := a.completeWithoutToolCalls(context.Background(), history, cfg, 2,
		Thought{Content: "答案"}, 100*time.Millisecond, 200*time.Millisecond, 3,
		true, time.Now().Add(-time.Millisecond))

	if resp == nil || resp.Content != "答案" {
		t.Fatalf("resp = %+v, want Content=答案", resp)
	}
	if resp.RequestID != "req-done" {
		t.Errorf("RequestID = %q, want req-done", resp.RequestID)
	}
	want := Metrics{
		TotalTurns:  3, // turn+1
		TotalTools:  3,
		LLMLatency:  100 * time.Millisecond,
		ToolLatency: 200 * time.Millisecond,
	}
	if resp.Metrics.TotalTurns != want.TotalTurns || resp.Metrics.TotalTools != want.TotalTools ||
		resp.Metrics.LLMLatency != want.LLMLatency || resp.Metrics.ToolLatency != want.ToolLatency {
		t.Errorf("Metrics = %+v, want %+v（Duration 不断言）", resp.Metrics, want)
	}
	if got := a.lifecycle.Status(); got != StatusCompleted {
		t.Errorf("status = %v, want %v", got, StatusCompleted)
	}
	if !hookLog.has(HookOnComplete) || !hookLog.has(HookAfterTurn) {
		t.Errorf("hook 触发记录 = %v, want 含 OnComplete 与 AfterTurn", hookLog.points)
	}
	if hookLog.resp != resp {
		t.Error("HookOnComplete 携带的响应应与返回值一致")
	}
	if _, turnsWithAgent, _ := rec.snapshot(); turnsWithAgent != 1 {
		t.Errorf("RecordTurnWithAgent 调用次数 = %d, want 1", turnsWithAgent)
	}
	if n := audit.countAction(auditActionAgentStop); n != 1 {
		t.Errorf("AgentStop 审计事件数 = %d, want 1", n)
	} else {
		ev, _ := audit.firstByAction(auditActionAgentStop)
		if ev.Details["turns"] != 3 {
			t.Errorf("AgentStop Details[turns] = %v, want 3", ev.Details["turns"])
		}
	}
}

// TestCompleteWithoutToolCalls_StreamEvent 完成路径流式事件携带响应。
func TestCompleteWithoutToolCalls_StreamEvent(t *testing.T) {
	a := newSplitAgent(t)
	a.capCache = &capabilityCache{}

	streamCh := make(chan StreamEvent, 4)
	cfg := loopConfig{
		requestID: "req-stream-done",
		stream:    true,
		streamCh:  streamCh,
		streamCtx: context.Background(),
	}
	resp := a.completeWithoutToolCalls(context.Background(),
		[]Message{{Role: RoleUser, Content: "问题"}}, cfg, 0,
		Thought{Content: "答案"}, 0, 0, 0, false, time.Time{})

	select {
	case ev := <-streamCh:
		if ev.Type != StreamEventComplete {
			t.Errorf("流事件类型 = %v, want %v", ev.Type, StreamEventComplete)
		}
		if ev.Content != "答案" {
			t.Errorf("流事件 Content = %q, want 答案（原始 thought 内容）", ev.Content)
		}
		if ev.Data != resp {
			t.Error("流事件 Data 应与响应一致")
		}
	default:
		t.Fatal("完成路径未推送流式完成事件")
	}
}

// TestGracefulShutdownOutcome 优雅关闭终态：部分内容/错误/指标/状态/流事件。
func TestGracefulShutdownOutcome(t *testing.T) {
	a := newSplitAgent(t)
	if err := a.lifecycle.SetStatus(StatusRunning); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	streamCh := make(chan StreamEvent, 4)
	cfg := loopConfig{
		requestID: "req-graceful",
		stream:    true,
		streamCh:  streamCh,
		streamCtx: context.Background(),
	}
	resp := a.gracefulShutdownOutcome(cfg, 1, "部分内容",
		50*time.Millisecond, 60*time.Millisecond, 2)

	if resp == nil || resp.Content != "部分内容" {
		t.Fatalf("resp = %+v, want Content=部分内容", resp)
	}
	if !errors.Is(resp.Error, ErrAgentStopped) {
		t.Errorf("resp.Error = %v, want ErrAgentStopped", resp.Error)
	}
	if resp.Metrics.TotalTurns != 2 || resp.Metrics.TotalTools != 2 {
		t.Errorf("Metrics = %+v, want TotalTurns=2 TotalTools=2", resp.Metrics)
	}
	if resp.Metrics.LLMLatency != 50*time.Millisecond || resp.Metrics.ToolLatency != 60*time.Millisecond {
		t.Errorf("Metrics 延迟字段 = %+v, want 50ms/60ms", resp.Metrics)
	}
	if got := a.lifecycle.Status(); got != StatusCancelled {
		t.Errorf("status = %v, want %v", got, StatusCancelled)
	}
	select {
	case ev := <-streamCh:
		if ev.Type != StreamEventError || ev.Content != "graceful shutdown: agent stopped after turn completion" {
			t.Errorf("流事件 = %+v, want StreamEventError/graceful shutdown", ev)
		}
	default:
		t.Error("未推送优雅关闭错误流事件")
	}
}
