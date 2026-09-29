package otel

import (
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/metrics"
)

// TestTelemetryProvider_Metrics 验证 Metrics() 返回构造时传入的指标收集器。
// 这是 OTel 接线的核心：loop 上报的 metrics 与 OTLP 导出必须是同一个实例。
func TestTelemetryProvider_Metrics(t *testing.T) {
	m := metrics.NewMetrics()
	tp, err := NewTelemetryProvider(TelemetryConfig{ServiceName: "test"}, m)
	if err != nil {
		t.Fatalf("NewTelemetryProvider 失败: %v", err)
	}
	defer func() { _ = tp.Shutdown() }()

	if got := tp.Metrics(); got != m {
		t.Fatal("Metrics() 未返回构造时传入的收集器")
	}
}

// TestTelemetryProvider_ExportWithoutExporter 验证未配置 OTLP endpoint 时
// ExportNow 明确报错，避免用户误以为已导出。
func TestTelemetryProvider_ExportWithoutExporter(t *testing.T) {
	m := metrics.NewMetrics()
	tp, err := NewTelemetryProvider(TelemetryConfig{ServiceName: "test", EnableMetrics: true}, m)
	if err != nil {
		t.Fatalf("NewTelemetryProvider 失败: %v", err)
	}
	defer func() { _ = tp.Shutdown() }()

	if err := tp.ExportNow(); err == nil {
		t.Fatal("无 OTLP exporter 时 ExportNow 应返回错误")
	}
}
