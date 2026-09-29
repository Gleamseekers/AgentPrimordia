package suite

// Phase 4.1: 集群分片基准 + 跨节点消息延迟
//
// 测量：
//   - 一致性哈希 GetNode 在不同节点数下的延迟
//   - 节点动态增减时的重新分片开销
//   - DistributedState 读写延迟

import (
	"fmt"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/cluster"
)

// BenchmarkConsistentHash_GetNode 基准：不同节点数下的分片查找
func BenchmarkConsistentHash_GetNode(b *testing.B) {
	nodeCounts := []int{3, 5, 10, 20, 50}

	for _, n := range nodeCounts {
		b.Run(fmt.Sprintf("Nodes_%d", n), func(b *testing.B) {
			h := cluster.NewConsistentHash(150)
			for i := 0; i < n; i++ {
				h.AddNode(fmt.Sprintf("node-%d", i))
			}

			keys := make([]string, 1000)
			for i := range keys {
				keys[i] = fmt.Sprintf("agent-task-%d", i)
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				h.GetNode(keys[i%len(keys)])
			}
		})
	}
}

// BenchmarkConsistentHash_AddNode 基准：节点加入开销
func BenchmarkConsistentHash_AddNode(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		h := cluster.NewConsistentHash(150)
		// 预填充 10 个节点
		for j := 0; j < 10; j++ {
			h.AddNode(fmt.Sprintf("existing-%d", j))
		}
		b.StartTimer()

		h.AddNode(fmt.Sprintf("new-node-%d", i))
	}
}

// BenchmarkConsistentHash_RemoveNode 基准：节点移除开销
func BenchmarkConsistentHash_RemoveNode(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		h := cluster.NewConsistentHash(150)
		for j := 0; j < 20; j++ {
			h.AddNode(fmt.Sprintf("node-%d", j))
		}
		b.StartTimer()

		h.RemoveNode(fmt.Sprintf("node-%d", i%20))
	}
}

// BenchmarkConsistentHash_GetNodes 基准：多副本节点查找
func BenchmarkConsistentHash_GetNodes(b *testing.B) {
	h := cluster.NewConsistentHash(150)
	for i := 0; i < 10; i++ {
		h.AddNode(fmt.Sprintf("node-%d", i))
	}

	replicaCounts := []int{2, 3, 5}
	for _, r := range replicaCounts {
		b.Run(fmt.Sprintf("Replicas_%d", r), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				h.GetNodes(fmt.Sprintf("key-%d", i), r)
			}
		})
	}
}

// BenchmarkDistributedState_SetGet 基准：分布式状态读写
func BenchmarkDistributedState_SetGet(b *testing.B) {
	state := cluster.NewDistributedState()

	b.Run("Set", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			state.Set(fmt.Sprintf("key-%d", i%1000), "value", 0)
		}
	})

	// 预填充
	for i := 0; i < 1000; i++ {
		state.Set(fmt.Sprintf("key-%d", i), "value", 0)
	}

	b.Run("Get", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			state.Get(fmt.Sprintf("key-%d", i%1000))
		}
	})

	b.Run("SetGet_Mixed", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("mixed-%d", i%500)
			if i%2 == 0 {
				state.Set(key, "updated", 0)
			} else {
				state.Get(key)
			}
		}
	})
}

// BenchmarkDistributedState_TTL 基准：带 TTL 的写入开销
func BenchmarkDistributedState_TTL(b *testing.B) {
	state := cluster.NewDistributedState()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state.Set(fmt.Sprintf("ttl-%d", i%100), "value", time.Minute)
	}
}
