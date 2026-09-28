
// http_transport_api_full_test.go — HTTP 传输层公共 API 测试。
// 2026-09-28：原文件被 //go:build ignore 禁用而腐烂（其测试的
// tr.handleMessage/messageEndpoint/inboundBufSize 内部符号已随
// HTTPTransport 迁入 internal/agent/transport 子包）。handler 级用例
// 已由 transport 包的 TestHTTPTransportHandleInvalidMethod /
// TestHTTPTransportHandleInvalidBody 覆盖（更优），此处仅保留
// 公共 API（Start/Send/Receive/Addr）级用例。
package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)






// 测试 Send 方法设置 Content-Type 为 application/json
func TestHTTPTransportAPI_ContentType(t *testing.T) {
	var receivedContentType string
	var ctMu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctMu.Lock()
		receivedContentType = r.Header.Get("Content-Type")
		ctMu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := NewHTTPTransport()
	if err := sender.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("sender 启动失败: %v", err)
	}
	defer sender.Close()

	msg := &BusMessage{
		ID:      "msg-ct",
		From:    "sender",
		To:      "receiver",
		Type:    BusMsgTaskRequest,
		Content: "Content-Type 测试",
	}

	target := server.Listener.Addr().String()
	if err := sender.Send(context.Background(), target, msg); err != nil {
		t.Fatalf("Send 失败: %v", err)
	}

	ctMu.Lock()
	ct := receivedContentType
	ctMu.Unlock()

	if ct != "application/json" {
		t.Errorf("Content-Type = %q, 期望 %q", ct, "application/json")
	}
}

// 测试多个发送者并发向单个接收者发送消息
func TestHTTPTransportAPI_MultipleSendersOneReceiver(t *testing.T) {
	receiver := NewHTTPTransport()
	if err := receiver.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("receiver 启动失败: %v", err)
	}
	defer receiver.Close()

	const senderCount = 5
	const msgsPerSender = 10
	const totalMsgs = senderCount * msgsPerSender

	senders := make([]*HTTPTransport, senderCount)
	for i := 0; i < senderCount; i++ {
		senders[i] = NewHTTPTransport()
		if err := senders[i].Start("127.0.0.1:0"); err != nil {
			t.Fatalf("sender[%d] 启动失败: %v", i, err)
		}
		defer senders[i].Close()
	}

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
				if err := senders[senderIdx].Send(context.Background(), receiver.Addr(), msg); err != nil {
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

	if received != totalMsgs {
		t.Errorf("接收到 %d 条消息, 期望 %d", received, totalMsgs)
	}
}

// 测试发送 Metadata 为 nil 的消息能正确传递
func TestHTTPTransportAPI_NilMetadata(t *testing.T) {
	sender := NewHTTPTransport()
	receiver := NewHTTPTransport()

	if err := sender.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("sender 启动失败: %v", err)
	}
	defer sender.Close()

	if err := receiver.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("receiver 启动失败: %v", err)
	}
	defer receiver.Close()

	msg := &BusMessage{
		ID:        "msg-nil-meta",
		From:      "sender",
		To:        "receiver",
		Type:      BusMsgTaskRequest,
		Content:   "无 Metadata 消息",
		Metadata:  nil,
		Timestamp: time.Now(),
	}

	if err := sender.Send(context.Background(), receiver.Addr(), msg); err != nil {
		t.Fatalf("Send 失败: %v", err)
	}

	select {
	case received := <-receiver.Receive():
		if received.ID != msg.ID {
			t.Errorf("ID = %q, 期望 %q", received.ID, msg.ID)
		}
		if received.Content != msg.Content {
			t.Errorf("Content = %q, 期望 %q", received.Content, msg.Content)
		}
		if received.Metadata != nil {
			t.Errorf("Metadata 期望为 nil, 实际 = %v", received.Metadata)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("等待消息超时")
	}
}

// 测试 Content 包含 Unicode 和特殊字符的消息能正确传递
func TestHTTPTransportAPI_SpecialCharacterContent(t *testing.T) {
	sender := NewHTTPTransport()
	receiver := NewHTTPTransport()

	if err := sender.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("sender 启动失败: %v", err)
	}
	defer sender.Close()

	if err := receiver.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("receiver 启动失败: %v", err)
	}
	defer receiver.Close()

	specialContents := []string{
		"你好世界 🌍🚀",
		"特殊字符: <>&\"'\\/\n\t",
		"日文: こんにちは 世界",
		"韩文: 안녕하세요",
		"表情: 😀🎉🔥💯",
		"混合: Hello世界🌍\n换行\t制表符",
	}

	for idx, content := range specialContents {
		msg := &BusMessage{
			ID:        fmt.Sprintf("msg-special-%d", idx),
			From:      "sender",
			To:        "receiver",
			Type:      BusMsgTaskRequest,
			Content:   content,
			Timestamp: time.Now(),
		}

		if err := sender.Send(context.Background(), receiver.Addr(), msg); err != nil {
			t.Fatalf("发送特殊字符消息 %d 失败: %v", idx, err)
		}

		select {
		case received := <-receiver.Receive():
			if received.Content != content {
				t.Errorf("消息 %d: Content = %q, 期望 %q", idx, received.Content, content)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("等待特殊字符消息 %d 超时", idx)
		}
	}
}

// 测试连续发送 50 条消息并全部正确接收
func TestHTTPTransportAPI_SequentialMessages(t *testing.T) {
	sender := NewHTTPTransport()
	receiver := NewHTTPTransport()

	if err := sender.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("sender 启动失败: %v", err)
	}
	defer sender.Close()

	if err := receiver.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("receiver 启动失败: %v", err)
	}
	defer receiver.Close()

	const totalMsgs = 50

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

	receivedIDs := make(map[string]bool, totalMsgs)
	timeout := time.After(10 * time.Second)
	for len(receivedIDs) < totalMsgs {
		select {
		case msg := <-receiver.Receive():
			receivedIDs[msg.ID] = true
		case <-timeout:
			t.Fatalf("超时: 接收到 %d/%d 条消息", len(receivedIDs), totalMsgs)
		}
	}

	for i := 0; i < totalMsgs; i++ {
		id := fmt.Sprintf("msg-seq-%d", i)
		if !receivedIDs[id] {
			t.Errorf("未接收到消息 %s", id)
		}
	}
}
