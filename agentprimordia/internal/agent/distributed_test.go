
package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// waitDiscoveryAddr 等待 DiscoveryServer 真实绑定地址就绪。
// Start 在 goroutine 中异步 Listen，就绪前 Addr() 返回配置值 "127.0.0.1:0"。
func waitDiscoveryAddr(t *testing.T, server *DiscoveryServer) string {
	t.Helper()
	const configured = "127.0.0.1:0"
	deadline := time.Now().Add(5 * time.Second)
	for {
		addr := server.Addr()
		if addr != configured {
			return addr
		}
		if time.Now().After(deadline) {
			t.Fatal("DiscoveryServer 未在超时内绑定真实地址")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestDistributed_TwoNodeTCP(t *testing.T) {
	node1 := NewTCPTransport()
	node2 := NewTCPTransport()

	if err := node1.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("node1 Start failed: %v", err)
	}
	defer node1.Close()

	if err := node2.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("node2 Start failed: %v", err)
	}
	defer node2.Close()

	discovery := NewLocalDiscovery()

	_ = discovery.Register(context.Background(), &AgentInfo{
		ID: "node-1", Name: "Node1", Address: node1.Addr(),
	})
	_ = discovery.Register(context.Background(), &AgentInfo{
		ID: "node-2", Name: "Node2", Address: node2.Addr(),
	})

	info, err := discovery.Discover(context.Background(), "node-2")
	if err != nil {
		t.Fatalf("Discover node-2 failed: %v", err)
	}

	msg := &BusMessage{
		ID:        "msg-1",
		From:      "node-1",
		To:        "node-2",
		Type:      BusMsgTaskRequest,
		Content:   "hello from node-1",
		Timestamp: time.Now(),
	}

	if err := node1.Send(context.Background(), info.Address, msg); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	select {
	case received := <-node2.Receive():
		if received.Content != "hello from node-1" {
			t.Errorf("Content = %q, want hello from node-1", received.Content)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestDistributed_HTTPWithAuth(t *testing.T) {
	localDisc := NewLocalDiscovery()
	auth := NewTokenAuthenticator("distributed-test-secret")
	authDisc := NewAuthenticatedDiscovery(localDisc, auth)

	server := NewDiscoveryServer(localDisc, "127.0.0.1:0")
	// Start 为阻塞式 Serve（显式 listener，见 discovery.Start），
	// 以 goroutine 启动；关闭后返回 http.ErrServerClosed 属正常。
	go func() {
		if err := server.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Logf("DiscoveryServer Start returned: %v", err)
		}
	}()
	defer server.Close()

	identity := &AgentIdentity{
		ID:    "agent-auth-1",
		Name:  "AuthenticatedAgent",
		Roles: []string{"worker"},
	}
	token, err := auth.GenerateToken(identity)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	info := &AgentInfo{
		ID:      "agent-auth-1",
		Name:    "AuthenticatedAgent",
		Address: "localhost:9090",
	}

	err = authDisc.Register(context.Background(), info, token)
	if err != nil {
		t.Fatalf("Register with auth failed: %v", err)
	}

	found, err := authDisc.Discover(context.Background(), "agent-auth-1")
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}
	if found.ID != "agent-auth-1" {
		t.Errorf("ID = %q, want agent-auth-1", found.ID)
	}

	err = authDisc.Register(context.Background(), &AgentInfo{ID: "bad-agent"}, "invalid-token")
	if err == nil {
		t.Error("expected error for unauthorized register")
	}
}

func TestDistributed_MultiNodeBroadcast(t *testing.T) {
	nodes := make([]*TCPTransport, 3)
	for i := range nodes {
		nodes[i] = NewTCPTransport()
		if err := nodes[i].Start("127.0.0.1:0"); err != nil {
			t.Fatalf("node%d Start failed: %v", i, err)
		}
		defer nodes[i].Close()
	}

	discovery := NewLocalDiscovery()
	for i, node := range nodes {
		_ = discovery.Register(context.Background(), &AgentInfo{
			ID:      fmt.Sprintf("node-%d", i),
			Name:    fmt.Sprintf("Node%d", i),
			Address: node.Addr(),
		})
	}

	sender := nodes[0]
	msg := &BusMessage{
		ID:        "msg-broadcast",
		From:      "node-0",
		Type:      BusMsgBroadcast,
		Content:   "broadcast message",
		Timestamp: time.Now(),
	}

	agents, _ := discovery.ListAgents(context.Background())
	for _, agent := range agents {
		if agent.ID == "node-0" {
			continue
		}
		if err := sender.Send(context.Background(), agent.Address, msg); err != nil {
			t.Errorf("Send to %s failed: %v", agent.ID, err)
		}
	}

	for i := 1; i < 3; i++ {
		select {
		case received := <-nodes[i].Receive():
			if received.Content != "broadcast message" {
				t.Errorf("node%d Content = %q, want broadcast message", i, received.Content)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("timeout waiting for message on node%d", i)
		}
	}
}

func TestDistributed_DiscoveryAndCommunicate(t *testing.T) {
	localDisc := NewLocalDiscovery()
	server := NewDiscoveryServer(localDisc, "127.0.0.1:0")
	// Start 为阻塞式 Serve（显式 listener，见 discovery.Start），
	// 以 goroutine 启动；关闭后返回 http.ErrServerClosed 属正常。
	go func() {
		if err := server.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Logf("DiscoveryServer Start returned: %v", err)
		}
	}()
	defer server.Close()

	httpDisc := NewHTTPDiscoveryClient("http://" + waitDiscoveryAddr(t, server))

	agent1Info := &AgentInfo{
		ID:           "agent-comm-1",
		Name:         "Communicator1",
		Address:      "localhost:10001",
		Capabilities: []string{"search"},
	}
	agent2Info := &AgentInfo{
		ID:           "agent-comm-2",
		Name:         "Communicator2",
		Address:      "localhost:10002",
		Capabilities: []string{"compute"},
	}

	if err := httpDisc.Register(context.Background(), agent1Info); err != nil {
		t.Fatalf("Register agent1 failed: %v", err)
	}

	_ = localDisc.Register(context.Background(), agent2Info)

	found, err := httpDisc.Discover(context.Background(), "agent-comm-2")
	if err != nil {
		t.Fatalf("Discover agent2 failed: %v", err)
	}
	if found.Name != "Communicator2" {
		t.Errorf("Name = %q, want Communicator2", found.Name)
	}

	agents, err := httpDisc.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("ListAgents failed: %v", err)
	}
	if len(agents) < 2 {
		t.Errorf("agents count = %d, want >= 2", len(agents))
	}
}
