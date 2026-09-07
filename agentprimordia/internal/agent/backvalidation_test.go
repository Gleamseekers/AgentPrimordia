// backvalidation_test.go — Task 15：回溯校验（预演 vs 实际）接入失败库测试
//
// 覆盖：
//   - 有预测且存在分歧：Validate 检出 → 分歧写入失败库
//   - 有预测但无分歧（预测命中）：失败库不写入
//   - 无预测：wmValidateAfterTool 安全跳过、失败库不写入
//   - failureStore 为 nil：wmValidatePrediction 不 panic（fire-and-forget）
package agent

import (
	"context"
	"strings"
	"testing"

	"agentprimordia/internal/agent/worldmodel"
	"agentprimordia/internal/persist"
	"log/slog"
)

// TestBackValidation_DivergenceRecorded 有预测且分歧时，失败库记录分歧描述。
func TestBackValidation_DivergenceRecorded(t *testing.T) {
	t.Parallel()
	tracker := worldmodel.NewWorldModelTracker()
	fs := persist.NewMemoryFailureStore()
	a := newReActAgent(ReActConfig{Name: "bv-agent", Logger: slog.Default()})
	a.capCache = &capabilityCache{
		worldTracker: tracker,
		failureStore: fs,
	}
	ctx := context.Background()

	// 预测将新增一个 tool_call 节点 "phantom tool"——该节点不会实际出现
	a.capCache.lastPrediction = []worldmodel.StateDelta{
		{Kind: worldmodel.KindToolCall, Summary: "phantom tool", Effect: "add_node"},
	}
	a.capCache.predictionIdx = 0

	// 工具执行前快照（此时图为空）
	a.wmSnapshotBeforeTool()

	// 模拟 wmObserveToolResult 实际向图添加不同节点
	tracker.Apply(worldmodel.ToolObserved{
		Turn:        0,
		ToolName:    "real_tool",
		ToolInput:   "{}",
		Observation: "real result",
	})

	// 回溯校验
	a.wmValidateAfterTool(ctx, 0)

	// 验证：分歧应记入失败库
	records, err := fs.List(ctx, "bv-agent")
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("预期 1 条失败记录，got %d", len(records))
	}
	if !strings.Contains(records[0].Error, wmErrorPrefixBackDiff) {
		t.Errorf("失败记录应含回溯差异前缀，got %q", records[0].Error)
	}
	if !strings.Contains(records[0].Error, "phantom tool") {
		t.Errorf("失败记录应含预测节点摘要，got %q", records[0].Error)
	}
	// 预测已消费：lastPrediction 应为 nil
	if a.capCache.lastPrediction != nil {
		t.Errorf("预测消费后 lastPrediction 应为 nil")
	}
}

// TestBackValidation_NoDivergence 预测命中（实际新增 = 预测）时不写失败库。
// 核心逻辑：before 图为空，工具执行后图出现预测中描述的节点 → 命中，无分歧。
func TestBackValidation_NoDivergence(t *testing.T) {
	t.Parallel()
	tracker := worldmodel.NewWorldModelTracker()
	fs := persist.NewMemoryFailureStore()
	a := newReActAgent(ReActConfig{Name: "bv-hit", Logger: slog.Default()})
	a.capCache = &capabilityCache{
		worldTracker: tracker,
		failureStore: fs,
	}
	ctx := context.Background()

	// 预测：工具执行后将新增一个 tool_call 节点 "hit_tool {}"
	a.capCache.lastPrediction = []worldmodel.StateDelta{
		{Kind: worldmodel.KindToolCall, Summary: "hit_tool {}", Effect: "add_node"},
	}
	a.capCache.predictionIdx = 0

	// 快照（此时图为空）
	a.wmSnapshotBeforeTool()

	// 工具执行：向图添加 "hit_tool {}" 节点（与预测完全匹配）
	tracker.Apply(worldmodel.ToolObserved{
		Turn:        0,
		ToolName:    "hit_tool",
		ToolInput:   "{}",
		Observation: "hit result",
	})

	// 回溯校验：预测节点已出现在实际状态图中 → 无分歧
	a.wmValidateAfterTool(ctx, 0)

	// 验证：失败库为空
	records, _ := fs.List(ctx, "bv-hit")
	if len(records) != 0 {
		t.Fatalf("预测命中时不应写失败库，got %d 条: %+v", len(records), records)
	}
	// 预测已消费
	if a.capCache.lastPrediction != nil {
		t.Errorf("预测消费后 lastPrediction 应为 nil")
	}
}

// TestBackValidation_NoPrediction 无预测时 wmValidateAfterTool 安全跳过。
func TestBackValidation_NoPrediction(t *testing.T) {
	t.Parallel()
	tracker := worldmodel.NewWorldModelTracker()
	fs := persist.NewMemoryFailureStore()
	a := newReActAgent(ReActConfig{Name: "bv-nopred", Logger: slog.Default()})
	a.capCache = &capabilityCache{
		worldTracker: tracker,
		failureStore: fs,
		// lastPrediction 为 nil
	}
	ctx := context.Background()

	// 图有内容
	tracker.Apply(worldmodel.ToolObserved{Turn: 0, ToolName: "t", ToolInput: "{}", Observation: "o"})

	// 无预测时调用——应安全跳过
	a.wmValidateAfterTool(ctx, 0)

	records, _ := fs.List(ctx, "bv-nopred")
	if len(records) != 0 {
		t.Fatalf("无预测时不应写失败库，got %d", len(records))
	}
}

// TestBackValidation_NilFailureStore failureStore 为 nil 时不 panic。
func TestBackValidation_NilFailureStore(t *testing.T) {
	t.Parallel()
	tracker := worldmodel.NewWorldModelTracker()
	a := newReActAgent(ReActConfig{Name: "bv-nilfs", Logger: slog.Default()})
	a.capCache = &capabilityCache{
		worldTracker: tracker,
		failureStore: nil, // 无失败库
	}
	ctx := context.Background()

	// 预测一个不存在的节点
	a.capCache.lastPrediction = []worldmodel.StateDelta{
		{Kind: worldmodel.KindToolCall, Summary: "ghost", Effect: "add_node"},
	}
	a.capCache.predictionIdx = 0

	a.wmSnapshotBeforeTool()
	tracker.Apply(worldmodel.ToolObserved{Turn: 0, ToolName: "other", ToolInput: "{}", Observation: "x"})

	// 不应 panic
	a.wmValidateAfterTool(ctx, 0)
}

// TestBackValidation_NilTracker tracker 为 nil 时全部操作安全跳过。
func TestBackValidation_NilTracker(t *testing.T) {
	t.Parallel()
	fs := persist.NewMemoryFailureStore()
	a := newReActAgent(ReActConfig{Name: "bv-niltracker", Logger: slog.Default()})
	a.capCache = &capabilityCache{
		worldTracker:   nil, // 无 tracker
		failureStore:   fs,
		lastPrediction: []worldmodel.StateDelta{{Kind: "task", Summary: "x", Effect: "add_node"}},
	}
	ctx := context.Background()

	a.wmSnapshotBeforeTool() // 应跳过（tracker nil）
	a.wmValidateAfterTool(ctx, 0)

	records, _ := fs.List(ctx, "bv-niltracker")
	if len(records) != 0 {
		t.Fatalf("tracker 为 nil 时不应写失败库，got %d", len(records))
	}
}
