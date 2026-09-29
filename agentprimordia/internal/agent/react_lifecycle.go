// react_lifecycle.go — Agent 生命周期方法
// 包含 Agent 的启停、暂停/恢复、检查点恢复、统计信息等生命周期管理
package agent

import (
	"context"
	"fmt"
	"maps"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/planning"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/persist"
)

// Stats returns current agent statistics
func (a *ReActAgent) Stats() AgentStats {
	// 优化（Task 3.5）：热路径计数器从原子变量读取，无需加锁
	stats := AgentStats{
		CurrentTurn:   int(a.atomicTurn.Load()),
		TotalMessages: int(a.atomicMessages.Load()),
	}

	// 仅对 ToolsCalled map 加锁（低频更新）
	// StartTime 也在锁内读取：reactLoopEngine 入口在 statsMu 保护下写入，
	// 避免与并发 Run 的数据竞争（-race 实测发现）
	a.statsMu.RLock()
	stats.StartTime = a.startTime
	stats.RequestID = a.stats.RequestID
	toolsCopy := make(map[string]int, len(a.stats.ToolsCalled))
	maps.Copy(toolsCopy, a.stats.ToolsCalled)
	// v3.6-1：复制自愈记录（只读切片）
	stats.PlanRecoveries = append([]PlanRecovery(nil), a.stats.PlanRecoveries...)
	// v3.6-2：复制流程修正计数
	stats.ProcessCorrections = a.stats.ProcessCorrections
	// v3.6-3：复制跨任务记忆命中计数
	stats.MemoryHits = a.stats.MemoryHits
	a.statsMu.RUnlock()
	stats.ToolsCalled = toolsCopy

	// 在锁外设置 status，避免嵌套锁定（lifecycle.Status 有自己的锁）
	stats.Status = a.lifecycle.Status()
	return stats
}

// recordUsage 记录 LLM Usage 到 CostTracker 和 Metrics
func (a *ReActAgent) recordUsage(usage llm.Usage) {
	if usage.TotalTokens == 0 {
		return
	}

	if ct := a.getCostTracker(); ct != nil {
		modelName := ""
		if info := a.config.Model.Info(); info.Name != "" {
			modelName = info.Name
		}
		_ = ct.Record(modelName, a.config.SessionID, a.config.Name, usage)
	}

	if m := a.getMetricsRecorder(); m != nil {
		modelName := ""
		if info := a.config.Model.Info(); info.Name != "" {
			modelName = info.Name
		}
		m.RecordTokenUsage(modelName, usage.PromptTokens, usage.CompletionTokens)
	}
}

// Stop 优雅停止 Agent，发送停止信号
func (a *ReActAgent) Stop() {
	a.lifecycle.Stop()
	a.logger.Info("Agent 收到停止信号", "name", a.config.Name)
}

// GracefulShutdown 优雅关闭 Agent
// 请求在当前 turn 完成后停止，而不是立即中断
// 如果 ctx 超时仍未完成，则回退到强制停止
func (a *ReActAgent) GracefulShutdown(ctx context.Context) error {
	a.logger.Info("Agent 请求优雅关闭", "name", a.config.Name)
	return a.lifecycle.GracefulShutdown(ctx)
}

// Name 返回 Agent 名称
func (a *ReActAgent) Name() string {
	return a.config.Name
}

// Pause 暂停 Agent
func (a *ReActAgent) Pause() {
	a.lifecycle.Pause()
	a.logger.Info("Agent 已暂停", "name", a.config.Name)
}

// Resume 恢复暂停的 Agent
func (a *ReActAgent) Resume() {
	a.lifecycle.Resume()
	a.logger.Info("Agent 已恢复", "name", a.config.Name)
}

// ResumeFromCheckpoint 从检查点恢复 Agent 执行
// 加载 CheckpointStore 中的状态，从上次中断的位置继续运行
func (a *ReActAgent) ResumeFromCheckpoint(ctx context.Context) (*Response, error) {
	a.runMu.Lock()
	defer a.runMu.Unlock()

	cs := a.getCheckpointStore()
	if cs == nil {
		return nil, fmt.Errorf("checkpoint store not configured")
	}

	state, err := cs.Load(ctx, a.config.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to load checkpoint: %w", err)
	}

	if state.Status != "paused" && state.Status != "failed" && state.Status != "cancelled" {
		return nil, fmt.Errorf("cannot resume from status %q, expected paused/failed/cancelled", state.Status)
	}

	a.logger.Info("Agent 从检查点恢复", "name", a.config.Name, "turn", state.TurnCount, "saved_at", state.SavedAt)

	return a.resumeFromState(ctx, state)
}

// resumeFromState 从给定状态快照恢复执行（v3.4-6 提取）。
// 检查点恢复（ResumeFromCheckpoint）与失败重放（ReplayFailure）共用。
// 注意：调用方必须持有 a.runMu。
func (a *ReActAgent) resumeFromState(ctx context.Context, state *persist.AgentState) (*Response, error) {
	history := make([]Message, 0, len(state.Messages))
	for _, m := range state.Messages {
		msg := Message{
			Role:    Role(m.Role),
			Content: m.Content,
		}
		if len(m.ToolCalls) > 0 {
			msg.ToolCalls = make([]ToolCall, len(m.ToolCalls))
			for j, tc := range m.ToolCalls {
				msg.ToolCalls[j] = ToolCall{
					ID:   tc.ID,
					Name: tc.Name,
					Args: tc.Args,
				}
			}
		}
		if m.ToolCallID != "" {
			msg.Metadata.Extra = map[string]string{"tool_call_id": m.ToolCallID}
		}
		history = append(history, msg)
	}

	// 注入请求 ID
	reqID := RequestIDFromCtx(ctx)
	if reqID == "" {
		reqID = NewRequestID()
		ctx = WithRequestID(ctx, reqID)
	}
	a.mu.Lock()
	a.currentRequestID = reqID
	a.mu.Unlock()

	// 优化（Task 3.5）：热路径计数器使用原子操作
	a.atomicTurn.Store(int64(state.TurnCount))
	a.atomicMessages.Store(int64(len(history)))

	// P1-2（评估报告 §4.2）：startTime 恢复必须在 statsMu 保护下写入。
	// 修复前此处无锁写 a.startTime，与 Stats() 持 RLock 读同一字段构成
	// 数据竞争（-race 实测）。恢复时减去已运行的时长，保持 Duration 累计一致。
	now := time.Now()
	if prevDur, err := time.ParseDuration(state.Metrics.Duration); err == nil && prevDur > 0 {
		now = now.Add(-prevDur)
	}
	a.statsMu.Lock()
	a.startTime = now
	a.stats.StartTime = now
	a.stats.RequestID = reqID
	a.statsMu.Unlock()
	_ = a.lifecycle.SetStatus(StatusRunning)

	if m := a.getMetricsRecorder(); m != nil {
		m.IncActiveAgents()
		defer m.DecActiveAgents()
	}

	defer func() {
		if a.lifecycle.Status() != StatusCompleted &&
			a.lifecycle.Status() != StatusFailed &&
			a.lifecycle.Status() != StatusCancelled {
			_ = a.lifecycle.SetStatus(StatusCompleted)
		}
	}()

	_ = a.fireHook(HookBeforeRun, &HookContext{})
	a.publishEvent(EventAgentResume, map[string]string{"name": a.config.Name, "from_turn": fmt.Sprintf("%d", state.TurnCount)})

	prevMetrics := state.Metrics
	var totalLLMLatency time.Duration
	var totalToolLatency time.Duration
	if d, parseErr := time.ParseDuration(prevMetrics.LLMLatency); parseErr == nil {
		totalLLMLatency = d
	}
	if d, parseErr := time.ParseDuration(prevMetrics.ToolLatency); parseErr == nil {
		totalToolLatency = d
	}
	toolCount := prevMetrics.TotalTools

	// 恢复路径绕过了 reactLoopEngine 入口，必须在此处填充 capCache，
	// 否则 runLoop 内对 a.capCache.model 等字段的无保护访问会 panic。
	// 注意：runLoop 不会像 reactLoopEngine 那样在 defer 中清理 capCache，
	// 因此这里显式清理，避免后续 Run() 误用本次恢复的旧引用。
	a.capCache = a.resolveCapabilities(reqID)
	defer func() { a.capCache = nil }()
	// P1-5：恢复路径绕过了 reactLoopEngine，必须在此处 flush 有界摘要
	// worker 池，否则 runLoop 内 saveMemory 派生的 worker goroutine
	// 不会被等待，随恢复调用次数线性泄漏。
	defer a.flushSummaryWriter()
	// 同理 flush 异步记忆写入队列（memoryWriter 的 goroutine 同样只在
	// reactLoopEngine 的 defer 中被等待；恢复路径不 flush 会泄漏）。
	defer a.flushMemoryWriter()

	// v3.4-1：若 checkpoint 含 plan 进度，从计划断点恢复——重建 plan 与进度，
	// 跳过已完成子任务、沿用其结果，仅执行剩余子任务。
	if state.Plan != nil {
		// v6.1 state-checkpoint 协议：恢复路径「续知而非重放」——历史消息
		// 不会重放给 tracker，世界状态由快照一次性载入（opt-in；无快照时
		// 恢复语义与 v6.0 完全一致）
		a.wmRestoreWorldState(state)
		pp := buildPlanProgressFromState(state.Plan)
		plan := &planning.Plan{SubTasks: pp.subtasks}
		return a.executePlanWithState(ctx, history, plan, loopConfig{requestID: reqID}, a.startTime, totalLLMLatency, totalToolLatency, toolCount, pp)
	}

	// v6.1 state-checkpoint 协议：同上，普通 ReAct 路径的世界状态恢复
	a.wmRestoreWorldState(state)

	return a.runLoop(ctx, history, state.TurnCount, loopConfig{requestID: reqID}, totalLLMLatency, totalToolLatency, toolCount)
}
