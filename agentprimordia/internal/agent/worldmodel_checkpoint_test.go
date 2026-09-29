// worldmodel_checkpoint_test.go — state-checkpoint 协议测试（v6.1 切片三）
//
// 覆盖：
//   - 续知端到端：崩溃前工具观察落图 → 检查点携带世界快照 → 全新 agent +
//     全新 tracker 恢复后世界状态在场（工具未被重放，节点只能来自快照）；
//   - 默认路径兼容：无 tracker 的检查点不含 WorldState（旧检查点双向兼容）；
//   - 恢复鲁棒性：快照损坏/为空时不 panic，恢复语义退化为 v6.0 重放路径。
package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/worldmodel"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/persist"
)

// TestWorldModel_StateCheckpoint_ContinueKnowing 续知端到端：
// 崩溃前世界状态经失败记录内嵌检查点恢复到全新 agent + 全新 tracker——
// 工具未被重放，事实已在场（「续知而非重放」，提案 E7–E10）
func TestWorldModel_StateCheckpoint_ContinueKnowing(t *testing.T) {
	t.Parallel()
	cstore, err := persist.InMemoryCheckpointStore()
	if err != nil {
		t.Fatalf("InMemoryCheckpointStore 失败: %v", err)
	}
	fstore := persist.NewMemoryFailureStore()

	// 第一段：工具调用一轮后 LLM 永久失败 → 检查点（含世界快照）落盘 + 失败记录
	trackerA := worldmodel.NewWorldModelTracker()
	agA, err := NewAgent("wm-ckpt-agent", "助手", &seqProvider{},
		WithMaxTurns(5),
		WithToolkit(newEchoRegistry(t)),
		WithCheckpointStore(cstore),
		WithFailureStore(fstore),
		WithWorldModel(trackerA),
	)
	if err != nil {
		t.Fatalf("NewAgent A 失败: %v", err)
	}
	if _, runErr := agA.Run(context.Background(), UserMessage("echo 一次然后崩")); runErr == nil {
		t.Fatal("期望第一段运行失败")
	}
	// 崩溃前世界事实：echo 调用 → 观察因果链
	wantCallID := worldmodel.NodeID(worldmodel.KindToolCall, "echo {}")
	if _, ok := trackerA.Graph().Node(wantCallID); !ok {
		t.Fatal("前置条件：崩溃前 echo 调用节点应在 trackerA 图中")
	}
	recs, err := fstore.List(context.Background(), "wm-ckpt-agent")
	if err != nil || len(recs) != 1 {
		t.Fatalf("期望 1 条失败记录，实际 %d（err=%v）", len(recs), err)
	}
	if recs[0].State == nil || len(recs[0].State.WorldState) == 0 {
		t.Fatal("失败记录内嵌检查点应携带世界快照")
	}

	// 第二段：全新 agent + 全新 tracker（空图）从失败记录一键重放
	trackerB := worldmodel.NewWorldModelTracker()
	agB, err := NewAgent("wm-ckpt-agent", "助手", llm.NewMockLLM(t).WithResponse("恢复后完成"),
		WithMaxTurns(5),
		WithToolkit(newEchoRegistry(t)),
		WithCheckpointStore(cstore),
		WithFailureStore(fstore),
		WithWorldModel(trackerB),
	)
	if err != nil {
		t.Fatalf("NewAgent B 失败: %v", err)
	}
	if got := len(trackerB.Graph().Nodes()); got != 0 {
		t.Fatalf("恢复前 trackerB 应为空图，got %d", got)
	}

	resp, err := agB.Inner().ReplayFailure(context.Background(), recs[0].ID)
	if err != nil {
		t.Fatalf("ReplayFailure 失败: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("恢复运行不应有错误: %v", resp.Error)
	}
	if resp.Content != "恢复后完成" {
		t.Fatalf("恢复后应完成最终回答，got %q", resp.Content)
	}

	// 续知断言：echo 调用与观察节点在场，但工具并未被重放
	//（恢复后的 LLM 直接返回最终回答，无工具调用——节点只能来自快照）
	callNode, ok := trackerB.Graph().Node(wantCallID)
	if !ok {
		t.Fatal("恢复后 echo 调用节点应在场（续知而非重放）")
	}
	if len(callNode.Edges) != 1 || callNode.Edges[0].Kind != worldmodel.EdgeCause {
		t.Fatalf("恢复后因果边应在场，got %+v", callNode.Edges)
	}
	obsNode, ok := trackerB.Graph().Node(callNode.Edges[0].To)
	if !ok || obsNode.Kind != worldmodel.KindObservation {
		t.Fatalf("恢复后观察节点应在场，got %+v", obsNode)
	}
}

// TestWorldModel_StateCheckpoint_DefaultNoWorldState 默认路径兼容：
// 未注入 tracker 时检查点不含 WorldState 字段
func TestWorldModel_StateCheckpoint_DefaultNoWorldState(t *testing.T) {
	t.Parallel()
	a := newReActAgent(ReActConfig{Name: "no-wm-ckpt", Logger: slog.Default()})
	a.capCache = &capabilityCache{}

	state := &persist.AgentState{AgentID: "no-wm-ckpt"}
	a.wmSaveWorldState(state)
	if len(state.WorldState) != 0 {
		t.Fatalf("无 tracker 时不应写 WorldState，got %s", state.WorldState)
	}

	// 无 WorldState 的恢复同样 no-op
	tracker := worldmodel.NewWorldModelTracker()
	a.capCache = &capabilityCache{worldTracker: tracker}
	a.wmRestoreWorldState(state)
	if got := len(tracker.Graph().Nodes()); got != 0 {
		t.Fatalf("无快照恢复不应改动 tracker，got %d 节点", got)
	}
}

// TestWorldModel_StateCheckpoint_CorruptSnapshot 鲁棒性：快照损坏不 panic，
// tracker 保持可用（恢复语义退化为 v6.0 重放路径）
func TestWorldModel_StateCheckpoint_CorruptSnapshot(t *testing.T) {
	t.Parallel()
	tracker := worldmodel.NewWorldModelTracker()
	a := newReActAgent(ReActConfig{Name: "corrupt-wm", Logger: slog.Default()})
	a.capCache = &capabilityCache{worldTracker: tracker}

	a.wmRestoreWorldState(&persist.AgentState{WorldState: []byte("{not-json")})
	a.wmRestoreWorldState(&persist.AgentState{WorldState: []byte(`{"nodes":[{"id":"","kind":"task"}]}`)})
	if got := len(tracker.Graph().Nodes()); got != 0 {
		t.Fatalf("损坏快照恢复后 tracker 应仍为空图，got %d", got)
	}
	// 恢复后 tracker 继续可用
	tracker.Apply(worldmodel.ToolObserved{Turn: 1, ToolName: "t", ToolInput: "x", Observation: "o"})
	if got := len(tracker.Graph().Nodes()); got != 2 {
		t.Fatalf("损坏快照恢复后 tracker 应可继续增量应用，got %d 节点", got)
	}
}

// TestWorldModel_StateCheckpoint_StoreRoundTrip 检查点存储层往返：
// WorldState 经 InMemoryCheckpointStore 落盘/加载后字节等价
func TestWorldModel_StateCheckpoint_StoreRoundTrip(t *testing.T) {
	t.Parallel()
	cstore, err := persist.InMemoryCheckpointStore()
	if err != nil {
		t.Fatalf("InMemoryCheckpointStore 失败: %v", err)
	}
	a := newReActAgent(ReActConfig{Name: "wm-store", Logger: slog.Default()})
	a.capCache = &capabilityCache{worldTracker: worldmodel.NewWorldModelTracker()}
	a.capCache.worldTracker.Apply(worldmodel.ToolObserved{Turn: 1, ToolName: "t", ToolInput: "x", Observation: "o"})

	state := &persist.AgentState{AgentID: "wm-store", Status: "paused"}
	a.wmSaveWorldState(state)
	if len(state.WorldState) == 0 {
		t.Fatal("有 tracker 时应写 WorldState")
	}
	if err := cstore.Save(context.Background(), state); err != nil {
		t.Fatalf("检查点保存失败: %v", err)
	}
	loaded, err := cstore.Load(context.Background(), "wm-store")
	if err != nil {
		t.Fatalf("检查点加载失败: %v", err)
	}
	if string(loaded.WorldState) != string(state.WorldState) {
		t.Fatalf("WorldState 存储往返应字节等价:\n%s\n%s", state.WorldState, loaded.WorldState)
	}

	// 反序列化→Restore 闭环
	var snap worldmodel.Snapshot
	if err := json.Unmarshal(loaded.WorldState, &snap); err != nil {
		t.Fatalf("快照反序列化失败: %v", err)
	}
	dst := worldmodel.NewWorldModelTracker()
	if err := dst.Restore(snap); err != nil {
		t.Fatalf("Restore 失败: %v", err)
	}
	if _, ok := dst.Graph().Node(worldmodel.NodeID(worldmodel.KindToolCall, "t x")); !ok {
		t.Fatal("存储往返后调用节点应在场")
	}
}
