// predict_test.go — LLM-as 状态预演测试（TDD 四用例：正常解析 / LLM 错误 / 分歧检测 / 无分歧）
package worldmodel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

// mockCompleter 模拟 LLM——固定返回预设响应或错误。
type mockCompleter struct {
	response string
	err      error
}

func (m *mockCompleter) Complete(_ context.Context, _ string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.response, nil
}

// TestPredictPrompt_GeneratesDelta 验证完整流程：
// mock LLM 返回合法 JSON → PredictStateDeltas 正确解析为 [][]StateDelta。
func TestPredictPrompt_GeneratesDelta(t *testing.T) {
	// 构造状态图（含一个既有节点，使 prompt 非空）
	graph := NewStateGraph()
	graph.AddNode(KindTask, "整理任务", 1)

	// mock LLM 返回两候选动作的增量：第一候选产生 1 个 add_node，第二候选产生 2 个
	deltas := [][]StateDelta{
		{{Kind: KindToolCall, Summary: "读取文件 main.go", Effect: "add_node"}},
		{
			{Kind: KindObservation, Summary: "文件存在", Effect: "add_node"},
			{Kind: KindHypothesis, Summary: "假设缓存命中", Effect: "add_node"},
		},
	}
	respBytes, _ := json.Marshal(deltas)
	mock := &mockCompleter{response: string(respBytes)}

	result, err := PredictStateDeltas(context.Background(), mock, graph, []string{"读取 main.go", "检查缓存"})
	if err != nil {
		t.Fatalf("PredictStateDeltas 不应返回错误: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("期望 2 组增量，实际 %d", len(result))
	}
	if len(result[0]) != 1 {
		t.Fatalf("第一候选期望 1 条增量，实际 %d", len(result[0]))
	}
	if len(result[1]) != 2 {
		t.Fatalf("第二候选期望 2 条增量，实际 %d", len(result[1]))
	}
	// 验证第一条增量内容
	d := result[0][0]
	if d.Kind != KindToolCall || d.Summary != "读取文件 main.go" || d.Effect != "add_node" {
		t.Errorf("增量内容不匹配: %+v", d)
	}
}

// TestPredictPrompt_LLMError 验证 LLM 返回错误时错误正确传播。
func TestPredictPrompt_LLMError(t *testing.T) {
	graph := NewStateGraph()
	expectedErr := errors.New("LLM 服务不可用")
	mock := &mockCompleter{err: expectedErr}

	_, err := PredictStateDeltas(context.Background(), mock, graph, []string{"候选动作"})
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("期望错误链包含原始错误，实际: %v", err)
	}
}

// TestValidate_DetectsDivergence 预测的新增节点未在实际图中出现 → 应报告分歧。
func TestValidate_DetectsDivergence(t *testing.T) {
	before := NewStateGraph()
	before.AddNode(KindTask, "整理任务", 1)

	// after 图：没有新增节点（模拟动作执行后状态未变化）
	after := NewStateGraph()
	after.AddNode(KindTask, "整理任务", 1)

	// 预测会产生一个新节点，但实际并未出现
	predicted := []StateDelta{
		{Kind: KindToolCall, Summary: "读取文件 main.go", Effect: "add_node"},
	}

	divergences := Validate(predicted, before, after)
	if len(divergences) != 1 {
		t.Fatalf("期望 1 条分歧，实际 %d: %v", len(divergences), divergences)
	}
	// 分歧描述应包含节点种类和摘要
	expected := fmt.Sprintf("预测新增节点 [tool_call] 读取文件 main.go 未在实际状态图中出现")
	if divergences[0] != expected {
		t.Errorf("分歧描述不匹配:\n期望: %s\n实际: %s", expected, divergences[0])
	}
}

// TestValidate_NoDivergence 预测的新增节点确实在 after 图中出现 → 无分歧。
func TestValidate_NoDivergence(t *testing.T) {
	before := NewStateGraph()
	before.AddNode(KindTask, "整理任务", 1)

	// after 图：多了一个 tool_call 节点
	after := NewStateGraph()
	after.AddNode(KindTask, "整理任务", 1)
	after.AddNode(KindToolCall, "读取文件 main.go", 2)

	// 预测的增量与实际变化一致
	predicted := []StateDelta{
		{Kind: KindToolCall, Summary: "读取文件 main.go", Effect: "add_node"},
	}

	divergences := Validate(predicted, before, after)
	if len(divergences) != 0 {
		t.Errorf("期望无分歧，实际 %d 条: %v", len(divergences), divergences)
	}
}

// TestPredictPrompt_Deterministic 验证 PredictPrompt 的确定性：相同输入必得相同输出。
func TestPredictPrompt_Deterministic(t *testing.T) {
	graph := NewStateGraph()
	graph.AddNode(KindTask, "任务A", 1)
	nodes := graph.Nodes()
	candidates := []string{"动作1", "动作2"}

	p1 := PredictPrompt(nodes, candidates)
	p2 := PredictPrompt(nodes, candidates)
	if p1 != p2 {
		t.Error("PredictPrompt 非确定性：相同输入产生了不同 prompt")
	}
}

// TestPredictStateDeltas_InvalidJSON 验证 LLM 返回非法 JSON 时的错误处理。
func TestPredictStateDeltas_InvalidJSON(t *testing.T) {
	graph := NewStateGraph()
	mock := &mockCompleter{response: "这不是 JSON"}

	_, err := PredictStateDeltas(context.Background(), mock, graph, []string{"动作"})
	if err == nil {
		t.Fatal("期望 JSON 解析错误，实际为 nil")
	}
}

// TestValidate_IgnoresNonAddNode 验证 Validate 忽略非 add_node 的增量。
func TestValidate_IgnoresNonAddNode(t *testing.T) {
	before := NewStateGraph()
	after := NewStateGraph()

	predicted := []StateDelta{
		{Kind: KindToolCall, Summary: "某操作", Effect: "remove_node"},
		{Kind: KindTask, Summary: "某边", Effect: "modify_edge"},
	}

	divergences := Validate(predicted, before, after)
	if len(divergences) != 0 {
		t.Errorf("非 add_node 增量应被忽略，实际分歧: %v", divergences)
	}
}
