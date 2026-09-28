package llm

import (
	"context"
	"testing"
	"time"
)

// TestInMemoryCache_CloseStopsTTLLoop 复现 v6.x 评估 §4.2 P1-6：
// NewInMemoryCacheWithFullConfig 在 TTL > 0 时启动 ttlCleanupLoop goroutine，
// 但没有任何停止机制 → 缓存淘汰后 goroutine 永久泄漏（进程生命周期内每缓存一个）。
// 修复：stopCh + Close()，goroutine select 监听停止信号。
func TestInMemoryCache_CloseStopsTTLLoop(t *testing.T) {
	c := NewInMemoryCacheWithFullConfig(InMemoryCacheFullConfig{
		MaxSize: 10,
		TTL:     20 * time.Millisecond,
	})
	if c.loopDone == nil {
		t.Fatal("TTL > 0 时应启动清理 goroutine（loopDone 未初始化）")
	}

	// 等待 loop 至少完成一个 tick（ interval = max(TTL/2, 1s) = 1s 太长，
	// 这里只验证 Close 能让它退出，不依赖 tick）
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case <-c.loopDone:
		// goroutine 已退出
	case <-time.After(2 * time.Second):
		t.Fatal("Close 后 TTL 清理 goroutine 未退出（泄漏）")
	}

	// Close 必须幂等：重复调用不 panic
	if err := c.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// TestInMemoryCache_NoTTLNoGoroutine 验证 TTL <= 0 时不启动 goroutine，
// Close 依然可安全调用。
func TestInMemoryCache_NoTTLNoGoroutine(t *testing.T) {
	c := NewInMemoryCacheWithFullConfig(InMemoryCacheFullConfig{MaxSize: 10})
	if c.loopDone != nil {
		t.Fatal("TTL <= 0 时不应启动清理 goroutine")
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close without TTL: %v", err)
	}
}

// TestCleanupExpired_RemovesLSHIndex 复现 v6.x 评估 §4.2 P1-7：
// cleanupExpired 只从 lruList/lruMap 删除过期 entry，不清理 LSH 索引 →
// 已淘汰 entry 仍被 probeCandidates 返回（慢路径反复扫描死对象，且阻止 GC）。
func TestCleanupExpired_RemovesLSHIndex(t *testing.T) {
	c := NewInMemoryCacheWithFullConfig(InMemoryCacheFullConfig{
		MaxSize: 10,
		TTL:     50 * time.Millisecond,
	})
	defer c.Close()
	ctx := context.Background()

	const query = "query about golang concurrency"
	resp := &CompletionResponse{ID: "r", Content: "answer"}
	if err := c.Set(ctx, query, resp); err != nil {
		t.Fatalf("Set: %v", err)
	}

	fp := PromptFingerprint(query)
	elem, ok := c.lruMap[fp]
	if !ok {
		t.Fatal("precondition: entry should exist in lruMap")
	}
	entry := elem.Value.(*CacheEntry)
	if entry.vector == nil {
		t.Fatal("precondition: entry should have a vector")
	}

	// 前置条件：LSH 索引包含该 entry
	if !lshContains(c, entry) {
		t.Fatal("precondition: LSH index should contain entry")
	}

	// 等待过期后手动触发清理（不依赖后台 loop 的 1s 间隔）
	time.Sleep(80 * time.Millisecond)
	c.cleanupExpired(50 * time.Millisecond)

	if _, ok := c.lruMap[fp]; ok {
		t.Error("lruMap 应已删除过期 entry")
	}
	if lshContains(c, entry) {
		t.Error("cleanupExpired 未清理 LSH 索引：过期 entry 仍被 probeCandidates 返回")
	}
}

// lshContains 判断 entry 是否仍在 LSH 候选集中（测试辅助）。
func lshContains(c *InMemoryCache, entry *CacheEntry) bool {
	for _, e := range c.lsh.probeCandidates(entry.vector) {
		if e == entry {
			return true
		}
	}
	return false
}
