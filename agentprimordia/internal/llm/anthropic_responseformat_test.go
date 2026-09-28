// anthropic_responseformat_test.go — ResponseFormat 边界回归（P0 修复固防）
//
// 背景：buildStructuredOutput 曾无条件解引用 rf.JSONSchema，
// ResponseFormat{Type: json_object}（OpenAI 生态最常见模式，无 schema）
// 即触发 nil pointer panic——公开 API 用一个坏参数崩溃整个进程。
package llm

import (
	"context"
	"strings"
	"testing"
)

// TestAnthropic_ResponseFormatJSONObjectNoSchema 不得 panic：
// json_object 模式无 schema 时应走通用 tool 兜底，正常发出请求。
func TestAnthropic_ResponseFormatJSONObjectNoSchema(t *testing.T) {
	p, err := NewAnthropicProvider(Config{APIKey: "test-key", BaseURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	// 指向不可达地址，请求必然失败——但失败必须是 error 而非 panic。
	_, err = p.Complete(context.Background(), &CompletionRequest{
		Messages:       []ChatMessage{{Role: "user", Content: "hi"}},
		ResponseFormat: &ResponseFormat{Type: ResponseFormatJSONObject},
	})
	if err == nil {
		t.Fatal("预期因网络失败返回 error（测试端点不可达）")
	}
	if strings.Contains(err.Error(), "nil pointer") {
		t.Fatalf("不得 panic: %v", err)
	}
}

// TestAnthropic_ResponseFormatText 无 ResponseFormat 与 text 模式均不注入 tool。
func TestAnthropic_ResponseFormatText(t *testing.T) {
	p, err := NewAnthropicProvider(Config{APIKey: "test-key", BaseURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	_, err = p.Complete(context.Background(), &CompletionRequest{
		Messages:       []ChatMessage{{Role: "user", Content: "hi"}},
		ResponseFormat: &ResponseFormat{Type: ResponseFormatText},
	})
	if err == nil {
		t.Fatal("预期因网络失败返回 error")
	}
	if strings.Contains(err.Error(), "nil pointer") {
		t.Fatalf("不得 panic: %v", err)
	}
}

// TestAnthropic_ResponseFormatJSONSchemaMissing json_schema 模式缺少
// schema 定义属无效请求，必须返回明确 error 而非 panic。
func TestAnthropic_ResponseFormatJSONSchemaMissing(t *testing.T) {
	p, err := NewAnthropicProvider(Config{APIKey: "test-key", BaseURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	_, err = p.Complete(context.Background(), &CompletionRequest{
		Messages:       []ChatMessage{{Role: "user", Content: "hi"}},
		ResponseFormat: &ResponseFormat{Type: ResponseFormatJSONSchema},
	})
	if err == nil {
		t.Fatal("预期返回 error")
	}
	if !strings.Contains(err.Error(), "json_schema") && !strings.Contains(err.Error(), "schema") {
		t.Fatalf("错误信息应指明缺少 schema: %v", err)
	}
}

// TestAnthropic_BuildStructuredOutputUnit 单测：三种 ResponseFormat 的构建行为。
func TestAnthropic_BuildStructuredOutputUnit(t *testing.T) {
	p, err := NewAnthropicProvider(Config{APIKey: "test-key", BaseURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	// json_object 无 schema → 通用兜底 tool，非 nil
	tool, choice, ok, err := p.buildStructuredOutputSafe(&ResponseFormat{Type: ResponseFormatJSONObject})
	if err != nil {
		t.Fatalf("json_object 不应报错: %v", err)
	}
	if !ok || tool.Name == "" || choice.Name == "" {
		t.Fatalf("json_object 兜底 tool 不应为空: ok=%v tool=%+v", ok, tool)
	}
	// text → 不注入
	_, _, ok, err = p.buildStructuredOutputSafe(&ResponseFormat{Type: ResponseFormatText})
	if err != nil || ok {
		t.Fatalf("text 不应注入也不应报错: ok=%v err=%v", ok, err)
	}
	// nil → 不注入
	_, _, ok, err = p.buildStructuredOutputSafe(nil)
	if err != nil || ok {
		t.Fatalf("nil 不应注入也不应报错: ok=%v err=%v", ok, err)
	}
	// json_schema 缺 schema → error
	_, _, _, err = p.buildStructuredOutputSafe(&ResponseFormat{Type: ResponseFormatJSONSchema})
	if err == nil {
		t.Fatal("json_schema 缺 schema 应报错")
	}
	// 完整 schema → 正常构建
	tool, _, ok, err = p.buildStructuredOutputSafe(&ResponseFormat{
		Type:       ResponseFormatJSONSchema,
		JSONSchema: &SchemaDef{Name: "my_schema", Schema: map[string]any{"type": "object"}},
	})
	if err != nil {
		t.Fatalf("完整 schema 不应报错: %v", err)
	}
	if !ok || tool.Name != "my_schema" {
		t.Fatalf("tool 名应为 my_schema, got %q ok=%v", tool.Name, ok)
	}
}
