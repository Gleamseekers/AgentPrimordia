package agent_test

import (
	"context"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/testutil"
)

func TestAcquireBuffer(t *testing.T) {
	buf := agent.AcquireBuffer()
	if buf == nil {
		t.Fatal("AcquireBuffer returned nil")
	}
	buf.WriteString("test data")
	if buf.String() != "test data" {
		t.Errorf("buffer content = %q, want %q", buf.String(), "test data")
	}
	agent.ReleaseBuffer(buf)
}

func TestCapabilityAgent_WithKnowledgeDistiller(t *testing.T) {
	a := testutil.NewTestAgent(testutil.TestAgentConfig{Name: "kd-test"})
	kd := a.WithKnowledgeDistiller(nil)
	if kd == nil {
		t.Fatal("WithKnowledgeDistiller returned nil")
	}
}

func TestCapabilityAgent_WithCapabilityEvolver(t *testing.T) {
	a := testutil.NewTestAgent(testutil.TestAgentConfig{Name: "evolve-test"})
	ev := a.WithCapabilityEvolver(nil)
	if ev == nil {
		t.Fatal("WithCapabilityEvolver returned nil")
	}
}

func TestCapabilityAgent_WithFeedbackLearner(t *testing.T) {
	a := testutil.NewTestAgent(testutil.TestAgentConfig{Name: "feedback-test"})
	fl := a.WithFeedbackLearner(nil)
	if fl == nil {
		t.Fatal("WithFeedbackLearner returned nil")
	}
}

func TestCapabilityAgent_GetMemoryStore_Nil(t *testing.T) {
	a := testutil.NewTestAgent(testutil.TestAgentConfig{Name: "mem-nil"})
	if mem := a.GetMemoryStore(); mem != nil {
		t.Error("expected nil memory store for fresh agent")
	}
}

func TestCapabilityAgent_GetRAGConfig(t *testing.T) {
	a := testutil.NewTestAgent(testutil.TestAgentConfig{Name: "rag"})
	_ = a.GetRAGConfig()
}

func TestCapabilityAgent_GetHITLConfig(t *testing.T) {
	a := testutil.NewTestAgent(testutil.TestAgentConfig{Name: "hitl"})
	_ = a.GetHITLConfig()
}

func TestCapabilityAgent_GetHooks(t *testing.T) {
	a := testutil.NewTestAgent(testutil.TestAgentConfig{Name: "hooks"})
	_ = a.GetHooks()
}

func TestCapabilityAgent_GetTracer(t *testing.T) {
	a := testutil.NewTestAgent(testutil.TestAgentConfig{Name: "tracer"})
	_ = a.GetTracer()
}

func TestCapabilityAgent_GetCostTracker(t *testing.T) {
	a := testutil.NewTestAgent(testutil.TestAgentConfig{Name: "cost"})
	_ = a.GetCostTracker()
}

func TestCapabilityAgent_Inner(t *testing.T) {
	a := testutil.NewTestAgent(testutil.TestAgentConfig{Name: "inner"})
	inner := a.Inner()
	if inner == nil {
		t.Fatal("Inner() returned nil")
	}
}

func TestCapabilityAgent_GracefulShutdown(t *testing.T) {
	a := testutil.NewTestAgent(testutil.TestAgentConfig{Name: "shutdown"})
	// 不应 panic
	_ = a.GracefulShutdown(context.Background())
}

func TestCapabilityAgent_Pause_Resume(t *testing.T) {
	a := testutil.NewTestAgent(testutil.TestAgentConfig{Name: "pause"})
	a.Pause()
	a.Resume()
}
