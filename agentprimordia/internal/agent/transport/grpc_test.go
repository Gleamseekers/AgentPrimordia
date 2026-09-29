package transport

import (
	"github.com/Gleamseekers/AgentPrimordia/internal/agent/bus"
	"context"
	"fmt"
	"testing"
	"time"
)

func TestGrpcTransport_SendAndReceive(t *testing.T) {
	tr := NewGrpcTransport()
	defer tr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	msg := &bus.BusMessage{
		ID:        "test-1",
		From:      "agent-a",
		To:        "agent-b",
		Type:      bus.BusMessageType("request"),
		Content:   "hello",
		Timestamp: time.Now(),
	}

	err := tr.Send(ctx, "nonexistent", msg)
	if err == nil {
		t.Error("expected error for nonexistent peer")
	}
}

func TestGrpcTransport_Receive(t *testing.T) {
	tr := NewGrpcTransport()
	defer tr.Close()

	ch := tr.Receive()
	if ch == nil {
		t.Error("expected non-nil inbox channel")
	}
}

func TestGrpcMsgSerialization(t *testing.T) {
	msg := &bus.BusMessage{
		ID:        "test-1",
		From:      "agent-a",
		To:        "agent-b",
		Type:      bus.BusMessageType("request"),
		Content:   "hello world",
		Metadata:  map[string]string{"key": "value"},
		Timestamp: time.Now(),
	}

	gm := toGrpcMsg(msg)
	restored := fromGrpcMsg(gm)

	if restored.ID != msg.ID {
		t.Errorf("ID mismatch: %s vs %s", restored.ID, msg.ID)
	}
	if restored.From != msg.From {
		t.Errorf("From mismatch")
	}
	if restored.Content != msg.Content {
		t.Errorf("Content mismatch")
	}
	if restored.Metadata["key"] != "value" {
		t.Errorf("Metadata mismatch")
	}
}

func TestGrpcTransport_StartAndStop(t *testing.T) {
	tr := NewGrpcTransport()

	err := tr.Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}

	err = tr.Close()
	if err != nil {
		t.Fatalf("Close error: %v", err)
	}
}

// TestGrpcTransport_E2E 端到端真实收发：服务端 Start 后，客户端 Send 应到达服务端 inbox。
func TestGrpcTransport_E2E(t *testing.T) {
	srv := NewGrpcTransport()
	if err := srv.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer srv.Close()

	client := NewGrpcTransport()
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	msg := &bus.BusMessage{
		ID:        "e2e-1",
		From:      "client-agent",
		To:        "server-agent",
		Type:      bus.BusMessageType("request"),
		Content:   "hello over gRPC",
		Metadata:  map[string]string{"route": "test"},
		Timestamp: time.Now(),
	}

	if err := client.Send(ctx, srv.Addr(), msg); err != nil {
		t.Fatalf("Send error: %v", err)
	}

	select {
	case got := <-srv.Receive():
		if got.ID != msg.ID || got.Content != msg.Content || got.From != msg.From {
			t.Errorf("消息不匹配: got %+v", got)
		}
		if got.Metadata["route"] != "test" {
			t.Errorf("Metadata 不匹配: %v", got.Metadata)
		}
	case <-ctx.Done():
		t.Fatal("等待入站消息超时")
	}
}

// TestGrpcTransport_SendNotStarted 未 Start 时 Send 应返回错误。
func TestGrpcTransport_SendNotStarted(t *testing.T) {
	tr := NewGrpcTransport()
	defer tr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := tr.Send(ctx, "127.0.0.1:1", &bus.BusMessage{ID: "x", From: "a"})
	if err == nil {
		t.Error("未启动时 Send 应返回错误")
	}
}

// TestGrpcTransport_E2E_MultipleMessages 多消息按序送达。
func TestGrpcTransport_E2E_MultipleMessages(t *testing.T) {
	srv := NewGrpcTransport()
	if err := srv.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer srv.Close()

	client := NewGrpcTransport()
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	const total = 10
	for i := 0; i < total; i++ {
		msg := &bus.BusMessage{ID: fmt.Sprintf("m-%d", i), From: "agent-a", Content: "payload"}
		if err := client.Send(ctx, srv.Addr(), msg); err != nil {
			t.Fatalf("Send #%d: %v", i, err)
		}
	}

	for i := 0; i < total; i++ {
		select {
		case got := <-srv.Receive():
			if got.ID != fmt.Sprintf("m-%d", i) {
				t.Errorf("消息顺序/内容不匹配: got %s, want m-%d", got.ID, i)
			}
		case <-ctx.Done():
			t.Fatal("消息接收超时")
		}
	}
}
