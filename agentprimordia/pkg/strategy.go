// Package ap — 认知引擎策略内核公共 API 导出（v5.2）。
//
// 策略驱动的认知内核：ReAct / Plan-Execute-Reflect / 验证循环可插拔热切换。
// 详见 docs/v5.2重构风险评审.md 的接口冻结点承诺。
package ap

import (
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/strategy"
)

// ===== 类型别名 =====

// Strategy 推理策略接口：实现 Name/Run 即插件
//
// Stability: Stable
// Since: 5.2.0
type Strategy = strategy.Strategy

// Engine 引擎原语接口（策略的最小依赖面）
//
// Stability: Stable
// Since: 5.2.0
type Engine = strategy.Engine

// Task 策略执行任务
//
// Stability: Stable
// Since: 5.2.0
type StrategyTask = strategy.Task

// Result 策略执行结果
//
// Stability: Stable
// Since: 5.2.0
type StrategyResult = strategy.Result

// Registry 策略注册表（注册/取用/默认策略热切换，并发安全）
//
// Stability: Stable
// Since: 5.2.0
type StrategyRegistry = strategy.Registry

// Verifier 结果校验器接口
//
// Stability: Stable
// Since: 5.2.0
type Verifier = strategy.Verifier

// VerificationReport 验证报告
//
// Stability: Stable
// Since: 5.2.0
type VerificationReport = strategy.VerificationReport

// ThinkBudget 思考预算（自适应思考深度）
//
// Stability: Stable
// Since: 5.2.0
type ThinkBudget = strategy.ThinkBudget

// PlanCheckpoint 计划级检查点
//
// Stability: Stable
// Since: 5.2.0
type PlanCheckpoint = strategy.PlanCheckpoint

// PlanCheckpointStore 计划级检查点存储接口
//
// Stability: Stable
// Since: 5.2.0
type PlanCheckpointStore = strategy.PlanCheckpointStore

// ABReport A/B 对照报告
//
// Stability: Stable
// Since: 5.2.0
type ABReport = strategy.ABReport

// ===== 构造器 =====

var (
	// NewStrategyRegistry 创建策略注册表
	//
	// Stability: Stable
	// Since: 5.2.0
	NewStrategyRegistry = strategy.NewRegistry

	// NewReActStrategy 创建经典 ReAct 策略
	//
	// Stability: Stable
	// Since: 5.2.0
	NewReActStrategy = func() Strategy { return strategy.NewReAct() }

	// NewPlanExecuteReflectStrategy 创建计划-执行-反思策略
	//
	// Stability: Stable
	// Since: 5.2.0
	NewPlanExecuteReflectStrategy = func(maxReplans int) Strategy {
		return strategy.NewPlanExecuteReflect(maxReplans)
	}

	// NewVerificationLoopStrategy 创建验证循环策略（verifier 必填）
	//
	// Stability: Stable
	// Since: 5.2.0
	NewVerificationLoopStrategy = func(v Verifier, maxCorrections int) Strategy {
		return strategy.NewVerificationLoop(v, maxCorrections)
	}

	// NewKeywordVerifier 创建确定性关键词校验器
	//
	// Stability: Stable
	// Since: 5.2.0
	NewKeywordVerifier = strategy.NewKeywordVerifier

	// NewSelfCheckVerifier 创建 LLM 自校验器
	//
	// Stability: Stable
	// Since: 5.2.0
	NewSelfCheckVerifier = strategy.NewSelfCheckVerifier

	// AdaptiveBudget 按任务信号计算自适应思考预算
	//
	// Stability: Stable
	// Since: 5.2.0
	AdaptiveBudget = strategy.AdaptiveBudget

	// NewInMemoryPlanCheckpointStore 创建内存计划级检查点存储
	//
	// Stability: Stable
	// Since: 5.2.0
	NewInMemoryPlanCheckpointStore = strategy.NewInMemoryPlanCheckpointStore

	// SavePlanCheckpoint 保存计划级检查点
	//
	// Stability: Stable
	// Since: 5.2.0
	SavePlanCheckpoint = strategy.SavePlanCheckpoint

	// ResumePlan 从计划级检查点恢复（返回计划与下一批可执行子任务）
	//
	// Stability: Stable
	// Since: 5.2.0
	ResumePlan = strategy.ResumePlan

	// ABCompare 双策略同任务集对照跑分
	//
	// Stability: Stable
	// Since: 5.2.0
	ABCompare = strategy.ABCompare
)

// 策略名常量
const (
	// StrategyNameReAct 经典 ReAct 策略名
	StrategyNameReAct = strategy.NameReAct
	// StrategyNamePlanReflect 计划-执行-反思策略名
	StrategyNamePlanReflect = strategy.NamePlanReflect
	// StrategyNameVerification 验证循环策略名
	StrategyNameVerification = strategy.NameVerification
)
