package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestOpenAIMultimodalProvider_VisionRequest 验证 vision 请求的线路格式
func TestOpenAIMultimodalProvider_VisionRequest(t *testing.T) {
	// 捕获请求体的 mock 服务器
	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证请求路径
		if r.URL.Path != "/chat/completions" {
			t.Errorf("期望路径 /chat/completions，实际: %s", r.URL.Path)
		}
		// 验证 Authorization 头
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-key" {
			t.Errorf("期望 Authorization: Bearer test-key，实际: %s", auth)
		}
		body, _ := io.ReadAll(r.Body)
		capturedBody = body

		// 返回合法的 vision 响应
		resp := `{
			"id": "chatcmpl-vision-test",
			"choices": [{
				"index": 0,
				"message": {"role": "assistant", "content": "A bar chart showing sales data"},
				"finish_reason": "stop"
			}],
			"usage": {"prompt_tokens": 100, "completion_tokens": 10, "total_tokens": 110}
		}`
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(resp))
	}))
	defer server.Close()

	// 创建 Provider，指向 mock 服务器
	provider, err := NewOpenAIMultimodalProvider(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "gpt-4o",
	})
	if err != nil {
		t.Fatalf("创建 Provider 失败: %v", err)
	}
	// 替换为默认 HTTP client（mock 服务器不需要自定义 transport）
	provider.client = &http.Client{}

	// 构建多模态请求：文本 + 图片 URL
	req := &CompletionRequestExt{
		Messages: []*ChatMessageExt{
			NewUserMultimodalMessage(
				NewTextContent("请描述这张图片"),
				NewImageURLContent("https://example.com/chart.png"),
			),
		},
	}

	ctx := context.Background()
	resp, err := provider.CompleteMultimodal(ctx, req)
	if err != nil {
		t.Fatalf("CompleteMultimodal 失败: %v", err)
	}

	// 验证响应解析
	if resp.Content != "A bar chart showing sales data" {
		t.Errorf("期望响应内容 'A bar chart showing sales data'，实际: %q", resp.Content)
	}
	if resp.ID != "chatcmpl-vision-test" {
		t.Errorf("期望响应 ID 'chatcmpl-vision-test'，实际: %q", resp.ID)
	}
	if resp.Usage.TotalTokens != 110 {
		t.Errorf("期望 TotalTokens=110，实际: %d", resp.Usage.TotalTokens)
	}

	// 解析捕获的请求体，验证线路格式
	var reqBody map[string]any
	if err := json.Unmarshal(capturedBody, &reqBody); err != nil {
		t.Fatalf("解析请求体失败: %v", err)
	}

	// 验证 model
	if reqBody["model"] != "gpt-4o" {
		t.Errorf("期望 model=gpt-4o，实际: %v", reqBody["model"])
	}

	// 验证 messages
	messages, ok := reqBody["messages"].([]any)
	if !ok || len(messages) == 0 {
		t.Fatalf("messages 为空或格式不正确")
	}

	msg := messages[0].(map[string]any)
	// 验证 role
	if msg["role"] != "user" {
		t.Errorf("期望 role=user，实际: %v", msg["role"])
	}

	// 核心断言：content 必须是数组（不是字符串）
	content, ok := msg["content"].([]any)
	if !ok {
		t.Fatalf("content 应为数组（多模态格式），实际类型: %T", msg["content"])
	}
	if len(content) != 2 {
		t.Fatalf("content 应有 2 个部分（text + image_url），实际: %d", len(content))
	}

	// 验证文本部分
	textPart, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("content[0] 应为对象")
	}
	if textPart["type"] != "text" {
		t.Errorf("content[0].type 应为 'text'，实际: %v", textPart["type"])
	}
	if textPart["text"] != "请描述这张图片" {
		t.Errorf("content[0].text 应为 '请描述这张图片'，实际: %v", textPart["text"])
	}

	// 验证图片 URL 部分
	imagePart, ok := content[1].(map[string]any)
	if !ok {
		t.Fatalf("content[1] 应为对象")
	}
	if imagePart["type"] != "image_url" {
		t.Errorf("content[1].type 应为 'image_url'，实际: %v", imagePart["type"])
	}
	imageURL, ok := imagePart["image_url"].(map[string]any)
	if !ok {
		t.Fatalf("content[1].image_url 应为对象")
	}
	if imageURL["url"] != "https://example.com/chart.png" {
		t.Errorf("image_url.url 应为 'https://example.com/chart.png'，实际: %v", imageURL["url"])
	}
}

// TestOpenAIMultimodalProvider_VisionResponse 验证 vision 响应的解析
func TestOpenAIMultimodalProvider_VisionResponse(t *testing.T) {
	expectedContent := "这是一张展示2024年销售数据的柱状图，其中Q3销售额最高。"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := `{
			"id": "chatcmpl-vision-resp-001",
			"choices": [{
				"index": 0,
				"message": {
					"role": "assistant",
					"content": "` + expectedContent + `"
				},
				"finish_reason": "stop"
			}],
			"usage": {"prompt_tokens": 200, "completion_tokens": 25, "total_tokens": 225}
		}`
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(resp))
	}))
	defer server.Close()

	provider, err := NewOpenAIMultimodalProvider(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "gpt-4o",
	})
	if err != nil {
		t.Fatalf("创建 Provider 失败: %v", err)
	}
	provider.client = &http.Client{}

	req := &CompletionRequestExt{
		Messages: []*ChatMessageExt{
			NewUserMultimodalMessage(
				NewTextContent("分析这张图表"),
				NewImageURLContent("https://example.com/sales-chart.png", "high"),
			),
		},
	}

	ctx := context.Background()
	resp, err := provider.CompleteMultimodal(ctx, req)
	if err != nil {
		t.Fatalf("CompleteMultimodal 失败: %v", err)
	}

	// 验证响应内容
	if resp.Content != expectedContent {
		t.Errorf("期望响应 %q，实际 %q", expectedContent, resp.Content)
	}
	if resp.Role != "assistant" {
		t.Errorf("期望 role=assistant，实际: %q", resp.Role)
	}
	if resp.Model != "gpt-4o" {
		t.Errorf("期望 model=gpt-4o，实际: %q", resp.Model)
	}
	if resp.Usage.PromptTokens != 200 {
		t.Errorf("期望 PromptTokens=200，实际: %d", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 25 {
		t.Errorf("期望 CompletionTokens=25，实际: %d", resp.Usage.CompletionTokens)
	}
	if resp.Usage.TotalTokens != 225 {
		t.Errorf("期望 TotalTokens=225，实际: %d", resp.Usage.TotalTokens)
	}
}
