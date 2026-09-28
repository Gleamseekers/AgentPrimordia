package llm

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestInMemoryCache_StatsConcurrent 复现 v6.x 评估 §4.2 P1-1：
// InMemoryCache.Stats() 以普通读读取 atomic 写的计数器（totalQuery/hits/misses/tokensSave），
// 并发 Get/Set + Stats 下构成数据竞争（-race 可检出）。
func TestInMemoryCache_StatsConcurrent(t *testing.T) {
	cache := NewInMemoryCache(dummyEmbedder, 1000, 0.5)
	resp := &CompletionResponse{ID: "r", Content: "a", Usage: Usage{TotalTokens: 10}}
	ctx := context.Background()
	if err := cache.Set(ctx, "q", resp); err != nil {
		t.Fatalf("Set: %v", err)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})
	// 写侧：并发 Get（atomic.AddInt64 计数器）
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					cache.Get(ctx, "q", 0.5)
				}
			}
		}()
	}
	// 读侧：并发 Stats（修复前为普通读）
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = cache.Stats(ctx)
			}
		}
	}()
	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// TestInMemoryCache_ClearConcurrent 复现同一竞争的另一半：
// Clear() 以普通写重置计数器，与并发 atomic.AddInt64 构成竞争。
func TestInMemoryCache_ClearConcurrent(t *testing.T) {
	cache := NewInMemoryCache(dummyEmbedder, 1000, 0.5)
	resp := &CompletionResponse{ID: "r", Content: "a"}
	ctx := context.Background()

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = cache.Set(ctx, "q", resp)
					cache.Get(ctx, "q", 0.5)
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = cache.Clear(ctx)
			}
		}
	}()
	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// TestFingerprintCache_StatsConcurrent 复现 FingerprintCache.Stats() 的同类竞争。
func TestFingerprintCache_StatsConcurrent(t *testing.T) {
	cache := NewFingerprintCache(1000, time.Hour)
	resp := &CompletionResponse{ID: "r", Content: "a", Usage: Usage{TotalTokens: 10}}
	ctx := context.Background()
	if err := cache.Set(ctx, "q", resp); err != nil {
		t.Fatalf("Set: %v", err)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					cache.Get(ctx, "q", 0)
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = cache.Stats(ctx)
			}
		}
	}()
	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()
}
