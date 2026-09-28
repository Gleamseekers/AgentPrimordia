package llm

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// promptFingerprintCache 缓存 prompt → fingerprint（perf-v6 Task 7 + Task B）
// 使用 container/list + map 实现 O(1) LRU，避免 sync.Map 无限制增长
type fingerprintCache struct {
	mu    sync.Mutex
	lru   *list.List               // 双向链表：Front=最旧, Back=最新
	items map[string]*list.Element // prompt → list.Element
	max   int
}

func newFingerprintCache(max int) *fingerprintCache {
	if max <= 0 {
		max = defaultFingerprintCacheSize
	}
	return &fingerprintCache{
		lru:   list.New(),
		items: make(map[string]*list.Element, max),
		max:   max,
	}
}

func (c *fingerprintCache) get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.items[key]; ok {
		c.lru.MoveToBack(elem) // O(1) 升级
		return elem.Value.(*fpCacheEntry).value, true
	}
	return "", false
}

func (c *fingerprintCache) put(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.items[key]; ok {
		elem.Value.(*fpCacheEntry).value = value
		c.lru.MoveToBack(elem)
		return
	}
	// 淘汰最旧
	for c.lru.Len() >= c.max {
		front := c.lru.Front()
		if front == nil {
			break
		}
		c.lru.Remove(front)
		oldKey := front.Value.(*fpCacheEntry).key
		delete(c.items, oldKey)
	}
	// Value 同时存 key（用于淘汰时反向查 map）和 value
	entry := &fpCacheEntry{key: key, value: value}
	listElem := c.lru.PushBack(entry)
	c.items[key] = listElem
}

type fpCacheEntry struct {
	key, value string
}

// promptFingerprintCache 全局实例（perf-v6 Task B：限制大小 1000）
var promptFingerprintCache = newFingerprintCache(1000)

const (
	defaultFingerprintCacheSize = 100
	defaultFingerprintDim       = 64
	fingerprintTruncateLen      = 16
	fnvOffsetBasis              = 2166136261
	fnvPrime                    = 16777619
)

type CacheTracer interface {
	StartSpan(ctx context.Context, name string) (context.Context, SpanLike)
	EndSpan(ctx context.Context, span SpanLike, err error)
}

type SpanLike interface {
	SetAttribute(key string, value any)
}

type CacheManager struct {
	cache   LLMCache
	tracer  CacheTracer
	enabled bool
	mu      sync.RWMutex
}

type CacheManagerConfig struct {
	Cache   LLMCache
	Tracer  CacheTracer
	Enabled bool
}

type FingerprintCache struct {
	entries    map[string]*fingerprintEntry
	maxSize    int
	ttl        time.Duration
	mu         sync.RWMutex
	totalQuery int64
	hits       int64
	misses     int64
	tokensSave int64
}

type fingerprintEntry struct {
	response  *CompletionResponse
	createdAt time.Time
	hitCount  int
}

func NewFingerprintCache(maxSize int, ttl time.Duration) *FingerprintCache {
	if maxSize <= 0 {
		maxSize = defaultFingerprintCacheSize
	}
	return &FingerprintCache{
		entries: make(map[string]*fingerprintEntry),
		maxSize: maxSize,
		ttl:     ttl,
	}
}

func (c *FingerprintCache) Get(_ context.Context, query string, _ float32) (*CompletionResponse, bool) {
	key := PromptFingerprint(query)

	c.mu.Lock()
	defer c.mu.Unlock()

	atomicAdd(&c.totalQuery, 1)

	entry, ok := c.entries[key]
	if ok {
		if c.ttl > 0 && time.Since(entry.createdAt) > c.ttl {
			delete(c.entries, key)
			atomicAdd(&c.misses, 1)
			return nil, false
		}
		entry.hitCount++
		atomicAdd(&c.hits, 1)
		atomicAdd(&c.tokensSave, int64(entry.response.Usage.TotalTokens))
		return entry.response, true
	}

	atomicAdd(&c.misses, 1)
	return nil, false
}

func (c *FingerprintCache) Set(_ context.Context, query string, resp *CompletionResponse) error {
	key := PromptFingerprint(query)

	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.entries) >= c.maxSize {
		var oldestKey string
		var oldestTime time.Time
		for k, v := range c.entries {
			if oldestTime.IsZero() || v.createdAt.Before(oldestTime) {
				oldestKey = k
				oldestTime = v.createdAt
			}
		}
		if oldestKey != "" {
			delete(c.entries, oldestKey)
		}
	}

	c.entries[key] = &fingerprintEntry{response: resp, createdAt: time.Now()}
	return nil
}

// GetRequest 使用完整 CompletionRequest 作为缓存 key（修复 v6.x 评估 Issue #2）。
// 优先采用，若调用方仍使用 Get(query string)，key 仅基于最后一条 user 消息，
// 在多 system prompt / 多工具集下会产生语义错误的命中。
func (c *FingerprintCache) GetRequest(_ context.Context, req *CompletionRequest) (*CompletionResponse, bool) {
	key := RequestFingerprint(req)
	return c.getByKey(key)
}

// SetRequest 为完整 CompletionRequest 写入缓存。
func (c *FingerprintCache) SetRequest(_ context.Context, req *CompletionRequest, resp *CompletionResponse) error {
	key := RequestFingerprint(req)
	return c.setByKey(key, resp)
}

// getByKey 内部按 fingerprint 查找。
func (c *FingerprintCache) getByKey(key string) (*CompletionResponse, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	atomicAdd(&c.totalQuery, 1)

	entry, ok := c.entries[key]
	if !ok {
		atomicAdd(&c.misses, 1)
		return nil, false
	}
	if c.ttl > 0 && time.Since(entry.createdAt) > c.ttl {
		delete(c.entries, key)
		atomicAdd(&c.misses, 1)
		return nil, false
	}
	entry.hitCount++
	atomicAdd(&c.hits, 1)
	atomicAdd(&c.tokensSave, int64(entry.response.Usage.TotalTokens))
	return entry.response, true
}

// setByKey 内部按 fingerprint 写入。
func (c *FingerprintCache) setByKey(key string, resp *CompletionResponse) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.entries) >= c.maxSize {
		var oldestKey string
		var oldestTime time.Time
		for k, v := range c.entries {
			if oldestTime.IsZero() || v.createdAt.Before(oldestTime) {
				oldestKey = k
				oldestTime = v.createdAt
			}
		}
		if oldestKey != "" {
			delete(c.entries, oldestKey)
		}
	}

	c.entries[key] = &fingerprintEntry{response: resp, createdAt: time.Now()}
	return nil
}

// Stats 返回缓存统计
// v6.x 评估 §4.2 P1-1：计数器由 atomic.AddInt64 写入，Stats 必须用
// atomic.LoadInt64 读取，否则并发下构成数据竞争（-race 可检出）。
func (c *FingerprintCache) Stats(_ context.Context) CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	total := atomic.LoadInt64(&c.totalQuery)
	hits := atomic.LoadInt64(&c.hits)
	misses := atomic.LoadInt64(&c.misses)
	tokensSave := atomic.LoadInt64(&c.tokensSave)
	var hitRate float64
	if total > 0 {
		hitRate = float64(hits) / float64(total)
	}
	return CacheStats{TotalQueries: total, CacheHits: hits, CacheMisses: misses,
		HitRate: hitRate, EntryCount: len(c.entries), TokensSaved: tokensSave}
}

func (c *FingerprintCache) Clear(_ context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*fingerprintEntry)
	// v6.x 评估 §4.2 P1-1：重置走 atomic.StoreInt64，避免与并发 AddInt64 竞争
	atomic.StoreInt64(&c.totalQuery, 0)
	atomic.StoreInt64(&c.hits, 0)
	atomic.StoreInt64(&c.misses, 0)
	atomic.StoreInt64(&c.tokensSave, 0)
	return nil
}

func (c *FingerprintCache) Invalidate(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.entries {
		if strings.Contains(k, key) {
			delete(c.entries, k)
		}
	}
	return nil
}

type NoopCache struct{}

func (NoopCache) Get(context.Context, string, float32) (*CompletionResponse, bool) { return nil, false }
func (NoopCache) Set(context.Context, string, *CompletionResponse) error           { return nil }
func (NoopCache) Stats(_ context.Context) CacheStats                               { return CacheStats{} }
func (NoopCache) Clear(_ context.Context) error                                    { return nil }
func (NoopCache) Invalidate(_ context.Context, _ string) error                     { return nil }

// GetRequest / SetRequest 实现 RequestLLMCache（v6.x 评估 §4.2 P1-5）：
// NoopCache 永远不命中。
func (NoopCache) GetRequest(context.Context, *CompletionRequest) (*CompletionResponse, bool) {
	return nil, false
}
func (NoopCache) SetRequest(context.Context, *CompletionRequest, *CompletionResponse) error {
	return nil
}

type HybridCache struct {
	fingerprint LLMCache
	semantic    *InMemoryCache
}

func NewHybridCache(fp LLMCache, sem *InMemoryCache) (*HybridCache, error) {
	if fp == nil {
		return nil, fmt.Errorf("fingerprint cache must not be nil")
	}
	return &HybridCache{fingerprint: fp, semantic: sem}, nil
}

func (h *HybridCache) Get(ctx context.Context, query string, similarity float32) (*CompletionResponse, bool) {
	if resp, ok := h.fingerprint.Get(ctx, query, 0); ok {
		return resp, true
	}
	if h.semantic != nil {
		return h.semantic.Get(ctx, query, similarity)
	}
	return nil, false
}

func (h *HybridCache) Set(ctx context.Context, query string, resp *CompletionResponse) error {
	_ = h.fingerprint.Set(ctx, query, resp)
	if h.semantic != nil {
		return h.semantic.Set(ctx, query, resp)
	}
	return nil
}

func (h *HybridCache) Stats(ctx context.Context) CacheStats { return h.fingerprint.Stats(ctx) }
func (h *HybridCache) Clear(ctx context.Context) error {
	_ = h.fingerprint.Clear(ctx)
	if h.semantic != nil {
		_ = h.semantic.Clear(ctx)
	}
	return nil
}
func (h *HybridCache) Invalidate(ctx context.Context, key string) error {
	if ic, ok := h.fingerprint.(interface {
		Invalidate(context.Context, string) error
	}); ok {
		_ = ic.Invalidate(ctx, key)
	}
	if h.semantic != nil {
		_ = h.semantic.Invalidate(ctx, key)
	}
	return nil
}

// GetRequest / SetRequest 实现 RequestLLMCache（v6.x 评估 §4.2 P1-5）：
// 请求键查找优先走 fingerprint 层，未命中再走 semantic 层。
// 注意：semantic 为具体类型 *InMemoryCache（已实现 RequestLLMCache），直接调用。
func (h *HybridCache) GetRequest(ctx context.Context, req *CompletionRequest) (*CompletionResponse, bool) {
	if rc, ok := h.fingerprint.(RequestLLMCache); ok {
		if resp, hit := rc.GetRequest(ctx, req); hit {
			return resp, true
		}
	}
	if h.semantic != nil {
		return h.semantic.GetRequest(ctx, req)
	}
	return nil, false
}

func (h *HybridCache) SetRequest(ctx context.Context, req *CompletionRequest, resp *CompletionResponse) error {
	if rc, ok := h.fingerprint.(RequestLLMCache); ok {
		_ = rc.SetRequest(ctx, req, resp)
	}
	if h.semantic != nil {
		_ = h.semantic.SetRequest(ctx, req, resp)
	}
	return nil
}

// Close 释放底层缓存资源（v6.x 评估 §4.2 P1-6）：
// 传播到实现 io.Closer 的下层缓存（如 InMemoryCache 的 TTL 清理 goroutine）。
func (h *HybridCache) Close() error {
	if c, ok := h.fingerprint.(interface{ Close() error }); ok {
		if err := c.Close(); err != nil {
			return err
		}
	}
	if h.semantic != nil {
		if err := h.semantic.Close(); err != nil {
			return err
		}
	}
	return nil
}

func PromptFingerprint(text string) string {
	// perf-v6 Task 7：缓存 prompt → fingerprint，避免重复 sha256
	if v, ok := promptFingerprintCache.get(text); ok {
		return v
	}
	normalized := normalizeText(text)
	hash := sha256.Sum256([]byte(normalized))
	fp := hex.EncodeToString(hash[:])[:16]
	promptFingerprintCache.put(text, fp)
	return fp
}

// RequestFingerprint 为完整的 CompletionRequest 生成稳定的指纹。
//
// 设计动机（修复 v6.x 评估报告 Issue #2）：
//  1. 仅基于"最后一条 user 消息"做 sha256 会让不同 system prompt /
//     工具列表 / 历史消息的相同 query 命中同一缓存，产生语义错误
//     的"假命中"。
//  2. 真正的 cache key 必须把"完整输入空间"折叠成指纹。
//
// 算法：
//   - System 角色消息 / 用户消息 / 助手消息 / 工具消息
//     按 "role\x1fcontent\x1ftoolName" 形式拼接后做规范化 + sha256。
//   - 工具定义按 Function.Name 排序后拼接 name + parameters 的稳定 JSON。
//   - Model 字段作为前缀参与，避免跨模型串缓存。
//   - temperature / max_tokens / response_format 参与指纹
//     （v6.x 评估 §4.2 P1-5：同消息不同采样参数不得串缓存）。
//
// 安全：使用 \x1f (US) 作为分隔符，规避 user content 自带的换行注入风险。
//
// 性能：通过 promptFingerprintCache (LRU) 缓存常见请求，避免重复 sha256。
func RequestFingerprint(req *CompletionRequest) string {
	if req == nil {
		return ""
	}
	var b strings.Builder
	b.Grow(512)
	// model 是 key 前缀
	if req.Model != "" {
		b.WriteString("model=")
		b.WriteString(req.Model)
		b.WriteByte('\x1f')
	}
	// 消息序列
	for _, m := range req.Messages {
		b.WriteString("msg:")
		b.WriteString(strings.ToLower(m.Role))
		b.WriteByte('\x1f')
		b.WriteString(strings.TrimSpace(m.Content))
		b.WriteByte('\x1f')
		if len(m.ToolCalls) > 0 {
			b.WriteString(strings.ToLower(m.ToolCallID))
		}
		b.WriteByte('\n')
	}
	// v6.x 评估 §4.2 P1-5：采样参数参与指纹。
	// Temperature 为指针：显式 0 与"未设置"必须区分（不同默认温度解析结果）。
	if req.Temperature != nil {
		b.WriteString("temp=")
		b.WriteString(strconv.FormatFloat(*req.Temperature, 'g', -1, 64))
		b.WriteByte('\x1f')
	}
	if req.MaxTokens > 0 {
		b.WriteString("max_tokens=")
		b.WriteString(strconv.Itoa(req.MaxTokens))
		b.WriteByte('\x1f')
	}
	if req.ResponseFormat != nil {
		b.WriteString("rf=")
		b.WriteString(string(req.ResponseFormat.Type))
		if req.ResponseFormat.JSONSchema != nil {
			b.WriteByte('\x1f')
			b.WriteString(req.ResponseFormat.JSONSchema.Name)
			// encoding/json 对 map key 排序，序列化稳定
			if raw, err := json.Marshal(req.ResponseFormat.JSONSchema.Schema); err == nil {
				b.WriteByte('\x1f')
				b.Write(raw)
			}
		}
		b.WriteByte('\n')
	}
	canonical := b.String()
	if v, ok := promptFingerprintCache.get(canonical); ok {
		return v
	}
	hash := sha256.Sum256([]byte(canonical))
	fp := hex.EncodeToString(hash[:])[:16]
	promptFingerprintCache.put(canonical, fp)
	return fp
}

// ToolCallRequestFingerprint 为 ToolCallRequest 生成稳定指纹。
//
// 设计动机与 RequestFingerprint 一致：把"工具列表+消息历史"纳入
// 缓存 key，避免不同工具集下相同 query 错误命中。
func ToolCallRequestFingerprint(req *ToolCallRequest) string {
	if req == nil {
		return ""
	}
	var b strings.Builder
	b.Grow(512)
	if req.Model != "" {
		b.WriteString("model=")
		b.WriteString(req.Model)
		b.WriteByte('\x1f')
	}
	// 工具：按 name 排序以保证稳定性
	toolNames := make([]string, 0, len(req.Tools))
	toolSchema := make(map[string]string, len(req.Tools))
	for _, t := range req.Tools {
		toolNames = append(toolNames, t.Function.Name)
		if t.Function.Parameters != nil {
			// 仅取 schema 字符串形式，不走 json.Marshal 避免非确定 map 顺序
			toolSchema[t.Function.Name] = fmt.Sprintf("%v", t.Function.Parameters)
		}
	}
	sort.Strings(toolNames)
	for _, name := range toolNames {
		b.WriteString("tool:")
		b.WriteString(strings.ToLower(name))
		b.WriteByte('\x1f')
		b.WriteString(toolSchema[name])
		b.WriteByte('\n')
	}
	// 消息
	for _, m := range req.Messages {
		b.WriteString("msg:")
		b.WriteString(strings.ToLower(m.Role))
		b.WriteByte('\x1f')
		b.WriteString(strings.TrimSpace(m.Content))
		b.WriteByte('\n')
	}
	canonical := b.String()
	if v, ok := promptFingerprintCache.get(canonical); ok {
		return v
	}
	hash := sha256.Sum256([]byte(canonical))
	fp := hex.EncodeToString(hash[:])[:16]
	promptFingerprintCache.put(canonical, fp)
	return fp
}

func normalizeText(text string) string {
	normalized := strings.ToLower(strings.TrimSpace(text))
	normalized = collapseWhitespace(normalized)
	return normalized
}

func collapseWhitespace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := true
	for _, r := range s {
		isSpace := r == ' ' || r == '\t' || r == '\n' || r == '\r'
		if isSpace && prevSpace {
			continue
		}
		if isSpace {
			b.WriteByte(' ')
		} else {
			b.WriteRune(r)
		}
		prevSpace = isSpace
	}
	return strings.TrimSpace(b.String())
}

func fingerprintToVector(text string) []float32 {
	normalized := strings.ToLower(strings.TrimSpace(text))
	normalized = collapseWhitespace(normalized)
	if normalized == "" {
		return make([]float32, defaultFingerprintDim)
	}

	tokens := tokenize(normalized)
	dim := defaultFingerprintDim
	vec := make([]float32, dim)

	for pos, token := range tokens {
		h1 := hashToken(token)
		h2 := hashToken(token + "_pos")
		idx1 := int(h1) % dim
		idx2 := int(h2) % dim
		weight := float32(1.0 + float32(pos)*0.01)
		vec[idx1] += weight
		vec[(idx2+1)%dim] += weight * 0.5

		if len(token) >= 2 {
			for i := 0; i < len(token)-1; i++ {
				bigram := token[i : i+2]
				h3 := hashToken(bigram)
				vec[int(h3)%dim] += weight * 0.3
			}
		}
	}

	for i := 1; i < len(tokens); i++ {
		bigramTok := tokens[i-1] + " " + tokens[i]
		h4 := hashToken(bigramTok)
		vec[int(h4)%dim] += 0.8
	}

	norm := float32(0)
	for _, v := range vec {
		norm += v * v
	}
	if norm > 0 {
		sqrtNorm := float32(math.Sqrt(float64(norm)))
		for i := range vec {
			vec[i] /= sqrtNorm
		}
	}
	return vec
}

func tokenize(text string) []string {
	var tokens []string
	var buf strings.Builder
	for _, r := range text {
		if isWordChar(r) {
			buf.WriteRune(r)
		} else {
			if buf.Len() > 0 {
				tokens = append(tokens, buf.String())
				buf.Reset()
			}
		}
	}
	if buf.Len() > 0 {
		tokens = append(tokens, buf.String())
	}
	return tokens
}

func isWordChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_'
}

func hashToken(s string) uint32 {
	h := uint32(fnvOffsetBasis)
	for _, c := range s {
		h ^= uint32(c)
		h *= fnvPrime
	}
	return h
}

func NewCacheManager(cfg CacheManagerConfig) *CacheManager {
	return &CacheManager{cache: cfg.Cache, tracer: cfg.Tracer, enabled: cfg.Enabled}
}

func (m *CacheManager) Get(ctx context.Context, query string, similarity float32) (*CompletionResponse, bool) {
	m.mu.RLock()
	enabled := m.enabled
	tracer := m.tracer
	cache := m.cache
	m.mu.RUnlock()

	if !enabled || cache == nil {
		return nil, false
	}

	resp, hit := cache.Get(ctx, query, similarity)

	if tracer != nil {
		ctx2, span := tracer.StartSpan(ctx, "cache.lookup")
		span.SetAttribute("cache.hit", hit)
		span.SetAttribute("cache.query_length", len(query))
		if hit {
			span.SetAttribute("cache.response_id", resp.ID)
		}
		tracer.EndSpan(ctx2, span, nil)
	}

	return resp, hit
}

func (m *CacheManager) Set(ctx context.Context, query string, resp *CompletionResponse) error {
	m.mu.RLock()
	cache := m.cache
	m.mu.RUnlock()
	if cache == nil {
		return nil
	}
	return cache.Set(ctx, query, resp)
}

func (m *CacheManager) Stats(ctx context.Context) CacheStats {
	m.mu.RLock()
	cache := m.cache
	m.mu.RUnlock()
	if cache == nil {
		return CacheStats{}
	}
	return cache.Stats(ctx)
}

func (m *CacheManager) Enable(enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enabled = enabled
}

func (m *CacheManager) Invalidate(ctx context.Context, key string) error {
	m.mu.RLock()
	cache := m.cache
	m.mu.RUnlock()
	if inv, ok := cache.(interface {
		Invalidate(context.Context, string) error
	}); ok {
		return inv.Invalidate(ctx, key)
	}
	return nil
}

func (m *CacheManager) Clear(ctx context.Context) error {
	m.mu.RLock()
	cache := m.cache
	m.mu.RUnlock()
	if cache != nil {
		return cache.Clear(ctx)
	}
	return nil
}

// GetRequest 委托底层缓存的完整请求键查找（v6.x 评估 §4.2 P1-5）。
// enabled=false 或底层缓存不支持请求键时返回未命中。
func (m *CacheManager) GetRequest(ctx context.Context, req *CompletionRequest) (*CompletionResponse, bool) {
	m.mu.RLock()
	enabled := m.enabled
	cache := m.cache
	m.mu.RUnlock()

	if !enabled || cache == nil {
		return nil, false
	}
	rc, ok := cache.(RequestLLMCache)
	if !ok {
		return nil, false
	}
	return rc.GetRequest(ctx, req)
}

// SetRequest 委托底层缓存的完整请求键写入（v6.x 评估 §4.2 P1-5）。
func (m *CacheManager) SetRequest(ctx context.Context, req *CompletionRequest, resp *CompletionResponse) error {
	m.mu.RLock()
	cache := m.cache
	m.mu.RUnlock()
	if cache == nil {
		return nil
	}
	rc, ok := cache.(RequestLLMCache)
	if !ok {
		return nil
	}
	return rc.SetRequest(ctx, req, resp)
}
