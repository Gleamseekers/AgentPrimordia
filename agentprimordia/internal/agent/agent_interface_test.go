package agent

import (
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
)

func TestAgentInterface_ReActAgent_Implements(t *testing.T) {
	var _ Agent = (*ReActAgent)(nil)
}

func TestAgentInterface_Name(t *testing.T) {
	agt, err := NewAgent("test-agent", "", llm.NewMockLLM(t))
	if err != nil {
		t.Fatal(err)
	}

	if agt.Name() != "test-agent" {
		t.Errorf("Name() = %q, want %q", agt.Name(), "test-agent")
	}
}

func TestAgentInterface_Stop(t *testing.T) {
	agt, err := NewAgent("stop-test", "", llm.NewMockLLM(t))
	if err != nil {
		t.Fatal(err)
	}

	agt.Stop()

	if !agt.Inner().lifecycle.IsStopped() {
		t.Error("after Stop(), lifecycle.IsStopped() = false, want true")
	}
}

func TestAgentInterface_Stats(t *testing.T) {
	agt, err := NewAgent("stats-test", "", llm.NewMockLLM(t))
	if err != nil {
		t.Fatal(err)
	}

	stats := agt.Stats()
	if stats.Status != StatusIdle {
		t.Errorf("Stats().Status = %q, want %q", stats.Status, StatusIdle)
	}
}
