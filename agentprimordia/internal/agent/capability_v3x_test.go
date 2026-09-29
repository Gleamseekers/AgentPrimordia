package agent

// v3.3-v3.6 能力接通验证：证明 CapabilityAgent 真正实现三个 Capable 接口，
// 且 ReAct 引擎可通过类型断言发现已注入能力（协议式微内核核心承诺）。

import (
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/autonomy"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/realtime"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/skills"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
)

func TestCapabilityAgent_ImplementsAutonomyCapable(t *testing.T) {
	a, _ := NewAgent("t", "", llm.NewMockLLM(t), WithMaxTurns(1))
	rt := autonomy.NewAutonomyRuntime(autonomy.RuntimeConfig{})
	cap := a.WithAutonomy(rt)
	var _ AutonomyCapable = cap
	if c, ok := any(cap).(AutonomyCapable); !ok || c.GetAutonomyRuntime() != rt {
		t.Error("AutonomyCapable 未接通")
	}
}

func TestCapabilityAgent_ImplementsSkillsCapable(t *testing.T) {
	a, _ := NewAgent("t", "", llm.NewMockLLM(t), WithMaxTurns(1))
	store := skills.NewStore()
	matcher := skills.NewMatcher(store, skills.MatcherConfig{})
	cap := a.WithSkills(store, matcher)
	var _ SkillsCapable = cap
	if c, ok := any(cap).(SkillsCapable); !ok || c.GetSkillStore() != store || c.GetSkillMatcher() != matcher {
		t.Error("SkillsCapable 未接通")
	}
}

func TestCapabilityAgent_ImplementsRealtimeCapable(t *testing.T) {
	a, _ := NewAgent("t", "", llm.NewMockLLM(t), WithMaxTurns(1))
	hub := realtime.NewRealtimeHub(realtime.HubConfig{})
	cap := a.WithRealtime(hub)
	var _ RealtimeCapable = cap
	if c, ok := any(cap).(RealtimeCapable); !ok || c.GetRealtimeHub() != hub {
		t.Error("RealtimeCapable 未接通")
	}
}

// 验证 NewAgent 的 Functional Options 路径也能接通（buildAgent 读 config 填充字段）
func TestNewAgent_WiresV3CapabilitiesFromConfig(t *testing.T) {
	rt := autonomy.NewAutonomyRuntime(autonomy.RuntimeConfig{})
	store := skills.NewStore()
	matcher := skills.NewMatcher(store, skills.MatcherConfig{})
	hub := realtime.NewRealtimeHub(realtime.HubConfig{})

	a, err := NewAgent("t", "", llm.NewMockLLM(t),
		WithMaxTurns(1),
		WithAutonomy(AutonomyConfig{Runtime: rt}),
		WithSkills(SkillsConfig{Store: store, Matcher: matcher}),
		WithRealtime(RealtimeConfig{Hub: hub}),
	)
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	if a.GetAutonomyRuntime() != rt {
		t.Error("config.Autonomy 未接通到 CapabilityAgent")
	}
	if a.GetSkillStore() != store || a.GetSkillMatcher() != matcher {
		t.Error("config.Skills 未接通到 CapabilityAgent")
	}
	if a.GetRealtimeHub() != hub {
		t.Error("config.Realtime 未接通到 CapabilityAgent")
	}
}

// 验证未注入时断言失败（能力确为可选，非默认满足）
func TestCapabilityAgent_V3CapableAbsentByDefault(t *testing.T) {
	a, _ := NewAgent("t", "", llm.NewMockLLM(t), WithMaxTurns(1))
	if a.GetAutonomyRuntime() != nil {
		t.Error("未注入时 autonomy 应为 nil")
	}
	if a.GetSkillStore() != nil {
		t.Error("未注入时 skills 应为 nil")
	}
	if a.GetRealtimeHub() != nil {
		t.Error("未注入时 realtime 应为 nil")
	}
}
