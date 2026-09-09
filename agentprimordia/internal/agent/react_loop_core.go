// react_loop_core.go — ReAct 循环核心体
// 包含 runLoop 主循环逻辑，被 reactLoopEngine 和 ResumeFromCheckpoint 共享
package agent

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"agentprimordia/internal/agent/learning"
	"agentprimordia/internal/llm"
	"agentprimordia/internal/observability"
	"agentprimordia/internal/tools"
)

// p2t4：审计动作常量（字符串字面量，与 internal/audit 标准动作保持一致）
// 避免 agent 包直接 import audit 包造成的循环依赖
const (
	auditActionAgentStart        = "agent.start"
	auditActionAgentStop         = "agent.stop"
	auditActionLLMCall           = "llm.call"
	auditActionGuardrailBlock    = "guardrail.block"
	auditActionGuardrailSanitize = "guardrail.sanitize"
	auditResultSuccess           = "success"
	auditResultBlocked           = "blocked"
)

// runLoop ReAct 循环核心体，被 reactLoopEngine 和 ResumeFromCheckpoint 共享
// 封装从 startTurn 开始的主循环逻辑，包括 LLM 调用、tool执行、checkpoint 保存等
// rootSpanCtx 为根 Span 上下文（可为零值），用于将 turn span 链接到根 span
func (a *ReActAgent) runLoop(ctx context.Context, history []Message, startTurn int, cfg loopConfig, totalLLMLatency time.Duration, totalToolLatency time.Duration, toolCount int, rootSpanCtx ...SpanContext) (*Response, error) {
	// 优化（Task 2）：从 capCache 一次性取所有能力引用；capCache 由 reactLoopEngine 在 Run 入口处填充
	tracer := Tracer(nil)
	costTracker := (*CostTracker)(nil)
	if a.capCache != nil {
		tracer = a.capCache.tracer
		costTracker = a.capCache.costTracker
	}

	// R1.3 G1-1：Planning 接入（仅在第一轮且 planner 已配置时尝试）
	// 用户输入可分解为多子任务时，走 DAG 执行；否则降级为正常 runLoop
	if startTurn == 0 {
		if !cfg.skipPlan { // v3.6-1：自愈降级路径跳过 plan 分支
			if planner := a.getPlannerOrNil(); planner != nil {
				userInput := extractUserInput(history)
				if userInput != "" {
					plan, planErr := planner.GeneratePlan(ctx, userInput)
					if planErr != nil {
						a.logger.Warn("Planning 失败，降级到正常 runLoop", "error", planErr)
					} else if plan != nil && len(plan.SubTasks) > 1 {
						a.logger.Info("使用 Plan 执行",
							"subtasks", len(plan.SubTasks),
							"goal", plan.Goal,
						)
						// v6.1 接线点②（planner 粗粒度计划）：任务/子任务落图 + 组建期预演门
						a.wmObservePlan(ctx, 0, userInput, plan)
						// v3.6-1：失败自动换路径（replan/降级），故障恢复不依赖人工
						return a.executePlanWithSelfHealing(ctx, history, plan, cfg)
					}
				}
			}
		}
	}

	// 优化（Task 3.5）：仅在需要记录 turn 延迟时获取时间戳
	// v3.5-4：启用全链路关联时也需计时（RecordTurn 关联到 trace）
	needTiming := a.getMetricsRecorder() != nil || a.getLabeledRecorder() != nil ||
		(a.capCache != nil && a.capCache.observability != nil)

	// v7.3-P2fix：子任务轮次预算覆盖全局 MaxTurns，防止 plan 场景下单子任务耗尽配额
	maxTurns := a.config.MaxTurns
	if cfg.subtaskMaxTurns > 0 && cfg.subtaskMaxTurns < maxTurns {
		maxTurns = cfg.subtaskMaxTurns
	}

	for turn := startTurn; turn < maxTurns; turn++ {
		var turnStart time.Time
		if needTiming {
			turnStart = time.Now()
		}

		if a.lifecycle.IsStopped() {
			_ = a.lifecycle.SetStatus(StatusCancelled)
			a.emitStream(cfg, StreamEvent{Type: StreamEventError, Content: ErrAgentStopped.Error()})
			return &Response{RequestID: cfg.requestID, Error: ErrAgentStopped}, ErrAgentStopped
		}

		if ctx.Err() != nil {
			_ = a.lifecycle.SetStatus(StatusCancelled)
			a.emitStream(cfg, StreamEvent{Type: StreamEventError, Content: ctx.Err().Error()})
			return &Response{RequestID: cfg.requestID, Error: ctx.Err()}, ctx.Err()
		}

		// 优化（Task 3.5）：使用原子操作替代 mutex，消除热路径上的锁竞争
		a.atomicTurn.Store(int64(turn + 1))
		a.atomicMessages.Store(int64(len(history)))

		_ = a.fireHookWithPool(HookBeforeTurn, turn)
		// 优化（Task 3）：仅在存在订阅者时构造 payload map，避免热点路径上的堆分配
		if a.hasEventSubscriber() {
			a.publishEvent(EventTurnStart, map[string]int{"turn": turn})
		}

		var turnSpan Span = &NoopSpan{}
		if tracer != nil {
			opts := []SpanOption{
				WithAttributes(map[string]any{"agent": a.config.Name, "turn": turn}),
			}
			// 如果有根 Span 上下文，将 turn span 链接到根 span
			if len(rootSpanCtx) > 0 && rootSpanCtx[0].IsValid() {
				opts = append(opts, WithParent(rootSpanCtx[0]))
			}
			turnSpan = tracer.Start(
				"turn."+strconv.Itoa(turn),
				SpanKindInternal,
				opts...,
			)
		}

		// 成本检查（v4.1 拆分：checkBudgetExceeded）
		if resp, cerr := a.checkBudgetExceeded(cfg, costTracker); cerr != nil {
			return resp, cerr
		}

		// 记忆注入 + 已解任务 fast-path（v4.1 拆分：injectMemoryContextAndFastPath）
		var fastResp *Response
		var fastHit bool
		history, fastResp, fastHit = a.injectMemoryContextAndFastPath(ctx, history, turn, startTurn, cfg)
		if fastHit {
			return fastResp, nil
		}

		// RAG 检索与注入（v4.1 拆分：ragRetrieveAndInject）
		history = a.ragRetrieveAndInject(ctx, history, turn, startTurn, cfg, tracer, turnSpan)

		trimmedHistory := a.trimContext(history, 0)
		// v6.1 接线点④：被裁消息转世界事实节点（提案 E6 截断债务的结构化偿还）
		a.wmNotifyTrimmed(history, trimmedHistory, turn)
		llmMessages := convertToLLMMessages(trimmedHistory)

		// 优化（Task 2 / Task 2.5 / perf-v2）：使用 capCache.toolkit 和预转换的 toolDefinitions
		var toolDefinitions []llm.ToolDefinition
		if a.capCache != nil && a.capCache.toolDefinitions != nil {
			toolDefinitions = a.capCache.toolDefinitions
		} else {
			var toolDefs []map[string]any
			var toolkit *tools.Registry
			if a.capCache != nil {
				toolkit = a.capCache.toolkit
			} else {
				toolkit = a.getToolkit()
			}
			if toolkit != nil {
				toolDefs = toolkit.Definitions()
			}
			toolDefinitions = convertToolDefsToLLMDefinitions(toolDefs)
		}

		llmStart := time.Now()
		if a.hasEventSubscriber() {
			a.publishEvent(EventLLMCall, map[string]int{"turn": turn})
		}

		var llmSpan Span = &NoopSpan{}
		if tracer != nil {
			llmSpan = tracer.Start(
				"llm.call",
				SpanKindClient,
				WithParent(turnSpan.SpanContext()),
				WithAttributes(map[string]any{"agent": a.config.Name, "turn": turn}),
			)
		}

		var thought Thought

		if cfg.stream {
			var sErr error
			thought, sErr = a.streamReasoning(ctx, cfg, llmMessages, toolDefinitions, llmStart)
			if thought.Content == "" && len(thought.ToolCalls) == 0 {
				err := fmt.Errorf("stream reasoning failed: %w", sErr)
				return &Response{RequestID: cfg.requestID, Error: err}, err
			}
		} else {
			var err error
			thought, err = a.syncReasoning(ctx, llmMessages, toolDefinitions, llmStart)
			if err != nil {
				a.handleOnError(ctx, err)
				_ = a.lifecycle.SetStatus(StatusFailed)
				return &Response{RequestID: cfg.requestID, Error: err}, err
			}
		}

		llmLatency := time.Since(llmStart)
		totalLLMLatency += llmLatency

		llmSpan.SetAttribute("latency_ms", llmLatency.Milliseconds())
		llmSpan.End()

		a.recordUsage(thought.Usage)

		// p2t4：写入 LLMCall 审计事件
		a.writeAudit(ctx, AuditEvent{
			Actor:    a.config.Name,
			Action:   auditActionLLMCall,
			Resource: a.capCache.model,
			Result:   auditResultSuccess,
			Details: map[string]any{
				"turn":              turn,
				"latency_ms":        llmLatency.Milliseconds(),
				"prompt_tokens":     thought.Usage.PromptTokens,
				"completion_tokens": thought.Usage.CompletionTokens,
			},
		})

		// 输出端护栏（v4.1 拆分：guardrailSanitizeOutput；PII 脱敏、注入拦截）
		if resp, gerr := a.guardrailSanitizeOutput(ctx, cfg, &thought, turn); gerr != nil {
			return resp, gerr
		}

		assistantMsg := Message{
			Role:      RoleAssistant,
			Content:   thought.Content,
			ToolCalls: thought.ToolCalls,
		}
		a.saveMemory(ctx, assistantMsg)

		_ = a.fireHookWithPool(HookAfterLLM, turn)
		if a.hasEventSubscriber() {
			a.publishEvent(EventLLMResponse, map[string]int{"turn": turn})
		}

		// 无tool调用 → Agent 完成
		if len(thought.ToolCalls) == 0 {
			// R1.4 G1-2：Reflection 接入完成路径
			// 对最终输出进行反思，必要时用 reflector 改进版本替换
			finalContent := thought.Content
			if improved, reflectErr := a.reflectAndImprove(ctx, finalContent); reflectErr == nil && improved != "" {
				finalContent = improved
			}
			duration := time.Since(a.startTime)
			response := &Response{
				RequestID: cfg.requestID,
				Content:   finalContent,
				Metrics: Metrics{
					TotalTurns:  turn + 1,
					TotalTools:  toolCount,
					Duration:    duration,
					LLMLatency:  totalLLMLatency,
					ToolLatency: totalToolLatency,
				},
			}
			_ = a.lifecycle.SetStatus(StatusCompleted)
			a.saveCheckpoint(ctx, history, turn+1, response.Metrics)
			_ = a.fireHookWithPoolResp(HookOnComplete, response)
			turnSpan.End()
			_ = a.fireHookWithPool(HookAfterTurn, turn)
			if needTiming {
				a.recordTurn(time.Since(turnStart))
			}
			if a.hasEventSubscriber() {
				a.publishEvent(EventTurnEnd, map[string]int{"turn": turn})
			}
			a.emitStream(cfg, StreamEvent{Type: StreamEventComplete, Content: thought.Content, Data: response})
			if cfg.stream {
				a.logger.Info("Agent 流式完成", "name", a.config.Name, "turns", turn+1, "duration", duration)
			} else {
				a.logger.Info("Agent 完成", "name", a.config.Name, "turns", turn+1, "duration", duration)
			}
			// p2t4：写入 AgentStop 审计事件
			a.writeAudit(ctx, AuditEvent{
				Actor:    a.config.Name,
				Action:   auditActionAgentStop,
				Resource: cfg.requestID,
				Result:   auditResultSuccess,
				Details:  map[string]any{"turns": turn + 1, "duration_ms": duration.Milliseconds()},
			})
			// v3.0：自适应学习——从本次交互中蒸馏知识
			a.distillKnowledge(ctx, history, finalContent)
			// v3.6-3：完成任务后把答案存为"已解决"记忆，供相似任务复用
			a.saveSolutionMemory(ctx, history, finalContent)
			return response, nil
		}

		history = append(history, assistantMsg)
		// v6.1 接线点②：本轮工具调用 = 计划（重新）形成（预演态）；思考文本 = 假设
		a.wmObserveAssistant(turn, thought)

		// v6.1 接线点⑤：工具执行前预演门（观察模式——缺陷写失败库+审计，不拦截）
		a.wmRehearseGate(ctx, turn)

		// 执行所有tool调用
		history, totalToolLatency, toolCount = a.executeToolCalls(ctx, history, thought.ToolCalls, turn, cfg, tracer, turnSpan, totalToolLatency, toolCount)

		// v6.1 接线点⑥：行动后回溯校验（计划路径 vs 实际轨迹，偏离写失败库+审计）
		a.wmBackDiffCheck(ctx, turn)

		turnSpan.End()
		_ = a.fireHookWithPool(HookAfterTurn, turn)
		if needTiming {
			a.recordTurn(time.Since(turnStart))
		}
		if a.hasEventSubscriber() {
			a.publishEvent(EventTurnEnd, map[string]int{"turn": turn})
		}

		if a.lifecycle.IsGracefulShutdown() {
			a.logger.Info("Agent 优雅关闭：当前 turn 已完成，退出循环", "name", a.config.Name, "turn", turn+1)
			_ = a.lifecycle.SetStatusWithReason(StatusCancelled, "graceful shutdown")
			duration := time.Since(a.startTime)
			response := &Response{
				RequestID: cfg.requestID,
				Content:   thought.Content,
				Error:     ErrAgentStopped,
				Metrics: Metrics{
					TotalTurns:  turn + 1,
					TotalTools:  toolCount,
					Duration:    duration,
					LLMLatency:  totalLLMLatency,
					ToolLatency: totalToolLatency,
				},
			}
			a.emitStream(cfg, StreamEvent{Type: StreamEventError, Content: "graceful shutdown: agent stopped after turn completion"})
			return response, ErrAgentStopped
		}

		a.saveCheckpoint(ctx, history, turn+1, Metrics{
			TotalTurns:  turn + 1,
			TotalTools:  toolCount,
			Duration:    time.Since(a.startTime),
			LLMLatency:  totalLLMLatency,
			ToolLatency: totalToolLatency,
		})
	}

	_ = a.lifecycle.SetStatus(StatusFailed)
	a.emitStream(cfg, StreamEvent{Type: StreamEventError, Content: ErrMaxTurnsExceeded.Error()})
	_ = a.fireHookWithPoolErr(HookOnError, ErrMaxTurnsExceeded)
	a.logger.Warn("Agent 超出最大轮次", "name", a.config.Name, "max_turns", a.config.MaxTurns)

	response := &Response{
		RequestID: cfg.requestID,
		Error:     ErrMaxTurnsExceeded,
		Metrics: Metrics{
			TotalTurns:  a.config.MaxTurns,
			TotalTools:  toolCount,
			Duration:    time.Since(a.startTime),
			LLMLatency:  totalLLMLatency,
			ToolLatency: totalToolLatency,
		},
	}

	return response, ErrMaxTurnsExceeded
}

// writeAudit 写入审计事件（如果 auditLogger 已配置）。
// p2t4：审计日志集成到 ReAct Loop 关键路径。
// 该方法是 fire-and-forget 模式，错误仅记录日志，不影响主流程。
func (a *ReActAgent) writeAudit(ctx context.Context, event AuditEvent) {
	if a.capCache == nil || a.capCache.auditLogger == nil {
		return
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	// v3.5-4：将当前请求的 trace_id 注入审计事件，作为全链路回溯关联键
	event.TraceID = a.capCache.traceID
	if err := a.capCache.auditLogger.Log(ctx, event); err != nil {
		a.logger.Warn("写入审计事件失败", "action", event.Action, "err", err)
	}
	// v3.5-4：同步写入全链路关联存储（trace → 审计 闭环）
	a.recordAuditObservability(event)
}

// recordAuditObservability 将审计事件关联到当前请求 trace（全链路闭环）。
func (a *ReActAgent) recordAuditObservability(event AuditEvent) {
	if a.capCache == nil || a.capCache.observability == nil || a.capCache.traceID == "" {
		return
	}
	a.capCache.observability.AddAuditEvent(a.capCache.traceID, observability.AuditEvent{
		Timestamp: event.Timestamp,
		Actor:     event.Actor,
		Action:    event.Action,
		Resource:  event.Resource,
		Result:    event.Result,
		Details:   event.Details,
	})
}

// distillKnowledge 从本次交互中蒸馏知识（v3.0 自适应学习）。
//
// 在 Agent 完成推理后调用，将用户输入和 Agent 输出封装为 Interaction，
// 交给 KnowledgeDistiller 提取事实/模式/偏好类知识。
//
// v6.x 修复（评估报告 Issue #10）：
//   - 旧实现同步调用，会阻塞 Agent 最终响应回包；且读 a.capCache.requestID，
//     而 capCache 在 reactLoopEngine 的 defer 中会被置 nil，存在竞态。
//   - 新实现：所有需要的字段（agent_name、session_id、requestID、distiller
//     引用）**进入函数前**先拷贝到局部变量，再以 fire-and-forget goroutine
//     执行；用独立的 background context（5 分钟超时）避免父 ctx 取消中断
//     蒸馏。
//
// 错误仅记日志，不影响主流程（fire-and-forget 语义）。
func (a *ReActAgent) distillKnowledge(ctx context.Context, history []Message, agentOutput string) {
	if a.capCache == nil || a.capCache.distiller == nil {
		return
	}

	// 从历史中提取用户输入（最后一条 user 消息）
	var userInput string
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == RoleUser {
			userInput = history[i].Content
			break
		}
	}
	if userInput == "" {
		return
	}

	// v6.x：在 goroutine 启动前一次性拷贝所有 capCache 字段，规避 defer
	// 将 capCache 置 nil 的竞态。
	distiller := a.capCache.distiller
	agentName := a.config.Name
	sessionID := a.config.SessionID
	requestID := a.capCache.requestID
	distillLogger := a.logger
	interaction := learning.Interaction{
		ID:          agentName + "_" + requestID,
		UserInput:   userInput,
		AgentOutput: agentOutput,
		Success:     true,
		Timestamp:   time.Now(),
		Metadata: map[string]string{
			"agent_name": agentName,
			"session_id": sessionID,
		},
	}

	// v6.x：fire-and-forget goroutine，背景 ctx 与原 ctx 解耦
	bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	go func() {
		defer cancel()
		items, err := distiller.Distill(bgCtx, interaction)
		if err != nil {
			distillLogger.Warn("知识蒸馏失败", "error", err, "interaction", interaction.ID)
			return
		}
		if len(items) > 0 {
			distillLogger.Info("知识蒸馏完成", "items", len(items), "interaction", interaction.ID)
		}
	}()
}

// ExtractUserInputFromHistory 把"提取最后一条 user 消息"逻辑独立出来，
// 供 saveSolutionMemory 复用（v6.x：saveSolutionMemory 也走异步路径）。
func ExtractUserInputFromHistory(history []Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == RoleUser {
			return history[i].Content
		}
	}
	return ""
}
