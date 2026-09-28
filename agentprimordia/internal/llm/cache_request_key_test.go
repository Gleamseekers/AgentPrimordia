package llm

import (
	"context"
	"testing"
	"time"
)

// TestCachedProvider_RequestKeyDistinguishesSystemPrompt 复现 v6.x 评估 §4.2 P1-5：
// CachedProvider.Complete 仅用 extractLastUserQuery（最后一条 user 消息）做缓存键，
// 不同 system prompt / 模型 / 参数的相同 query 会互相"假命中"。
// 修复：切换到 GetRequest/SetRequest（RequestFingerprint 覆盖完整输入空间）。
func TestCachedProvider_RequestKeyDistinguishesSystemPrompt(t *testing.T) {
	inner := &mockProviderForCache{
		response: &CompletionResponse{ID: "resp", Content: "answer", Usage: Usage{TotalTokens: 10}},
	}
	cache := NewInMemoryCache(dummyEmbedder, 100, 0.8)

	cached, err := NewCachedProvider(inner, cache, 0.8)
	if err != nil {
		t.Fatalf("NewCachedProvider: %v", err)
	}

	reqA := &CompletionRequest{
		Messages: []ChatMessage{
			{Role: "system", Content: "You are a weather expert."},
			{Role: "user", Content: "hello"},
		},
	}
	reqB := &CompletionRequest{
		Messages: []ChatMessage{
			{Role: "system", Content: "You are a travel agent."},
			{Role: "user", Content: "hello"}, // 相同 user query
		},
	}

	if _, err := cached.Complete(context.Background(), reqA); err != nil {
		t.Fatalf("Complete reqA: %v", err)
	}
	if _, err := cached.Complete(context.Background(), reqB); err != nil {
		t.Fatalf("Complete reqB: %v", err)
	}
	if inner.callCount != 2 {
		t.Fatalf("inner callCount = %d, want 2（不同 system prompt 不得假命中）", inner.callCount)
	}

	// 重复 reqA 必须命中（键稳定性）
	if _, err := cached.Complete(context.Background(), reqA); err != nil {
		t.Fatalf("Complete reqA repeat: %v", err)
	}
	if inner.callCount != 2 {
		t.Errorf("inner callCount = %d, want 2（重复请求应命中缓存）", inner.callCount)
	}
}

// TestCachedProvider_RequestKeyDistinguishesTemperature 验证
// temperature 参与缓存键（修复后 RequestFingerprint 纳入 temperature）。
func TestCachedProvider_RequestKeyDistinguishesTemperature(t *testing.T) {
	inner := &mockProviderForCache{
		response: &CompletionResponse{ID: "resp", Content: "answer"},
	}
	cache := NewInMemoryCache(dummyEmbedder, 100, 0.8)

	cached, err := NewCachedProvider(inner, cache, 0.8)
	if err != nil {
		t.Fatalf("NewCachedProvider: %v", err)
	}

	tempLow, tempHigh := 0.1, 0.9
	reqLow := &CompletionRequest{
		Messages:    []ChatMessage{{Role: "user", Content: "hi"}},
		Temperature: &tempLow,
	}
	reqHigh := &CompletionRequest{
		Messages:    []ChatMessage{{Role: "user", Content: "hi"}},
		Temperature: &tempHigh,
	}

	_, _ = cached.Complete(context.Background(), reqLow)
	_, _ = cached.Complete(context.Background(), reqHigh)

	if inner.callCount != 2 {
		t.Errorf("inner callCount = %d, want 2（不同 temperature 不得假命中）", inner.callCount)
	}
}

// TestCachedProvider_RequestKeyDistinguishesHistory 验证消息历史参与缓存键：
// 相同最后一条 user 消息、不同历史 → 不得假命中。
func TestCachedProvider_RequestKeyDistinguishesHistory(t *testing.T) {
	inner := &mockProviderForCache{
		response: &CompletionResponse{ID: "resp", Content: "answer"},
	}
	cache := NewInMemoryCache(dummyEmbedder, 100, 0.8)

	cached, err := NewCachedProvider(inner, cache, 0.8)
	if err != nil {
		t.Fatalf("NewCachedProvider: %v", err)
	}

	reqA := &CompletionRequest{
		Messages: []ChatMessage{
			{Role: "user", Content: "hi"},
		},
	}
	reqB := &CompletionRequest{
		Messages: []ChatMessage{
			{Role: "user", Content: "previous turn"},
			{Role: "assistant", Content: "previous answer"},
			{Role: "user", Content: "hi"}, // 相同最后一条 user 消息
		},
	}

	_, _ = cached.Complete(context.Background(), reqA)
	_, _ = cached.Complete(context.Background(), reqB)

	if inner.callCount != 2 {
		t.Errorf("inner callCount = %d, want 2（不同历史不得假命中）", inner.callCount)
	}
}

// TestRequestFingerprint_DistinguishesTemperature 验证 temperature 参与指纹。
func TestRequestFingerprint_DistinguishesTemperature(t *testing.T) {
	base := &CompletionRequest{
		Model:    "gpt-4",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	}
	t1, t2 := 0.1, 0.9
	r1 := *base
	r1.Temperature = &t1
	r2 := *base
	r2.Temperature = &t2

	fp1 := RequestFingerprint(&r1)
	fp2 := RequestFingerprint(&r2)
	if fp1 == fp2 {
		t.Fatal("temperature must affect fingerprint")
	}
	// 显式 0 与未设置必须区分（指针语义）
	r3 := *base
	zero := 0.0
	r3.Temperature = &zero
	if RequestFingerprint(&r3) == RequestFingerprint(base) {
		t.Error("explicit temperature=0 must differ from unset")
	}
}

// TestRequestFingerprint_DistinguishesMaxTokens 验证 max_tokens 参与指纹。
func TestRequestFingerprint_DistinguishesMaxTokens(t *testing.T) {
	r1 := &CompletionRequest{Model: "m", Messages: []ChatMessage{{Role: "user", Content: "hi"}}, MaxTokens: 100}
	r2 := &CompletionRequest{Model: "m", Messages: []ChatMessage{{Role: "user", Content: "hi"}}, MaxTokens: 200}
	if RequestFingerprint(r1) == RequestFingerprint(r2) {
		t.Fatal("max_tokens must affect fingerprint")
	}
}

// TestRequestFingerprint_DistinguishesResponseFormat 验证 response_format 参与指纹。
func TestRequestFingerprint_DistinguishesResponseFormat(t *testing.T) {
	r1 := &CompletionRequest{Model: "m", Messages: []ChatMessage{{Role: "user", Content: "hi"}}}
	r2 := &CompletionRequest{
		Model:    "m",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
		ResponseFormat: &ResponseFormat{
			Type: ResponseFormatJSONObject,
		},
	}
	r3 := &CompletionRequest{
		Model:    "m",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
		ResponseFormat: &ResponseFormat{
			Type: ResponseFormatJSONSchema,
			JSONSchema: &SchemaDef{
				Name:   "out",
				Schema: map[string]any{"type": "object"},
			},
		},
	}
	if RequestFingerprint(r1) == RequestFingerprint(r2) {
		t.Error("response_format presence must affect fingerprint")
	}
	if RequestFingerprint(r2) == RequestFingerprint(r3) {
		t.Error("response_format type/schema must affect fingerprint")
	}
}

// TestInMemoryCache_RequestKeyRoundTrip 验证 InMemoryCache 的
// GetRequest/SetRequest 往返：相同请求命中，不同请求不命中。
func TestInMemoryCache_RequestKeyRoundTrip(t *testing.T) {
	cache := NewInMemoryCache(dummyEmbedder, 100, 0.8)
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

// TestInMemoryCache_RequestKeyTTL 验证请求键条目同样受 TTL 约束。
func TestInMemoryCache_RequestKeyTTL(t *testing.T) {
	cache := NewInMemoryCacheWithFullConfig(InMemoryCacheFullConfig{
		MaxSize: 100,
		TTL:     50 * time.Millisecond,
	})
	defer cache.Close()
	ctx := context.Background()

	req := &CompletionRequest{Model: "m", Messages: []ChatMessage{{Role: "user", Content: "hi"}}}
	resp := &CompletionResponse{ID: "r", Content: "cached"}
	if err := cache.SetRequest(ctx, req, resp); err != nil {
		t.Fatalf("SetRequest: %v", err)
	}
	if _, ok := cache.GetRequest(ctx, req); !ok {
		t.Fatal("expected hit before TTL expiry")
	}
	time.Sleep(80 * time.Millisecond)
	if _, ok := cache.GetRequest(ctx, req); ok {
		t.Error("expected miss after TTL expiry")
	}
}

// TestCacheManager_RequestKeyDelegation 验证 CacheManager 把
// GetRequest/SetRequest 委托给底层支持请求键的缓存。
func TestCacheManager_RequestKeyDelegation(t *testing.T) {
	fc := NewFingerprintCache(100, time.Hour)
	mgr := NewCacheManager(CacheManagerConfig{Cache: fc, Enabled: true})
	ctx := context.Background()

	req := &CompletionRequest{
		Model:    "gpt-4",
		Messages: []ChatMessage{{Role: "system", Content: "s"}, {Role: "user", Content: "hi"}},
	}
	resp := &CompletionResponse{ID: "r", Content: "via manager"}

	if err := mgr.SetRequest(ctx, req, resp); err != nil {
		t.Fatalf("SetRequest: %v", err)
	}
	got, ok := mgr.GetRequest(ctx, req)
	if !ok || got.Content != "via manager" {
		t.Fatalf("GetRequest via manager = %v, %v; want hit", got, ok)
	}

	// 禁用后不得命中
	mgr.Enable(false)
	if _, ok := mgr.GetRequest(ctx, req); ok {
		t.Error("disabled manager must not hit")
	}
}
