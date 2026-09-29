package context

import (
	"context"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/core"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
)

func TestEstimateTokens(t *testing.T) {
	msgs := []core.Message{
		core.SystemMessage("You are a helpful assistant"),
		core.UserMessage("Hello, how are you?"),
		{Role: core.RoleAssistant, Content: "I'm doing well, thank you!"},
	}

	tokens := EstimateTokens(msgs)
	if tokens <= 0 {
		t.Error("EstimateTokens should return positive value")
	}

	expectedApprox := 0
	for _, m := range msgs {
		expectedApprox += len(m.Content) / 4
	}
	if tokens < expectedApprox/2 || tokens > expectedApprox*2 {
		t.Errorf("EstimateTokens = %d, expected approximately %d", tokens, expectedApprox)
	}
}

func TestEstimateTokens_Empty(t *testing.T) {
	tokens := EstimateTokens(nil)
	if tokens != 0 {
		t.Errorf("EstimateTokens(nil) = %d, want 0", tokens)
	}
}

func TestCompressStrategy_TrimShort(t *testing.T) {
	strategy := NewCompressStrategy(CompressConfig{
		MaxTokens:          1000,
		KeepSystemMessages: true,
		KeepRecentN:        2,
	})

	msgs := []core.Message{
		core.SystemMessage("system"),
		core.UserMessage("hello"),
	}

	result := strategy.Trim(msgs, 10)
	if len(result) != 2 {
		t.Errorf("Trim short messages: got %d, want 2", len(result))
	}
}

func TestCompressStrategy_TrimWithSummary(t *testing.T) {
	mockLLM := &compressMockLLM{summary: "This is a summary of the conversation"}

	strategy := NewCompressStrategy(CompressConfig{
		MaxTokens:          100,
		KeepSystemMessages: true,
		KeepRecentN:        2,
		SummaryModel:       mockLLM,
	})

	msgs := []core.Message{
		core.SystemMessage("system prompt"),
		core.UserMessage("question 1"),
		{Role: core.RoleAssistant, Content: "answer 1 with some details about the topic"},
		core.UserMessage("question 2"),
		{Role: core.RoleAssistant, Content: "answer 2 with more information"},
		core.UserMessage("question 3"),
		{Role: core.RoleAssistant, Content: "answer 3"},
	}

	result := strategy.Trim(msgs, 4)

	hasSummary := false
	for _, m := range result {
		if m.Role == core.RoleSystem && len(m.Content) > 20 {
			hasSummary = true
		}
	}
	if !hasSummary {
		t.Error("expected a summary message in result")
	}
}

func TestCompressStrategy_TrimFallback(t *testing.T) {
	strategy := NewCompressStrategy(CompressConfig{
		MaxTokens:          100,
		KeepSystemMessages: true,
		KeepRecentN:        2,
		SummaryModel:       nil,
	})

	msgs := []core.Message{
		core.SystemMessage("system prompt"),
		core.UserMessage("question 1"),
		{Role: core.RoleAssistant, Content: "answer 1"},
		core.UserMessage("question 2"),
		{Role: core.RoleAssistant, Content: "answer 2"},
		core.UserMessage("question 3"),
		{Role: core.RoleAssistant, Content: "answer 3"},
	}

	result := strategy.Trim(msgs, 4)

	if len(result) > 4 {
		t.Errorf("Trim with fallback: got %d messages, want <= 4", len(result))
	}

	hasSystem := false
	for _, m := range result {
		if m.Role == core.RoleSystem && m.Content == "system prompt" {
			hasSystem = true
		}
	}
	if !hasSystem {
		t.Error("expected system message to be preserved")
	}
}

func TestCompressStrategy_KeepSystem(t *testing.T) {
	strategy := NewCompressStrategy(CompressConfig{
		MaxTokens:          100,
		KeepSystemMessages: true,
		KeepRecentN:        1,
		SummaryModel:       nil,
	})

	msgs := []core.Message{
		core.SystemMessage("important system prompt"),
		core.SystemMessage("second system instruction"),
		core.UserMessage("question 1"),
		{Role: core.RoleAssistant, Content: "answer 1"},
		core.UserMessage("question 2"),
		{Role: core.RoleAssistant, Content: "answer 2"},
	}

	result := strategy.Trim(msgs, 3)

	systemCount := 0
	for _, m := range result {
		if m.Role == core.RoleSystem {
			systemCount++
		}
	}
	if systemCount < 1 {
		t.Error("expected at least one system message to be preserved")
	}
}

func TestCompressStrategy_KeepRecent(t *testing.T) {
	strategy := NewCompressStrategy(CompressConfig{
		MaxTokens:          100,
		KeepSystemMessages: true,
		KeepRecentN:        2,
		SummaryModel:       nil,
	})

	msgs := []core.Message{
		core.SystemMessage("system"),
		core.UserMessage("old question"),
		{Role: core.RoleAssistant, Content: "old answer"},
		core.UserMessage("recent question"),
		{Role: core.RoleAssistant, Content: "recent answer"},
	}

	result := strategy.Trim(msgs, 3)

	lastUser := ""
	for _, m := range result {
		if m.Role == core.RoleUser {
			lastUser = m.Content
		}
	}
	if lastUser != "recent question" {
		t.Errorf("last user message = %q, want %q", lastUser, "recent question")
	}
}

func TestCompressStrategy_NoCompressionNeeded(t *testing.T) {
	strategy := NewCompressStrategy(CompressConfig{
		MaxTokens:          10000,
		KeepSystemMessages: true,
		KeepRecentN:        10,
	})

	msgs := []core.Message{
		core.SystemMessage("system"),
		core.UserMessage("hello"),
	}

	result := strategy.Trim(msgs, 10)
	if len(result) != 2 {
		t.Errorf("Trim with no compression needed: got %d, want 2", len(result))
	}
}

type compressMockLLM struct {
	summary string
}

func (m *compressMockLLM) Complete(_ context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return &llm.CompletionResponse{
		ID:      "summary-resp",
		Content: m.summary,
		Usage:   llm.Usage{PromptTokens: 50, CompletionTokens: 20, TotalTokens: 70},
	}, nil
}

func (m *compressMockLLM) Stream(_ context.Context, _ *llm.CompletionRequest) (<-chan llm.Chunk, error) {
	return nil, nil
}

func (m *compressMockLLM) CallTools(_ context.Context, _ *llm.ToolCallRequest) (*llm.ToolCallResponse, error) {
	return nil, nil
}

func (m *compressMockLLM) Embeddings(_ context.Context, _ []string) ([][]float32, error) {
	return nil, nil
}

func (m *compressMockLLM) Info() llm.ModelInfo {
	return llm.ModelInfo{Name: "mock-summary"}
}
