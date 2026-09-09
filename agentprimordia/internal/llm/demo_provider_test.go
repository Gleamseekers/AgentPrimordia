package llm

import (
	"context"
	"strings"
	"testing"
)

func TestDemoProvider_Complete(t *testing.T) {
	dp := NewDemoProvider()

	tests := []struct {
		name     string
		input    string
		contains []string
	}{
		{
			name:     "问候",
			input:    "hello",
			contains: []string{"你好", "AgentPrimordia"},
		},
		{
			name:     "中文问候",
			input:    "你好",
			contains: []string{"你好", "AgentPrimordia"},
		},
		{
			name:     "代码审查",
			input:    "review this code: func add(a, b int) int { return a + b }",
			contains: []string{"代码", "函数"},
		},
		{
			name:     "帮助请求",
			input:    "help me with a task",
			contains: []string{"能力", "工具"},
		},
		{
			name:     "通用问题",
			input:    "what can you do?",
			contains: []string{"能力", "工具"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &CompletionRequest{
				Messages: []ChatMessage{
					{Role: "user", Content: tt.input},
				},
			}
			resp, err := dp.Complete(context.Background(), req)
			if err != nil {
				t.Fatalf("Complete() error = %v", err)
			}
			if resp == nil {
				t.Fatal("Complete() returned nil response")
			}
			if resp.Content == "" {
				t.Fatal("Complete() returned empty content")
			}
			for _, substr := range tt.contains {
				if !strings.Contains(resp.Content, substr) {
					t.Errorf("Complete() content missing %q, got: %s", substr, resp.Content)
				}
			}
			if resp.Role != "assistant" {
				t.Errorf("Complete() role = %q, want %q", resp.Role, "assistant")
			}
			if resp.Model != "demo" {
				t.Errorf("Complete() model = %q, want %q", resp.Model, "demo")
			}
		})
	}
}

func TestDemoProvider_Stream(t *testing.T) {
	dp := NewDemoProvider()
	req := &CompletionRequest{
		Messages: []ChatMessage{
			{Role: "user", Content: "hello"},
		},
	}
	ch, err := dp.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	var fullContent string
	var gotDone bool
	for chunk := range ch {
		fullContent += chunk.Content
		if chunk.Done {
			gotDone = true
		}
	}
	if !gotDone {
		t.Error("Stream() never sent Done chunk")
	}
	if fullContent == "" {
		t.Error("Stream() produced empty content")
	}
}

func TestDemoProvider_CallTools(t *testing.T) {
	dp := NewDemoProvider()
	req := &ToolCallRequest{
		Messages: []ChatMessage{
			{Role: "user", Content: "list files"},
		},
		Tools: []ToolDefinition{
			{
				Type:     "function",
				Function: FunctionDefinition{Name: "list_files", Description: "列出目录文件"},
			},
		},
	}
	resp, err := dp.CallTools(context.Background(), req)
	if err != nil {
		t.Fatalf("CallTools() error = %v", err)
	}
	if resp == nil {
		t.Fatal("CallTools() returned nil response")
	}
}

func TestDemoProvider_Info(t *testing.T) {
	dp := NewDemoProvider()
	info := dp.Info()
	if info.Name != "demo" {
		t.Errorf("Info().Name = %q, want %q", info.Name, "demo")
	}
	if info.Provider != "demo" {
		t.Errorf("Info().Provider = %q, want %q", info.Provider, "demo")
	}
}

func TestDemoProvider_MultiTurn(t *testing.T) {
	dp := NewDemoProvider()
	req := &CompletionRequest{
		Messages: []ChatMessage{
			{Role: "system", Content: "You are a helpful assistant."},
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "你好！我是 AgentPrimordia 的 Demo 助手。"},
			{Role: "user", Content: "what can you do?"},
		},
	}
	resp, err := dp.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if resp.Content == "" {
		t.Error("Complete() returned empty content for multi-turn")
	}
}

func TestDemoProvider_ContextCanceled(t *testing.T) {
	dp := NewDemoProvider()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := &CompletionRequest{
		Messages: []ChatMessage{
			{Role: "user", Content: "hello"},
		},
	}
	_, err := dp.Complete(ctx, req)
	if err == nil {
		t.Error("Complete() should return error on canceled context")
	}
}
