package suite

import (
	"context"
	"fmt"
	"testing"

	ap "github.com/Gleamseekers/AgentPrimordia/pkg"
)

// BenchmarkLatency 基准：Agent 延迟
func BenchmarkLatency(b *testing.B) {
	agent, err := ap.NewAgent("LatencyAgent", "你是助手", &benchMockLLM{}, ap.WithMaxTurns(1))
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = agent.Run(context.Background(), ap.UserMessage("hello"))
	}
}

// BenchmarkConcurrent 基准：并发 Agent 吞吐量
func BenchmarkConcurrent(b *testing.B) {
	pool := ap.NewPool(ap.PoolConfig{
		MaxConcurrency: 10,
		DefaultAgent: ap.ReActAgentConfig{
			SystemPrompt: "你是助手",
			MaxTurns:     3,
		},
	})
	defer pool.Close()
	pool.SetModel(&benchMockLLM{})

	tasks := make([]ap.TaskConfig, 10)
	for i := range tasks {
		tasks[i] = ap.TaskConfig{
			ID:     fmt.Sprintf("bench-%d", i),
			Title:  "基准测试",
			Prompt: "处理任务",
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = pool.Dispatch(context.Background(), tasks)
	}
}

// BenchmarkFirstTokenLatency 基准：首 Token 延迟
func BenchmarkFirstTokenLatency(b *testing.B) {
	agent, err := ap.NewAgent("StreamAgent", "你是助手", &benchMockLLM{}, ap.WithMaxTurns(1))
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch, _ := agent.StreamRun(context.Background(), ap.UserMessage("hello"))
		// 等待第一个事件
		if ch != nil {
			<-ch
		}
	}
}

// BenchmarkMemoryLatency 基准：记忆操作延迟
func BenchmarkMemoryLatency(b *testing.B) {
	memory, _ := ap.WithInMemory()
	defer memory.Close()
	ctx := context.Background()

	// 预填充数据
	for i := 0; i < 1000; i++ {
		_ = memory.Add(ctx, &ap.Episode{
			ID:      fmt.Sprintf("pre-%d", i),
			Content: fmt.Sprintf("预填充记忆条目 %d，包含一些常见关键词如文件、搜索、分析", i),
			Role:    "user",
		})
	}

	b.Run("Search_1K", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = memory.Search(ctx, "文件搜索", nil)
		}
	})
}

// BenchmarkVectorSearch 基准：向量搜索延迟
func BenchmarkVectorSearch(b *testing.B) {
	ctx := context.Background()
	store := ap.NewVectorStore(128)
	vectors := make([][]float32, 10000)
	for i := range vectors {
		v := make([]float32, 128)
		for j := range v {
			v[j] = float32(i*128+j) / 100000.0
		}
		_ = store.Add(ctx, fmt.Sprintf("vec-%d", i), v, nil)
	}

	query := make([]float32, 128)
	for i := range query {
		query[i] = 0.5
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = store.Search(ctx, query, 10)
	}
}
