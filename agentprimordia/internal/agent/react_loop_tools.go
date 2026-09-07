// react_loop_tools.go — tool执行与 HITL 处理
// 包含 executeToolCalls 和 handleHITL 函数
package agent

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"agentprimordia/internal/agent/tool_learning"
)

// executeToolCalls 执行一组tool调用，处理 HITL 确认、追踪、Hook 等。
// 返回更新后的 history、累计tool延迟和tool调用计数。
//
// R1.6：当 config.ParallelToolExecution 为 true 且tool数 > 1 时，并行执行
// tool调用本身（executeTool），但 hooks/事件/history 仍按原始顺序串行处理，
// 保证下游消费者（事件订阅者、memory、stream）的消息顺序与原实现一致。
func (a *ReActAgent) executeToolCalls(ctx context.Context, history []Message, toolCalls []ToolCall, turn int, cfg loopConfig, tracer Tracer, turnSpan Span, totalToolLatency time.Duration, toolCount int) ([]Message, time.Duration, int) {
	// 串行模式：保持向后兼容（默认）
	if !a.config.ParallelToolExecution || len(toolCalls) <= 1 {
		return a.executeToolCallsSerial(ctx, history, toolCalls, turn, cfg, tracer, turnSpan, totalToolLatency, toolCount)
	}
	// 并行模式：先并行执行tool，再串行处理结果
	return a.executeToolCallsParallel(ctx, history, toolCalls, turn, cfg, tracer, turnSpan, totalToolLatency, toolCount)
}

// executeToolCallsSerial 串行执行tool（原始实现，保持 100% 行为兼容）
func (a *ReActAgent) executeToolCallsSerial(ctx context.Context, history []Message, toolCalls []ToolCall, turn int, cfg loopConfig, tracer Tracer, turnSpan Span, totalToolLatency time.Duration, toolCount int) ([]Message, time.Duration, int) {
	for _, tc := range toolCalls {
		result, err, latency := a.executeSingleTool(ctx, &tc, turn, cfg, tracer, turnSpan)
		totalToolLatency += latency
		toolCount = a.processToolResult(ctx, &tc, result, err, latency, turn, cfg, toolCount)
		history = append(history, result.ToMessage())
		a.saveMemory(ctx, result.ToMessage())
	}
	return history, totalToolLatency, toolCount
}

// executeToolCallsParallel 并行执行tool（Phase 1 G1-4）
// 仅并行 executeTool() 本身（最重 I/O），其它副作用（hooks/events/history/memory）仍串行。
//
// 实现说明：
//   - 使用 sync.WaitGroup 等待所有 goroutine 完成
//   - 通过 per-index 切片收集结果，保证原始顺序
//   - MaxParallelTools 控制并发上限；0 表示无限制
//   - 整体 ctx 取消时，未完成的 goroutine 通过 ctx 传播到 executeTool 内部
func (a *ReActAgent) executeToolCallsParallel(ctx context.Context, history []Message, toolCalls []ToolCall, turn int, cfg loopConfig, tracer Tracer, turnSpan Span, totalToolLatency time.Duration, toolCount int) ([]Message, time.Duration, int) {
	n := len(toolCalls)
	type execResult struct {
		result  *ToolResult
		err     error
		latency time.Duration
		tc      ToolCall // 拷贝避免闭包问题
	}
	results := make([]execResult, n)
	var wg sync.WaitGroup

	maxParallel := a.config.MaxParallelTools
	if maxParallel <= 0 || maxParallel > n {
		maxParallel = n
	}

	// 简单的信号量：限制同时在飞的 goroutine 数量
	sem := make(chan struct{}, maxParallel)

	for i := range toolCalls {
		i := i // 显式捕获
		tc := toolCalls[i]
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			// v3.4-4：单个 tool goroutine panic 不击穿整个循环，转为错误结果
			defer func() {
				if r := recover(); r != nil {
					a.logger.Error("并行 tool 执行 panic 已恢复", "tool", tc.Name, "panic", r)
					results[i] = execResult{
						result: &ToolResult{
							ToolCallID: tc.ID,
							Content:    fmt.Sprintf("tool %s panic recovered: %v", tc.Name, r),
							IsError:    true,
						},
						err:     fmt.Errorf("tool %s panic: %v", tc.Name, r),
						latency: 0,
						tc:      tc,
					}
				}
			}()
			result, err, latency := a.executeSingleTool(ctx, &tc, turn, cfg, tracer, turnSpan)
			results[i] = execResult{result: result, err: err, latency: latency, tc: tc}
		}()
	}
	wg.Wait()

	// 串行处理结果（保证 hooks/memory/history 顺序与原实现一致）
	for i := range toolCalls {
		r := results[i]
		totalToolLatency += r.latency
		toolCount = a.processToolResult(ctx, &toolCalls[i], r.result, r.err, r.latency, turn, cfg, toolCount)
		history = append(history, r.result.ToMessage())
		a.saveMemory(ctx, r.result.ToMessage())
	}
	return history, totalToolLatency, toolCount
}

// executeSingleTool 执行单个tool（含 HITL 处理）。返回 result、err、本次 latency。
// 提取自原 executeToolCalls 循环体，便于串行/并行复用。
// R1.5：在 HITL 通过后注入 ToolLearning 参数建议（confidence > threshold 时替换 args）。
func (a *ReActAgent) executeSingleTool(ctx context.Context, tc *ToolCall, turn int, cfg loopConfig, tracer Tracer, turnSpan Span) (*ToolResult, error, time.Duration) {
	a.emitStream(cfg, StreamEvent{Type: StreamEventToolCall, Content: tc.Name, Data: tc})
	_ = a.fireHook(HookBeforeTool, &HookContext{ToolCall: tc, Turn: turn})
	if a.hasEventSubscriber() {
		a.publishEvent(EventToolCall, map[string]string{"tool": tc.Name, "turn": strconv.Itoa(turn)})
	}

	// HITL 处理
	modifiedTC, skip := a.handleHITL(ctx, tc, turn, cfg)
	if skip {
		result := &ToolResult{ToolCallID: modifiedTC.ID, Content: "人类拒绝执行此操作", IsError: true}
		return result, nil, 0
	}

	// R1.5 G1-3：ToolLearning 参数建议（高置信度时替换 args）
	// 在 HITL 之后注入：HITL 的人类显式选择优先于学习建议
	if learner := a.getToolLearnerOrNil(); learner != nil {
		threshold := a.config.ToolLearningConfidenceThreshold
		if threshold <= 0 {
			threshold = 0.7
		}
		suggestion, suggErr := learner.SuggestImprovement(ctx, modifiedTC.Name, modifiedTC.Args)
		if suggErr == nil && suggestion != nil && suggestion.Confidence > threshold &&
			suggestion.ImprovedArgs != "" && suggestion.ImprovedArgs != modifiedTC.Args {
			a.logger.Debug("ToolLearning 建议优化参数",
				"tool", modifiedTC.Name,
				"confidence", suggestion.Confidence,
				"reason", suggestion.Reason,
			)
			modifiedTC.Args = suggestion.ImprovedArgs
		}

		// v3.6-2：流程修正——命中高频失败模式时自动规避（失败模式被自动规避）
		correction, corrErr := learner.SuggestProcessCorrection(ctx, modifiedTC.Name, modifiedTC.Args)
		if corrErr == nil && correction != nil && correction.Avoid && correction.Confidence > threshold {
			a.logger.Warn("ToolLearning 流程修正：规避已知失败调用",
				"tool", modifiedTC.Name,
				"confidence", correction.Confidence,
				"reason", correction.Reason,
			)
			a.emitStream(cfg, StreamEvent{Type: StreamEventThought, Content: fmt.Sprintf("[ToolLearning 流程修正] 规避 %s：%s", modifiedTC.Name, correction.Reason)})
			a.statsMu.Lock()
			a.stats.ProcessCorrections++
			a.statsMu.Unlock()
			if correction.AlternativeArgs != "" && correction.AlternativeArgs != modifiedTC.Args {
				a.logger.Info("ToolLearning 采用替代参数规避失败", "tool", modifiedTC.Name)
				modifiedTC.Args = correction.AlternativeArgs
			} else {
				// 无替代参数：跳过该调用（不执行已知失败调用）
				result := &ToolResult{
					ToolCallID: modifiedTC.ID,
					Content:    fmt.Sprintf("[ToolLearning 流程修正] 已规避已知失败调用 %s：%s", modifiedTC.Name, correction.Reason),
					IsError:    false,
				}
				return result, nil, 0
			}
		}
	}

	toolStart := time.Now()
	var toolSpan Span = &NoopSpan{}
	if tracer != nil {
		toolSpan = tracer.Start(
			"tool."+modifiedTC.Name,
			SpanKindClient,
			WithParent(turnSpan.SpanContext()),
			WithAttributes(map[string]any{"tool": modifiedTC.Name, "agent": a.config.Name}),
		)
	}
	result, err := a.executeTool(ctx, modifiedTC)
	latency := time.Since(toolStart)
	toolSpan.SetAttribute("latency_ms", latency.Milliseconds())
	if err != nil {
		toolSpan.SetStatus(SpanStatusError, err.Error())
	}
	toolSpan.End()
	a.recordTool(latency, err, modifiedTC.Name)
	return result, err, latency
}

// processToolResult 处理单个tool的结果（hooks/事件/统计）。返回更新后的 toolCount。
// R1.5：在此阶段根据执行结果记录 ToolLearning 经验。
func (a *ReActAgent) processToolResult(ctx context.Context, tc *ToolCall, result *ToolResult, err error, latency time.Duration, turn int, cfg loopConfig, toolCount int) int {
	if err != nil {
		a.emitStream(cfg, StreamEvent{Type: StreamEventError, Content: fmt.Sprintf("tool %s error: %v", tc.Name, err)})
		_ = a.fireHook(HookOnError, &HookContext{Error: err, Turn: turn})
		if a.hasEventSubscriber() {
			a.publishEvent(EventAgentError, map[string]string{"tool": tc.Name, "error": err.Error()})
		}
	} else {
		a.emitStream(cfg, StreamEvent{Type: StreamEventToolResult, Content: result.Content, Data: result})
	}

	// 构造完整的 HookContext，携带工具调用全量数据供下游 Hook 消费者使用
	afterToolCtx := &HookContext{
		ToolResult: result,
		ToolCall:   tc,
		Turn:       turn,
		Duration:   latency,
		Error:      err,
	}
	if cfg.stream {
		if err == nil {
			_ = a.fireHook(HookAfterTool, afterToolCtx)
		}
	} else {
		_ = a.fireHook(HookAfterTool, afterToolCtx)
		if a.hasEventSubscriber() {
			a.publishEvent(EventToolResult, map[string]string{"tool": tc.Name})
		}
	}

	toolCount++
	a.statsMu.Lock()
	a.stats.ToolsCalled[tc.Name]++
	a.statsMu.Unlock()

	// v6.1 接线点③：工具调用/观察落图（因果链 tool_call → observation）
	a.wmObserveToolResult(turn, tc, result, err)

	// R1.5 G1-3：ToolLearning 经验记录（fire-and-forget，失败仅记录日志）
	if learner := a.getToolLearnerOrNil(); learner != nil {
		if err != nil || (result != nil && result.IsError) {
			errMsg := ""
			if err != nil {
				errMsg = err.Error()
			} else if result != nil {
				errMsg = result.Content
			}
			if recErr := learner.RecordFailure(ctx, tc.Name, tc.Args, errMsg); recErr != nil {
				a.logger.Warn("ToolLearning RecordFailure 失败", "tool", tc.Name, "err", recErr)
			}
		} else {
			if recErr := learner.RecordSuccess(ctx, tc.Name, tc.Args, result.Content); recErr != nil {
				a.logger.Warn("ToolLearning RecordSuccess 失败", "tool", tc.Name, "err", recErr)
			}
		}
	}
	return toolCount
}

// getToolLearnerOrNil 通过 capCache 获取 toolLearner（nil-safe）
func (a *ReActAgent) getToolLearnerOrNil() tool_learning.ToolLearner {
	if a.capCache == nil {
		return a.getToolLearner()
	}
	return a.capCache.toolLearner
}

// handleHITL 处理 Human-in-the-Loop 确认流程。
// 返回可能被修改的 ToolCall 和是否跳过执行的标志。
func (a *ReActAgent) handleHITL(ctx context.Context, tc *ToolCall, turn int, cfg loopConfig) (_ ToolCall, skip bool) {
	if a.hitlMgr == nil || !a.hitlMgr.ShouldInterrupt(tc.Name, InterruptToolConfirm) {
		return *tc, false
	}

	a.emitStream(cfg, StreamEvent{Type: StreamEventThought, Content: fmt.Sprintf("[HITL] tool %s 需要人类确认", tc.Name)})
	_ = a.lifecycle.WaitForInput(fmt.Sprintf("tool %s 需确认", tc.Name))

	humanResp, hitlErr := a.hitlMgr.RequestInterrupt(ctx, &InterruptRequest{
		Reason:  InterruptToolConfirm,
		Message: fmt.Sprintf("Agent 请求执行tool %s，参数: %s", tc.Name, tc.Args),
		Data:    map[string]any{"tool": tc.Name, "args": tc.Args},
		Turn:    turn,
	})

	_ = a.lifecycle.SetStatus(StatusRunning)

	if hitlErr != nil {
		a.emitStream(cfg, StreamEvent{Type: StreamEventError, Content: fmt.Sprintf("[HITL] 等待人类确认失败: %v", hitlErr)})
		*tc = ToolCall{ID: tc.ID, Name: tc.Name, Args: tc.Args}
		return *tc, true
	}

	if !humanResp.Approved {
		a.emitStream(cfg, StreamEvent{Type: StreamEventThought, Content: fmt.Sprintf("[HITL] 人类拒绝执行tool %s", tc.Name)})
		return *tc, true
	}

	if humanResp.Modified != nil {
		if modifiedArgs, ok := humanResp.Modified["args"].(string); ok {
			tc.Args = modifiedArgs
		}
	}

	a.emitStream(cfg, StreamEvent{Type: StreamEventThought, Content: fmt.Sprintf("[HITL] 人类已确认执行tool %s", tc.Name)})
	return *tc, false
}
