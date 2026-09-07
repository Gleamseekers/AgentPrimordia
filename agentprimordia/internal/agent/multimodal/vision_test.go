// Package multimodal — P5 多模态端到端 vision 测试
//
// 测试覆盖完整管线：
// multimodal.Message (ContentParts) → MultimodalAdapter → llm.CompletionRequestExt
// → OpenAIMultimodalProvider → mock OpenAI API → 响应解析
package multimodal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentprimordia/internal/llm"
)

// TestMultimodalMessage_ToLLMFormat 验证 multimodal.Message 经 Adapter 转换后
// 生成正确的 llm.CompletionRequestExt 格式（文本 + 图片 URL 混合内容）。
func TestMultimodalMessage_ToLLMFormat(t *testing.T) {
	adapter := &MultimodalAdapter{}

	// 构建包含文本和图片 URL 的多模态消息
	msg := UserMultimodalMessage(
		ContentPart{Type: "text", Text: "这张图片里有什么？"},
		ContentPart{Type: "image_url", URL: "https://example.com/cat.png", Detail: "high"},
	)

	// 转换为 ChatMessageExt（llm 层格式）
	history := []Message{msg}
	extMsgs := adapter.ConvertHistoryToExt(history)

	if len(extMsgs) != 1 {
		t.Fatalf("转换后消息数 = %d, 期望 1", len(extMsgs))
	}

	ext := extMsgs[0]

	// 校验角色
	if ext.Role != "user" {
		t.Errorf("Role = %q, 期望 user", ext.Role)
	}

	// 校验内容片段数量
	if len(ext.Contents) != 2 {
		t.Fatalf("Contents 数量 = %d, 期望 2", len(ext.Contents))
	}

	// 校验文本部分
	if ext.Contents[0].Type != llm.ContentTypeText {
		t.Errorf("Contents[0].Type = %q, 期望 %q", ext.Contents[0].Type, llm.ContentTypeText)
	}
	if ext.Contents[0].Text != "这张图片里有什么？" {
		t.Errorf("Contents[0].Text = %q, 期望 '这张图片里有什么？'", ext.Contents[0].Text)
	}

	// 校验图片 URL 部分
	if ext.Contents[1].Type != llm.ContentTypeImageURL {
		t.Errorf("Contents[1].Type = %q, 期望 %q", ext.Contents[1].Type, llm.ContentTypeImageURL)
	}
	if ext.Contents[1].URL != "https://example.com/cat.png" {
		t.Errorf("Contents[1].URL = %q", ext.Contents[1].URL)
	}
	if ext.Contents[1].Detail != "high" {
		t.Errorf("Contents[1].Detail = %q, 期望 high", ext.Contents[1].Detail)
	}

	// 组装为 CompletionRequestExt 并验证整体结构
	req := &llm.CompletionRequestExt{
		Messages:  extMsgs,
		Model:     "gpt-4o",
		MaxTokens: 1024,
	}
	if !req.HasMultimodalContent() {
		t.Error("HasMultimodalContent() 应返回 true")
	}

	// 验证降级路径：转回标准 CompletionRequest 后文本被正确提取
	stdReq := req.ToCompletionRequest()
	if stdReq.Messages[0].Content != "这张图片里有什么？" {
		t.Errorf("降级后 Content = %q, 期望 '这张图片里有什么？'", stdReq.Messages[0].Content)
	}
}

// TestMultimodalMessage_ToLLMFormat_MultipleTypes 验证 Adapter 能正确处理
// 文本 + 图片 URL + Base64 图片的混合内容。
func TestMultimodalMessage_ToLLMFormat_MultipleTypes(t *testing.T) {
	adapter := &MultimodalAdapter{}

	msg := UserMultimodalMessage(
		ContentPart{Type: "text", Text: "比较这两张图"},
		ContentPart{Type: "image_url", URL: "https://example.com/a.png"},
		ContentPart{Type: "image_b64", Data: "base64data", MIME: "image/png", Detail: "low"},
	)

	contents := adapter.ToLLMContents(msg.ContentParts)
	if len(contents) != 3 {
		t.Fatalf("Contents 数量 = %d, 期望 3", len(contents))
	}

	// 文本
	if contents[0].Type != llm.ContentTypeText || contents[0].Text != "比较这两张图" {
		t.Errorf("contents[0] 不正确: type=%q text=%q", contents[0].Type, contents[0].Text)
	}
	// 图片 URL（默认 detail=auto）
	if contents[1].Type != llm.ContentTypeImageURL || contents[1].Detail != "auto" {
		t.Errorf("contents[1] 不正确: type=%q detail=%q", contents[1].Type, contents[1].Detail)
	}
	// Base64 图片
	if contents[2].Type != llm.ContentTypeImageB64 || contents[2].Data != "base64data" {
		t.Errorf("contents[2] 不正确: type=%q data=%q", contents[2].Type, contents[2].Data)
	}
	if contents[2].MIME != "image/png" {
		t.Errorf("contents[2].MIME = %q, 期望 image/png", contents[2].MIME)
	}
	if contents[2].Detail != "low" {
		t.Errorf("contents[2].Detail = %q, 期望 low", contents[2].Detail)
	}
}

// TestMultimodal_VisionEndToEnd 端到端 vision 管线测试：
// multimodal.Message → Adapter 转换 → CompletionRequestExt → OpenAIMultimodalProvider
// → mock HTTP 服务器 → 响应解析 → 最终结果。
func TestMultimodal_VisionEndToEnd(t *testing.T) {
	// 记录请求以便校验
	var capturedBody map[string]any

	// 创建模拟 OpenAI API 服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 校验请求头
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Errorf("Authorization = %q, 期望 'Bearer test-key'", auth)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, 期望 'application/json'", ct)
		}

		// 解析并校验请求体
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Errorf("请求体解析失败: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// 校验 model
		if model, _ := capturedBody["model"].(string); model != "gpt-4o" {
			t.Errorf("model = %q, 期望 gpt-4o", model)
		}

		// 校验 messages 结构：应包含多模态 content 数组
		msgs, ok := capturedBody["messages"].([]any)
		if !ok || len(msgs) == 0 {
			t.Error("messages 为空或格式不正确")
			http.Error(w, "bad messages", http.StatusBadRequest)
			return
		}

		firstMsg, ok := msgs[0].(map[string]any)
		if !ok {
			t.Error("第一条消息格式不正确")
			http.Error(w, "bad message format", http.StatusBadRequest)
			return
		}

		// 校验 role
		if role, _ := firstMsg["role"].(string); role != "user" {
			t.Errorf("role = %q, 期望 user", role)
		}

		// 校验 content 为数组（多模态格式）
		contentParts, isArray := firstMsg["content"].([]any)
		if !isArray {
			t.Errorf("content 应为数组（多模态格式），实际类型: %T", firstMsg["content"])
			http.Error(w, "expected multimodal content array", http.StatusBadRequest)
			return
		}

		if len(contentParts) != 2 {
			t.Errorf("content 部分数量 = %d, 期望 2", len(contentParts))
		}

		// 校验第一部分为文本
		if part0, ok := contentParts[0].(map[string]any); ok {
			if part0["type"] != "text" {
				t.Errorf("part[0].type = %v, 期望 text", part0["type"])
			}
			if part0["text"] != "这张图片里有什么？" {
				t.Errorf("part[0].text = %v", part0["text"])
			}
		}

		// 校验第二部分为图片 URL
		if part1, ok := contentParts[1].(map[string]any); ok {
			if part1["type"] != "image_url" {
				t.Errorf("part[1].type = %v, 期望 image_url", part1["type"])
			}
			imageURL, _ := part1["image_url"].(map[string]any)
			if url, _ := imageURL["url"].(string); url != "https://example.com/dog.png" {
				t.Errorf("image_url.url = %q, 期望 https://example.com/dog.png", url)
			}
		}

		// 返回模拟 vision 响应
		resp := map[string]any{
			"id":      "chatcmpl-vision-e2e",
			"object":  "chat.completion",
			"created": 1700000000,
			"model":   "gpt-4o",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "图片中有一只金毛犬坐在草地上。",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]int{
				"prompt_tokens":     100,
				"completion_tokens": 15,
				"total_tokens":      115,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// --- 执行完整管线 ---

	// 1. 构建 multimodal 消息
	msg := UserMultimodalMessage(
		ContentPart{Type: "text", Text: "这张图片里有什么？"},
		ContentPart{Type: "image_url", URL: "https://example.com/dog.png", Detail: "auto"},
	)

	// 2. 通过 Adapter 转换为 llm 格式
	adapter := &MultimodalAdapter{}
	extMsgs := adapter.ConvertHistoryToExt([]Message{msg})

	// 3. 创建 OpenAI 多模态 Provider（指向 mock 服务器）
	provider, err := llm.NewOpenAIMultimodalProvider(llm.Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "gpt-4o",
	})
	if err != nil {
		t.Fatalf("创建 Provider 失败: %v", err)
	}

	// 4. 组装请求
	req := &llm.CompletionRequestExt{
		Messages:  extMsgs,
		Model:     "gpt-4o",
		MaxTokens: 1024,
	}

	// 5. 发送多模态请求
	resp, err := provider.CompleteMultimodal(context.Background(), req)
	if err != nil {
		t.Fatalf("CompleteMultimodal 失败: %v", err)
	}

	// 6. 校验响应
	if resp.Content != "图片中有一只金毛犬坐在草地上。" {
		t.Errorf("Content = %q, 期望 '图片中有一只金毛犬坐在草地上。'", resp.Content)
	}
	if resp.Role != "assistant" {
		t.Errorf("Role = %q, 期望 assistant", resp.Role)
	}
	if resp.Model != "gpt-4o" {
		t.Errorf("Model = %q, 期望 gpt-4o", resp.Model)
	}
	if resp.Usage.TotalTokens != 115 {
		t.Errorf("TotalTokens = %d, 期望 115", resp.Usage.TotalTokens)
	}
	if resp.Usage.PromptTokens != 100 {
		t.Errorf("PromptTokens = %d, 期望 100", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 15 {
		t.Errorf("CompletionTokens = %d, 期望 15", resp.Usage.CompletionTokens)
	}
}

// TestMultimodal_VisionEndToEnd_Base64Image 验证 Base64 图片的端到端管线。
func TestMultimodal_VisionEndToEnd_Base64Image(t *testing.T) {
	var capturedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)

		// 校验 base64 图片被正确转换为 data: URL
		msgs := capturedBody["messages"].([]any)
		firstMsg := msgs[0].(map[string]any)
		contentParts := firstMsg["content"].([]any)
		part1 := contentParts[1].(map[string]any)
		imageURL := part1["image_url"].(map[string]any)
		urlStr := imageURL["url"].(string)
		if !strings.HasPrefix(urlStr, "data:image/png;base64,") {
			t.Errorf("base64 图片 URL 前缀不正确: %q", urlStr[:40])
		}

		resp := map[string]any{
			"id":    "chatcmpl-b64-test",
			"model": "gpt-4o",
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"role":    "assistant",
						"content": "这是一张公司 logo 图片。",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]int{"total_tokens": 50},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// 使用便捷函数创建带 base64 图片的消息
	mmMsg := UserImageB64Message("这是什么？", "iVBORw0KGgo=", "image/png")

	// MultimodalMessage → ChatMessageExt
	chatExt := mmMsg.ToChatMessageExt()

	provider, err := llm.NewOpenAIMultimodalProvider(llm.Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "gpt-4o",
	})
	if err != nil {
		t.Fatalf("创建 Provider 失败: %v", err)
	}

	resp, err := provider.CompleteMultimodal(context.Background(), &llm.CompletionRequestExt{
		Messages: []*llm.ChatMessageExt{chatExt},
	})
	if err != nil {
		t.Fatalf("CompleteMultimodal 失败: %v", err)
	}

	if resp.Content != "这是一张公司 logo 图片。" {
		t.Errorf("Content = %q", resp.Content)
	}
}

// TestMultimodal_VisionEndToEnd_HistoryRoundTrip 验证多轮对话中多模态消息
// 经 Adapter 转换后能正确传递至 Provider。
func TestMultimodal_VisionEndToEnd_HistoryRoundTrip(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		defer r.Body.Close()
		_ = json.NewDecoder(r.Body).Decode(&body)

		msgs := body["messages"].([]any)
		// 应有 2 条消息: system + user(含图片)
		if len(msgs) != 2 {
			t.Errorf("消息数量 = %d, 期望 2", len(msgs))
		}

		// 第一条: system（纯文本）
		sysMsg := msgs[0].(map[string]any)
		if sysMsg["content"].(string) != "你是一个视觉助手" {
			t.Errorf("system content = %v", sysMsg["content"])
		}

		// 第二条: user（多模态数组）
		userMsg := msgs[1].(map[string]any)
		parts := userMsg["content"].([]any)
		if len(parts) != 2 {
			t.Errorf("user content parts = %d, 期望 2", len(parts))
		}

		resp := map[string]any{
			"id":    "chatcmpl-history",
			"model": "gpt-4o",
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"role":    "assistant",
						"content": "看到了历史图片。",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]int{"total_tokens": 80},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// 构建多轮历史
	history := []Message{
		{Role: RoleSystem, Content: "你是一个视觉助手"},
		UserMultimodalMessage(
			ContentPart{Type: "text", Text: "看看这张图"},
			ContentPart{Type: "image_url", URL: "https://example.com/hist.png"},
		),
	}

	adapter := &MultimodalAdapter{}
	extMsgs := adapter.ConvertHistoryToExt(history)

	provider, err := llm.NewOpenAIMultimodalProvider(llm.Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "gpt-4o",
	})
	if err != nil {
		t.Fatalf("创建 Provider 失败: %v", err)
	}

	resp, err := provider.CompleteMultimodal(context.Background(), &llm.CompletionRequestExt{
		Messages: extMsgs,
	})
	if err != nil {
		t.Fatalf("CompleteMultimodal 失败: %v", err)
	}

	if resp.Content != "看到了历史图片。" {
		t.Errorf("Content = %q", resp.Content)
	}
}
