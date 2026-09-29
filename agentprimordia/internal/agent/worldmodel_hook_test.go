// worldmodel_hook_test.go — 世界模型接线层测试（v6.1 第二切片）
//
// 覆盖：
//   - 默认路径零变化：不注入 tracker 时发现入口为 nil（铁律 7 / 提案 §2.1）；
//   - opt-in 端到端：工具调用/观察/假设落图，计划步骤与调用节点 ID 收敛；
//   - 上下文裁剪 → observation 事实节点（接线点④）；
//   - 预演门 / 回溯差异 → 失败库 + 审计（接线点⑤⑥，观察模式）；
//   - planner 粗粒度计划落图与组建期预演（接线点② planner 分支）；
//   - 注入链：NewAgent 选项与 ReActAgent 链式 API 两条入口。
package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/planning"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/worldmodel"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/persist"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools"
	"log/slog"
)

// recordingAuditLogger 审计事件记录器（接线测试用）
type recordingAuditLogger struct {
	events []AuditEvent
}

func (r *recordingAuditLogger) Log(_ context.Context, e AuditEvent) error {
	r.events = append(r.events, e)
	return nil
}

// trimKeepLast 裁剪策略：只保留最后 keepLast 条（头部丢弃，触发接线点④）
type trimKeepLast struct{ keepLast int }

func (s trimKeepLast) Trim(messages []Message, _ int) []Message {
	if len(messages) <= s.keepLast {
		return messages
	}
	return messages[len(messages)-s.keepLast:]
}

// newWMAgent 构造带世界模型的 agent + tracker + 失败库 + 审计记录器
func newWMAgent(t *testing.T, mock llm.Provider, opts ...Option) (*CapabilityAgent, *worldmodel.WorldModelTracker, *persist.MemoryFailureStore, *recordingAuditLogger) {
	t.Helper()
	tracker := worldmodel.NewWorldModelTracker()
	fs := persist.NewMemoryFailureStore()
	audit := &recordingAuditLogger{}
	base := []Option{WithMaxTurns(10), WithWorldModel(tracker), WithFailureStore(fs)}
	base = append(base, opts...)
	agent, err := NewAgent("wm-agent", "", mock, base...)
	if err != nil {
		t.Fatalf("创建 agent 失败: %v", err)
	}
	// CapabilityAgent 链上追加审计记录器（同一包装器内变更，不丢失既有能力）
	return agent.WithAuditLogger(audit), tracker, fs, audit
}

// TestWorldModel_DefaultDisabled 默认不注入：发现入口为 nil（铁律 7）
func TestWorldModel_DefaultDisabled(t *testing.T) {
	t.Parallel()
	agent, err := NewAgent("plain-agent", "", llm.NewMockLLM(t).WithResponse("ok"), WithMaxTurns(5))
	if err != nil {
		t.Fatalf("创建 agent 失败: %v", err)
	}
	if got := agent.inner.getWorldModelTracker(); got != nil {
		t.Fatalf("默认构造不应启用世界模型，got %v", got)
	}
	if got := agent.inner.wmTracker(); got != nil {
		t.Fatalf("wmTracker 应为 nil，got %v", got)
	}
}

// TestWorldModel_ChainAPIInjection 链式入口：ReActAgent.WithWorldModel 后可发现
func TestWorldModel_ChainAPIInjection(t *testing.T) {
	t.Parallel()
	tracker := worldmodel.NewWorldModelTracker()
	a := newReActAgent(ReActConfig{Name: "chain-agent", Logger: slog.Default()})
	_ = a.WithWorldModel(tracker)
	if got := a.getWorldModelTracker(); got != tracker {
		t.Fatalf("链式注入后应可发现 tracker，got %v", got)
	}
}

// TestWorldModel_OptIn_ObservesToolEvents 端到端：工具轮落图 + ID 收敛 + 因果边
func TestWorldModel_OptIn_ObservesToolEvents(t *testing.T) {
	t.Parallel()
	mock := llm.NewMockLLM(t).
		WithToolResponse([]llm.FunctionCall{{ID: "call_1", Name: "get_time", Arguments: `{"tz":"utc"}`}}).
		WithResponse("现在是 12:00 PM。")

	registry := tools.NewRegistry()
	_ = registry.Register(&mockTimeTool{name: "get_time"})

	agent, tracker, fs, audit := newWMAgent(t, mock, WithToolkit(registry))

	resp, err := agent.Run(context.Background(), UserMessage("现在几点？"))
	if err != nil || resp.Error != nil {
		t.Fatalf("运行失败: %v / %v", err, resp.Error)
	}

	// 接线点②：计划（重新）形成，步骤与实际调用收敛到同一节点 ID
	plan, ok := tracker.CurrentPlan()
	if !ok {
		t.Fatal("工具轮后应有当前计划")
	}
	if len(plan.Steps) != 1 {
		t.Fatalf("计划步骤数应为 1，got %d", len(plan.Steps))
	}
	wantStepID := worldmodel.NodeID(worldmodel.KindToolCall, "get_time {\"tz\":\"utc\"}")
	if plan.Steps[0].ID != wantStepID {
		t.Fatalf("计划步骤 ID 应为 NodeID 派生值：%s != %s", plan.Steps[0].ID, wantStepID)
	}

	// 接线点③：因果链 tool_call → observation
	g := tracker.Graph()
	callNode, ok := g.Node(wantStepID)
	if !ok {
		t.Fatalf("调用节点应落图: %s", wantStepID)
	}
	if len(callNode.Edges) != 1 || callNode.Edges[0].Kind != worldmodel.EdgeCause {
		t.Fatalf("调用节点应恰有一条 cause 出边，got %+v", callNode.Edges)
	}
	obsNode, ok := g.Node(callNode.Edges[0].To)
	if !ok || obsNode.Kind != worldmodel.KindObservation {
		t.Fatalf("观察节点应落图且为 observation 类型，got %+v", obsNode)
	}
	if !strings.Contains(obsNode.Summary, "12:00 PM") {
		t.Fatalf("观察摘要应含工具结果，got %q", obsNode.Summary)
	}

	// 回溯一致：预演门/回溯差异均不应产生世界模型失败记录与审计事件
	records, _ := fs.List(context.Background(), "wm-agent")
	if len(records) != 0 {
		t.Fatalf("一致轨迹不应写失败库，got %+v", records)
	}
	if wmAuditCount(audit) != 0 {
		t.Fatalf("一致轨迹不应写世界模型审计事件，got %+v", audit.events)
	}
}

// wmAuditCount 统计世界模型动作的审计事件数
func wmAuditCount(audit *recordingAuditLogger) int {
	n := 0
	for _, e := range audit.events {
		if e.Action == auditActionWMRehearsalFailed || e.Action == auditActionWMBackDiff {
			n++
		}
	}
	return n
}

// TestWorldModel_ObserveAssistant_Hypothesis 接线点②：思考文本 → 假设节点；
// 空思考文本（纯工具调用）不产生假设节点（MockLLM 工具轮 Content 为空即此形态）
func TestWorldModel_ObserveAssistant_Hypothesis(t *testing.T) {
	t.Parallel()
	tracker := worldmodel.NewWorldModelTracker()
	a := newReActAgent(ReActConfig{Name: "hyp-agent", Logger: slog.Default()})
	a.capCache = &capabilityCache{worldTracker: tracker}

	stepID := worldmodel.NodeID(worldmodel.KindToolCall, "get_time {}")
	a.wmObserveAssistant(0, Thought{
		Content:   "我需要先查询时间",
		ToolCalls: []ToolCall{{ID: "c1", Name: "get_time", Args: "{}"}},
	})

	// 计划 + 假设同时落图
	plan, ok := tracker.CurrentPlan()
	if !ok || len(plan.Steps) != 1 || plan.Steps[0].ID != stepID {
		t.Fatalf("应有单步计划且 ID 收敛，ok=%v plan=%+v", ok, plan)
	}
	g := tracker.Graph()
	var hypNode *worldmodel.StateNode
	for i, n := range g.Nodes() {
		if n.Kind == worldmodel.KindHypothesis {
			hypNode = &g.Nodes()[i]
		}
	}
	if hypNode == nil || hypNode.Summary != "我需要先查询时间" {
		t.Fatalf("思考文本应产生假设节点，got %+v", hypNode)
	}

	// 空思考文本：只有计划，无假设节点
	tracker2 := worldmodel.NewWorldModelTracker()
	a.capCache = &capabilityCache{worldTracker: tracker2}
	a.wmObserveAssistant(1, Thought{ToolCalls: []ToolCall{{ID: "c2", Name: "t", Args: "x"}}})
	for _, n := range tracker2.Graph().Nodes() {
		if n.Kind == worldmodel.KindHypothesis {
			t.Fatal("空思考文本不应产生假设节点")
		}
	}
}

// TestWorldModel_TrimNotificationToGraph 接线点④：被裁消息 → observation 事实节点
func TestWorldModel_TrimNotificationToGraph(t *testing.T) {
	t.Parallel()
	mock := llm.NewMockLLM(t).
		WithToolResponse([]llm.FunctionCall{{ID: "c1", Name: "get_time", Arguments: "{}"}}).
		WithResponse("完成")

	registry := tools.NewRegistry()
	_ = registry.Register(&mockTimeTool{name: "get_time"})

	agent, tracker, _, _ := newWMAgent(t, mock, WithToolkit(registry), WithContextWindow(trimKeepLast{keepLast: 1}))
	if _, err := agent.Run(context.Background(), UserMessage("上下文裁剪标记消息")); err != nil {
		t.Fatalf("运行失败: %v", err)
	}

	g := tracker.Graph()
	var foundTrimmed bool
	for _, n := range g.Nodes() {
		if n.Kind == worldmodel.KindObservation && strings.Contains(n.Summary, "上下文裁剪标记消息") {
			foundTrimmed = true
		}
	}
	if !foundTrimmed {
		t.Fatal("被裁剪的用户消息应转为 observation 事实节点")
	}
}

// TestWorldModel_RehearseGateRecordsMissingPrecondition 接线点⑤：预演缺陷 → 失败库+审计
func TestWorldModel_RehearseGateRecordsMissingPrecondition(t *testing.T) {
	t.Parallel()
	tracker := worldmodel.NewWorldModelTracker()
	// 步骤依赖一个既非计划内前序步骤、也不在状态图中的节点 → 预演缺陷
	tracker.Apply(worldmodel.PlanRevised{
		Turn: 1, Goal: "g",
		Steps: []worldmodel.PlanStep{{
			ID:        worldmodel.NodeID(worldmodel.KindToolCall, "t x"),
			Summary:   "t x",
			DependsOn: []string{"ghost-node"},
		}},
	})

	fs := persist.NewMemoryFailureStore()
	audit := &recordingAuditLogger{}
	a := newReActAgent(ReActConfig{Name: "gate-agent", Logger: slog.Default()})
	a.capCache = &capabilityCache{worldTracker: tracker, failureStore: fs, auditLogger: audit}

	a.wmRehearseGate(context.Background(), 1)

	records, err := fs.List(context.Background(), "gate-agent")
	if err != nil || len(records) != 1 {
		t.Fatalf("预演缺陷应写一条失败记录，err=%v records=%d", err, len(records))
	}
	if !strings.Contains(records[0].Error, wmErrorPrefixRehearsal) || !strings.Contains(records[0].Error, "ghost-node") {
		t.Fatalf("失败记录应含预演门标签与缺失前提，got %q", records[0].Error)
	}
	if len(audit.events) != 1 || audit.events[0].Action != auditActionWMRehearsalFailed {
		t.Fatalf("应写一条预演门审计事件，got %+v", audit.events)
	}
}

// TestWorldModel_BackDiffRecordsDivergence 接线点⑥：轨迹偏离 → 失败库+审计；一致 → 零副作用
func TestWorldModel_BackDiffRecordsDivergence(t *testing.T) {
	t.Parallel()
	tracker := worldmodel.NewWorldModelTracker()
	stepA := worldmodel.NodeID(worldmodel.KindToolCall, "search q=1")
	stepB := worldmodel.NodeID(worldmodel.KindToolCall, "fetch url=2")
	tracker.Apply(worldmodel.PlanRevised{Turn: 1, Goal: "g", Steps: []worldmodel.PlanStep{
		{ID: stepA, Summary: "search q=1"},
		{ID: stepB, Summary: "fetch url=2"},
	}})
	// 第二步参数被替换 → 偏离
	tracker.Apply(worldmodel.ToolObserved{Turn: 1, ToolName: "search", ToolInput: "q=1", Observation: "r1"})
	tracker.Apply(worldmodel.ToolObserved{Turn: 1, ToolName: "fetch", ToolInput: "url=3", Observation: "r3"})

	fs := persist.NewMemoryFailureStore()
	audit := &recordingAuditLogger{}
	a := newReActAgent(ReActConfig{Name: "bd-agent", Logger: slog.Default()})
	a.capCache = &capabilityCache{worldTracker: tracker, failureStore: fs, auditLogger: audit}

	a.wmBackDiffCheck(context.Background(), 1)

	records, _ := fs.List(context.Background(), "bd-agent")
	if len(records) != 1 {
		t.Fatalf("偏离应写一条失败记录，got %d", len(records))
	}
	if !strings.Contains(records[0].Error, wmErrorPrefixBackDiff) {
		t.Fatalf("失败记录应含回溯差异标签，got %q", records[0].Error)
	}
	if len(audit.events) != 1 || audit.events[0].Action != auditActionWMBackDiff {
		t.Fatalf("应写一条回溯差异审计事件，got %+v", audit.events)
	}

	// 一致场景：无新事件
	tracker.Apply(worldmodel.PlanRevised{Turn: 2, Goal: "g", Steps: []worldmodel.PlanStep{{ID: stepA, Summary: "search q=1"}}})
	tracker.Apply(worldmodel.ToolObserved{Turn: 2, ToolName: "search", ToolInput: "q=1", Observation: "r1"})
	a.wmBackDiffCheck(context.Background(), 2)
	records2, _ := fs.List(context.Background(), "bd-agent")
	if len(records2) != 1 {
		t.Fatalf("一致轨迹不应新增失败记录，got %d", len(records2))
	}
	if len(audit.events) != 1 {
		t.Fatalf("一致轨迹不应新增审计事件，got %d", len(audit.events))
	}
}

// TestWorldModel_ObservePlan_PlannerCoarse 接线点② planner 分支：粗粒度计划落图 + 组建期预演
func TestWorldModel_ObservePlan_PlannerCoarse(t *testing.T) {
	t.Parallel()
	tracker := worldmodel.NewWorldModelTracker()
	fs := persist.NewMemoryFailureStore()
	audit := &recordingAuditLogger{}
	a := newReActAgent(ReActConfig{Name: "plan-agent", Logger: slog.Default()})
	a.capCache = &capabilityCache{worldTracker: tracker, failureStore: fs, auditLogger: audit}

	plan := &planning.Plan{
		Goal: "两步任务",
		SubTasks: []planning.SubTask{
			{ID: "1", Description: "第一步：读取数据"},
			{ID: "2", Description: "第二步：写入结果", DependsOn: []string{"1"}},
			{ID: "3", Description: "第三步：依赖未知节点", DependsOn: []string{"ghost"}},
		},
	}
	a.wmObservePlan(context.Background(), 0, "用户任务输入", plan)

	g := tracker.Graph()
	var hasTask, hasPlan bool
	for _, n := range g.Nodes() {
		switch n.Kind {
		case worldmodel.KindTask:
			if n.Summary == "用户任务输入" {
				hasTask = true
			}
		case worldmodel.KindPlan:
			if n.Summary == "两步任务" {
				hasPlan = true
			}
		}
	}
	if !hasTask || !hasPlan {
		t.Fatalf("任务/计划节点应落图：task=%v plan=%v", hasTask, hasPlan)
	}

	// 步骤 ID 用 NodeID 派生（与后续工具观测同一 ID 空间）
	planGot, ok := tracker.CurrentPlan()
	if !ok || len(planGot.Steps) != 3 {
		t.Fatalf("当前计划应含 3 步，ok=%v steps=%d", ok, len(planGot.Steps))
	}
	wantFirst := worldmodel.NodeID(worldmodel.KindToolCall, "第一步：读取数据")
	if planGot.Steps[0].ID != wantFirst {
		t.Fatalf("步骤 ID 应为 NodeID 派生值：%s != %s", planGot.Steps[0].ID, wantFirst)
	}
	// 计划内依赖映射为步骤节点 ID
	if planGot.Steps[1].DependsOn[0] != wantFirst {
		t.Fatalf("计划内依赖应映射为步骤 ID，got %v", planGot.Steps[1].DependsOn)
	}

	// 组建期预演：第三步依赖 ghost → 预演缺陷入失败库
	records, _ := fs.List(context.Background(), "plan-agent")
	if len(records) != 1 || !strings.Contains(records[0].Error, wmErrorPrefixRehearsal) {
		t.Fatalf("组建期预演缺陷应写失败库，got %+v", records)
	}
}

// TestWorldModel_NilTrackerHooksAreNoOp tracker 为 nil 时全部挂钩安全短路
func TestWorldModel_NilTrackerHooksAreNoOp(t *testing.T) {
	t.Parallel()
	a := newReActAgent(ReActConfig{Name: "nil-wm", Logger: slog.Default()})
	a.capCache = &capabilityCache{}

	ctx := context.Background()
	a.wmObserveAssistant(0, Thought{Content: "x"})
	a.wmObserveToolResult(0, &ToolCall{Name: "t"}, &ToolResult{Content: "r"}, nil)
	a.wmNotifyTrimmed([]Message{UserMessage("a")}, nil, 0)
	a.wmRehearseGate(ctx, 0)
	a.wmBackDiffCheck(ctx, 0)
	a.wmObservePlan(ctx, 0, "task", &planning.Plan{Goal: "g", SubTasks: []planning.SubTask{{ID: "1", Description: "d"}}})
}

// TestWorldModel_SummarizeDeterministic 摘要确定性：截断 + 首尾空白处理
func TestWorldModel_SummarizeDeterministic(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("长", wmSummaryMaxRunes+10)
	got := wmSummarize("  " + long + "  ")
	if got != wmSummarize(strings.TrimSpace(long)) {
		t.Fatal("仅首尾空白差异应得同一摘要")
	}
	if want := strings.Repeat("长", wmSummaryMaxRunes) + "…"; got != want {
		t.Fatalf("超长摘要应确定性截断，got len=%d", len([]rune(got)))
	}
	if wmSummarize(errors.New("boom").Error()) != "boom" {
		t.Fatal("短摘要应原样保留")
	}
}
