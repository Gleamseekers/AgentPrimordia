// react_loop_core.go — ReAct 循环核心体
// 包含 runLoop 主循环逻辑，被 reactLoopEngine 和 ResumeFromCheckpoint 共享。
// 结构（按职责拆分，均为行为保持重构）：
//   - runLoop     主循环：turn 前置检查 → runLoopTurn → checkpoint
//   - runLoopTurn 单轮逻辑：上下文准备 → LLM 调用 → tool 执行 → turn 收尾
//   - 其余函数为上述两者按阶段抽出的命名单元，职责见各自注释
package agent

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent/learning"
	"github.com/Gleamseekers/AgentPrimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/internal/observability"
	"github.com/Gleamseekers/AgentPrimordia/internal/tools"
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

	// R1.3 G1-1：Planning 接入（tryPlanExecution：仅首轮且 planner 已配置时尝试；
	// 命中多子任务计划时走 DAG 执行并直接返回，否则降级为正常 runLoop）
	if resp, planErr, planned := a.tryPlanExecution(ctx, history, startTurn, cfg); planned {
		return resp, planErr
	}

	// 优化（Task 3.5）：仅在需要记录 turn 延迟时获取时间戳
	// v3.5-4：启用全链路关联时也需计时（RecordTurn 关联到 trace）
	needTiming := a.getMetricsRecorder() != nil || a.getLabeledRecorder() != nil ||
		(a.capCache != nil && a.capCache.observability != nil)

	// v7.3-P2fix：子任务轮次预算覆盖全局 MaxTurns（resolveMaxTurns），
	// 防止 plan 场景下单子任务耗尽配额
	maxTurns := a.resolveMaxTurns(cfg)

	for turn := startTurn; turn < maxTurns; turn++ {
		var turnStart time.Time
		if needTiming {
			turnStart = time.Now()
		}

		// 轮次前置检查（checkTurnPreconditions）：停止信号 / ctx 取消
		if perr := a.checkTurnPreconditions(ctx, cfg); perr != nil {
			return &Response{RequestID: cfg.requestID, Error: perr}, perr
		}

		// 优化（Task 3.5）：使用原子操作替代 mutex，消除热路径上的锁竞争
		a.atomicTurn.Store(int64(turn + 1))
		a.atomicMessages.Store(int64(len(history)))

		_ = a.fireHookWithPool(HookBeforeTurn, turn)
		// 优化（Task 3）：仅在存在订阅者时构造 payload map，避免热点路径上的堆分配
		if a.hasEventSubscriber() {
			a.publishEvent(EventTurnStart, map[string]int{"turn": turn})
		}

		turnSpan := a.startTurnSpan(tracer, turn, rootSpanCtx)

		// P1-1（评估报告 §4.2）：单轮逻辑包在闭包内执行，turnSpan 一律
		// defer End()——早退路径（预算超限 / 记忆 fast-path / LLM 错误 /
		// guardrail 拦截 / 优雅关闭）与 panic 路径同样闭合 span，
		// 不再依赖每个 return 前手动 End()。
		out := func() turnOutcome {
			defer turnSpan.End()
			return a.runLoopTurn(ctx, history, turn, startTurn, cfg, tracer, turnSpan,
				costTracker, totalLLMLatency, totalToolLatency, toolCount, needTiming, turnStart)
		}()
		if out.finished {
			return out.resp, out.err
		}
		history = out.history
		totalLLMLatency = out.totalLLMLatency
		totalToolLatency = out.totalToolLatency
		toolCount = out.toolCount

		a.saveCheckpoint(ctx, history, turn+1, turnMetrics(
			turn+1, toolCount, time.Since(a.startTime), totalLLMLatency, totalToolLatency))
	}

	// 循环耗尽：构造"超出最大轮次"终态（maxTurnsExceededOutcome）
	return a.maxTurnsExceededOutcome(cfg, toolCount, totalLLMLatency, totalToolLatency)
}

// tryPlanExecution 首轮 Planning 接入（R1.3 G1-1，从 runLoop 抽出）。
//
// 仅在 startTurn == 0 且未走自愈降级路径（cfg.skipPlan）时尝试：
//   - planner 未配置 / 无用户输入 / 计划生成失败 / 子任务不超过 1 个
//     → planned=false，runLoop 继续正常循环；
//   - 计划可分解为多个子任务 → 任务/子任务落世界模型图并进入 DAG 执行
//     （含 replan/降级自愈），planned=true，runLoop 原样返回其 (resp, err)。
func (a *ReActAgent) tryPlanExecution(ctx context.Context, history []Message, startTurn int, cfg loopConfig) (resp *Response, err error, planned bool) {
	if startTurn != 0 || cfg.skipPlan { // v3.6-1：自愈降级路径跳过 plan 分支
		return nil, nil, false
	}
	planner := a.getPlannerOrNil()
	if planner == nil {
		return nil, nil, false
	}
	userInput := extractUserInput(history)
	if userInput == "" {
		return nil, nil, false
	}
	plan, planErr := planner.GeneratePlan(ctx, userInput)
	if planErr != nil {
		a.logger.Warn("Planning 失败，降级到正常 runLoop", "error", planErr)
		return nil, nil, false
	}
	if plan == nil || len(plan.SubTasks) <= 1 {
		return nil, nil, false
	}
	a.logger.Info("使用 Plan 执行",
		"subtasks", len(plan.SubTasks),
		"goal", plan.Goal,
	)
	// v6.1 接线点②（planner 粗粒度计划）：任务/子任务落图 + 组建期预演门
	a.wmObservePlan(ctx, 0, userInput, plan)
	// v3.6-1：失败自动换路径（replan/降级），故障恢复不依赖人工
	resp, err = a.executePlanWithSelfHealing(ctx, history, plan, cfg)
	return resp, err, true
}

// resolveMaxTurns 解析主循环最大轮次（v7.3-P2fix，从 runLoop 抽出）：
// 子任务轮次预算 cfg.subtaskMaxTurns 非零且小于全局 MaxTurns 时覆盖之，
// 防止 plan 场景下单子任务耗尽全局配额。
func (a *ReActAgent) resolveMaxTurns(cfg loopConfig) int {
	maxTurns := a.config.MaxTurns
	if cfg.subtaskMaxTurns > 0 && cfg.subtaskMaxTurns < maxTurns {
		maxTurns = cfg.subtaskMaxTurns
	}
	return maxTurns
}

// checkTurnPreconditions 每轮进入主循环前的前置检查（从 runLoop 抽出）：
//   - lifecycle 已停止 → 置 StatusCancelled、推送错误流事件，返回 ErrAgentStopped；
//   - ctx 已取消/超时 → 置 StatusCancelled、推送错误流事件，返回 ctx.Err()。
//
// 正常放行返回 nil；调用方负责把返回的错误包进 *Response 后返回。
func (a *ReActAgent) checkTurnPreconditions(ctx context.Context, cfg loopConfig) error {
	if a.lifecycle.IsStopped() {
		_ = a.lifecycle.SetStatus(StatusCancelled)
		a.emitStream(cfg, StreamEvent{Type: StreamEventError, Content: ErrAgentStopped.Error()})
		return ErrAgentStopped
	}
	if ctx.Err() != nil {
		_ = a.lifecycle.SetStatus(StatusCancelled)
		a.emitStream(cfg, StreamEvent{Type: StreamEventError, Content: ctx.Err().Error()})
		return ctx.Err()
	}
	return nil
}

// startTurnSpan 创建本轮 turn span（从 runLoop 抽出）。
// tracer 未配置时返回 NoopSpan；传入有效根 Span 上下文时把 turn span
// 链接到根 span（全链路关联）。
func (a *ReActAgent) startTurnSpan(tracer Tracer, turn int, rootSpanCtx []SpanContext) Span {
	if tracer == nil {
		return &NoopSpan{}
	}
	opts := []SpanOption{
		WithAttributes(map[string]any{"agent": a.config.Name, "turn": turn}),
	}
	// 如果有根 Span 上下文，将 turn span 链接到根 span
	if len(rootSpanCtx) > 0 && rootSpanCtx[0].IsValid() {
		opts = append(opts, WithParent(rootSpanCtx[0]))
	}
	return tracer.Start(
		"turn."+strconv.Itoa(turn),
		SpanKindInternal,
		opts...,
	)
}

// turnMetrics 构造轮次指标快照。
// runLoopTurn 完成/优雅关闭与 runLoop 超轮次三条终态路径共用同一字段集，
// 抽出来避免重复的 Metrics 字面量（行为零变化）。
func turnMetrics(totalTurns, toolCount int, duration, llmLatency, toolLatency time.Duration) Metrics {
	return Metrics{
		TotalTurns:  totalTurns,
		TotalTools:  toolCount,
		Duration:    duration,
		LLMLatency:  llmLatency,
		ToolLatency: toolLatency,
	}
}

// maxTurnsExceededOutcome 主循环耗尽的终态处理（从 runLoop 抽出）：
// 置 StatusFailed、推送错误流事件、触发 OnError hook、记录告警日志，
// 并构造带累计指标的响应，返回 (response, ErrMaxTurnsExceeded)。
func (a *ReActAgent) maxTurnsExceededOutcome(cfg loopConfig, toolCount int, totalLLMLatency, totalToolLatency time.Duration) (*Response, error) {
	_ = a.lifecycle.SetStatus(StatusFailed)
	a.emitStream(cfg, StreamEvent{Type: StreamEventError, Content: ErrMaxTurnsExceeded.Error()})
	_ = a.fireHookWithPoolErr(HookOnError, ErrMaxTurnsExceeded)
	a.logger.Warn("Agent 超出最大轮次", "name", a.config.Name, "max_turns", a.config.MaxTurns)

	response := &Response{
		RequestID: cfg.requestID,
		Error:     ErrMaxTurnsExceeded,
		Metrics: turnMetrics(a.config.MaxTurns, toolCount,
			time.Since(a.startTime), totalLLMLatency, totalToolLatency),
	}

	return response, ErrMaxTurnsExceeded
}

// turnOutcome 单轮 runLoop 的执行结果（P1-1：turnSpan 泄漏修复引入）。
// finished 为 true 表示 runLoop 应立即以 (resp, err) 返回。
type turnOutcome struct {
	resp             *Response
	err              error
	finished         bool
	history          []Message
	totalLLMLatency  time.Duration
	totalToolLatency time.Duration
	toolCount        int
}

// runLoopTurn 执行单轮 ReAct 逻辑（P1-1 从 runLoop 主体抽出）。
//
// 顺序：成本检查 → 记忆注入/已解任务 fast-path → 技能指引注入 → RAG 注入 →
// 上下文裁剪 → LLM 调用（invokeLLM）→ 输出端护栏 → 无 tool 调用则完成
// （completeWithoutToolCalls）并返回，否则执行本轮全部 tool 调用后进入
// 优雅关闭判定。
//
// 阶段函数（继续拆分，均在本文件，职责见各自注释）：resolveToolDefinitions /
// invokeLLM / recordLLMUsageAndAudit / finishTurnBookkeeping /
// completeWithoutToolCalls / gracefulShutdownOutcome。
//
// span 纪律（P1-1）：turnSpan 由调用方闭包 defer End()；llmSpan 在 invokeLLM
// 内用内层闭包 defer End()，使 sync/stream 错误路径不再泄漏，同时 llm.call
// 时长只覆盖 LLM 调用本身（不把后续 tool 执行计入）。
func (a *ReActAgent) runLoopTurn(
	ctx context.Context,
	history []Message,
	turn, startTurn int,
	cfg loopConfig,
	tracer Tracer,
	turnSpan Span,
	costTracker *CostTracker,
	totalLLMLatency, totalToolLatency time.Duration,
	toolCount int,
	needTiming bool,
	turnStart time.Time,
) turnOutcome {
	done := func(resp *Response, err error) turnOutcome {
		return turnOutcome{
			resp: resp, err: err, finished: true,
			history: history, totalLLMLatency: totalLLMLatency,
			totalToolLatency: totalToolLatency, toolCount: toolCount,
		}
	}

	// 成本检查（v4.1 拆分：checkBudgetExceeded）
	if resp, cerr := a.checkBudgetExceeded(cfg, costTracker); cerr != nil {
		return done(resp, cerr)
	}

	// 记忆注入 + 已解任务 fast-path（v4.1 拆分：injectMemoryContextAndFastPath）
	var fastResp *Response
	var fastHit bool
	history, fastResp, fastHit = a.injectMemoryContextAndFastPath(ctx, history, turn, startTurn, cfg)
	if fastHit {
		return done(fastResp, nil)
	}

	// 技能匹配注入（v7.4 接线）：命中已习得技能时把步骤指引作为 system 上下文注入；
	// 未配置 Matcher 时原样返回（默认路径零变更）。
	history = a.injectSkillGuidance(history)

	// RAG 检索与注入（v4.1 拆分：ragRetrieveAndInject）
	history = a.ragRetrieveAndInject(ctx, history, turn, startTurn, cfg, tracer, turnSpan)

	trimmedHistory := a.trimContext(history, 0)
	// v6.1 接线点④：被裁消息转世界事实节点（提案 E6 截断债务的结构化偿还）
	a.wmNotifyTrimmed(history, trimmedHistory, turn)
	llmMessages := convertToLLMMessages(trimmedHistory)

	// 优化（Task 2 / Task 2.5 / perf-v2）：capCache 预转换的 toolDefinitions
	// 优先，否则回退 toolkit.Definitions() 现转换（resolveToolDefinitions）
	toolDefinitions := a.resolveToolDefinitions()

	// LLM 调用阶段（invokeLLM）：流式/非流式分发 + llmSpan 闭合纪律
	thought, llmLatency, llmErr := a.invokeLLM(ctx, cfg, llmMessages, toolDefinitions, tracer, turnSpan, turn)
	if llmErr != nil {
		// 与原实现一致：仅非流式路径触发 onError hook 与 StatusFailed
		if !cfg.stream {
			a.handleOnError(ctx, llmErr)
			_ = a.lifecycle.SetStatus(StatusFailed)
		}
		return done(&Response{RequestID: cfg.requestID, Error: llmErr}, llmErr)
	}

	totalLLMLatency += llmLatency

	// LLM 调用后置：用量记账 + LLMCall 审计事件（recordLLMUsageAndAudit）
	a.recordLLMUsageAndAudit(ctx, turn, thought, llmLatency)

	// 输出端护栏（v4.1 拆分：guardrailSanitizeOutput；PII 脱敏、注入拦截）
	if resp, gerr := a.guardrailSanitizeOutput(ctx, cfg, &thought, turn); gerr != nil {
		return done(resp, gerr)
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

	// 无tool调用 → Agent 完成（completeWithoutToolCalls：反思/终态/收尾/学习）
	if len(thought.ToolCalls) == 0 {
		response := a.completeWithoutToolCalls(ctx, history, cfg, turn, thought,
			totalLLMLatency, totalToolLatency, toolCount, needTiming, turnStart)
		return done(response, nil)
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

	// turn 收尾记账（finishTurnBookkeeping：AfterTurn hook + 耗时 + TurnEnd 事件）
	a.finishTurnBookkeeping(turn, needTiming, turnStart)

	if a.lifecycle.IsGracefulShutdown() {
		// 优雅关闭终态（gracefulShutdownOutcome）：当前 turn 已完成，带部分结果退出
		response := a.gracefulShutdownOutcome(cfg, turn, thought.Content,
			totalLLMLatency, totalToolLatency, toolCount)
		return done(response, ErrAgentStopped)
	}

	return turnOutcome{
		history: history, totalLLMLatency: totalLLMLatency,
		totalToolLatency: totalToolLatency, toolCount: toolCount,
	}
}

// resolveToolDefinitions 解析本轮 LLM 调用使用的工具定义（从 runLoopTurn 抽出）。
// 优化（Task 2 / Task 2.5 / perf-v2）：优先返回 capCache 预转换结果；
// 否则回退到 capCache.toolkit / getToolkit() 的 Definitions() 现转换。
func (a *ReActAgent) resolveToolDefinitions() []llm.ToolDefinition {
	if a.capCache != nil && a.capCache.toolDefinitions != nil {
		return a.capCache.toolDefinitions
	}
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
	return convertToolDefsToLLMDefinitions(toolDefs)
}

// invokeLLM 执行本轮 LLM 调用（从 runLoopTurn 抽出）：流式/非流式分发、
// LLMCall 事件推送、llmSpan 创建与闭合；返回思考结果、调用耗时与错误。
//
// span 纪律（P1-1）：llmSpan 用内层闭包 defer End()——sync/stream 错误路径
// 不再泄漏，同时 llm.call 时长只覆盖 LLM 调用本身（不把后续 tool 执行计入）。
// 流式路径产出空思考（无内容且无 tool 调用）时包装为错误返回，与原实现一致。
func (a *ReActAgent) invokeLLM(
	ctx context.Context,
	cfg loopConfig,
	llmMessages []llm.ChatMessage,
	toolDefinitions []llm.ToolDefinition,
	tracer Tracer,
	turnSpan Span,
	turn int,
) (Thought, time.Duration, error) {
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

	// P1-1：llmSpan 用内层闭包 defer End()（见函数注释）
	var (
		thought    Thought
		llmErr     error
		llmLatency time.Duration
	)
	func() {
		defer llmSpan.End()
		if cfg.stream {
			var sErr error
			thought, sErr = a.streamReasoning(ctx, cfg, llmMessages, toolDefinitions, llmStart)
			if thought.Content == "" && len(thought.ToolCalls) == 0 {
				llmErr = fmt.Errorf("stream reasoning failed: %w", sErr)
				return
			}
		} else {
			thought, llmErr = a.syncReasoning(ctx, llmMessages, toolDefinitions, llmStart)
			if llmErr != nil {
				return
			}
		}
		llmLatency = time.Since(llmStart)
		llmSpan.SetAttribute("latency_ms", llmLatency.Milliseconds())
	}()

	return thought, llmLatency, llmErr
}

// recordLLMUsageAndAudit LLM 调用后置处理（从 runLoopTurn 抽出）：
// 用量记账（recordUsage）+ p2t4 LLMCall 审计事件（含 turn、耗时、token 数）。
func (a *ReActAgent) recordLLMUsageAndAudit(ctx context.Context, turn int, thought Thought, llmLatency time.Duration) {
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
}

// finishTurnBookkeeping turn 收尾记账（从 runLoopTurn 抽出，两条路径共用）：
// AfterTurn hook、turn 耗时记录（needTiming 时）、TurnEnd 事件推送。
// 无 tool 调用的完成路径与 tool 执行后的循环路径都调用本函数，消除重复分支。
func (a *ReActAgent) finishTurnBookkeeping(turn int, needTiming bool, turnStart time.Time) {
	_ = a.fireHookWithPool(HookAfterTurn, turn)
	if needTiming {
		a.recordTurn(time.Since(turnStart))
	}
	if a.hasEventSubscriber() {
		a.publishEvent(EventTurnEnd, map[string]int{"turn": turn})
	}
}

// completeWithoutToolCalls 无 tool 调用的完成路径（从 runLoopTurn 抽出，
// R1.4 G1-2 Reflection 接入）：反思改进最终输出 → 构造 Response →
// 置 StatusCompleted → checkpoint → OnComplete hook → turn 收尾记账 →
// 流式完成事件与日志 → AgentStop 审计 → 知识蒸馏与已解记忆落库。
//
// 注意：传入的 history 尚未追加本轮 assistantMsg（与原实现的调用点一致）。
func (a *ReActAgent) completeWithoutToolCalls(
	ctx context.Context,
	history []Message,
	cfg loopConfig,
	turn int,
	thought Thought,
	totalLLMLatency, totalToolLatency time.Duration,
	toolCount int,
	needTiming bool,
	turnStart time.Time,
) *Response {
	// R1.4 G1-2：对最终输出进行反思，必要时用 reflector 改进版本替换
	finalContent := thought.Content
	if improved, reflectErr := a.reflectAndImprove(ctx, finalContent); reflectErr == nil && improved != "" {
		finalContent = improved
	}
	duration := time.Since(a.startTime)
	response := &Response{
		RequestID: cfg.requestID,
		Content:   finalContent,
		Metrics:   turnMetrics(turn+1, toolCount, duration, totalLLMLatency, totalToolLatency),
	}
	_ = a.lifecycle.SetStatus(StatusCompleted)
	a.saveCheckpoint(ctx, history, turn+1, response.Metrics)
	_ = a.fireHookWithPoolResp(HookOnComplete, response)
	a.finishTurnBookkeeping(turn, needTiming, turnStart)
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
	return response
}

// gracefulShutdownOutcome 优雅关闭终态（从 runLoopTurn 抽出）：
// 本轮 tool 已执行完毕，记录日志、置 StatusCancelled(graceful shutdown)，
// 构造带部分内容与 ErrAgentStopped 的响应并推送错误流事件。
func (a *ReActAgent) gracefulShutdownOutcome(cfg loopConfig, turn int, content string, totalLLMLatency, totalToolLatency time.Duration, toolCount int) *Response {
	a.logger.Info("Agent 优雅关闭：当前 turn 已完成，退出循环", "name", a.config.Name, "turn", turn+1)
	_ = a.lifecycle.SetStatusWithReason(StatusCancelled, "graceful shutdown")
	response := &Response{
		RequestID: cfg.requestID,
		Content:   content,
		Error:     ErrAgentStopped,
		Metrics:   turnMetrics(turn+1, toolCount, time.Since(a.startTime), totalLLMLatency, totalToolLatency),
	}
	a.emitStream(cfg, StreamEvent{Type: StreamEventError, Content: "graceful shutdown: agent stopped after turn completion"})
	return response
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
