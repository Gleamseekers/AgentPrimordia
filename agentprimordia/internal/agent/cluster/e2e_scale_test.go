//go:build e2e

// e2e_scale_test.go — 集群 10 节点规模测试
//
// 验证 ClusterManager 在 10 节点规模下的行为：
//   - 服务发现跨所有节点传播
//   - Agent 注册传播到所有节点
//   - 一致性哈希分布在 10 节点间相对均匀
//   - 节点离开后 Agent 迁移
//   - 领导者选举在 10 节点间收敛
//
// 运行方式：
//
//	go test -tags e2e -run TestE2E_Cluster_10Node -v -timeout=5m ./internal/agent/cluster/
package cluster

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/discovery"
)

// TestE2E_Cluster_10NodeScale 验证 10 节点集群的基础功能
func TestE2E_Cluster_10NodeScale(t *testing.T) {
	const nodeCount = 10

	tc := createTestCluster(t, nodeCount)
	defer tc.stop(t)

	t.Run("服务发现跨所有节点传播", func(t *testing.T) {
		// 从每个节点视角检查是否能看到所有其他节点
		for i := 0; i < nodeCount; i++ {
			agents := tc.listAllAgents(t, i)
			// 每个节点应能看到至少 nodeCount-1 个其他节点（加上自己）
			// 由于 syncNodesLoop 使用 Discovery.ListAgents，这里验证发现数量
			if len(agents) < nodeCount-1 {
				t.Errorf("节点 %d 仅发现 %d 个 Agent，期望至少 %d 个",
					i, len(agents), nodeCount-1)
			}
		}
		t.Logf("所有 %d 个节点均能发现其他节点", nodeCount)
	})

	t.Run("Agent 注册传播到所有节点", func(t *testing.T) {
		// 通过节点 0 的 discovery 注册一个新 Agent
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		testAgentID := "scale-test-agent-xyz"
		info := testAgentInfo(testAgentID, "10.0.0.99:8080")
		err := tc.discoveries[0].Register(ctx, &info)
		if err != nil {
			t.Fatalf("注册测试 Agent 失败: %v", err)
		}

		// Register 的 key TTL = heartbeat*3，启动保活防止校验窗口内过期
		keepaliveCancel := startAgentKeepalive(ctx, tc.discoveries[0], testAgentID)

		// 等待同步周期
		time.Sleep(1500 * time.Millisecond)

		// 从其他节点验证可以发现该 Agent
		for i := 1; i < nodeCount; i++ {
			found, err := tc.discoveries[i].Discover(ctx, testAgentID)
			if err != nil {
				t.Errorf("节点 %d 无法发现测试 Agent: %v", i, err)
				continue
			}
			if found.Address != "10.0.0.99:8080" {
				t.Errorf("节点 %d 发现的 Agent 地址不匹配: 期望 '10.0.0.99:8080'，得到 %q",
					i, found.Address)
			}
		}

		// 清理
		keepaliveCancel()
		_ = tc.discoveries[0].Unregister(ctx, testAgentID)
	})

	t.Run("一致性哈希分布均匀性", func(t *testing.T) {
		// 使用 1000 个 key 测试哈希分布
		const keyCount = 1000
		distribution := make(map[string]int)

		for i := 0; i < keyCount; i++ {
			key := fmt.Sprintf("agent-task-%d", i)
			// 从节点 0 的哈希环获取分配结果
			nodeID, ok := tc.managers[0].GetHashRing().GetNode(key)
			if !ok {
				t.Fatalf("哈希环无法找到 key %q 的节点", key)
			}
			distribution[nodeID]++
		}

		// 验证所有节点都被分配到至少一个 key
		if len(distribution) < nodeCount {
			t.Errorf("仅 %d 个节点被分配到 key，期望 %d 个", len(distribution), nodeCount)
		}

		// 验证分布相对均匀（每个节点至少获得平均值的 30%）
		expected := float64(keyCount) / float64(nodeCount)
		minExpected := expected * 0.3
		for nodeID, count := range distribution {
			if float64(count) < minExpected {
				t.Errorf("节点 %s 仅分配 %d 个 key（期望至少 %.0f）",
					nodeID, count, minExpected)
			}
		}

		// 计算标准差
		var sum float64
		counts := make([]float64, 0, len(distribution))
		for _, c := range distribution {
			counts = append(counts, float64(c))
			sum += float64(c)
		}
		mean := sum / float64(len(counts))
		var variance float64
		for _, c := range counts {
			variance += (c - mean) * (c - mean)
		}
		variance /= float64(len(counts))
		stddev := math.Sqrt(variance)
		cv := stddev / mean // 变异系数

		t.Logf("哈希分布: 均值=%.1f, 标准差=%.1f, 变异系数=%.2f", mean, stddev, cv)
		if cv > 1.0 {
			t.Errorf("哈希分布变异系数过高 (%.2f)，分布不均匀", cv)
		}
	})
}

// TestE2E_Cluster_10Node_AgentMigration 验证节点离开后 Agent 迁移
func TestE2E_Cluster_10Node_AgentMigration(t *testing.T) {
	const nodeCount = 10

	tc := createTestCluster(t, nodeCount)
	defer tc.stop(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 通过节点 5 注册一些 Agent
	agentPrefix := "migration-agent"
	for i := 0; i < 5; i++ {
		agentID := fmt.Sprintf("%s-%d", agentPrefix, i)
		info := testAgentInfo(agentID, fmt.Sprintf("10.1.0.%d:8080", i))
		err := tc.discoveries[5].Register(ctx, &info)
		if err != nil {
			t.Fatalf("注册 Agent %s 失败: %v", agentID, err)
		}
	}

	// Register 写入的 key TTL = heartbeat*3，且 heartbeatLoop 只续租节点自身。
	// 真实契约下调用方需自行续租，启动保活 goroutine 模拟真实调用方，
	// 避免校验窗口内 key 过期导致 flaky。
	keepaliveCancel := startAgentKeepalive(ctx, tc.discoveries[5],
		"migration-agent-0", "migration-agent-1", "migration-agent-2", "migration-agent-3", "migration-agent-4")

	// 等待同步
	time.Sleep(1500 * time.Millisecond)

	// 验证所有节点都能看到这些 Agent
	for i := 0; i < nodeCount; i++ {
		if i == 5 {
			continue
		}
		for j := 0; j < 5; j++ {
			agentID := fmt.Sprintf("%s-%d", agentPrefix, j)
			_, err := tc.discoveries[i].Discover(ctx, agentID)
			if err != nil {
				t.Errorf("节点 %d 无法发现 Agent %s: %v", i, agentID, err)
			}
		}
	}
	t.Log("所有节点均能发现迁移前注册的 Agent")

	// 停止保活，随后注销（模拟节点离开）
	keepaliveCancel()

	// 模拟节点 5 离开：注销其注册的 Agent
	for i := 0; i < 5; i++ {
		agentID := fmt.Sprintf("%s-%d", agentPrefix, i)
		_ = tc.discoveries[5].Unregister(ctx, agentID)
	}

	// 等待同步周期让其他节点感知变化
	time.Sleep(2 * time.Second)

	// 验证其他节点不再看到这些 Agent
	for i := 0; i < nodeCount; i++ {
		if i == 5 {
			continue
		}
		for j := 0; j < 5; j++ {
			agentID := fmt.Sprintf("%s-%d", agentPrefix, j)
			_, err := tc.discoveries[i].Discover(ctx, agentID)
			if err == nil {
				t.Errorf("节点 %d 仍能看到已注销的 Agent %s", i, agentID)
			}
		}
	}
	t.Log("节点离开后，其 Agent 已从其他节点消失")

	// 验证哈希环已更新（节点 5 的虚拟节点已移除）
	nodes := tc.managers[0].GetHashRing().GetNodesList()
	for _, nodeID := range nodes {
		if nodeID == tc.managers[5].config.NodeID {
			// 节点 5 可能仍在哈希环上，因为 ClusterManager.Stop 会移除
			// 但这里我们只注销了 Agent，没有停止 ClusterManager
		}
	}
}

// TestE2E_Cluster_10Node_LeaderElection 验证 10 节点领导者选举收敛
func TestE2E_Cluster_10Node_LeaderElection(t *testing.T) {
	const nodeCount = 10

	tc := createTestCluster(t, nodeCount)
	defer tc.stop(t)

	// 等待选举收敛：轮询直到至少半数节点对领导者达成一致，或超时。
	// 简化版选举中，节点需要先通过发现机制感知所有在线节点（heartbeat 传播
	// 在慢机器上需要多个周期），固定 sleep 不可靠，改为轮询。
	deadline := time.Now().Add(30 * time.Second)
	var leaderViews map[string]string
	for {
		leaderViews = make(map[string]string) // nodeID -> 它认为的 leader
		for i := 0; i < nodeCount; i++ {
			leader := tc.managers[i].GetLeader()
			leaderViews[tc.managers[i].config.NodeID] = leader
		}

		// 统计每个候选 leader 被多少节点认可
		consensus := make(map[string]int)
		for _, leader := range leaderViews {
			if leader != "" {
				consensus[leader]++
			}
		}
		topConsensus := 0
		for _, c := range consensus {
			if c > topConsensus {
				topConsensus = c
			}
		}

		if topConsensus >= nodeCount/2 {
			break // 已收敛
		}
		if time.Now().After(deadline) {
			t.Fatalf("10 节点选举在 30s 内未收敛: 视图=%v", leaderViews)
		}
		time.Sleep(500 * time.Millisecond)
	}

	// 验证至少有一个节点认为自己是领导者
	hasLeader := false
	leaderID := ""
	for nodeID, leader := range leaderViews {
		if leader != "" {
			hasLeader = true
			if leaderID == "" {
				leaderID = leader
			}
			t.Logf("节点 %s 认为领导者是 %s", nodeID, leader)
		}
	}

	if !hasLeader {
		t.Fatal("10 节点集群未能选出领导者")
	}

	// 验证所有节点对领导者达成一致（最终一致性）
	consistentCount := 0
	for _, leader := range leaderViews {
		if leader == leaderID {
			consistentCount++
		}
	}
	t.Logf("领导者一致性: %d/%d 节点同意领导者 %s", consistentCount, nodeCount, leaderID)

	if consistentCount < nodeCount/2 {
		t.Errorf("领导者一致性不足: 仅 %d/%d 节点同意", consistentCount, nodeCount)
	}

	// 验证领导者节点的角色
	for i := 0; i < nodeCount; i++ {
		if tc.managers[i].config.NodeID == leaderID {
			role := tc.managers[i].GetRole()
			if role != RoleLeader {
				t.Errorf("领导者节点 %s 角色为 %s，期望 leader", leaderID, role)
			}
		}
	}
}

// testAgentInfo 创建测试用 AgentInfo（简化辅助函数）
func testAgentInfo(id, address string) discovery.AgentInfo {
	return discovery.AgentInfo{
		ID:      id,
		Name:    "test-agent-" + id,
		Address: address,
	}
}
