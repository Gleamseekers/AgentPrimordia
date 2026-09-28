// Package otel 已迁移至 internal/observability/export/otel。
//
// 本文件保留向后兼容的类型别名，让现有调用方继续编译。
// 移除计划：v7.x。
//
// 状态（v7.3 复核）：全仓 0 引用（含测试），`pkg/otel.go` 直接引用
// internal/observability/export/otel，不经本包。本包已无任何消费者，
// 仅作为兼容层保留；若无调用方，可在下一次大版本一并删除。
package otel

import (
	"context"
	"time"

	obsotel "agentprimordia/internal/observability/export/otel"
)

// ===== 类型别名 =====

// OTelBridge OTel 兼容桥接（已迁移）。
type OTelBridge = obsotel.OTelBridge

// SpanBridge Span 桥接接口（已迁移）。
type SpanBridge = obsotel.SpanBridge

// EnhancedBridge 增强版 OTel 桥接器（已迁移）。
type EnhancedBridge = obsotel.EnhancedBridge

// BridgeConfig 桥接器配置（已迁移）。
type BridgeConfig = obsotel.BridgeConfig

// TelemetryProvider 遥测统一入口（已迁移）。
type TelemetryProvider = obsotel.TelemetryProvider

// TelemetryConfig 遥测配置（已迁移）。
type TelemetryConfig = obsotel.TelemetryConfig

// OTLPExporter OTLP HTTP/JSON 导出器（已迁移）。
type OTLPExporter = obsotel.OTLPExporter

// OTLPConfig OTLP 导出器配置（已迁移）。
type OTLPConfig = obsotel.OTLPConfig

// MetricExporter 指标导出器（已迁移）。
type MetricExporter = obsotel.MetricExporter

// CounterMetric 计数器指标（已迁移）。
type CounterMetric = obsotel.CounterMetric

// GaugeMetric 仪表盘指标（已迁移）。
type GaugeMetric = obsotel.GaugeMetric

// HistogramMetric 直方图指标（已迁移）。
type HistogramMetric = obsotel.HistogramMetric

// Baggage W3C Baggage 传播器（已迁移）。
type Baggage = obsotel.Baggage

// BaggageItem Baggage 条目（已迁移）。
type BaggageItem = obsotel.BaggageItem

// ===== 函数别名 =====

// NewOTelBridge 创建 OTel 兼容桥接（已迁移）。
var NewOTelBridge = obsotel.NewOTelBridge

// NewEnhancedBridge 创建增强版桥接器（已迁移）。
var NewEnhancedBridge = obsotel.NewEnhancedBridge

// NewTelemetryProvider 创建遥测提供者（已迁移）。
var NewTelemetryProvider = obsotel.NewTelemetryProvider

// NewOTLPExporter 创建 OTLP 导出器（已迁移）。
var NewOTLPExporter = obsotel.NewOTLPExporter

// NewMetricExporter 创建指标导出器（已迁移）。
var NewMetricExporter = obsotel.NewMetricExporter

// ParseBaggage 从 W3C Baggage header 解析（已迁移）。
var ParseBaggage = obsotel.ParseBaggage

// Extract 从 context 提取 Baggage（已迁移）。
var Extract = obsotel.Extract

// BridgeEnabled 标识 OTel SDK 桥接是否启用。
const BridgeEnabled = obsotel.BridgeEnabled

// ===== context 操作转发（确保类型兼容） =====

// InjectBaggage 向 context 注入 Baggage（转发至新包）。
func InjectBaggage(ctx context.Context, b *Baggage) context.Context {
	return b.Inject(ctx)
}

// BaggageFromContext 从 context 提取 Baggage（转发至新包）。
func BaggageFromContext(ctx context.Context) *Baggage {
	return Extract(ctx)
}

// 确保 time 被引用，避免 unused import。
var _ = time.Now
