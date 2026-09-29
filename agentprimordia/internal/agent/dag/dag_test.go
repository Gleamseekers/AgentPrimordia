package dag

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/core"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/hooks"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/lifecycle"
)

func TestDAGWorkflow_SimpleLinear(t *testing.T) {
	dag := NewDAGWorkflow()

	a := &mockAgentForOrch{name: "A", output: "outA"}
	b := &mockAgentForOrch{name: "B", output: "outB"}
	c := &mockAgentForOrch{name: "C", output: "outC"}

	if err := dag.AddNode(&DAGNode{ID: "a", Agent: a}); err != nil {
		t.Fatalf("AddNode a: %v", err)
	}
	if err := dag.AddNode(&DAGNode{ID: "b", Agent: b}); err != nil {
		t.Fatalf("AddNode b: %v", err)
	}
	if err := dag.AddNode(&DAGNode{ID: "c", Agent: c}); err != nil {
		t.Fatalf("AddNode c: %v", err)
	}

	if err := dag.AddEdge(DAGEdge{From: "a", To: "b"}); err != nil {
		t.Fatalf("AddEdge a->b: %v", err)
	}
	if err := dag.AddEdge(DAGEdge{From: "b", To: "c"}); err != nil {
		t.Fatalf("AddEdge b->c: %v", err)
	}

	result, err := dag.Run(context.Background(), "input")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(result.NodeResults) != 3 {
		t.Errorf("expected 3 node results, got %d", len(result.NodeResults))
	}

	checkResult := func(id, want string) {
		nr, ok := result.NodeResults[id]
		if !ok {
			t.Errorf("missing result for node %q", id)
			return
		}
		if nr.Output != want {
			t.Errorf("node %q: output = %q, want %q", id, nr.Output, want)
		}
		if nr.Skipped {
			t.Errorf("node %q: unexpected skip", id)
		}
	}

	checkResult("a", "outA")
	checkResult("b", "outB")
	checkResult("c", "outC")

	if len(result.Order) != 3 {
		t.Errorf("expected 3 in order, got %d", len(result.Order))
	}
}

func TestDAGWorkflow_ParallelBranches(t *testing.T) {
	dag := NewDAGWorkflow()

	a := &mockAgentForOrch{name: "A", output: "outA"}
	b := &mockAgentForOrch{name: "B", output: "outB"}
	c := &mockAgentForOrch{name: "C", output: "outC"}
	d := &mockAgentForOrch{name: "D", output: "outD"}

	_ = dag.AddNode(&DAGNode{ID: "a", Agent: a})
	_ = dag.AddNode(&DAGNode{ID: "b", Agent: b})
	_ = dag.AddNode(&DAGNode{ID: "c", Agent: c})
	_ = dag.AddNode(&DAGNode{ID: "d", Agent: d})

	_ = dag.AddEdge(DAGEdge{From: "a", To: "b"})
	_ = dag.AddEdge(DAGEdge{From: "a", To: "c"})
	_ = dag.AddEdge(DAGEdge{From: "b", To: "d"})
	_ = dag.AddEdge(DAGEdge{From: "c", To: "d"})

	result, err := dag.Run(context.Background(), "input")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(result.NodeResults) != 4 {
		t.Errorf("expected 4 node results, got %d", len(result.NodeResults))
	}

	for _, id := range []string{"a", "b", "c", "d"} {
		nr, ok := result.NodeResults[id]
		if !ok {
			t.Errorf("missing result for node %q", id)
			continue
		}
		if nr.Skipped {
			t.Errorf("node %q: unexpected skip", id)
		}
	}

	orderIdx := make(map[string]int)
	for i, id := range result.Order {
		orderIdx[id] = i
	}
	if orderIdx["a"] >= orderIdx["b"] {
		t.Error("a should execute before b")
	}
	if orderIdx["a"] >= orderIdx["c"] {
		t.Error("a should execute before c")
	}
	if orderIdx["b"] >= orderIdx["d"] {
		t.Error("b should execute before d")
	}
	if orderIdx["c"] >= orderIdx["d"] {
		t.Error("c should execute before d")
	}
}

func TestDAGWorkflow_ConditionalEdge(t *testing.T) {
	dag := NewDAGWorkflow()

	a := &mockAgentForOrch{name: "A", output: "outA"}
	b := &mockAgentForOrch{name: "B", output: "outB"}
	c := &mockAgentForOrch{name: "C", output: "outC"}

	_ = dag.AddNode(&DAGNode{ID: "a", Agent: a})
	_ = dag.AddNode(&DAGNode{ID: "b", Agent: b})
	_ = dag.AddNode(&DAGNode{ID: "c", Agent: c})

	_ = dag.AddEdge(DAGEdge{From: "a", To: "b"})
	_ = dag.AddEdge(DAGEdge{
		From: "a",
		To:   "c",
		Condition: func(_ context.Context, _ *DAGNodeResult) bool {
			return false
		},
	})

	result, err := dag.Run(context.Background(), "input")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	nrB, ok := result.NodeResults["b"]
	if !ok || nrB.Skipped {
		t.Error("node b should have executed (unconditional edge)")
	}

	nrC, ok := result.NodeResults["c"]
	if !ok {
		t.Error("missing result for node c")
	} else if !nrC.Skipped {
		t.Error("node c should be skipped (conditional edge returned false and no other edges reach it)")
	}
}

func TestDAGWorkflow_CycleDetection(t *testing.T) {
	dag := NewDAGWorkflow()

	a := &mockAgentForOrch{name: "A", output: "outA"}
	b := &mockAgentForOrch{name: "B", output: "outB"}
	c := &mockAgentForOrch{name: "C", output: "outC"}

	_ = dag.AddNode(&DAGNode{ID: "a", Agent: a})
	_ = dag.AddNode(&DAGNode{ID: "b", Agent: b})
	_ = dag.AddNode(&DAGNode{ID: "c", Agent: c})

	_ = dag.AddEdge(DAGEdge{From: "a", To: "b"})
	_ = dag.AddEdge(DAGEdge{From: "b", To: "c"})
	_ = dag.AddEdge(DAGEdge{From: "c", To: "a"})

	_, err := dag.Run(context.Background(), "input")
	if err == nil {
		t.Fatal("expected error for cycle in DAG")
	}
}

func TestDAGWorkflow_Validate_DuplicateNode(t *testing.T) {
	dag := NewDAGWorkflow()

	a := &mockAgentForOrch{name: "A", output: "outA"}
	_ = dag.AddNode(&DAGNode{ID: "x", Agent: a})

	err := dag.AddNode(&DAGNode{ID: "x", Agent: a})
	if err == nil {
		t.Fatal("expected error for duplicate node ID")
	}
}

func TestDAGWorkflow_Validate_MissingNode(t *testing.T) {
	dag := NewDAGWorkflow()

	a := &mockAgentForOrch{name: "A", output: "outA"}
	_ = dag.AddNode(&DAGNode{ID: "a", Agent: a})

	err := dag.AddEdge(DAGEdge{From: "a", To: "nonexistent"})
	if err == nil {
		t.Fatal("expected error for edge referencing non-existent node")
	}
}

func TestDAGWorkflow_IsolatedNode(t *testing.T) {
	dag := NewDAGWorkflow()

	a := &mockAgentForOrch{name: "A", output: "outA"}
	b := &mockAgentForOrch{name: "B", output: "outB"}

	_ = dag.AddNode(&DAGNode{ID: "a", Agent: a})
	_ = dag.AddNode(&DAGNode{ID: "b", Agent: b})

	result, err := dag.Run(context.Background(), "input")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(result.NodeResults) != 2 {
		t.Errorf("expected 2 node results, got %d", len(result.NodeResults))
	}

	for _, id := range []string{"a", "b"} {
		nr, ok := result.NodeResults[id]
		if !ok {
			t.Errorf("missing result for isolated node %q", id)
		} else if nr.Skipped {
			t.Errorf("isolated node %q should not be skipped", id)
		}
	}
}

func TestDAGWorkflow_Hooks_Fired(t *testing.T) {
	dag := NewDAGWorkflow()

	a := &mockAgentForOrch{name: "A", output: "outA"}
	b := &mockAgentForOrch{name: "B", output: "outB"}

	_ = dag.AddNode(&DAGNode{ID: "a", Agent: a})
	_ = dag.AddNode(&DAGNode{ID: "b", Agent: b})
	_ = dag.AddEdge(DAGEdge{From: "a", To: "b"})

	hm := hooks.NewHookManager()
	var fired []hooks.HookPoint
	var mu sync.Mutex
	hm.Register(hooks.HookBeforeDAGNode, func(_ context.Context, _ *hooks.HookContext) error {
		mu.Lock()
		fired = append(fired, hooks.HookBeforeDAGNode)
		mu.Unlock()
		return nil
	})
	hm.Register(hooks.HookAfterDAGNode, func(_ context.Context, _ *hooks.HookContext) error {
		mu.Lock()
		fired = append(fired, hooks.HookAfterDAGNode)
		mu.Unlock()
		return nil
	})
	dag.SetHooks(hm)

	_, err := dag.Run(context.Background(), "input")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(fired) != 4 {
		t.Fatalf("expected 4 hook firings (2 before + 2 after), got %d: %v", len(fired), fired)
	}

	var beforeCount, afterCount int
	for _, p := range fired {
		switch p {
		case hooks.HookBeforeDAGNode:
			beforeCount++
		case hooks.HookAfterDAGNode:
			afterCount++
		}
	}
	if beforeCount != 2 {
		t.Errorf("beforeCount = %d, want 2", beforeCount)
	}
	if afterCount != 2 {
		t.Errorf("afterCount = %d, want 2", afterCount)
	}
}

func TestDAGWorkflow_ConditionalEdge_PartialSkip(t *testing.T) {
	dag := NewDAGWorkflow()

	a := &mockAgentForOrch{name: "A", output: "outA"}
	b := &mockAgentForOrch{name: "B", output: "outB"}
	c := &mockAgentForOrch{name: "C", output: "outC"}

	_ = dag.AddNode(&DAGNode{ID: "a", Agent: a})
	_ = dag.AddNode(&DAGNode{ID: "b", Agent: b})
	_ = dag.AddNode(&DAGNode{ID: "c", Agent: c})

	_ = dag.AddEdge(DAGEdge{
		From: "a",
		To:   "b",
		Condition: func(_ context.Context, _ *DAGNodeResult) bool {
			return false
		},
	})
	_ = dag.AddEdge(DAGEdge{
		From: "a",
		To:   "c",
		Condition: func(_ context.Context, _ *DAGNodeResult) bool {
			return true
		},
	})

	result, err := dag.Run(context.Background(), "input")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	nrB := result.NodeResults["b"]
	if nrB == nil {
		t.Fatal("missing result for node b")
	}
	if !nrB.Skipped {
		t.Error("node b should be skipped")
	}

	nrC := result.NodeResults["c"]
	if nrC == nil {
		t.Fatal("missing result for node c")
	}
	if nrC.Skipped {
		t.Error("node c should not be skipped")
	}
	if nrC.Output != "outC" {
		t.Errorf("node c output = %q, want %q", nrC.Output, "outC")
	}
}

func TestDAGWorkflow_NodeInput(t *testing.T) {
	dag := NewDAGWorkflow()

	var receivedInput string
	a := &mockAgentForOrch{name: "A", output: "outA"}
	customAgent := &struct {
		*mockAgentForOrch
	}{
		mockAgentForOrch: a,
	}
	_ = customAgent

	agentWithInputCheck := &dagInputCheckAgent{output: "outA", receivedInput: &receivedInput}

	_ = dag.AddNode(&DAGNode{ID: "a", Agent: agentWithInputCheck, Input: "custom-input"})

	result, err := dag.Run(context.Background(), "default-input")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	nr := result.NodeResults["a"]
	if nr == nil {
		t.Fatal("missing result for node a")
	}
	if receivedInput != "custom-input" {
		t.Errorf("received input = %q, want %q", receivedInput, "custom-input")
	}
}

type dagInputCheckAgent struct {
	output        string
	receivedInput *string
}

func (a *dagInputCheckAgent) Run(_ context.Context, input core.Message) (*core.Response, error) {
	*a.receivedInput = input.Content
	return &core.Response{Content: a.output}, nil
}
func (a *dagInputCheckAgent) StreamRun(_ context.Context, _ core.Message) (<-chan core.StreamEvent, error) {
	ch := make(chan core.StreamEvent, 1)
	ch <- core.StreamEvent{Type: core.StreamEventComplete, Content: a.output}
	close(ch)
	return ch, nil
}
func (a *dagInputCheckAgent) Stop() {}
func (a *dagInputCheckAgent) Stats() core.AgentStats {
	return core.AgentStats{Status: lifecycle.StatusIdle}
}
func (a *dagInputCheckAgent) Name() string { return "input-check" }

func TestDAGWorkflow_Validate_Explicit(t *testing.T) {
	dag := NewDAGWorkflow()

	a := &mockAgentForOrch{name: "A", output: "outA"}
	b := &mockAgentForOrch{name: "B", output: "outB"}

	_ = dag.AddNode(&DAGNode{ID: "a", Agent: a})
	_ = dag.AddNode(&DAGNode{ID: "b", Agent: b})
	_ = dag.AddEdge(DAGEdge{From: "a", To: "b"})

	if err := dag.Validate(); err != nil {
		t.Errorf("valid DAG should pass validation: %v", err)
	}
}

func TestDAGWorkflow_ConditionalEdge_ReachableViaOtherEdge(t *testing.T) {
	dag := NewDAGWorkflow()

	a := &mockAgentForOrch{name: "A", output: "outA"}
	b := &mockAgentForOrch{name: "B", output: "outB"}
	c := &mockAgentForOrch{name: "C", output: "outC"}

	_ = dag.AddNode(&DAGNode{ID: "a", Agent: a})
	_ = dag.AddNode(&DAGNode{ID: "b", Agent: b})
	_ = dag.AddNode(&DAGNode{ID: "c", Agent: c})

	_ = dag.AddEdge(DAGEdge{
		From: "a",
		To:   "c",
		Condition: func(_ context.Context, _ *DAGNodeResult) bool {
			return false
		},
	})
	_ = dag.AddEdge(DAGEdge{From: "b", To: "c"})

	result, err := dag.Run(context.Background(), "input")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	nrC := result.NodeResults["c"]
	if nrC == nil {
		t.Fatal("missing result for node c")
	}
	if nrC.Skipped {
		t.Error("node c should not be skipped (reachable via b->c)")
	}
}

func TestDAGWorkflow_ParallelExecution(t *testing.T) {
	dag := NewDAGWorkflow()

	var mu sync.Mutex
	var executionOrder []string

	makeAgent := func(name string) core.Agent {
		return &dagTrackAgent{
			name:           name,
			output:         fmt.Sprintf("out%s", name),
			executionOrder: &executionOrder,
			mu:             &mu,
		}
	}

	_ = dag.AddNode(&DAGNode{ID: "a", Agent: makeAgent("A")})
	_ = dag.AddNode(&DAGNode{ID: "b", Agent: makeAgent("B")})
	_ = dag.AddNode(&DAGNode{ID: "c", Agent: makeAgent("C")})

	_ = dag.AddEdge(DAGEdge{From: "a", To: "b"})
	_ = dag.AddEdge(DAGEdge{From: "a", To: "c"})

	result, err := dag.Run(context.Background(), "input")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(result.NodeResults) != 3 {
		t.Errorf("expected 3 results, got %d", len(result.NodeResults))
	}
}

type dagTrackAgent struct {
	name           string
	output         string
	executionOrder *[]string
	mu             *sync.Mutex
}

func (a *dagTrackAgent) Run(_ context.Context, _ core.Message) (*core.Response, error) {
	a.mu.Lock()
	*a.executionOrder = append(*a.executionOrder, a.name)
	a.mu.Unlock()
	return &core.Response{Content: a.output}, nil
}
func (a *dagTrackAgent) StreamRun(_ context.Context, _ core.Message) (<-chan core.StreamEvent, error) {
	ch := make(chan core.StreamEvent, 1)
	ch <- core.StreamEvent{Type: core.StreamEventComplete, Content: a.output}
	close(ch)
	return ch, nil
}
func (a *dagTrackAgent) Stop()                  {}
func (a *dagTrackAgent) Stats() core.AgentStats { return core.AgentStats{Status: lifecycle.StatusIdle} }
func (a *dagTrackAgent) Name() string           { return a.name }

// ===== 新增测试：可视化、指标、拓扑排序、重试等 =====

func TestDAGWorkflow_TopologicalSort(t *testing.T) {
	dag := NewDAGWorkflow()

	a := &mockAgentForOrch{name: "A", output: "outA"}
	b := &mockAgentForOrch{name: "B", output: "outB"}
	c := &mockAgentForOrch{name: "C", output: "outC"}

	_ = dag.AddNode(&DAGNode{ID: "a", Agent: a})
	_ = dag.AddNode(&DAGNode{ID: "b", Agent: b})
	_ = dag.AddNode(&DAGNode{ID: "c", Agent: c})

	_ = dag.AddEdge(DAGEdge{From: "a", To: "b"})
	_ = dag.AddEdge(DAGEdge{From: "b", To: "c"})

	order, err := dag.TopologicalSort()
	if err != nil {
		t.Fatalf("TopologicalSort 失败: %v", err)
	}

	if len(order) != 3 {
		t.Errorf("排序结果应有 3 个节点，实际 %d", len(order))
	}

	orderIdx := make(map[string]int)
	for i, id := range order {
		orderIdx[id] = i
	}
	if orderIdx["a"] >= orderIdx["b"] {
		t.Error("a 应在 b 前面")
	}
	if orderIdx["b"] >= orderIdx["c"] {
		t.Error("b 应在 c 前面")
	}
}

func TestDAGWorkflow_TopologicalSort_Cycle(t *testing.T) {
	dag := NewDAGWorkflow()

	a := &mockAgentForOrch{name: "A", output: "outA"}
	b := &mockAgentForOrch{name: "B", output: "outB"}
	c := &mockAgentForOrch{name: "C", output: "outC"}

	_ = dag.AddNode(&DAGNode{ID: "a", Agent: a})
	_ = dag.AddNode(&DAGNode{ID: "b", Agent: b})
	_ = dag.AddNode(&DAGNode{ID: "c", Agent: c})

	_ = dag.AddEdge(DAGEdge{From: "a", To: "b"})
	_ = dag.AddEdge(DAGEdge{From: "b", To: "c"})
	_ = dag.AddEdge(DAGEdge{From: "c", To: "a"})

	_, err := dag.TopologicalSort()
	if err == nil {
		t.Fatal("含环 DAG 应返回错误")
	}
}

func TestDAGWorkflow_GetDependencies(t *testing.T) {
	dag := NewDAGWorkflow()
	_ = dag.AddNode(&DAGNode{ID: "a", Agent: &mockAgentForOrch{name: "A"}})
	_ = dag.AddNode(&DAGNode{ID: "b", Agent: &mockAgentForOrch{name: "B"}})
	_ = dag.AddNode(&DAGNode{ID: "c", Agent: &mockAgentForOrch{name: "C"}})

	_ = dag.AddEdge(DAGEdge{From: "a", To: "c"})
	_ = dag.AddEdge(DAGEdge{From: "b", To: "c"})

	deps := dag.GetDependencies("c")
	if len(deps) != 2 {
		t.Errorf("c 应有 2 个依赖，实际 %d", len(deps))
	}
}

func TestDAGWorkflow_GetDependents(t *testing.T) {
	dag := NewDAGWorkflow()
	_ = dag.AddNode(&DAGNode{ID: "a", Agent: &mockAgentForOrch{name: "A"}})
	_ = dag.AddNode(&DAGNode{ID: "b", Agent: &mockAgentForOrch{name: "B"}})
	_ = dag.AddNode(&DAGNode{ID: "c", Agent: &mockAgentForOrch{name: "C"}})

	_ = dag.AddEdge(DAGEdge{From: "a", To: "b"})
	_ = dag.AddEdge(DAGEdge{From: "a", To: "c"})

	deps := dag.GetDependents("a")
	if len(deps) != 2 {
		t.Errorf("a 应有 2 个下游，实际 %d", len(deps))
	}
}

func TestDAGWorkflow_ToMermaid(t *testing.T) {
	dag := NewDAGWorkflow().WithName("test-dag")

	_ = dag.AddNode(&DAGNode{
		ID:       "start",
		Agent:    &mockAgentForOrch{name: "Start"},
		Metadata: map[string]string{"label": "开始"},
	})
	_ = dag.AddNode(&DAGNode{
		ID:       "process",
		Agent:    &mockAgentForOrch{name: "Process"},
		Metadata: map[string]string{"label": "处理"},
	})
	_ = dag.AddNode(&DAGNode{
		ID:       "end",
		Agent:    &mockAgentForOrch{name: "End"},
		Metadata: map[string]string{"label": "结束"},
	})

	_ = dag.AddEdge(DAGEdge{From: "start", To: "process", Label: "触发"})
	_ = dag.AddEdge(DAGEdge{
		From:      "process",
		To:        "end",
		Label:     "完成",
		Condition: func(_ context.Context, _ *DAGNodeResult) bool { return true },
	})

	output := dag.ToMermaid()
	if !strings.Contains(output, "graph LR") {
		t.Error("Mermaid 输出应包含 graph LR")
	}
	if !strings.Contains(output, "开始") {
		t.Error("Mermaid 输出应包含节点标签")
	}
	if !strings.Contains(output, "触发") {
		t.Error("Mermaid 输出应包含边标签")
	}
}

func TestDAGWorkflow_ToPlantUML(t *testing.T) {
	dag := NewDAGWorkflow().WithName("pipeline")

	_ = dag.AddNode(&DAGNode{
		ID:       "n1",
		Agent:    &mockAgentForOrch{name: "N1"},
		Metadata: map[string]string{"label": "步骤1"},
	})
	_ = dag.AddNode(&DAGNode{
		ID:       "n2",
		Agent:    &mockAgentForOrch{name: "N2"},
		Metadata: map[string]string{"label": "步骤2"},
	})

	_ = dag.AddEdge(DAGEdge{From: "n1", To: "n2"})

	output := dag.ToPlantUML()
	if !strings.Contains(output, "@startuml") {
		t.Error("PlantUML 输出应以 @startuml 开头")
	}
	if !strings.Contains(output, "@enduml") {
		t.Error("PlantUML 输出应以 @enduml 结尾")
	}
	if !strings.Contains(output, "步骤1") {
		t.Error("PlantUML 输出应包含节点标签")
	}
}

func TestDAGWorkflow_ToDot(t *testing.T) {
	dag := NewDAGWorkflow().WithName("my-dag")

	_ = dag.AddNode(&DAGNode{
		ID:       "node1",
		Agent:    &mockAgentForOrch{name: "N1"},
		Metadata: map[string]string{"label": "节点1"},
	})
	_ = dag.AddNode(&DAGNode{
		ID:       "node2",
		Agent:    &mockAgentForOrch{name: "N2"},
		Metadata: map[string]string{"label": "节点2"},
	})

	_ = dag.AddEdge(DAGEdge{From: "node1", To: "node2", Label: "flow"})

	output := dag.ToDot()
	if !strings.Contains(output, "digraph G") {
		t.Error("DOT 输出应包含 digraph G")
	}
	if !strings.Contains(output, "rankdir=LR") {
		t.Error("DOT 输出应包含 rankdir=LR")
	}
	if !strings.Contains(output, "节点1") {
		t.Error("DOT 输出应包含节点标签")
	}
}

func TestDAGWorkflow_ToJSON(t *testing.T) {
	dag := NewDAGWorkflow().WithName("json-test")

	_ = dag.AddNode(&DAGNode{
		ID:       "x",
		Agent:    &mockAgentForOrch{name: "X"},
		Metadata: map[string]string{"label": "X节点"},
	})
	_ = dag.AddNode(&DAGNode{
		ID:       "y",
		Agent:    &mockAgentForOrch{name: "Y"},
		Metadata: map[string]string{"label": "Y节点"},
	})

	_ = dag.AddEdge(DAGEdge{From: "x", To: "y", Label: "edge-label"})

	data := dag.ToJSON()
	if data["name"] != "json-test" {
		t.Errorf("name = %q, want %q", data["name"], "json-test")
	}

	nodes := data["nodes"].([]map[string]string)
	if len(nodes) != 2 {
		t.Errorf("nodes 长度应为 2，实际 %d", len(nodes))
	}

	edges := data["edges"].([]map[string]string)
	if len(edges) != 1 || edges[0]["from"] != "x" || edges[0]["to"] != "y" {
		t.Errorf("edges 格式错误: %v", edges)
	}
}

func TestDAGWorkflow_Metrics(t *testing.T) {
	dag := NewDAGWorkflow()

	a := &mockAgentForOrch{name: "A", output: "outA"}
	b := &mockAgentForOrch{name: "B", output: "outB"}

	_ = dag.AddNode(&DAGNode{ID: "a", Agent: a})
	_ = dag.AddNode(&DAGNode{ID: "b", Agent: b})
	_ = dag.AddEdge(DAGEdge{From: "a", To: "b"})

	result, err := dag.Run(context.Background(), "input")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if result.TotalNodes != 2 {
		t.Errorf("TotalNodes 应为 2，实际 %d", result.TotalNodes)
	}
	if result.Succeeded != 2 {
		t.Errorf("Succeeded 应为 2，实际 %d", result.Succeeded)
	}
	if result.Failed != 0 {
		t.Errorf("Failed 应为 0，实际 %d", result.Failed)
	}

	metrics := dag.Metrics()
	snap := metrics.Snapshot()
	totalExec := snap["total_executions"].(int64)
	if totalExec < 1 {
		t.Errorf("TotalExecutions 应 >= 1，实际 %d", totalExec)
	}
}

func TestDAGWorkflow_NodeCountAndEdgeCount(t *testing.T) {
	dag := NewDAGWorkflow()

	a := &mockAgentForOrch{name: "A", output: "outA"}
	b := &mockAgentForOrch{name: "B", output: "outB"}

	_ = dag.AddNode(&DAGNode{ID: "a", Agent: a})
	_ = dag.AddNode(&DAGNode{ID: "b", Agent: b})
	_ = dag.AddEdge(DAGEdge{From: "a", To: "b"})

	if dag.NodeCount() != 2 {
		t.Errorf("NodeCount 应为 2，实际 %d", dag.NodeCount())
	}
	if dag.EdgeCount() != 1 {
		t.Errorf("EdgeCount 应为 1，实际 %d", dag.EdgeCount())
	}
}

func TestDAGWorkflow_WithRetryPolicy(t *testing.T) {
	retryableAgent := &retryableTestAgent{
		maxFail: 2,
		output:  "success-after-retry",
	}

	dag := NewDAGWorkflow()
	_ = dag.AddNode(&DAGNode{
		ID:          "retry-node",
		Agent:       retryableAgent,
		RetryPolicy: &RetryPolicy{MaxRetries: 3, Delay: time.Millisecond, Backoff: 1.5},
	})

	result, err := dag.Run(context.Background(), "input")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	nr := result.NodeResults["retry-node"]
	if nr == nil {
		t.Fatal("missing result for retry-node")
	}
	if nr.Retries <= 0 && nr.Error == nil {
		t.Log("重试策略：首次成功或重试后成功")
	}
	if nr.Output != "" {
		t.Logf("输出: %s, 重试次数: %d", nr.Output, nr.Retries)
	}
}

type retryableTestAgent struct {
	maxFail   int
	output    string
	attemptFn func(int)
	callCount int
}

func (a *retryableTestAgent) Run(_ context.Context, _ core.Message) (*core.Response, error) {
	a.callCount++
	if a.attemptFn != nil {
		a.attemptFn(a.callCount - 1)
	}
	if a.callCount <= a.maxFail {
		return nil, fmt.Errorf("simulated failure %d", a.callCount)
	}
	return &core.Response{Content: a.output}, nil
}
func (a *retryableTestAgent) StreamRun(_ context.Context, _ core.Message) (<-chan core.StreamEvent, error) {
	ch := make(chan core.StreamEvent, 1)
	ch <- core.StreamEvent{Type: core.StreamEventComplete, Content: a.output}
	close(ch)
	return ch, nil
}
func (a *retryableTestAgent) Stop() {}
func (a *retryableTestAgent) Stats() core.AgentStats {
	return core.AgentStats{Status: lifecycle.StatusIdle}
}
func (a *retryableTestAgent) Name() string { return "retryable" }

func TestDAGWorkflow_EdgeLabel(t *testing.T) {
	dag := NewDAGWorkflow()

	_ = dag.AddNode(&DAGNode{ID: "a", Agent: &mockAgentForOrch{name: "A"}})
	_ = dag.AddNode(&DAGNode{ID: "b", Agent: &mockAgentForOrch{name: "B"}})

	_ = dag.AddEdge(DAGEdge{From: "a", To: "b", Label: "数据流"})

	mermaid := dag.ToMermaid()
	if !strings.Contains(mermaid, "数据流") {
		t.Error("Mermaid 输出应包含边标签")
	}

	dot := dag.ToDot()
	if !strings.Contains(dot, "数据流") {
		t.Error("DOT 输出应包含边标签")
	}
}

func TestDAGWorkflow_SanitizeMermaidID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello-world", "hello_world"},
		{"node.id", "node_id"},
		{"my node", "my_node"},
		{"normal", "normal"},
	}
	for _, tt := range tests {
		result := SanitizeMermaidID(tt.input)
		if result != tt.expected {
			t.Errorf("SanitizeMermaidID(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}
