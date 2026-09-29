// dispatcher.go — Pool 核心调度引擎
//
// 职责：Pool 结构体定义、构造、任务派发（Dispatch）、任务执行（executeTask）、
// Agent 创建（createAgentForTask）、并发信号量（acquireSlot / releaseSlot）。
//
// 拆分说明（代码审查优化）：
//   - dispatcher_ops.go        — 任务查询/取消/清理/生命周期/统计
//   - dispatcher_extensions.go — GoroutinePool / LLMBatch 扩展集成
package pool

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent"
	"github.com/Gleamseekers/AgentPrimordia/internal/concurrency"
	"github.com/Gleamseekers/AgentPrimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/internal/tools"
)

const (
	// perf-v6 Task H：默认 maxConcurrency 提升到 2 * NumCPU（最少 16）
	// 老值 10 在多核机器上明显不足
	defaultMaxConcurrency   = 16
	defaultPoolTimeout      = 5 * time.Minute
	poolEventBufferSize     = 100
	defaultMaxTurns         = 50
	defaultMaxRetainedTasks = 1000
)

var (
	ErrTimeout      = errors.New("operation timed out")
	ErrTaskNotFound = errors.New("task not found")
	// ErrContextCanceled 表示任务因上下文取消而终止。
	// pkg.ErrContextCanceled re-export 本 sentinel；executeTask 的 ctx 取消
	// 路径以 %w 包装返回，使结构化错误码 CTX_001 可命中
	//（修复评估报告 §四.1-④：此前 pkg 侧 sentinel 无任何抛出点）。
	ErrContextCanceled = errors.New("context canceled")
)

// TaskError 单个任务错误信息
type TaskError struct {
	TaskID string
	Error  error
}

// AggregatedError 聚合多个任务的错误信息
// 支持 errors.Is/errors.As 解包，errors.Join 风格的语义
type AggregatedError struct {
	TaskErrors []TaskError
}

// Error 返回聚合的错误信息
func (e *AggregatedError) Error() string {
	if len(e.TaskErrors) == 0 {
		return "no errors"
	}
	if len(e.TaskErrors) == 1 {
		return fmt.Sprintf("task %s failed: %v", e.TaskErrors[0].TaskID, e.TaskErrors[0].Error)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d tasks failed: ", len(e.TaskErrors))
	for i, te := range e.TaskErrors {
		if i > 0 {
			b.WriteString("; ")
		}
		fmt.Fprintf(&b, "%s: %v", te.TaskID, te.Error)
	}
	return b.String()
}

// Unwrap 返回所有子错误，支持 errors.Is/errors.As 遍历
func (e *AggregatedError) Unwrap() []error {
	errs := make([]error, len(e.TaskErrors))
	for i, te := range e.TaskErrors {
		errs[i] = te.Error
	}
	return errs
}

// Is 检查 AggregatedError 中是否包含目标错误
func (e *AggregatedError) Is(target error) bool {
	for _, te := range e.TaskErrors {
		if errors.Is(te.Error, target) {
			return true
		}
	}
	return false
}

// As 尝试将 AggregatedError 中的任意错误转换为目标类型
func (e *AggregatedError) As(target interface{}) bool {
	for _, te := range e.TaskErrors {
		if errors.As(te.Error, target) {
			return true
		}
	}
	return false
}

type Pool struct {
	mu           sync.RWMutex
	config       PoolConfig
	tasks        map[string]*poolTask
	eventCh      chan PoolEvent
	agents       map[string]agent.Agent
	stats        PoolStats
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	startTime    time.Time
	model        llm.Provider
	toolkit      *tools.Registry
	agentFactory AgentFactory
	closeOnce    sync.Once
	shutdown     atomic.Bool // 优雅关闭标志，置位后拒绝新任务

	// 动态并发度信号量（替代固定容量的 channel）
	// 使用 sync.Cond 实现可动态调整上限的计数信号量
	semaMu   sync.Mutex
	semaCond *sync.Cond

	// droppedEvents 记录因 eventCh 满而被丢弃的事件数（用于可观测性）
	droppedEvents atomic.Int64
	semaCount     int64 // 当前已获取令牌的 goroutine 数

	// 优化（perf-v2）：会话索引，将 GetTasksBySession/CancelBySession 从 O(n) 优化为 O(k)
	sessionIndex map[string]map[string]struct{} // sessionID -> set of taskIDs

	// 优化（Task 8）：使用 atomic 增量维护 stats，Stats() 无需遍历 tasks map
	runningCount   atomic.Int64
	queuedCount    atomic.Int64
	completedCount atomic.Int64
	failedCount    atomic.Int64

	// Task 9：动态 Agent 池（自动扩缩容）
	autoScaler         *AutoScaler
	autoScalerRunning  atomic.Bool
	dynamicConcurrency atomic.Int64 // 动态并发度限制，由 AutoScaler 更新

	// Phase 3 Task 4：内部动态协程池（可选）。
	// nil 表示未启用，调用 SubmitBackground / GoroutinePoolStats 会返回错误。
	goroutinePool *concurrency.GoroutinePool

	// Phase 3 Task 6：可选的 LLM BatchProcessor 引用。
	// 调用 SetLLMBatchProcessor 后，Pool 的 model 字段会被替换为该 BatchProcessor，
	// 实现 Agent 调用的自动批处理合并。
	batchProcessor *llm.BatchProcessor

	// Phase 5 Task 5：可选的多租户配额管理。
	// nil 表示未启用；调用 EnableTenantRegistry 后启用 AcquireForTenant / SubmitForTenant。
	tenantRegistry *TenantRegistry
}

type poolTask struct {
	config     TaskConfig
	sessionID  string
	status     atomic.Value // PoolTaskStatus string (perf-v6 Task A：atomic.Value 无锁读)
	result     *TaskResult
	startTime  time.Time
	retryCount int
	cancelCtx  context.Context
	cancelFunc context.CancelFunc
}

// loadStatus 从 atomic.Value 安全读取 PoolTaskStatus（perf-v6 Task A）
func (pt *poolTask) loadStatus() PoolTaskStatus {
	v := pt.status.Load()
	if v == nil {
		return PoolTaskQueued
	}
	return v.(PoolTaskStatus)
}

// storeStatus 安全写 PoolTaskStatus 到 atomic.Value（perf-v6 Task A）
func (pt *poolTask) storeStatus(s PoolTaskStatus) {
	pt.status.Store(s)
}

func NewPool(cfg PoolConfig) *Pool {
	if cfg.MaxConcurrency <= 0 {
		cfg.MaxConcurrency = defaultMaxConcurrency
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultPoolTimeout
	}
	if cfg.MaxRetainedTasks <= 0 {
		cfg.MaxRetainedTasks = defaultMaxRetainedTasks
	}

	ctx, cancel := context.WithCancel(context.Background())

	p := &Pool{
		config:       cfg,
		tasks:        make(map[string]*poolTask),
		agents:       make(map[string]agent.Agent),
		eventCh:      make(chan PoolEvent, poolEventBufferSize),
		sessionIndex: make(map[string]map[string]struct{}),
		stats: PoolStats{
			MaxConcurrency: cfg.MaxConcurrency,
		},
		ctx:    ctx,
		cancel: cancel,
	}
	p.semaCond = sync.NewCond(&p.semaMu)

	// Task 9：初始化 AutoScaler 和动态并发度
	p.dynamicConcurrency.Store(int64(cfg.MaxConcurrency))
	if cfg.AutoScaler != nil {
		p.autoScaler = NewAutoScaler(*cfg.AutoScaler)
	}

	// Phase 3 Task 4：初始化内部 GoroutinePool（可选）
	if cfg.GoroutinePool != nil {
		gpCfg := concurrency.Config{
			MinWorkers:  cfg.GoroutinePool.MinWorkers,
			MaxWorkers:  cfg.GoroutinePool.MaxWorkers,
			QueueSize:   cfg.GoroutinePool.QueueSize,
			IdleTimeout: cfg.GoroutinePool.IdleTimeout,
		}
		p.goroutinePool = concurrency.NewGoroutinePool(gpCfg)
	}

	return p
}

type dispatchItem struct {
	idx        int
	task       TaskConfig
	taskCtx    context.Context
	taskCancel context.CancelFunc
}

func (p *Pool) Dispatch(ctx context.Context, tasks []TaskConfig) ([]*TaskResult, error) {
	if len(tasks) == 0 {
		return []*TaskResult{}, nil
	}

	// 优雅关闭期间拒绝新任务
	if p.shutdown.Load() {
		return nil, errors.New("pool is shutting down")
	}

	p.mu.Lock()
	p.startTime = time.Now()
	p.stats.TotalTasks += len(tasks)
	p.mu.Unlock()
	// 优化（Task 8）：使用原子计数
	p.queuedCount.Add(int64(len(tasks)))

	// M8 修复：派发前检查是否需要清理终态任务，避免 task map 无界增长
	p.cleanupIfNeeded()
	results := make([]*TaskResult, len(tasks))
	errCh := make(chan error, len(tasks))

	taskCh := make(chan dispatchItem, len(tasks))

	// 优化（Task 3.6）：先批量构建 poolTask，再一次加锁写入 map，
	// 将 N 次 Lock/Unlock 减少为 1 次，降低高并发派发时的锁竞争。
	type preparedTask struct {
		pt   *poolTask
		item dispatchItem
	}
	prepared := make([]preparedTask, 0, len(tasks))
	now := time.Now()

	for i, task := range tasks {
		if task.ID == "" {
			task.ID = generateTaskID(i)
		}

		taskCtx, taskCancel := context.WithCancel(ctx)

		pt := &poolTask{
			config:     task,
			sessionID:  task.SessionID,
			startTime:  now,
			cancelCtx:  taskCtx,
			cancelFunc: taskCancel,
		}
		pt.storeStatus(PoolTaskQueued) // perf-v6 Task A

		prepared = append(prepared, preparedTask{
			pt:   pt,
			item: dispatchItem{idx: i, task: task, taskCtx: taskCtx, taskCancel: taskCancel},
		})
	}

	// 单次加锁：批量写入 + 重复检测
	p.mu.Lock()
	for _, prep := range prepared {
		if _, exists := p.tasks[prep.item.task.ID]; exists {
			p.mu.Unlock()
			// 回滚：取消所有已创建的 context
			for _, p2 := range prepared {
				p2.item.taskCancel()
			}
			close(taskCh)
			return nil, fmt.Errorf("duplicate task ID: %s", prep.item.task.ID)
		}
	}
	for _, prep := range prepared {
		p.tasks[prep.item.task.ID] = prep.pt
		// 优化（perf-v2）：维护会话索引
		if prep.item.task.SessionID != "" {
			if p.sessionIndex[prep.item.task.SessionID] == nil {
				p.sessionIndex[prep.item.task.SessionID] = make(map[string]struct{})
			}
			p.sessionIndex[prep.item.task.SessionID][prep.item.task.ID] = struct{}{}
		}
	}
	p.mu.Unlock()

	// 写入 dispatch channel
	for _, prep := range prepared {
		taskCh <- prep.item
	}
	close(taskCh)

	// 使用固定 worker 池，避免任务数过大时创建大量 goroutine
	workerCount := p.config.MaxConcurrency
	if workerCount > len(tasks) {
		workerCount = len(tasks)
	}
	p.wg.Add(workerCount)
	for w := 0; w < workerCount; w++ {
		go func() {
			defer p.wg.Done()
			for item := range taskCh {
				func() {
					defer item.taskCancel()

					result, err := p.executeTask(item.taskCtx, item.task)
					if result == nil {
						result = &TaskResult{
							TaskID: item.task.ID,
							Task:   item.task,
							Error:  err,
							Status: PoolTaskFailed,
						}
					}
					results[item.idx] = result

					if err != nil {
						errCh <- err
					} else {
						errCh <- nil
					}
				}()
			}
		}()
	}

	p.wg.Wait()
	close(errCh)

	// 收集所有错误，构建聚合错误信息
	var taskErrors []TaskError
	for _, r := range results {
		if r != nil && r.Error != nil {
			taskErrors = append(taskErrors, TaskError{TaskID: r.TaskID, Error: r.Error})
		}
	}

	if len(taskErrors) == 0 {
		return results, nil
	}
	if len(taskErrors) == 1 {
		return results, taskErrors[0].Error
	}
	return results, &AggregatedError{TaskErrors: taskErrors}
}

func (p *Pool) executeTask(ctx context.Context, task TaskConfig) (*TaskResult, error) {
	startTime := time.Now()

	pt := p.getTask(task.ID)
	if pt == nil {
		return &TaskResult{
			TaskID: task.ID,
			Task:   task,
			Error:  ErrTaskNotFound,
			Status: PoolTaskFailed,
		}, fmt.Errorf("%w: %s", ErrTaskNotFound, task.ID)
	}

	// 获取动态信号量（仅获取一次，重试期间持有不释放）
	if err := p.acquireSlot(ctx); err != nil {
		if err == ctx.Err() {
			p.mu.Lock()
			pt.storeStatus(PoolTaskCancelled)
			p.mu.Unlock()
			p.queuedCount.Add(-1)
			p.emitEvent(PoolEvent{Type: "task_cancelled", TaskID: task.ID})
			// 修复评估报告 §四.1-④：以 %w 包装 ErrContextCanceled，
			// 使 CTX_001 错误码可命中（errors.Is 仍匹配 context.Canceled）。
			cancelErr := fmt.Errorf("%w: %w", ErrContextCanceled, ctx.Err())
			return &TaskResult{
				TaskID: task.ID,
				Task:   task,
				Error:  cancelErr,
				Status: PoolTaskCancelled,
			}, cancelErr
		}
		p.mu.Lock()
		pt.storeStatus(PoolTaskFailed)
		p.mu.Unlock()
		p.queuedCount.Add(-1)
		p.failedCount.Add(1)
		p.emitEvent(PoolEvent{Type: "task_failed", TaskID: task.ID, Data: "timeout"})
		return &TaskResult{
			TaskID: task.ID,
			Task:   task,
			Error:  ErrTimeout,
			Status: PoolTaskFailed,
		}, ErrTimeout
	}
	defer p.releaseSlot()

	// 重试循环（semaphore 已持有，不再重复获取）
	p.queuedCount.Add(-1)
	p.runningCount.Add(1)
	defer p.runningCount.Add(-1)
	for {
		p.mu.Lock()
		pt.storeStatus(PoolTaskRunning)
		p.mu.Unlock()
		p.emitEvent(PoolEvent{Type: "task_started", TaskID: task.ID, Data: task.Title})

		taskCtx, cancel := context.WithTimeout(ctx, p.config.Timeout)

		agt, err := p.createAgentForTask(task)
		if err != nil {
			cancel()
			p.mu.Lock()
			pt.storeStatus(PoolTaskFailed)
			p.mu.Unlock()
			p.failedCount.Add(1)
			p.emitEvent(PoolEvent{Type: "task_failed", TaskID: task.ID, Data: err.Error()})
			return &TaskResult{
				TaskID: task.ID,
				Task:   task,
				Error:  err,
				Status: PoolTaskFailed,
			}, err
		}
		result, runErr := agt.Run(taskCtx, agent.UserMessage(task.Prompt))
		err = runErr
		taskCtxErr := taskCtx.Err()
		cancel()

		duration := time.Since(startTime)

		if err != nil {
			if taskCtxErr == context.DeadlineExceeded {
				p.mu.Lock()
				pt.storeStatus(PoolTaskFailed)
				p.mu.Unlock()
				p.failedCount.Add(1)
				p.emitEvent(PoolEvent{Type: "task_failed", TaskID: task.ID, Data: "timeout"})
			} else if taskCtxErr == context.Canceled || ctx.Err() != nil {
				p.mu.Lock()
				pt.storeStatus(PoolTaskCancelled)
				p.mu.Unlock()
				p.emitEvent(PoolEvent{Type: "task_cancelled", TaskID: task.ID})
			} else {
				p.mu.Lock()
				pt.storeStatus(PoolTaskFailed)
				p.mu.Unlock()

				if p.isRetryable(err) && pt.retryCount < p.config.RetryPolicy.MaxRetries {
					pt.retryCount++
					if ctx.Err() != nil {
						p.mu.Lock()
						pt.storeStatus(PoolTaskCancelled)
						p.mu.Unlock()
						return pt.result, fmt.Errorf("%w: %w", ErrContextCanceled, ctx.Err())
					}
					select {
					case <-time.After(p.config.RetryPolicy.Backoff):
					case <-ctx.Done():
						p.mu.Lock()
						pt.storeStatus(PoolTaskCancelled)
						p.mu.Unlock()
						return pt.result, fmt.Errorf("%w: %w", ErrContextCanceled, ctx.Err())
					}
					continue
				}

				p.failedCount.Add(1)
				p.emitEvent(PoolEvent{Type: "task_failed", TaskID: task.ID, Data: err.Error()})
			}
		} else {
			p.mu.Lock()
			pt.storeStatus(PoolTaskCompleted)
			p.mu.Unlock()
			p.completedCount.Add(1)
			p.emitEvent(PoolEvent{Type: "task_completed", TaskID: task.ID})
		}

		// 竞态修复（评估报告 §三.1-①）：pt.result 与 GetTask/ListTasks 的
		// p.mu.RLock 读并发，必须持锁写。
		p.mu.Lock()
		pt.result = &TaskResult{
			TaskID:   task.ID,
			Task:     task,
			Response: result,
			Error:    err,
			Duration: duration,
			Status:   pt.loadStatus(),
		}
		delete(p.agents, task.ID)
		p.mu.Unlock()

		return pt.result, err
	}
}

func (p *Pool) createAgentForTask(task TaskConfig) (agent.Agent, error) {
	// 竞态修复（评估报告 §三.1-②）：p.model/p.toolkit 与 SetModel/SetToolkit
	// 的持锁写并发，必须在同一 RLock 内读取。
	p.mu.RLock()
	factory := p.agentFactory
	model := p.model
	toolkit := p.toolkit
	p.mu.RUnlock()

	if factory != nil {
		factoryCfg := AgentFactoryConfig{
			Name:         task.Title,
			SystemPrompt: p.config.DefaultAgent.SystemPrompt,
			MaxTurns:     task.MaxTurns,
			Temperature:  p.config.DefaultAgent.Temperature,
			FilesScope:   task.FilesScope,
			SessionID:    task.SessionID,
			Metadata:     task.Metadata,
		}

		if factoryCfg.MaxTurns == 0 {
			factoryCfg.MaxTurns = p.config.DefaultAgent.MaxTurns
		}
		if factoryCfg.MaxTurns == 0 {
			factoryCfg.MaxTurns = defaultMaxTurns
		}

		agt := factory(factoryCfg)
		p.mu.Lock()
		p.agents[task.ID] = agt
		p.mu.Unlock()
		return agt, nil
	}

	// 解析配置参数
	name := task.Title
	systemPrompt := p.config.DefaultAgent.SystemPrompt

	maxTurns := task.MaxTurns
	if maxTurns == 0 {
		maxTurns = p.config.DefaultAgent.MaxTurns
	}
	if maxTurns == 0 {
		maxTurns = defaultMaxTurns
	}

	temperature := p.config.DefaultAgent.Temperature

	// 使用 NewAgent 创建 Agent，通过 Option 模式注入配置
	reactAgt, err := agent.NewAgent(name, systemPrompt, model,
		agent.WithMaxTurns(maxTurns),
		agent.WithTemperature(temperature),
	)
	if err != nil {
		return nil, err
	}

	// v0.7.0: Toolkit 字段已废弃，通过链式 API 注入tool能力
	var agt agent.Agent = reactAgt
	if toolkit != nil {
		agt = reactAgt.WithToolkit(toolkit)
	}

	p.mu.Lock()
	p.agents[task.ID] = agt
	p.mu.Unlock()
	return agt, nil
}

// SetAgentFactory 设置 Agent 工厂函数，替代默认的 ReActAgent 创建逻辑
func (p *Pool) SetAgentFactory(factory AgentFactory) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.agentFactory = factory
}

// acquireSlot 获取动态并发度信号量令牌。
// 使用 sync.Cond 阻塞等待，避免轮询造成的 CPU 浪费和延迟。
// 返回 error 仅在 ctx 被取消或超时时非 nil。
func (p *Pool) acquireSlot(ctx context.Context) error {
	// 快速路径：检查是否可以立即获取
	p.semaMu.Lock()
	if p.semaCount < p.dynamicConcurrency.Load() {
		p.semaCount++
		p.semaMu.Unlock()
		return nil
	}
	p.semaMu.Unlock()

	// 慢路径：通过 sync.Cond 阻塞等待令牌
	// 辅助 goroutine 在 ctx 取消或超时到期时 Broadcast 唤醒所有等待者
	//（竞态修复评估报告 §三.1-③：此前定时器不唤醒 Cond，等待者会无限阻塞
	// 直到其他事件触发，超时语义被绕过）。
	p.semaMu.Lock()
	defer p.semaMu.Unlock()

	var timerExpired atomic.Bool
	var timerC <-chan time.Time
	if p.config.Timeout > 0 {
		timer := time.NewTimer(p.config.Timeout)
		defer timer.Stop()
		timerC = timer.C
	}

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			p.semaCond.Broadcast() // 唤醒所有等待者
		case <-timerC:
			timerExpired.Store(true)
			p.semaCond.Broadcast() // 超时到期也唤醒，避免等待者无限阻塞
		case <-done:
		}
	}()

	for p.semaCount >= p.dynamicConcurrency.Load() {
		// 超时优先：定时器已到期则不再等待
		if timerExpired.Load() {
			return ErrTimeout
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		p.semaCond.Wait()
	}
	// Wait 返回后（被 Signal 唤醒且定时器恰在同一时刻到期）再次确认超时
	if timerExpired.Load() {
		return ErrTimeout
	}

	p.semaCount++
	return nil
}

// releaseSlot 释放动态并发度信号量令牌，并唤醒一个等待者。
func (p *Pool) releaseSlot() {
	p.semaMu.Lock()
	p.semaCount--
	p.semaCond.Signal()
	p.semaMu.Unlock()
}

// generateTaskID 生成唯一任务 ID。优化（Task 8）：使用 strconv 避免 fmt.Sprintf 的反射分配。
var taskIDCounter atomic.Int64

func generateTaskID(index int) string {
	seq := taskIDCounter.Add(1)
	return "task_" + strconv.FormatInt(time.Now().UnixNano(), 10) + "_" + strconv.FormatInt(int64(index), 10) + "_" + strconv.FormatInt(seq, 10)
}
