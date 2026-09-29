package governance

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/resilience"
)

// TokenBucket 令牌桶限流器（v6.x 评估报告 §五.1 "重复实现大赏" 修复）。
//
// v6.x：canonical 实现迁移到 internal/resilience/rate_limiter.go，
// governance 通过类型别名复用，消除与 llm/rate_limiter.go 的重复实现。
// 本处保留 TokenBucket 名称与 NewTokenBucket 构造器，调用方无需改动。
type TokenBucket = resilience.TokenBucket

// NewTokenBucket 创建一个令牌桶。
// rate 是每秒允许的请求数（QPS 上限），burst 是最大突发量。
// 如果 rate <= 0 则不限流。
func NewTokenBucket(rate, burst int) *TokenBucket {
	return resilience.NewTokenBucket(rate, burst)
}

// --- QuotaManager ---

// QuotaManager 跟踪单个租户的配额使用情况。
//
// 线程安全：所有字段通过内部锁或原子操作保护。
type QuotaManager struct {
	tenantID string
	quotas   TenantQuota

	// QPS 令牌桶
	bucket *TokenBucket

	// 每日 Token 用量
	dayTokens atomic.Int64
	// 当前计数器的日期键
	dayKey atomic.Value // string

	// 当前已用资源计数
	agentCount   atomic.Int64
	sessionCount atomic.Int64

	mu sync.RWMutex
}

// dailyKey 返回 UTC 当天的日期字符串。
func dailyKey() string {
	return time.Now().UTC().Format("2006-01-02")
}

// NewQuotaManager 为指定租户创建 QuotaManager。
func NewQuotaManager(tenantID string, quotas TenantQuota) *QuotaManager {
	qm := &QuotaManager{
		tenantID: tenantID,
		quotas:   quotas,
	}
	qm.bucket = NewTokenBucket(quotas.MaxQPS, quotas.MaxQPS)
	qm.dayKey.Store(dailyKey())
	return qm
}

// CheckQPS 检查是否允许一次调用（QPS 限流）。
func (q *QuotaManager) CheckQPS() error {
	if q.quotas.MaxQPS <= 0 {
		return nil // 无限制
	}
	if !q.bucket.Take(1) {
		return fmt.Errorf("%w: QPS limit %d exceeded", ErrQuotaExceeded, q.quotas.MaxQPS)
	}
	return nil
}

// RecordTokens 记录 Token 消耗。如果超出日配额返回 ErrQuotaExceeded。
func (q *QuotaManager) RecordTokens(n int64) error {
	if n <= 0 {
		return nil
	}
	if q.quotas.MaxTokensPerDay <= 0 {
		return nil // 无限制
	}

	// 检查是否需要重置日计数器（跨天）
	key := dailyKey()
	storedKey, _ := q.dayKey.Load().(string)
	if storedKey != key {
		q.mu.Lock()
		// 双重检查
		storedKey2, _ := q.dayKey.Load().(string)
		if storedKey2 != key {
			q.dayTokens.Store(0)
			q.dayKey.Store(key)
		}
		q.mu.Unlock()
	}

	// 先检查再增加（避免超额）
	current := q.dayTokens.Load()
	if current+n > q.quotas.MaxTokensPerDay {
		return fmt.Errorf("%w: daily token quota %d exceeded (used=%d, add=%d)",
			ErrQuotaExceeded, q.quotas.MaxTokensPerDay, current, n)
	}

	newVal := q.dayTokens.Add(n)
	if newVal > q.quotas.MaxTokensPerDay {
		// 超额了，回滚并返回错误
		q.dayTokens.Add(-n)
		return fmt.Errorf("%w: daily token quota %d exceeded", ErrQuotaExceeded, q.quotas.MaxTokensPerDay)
	}

	return nil
}

// DailyTokensUsed 返回当日已用 Token 数。
func (q *QuotaManager) DailyTokensUsed() int64 {
	// 检查是否跨天
	key := dailyKey()
	storedKey, _ := q.dayKey.Load().(string)
	if storedKey != key {
		return 0
	}
	return q.dayTokens.Load()
}

// CheckAgentCount 检查是否还能创建新 Agent。
func (q *QuotaManager) CheckAgentCount() error {
	if q.quotas.MaxAgents <= 0 {
		return nil
	}
	if int(q.agentCount.Load()) >= q.quotas.MaxAgents {
		return fmt.Errorf("%w: agent limit %d exceeded", ErrQuotaExceeded, q.quotas.MaxAgents)
	}
	return nil
}

// IncrementAgentCount 增加 Agent 计数。
func (q *QuotaManager) IncrementAgentCount() {
	q.agentCount.Add(1)
}

// DecrementAgentCount 减少 Agent 计数。
func (q *QuotaManager) DecrementAgentCount() {
	q.agentCount.Add(-1)
}

// CheckSessionCount 检查是否还能创建新 Session。
func (q *QuotaManager) CheckSessionCount() error {
	if q.quotas.MaxSessions <= 0 {
		return nil
	}
	if int(q.sessionCount.Load()) >= q.quotas.MaxSessions {
		return fmt.Errorf("%w: session limit %d exceeded", ErrQuotaExceeded, q.quotas.MaxSessions)
	}
	return nil
}

// IncrementSessionCount 增加 Session 计数。
func (q *QuotaManager) IncrementSessionCount() {
	q.sessionCount.Add(1)
}

// DecrementSessionCount 减少 Session 计数。
func (q *QuotaManager) DecrementSessionCount() {
	q.sessionCount.Add(-1)
}

// QuotaStatus 返回当前配额使用快照。
type QuotaStatus struct {
	TenantID        string
	Plan            TenantPlan
	DailyTokensUsed int64
	MaxTokensPerDay int64
	AgentCount      int
	MaxAgents       int
	SessionCount    int
	MaxSessions     int
	MaxQPS          int
}

// Status 返回配额状态快照。
func (q *QuotaManager) Status() QuotaStatus {
	return QuotaStatus{
		TenantID:        q.tenantID,
		Plan:            PlanFree,
		DailyTokensUsed: q.DailyTokensUsed(),
		MaxTokensPerDay: q.quotas.MaxTokensPerDay,
		AgentCount:      int(q.agentCount.Load()),
		MaxAgents:       q.quotas.MaxAgents,
		SessionCount:    int(q.sessionCount.Load()),
		MaxSessions:     q.quotas.MaxSessions,
		MaxQPS:          q.quotas.MaxQPS,
	}
}
