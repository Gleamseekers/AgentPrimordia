
package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 功能验证 - SendWithAck：发送带ACK确认的消息，验证ACK自动回传
func TestTCPTransportAPI_SendWithAck(t *testing.T) {
	sender := NewTCPTransport()
	receiver := NewTCPTransport()

	if err := sender.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("sender 启动失败: %v", err)
	}
	defer sender.Close()

	if err := receiver.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("receiver 启动失败: %v", err)
	}
	defer receiver.Close()

	msg := &BusMessage{
		ID:        "msg-ack-ok",
		From:      "sender",
		To:        "receiver",
		Type:      BusMsgTaskRequest,
		Content:   "带ACK确认的消息",
		Timestamp: time.Now(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := sender.SendWithAck(ctx, receiver.Addr(), msg); err != nil {
		t.Fatalf("SendWithAck 失败: %v", err)
	}

	select {
	case received := <-receiver.Receive():
		if received.ID != "msg-ack-ok" {
			t.Errorf("接收消息 ID = %q, 期望 %q", received.ID, "msg-ack-ok")
		}
	case <-time.After(3 * time.Second):
		t.Error("接收消息超时")
	}
}

// 功能验证 - SendWithAck超时：向不存在的目标发送，短超时后返回错误
func TestTCPTransportAPI_SendWithAckTimeout(t *testing.T) {
	cfg := TCPTransportConfig{
		AckTimeout:    200 * time.Millisecond,
		MaxRetries:    1,
		RetryInterval: 50 * time.Millisecond,
		PoolSize:      4,
	}
	sender := NewTCPTransportWithConfig(cfg)

	if err := sender.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("sender 启动失败: %v", err)
	}
	defer sender.Close()

	msg := &BusMessage{
		ID:        "msg-ack-timeout",
		From:      "sender",
		To:        "receiver",
		Type:      BusMsgTaskRequest,
		Content:   "ACK超时测试",
		Timestamp: time.Now(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := sender.SendWithAck(ctx, "127.0.0.1:1", msg)
	if err == nil {
		t.Error("期望返回发送失败错误，但 SendWithAck 成功")
	}
}

// 功能验证 - 连接池统计：启动后PoolStats返回(0,0)
func TestTCPTransportAPI_PoolStatsAfterStart(t *testing.T) {
	tr := NewTCPTransport()

	if err := tr.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	defer func() { _ = tr.Close() }()

	active, idle := tr.PoolStats()
	if active != 0 || idle != 0 {
		t.Errorf("启动后 PoolStats = (%d, %d), 期望 (0, 0)", active, idle)
	}
}

// 功能验证 - 自定义配置：使用自定义配置创建Transport并验证功能正常
func TestTCPTransportAPI_CustomConfig(t *testing.T) {
	cfg := TCPTransportConfig{
		AckTimeout:    1 * time.Second,
		MaxRetries:    1,
		RetryInterval: 100 * time.Millisecond,
		PoolSize:      4,
	}
	sender := NewTCPTransportWithConfig(cfg)
	receiver := NewTCPTransport()

	if err := sender.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("sender 启动失败: %v", err)
	}
	defer sender.Close()

	if err := receiver.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("receiver 启动失败: %v", err)
	}
	defer receiver.Close()

	msg := &BusMessage{
		ID:        "msg-custom-cfg",
		From:      "sender",
		To:        "receiver",
		Type:      BusMsgTaskRequest,
		Content:   "自定义配置测试",
		Timestamp: time.Now(),
	}

	if err := sender.Send(context.Background(), receiver.Addr(), msg); err != nil {
		t.Fatalf("自定义配置下 Send 失败: %v", err)
	}

	select {
	case received := <-receiver.Receive():
		if received.ID != msg.ID {
			t.Errorf("ID = %q, 期望 %q", received.ID, msg.ID)
		}
		if received.Content != msg.Content {
			t.Errorf("Content = %q, 期望 %q", received.Content, msg.Content)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("等待消息超时")
	}
}

// 边界条件 - 发送到未启动的Transport：Send前未Start应返回错误
func TestTCPTransportAPI_SendBeforeStart(t *testing.T) {
	tr := NewTCPTransport()

	msg := &BusMessage{
		ID:      "msg-no-start",
		From:    "agent-1",
		To:      "agent-2",
		Type:    BusMsgTaskRequest,
		Content: "未启动发送测试",
	}

	err := tr.Send(context.Background(), "127.0.0.1:9999", msg)
	if err == nil {
		t.Error("期望返回错误，但 Send 成功")
	}
	if !strings.Contains(err.Error(), "transport not started") {
		t.Errorf("错误信息 = %q, 期望包含 %q", err.Error(), "transport not started")
	}
}

// 边界条件 - SendWithAck未启动：SendWithAck前未Start应返回错误
func TestTCPTransportAPI_SendWithAckBeforeStart(t *testing.T) {
	tr := NewTCPTransport()

	msg := &BusMessage{
		ID:      "msg-ack-no-start",
		From:    "agent-1",
		To:      "agent-2",
		Type:    BusMsgTaskRequest,
		Content: "未启动ACK发送测试",
	}

	err := tr.SendWithAck(context.Background(), "127.0.0.1:9999", msg)
	if err == nil {
		t.Error("期望返回错误，但 SendWithAck 成功")
	}
	if !strings.Contains(err.Error(), "transport not started") {
		t.Errorf("错误信息 = %q, 期望包含 %q", err.Error(), "transport not started")
	}
}

// 边界条件 - 关闭后发送：Close后Send应返回错误
func TestTCPTransportAPI_SendAfterClose(t *testing.T) {
	tr := NewTCPTransport()

	if err := tr.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}

	if err := tr.Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}

	msg := &BusMessage{
		ID:      "msg-after-close",
		From:    "agent-1",
		To:      "agent-2",
		Type:    BusMsgTaskRequest,
		Content: "关闭后发送测试",
	}

	err := tr.Send(context.Background(), "127.0.0.1:9999", msg)
	if err == nil {
		t.Error("期望返回错误，但关闭后 Send 成功")
	}
}

// 边界条件 - 双重Close：连续两次Close不应panic
func TestTCPTransportAPI_DoubleClose(t *testing.T) {
	tr := NewTCPTransport()

	if err := tr.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}

	if err := tr.Close(); err != nil {
		t.Fatalf("第一次 Close 失败: %v", err)
	}

	if err := tr.Close(); err != nil {
		t.Errorf("第二次 Close 不应返回错误, got: %v", err)
	}
}

// 边界条件 - 双重Start：连续两次Start应返回错误
func TestTCPTransportAPI_DoubleStart(t *testing.T) {
	tr := NewTCPTransport()

	if err := tr.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("第一次 Start 失败: %v", err)
	}
	defer func() { _ = tr.Close() }()

	err := tr.Start("127.0.0.1:0")
	if err == nil {
		t.Error("期望双重Start返回错误")
	}
	if !strings.Contains(err.Error(), "transport already started") {
		t.Errorf("错误信息 = %q, 期望包含 %q", err.Error(), "transport already started")
	}
}

// 并发安全 - 多发送者：10个goroutine同时向一个接收者发送消息
func TestTCPTransportAPI_MultipleSenders(t *testing.T) {
	receiver := NewTCPTransport()

	if err := receiver.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("receiver 启动失败: %v", err)
	}
	defer receiver.Close()

	sender := NewTCPTransport()

	if err := sender.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("sender 启动失败: %v", err)
	}
	defer sender.Close()

	const senderCount = 10
	const msgsPerSender = 5
	const totalMsgs = senderCount * msgsPerSender

	var sendErr atomic.Int32
	var wg sync.WaitGroup

	for i := 0; i < senderCount; i++ {
		for j := 0; j < msgsPerSender; j++ {
			wg.Add(1)
			go func(senderIdx, msgIdx int) {
				defer wg.Done()
				msg := &BusMessage{
					ID:        fmt.Sprintf("sender%d-msg%d", senderIdx, msgIdx),
					From:      fmt.Sprintf("sender-%d", senderIdx),
					To:        "receiver",
					Type:      BusMsgTaskRequest,
					Content:   fmt.Sprintf("来自 sender-%d 的消息 %d", senderIdx, msgIdx),
					Timestamp: time.Now(),
				}
				if err := sender.Send(context.Background(), receiver.Addr(), msg); err != nil {
					sendErr.Add(1)
				}
			}(i, j)
		}
	}
	wg.Wait()

	if sendErr.Load() > 0 {
		t.Errorf("%d 次并发发送失败", sendErr.Load())
	}

	received := 0
	timeout := time.After(10 * time.Second)
	for received < totalMsgs {
		select {
		case <-receiver.Receive():
			received++
		case <-timeout:
			t.Fatalf("超时: 接收到 %d/%d 条消息", received, totalMsgs)
		}
	}
}

// 并发安全 - 并发Start/Close：并发调用Start和Close不应产生数据竞争
// TestTCPTransportAPI_ConcurrentStartClose 并发 Start/Close 不 panic、不 race。
// 2026-09-28：原实现直接访问 transport 包内部字段（tr.mu/tr.ln），随
// TCPTransport 迁入 internal/agent/transport 后不可达；并发安全改由
// -race 检测器断言（CI 全量 race 即本测试的防线），此处仅保留公共 API
// 形态的冒烟 + Close 幂等性验证。
func TestTCPTransportAPI_ConcurrentStartClose(t *testing.T) {
	const iterations = 20
	var wg sync.WaitGroup

	for i := 0; i < iterations; i++ {
		tr := NewTCPTransport()
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = tr.Start("127.0.0.1:0")
		}()
		go func() {
			defer wg.Done()
			_ = tr.Close()
		}()
		wg.Wait()
		// Close 幂等：并发后再调用一次不应 panic。
		_ = tr.Close()
	}
}

// 性能 - 连续发送：顺序发送100条消息并验证全部接收
func TestTCPTransportAPI_SequentialSend(t *testing.T) {
	sender := NewTCPTransport()
	receiver := NewTCPTransport()

	if err := sender.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("sender 启动失败: %v", err)
	}
	defer sender.Close()

	if err := receiver.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("receiver 启动失败: %v", err)
	}
	defer receiver.Close()

	const totalMsgs = 100

	receivedIDs := make(map[string]bool, totalMsgs)
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		timeout := time.After(15 * time.Second)
		for {
			select {
			case msg, ok := <-receiver.Receive():
				if !ok {
					return
				}
				mu.Lock()
				receivedIDs[msg.ID] = true
				mu.Unlock()
				if len(receivedIDs) == totalMsgs {
					return
				}
			case <-timeout:
				return
			}
		}
	}()

	for i := 0; i < totalMsgs; i++ {
		msg := &BusMessage{
			ID:        fmt.Sprintf("msg-seq-%d", i),
			From:      "sender",
			To:        "receiver",
			Type:      BusMsgTaskRequest,
			Content:   fmt.Sprintf("顺序消息 %d", i),
			Timestamp: time.Now(),
		}
		if err := sender.Send(context.Background(), receiver.Addr(), msg); err != nil {
			t.Fatalf("发送顺序消息 %d 失败: %v", i, err)
		}
	}

	wg.Wait()

	mu.Lock()
	count := len(receivedIDs)
	mu.Unlock()

	if count != totalMsgs {
		mu.Lock()
		missing := make([]int, 0)
		for i := 0; i < totalMsgs; i++ {
			id := fmt.Sprintf("msg-seq-%d", i)
			if !receivedIDs[id] {
				missing = append(missing, i)
			}
		}
		mu.Unlock()
		t.Errorf("接收到 %d/%d 条消息, 缺失: %v", count, totalMsgs, missing)
	}
}

// 错误处理 - 无效目标地址：发送到无效地址应返回错误
func TestTCPTransportAPI_InvalidTarget(t *testing.T) {
	cfg := TCPTransportConfig{
		AckTimeout:    1 * time.Second,
		MaxRetries:    2,
		RetryInterval: 50 * time.Millisecond,
		PoolSize:      4,
	}
	sender := NewTCPTransportWithConfig(cfg)

	if err := sender.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("sender 启动失败: %v", err)
	}
	defer sender.Close()

	msg := &BusMessage{
		ID:        "msg-invalid-target",
		From:      "sender",
		To:        "nowhere",
		Type:      BusMsgTaskRequest,
		Content:   "无效目标测试",
		Timestamp: time.Now(),
	}

	err := sender.Send(context.Background(), "127.0.0.1:1", msg)
	if err == nil {
		t.Error("期望返回错误，但发送到无效地址成功")
	}
	if !strings.Contains(err.Error(), "send failed") {
		t.Errorf("错误信息 = %q, 期望包含 %q", err.Error(), "send failed")
	}
}

// 错误处理 - 上下文取消：使用已取消的上下文发送应返回上下文错误
func TestTCPTransportAPI_ContextCancelled(t *testing.T) {
	sender := NewTCPTransport()

	if err := sender.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("sender 启动失败: %v", err)
	}
	defer sender.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	msg := &BusMessage{
		ID:        "msg-ctx-cancel",
		From:      "sender",
		To:        "receiver",
		Type:      BusMsgTaskRequest,
		Content:   "上下文取消测试",
		Timestamp: time.Now(),
	}

	err := sender.Send(ctx, "127.0.0.1:1", msg)
	if err == nil {
		t.Error("期望返回错误，但使用已取消上下文发送成功")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("期望 context.Canceled 错误, 实际: %v", err)
	}
}
