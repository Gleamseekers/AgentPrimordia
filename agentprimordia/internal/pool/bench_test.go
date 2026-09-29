package pool

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm"
)

// makeBenchTasks 生成指定数量的 TaskConfig
func makeBenchTasks(n int) []TaskConfig {
	tasks := make([]TaskConfig, n)
	for i := 0; i < n; i++ {
		tasks[i] = TaskConfig{
			ID:     fmt.Sprintf("bench-task-%d", i),
			Title:  fmt.Sprintf("Bench Task %d", i),
			Prompt: "benchmark prompt",
		}
	}
	return tasks
}

// BenchmarkPool_Dispatch_10Agents 测试 10 个并发任务的调度开销
func BenchmarkPool_Dispatch_10Agents(b *testing.B) {
	b.ReportAllocs()

	tasks := makeBenchTasks(10)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		mockLLM := llm.NewMockLLM(nil)
		for j := 0; j < 10; j++ {
			mockLLM.WithResponse("done")
		}

		pool := NewPool(PoolConfig{
			MaxConcurrency: 10,
		})
		pool.SetModel(mockLLM)
		b.StartTimer()

		_, err := pool.Dispatch(context.Background(), tasks)
		if err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
		pool.Close()
	}
}

// BenchmarkPool_Dispatch_100Agents 测试 100 个并发任务的调度开销
func BenchmarkPool_Dispatch_100Agents(b *testing.B) {
	b.ReportAllocs()

	tasks := makeBenchTasks(100)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		mockLLM := llm.NewMockLLM(nil)
		for j := 0; j < 100; j++ {
			mockLLM.WithResponse("done")
		}

		pool := NewPool(PoolConfig{
			MaxConcurrency: 100,
		})
		pool.SetModel(mockLLM)
		b.StartTimer()

		_, err := pool.Dispatch(context.Background(), tasks)
		if err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
		pool.Close()
	}
}

// BenchmarkPoolTailLatency v5.1 调度质量：Pool 调度尾延迟分布（P50/P95/P99）。
//
// 每批并发调度 10 个任务并测量整批墙钟时间；批次间波动即延迟分布。
// 输出 p50-ns / p95-ns / p99-ns 自定义指标，供尾延迟基线入库
// （bench/results/2026-Q3-v5.1-pool-tail-latency.json）与回归门解析。
//
// 运行：
//
//	go test -bench=BenchmarkPoolTailLatency -benchtime=300x -run=^$ ./internal/pool
func BenchmarkPoolTailLatency(b *testing.B) {
	const batchSize = 10
	tasks := makeBenchTasks(batchSize)
	latencies := make([]float64, 0, b.N)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		mockLLM := llm.NewMockLLM(nil)
		for j := 0; j < batchSize; j++ {
			mockLLM.WithResponse("done")
		}
		pool := NewPool(PoolConfig{
			MaxConcurrency: batchSize,
		})
		pool.SetModel(mockLLM)
		b.StartTimer()

		start := time.Now()
		if _, err := pool.Dispatch(context.Background(), tasks); err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
		latencies = append(latencies, float64(time.Since(start).Nanoseconds()))
		b.StopTimer()
		pool.Close()
	}
	b.StartTimer()

	pct := func(p float64) float64 {
		sorted := append([]float64(nil), latencies...)
		sort.Float64s(sorted)
		return sorted[int(float64(len(sorted)-1)*p)]
	}
	b.ReportMetric(pct(0.50), "p50-ns")
	b.ReportMetric(pct(0.95), "p95-ns")
	b.ReportMetric(pct(0.99), "p99-ns")
}
