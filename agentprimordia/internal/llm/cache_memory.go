package llm

// 本文件从 cache.go 拆分而来，包含 InMemoryCache 的实现。

import (
	"container/list"
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type CacheEntry struct {
	Key       string              `json:"key"`
	Query     string              `json:"query"`
	Response  *CompletionResponse `json:"response"`
	CreatedAt time.Time           `json:"created_at"`
	HitCount  atomic.Int64        `json:"hit_count"`
	Model     string              `json:"model"`
	vector    []float32
}

// AddHit 原子地增加 HitCount（perf-v6 Task B）
func (e *CacheEntry) AddHit() int64 {
	return e.HitCount.Add(1)
}

// GetHits 获取当前 HitCount（perf-v6 Task B）
func (e *CacheEntry) GetHits() int64 {
	return e.HitCount.Load()
}

// CacheStats 缓存统计
type CacheStats struct {
	TotalQueries int64   `json:"total_queries"`
	CacheHits    int64   `json:"cache_hits"`
	CacheMisses  int64   `json:"cache_misses"`
	HitRate      float64 `json:"hit_rate"`
	EntryCount   int     `json:"entry_count"`
	TokensSaved  int64   `json:"tokens_saved"`
	CostSavedUSD float64 `json:"cost_saved_usd"`
}

// EmbeddingFunc 文本向量化函数
type EmbeddingFunc func(ctx context.Context, text string) ([]float32, error)

// LLMCache LLM 缓存接口
type LLMCache interface {
	Get(ctx context.Context, query string, similarity float32) (*CompletionResponse, bool)
	Set(ctx context.Context, query string, resp *CompletionResponse) error
	Stats(ctx context.Context) CacheStats
	Clear(ctx context.Context) error
	Invalidate(ctx context.Context, prompt string) error
}

// InMemoryCache 内存缓存（基于向量相似度）
// perf-v6 Task 1：container/list LRU + 多表 LSH 索引加速慢路径
type InMemoryCache struct {
	// 优化（perf-v6）：O(1) LRU 实现
	// lruList 维护从最旧到最新的顺序；Front() = 最旧（淘汰候选），Back() = 最新
	// lruMap fingerprint → *list.Element（O(1) 查找 + MoveToBack）
	lruList *list.List
	lruMap  map[string]*list.Element
	// perf-v6 Task 1：多表 LSH 索引用于向量慢路径
	lsh *lshBucketCache
	// entries 保留 slice 用于慢路径（向量搜索）；通过 lruMap.Value 拿到 *CacheEntry
	embedder   EmbeddingFunc
	maxSize    int
	minScore   float32
	ttl        time.Duration
	mu         sync.RWMutex
	totalQuery int64
	hits       int64
	misses     int64
	tokensSave int64
	// v6.x 评估 §4.2 P1-6：TTL 清理 goroutine 的停止机制
	// stopCh 由 Close() 关闭；loopDone 由 goroutine 退出时关闭（测试/优雅关闭可等待）
	stopCh    chan struct{}
	loopDone  chan struct{}
	closeOnce sync.Once
}

// NewInMemoryCache 创建内存缓存
func NewInMemoryCache(embedder EmbeddingFunc, maxSize int, minScore float32) *InMemoryCache {
	if minScore < 0 {
		minScore = 0
	}
	if minScore > 1 {
		minScore = 1
	}
	return NewInMemoryCacheWithFullConfig(InMemoryCacheFullConfig{
		Embedder: embedder,
		MaxSize:  maxSize,
		MinScore: minScore,
	})
}

type InMemoryCacheFullConfig struct {
	Embedder EmbeddingFunc
	MaxSize  int
	MinScore float32
	TTL      time.Duration
}

func NewInMemoryCacheWithFullConfig(cfg InMemoryCacheFullConfig) *InMemoryCache {
	if cfg.MaxSize <= 0 {
		cfg.MaxSize = defaultCacheMaxSize
	}
	c := &InMemoryCache{
		lruList:  list.New(),
		lruMap:   make(map[string]*list.Element, cfg.MaxSize),
		lsh:      newLSHCache(), // perf-v6 Task 1：多表 LSH 索引
		embedder: cfg.Embedder,
		maxSize:  cfg.MaxSize,
		minScore: cfg.MinScore,
		ttl:      cfg.TTL,
		stopCh:   make(chan struct{}), // v6.x 评估 §4.2 P1-6
	}
	// perf-v6 Task 8：启动 TTL 后台清理 goroutine
	// v6.x 评估 §4.2 P1-6：goroutine 监听 stopCh，Close() 可将其停止，
	// 避免缓存淘汰后 goroutine 永久泄漏
	if cfg.TTL > 0 {
		c.loopDone = make(chan struct{})
		go c.ttlCleanupLoop(cfg.TTL)
	}
	return c
}

// Close 停止 TTL 后台清理 goroutine 并释放相关资源（v6.x 评估 §4.2 P1-6）。
// 幂等：重复调用安全。未配置 TTL 时也可安全调用。
func (c *InMemoryCache) Close() error {
	c.closeOnce.Do(func() {
		close(c.stopCh)
	})
	return nil
}

// ttlCleanupLoop 定期清理过期缓存（perf-v6 Task 8）
// 间隔为 TTL/2，保证过期 entry 不会长时间占用内存
// v6.x 评估 §4.2 P1-6：select 监听 stopCh，Close() 后退出
func (c *InMemoryCache) ttlCleanupLoop(ttl time.Duration) {
	defer close(c.loopDone)
	interval := ttl / 2
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.cleanupExpired(ttl)
		case <-c.stopCh:
			return
		}
	}
}

// cleanupExpired 扫描并删除过期 entry（perf-v6 Task 8）
// v6.x 评估 §4.2 P1-7：删除时必须同步清理 LSH 索引，
// 否则过期 entry 仍被 probeCandidates 返回（死对象无法 GC + 慢路径反复扫描）
func (c *InMemoryCache) cleanupExpired(ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	var toRemove []*list.Element
	for elem := c.lruList.Front(); elem != nil; elem = elem.Next() {
		entry := elem.Value.(*CacheEntry)
		if now.Sub(entry.CreatedAt) > ttl {
			toRemove = append(toRemove, elem)
		}
	}
	for _, elem := range toRemove {
		entry := elem.Value.(*CacheEntry)
		c.lruList.Remove(elem)
		delete(c.lruMap, entry.Key)
		if entry.vector != nil {
			c.lsh.remove(entry, entry.vector) // v6.x 评估 §4.2 P1-7
		}
	}
}

// Get 查找缓存，similarity 为最低相似度阈值（0-1）
// 优化（perf-v3）：先检查 fingerprint 精确匹配 O(1) fast-path，未命中再走向量相似度
// 优化（perf-v6 Task 1）：O(1) LRU 升级（MoveToBack）
func (c *InMemoryCache) Get(ctx context.Context, query string, similarity float32) (*CompletionResponse, bool) {
	// fast-path：精确匹配（O(1) hash map 查找）
	fp := PromptFingerprint(query)
	c.mu.RLock()
	atomicAdd(&c.totalQuery, 1)
	if elem, ok := c.lruMap[fp]; ok {
		entry := elem.Value.(*CacheEntry)
		if c.ttl <= 0 || time.Since(entry.CreatedAt) <= c.ttl {
			// perf-v6 Task 1：O(1) 升级到最新（MoveToBack）
			c.mu.RUnlock()
			c.mu.Lock()
			c.lruList.MoveToBack(elem)
			c.mu.Unlock()
			entry.AddHit()
			atomicAdd(&c.hits, 1)
			if entry.Response != nil {
				atomicAdd(&c.tokensSave, int64(entry.Response.Usage.TotalTokens))
			}
			return entry.Response, true
		}
	}
	c.mu.RUnlock()

	// slow-path：向量相似度搜索
	var queryVec []float32
	if c.embedder != nil {
		// perf-v6 Task 5：先查 embedding 缓存（避免重复远程调用）
		if cached, ok := getCachedEmbedding(query); ok {
			queryVec = cached
		} else {
			v, err := c.embedder(ctx, query)
			if err != nil {
				return nil, false
			}
			queryVec = v
			putCachedEmbedding(query, v)
		}
	} else {
		queryVec = fingerprintToVector(query)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// perf-v6 Task 1：多表 LSH 索引加速慢路径
	// 从 4 个 hash table 收集候选 entries（多探测 +/- 2 邻近 bucket）
	candidates := c.lsh.probeCandidates(queryVec)

	var bestEntry *CacheEntry
	var bestScore float32
	var bestElem *list.Element
	candidateSet := make(map[*CacheEntry]*list.Element, len(candidates))

	// 阶段 1：扫描 LSH 候选
	for _, entry := range candidates {
		if c.ttl > 0 && time.Since(entry.CreatedAt) > c.ttl {
			continue
		}
		// 在 lruList 中找到对应 elem（用于后续 MoveToBack）
		if elem, ok := c.lruMap[entry.Key]; ok {
			candidateSet[entry] = elem
		}
		score := cosineSimilarity(queryVec, entry.vector)
		if score > bestScore {
			bestScore = score
			bestEntry = entry
			bestElem = nil // 阶段 1 不更新 bestElem
			if elem, ok := c.lruMap[entry.Key]; ok {
				bestElem = elem
			}
		}
	}

	// 阶段 2：LSH 未找到足够相似度时，全量扫描 fallback
	for elem := c.lruList.Front(); elem != nil; elem = elem.Next() {
		entry := elem.Value.(*CacheEntry)
		if _, seen := candidateSet[entry]; seen {
			continue // 已扫描过
		}
		if c.ttl > 0 && time.Since(entry.CreatedAt) > c.ttl {
			continue
		}
		score := cosineSimilarity(queryVec, entry.vector)
		if score > bestScore {
			bestScore = score
			bestEntry = entry
			bestElem = elem
		}
	}

	if bestEntry != nil && bestScore >= similarity {
		if bestElem != nil {
			c.lruList.MoveToBack(bestElem) // 命中升级
		}
		bestEntry.AddHit()
		atomicAdd(&c.hits, 1)
		if bestEntry.Response != nil {
			atomicAdd(&c.tokensSave, int64(bestEntry.Response.Usage.TotalTokens))
		}
		return bestEntry.Response, true
	}

	atomicAdd(&c.misses, 1)
	return nil, false
}

// Set 写入缓存（perf-v6 Task 1：O(1) LRU 淘汰）
func (c *InMemoryCache) Set(ctx context.Context, query string, resp *CompletionResponse) error {
	var vec []float32
	if c.embedder != nil {
		// perf-v6 Task 5：先查 embedding 缓存（避免重复远程调用）
		if cached, ok := getCachedEmbedding(query); ok {
			vec = cached
		} else {
			v, err := c.embedder(ctx, query)
			if err != nil {
				return err
			}
			vec = v
			putCachedEmbedding(query, v)
		}
	} else {
		vec = fingerprintToVector(query)
	}

	fp := PromptFingerprint(query)

	return c.setByKeyAndVector(fp, query, vec, resp)
}

// setByKeyAndVector 按给定指纹键 + 向量写入缓存（Set / SetRequest 共用）。
// 保证两条写入路径的"更新语义 + LRU 淘汰 + LSH 索引维护"完全一致。
func (c *InMemoryCache) setByKeyAndVector(fp, query string, vec []float32, resp *CompletionResponse) error {
	if resp == nil {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// 如果 fp 已存在，先删除旧 entry（更新语义）
	if oldElem, ok := c.lruMap[fp]; ok {
		oldEntry := oldElem.Value.(*CacheEntry)
		c.lruList.Remove(oldElem)
		delete(c.lruMap, fp)
		if oldEntry.vector != nil {
			c.lsh.remove(oldEntry, oldEntry.vector) // perf-v6 Task 1
		}
	}

	// O(1) 淘汰最旧
	for c.lruList.Len() >= c.maxSize {
		front := c.lruList.Front()
		if front == nil {
			break
		}
		evicted := front.Value.(*CacheEntry)
		c.lruList.Remove(front)
		delete(c.lruMap, evicted.Key)
		if evicted.vector != nil {
			c.lsh.remove(evicted, evicted.vector) // perf-v6 Task 1
		}
	}

	entry := &CacheEntry{
		Key:       fp,
		Query:     query,
		Response:  resp,
		CreatedAt: time.Now(),
		Model:     resp.Model,
		vector:    vec,
	}
	// Push 到最新位置
	elem := c.lruList.PushBack(entry)
	c.lruMap[fp] = elem
	// perf-v6 Task 1：维护 LSH 索引
	if vec != nil {
		c.lsh.add(entry, vec)
	}

	return nil
}

// GetRequest 按完整 CompletionRequest 指纹精确查找（v6.x 评估 §4.2 P1-5）。
// 与 Get(query) 的区别：键覆盖完整输入空间（model + 全消息序列 +
// temperature/max_tokens/response_format，见 RequestFingerprint），
// 避免"相同最后一条 user 消息"在不同 system prompt / 参数下假命中。
func (c *InMemoryCache) GetRequest(ctx context.Context, req *CompletionRequest) (*CompletionResponse, bool) {
	fp := RequestFingerprint(req)

	c.mu.RLock()
	elem, ok := c.lruMap[fp]
	var expired bool
	if ok {
		entry := elem.Value.(*CacheEntry)
		expired = c.ttl > 0 && time.Since(entry.CreatedAt) > c.ttl
	}
	c.mu.RUnlock()

	atomicAdd(&c.totalQuery, 1)

	if !ok {
		atomicAdd(&c.misses, 1)
		return nil, false
	}

	if expired {
		// 过期删除：与 cleanupExpired 一致，同步清理 LSH 索引（P1-7）
		c.mu.Lock()
		if cur, still := c.lruMap[fp]; still && cur == elem {
			entry := cur.Value.(*CacheEntry)
			c.lruList.Remove(cur)
			delete(c.lruMap, fp)
			if entry.vector != nil {
				c.lsh.remove(entry, entry.vector)
			}
		}
		c.mu.Unlock()
		atomicAdd(&c.misses, 1)
		return nil, false
	}

	// 命中：O(1) 升级到最新（已淘汰 elem 上 MoveToBack 为 no-op）
	c.mu.Lock()
	c.lruList.MoveToBack(elem)
	c.mu.Unlock()

	entry := elem.Value.(*CacheEntry)
	entry.AddHit()
	atomicAdd(&c.hits, 1)
	if entry.Response != nil {
		atomicAdd(&c.tokensSave, int64(entry.Response.Usage.TotalTokens))
	}
	return entry.Response, true
}

// SetRequest 按完整 CompletionRequest 指纹写入缓存（v6.x 评估 §4.2 P1-5）。
// 向量由请求指纹确定性派生（不走 embedder，避免额外远端调用）：
// 请求键条目以精确匹配为主，慢路径相似度为辅。
func (c *InMemoryCache) SetRequest(ctx context.Context, req *CompletionRequest, resp *CompletionResponse) error {
	fp := RequestFingerprint(req)
	vec := fingerprintToVector(fp)
	return c.setByKeyAndVector(fp, extractLastUserQuery(req.Messages), vec, resp)
}

// Stats 返回缓存统计
// v6.x 评估 §4.2 P1-1：计数器由 atomic.AddInt64 写入，Stats 必须用
// atomic.LoadInt64 读取，否则并发 Get/Set + Stats 构成数据竞争（-race 可检出）。
func (c *InMemoryCache) Stats(_ context.Context) CacheStats {
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

	return CacheStats{
		TotalQueries: total,
		CacheHits:    hits,
		CacheMisses:  misses,
		HitRate:      hitRate,
		EntryCount:   c.lruList.Len(),
		TokensSaved:  tokensSave,
	}
}

// Clear 清空缓存
// v6.x 评估 §4.2 P1-1：计数器重置同样走 atomic.StoreInt64，
// 避免与并发 atomic.AddInt64 构成竞争。
func (c *InMemoryCache) Clear(_ context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lruList = list.New()
	c.lruMap = make(map[string]*list.Element, c.maxSize)
	c.lsh = newLSHCache() // perf-v6 Task 1：重建 LSH 索引
	atomic.StoreInt64(&c.totalQuery, 0)
	atomic.StoreInt64(&c.hits, 0)
	atomic.StoreInt64(&c.misses, 0)
	atomic.StoreInt64(&c.tokensSave, 0)
	return nil
}

// InvalidateAll 清空缓存并重置统计（同Clear，语义更明确）
func (c *InMemoryCache) InvalidateAll(ctx context.Context) error { return c.Clear(ctx) }

func (c *InMemoryCache) Invalidate(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	// 遍历删除匹配的 entries
	var toRemove []*list.Element
	for elem := c.lruList.Front(); elem != nil; elem = elem.Next() {
		entry := elem.Value.(*CacheEntry)
		if strings.Contains(entry.Query, key) || entry.Key == key {
			toRemove = append(toRemove, elem)
		}
	}
	for _, elem := range toRemove {
		entry := elem.Value.(*CacheEntry)
		c.lruList.Remove(elem)
		delete(c.lruMap, entry.Key)
		if entry.vector != nil {
			c.lsh.remove(entry, entry.vector) // perf-v6 Task 1
		}
	}
	return nil
}

func (c *InMemoryCache) SetWithVector(query string, resp *CompletionResponse, vector []float32) error {
	fp := PromptFingerprint(query)
	return c.setByKeyAndVector(fp, query, vector, resp)
}

// RequestLLMCache 完整请求键缓存扩展接口（v6.x 评估 §4.2 P1-5）。
//
// LLMCache 的可选扩展：实现者按 RequestFingerprint（完整输入空间：
// model + 全消息序列 + temperature/max_tokens/response_format）做键，
// 避免仅基于"最后一条 user 消息"的假命中。未实现该接口的缓存
// （如第三方 LLMCache 实现）自动回退到 query 级 Get/Set，语义保持兼容。
//
// 包内实现：InMemoryCache / FingerprintCache / SQLiteCache / HybridCache / NoopCache。
type RequestLLMCache interface {
	GetRequest(ctx context.Context, req *CompletionRequest) (*CompletionResponse, bool)
	SetRequest(ctx context.Context, req *CompletionRequest, resp *CompletionResponse) error
}

// CachedProvider 带 LLM 缓存的 Provider 装饰器
type CachedProvider struct {
	inner    Provider
	cache    LLMCache
	manager  *CacheManager
	minScore float32
}

// NewCachedProvider 创建带缓存的 Provider
func NewCachedProvider(inner Provider, cache LLMCache, minScore float32) (*CachedProvider, error) {
	if inner == nil {
		return nil, fmt.Errorf("inner provider must not be nil")
	}
	if cache == nil {
		return nil, fmt.Errorf("cache must not be nil")
	}
	return &CachedProvider{
		inner:    inner,
		cache:    cache,
		minScore: minScore,
	}, nil
}

// NewCachedProviderWithManager 创建带 CacheManager 的缓存 Provider
func NewCachedProviderWithManager(inner Provider, mgr *CacheManager, minScore float32) (*CachedProvider, error) {
	if inner == nil {
		return nil, fmt.Errorf("inner provider must not be nil")
	}
	if mgr == nil {
		return nil, fmt.Errorf("cache manager must not be nil")
	}
	return &CachedProvider{
		inner:    inner,
		cache:    mgr.cache,
		manager:  mgr,
		minScore: minScore,
	}, nil
}

// requestCache 返回底层缓存的请求键扩展（若支持）。
// manager 与裸 cache 两条装配路径都覆盖。
func (p *CachedProvider) requestCache() RequestLLMCache {
	if p.manager != nil {
		if rc, ok := p.manager.cache.(RequestLLMCache); ok {
			return rc
		}
		return nil
	}
	if rc, ok := p.cache.(RequestLLMCache); ok {
		return rc
	}
	return nil
}

// Complete 实现 Provider 接口 — 先查缓存，未命中再调用内部 Provider
//
// v6.x 评估 §4.2 P1-5：缓存键从 extractLastUserQuery（仅最后一条 user 消息）
// 切换到 RequestFingerprint（完整输入空间）。不同 system prompt / 模型 /
// temperature / 历史消息的相同 query 不再互相假命中。
// 底层缓存不支持请求键时，回退到原有 query 级语义（兼容第三方 LLMCache）。
func (p *CachedProvider) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	// 优先：完整请求指纹键
	if rc := p.requestCache(); rc != nil {
		if cached, ok := rc.GetRequest(ctx, req); ok {
			return cached, nil
		}
		resp, err := p.inner.Complete(ctx, req)
		if err != nil {
			return nil, err
		}
		_ = rc.SetRequest(ctx, req, resp)
		return resp, nil
	}

	// 回退：query 级键（仅最后一条 user 消息）
	query := extractLastUserQuery(req.Messages)

	if query != "" {
		var cached *CompletionResponse
		var ok bool
		if p.manager != nil {
			cached, ok = p.manager.Get(ctx, query, p.minScore)
		} else {
			cached, ok = p.cache.Get(ctx, query, p.minScore)
		}
		if ok {
			return cached, nil
		}
	}

	resp, err := p.inner.Complete(ctx, req)
	if err != nil {
		return nil, err
	}

	if query != "" {
		if p.manager != nil {
			_ = p.manager.Set(ctx, query, resp)
		} else {
			_ = p.cache.Set(ctx, query, resp)
		}
	}

	return resp, nil
}

// Stream 实现 Provider 接口 — 不缓存流式请求
func (p *CachedProvider) Stream(ctx context.Context, req *CompletionRequest) (<-chan Chunk, error) {
	return p.inner.Stream(ctx, req)
}

// CallTools 实现 Provider 接口 — 不缓存tool调用
func (p *CachedProvider) CallTools(ctx context.Context, req *ToolCallRequest) (*ToolCallResponse, error) {
	return p.inner.CallTools(ctx, req)
}

// Embeddings 实现 Embedder 接口，通过类型断言委托给底层 Provider
func (p *CachedProvider) Embeddings(ctx context.Context, texts []string) ([][]float32, error) {
	if embedder, ok := p.inner.(Embedder); ok {
		return embedder.Embeddings(ctx, texts)
	}
	return nil, ErrNotSupported
}

// Info 实现 Provider 接口
func (p *CachedProvider) Info() ModelInfo {
	return p.inner.Info()
}

// CacheStats 返回缓存统计信息
func (p *CachedProvider) CacheStats() CacheStats {
	if p.manager != nil {
		return p.manager.Stats(context.Background())
	}
	return p.cache.Stats(context.Background())
}

// EnableCache 启用/禁用缓存
func (p *CachedProvider) EnableCache(enabled bool) {
	if p.manager != nil {
		p.manager.Enable(enabled)
	}
}

// InvalidateCache 使指定 key 的缓存失效
func (p *CachedProvider) InvalidateCache(key string) {
	ctx := context.Background()
	if p.manager != nil {
		_ = p.manager.Invalidate(ctx, key)
	} else {
		_ = p.cache.Invalidate(ctx, key)
	}
}

// ClearCache 清空所有缓存
func (p *CachedProvider) ClearCache() {
	ctx := context.Background()
	if p.manager != nil {
		_ = p.manager.Clear(ctx)
	} else {
		_ = p.cache.Clear(ctx)
	}
}

// cosineSimilarity 计算两个向量的余弦相似度
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float32
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
}

// extractLastUserQuery 提取最后一条用户消息
func extractLastUserQuery(messages []ChatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content
		}
	}
	return ""
}

// atomicAdd 原子加法辅助
func atomicAdd(val *int64, delta int64) {
	atomic.AddInt64(val, delta)
}
