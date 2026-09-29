package context

import (
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/core"
)

func TestDefaultTrim_UnderLimit(t *testing.T) {
	strategy := NewDefaultStrategy(80)

	messages := []core.Message{
		core.SystemMessage("system"),
		core.UserMessage("hello"),
		{Role: core.RoleAssistant, Content: "hi"},
	}

	result := strategy.Trim(messages, 10)
	if len(result) != 3 {
		t.Errorf("expected 3 messages, got %d", len(result))
	}
}

func TestDefaultTrim_ExceedLimit(t *testing.T) {
	strategy := NewDefaultStrategy(5)

	messages := []core.Message{
		core.SystemMessage("system"),
		core.UserMessage("msg1"),
		{Role: core.RoleAssistant, Content: "reply1"},
		core.UserMessage("msg2"),
		{Role: core.RoleAssistant, Content: "reply2"},
		core.UserMessage("msg3"),
		{Role: core.RoleAssistant, Content: "reply3"},
		core.UserMessage("msg4"),
		{Role: core.RoleAssistant, Content: "reply4"},
		core.UserMessage("msg5"),
		{Role: core.RoleAssistant, Content: "reply5"},
	}

	result := strategy.Trim(messages, 6)
	if len(result) != 6 {
		t.Errorf("expected 6 messages, got %d", len(result))
	}

	if result[0].Role != core.RoleSystem {
		t.Error("first message should be system message")
	}

	if result[1].Content != "reply3" {
		t.Errorf("expected second message to be 'reply3', got '%s'", result[1].Content)
	}
}

func TestDefaultTrim_OnlySystem(t *testing.T) {
	strategy := NewDefaultStrategy(80)

	messages := []core.Message{
		core.SystemMessage("system only"),
	}

	result := strategy.Trim(messages, 10)
	if len(result) != 1 {
		t.Errorf("expected 1 message, got %d", len(result))
	}
}

func TestDefaultTrim_EmptyMessages(t *testing.T) {
	strategy := NewDefaultStrategy(80)

	messages := []core.Message{}

	result := strategy.Trim(messages, 10)
	if len(result) != 0 {
		t.Errorf("expected 0 messages, got %d", len(result))
	}
}

func TestDefaultTrim_CustomKeepLast(t *testing.T) {
	strategy := NewDefaultStrategy(3)

	messages := []core.Message{
		core.SystemMessage("system"),
		core.UserMessage("msg1"),
		{Role: core.RoleAssistant, Content: "reply1"},
		core.UserMessage("msg2"),
		{Role: core.RoleAssistant, Content: "reply2"},
		core.UserMessage("msg3"),
		{Role: core.RoleAssistant, Content: "reply3"},
	}

	result := strategy.Trim(messages, 4)
	if len(result) != 4 {
		t.Errorf("expected 4 messages, got %d", len(result))
	}

	if result[0].Role != core.RoleSystem {
		t.Error("first message should be system message")
	}

	if result[1].Content != "reply2" {
		t.Errorf("expected second message to be 'reply2', got '%s'", result[1].Content)
	}
}

func TestDefaultTrim_ZeroKeepLast(t *testing.T) {
	strategy := NewDefaultStrategy(0)

	messages := []core.Message{
		core.SystemMessage("system"),
		core.UserMessage("msg1"),
		{Role: core.RoleAssistant, Content: "reply1"},
	}

	result := strategy.Trim(messages, 1)
	if len(result) != 1 {
		t.Errorf("expected 1 message, got %d", len(result))
	}

	if result[0].Role != core.RoleSystem {
		t.Error("first message should be system message")
	}
}

func TestDefaultTrim_ZeroMaxMessages(t *testing.T) {
	strategy := NewDefaultStrategy(80)

	messages := []core.Message{
		core.SystemMessage("system"),
		core.UserMessage("msg1"),
	}

	result := strategy.Trim(messages, 0)
	if len(result) != 2 {
		t.Errorf("expected 2 messages (no trimming), got %d", len(result))
	}
}

func TestDefaultStrategy_DefaultKeepLast(t *testing.T) {
	strategy := NewDefaultStrategy(0)
	if strategy.KeepLast != 80 {
		t.Errorf("expected default KeepLast=80, got %d", strategy.KeepLast)
	}
}
