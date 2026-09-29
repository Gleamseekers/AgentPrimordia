//go:build e2e

// scale_helpers_test.go — 集群规模测试辅助工具
//
// 提供创建多节点测试集群和内存泄漏检测的辅助函数。
package cluster

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/discovery"
)

// testCluster 测试集群，包含多个 ClusterManager 和共享的 MemKVStore
type testCluster struct {
	kv          *MemKVStore
	discoveries []*DistributedDiscovery
	managers    []*ClusterManager
	cancel      context.CancelFunc
}

// createTestCluster 创建 n 节点测试集群
//
// 所有节点共享同一个 MemKVStore 作为 KV 后端，
// 使用较短的心跳/选举超时代价以加速测试。
func createTestCluster(t *testing.T, n int) *testCluster {
	t.Helper()
	if n <= 0 || n > 20 {
		t.Fatalf("节点数必须在 1-20 之间，得到 %d", n)
	}

	kv := NewMemKVStore()
	ctx, cancel := context.WithCancel(context.Background())

	tc := &testCluster{
		kv:          kv,
		discoveries: make([]*DistributedDiscovery, n),
		managers:    make([]*ClusterManager, n),
		cancel:      cancel,
	}

	// 创建 n 个 DistributedDiscovery 实例，共享同一个 MemKVStore
	for i := 0; i < n; i++ {
		nodeID := fmt.Sprintf("scale-node-%02d", i)
		dd := NewDistributedDiscovery(DistributedDiscoveryConfig{
			NodeID:            nodeID,
			KVStore:           kv,
			HeartbeatInterval: 500 * time.Millisecond,
			SyncInterval:      300 * time.Millisecond,
		})
		if err := dd.Start(ctx); err != nil {
			cancel()
			t.Fatalf("节点 %s 发现启动失败: %v", nodeID, err)
		}
		tc.discoveries[i] = dd
	}

	// 创建 n 个 ClusterManager 实例
	for i := 0; i < n; i++ {
		nodeID := fmt.Sprintf("scale-node-%02d", i)
		mgr := NewClusterManager(ClusterConfig{
			NodeID:            nodeID,
			ListenAddr:        fmt.Sprintf("127.0.0.1:%d", 19000+i),
			Discovery:         tc.discoveries[i],
			StateStore:        kv, // 共享 KV 后端，使领导者选举跨节点收敛
			HeartbeatInterval: 500 * time.Millisecond,
			HeartbeatTimeout:  2 * time.Second,
			ElectionTimeout:   1 * time.Second,
		})
		if err := mgr.Start(ctx); err != nil {
			cancel()
			t.Fatalf("节点 %s 管理器启动失败: %v", nodeID, err)
		}
		tc.managers[i] = mgr
	}

	// 等待节点同步（至少一个同步周期）
	time.Sleep(1500 * time.Millisecond)

	return tc
}

// stop 停止测试集群并清理资源
func (tc *testCluster) stop(t *testing.T) {
	t.Helper()
	ctx := context.Background()

	// 先停止所有 ClusterManager
	for i, mgr := range tc.managers {
		if mgr != nil {
			_ = mgr.Stop(ctx)
			tc.managers[i] = nil
		}
	}

	// 取消 context
	tc.cancel()

	// 最后关闭 KV 存储（一次性清理，避免 DistributedDiscovery.Close 提前关闭）
	_ = tc.kv.Close()
}

// listAllAgents 从指定节点的视角列出所有可见 Agent
func (tc *testCluster) listAllAgents(t *testing.T, nodeIdx int) []*discovery.AgentInfo {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	agents, err := tc.discoveries[nodeIdx].ListAgents(ctx)
	if err != nil {
		t.Fatalf("节点 %d 列出 Agent 失败: %v", nodeIdx, err)
	}
	return agents
}

// startAgentKeepalive 为已注册的 Agent 启动续租 goroutine。
//
// Register 写入的 key TTL = heartbeat*3（测试配置 500ms*3 = 1.5s），且
// heartbeatLoop 只续租节点自身。真实契约下调用方需自行续租，否则 key 会在
// TTL 后过期。返回 cancel 停止全部续租 goroutine。
func startAgentKeepalive(ctx context.Context, dd *DistributedDiscovery, agentIDs ...string) context.CancelFunc {
	keepaliveCtx, cancel := context.WithCancel(ctx)
	for _, id := range agentIDs {
		agentID := id
		go func() {
			ticker := time.NewTicker(400 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-keepaliveCtx.Done():
					return
				case <-ticker.C:
					_ = dd.Heartbeat(keepaliveCtx, agentID)
				}
			}
		}()
	}
	return cancel
}

// assertNoMemoryLeak 检测内存泄漏
//
// startMB/endMB 为测试前后的内存使用量（MB），
// thresholdPct 为允许的最大增长百分比（如 10 表示 10%）。
func assertNoMemoryLeak(t *testing.T, startMB, endMB float64, thresholdPct float64) {
	t.Helper()
	if startMB <= 0 {
		t.Fatal("起始内存使用量必须大于 0")
	}
	growthPct := ((endMB - startMB) / startMB) * 100
	if growthPct > thresholdPct {
		t.Errorf("检测到潜在内存泄漏: 起始 %.1fMB, 结束 %.1fMB, 增长 %.1f%% (阈值 %.1f%%)",
			startMB, endMB, growthPct, thresholdPct)
	} else {
		t.Logf("内存检查通过: 起始 %.1fMB, 结束 %.1fMB, 增长 %.1f%%",
			startMB, endMB, growthPct)
	}
}

// getMemoryMB 获取当前进程内存使用量（MB）
func getMemoryMB() float64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return float64(m.Alloc) / 1024 / 1024
}
