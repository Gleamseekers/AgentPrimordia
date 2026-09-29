// Package cost 提供成本追踪与预算管理
package cost

import (
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// CostRecord 单次成本记录
type CostRecord struct {
	Model            string    `json:"model"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	TotalTokens      int       `json:"total_tokens"`
	CostUSD          float64   `json:"cost_usd"`
	Timestamp        time.Time `json:"timestamp"`
	SessionID        string    `json:"session_id"`
	AgentName        string    `json:"agent_name"`
}

// BudgetConfig 预算配置
type BudgetConfig struct {
	MaxTotalCostUSD     float64
	MaxTokensPerCall    int
	MaxTokensPerSession int
	OnBudgetExceed      func(summary *CostSummary)
}

// CostSummary 成本汇总
type CostSummary struct {
	TotalCostUSD      float64               `json:"total_cost_usd"`
	TotalPromptTokens int64                 `json:"total_prompt_tokens"`
	TotalCompTokens   int64                 `json:"total_completion_tokens"`
	TotalTokens       int64                 `json:"total_tokens"`
	CallCount         int                   `json:"call_count"`
	ByModel           map[string]*ModelCost `json:"by_model"`
}

// ModelCost 单模型成本
type ModelCost struct {
	CostUSD float64 `json:"cost_usd"`
	Calls   int     `json:"calls"`
	Tokens  int64   `json:"tokens"`
}

// recordBudgetCheckHook 仅用于测试，在 records 追加后、预算检查前调用。
// 生产代码中保持 nil。
var recordBudgetCheckHook func()

// CostTracker 成本追踪器
// perf-v5 Task 13：累计字段改 atomic，CheckBudget 改 O(1)
type CostTracker struct {
	pricing map[string]llm.ModelPricing
	budget  *BudgetConfig
	records []CostRecord
	mu      sync.RWMutex

	// 原子累加字段（O(1) 预算检查）
	totalCostBits     atomic.Uint64 // math.Float64bits 后的位模式
	totalPromptTokens atomic.Int64
	totalCompTokens   atomic.Int64
	totalTokens       atomic.Int64
	callCount         atomic.Int64
	lastTokens        atomic.Int64 // 最近一次调用的 token 数（用于 MaxTokensPerCall）
}

// NewCostTracker 创建成本追踪器
func NewCostTracker(pricing map[string]llm.ModelPricing, budget *BudgetConfig) *CostTracker {
	return &CostTracker{
		pricing: pricing,
		budget:  budget,
		records: make([]CostRecord, 0),
	}
}

// Record 记录一次 LLM 调用的 Usage
func (t *CostTracker) Record(model, sessionID, agentName string, usage llm.Usage) error {
	cost := llm.EstimateCost(model, usage, t.pricing)

	record := CostRecord{
		Model:            model,
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		TotalTokens:      usage.TotalTokens,
		CostUSD:          cost,
		Timestamp:        time.Now(),
		SessionID:        sessionID,
		AgentName:        agentName,
	}

	// 在锁内先追加记录并更新原子累加字段，再进行预算检查，保证 CheckBudget() 并发读取时
	// records 与原子累加值之间不存在不一致窗口。
	t.mu.Lock()
	t.records = append(t.records, record)

	// 原子累加必须在预算检查前完成
	for {
		oldBits := t.totalCostBits.Load()
		oldCost := math.Float64frombits(oldBits)
		newBits := math.Float64bits(oldCost + cost)
		if t.totalCostBits.CompareAndSwap(oldBits, newBits) {
			break
		}
	}
	t.totalPromptTokens.Add(int64(usage.PromptTokens))
	t.totalCompTokens.Add(int64(usage.CompletionTokens))
	t.totalTokens.Add(int64(usage.TotalTokens))
	t.callCount.Add(1)
	t.lastTokens.Store(int64(usage.TotalTokens))

	if recordBudgetCheckHook != nil {
		recordBudgetCheckHook()
	}
	t.mu.Unlock()

	// 预算检查使用原子累加字段，O(1) 且无锁；Record 中已保证 atomic 字段与 records 同步更新。
	exceeded := t.CheckBudget()
	if exceeded && t.budget != nil && t.budget.OnBudgetExceed != nil {
		t.budget.OnBudgetExceed(t.Summary())
	}

	return nil
}

// Records 返回所有成本记录的快照（用于导出器等只读消费场景）
//
// 返回的切片是 records 字段的浅拷贝，调用方不应修改其中元素；
// 该方法在锁内完成复制，保证并发安全。
func (t *CostTracker) Records() []CostRecord {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]CostRecord, len(t.records))
	copy(out, t.records)
	return out
}

// LastRecord 返回最近一次调用记录；若不存在则返回 nil
func (t *CostTracker) LastRecord() *CostRecord {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if len(t.records) == 0 {
		return nil
	}
	last := t.records[len(t.records)-1]
	return &last
}

// Summary 返回成本汇总
func (t *CostTracker) Summary() *CostSummary {
	t.mu.RLock()
	defer t.mu.RUnlock()

	summary := &CostSummary{
		ByModel: make(map[string]*ModelCost),
	}

	for _, r := range t.records {
		summary.CallCount++
		summary.TotalCostUSD += r.CostUSD
		summary.TotalPromptTokens += int64(r.PromptTokens)
		summary.TotalCompTokens += int64(r.CompletionTokens)
		summary.TotalTokens += int64(r.TotalTokens)

		mc, ok := summary.ByModel[r.Model]
		if !ok {
			mc = &ModelCost{}
			summary.ByModel[r.Model] = mc
		}
		mc.CostUSD += r.CostUSD
		mc.Calls++
		mc.Tokens += int64(r.TotalTokens)
	}

	return summary
}

// CheckBudget 检查是否超出预算（perf-v5 Task 13：O(1) 读取原子累加字段）
func (t *CostTracker) CheckBudget() bool {
	if t.budget == nil {
		return false
	}

	if t.budget.MaxTotalCostUSD > 0 {
		totalCost := math.Float64frombits(t.totalCostBits.Load())
		if totalCost > t.budget.MaxTotalCostUSD {
			return true
		}
	}

	if t.budget.MaxTokensPerSession > 0 {
		if t.totalTokens.Load() > int64(t.budget.MaxTokensPerSession) {
			return true
		}
	}

	// 检查单次调用 Token 上限
	if t.budget.MaxTokensPerCall > 0 {
		if t.lastTokens.Load() > int64(t.budget.MaxTokensPerCall) {
			return true
		}
	}

	return false
}

// Reset 重置追踪器
func (t *CostTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.records = make([]CostRecord, 0)
	t.totalCostBits.Store(0)
	t.totalPromptTokens.Store(0)
	t.totalCompTokens.Store(0)
	t.totalTokens.Store(0)
	t.callCount.Store(0)
	t.lastTokens.Store(0)
}

// SetRecordBudgetCheckHook 设置预算检查 hook（仅测试用）
func SetRecordBudgetCheckHook(hook func()) {
	recordBudgetCheckHook = hook
}
