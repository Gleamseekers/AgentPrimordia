//go:build sqlite
// +build sqlite

package llm

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestSQLiteCache_StatsConcurrent 复现 v6.x 评估 §4.2 P1-1 的 SQLite 变体：
// Stats() 以普通读读取 atomic 写的计数器，并发下构成数据竞争。
func TestSQLiteCache_StatsConcurrent(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "cache.db")
	cache, err := NewSQLiteCache(dsn)
	if err != nil {
		t.Fatalf("NewSQLiteCache: %v", err)
	}
	defer cache.Close()

	ctx := context.Background()
	resp := &CompletionResponse{ID: "r", Content: "a", Usage: Usage{TotalTokens: 10}}
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

// TestSQLiteCache_RequestKeyRoundTrip 验证 SQLiteCache 的请求键 API 往返。
func TestSQLiteCache_RequestKeyRoundTrip(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "cache.db")
	cache, err := NewSQLiteCache(dsn)
	if err != nil {
		t.Fatalf("NewSQLiteCache: %v", err)
	}
	defer cache.Close()

	ctx := context.Background()
	req := &CompletionRequest{
		Model:    "gpt-4",
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	}
	resp := &CompletionResponse{ID: "r", Content: "cached", Usage: Usage{TotalTokens: 7}}

	if err := cache.SetRequest(ctx, req, resp); err != nil {
		t.Fatalf("SetRequest: %v", err)
	}
	got, ok := cache.GetRequest(ctx, req)
	if !ok || got.Content != "cached" {
		t.Fatalf("GetRequest(req) = %v, %v; want hit cached", got, ok)
	}

	other := &CompletionRequest{
		Model:    "gpt-4",
		Messages: []ChatMessage{{Role: "system", Content: "different"}, {Role: "user", Content: "hello"}},
	}
	if _, ok := cache.GetRequest(ctx, other); ok {
		t.Error("GetRequest(other) should miss（键必须覆盖完整请求）")
	}
}
