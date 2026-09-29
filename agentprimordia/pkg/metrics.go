// Stability: Stable — Prometheus 指标收集与导出。
package ap

import (
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/metrics"
)

// AgentMetricsCollector 是 Agent 指标收集器，记录 LLM 调用、tool调用和轮次等指标
type AgentMetricsCollector = metrics.AgentMetrics

// Histogram 是直方图指标，用于统计值分布（如延迟、Token 用量）
type Histogram = metrics.Histogram

// HistogramSnapshot 是直方图的快照，包含分位数和统计值
type HistogramSnapshot = metrics.HistogramSnapshot

// MetricsSnapshot 是指标系统的完整快照
type MetricsSnapshot = metrics.MetricsSnapshot

// PrometheusHandler 是 Prometheus 格式的指标 HTTP 处理器
type PrometheusHandler = metrics.PrometheusHandler

// TelemetryExporter 是遥测数据导出接口
type TelemetryExporter = metrics.TelemetryExporter

// MultiExporter 是多目标导出器，支持同时导出到多个目标
type MultiExporter = metrics.MultiExporter

// LogExporter 是日志格式的指标导出器
type LogExporter = metrics.LogExporter

// JSONExporter 是 JSON 格式的指标导出器
type JSONExporter = metrics.JSONExporter

// MetricsHandler 提供 Prometheus /metrics 端点（可组合 http.Handler）
type MetricsHandler = metrics.Handler

// MetricsExporter 是指标导出器的通用接口
type MetricsExporter = metrics.MetricsExporter

// CostExporterConfig 成本导出器配置（与 metrics.CostExporterConfig 类型别名）
type CostExporterConfig = metrics.CostExporterConfig

// CostExporterSnapshot 成本导出器运行时快照
type CostExporterSnapshot = metrics.CostExporterSnapshot

var (
	// NewMetrics 创建指标收集器实例
	NewMetrics = metrics.NewMetrics
	// NewHistogram 创建直方图指标，参数为桶边界列表
	NewHistogram = metrics.NewHistogram
	// NewPrometheusHandler 创建 Prometheus 格式的 HTTP 处理器
	NewPrometheusHandler = metrics.NewPrometheusHandler
	// NewHandler 创建可组合的 Prometheus /metrics HTTP handler
	NewHandler = metrics.NewHandler
	// NewMultiExporter 创建多目标导出器
	NewMultiExporter = metrics.NewMultiExporter
	// NewLogExporter 创建日志格式导出器
	NewLogExporter = metrics.NewLogExporter
	// NewJSONExporter 创建 JSON 格式导出器
	NewJSONExporter = metrics.NewJSONExporter
	// NewMetricsExporter 创建通用指标导出器
	NewMetricsExporter = metrics.NewMetricsExporter
	// NewCostExporter 创建成本导出器，将 agent.CostTracker 数据导出到 Prometheus
	NewCostExporter = metrics.NewCostExporter
	// DefaultMetrics 返回默认的指标收集器实例
	DefaultMetrics = metrics.DefaultMetrics
)

// 成本相关包级 helper（操作默认 metrics 实例）
var (
	// RecordCostUSD 向默认 metrics 记录累计成本（USD）
	RecordCostUSD = metrics.RecordCostUSD
	// RecordCostCalls 向默认 metrics 记录 LLM 调用次数
	RecordCostCalls = metrics.RecordCostCalls
	// RecordCostTokens 向默认 metrics 记录 token 数（按 kind 拆分）
	RecordCostTokens = metrics.RecordCostTokens
	// SetLastCostUSD 向默认 metrics 设置最近一次调用成本（gauge）
	SetLastCostUSD = metrics.SetLastCostUSD
)

// CostSource 抽象 CostTracker 数据源（metrics.CostSource 别名）
type CostSource = metrics.CostSource

// CostSourceSummary 抽象汇总数据（metrics.CostSourceSummary 别名）
type CostSourceSummary = metrics.CostSourceSummary

// CostSourceModelCost 单模型成本（metrics.CostSourceModelCost 别名）
type CostSourceModelCost = metrics.CostSourceModelCost

// CostSourceRecord 抽象单次调用记录（metrics.CostSourceRecord 别名）
type CostSourceRecord = metrics.CostSourceRecord

// CostTrackerSource 把 agent.CostTracker 包装为 metrics.CostSource
//
// 这是 *agent.CostTracker 满足 metrics.CostSource 接口的现成适配器；
// 用户既可直接用 ap.NewCostExporter(ap.CostExporterConfig{Source: ctk})，
// 也可传 ap.WrapCostTracker(ctk) 作为更直观的等价写法。
type CostTrackerSource struct {
	Tracker *agent.CostTracker
}

// WrapCostTracker 把 *agent.CostTracker 包装为 metrics.CostSource
func WrapCostTracker(ct *agent.CostTracker) *CostTrackerSource {
	return &CostTrackerSource{Tracker: ct}
}

// Summary 实现 metrics.CostSource
func (s *CostTrackerSource) Summary() metrics.CostSourceSummary {
	raw := s.Tracker.Summary()
	out := metrics.CostSourceSummary{
		ByModel: make(map[string]metrics.CostSourceModelCost, len(raw.ByModel)),
	}
	for model, mc := range raw.ByModel {
		out.ByModel[model] = metrics.CostSourceModelCost{
			CostUSD: mc.CostUSD,
			Calls:   mc.Calls,
			Tokens:  mc.Tokens,
		}
	}
	return out
}

// Records 实现 metrics.CostSource
func (s *CostTrackerSource) Records() []metrics.CostSourceRecord {
	raw := s.Tracker.Records()
	out := make([]metrics.CostSourceRecord, 0, len(raw))
	for _, r := range raw {
		out = append(out, metrics.CostSourceRecord{
			Model:            r.Model,
			AgentName:        r.AgentName,
			CostUSD:          r.CostUSD,
			PromptTokens:     r.PromptTokens,
			CompletionTokens: r.CompletionTokens,
			TotalTokens:      r.TotalTokens,
		})
	}
	return out
}
