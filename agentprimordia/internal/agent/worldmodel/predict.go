// predict.go — LLM-as 状态预演（行动前预测状态增量 + 回溯校验）
//
// 设计定位（V7 路线图 §三 命题 2）：
//   - 在 agent 执行候选动作之前，用 LLM 预测该动作对状态图的影响（状态增量）；
//   - 执行后用 Validate 做回溯校验：预测的新增节点是否真的出现在图中——
//     未命中即「预演偏差」，供 reflection 通道消费以修正后续策略；
//   - Completer 接口解耦 LLM 实现（标准库，零外部依赖）；
//   - PredictPrompt 纯函数：相同输入必得相同 prompt（确定性可测试）；
//   - JSON 协议：LLM 返回 [][]StateDelta，每个候选动作对应一组增量。
package worldmodel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Completer LLM 补全抽象——预演模块唯一依赖的 LLM 接口。
// 实现方可以是 OpenAI provider、MockLLM 或任何满足签名的类型。
type Completer interface {
	Complete(ctx context.Context, prompt string) (string, error)
}

// StateDelta 单条状态增量——描述一个候选动作对状态图的预期影响。
type StateDelta struct {
	Kind    NodeKind `json:"kind"`    // 节点种类
	Summary string   `json:"summary"` // 节点摘要
	Effect  string   `json:"effect"`  // "add_node" | "remove_node" | "modify_edge"
}

// PredictPrompt 构造预演 prompt：将当前状态图节点 + 候选动作序列化为
// 结构化提示，要求 LLM 以 JSON 数组的数组返回每组候选对应的 StateDelta 列表。
// 纯函数——相同输入必得相同 prompt（确定性可测试）。
func PredictPrompt(nodes []StateNode, candidates []string) string {
	var b strings.Builder

	b.WriteString("你是一个世界模型状态预演引擎。\n")
	b.WriteString("给定当前状态图的节点列表和若干候选动作，")
	b.WriteString("请预测每个动作执行后对状态图的影响。\n\n")

	// 当前状态图节点
	b.WriteString("## 当前状态图节点\n")
	if len(nodes) == 0 {
		b.WriteString("（空图）\n")
	} else {
		for _, n := range nodes {
			b.WriteString(fmt.Sprintf("- [%s] %s: %s\n", n.ID, string(n.Kind), n.Summary))
		}
	}
	b.WriteString("\n")

	// 候选动作
	b.WriteString("## 候选动作\n")
	for i, c := range candidates {
		b.WriteString(fmt.Sprintf("%d. %s\n", i+1, c))
	}
	b.WriteString("\n")

	// 输出格式要求
	b.WriteString("## 输出要求\n")
	b.WriteString("返回一个 JSON 数组的数组，每个子数组对应一个候选动作的 StateDelta 列表。\n")
	b.WriteString("每个 StateDelta 包含：\n")
	b.WriteString("  - kind: 节点种类（task/plan/tool_call/observation/hypothesis）\n")
	b.WriteString("  - summary: 节点摘要\n")
	b.WriteString("  - effect: \"add_node\" | \"remove_node\" | \"modify_edge\"\n")
	b.WriteString("只返回 JSON，不要其他内容。\n\n")
	b.WriteString("示例输出格式：\n")
	b.WriteString(`[[{"kind":"tool_call","summary":"读取文件 main.go","effect":"add_node"}]]`)
	b.WriteString("\n")

	return b.String()
}

// PredictStateDeltas 调用 LLM 预测候选动作的状态增量。
//
// 流程：
//  1. 从状态图取全部节点快照；
//  2. 用 PredictPrompt 构造提示；
//  3. 调用 llm.Complete 获取 LLM 响应；
//  4. 解析 JSON 响应为 [][]StateDelta。
//
// 错误传播：LLM 返回错误或 JSON 解析失败时返回 error。
func PredictStateDeltas(ctx context.Context, llm Completer, graph *StateGraph, candidates []string) ([][]StateDelta, error) {
	nodes := graph.Nodes()
	prompt := PredictPrompt(nodes, candidates)

	resp, err := llm.Complete(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("状态预演 LLM 调用失败: %w", err)
	}

	// 解析 JSON 响应
	var deltas [][]StateDelta
	if err := json.Unmarshal([]byte(strings.TrimSpace(resp)), &deltas); err != nil {
		return nil, fmt.Errorf("状态预演 JSON 解析失败: %w", err)
	}

	return deltas, nil
}

// Validate 回溯校验：将预测的状态增量与实际状态图变化做对比。
//
// 对 predicted 中每条 effect="add_node" 的增量，计算其确定性节点 ID
// （NodeID(kind, summary)），检查该 ID 是否存在于 after 图但不在 before 图中。
// 预测出现但实际未观测到的增量记为分歧（divergence）。
//
// 返回分歧描述列表（可为空，表示全部命中）。
func Validate(predicted []StateDelta, before, after *StateGraph) []string {
	// 收集 before/after 的节点 ID 集合
	beforeIDs := make(map[string]bool)
	for _, n := range before.Nodes() {
		beforeIDs[n.ID] = true
	}
	afterIDs := make(map[string]bool)
	for _, n := range after.Nodes() {
		afterIDs[n.ID] = true
	}

	// 实际新增 = after 有而 before 没有的节点
	newIDs := make(map[string]bool)
	for id := range afterIDs {
		if !beforeIDs[id] {
			newIDs[id] = true
		}
	}

	// 检查预测的 add_node 是否都在实际新增中
	var divergences []string
	for _, d := range predicted {
		if d.Effect != "add_node" {
			continue
		}
		id := NodeID(d.Kind, d.Summary)
		if !newIDs[id] {
			divergences = append(divergences,
				fmt.Sprintf("预测新增节点 [%s] %s 未在实际状态图中出现", string(d.Kind), d.Summary))
		}
	}

	return divergences
}

// GraphFromNodeIDs 由节点快照切片构造只读状态图（无边）。
// 专用于回溯校验「before」快照：Validate 仅比较节点 ID 集合，
// 边信息不参与分歧判定，因此重建时忽略原始边。
func GraphFromNodeIDs(nodes []StateNode) *StateGraph {
	g := NewStateGraph()
	for _, n := range nodes {
		// AddNode 以 (Kind, Summary) 派生确定性 ID，与原始节点 ID 一致；
		// CreatedAtTurn 取原始值，但不影响 Validate 的节点存在性判定。
		g.AddNode(n.Kind, n.Summary, n.CreatedAtTurn)
	}
	return g
}
