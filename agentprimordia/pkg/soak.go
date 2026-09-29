// Stability: Experimental — v3.0.0 新增持续负载测试（Soak Test）框架，API 可能随实践演进而调整。
package ap

import (
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm/soak"
)

// ===== Soak Test 运行器 =====

// SoakRunner Soak Test 运行器，执行长时间负载测试以检测退化行为
type SoakRunner = soak.SoakRunner

// SoakRunnerConfig Soak Test 运行器配置
type SoakRunnerConfig = soak.RunnerConfig

// SoakResult Soak Test 结果
type SoakResult = soak.SoakResult

// SoakSample 采样点
type SoakSample = soak.Sample

// SoakResponse 请求响应
type SoakResponse = soak.Response

var (
	// NewSoakRunner 创建 Soak Test 运行器
	NewSoakRunner = soak.NewRunner
	// SoakRunnerConfigWithDefaults 填充运行器配置默认值
	SoakRunnerConfigWithDefaults = soak.RunnerConfigWithDefaults
	// SoakFormatReport 将 Soak Test 结果格式化为 Markdown 报告
	SoakFormatReport = soak.FormatReport
)

// ===== 负载模式 =====

// SoakLoadPattern 负载模式接口
type SoakLoadPattern = soak.LoadPattern

// ConstantLoadPattern 恒定负载模式
type ConstantLoadPattern = soak.ConstantLoadPattern

// StepLoadPattern 阶梯负载模式
type StepLoadPattern = soak.StepLoadPattern

// BurstLoadPattern 突发负载模式
type BurstLoadPattern = soak.BurstLoadPattern

// RandomLoadPattern 随机负载模式
type RandomLoadPattern = soak.RandomLoadPattern

// RampLoadPattern 渐进负载模式
type RampLoadPattern = soak.RampLoadPattern

var (
	// ConstantPattern 创建恒定负载模式
	ConstantPattern = soak.ConstantPattern
	// StepPattern 创建阶梯负载模式
	StepPattern = soak.StepPattern
	// BurstPattern 创建突发负载模式
	BurstPattern = soak.BurstPattern
	// RandomPattern 创建随机负载模式
	RandomPattern = soak.RandomPattern
	// RampPattern 创建渐进负载模式
	RampPattern = soak.RampPattern
)

// ===== 退化检测 =====

// SoakTrend 趋势类型
type SoakTrend = soak.Trend

// DegradationThreshold 退化检测阈值
type DegradationThreshold = soak.DegradationThreshold

// DegradationReport 退化分析报告
type DegradationReport = soak.DegradationReport

const (
	// TrendStable 稳定
	TrendStable = soak.TrendStable
	// TrendImproving 改善
	TrendImproving = soak.TrendImproving
	// TrendDegrading 退化
	TrendDegrading = soak.TrendDegrading
)

var (
	// AnalyzeDegradation 分析采样数据，检测退化趋势
	AnalyzeDegradation = soak.AnalyzeDegradation
	// DefaultDegradationThreshold 默认退化检测阈值
	DefaultDegradationThreshold = soak.DefaultDegradationThreshold
)
