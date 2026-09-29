package ap

import (
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/metrics"
	obsotel "github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/observability/export/otel"
)

// TestWithTelemetry 验证 WithTelemetry 把 TelemetryProvider 的 Tracer 与
// Metrics 一次性注入 Agent 配置——即「ReAct loop → OTel」的运行时接线入口。
func TestWithTelemetry(t *testing.T) {
	m := metrics.NewMetrics()
	tp, err := obsotel.NewTelemetryProvider(obsotel.TelemetryConfig{
		ServiceName:   "wiring-test",
		EnableTraces:  true,
		EnableMetrics: true,
	}, m)
	if err != nil {
		t.Fatalf("NewTelemetryProvider 失败: %v", err)
	}
	defer func() { _ = tp.Shutdown() }()

	cfg := &agent.AgentConfig{}
	WithTelemetry(tp)(cfg)

	if cfg.Observability.Tracer == nil {
		t.Error("WithTelemetry 未注入 Tracer")
	}
	if cfg.Observability.Metrics == nil {
		t.Error("WithTelemetry 未注入 Metrics")
	}
}

// TestWithTelemetry_DisablesMetrics 验证仅启用 traces 时不注入 Metrics（不覆盖用户已有配置）
func TestWithTelemetry_DisablesMetrics(t *testing.T) {
	m := metrics.NewMetrics()
	tp, err := obsotel.NewTelemetryProvider(obsotel.TelemetryConfig{
		ServiceName:  "wiring-test",
		EnableTraces: true,
	}, m)
	if err != nil {
		t.Fatalf("NewTelemetryProvider 失败: %v", err)
	}
	defer func() { _ = tp.Shutdown() }()

	cfg := &agent.AgentConfig{}
	WithTelemetry(tp)(cfg)

	if cfg.Observability.Metrics != nil {
		t.Error("仅启用 traces 时不应注入 Metrics")
	}
}
