// Stability: Stable — v3.0.0 新增混沌工程框架，经充分测试验证，API 已冻结。
package ap

import (
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/chaos"
)

// ===== 实验引擎 =====

// ChaosEngine 混沌实验引擎，编排实验定义→注入→观测→判定
// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type ChaosEngine = chaos.ChaosEngine

// ChaosExperiment 混沌实验定义
// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type ChaosExperiment = chaos.Experiment

// ChaosExperimentResult 实验结果
// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type ChaosExperimentResult = chaos.ExperimentResult

// ChaosExperimentStatus 实验状态
// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type ChaosExperimentStatus = chaos.ExperimentStatus

// ChaosExperimentSummary 实验摘要（用于批量报告）
// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type ChaosExperimentSummary = chaos.ExperimentSummary

const (
	// ChaosStatusPending 实验待执行
	// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	ChaosStatusPending = chaos.StatusPending
	// ChaosStatusRunning 实验执行中
	// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	ChaosStatusRunning = chaos.StatusRunning
	// ChaosStatusCompleted 实验已完成
	// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	ChaosStatusCompleted = chaos.StatusCompleted
	// ChaosStatusAborted 实验已中止
	// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	ChaosStatusAborted = chaos.StatusAborted
	// ChaosStatusFailed 实验失败
	// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	ChaosStatusFailed = chaos.StatusFailed
)

var (
	// NewChaosEngine 创建混沌实验引擎
	// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewChaosEngine = chaos.NewEngine
	// ChaosFormatReport 将实验结果格式化为 Markdown 报告
	// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	ChaosFormatReport = chaos.FormatReport
	// ChaosSummarize 生成实验摘要
	// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	ChaosSummarize = chaos.Summarize
	// ChaosFormatSummaryTable 将多个实验摘要格式化为表格
	// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	ChaosFormatSummaryTable = chaos.FormatSummaryTable
)

// ===== 故障接口与类型 =====

// ChaosFault 故障定义接口
// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type ChaosFault = chaos.Fault

// ChaosCleanupFunc 故障清理函数
// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type ChaosCleanupFunc = chaos.CleanupFunc

// ChaosFaultResult 故障注入结果
// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type ChaosFaultResult = chaos.FaultResult

// ===== 网络故障 =====

// ChaosNetworkDelayFault 网络延迟故障
// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type ChaosNetworkDelayFault = chaos.NetworkDelayFault

// ChaosNetworkPartitionFault 网络分区故障
// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type ChaosNetworkPartitionFault = chaos.NetworkPartitionFault

// ChaosConnectionRefusedFault 连接拒绝故障
// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type ChaosConnectionRefusedFault = chaos.ConnectionRefusedFault

var (
	// NewChaosNetworkDelayFault 创建网络延迟故障
	// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewChaosNetworkDelayFault = chaos.NewNetworkDelayFault
	// NewChaosNetworkPartitionFault 创建网络分区故障
	// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewChaosNetworkPartitionFault = chaos.NewNetworkPartitionFault
	// NewChaosConnectionRefusedFault 创建连接拒绝故障
	// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewChaosConnectionRefusedFault = chaos.NewConnectionRefusedFault
)

// ===== 资源压力故障 =====

// ChaosCPUStressFault CPU 压力故障
// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type ChaosCPUStressFault = chaos.CPUStressFault

// ChaosMemoryStressFault 内存压力故障
// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type ChaosMemoryStressFault = chaos.MemoryStressFault

// ChaosProcessKillFault 进程杀死故障
// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type ChaosProcessKillFault = chaos.ProcessKillFault

var (
	// NewChaosCPUStressFault 创建 CPU 压力故障
	// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewChaosCPUStressFault = chaos.NewCPUStressFault
	// NewChaosMemoryStressFault 创建内存压力故障
	// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewChaosMemoryStressFault = chaos.NewMemoryStressFault
	// NewChaosProcessKillFault 创建进程杀死故障
	// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewChaosProcessKillFault = chaos.NewProcessKillFault
)

// ===== 组合与测试故障 =====

// ChaosCompositeFault 组合故障（多个故障同时注入）
// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type ChaosCompositeFault = chaos.CompositeFault

// ChaosNoopFault 空操作故障（用于测试框架）
// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type ChaosNoopFault = chaos.NoopFault

var (
	// NewChaosCompositeFault 创建组合故障
	// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewChaosCompositeFault = chaos.NewCompositeFault
	// NewChaosNoopFault 创建空操作故障
	// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewChaosNoopFault = chaos.NewNoopFault
)

// ===== LLM Provider 故障模拟 =====

// LLMHTTPStatusFault LLM HTTP 状态码故障
// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type LLMHTTPStatusFault = chaos.LLMHTTPStatusFault

// LLMTimeoutFault LLM 超时故障
// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type LLMTimeoutFault = chaos.LLMTimeoutFault

// LLMIntermittentFault LLM 间歇性故障
// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type LLMIntermittentFault = chaos.LLMIntermittentFault

// LLMSlowResponseFault LLM 慢响应故障
// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type LLMSlowResponseFault = chaos.LLMSlowResponseFault

// LLMFaultScenario 预定义 LLM 故障场景序列
// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type LLMFaultScenario = chaos.LLMFaultScenario

var (
	// LLMHTTP503Fault 创建 503 故障
	// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	LLMHTTP503Fault = chaos.LLMHTTP503Fault
	// LLMHTTP429Fault 创建 429 限流故障
	// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	LLMHTTP429Fault = chaos.LLMHTTP429Fault
	// LLMHTTP500Fault 创建 500 server error
	// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	LLMHTTP500Fault = chaos.LLMHTTP500Fault
	// NewLLMTimeoutFault 创建超时故障
	// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewLLMTimeoutFault = chaos.NewLLMTimeoutFault
	// NewLLMIntermittentFault 创建间歇性故障
	// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewLLMIntermittentFault = chaos.NewLLMIntermittentFault
	// NewLLMSlowResponseFault 创建慢响应故障
	// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewLLMSlowResponseFault = chaos.NewLLMSlowResponseFault
	// LLMFailoverScenario 创建完整的 LLM 故障转移场景
	// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	LLMFailoverScenario = chaos.LLMFailoverScenario
	// LLMChaosScenario 创建 LLM 混沌场景
	// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	LLMChaosScenario = chaos.LLMChaosScenario
)

// ===== 稳态验证器 =====

// ChaosSteadyState 稳态条件接口
// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type ChaosSteadyState = chaos.SteadyState

// ChaosSteadyStateResult 稳态检查结果
// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type ChaosSteadyStateResult = chaos.SteadyStateResult

// SLOSteadyState 基于 SLO 的稳态条件
// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type SLOSteadyState = chaos.SLOSteadyState

// AvailabilitySteadyState 可用性稳态条件
// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type AvailabilitySteadyState = chaos.AvailabilitySteadyState

// LatencySteadyState 延迟稳态条件
// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type LatencySteadyState = chaos.LatencySteadyState

// CompositeSteadyState 组合稳态条件（所有条件都必须满足）
// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type CompositeSteadyState = chaos.CompositeSteadyState

var (
	// NewSLOSteadyState 创建基于 SLO 的稳态条件
	// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewSLOSteadyState = chaos.NewSLOSteadyState
	// NewAvailabilitySteadyState 创建可用性稳态条件
	// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewAvailabilitySteadyState = chaos.NewAvailabilitySteadyState
	// NewLatencySteadyState 创建延迟稳态条件
	// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewLatencySteadyState = chaos.NewLatencySteadyState
	// NewCompositeSteadyState 创建组合稳态条件
	// Deprecated: 无外部采用证据的周边能力（混沌工程 / 稳态断言），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewCompositeSteadyState = chaos.NewCompositeSteadyState
)
