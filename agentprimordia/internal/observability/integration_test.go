package observability

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// avgLatencyMs 计算 CorrelationStore 中所有 span 的平均耗时（毫秒）。
// 无 span 时返回 0。
func avgLatencyMs(store *CorrelationStore) float64 {
	traces := store.List(0)
	var totalMs int64
	var count int
	for _, rt := range traces {
		for _, sp := range rt.Spans {
			totalMs += sp.DurationMs
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return float64(totalMs) / float64(count)
}

// setupComponents 创建测试所需的 CorrelationStore、AlertEngine 和 DashboardHandler。
func setupComponents(t *testing.T) (*CorrelationStore, *AlertEngine, http.Handler) {
	t.Helper()
	store := NewCorrelationStore()
	engine := NewAlertEngine(store)
	handler := DashboardHandler(store, engine)
	return store, engine, handler
}

// registerSlowAlert 注册一条「平均延迟 > thresholdMs 时触发」的告警规则。
func registerSlowAlert(engine *AlertEngine, thresholdMs float64, severity AlertSeverity) {
	engine.RegisterRule(NewThresholdAlertRule(ThresholdAlertConfig{
		Name:      "avg_latency_high",
		Threshold: thresholdMs,
		Severity:  severity,
		MetricFn:  avgLatencyMs,
	}))
}

// TestObservability_EndToEnd 端到端验证：trace 记录 → alert 评估 → dashboard HTTP 响应。
func TestObservability_EndToEnd(t *testing.T) {
	store, engine, handler := setupComponents(t)

	// 注册阈值告警规则：平均延迟 > 500ms 触发 warning
	registerSlowAlert(engine, 500, SeverityWarning)

	// 模拟一次 Agent 运行：Start → AddSpan(LLM) → AddSpan(慢工具) → End
	traceID := "trace-e2e-001"
	store.Start(traceID, "agent-alpha", "session-001")

	// Span 1：LLM 调用（快速，100ms）
	store.AddSpan(traceID, SpanRecord{
		Name:       "llm.call",
		Kind:       "llm",
		SpanID:     "span-llm-001",
		Status:     "ok",
		DurationMs: 100,
		StartedAt:  time.Now(),
	})

	// Span 2：工具调用（慢速，1200ms，超过阈值）
	store.AddSpan(traceID, SpanRecord{
		Name:       "tool.call",
		Kind:       "tool",
		SpanID:     "span-tool-001",
		Status:     "ok",
		DurationMs: 1200,
		StartedAt:  time.Now(),
	})

	store.End(traceID)

	// 验证 1：trace 已记录
	traces := store.List(0)
	if len(traces) != 1 {
		t.Fatalf("期望 1 条 trace，实际 %d 条", len(traces))
	}
	if traces[0].TraceID != traceID {
		t.Fatalf("期望 traceID=%q，实际 %q", traceID, traces[0].TraceID)
	}
	if len(traces[0].Spans) != 2 {
		t.Fatalf("期望 2 个 span，实际 %d 个", len(traces[0].Spans))
	}

	// 验证 2：告警规则触发（平均延迟 = (100+1200)/2 = 650ms > 500ms）
	alerts := engine.Evaluate()
	if len(alerts) == 0 {
		t.Fatal("期望告警触发，实际无告警")
	}
	if alerts[0].Rule != "avg_latency_high" {
		t.Fatalf("期望规则名=%q，实际 %q", "avg_latency_high", alerts[0].Rule)
	}
	if alerts[0].Severity != SeverityWarning {
		t.Fatalf("期望严重度=%q，实际 %q", SeverityWarning, alerts[0].Severity)
	}
	if alerts[0].Actual != 650 {
		t.Fatalf("期望 actual=650，实际 %f", alerts[0].Actual)
	}

	// 验证 3：Dashboard HTTP — /dashboard/summary
	t.Run("DashboardSummary", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/dashboard/summary", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("期望状态码 200，实际 %d", rec.Code)
		}

		var summary map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &summary); err != nil {
			t.Fatalf("JSON 解析失败: %v", err)
		}
		if summary["total_traces"].(float64) != 1 {
			t.Fatalf("期望 total_traces=1，实际 %v", summary["total_traces"])
		}
		if summary["total_spans"].(float64) != 2 {
			t.Fatalf("期望 total_spans=2，实际 %v", summary["total_spans"])
		}
	})

	// 验证 4：Dashboard HTTP — /dashboard/alerts
	t.Run("DashboardAlerts", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/dashboard/alerts", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("期望状态码 200，实际 %d", rec.Code)
		}

		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("JSON 解析失败: %v", err)
		}
		count := body["count"].(float64)
		if count < 1 {
			t.Fatalf("期望告警数 >= 1，实际 %v", count)
		}
		alertList := body["alerts"].([]any)
		if len(alertList) == 0 {
			t.Fatal("期望告警列表非空，实际为空")
		}
	})
}

// TestObservability_NoAlertWhenHealthy 所有调用均快速（< 500ms），不应触发告警。
func TestObservability_NoAlertWhenHealthy(t *testing.T) {
	store, engine, handler := setupComponents(t)
	registerSlowAlert(engine, 500, SeverityWarning)

	// 模拟一次健康的 Agent 运行
	traceID := "trace-healthy-001"
	store.Start(traceID, "agent-beta", "session-002")

	// 两个快速 span
	store.AddSpan(traceID, SpanRecord{
		Name:       "llm.call",
		Kind:       "llm",
		SpanID:     "span-llm-h01",
		Status:     "ok",
		DurationMs: 80,
		StartedAt:  time.Now(),
	})
	store.AddSpan(traceID, SpanRecord{
		Name:       "tool.call",
		Kind:       "tool",
		SpanID:     "span-tool-h01",
		Status:     "ok",
		DurationMs: 200,
		StartedAt:  time.Now(),
	})
	store.End(traceID)

	// 平均延迟 = (80+200)/2 = 140ms < 500ms，不应触发告警
	alerts := engine.Evaluate()
	if len(alerts) != 0 {
		t.Fatalf("期望无告警，实际触发 %d 条: %v", len(alerts), alerts)
	}

	// Dashboard /dashboard/alerts 应返回空列表
	req := httptest.NewRequest(http.MethodGet, "/dashboard/alerts", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("期望状态码 200，实际 %d", rec.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("JSON 解析失败: %v", err)
	}
	if body["count"].(float64) != 0 {
		t.Fatalf("期望告警数=0，实际 %v", body["count"])
	}
}

// TestObservability_MultipleTraces 记录多条 trace，验证 List 返回及 Dashboard 聚合。
func TestObservability_MultipleTraces(t *testing.T) {
	store, engine, handler := setupComponents(t)
	registerSlowAlert(engine, 300, SeverityCritical)

	// 写入 3 条 trace，每条各有不同 span
	for i, dur := range []int64{100, 500, 800} {
		tid := "trace-multi-" + string(rune('A'+i))
		store.Start(tid, "agent-gamma", "session-003")
		store.AddSpan(tid, SpanRecord{
			Name:       "llm.call",
			Kind:       "llm",
			SpanID:     "span-llm-m" + string(rune('0'+i)),
			Status:     "ok",
			DurationMs: dur,
			StartedAt:  time.Now(),
		})
		store.End(tid)
	}

	// 验证 1：List 返回 3 条 trace
	traces := store.List(0)
	if len(traces) != 3 {
		t.Fatalf("期望 3 条 trace，实际 %d 条", len(traces))
	}

	// 验证 2：按 Agent 名筛选
	byAgent := store.ListByAgent("agent-gamma", 0)
	if len(byAgent) != 3 {
		t.Fatalf("期望 agent-gamma 有 3 条 trace，实际 %d 条", len(byAgent))
	}

	// 验证 3：平均延迟 = (100+500+800)/3 ≈ 466.67ms > 300ms，告警触发
	alerts := engine.Evaluate()
	if len(alerts) == 0 {
		t.Fatal("期望告警触发，实际无告警")
	}
	if alerts[0].Severity != SeverityCritical {
		t.Fatalf("期望严重度=%q，实际 %q", SeverityCritical, alerts[0].Severity)
	}

	// 验证 4：Dashboard summary 聚合正确
	req := httptest.NewRequest(http.MethodGet, "/dashboard/summary", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("期望状态码 200，实际 %d", rec.Code)
	}

	var summary map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &summary); err != nil {
		t.Fatalf("JSON 解析失败: %v", err)
	}
	if summary["total_traces"].(float64) != 3 {
		t.Fatalf("期望 total_traces=3，实际 %v", summary["total_traces"])
	}
	if summary["total_spans"].(float64) != 3 {
		t.Fatalf("期望 total_spans=3，实际 %v", summary["total_spans"])
	}
	// 只有 1 个 agent（agent-gamma）
	if summary["agents"].(float64) != 1 {
		t.Fatalf("期望 agents=1，实际 %v", summary["agents"])
	}
}
