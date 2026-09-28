
package agent

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestDistributedIntegration 端到端分布式集成测试
func TestDistributedIntegration(t *testing.T) {
	// 1. 启动 Discovery 服务
	discovery := NewLocalDiscovery()
	auth := NewTokenAuthenticator("distributed-test-secret")
	authDiscovery := NewAuthenticatedDiscovery(discovery, auth)

	// 2. 为每个 Agent 创建 TCPTransport
	transport1 := NewTCPTransport()
	transport2 := NewTCPTransport()
	transport3 := NewTCPTransport()

	// 启动传输层
	if err := transport1.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("transport1 启动失败: %v", err)
	}
	defer transport1.Close()

	if err := transport2.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("transport2 启动失败: %v", err)
	}
	defer transport2.Close()

	if err := transport3.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("transport3 启动失败: %v", err)
	}
	defer transport3.Close()

	// 3. 生成认证 Token
	identity1 := &AgentIdentity{
		ID:    "agent-1",
		Name:  "Agent One",
		Roles: []string{"worker"},
	}
	token1, err := auth.GenerateToken(identity1)
	if err != nil {
		t.Fatalf("生成 token1 失败: %v", err)
	}

	identity2 := &AgentIdentity{
		ID:    "agent-2",
		Name:  "Agent Two",
		Roles: []string{"worker", "coordinator"},
	}
	token2, err := auth.GenerateToken(identity2)
	if err != nil {
		t.Fatalf("生成 token2 失败: %v", err)
	}

	identity3 := &AgentIdentity{
		ID:    "agent-3",
		Name:  "Agent Three",
		Roles: []string{"worker"},
	}
	token3, err := auth.GenerateToken(identity3)
	if err != nil {
		t.Fatalf("生成 token3 失败: %v", err)
	}

	// 4. 注册 Agent 到 Discovery 服务
	ctx := context.Background()

	info1 := &AgentInfo{
		ID:           "agent-1",
		Name:         "Agent One",
		Address:      transport1.Addr(),
		Capabilities: []string{"chat", "reasoning"},
		Metadata:     map[string]string{"version": "1.0"},
	}
	if err := authDiscovery.Register(ctx, info1, token1); err != nil {
		t.Fatalf("agent-1 注册失败: %v", err)
	}

	info2 := &AgentInfo{
		ID:           "agent-2",
		Name:         "Agent Two",
		Address:      transport2.Addr(),
		Capabilities: []string{"chat", "coordination"},
		Metadata:     map[string]string{"version": "1.0"},
	}
	if err := authDiscovery.Register(ctx, info2, token2); err != nil {
		t.Fatalf("agent-2 注册失败: %v", err)
	}

	info3 := &AgentInfo{
		ID:           "agent-3",
		Name:         "Agent Three",
		Address:      transport3.Addr(),
		Capabilities: []string{"chat", "analysis"},
		Metadata:     map[string]string{"version": "1.0"},
	}
	if err := authDiscovery.Register(ctx, info3, token3); err != nil {
		t.Fatalf("agent-3 注册失败: %v", err)
	}

	// 5. 验证 Agent 发现
	discovered, err := authDiscovery.Discover(ctx, "agent-2")
	if err != nil {
		t.Fatalf("发现 agent-2 失败: %v", err)
	}
	if discovered.Address != transport2.Addr() {
		t.Errorf("agent-2 地址不匹配: 期望 %s, 得到 %s", transport2.Addr(), discovered.Address)
	}

	// 6. 按角色列出 Agent
	workers, err := authDiscovery.ListAgentsByRole(ctx, "worker")
	if err != nil {
		t.Fatalf("列出 worker 失败: %v", err)
	}
	if len(workers) != 3 {
		t.Errorf("应该有 3 个 worker, 得到 %d", len(workers))
	}

	coordinators, err := authDiscovery.ListAgentsByRole(ctx, "coordinator")
	if err != nil {
		t.Fatalf("列出 coordinator 失败: %v", err)
	}
	if len(coordinators) != 1 {
		t.Errorf("应该有 1 个 coordinator, 得到 %d", len(coordinators))
	}

	// 7. 测试消息传递（agent-1 -> agent-2）
	msg1 := &BusMessage{
		ID:      "msg-1",
		From:    "agent-1",
		To:      "agent-2",
		Content: "Hello from agent-1",
	}

	// 发送消息
	if err := transport1.Send(ctx, transport2.Addr(), msg1); err != nil {
		t.Fatalf("发送消息失败: %v", err)
	}

	// 等待接收
	select {
	case recv := <-transport2.Receive():
		if recv.ID != msg1.ID {
			t.Errorf("消息 ID 不匹配: 期望 %s, 得到 %s", msg1.ID, recv.ID)
		}
		if recv.From != "agent-1" {
			t.Errorf("消息 From 不匹配: 期望 agent-1, 得到 %s", recv.From)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("接收消息超时")
	}

	// 8. 测试带 ACK 的消息传递
	msg2 := &BusMessage{
		ID:      "msg-2",
		From:    "agent-2",
		To:      "agent-3",
		Content: "Hello from agent-2 with ACK",
	}

	// 发送带 ACK 的消息
	if err := transport2.SendWithAck(ctx, transport3.Addr(), msg2); err != nil {
		t.Fatalf("发送带 ACK 的消息失败: %v", err)
	}

	// 等待接收
	select {
	case recv := <-transport3.Receive():
		if recv.ID != msg2.ID {
			t.Errorf("消息 ID 不匹配: 期望 %s, 得到 %s", msg2.ID, recv.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("接收带 ACK 的消息超时")
	}

	// 9. 测试心跳
	if err := authDiscovery.Heartbeat(ctx, "agent-1"); err != nil {
		t.Errorf("agent-1 心跳失败: %v", err)
	}
	if err := authDiscovery.Heartbeat(ctx, "agent-2"); err != nil {
		t.Errorf("agent-2 心跳失败: %v", err)
	}
	if err := authDiscovery.Heartbeat(ctx, "agent-3"); err != nil {
		t.Errorf("agent-3 心跳失败: %v", err)
	}

	// 10. 测试注销
	if err := authDiscovery.Unregister(ctx, "agent-3", token3); err != nil {
		t.Fatalf("注销 agent-3 失败: %v", err)
	}

	// 验证 agent-3 已注销
	_, err = authDiscovery.Discover(ctx, "agent-3")
	if err == nil {
		t.Error("agent-3 应该已被注销")
	}

	// 11. 测试连接池统计
	active, idle := transport1.PoolStats()
	t.Logf("transport1 连接池: active=%d, idle=%d", active, idle)

	// 12. 列出所有 Agent
	allAgents, err := authDiscovery.ListAgents(ctx)
	if err != nil {
		t.Fatalf("列出所有 Agent 失败: %v", err)
	}
	if len(allAgents) != 2 {
		t.Errorf("应该有 2 个 Agent, 得到 %d", len(allAgents))
	}

	t.Log("分布式集成测试通过")
}

// TestDistributedMessageRouting 测试分布式消息路由
func TestDistributedMessageRouting(t *testing.T) {
	// 创建 3 个传输层
	transport1 := NewTCPTransport()
	transport2 := NewTCPTransport()
	transport3 := NewTCPTransport()

	if err := transport1.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("transport1 启动失败: %v", err)
	}
	defer transport1.Close()

	if err := transport2.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("transport2 启动失败: %v", err)
	}
	defer transport2.Close()

	if err := transport3.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("transport3 启动失败: %v", err)
	}
	defer transport3.Close()

	ctx := context.Background()

	// 创建 Discovery 服务
	discovery := NewLocalDiscovery()

	// 注册所有 Agent
	info1 := &AgentInfo{
		ID:      "agent-1",
		Name:    "Agent One",
		Address: transport1.Addr(),
	}
	info2 := &AgentInfo{
		ID:      "agent-2",
		Name:    "Agent Two",
		Address: transport2.Addr(),
	}
	info3 := &AgentInfo{
		ID:      "agent-3",
		Name:    "Agent Three",
		Address: transport3.Addr(),
	}

	discovery.Register(ctx, info1)
	discovery.Register(ctx, info2)
	discovery.Register(ctx, info3)

	// 启动接收协程
	received1 := make(chan *BusMessage, 10)
	received2 := make(chan *BusMessage, 10)
	received3 := make(chan *BusMessage, 10)

	go func() {
		for msg := range transport1.Receive() {
			received1 <- msg
		}
	}()
	go func() {
		for msg := range transport2.Receive() {
			received2 <- msg
		}
	}()
	go func() {
		for msg := range transport3.Receive() {
			received3 <- msg
		}
	}()

	// 发送消息到特定 Agent
	msg1 := &BusMessage{
		ID:      "msg-to-2",
		From:    "agent-1",
		To:      "agent-2",
		Content: "Message to agent-2",
	}

	// 通过 Discovery 获取目标地址
	target, err := discovery.Discover(ctx, "agent-2")
	if err != nil {
		t.Fatalf("发现 agent-2 失败: %v", err)
	}

	// 发送消息
	if err := transport1.Send(ctx, target.Address, msg1); err != nil {
		t.Fatalf("发送消息失败: %v", err)
	}

	// 验证只有 agent-2 收到消息
	select {
	case recv := <-received2:
		if recv.ID != msg1.ID {
			t.Errorf("消息 ID 不匹配")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("agent-2 未收到消息")
	}

	// 验证 agent-1 和 agent-3 没有收到消息
	select {
	case <-received1:
		t.Error("agent-1 不应该收到消息")
	case <-received3:
		t.Error("agent-3 不应该收到消息")
	case <-time.After(100 * time.Millisecond):
		// 预期行为
	}

	// 广播消息到所有 Agent
	broadcastMsg := &BusMessage{
		ID:      "broadcast",
		From:    "agent-1",
		To:      "", // 空表示广播
		Content: "Broadcast message",
	}

	agents, _ := discovery.ListAgents(ctx)
	for _, agent := range agents {
		if agent.ID != "agent-1" { // 不发送给自己
			target, _ := discovery.Discover(ctx, agent.ID)
			transport1.Send(ctx, target.Address, broadcastMsg)
		}
	}

	// 验证 agent-2 和 agent-3 都收到广播
	select {
	case <-received2:
		// 收到
	case <-time.After(2 * time.Second):
		t.Fatal("agent-2 未收到广播")
	}

	select {
	case <-received3:
		// 收到
	case <-time.After(2 * time.Second):
		t.Fatal("agent-3 未收到广播")
	}

	t.Log("分布式消息路由测试通过")
}

// TestDistributedFaultTolerance 测试分布式容错
func TestDistributedFaultTolerance(t *testing.T) {
	transport1 := NewTCPTransport()
	transport2 := NewTCPTransport()

	if err := transport1.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("transport1 启动失败: %v", err)
	}
	defer transport1.Close()

	ctx := context.Background()

	// 尝试发送到一个不存在的目标
	msg := &BusMessage{
		ID:      "msg-fail",
		From:    "agent-1",
		To:      "non-existent",
		Content: "This will fail",
	}

	// 应该重试后失败
	err := transport1.Send(ctx, "127.0.0.1:59999", msg)
	if err == nil {
		t.Error("发送到不存在的目标应该失败")
	}

	t.Logf("容错测试: 发送失败符合预期 - %v", err)

	// 测试目标启动后关闭
	if err := transport2.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("transport2 启动失败: %v", err)
	}

	addr2 := transport2.Addr()

	// 关闭 transport2
	transport2.Close()

	// 尝试发送到已关闭的目标
	err = transport1.Send(ctx, addr2, msg)
	if err == nil {
		t.Error("发送到已关闭的目标应该失败")
	}

	t.Logf("容错测试: 发送到已关闭目标失败符合预期 - %v", err)

	t.Log("分布式容错测试通过")
}

// TestDistributedPerformance 测试分布式性能
func TestDistributedPerformance(t *testing.T) {
	transport1 := NewTCPTransport()
	transport2 := NewTCPTransport()

	if err := transport1.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("transport1 启动失败: %v", err)
	}
	defer transport1.Close()

	if err := transport2.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("transport2 启动失败: %v", err)
	}
	defer transport2.Close()

	ctx := context.Background()

	// 启动接收协程
	received := make(chan *BusMessage, 1000)
	go func() {
		for msg := range transport2.Receive() {
			received <- msg
		}
	}()

	// 发送大量消息
	numMessages := 100
	start := time.Now()

	for i := 0; i < numMessages; i++ {
		msg := &BusMessage{
			ID:      fmt.Sprintf("msg-%d", i),
			From:    "agent-1",
			To:      "agent-2",
			Content: fmt.Sprintf("Message %d", i),
		}
		if err := transport1.Send(ctx, transport2.Addr(), msg); err != nil {
			t.Fatalf("发送消息 %d 失败: %v", i, err)
		}
	}

	// 等待所有消息被接收
	receivedCount := 0
	timeout := time.After(10 * time.Second)
	for receivedCount < numMessages {
		select {
		case <-received:
			receivedCount++
		case <-timeout:
			t.Fatalf("接收超时: 只收到 %d/%d 条消息", receivedCount, numMessages)
		}
	}

	elapsed := time.Since(start)
	messagesPerSecond := float64(numMessages) / elapsed.Seconds()

	t.Logf("性能测试: 发送 %d 条消息耗时 %v (%.2f msg/s)", numMessages, elapsed, messagesPerSecond)

	if messagesPerSecond < 100 {
		t.Errorf("性能过低: %.2f msg/s", messagesPerSecond)
	}

	t.Log("分布式性能测试通过")
}
