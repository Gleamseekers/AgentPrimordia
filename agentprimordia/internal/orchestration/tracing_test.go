package orchestration

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/trace"
)

// mockSpan 用于在测试中记录 span 调用
type mockSpan struct {
	mu         sync.Mutex
	name       string
	kind       trace.SpanKind
	attributes map[string]any
	status     trace.SpanStatus
	statusDesc string
	ended      bool
	parentCtx  trace.SpanContext
}

func (s *mockSpan) SetName(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.name = name
}

func (s *mockSpan) SetAttribute(key string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.attributes == nil {
		s.attributes = make(map[string]any)
	}
	s.attributes[key] = value
}

func (s *mockSpan) SetStatus(status trace.SpanStatus, description string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
	s.statusDesc = description
}

func (s *mockSpan) SpanContext() trace.SpanContext {
	s.mu.Lock()
	defer s.mu.Unlock()
	return trace.SpanContext{
		TraceID: "trace-" + s.name,
		SpanID:  "span-" + s.name,
	}
}

func (s *mockSpan) IsEnded() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ended
}

func (s *mockSpan) End() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ended = true
}

// mockTracer 是测试用的追踪器，记录所有创建的 span
type mockTracer struct {
	mu    sync.Mutex
	spans []*mockSpan
}

func (t *mockTracer) Start(name string, kind trace.SpanKind, opts ...trace.SpanOption) trace.Span {
	cfg := &trace.SpanConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	s := &mockSpan{
		name:      name,
		kind:      kind,
		parentCtx: cfg.ParentContext,
	}

	t.mu.Lock()
	t.spans = append(t.spans, s)
	t.mu.Unlock()

	return s
}

func (t *mockTracer) Spans() []*mockSpan {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]*mockSpan, len(t.spans))
	copy(out, t.spans)
	return out
}

func (t *mockTracer) FindSpan(name string) *mockSpan {
	for _, s := range t.Spans() {
		if s.name == name {
			return s
		}
	}
	return nil
}

// TestTracingConfig_Default 验证默认配置关闭追踪
func TestTracingConfig_Default(t *testing.T) {
	cfg := DefaultTracingConfig()
	if cfg.Enabled {
		t.Errorf("default config should not enable tracing")
	}
	if cfg.Tracer == nil {
		t.Errorf("default config should have a noop tracer")
	}
}

// TestTracingConfig_WithTracer 验证 WithTracer 构造器
func TestTracingConfig_WithTracer(t *testing.T) {
	tr := &mockTracer{}
	cfg := WithTracer(tr)
	if !cfg.Enabled {
		t.Errorf("WithTracer should enable tracing when tracer is non-nil")
	}
	if cfg.Tracer == nil {
		t.Errorf("WithTracer should set tracer")
	}
}

// TestTracingConfig_WithNilTracer 验证 nil tracer 不会启用追踪
func TestTracingConfig_WithNilTracer(t *testing.T) {
	cfg := WithTracer(nil)
	if cfg.Enabled {
		t.Errorf("WithTracer(nil) should not enable tracing")
	}
}

// TestNoopTracer_Start 验证 noop tracer 返回 noop span
func TestNoopTracer_Start(t *testing.T) {
	tr := NewNoopTracer()
	span := tr.Start("test", trace.SpanKindInternal)
	if span == nil {
		t.Fatalf("noop tracer should return a span")
	}
	// 验证所有方法都是 no-op，不 panic
	span.SetAttribute("key", "value")
	span.SetStatus(trace.SpanStatusOK, "")
	span.End()
}

// TestFromAgentTracer_Adapts 验证 agent.Tracer -> orchestration.Tracer 适配
func TestFromAgentTracer_Adapts(t *testing.T) {
	tr := &mockTracer{}
	adapter := FromAgentTracer(tr)
	if adapter == nil {
		t.Fatalf("FromAgentTracer should return a non-nil tracer")
	}

	span := adapter.Start("adapted", trace.SpanKindInternal)
	span.End()

	if tr.FindSpan("adapted") == nil {
		t.Errorf("adapted tracer should produce spans in underlying agent.Tracer")
	}
}

// TestFromAgentTracer_NilReturnsNoop 验证 nil agent.Tracer 返回 noop
func TestFromAgentTracer_NilReturnsNoop(t *testing.T) {
	adapter := FromAgentTracer(nil)
	_, ok := adapter.(*noopTracer)
	if !ok {
		t.Errorf("FromAgentTracer(nil) should return noopTracer")
	}
}

// mockStepExecutor 是测试用 mock，记录 step 执行
type mockStepExecutor struct {
	mu     sync.Mutex
	called int
	err    error
	result *StepResult
}

func (m *mockStepExecutor) Execute(ctx context.Context, step *AgentStep, input map[string]any) *StepResult {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.called++

	if m.result != nil {
		return m.result
	}

	if m.err != nil {
		return &StepResult{
			StepID:    step.ID,
			StepName:  step.Name,
			Status:    StepFailed,
			Error:     m.err,
			StartTime: time.Now(),
			EndTime:   time.Now(),
		}
	}

	return &StepResult{
		StepID:    step.ID,
		StepName:  step.Name,
		Status:    StepCompleted,
		Output:    map[string]any{"content": "ok"},
		StartTime: time.Now(),
		EndTime:   time.Now(),
	}
}

// TestTracingStepExecutor_Success 验证成功路径下 span 状态为 OK
func TestTracingStepExecutor_Success(t *testing.T) {
	tr := &mockTracer{}
	inner := &mockStepExecutor{
		result: &StepResult{
			Status:    StepCompleted,
			Duration:  100 * time.Millisecond,
			Output:    map[string]any{"content": "ok"},
			StartTime: time.Now(),
			EndTime:   time.Now(),
		},
	}

	exec := NewTracingStepExecutor(inner, WithTracer(tr), SequentialMode, "orch-1")
	step := &AgentStep{ID: "step-1", Name: "Step 1"}
	result := exec.Execute(context.Background(), step, nil)

	if result == nil {
		t.Fatalf("result should not be nil")
	}
	if result.Status != StepCompleted {
		t.Errorf("result.Status = %v, want %v", result.Status, StepCompleted)
	}

	span := tr.FindSpan("orchestration.step.step-1")
	if span == nil {
		t.Fatalf("expected span 'orchestration.step.step-1' to be created")
	}

	if span.kind != trace.SpanKindInternal {
		t.Errorf("span kind = %v, want %v", span.kind, trace.SpanKindInternal)
	}
	if span.attributes["orchestration.id"] != "orch-1" {
		t.Errorf("orchestration.id = %v, want orch-1", span.attributes["orchestration.id"])
	}
	if span.attributes["step.id"] != "step-1" {
		t.Errorf("step.id = %v", span.attributes["step.id"])
	}
	if span.attributes["step.status"] != string(StepCompleted) {
		t.Errorf("step.status = %v, want %v", span.attributes["step.status"], StepCompleted)
	}
	if span.status != trace.SpanStatusOK {
		t.Errorf("span status = %v, want OK", span.status)
	}
	if !span.ended {
		t.Errorf("span should be ended")
	}
}

// TestTracingStepExecutor_Failure 验证失败路径下 span 状态为 Error
func TestTracingStepExecutor_Failure(t *testing.T) {
	tr := &mockTracer{}
	inner := &mockStepExecutor{err: errors.New("step failed")}

	exec := NewTracingStepExecutor(inner, WithTracer(tr), SequentialMode, "orch-2")
	step := &AgentStep{ID: "step-2", Name: "Step 2"}
	result := exec.Execute(context.Background(), step, nil)

	if result.Status != StepFailed {
		t.Errorf("result.Status = %v, want StepFailed", result.Status)
	}

	span := tr.FindSpan("orchestration.step.step-2")
	if span == nil {
		t.Fatalf("expected span to be created")
	}
	if span.status != trace.SpanStatusError {
		t.Errorf("span status = %v, want Error", span.status)
	}
	if span.statusDesc != "step failed" {
		t.Errorf("span statusDesc = %q", span.statusDesc)
	}
}

// TestTracingStepExecutor_Disabled 验证追踪关闭时不创建 span
func TestTracingStepExecutor_Disabled(t *testing.T) {
	tr := &mockTracer{}
	inner := &mockStepExecutor{
		result: &StepResult{Status: StepCompleted, Duration: time.Millisecond, Output: map[string]any{}, StartTime: time.Now(), EndTime: time.Now()},
	}

	exec := NewTracingStepExecutor(inner, DefaultTracingConfig(), SequentialMode, "orch-3")
	step := &AgentStep{ID: "step-3", Name: "Step 3"}
	exec.Execute(context.Background(), step, nil)

	if tr.FindSpan("orchestration.step.step-3") != nil {
		t.Errorf("disabled tracing should not create spans")
	}
}

// TestTracingPipeline_Success 验证 pipeline 整体 span
func TestTracingPipeline_Success(t *testing.T) {
	tr := &mockTracer{}
	p := NewPipeline(time.Minute)
	_ = p.AddStage(&Stage{
		Name:    "stage-1",
		Handler: func(ctx context.Context, input string) (string, error) { return "out-1", nil },
	})
	_ = p.AddStage(&Stage{
		Name:    "stage-2",
		Handler: func(ctx context.Context, input string) (string, error) { return "out-2", nil },
	})

	tp := NewTracingPipeline(p, WithTracer(tr), "test-pipeline")
	result, err := tp.Execute(context.Background(), "initial")

	if err != nil {
		t.Errorf("pipeline should succeed: %v", err)
	}
	if result == nil {
		t.Fatalf("result should not be nil")
	}
	if result.Status != PipelineStatusSuccess {
		t.Errorf("result.Status = %v, want Success", result.Status)
	}

	span := tr.FindSpan("orchestration.pipeline.test-pipeline")
	if span == nil {
		t.Fatalf("expected pipeline span")
	}
	if span.attributes["pipeline.name"] != "test-pipeline" {
		t.Errorf("pipeline.name = %v", span.attributes["pipeline.name"])
	}
	if span.attributes["pipeline.stages"] != 2 {
		t.Errorf("pipeline.stages = %v, want 2", span.attributes["pipeline.stages"])
	}
	if span.status != trace.SpanStatusOK {
		t.Errorf("span status = %v, want OK", span.status)
	}
}

// TestTracingPipeline_DefaultName 验证 pipeline 名为空时使用默认名
func TestTracingPipeline_DefaultName(t *testing.T) {
	tr := &mockTracer{}
	p := NewPipeline(time.Minute)
	_ = p.AddStage(&Stage{
		Name:    "stage-1",
		Handler: func(ctx context.Context, input string) (string, error) { return "ok", nil },
	})

	tp := NewTracingPipeline(p, WithTracer(tr), "")
	_, _ = tp.Execute(context.Background(), "in")

	if tr.FindSpan("orchestration.pipeline.pipeline") == nil {
		t.Errorf("empty name should fall back to 'pipeline'")
	}
}

// TestTracingPipeline_Failure 验证失败 pipeline 的 span 状态为 Error
func TestTracingPipeline_Failure(t *testing.T) {
	tr := &mockTracer{}
	p := NewPipeline(time.Minute)
	_ = p.AddStage(&Stage{
		Name:    "failing-stage",
		Handler: func(ctx context.Context, input string) (string, error) { return "", errors.New("boom") },
	})

	tp := NewTracingPipeline(p, WithTracer(tr), "failing")
	_, _ = tp.Execute(context.Background(), "in")

	span := tr.FindSpan("orchestration.pipeline.failing")
	if span == nil {
		t.Fatalf("expected span")
	}
	if span.status != trace.SpanStatusError {
		t.Errorf("span status = %v, want Error", span.status)
	}
	// Pipeline 内部会把错误包装为 "stage '...' failed: boom"
	if !contains(span.statusDesc, "boom") {
		t.Errorf("span statusDesc = %q, want to contain 'boom'", span.statusDesc)
	}
}

// contains 简单的子串检查辅助函数
func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestTracingStepExecutor_Attributes 验证完整属性被注入
func TestTracingStepExecutor_Attributes(t *testing.T) {
	tr := &mockTracer{}
	inner := &mockStepExecutor{
		result: &StepResult{
			Status:    StepCompleted,
			Duration:  250 * time.Millisecond,
			Output:    map[string]any{"content": "ok"},
			StartTime: time.Now(),
			EndTime:   time.Now(),
		},
	}

	exec := NewTracingStepExecutor(inner, WithTracer(tr), ParallelMode, "orch-attrs")
	step := &AgentStep{
		ID:       "step-attrs",
		Name:     "Detailed Step",
		Priority: 5,
	}
	exec.Execute(context.Background(), step, nil)

	span := tr.FindSpan("orchestration.step.step-attrs")
	if span == nil {
		t.Fatalf("expected span")
	}

	wantAttrs := map[string]any{
		"orchestration.id":   "orch-attrs",
		"orchestration.mode": string(ParallelMode),
		"step.id":            "step-attrs",
		"step.name":          "Detailed Step",
		"step.priority":      5,
		"step.status":        string(StepCompleted),
		"step.duration_ms":   int64(250),
	}
	for k, want := range wantAttrs {
		if got := span.attributes[k]; got != want {
			t.Errorf("attribute[%s] = %v (%T), want %v (%T)", k, got, got, want, want)
		}
	}
}

// TestTracingPipeline_Delegation 验证 TracingPipeline 完整代理原始 Pipeline
func TestTracingPipeline_Delegation(t *testing.T) {
	tr := &mockTracer{}
	p := NewPipeline(time.Minute)
	_ = p.AddStage(&Stage{
		Name:    "s1",
		Handler: func(ctx context.Context, input string) (string, error) { return input + "-s1", nil },
	})
	_ = p.AddStage(&Stage{
		Name:    "s2",
		Handler: func(ctx context.Context, input string) (string, error) { return input + "-s2", nil },
	})

	tp := NewTracingPipeline(p, WithTracer(tr), "delegation")

	// 验证所有代理方法工作
	if tp.StageCount() != 2 {
		t.Errorf("StageCount = %d, want 2", tp.StageCount())
	}
	if len(tp.GetStages()) != 2 {
		t.Errorf("GetStages length = %d, want 2", len(tp.GetStages()))
	}
	if err := tp.AddStage(&Stage{Name: "s3", Handler: func(ctx context.Context, input string) (string, error) { return input, nil }}); err != nil {
		t.Errorf("AddStage failed: %v", err)
	}
	if tp.StageCount() != 3 {
		t.Errorf("StageCount after Add = %d, want 3", tp.StageCount())
	}

	// 验证 Execute 工作
	result, err := tp.Execute(context.Background(), "init")
	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}
	if result.FinalOutput != "init-s1-s2" {
		t.Errorf("FinalOutput = %q, want %q", result.FinalOutput, "init-s1-s2")
	}
}

// TestConfigureOrchestratorTracing_NoOp 验证配置函数当前为 no-op（接口留待后续注入）
func TestConfigureOrchestratorTracing_NoOp(t *testing.T) {
	o := NewOrchestrator(OrchestratorConfig{Name: "test"})
	// 当前实现为 no-op；调用不应 panic
	ConfigureOrchestratorTracing(o, WithTracer(&mockTracer{}))
}

// 编译期类型断言
var (
	_ trace.Span = (*mockSpan)(nil)
	_ Tracer     = (*mockTracer)(nil)
	_ Tracer     = (*noopTracer)(nil)
	_ Tracer     = tracerAdapter{}
)

// 避免 agent 包 unused 警告（如果未来需要 agent.Tracer）
var _ = agent.NewNoopTracer
