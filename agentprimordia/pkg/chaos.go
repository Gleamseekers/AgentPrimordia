// Stability: Stable — v3.0.0 新增混沌工程框架，经充分测试验证，API 已冻结。
package ap

import (
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/chaos"
)

// ===== 实验引擎 =====

// ChaosEngine 混沌实验引擎，编排实验定义→注入→观测→判定
type ChaosEngine = chaos.ChaosEngine

// ChaosExperiment 混沌实验定义
type ChaosExperiment = chaos.Experiment

// ChaosExperimentResult 实验结果
type ChaosExperimentResult = chaos.ExperimentResult

// ChaosExperimentStatus 实验状态
type ChaosExperimentStatus = chaos.ExperimentStatus

// ChaosExperimentSummary 实验摘要（用于批量报告）
type ChaosExperimentSummary = chaos.ExperimentSummary

const (
	// ChaosStatusPending 实验待执行
	ChaosStatusPending = chaos.StatusPending
	// ChaosStatusRunning 实验执行中
	ChaosStatusRunning = chaos.StatusRunning
	// ChaosStatusCompleted 实验已完成
	ChaosStatusCompleted = chaos.StatusCompleted
	// ChaosStatusAborted 实验已中止
	ChaosStatusAborted = chaos.StatusAborted
	// ChaosStatusFailed 实验失败
	ChaosStatusFailed = chaos.StatusFailed
)

var (
	// NewChaosEngine 创建混沌实验引擎
	NewChaosEngine = chaos.NewEngine
	// ChaosFormatReport 将实验结果格式化为 Markdown 报告
	ChaosFormatReport = chaos.FormatReport
	// ChaosSummarize 生成实验摘要
	ChaosSummarize = chaos.Summarize
	// ChaosFormatSummaryTable 将多个实验摘要格式化为表格
	ChaosFormatSummaryTable = chaos.FormatSummaryTable
)

// ===== 故障接口与类型 =====

// ChaosFault 故障定义接口
type ChaosFault = chaos.Fault

// ChaosCleanupFunc 故障清理函数
type ChaosCleanupFunc = chaos.CleanupFunc

// ChaosFaultResult 故障注入结果
type ChaosFaultResult = chaos.FaultResult

// ===== 网络故障 =====

// ChaosNetworkDelayFault 网络延迟故障
type ChaosNetworkDelayFault = chaos.NetworkDelayFault

// ChaosNetworkPartitionFault 网络分区故障
type ChaosNetworkPartitionFault = chaos.NetworkPartitionFault

// ChaosConnectionRefusedFault 连接拒绝故障
type ChaosConnectionRefusedFault = chaos.ConnectionRefusedFault

var (
	// NewChaosNetworkDelayFault 创建网络延迟故障
	NewChaosNetworkDelayFault = chaos.NewNetworkDelayFault
	// NewChaosNetworkPartitionFault 创建网络分区故障
	NewChaosNetworkPartitionFault = chaos.NewNetworkPartitionFault
	// NewChaosConnectionRefusedFault 创建连接拒绝故障
	NewChaosConnectionRefusedFault = chaos.NewConnectionRefusedFault
)

// ===== 资源压力故障 =====

// ChaosCPUStressFault CPU 压力故障
type ChaosCPUStressFault = chaos.CPUStressFault

// ChaosMemoryStressFault 内存压力故障
type ChaosMemoryStressFault = chaos.MemoryStressFault

// ChaosProcessKillFault 进程杀死故障
type ChaosProcessKillFault = chaos.ProcessKillFault

var (
	// NewChaosCPUStressFault 创建 CPU 压力故障
	NewChaosCPUStressFault = chaos.NewCPUStressFault
	// NewChaosMemoryStressFault 创建内存压力故障
	NewChaosMemoryStressFault = chaos.NewMemoryStressFault
	// NewChaosProcessKillFault 创建进程杀死故障
	NewChaosProcessKillFault = chaos.NewProcessKillFault
)

// ===== 组合与测试故障 =====

// ChaosCompositeFault 组合故障（多个故障同时注入）
type ChaosCompositeFault = chaos.CompositeFault

// ChaosNoopFault 空操作故障（用于测试框架）
type ChaosNoopFault = chaos.NoopFault

var (
	// NewChaosCompositeFault 创建组合故障
	NewChaosCompositeFault = chaos.NewCompositeFault
	// NewChaosNoopFault 创建空操作故障
	NewChaosNoopFault = chaos.NewNoopFault
)

// ===== LLM Provider 故障模拟 =====

// LLMHTTPStatusFault LLM HTTP 状态码故障
type LLMHTTPStatusFault = chaos.LLMHTTPStatusFault

// LLMTimeoutFault LLM 超时故障
type LLMTimeoutFault = chaos.LLMTimeoutFault

// LLMIntermittentFault LLM 间歇性故障
type LLMIntermittentFault = chaos.LLMIntermittentFault

// LLMSlowResponseFault LLM 慢响应故障
type LLMSlowResponseFault = chaos.LLMSlowResponseFault

// LLMFaultScenario 预定义 LLM 故障场景序列
type LLMFaultScenario = chaos.LLMFaultScenario

var (
	// LLMHTTP503Fault 创建 503 故障
	LLMHTTP503Fault = chaos.LLMHTTP503Fault
	// LLMHTTP429Fault 创建 429 限流故障
	LLMHTTP429Fault = chaos.LLMHTTP429Fault
	// LLMHTTP500Fault 创建 500 server error
	LLMHTTP500Fault = chaos.LLMHTTP500Fault
	// NewLLMTimeoutFault 创建超时故障
	NewLLMTimeoutFault = chaos.NewLLMTimeoutFault
	// NewLLMIntermittentFault 创建间歇性故障
	NewLLMIntermittentFault = chaos.NewLLMIntermittentFault
	// NewLLMSlowResponseFault 创建慢响应故障
	NewLLMSlowResponseFault = chaos.NewLLMSlowResponseFault
	// LLMFailoverScenario 创建完整的 LLM 故障转移场景
	LLMFailoverScenario = chaos.LLMFailoverScenario
	// LLMChaosScenario 创建 LLM 混沌场景
	LLMChaosScenario = chaos.LLMChaosScenario
)

// ===== 稳态验证器 =====

// ChaosSteadyState 稳态条件接口
type ChaosSteadyState = chaos.SteadyState

// ChaosSteadyStateResult 稳态检查结果
type ChaosSteadyStateResult = chaos.SteadyStateResult

// SLOSteadyState 基于 SLO 的稳态条件
type SLOSteadyState = chaos.SLOSteadyState

// AvailabilitySteadyState 可用性稳态条件
type AvailabilitySteadyState = chaos.AvailabilitySteadyState

// LatencySteadyState 延迟稳态条件
type LatencySteadyState = chaos.LatencySteadyState

// CompositeSteadyState 组合稳态条件（所有条件都必须满足）
type CompositeSteadyState = chaos.CompositeSteadyState

var (
	// NewSLOSteadyState 创建基于 SLO 的稳态条件
	NewSLOSteadyState = chaos.NewSLOSteadyState
	// NewAvailabilitySteadyState 创建可用性稳态条件
	NewAvailabilitySteadyState = chaos.NewAvailabilitySteadyState
	// NewLatencySteadyState 创建延迟稳态条件
	NewLatencySteadyState = chaos.NewLatencySteadyState
	// NewCompositeSteadyState 创建组合稳态条件
	NewCompositeSteadyState = chaos.NewCompositeSteadyState
)
