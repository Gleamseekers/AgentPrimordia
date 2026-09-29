package pool

import (
	"context"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent"
	"github.com/Gleamseekers/AgentPrimordia/internal/llm"
)

func TestPool_WithAgentFactory(t *testing.T) {
	mockLLM := llm.NewMockLLM(t).WithResponse("Factory agent response")

	var factoryCalled bool
	factory := func(config AgentFactoryConfig) agent.Agent {
		factoryCalled = true
		a, err := agent.NewAgent(config.Name, config.SystemPrompt, mockLLM, agent.WithMaxTurns(config.MaxTurns))
		if err != nil {
			t.Fatalf("failed to create agent: %v", err)
		}
		return a
	}

	pool := NewPool(PoolConfig{
		MaxConcurrency: 5,
		Timeout:        30 * time.Second,
	})
	pool.SetAgentFactory(factory)
	defer pool.Close()

	tasks := []TaskConfig{
		{ID: "factory-1", Title: "Factory Task", Prompt: "Do something"},
	}

	results, err := pool.Dispatch(context.Background(), tasks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !factoryCalled {
		t.Error("expected AgentFactory to be called")
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].Status != PoolTaskCompleted {
		t.Errorf("expected Completed, got %s", results[0].Status)
	}
}

func TestPool_AgentFactoryReceivesFullConfig(t *testing.T) {
	mockLLM := llm.NewMockLLM(t).WithResponse("Full config response")

	var receivedConfig AgentFactoryConfig
	factory := func(config AgentFactoryConfig) agent.Agent {
		receivedConfig = config
		a, err := agent.NewAgent(config.Name, config.SystemPrompt, mockLLM,
			agent.WithMaxTurns(config.MaxTurns),
			agent.WithTemperature(config.Temperature),
		)
		if err != nil {
			t.Fatalf("failed to create agent: %v", err)
		}
		return a
	}

	pool := NewPool(PoolConfig{
		MaxConcurrency: 5,
		Timeout:        30 * time.Second,
		DefaultAgent: ReActAgentConfig{
			SystemPrompt: "You are a helper",
			MaxTurns:     20,
			Temperature:  0.7,
		},
	})
	pool.SetAgentFactory(factory)
	defer pool.Close()

	tasks := []TaskConfig{
		{
			ID:         "full-cfg-1",
			Title:      "Full Config Task",
			Prompt:     "Do something",
			FilesScope: []string{"/src/"},
			Metadata:   map[string]string{"env": "test"},
		},
	}

	_, err := pool.Dispatch(context.Background(), tasks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedConfig.Name != "Full Config Task" {
		t.Errorf("Name = %q, want %q", receivedConfig.Name, "Full Config Task")
	}
	if receivedConfig.SystemPrompt != "You are a helper" {
		t.Errorf("SystemPrompt = %q, want %q", receivedConfig.SystemPrompt, "You are a helper")
	}
	if receivedConfig.MaxTurns != 20 {
		t.Errorf("MaxTurns = %d, want %d", receivedConfig.MaxTurns, 20)
	}
	if receivedConfig.Temperature != 0.7 {
		t.Errorf("Temperature = %f, want %f", receivedConfig.Temperature, 0.7)
	}
	if len(receivedConfig.FilesScope) != 1 || receivedConfig.FilesScope[0] != "/src/" {
		t.Errorf("FilesScope = %v, want [/src/]", receivedConfig.FilesScope)
	}
	if receivedConfig.Metadata["env"] != "test" {
		t.Errorf("Metadata[env] = %q, want %q", receivedConfig.Metadata["env"], "test")
	}
}

func TestPool_DefaultFactoryWithoutAgentFactory(t *testing.T) {
	mockLLM := llm.NewMockLLM(t).WithResponse("Default factory response")

	pool := NewPool(PoolConfig{
		MaxConcurrency: 5,
		Timeout:        30 * time.Second,
	})
	pool.SetModel(mockLLM)
	defer pool.Close()

	tasks := []TaskConfig{
		{ID: "default-1", Title: "Default Task", Prompt: "Do something"},
	}

	results, err := pool.Dispatch(context.Background(), tasks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if results[0].Status != PoolTaskCompleted {
		t.Errorf("expected Completed, got %s", results[0].Status)
	}
}
