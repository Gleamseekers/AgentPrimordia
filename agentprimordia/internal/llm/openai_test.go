package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestServer(handler http.HandlerFunc) (*httptest.Server, *OpenAIProvider) {
	server := httptest.NewServer(handler)
	provider, _ := NewOpenAIProvider(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "gpt-4o-mini",
	})
	return server, provider
}

func TestOpenAIProvider_Complete_Success(t *testing.T) {
	server, provider := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}

		var reqBody map[string]any
		_ = json.NewDecoder(r.Body).Decode(&reqBody)

		resp := map[string]any{
			"id": "chatcmpl-123",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "Hello, world!",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 5,
				"total_tokens":      15,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	resp, err := provider.Complete(context.Background(), &CompletionRequest{
		Messages: []ChatMessage{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Hello, world!" {
		t.Errorf("expected 'Hello, world!', got '%s'", resp.Content)
	}
	if resp.ID != "chatcmpl-123" {
		t.Errorf("expected ID 'chatcmpl-123', got '%s'", resp.ID)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("expected 15 total tokens, got %d", resp.Usage.TotalTokens)
	}
}

func TestOpenAIProvider_Complete_WithToolCalls(t *testing.T) {
	server, provider := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id": "chatcmpl-456",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "",
						"tool_calls": []map[string]any{
							{
								"id":   "call_abc123",
								"type": "function",
								"function": map[string]any{
									"name":      "get_weather",
									"arguments": `{"city":"Beijing"}`,
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     20,
				"completion_tokens": 10,
				"total_tokens":      30,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	resp, err := provider.Complete(context.Background(), &CompletionRequest{
		Messages: []ChatMessage{{Role: "user", Content: "Weather?"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "" {
		t.Errorf("expected empty content, got '%s'", resp.Content)
	}
}

func TestOpenAIProvider_Complete_APIError(t *testing.T) {
	server, provider := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		resp := map[string]any{
			"error": map[string]any{
				"message": "Invalid request",
				"type":    "invalid_request_error",
				"code":    "invalid_request",
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	_, err := provider.Complete(context.Background(), &CompletionRequest{
		Messages: []ChatMessage{{Role: "user", Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// perf-v6 round 8 Task 3：错误现在是 *RetryableError，用 errors.As 提取内部 APIError
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError inside *RetryableError, got %T: %v", err, err)
	}
	if apiErr.Type != "invalid_request_error" {
		t.Errorf("expected type 'invalid_request_error', got '%s'", apiErr.Type)
	}
}

func TestOpenAIProvider_Complete_HTTPError(t *testing.T) {
	server, provider := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = fmt.Fprint(w, "Rate limited")
	})
	defer server.Close()

	_, err := provider.Complete(context.Background(), &CompletionRequest{
		Messages: []ChatMessage{{Role: "user", Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("expected error to contain '429', got '%s'", err.Error())
	}
}

func TestOpenAIProvider_Complete_InvalidJSON(t *testing.T) {
	server, provider := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "not json")
	})
	defer server.Close()

	_, err := provider.Complete(context.Background(), &CompletionRequest{
		Messages: []ChatMessage{{Role: "user", Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestOpenAIProvider_Stream_Basic(t *testing.T) {
	server, provider := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]any
		_ = json.NewDecoder(r.Body).Decode(&reqBody)
		if !reqBody["stream"].(bool) {
			t.Error("expected stream=true")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"id\":\"chatcmpl-1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello\"},\"finish_reason\":null}]}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"id\":\"chatcmpl-1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\" world\"},\"finish_reason\":null}]}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"id\":\"chatcmpl-1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"!\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":3,\"total_tokens\":8}}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	})
	defer server.Close()

	ch, err := provider.Stream(context.Background(), &CompletionRequest{
		Messages: []ChatMessage{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var chunks []Chunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	if len(chunks) < 3 {
		t.Fatalf("expected at least 3 chunks, got %d", len(chunks))
	}

	fullContent := ""
	for _, c := range chunks {
		fullContent += c.Content
	}
	if fullContent != "Hello world!" {
		t.Errorf("expected 'Hello world!', got '%s'", fullContent)
	}

	lastChunk := chunks[len(chunks)-1]
	if !lastChunk.Done {
		t.Error("last chunk should have Done=true")
	}
}

func TestOpenAIProvider_Stream_DoneSignal(t *testing.T) {
	server, provider := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"id\":\"chatcmpl-1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Done\"},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	})
	defer server.Close()

	ch, err := provider.Stream(context.Background(), &CompletionRequest{
		Messages: []ChatMessage{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var lastChunk Chunk
	for chunk := range ch {
		lastChunk = chunk
	}

	if !lastChunk.Done {
		t.Error("expected Done=true on last chunk")
	}
}

func TestOpenAIProvider_Stream_ContextCancel(t *testing.T) {
	server, provider := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; i < 100; i++ {
			_, _ = fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"chunk%d\"},\"finish_reason\":null}]}\n\n", i)
			w.(http.Flusher).Flush()
			time.Sleep(10 * time.Millisecond)
		}
	})
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	ch, err := provider.Stream(ctx, &CompletionRequest{
		Messages: []ChatMessage{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	chunkCount := 0
	for range ch {
		chunkCount++
	}

	if chunkCount >= 100 {
		t.Error("expected stream to be canceled early")
	}
}

func TestOpenAIProvider_CallTools_Success(t *testing.T) {
	server, provider := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]any
		_ = json.NewDecoder(r.Body).Decode(&reqBody)

		tools, ok := reqBody["tools"].([]any)
		if !ok || len(tools) == 0 {
			t.Error("expected tools in request")
		}

		resp := map[string]any{
			"id": "chatcmpl-789",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "",
						"tool_calls": []map[string]any{
							{
								"id":   "call_xyz",
								"type": "function",
								"function": map[string]any{
									"name":      "get_weather",
									"arguments": `{"city":"Shanghai"}`,
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     30,
				"completion_tokens": 15,
				"total_tokens":      45,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	resp, err := provider.CallTools(context.Background(), &ToolCallRequest{
		Messages: []ChatMessage{{Role: "user", Content: "Weather?"}},
		Tools: []ToolDefinition{
			{
				Type: "function",
				Function: FunctionDefinition{
					Name:        "get_weather",
					Description: "Get weather for a city",
					Parameters:  map[string]any{"type": "object"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "get_weather" {
		t.Errorf("expected tool name 'get_weather', got '%s'", resp.ToolCalls[0].Name)
	}
	if resp.ToolCalls[0].ID != "call_xyz" {
		t.Errorf("expected tool ID 'call_xyz', got '%s'", resp.ToolCalls[0].ID)
	}
	if resp.ToolCalls[0].Arguments != `{"city":"Shanghai"}` {
		t.Errorf("expected arguments '{\"city\":\"Shanghai\"}', got '%s'", resp.ToolCalls[0].Arguments)
	}
}

func TestOpenAIProvider_Embeddings_APIError(t *testing.T) {
	server, provider := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		resp := map[string]any{
			"error": map[string]any{
				"message": "Incorrect API key provided",
				"type":    "invalid_request_error",
				"code":    "invalid_api_key",
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	_, err := provider.Embeddings(context.Background(), []string{"hello"})
	if err == nil {
		t.Error("expected error with 401 response")
	}
}

func TestOpenAIProvider_Info(t *testing.T) {
	provider, _ := NewOpenAIProvider(Config{
		APIKey: "test-key",
		Model:  "gpt-4o",
	})

	info := provider.Info()
	if info.Name != "gpt-4o" {
		t.Errorf("expected 'gpt-4o', got '%s'", info.Name)
	}
	if info.Provider != "openai" {
		t.Errorf("expected 'openai', got '%s'", info.Provider)
	}
	if !info.SupportsTools {
		t.Error("SupportsTools should be true")
	}
	if !info.SupportsStreaming {
		t.Error("SupportsStreaming should be true")
	}
}

func TestOpenAIProvider_NewWithDefaults(t *testing.T) {
	provider, err := NewOpenAIProvider(Config{
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider.config.BaseURL != defaultBaseURL {
		t.Errorf("expected baseURL '%s', got '%s'", defaultBaseURL, provider.config.BaseURL)
	}
	if provider.config.Model != "gpt-4o-mini" {
		t.Errorf("expected model 'gpt-4o-mini', got '%s'", provider.config.Model)
	}
}

func TestOpenAIProvider_CustomBaseURL(t *testing.T) {
	provider, err := NewOpenAIProvider(Config{
		APIKey:  "test-key",
		BaseURL: "https://api.deepseek.com/v1",
		Model:   "deepseek-chat",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider.config.BaseURL != "https://api.deepseek.com/v1" {
		t.Errorf("expected baseURL 'https://api.deepseek.com/v1', got '%s'", provider.config.BaseURL)
	}
}

func TestOpenAIProvider_New_NoAPIKey(t *testing.T) {
	_, err := NewOpenAIProvider(Config{
		Model: "gpt-4o-mini",
	})
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
}

func TestOpenAIProvider_Complete_WithTemperatureAndMaxTokens(t *testing.T) {
	server, provider := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]any
		_ = json.NewDecoder(r.Body).Decode(&reqBody)

		temp, hasTemp := reqBody["temperature"]
		if !hasTemp || temp != 0.7 {
			t.Errorf("expected temperature 0.7, got %v", temp)
		}
		maxTokens, hasMax := reqBody["max_tokens"]
		if !hasMax || maxTokens != float64(100) {
			t.Errorf("expected max_tokens 100, got %v", maxTokens)
		}

		resp := map[string]any{
			"id": "chatcmpl-temp",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "Response with params",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     5,
				"completion_tokens": 3,
				"total_tokens":      8,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	resp, err := provider.Complete(context.Background(), &CompletionRequest{
		Messages:    []ChatMessage{{Role: "user", Content: "Hi"}},
		Temperature: Float64Ptr(0.7),
		MaxTokens:   100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Response with params" {
		t.Errorf("unexpected content: %s", resp.Content)
	}
}

func TestOpenAIProvider_Complete_EmptyChoices(t *testing.T) {
	server, provider := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id":      "chatcmpl-empty",
			"choices": []map[string]any{},
			"usage": map[string]any{
				"prompt_tokens":     5,
				"completion_tokens": 0,
				"total_tokens":      5,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	_, err := provider.Complete(context.Background(), &CompletionRequest{
		Messages: []ChatMessage{{Role: "user", Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
	if !strings.Contains(err.Error(), "empty choices") {
		t.Errorf("expected 'empty choices' error, got '%s'", err.Error())
	}
}

// TestOpenAIProvider_Complete_TrimsWhitespace covers sensenova 等 OpenAI 兼容
// 服务在响应开头/结尾带换行/空格的场景：provider 应在边界 trim 掉，
// 避免上游格式差异泄漏到所有调用方。
func TestOpenAIProvider_Complete_TrimsWhitespace(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"纯前导换行", "\n\n实际内容", "实际内容"},
		{"前后空白", "  实际内容  \n", "实际内容"},
		{"多行带尾随换行", "第一行\n第二行\n", "第一行\n第二行"},
		{"无空白保持原样", "正常内容", "正常内容"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			server, provider := newTestServer(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id": "chatcmpl-trim",
					"choices": []map[string]any{{
						"index": 0,
						"message": map[string]any{
							"role":    "assistant",
							"content": c.raw,
						},
						"finish_reason": "stop",
					}},
					"usage": map[string]any{"total_tokens": 1},
				})
			})
			defer server.Close()

			resp, err := provider.Complete(context.Background(), &CompletionRequest{
				Messages: []ChatMessage{{Role: "user", Content: "x"}},
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.Content != c.want {
				t.Errorf("raw=%q want=%q got=%q", c.raw, c.want, resp.Content)
			}
		})
	}
}
