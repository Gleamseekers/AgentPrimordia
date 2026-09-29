// worldmodel_consistency_test.go — 状态断言一致性门（v6.1 随行项，提案 §三.2）
//
// 门语义（AGENTS.md §5 修订稿）：世界模型状态更新必须与消息序列回放逐条
// 对账——本文件是 CI 回归门的落点：
//   - 回放端：仅从消息序列推导「应存在的世界事实」（计划步骤 / 工具调用 /
//     观察 / 因果配对），不经过任何接线代码；纯函数，同输入必同输出；
//   - 断言端：方向性包含——回放事实 ⊆ 状态图事实（被裁剪历史在图上保留，
//     图的超集合法；图不得缺失回放可见的任何事实，也不得与之矛盾）；
//   - 负向对照：对空图跑同一对账必须报不一致，证明门有检出能力。
package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent/worldmodel"
	"github.com/Gleamseekers/AgentPrimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/internal/persist"
	"github.com/Gleamseekers/AgentPrimordia/internal/tools"
)

// replayedFacts 消息序列回放推导的世界事实（独立于接线实现，逐条可对账）。
type replayedFacts struct {
	callIDs      []string    // 工具调用节点 ID（NodeID 派生，按消息序）
	obsIDs       []string    // 观察节点 ID（有配对前提，按消息序）
	causePairs   [][2]string // (调用节点 ID, 观察节点 ID) 位置配对
	orphanObsIDs []string    // 无配对前提的观察（E7 有损形态：assistant ToolCalls 缺失）
	lastPlanIDs  []string    // 最后一个含工具调用的 assistant 轮的步骤 ID 序
}

// replayWorldFacts 从消息序列推导世界事实。
// 纯函数：同一消息序列必得同一事实集（一致性门的可复现前提）。
func replayWorldFacts(history []Message) replayedFacts {
	var f replayedFacts
	pendingCalls := 0
	for i := range history {
		m := history[i]
		switch m.Role {
		case RoleAssistant:
			if len(m.ToolCalls) > 0 {
				pendingCalls = len(m.ToolCalls)
				f.lastPlanIDs = f.lastPlanIDs[:0]
				for _, tc := range m.ToolCalls {
					id := worldmodel.NodeID(worldmodel.KindToolCall, wmToolCallSummary(tc.Name, tc.Args))
					f.callIDs = append(f.callIDs, id)
					f.lastPlanIDs = append(f.lastPlanIDs, id)
				}
			} else {
				pendingCalls = 0
			}
		case RoleTool:
			obsID := worldmodel.NodeID(worldmodel.KindObservation, wmSummarize(m.Content))
			if pendingCalls > 0 && len(f.callIDs) > 0 {
				f.obsIDs = append(f.obsIDs, obsID)
				f.causePairs = append(f.causePairs, [2]string{f.callIDs[len(f.callIDs)-1], obsID})
				pendingCalls--
			} else {
				// E7 有损形态（检查点文本路径 ToolCalls 缺失）：观察无法配对，
				// 但观察事实本身可回放——仍须在图中存在
				f.orphanObsIDs = append(f.orphanObsIDs, obsID)
			}
		}
	}
	return f
}

// assertConsistent 状态图 vs 回放事实逐条对账，返回不一致清单（空 = 通过）。
// 图因去重/被裁历史保留产生的回放可见范围之外超集是合法的；
// 回放可见事实缺失才是失配。
func assertConsistent(g *worldmodel.StateGraph, f replayedFacts) []string {
	var bad []string
	hasCauseEdge := func(from, to string) bool {
		n, ok := g.Node(from)
		if !ok {
			return false
		}
		for _, e := range n.Edges {
			if e.To == to && e.Kind == worldmodel.EdgeCause {
				return true
			}
		}
		return false
	}
	for _, id := range f.callIDs {
		if n, ok := g.Node(id); !ok || n.Kind != worldmodel.KindToolCall {
			bad = append(bad, "回放可见的工具调用节点缺失于图: "+id)
		}
	}
	for _, id := range f.obsIDs {
		if n, ok := g.Node(id); !ok || n.Kind != worldmodel.KindObservation {
			bad = append(bad, "回放可见的观察节点缺失于图: "+id)
		}
	}
	for _, id := range f.orphanObsIDs {
		if n, ok := g.Node(id); !ok || n.Kind != worldmodel.KindObservation {
			bad = append(bad, "有损回放可见的观察节点缺失于图: "+id)
		}
	}
	for _, pair := range f.causePairs {
		if !hasCauseEdge(pair[0], pair[1]) {
			bad = append(bad, "回放可见的因果边缺失: "+pair[0]+" → "+pair[1])
		}
	}
	return bad
}

// assertPlanConsistent 当前计划与最后一个工具轮的步骤序列对账。
func assertPlanConsistent(tr *worldmodel.WorldModelTracker, lastPlanIDs []string) []string {
	plan, ok := tr.CurrentPlan()
	if !ok {
		if len(lastPlanIDs) == 0 {
			return nil
		}
		return []string{"回放存在工具调用轮，但 tracker 无当前计划"}
	}
	if len(lastPlanIDs) == 0 {
		return nil // 无工具轮的计划（如 planner 粗粒度计划）不在此门范围
	}
	got := plan.Path()
	if len(got) != len(lastPlanIDs) {
		return []string{"计划步骤数与回放不一致"}
	}
	var bad []string
	for i := range got {
		if got[i] != lastPlanIDs[i] {
			bad = append(bad, "计划步骤 "+got[i]+" 与回放步骤 "+lastPlanIDs[i]+" 不一致")
		}
	}
	return bad
}

// TestWorldModelConsistencyGate_ReplayReconciliation 一致性门主用例：
// 两轮工具任务（含上下文裁剪）后，从真实检查点消息序列（文本有损路径：
// ToolCalls 不落盘，观察内容保留——E7 债务在观察侧的对账不受影响）回放，
// 与状态图逐条对账全通过；计划与最后一个工具轮的步骤序列收敛。
func TestWorldModelConsistencyGate_ReplayReconciliation(t *testing.T) {
	t.Parallel()
	cstore, err := persist.InMemoryCheckpointStore()
	if err != nil {
		t.Fatalf("InMemoryCheckpointStore 失败: %v", err)
	}
	mock := llm.NewMockLLM(t).
		WithToolResponse([]llm.FunctionCall{
			{ID: "c1", Name: "get_time", Arguments: `{"tz":"utc"}`},
		}).
		WithToolResponse([]llm.FunctionCall{
			{ID: "c2", Name: "get_time", Arguments: `{"tz":"pst"}`},
		}).
		WithResponse("两次时间都拿到了，完成")

	registry := tools.NewRegistry()
	_ = registry.Register(&mockTimeTool{name: "get_time"})

	agent, tracker, _, _ := newWMAgent(t, mock,
		WithToolkit(registry),
		WithCheckpointStore(cstore),
		WithContextWindow(trimKeepLast{keepLast: 6}), // 保持消息完整的同时允许裁剪触发
	)
	if _, err := agent.Run(context.Background(), UserMessage("查两次时间")); err != nil {
		t.Fatalf("运行失败: %v", err)
	}

	// 回放输入 1：检查点消息序列（持久化的文本路径，E7 有损形态——
	// 只有 Role/Content，ToolCalls 不在；观察侧事实完整保留）
	state, err := cstore.Load(context.Background(), "wm-agent")
	if err != nil {
		t.Fatalf("加载检查点失败: %v", err)
	}
	var lossyHistory []Message
	for _, m := range state.Messages {
		lossyHistory = append(lossyHistory, Message{Role: Role(m.Role), Content: m.Content})
	}
	facts := replayWorldFacts(lossyHistory)
	if len(facts.orphanObsIDs) != 2 {
		t.Fatalf("有损回放应检出两次孤儿观察（实际 %d），观察内容应在检查点文本路径完整保留", len(facts.orphanObsIDs))
	}
	if len(facts.callIDs) != 0 {
		t.Fatalf("有损回放不应推导出调用节点（ToolCalls 缺失），got %d", len(facts.callIDs))
	}
	if bad := assertConsistent(tracker.Graph(), facts); len(bad) > 0 {
		t.Fatalf("有损回放与状态图对账失败（%d 处）：\n%s", len(bad), strings.Join(bad, "\n"))
	}

	// 回放输入 2：规范化全量历史（补回 E7 丢失的 ToolCalls——即 state-checkpoint
	// 协议之后「续知」载体应有的形态），计划侧对账在此口径上进行
	fullHistory := make([]Message, 0, len(lossyHistory))
	toolCallsByTurn := [][]ToolCall{
		{{ID: "c1", Name: "get_time", Args: `{"tz":"utc"}`}},
		{{ID: "c2", Name: "get_time", Args: `{"tz":"pst"}`}},
	}
	turn := -1
	for _, m := range lossyHistory {
		if m.Role == RoleAssistant {
			turn++
			if turn < len(toolCallsByTurn) && len(toolCallsByTurn[turn]) > 0 {
				m.ToolCalls = toolCallsByTurn[turn]
			}
		}
		fullHistory = append(fullHistory, m)
	}
	fullFacts := replayWorldFacts(fullHistory)
	if bad := assertConsistent(tracker.Graph(), fullFacts); len(bad) > 0 {
		t.Fatalf("全量回放与状态图对账失败（%d 处）：\n%s", len(bad), strings.Join(bad, "\n"))
	}
	if bad := assertPlanConsistent(tracker, fullFacts.lastPlanIDs); len(bad) > 0 {
		t.Fatalf("计划与回放对账失败：%v", bad)
	}
}

// TestWorldModelConsistencyGate_NegativeControl 负向对照：
// 空图对账必须检出全部三类不一致，证明门有检出能力（非恒真断言）
func TestWorldModelConsistencyGate_NegativeControl(t *testing.T) {
	t.Parallel()
	empty := worldmodel.NewStateGraph()
	facts := replayedFacts{
		callIDs: []string{worldmodel.NodeID(worldmodel.KindToolCall, "t x")},
		obsIDs:  []string{worldmodel.NodeID(worldmodel.KindObservation, "o")},
	}
	facts.causePairs = append(facts.causePairs, [2]string{facts.callIDs[0], facts.obsIDs[0]})
	bad := assertConsistent(empty, facts)
	if len(bad) != 3 {
		t.Fatalf("空图对账应检出 3 处不一致（调用/观察/因果），got %d: %v", len(bad), bad)
	}

	// 计划侧负向对照：回放有工具轮而 tracker 无计划
	if bad := assertPlanConsistent(worldmodel.NewWorldModelTracker(), facts.callIDs); len(bad) == 0 {
		t.Fatal("无计划 tracker 的计划对账应报不一致")
	}
}
