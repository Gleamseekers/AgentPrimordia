//go:build etcd

// etcd_discovery_test.go — etcd KV 存储后端测试
package cluster

import (
	"context"
	"testing"
	"time"
)

// TestValidateEtcdEndpoint 测试端点格式验证
func TestValidateEtcdEndpoint(t *testing.T) {
	tests := []struct {
		name    string
		ep      string
		wantErr bool
	}{
		// 合法端点
		{"host:port", "localhost:2379", false},
		{"ip:port", "10.0.0.1:2379", false},
		{"http URL", "http://localhost:2379", false},
		{"https URL", "https://etcd.example.com:2379", false},
		{"http URL with slash", "http://localhost:2379/", false},

		// 非法端点
		{"empty", "", true},
		{"no port", "localhost", true},
		{"ftp scheme", "ftp://localhost:2379", true},
		{"file scheme", "file:///etc/passwd", true},
		{"user info", "http://user:pass@localhost:2379", true},
		{"path", "http://localhost:2379/admin", true},
		{"query params", "http://localhost:2379?foo=bar", true},
		{"empty host URL", "http://:2379", true},
		{"empty host:port", ":2379", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEtcdEndpoint(tt.ep)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateEtcdEndpoint(%q) error = %v, wantErr %v", tt.ep, err, tt.wantErr)
			}
		})
	}
}

// TestNewEtcdKVStore_InvalidEndpoints 测试无效端点拒绝
func TestNewEtcdKVStore_InvalidEndpoints(t *testing.T) {
	// 空端点
	_, err := NewEtcdKVStore(EtcdKVStoreConfig{})
	if err == nil {
		t.Fatal("expected error for empty endpoints")
	}

	// 非法格式端点
	_, err = NewEtcdKVStore(EtcdKVStoreConfig{
		Endpoints: []string{"ftp://evil.com:2379"},
	})
	if err == nil {
		t.Fatal("expected error for invalid scheme")
	}

	// 包含路径的端点
	_, err = NewEtcdKVStore(EtcdKVStoreConfig{
		Endpoints: []string{"http://localhost:2379/admin/secret"},
	})
	if err == nil {
		t.Fatal("expected error for endpoint with path")
	}
}

// TestNewEtcdKVStore_ConnectionFailed 测试连接失败（无真实 etcd 时）
func TestNewEtcdKVStore_ConnectionFailed(t *testing.T) {
	// 使用不可达的端点，验证连接失败时正确返回错误
	_, err := NewEtcdKVStore(EtcdKVStoreConfig{
		Endpoints:   []string{"localhost:59999"}, // 不太可能有 etcd 监听
		DialTimeout: 500 * time.Millisecond,
	})
	if err == nil {
		t.Skip("etcd is running on localhost:59999, skipping connection failure test")
	}
	// 应该返回连接错误
	t.Logf("expected connection error: %v", err)
}

// TestEtcdKVStore_Integration 集成测试（需要真实 etcd）
// 运行方式：先启动 etcd，然后 `go test -tags etcd -run TestEtcdKVStore_Integration`
func TestEtcdKVStore_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	store, err := NewEtcdKVStore(EtcdKVStoreConfig{
		Endpoints:   []string{"localhost:2379"},
		DialTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Skipf("etcd not available: %v", err)
	}
	defer store.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 测试 Put + Get
	t.Run("PutGet", func(t *testing.T) {
		key := "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/test/integration_" + t.Name()
		value := `{"id":"test-agent","name":"Test"}`

		if err := store.Put(ctx, key, value, 30*time.Second); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		got, err := store.Get(ctx, key)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if got != value {
			t.Errorf("Get = %q, want %q", got, value)
		}

		// 清理
		store.Delete(ctx, key)
	})

	// 测试 ListByPrefix
	t.Run("ListByPrefix", func(t *testing.T) {
		prefix := "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/test/list_"
		for i := 0; i < 3; i++ {
			key := prefix + string(rune('a'+i))
			store.Put(ctx, key, "value_"+string(rune('a'+i)), 30*time.Second)
		}

		result, err := store.ListByPrefix(ctx, prefix)
		if err != nil {
			t.Fatalf("ListByPrefix failed: %v", err)
		}
		if len(result) != 3 {
			t.Errorf("ListByPrefix returned %d items, want 3", len(result))
		}

		// 清理
		for key := range result {
			store.Delete(ctx, key)
		}
	})

	// 测试 Watch
	t.Run("Watch", func(t *testing.T) {
		prefix := "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/test/watch_"
		watchCh := store.Watch(ctx, prefix)

		// etcd Watch 注册是异步的：若 Put 发生在 watch 建立之前，事件会永久丢失。
		// 这里采用「重试写入 + 接收」循环：先写入，若 watch 尚未就绪则间隔重试，
		// 直到收到事件或超时，避免与 CI 等慢速环境下的注册延迟产生竞态。
		deadline := time.Now().Add(10 * time.Second)
		key := prefix + "node1"
		for {
			if err := store.Put(ctx, key, "data1", 30*time.Second); err != nil {
				t.Fatalf("Put failed: %v", err)
			}

			select {
			case event := <-watchCh:
				if event.Type != EventPut {
					t.Errorf("event type = %v, want EventPut", event.Type)
				}
				if event.Key != key {
					t.Errorf("event key = %q, want %q", event.Key, key)
				}
				if event.Value != "data1" {
					t.Errorf("event value = %q, want %q", event.Value, "data1")
				}

				// 清理
				store.Delete(ctx, key)
				return
			case <-time.After(500 * time.Millisecond):
				if time.Now().After(deadline) {
					t.Fatal("Watch timeout: no event received")
				}
				// watch 可能尚未注册，重试写入触发下一次事件
			}
		}
	})

	// 测试 TTL 过期
	t.Run("TTLExpiry", func(t *testing.T) {
		key := "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/test/ttl_key"
		if err := store.Put(ctx, key, "short-lived", 1*time.Second); err != nil {
			t.Fatalf("Put with TTL failed: %v", err)
		}

		// 立即读取应存在
		if _, err := store.Get(ctx, key); err != nil {
			t.Fatalf("Get immediately after Put failed: %v", err)
		}

		// 等待过期：etcd lease 到期检查粒度约 1s，CI 负载下留足余量
		time.Sleep(3 * time.Second)

		_, err := store.Get(ctx, key)
		if err == nil {
			t.Error("expected error after TTL expiry, got nil")
		}
	})
}

// TestLeaseManager_Integration 租约管理器集成测试
func TestLeaseManager_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	store, err := NewEtcdKVStore(EtcdKVStoreConfig{
		Endpoints:   []string{"localhost:2379"},
		DialTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Skipf("etcd not available: %v", err)
	}
	defer store.Close()

	lm := NewLeaseManager(store.client, nil)
	defer lm.Close()

	ctx := context.Background()
	key := "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/test/lease_node1"

	// 注册
	if err := lm.RegisterWithLease(ctx, key, `{"node":"1"}`, 5); err != nil {
		t.Fatalf("RegisterWithLease failed: %v", err)
	}

	// 验证键存在
	val, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get after register failed: %v", err)
	}
	if val != `{"node":"1"}` {
		t.Errorf("value = %q, want %q", val, `{"node":"1"}`)
	}

	// 注销
	if err := lm.Deregister(ctx, key); err != nil {
		t.Fatalf("Deregister failed: %v", err)
	}

	// 验证键已删除
	time.Sleep(100 * time.Millisecond)
	_, err = store.Get(ctx, key)
	if err == nil {
		t.Error("expected key to be deleted after deregister")
	}
}
