package dag

import (
	"context"
	"errors"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/core"
)

type mockDelegateAgent struct {
	name   string
	output string
	err    error
}

func (m *mockDelegateAgent) Name() string           { return m.name }
func (m *mockDelegateAgent) Stats() core.AgentStats { return core.AgentStats{} }
func (m *mockDelegateAgent) Stop()                  {}
func (m *mockDelegateAgent) Run(ctx context.Context, msg core.Message) (*core.Response, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &core.Response{Content: m.output}, nil
}
func (m *mockDelegateAgent) StreamRun(_ context.Context, _ core.Message) (<-chan core.StreamEvent, error) {
	ch := make(chan core.StreamEvent, 1)
	ch <- core.StreamEvent{Type: core.StreamEventComplete, Content: m.output}
	close(ch)
	return ch, nil
}

func TestAgentDelegateNode_Basic(t *testing.T) {
	agent := &mockDelegateAgent{name: "test-agent", output: "agent result"}
	node := NewAgentDelegateNode("delegate-1", agent)

	if node.Name() != "delegate-1" {
		t.Errorf("Name = %q, want delegate-1", node.Name())
	}

	resp, err := node.Run(context.Background(), core.UserMessage("input"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp.Content != "agent result" {
		t.Errorf("Content = %q, want agent result", resp.Content)
	}
}

func TestAgentDelegateNode_WithInputMapper(t *testing.T) {
	agent := &mockDelegateAgent{name: "mapper-agent", output: "mapped input"}
	node := NewAgentDelegateNode("node-with-mapper", agent)
	node.WithInputMapper(MapFromDependent("upstream"))

	mapper := node.GetInputMapper()
	if mapper == nil {
		t.Fatal("input mapper should not be nil")
	}

	results := map[string]*DAGNodeResult{
		"upstream": {NodeID: "upstream", Output: "upstream data"},
	}
	result := mapper(results)
	if result != "upstream data" {
		t.Errorf("mapper result = %q, want upstream data", result)
	}
}

func TestAgentDelegateNode_WithMetadata(t *testing.T) {
	agent := &mockDelegateAgent{name: "meta-agent", output: "ok"}
	node := NewAgentDelegateNode("meta-node", agent)
	node.WithMetadata("role", "summarizer", "priority", "high")

	if node.metadata["role"] != "summarizer" {
		t.Errorf("role = %q, want summarizer", node.metadata["role"])
	}
	if node.metadata["priority"] != "high" {
		t.Errorf("priority = %q, want high", node.metadata["priority"])
	}
}

func TestSubWorkflowNode_Run(t *testing.T) {
	sub := NewDAGBuilder("sub-test").
		Node("s1", func(ctx context.Context, input string) (string, error) {
			return "step1: " + input, nil
		}).
		Node("s2", func(ctx context.Context, input string) (string, error) {
			return "step2: processed", nil
		}).
		Edge("s1", "s2").
		MustBuild()

	node := NewSubWorkflowNode("sub-wrapper", sub)
	resp, err := node.Run(context.Background(), core.UserMessage("test-input"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp.Content == "" {
		t.Error("output should not be empty")
	}
}

func TestMapFromDependent(t *testing.T) {
	mapper := MapFromDependent("a", "b")
	results := map[string]*DAGNodeResult{
		"a": {Output: "result A"},
		"b": {Output: "result B"},
		"c": {Output: "result C"},
	}
	result := mapper(results)
	if result != "result A\nresult B" {
		t.Errorf("result = %q, want result A\\nresult B", result)
	}
}

func TestMapFromDependent_Empty(t *testing.T) {
	mapper := MapFromDependent("nonexistent")
	result := mapper(map[string]*DAGNodeResult{})
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

func TestMapConcatAll(t *testing.T) {
	mapper := MapConcatAll()
	results := map[string]*DAGNodeResult{
		"node1": {NodeID: "node1", Output: "output1"},
		"node2": {NodeID: "node2", Output: "output2"},
	}
	result := mapper(results)
	if !containsString(result, "[node1] output1") || !containsString(result, "[node2] output2") {
		t.Errorf("concat result missing expected content: %q", result)
	}
}

func TestMapTemplate(t *testing.T) {
	mapper := MapTemplate("Research: {research}\nAnalysis: {analysis}")
	results := map[string]*DAGNodeResult{
		"research": {Output: "found X"},
		"analysis": {Output: "X means Y"},
	}
	result := mapper(results)
	expected := "Research: found X\nAnalysis: X means Y"
	if result != expected {
		t.Errorf("template result = %q, want %q", result, expected)
	}
}

func TestDAGBuilder_DelegateNode(t *testing.T) {
	agent := &mockDelegateAgent{name: "worker", output: "work done"}
	dag, err := NewDAGBuilder("delegate-test").
		Node("input", func(ctx context.Context, input string) (string, error) {
			return "prepared: " + input, nil
		}).
		DelegateNode("worker", agent).
		Edge("input", "worker").
		Build()

	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if dag.NodeCount() != 2 {
		t.Errorf("NodeCount = %d, want 2", dag.NodeCount())
	}
}

func TestDAGBuilder_SubWorkflowAsNode(t *testing.T) {
	sub := NewDAGBuilder("inner").
		Node("x", func(ctx context.Context, input string) (string, error) { return "X", nil }).
		MustBuild()

	dag, err := NewDAGBuilder("outer").
		Node("main", func(ctx context.Context, input string) (string, error) { return "main", nil }).
		SubWorkflowAsNode("inner-dag", sub).
		Edge("main", "inner-dag").
		Build()

	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if dag.NodeCount() != 2 {
		t.Errorf("NodeCount = %d, want 2", dag.NodeCount())
	}
}

func TestAgentDelegateNode_ErrorPropagation(t *testing.T) {
	agent := &mockDelegateAgent{name: "failing", err: errors.New("agent failed")}
	node := NewAgentDelegateNode("failing-node", agent)

	_, err := node.Run(context.Background(), core.UserMessage("input"))
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "agent failed" {
		t.Errorf("error = %v, want agent failed", err)
	}
}
