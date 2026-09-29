// grpc_bus_test.go — gRPC 跨节点消息总线测试
package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/bus"
)

// TestGRPCRemoteNode_InvalidConfig 测试无效配置
func TestGRPCRemoteNode_InvalidConfig(t *testing.T) {
	// 空地址
	_, err := NewGRPCRemoteNode(GRPCRemoteNodeConfig{})
	if err == nil {
		t.Error("expected error for empty address")
	}
}

// TestGRPCRemoteNode_ConnectionFailed 测试连接失败
func TestGRPCRemoteNode_ConnectionFailed(t *testing.T) {
	// 使用不可达的地址
	_, err := NewGRPCRemoteNode(GRPCRemoteNodeConfig{
		ID:          "test-node",
		Address:     "localhost:59998",
		DialTimeout: 500 * time.Millisecond,
	})
	if err == nil {
		t.Skip("something is listening on :59998")
	}
	t.Logf("expected connection error: %v", err)
}

// TestGRPCClusterServer_StartStop 测试服务器启停
func TestGRPCClusterServer_StartStop(t *testing.T) {
	localBus := bus.NewLocalMessageBus()
	server := NewGRPCClusterServer(GRPCClusterServerConfig{
		NodeID:     "node-1",
		Bus:        localBus,
		ListenAddr: "127.0.0.1:19091",
	})

	// 启动
	if err := server.Start("127.0.0.1:19091"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// 重复启动应失败
	if err := server.Start("127.0.0.1:19091"); err == nil {
		t.Error("expected error for double start")
	}

	// 停止
	server.Stop()

	// 重复停止应安全
	server.Stop()
}

// TestGRPCClusterServer_EndToEnd 端到端 gRPC 消息测试
func TestGRPCClusterServer_EndToEnd(t *testing.T) {
	// 创建本地总线并注册处理器
	localBus := bus.NewLocalMessageBus()
	localBus.Register("agent-1", func(ctx context.Context, msg *bus.BusMessage) (*bus.BusMessage, error) {
		return &bus.BusMessage{
			ID:        "reply_" + msg.ID,
			From:      "agent-1",
			Type:      bus.BusMsgResponse,
			Content:   "echo: " + msg.Content,
			Timestamp: time.Now(),
		}, nil
	})

	// 启动 gRPC 服务器
	server := NewGRPCClusterServer(GRPCClusterServerConfig{
		NodeID:     "server-node",
		Bus:        localBus,
		ListenAddr: "127.0.0.1:19092",
	})
	if err := server.Start("127.0.0.1:19092"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer server.Stop()

	// 等待服务器就绪
	time.Sleep(100 * time.Millisecond)

	// 创建 gRPC 客户端
	client, err := NewGRPCRemoteNode(GRPCRemoteNodeConfig{
		ID:          "client-node",
		Address:     "127.0.0.1:19092",
		DialTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewGRPCRemoteNode failed: %v", err)
	}
	defer client.Close()

	// 发送消息
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	msg := &bus.BusMessage{
		ID:        "test_msg_1",
		From:      "client-agent",
		To:        "agent-1",
		Type:      bus.BusMsgQuery,
		Content:   "hello gRPC",
		Timestamp: time.Now(),
	}

	reply, err := client.SendMessage(ctx, msg)
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	if reply.Content != "echo: hello gRPC" {
		t.Errorf("reply content = %q, want 'echo: hello gRPC'", reply.Content)
	}
}

// TestClusterMessageConversion 测试消息转换
func TestClusterMessageConversion(t *testing.T) {
	now := time.Now()
	busMsg := &bus.BusMessage{
		ID:        "msg_123",
		From:      "agent-a",
		To:        "agent-b",
		Type:      bus.BusMsgQuery,
		Content:   "test content",
		Metadata:  map[string]string{"key": "value"},
		Timestamp: now,
	}

	// BusMessage → ClusterMessage
	clusterMsg := busMessageToCluster(busMsg)
	if clusterMsg.ID != "msg_123" {
		t.Errorf("ID = %q", clusterMsg.ID)
	}
	if clusterMsg.From != "agent-a" {
		t.Errorf("From = %q", clusterMsg.From)
	}
	if clusterMsg.Type != "query" {
		t.Errorf("Type = %q", clusterMsg.Type)
	}

	// ClusterMessage → BusMessage
	backToBus := clusterToBusMessage(clusterMsg)
	if backToBus.ID != busMsg.ID {
		t.Errorf("round-trip ID mismatch: %q vs %q", backToBus.ID, busMsg.ID)
	}
	if backToBus.Content != busMsg.Content {
		t.Errorf("round-trip Content mismatch")
	}
	if string(backToBus.Type) != string(busMsg.Type) {
		t.Errorf("round-trip Type mismatch")
	}
}

// TestRawMessage 测试原始消息编解码
func TestRawMessage(t *testing.T) {
	msg := &rawMessage{data: []byte("test data")}

	// Marshal
	data, err := msg.Marshal()
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if string(data) != "test data" {
		t.Errorf("Marshal = %q", string(data))
	}

	// Unmarshal
	msg2 := &rawMessage{}
	if err := msg2.Unmarshal([]byte("new data")); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if string(msg2.data) != "new data" {
		t.Errorf("Unmarshal data = %q", string(msg2.data))
	}

	// Reset
	msg2.Reset()
	if msg2.data != nil {
		t.Error("Reset should nil data")
	}
}
