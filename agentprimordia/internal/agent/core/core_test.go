package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/multimodal"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm"
)

// ===== Message 测试 =====

func TestUserMessage(t *testing.T) {
	t.Parallel()
	msg := UserMessage("hello world")
	if msg.Role != RoleUser {
		t.Errorf("expected RoleUser, got %s", msg.Role)
	}
	if msg.Content != "hello world" {
		t.Errorf("expected 'hello world', got %s", msg.Content)
	}
	if msg.Metadata.Timestamp.IsZero() {
		t.Error("timestamp should not be zero")
	}
}

func TestSystemMessage(t *testing.T) {
	t.Parallel()
	msg := SystemMessage("system prompt")
	if msg.Role != RoleSystem {
		t.Errorf("expected RoleSystem, got %s", msg.Role)
	}
	if msg.Content != "system prompt" {
		t.Errorf("expected 'system prompt', got %s", msg.Content)
	}
}

func TestMessage_HasMultimodal(t *testing.T) {
	t.Parallel()
	// 纯文本消息
	msg := &Message{Content: "text only"}
	if msg.HasMultimodal() {
		t.Error("text-only message should not be multimodal")
	}

	// 含非文本 ContentPart
	msg = &Message{
		ContentParts: []multimodal.ContentPart{
			{Type: "image", Text: ""},
		},
	}
	if !msg.HasMultimodal() {
		t.Error("message with image part should be multimodal")
	}

	// 仅含文本 ContentPart
	msg = &Message{
		ContentParts: []multimodal.ContentPart{
			{Type: "text", Text: "hello"},
		},
	}
	if msg.HasMultimodal() {
		t.Error("message with only text parts should not be multimodal")
	}
}

func TestMessage_TextContent(t *testing.T) {
	t.Parallel()
	// 纯 Content
	msg := &Message{Content: "plain text"}
	if msg.TextContent() != "plain text" {
		t.Errorf("expected 'plain text', got %s", msg.TextContent())
	}

	// ContentParts 优先
	msg = &Message{
		Content: "fallback",
		ContentParts: []multimodal.ContentPart{
			{Type: "text", Text: "part1"},
			{Type: "text", Text: "part2"},
		},
	}
	tc := msg.TextContent()
	if tc != "part1 part2" {
		t.Errorf("expected 'part1 part2', got %s", tc)
	}

	// ContentParts 为空时回退到 Content
	msg = &Message{Content: "fallback", ContentParts: nil}
	if msg.TextContent() != "fallback" {
		t.Errorf("expected 'fallback', got %s", msg.TextContent())
	}

	// ContentParts 全为非文本且 Content 为空
	msg = &Message{
		ContentParts: []multimodal.ContentPart{
			{Type: "image"},
		},
	}
	if msg.TextContent() != "" {
		t.Errorf("expected empty, got %s", msg.TextContent())
	}
}

// ===== ToolResult 测试 =====

func TestToolResult_ToMessage(t *testing.T) {
	t.Parallel()
	tr := &ToolResult{
		ToolCallID: "call-1",
		Content:    "result data",
		IsError:    false,
	}
	msg := tr.ToMessage()
	if msg.Role != RoleTool {
		t.Errorf("expected RoleTool, got %s", msg.Role)
	}
	if msg.Content != "result data" {
		t.Errorf("expected 'result data', got %s", msg.Content)
	}
	if msg.Metadata.Extra["tool_call_id"] != "call-1" {
		t.Errorf("expected tool_call_id 'call-1', got %s", msg.Metadata.Extra["tool_call_id"])
	}
	if _, ok := msg.Metadata.Extra["is_error"]; ok {
		t.Error("is_error should not be set for non-error results")
	}

	// 错误结果
	tr.IsError = true
	msg = tr.ToMessage()
	if msg.Metadata.Extra["is_error"] != "true" {
		t.Error("is_error should be 'true' for error results")
	}
}

// ===== Response 测试 =====

func TestResponse_ErrorCode(t *testing.T) {
	t.Parallel()
	// 无错误
	r := &Response{Error: nil}
	if r.ErrorCode() != "" {
		t.Errorf("expected empty error code, got %s", r.ErrorCode())
	}

	// 有 Code() 方法的错误
	r = &Response{Error: &codedError{code: "E001"}}
	if r.ErrorCode() != "E001" {
		t.Errorf("expected E001, got %s", r.ErrorCode())
	}

	// 普通错误
	r = &Response{Error: errors.New("plain error")}
	if r.ErrorCode() != "UNKNOWN" {
		t.Errorf("expected UNKNOWN, got %s", r.ErrorCode())
	}
}

type codedError struct {
	code string
	msg  string
}

func (e *codedError) Error() string { return e.msg }
func (e *codedError) Code() string  { return e.code }

// ===== RAG 测试 =====

func TestFormatRAGDocuments(t *testing.T) {
	t.Parallel()
	// 空列表
	if result := FormatRAGDocuments(nil); result != "" {
		t.Errorf("expected empty string, got %s", result)
	}

	// 有文档
	docs := []*RAGDocument{
		{ID: "1", Content: "content1", Score: 0.9, Role: "user"},
		{ID: "2", Content: "content2", Score: 0.7},
	}
	result := FormatRAGDocuments(docs)
	if result == "" {
		t.Error("expected non-empty result")
	}
	if !contains(result, "content1") || !contains(result, "content2") {
		t.Error("result should contain document contents")
	}
	if !contains(result, "user") {
		t.Error("result should contain role 'user'")
	}
	if !contains(result, "知识") {
		t.Error("result should contain default role '知识' for doc without Role")
	}
}

func TestRAGDocument_RAGContextForPrompt(t *testing.T) {
	t.Parallel()
	doc := &RAGDocument{
		Content: "test content",
		Score:   0.85,
		Role:    "assistant",
	}
	result := doc.RAGContextForPrompt()
	if !contains(result, "test content") {
		t.Error("result should contain content")
	}
	if !contains(result, "assistant") {
		t.Error("result should contain role")
	}

	// 无 Role 时使用默认
	doc.Role = ""
	result = doc.RAGContextForPrompt()
	if !contains(result, "知识") {
		t.Error("result should contain default role '知识'")
	}
}

// ===== RequestID 测试 =====

func TestNewRequestID(t *testing.T) {
	t.Parallel()
	id1 := NewRequestID()
	id2 := NewRequestID()
	if id1 == id2 {
		t.Error("request IDs should be unique")
	}
	if len(id1) != 32 {
		t.Errorf("expected 32 char hex, got %d", len(id1))
	}
}

func TestWithRequestID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	reqID := "test-req-123"
	ctx = WithRequestID(ctx, reqID)

	extracted := RequestIDFromCtx(ctx)
	if extracted != reqID {
		t.Errorf("expected %s, got %s", reqID, extracted)
	}
}

func TestRequestIDFromCtx_Empty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	if id := RequestIDFromCtx(ctx); id != "" {
		t.Errorf("expected empty string, got %s", id)
	}
}

// ===== Agent 接口验证 =====

func TestAgentInterface_Compliance(t *testing.T) {
	t.Parallel()
	var _ Agent = &mockAgentForTest{}
}

type mockAgentForTest struct{}

func (m *mockAgentForTest) Run(_ context.Context, _ Message) (*Response, error) {
	return &Response{}, nil
}
func (m *mockAgentForTest) StreamRun(_ context.Context, _ Message) (<-chan StreamEvent, error) {
	return nil, nil
}
func (m *mockAgentForTest) Stop() {}
func (m *mockAgentForTest) Stats() AgentStats {
	return AgentStats{}
}
func (m *mockAgentForTest) Name() string { return "mock" }

// ===== Usage / Metrics 验证 =====

func TestUsage_Fields(t *testing.T) {
	t.Parallel()
	u := Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}
	if u.TotalTokens != u.PromptTokens+u.CompletionTokens {
		t.Error("TotalTokens should equal sum of prompt and completion")
	}
}

func TestMetrics_Fields(t *testing.T) {
	t.Parallel()
	m := Metrics{
		TotalTurns:  3,
		TotalTools:  5,
		Duration:    2 * time.Second,
		LLMLatency:  500 * time.Millisecond,
		ToolLatency: 300 * time.Millisecond,
	}
	if m.TotalTurns != 3 {
		t.Errorf("expected 3 turns, got %d", m.TotalTurns)
	}
}

func TestThought_Fields(t *testing.T) {
	t.Parallel()
	th := Thought{
		Content: "I should search for information",
		ToolCalls: []ToolCall{
			{ID: "tc1", Name: "search", Args: `{"q":"test"}`},
		},
		Usage: llm.Usage{PromptTokens: 10, CompletionTokens: 5},
	}
	if th.Content == "" {
		t.Error("Content should not be empty")
	}
	if len(th.ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(th.ToolCalls))
	}
}

// ===== 辅助函数 =====

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
