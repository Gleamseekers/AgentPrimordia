// Stability: 混合 —
//
//	Tracer 接口与 Noop 实现: Stable。
//	OTLP / Telemetry Provider / WithTelemetry / TelemetryFromEnv: Experimental。
//
// 接线状态（v7.4）：`TelemetryFromEnv()` 是该子集的**生产构造点**——
// `ap start` 生成的 quickstart 项目在启动时读取 OTEL_EXPORTER_OTLP_ENDPOINT /
// AP_OTEL_ENABLED，构造 Provider 并经 `WithTelemetry` 注入 Agent，
// 打通「ReAct loop → LoggingTracer/Metrics → OTLP 导出」端到端链路。
// 端到端证据见 pkg/otel_env_test.go（httptest 收集 /v1/traces 与 /v1/metrics）。
package ap

import (
	"os"
	"strings"
	"time"

	"agentprimordia/internal/agent"
	"agentprimordia/internal/metrics"
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

// TelemetryFromEnv 从环境变量构造 TelemetryProvider（v7.4 端到端接线）。
//
// 未配置时返回 (nil, false, nil)——调用方应视为"不启用遥测"并跳过注入。
// 该函数是 WithTelemetry 的生产构造点：由生成的 quickstart 项目在启动时调用。
//
// 环境变量：
//
//	AP_OTEL_ENABLED=1            显式启用
//	OTEL_EXPORTER_OTLP_ENDPOINT  导出端点（设置即视为启用，OpenTelemetry 标准变量）
//	OTEL_SERVICE_NAME            服务名（默认 agentprimordia）
//	OTEL_EXPORTER_OTLP_HEADERS   附加请求头，格式 key=value[,key2=value2]
//	AP_OTEL_EXPORT_INTERVAL      周期导出间隔（time.ParseDuration，默认 30s）
//
// 返回的 Provider 含后台导出 goroutine，调用方必须在退出时调用 Shutdown()。
//
// Stability: Experimental
func TelemetryFromEnv() (*TelemetryProvider, bool, error) {
	endpoint := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	enabled := isEnvTruthy(os.Getenv("AP_OTEL_ENABLED")) || endpoint != ""
	if !enabled {
		return nil, false, nil
	}

	serviceName := strings.TrimSpace(os.Getenv("OTEL_SERVICE_NAME"))
	if serviceName == "" {
		serviceName = "agentprimordia"
	}

	cfg := TelemetryConfig{
		ServiceName:    serviceName,
		ServiceVersion: Version,
		OTLPEndpoint:   endpoint,
		OTLPHeaders:    parseOTLPHeaders(os.Getenv("OTEL_EXPORTER_OTLP_HEADERS")),
		ExportInterval: 30 * time.Second,
		EnableTraces:   true,
		EnableMetrics:  true,
	}
	if raw := strings.TrimSpace(os.Getenv("AP_OTEL_EXPORT_INTERVAL")); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			cfg.ExportInterval = d
		}
	}

	tp, err := NewTelemetryProvider(cfg, metrics.NewMetrics())
	if err != nil {
		return nil, false, err
	}
	return tp, true, nil
}

// isEnvTruthy 判定环境变量是否表示"开启"。
func isEnvTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// parseOTLPHeaders 解析 "key=value,key2=value2" 形式的 OTLP 请求头。
func parseOTLPHeaders(raw string) map[string]string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	out := make(map[string]string)
	for _, pair := range strings.Split(raw, ",") {
		kv := strings.SplitN(strings.TrimSpace(pair), "=", 2)
		if len(kv) != 2 {
			continue
		}
		k := strings.TrimSpace(kv[0])
		if k == "" {
			continue
		}
		out[k] = strings.TrimSpace(kv[1])
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
