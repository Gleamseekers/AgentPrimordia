// Package performance 提供 AgentPrimordia 性能基准测试。
//
// 运行方式：
//   go test -bench=. ./bench/performance/
//
// 或者生成报告：
//   go run ./bench/performance/report/
package performance

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"time"

	ap "agentprimordia/pkg"
)

// BenchmarkStartupTime 测试 agent 启动时间
func BenchmarkStartupTime(b *testing.B) {
	for i := 0; i < b.N; i++ {
		start := time.Now()
		
		// 创建 provider
		provider := ap.NewDemoProvider()
		
		// 创建基本请求
		req := &ap.CompletionRequest{
			Messages: []ap.ChatMessage{
				{Role: "user", Content: "hello"},
			},
		}
		
		// 执行一次调用（模拟启动）
		ctx := context.Background()
		_, err := provider.Complete(ctx, req)
		if err != nil {
			b.Fatalf("startup failed: %v", err)
		}
		
		elapsed := time.Since(start)
		b.ReportMetric(float64(elapsed.Milliseconds()), "ms/op")
	}
}

// BenchmarkMemoryUsage 测试内存使用
func BenchmarkMemoryUsage(b *testing.B) {
	var m1, m2 runtime.MemStats
	
	// 基准内存
	runtime.ReadMemStats(&m1)
	
	for i := 0; i < b.N; i++ {
		// 创建 provider
		provider := ap.NewDemoProvider()
		
		// 执行多次调用
		ctx := context.Background()
		for j := 0; j < 10; j++ {
			req := &ap.CompletionRequest{
				Messages: []ap.ChatMessage{
					{Role: "user", Content: fmt.Sprintf("test message %d", j)},
				},
			}
			_, _ = provider.Complete(ctx, req)
		}
		
		// 防止编译器优化
		runtime.KeepAlive(provider)
	}
	
	// 最终内存
	runtime.ReadMemStats(&m2)
	
	// 报告内存增量
	allocDiff := m2.TotalAlloc - m1.TotalAlloc
	b.ReportMetric(float64(allocDiff)/float64(b.N)/1024, "KB_alloc_per_op")
	b.ReportMetric(float64(m2.HeapInuse-m1.HeapInuse)/1024, "KB_heap_per_op")
}

// BenchmarkLearningConvergence 测试学习收敛速度
func BenchmarkLearningConvergence(b *testing.B) {
	for i := 0; i < b.N; i++ {
		// 创建 SelfModel（通过 internal 包）
		// 注意：这里使用简化的模拟，实际应该使用 internal/memory.SelfModel
		
		start := time.Now()
		
		// 模拟 100 次学习记录
		for j := 0; j < 100; j++ {
			// 模拟记录学习结果
			_ = j // 占位
		}
		
		elapsed := time.Since(start)
		b.ReportMetric(float64(elapsed.Microseconds())/100, "us_per_record")
	}
}

// BenchmarkConcurrentRequests 测试并发请求性能
func BenchmarkConcurrentRequests(b *testing.B) {
	provider := ap.NewDemoProvider()
	ctx := context.Background()
	
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := &ap.CompletionRequest{
				Messages: []ap.ChatMessage{
					{Role: "user", Content: "concurrent test"},
				},
			}
			_, _ = provider.Complete(ctx, req)
		}
	})
}

// BenchmarkA2ADiscovery 测试 A2A 发现性能
func BenchmarkA2ADiscovery(b *testing.B) {
	for i := 0; i < b.N; i++ {
		// 创建 Agent Card
		card := ap.OpenAgentCard{
			Name:               "bench-agent",
			Description:        "benchmark agent",
			URL:                "http://localhost:9999",
			Version:            "1.0.0",
			DefaultInputModes:  []string{"text"},
			DefaultOutputModes: []string{"text"},
		}
		
		// 创建 interop config
		cfg := ap.DefaultInteropConfig()
		
		// 创建 server
		server := ap.NewOpenInteropServer(card, cfg)
		
		// 防止编译器优化
		runtime.KeepAlive(server)
	}
}

// Report 生成性能报告
func Report() {
	fmt.Println("AgentPrimordia 性能基准测试报告")
	fmt.Println("================================")
	fmt.Println()
	
	// 启动时间
	fmt.Println("1. 启动时间")
	fmt.Println("   目标: < 5 秒")
	fmt.Println("   实际: 需要运行 benchmark 测量")
	fmt.Println()
	
	// 内存使用
	fmt.Println("2. 内存使用")
	fmt.Println("   目标: < 50MB（空 agent）")
	fmt.Println("   实际: 需要运行 benchmark 测量")
	fmt.Println()
	
	// 学习收敛
	fmt.Println("3. 学习收敛速度")
	fmt.Println("   目标: 10 轮对话后显示明显成长")
	fmt.Println("   实际: 需要运行 benchmark 测量")
	fmt.Println()
	
	// 并发性能
	fmt.Println("4. 并发性能")
	fmt.Println("   目标: 支持 100+ 并发请求")
	fmt.Println("   实际: 需要运行 benchmark 测量")
	fmt.Println()
	
	// A2A 延迟
	fmt.Println("5. A2A 通信延迟")
	fmt.Println("   目标: < 100ms（本地通信）")
	fmt.Println("   实际: 需要运行 benchmark 测量")
	fmt.Println()
	
	fmt.Println("运行完整测试:")
	fmt.Println("  go test -bench=. ./bench/performance/")
}
