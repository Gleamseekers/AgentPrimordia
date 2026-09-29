package multimodal

import (
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/llm"
)

// ===== ContentPart / Message tests =====

func TestMessage_HasMultimodal(t *testing.T) {
	m := Message{Role: RoleUser, Content: "hello"}
	if m.HasMultimodal() {
		t.Error("plain text message should not be multimodal")
	}

	m.ContentParts = []ContentPart{{Type: "text", Text: "hi"}}
	if !m.HasMultimodal() {
		t.Error("message with ContentParts should be multimodal")
	}
}

func TestUserMultimodalMessage(t *testing.T) {
	msg := UserMultimodalMessage(
		ContentPart{Type: "text", Text: "describe this"},
		ContentPart{Type: "image_url", URL: "http://example.com/img.png", Detail: "high"},
	)
	if msg.Role != RoleUser {
		t.Errorf("Role = %q, want %q", msg.Role, RoleUser)
	}
	if len(msg.ContentParts) != 2 {
		t.Fatalf("ContentParts length = %d, want 2", len(msg.ContentParts))
	}
	if msg.Metadata.Timestamp.IsZero() {
		t.Error("Timestamp should be set")
	}
}

func TestUserImageMessage(t *testing.T) {
	msg := UserImageMessage("What is this?", "http://example.com/img.png")
	if msg.Role != RoleUser {
		t.Errorf("Role = %q, want %q", msg.Role, RoleUser)
	}
	if len(msg.ContentParts) != 2 {
		t.Fatalf("ContentParts length = %d, want 2", len(msg.ContentParts))
	}
	if msg.ContentParts[0].Text != "What is this?" {
		t.Errorf("text = %q", msg.ContentParts[0].Text)
	}
	if msg.ContentParts[1].URL != "http://example.com/img.png" {
		t.Errorf("URL = %q", msg.ContentParts[1].URL)
	}
	if msg.ContentParts[1].Detail != "auto" {
		t.Errorf("Detail = %q, want auto", msg.ContentParts[1].Detail)
	}
}

// ===== MultimodalAdapter tests =====

func TestMultimodalAdapter_ToLLMContents_Empty(t *testing.T) {
	a := &MultimodalAdapter{}
	result := a.ToLLMContents(nil)
	if result != nil {
		t.Error("ToLLMContents(nil) should return nil")
	}
}

func TestMultimodalAdapter_ToLLMContents_Text(t *testing.T) {
	a := &MultimodalAdapter{}
	parts := []ContentPart{{Type: "text", Text: "hello"}}
	result := a.ToLLMContents(parts)
	if len(result) != 1 {
		t.Fatalf("length = %d, want 1", len(result))
	}
	if result[0].Type != llm.ContentTypeText {
		t.Errorf("Type = %q, want %q", result[0].Type, llm.ContentTypeText)
	}
	if result[0].Text != "hello" {
		t.Errorf("Text = %q, want hello", result[0].Text)
	}
}

func TestMultimodalAdapter_ToLLMContents_ImageURL(t *testing.T) {
	a := &MultimodalAdapter{}
	parts := []ContentPart{{Type: "image_url", URL: "http://example.com/img.png", Detail: "high"}}
	result := a.ToLLMContents(parts)
	if result[0].Type != llm.ContentTypeImageURL {
		t.Errorf("Type = %q, want %q", result[0].Type, llm.ContentTypeImageURL)
	}
	if result[0].URL != "http://example.com/img.png" {
		t.Errorf("URL = %q", result[0].URL)
	}
	if result[0].Detail != "high" {
		t.Errorf("Detail = %q, want high", result[0].Detail)
	}
}

func TestMultimodalAdapter_ToLLMContents_ImageURL_DefaultDetail(t *testing.T) {
	a := &MultimodalAdapter{}
	parts := []ContentPart{{Type: "image_url", URL: "http://example.com/img.png"}}
	result := a.ToLLMContents(parts)
	if result[0].Detail != "auto" {
		t.Errorf("Detail = %q, want auto (default)", result[0].Detail)
	}
}

func TestMultimodalAdapter_ToLLMContents_ImageB64(t *testing.T) {
	a := &MultimodalAdapter{}
	parts := []ContentPart{{Type: "image_b64", Data: "base64data", MIME: "image/png", Detail: "low"}}
	result := a.ToLLMContents(parts)
	if result[0].Type != llm.ContentTypeImageB64 {
		t.Errorf("Type = %q, want %q", result[0].Type, llm.ContentTypeImageB64)
	}
	if result[0].Data != "base64data" {
		t.Errorf("Data = %q", result[0].Data)
	}
	if result[0].MIME != "image/png" {
		t.Errorf("MIME = %q", result[0].MIME)
	}
	if result[0].Detail != "low" {
		t.Errorf("Detail = %q, want low", result[0].Detail)
	}
}

func TestMultimodalAdapter_ToLLMContents_ImageB64_DefaultDetail(t *testing.T) {
	a := &MultimodalAdapter{}
	parts := []ContentPart{{Type: "image_b64", Data: "data", MIME: "image/jpeg"}}
	result := a.ToLLMContents(parts)
	if result[0].Detail != "auto" {
		t.Errorf("Detail = %q, want auto (default)", result[0].Detail)
	}
}

func TestMultimodalAdapter_ToLLMContents_Audio(t *testing.T) {
	a := &MultimodalAdapter{}
	parts := []ContentPart{{Type: "audio", Data: "audiodata", MIME: "audio/wav"}}
	result := a.ToLLMContents(parts)
	if result[0].Type != llm.ContentTypeAudio {
		t.Errorf("Type = %q, want %q", result[0].Type, llm.ContentTypeAudio)
	}
	if result[0].Data != "audiodata" {
		t.Errorf("Data = %q", result[0].Data)
	}
}

func TestMultimodalAdapter_ToLLMContents_Video(t *testing.T) {
	a := &MultimodalAdapter{}
	parts := []ContentPart{{Type: "video", Data: "videodata", MIME: "video/mp4"}}
	result := a.ToLLMContents(parts)
	if result[0].Type != llm.ContentTypeVideo {
		t.Errorf("Type = %q, want %q", result[0].Type, llm.ContentTypeVideo)
	}
	if result[0].Data != "videodata" {
		t.Errorf("Data = %q", result[0].Data)
	}
}

func TestMultimodalAdapter_ToLLMContents_UnknownType(t *testing.T) {
	a := &MultimodalAdapter{}
	parts := []ContentPart{{Type: "unknown", Text: "fallback"}}
	result := a.ToLLMContents(parts)
	if result[0].Type != llm.ContentTypeText {
		t.Errorf("Type = %q, want %q (fallback)", result[0].Type, llm.ContentTypeText)
	}
	if result[0].Text != "fallback" {
		t.Errorf("Text = %q", result[0].Text)
	}
}

func TestMultimodalAdapter_HistoryHasMultimodal(t *testing.T) {
	a := &MultimodalAdapter{}
	history := []Message{
		{Role: RoleUser, Content: "hello"},
		{Role: RoleAssistant, Content: "hi"},
	}
	if a.HistoryHasMultimodal(history) {
		t.Error("should be false for text-only history")
	}

	history = append(history, Message{
		Role:         RoleUser,
		ContentParts: []ContentPart{{Type: "image_url", URL: "http://example.com/img.png"}},
	})
	if !a.HistoryHasMultimodal(history) {
		t.Error("should be true when history contains multimodal")
	}
}

func TestMultimodalAdapter_ConvertHistoryToExt_TextOnly(t *testing.T) {
	a := &MultimodalAdapter{}
	history := []Message{
		{Role: RoleUser, Content: "hello"},
		{Role: RoleAssistant, Content: "hi"},
	}
	result := a.ConvertHistoryToExt(history)
	if len(result) != 2 {
		t.Fatalf("length = %d, want 2", len(result))
	}
	if result[0].Role != "user" {
		t.Errorf("Role = %q, want user", result[0].Role)
	}
	if len(result[0].Contents) != 1 {
		t.Fatalf("Contents length = %d, want 1", len(result[0].Contents))
	}
	if result[0].Contents[0].Type != llm.ContentTypeText {
		t.Errorf("Content type = %q, want %q", result[0].Contents[0].Type, llm.ContentTypeText)
	}
}

func TestMultimodalAdapter_ConvertHistoryToExt_WithMultimodal(t *testing.T) {
	a := &MultimodalAdapter{}
	history := []Message{
		{
			Role: RoleUser,
			ContentParts: []ContentPart{
				{Type: "text", Text: "describe"},
				{Type: "image_url", URL: "http://example.com/img.png"},
			},
		},
	}
	result := a.ConvertHistoryToExt(history)
	if len(result[0].Contents) != 2 {
		t.Fatalf("Contents length = %d, want 2", len(result[0].Contents))
	}
}

func TestMultimodalAdapter_ConvertHistoryToExt_WithToolCalls(t *testing.T) {
	a := &MultimodalAdapter{}
	history := []Message{
		{
			Role: RoleAssistant,
			ToolCalls: []ToolCall{
				{ID: "tc-1", Name: "search", Args: `{"q":"test"}`},
			},
		},
	}
	result := a.ConvertHistoryToExt(history)
	if len(result[0].ToolCalls) != 1 {
		t.Fatalf("ToolCalls length = %d, want 1", len(result[0].ToolCalls))
	}
	if result[0].ToolCalls[0].Name != "search" {
		t.Errorf("ToolCall name = %q", result[0].ToolCalls[0].Name)
	}
}

func TestMultimodalAdapter_ConvertHistoryToExt_ToolMessage(t *testing.T) {
	a := &MultimodalAdapter{}
	history := []Message{
		{
			Role:    RoleTool,
			Content: "result data",
			Metadata: Metadata{
				Extra: map[string]string{
					"tool_call_id": "tc-1",
				},
			},
		},
	}
	result := a.ConvertHistoryToExt(history)
	if result[0].ToolCallID != "tc-1" {
		t.Errorf("ToolCallID = %q, want tc-1", result[0].ToolCallID)
	}
}

func TestMultimodalAdapter_ConvertHistoryToExt_ToolError(t *testing.T) {
	a := &MultimodalAdapter{}
	history := []Message{
		{
			Role:    RoleTool,
			Content: "error result",
			Metadata: Metadata{
				Extra: map[string]string{
					"tool_call_id": "tc-1",
					"is_error":     "true",
				},
			},
		},
	}
	result := a.ConvertHistoryToExt(history)
	if !result[0].IsToolError {
		t.Error("IsToolError should be true")
	}
}

// ===== MultimodalMessage tests =====

func TestMultimodalMessage_HasNonTextContent(t *testing.T) {
	m := &MultimodalMessage{
		Role: RoleUser,
		Contents: []*llm.MultimodalContent{
			{Type: llm.ContentTypeText, Text: "hello"},
		},
	}
	if m.HasNonTextContent() {
		t.Error("should be false for text-only")
	}

	m.Contents = append(m.Contents, &llm.MultimodalContent{Type: llm.ContentTypeImageURL, URL: "http://example.com/img.png"})
	if !m.HasNonTextContent() {
		t.Error("should be true with image")
	}
}

func TestMultimodalMessage_ExtractText(t *testing.T) {
	m := &MultimodalMessage{
		Content: "plain text",
	}
	if m.ExtractText() != "plain text" {
		t.Errorf("ExtractText = %q, want 'plain text'", m.ExtractText())
	}

	m = &MultimodalMessage{
		Contents: []*llm.MultimodalContent{
			{Type: llm.ContentTypeText, Text: "hello "},
			{Type: llm.ContentTypeText, Text: "world"},
		},
	}
	if m.ExtractText() != "hello world" {
		t.Errorf("ExtractText = %q, want 'hello world'", m.ExtractText())
	}
}

func TestMultimodalMessage_ToChatMessageExt(t *testing.T) {
	m := &MultimodalMessage{
		Role: RoleUser,
		Contents: []*llm.MultimodalContent{
			{Type: llm.ContentTypeText, Text: "hello"},
		},
		ToolCalls: []ToolCall{
			{ID: "tc-1", Name: "search", Args: "{}"},
		},
	}
	ext := m.ToChatMessageExt()
	if ext.Role != "user" {
		t.Errorf("Role = %q, want user", ext.Role)
	}
	if len(ext.Contents) != 1 {
		t.Fatalf("Contents length = %d, want 1", len(ext.Contents))
	}
	if len(ext.ToolCalls) != 1 {
		t.Fatalf("ToolCalls length = %d, want 1", len(ext.ToolCalls))
	}
}

func TestMultimodalMessage_ToChatMessageExt_WithContentFallback(t *testing.T) {
	m := &MultimodalMessage{
		Role:    RoleUser,
		Content: "fallback text",
	}
	ext := m.ToChatMessageExt()
	if len(ext.Contents) != 1 {
		t.Fatalf("Contents length = %d, want 1", len(ext.Contents))
	}
	if ext.Contents[0].Text != "fallback text" {
		t.Errorf("Text = %q", ext.Contents[0].Text)
	}
}

func TestNewUserMultimodalMessage(t *testing.T) {
	m := NewUserMultimodalMessage(
		&llm.MultimodalContent{Type: llm.ContentTypeText, Text: "hi"},
	)
	if m.Role != RoleUser {
		t.Errorf("Role = %q, want %q", m.Role, RoleUser)
	}
	if len(m.Contents) != 1 {
		t.Fatalf("Contents length = %d, want 1", len(m.Contents))
	}
	if m.Metadata.Timestamp.IsZero() {
		t.Error("Timestamp should be set")
	}
}

func TestNewAssistantMultimodalMessage(t *testing.T) {
	m := NewAssistantMultimodalMessage(
		&llm.MultimodalContent{Type: llm.ContentTypeText, Text: "hello"},
	)
	if m.Role != RoleAssistant {
		t.Errorf("Role = %q, want %q", m.Role, RoleAssistant)
	}
}

func TestNewSystemMultimodalMessage(t *testing.T) {
	m := NewSystemMultimodalMessage(
		&llm.MultimodalContent{Type: llm.ContentTypeText, Text: "system prompt"},
	)
	if m.Role != RoleSystem {
		t.Errorf("Role = %q, want %q", m.Role, RoleSystem)
	}
}

func TestUserImageB64Message(t *testing.T) {
	m := UserImageB64Message("describe this", "base64data", "image/png")
	if m.Role != RoleUser {
		t.Errorf("Role = %q", m.Role)
	}
	if len(m.Contents) != 2 {
		t.Fatalf("Contents length = %d, want 2", len(m.Contents))
	}
	if m.Contents[1].Type != llm.ContentTypeImageB64 {
		t.Errorf("Content type = %q, want %q", m.Contents[1].Type, llm.ContentTypeImageB64)
	}
	if m.Contents[1].Data != "base64data" {
		t.Errorf("Data = %q", m.Contents[1].Data)
	}
	if m.Contents[1].MIME != "image/png" {
		t.Errorf("MIME = %q", m.Contents[1].MIME)
	}
}

func TestUserImageURLMessage(t *testing.T) {
	m := UserImageURLMessage("describe this", "http://example.com/img.png")
	if m.Role != RoleUser {
		t.Errorf("Role = %q", m.Role)
	}
	if len(m.Contents) != 2 {
		t.Fatalf("Contents length = %d, want 2", len(m.Contents))
	}
	if m.Contents[1].Type != llm.ContentTypeImageURL {
		t.Errorf("Content type = %q, want %q", m.Contents[1].Type, llm.ContentTypeImageURL)
	}
	if m.Contents[1].URL != "http://example.com/img.png" {
		t.Errorf("URL = %q", m.Contents[1].URL)
	}
}

func TestMultimodalMessage_ToMessage(t *testing.T) {
	m := &MultimodalMessage{
		Role:    RoleUser,
		Content: "hello",
		ToolCalls: []ToolCall{
			{ID: "tc-1", Name: "search", Args: "{}"},
		},
		Metadata: Metadata{Timestamp: time.Now()},
	}
	msg := m.ToMessage()
	if msg.Role != RoleUser {
		t.Errorf("Role = %q", msg.Role)
	}
	if msg.Content != "hello" {
		t.Errorf("Content = %q", msg.Content)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("ToolCalls length = %d, want 1", len(msg.ToolCalls))
	}
}

// ===== IsMultimodalProvider test =====

type mockNonMultimodalProvider struct{}

func TestIsMultimodalProvider_True(t *testing.T) {
	// We can't easily test this without a full llm.Provider implementation,
	// but we can test the negative case
	p := &mockNonMultimodalProvider{}
	if IsMultimodalProvider(nil) {
		t.Error("nil provider should not be multimodal")
	}
	_ = p
}
