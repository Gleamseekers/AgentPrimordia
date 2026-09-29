// dispatcher_ops.go — Pool 任务查询、取消、清理与生命周期管理
//
// 从 dispatcher.go 拆分（Phase 代码审查优化），职责：
//   - 任务状态查询（GetTask / ListTasks / GetTasksBySession / ListAgents）
//   - 任务取消（Cancel / CancelAll / CancelBySession）
//   - 任务清理（Cleanup / cleanupIfNeeded）
//   - 生命周期（GracefulShutdown / Close）
//   - 统计与事件（Stats / EventChannel / emitEvent / DroppedEvents）
//   - 配置设置（SetModel / SetToolkit）
package pool

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools"
)

func (p *Pool) getTask(id string) *poolTask {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.tasks[id]
}

func (p *Pool) emitEvent(event PoolEvent) {
	event.Timestamp = time.Now()
	select {
	case p.eventCh <- event:
	default:
		// 事件 channel 满，丢弃事件并记录 warning（背压策略）
		n := p.droppedEvents.Add(1)
		if n == 1 || n%100 == 0 {
			// 首次丢弃或每 100 次记录一次，避免日志风暴
			slog.Warn("[pool] event channel full, events dropped",
				"dropped_total", n,
				"event_type", event.Type,
			)
		}
	}
}

// DroppedEvents 返回因 channel 满而被丢弃的事件总数。
func (p *Pool) DroppedEvents() int64 {
	return p.droppedEvents.Load()
}

func (p *Pool) isRetryable(err error) bool {
	if p.config.RetryPolicy.MaxRetries == 0 {
		return false
	}

	errMsg := err.Error()
	for _, pattern := range p.config.RetryPolicy.RetryableErrors {
		if strings.Contains(errMsg, pattern) {
			return true
		}
	}

	return false
}

func (p *Pool) Cancel(taskID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	task, exists := p.tasks[taskID]
	if !exists {
		return ErrTaskNotFound
	}

	if task.loadStatus() == PoolTaskRunning {
		task.cancelFunc()
		task.storeStatus(PoolTaskCancelled) // perf-v6 Task A
	}

	p.emitEvent(PoolEvent{Type: "task_cancelled", TaskID: taskID})
	return nil
}

func (p *Pool) CancelAll() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for id, task := range p.tasks {
		if task.loadStatus() == PoolTaskQueued || task.loadStatus() == PoolTaskRunning {
			task.cancelFunc()
			task.storeStatus(PoolTaskCancelled) // perf-v6 Task A
			p.emitEvent(PoolEvent{Type: "task_cancelled", TaskID: id})
		}
	}
}

// GetTasksBySession 返回指定会话的任务结果。
// 优化（perf-v2）：使用会话索引 O(k) 查找替代 O(n) 全量遍历。
func (p *Pool) GetTasksBySession(sessionID string) []TaskResult {
	p.mu.RLock()
	defer p.mu.RUnlock()

	taskIDs, ok := p.sessionIndex[sessionID]
	if !ok {
		return nil
	}

	results := make([]TaskResult, 0, len(taskIDs))
	for taskID := range taskIDs {
		pt, exists := p.tasks[taskID]
		if !exists {
			continue
		}
		if pt.result != nil {
			results = append(results, *pt.result)
		} else {
			results = append(results, TaskResult{
				TaskID: pt.config.ID,
				Task:   pt.config,
				Status: pt.loadStatus(),
			})
		}
	}
	return results
}

// CancelBySession 取消指定会话的所有任务。
// 优化（perf-v2）：使用会话索引 O(k) 查找替代 O(n) 全量遍历。
func (p *Pool) CancelBySession(sessionID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	taskIDs, ok := p.sessionIndex[sessionID]
	if !ok {
		return nil
	}

	for taskID := range taskIDs {
		pt, exists := p.tasks[taskID]
		if !exists {
			continue
		}
		if pt.loadStatus() == PoolTaskQueued || pt.loadStatus() == PoolTaskRunning {
			pt.cancelFunc()
			pt.storeStatus(PoolTaskCancelled) // perf-v6 Task A
			p.emitEvent(PoolEvent{Type: "task_cancelled", TaskID: taskID})
		}
	}
	return nil
}

func (p *Pool) GetTask(id string) (TaskResult, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	pt, exists := p.tasks[id]
	if !exists || pt.result == nil {
		return TaskResult{}, false
	}
	return *pt.result, true
}

// ListTasks 返回所有任务的结果列表
func (p *Pool) ListTasks() []TaskResult {
	p.mu.RLock()
	defer p.mu.RUnlock()

	results := make([]TaskResult, 0, len(p.tasks))
	for _, pt := range p.tasks {
		if pt.result != nil {
			results = append(results, *pt.result)
		} else {
			results = append(results, TaskResult{
				TaskID: pt.config.ID,
				Task:   pt.config,
				Status: pt.loadStatus(),
			})
		}
	}
	return results
}

// ListAgents 返回所有 Agent 的统计信息，按任务 ID 索引
func (p *Pool) ListAgents() map[string]agent.AgentStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make(map[string]agent.AgentStats, len(p.agents))
	for id, agt := range p.agents {
		result[id] = agt.Stats()
	}
	return result
}

// Stats 直接读取原子计数器 + 缓存的 stats 字段，避免双重锁。
// 优化（Task 8）：Stats() 不再调用 updateStats() 走写锁后再读 RLock，
// 改为仅一次读锁 + 直接读原子变量。
func (p *Pool) Stats() PoolStats {
	p.mu.RLock()
	defer p.mu.RUnlock()
	// 原子读取叠加到返回的快照上，避免修改共享的 p.stats
	stats := p.stats
	stats.RunningTasks = int(p.runningCount.Load())
	stats.QueuedTasks = int(p.queuedCount.Load())
	stats.CompletedTasks = int(p.completedCount.Load())
	stats.FailedTasks = int(p.failedCount.Load())
	stats.ActiveConcurrency = stats.RunningTasks
	return stats
}

func (p *Pool) EventChannel() <-chan PoolEvent {
	return p.eventCh
}

func (p *Pool) SetModel(model llm.Provider) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.model = model
}

func (p *Pool) SetToolkit(registry *tools.Registry) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.toolkit = registry
}

// Cleanup 清理已完成、失败或取消的任务记录，释放 tasks map 中的内存
// 优化（perf-v2）：同步清理会话索引
func (p *Pool) Cleanup() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, t := range p.tasks {
		if t.loadStatus() == PoolTaskCompleted || t.loadStatus() == PoolTaskFailed || t.loadStatus() == PoolTaskCancelled {
			// 清理会话索引
			if t.sessionID != "" {
				if ids, ok := p.sessionIndex[t.sessionID]; ok {
					delete(ids, id)
					if len(ids) == 0 {
						delete(p.sessionIndex, t.sessionID)
					}
				}
			}
			delete(p.tasks, id)
		}
	}
}

// cleanupIfNeeded 当 task map 超过 MaxRetainedTasks 阈值时自动清理终态任务（M8 修复）。
// 优化（perf-v2）：同步清理会话索引，避免索引泄漏。
func (p *Pool) cleanupIfNeeded() {
	if p.config.MaxRetainedTasks <= 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.tasks) < p.config.MaxRetainedTasks {
		return
	}
	for id, t := range p.tasks {
		if t.loadStatus() == PoolTaskCompleted || t.loadStatus() == PoolTaskFailed || t.loadStatus() == PoolTaskCancelled {
			// 清理会话索引
			if t.sessionID != "" {
				if ids, ok := p.sessionIndex[t.sessionID]; ok {
					delete(ids, id)
					if len(ids) == 0 {
						delete(p.sessionIndex, t.sessionID)
					}
				}
			}
			delete(p.tasks, id)
		}
	}
}

// GracefulShutdown 优雅关闭：停止接受新任务，等待正在执行的任务完成
func (p *Pool) GracefulShutdown(ctx context.Context) error {
	// 标记为关闭状态，拒绝新任务
	p.shutdown.Store(true)

	// 如果 Pool 已关闭（cancel 已调用），直接返回
	select {
	case <-p.ctx.Done():
		return nil
	default:
	}

	// 等待正在执行的任务完成
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.ctx.Done():
			// Pool 已被 Close()，直接返回
			return nil
		case <-ticker.C:
			if p.runningCount.Load() == 0 {
				p.Close()
				return nil
			}
		}
	}
}

func (p *Pool) Close() {
	p.closeOnce.Do(func() {
		// 广播唤醒所有等待信号量的 goroutine
		p.semaCond.Broadcast()
		p.cancel()
		p.wg.Wait()
		close(p.eventCh)
	})
}
