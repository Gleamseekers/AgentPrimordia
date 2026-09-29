package orchestration

import (
	"context"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/cmd/example/demo"
	"github.com/Gleamseekers/AgentPrimordia/internal/agent"
)

func TestDefaultStepExecutor_ExecutesAgent(t *testing.T) {
	ag, err := agent.NewAgent("Step-Agent", "你是测试助手", demo.NewDemoLLM("hello"), agent.WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}

	step := &AgentStep{ID: "s1", Name: "s1", Agent: ag, Prompt: "say hello"}
	exec := NewDefaultStepExecutor(nil)
	result := exec.Execute(context.Background(), step, nil)

	if result.Status != StepCompleted {
		t.Errorf("expected completed, got %s, error: %v", result.Status, result.Error)
	}
	if result.Output["content"] != "hello" {
		t.Errorf("unexpected output: %v", result.Output)
	}
}
