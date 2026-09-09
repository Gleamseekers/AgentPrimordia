// react_plan_executor.go — R1.3 Planning 接入（G1-1）
// 在 ReAct Agent 入口处，对复杂任务先调用 planner 分解为子任务，
// 然后按依赖图（DAG）分层调度执行。每层内串行执行（同层并行留作后续优化）。
package agent

import (
	"context"
	"fmt"
	"time"

	"agentprimordia/internal/agent/planning"
	"agentprimordia/internal/persist"
)

// getPlannerOrNil 通过 capCache 获取 planner（nil-safe）
func (a *ReActAgent) getPlannerOrNil() planning.Planner {
	if a.capCache == nil {
		return a.getPlanner()
	}
	return a.capCache.planner
}

// getEnhancedPlannerOrNil 检查当前 planner 是否为 *planning.EnhancedPlanner。
// 返回非 nil 表示 planner 已配置为增强规划器（含 Deadlock 检测器和 Recovery 策略）；
// 返回 nil 表示未配置或非增强规划器，调用方应跳过死路检测逻辑（opt-in 语义）。
func (a *ReActAgent) getEnhancedPlannerOrNil() *planning.EnhancedPlanner {
	p := a.getPlannerOrNil()
	if p == nil {
		return nil
	}
	ep, ok := p.(*planning.EnhancedPlanner)
	if !ok {
		return nil
	}
	return ep
}

// extractUserInput 从 history 中取出最后一条 UserMessage 的内容
func extractUserInput(history []Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == RoleUser {
			return history[i].Content
		}
	}
	return ""
}

// executePlanWithSelfHealing 执行计划，失败时自动换路径（v3.6-1）。
//
// 自愈流程（PlanRecoveryMode 非 "off" 时启用）：
//  1. 先执行 executePlan；
//  2. 失败后尝试 replan：带失败反馈重新生成计划并执行（换路径）；
//  3. replan 不可用或仍失败则降级为普通 runLoop（整任务单循环），
//     保证请求不因计划失败而中断（故障恢复不依赖人工）；
//  4. 每次自愈动作记录到 stats.PlanRecoveries。
func (a *ReActAgent) executePlanWithSelfHealing(ctx context.Context, history []Message, plan *planning.Plan, cfg loopConfig) (*Response, error) {
	resp, err := a.executePlan(ctx, history, plan, cfg, a.startTime, 0, 0, 0)
	if err == nil {
		return resp, nil
	}

	if a.config.PlanRecoveryMode == "off" {
		return nil, err
	}

	userInput := extractUserInput(history)

	// 方案 1：replan——带失败反馈重新分解并执行
	if planner := a.getPlannerOrNil(); planner != nil && userInput != "" {
		a.logger.Warn("Plan 执行失败，尝试 replan 换路径", "error", err)
		newPlan, planErr := planner.GeneratePlan(ctx,
			userInput+"（注意：上次计划执行失败："+err.Error()+"，请重新设计更稳妥的步骤）")
		if planErr == nil && newPlan != nil && len(newPlan.SubTasks) > 0 {
			newResp, newErr := a.executePlan(ctx, history, newPlan, cfg, a.startTime, 0, 0, 0)
			if newErr == nil {
				a.recordPlanRecovery(PlanRecovery{Method: "replan", Success: true, Error: err.Error()})
				return newResp, nil
			}
			a.recordPlanRecovery(PlanRecovery{Method: "replan", Success: false, Error: newErr.Error()})
			err = newErr
		} else if planErr != nil {
			err = planErr
		}
	}

	// 方案 2：降级——整任务回退到普通 runLoop（skipPlan 防止递归重入 plan 分支）
	a.logger.Warn("replan 失败或不可用，降级到普通 runLoop", "error", err)
	degradeCfg := cfg
	degradeCfg.skipPlan = true
	fallback, fbErr := a.runLoop(ctx, history, 0, degradeCfg, 0, 0, 0)
	recoveryErr := err
	if fbErr != nil {
		recoveryErr = fbErr
	}
	a.recordPlanRecovery(PlanRecovery{Method: "degrade", Success: fbErr == nil, Error: recoveryErr.Error()})
	return fallback, fbErr
}

// recordPlanRecovery 记录一次自愈动作到 stats（v3.6-1）。
func (a *ReActAgent) recordPlanRecovery(rec PlanRecovery) {
	rec.Timestamp = time.Now()
	a.statsMu.Lock()
	a.stats.PlanRecoveries = append(a.stats.PlanRecoveries, rec)
	a.statsMu.Unlock()
	a.logger.Warn("自愈动作已记录", "method", rec.Method, "success", rec.Success)
}

// tryDeadlockRecovery 死路检测触发后的自动恢复（fire-and-forget）。
// 通过 EnhancedPlanner.Recovery.Recover 生成替代方案并记录恢复动作。
// 恢复失败仅记录日志，不影响当前执行流程（当前子任务仍会失败返回，
// 由上层 executePlanWithSelfHealing 做 replan/降级）。
func (a *ReActAgent) tryDeadlockRecovery(ctx context.Context, plan *planning.Plan, failedSubTaskID string, ep *planning.EnhancedPlanner) {
	if ep.Recovery == nil {
		return
	}
	// 使用独立 background context，避免父 ctx 取消中断恢复
	bgCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	go func() {
		defer cancel()
		newPlan, err := ep.Recovery.Recover(bgCtx, plan, failedSubTaskID)
		if err != nil {
			a.logger.Warn("死路恢复失败", "subtask", failedSubTaskID, "err", err)
			return
		}
		a.logger.Info("死路恢复成功，已生成替代方案",
			"subtask", failedSubTaskID,
			"new_subtasks", len(newPlan.SubTasks),
		)
		a.recordPlanRecovery(PlanRecovery{
			Method:  "deadlock_recovery",
			Success: true,
			Error:   "deadlock detected for subtask: " + failedSubTaskID,
		})
	}()
}

// executePlan 按依赖关系调度子任务，返回最终 Response。
//
// 行为契约：
//   - 仅有 1 个子任务时直接执行（视为普通任务，不走 Plan 分支）
//   - 拓扑排序后按层执行；同层子任务目前串行执行（同层并行是后续优化）
//   - 子任务失败自动重试（默认 1 次）；重试耗尽仍失败则保存 failed checkpoint 并返回
//   - 每个子任务完成后保存带 Plan 进度的 checkpoint，支持断点续跑整个计划
//   - 最终 Content = 最后一个完成子任务的 Content（合并策略可后续扩展）
//
// 这是 R1.3 G1-1 闭环的接入点。调用方应先调用 getPlannerOrNil() 检查 planner
// 是否存在，并对 GeneratePlan 失败做优雅降级（直接返回错误或回退到正常 runLoop）。
func (a *ReActAgent) executePlan(ctx context.Context, history []Message, plan *planning.Plan, cfg loopConfig, startTime time.Time, totalLLMLatency time.Duration, totalToolLatency time.Duration, toolCount int) (*Response, error) {
	return a.executePlanWithState(ctx, history, plan, cfg, startTime, totalLLMLatency, totalToolLatency, toolCount, nil)
}

// defaultPlanSubtaskRetries 子任务默认重试次数（失败时额外尝试的次数）
const defaultPlanSubtaskRetries = 1

// planProgress Plan 执行的中间进度（checkpoint 与恢复共用）
type planProgress struct {
	subtasks  []planning.SubTask
	completed []string
	results   map[string]string
	tools     int
	llmL      time.Duration
	toolL     time.Duration
}

// runSubtaskWithRetry 带重试地执行子任务执行函数。
// 失败时最多额外重试 maxRetries 次（总计尝试 maxRetries+1 次）；ctx 取消时立即返回。
// v3.6-1：每次重试会把上一次失败原因作为 hint 传入，驱动"换一种方案"。
func runSubtaskWithRetry(ctx context.Context, run func(feedback string) (*Response, error), maxRetries int) (*Response, error) {
	var lastErr error
	feedback := ""
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		resp, err := run(feedback)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		feedback = fmt.Sprintf("上一次尝试失败：%v，请更换实现方案或改用其他工具。", err)
	}
	return nil, lastErr
}

// runPlanSubtask 执行单个子任务：优先使用注入的执行器（测试/扩展），
// 否则走默认 executeSubTask；失败时按配置的重试次数自动重试并注入失败反馈。
func (a *ReActAgent) runPlanSubtask(ctx context.Context, st planning.SubTask, history []Message, cfg loopConfig) (*Response, error) {
	maxRetries := a.config.PlanSubtaskRetries
	if maxRetries <= 0 {
		maxRetries = defaultPlanSubtaskRetries
	}
	run := func(feedback string) (*Response, error) {
		if a.subtaskExecutor != nil {
			return a.subtaskExecutor(ctx, st, history, cfg)
		}
		return a.executeSubTask(ctx, st, history, cfg, feedback)
	}
	return runSubtaskWithRetry(ctx, run, maxRetries)
}

// buildSubTaskHistory 构造子任务的隔离上下文（v3.4-2）：
// 仅注入 原始目标 + 前置依赖子任务的结果摘要，替代全量 history 继承，
// 避免长 pipeline 中子任务间互相携带内部对话导致 context 膨胀。
// 当前子任务描述由 executeSubTask 负责追加。
func buildSubTaskHistory(history []Message, task planning.SubTask, results map[string]string) []Message {
	ctx := make([]Message, 0, len(task.DependsOn)+2)
	if goal := extractUserInput(history); goal != "" {
		ctx = append(ctx, UserMessage("目标: "+goal))
	}
	for _, dep := range task.DependsOn {
		if out, ok := results[dep]; ok && out != "" {
			ctx = append(ctx, UserMessage(fmt.Sprintf("[前置子任务 %s 结果]\n%s", dep, out)))
		}
	}
	return ctx
}

// executePlanWithState 是 executePlan 的实现，支持从既有进度（initial）恢复。
// initial 为 nil 表示全新执行；恢复时跳过已完成子任务、沿用其结果。
func (a *ReActAgent) executePlanWithState(ctx context.Context, history []Message, plan *planning.Plan, cfg loopConfig, startTime time.Time, totalLLMLatency time.Duration, totalToolLatency time.Duration, toolCount int, initial *planProgress) (*Response, error) {
	if plan == nil || len(plan.SubTasks) == 0 {
		return nil, fmt.Errorf("empty plan")
	}
	if len(plan.SubTasks) == 1 && initial == nil {
		// 单子任务视为普通任务：复用 runLoop
		return a.executeSubTask(ctx, plan.SubTasks[0], history, cfg)
	}

	// 构建 DAG 并拓扑排序
	graph := buildDependencyGraph(plan.SubTasks)
	layers, err := graph.topologicalLayers()
	if err != nil {
		return nil, fmt.Errorf("plan DAG invalid: %w", err)
	}

	a.logger.Info("Plan 执行开始",
		"name", a.config.Name,
		"subtasks", len(plan.SubTasks),
		"layers", len(layers),
	)

	// v7.3-P2fix：按子任务数均分轮次和超时预算，防止单子任务耗尽全局配额。
	// 轮次预算：总 MaxTurns / 子任务数，下限 3 轮（保证基本执行空间）。
	// 超时预算：从 context deadline 均分，无 deadline 时用 60s * 子任务数。
	nSubtasks := len(plan.SubTasks)
	subtaskTurns := a.config.MaxTurns / nSubtasks
	if subtaskTurns < 3 {
		subtaskTurns = 3
	}
	subTimeout := 60 * time.Second // 默认每子任务 60s
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining > 0 {
			subTimeout = remaining / time.Duration(nSubtasks)
			if subTimeout < 10*time.Second {
				subTimeout = 10 * time.Second
			}
		}
	}
	cfg.subtaskMaxTurns = subtaskTurns

	a.logger.Info("子任务预算分配",
		"turns_per_subtask", subtaskTurns,
		"timeout_per_subtask", subTimeout,
	)

	// 初始化进度：全新执行或从 checkpoint 恢复
	pp := &planProgress{subtasks: plan.SubTasks, results: make(map[string]string)}
	if initial != nil {
		pp.completed = append([]string(nil), initial.completed...)
		for k, v := range initial.results {
			pp.results[k] = v
		}
		pp.tools = initial.tools
		pp.llmL = initial.llmL
		pp.toolL = initial.toolL
	}
	done := make(map[string]bool, len(pp.completed))
	for _, id := range pp.completed {
		done[id] = true
	}

	var lastOutput string

	for _, layer := range layers {
		for _, st := range layer {
			// 恢复路径：跳过已完成子任务，沿用其输出
			if done[st.ID] {
				if r, ok := pp.results[st.ID]; ok {
					lastOutput = r
				}
				continue
			}

			a.emitStream(cfg, StreamEvent{Type: StreamEventThought, Content: fmt.Sprintf("[Plan] 执行子任务 %s: %s", st.ID, st.Description)})
			// v3.4-2：子任务上下文隔离——仅注入目标 + 前置依赖结果，而非全量历史
			subHistory := buildSubTaskHistory(history, st, pp.results)
			// v7.3-P2fix：每子任务独立超时，避免一个子任务卡死拖垮整个 plan
			subCtx, subCancel := context.WithTimeout(ctx, subTimeout)
			resp, err := a.runPlanSubtask(subCtx, st, subHistory, cfg)
			subCancel()
			if err != nil {
				a.logger.Warn("Plan 子任务失败",
					"subtask_id", st.ID,
					"subtask_desc", st.Description,
					"error", err,
				)
				// Task 12：DeadlockDetector 接入——子任务失败后检查死路
				if ep := a.getEnhancedPlannerOrNil(); ep != nil && ep.Deadlock != nil {
					if ep.Deadlock.RecordFailure(st.ID) {
						a.logger.Warn("检测到子任务死路，触发自动恢复",
							"subtask_id", st.ID,
						)
						a.tryDeadlockRecovery(ctx, plan, st.ID, ep)
					}
				}
				// 保存 failed checkpoint（含已完成进度），供断点续跑
				a.savePlanCheckpoint(ctx, history, pp, "failed")
				return nil, fmt.Errorf("subtask %s failed: %w", st.ID, err)
			}
			pp.completed = append(pp.completed, st.ID)
			if resp != nil {
				lastOutput = resp.Content
				pp.results[st.ID] = resp.Content
				pp.tools += resp.Metrics.TotalTools
				pp.llmL += resp.Metrics.LLMLatency
				pp.toolL += resp.Metrics.ToolLatency
			}
			a.savePlanCheckpoint(ctx, history, pp, "running")
		}
	}

	totalLLMLatency += pp.llmL
	totalToolLatency += pp.toolL
	toolCount += pp.tools

	return &Response{
		RequestID: cfg.requestID,
		Content:   lastOutput,
		Metrics: Metrics{
			TotalTurns:  len(plan.SubTasks),
			TotalTools:  toolCount,
			Duration:    time.Since(startTime),
			LLMLatency:  totalLLMLatency,
			ToolLatency: totalToolLatency,
		},
	}, nil
}

// savePlanCheckpoint 保存带 Plan 进度的 checkpoint（v3.4-1）。
// 与 saveCheckpoint 并存：saveCheckpoint 由 runLoop 每轮调用（无 plan 语义），
// 本方法由 executePlan 在子任务边界调用（含 plan/子任务进度）。
func (a *ReActAgent) savePlanCheckpoint(ctx context.Context, history []Message, pp *planProgress, status string) {
	cs := a.getCheckpointStore()
	if cs == nil {
		return
	}

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

	cpSubtasks := make([]persist.CheckpointSubTask, 0, len(pp.subtasks))
	for _, st := range pp.subtasks {
		cpSubtasks = append(cpSubtasks, persist.CheckpointSubTask{
			ID:          st.ID,
			Description: st.Description,
			DependsOn:   st.DependsOn,
		})
	}

	state := &persist.AgentState{
		AgentID:   a.config.Name,
		SessionID: a.config.SessionID,
		Status:    status,
		Messages:  msgs,
		TurnCount: len(pp.completed),
		Metrics: persist.CheckpointMetrics{
			TotalTurns:  len(pp.completed),
			TotalTools:  pp.tools,
			LLMLatency:  pp.llmL.String(),
			ToolLatency: pp.toolL.String(),
		},
		Plan: &persist.CheckpointPlan{
			Subtasks:      cpSubtasks,
			Completed:     pp.completed,
			Results:       pp.results,
			TotalTools:    pp.tools,
			LLMLatencyNs:  pp.llmL.Nanoseconds(),
			ToolLatencyNs: pp.toolL.Nanoseconds(),
		},
		SavedAt: time.Now().UTC(),
	}

	if err := cs.Save(ctx, state); err != nil {
		a.logger.Warn("保存 plan 检查点失败", "error", err)
	}
}

// buildPlanProgressFromState 从 checkpoint 状态重建 plan 进度，供断点续跑使用。
func buildPlanProgressFromState(cp *persist.CheckpointPlan) *planProgress {
	if cp == nil {
		return nil
	}
	subtasks := make([]planning.SubTask, 0, len(cp.Subtasks))
	for _, s := range cp.Subtasks {
		subtasks = append(subtasks, planning.SubTask{
			ID:          s.ID,
			Description: s.Description,
			DependsOn:   s.DependsOn,
		})
	}
	return &planProgress{
		subtasks:  subtasks,
		completed: append([]string(nil), cp.Completed...),
		results:   cp.Results,
		tools:     cp.TotalTools,
		llmL:      time.Duration(cp.LLMLatencyNs),
		toolL:     time.Duration(cp.ToolLatencyNs),
	}
}

// executeSubTask 执行单个子任务（复用 runLoop 的核心逻辑）
//
// 实现：把子任务描述追加为新的 UserMessage，然后递归调用 runLoop。
// 这样每个子任务都可以独立调用tool、产生 ReAct 循环。
// v3.6-1：feedback 携带上一次失败原因（换方案提示），为空时省略。
func (a *ReActAgent) executeSubTask(ctx context.Context, task planning.SubTask, history []Message, cfg loopConfig, feedback ...string) (*Response, error) {
	taskHistory := make([]Message, 0, len(history)+2)
	taskHistory = append(taskHistory, history...)
	taskHistory = append(taskHistory, UserMessage(task.Description))
	if len(feedback) > 0 && feedback[0] != "" {
		taskHistory = append(taskHistory, UserMessage(feedback[0]))
	}
	return a.runLoop(ctx, taskHistory, 0, cfg, 0, 0, 0)
}

// ===== 简单 DAG 实现 =====
// 内部使用 map[string][]string 表示依赖关系；
// 提供拓扑分层（Kahn 算法），输出每层可独立执行的子任务集合。

// depGraph 简单依赖图
type depGraph struct {
	nodes    map[string]planning.SubTask
	inEdges  map[string]int      // 节点的入度（依赖数）
	outEdges map[string][]string // 节点 → 它解锁的下游
	allIDs   []string
}

// buildDependencyGraph 从 SubTask 列表构建依赖图
func buildDependencyGraph(tasks []planning.SubTask) *depGraph {
	g := &depGraph{
		nodes:    make(map[string]planning.SubTask, len(tasks)),
		inEdges:  make(map[string]int, len(tasks)),
		outEdges: make(map[string][]string, len(tasks)),
		allIDs:   make([]string, 0, len(tasks)),
	}
	// 初始化
	for _, t := range tasks {
		g.nodes[t.ID] = t
		g.inEdges[t.ID] = 0
		g.allIDs = append(g.allIDs, t.ID)
	}
	// 构建依赖边（注意：ID 不存在时容错，跳过该依赖）
	for _, t := range tasks {
		for _, dep := range t.DependsOn {
			if _, ok := g.nodes[dep]; !ok {
				continue // 跳过悬空依赖
			}
			g.outEdges[dep] = append(g.outEdges[dep], t.ID)
			g.inEdges[t.ID]++
		}
	}
	return g
}

// topologicalLayers 用 Kahn 算法做拓扑分层。
// 返回各层子任务列表；同层任务可独立执行（无依赖关系）。
// 当存在环时返回错误。
func (g *depGraph) topologicalLayers() ([][]planning.SubTask, error) {
	// 复制入度用于就地修改
	indeg := make(map[string]int, len(g.inEdges))
	for k, v := range g.inEdges {
		indeg[k] = v
	}

	var layers [][]planning.SubTask
	processed := 0
	for processed < len(g.allIDs) {
		var layer []planning.SubTask
		for _, id := range g.allIDs {
			if indeg[id] == 0 {
				layer = append(layer, g.nodes[id])
			}
		}
		if len(layer) == 0 {
			return nil, fmt.Errorf("cycle detected: %d unprocessed nodes", len(g.allIDs)-processed)
		}
		layers = append(layers, layer)
		// 标记当前层节点为已处理：把它们入度设为 -1（已访问）
		for _, t := range layer {
			indeg[t.ID] = -1
			processed++
		}
		// 减少下游入度
		for _, t := range layer {
			for _, downstream := range g.outEdges[t.ID] {
				if indeg[downstream] > 0 {
					indeg[downstream]--
				}
			}
		}
	}
	return layers, nil
}
