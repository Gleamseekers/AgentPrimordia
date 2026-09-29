// Stability: Experimental — v3.4.0 新增技能进化能力，API 可能随使用场景演进而调整。
package ap

import (
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/skills"
)

// --- 核心类型导出 ---

// Skill 技能抽象
type Skill = skills.Skill

// StepDef 技能步骤定义
type StepDef = skills.StepDef

// IOSchema 输入/输出 schema
type IOSchema = skills.IOSchema

// SkillStatus 技能状态
type SkillStatus = skills.SkillStatus

// Store 技能库
type SkillStore = skills.Store

// Matcher 技能匹配器
type SkillMatcher = skills.Matcher

// MatchResult 匹配结果
type SkillMatchResult = skills.MatchResult

// Acquisition 技能习得流水线
type SkillAcquisition = skills.Acquisition

// UsageTracker 技能使用追踪器
type SkillUsageTracker = skills.UsageTracker

// Trajectory 成功工具调用序列（技能习得原料）
type SkillTrajectory = skills.Trajectory

// ToolCallRecord 工具调用轨迹记录
type SkillToolCallRecord = skills.ToolCallRecord

// SkillDistiller LLM 提炼接口（由外部 learning/llm_distiller.go 适配）
type SkillDistiller = skills.SkillDistiller

// MatcherConfig 匹配器配置
type SkillMatcherConfig = skills.MatcherConfig

// Trigger 习得触发器
type SkillTrigger = skills.Trigger

// TriggerConfig 触发器配置
type SkillTriggerConfig = skills.TriggerConfig

// TriggerStrategy 习得触发策略
type SkillTriggerStrategy = skills.TriggerStrategy

// Verification 技能验证门：新技能必须通过测试用例才可启用
type SkillVerification = skills.Verification

// TestCase 技能验证测试用例
type SkillTestCase = skills.TestCase

// UsageRecord 技能调用日志
type SkillUsageRecord = skills.UsageRecord

// --- 状态常量导出 ---

const (
	// SkillDraft 草稿
	SkillDraft = skills.SkillDraft
	// SkillVerified 已验证
	SkillVerified = skills.SkillVerified
	// SkillActive 活跃
	SkillActive = skills.SkillActive
	// SkillDeprecated 已弃用
	SkillDeprecated = skills.SkillDeprecated
)

// --- 触发策略常量导出 ---

const (
	// SkillTriggerRepeatPattern 重复模式检测（同类任务出现 N 次）
	SkillTriggerRepeatPattern = skills.TriggerRepeatPattern
	// SkillTriggerLowSuccess 任务完成率低（低于阈值时触发习得）
	SkillTriggerLowSuccess = skills.TriggerLowSuccess
)

// --- 构造器导出 ---

var (
	// NewSkill 创建新技能
	NewSkill = skills.NewSkill
	// NewSkillStore 创建技能库
	NewSkillStore = skills.NewStore
	// NewSkillMatcher 创建技能匹配器
	NewSkillMatcher = skills.NewMatcher
	// NewSkillUsageTracker 创建使用追踪器
	NewSkillUsageTracker = skills.NewUsageTracker
	// NewSkillAcquisition 创建习得流水线
	NewSkillAcquisition = skills.NewAcquisition
	// NewSkillTrigger 创建习得触发器
	NewSkillTrigger = skills.NewTrigger
	// NewSkillVerification 创建技能验证门
	NewSkillVerification = skills.NewVerification
)
