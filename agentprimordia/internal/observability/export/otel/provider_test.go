package otel

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/metrics"
)

var handler200 = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
})

func TestTelemetryProvider_DefaultNoop(t *testing.T) {
	tp, err := NewTelemetryProvider(TelemetryConfig{}, nil)
	if err != nil {
		t.Fatalf("NewTelemetryProvider error: %v", err)
	}
	tr := tp.Tracer()
	if tr == nil {
		t.Fatal("Tracer should not be nil")
	}
	span := tr.Start("test", agent.SpanKindInternal)
	span.End()
	_ = tp.Shutdown()
}

func TestTelemetryProvider_WithTraces(t *testing.T) {
	tp, err := NewTelemetryProvider(TelemetryConfig{EnableTraces: true}, nil)
	if err != nil {
		t.Fatalf("NewTelemetryProvider error: %v", err)
	}
	lt, ok := tp.LoggingTracer()
	if !ok {
		t.Fatal("should have LoggingTracer when EnableTraces=true")
	}
	span := lt.Start("test-op", agent.SpanKindInternal)
	span.End()
	if lt.String() == "" {
		t.Error("LoggingTracer should have output after span end")
	}
	_ = tp.Shutdown()
}

func TestTelemetryProvider_WithOTLPExport(t *testing.T) {
	server := httptest.NewServer(handler200)
	defer server.Close()

	m := metrics.NewMetrics()
	tp, err := NewTelemetryProvider(TelemetryConfig{
		EnableTraces:  true,
		EnableMetrics: true,
		OTLPEndpoint:  server.URL,
	}, m)
	if err != nil {
		t.Fatalf("NewTelemetryProvider error: %v", err)
	}

	err = tp.ExportNow()
	if err != nil {
		t.Fatalf("ExportNow error: %v", err)
	}
	_ = tp.Shutdown()
}

func TestTelemetryProvider_ExportNow_NoExporter(t *testing.T) {
	tp, err := NewTelemetryProvider(TelemetryConfig{EnableTraces: true}, nil)
	if err != nil {
		t.Fatalf("NewTelemetryProvider error: %v", err)
	}
	err = tp.ExportNow()
	if err == nil {
		t.Error("should error when no exporter configured")
	}
	_ = tp.Shutdown()
}

func TestTelemetryProvider_BridgeEnabled(t *testing.T) {
	tp, _ := NewTelemetryProvider(TelemetryConfig{}, nil)
	if !tp.BridgeEnabled() {
		t.Error("BridgeEnabled should be true (unified implementation)")
	}
	_ = tp.Shutdown()
}
