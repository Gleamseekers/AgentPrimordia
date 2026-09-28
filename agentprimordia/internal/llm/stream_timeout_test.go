package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// slowStreamServer 返回一个立即发送响应头、随后以 chunkInterval 间隔
// 缓慢推送 totalChunks 个 SSE chunk 的 httptest.Server。
// 用于验证流式路径不受 http.Client.Timeout 整体超时掐断。
func slowStreamServer(t *testing.T, totalChunks int, chunkInterval time.Duration) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for i := 0; i < totalChunks; i++ {
			_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":"x"}}]}` + "\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
			time.Sleep(chunkInterval)
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestStream_NotTruncatedByClientTimeout 复现 v6.x 评估 §4.2 P1-4：
// http.Client.Timeout（默认 120s）覆盖响应体读取全过程，长流会被整体掐断。
// 修复：流式路径改用无整体超时的 stream client（ResponseHeaderTimeout + ctx deadline）。
//
// 测试手法：把 provider 的非流式 client 整体超时压到 200ms（模拟旧配置），
// 服务器以 100ms 间隔推送 5 个 chunk（总时长 500ms > 200ms）。
// 旧代码（流式走 p.client）会在 200ms 处被掐断；新代码必须完整收到 5 个 chunk。
func TestStream_NotTruncatedByClientTimeout(t *testing.T) {
	srv := slowStreamServer(t, 5, 100*time.Millisecond)

	p, err := NewOpenAIProvider(Config{APIKey: "k", BaseURL: srv.URL, Model: "m"})
	if err != nil {
		t.Fatalf("NewOpenAIProvider: %v", err)
	}
	// 模拟旧配置：非流式 client 整体超时 200ms
	p.client.Timeout = 200 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ch, err := p.Stream(ctx, &CompletionRequest{Messages: []ChatMessage{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var got int
	for c := range ch {
		if c.Done {
			break
		}
		if c.Content != "" {
			got++
		}
	}
	if got != 5 {
		t.Errorf("received %d chunks, want 5（长流被 Client.Timeout 掐断）", got)
	}
}

// TestStream_AnthropicNotTruncatedByClientTimeout 同一回归覆盖
// BaseProvider 底座路径（Anthropic/Azure 经嵌入 BaseProvider 发请求）。
func TestStream_AnthropicNotTruncatedByClientTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for i := 0; i < 5; i++ {
			_, _ = w.Write([]byte(`data: {"type":"content_block_delta","delta":{"type":"text","text":"x"}}` + "\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
			time.Sleep(100 * time.Millisecond)
		}
		_, _ = w.Write([]byte(`data: {"type":"message_stop"}` + "\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer srv.Close()

	p, err := NewAnthropicProvider(Config{APIKey: "k", BaseURL: srv.URL, Model: "m"})
	if err != nil {
		t.Fatalf("NewAnthropicProvider: %v", err)
	}
	// 模拟旧配置：整体超时 200ms（经 BaseProvider 嵌入字段访问）
	p.client.Timeout = 200 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ch, err := p.Stream(ctx, &CompletionRequest{Messages: []ChatMessage{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var got int
	for c := range ch {
		if c.Done {
			break
		}
		if c.Content != "" {
			got++
		}
	}
	if got != 5 {
		t.Errorf("received %d chunks, want 5（长流被 Client.Timeout 掐断）", got)
	}
}

// TestStream_HeaderTimeoutStillEnforced 验证流式路径的响应头超时仍然生效：
// 服务器延迟 500ms 才发送响应头，超过 ResponseHeaderTimeout（测试临时调小）时必须失败。
func TestStream_HeaderTimeoutStillEnforced(t *testing.T) {
	var served atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served.Add(1)
		time.Sleep(500 * time.Millisecond) // 响应头延迟远超测试超时
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p, err := NewOpenAIProvider(Config{APIKey: "k", BaseURL: srv.URL, Model: "m"})
	if err != nil {
		t.Fatalf("NewOpenAIProvider: %v", err)
	}
	// 临时把 stream transport 的响应头超时调小（测试后恢复，避免污染单例）
	tr := p.streamClient.Transport.(*http.Transport)
	orig := tr.ResponseHeaderTimeout
	tr.ResponseHeaderTimeout = 50 * time.Millisecond
	defer func() { tr.ResponseHeaderTimeout = orig }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = p.Stream(ctx, &CompletionRequest{Messages: []ChatMessage{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatal("expected error when server delays response headers beyond ResponseHeaderTimeout")
	}
}
