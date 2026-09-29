package agent

import (
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/skills"
)

// SkillsCapable 标识 Agent 具备技能进化能力。
// 引擎通过此接口发现 Agent 是否配置了技能库，
// 从而在运行时自动匹配和调用已习得的技能。
type SkillsCapable interface {
	GetSkillStore() *skills.Store
	GetSkillMatcher() *skills.Matcher
}

// SkillsConfig 技能进化能力配置
type SkillsConfig struct {
	// Store 技能库
	Store *skills.Store
	// Matcher 技能匹配器
	Matcher *skills.Matcher
	// Acquisition 习得流水线（可选）
	Acquisition *skills.Acquisition
	// UsageTracker 使用追踪器（可选）
	UsageTracker *skills.UsageTracker
}

// WithSkills 注入技能进化能力（链式注入模式）
func WithSkills(cfg SkillsConfig) Option {
	return func(c *AgentConfig) { c.Skills = cfg }
}
