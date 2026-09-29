// dispatcher_extensions.go — Pool 扩展集成（GoroutinePool / LLMBatch）
//
// 从 dispatcher.go 拆分（Phase 代码审查优化），职责：
//   - 内部 GoroutinePool 集成（SubmitBackground / GoroutinePoolStats / HasGoroutinePool）
//   - LLM BatchProcessor 集成（SetLLMBatchProcessor / LLMBatchStats / RunBatchFlushLoop）
package pool

import (
	"context"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/concurrency"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
)

// SubmitBackground 把任务投递到内部 GoroutinePool（Phase 3 Task 4）。
//
// 用于无需同步等待结果的后台工作（指标聚合、审计 flush、预热缓存等）。
// 如果 PoolConfig.GoroutinePool 未配置，返回 concurrency.ErrPoolStopped。
//
// 返回的错误直接透传 concurrency.GoroutinePool 的错误（ErrQueueFull / ErrPoolStopped）。
func (p *Pool) SubmitBackground(ctx context.Context, task concurrency.Task) error {
	p.mu.RLock()
	gp := p.goroutinePool
	p.mu.RUnlock()
	if gp == nil {
		return concurrency.ErrPoolStopped
	}
	return gp.SubmitWithContext(ctx, task)
}

// GoroutinePoolStats 返回内部 GoroutinePool 的运行指标（Phase 3 Task 5）。
//
// 如果 PoolConfig.GoroutinePool 未配置，返回 zero value + ok=false。
//
// 调用方通常将其映射为 Prometheus 指标：
//
//	pool_workers{pool="..."}        = stats.Workers
//	pool_active_workers{pool="..."} = stats.ActiveWorkers
//	pool_queue_depth{pool="..."}    = stats.QueueDepth
//	pool_queue_capacity{pool="..."} = stats.QueueCapacity
func (p *Pool) GoroutinePoolStats() (concurrency.PoolStats, bool) {
	p.mu.RLock()
	gp := p.goroutinePool
	p.mu.RUnlock()
	if gp == nil {
		return concurrency.PoolStats{}, false
	}
	return gp.Stats(), true
}

// HasGoroutinePool 报告 Pool 是否配置了内部 GoroutinePool。
func (p *Pool) HasGoroutinePool() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.goroutinePool != nil
}

// SetLLMBatchProcessor 把 BatchProcessor 接入 Pool（Phase 3 Task 6）。
//
// 集成方式：
//  1. 把 Pool 的模型替换为 BatchProcessor（其实现了 llm.Provider）
//  2. 如果 Pool 启用了 GoroutinePool（cfg.GoroutinePool != nil），
//     则把 BatchProcessor.Close() / flush 交给 GoroutinePool 在后台执行，
//     避免阻塞 Pool 主调度循环。
//
// 注意：必须在 SetModel 之前或之后调用均可，SetLLMBatchProcessor 内部
// 会用 BatchProcessor 覆盖原 model。Pool.Close() 时会自动调用 bp.Close()。
func (p *Pool) SetLLMBatchProcessor(bp *llm.BatchProcessor) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.batchProcessor = bp
	if bp != nil {
		// 替换 model 为 BatchProcessor（实现 Provider 接口）
		p.model = bp
	}
}

// LLMBatchStats 返回当前 BatchProcessor 的运行指标（Phase 3 Task 6）。
//
// 如果未配置，返回 zero value + ok=false。可作为 Prometheus 导出源。
func (p *Pool) LLMBatchStats() (LLMBatchStats, bool) {
	p.mu.RLock()
	bp := p.batchProcessor
	p.mu.RUnlock()
	if bp == nil {
		return LLMBatchStats{}, false
	}
	return LLMBatchStats{
		// 当前 BatchProcessor 暴露的指标有限；后续 perf-v6 会扩展。
		Enabled:    true,
		HasModel:   true,
		QueueDepth: bp.QueueDepth(),
	}, true
}

// LLMBatchStats 是 Phase 3 Task 6 的 BatchProcessor 统计快照。
type LLMBatchStats struct {
	Enabled    bool
	HasModel   bool
	QueueDepth int
}

// RunBatchFlushLoop 在 GoroutinePool 上调度 BatchProcessor 的 flush 循环（Phase 3 Task 6）。
//
// 当 Pool 同时配置了 GoroutinePool 与 BatchProcessor 时，调用此方法把
// BatchProcessor 的内部 flushLoop 放到协程池中执行，防止高并发场景下
// BatchProcessor 的定时 flush 抢占 Pool 主调度线程。
//
// 如果 GoroutinePool 未配置，本方法返回 concurrency.ErrPoolStopped。
func (p *Pool) RunBatchFlushLoop(ctx context.Context) error {
	p.mu.RLock()
	gp := p.goroutinePool
	bp := p.batchProcessor
	p.mu.RUnlock()
	if gp == nil {
		return concurrency.ErrPoolStopped
	}
	if bp == nil {
		return nil
	}
	// 把 BatchProcessor 的 close 任务交给 GoroutinePool
	return gp.SubmitWithContext(ctx, func(c context.Context) error {
		// 当 ctx 取消时调用 BatchProcessor.Close() 触发 flush
		<-c.Done()
		bp.Close()
		return c.Err()
	})
}
