// perf-v4 Task 12.4：LLM Cache 性能基线
// perf-v6 round 8 Task 1：SSE 解析热路径性能基线
// phase3 Task 6：LLM BatchProcessor 性能基线
package llm

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/jsonutil"
)

// BenchmarkCache_Get_FingerprintHit 精确指纹命中快路径
func BenchmarkCache_Get_FingerprintHit(b *testing.B) {
	cache := NewInMemoryCache(nil, 100, 0.9)
	ctx := context.Background()
	mockResp := &CompletionResponse{ID: "test", Model: "m", Content: "ok"}
	_ = cache.Set(ctx, "test query", mockResp)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = cache.Get(ctx, "test query", 0.9)
	}
}

// BenchmarkCache_Get_Miss 缓存未命中场景
func BenchmarkCache_Get_Miss(b *testing.B) {
	cache := NewInMemoryCache(nil, 100, 0.9)
	ctx := context.Background()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = cache.Get(ctx, "test query that is not in cache", 0.9)
	}
}

// BenchmarkCache_Set 写入场景
func BenchmarkCache_Set(b *testing.B) {
	cache := NewInMemoryCache(nil, 1000, 0.9)
	ctx := context.Background()
	mockResp := &CompletionResponse{ID: "test", Model: "m", Content: "ok"}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = cache.Set(ctx, "test query", mockResp)
	}
}

// BenchmarkCache_FullCycle Set + Get 完整周期
func BenchmarkCache_FullCycle(b *testing.B) {
	cache := NewInMemoryCache(nil, 1000, 0.9)
	ctx := context.Background()
	mockResp := &CompletionResponse{ID: "test", Model: "m", Content: "ok"}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = cache.Set(ctx, "test query", mockResp)
		_, _ = cache.Get(ctx, "test query", 0.9)
	}
}

// perf-v6 round 8 Task 1：SSE 解析热路径性能基线
// 对比 stdlib strings.NewReader 路径与 jsonutil.DecodeString（pooled stringReader）路径
const sseSampleData = `{"id":"chatcmpl-123","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4o-mini","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"},"finish_reason":null}],"usage":null}`

type sseBenchResp struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

// BenchmarkSSE_Decode_Stdlib 模拟 stdlib 路径：json.NewDecoder(strings.NewReader(data)).Decode(&v)
func BenchmarkSSE_Decode_Stdlib(b *testing.B) {
	for i := 0; i < b.N; i++ {
		var got sseBenchResp
		_ = json.NewDecoder(strings.NewReader(sseSampleData)).Decode(&got)
	}
}

// BenchmarkSSE_Decode_Jsonutil 模拟优化路径：jsonutil.DecodeString
func BenchmarkSSE_Decode_Jsonutil(b *testing.B) {
	for i := 0; i < b.N; i++ {
		var got sseBenchResp
		_ = jsonutil.DecodeString(sseSampleData, &got)
	}
}

// BenchmarkBatchProcessor_Concurrent 对比批处理器在并发场景下的吞吐
func BenchmarkBatchProcessor_Concurrent(b *testing.B) {
	mock := newBatchMockProvider()
	bp := NewBatchProcessor(mock, BatchConfig{
		MaxBatchSize: 16,
		FlushTimeout: 50 * time.Millisecond,
	})
	defer bp.Close()

	ctx := context.Background()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		for j := 0; j < 10; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = bp.Complete(ctx, &CompletionRequest{
					Messages: []ChatMessage{{Role: "user", Content: "bench"}},
					Model:    "batch-mock-model",
				})
			}()
		}
		wg.Wait()
	}
}

// BenchmarkBatchProcessor_vs_Direct 对比批处理 vs 直接调用的吞吐差异
func BenchmarkBatchProcessor_vs_Direct(b *testing.B) {
	mock := newBatchMockProvider()
	bp := NewBatchProcessor(mock, BatchConfig{
		MaxBatchSize: 16,
		FlushTimeout: 50 * time.Millisecond,
	})
	defer bp.Close()

	ctx := context.Background()
	b.Run("Batch", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			var wg sync.WaitGroup
			for j := 0; j < 10; j++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, _ = bp.Complete(ctx, &CompletionRequest{
						Messages: []ChatMessage{{Role: "user", Content: "x"}},
						Model:    "batch-mock-model",
					})
				}()
			}
			wg.Wait()
		}
	})
	b.Run("Direct", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			var wg sync.WaitGroup
			for j := 0; j < 10; j++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, _ = mock.Complete(ctx, &CompletionRequest{
						Messages: []ChatMessage{{Role: "user", Content: "x"}},
						Model:    "batch-mock-model",
					})
				}()
			}
			wg.Wait()
		}
	})
}
