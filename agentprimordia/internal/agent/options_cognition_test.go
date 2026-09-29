// options_cognition_test.go — 认知能力（Planning/Reflection）Option 单元测试
// 覆盖 WithPlanner / WithReflector / WithReflectionThreshold / WithCognition，
// 以及 NewAgent 构建链路对认知能力的注入。
package agent

import (
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/reflection"
)

// ===== 顶层快捷注入 Option 测试 =====

func TestWithPlanner(t *testing.T) {
	cfg := defaultConfig()
	p := &stubPlanner{}
	WithPlanner(p)(&cfg)
	if cfg.Cognition.Planner == nil {
		t.Error("Cognition.Planner 未被正确设置")
	}
	// 传 nil 验证不 panic
	WithPlanner(nil)(&cfg)
	if cfg.Cognition.Planner != nil {
		t.Error("Cognition.Planner 应为 nil")
	}
}

func TestWithReflector(t *testing.T) {
	cfg := defaultConfig()
	r := &stubReflector{}
	WithReflector(r)(&cfg)
	if cfg.Cognition.Reflector == nil {
		t.Error("Cognition.Reflector 未被正确设置")
	}
	WithReflector(nil)(&cfg)
	if cfg.Cognition.Reflector != nil {
		t.Error("Cognition.Reflector 应为 nil")
	}
}

func TestWithReflectionThreshold(t *testing.T) {
	cfg := defaultConfig()
	WithReflectionThreshold("medium")(&cfg)
	if cfg.Cognition.ReflectionSeverityThreshold != "medium" {
		t.Errorf("ReflectionSeverityThreshold = %q, want %q",
			cfg.Cognition.ReflectionSeverityThreshold, "medium")
	}
}

// ===== 分组注入 Option 测试 =====

func TestWithCognition(t *testing.T) {
	cfg := defaultConfig()
	group := CognitionConfig{
		Planner:                     &stubPlanner{},
		Reflector:                   &stubReflector{},
		ReflectionSeverityThreshold: "critical",
	}
	WithCognition(group)(&cfg)
	if cfg.Cognition.Planner == nil || cfg.Cognition.Reflector == nil {
		t.Error("WithCognition 未整体设置分组字段")
	}
	if cfg.Cognition.ReflectionSeverityThreshold != "critical" {
		t.Errorf("ReflectionSeverityThreshold = %q, want %q",
			cfg.Cognition.ReflectionSeverityThreshold, "critical")
	}
}

// ===== NewAgent 构建链路注入测试 =====

// TestNewAgent_CognitionInjected 验证 NewAgent 会把 Cognition 分组
// 注入到 CapabilityAgent，使 ReAct 引擎能通过 Capable 接口发现能力。
func TestNewAgent_CognitionInjected(t *testing.T) {
	p := &stubPlanner{}
	r := &stubReflector{}

	a, err := NewAgent("cognition-bot", "you are helpful", &mockLLMProvider{},
		WithPlanner(p),
		WithReflector(r),
		WithReflectionThreshold(string(reflection.SeverityMedium)),
	)
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}

	if got := a.GetPlanner(); got == nil {
		t.Error("NewAgent 未注入 Planner")
	}
	if got := a.GetReflector(); got == nil {
		t.Error("NewAgent 未注入 Reflector")
	}
}

// TestNewAgent_NoCognition 验证未注入认知能力时不影响正常构建。
func TestNewAgent_NoCognition(t *testing.T) {
	a, err := NewAgent("plain-bot", "you are helpful", &mockLLMProvider{})
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}
	if got := a.GetPlanner(); got != nil {
		t.Error("未注入时 Planner 应为 nil")
	}
	if got := a.GetReflector(); got != nil {
		t.Error("未注入时 Reflector 应为 nil")
	}
}
