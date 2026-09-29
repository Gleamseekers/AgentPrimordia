package transport

import (
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/bus"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ===== Transport 接口满足性测试 =====

// TestHTTPTransportImplementsInterface 编译期验证 HTTPTransport 实现了 Transport 接口
func TestHTTPTransportImplementsInterface(t *testing.T) {
	var _ Transport = (*HTTPTransport)(nil)
}

// TestTCPTransportImplementsInterface 编译期验证 TCPTransport 实现了 Transport 接口
func TestTCPTransportImplementsInterface(t *testing.T) {
	var _ Transport = (*TCPTransport)(nil)
}

// ===== HTTPTransport 测试 =====

// TestHTTPTransportStartClose 测试 HTTP 传输层启动和关闭
func TestHTTPTransportStartClose(t *testing.T) {
	tr := NewHTTPTransport()

	// 使用端口 0 自动分配
	if err := tr.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("启动 HTTP 传输层失败: %v", err)
	}

	addr := tr.Addr()
	if addr == "" {
		t.Fatal("启动后地址不应为空")
	}

	if err := tr.Close(); err != nil {
		t.Fatalf("关闭 HTTP 传输层失败: %v", err)
	}
}

// TestHTTPTransportDoubleStart 测试重复启动
func TestHTTPTransportDoubleStart(t *testing.T) {
	tr := NewHTTPTransport()

	if err := tr.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("首次启动失败: %v", err)
	}
	defer tr.Close()

	if err := tr.Start("127.0.0.1:0"); err == nil {
		t.Fatal("重复启动应该返回错误")
	}
}

// TestHTTPTransportSendNotStarted 测试未启动时发送
func TestHTTPTransportSendNotStarted(t *testing.T) {
	tr := NewHTTPTransport()

	msg := &bus.BusMessage{ID: "1", From: "a", To: "b", Type: bus.BusMsgTaskRequest}
	err := tr.Send(context.Background(), "127.0.0.1:12345", msg)
	if err == nil {
		t.Fatal("未启动时发送应该返回错误")
	}
}

// TestHTTPTransportCloseNotStarted 测试未启动时关闭
func TestHTTPTransportCloseNotStarted(t *testing.T) {
	tr := NewHTTPTransport()
	// 未启动时关闭不应报错
	if err := tr.Close(); err != nil {
		t.Fatalf("未启动时关闭不应报错: %v", err)
	}
}

// TestHTTPTransportSendReceive 测试完整的发送/接收流程
func TestHTTPTransportSendReceive(t *testing.T) {
	// 启动服务端
	server := NewHTTPTransport()
	if err := server.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("启动服务端失败: %v", err)
	}
	defer server.Close()

	// 启动客户端
	client := NewHTTPTransport()
	if err := client.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("启动客户端失败: %v", err)
	}
	defer client.Close()

	// 发送消息
	msg := &bus.BusMessage{
		ID:        "msg-1",
		From:      "client",
		To:        "server",
		Type:      bus.BusMsgTaskRequest,
		Content:   "hello",
		Timestamp: time.Now(),
	}

	if err := client.Send(context.Background(), server.Addr(), msg); err != nil {
		t.Fatalf("发送消息失败: %v", err)
	}

	// 接收消息
	select {
	case received := <-server.Receive():
		if received.ID != "msg-1" {
			t.Fatalf("期望消息 ID %q，实际为 %q", "msg-1", received.ID)
		}
		if received.Content != "hello" {
			t.Fatalf("期望内容 %q，实际为 %q", "hello", received.Content)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("接收消息超时")
	}
}

// TestHTTPTransportHandleInvalidMethod 测试 HTTP 非 POST 请求
func TestHTTPTransportHandleInvalidMethod(t *testing.T) {
	tr := NewHTTPTransport()
	if err := tr.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	defer tr.Close()

	url := fmt.Sprintf("http://%s%s", tr.Addr(), messageEndpoint)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET 请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("期望状态码 %d，实际为 %d", http.StatusMethodNotAllowed, resp.StatusCode)
	}
}

// TestHTTPTransportHandleInvalidBody 测试 HTTP 无效请求体
func TestHTTPTransportHandleInvalidBody(t *testing.T) {
	tr := NewHTTPTransport()
	if err := tr.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	defer tr.Close()

	url := fmt.Sprintf("http://%s%s", tr.Addr(), messageEndpoint)
	resp, err := http.Post(url, "application/json", strings.NewReader("invalid json"))
	if err != nil {
		t.Fatalf("POST 请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("期望状态码 %d，实际为 %d", http.StatusBadRequest, resp.StatusCode)
	}
}

// TestHTTPTransportAddrNotStarted 测试未启动时获取地址
func TestHTTPTransportAddrNotStarted(t *testing.T) {
	tr := NewHTTPTransport()
	addr := tr.Addr()
	if addr != "" {
		t.Fatalf("未启动时地址应为空，实际为 %q", addr)
	}
}

// TestHTTPTransportWithTLS 测试 TLS 配置方法
func TestHTTPTransportWithTLS(t *testing.T) {
	tr := NewHTTPTransport()
	// 配置 TLS 但不实际使用（因为需要证书）
	result := tr.WithTLS(nil)
	if result != tr {
		t.Fatal("WithTLS 应该返回自身以支持链式调用")
	}
}

// TestHTTPTransportSendToInvalidTarget 测试向不可达目标发送
func TestHTTPTransportSendToInvalidTarget(t *testing.T) {
	tr := NewHTTPTransport()
	if err := tr.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	defer tr.Close()

	msg := &bus.BusMessage{ID: "1", From: "a", To: "b", Type: bus.BusMsgTaskRequest}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := tr.Send(ctx, "127.0.0.1:1", msg)
	if err == nil {
		t.Fatal("向不可达目标发送应该返回错误")
	}
}

// ===== ConnPool 测试 =====

// TestConnPoolCreation 测试连接池创建
func TestConnPoolCreation(t *testing.T) {
	pool := NewConnPool(10, 90*time.Second)
	if pool == nil {
		t.Fatal("NewConnPool 不应返回 nil")
	}
	if pool.MaxIdleConns != 10 {
		t.Fatalf("MaxIdleConns = %d, want 10", pool.MaxIdleConns)
	}
	if pool.MaxIdleConnsPerHost != 10 {
		t.Fatalf("MaxIdleConnsPerHost = %d, want 10", pool.MaxIdleConnsPerHost)
	}
	if pool.IdleConnTimeout != 90*time.Second {
		t.Fatalf("IdleConnTimeout = %v, want 90s", pool.IdleConnTimeout)
	}
	tr := pool.Transport()
	if tr == nil {
		t.Fatal("Transport() 不应返回 nil")
	}
}

// TestConnPoolStats 测试连接池统计信息
func TestConnPoolStats(t *testing.T) {
	pool := NewConnPool(10, 90*time.Second)

	// 初始状态全为 0
	stats := pool.Stats()
	if stats.Active != 0 {
		t.Fatalf("初始 Active = %d, want 0", stats.Active)
	}
	if stats.Idle != 0 {
		t.Fatalf("初始 Idle = %d, want 0", stats.Idle)
	}
	if stats.Wait != 0 {
		t.Fatalf("初始 Wait = %d, want 0", stats.Wait)
	}
}

// TestConnPoolWithHTTPTransport 测试连接池与 HTTPTransport 集成
func TestConnPoolWithHTTPTransport(t *testing.T) {
	// 启动一个测试 HTTP 服务器
	var requestCount atomic.Int32
	server := &http.Server{
		Addr: "127.0.0.1:0",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount.Add(1)
			w.WriteHeader(http.StatusOK)
		}),
	}
	ln, err := net.Listen("tcp", server.Addr)
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	go func() { _ = server.Serve(ln) }()
	defer server.Close()

	// 使用连接池发送请求
	pool := NewConnPool(10, 90*time.Second)
	tr := NewHTTPTransport().WithConnPool(pool)
	if err := tr.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("启动 HTTP 传输层失败: %v", err)
	}
	defer tr.Close()

	// 发送多条消息验证连接复用
	target := ln.Addr().String()
	for i := 0; i < 5; i++ {
		msg := &bus.BusMessage{
			ID:      fmt.Sprintf("msg-%d", i),
			From:    "client",
			To:      "server",
			Type:    bus.BusMsgTaskRequest,
			Content: "ping",
		}
		if err := tr.Send(context.Background(), target, msg); err != nil {
			t.Fatalf("发送消息 %d 失败: %v", i, err)
		}
	}

	if requestCount.Load() != 5 {
		t.Fatalf("期望收到 5 个请求，实际 %d", requestCount.Load())
	}
}

// TestConnPoolClose 测试连接池关闭
func TestConnPoolClose(t *testing.T) {
	pool := NewConnPool(10, 90*time.Second)
	if err := pool.Close(); err != nil {
		t.Fatalf("Close() 失败: %v", err)
	}
}

// TestConnPoolConcurrentUse 测试连接池并发安全
func TestConnPoolConcurrentUse(t *testing.T) {
	// 启动测试服务器
	server := &http.Server{
		Addr: "127.0.0.1:0",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	}
	ln, err := net.Listen("tcp", server.Addr)
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	go func() { _ = server.Serve(ln) }()
	defer server.Close()

	pool := NewConnPool(20, 90*time.Second)
	tr := NewHTTPTransport().WithConnPool(pool)
	if err := tr.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("启动 HTTP 传输层失败: %v", err)
	}
	defer tr.Close()

	target := ln.Addr().String()

	// 并发发送
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			msg := &bus.BusMessage{
				ID:      fmt.Sprintf("concurrent-%d", i),
				From:    "client",
				To:      "server",
				Type:    bus.BusMsgTaskRequest,
				Content: "ping",
			}
			if err := tr.Send(context.Background(), target, msg); err != nil {
				t.Errorf("并发发送 %d 失败: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
}

// TestHTTPTransportSendReceiveWithConnPool 测试使用连接池的完整收发流程
func TestHTTPTransportSendReceiveWithConnPool(t *testing.T) {
	server := NewHTTPTransport()
	if err := server.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("启动服务端失败: %v", err)
	}
	defer server.Close()

	pool := NewConnPool(10, 90*time.Second)
	client := NewHTTPTransport().WithConnPool(pool)
	if err := client.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("启动客户端失败: %v", err)
	}
	defer client.Close()

	msg := &bus.BusMessage{
		ID:        "pool-msg-1",
		From:      "client",
		To:        "server",
		Type:      bus.BusMsgTaskRequest,
		Content:   "pooledhello",
		Timestamp: time.Now(),
	}

	if err := client.Send(context.Background(), server.Addr(), msg); err != nil {
		t.Fatalf("发送消息失败: %v", err)
	}

	select {
	case received := <-server.Receive():
		if received.ID != "pool-msg-1" {
			t.Fatalf("期望消息 ID %q，实际为 %q", "pool-msg-1", received.ID)
		}
		if received.Content != "pooledhello" {
			t.Fatalf("期望内容 %q，实际为 %q", "pooledhello", received.Content)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("接收消息超时")
	}
}

// ===== TCPTransport 测试 =====

// TestTCPTransportStartClose 测试 TCP 传输层启动和关闭
func TestTCPTransportStartClose(t *testing.T) {
	tr := NewTCPTransport()
	if err := tr.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("启动 TCP 传输层失败: %v", err)
	}
	defer tr.Close()

	addr := tr.Addr()
	if addr == "" {
		t.Fatal("启动后地址不应为空")
	}
}

// TestTCPTransportAddrNotStarted 测试未启动时获取地址
func TestTCPTransportAddrNotStarted(t *testing.T) {
	tr := NewTCPTransport()
	addr := tr.Addr()
	if addr != "" {
		t.Fatalf("未启动时地址应为空，实际为 %q", addr)
	}
}

// TestTCPTransportDoubleStart 测试重复启动
func TestTCPTransportDoubleStart(t *testing.T) {
	tr := NewTCPTransport()
	if err := tr.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("首次启动失败: %v", err)
	}
	defer tr.Close()

	if err := tr.Start("127.0.0.1:0"); err == nil {
		t.Fatal("重复启动应该返回错误")
	}
}

// TestTCPTransportPoolStats 测试连接池统计
func TestTCPTransportPoolStats(t *testing.T) {
	tr := NewTCPTransport()
	active, idle := tr.PoolStats()
	if active != 0 || idle != 0 {
		t.Fatalf("未启动时连接池统计应为 0,0，实际为 %d,%d", active, idle)
	}
}

// ===== connPool 测试 =====

// TestConnPoolBasic 测试连接池基本操作
func TestConnPoolBasic(t *testing.T) {
	// 先启动一个 TCP 服务
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// 简单回写
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}(conn)
		}
	}()

	dialer := &net.Dialer{Timeout: 5 * time.Second}
	pool := newConnPool(4, dialer)
	defer pool.CloseAll()

	ctx := context.Background()
	target := ln.Addr().String()

	// 获取连接
	conn, err := pool.Get(ctx, target)
	if err != nil {
		t.Fatalf("获取连接失败: %v", err)
	}

	// 归还连接
	pool.Put(target, conn)

	// 检查统计
	_, idle := pool.Stats()
	if idle != 1 {
		t.Fatalf("归还后空闲连接应为 1，实际为 %d", idle)
	}
}

// TestConnPoolCloseAll 测试关闭连接池
func TestConnPoolCloseAll(t *testing.T) {
	pool := newConnPool(4, &net.Dialer{Timeout: 5 * time.Second})
	pool.CloseAll()

	ctx := context.Background()
	_, err := pool.Get(ctx, "127.0.0.1:1")
	if err == nil {
		t.Fatal("连接池关闭后获取连接应该返回错误")
	}
}

// ===== 辅助测试 =====

// TestHTTPTransportHandleConnInvalidData 测试 TCP 处理无效数据
func TestTCPTransportHandleConnInvalidData(t *testing.T) {
	server := NewTCPTransport()
	if err := server.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("启动服务端失败: %v", err)
	}
	defer server.Close()

	// 手动连接并发无效 JSON
	conn, err := net.DialTimeout("tcp", server.Addr(), 2*time.Second)
	if err != nil {
		t.Fatalf("连接服务端失败: %v", err)
	}
	defer conn.Close()

	// 发送无效 JSON（应该被忽略，不会崩溃）
	_, _ = conn.Write([]byte("invalid json\n"))

	// 发送有效 JSON
	validMsg := bus.BusMessage{
		ID:        "valid-1",
		From:      "raw-client",
		To:        "server",
		Type:      bus.BusMsgQuery,
		Content:   "valid content",
		Timestamp: time.Now(),
	}
	data, _ := json.Marshal(validMsg)
	_, _ = conn.Write(append(data, '\n'))

	// 应该只收到有效消息
	select {
	case received := <-server.Receive():
		if received.ID != "valid-1" {
			t.Fatalf("期望收到有效消息 ID %q，实际为 %q", "valid-1", received.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("接收有效消息超时")
	}
}
