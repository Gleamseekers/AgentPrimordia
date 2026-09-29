package otel

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/metrics"
)

// TelemetryConfig 遥测配置
type TelemetryConfig struct {
	ServiceName    string
	ServiceVersion string
	OTLPEndpoint   string
	OTLPHeaders    map[string]string
	ExportInterval time.Duration
	EnableTraces   bool
	EnableMetrics  bool
}

// TelemetryProvider 遥测统一入口
type TelemetryProvider struct {
	config   TelemetryConfig
	tracer   agent.Tracer
	exporter *OTLPExporter
	bridge   *OTelBridge
	metrics  *metrics.AgentMetrics
	mu       sync.RWMutex
	stopCh   chan struct{}
	logger   *slog.Logger
}

// NewTelemetryProvider 创建遥测提供者
func NewTelemetryProvider(config TelemetryConfig, m *metrics.AgentMetrics) (*TelemetryProvider, error) {
	tp := &TelemetryProvider{
		config:  config,
		metrics: m,
		bridge:  NewOTelBridge(),
		stopCh:  make(chan struct{}),
		logger:  slog.Default(),
	}

	if config.EnableTraces {
		tp.tracer = agent.NewLoggingTracer()
	} else {
		tp.tracer = agent.NewNoopTracer()
	}

	if config.OTLPEndpoint != "" && (config.EnableTraces || config.EnableMetrics) {
		tp.exporter = NewOTLPExporter(OTLPConfig{
			Endpoint: config.OTLPEndpoint,
			Headers:  config.OTLPHeaders,
		})
		if config.ExportInterval > 0 {
			// exportLoop goroutine 生命周期由 stopCh 控制：
			// Shutdown() 关闭 stopCh 后 goroutine 退出，无需额外等待
			go tp.exportLoop()
		}
	}

	return tp, nil
}

// Tracer 返回追踪器
func (tp *TelemetryProvider) Tracer() agent.Tracer {
	tp.mu.RLock()
	defer tp.mu.RUnlock()
	return tp.tracer
}

// LoggingTracer 返回 LoggingTracer（仅当启用 trace 时可用）
func (tp *TelemetryProvider) LoggingTracer() (*agent.LoggingTracer, bool) {
	tp.mu.RLock()
	defer tp.mu.RUnlock()
	lt, ok := tp.tracer.(*agent.LoggingTracer)
	return lt, ok
}

// Metrics 返回底层指标收集器，供注入 Agent（WithTelemetry）或读取快照使用。
func (tp *TelemetryProvider) Metrics() *metrics.AgentMetrics {
	tp.mu.RLock()
	defer tp.mu.RUnlock()
	return tp.metrics
}

// MetricsEnabled 返回指标采集是否启用（对应 TelemetryConfig.EnableMetrics）。
func (tp *TelemetryProvider) MetricsEnabled() bool {
	tp.mu.RLock()
	defer tp.mu.RUnlock()
	return tp.config.EnableMetrics
}

// BridgeEnabled 返回 OTel SDK 桥接是否启用
func (tp *TelemetryProvider) BridgeEnabled() bool {
	return BridgeEnabled
}

// ExportNow 立即导出当前遥测数据
func (tp *TelemetryProvider) ExportNow() error {
	tp.mu.RLock()
	defer tp.mu.RUnlock()

	if tp.exporter == nil {
		return fmt.Errorf("no OTLP exporter configured")
	}

	if tp.config.EnableTraces {
		if lt, ok := tp.tracer.(*agent.LoggingTracer); ok {
			if err := tp.exporter.ExportTraces(lt); err != nil {
				tp.logger.Error("export traces failed", "error", err)
			}
		}
	}

	if tp.config.EnableMetrics && tp.metrics != nil {
		if err := tp.exporter.ExportMetrics(tp.metrics.Snapshot()); err != nil {
			tp.logger.Error("export metrics failed", "error", err)
		}
	}

	return nil
}

// Shutdown 关闭遥测提供者
func (tp *TelemetryProvider) Shutdown() error {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	close(tp.stopCh)
	if tp.exporter != nil {
		tp.exporter.Close()
	}
	return tp.bridge.Shutdown()
}

func (tp *TelemetryProvider) exportLoop() {
	ticker := time.NewTicker(tp.config.ExportInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := tp.ExportNow(); err != nil {
				tp.logger.Warn("周期性导出失败", "error", err)
			}
		case <-tp.stopCh:
			return
		}
	}
}
