// Stability: 混合 —
//
//	Tracer 接口与 Noop 实现: Stable。
//	OTLP / Telemetry Provider / WithTelemetry: Experimental。
//
// 注意（v7.3 复核）：WithTelemetry / NewTelemetryProvider / NewOTLPExporter
// **当前无任何生产接线**（cmd/ 与 ecosystem/ 零调用，loop 的 tracer 默认 nil），
// 属"API 已定义、运行时未接线"。在补上真实二进制接线并给出 OTLP 导出证据前，
// 不应将其计入 Stable 能力叙事。接线计划见 docs/实验性能力清单.md。
package ap

import (
	"agentprimordia/internal/agent"
	obsotel "agentprimordia/internal/observability/export/otel"
)

type Tracer = agent.Tracer
type TracerDebug = agent.TracerDebug
type NoopTracer = agent.NoopTracer

var NewNoopTracer = agent.NewNoopTracer

type OTLPConfig = obsotel.OTLPConfig
type OTLPExporter = obsotel.OTLPExporter

var NewOTLPExporter = obsotel.NewOTLPExporter

type TelemetryConfig = obsotel.TelemetryConfig
type TelemetryProvider = obsotel.TelemetryProvider

var NewTelemetryProvider = obsotel.NewTelemetryProvider

// WithTelemetry 将 TelemetryProvider 的 Tracer 与 Metrics 一次性注入 Agent，
// 打通「ReAct loop → OTel」运行时接线：loop 产生 span 与指标，
// 由 provider 经 OTLPExporter 导出（周期或 ExportNow）。
//
// Stability: Experimental
func WithTelemetry(tp *obsotel.TelemetryProvider) agent.Option {
	opts := []agent.Option{agent.WithTracer(tp.Tracer())}
	if tp.MetricsEnabled() {
		opts = append(opts, agent.WithMetrics(tp.Metrics()))
	}
	return func(c *agent.AgentConfig) {
		for _, o := range opts {
			o(c)
		}
	}
}
