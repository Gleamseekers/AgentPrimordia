package agent

import (
	"context"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/observability"
)

// TestObservability_ClosedLoop 验证 v3.5-4 全链路闭环：
// 单次 agent.Run 产生的 Span/审计事件/指标均以同一 trace_id 关联，
// 通过 CorrelationStore.Get(traceID) 可一次还原整次请求。
func TestObservability_ClosedLoop(t *testing.T) {
	mockProvider := &outputGuardMockProvider{content: "完成"}
	corr := observability.NewCorrelationStore()
	audit := &memoryAuditLogger{}

	ag := newReActAgent(ReActConfig{
		Name:     "obs-agent",
		Model:    mockProvider,
		MaxTurns: 3,
	}).WithAuditLogger(audit).WithObservability(corr)

	resp, err := ag.Run(context.Background(), Message{Role: RoleUser, Content: "测试"})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	// 关联键：无 Tracer 时回退到 request_id
	key := resp.RequestID
	if key == "" {
		t.Fatal("请求应产生 request_id 作为关联键")
	}

	if corr.Len() != 1 {
		t.Fatalf("CorrelationStore.Len = %d, want 1", corr.Len())
	}

	rt := corr.Get(key)
	if rt == nil {
		t.Fatal("Get(traceID) 应为非空（单请求全链路视图）")
	}

	// ===== Trace 域 =====
	if corr.SpanCount(key) < 1 {
		t.Errorf("Span 数 = %d, want ≥1（agent.run）", corr.SpanCount(key))
	}
	if rt.Spans[0].Name != "agent.run" {
		t.Errorf("根 span 名 = %q, want agent.run", rt.Spans[0].Name)
	}

	// ===== Audit 域 =====
	// 必须覆盖 agent.start / llm.call / agent.stop 且全部携带关联键
	actionSet := map[string]bool{}
	for _, ev := range rt.AuditEvents {
		if ev.TraceID != key {
			t.Errorf("审计事件 TraceID = %q, want %q（action=%s）", ev.TraceID, key, ev.Action)
		}
		actionSet[ev.Action] = true
	}
	for _, want := range []string{auditActionAgentStart, auditActionLLMCall, auditActionAgentStop} {
		if !actionSet[want] {
			t.Errorf("审计域缺少 %s 事件", want)
		}
	}

	// ===== Metrics 域 =====
	if rt.Metrics.LLMCalls < 1 {
		t.Errorf("LLMCalls = %d, want ≥1", rt.Metrics.LLMCalls)
	}
	if rt.Metrics.Turns < 1 {
		t.Errorf("Turns = %d, want ≥1", rt.Metrics.Turns)
	}

	// ===== 闭合 =====
	if rt.EndedAt.IsZero() {
		t.Error("请求未闭合（EndedAt 为零值）")
	}
	if rt.AgentName != "obs-agent" {
		t.Errorf("AgentName = %q, want obs-agent", rt.AgentName)
	}
}

// TestObservability_WithTracer 验证配置 Tracer 时以真实 trace_id 关联。
func TestObservability_WithTracer(t *testing.T) {
	mockProvider := &outputGuardMockProvider{content: "完成"}
	corr := observability.NewCorrelationStore()
	tracer := NewLoggingTracer()

	ag := newReActAgent(ReActConfig{
		Name:     "obs-trace-agent",
		Model:    mockProvider,
		MaxTurns: 1,
	}).WithTracer(tracer).WithObservability(corr)

	_, err := ag.Run(context.Background(), Message{Role: RoleUser, Content: "hi"})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	traces := corr.List(1)
	if len(traces) != 1 {
		t.Fatalf("List = %d, want 1", len(traces))
	}
	rt := traces[0]
	if rt.TraceID == "" {
		t.Fatal("配置 Tracer 后 trace_id 不应为空")
	}
	if len(rt.Spans) == 0 {
		t.Fatal("应记录 agent.run span")
	}
	for _, sp := range rt.Spans {
		if sp.TraceID != rt.TraceID {
			t.Errorf("span.TraceID = %q, want %q", sp.TraceID, rt.TraceID)
		}
	}
	for _, ev := range rt.AuditEvents {
		if ev.TraceID != rt.TraceID {
			t.Errorf("审计事件 TraceID = %q, want %q", ev.TraceID, rt.TraceID)
		}
	}
	if rt.Spans[0].SpanID == "" {
		t.Error("配置 Tracer 后 root span 应携带 span_id")
	}
}

// TestObservability_MultiRequests 验证多次请求各自独立可回溯。
func TestObservability_MultiRequests(t *testing.T) {
	corr := observability.NewCorrelationStore()
	ag := newReActAgent(ReActConfig{
		Name:     "obs-multi",
		Model:    &outputGuardMockProvider{content: "done"},
		MaxTurns: 1,
	}).WithObservability(corr)

	for i := 0; i < 3; i++ {
		resp, err := ag.Run(context.Background(), Message{Role: RoleUser, Content: "t"})
		if err != nil {
			t.Fatalf("Run#%d error: %v", i, err)
		}
		if corr.Get(resp.RequestID) == nil {
			t.Errorf("请求 %q 应可回溯", resp.RequestID)
		}
	}
	if corr.Len() != 3 {
		t.Errorf("Len = %d, want 3", corr.Len())
	}
	byAgent := corr.ListByAgent("obs-multi", 0)
	if len(byAgent) != 3 {
		t.Errorf("ListByAgent = %d, want 3", len(byAgent))
	}
}
