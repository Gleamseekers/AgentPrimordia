package ap

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm"
)

// TestTelemetryFromEnvDisabledByDefault 未配置时不得构造 Provider（默认零开销）。
func TestTelemetryFromEnvDisabledByDefault(t *testing.T) {
	t.Setenv("AP_OTEL_ENABLED", "")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	tp, ok, err := TelemetryFromEnv()
	if err != nil {
		t.Fatalf("TelemetryFromEnv 失败: %v", err)
	}
	if ok || tp != nil {
		t.Errorf("未配置时应返回 (nil,false)，实际 ok=%v tp=%v", ok, tp)
	}
}

// TestTelemetryFromEnvEndpointEnables 设置 OTLP 端点即启用（OTel 标准变量）。
func TestTelemetryFromEnvEndpointEnables(t *testing.T) {
	t.Setenv("AP_OTEL_ENABLED", "")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4318")
	t.Setenv("OTEL_SERVICE_NAME", "unit-test-svc")

	tp, ok, err := TelemetryFromEnv()
	if err != nil {
		t.Fatalf("TelemetryFromEnv 失败: %v", err)
	}
	if !ok || tp == nil {
		t.Fatal("设置 OTLP 端点后应启用遥测")
	}
	defer func() { _ = tp.Shutdown() }()
	if !tp.MetricsEnabled() {
		t.Error("应启用指标采集")
	}
	if _, ok := tp.LoggingTracer(); !ok {
		t.Error("应启用 LoggingTracer")
	}
}

// TestTelemetryFromEnvExplicitFlag 显式 AP_OTEL_ENABLED=1 亦启用。
func TestTelemetryFromEnvExplicitFlag(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("AP_OTEL_ENABLED", "true")

	tp, ok, err := TelemetryFromEnv()
	if err != nil || !ok || tp == nil {
		t.Fatalf("显式开关应启用遥测，ok=%v err=%v", ok, err)
	}
	_ = tp.Shutdown()
}

// TestParseOTLPHeaders 请求头解析。
func TestParseOTLPHeaders(t *testing.T) {
	got := parseOTLPHeaders("authorization=Bearer abc, x-tenant = t1 ")
	if got["authorization"] != "Bearer abc" {
		t.Errorf("authorization = %q", got["authorization"])
	}
	if got["x-tenant"] != "t1" {
		t.Errorf("x-tenant = %q", got["x-tenant"])
	}
	if v := parseOTLPHeaders(""); v != nil {
		t.Errorf("空输入应返回 nil，实际 %v", v)
	}
	if v := parseOTLPHeaders("bad-no-equals"); v != nil {
		t.Errorf("无 = 的项应被忽略，实际 %v", v)
	}
}

// TestTelemetryEndToEndExport 端到端证据：env → Provider → WithTelemetry →
// Agent 运行 → OTLP 导出到真实 HTTP 端点（/v1/traces 与 /v1/metrics）。
// 这是「ReAct loop → OTel 运行时接线」从"只有 API"变为"真实可证"的判据。
func TestTelemetryEndToEndExport(t *testing.T) {
	var mu sync.Mutex
	got := map[string]int{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		got[r.URL.Path] += len(body)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", srv.URL)
	t.Setenv("OTEL_SERVICE_NAME", "e2e-agent")
	t.Setenv("AP_OTEL_ENABLED", "1")

	tp, ok, err := TelemetryFromEnv()
	if err != nil || !ok {
		t.Fatalf("TelemetryFromEnv: ok=%v err=%v", ok, err)
	}
	defer func() { _ = tp.Shutdown() }()

	mock := llm.NewMockLLM(t)
	mock.WithResponse("done")
	ag, err := NewAgent("otel-agent", "you are helpful", mock, WithTelemetry(tp))
	if err != nil {
		t.Fatalf("NewAgent 失败: %v", err)
	}
	if _, err := ag.Run(context.Background(), UserMessage("你好")); err != nil {
		t.Fatalf("Run 失败: %v", err)
	}
	if err := tp.ExportNow(); err != nil {
		t.Fatalf("ExportNow 失败: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if got["/v1/traces"] == 0 {
		t.Errorf("未向 /v1/traces 导出数据，实际 %v", got)
	}
	if got["/v1/metrics"] == 0 {
		t.Errorf("未向 /v1/metrics 导出数据，实际 %v", got)
	}
}
