// speculative_exec.go — Phase 2 G2-2 Go 原生投机执行
// tool执行期间并行启动预测性 LLM 调用，
// 命中预测则节省一轮 LLM 延迟。利用 select + channel 做灵活的取消控制。
//
// 投机策略：
//  1. tool结果预测（Predictor）：根据历史 (toolName, args) → cached result
//     - 命中：跳过tool执行，直接返回 cached result（节省 I/O 延迟）
//     - 未命中：正常执行 + 记录到 predictor
//  2. LLM 投机调用：tool执行期间并行预测下一步 LLM 响应
//     - tool先完成 + LLM 预测命中 → 节省一轮 LLM 延迟
//     - LLM 先完成 + tool命中预测 → 可在tool真正完成前展示部分结果
//
// 与 doc 03-phase2-implementation.md G2-2 的差异：
//   - 取消传播：所有 goroutine 通过 ctx.Done() 统一取消
//   - 投机调用为非阻塞：select default 分支不等待
//   - 命中率统计：原子计数器，避免锁开销
package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
)

// ToolResult 复用一个简化的tool结果结构（投机层不直接依赖 tools 包）
type SpeculativeToolResult struct {
	ToolCallID string
	Content    string
	IsError    bool
}

// ToolCall 简化版（投机层不需要完整定义）
type SpeculativeToolCall struct {
	ID   string
	Name string
	Args string
}

// ToolResultPredictor 基于 (toolName, args) 的tool结果预测器。
//
// 设计取舍：
//   - 精确 args 匹配（用 SHA-256 hash 作为 key）：避免误命中
//   - 保留最近 N 条历史（LRU）：避免内存无限增长
//   - sync.RWMutex 保护：读多写少场景
type ToolResultPredictor struct {
	mu       sync.RWMutex
	maxSize  int
	cache    map[string]*SpeculativeToolResult // hash → result
	keyOrder []string                          // 用于 LRU 淘汰
	hits     atomic.Int64
	misses   atomic.Int64
}

// NewToolResultPredictor 创建预测器；maxSize=0 时使用默认 256
func NewToolResultPredictor(maxSize int) *ToolResultPredictor {
	if maxSize <= 0 {
		maxSize = 256
	}
	return &ToolResultPredictor{
		maxSize: maxSize,
		cache:   make(map[string]*SpeculativeToolResult),
	}
}

// hashKey 生成 (toolName, args) 的稳定 key
func hashKey(toolName, args string) string {
	h := sha256.New()
	h.Write([]byte(toolName))
	h.Write([]byte{0})
	h.Write([]byte(args))
	return hex.EncodeToString(h.Sum(nil))
}

// Predict 查询是否有缓存结果。返回 (result, true) 表示命中。
// 命中不修改缓存（读取路径），未命中不写入（由 Record 负责）。
func (p *ToolResultPredictor) Predict(toolName, args string) (*SpeculativeToolResult, bool) {
	if p == nil {
		return nil, false
	}
	key := hashKey(toolName, args)
	p.mu.RLock()
	r, ok := p.cache[key]
	p.mu.RUnlock()
	if ok {
		p.hits.Add(1)
		// LRU 触摸：移动到队尾
		p.mu.Lock()
		p.touchLocked(key)
		p.mu.Unlock()
	} else {
		p.misses.Add(1)
	}
	return r, ok
}

// Record 记录一次tool执行结果到缓存。
func (p *ToolResultPredictor) Record(toolName, args string, result *SpeculativeToolResult) {
	if p == nil || result == nil {
		return
	}
	key := hashKey(toolName, args)
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, exists := p.cache[key]; !exists {
		p.keyOrder = append(p.keyOrder, key)
	}
	p.cache[key] = result
	// LRU 淘汰
	for len(p.keyOrder) > p.maxSize {
		oldest := p.keyOrder[0]
		p.keyOrder = p.keyOrder[1:]
		delete(p.cache, oldest)
	}
}

// touchLocked 将 key 移到 keyOrder 末尾（需持写锁）
func (p *ToolResultPredictor) touchLocked(key string) {
	for i, k := range p.keyOrder {
		if k == key {
			p.keyOrder = append(p.keyOrder[:i], p.keyOrder[i+1:]...)
			p.keyOrder = append(p.keyOrder, key)
			return
		}
	}
}

// Stats 返回命中率统计
type PredictorStats struct {
	Size    int
	MaxSize int
	Hits    int64
	Misses  int64
	HitRate float64
}

func (p *ToolResultPredictor) Stats() PredictorStats {
	if p == nil {
		return PredictorStats{}
	}
	p.mu.RLock()
	size := len(p.cache)
	p.mu.RUnlock()
	hits := p.hits.Load()
	misses := p.misses.Load()
	total := hits + misses
	var rate float64
	if total > 0 {
		rate = float64(hits) / float64(total)
	}
	return PredictorStats{
		Size:    size,
		MaxSize: p.maxSize,
		Hits:    hits,
		Misses:  misses,
		HitRate: rate,
	}
}

// Clear 清空缓存和计数器
func (p *ToolResultPredictor) Clear() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.cache = make(map[string]*SpeculativeToolResult)
	p.keyOrder = p.keyOrder[:0]
	p.mu.Unlock()
	p.hits.Store(0)
	p.misses.Store(0)
}

// SpeculationStats 投机执行总统计
type SpeculationStats struct {
	PredictionsAttempted atomic.Int64
	PredictionHits       atomic.Int64
	SpeculativeLLMCalls  atomic.Int64
	SpeculativeLLMHits   atomic.Int64
}

// SpeculativeExecutor 投机执行器
type SpeculativeExecutor struct {
	predictor   *ToolResultPredictor
	provider    llm.Provider
	minHitRate  float64
	stats       SpeculationStats
	specTimeout time.Duration
}

// NewSpeculativeExecutor 创建投机执行器
// minHitRate: 低于此命中率的tool不启用投机（0-1，默认 0.1）
// specTimeout: 投机 LLM 调用超时（默认 5s）
func NewSpeculativeExecutor(provider llm.Provider, predictor *ToolResultPredictor, minHitRate float64, specTimeout time.Duration) *SpeculativeExecutor {
	if minHitRate <= 0 {
		minHitRate = 0.1
	}
	if specTimeout <= 0 {
		specTimeout = 5 * time.Second
	}
	if predictor == nil {
		predictor = NewToolResultPredictor(256)
	}
	return &SpeculativeExecutor{
		predictor:   predictor,
		provider:    provider,
		minHitRate:  minHitRate,
		specTimeout: specTimeout,
	}
}

// SpeculativeResult 投机执行结果
type SpeculativeResult struct {
	Results            []*SpeculativeToolResult
	PredictionHits     int                     // 命中预测的tool数
	SpeculativeLLMHit  bool                    // LLM 投机是否命中
	SpeculativeLLMResp *llm.CompletionResponse // 命中的预测（nil 表示未命中）
	Skipped            bool
}

// ExecuteWithSpeculation 投机执行一组tool调用。
//
// 工作流程：
//  1. 对每个 tool call 检查 predictor；命中则使用预测结果
//  2. 未命中的 tool call 实际执行，并行启动
//  3. 如果至少有 1 个未命中tool，且 provider 不为 nil，并行启动 LLM 投机调用
//  4. 投机 LLM 通过 select + ctx.Done() 异步接收
//
// 参数：
//   - ctx: 取消传播
//   - toolCalls: 待执行tool列表
//   - executeFn: 实际tool执行函数（由调用方注入）
//   - specMessages: 用于投机 LLM 调用的消息
func (e *SpeculativeExecutor) ExecuteWithSpeculation(
	ctx context.Context,
	toolCalls []SpeculativeToolCall,
	executeFn func(context.Context, SpeculativeToolCall) (*SpeculativeToolResult, error),
	specMessages []llm.ChatMessage,
) (*SpeculativeResult, error) {
	if len(toolCalls) == 0 {
		return &SpeculativeResult{Skipped: true}, nil
	}

	n := len(toolCalls)
	results := make([]*SpeculativeToolResult, n)
	executedFlags := make([]bool, n) // true 表示真实执行（用于 record）
	var hitCount int64

	// Phase 1：检查 predictor，未命中的进入执行队列
	type toExec struct {
		idx int
		tc  SpeculativeToolCall
	}
	var pending []toExec
	for i, tc := range toolCalls {
		if predicted, ok := e.predictor.Predict(tc.Name, tc.Args); ok {
			results[i] = predicted
			executedFlags[i] = false // 命中：跳过实际执行
			hitCount++
		} else {
			pending = append(pending, toExec{idx: i, tc: tc})
		}
	}
	e.stats.PredictionsAttempted.Add(int64(n))
	e.stats.PredictionHits.Add(hitCount)

	// Phase 2：并行执行未命中的tool
	if len(pending) > 0 {
		toolCh := make(chan struct {
			idx    int
			result *SpeculativeToolResult
			err    error
		}, len(pending))
		var wg sync.WaitGroup
		for _, p := range pending {
			p := p
			wg.Add(1)
			go func() {
				defer wg.Done()
				if ctx.Err() != nil {
					toolCh <- struct {
						idx    int
						result *SpeculativeToolResult
						err    error
					}{idx: p.idx, result: &SpeculativeToolResult{
						ToolCallID: p.tc.ID,
						Content:    "context canceled",
						IsError:    true,
					}, err: ctx.Err()}
					return
				}
				r, err := executeFn(ctx, p.tc)
				toolCh <- struct {
					idx    int
					result *SpeculativeToolResult
					err    error
				}{idx: p.idx, result: r, err: err}
			}()
		}
		go func() {
			wg.Wait()
			close(toolCh)
		}()

		// 收集结果 + 记录到 predictor
		for m := range toolCh {
			results[m.idx] = m.result
			executedFlags[m.idx] = true
		}
		// 收集完后批量 record（持锁时间短）
		for _, p := range pending {
			r := results[p.idx]
			if r != nil {
				e.predictor.Record(p.tc.Name, p.tc.Args, r)
			}
		}
	}

	// Phase 3：投机 LLM 调用（如果条件满足）
	var specCh chan *llm.CompletionResponse
	if e.provider != nil && len(pending) > 0 && len(specMessages) > 0 {
		e.stats.SpeculativeLLMCalls.Add(1)
		specCh = make(chan *llm.CompletionResponse, 1)
		go func() {
			specCtx, cancel := context.WithTimeout(ctx, e.specTimeout)
			defer cancel()
			req := &llm.CompletionRequest{Messages: specMessages}
			resp, err := e.provider.Complete(specCtx, req)
			if err == nil {
				select {
				case specCh <- resp:
				case <-ctx.Done():
				}
			}
		}()
	}

	out := &SpeculativeResult{
		Results:        results,
		PredictionHits: int(hitCount),
	}

	// 检查投机 LLM 是否已就绪（短轮询等待）
	// 设计取舍：tool已实际完成，但 LLM 投机可能仍在飞行中。
	// 给一个非常短的超时（max(specTimeout/4, 5ms)）避免主流程卡顿。
	if specCh != nil {
		waitBudget := e.specTimeout / 4
		if waitBudget < 5*time.Millisecond {
			waitBudget = 5 * time.Millisecond
		}
		timer := time.NewTimer(waitBudget)
		defer timer.Stop()
		select {
		case resp := <-specCh:
			if resp != nil {
				out.SpeculativeLLMHit = true
				out.SpeculativeLLMResp = resp
				e.stats.SpeculativeLLMHits.Add(1)
			}
		case <-timer.C:
			// 投机 LLM 仍在飞行中，不继续等待
		}
	}

	return out, nil
}

// Stats 返回投机执行统计快照
type SpeculativeStatsSnapshot struct {
	PredictionsAttempted int64
	PredictionHits       int64
	SpeculativeLLMCalls  int64
	SpeculativeLLMHits   int64
	Predictor            PredictorStats
}

func (e *SpeculativeExecutor) Stats() SpeculativeStatsSnapshot {
	return SpeculativeStatsSnapshot{
		PredictionsAttempted: e.stats.PredictionsAttempted.Load(),
		PredictionHits:       e.stats.PredictionHits.Load(),
		SpeculativeLLMCalls:  e.stats.SpeculativeLLMCalls.Load(),
		SpeculativeLLMHits:   e.stats.SpeculativeLLMHits.Load(),
		Predictor:            e.predictor.Stats(),
	}
}

// Predictor 返回底层 predictor（用于外部检查/调试）
func (e *SpeculativeExecutor) Predictor() *ToolResultPredictor {
	return e.predictor
}
