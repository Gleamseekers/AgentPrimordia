// react_persist.go — 记忆保存 + 检查点 + 上下文裁剪
// 负责 Agent 运行过程中的持久化操作：记忆存储、检查点保存、上下文窗口裁剪
package agent

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"agentprimordia/internal/memory"
	"agentprimordia/internal/persist"
)

// saveMemoryChBuffer 是异步 saveMemory 队列容量。
// 高吞吐场景下消息数远大于此值时会触发丢弃，但持久化路径不应阻塞主循环。
const saveMemoryChBuffer = 256

// memoryWriter 封装异步记忆写入队列，从 ReActAgent 中剥离独立管理。
// 每个 Run() 都有独立的 channel + goroutine + doneCh，
// flush 关闭 channel 后等待 doneCh，下次 saveMemory 重新创建。
type memoryWriter struct {
	ch     chan *memory.Episode
	doneCh chan struct{}
	mu     sync.Mutex
	logger *slog.Logger
	mem    MemoryStore
}

// newMemoryWriter 创建 memoryWriter 实例（不启动 goroutine，首次 submit 时懒启动）
func newMemoryWriter(mem MemoryStore, logger *slog.Logger) *memoryWriter {
	return &memoryWriter{logger: logger, mem: mem}
}

// ensureStarted 确保消费 goroutine 已启动（懒启动，锁内完成）
func (w *memoryWriter) ensureStarted() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.ch == nil {
		ch := make(chan *memory.Episode, saveMemoryChBuffer)
		w.ch = ch
		doneCh := make(chan struct{})
		w.doneCh = doneCh
		mem := w.mem
		logger := w.logger
		go func() {
			defer close(doneCh)
			for ep := range ch {
				writeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				if err := mem.Add(writeCtx, ep); err != nil {
					logger.Warn("保存记忆失败", "error", err, "role", ep.Role)
				}
				cancel()
			}
		}()
	}
}

// submit 非阻塞提交 Episode 到写入队列
func (w *memoryWriter) submit(ep *memory.Episode) {
	w.ensureStarted()
	select {
	case w.ch <- ep:
	default:
		w.logger.Warn("异步记忆队列已满，丢弃写入", "role", ep.Role)
	}
}

// flush 关闭写入队列并等待所有待写入完成
// 关闭后下次 submit 会重新创建 channel 和 goroutine
func (w *memoryWriter) flush() {
	w.mu.Lock()
	ch := w.ch
	doneCh := w.doneCh
	w.ch = nil
	w.doneCh = nil
	w.mu.Unlock()

	if ch == nil {
		return
	}
	close(ch)
	if doneCh != nil {
		<-doneCh
	}
}

// summaryWorkerCount 摘要提取 worker 池大小（P1-5）。
// 修复前 saveMemory 为每条消息派生一个 goroutine，无并发上限；
// 现在固定 worker 数消费有界队列，与 memoryWriter 同一范式。
const summaryWorkerCount = 4

// summaryQueueBuffer 摘要任务队列容量。队列满时非阻塞丢弃（与
// memoryWriter 语义一致）——摘要属增强能力，不应阻塞 ReAct 主循环。
const summaryQueueBuffer = 256

// summaryTask 一次异步摘要提取任务（P1-5）。
// parentCtx 在提交时快照（而非在 worker 内读 a.hookCtx），
// 避免运行结束后读到已取消的 context。
type summaryTask struct {
	epID      string
	content   string
	mem       MemoryStore
	parentCtx context.Context
}

// summaryWriter 有界异步摘要写入器（P1-5：替代原来"每条消息一个 goroutine"）。
//
// 以 memoryWriter 为范本：
//   - 固定 summaryWorkerCount 个 worker 消费有界队列；
//   - submit 非阻塞，队列满即丢弃并告警；
//   - flush 关闭队列并用 WaitGroup 等待全部在途任务完成，
//     保证 Run 结束时不会遗弃在途摘要 goroutine。
type summaryWriter struct {
	ch         chan summaryTask
	wg         sync.WaitGroup
	mu         sync.Mutex
	closed     bool
	logger     *slog.Logger
	queueCap   int // 队列容量（默认 summaryQueueBuffer）
	workers    int // worker 数（默认 summaryWorkerCount）
	summarizer memory.SummaryExtractor
	mem        MemoryStore
}

// newSummaryWriter 创建 summaryWriter（不启动 worker，由 refresh 惰性启动）。
// 能力引用存为字段而非闭包捕获：capCache 每次 Run 会重新解析，
// summarizer / mem 可能变化，worker 执行任务时读取当前值。
func newSummaryWriter(summarizer memory.SummaryExtractor, mem MemoryStore, workers int, logger *slog.Logger) *summaryWriter {
	if workers <= 0 {
		workers = summaryWorkerCount
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &summaryWriter{
		logger:     logger,
		queueCap:   summaryQueueBuffer,
		workers:    workers,
		summarizer: summarizer,
		mem:        mem,
	}
}

// refresh 更新能力引用并惰性启动 worker 池（锁内完成）。
// 已在运行中的池只更新引用，不重复启动 worker。
func (w *summaryWriter) refresh(summarizer memory.SummaryExtractor, mem MemoryStore) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.summarizer = summarizer
	w.mem = mem
	if w.ch != nil || w.closed {
		return
	}
	cap := w.queueCap
	if cap <= 0 {
		cap = summaryQueueBuffer
	}
	ch := make(chan summaryTask, cap)
	w.ch = ch
	for i := 0; i < w.workers; i++ {
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			for task := range ch {
				w.runTask(task)
			}
		}()
	}
}

// runTask 执行单个摘要提取任务（含 panic 恢复，不击穿 worker）。
// 能力引用在每个任务执行时读取当前值，避免跨 Run 用到过期引用。
func (w *summaryWriter) runTask(task summaryTask) {
	defer func() {
		if r := recover(); r != nil {
			w.logger.Warn("异步摘要提取 panic", "error", r)
		}
	}()

	w.mu.Lock()
	summarizer := w.summarizer
	mem := w.mem
	w.mu.Unlock()
	if summarizer == nil || mem == nil {
		return
	}

	// 使用父级 context + 超时，防止泄漏
	parent := task.parentCtx
	if parent == nil {
		parent = context.Background()
	}
	sumCtx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()

	result, err := summarizer.ExtractSummary(sumCtx, task.content)
	if err != nil {
		w.logger.Warn("异步摘要提取失败", "id", task.epID, "error", err)
		return
	}
	w.logger.Info("异步摘要提取成功", "id", task.epID, "summary_len", len(result.Summary), "topics", result.Topics)
	// M2 修复：存储摘要结果，不再只记录日志丢弃
	if err := mem.UpdateSummary(sumCtx, task.epID, result.Summary, result.Topics); err != nil {
		w.logger.Warn("异步摘要存储失败", "id", task.epID, "error", err)
	}
}

// submit 非阻塞提交摘要任务到队列。
//
// 非阻塞发送在 mu 保护内完成：flush 会关闭 channel，若发送在锁外进行，
// 存在"读到 ch 后被 flush 关闭、再向已关闭 channel 发送"的竞态（panic）。
// 由于发送是 select+default 的非阻塞操作，持锁不会造成阻塞。
func (w *summaryWriter) submit(task summaryTask) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || w.ch == nil {
		return
	}
	select {
	case w.ch <- task:
	default:
		w.logger.Warn("异步摘要队列已满，丢弃写入", "id", task.epID)
	}
}

// flush 关闭队列并等待所有在途摘要任务完成。
// 关闭后 summaryWriter 不再复用（下次 saveMemory 会创建新实例）。
func (w *summaryWriter) flush() {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.closed = true
	ch := w.ch
	w.ch = nil
	w.mu.Unlock()

	if ch != nil {
		close(ch)
	}
	w.wg.Wait()
}

// saveMemory 将消息保存到 Memory。
// 优化（Task 1）：将 mem.Add() 写入异步化到独立的 goroutine 中，
// 避免 SQLite 同步写入阻塞 ReAct 主循环。
// 优化（perf-v3）：优先使用 capCache 缓存的 memoryStore 和 summarizer，避免每轮重复类型断言。
func (a *ReActAgent) saveMemory(ctx context.Context, msg Message) {
	// 优先使用 capCache 中缓存的能力引用，避免每轮重复类型断言
	var mem MemoryStore
	var summarizer memory.SummaryExtractor
	if a.capCache != nil {
		mem = a.capCache.memoryStore
		summarizer = a.capCache.summarizer
	} else {
		mem = a.getMemoryStore()
		summarizer = a.getSummarizer()
	}
	if mem == nil {
		return
	}
	ep := &memory.Episode{
		ID:        a.idGen.next(),
		SessionID: a.resolveSessionID(msg),
		Role:      string(msg.Role),
		Content:   msg.Content,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	// 非阻塞提交到异步写入队列
	if a.memWriter == nil {
		a.memWriter = newMemoryWriter(mem, a.logger)
	} else {
		// 更新 memWriter 的 mem 引用（可能因 capCache 变化而不同）
		a.memWriter.mem = mem
	}
	a.memWriter.submit(ep)

	// P1-5：异步提取摘要纳入有界 worker 池（替代原来每条消息一个 goroutine）。
	// parentCtx 在提交时快照，避免 worker 内读取运行期共享字段。
	if summarizer != nil && ep.ID != "" {
		parentCtx := a.getHookCtx()
		if parentCtx == nil {
			parentCtx = context.Background()
		}
		a.ensureSummaryWriter(summarizer, mem)
		a.summaryWriter.submit(summaryTask{
			epID:      ep.ID,
			content:   ep.Content,
			mem:       mem,
			parentCtx: parentCtx,
		})
	}
}

// ensureSummaryWriter 惰性创建/复用有界摘要 worker 池（P1-5）。
// 已 flush 关闭的池不再复用，下次 saveMemory 创建新实例。
func (a *ReActAgent) ensureSummaryWriter(summarizer memory.SummaryExtractor, mem MemoryStore) {
	a.summaryMu.Lock()
	defer a.summaryMu.Unlock()
	if a.summaryWriter == nil || a.summaryWriter.isClosed() {
		a.summaryWriter = newSummaryWriter(summarizer, mem, summaryWorkerCount, a.logger)
	}
	// 刷新能力引用（capCache 跨 Run 可能变化）并确保 worker 池已启动
	a.summaryWriter.refresh(summarizer, mem)
}

// flushSummaryWriter 关闭摘要 worker 池并等待所有在途任务完成（P1-5）。
// 应在 agent 运行结束（reactLoopEngine 的 defer）时调用。
func (a *ReActAgent) flushSummaryWriter() {
	a.summaryMu.Lock()
	w := a.summaryWriter
	a.summaryMu.Unlock()
	if w != nil {
		w.flush()
	}
}

// isClosed 报告 summaryWriter 是否已 flush 关闭。
func (w *summaryWriter) isClosed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closed
}

// flushMemoryWriter 关闭异步写入队列并等待所有待写入完成。
// 应在 agent 运行结束（reactLoopEngine 的 defer）时调用。
// 关闭后下次 saveMemory 会重新创建 channel 和 goroutine。
func (a *ReActAgent) flushMemoryWriter() {
	if a.memWriter != nil {
		a.memWriter.flush()
	}
}

// saveCheckpoint 保存 Agent 状态
func (a *ReActAgent) saveCheckpoint(ctx context.Context, history []Message, turnCount int, m Metrics) {
	cs := a.getCheckpointStore()
	if cs == nil {
		return
	}

	// 转换消息格式（保留 tool_calls 和 tool_call_id，确保恢复后 tool 链路完整）
	msgs := make([]persist.CheckpointMessage, len(history))
	for i, m := range history {
		msgs[i] = persist.CheckpointMessage{
			Role:    string(m.Role),
			Content: m.Content,
		}
		if len(m.ToolCalls) > 0 {
			msgs[i].ToolCalls = make([]persist.CheckpointToolCall, len(m.ToolCalls))
			for j, tc := range m.ToolCalls {
				msgs[i].ToolCalls[j] = persist.CheckpointToolCall{
					ID:   tc.ID,
					Name: tc.Name,
					Args: tc.Args,
				}
			}
		}
		if m.Role == RoleTool {
			if id, ok := m.Metadata.Extra["tool_call_id"]; ok {
				msgs[i].ToolCallID = id
			}
		}
	}

	state := &persist.AgentState{
		AgentID:   a.config.Name,
		SessionID: a.config.SessionID,
		Status:    string(a.lifecycle.Status()),
		Messages:  msgs,
		TurnCount: turnCount,
		Metrics: persist.CheckpointMetrics{
			TotalTurns:  m.TotalTurns,
			TotalTools:  m.TotalTools,
			Duration:    m.Duration.String(),
			LLMLatency:  m.LLMLatency.String(),
			ToolLatency: m.ToolLatency.String(),
		},
		SavedAt: time.Now().UTC(),
	}
	// v6.1 state-checkpoint 协议：世界模型快照随检查点落盘（opt-in；
	// tracker 未注入时 no-op，检查点格式向后兼容）
	a.wmSaveWorldState(state)

	if err := cs.Save(ctx, state); err != nil {
		a.logger.Warn("保存检查点失败", "error", err)
	}
}

// defaultMaxHistoryMessages 默认历史消息保留上限（perf-v4 Task 11）
// 默认场景下 history 不再无界增长，同时减少 LLM token 消耗
const defaultMaxHistoryMessages = 100

// trimContext 应用上下文窗口策略裁剪历史
// perf-v4 Task 11：当未配置自定义策略时，默认使用滑动窗口
// （保留系统提示词 + 最近 N 条消息），避免长对话无界增长
// 优化（perf-v3）：优先使用 capCache 缓存的 contextWindow，避免每轮重复类型断言
func (a *ReActAgent) trimContext(history []Message, maxMessages int) []Message {
	var cw ContextWindowStrategy
	if a.capCache != nil {
		cw = a.capCache.contextWindow
	} else {
		cw = a.getContextWindowStrategy()
	}
	if cw != nil {
		return cw.Trim(history, maxMessages)
	}
	// 默认滑动窗口策略
	if maxMessages <= 0 {
		maxMessages = defaultMaxHistoryMessages
	}
	if len(history) <= maxMessages {
		return history
	}
	// 保留第一条系统消息（如果有）+ 最近 N-1 条
	result := make([]Message, 0, maxMessages)
	start := 0
	if len(history) > 0 && history[0].Role == RoleSystem {
		result = append(result, history[0])
		start = 1
	}
	tail := maxMessages - len(result)
	if tail < 0 {
		tail = 0
	}
	nonSystem := history[start:]
	if len(nonSystem) > tail {
		nonSystem = nonSystem[len(nonSystem)-tail:]
	}
	result = append(result, nonSystem...)
	return result
}
