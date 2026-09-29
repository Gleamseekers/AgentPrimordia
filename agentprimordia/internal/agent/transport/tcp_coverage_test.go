package transport

import (
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/bus"
	"context"
	"net"
	"testing"
	"time"
)

// ===== TCPTransport Send 测试 =====

// TestTCPTransportSendNotStarted 测试未启动时发送
func TestTCPTransportSendNotStarted(t *testing.T) {
	tr := NewTCPTransport()
	msg := &bus.BusMessage{ID: "1", From: "a", To: "b", Type: bus.BusMsgTaskRequest}
	err := tr.Send(context.Background(), "127.0.0.1:12345", msg)
	if err == nil {
		t.Fatal("未启动时发送应该返回错误")
	}
}

// TestTCPTransportSendWithAckNotStarted 测试未启动时 SendWithAck
func TestTCPTransportSendWithAckNotStarted(t *testing.T) {
	tr := NewTCPTransport()
	msg := &bus.BusMessage{ID: "1", From: "a", To: "b", Type: bus.BusMsgTaskRequest}
	err := tr.SendWithAck(context.Background(), "127.0.0.1:12345", msg)
	if err == nil {
		t.Fatal("未启动时 SendWithAck 应该返回错误")
	}
}

// TestTCPTransportSendReceive 测试 TCP 完整发送/接收流程
func TestTCPTransportSendReceive(t *testing.T) {
	server := NewTCPTransport()
	if err := server.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("启动服务端失败: %v", err)
	}
	defer server.Close()

	client := NewTCPTransport()
	if err := client.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("启动客户端失败: %v", err)
	}
	defer client.Close()

	msg := &bus.BusMessage{
		ID:        "tcp-msg-1",
		From:      "client",
		To:        "server",
		Type:      bus.BusMsgTaskRequest,
		Content:   "hello tcp",
		Timestamp: time.Now(),
	}

	if err := client.Send(context.Background(), server.Addr(), msg); err != nil {
		t.Fatalf("TCP 发送失败: %v", err)
	}

	select {
	case received := <-server.Receive():
		if received.ID != "tcp-msg-1" {
			t.Fatalf("期望消息 ID %q，实际为 %q", "tcp-msg-1", received.ID)
		}
		if received.Content != "hello tcp" {
			t.Fatalf("期望内容 %q，实际为 %q", "hello tcp", received.Content)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("TCP 接收消息超时")
	}
}

// TestTCPTransportSendWithAck 测试带 ACK 确认的发送
func TestTCPTransportSendWithAck(t *testing.T) {
	server := NewTCPTransport()
	if err := server.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("启动服务端失败: %v", err)
	}
	defer server.Close()

	client := NewTCPTransportWithConfig(TCPTransportConfig{
		AckTimeout:    3 * time.Second,
		MaxRetries:    2,
		RetryInterval: 100 * time.Millisecond,
		PoolSize:      4,
	})
	if err := client.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("启动客户端失败: %v", err)
	}
	defer client.Close()

	msg := &bus.BusMessage{
		ID:        "ack-msg-1",
		From:      "client",
		To:        "server",
		Type:      bus.BusMsgTaskRequest,
		Content:   "need ack",
		Timestamp: time.Now(),
	}

	// SendWithAck 应该成功（服务端 handleConn 会自动回复 ACK）
	if err := client.SendWithAck(context.Background(), server.Addr(), msg); err != nil {
		t.Fatalf("SendWithAck 失败: %v", err)
	}

	// 服务端应该收到消息
	select {
	case received := <-server.Receive():
		if received.ID != "ack-msg-1" {
			t.Fatalf("期望消息 ID %q，实际为 %q", "ack-msg-1", received.ID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("接收消息超时")
	}
}

// TestTCPTransportSendRetry 测试发送重试（目标不可达）
func TestTCPTransportSendRetry(t *testing.T) {
	client := NewTCPTransportWithConfig(TCPTransportConfig{
		AckTimeout:    1 * time.Second,
		MaxRetries:    1,
		RetryInterval: 50 * time.Millisecond,
		PoolSize:      2,
	})
	if err := client.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("启动客户端失败: %v", err)
	}
	defer client.Close()

	msg := &bus.BusMessage{
		ID:   "retry-msg",
		From: "client",
		To:   "unreachable",
		Type: bus.BusMsgTaskRequest,
	}

	// 发送到一个不存在的地址，应该失败
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := client.Send(ctx, "127.0.0.1:1", msg)
	if err == nil {
		t.Fatal("发送到不可达地址应该返回错误")
	}
}

// TestTCPTransportSendContextCanceled 测试上下文取消
func TestTCPTransportSendContextCanceled(t *testing.T) {
	client := NewTCPTransportWithConfig(TCPTransportConfig{
		AckTimeout:    5 * time.Second,
		MaxRetries:    10,
		RetryInterval: 500 * time.Millisecond,
		PoolSize:      2,
	})
	if err := client.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("启动客户端失败: %v", err)
	}
	defer client.Close()

	msg := &bus.BusMessage{
		ID:   "ctx-msg",
		From: "client",
		To:   "unreachable",
		Type: bus.BusMsgTaskRequest,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := client.Send(ctx, "127.0.0.1:1", msg)
	if err == nil {
		t.Fatal("上下文取消后发送应该返回错误")
	}
}

// TestTCPTransportMultipleMessages 测试多条消息发送（连接复用）
func TestTCPTransportMultipleMessages(t *testing.T) {
	server := NewTCPTransport()
	if err := server.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("启动服务端失败: %v", err)
	}
	defer server.Close()

	client := NewTCPTransport()
	if err := client.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("启动客户端失败: %v", err)
	}
	defer client.Close()

	// 发送多条消息
	for i := 0; i < 5; i++ {
		msg := &bus.BusMessage{
			ID:        "multi-" + string(rune('0'+i)),
			From:      "client",
			To:        "server",
			Type:      bus.BusMsgTaskRequest,
			Content:   "msg",
			Timestamp: time.Now(),
		}
		if err := client.Send(context.Background(), server.Addr(), msg); err != nil {
			t.Fatalf("发送第 %d 条消息失败: %v", i, err)
		}
	}

	// 接收所有消息
	received := 0
	timeout := time.After(5 * time.Second)
	for received < 5 {
		select {
		case <-server.Receive():
			received++
		case <-timeout:
			t.Fatalf("超时：只收到 %d/5 条消息", received)
		}
	}
}

// ===== connPool 补充测试 =====

// TestConnPoolInvalidate 测试连接池失效操作
func TestConnPoolInvalidate(t *testing.T) {
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
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 1024)
				for {
					if _, err := c.Read(buf); err != nil {
						return
					}
				}
			}(conn)
		}
	}()

	dialer := &net.Dialer{Timeout: 5 * time.Second}
	pool := newConnPool(4, dialer)
	defer pool.CloseAll()

	ctx := context.Background()
	target := ln.Addr().String()

	// 获取并归还连接
	conn, err := pool.Get(ctx, target)
	if err != nil {
		t.Fatalf("获取连接失败: %v", err)
	}
	pool.Put(target, conn)

	_, idle := pool.Stats()
	if idle != 1 {
		t.Fatalf("归还后空闲连接应为 1，实际为 %d", idle)
	}

	// 失效该目标的所有连接
	pool.Invalidate(target)

	_, idle = pool.Stats()
	if idle != 0 {
		t.Fatalf("Invalidate 后空闲连接应为 0，实际为 %d", idle)
	}
}

// TestConnPoolMaxSize 测试连接池大小限制
func TestConnPoolMaxSize(t *testing.T) {
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
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 1024)
				for {
					if _, err := c.Read(buf); err != nil {
						return
					}
				}
			}(conn)
		}
	}()

	dialer := &net.Dialer{Timeout: 5 * time.Second}
	pool := newConnPool(2, dialer) // 最大 2 个空闲连接
	defer pool.CloseAll()

	ctx := context.Background()
	target := ln.Addr().String()

	// 获取 3 个连接
	conns := make([]net.Conn, 3)
	for i := 0; i < 3; i++ {
		conns[i], err = pool.Get(ctx, target)
		if err != nil {
			t.Fatalf("获取连接 %d 失败: %v", i, err)
		}
	}

	// 归还所有 3 个（但池大小只有 2）
	for _, c := range conns {
		pool.Put(target, c)
	}

	_, idle := pool.Stats()
	if idle > 2 {
		t.Fatalf("空闲连接不应超过 maxSize=2，实际为 %d", idle)
	}
}

// TestConnPoolPutAfterClose 测试关闭后归还连接
func TestConnPoolPutAfterClose(t *testing.T) {
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
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 1024)
				for {
					if _, err := c.Read(buf); err != nil {
						return
					}
				}
			}(conn)
		}
	}()

	dialer := &net.Dialer{Timeout: 5 * time.Second}
	pool := newConnPool(4, dialer)

	ctx := context.Background()
	target := ln.Addr().String()

	conn, err := pool.Get(ctx, target)
	if err != nil {
		t.Fatalf("获取连接失败: %v", err)
	}

	// 关闭连接池
	pool.CloseAll()

	// 归还连接不应 panic（连接会被直接关闭）
	pool.Put(target, conn)
}

// TestTCPTransportCloseNotStarted 测试未启动时关闭
func TestTCPTransportCloseNotStarted(t *testing.T) {
	tr := NewTCPTransport()
	if err := tr.Close(); err != nil {
		t.Fatalf("未启动时关闭不应报错: %v", err)
	}
}
