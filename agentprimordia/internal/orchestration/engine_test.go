package orchestration

import (
	"context"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/cmd/example/demo"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent"
)

func TestExecutionEngine_Parallel(t *testing.T) {
	ag, err := agent.NewAgent("Test-Agent", "你是测试助手", demo.NewDemoLLM("ok"), agent.WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}

	steps := []*AgentStep{
		{ID: "p1", Name: "p1", Agent: ag, Prompt: "go"},
		{ID: "p2", Name: "p2", Agent: ag, Prompt: "go"},
		{ID: "p3", Name: "p3", Agent: ag, Prompt: "go"},
	}

	engine := NewExecutionEngine(ExecutionEngineConfig{MaxConcurrency: 2})
	result, err := engine.Run(context.Background(), ParallelMode, steps, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != StatusCompleted {
		t.Errorf("expected completed, got %s", result.Status)
	}
	if len(result.Steps) != 3 {
		t.Errorf("expected 3 steps, got %d", len(result.Steps))
	}
}
