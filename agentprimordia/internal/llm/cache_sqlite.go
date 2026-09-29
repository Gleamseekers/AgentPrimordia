//go:build sqlite
// +build sqlite

package llm

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"

	"github.com/Gleamseekers/AgentPrimordia/internal/jsonutil" // perf-v6 round 8 Task 1：统一 JSON 序列化
)

const (
	defaultSQLiteCacheMaxSize = 1000
	sqliteMaxOpenConns        = 5
	sqliteMaxIdleConns        = 2
)

type SQLiteCacheConfig struct {
	DSN       string
	MaxSize   int
	TTL       time.Duration
	MinScore  float32
	EnableSem bool
}

type SQLiteCache struct {
	db         *sql.DB
	path       string
	maxSize    int
	ttl        time.Duration
	minScore   float32
	enableSem  bool
	mu         sync.RWMutex
	totalQuery int64
	hits       int64
	misses     int64
	tokensSave int64
}

func NewSQLiteCache(dsn string) (*SQLiteCache, error) {
	return NewSQLiteCacheWithConfig(SQLiteCacheConfig{DSN: dsn})
}

func NewSQLiteCacheWithConfig(cfg SQLiteCacheConfig) (*SQLiteCache, error) {
	if cfg.DSN == "" {
		cfg.DSN = "file::memory:?cache=shared"
	}
	if cfg.MaxSize <= 0 {
		cfg.MaxSize = defaultSQLiteCacheMaxSize
	}

	db, err := sql.Open("sqlite", cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("open cache db: %w", err)
	}

	isInMemory := strings.Contains(cfg.DSN, ":memory:")
	if isInMemory {
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
	} else {
		db.SetMaxOpenConns(sqliteMaxOpenConns)
		db.SetMaxIdleConns(sqliteMaxIdleConns)
	}

	c := &SQLiteCache{
		db:        db,
		path:      cfg.DSN,
		maxSize:   cfg.MaxSize,
		ttl:       cfg.TTL,
		minScore:  cfg.MinScore,
		enableSem: cfg.EnableSem,
	}

	if err := c.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}

	return c, nil
}

func (c *SQLiteCache) initSchema() error {
	isInMemory := strings.Contains(c.path, ":memory:") || strings.Contains(c.path, "mode=memory")

	if !isInMemory {
		pragmas := []string{
			"PRAGMA journal_mode=WAL;",
			"PRAGMA busy_timeout=5000;",
			"PRAGMA synchronous=NORMAL;",
		}
		for _, p := range pragmas {
			if _, err := c.db.Exec(p); err != nil {
				return fmt.Errorf("pragma %s: %w", p, err)
			}
		}
	}

	schema := `
	CREATE TABLE IF NOT EXISTS cache_entries (
		fingerprint TEXT PRIMARY KEY,
		query TEXT NOT NULL,
		response_id TEXT NOT NULL,
		content TEXT NOT NULL,
		usage TEXT,
		vector TEXT,
		created_at TEXT NOT NULL,
		hit_count INTEGER DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_cache_created ON cache_entries(created_at);
	`
	_, err := c.db.Exec(schema)
	return err
}

func (c *SQLiteCache) Get(ctx context.Context, query string, similarity float32) (*CompletionResponse, bool) {
	fp := PromptFingerprint(query)

	c.mu.RLock()
	ttl := c.ttl
	enableSem := c.enableSem
	c.mu.RUnlock()

	atomic.AddInt64(&c.totalQuery, 1)

	var respID, content, usageJSON string
	var createdAt string
	err := c.db.QueryRowContext(ctx,
		"SELECT response_id, content, usage, created_at FROM cache_entries WHERE fingerprint = ?",
		fp,
	).Scan(&respID, &content, &usageJSON, &createdAt)
	if err == nil {
		if ttl > 0 {
			ct, err := time.Parse(time.RFC3339, createdAt)
			if err != nil {
				slog.Warn("缓存条目时间解析失败", "created_at", createdAt, "error", err)
			} else if !ct.IsZero() && time.Since(ct) > ttl {
				if _, err := c.db.ExecContext(ctx, "DELETE FROM cache_entries WHERE fingerprint = ?", fp); err != nil {
					slog.Warn("缓存条目删除失败", "fingerprint", fp, "error", err)
				}
				atomic.AddInt64(&c.misses, 1)
				return nil, false
			}
		}
		if _, err := c.db.ExecContext(ctx, "UPDATE cache_entries SET hit_count = hit_count + 1 WHERE fingerprint = ?", fp); err != nil {
			slog.Warn("缓存命中计数更新失败", "fingerprint", fp, "error", err)
		}
		resp := &CompletionResponse{ID: respID, Content: content}
		if usageJSON != "" {
			var u Usage
			// perf-v6 round 8 Task 1：使用 pooled reader
			if err := jsonutil.Unmarshal([]byte(usageJSON), &u); err != nil {
				slog.Warn("缓存 usage 反序列化失败", "error", err)
			}
			resp.Usage = u
		}
		atomic.AddInt64(&c.hits, 1)
		if resp.Usage.TotalTokens > 0 {
			atomic.AddInt64(&c.tokensSave, int64(resp.Usage.TotalTokens))
		}
		return resp, true
	}

	if enableSem && similarity > 0 {
		return c.semanticSearch(ctx, query, similarity)
	}

	atomic.AddInt64(&c.misses, 1)
	return nil, false
}

func (c *SQLiteCache) semanticSearch(ctx context.Context, query string, similarity float32) (*CompletionResponse, bool) {
	// 限制语义搜索的条目数量，避免大量数据时的性能问题
	var entryCount int
	row := c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM cache_entries")
	if err := row.Scan(&entryCount); err == nil && entryCount > defaultSQLiteCacheMaxSize {
		// 条目过多时跳过语义搜索，退化为精确匹配
		return nil, false
	}

	queryVec := fingerprintToVector(query)

	rows, err := c.db.QueryContext(ctx,
		"SELECT fingerprint, response_id, content, usage, vector, created_at FROM cache_entries",
	)
	if err != nil {
		atomic.AddInt64(&c.misses, 1)
		return nil, false
	}
	defer rows.Close()

	var bestResp *CompletionResponse
	var bestScore float32

	for rows.Next() {
		var fp, respID, content, usageJSON, vecJSON, createdAt string
		if err := rows.Scan(&fp, &respID, &content, &usageJSON, &vecJSON, &createdAt); err != nil {
			continue
		}

		if c.ttl > 0 {
			ct, err := time.Parse(time.RFC3339, createdAt)
			if err != nil {
				slog.Warn("语义搜索条目时间解析失败", "error", err)
			} else if !ct.IsZero() && time.Since(ct) > c.ttl {
				continue
			}
		}

		var entryVec []float32
		if vecJSON != "" {
			// perf-v6 round 8 Task 1：使用 pooled reader
			if err := jsonutil.Unmarshal([]byte(vecJSON), &entryVec); err != nil {
				slog.Warn("语义搜索向量反序列化失败", "error", err)
			}
		}
		if len(entryVec) == 0 {
			continue
		}

		score := cosineSimilarity(queryVec, entryVec)
		if score > bestScore {
			bestScore = score
			resp := &CompletionResponse{ID: respID, Content: content}
			if usageJSON != "" {
				var u Usage
				// perf-v6 round 8 Task 1：使用 pooled reader
				if err := jsonutil.Unmarshal([]byte(usageJSON), &u); err != nil {
					slog.Warn("语义搜索 usage 反序列化失败", "error", err)
				}
				resp.Usage = u
			}
			bestResp = resp
		}
	}

	if bestResp != nil && bestScore >= similarity {
		c.db.ExecContext(ctx, "UPDATE cache_entries SET hit_count = hit_count + 1 WHERE fingerprint = ?",
			PromptFingerprint(query))
		atomic.AddInt64(&c.hits, 1)
		if bestResp.Usage.TotalTokens > 0 {
			atomic.AddInt64(&c.tokensSave, int64(bestResp.Usage.TotalTokens))
		}
		return bestResp, true
	}

	atomic.AddInt64(&c.misses, 1)
	return nil, false
}

func (c *SQLiteCache) Set(ctx context.Context, query string, resp *CompletionResponse) error {
	fp := PromptFingerprint(query)
	return c.setByFingerprint(ctx, fp, query, query, resp)
}

// GetRequest 按完整 CompletionRequest 指纹精确查找（v6.x 评估 §4.2 P1-5）。
// 与 Get(query) 的区别：键覆盖完整输入空间（model + 全消息序列 +
// temperature/max_tokens/response_format，见 RequestFingerprint），
// 避免"相同最后一条 user 消息"在不同 system prompt / 参数下假命中。
func (c *SQLiteCache) GetRequest(ctx context.Context, req *CompletionRequest) (*CompletionResponse, bool) {
	fp := RequestFingerprint(req)

	atomic.AddInt64(&c.totalQuery, 1)

	var respID, content, usageJSON string
	var createdAt string
	err := c.db.QueryRowContext(ctx,
		"SELECT response_id, content, usage, created_at FROM cache_entries WHERE fingerprint = ?",
		fp,
	).Scan(&respID, &content, &usageJSON, &createdAt)
	if err != nil {
		atomic.AddInt64(&c.misses, 1)
		return nil, false
	}

	if c.ttl > 0 {
		ct, perr := time.Parse(time.RFC3339, createdAt)
		if perr != nil {
			slog.Warn("缓存条目时间解析失败", "created_at", createdAt, "error", perr)
		} else if !ct.IsZero() && time.Since(ct) > c.ttl {
			if _, derr := c.db.ExecContext(ctx, "DELETE FROM cache_entries WHERE fingerprint = ?", fp); derr != nil {
				slog.Warn("缓存条目删除失败", "fingerprint", fp, "error", derr)
			}
			atomic.AddInt64(&c.misses, 1)
			return nil, false
		}
	}

	if _, uerr := c.db.ExecContext(ctx, "UPDATE cache_entries SET hit_count = hit_count + 1 WHERE fingerprint = ?", fp); uerr != nil {
		slog.Warn("缓存命中计数更新失败", "fingerprint", fp, "error", uerr)
	}
	resp := &CompletionResponse{ID: respID, Content: content}
	if usageJSON != "" {
		var u Usage
		// perf-v6 round 8 Task 1：使用 pooled reader
		if err := jsonutil.Unmarshal([]byte(usageJSON), &u); err != nil {
			slog.Warn("缓存 usage 反序列化失败", "error", err)
		}
		resp.Usage = u
	}
	atomic.AddInt64(&c.hits, 1)
	if resp.Usage.TotalTokens > 0 {
		atomic.AddInt64(&c.tokensSave, int64(resp.Usage.TotalTokens))
	}
	return resp, true
}

// SetRequest 按完整 CompletionRequest 指纹写入缓存（v6.x 评估 §4.2 P1-5）。
// 语义向量由请求指纹确定性派生（query 列仍存最后一条 user 消息便于排查）。
func (c *SQLiteCache) SetRequest(ctx context.Context, req *CompletionRequest, resp *CompletionResponse) error {
	fp := RequestFingerprint(req)
	return c.setByFingerprint(ctx, fp, extractLastUserQuery(req.Messages), fp, resp)
}

// setByFingerprint 按给定指纹写入缓存（Set / SetRequest 共用）。
// fp 为存储主键；query 为可读性列；vecText 为语义向量的派生源文本。
func (c *SQLiteCache) setByFingerprint(ctx context.Context, fp, query, vecText string, resp *CompletionResponse) error {
	// perf-v6 round 8 Task 1：使用 pooled buffer 序列化
	usageJSON, err := jsonutil.Marshal(resp.Usage)
	if err != nil {
		slog.Warn("缓存 usage 序列化失败", "error", err)
	}

	var vecJSON string
	if c.enableSem {
		vec := fingerprintToVector(vecText)
		// perf-v6 round 8 Task 1：使用 pooled buffer 序列化
		vj, err := jsonutil.Marshal(vec)
		if err != nil {
			slog.Warn("缓存向量序列化失败", "error", err)
		}
		vecJSON = string(vj)
	}

	c.mu.RLock()
	maxSize := c.maxSize
	c.mu.RUnlock()

	if maxSize > 0 {
		var count int
		c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM cache_entries").Scan(&count)
		if count >= maxSize {
			evictCount := count / 10
			if evictCount < 1 {
				evictCount = 1
			}
			c.db.ExecContext(ctx,
				"DELETE FROM cache_entries WHERE fingerprint IN (SELECT fingerprint FROM cache_entries ORDER BY created_at ASC LIMIT ?)",
				evictCount,
			)
		}
	}

	_, execErr := c.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO cache_entries (fingerprint, query, response_id, content, usage, vector, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		fp, query, resp.ID, resp.Content, string(usageJSON), vecJSON, time.Now().Format(time.RFC3339),
	)
	return execErr
}

// Stats 返回缓存统计
// v6.x 评估 §4.2 P1-1：计数器由 atomic.AddInt64 写入，Stats 必须用
// atomic.LoadInt64 读取，否则并发下构成数据竞争（-race 可检出）。
func (c *SQLiteCache) Stats(ctx context.Context) CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var count int
	c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM cache_entries").Scan(&count)

	total := atomic.LoadInt64(&c.totalQuery)
	hits := atomic.LoadInt64(&c.hits)
	misses := atomic.LoadInt64(&c.misses)
	tokensSave := atomic.LoadInt64(&c.tokensSave)
	var hitRate float64
	if total > 0 {
		hitRate = float64(hits) / float64(total)
	}
	return CacheStats{
		TotalQueries: total, CacheHits: hits, CacheMisses: misses,
		HitRate: hitRate, EntryCount: count, TokensSaved: tokensSave,
	}
}

func (c *SQLiteCache) Clear(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.db.ExecContext(ctx, "DELETE FROM cache_entries")
	if err != nil {
		return err
	}
	atomic.StoreInt64(&c.totalQuery, 0)
	atomic.StoreInt64(&c.hits, 0)
	atomic.StoreInt64(&c.misses, 0)
	atomic.StoreInt64(&c.tokensSave, 0)
	return nil
}

func (c *SQLiteCache) Invalidate(ctx context.Context, key string) error {
	fp := PromptFingerprint(key)
	_, err := c.db.ExecContext(ctx, "DELETE FROM cache_entries WHERE fingerprint = ?", fp)
	return err
}

func (c *SQLiteCache) Close() error {
	return c.db.Close()
}
