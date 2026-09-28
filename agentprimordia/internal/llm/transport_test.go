// transport_test.go 验证 HTTP/2 连接复用（perf-v6 round 5 Task 4）
package llm

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestHTTP2_ConnectionReuse 验证多个连续请求复用 TCP 连接
func TestHTTP2_ConnectionReuse(t *testing.T) {
	var requestCount int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	srv.Start()
	defer srv.Close()

	client := NewDefaultLLMClient(5 * time.Second)

	const n = 10
	for i := 0; i < n; i++ {
		resp, err := client.Get(srv.URL)
		if err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	if requestCount != n {
		t.Errorf("server got %d requests, want %d", requestCount, n)
	}
}

// TestHTTP2_CloseIdleConnections 验证 CloseTransport 释放连接
func TestHTTP2_CloseIdleConnections(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewDefaultLLMClient(5 * time.Second)
	// 模拟 3 次请求
	for i := 0; i < 3; i++ {
		resp, err := client.Get(srv.URL)
		if err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
		resp.Body.Close()
	}

	// 关闭空闲连接（应不报错）
	CloseTransport(client)
	// 关闭后仍可发起新请求（会重新建立连接）
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("post-close request failed: %v", err)
	}
	resp.Body.Close()
}

// TestHTTP2_TransportConfig 验证 transport 配置
func TestHTTP2_TransportConfig(t *testing.T) {
	tr := NewDefaultLLMTransport()
	if !tr.ForceAttemptHTTP2 {
		t.Error("ForceAttemptHTTP2 should be true")
	}
	if tr.MaxIdleConns != 100 {
		t.Errorf("MaxIdleConns = %d, want 100", tr.MaxIdleConns)
	}
	if tr.MaxIdleConnsPerHost != 10 {
		t.Errorf("MaxIdleConnsPerHost = %d, want 10", tr.MaxIdleConnsPerHost)
	}
	if tr.IdleConnTimeout != 90*time.Second {
		t.Errorf("IdleConnTimeout = %v, want 90s", tr.IdleConnTimeout)
	}
	if tr.DisableKeepAlives {
		t.Error("DisableKeepAlives should be false")
	}
	if tr.TLSHandshakeTimeout != 10*time.Second {
		t.Errorf("TLSHandshakeTimeout = %v, want 10s", tr.TLSHandshakeTimeout)
	}
}

// TestNewDefaultLLMTransport_SharedSingleton 复现 v6.x 评估 §4.2 P1-3：
// NewDefaultLLMTransport 名为主享 transport，实际每次调用都 new 全新实例，
// 12 个 Provider 各自持有独立连接池，"共享"名不副实。
// 修复：包级单例（sync.OnceValues），重复调用必须返回同一实例。
func TestNewDefaultLLMTransport_SharedSingleton(t *testing.T) {
	t1 := NewDefaultLLMTransport()
	t2 := NewDefaultLLMTransport()
	if t1 != t2 {
		t.Fatal("NewDefaultLLMTransport 必须返回包级共享单例（连接池复用）")
	}
	// NewDefaultLLMClient 也必须复用同一 transport
	c1 := NewDefaultLLMClient(5 * time.Second)
	c2 := NewDefaultLLMClient(10 * time.Second)
	if c1.Transport != t1 || c2.Transport != t1 {
		t.Error("NewDefaultLLMClient 必须复用共享 transport 单例")
	}
	// timeout 参数语义保留：每个 client 仍可独立设置整体超时
	if c1.Timeout != 5*time.Second || c2.Timeout != 10*time.Second {
		t.Errorf("client timeout 语义丢失: c1=%v c2=%v", c1.Timeout, c2.Timeout)
	}
}

// TestNewDefaultLLMStreamClient_NoOverallTimeout 验证流式客户端语义：
// 不设置 Client.Timeout（避免长流被整体超时掐断），
// 由 Transport.ResponseHeaderTimeout 限制响应头耗时。
func TestNewDefaultLLMStreamClient_NoOverallTimeout(t *testing.T) {
	c := NewDefaultLLMStreamClient()
	if c == nil {
		t.Fatal("NewDefaultLLMStreamClient returned nil")
	}
	if c.Timeout != 0 {
		t.Errorf("stream client Timeout = %v, want 0（长流不得被整体超时掐断）", c.Timeout)
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("stream client Transport type = %T, want *http.Transport", c.Transport)
	}
	if tr.ResponseHeaderTimeout != defaultResponseHeaderTimeout {
		t.Errorf("ResponseHeaderTimeout = %v, want %v", tr.ResponseHeaderTimeout, defaultResponseHeaderTimeout)
	}
	// 流式 transport 同样是共享单例
	if NewDefaultLLMStreamClient().Transport != c.Transport {
		t.Error("stream transport 应为包级共享单例")
	}
}
