package otel

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent"
	"github.com/Gleamseekers/AgentPrimordia/internal/metrics"
)

func TestOTLPExporter_ExportTraces(t *testing.T) {
	var receivedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/traces" {
			t.Errorf("path = %q, want /v1/traces", r.URL.Path)
		}
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	exporter := NewOTLPExporter(OTLPConfig{Endpoint: server.URL})
	defer exporter.Close()

	tracer := agent.NewLoggingTracer()
	span := tracer.Start("test-op", agent.SpanKindClient)
	span.SetAttribute("key", "value")
	span.End()

	err := exporter.ExportTraces(tracer)
	if err != nil {
		t.Fatalf("ExportTraces error: %v", err)
	}
	if len(receivedBody) == 0 {
		t.Fatal("should send data to server")
	}
	var payload map[string]any
	if err := json.Unmarshal(receivedBody, &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	resourceSpans, ok := payload["resourceSpans"].([]any)
	if !ok || len(resourceSpans) == 0 {
		t.Error("should contain resourceSpans")
	}
}

func TestOTLPExporter_ExportMetrics(t *testing.T) {
	var receivedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/metrics" {
			t.Errorf("path = %q, want /v1/metrics", r.URL.Path)
		}
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	exporter := NewOTLPExporter(OTLPConfig{Endpoint: server.URL})
	defer exporter.Close()

	m := metrics.NewMetrics()
	m.RecordLLMCall(100*time.Millisecond, nil)
	m.RecordToolCall(50*time.Millisecond, nil)

	err := exporter.ExportMetrics(m.Snapshot())
	if err != nil {
		t.Fatalf("ExportMetrics error: %v", err)
	}
	if len(receivedBody) == 0 {
		t.Fatal("should send data to server")
	}
}

func TestOTLPExporter_RetryOnFailure(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	exporter := NewOTLPExporter(OTLPConfig{Endpoint: server.URL, MaxRetry: 2})
	defer exporter.Close()

	m := metrics.NewMetrics()
	err := exporter.ExportMetrics(m.Snapshot())
	if err != nil {
		t.Fatalf("should succeed after retry: %v", err)
	}
	if callCount < 2 {
		t.Errorf("expected at least 2 calls, got %d", callCount)
	}
}

func TestOTLPExporter_NoRetryOnClientError(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	exporter := NewOTLPExporter(OTLPConfig{Endpoint: server.URL, MaxRetry: 3})
	defer exporter.Close()

	m := metrics.NewMetrics()
	err := exporter.ExportMetrics(m.Snapshot())
	if err == nil {
		t.Error("should return error on 4xx")
	}
	if callCount != 1 {
		t.Errorf("should not retry on 4xx, got %d calls", callCount)
	}
}
