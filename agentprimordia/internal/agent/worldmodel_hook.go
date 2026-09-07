// worldmodel_hook.go — 世界模型接线层（v6.1 第二切片，提案 §2.1 接线点①–⑥）
//
// 职责边界：
//   - 只做「消息流 → 最小事件流」的转换与挂载：所有挂钩 nil-safe，
//     tracker 未注入（默认）时一个字节都不进入世界模型路径（铁律 7 /
//     提案 §2.1 opt-in：不调用 WithWorldModel 时默认 ReAct 行为零变更）；
//   - 预演门与回溯差异在 v6.1 为**观察模式**：不拦截执行（阻断语义待
//     治理策略），异常写入失败库（persist.FailureRecord）并触发审计；
//   - 计划步骤 ID 用 worldmodel.NodeID(KindToolCall, 摘要) 派生，与
//     ToolObserved 落图节点收敛到同一确定性 ID 空间——回溯差异可比（内核
//     契约见 worldmodel/tracker.go PlanTrajectory 与 worldmodel/graph.go NodeID）。
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"agentprimordia/internal/agent/planning"
	"agentprimordia/internal/agent/worldmodel"
	"agentprimordia/internal/persist"
)

// WorldModelCapable 世界模型能力接口（协议式微内核发现入口，接线点①）。
// CapabilityAgent 实现本接口；engine 经 a.self 断言发现。
type WorldModelCapable interface {
	GetWorldModelTracker() *worldmodel.WorldModelTracker
}

// 审计动作常量（沿用 p2t4 字符串字面量模式，避免 agent 包 import audit 包）
const (
	auditActionWMRehearsalFailed = "worldmodel.rehearsal_failed"
	auditActionWMBackDiff        = "worldmodel.backdiff_diverged"
	auditResultWMDetected        = "detected" // 异常已检出（非拦截）
)

const (
	// wmSummaryMaxRunes 世界事实摘要上限（rune 数；确定性截断，防长观察撑爆状态图）
	wmSummaryMaxRunes = 256
	// wmErrorPrefixRehearsal / wmErrorPrefixBackDiff 失败库记录的 Error 前缀标签
	wmErrorPrefixRehearsal = "[世界模型:预演门]"
	wmErrorPrefixBackDiff  = "[世界模型:回溯差异]"
)

// getWorldModelTracker 经接口发现获取世界模型跟踪器（接线点①发现入口）。
func (a *ReActAgent) getWorldModelTracker() *worldmodel.WorldModelTracker {
	if c, ok := a.self.(WorldModelCapable); ok {
		return c.GetWorldModelTracker()
	}
	return nil
}

// wmTracker 取当前 tracker：优先 capCache（Run 期间一次性解析），
// 回退接口发现（capCache 为空的路径）。
func (a *ReActAgent) wmTracker() *worldmodel.WorldModelTracker {
	if a.capCache != nil {
		return a.capCache.worldTracker
	}
	return a.getWorldModelTracker()
}

// wmFailureStore 取失败库引用（优先 capCache）。
func (a *ReActAgent) wmFailureStore() persist.FailureStore {
	if a.capCache != nil {
		return a.capCache.failureStore
	}
	return a.getFailureStore()
}

// wmSummarize 确定性世界事实摘要：去首尾空白，超长按 rune 截断加省略号。
// 同一输入必得同一摘要——状态图节点去重依赖该确定性。
func wmSummarize(s string) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= wmSummaryMaxRunes {
		return s
	}
	return string(runes[:wmSummaryMaxRunes]) + "…"
}

// wmToolCallSummary 工具调用摘要（与 tracker.onToolObserved 的
// 「工具名 输入」格式对齐；空白差异由 NodeID 规范化兜底）。
func wmToolCallSummary(name, input string) string {
	return name + " " + wmSummarize(input)
}

// wmObserveAssistant 接线点②：assistantMsg 落入 history 后调用（提案 E3）。
//   - 本轮 ToolCalls 即「计划（重新）形成」（预演态）：步骤 ID 由
//     NodeID(KindToolCall, 摘要) 派生，执行后与 ToolObserved 节点收敛；
//   - 思考文本是推理产物 → HypothesisFormed（与观测事实分型）；
//   - 无工具调用（最终回答）不产生事件。
func (a *ReActAgent) wmObserveAssistant(turn int, thought Thought) {
	t := a.wmTracker()
	if t == nil {
		return
	}
	if len(thought.ToolCalls) > 0 {
		steps := make([]worldmodel.PlanStep, 0, len(thought.ToolCalls))
		for _, tc := range thought.ToolCalls {
			summary := wmToolCallSummary(tc.Name, tc.Args)
			steps = append(steps, worldmodel.PlanStep{
				ID:      worldmodel.NodeID(worldmodel.KindToolCall, summary),
				Summary: summary,
			})
		}
		goal := fmt.Sprintf("turn %d 行动计划", turn)
		t.Apply(worldmodel.PlanRevised{Turn: turn, Goal: goal, Steps: steps})
		if text := wmSummarize(thought.Content); text != "" {
			t.Apply(worldmodel.HypothesisFormed{Turn: turn, Text: text})
		}
	}
}

// wmObservePlan 接线点②（planner 粗粒度计划）：GeneratePlan 成功后调用。
// 任务节点取用户输入，步骤摘要取子任务描述，DependsOn 引用计划内更早
// 子任务时映射为对应步骤节点 ID（前向引用/外部引用原样保留，交由
// Rehearse 按「计划内步骤 → 状态图节点」优先级判定）；随后立即跑一次预演门。
func (a *ReActAgent) wmObservePlan(ctx context.Context, turn int, task string, plan *planning.Plan) {
	t := a.wmTracker()
	if t == nil || plan == nil {
		return
	}
	stepIDOf := make(map[string]string, len(plan.SubTasks)) // planner 子任务 ID → 步骤节点 ID
	steps := make([]worldmodel.PlanStep, 0, len(plan.SubTasks))
	for _, st := range plan.SubTasks {
		summary := wmSummarize(st.Description)
		sid := worldmodel.NodeID(worldmodel.KindToolCall, summary)
		if st.ID != "" {
			stepIDOf[st.ID] = sid
		}
		var deps []string
		for _, d := range st.DependsOn {
			if d == "" {
				continue
			}
			if mapped, ok := stepIDOf[d]; ok {
				deps = append(deps, mapped)
			} else {
				deps = append(deps, d)
			}
		}
		steps = append(steps, worldmodel.PlanStep{ID: sid, Summary: summary, DependsOn: deps})
	}
	t.Apply(worldmodel.PlanRevised{
		Turn:  turn,
		Task:  wmSummarize(task),
		Goal:  wmSummarize(plan.Goal),
		Steps: steps,
	})
	a.wmRehearseGate(ctx, turn)
}

// wmObserveToolResult 接线点③：工具结果回写后调用（提案 E3）。
// 成功与失败观察都是世界事实——失败观察以「错误: …」摘要落图。
func (a *ReActAgent) wmObserveToolResult(turn int, tc *ToolCall, result *ToolResult, err error) {
	t := a.wmTracker()
	if t == nil {
		return
	}
	observation := ""
	switch {
	case err != nil:
		observation = "错误: " + err.Error()
	case result != nil:
		observation = result.Content
	}
	t.Apply(worldmodel.ToolObserved{
		Turn:        turn,
		ToolName:    tc.Name,
		ToolInput:   wmSummarize(tc.Args),
		Observation: wmSummarize(observation),
	})
}

// wmNotifyTrimmed 接线点④：上下文裁剪发生后调用（提案 E6 截断债务）。
// 以「公共前缀 + 长度差」定位被丢弃的消息（滑动窗口/Token 预算等头部丢弃
// 策略精确定位；中部丢弃策略为保守近似），转交 tracker 落为 observation
// 事实节点——被裁事实在图上不丢。
func (a *ReActAgent) wmNotifyTrimmed(before, after []Message, turn int) {
	t := a.wmTracker()
	if t == nil || len(before) <= len(after) {
		return
	}
	prefix := 0
	for prefix < len(after) && wmMessageKey(before[prefix]) == wmMessageKey(after[prefix]) {
		prefix++
	}
	dropped := len(before) - len(after)
	msgs := make([]worldmodel.TrimmedMessage, 0, dropped)
	for _, m := range before[prefix : prefix+dropped] {
		// 消息自身轮次不可知（-1）：tracker 回退为裁剪发生轮次
		msgs = append(msgs, worldmodel.TrimmedMessage{Role: string(m.Role), Content: m.Content, Turn: -1})
	}
	t.TrimNotification(msgs, turn)
}

// wmMessageKey 消息位置对齐键（角色 + 内容；确定性、无分配敏感路径）。
func wmMessageKey(m Message) string {
	return string(m.Role) + "\x00" + m.Content
}

// wmRehearseGate 接线点⑤：工具执行前调用（提案 §2.1：预演 gate 在 tool 执行前）。
// v6.1 观察模式：预演不过**不拦截执行**（阻断语义待治理策略），缺陷写入
// 失败库并触发审计——预演门/失败库是工程地板硬交付（路线图 §三）。
func (a *ReActAgent) wmRehearseGate(ctx context.Context, turn int) {
	t := a.wmTracker()
	if t == nil {
		return
	}
	plan, ok := t.CurrentPlan()
	if !ok {
		return
	}
	report := worldmodel.Rehearse(plan, t.Graph())
	if report.Pass {
		return
	}
	detail := wmErrorPrefixRehearsal + " 计划「" + wmSummarize(plan.Goal) + "」预演未通过：" +
		strings.Join(report.MissingPreconditions, "；")
	a.wmRecordWorldModelAnomaly(ctx, turn, detail, auditActionWMRehearsalFailed)
}

// wmBackDiffCheck 接线点⑥：工具执行批次结束后调用（行动后回溯校验）。
// 对比当前计划路径与计划形成以来的实际调用轨迹（PlanTrajectory），
// 偏离写入失败库并触发审计；一致时零副作用。
func (a *ReActAgent) wmBackDiffCheck(ctx context.Context, turn int) {
	t := a.wmTracker()
	if t == nil {
		return
	}
	plan, ok := t.CurrentPlan()
	if !ok {
		return
	}
	actual := t.PlanTrajectory()
	if len(actual) == 0 {
		return
	}
	diff := worldmodel.ComparePaths(plan.Path(), actual)
	if diff.DivergedAt == "" {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s turn %d 计划「%s」与实际轨迹偏离于 %s", wmErrorPrefixBackDiff, turn, wmSummarize(plan.Goal), diff.DivergedAt)
	if len(diff.PlannedButSkipped) > 0 {
		fmt.Fprintf(&b, "；计划未执行: %s", strings.Join(diff.PlannedButSkipped, ","))
	}
	if len(diff.ExecutedButUnplanned) > 0 {
		fmt.Fprintf(&b, "；计划外执行: %s", strings.Join(diff.ExecutedButUnplanned, ","))
	}
	a.wmRecordWorldModelAnomaly(ctx, turn, b.String(), auditActionWMBackDiff)
}

// wmValidatePrediction Task 15：回溯校验——预演 vs 实际差异记录进失败库。
// 对单条工具调用的预测增量做 Validate，分歧逐条写入失败库（fire-and-forget）。
// graphBefore 为工具执行前的状态图快照；当前图由 tracker 持有（执行后状态）。
// 若无 tracker / 无预测 / 无 failureStore，安全跳过。
func (a *ReActAgent) wmValidatePrediction(ctx context.Context, turn int, predicted []worldmodel.StateDelta, graphBefore *worldmodel.StateGraph) {
	t := a.wmTracker()
	if t == nil || len(predicted) == 0 || graphBefore == nil {
		return
	}
	graphAfter := t.Graph()
	divergences := worldmodel.Validate(predicted, graphBefore, graphAfter)
	for _, d := range divergences {
		detail := wmErrorPrefixBackDiff + " " + d
		a.wmRecordWorldModelAnomaly(ctx, turn, detail, auditActionWMBackDiff)
	}
}

// wmSnapshotBeforeTool Task 15：工具执行前快照当前状态图。
// 将当前图节点快照存入 capCache.graphBeforeTool，供后续 Validate 使用。
// 同时从 capCache.lastPrediction 中取出当前索引对应的预测。
// tracker / capCache / prediction 均为 nil 时安全跳过。
func (a *ReActAgent) wmSnapshotBeforeTool() {
	t := a.wmTracker()
	if t == nil || a.capCache == nil || len(a.capCache.lastPrediction) == 0 {
		return
	}
	a.capCache.graphBeforeTool = worldmodel.GraphFromNodeIDs(t.Graph().Nodes())
}

// wmValidateAfterTool Task 15：工具执行后做回溯校验并消费预测。
// 从 capCache 取出当前索引的预测（若存在）、配合 graphBeforeTool 调用
// wmValidatePrediction；处理完毕后推进 predictionIdx 或清空预测状态。
func (a *ReActAgent) wmValidateAfterTool(ctx context.Context, turn int) {
	if a.capCache == nil {
		return
	}
	c := a.capCache
	if len(c.lastPrediction) == 0 || c.graphBeforeTool == nil {
		return
	}
	idx := c.predictionIdx
	if idx >= len(c.lastPrediction) {
		// 预测已全部消费完毕；清空状态
		c.lastPrediction = nil
		c.graphBeforeTool = nil
		c.predictionIdx = 0
		return
	}
	a.wmValidatePrediction(ctx, turn, c.lastPrediction[idx:idx+1], c.graphBeforeTool)
	c.predictionIdx++
	if c.predictionIdx >= len(c.lastPrediction) {
		// 全部预测已消费
		c.lastPrediction = nil
		c.graphBeforeTool = nil
		c.predictionIdx = 0
	}
}

// wmSaveWorldState state-checkpoint 协议（v6.1 切片三，提案 E7–E10）：
// 把世界模型快照嵌入检查点（tracker 未注入时为 no-op，检查点无 WorldState
// 字段——旧检查点/旧 agent 双向兼容）。序列化失败仅告警，不影响检查点保存。
func (a *ReActAgent) wmSaveWorldState(state *persist.AgentState) {
	t := a.wmTracker()
	if t == nil || state == nil {
		return
	}
	data, err := json.Marshal(t.Snapshot())
	if err != nil {
		a.logger.Warn("世界模型快照序列化失败", "error", err)
		return
	}
	state.WorldState = data
}

// wmRestoreWorldState state-checkpoint 协议：从检查点恢复世界模型状态。
// 「续知而非重放」——恢复路径重建 history 后不再把历史消息重放给 tracker，
// 结构化世界状态（图/计划/轨迹）由本方法一次性载入；快照损坏（校验失败）
// 时告警并以空图继续（恢复语义不因世界模型失败而阻塞主流程）。
func (a *ReActAgent) wmRestoreWorldState(state *persist.AgentState) {
	t := a.wmTracker()
	if t == nil || state == nil || len(state.WorldState) == 0 {
		return
	}
	var snap worldmodel.Snapshot
	if err := json.Unmarshal(state.WorldState, &snap); err != nil {
		a.logger.Warn("世界模型快照反序列化失败，以空图恢复", "error", err)
		return
	}
	if err := t.Restore(snap); err != nil {
		a.logger.Warn("世界模型快照校验失败，以空图恢复", "error", err)
	}
}

// wmRecordWorldModelAnomaly 世界模型异常 → 失败库 + 审计。
// fire-and-forget 语义：失败库写入失败仅告警，不影响主流程（与 writeAudit 同）。
func (a *ReActAgent) wmRecordWorldModelAnomaly(ctx context.Context, turn int, detail, auditAction string) {
	if fs := a.wmFailureStore(); fs != nil {
		rec := &persist.FailureRecord{
			ID:        fmt.Sprintf("wm-%d", time.Now().UnixNano()),
			AgentID:   a.config.Name,
			SessionID: a.config.SessionID,
			Phase:     persist.PhaseRun,
			Error:     detail,
			Turn:      turn,
			CreatedAt: time.Now(),
		}
		if err := fs.Record(ctx, rec); err != nil {
			a.logger.Warn("世界模型异常写入失败库失败", "error", err)
		}
	}
	a.writeAudit(ctx, AuditEvent{
		Actor:    a.config.Name,
		Action:   auditAction,
		Resource: a.config.Name,
		Result:   auditResultWMDetected,
		Details:  map[string]any{"turn": turn, "detail": detail},
	})
}
