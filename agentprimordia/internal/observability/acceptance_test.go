package observability_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/observability"
	obsotel "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/observability/export/otel"
)

// ===== 可观测性验收测试 A1–A5 =====
//
// 对应规格 §5.4 断言：
//   A1: trace 完整性——span 覆盖所有 turn/tool
//   A2: 审计关联——trace_id → audit events
//   A3: 告警延迟——评估 < 5s
//   A4: Prometheus 格式兼容
//   A5: 仪表盘延迟——响应 < 1s

// TestA1_TraceCompleteness 验证 span 覆盖所有 turn/tool 调用。
//
// 场景：一次 agent 运行产生 N 个 turn + M 个 tool 调用，
// 每个 turn/tool 都应记录对应的 span。
func TestA1_TraceCompleteness(t *testing.T) {
	store := observability.NewCorrelationStore()
	traceID := "acceptance-trace-a1"
	store.Start(traceID, "agent-accept", "sess-a1")

	// 模拟 3 个 turn，每个 turn 有 1 次 LLM 调用 + 2 次 tool 调用
	turns := 3
	toolsPerTurn := 2
	for i := 0; i < turns; i++ {
		store.RecordTurn(traceID)
		store.AddSpan(traceID, observability.SpanRecord{
			Name:   "agent.turn",
			Kind:   "internal",
			SpanID: "turn-" + string(rune('0'+i)),
		})
		store.RecordLLM(traceID, 100*time.Millisecond, 50, 100, 0.001)
		store.AddSpan(traceID, observability.SpanRecord{
			Name:   "llm.call",
			Kind:   "client",
			SpanID: "llm-" + string(rune('0'+i)),
		})
		for j := 0; j < toolsPerTurn; j++ {
			store.RecordTool(traceID, 50*time.Millisecond)
			store.AddSpan(traceID, observability.SpanRecord{
				Name:   "tool.call",
				Kind:   "client",
				SpanID: "tool-" + string(rune('0'+i)) + string(rune('0'+j)),
			})
		}
	}
	store.End(traceID)

	// 断言：trace 完整性
	rt := store.Get(traceID)
	if rt == nil {
		t.Fatal("trace not found")
	}

	expectedTurns := turns
	if rt.Metrics.Turns != expectedTurns {
		t.Errorf("turns = %d, want %d", rt.Metrics.Turns, expectedTurns)
	}

	expectedLLMCalls := turns
	if rt.Metrics.LLMCalls != expectedLLMCalls {
		t.Errorf("llm_calls = %d, want %d", rt.Metrics.LLMCalls, expectedLLMCalls)
	}

	expectedToolCalls := turns * toolsPerTurn
	if rt.Metrics.ToolCalls != expectedToolCalls {
		t.Errorf("tool_calls = %d, want %d", rt.Metrics.ToolCalls, expectedToolCalls)
	}

	// 每个 turn + llm + tool 都应有对应 span
	expectedSpans := turns + expectedLLMCalls + expectedToolCalls
	if len(rt.Spans) != expectedSpans {
		t.Errorf("spans = %d, want %d (trace 完整性：每个 turn/llm/tool 对应一个 span)", len(rt.Spans), expectedSpans)
	}

	// 验证 span 名称覆盖
	spanNames := make(map[string]int)
	for _, sp := range rt.Spans {
		spanNames[sp.Name]++
	}
	if spanNames["agent.turn"] != turns {
		t.Errorf("agent.turn spans = %d, want %d", spanNames["agent.turn"], turns)
	}
	if spanNames["llm.call"] != expectedLLMCalls {
		t.Errorf("llm.call spans = %d, want %d", spanNames["llm.call"], expectedLLMCalls)
	}
	if spanNames["tool.call"] != expectedToolCalls {
		t.Errorf("tool.call spans = %d, want %d", spanNames["tool.call"], expectedToolCalls)
	}
}

// TestA2_AuditCorrelation 验证 trace_id → audit events 关联。
//
// 场景：一次 agent 运行产生的全部审计事件都可通过 trace_id 回溯。
func TestA2_AuditCorrelation(t *testing.T) {
	store := observability.NewCorrelationStore()
	traceID := "acceptance-trace-a2"
	store.Start(traceID, "agent-accept", "sess-a2")

	// 模拟多个审计事件
	auditActions := []string{
		"agent.start",
		"llm.call",
		"tool.call",
		"tool.call",
		"agent.end",
	}
	for _, action := range auditActions {
		store.AddAuditEvent(traceID, observability.AuditEvent{
			Action: action,
			Result: "success",
		})
	}
	store.End(traceID)

	// 断言：审计关联
	rt := store.Get(traceID)
	if rt == nil {
		t.Fatal("trace not found")
	}

	if len(rt.AuditEvents) != len(auditActions) {
		t.Errorf("audit events = %d, want %d", len(rt.AuditEvents), len(auditActions))
	}

	// 每个审计事件都应关联到同一个 trace_id
	for i, ev := range rt.AuditEvents {
		if ev.TraceID != traceID {
			t.Errorf("audit[%d].trace_id = %q, want %q", i, ev.TraceID, traceID)
		}
	}

	// 验证审计动作完整覆盖
	actionSet := make(map[string]int)
	for _, ev := range rt.AuditEvents {
		actionSet[ev.Action]++
	}
	if actionSet["agent.start"] != 1 || actionSet["agent.end"] != 1 {
		t.Error("缺少 agent.start 或 agent.end 审计事件")
	}
	if actionSet["tool.call"] != 2 {
		t.Errorf("tool.call audit count = %d, want 2", actionSet["tool.call"])
	}
}

// TestA3_AlertLatency 验证告警评估延迟 < 5s。
//
// 场景：注册多条规则，评估耗时应在 5 秒以内。
func TestA3_AlertLatency(t *testing.T) {
	store := observability.NewCorrelationStore()
	// 创建大量请求数据模拟真实场景
	for i := 0; i < 100; i++ {
		traceID := "latency-trace-" + string(rune(i))
		store.Start(traceID, "agent-lat", "sess-lat")
		for j := 0; j < 5; j++ {
			store.AddSpan(traceID, observability.SpanRecord{Name: "span", SpanID: "s"})
		}
		store.End(traceID)
	}

	engine := observability.NewAlertEngine(store)
	// 注册 10 条规则
	for i := 0; i < 10; i++ {
		engine.RegisterRule(observability.NewThresholdAlertRule(observability.ThresholdAlertConfig{
			Name:      "latency_rule",
			Threshold: 50,
			Severity:  observability.SeverityWarning,
			MetricFn: func(store *observability.CorrelationStore) float64 {
				return float64(store.Len())
			},
		}))
	}

	// 测量评估耗时
	start := time.Now()
	_ = engine.Evaluate()
	elapsed := time.Since(start)

	// 断言：告警延迟 < 5s
	if elapsed >= 5*time.Second {
		t.Errorf("告警评估耗时 %v，超过 5s 阈值", elapsed)
	}
}

// TestA4_PrometheusFormatCompat 验证 Prometheus 格式兼容。
//
// 场景：MetricExporter 导出的文本应包含标准 Prometheus TYPE/指标行。
func TestA4_PrometheusFormatCompat(t *testing.T) {
	exporter := obsotel.NewMetricExporter()

	// 记录各类指标
	exporter.RecordCounter("ap_llm_total_calls", 10, map[string]string{"provider": "openai"})
	exporter.RecordGauge("ap_active_agents", 3, map[string]string{})
	exporter.RecordHistogram("ap_span_duration_ms", 42.0,
		map[string]string{"name": "llm.call"},
		[]float64{1, 5, 10, 50, 100, 500, 1000, 5000})

	output := exporter.ExportPrometheus()

	// 断言 A4-a：包含 counter TYPE 声明
	if !strings.Contains(output, "# TYPE ap_llm_total_calls counter") {
		t.Error("Prometheus 输出缺少 counter TYPE 声明")
	}

	// 断言 A4-b：包含 gauge TYPE 声明
	if !strings.Contains(output, "# TYPE ap_active_agents gauge") {
		t.Error("Prometheus 输出缺少 gauge TYPE 声明")
	}

	// 断言 A4-c：包含 histogram TYPE 声明
	if !strings.Contains(output, "# TYPE ap_span_duration_ms histogram") {
		t.Error("Prometheus 输出缺少 histogram TYPE 声明")
	}

	// 断言 A4-d：counter 行格式正确（name{labels} value）
	if !strings.Contains(output, `ap_llm_total_calls{provider="openai"} 10`) {
		t.Errorf("counter 行格式不正确，输出:\n%s", output)
	}

	// 断言 A4-e：histogram 包含 bucket/sum/count 行
	if !strings.Contains(output, "ap_span_duration_ms_bucket") {
		t.Error("histogram 缺少 bucket 行")
	}
	if !strings.Contains(output, "ap_span_duration_ms_sum") {
		t.Error("histogram 缺少 sum 行")
	}
	if !strings.Contains(output, "ap_span_duration_ms_count") {
		t.Error("histogram 缺少 count 行")
	}

	// 断言 A4-f：+Inf bucket 存在
	if !strings.Contains(output, `le="+Inf"`) {
		t.Error("histogram 缺少 +Inf bucket")
	}
}

// TestA5_DashboardLatency 验证仪表盘 API 响应延迟 < 1s。
//
// 场景：在有数据的 store 上请求 summary 和 alerts，响应时间应 < 1s。
func TestA5_DashboardLatency(t *testing.T) {
	store := observability.NewCorrelationStore()
	// 创建 50 个请求模拟真实数据量
	for i := 0; i < 50; i++ {
		traceID := "dash-latency-" + string(rune(i))
		store.Start(traceID, "agent-dash", "sess-dash")
		for j := 0; j < 3; j++ {
			store.AddSpan(traceID, observability.SpanRecord{Name: "span", SpanID: "s"})
		}
		store.AddAuditEvent(traceID, observability.AuditEvent{Action: "test", Result: "ok"})
		store.End(traceID)
	}

	engine := observability.NewAlertEngine(store)
	engine.RegisterRule(observability.NewThresholdAlertRule(observability.ThresholdAlertConfig{
		Name:      "dashboard_test_rule",
		Threshold: 10,
		Severity:  observability.SeverityInfo,
		MetricFn: func(store *observability.CorrelationStore) float64 {
			return float64(store.Len())
		},
	}))

	h := observability.DashboardHandler(store, engine)

	// 测试 /dashboard/summary 延迟
	start := time.Now()
	req := httptest.NewRequest(http.MethodGet, "/dashboard/summary", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	summaryElapsed := time.Since(start)

	if rec.Code != http.StatusOK {
		t.Fatalf("/dashboard/summary status = %d, want 200", rec.Code)
	}
	if summaryElapsed >= 1*time.Second {
		t.Errorf("/dashboard/summary 响应耗时 %v，超过 1s 阈值", summaryElapsed)
	}

	// 验证响应体包含预期字段
	var summary map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &summary); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	for _, key := range []string{"total_traces", "total_spans", "total_audits", "agents"} {
		if _, ok := summary[key]; !ok {
			t.Errorf("summary 缺少字段 %q", key)
		}
	}

	// 测试 /dashboard/alerts 延迟
	start = time.Now()
	req = httptest.NewRequest(http.MethodGet, "/dashboard/alerts", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	alertsElapsed := time.Since(start)

	if rec.Code != http.StatusOK {
		t.Fatalf("/dashboard/alerts status = %d, want 200", rec.Code)
	}
	if alertsElapsed >= 1*time.Second {
		t.Errorf("/dashboard/alerts 响应耗时 %v，超过 1s 阈值", alertsElapsed)
	}
}
