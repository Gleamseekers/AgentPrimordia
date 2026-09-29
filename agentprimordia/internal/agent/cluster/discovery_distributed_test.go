package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent/discovery"
)

func TestMemKVStorePutGet(t *testing.T) {
	kv := NewMemKVStore()
	defer kv.Close()

	ctx := context.Background()

	// Put + Get
	if err := kv.Put(ctx, "key1", "value1", 0); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	val, err := kv.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if val != "value1" {
		t.Errorf("Get = %q, want %q", val, "value1")
	}

	// Get 不存在的 key
	_, err = kv.Get(ctx, "nonexistent")
	if err == nil {
		t.Error("Get nonexistent should return error")
	}
}

func TestMemKVStoreDelete(t *testing.T) {
	kv := NewMemKVStore()
	defer kv.Close()

	ctx := context.Background()

	_ = kv.Put(ctx, "key1", "value1", 0)
	_ = kv.Delete(ctx, "key1")

	_, err := kv.Get(ctx, "key1")
	if err == nil {
		t.Error("Get after Delete should return error")
	}
}

func TestMemKVStoreTTL(t *testing.T) {
	kv := NewMemKVStore()
	defer kv.Close()

	ctx := context.Background()

	// 写入带 TTL 的 key
	_ = kv.Put(ctx, "temp", "value", 50*time.Millisecond)

	// 立即可读
	val, err := kv.Get(ctx, "temp")
	if err != nil || val != "value" {
		t.Fatalf("Get immediately should succeed: val=%q err=%v", val, err)
	}

	// 等待过期
	time.Sleep(60 * time.Millisecond)

	_, err = kv.Get(ctx, "temp")
	if err == nil {
		t.Error("Get after TTL expiry should return error")
	}
}

func TestMemKVStoreListByPrefix(t *testing.T) {
	kv := NewMemKVStore()
	defer kv.Close()

	ctx := context.Background()

	_ = kv.Put(ctx, "agents/a1", "data1", 0)
	_ = kv.Put(ctx, "agents/a2", "data2", 0)
	_ = kv.Put(ctx, "other/b1", "data3", 0)

	result, err := kv.ListByPrefix(ctx, "agents/")
	if err != nil {
		t.Fatalf("ListByPrefix failed: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("ListByPrefix returned %d items, want 2", len(result))
	}
}

func TestMemKVStoreWatch(t *testing.T) {
	kv := NewMemKVStore()
	defer kv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := kv.Watch(ctx, "watch/")

	// 写入 key
	_ = kv.Put(ctx, "watch/key1", "value1", 0)

	select {
	case event := <-ch:
		if event.Key != "watch/key1" || event.Value != "value1" {
			t.Errorf("Watch event = %+v, want key=watch/key1 value=value1", event)
		}
		if event.Type != EventPut {
			t.Errorf("Event type = %d, want %d", event.Type, EventPut)
		}
	case <-time.After(1 * time.Second):
		t.Error("Watch did not receive event")
	}
}

func TestDistributedDiscoveryRegisterAndDiscover(t *testing.T) {
	kv := NewMemKVStore()
	defer kv.Close()

	ddb := NewDistributedDiscovery(DistributedDiscoveryConfig{
		NodeID:  "node-1",
		KVStore: kv,
	})

	ctx := context.Background()

	// 注册 Agent
	agentInfo := &discovery.AgentInfo{
		ID:      "agent-1",
		Name:    "Agent One",
		Address: "localhost:8080",
	}
	if err := ddb.Register(ctx, agentInfo); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// 发现 Agent
	found, err := ddb.Discover(ctx, "agent-1")
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}
	if found.ID != "agent-1" || found.Name != "Agent One" {
		t.Errorf("Discover returned %+v, want agent-1/Agent One", found)
	}
}

func TestDistributedDiscoveryUnregister(t *testing.T) {
	kv := NewMemKVStore()
	defer kv.Close()

	ddb := NewDistributedDiscovery(DistributedDiscoveryConfig{
		NodeID:  "node-1",
		KVStore: kv,
	})

	ctx := context.Background()

	_ = ddb.Register(ctx, &discovery.AgentInfo{ID: "agent-1", Name: "Agent One"})

	// 注销
	if err := ddb.Unregister(ctx, "agent-1"); err != nil {
		t.Fatalf("Unregister failed: %v", err)
	}

	// 发现应该失败
	_, err := ddb.Discover(ctx, "agent-1")
	if err == nil {
		t.Error("Discover after Unregister should fail")
	}
}

func TestDistributedDiscoveryListAgents(t *testing.T) {
	kv := NewMemKVStore()
	defer kv.Close()

	ddb := NewDistributedDiscovery(DistributedDiscoveryConfig{
		NodeID:  "node-1",
		KVStore: kv,
	})

	ctx := context.Background()

	_ = ddb.Register(ctx, &discovery.AgentInfo{ID: "agent-1", Name: "Agent One"})
	_ = ddb.Register(ctx, &discovery.AgentInfo{ID: "agent-2", Name: "Agent Two"})

	agents, err := ddb.ListAgents(ctx)
	if err != nil {
		t.Fatalf("ListAgents failed: %v", err)
	}
	if len(agents) != 2 {
		t.Errorf("ListAgents returned %d agents, want 2", len(agents))
	}
}

func TestDistributedDiscoveryHeartbeat(t *testing.T) {
	kv := NewMemKVStore()
	defer kv.Close()

	ddb := NewDistributedDiscovery(DistributedDiscoveryConfig{
		NodeID:            "node-1",
		KVStore:           kv,
		HeartbeatInterval: 100 * time.Millisecond,
	})

	ctx := context.Background()

	_ = ddb.Register(ctx, &discovery.AgentInfo{ID: "agent-1", Name: "Agent One"})

	originalTime := time.Now()
	time.Sleep(10 * time.Millisecond)

	// 心跳应更新 LastSeen
	if err := ddb.Heartbeat(ctx, "agent-1"); err != nil {
		t.Fatalf("Heartbeat failed: %v", err)
	}

	found, _ := ddb.Discover(ctx, "agent-1")
	if !found.LastSeen.After(originalTime) {
		t.Error("Heartbeat should update LastSeen")
	}
}
