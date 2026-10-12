// Stability: Experimental — v3.4.0 新增技能进化能力，API 可能随使用场景演进而调整。
package ap

import (
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/skills"
)

// --- 核心类型导出 ---

// Skill 技能抽象
// Deprecated: 无外部采用证据的周边能力（技能进化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type Skill = skills.Skill

// StepDef 技能步骤定义
// Deprecated: 无外部采用证据的周边能力（技能进化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type StepDef = skills.StepDef

// IOSchema 输入/输出 schema
// Deprecated: 无外部采用证据的周边能力（技能进化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type IOSchema = skills.IOSchema

// SkillStatus 技能状态
// Deprecated: 无外部采用证据的周边能力（技能进化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type SkillStatus = skills.SkillStatus

// Store 技能库
// Deprecated: 无外部采用证据的周边能力（技能进化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type SkillStore = skills.Store

// Matcher 技能匹配器
// Deprecated: 无外部采用证据的周边能力（技能进化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type SkillMatcher = skills.Matcher

// MatchResult 匹配结果
// Deprecated: 无外部采用证据的周边能力（技能进化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type SkillMatchResult = skills.MatchResult

// Acquisition 技能习得流水线
// Deprecated: 无外部采用证据的周边能力（技能进化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type SkillAcquisition = skills.Acquisition

// UsageTracker 技能使用追踪器
// Deprecated: 无外部采用证据的周边能力（技能进化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type SkillUsageTracker = skills.UsageTracker

// Trajectory 成功工具调用序列（技能习得原料）
// Deprecated: 无外部采用证据的周边能力（技能进化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type SkillTrajectory = skills.Trajectory

// ToolCallRecord 工具调用轨迹记录
// Deprecated: 无外部采用证据的周边能力（技能进化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type SkillToolCallRecord = skills.ToolCallRecord

// SkillDistiller LLM 提炼接口（由外部 learning/llm_distiller.go 适配）
// Deprecated: 无外部采用证据的周边能力（技能进化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type SkillDistiller = skills.SkillDistiller

// MatcherConfig 匹配器配置
// Deprecated: 无外部采用证据的周边能力（技能进化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type SkillMatcherConfig = skills.MatcherConfig

// Trigger 习得触发器
// Deprecated: 无外部采用证据的周边能力（技能进化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type SkillTrigger = skills.Trigger

// TriggerConfig 触发器配置
// Deprecated: 无外部采用证据的周边能力（技能进化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type SkillTriggerConfig = skills.TriggerConfig

// TriggerStrategy 习得触发策略
// Deprecated: 无外部采用证据的周边能力（技能进化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type SkillTriggerStrategy = skills.TriggerStrategy

// Verification 技能验证门：新技能必须通过测试用例才可启用
// Deprecated: 无外部采用证据的周边能力（技能进化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type SkillVerification = skills.Verification

// TestCase 技能验证测试用例
// Deprecated: 无外部采用证据的周边能力（技能进化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type SkillTestCase = skills.TestCase

// UsageRecord 技能调用日志
// Deprecated: 无外部采用证据的周边能力（技能进化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type SkillUsageRecord = skills.UsageRecord

// --- 状态常量导出 ---

const (
	// SkillDraft 草稿
	// Deprecated: 无外部采用证据的周边能力（技能进化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	SkillDraft = skills.SkillDraft
	// SkillVerified 已验证
	// Deprecated: 无外部采用证据的周边能力（技能进化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	SkillVerified = skills.SkillVerified
	// SkillActive 活跃
	// Deprecated: 无外部采用证据的周边能力（技能进化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	SkillActive = skills.SkillActive
	// SkillDeprecated 已弃用
	// Deprecated: 无外部采用证据的周边能力（技能进化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	SkillDeprecated = skills.SkillDeprecated
)

// --- 触发策略常量导出 ---

const (
	// SkillTriggerRepeatPattern 重复模式检测（同类任务出现 N 次）
	// Deprecated: 无外部采用证据的周边能力（技能进化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	SkillTriggerRepeatPattern = skills.TriggerRepeatPattern
	// SkillTriggerLowSuccess 任务完成率低（低于阈值时触发习得）
	// Deprecated: 无外部采用证据的周边能力（技能进化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	SkillTriggerLowSuccess = skills.TriggerLowSuccess
)

// --- 构造器导出 ---

var (
	// NewSkill 创建新技能
	// Deprecated: 无外部采用证据的周边能力（技能进化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewSkill = skills.NewSkill
	// NewSkillStore 创建技能库
	// Deprecated: 无外部采用证据的周边能力（技能进化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewSkillStore = skills.NewStore
	// NewSkillMatcher 创建技能匹配器
	// Deprecated: 无外部采用证据的周边能力（技能进化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewSkillMatcher = skills.NewMatcher
	// NewSkillUsageTracker 创建使用追踪器
	// Deprecated: 无外部采用证据的周边能力（技能进化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewSkillUsageTracker = skills.NewUsageTracker
	// NewSkillAcquisition 创建习得流水线
	// Deprecated: 无外部采用证据的周边能力（技能进化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewSkillAcquisition = skills.NewAcquisition
	// NewSkillTrigger 创建习得触发器
	// Deprecated: 无外部采用证据的周边能力（技能进化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewSkillTrigger = skills.NewTrigger
	// NewSkillVerification 创建技能验证门
	// Deprecated: 无外部采用证据的周边能力（技能进化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewSkillVerification = skills.NewVerification
)
