package builtin

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// TestHTTPClient_共享Transport_单例 验证安全 Transport 为懒初始化单例：
// 旧实现每请求新建 Transport 且从不 CloseIdleConnections，导致连接/goroutine 泄漏。
func TestHTTPClient_共享Transport_单例(t *testing.T) {
	c := NewHTTPClient()

	first := c.httpTransport()
	if first == nil {
		t.Fatal("httpTransport 不应返回 nil")
	}
	for i := 0; i < 5; i++ {
		if got := c.httpTransport(); got != first {
			t.Fatalf("httpTransport 应返回同一实例（单例），第 %d 次获取返回了不同实例", i+1)
		}
	}

	other := NewHTTPClient()
	if other.httpTransport() == first {
		t.Error("不同 HTTPClient 实例不应共享同一 Transport")
	}
}

// TestHTTPClient_共享Transport_连接复用 验证并发多请求后 TCP 连接被复用，
// 而非每请求新建连接后泄漏。
func TestHTTPClient_共享Transport_连接复用(t *testing.T) {
	var newConns atomic.Int64
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	ts.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			newConns.Add(1)
		}
	}
	ts.Start()
	defer ts.Close()

	c := NewHTTPClient().WithAllowPrivate(true)

	const total = 10
	for i := 0; i < total; i++ {
		args, _ := json.Marshal(map[string]any{"url": ts.URL})
		result, err := c.Execute(context.Background(), args)
		if err != nil || result.IsError {
			t.Fatalf("请求 %d 失败: err=%v result=%s", i, err, result.Content)
		}
	}

	if got := newConns.Load(); got != 1 {
		t.Errorf("期望 %d 次请求复用同一条 TCP 连接（新建 1 次），实际新建 %d 次", total, got)
	}
}

// TestHTTPClient_超大请求体被拒 验证请求体大小上限：超大 body 在发起网络请求前被拒绝。
func TestHTTPClient_超大请求体被拒(t *testing.T) {
	c := NewHTTPClient().WithAllowPrivate(true)

	args, _ := json.Marshal(map[string]any{
		"url":    "http://127.0.0.1:1/",
		"method": "POST",
		"body":   strings.Repeat("a", httpClientMaxRequestBody+1),
	})
	result, err := c.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("超过上限的请求体应被拒绝")
	}
	if !strings.Contains(result.Content, "body") {
		t.Errorf("期望请求体超限提示，got: %s", result.Content)
	}
}

// TestHTTPClient_请求体上限内放行 验证未超限的请求体正常放行（不误杀）。
func TestHTTPClient_请求体上限内放行(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer ts.Close()

	c := NewHTTPClient().WithAllowPrivate(true)
	args, _ := json.Marshal(map[string]any{
		"url":    ts.URL,
		"method": "POST",
		"body":   strings.Repeat("a", 1024),
	})
	result, err := c.Execute(context.Background(), args)
	if err != nil || result.IsError {
		t.Fatalf("上限内的请求体应放行: err=%v result=%s", err, result.Content)
	}
}
