package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm"
)

// ===== 测试用 Mock LLM Provider =====

// mockLLMProvider 实现 llm.Provider 接口，用于 config 测试
type mockLLMProvider struct{}

func (m *mockLLMProvider) Complete(_ context.Context, _ *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return &llm.CompletionResponse{
		ID:      "mock-complete",
		Content: "mock response",
		Role:    "assistant",
	}, nil
}

func (m *mockLLMProvider) Stream(_ context.Context, _ *llm.CompletionRequest) (<-chan llm.Chunk, error) {
	ch := make(chan llm.Chunk, 1)
	go func() {
		defer close(ch)
		ch <- llm.Chunk{Content: "mock stream", Done: true}
	}()
	return ch, nil
}

func (m *mockLLMProvider) CallTools(_ context.Context, _ *llm.ToolCallRequest) (*llm.ToolCallResponse, error) {
	return &llm.ToolCallResponse{
		Content:   "mock tool response",
		ToolCalls: nil,
	}, nil
}

func (m *mockLLMProvider) Info() llm.ModelInfo {
	return llm.ModelInfo{
		Name:              "mock-config-model",
		Provider:          "mock",
		MaxContext:        4096,
		SupportsTools:     true,
		SupportsStreaming: true,
	}
}

// ===== defaultConfig 测试 =====

func TestDefaultConfig(t *testing.T) {
	cfg := defaultConfig()

	if cfg.MaxTurns != 50 {
		t.Errorf("defaultConfig().MaxTurns = %d, want 50", cfg.MaxTurns)
	}
	if cfg.Temperature != 0.0 {
		t.Errorf("defaultConfig().Temperature = %v, want 0.0", cfg.Temperature)
	}
	if cfg.Lifecycle == nil {
		t.Error("defaultConfig().Lifecycle 不应为 nil")
	}
	if cfg.Logger == nil {
		t.Error("defaultConfig().Logger 不应为 nil")
	}
}

// ===== Validate 测试 =====

func TestAgentConfig_Validate_NameRequired(t *testing.T) {
	cfg := defaultConfig()
	cfg.Name = ""
	cfg.Model = &mockLLMProvider{}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("空 Name 应返回错误，但得到 nil")
	}
	if !strings.Contains(err.Error(), "agent name is required") {
		t.Errorf("错误信息应包含 'agent name is required'，实际: %v", err)
	}
}

func TestAgentConfig_Validate_ModelRequired(t *testing.T) {
	cfg := defaultConfig()
	cfg.Name = "test-agent"
	cfg.Model = nil

	err := cfg.Validate()
	if err == nil {
		t.Fatal("nil Model 应返回错误，但得到 nil")
	}
	if !strings.Contains(err.Error(), "agent model (LLM Provider) is required") {
		t.Errorf("错误信息应包含 'agent model (LLM Provider) is required'，实际: %v", err)
	}
}

func TestAgentConfig_Validate_MaxTurnsPositive(t *testing.T) {
	cfg := defaultConfig()
	cfg.Name = "test-agent"
	cfg.Model = &mockLLMProvider{}
	cfg.MaxTurns = 0

	err := cfg.Validate()
	if err == nil {
		t.Fatal("MaxTurns=0 应返回错误，但得到 nil")
	}
	if !strings.Contains(err.Error(), "MaxTurns must be positive") {
		t.Errorf("错误信息应包含 'MaxTurns must be positive'，实际: %v", err)
	}
}

func TestAgentConfig_Validate_Valid(t *testing.T) {
	cfg := defaultConfig()
	cfg.Name = "test-agent"
	cfg.Model = &mockLLMProvider{}

	err := cfg.Validate()
	if err != nil {
		t.Errorf("合法配置应返回 nil 错误，实际: %v", err)
	}
}
