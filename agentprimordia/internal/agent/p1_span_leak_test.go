// p1_span_leak_test.go — P1-1：runLoop 早退路径 trace span 泄漏回归测试
//
// 评估报告 §4.2 P1-1：runLoop 在 checkBudgetExceeded / 记忆 fast-path /
// LLM 错误 / guardrail 拦截等早退路径上直接 return，创建好的 turnSpan 与
// llmSpan 从未 End()，导致 trace 永久挂起（观测端看到永不闭合的 span）。
//
// 本文件用 recordingTracer 记录每个 span 的闭合状态，验证所有早退路径
// 下 span 都被闭合。
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/hitl"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/memory"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/tools"
)

// recordingSpan 记录自身是否已闭合的 Span 实现（测试专用）。
type recordingSpan struct {
	name  string
	ended atomic.Bool
}

func (s *recordingSpan) SetName(string)               {}
func (s *recordingSpan) SetAttribute(string, any)     {}
func (s *recordingSpan) SetStatus(SpanStatus, string) {}
func (s *recordingSpan) SpanContext() SpanContext     { return SpanContext{} }
func (s *recordingSpan) IsEnded() bool                { return s.ended.Load() }
func (s *recordingSpan) End()                         { s.ended.Store(true) }

// recordingTracer 记录所有 Start() 产生的 span，便于事后检查泄漏。
type recordingTracer struct {
	mu    sync.Mutex
	spans []*recordingSpan
}

func (t *recordingTracer) Start(name string, _ SpanKind, _ ...SpanOption) Span {
	s := &recordingSpan{name: name}
	t.mu.Lock()
	t.spans = append(t.spans, s)
	t.mu.Unlock()
	return s
}

// unended 返回所有未闭合的 span 名称（空切片表示无泄漏）。
func (t *recordingTracer) unended() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]string, 0, 4)
	for _, s := range t.spans {
		if !s.ended.Load() {
			out = append(out, s.name)
		}
	}
	return out
}

// countNames 返回 span 总数与指定前缀的 span 数。
func (t *recordingTracer) countNames(prefix string) (total, matched int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	total = len(t.spans)
	for _, s := range t.spans {
		if strings.HasPrefix(s.name, prefix) {
			matched++
		}
	}
	return total, matched
}

// assertNoSpanLeak 校验 tracer 中不存在未闭合的 span。
func assertNoSpanLeak(t *testing.T, tr *recordingTracer) {
	t.Helper()
	if un := tr.unended(); len(un) > 0 {
		t.Errorf("trace span 泄漏（未 End()）：%v", un)
	}
}

// solvedMemoryStore 预置一条"已解决"记忆，用于触发跨任务记忆 fast-path。
type solvedMemoryStore struct {
	mu       sync.Mutex
	episodes []*memory.Episode
}

func (s *solvedMemoryStore) Add(_ context.Context, ep *memory.Episode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.episodes = append(s.episodes, ep)
	return nil
}

func (s *solvedMemoryStore) UpdateSummary(_ context.Context, _, _, _ string) error { return nil }

func (s *solvedMemoryStore) Search(_ context.Context, _ string, _ *memory.SearchOptions) ([]*memory.Episode, error) {
	return []*memory.Episode{{
		ID:       "solved-1",
		Content:  "已解决的答案",
		Metadata: map[string]string{"solved": "true"},
	}}, nil
}

// p1NoopTool 无副作用占位工具。
type p1NoopTool struct{ name string }

func (n *p1NoopTool) Name() string        { return n.name }
func (n *p1NoopTool) Description() string { return "noop" }
func (n *p1NoopTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (n *p1NoopTool) Execute(_ context.Context, _ json.RawMessage) (*tools.Result, error) {
	return tools.NewResult("noop done"), nil
}

// newP1ToolRegistry 构造含无副作用工具的注册表。
func newP1ToolRegistry(names ...string) *tools.Registry {
	reg := tools.NewRegistry()
	for _, name := range names {
		if err := reg.Register(&p1NoopTool{name: name}); err != nil {
			panic(err)
		}
	}
	return reg
}

// newP1Agent 构造带 recordingTracer 的 Agent。
// 返回 *CapabilityAgent：其 With* 方法为原地修改，后续追加能力不会
// 覆盖前面已注入的能力（ReActAgent.With* 会重建包装器导致能力丢失）。
func newP1Agent(t *testing.T, name string, model llm.Provider, opts ...Option) (*CapabilityAgent, *recordingTracer) {
	t.Helper()
	tr := &recordingTracer{}
	base := []Option{WithMaxTurns(5), WithTracer(tr)}
	cap, err := NewAgent(name, "you are helpful", model, append(base, opts...)...)
	if err != nil {
		t.Fatalf("NewAgent(%s): %v", name, err)
	}
	return cap, tr
}

// assertTracerActive 防止测试空跑：tracer 必须真的记录到 span。
func assertTracerActive(t *testing.T, tr *recordingTracer, minSpans int) {
	t.Helper()
	if total, _ := tr.countNames(""); total < minSpans {
		t.Errorf("tracer 仅记录 %d 个 span（期望 >= %d）——tracer 未生效，测试可能空跑", total, minSpans)
	}
}

// TestRunLoop_BudgetExceeded_ClosesTurnSpan 预算超限早退路径：
// 第二轮 checkBudgetExceeded 命中时 turnSpan 必须闭合。
func TestRunLoop_BudgetExceeded_ClosesTurnSpan(t *testing.T) {
	mock := llm.NewMockLLM(t)
	mock.WithToolResponse([]llm.FunctionCall{{ID: "c1", Name: "p1_noop", Arguments: "{}"}})
	mock.WithResponse("done")

	// 单价巨大 + 预算极小：第一轮 usage 记录后第二轮必然超限
	pricing := map[string]llm.ModelPricing{
		"mock-model": {PromptPricePer1M: 1e9, CompletionPricePer1M: 1e9},
	}
	ct := NewCostTracker(pricing, &BudgetConfig{MaxTotalCostUSD: 1e-9})

	a, tr := newP1Agent(t, "span-budget", mock,
		WithCostTracker(ct), WithToolkit(newP1ToolRegistry("p1_noop")))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if _, err := a.Run(ctx, UserMessage("do it")); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("期望 ErrBudgetExceeded，实际: %v", err)
	}
	assertNoSpanLeak(t, tr)
}

// TestRunLoop_LLMError_ClosesTurnAndLLMSpan LLM 错误早退路径：
// turnSpan 与 llmSpan 都必须闭合（修复前两者均泄漏）。
func TestRunLoop_LLMError_ClosesTurnAndLLMSpan(t *testing.T) {
	mock := llm.NewMockLLM(t).WithError(errors.New("boom"))

	a, tr := newP1Agent(t, "span-llmerr", mock)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if _, err := a.Run(ctx, UserMessage("go")); err == nil {
		t.Fatal("期望 LLM 错误，实际成功")
	}
	assertNoSpanLeak(t, tr)
	assertTracerActive(t, tr, 2)
}

// TestRunLoop_MemoryFastPath_ClosesTurnSpan 记忆 fast-path 早退路径：
// 命中已解任务直接返回时 turnSpan 必须闭合。
func TestRunLoop_MemoryFastPath_ClosesTurnSpan(t *testing.T) {
	mock := llm.NewMockLLM(t).WithResponse("unused")

	a, tr := newP1Agent(t, "span-fastpath", mock,
		WithMemory(&solvedMemoryStore{}))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := a.Run(ctx, UserMessage("同一个问题"))
	if err != nil {
		t.Fatalf("fast-path 不应出错: %v", err)
	}
	if resp == nil || resp.Content != "已解决的答案" {
		t.Fatalf("应命中已解答案，实际: %+v", resp)
	}
	assertNoSpanLeak(t, tr)

	// 必须真的走到 fast-path（tracer 记录到 turn span 才说明不是空跑）
	if total, _ := tr.countNames("turn."); total == 0 {
		t.Error("tracer 未记录到任何 span——fast-path 场景 tracer 未生效")
	}
}

// TestRunLoop_OutputGuardBlocked_ClosesTurnSpan 输出端护栏拦截早退路径：
// guardrailSanitizeOutput 返回 ErrOutputBlocked 时 turnSpan 必须闭合。
func TestRunLoop_OutputGuardBlocked_ClosesTurnSpan(t *testing.T) {
	mock := llm.NewMockLLM(t).WithResponse("敏感内容")

	// CapabilityAgent.With* 为原地修改，不会覆盖其它能力
	a, tr := newP1Agent(t, "span-guard", mock)
	a.WithOutputGuard(func(string) (string, bool, error) {
		return "", true, nil // blocked
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if _, err := a.Run(ctx, UserMessage("go")); !errors.Is(err, ErrOutputBlocked) {
		t.Fatalf("期望 ErrOutputBlocked，实际: %v", err)
	}
	assertNoSpanLeak(t, tr)
	assertTracerActive(t, tr, 2)
}

// TestRunLoop_NormalCompletion_ClosesAllSpans 正常完成路径：
// agent.run / turn.0 / llm.call 三个 span 全部闭合。
func TestRunLoop_NormalCompletion_ClosesAllSpans(t *testing.T) {
	mock := llm.NewMockLLM(t).WithResponse("hello")

	a, tr := newP1Agent(t, "span-ok", mock)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if _, err := a.Run(ctx, UserMessage("hi")); err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertNoSpanLeak(t, tr)
	assertTracerActive(t, tr, 3)

	if total, turns := tr.countNames("turn."); total != 3 || turns != 1 {
		t.Errorf("span 统计 = (总数 %d, turn %d), want (3, 1)（agent.run + turn.0 + llm.call）", total, turns)
	}
}

// TestRunLoop_HITLReject_ClosesTurnSpan HITL 拒绝路径（串行模式）：
// 工具被跳过后 Agent 继续完成，turnSpan 不泄漏。
func TestRunLoop_HITLReject_ClosesTurnSpan(t *testing.T) {
	mock := llm.NewMockLLM(t)
	mock.WithToolResponse([]llm.FunctionCall{{ID: "c1", Name: "p1_noop", Arguments: "{}"}})
	mock.WithResponse("skipped by human")

	humanChan := make(chan *hitl.HumanResponse, 1)
	humanChan <- &hitl.HumanResponse{Approved: false}
	hitlCfg := HITLConfig{
		InterruptPoints: []hitl.InterruptPoint{{Type: hitl.InterruptToolConfirm, ToolName: ""}},
		HumanInputChan:  humanChan,
	}

	a, tr := newP1Agent(t, "span-hitl", mock,
		WithHITL(&hitlCfg), WithToolkit(newP1ToolRegistry("p1_noop")))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if _, err := a.Run(ctx, UserMessage("do it")); err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertNoSpanLeak(t, tr)
	assertTracerActive(t, tr, 2)
}

// TestRunLoop_MaxTurnsExceeded_ClosesTurnSpan 超出最大轮次路径：
// 循环耗尽后返回 ErrMaxTurnsExceeded，每个 turnSpan 必须闭合。
func TestRunLoop_MaxTurnsExceeded_ClosesTurnSpan(t *testing.T) {
	mock := llm.NewMockLLM(t)
	// 永远返回 tool 调用 → 打满 MaxTurns
	for i := 0; i < 10; i++ {
		mock.WithToolResponse([]llm.FunctionCall{{ID: "c1", Name: "p1_noop", Arguments: "{}"}})
	}

	a, tr := newP1Agent(t, "span-maxturns", mock,
		WithToolkit(newP1ToolRegistry("p1_noop")))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if _, err := a.Run(ctx, UserMessage("loop forever")); !errors.Is(err, ErrMaxTurnsExceeded) {
		t.Fatalf("期望 ErrMaxTurnsExceeded，实际: %v", err)
	}
	assertNoSpanLeak(t, tr)
	assertTracerActive(t, tr, 11)

	if _, turns := tr.countNames("turn."); turns != 5 {
		t.Errorf("turn span 数量 = %d, want 5（MaxTurns=5）", turns)
	}
}

// TestRunLoop_GracefulShutdown_ClosesTurnSpan 优雅关闭路径：
// turn 完成后退出循环，最后一个 turnSpan 必须闭合。
func TestRunLoop_GracefulShutdown_ClosesTurnSpan(t *testing.T) {
	mock := llm.NewMockLLM(t)
	for i := 0; i < 10; i++ {
		mock.WithToolResponse([]llm.FunctionCall{{ID: "c1", Name: "p1_noop", Arguments: "{}"}})
	}

	a, tr := newP1Agent(t, "span-graceful", mock,
		WithToolkit(newP1ToolRegistry("p1_noop")))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 第一轮结束后触发优雅关闭
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = a.GracefulShutdown(context.Background())
	}()

	_, _ = a.Run(ctx, UserMessage("loop"))
	assertNoSpanLeak(t, tr)
}
