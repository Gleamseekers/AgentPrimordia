package cluster

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/bus"
)

func TestRemoteNodePing(t *testing.T) {
	// 创建测试服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cluster/ping" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	node := NewRemoteNode("node-2", server.URL)
	ctx := context.Background()

	if err := node.Ping(ctx); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
}

func TestRemoteNodeSendMessage(t *testing.T) {
	// 创建接收消息的测试服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cluster/message" && r.Method == http.MethodPost {
			var msg bus.BusMessage
			if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}

			// 返回回复
			reply := bus.BusMessage{
				ID:      "reply-" + msg.ID,
				From:    msg.To,
				To:      msg.From,
				Content: "reply to: " + msg.Content,
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(&reply)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	node := NewRemoteNode("node-2", server.URL)
	ctx := context.Background()

	msg := &bus.BusMessage{
		ID:      "msg-1",
		From:    "agent-1",
		To:      "agent-2",
		Content: "hello remote",
	}

	reply, err := node.SendMessage(ctx, msg)
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}
	if reply.From != "agent-2" {
		t.Errorf("Reply From = %q, want %q", reply.From, "agent-2")
	}
	if reply.Content != "reply to: hello remote" {
		t.Errorf("Reply Content = %q, want %q", reply.Content, "reply to: hello remote")
	}
}

func TestRemoteMessageBusLocalSend(t *testing.T) {
	localBus := bus.NewLocalMessageBus()
	remoteBus := NewRemoteMessageBus(RemoteBusConfig{
		Local: localBus,
	})

	// 注册本地 Agent
	localBus.Register("agent-1", func(ctx context.Context, msg *bus.BusMessage) (*bus.BusMessage, error) {
		return &bus.BusMessage{
			ID:      "reply-" + msg.ID,
			From:    msg.To,
			To:      msg.From,
			Content: "local reply",
		}, nil
	})

	ctx := context.Background()
	msg := &bus.BusMessage{
		ID:      "msg-1",
		From:    "agent-0",
		To:      "agent-1",
		Content: "hello local",
	}

	reply, err := remoteBus.Send(ctx, msg)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	if reply.Content != "local reply" {
		t.Errorf("Reply = %q, want %q", reply.Content, "local reply")
	}

	stats := remoteBus.GetStats()
	if stats.LocalSends != 1 {
		t.Errorf("LocalSends = %d, want 1", stats.LocalSends)
	}
}

func TestRemoteMessageBusRemoteForward(t *testing.T) {
	localBus := bus.NewLocalMessageBus()

	// 创建远程服务器（模拟另一个节点上的 agent）
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cluster/message" && r.Method == http.MethodPost {
			var msg bus.BusMessage
			_ = json.NewDecoder(r.Body).Decode(&msg)
			reply := bus.BusMessage{
				ID:      "reply-" + msg.ID,
				From:    msg.To,
				To:      msg.From,
				Content: "remote reply",
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(&reply)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	remoteBus := NewRemoteMessageBus(RemoteBusConfig{
		Local: localBus,
	})

	// 添加远程节点
	remoteBus.AddNode(NewRemoteNode("node-2", server.URL))

	// 本地没有 agent-2，应该转发到远程
	ctx := context.Background()
	msg := &bus.BusMessage{
		ID:      "msg-1",
		From:    "agent-0",
		To:      "agent-2",
		Content: "hello remote",
	}

	reply, err := remoteBus.Send(ctx, msg)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	if reply.Content != "remote reply" {
		t.Errorf("Reply = %q, want %q", reply.Content, "remote reply")
	}

	stats := remoteBus.GetStats()
	if stats.RemoteForwards != 1 {
		t.Errorf("RemoteForwards = %d, want 1", stats.RemoteForwards)
	}
}

func TestRemoteMessageBusBroadcast(t *testing.T) {
	localBus := bus.NewLocalMessageBus()

	// 注册本地 Agent
	localBus.Register("agent-1", func(ctx context.Context, msg *bus.BusMessage) (*bus.BusMessage, error) {
		return &bus.BusMessage{ID: "reply", From: "agent-1", Content: "ok"}, nil
	})

	// 创建模拟远程节点
	broadcastReceived := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cluster/broadcast" && r.Method == http.MethodPost {
			broadcastReceived = true
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	remoteBus := NewRemoteMessageBus(RemoteBusConfig{
		Local: localBus,
	})
	remoteBus.AddNode(NewRemoteNode("node-2", server.URL))

	ctx := context.Background()
	msg := &bus.BusMessage{
		ID:      "broadcast-1",
		From:    "agent-0",
		Content: "broadcast message",
	}

	results := remoteBus.Broadcast(ctx, msg)

	// 本地应该有回复
	if len(results) == 0 {
		t.Error("Broadcast should have local results")
	}

	// 远程应该收到广播
	if !broadcastReceived {
		t.Error("Remote node should receive broadcast")
	}

	stats := remoteBus.GetStats()
	if stats.Broadcasts != 1 {
		t.Errorf("Broadcasts = %d, want 1", stats.Broadcasts)
	}
	if stats.RemoteBroadcasts != 1 {
		t.Errorf("RemoteBroadcasts = %d, want 1", stats.RemoteBroadcasts)
	}
}

func TestRemoteMessageBusStateSync(t *testing.T) {
	state := NewDistributedState()
	state.Set("key1", "value1", 0)
	state.Set("key2", "value2", 0)

	localBus := bus.NewLocalMessageBus()

	// 创建接收状态同步的远程服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cluster/state/sync" && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	remoteBus := NewRemoteMessageBus(RemoteBusConfig{
		Local: localBus,
		State: state,
	})
	remoteBus.AddNode(NewRemoteNode("node-2", server.URL))

	ctx := context.Background()
	if err := remoteBus.SyncState(ctx); err != nil {
		t.Fatalf("SyncState failed: %v", err)
	}
}

func TestRemoteMessageBusHandlers(t *testing.T) {
	localBus := bus.NewLocalMessageBus()
	localBus.Register("agent-1", func(ctx context.Context, msg *bus.BusMessage) (*bus.BusMessage, error) {
		return &bus.BusMessage{ID: "reply", From: "agent-1", To: msg.From, Content: "handled"}, nil
	})

	remoteBus := NewRemoteMessageBus(RemoteBusConfig{
		Local: localBus,
	})

	// 测试 MessageHandler
	msg := bus.BusMessage{ID: "m1", From: "external", To: "agent-1", Content: "test"}
	body, _ := json.Marshal(&msg)

	req := httptest.NewRequest(http.MethodPost, "/cluster/message", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	remoteBus.MessageHandler()(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("MessageHandler status = %d, want %d", w.Code, http.StatusOK)
	}

	var reply bus.BusMessage
	_ = json.Unmarshal(w.Body.Bytes(), &reply)
	if reply.Content != "handled" {
		t.Errorf("Reply content = %q, want %q", reply.Content, "handled")
	}

	// 测试 PingHandler
	req2 := httptest.NewRequest(http.MethodGet, "/cluster/ping", nil)
	w2 := httptest.NewRecorder()
	remoteBus.PingHandler()(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("PingHandler status = %d, want %d", w2.Code, http.StatusOK)
	}

	// 测试 StateSyncHandler
	remoteBusWithState := NewRemoteMessageBus(RemoteBusConfig{
		Local: bus.NewLocalMessageBus(),
		State: NewDistributedState(),
	})

	snapshot := map[string]RemoteEntry{
		"key1": {Value: "val1", Version: 1},
	}
	body2, _ := json.Marshal(snapshot)

	req3 := httptest.NewRequest(http.MethodPost, "/cluster/state/sync", strings.NewReader(string(body2)))
	w3 := httptest.NewRecorder()
	remoteBusWithState.StateSyncHandler()(w3, req3)

	if w3.Code != http.StatusOK {
		t.Errorf("StateSyncHandler status = %d, want %d", w3.Code, http.StatusOK)
	}

	respBody, _ := io.ReadAll(w3.Body)
	var syncResult map[string]any
	_ = json.Unmarshal(respBody, &syncResult)
	if syncResult["merged"] == nil {
		t.Error("StateSyncHandler should return merged count")
	}
}
