package agent

import (
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/multimodal"
)

func TestConvertMultimodalMessage(t *testing.T) {
	msg := Message{
		Role: "user",
		ContentParts: []multimodal.ContentPart{
			{Type: "text", Text: "描述这张图"},
			{Type: "image_url", URL: "https://example.com/img.png", MIME: "image/png"},
		},
	}
	if !msg.HasMultimodal() {
		t.Fatal("should detect multimodal")
	}
	text := msg.TextContent()
	if text != "描述这张图" {
		t.Errorf("expected '描述这张图', got '%s'", text)
	}
}

func TestConvertTextOnlyMessage(t *testing.T) {
	msg := Message{Role: "user", Content: "hello"}
	if msg.HasMultimodal() {
		t.Error("plain text should not be multimodal")
	}
	text := msg.TextContent()
	if text != "hello" {
		t.Errorf("expected 'hello', got '%s'", text)
	}
}
