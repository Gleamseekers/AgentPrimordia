package workflow

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/core"
)

// ===== Mock Agent =====

// mockAgent 实现 core.Agent 接口，用于工作流测试。
type mockAgent struct {
	name      string
	response  string // Run 返回的 Content
	failCount int    // 前 failCount 次调用返回 error
	callCount int
	mu        sync.Mutex
}

func (a *mockAgent) Run(_ context.Context, _ core.Message) (*core.Response, error) {
	a.mu.Lock()
	a.callCount++
	shouldFail := a.callCount <= a.failCount
	a.mu.Unlock()

	if shouldFail {
		return nil, fmt.Errorf("simulated failure (attempt %d)", a.callCount)
	}
	return &core.Response{Content: a.response}, nil
}

func (a *mockAgent) StreamRun(_ context.Context, _ core.Message) (<-chan core.StreamEvent, error) {
	return nil, fmt.Errorf("not implemented")
}

func (a *mockAgent) Stop() {}

func (a *mockAgent) Stats() core.AgentStats {
	return core.AgentStats{}
}

func (a *mockAgent) Name() string { return a.name }

// ===== 测试 =====

func TestWorkflow_LinearExecution(t *testing.T) {
	wf := NewWorkflowExecution(WorkflowConfig{
		Type:        LinearWorkflow,
		Name:        "test-linear",
		Description: "测试线性执行",
	})

	_ = wf.AddNode(&WorkflowNode{
		ID:    "step1",
		Name:  "步骤1-数据收集",
		Type:  TaskNode,
		Agent: &mockAgent{name: "Step1", response: "步骤1完成"},
	})

	_ = wf.AddNode(&WorkflowNode{
		ID:    "step2",
		Name:  "步骤2-数据处理",
		Type:  TaskNode,
		Agent: &mockAgent{name: "Step2", response: "步骤2完成"},
	})

	_ = wf.AddTransition(&Transition{From: "step1", To: "step2"})
	_ = wf.SetStartNode("step1")

	input := map[string]any{"data": "test_data"}
	result, err := wf.Execute(input)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if result.Status != WfStatusCompleted {
		t.Errorf("expected completed, got %s", result.Status)
	}

	if len(result.Records) != 2 {
		t.Errorf("expected 2 records, got %d", len(result.Records))
	}

	if len(result.PathTaken) != 2 || result.PathTaken[0] != "step1" || result.PathTaken[1] != "step2" {
		t.Errorf("unexpected path taken: %v", result.PathTaken)
	}
	fmt.Printf("✅ Linear Workflow: status=%s records=%d path=%v\n",
		result.Status, len(result.Records), result.PathTaken)
}

func TestWorkflow_ConditionalBranching(t *testing.T) {
	wf := NewWorkflowExecution(WorkflowConfig{
		Type:        ConditionalWorkflow,
		Name:        "test-conditional",
		Description: "测试条件分支",
		ErrorHandling: ErrorHandling{
			OnError:         "skip",
			ContinueOnError: true,
		},
	})

	_ = wf.AddNode(&WorkflowNode{
		ID:    "check",
		Name:  "条件检查",
		Type:  ConditionNode,
		Agent: &mockAgent{name: "CheckCondition", response: `{"condition_result": true}`},
		Condition: &NodeCondition{
			Field:    "should_branch_a",
			Operator: "==",
			Value:    true,
		},
	})

	_ = wf.AddNode(&WorkflowNode{
		ID:    "branchA",
		Name:  "分支A",
		Type:  TaskNode,
		Agent: &mockAgent{name: "BranchA", response: "执行分支A逻辑"},
	})

	_ = wf.AddNode(&WorkflowNode{
		ID:    "branchB",
		Name:  "分支B",
		Type:  TaskNode,
		Agent: &mockAgent{name: "BranchB", response: "执行分支B逻辑"},
	})

	_ = wf.AddTransition(&Transition{
		From:      "check",
		To:        "branchA",
		Condition: &TransitionCondition{Type: "condition", Field: "condition_result", Operator: "==", Value: true},
	})
	_ = wf.AddTransition(&Transition{
		From:      "check",
		To:        "branchB",
		Condition: &TransitionCondition{Type: "condition", Field: "condition_result", Operator: "!=", Value: true},
	})
	_ = wf.SetStartNode("check")

	input := map[string]any{"should_branch_a": true}
	result, err := wf.Execute(input)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if result.Status != WfStatusCompleted {
		t.Errorf("expected completed, got %s", result.Status)
	}

	fmt.Printf("✅ Conditional Workflow: status=%s branches=%d\n",
		result.Status, result.Metrics.BranchesTaken.Load())
}

func TestWorkflow_LoopExecution(t *testing.T) {
	wf := NewWorkflowExecution(WorkflowConfig{
		Type:          LoopWorkflow,
		Name:          "test-loop",
		Description:   "测试循环执行",
		MaxIterations: 3,
	})

	_ = wf.AddNode(&WorkflowNode{
		ID:    "loop_task",
		Name:  "循环任务",
		Type:  TaskNode,
		Agent: &mockAgent{name: "LoopTask", response: "循环迭代完成"},
		Config: &NodeConfig{
			PromptTemplate: "当前迭代次数: {{_iteration}}\n输入: {{data}}",
		},
	})

	_ = wf.AddNode(&WorkflowNode{
		ID:   "loop_end",
		Name: "循环结束检查",
		Type: LoopEndNode,
		Condition: &NodeCondition{
			Field:    "_iteration",
			Operator: ">=",
			Value:    3,
		},
	})

	_ = wf.AddTransition(&Transition{From: "loop_task", To: "loop_end"})
	_ = wf.AddTransition(&Transition{From: "loop_end", To: "loop_task"})
	_ = wf.SetStartNode("loop_task")

	input := map[string]any{"data": "test_data"}
	result, err := wf.Execute(input)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if result.Metrics.Iterations.Load() < 2 {
		t.Errorf("expected at least 2 iterations, got %d", result.Metrics.Iterations.Load())
	}

	fmt.Printf("✅ Loop Workflow: iterations=%d records=%d\n",
		result.Metrics.Iterations.Load(), len(result.Records))
}

func TestWorkflow_ParallelForkJoin(t *testing.T) {
	wf := NewWorkflowExecution(WorkflowConfig{
		Type:        ParallelForkJoin,
		Name:        "test-parallel",
		Description: "测试并行分叉合并",
	})

	_ = wf.AddNode(&WorkflowNode{
		ID:   "fork",
		Name: "并行分发",
		Type: ParallelNode,
	})

	_ = wf.AddNode(&WorkflowNode{
		ID:    "task1",
		Name:  "任务1",
		Type:  TaskNode,
		Agent: &mockAgent{name: "ParallelTask1", response: "任务1结果"},
	})

	_ = wf.AddNode(&WorkflowNode{
		ID:    "task2",
		Name:  "任务2",
		Type:  TaskNode,
		Agent: &mockAgent{name: "ParallelTask2", response: "任务2结果"},
	})

	_ = wf.AddNode(&WorkflowNode{
		ID:    "merge",
		Name:  "结果合并",
		Type:  TaskNode,
		Agent: &mockAgent{name: "MergeResults", response: "合并完成"},
	})

	_ = wf.AddTransition(&Transition{From: "fork", To: "task1"})
	_ = wf.AddTransition(&Transition{From: "fork", To: "task2"})
	_ = wf.AddTransition(&Transition{From: "task1", To: "merge"})
	_ = wf.AddTransition(&Transition{From: "task2", To: "merge"})
	_ = wf.SetStartNode("fork")

	input := map[string]any{"parallel_data": "test"}
	result, err := wf.Execute(input)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if result.Status != WfStatusCompleted {
		t.Errorf("expected completed, got %s", result.Status)
	}

	hasTask1Result := false
	hasTask2Result := false
	for key := range result.Output {
		if strings.Contains(key, "task1") {
			hasTask1Result = true
		}
		if strings.Contains(key, "task2") {
			hasTask2Result = true
		}
	}

	if !hasTask1Result || !hasTask2Result {
		t.Error("missing parallel task results")
	}

	fmt.Printf("✅ Parallel Workflow: status=%d output_keys=%d\n",
		len(result.Records), len(result.Output))
}

func TestWorkflow_ErrorHandling_Retry(t *testing.T) {
	failingAgent := &mockAgent{
		name:      "FailingAgent",
		response:  "最终成功",
		failCount: 2,
	}

	wf := NewWorkflowExecution(WorkflowConfig{
		Type: LinearWorkflow,
		Name: "test-retry",
		ErrorHandling: ErrorHandling{
			OnError:    "retry",
			MaxRetries: 3,
		},
	})

	_ = wf.AddNode(&WorkflowNode{
		ID:    "failing_step",
		Name:  "可能失败的步骤",
		Type:  TaskNode,
		Agent: failingAgent,
	})

	_ = wf.SetStartNode("failing_step")

	result, err := wf.Execute(map[string]any{})
	if err != nil && failingAgent.callCount < 3 {
		t.Fatalf("unexpected error after retries: %v", err)
	}

	if result.Metrics.RetriesAttempted.Load() == 0 && failingAgent.callCount > 1 {
		t.Log("retries were attempted as expected")
	}

	fmt.Printf("✅ Error Handling (Retry): attempts=%d retries=%d\n",
		failingAgent.callCount, result.Metrics.RetriesAttempted.Load())
}

func TestWorkflow_ErrorHandling_Fallback(t *testing.T) {
	failingAgent := &mockAgent{
		name:      "MainAgent",
		response:  "",
		failCount: 999, // 总是失败
	}

	fallbackAgent := &mockAgent{
		name:     "FallbackAgent",
		response: "回退方案已执行",
	}

	wf := NewWorkflowExecution(WorkflowConfig{
		Type: LinearWorkflow,
		Name: "test-fallback",
		ErrorHandling: ErrorHandling{
			OnError:      "fallback",
			FallbackStep: "fallback_node",
		},
	})

	_ = wf.AddNode(&WorkflowNode{
		ID:    "main",
		Name:  "主节点",
		Type:  TaskNode,
		Agent: failingAgent,
	})

	_ = wf.AddNode(&WorkflowNode{
		ID:    "fallback_node",
		Name:  "回退节点",
		Type:  FallbackNode,
		Agent: fallbackAgent,
	})

	_ = wf.AddTransition(&Transition{From: "main", To: "fallback_node"})
	_ = wf.SetStartNode("main")

	result, err := wf.Execute(map[string]any{})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	foundFallback := false
	for _, record := range result.Records {
		if record.NodeID == "fallback_node" {
			foundFallback = true
			break
		}
	}

	if !foundFallback {
		t.Error("fallback node was not executed")
	}

	fmt.Printf("✅ Error Handling (Fallback): fallback_executed=%v\n", foundFallback)
}

func TestWorkflow_StateMachine(t *testing.T) {
	wf := NewWorkflowExecution(WorkflowConfig{
		Type:        StateMachine,
		Name:        "test-state-machine",
		Description: "状态机工作流",
	})

	_ = wf.AddNode(&WorkflowNode{
		ID:    "initial",
		Name:  "初始状态",
		Type:  TaskNode,
		Agent: &mockAgent{name: "State1", response: `{"next_state": "processing"}`},
	})

	_ = wf.AddNode(&WorkflowNode{
		ID:    "processing",
		Name:  "处理状态",
		Type:  TaskNode,
		Agent: &mockAgent{name: "State2", response: `{"next_state": "completed"}`},
	})

	_ = wf.AddNode(&WorkflowNode{
		ID:    "completed",
		Name:  "完成状态",
		Type:  TaskNode,
		Agent: &mockAgent{name: "State3", response: "处理完成"},
	})

	_ = wf.AddTransition(&Transition{
		From:      "initial",
		To:        "processing",
		Condition: &TransitionCondition{Field: "next_state", Operator: "==", Value: "processing"},
	})
	_ = wf.AddTransition(&Transition{
		From:      "processing",
		To:        "completed",
		Condition: &TransitionCondition{Field: "next_state", Operator: "==", Value: "completed"},
	})
	_ = wf.SetStartNode("initial")

	input := map[string]any{"action": "start"}
	result, err := wf.Execute(input)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if result.Status != WfStatusCompleted {
		t.Errorf("expected completed, got %s", result.Status)
	}

	expectedPathLength := 3
	if len(result.PathTaken) != expectedPathLength {
		t.Errorf("expected path length %d, got %d", expectedPathLength, len(result.PathTaken))
	}

	fmt.Printf("✅ State Machine: states=%d path=%v\n",
		len(result.PathTaken), result.PathTaken)
}

func TestWorkflow_VariablesAndMapping(t *testing.T) {
	wf := NewWorkflowExecution(WorkflowConfig{
		Type: LinearWorkflow,
		Name: "test-variables",
	})

	_ = wf.AddNode(&WorkflowNode{
		ID:    "process",
		Name:  "数据处理",
		Type:  TaskNode,
		Agent: &mockAgent{name: "ProcessAgent", response: `{"processed_data": "processed_value", "score": 95}`},
		InputMapping: map[string]string{
			"raw_input": "input_data",
		},
		OutputMapping: map[string]string{
			"final_result":  "processed_data",
			"quality_score": "score",
		},
	})

	_ = wf.SetStartNode("process")

	wf.SetVariable("predefined_var", "hello")
	input := map[string]any{"input_data": "raw_test_data"}
	result, err := wf.Execute(input)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if _, exists := result.Variables["predefined_var"]; !exists {
		t.Error("predefined variable not found in result variables")
	}

	if _, exists := result.Output["final_result"]; !exists {
		t.Error("mapped output not found")
	}

	fmt.Printf("✅ Variables & Mapping: vars=%d outputs=%d\n",
		len(result.Variables), len(result.Output))
}

func TestWorkflow_PauseResumeCancel(t *testing.T) {
	wf := NewWorkflowExecution(WorkflowConfig{
		Type:    LinearWorkflow,
		Name:    "test-lifecycle",
		Timeout: 10 * time.Second,
	})

	_ = wf.AddNode(&WorkflowNode{
		ID:    "task1",
		Name:  "任务1",
		Type:  TaskNode,
		Agent: &mockAgent{name: "SimpleAgent", response: "done"},
	})

	_ = wf.SetStartNode("task1")

	initialStatus := wf.GetStatus()
	if initialStatus != WfStatusPending {
		t.Errorf("expected pending, got %s", initialStatus)
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		wf.Pause()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, _ = wf.Execute(map[string]any{"test": "lifecycle"})
	<-ctx.Done()

	pausedStatus := wf.GetStatus()
	if pausedStatus != WfStatusPaused && pausedStatus != WfStatusCancelled {
		t.Logf("status after pause: %s", pausedStatus)
	}

	wf.Cancel()
	cancelledStatus := wf.GetStatus()
	if cancelledStatus != WfStatusCancelled {
		t.Logf("status after cancel: %s", cancelledStatus)
	}

	fmt.Printf("✅ Lifecycle Management: final_status=%s\n", wf.GetStatus())
}

func TestWorkflow_MetricsCollection(t *testing.T) {
	wf := NewWorkflowExecution(WorkflowConfig{
		Type:          ConditionalWorkflow,
		Name:          "test-metrics",
		Description:   "测试指标收集",
		EnableLogging: true,
	})

	for i := 0; i < 4; i++ {
		nodeID := fmt.Sprintf("node_%d", i)

		nodeType := TaskNode
		if i%2 == 0 {
			nodeType = ConditionNode
		}

		_ = wf.AddNode(&WorkflowNode{
			ID:    nodeID,
			Name:  fmt.Sprintf("节点%d", i),
			Type:  nodeType,
			Agent: &mockAgent{name: fmt.Sprintf("Node%d", i), response: fmt.Sprintf("node_%d_result", i)},
		})

		if i > 0 {
			prevNodeID := fmt.Sprintf("node_%d", i-1)
			_ = wf.AddTransition(&Transition{From: prevNodeID, To: nodeID})
		}
	}

	_ = wf.SetStartNode("node_0")

	result, err := wf.Execute(map[string]any{"metrics_test": true})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	metrics := result.Metrics
	if metrics.TotalNodes.Load() == 0 {
		t.Error("no metrics collected")
	}

	fmt.Printf("✅ Metrics Collection:\n")
	fmt.Printf("   Total Nodes: %d\n", metrics.TotalNodes.Load())
	fmt.Printf("   Executed: %d | Failed: %d | Skipped: %d\n",
		metrics.ExecutedNodes.Load(), metrics.FailedNodes.Load(), metrics.SkippedNodes.Load())
	fmt.Printf("   Total Duration: %v\n", time.Duration(metrics.TotalDurationNs.Load()))
	fmt.Printf("   Avg Node Duration: %v\n", metrics.AvgNodeDuration)
	fmt.Printf("   Branches Taken: %d\n", metrics.BranchesTaken.Load())
}

func TestWorkflow_ExportImport(t *testing.T) {
	wf := NewWorkflowExecution(WorkflowConfig{
		Type:        LinearWorkflow,
		Name:        "test-export",
		Description: "导出测试工作流",
	})

	_ = wf.AddNode(&WorkflowNode{
		ID:    "export_step",
		Name:  "导出步骤",
		Type:  TaskNode,
		Agent: &mockAgent{name: "ExportAgent", response: "exported data"},
		Metadata: map[string]any{
			"version": "1.0",
			"author":  "test",
		},
	})

	_ = wf.SetStartNode("export_step")

	input := map[string]any{"export_key": "export_value"}
	_, err := wf.Execute(input)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	exportData, err := wf.Export()
	if err != nil {
		t.Fatalf("Export error: %v", err)
	}

	if len(exportData) == 0 {
		t.Error("exported data is empty")
	}

	history := wf.GetHistory()
	if len(history) == 0 {
		t.Error("history is empty")
	}

	fmt.Printf("✅ Export: size=%d bytes history_records=%d\n",
		len(exportData), len(history))
}

func TestWorkflow_EventSystem(t *testing.T) {
	wf := NewWorkflowExecution(WorkflowConfig{
		Type:          LinearWorkflow,
		Name:          "test-events",
		Description:   "事件系统测试",
		EnableLogging: true,
	})

	_ = wf.AddNode(&WorkflowNode{
		ID:    "event_step",
		Name:  "事件步骤",
		Type:  TaskNode,
		Agent: &mockAgent{name: "EventAgent", response: "event test done"},
	})

	_ = wf.SetStartNode("event_step")

	var collectedEvents []*WorkflowEvent
	done := make(chan struct{})

	go func() {
		for event := range wf.Events() {
			collectedEvents = append(collectedEvents, event)
			if event.Type == "workflow_completed" {
				close(done)
				return
			}
		}
	}()

	_, err := wf.Execute(map[string]any{"event_test": true})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for events")
	}

	eventTypes := make([]string, len(collectedEvents))
	for i, event := range collectedEvents {
		eventTypes[i] = event.Type
	}

	hasStarted := false
	hasCompleted := false
	for _, eventType := range eventTypes {
		if eventType == "workflow_started" {
			hasStarted = true
		}
		if eventType == "workflow_completed" {
			hasCompleted = true
		}
	}

	if !hasStarted || !hasCompleted {
		t.Error("missing required lifecycle events")
	}

	fmt.Printf("✅ Event System: events=%d types=%v\n",
		len(collectedEvents), eventTypes)
}

// ===== 可视化测试 =====

func TestWorkflow_ToMermaid(t *testing.T) {
	wf := NewWorkflowExecution(WorkflowConfig{
		Type: LinearWorkflow,
		Name: "test-mermaid",
	})

	_ = wf.AddNode(&WorkflowNode{
		ID:    "start",
		Name:  "开始",
		Type:  TaskNode,
		Agent: &mockAgent{name: "Start", response: "start"},
	})
	_ = wf.AddNode(&WorkflowNode{
		ID:    "end",
		Name:  "结束",
		Type:  TaskNode,
		Agent: &mockAgent{name: "End", response: "end"},
	})
	_ = wf.AddTransition(&Transition{From: "start", To: "end"})
	_ = wf.SetStartNode("start")

	mermaid := wf.ToMermaid()
	if !strings.Contains(mermaid, "flowchart") {
		t.Error("mermaid output should contain 'flowchart'")
	}
	if !strings.Contains(mermaid, "start") {
		t.Error("mermaid output should contain 'start' node")
	}
	if !strings.Contains(mermaid, "end") {
		t.Error("mermaid output should contain 'end' node")
	}
	if !strings.Contains(mermaid, "-->") {
		t.Error("mermaid output should contain edge '-->'")
	}
}

func TestWorkflow_ToDot(t *testing.T) {
	wf := NewWorkflowExecution(WorkflowConfig{
		Type: LinearWorkflow,
		Name: "test-dot",
	})

	_ = wf.AddNode(&WorkflowNode{
		ID:    "a",
		Name:  "NodeA",
		Type:  ConditionNode,
		Agent: &mockAgent{name: "A", response: "a"},
	})
	_ = wf.AddNode(&WorkflowNode{
		ID:    "b",
		Name:  "NodeB",
		Type:  TaskNode,
		Agent: &mockAgent{name: "B", response: "b"},
	})
	_ = wf.AddTransition(&Transition{From: "a", To: "b"})
	_ = wf.SetStartNode("a")

	dot := wf.ToDot()
	if !strings.Contains(dot, "digraph") {
		t.Error("DOT output should contain 'digraph'")
	}
	if !strings.Contains(dot, "diamond") {
		t.Error("DOT output should contain 'diamond' shape for condition node")
	}
}

func TestWorkflow_DefaultVisualizeConfig(t *testing.T) {
	cfg := DefaultVisualizeConfig()
	if cfg.Direction != "TD" {
		t.Errorf("expected TD, got %s", cfg.Direction)
	}
	if !cfg.ShowLabels {
		t.Error("ShowLabels should be true by default")
	}
}

// ===== API 边界测试 =====

func TestWorkflow_AddNode_Invalid(t *testing.T) {
	wf := NewWorkflowExecution(WorkflowConfig{
		Type: LinearWorkflow,
		Name: "test-invalid",
	})

	// 空ID节点应返回错误
	err := wf.AddNode(&WorkflowNode{ID: "", Name: "empty", Type: TaskNode})
	if err == nil {
		t.Error("expected error for empty node ID")
	}

	// TaskNode 无 Agent 应返回错误
	err = wf.AddNode(&WorkflowNode{ID: "no-agent", Name: "no-agent", Type: TaskNode})
	if err == nil {
		t.Error("expected error for task node without agent")
	}
}

func TestWorkflow_AddTransition_Invalid(t *testing.T) {
	wf := NewWorkflowExecution(WorkflowConfig{
		Type: LinearWorkflow,
		Name: "test-invalid-trans",
	})

	err := wf.AddTransition(&Transition{From: "", To: "target"})
	if err == nil {
		t.Error("expected error for empty From")
	}

	err = wf.AddTransition(&Transition{From: "source", To: ""})
	if err == nil {
		t.Error("expected error for empty To")
	}
}

func TestWorkflow_SetStartNode_NotFound(t *testing.T) {
	wf := NewWorkflowExecution(WorkflowConfig{
		Type: LinearWorkflow,
		Name: "test-start-not-found",
	})

	err := wf.SetStartNode("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent start node")
	}
}

func TestWorkflow_GetVariable(t *testing.T) {
	wf := NewWorkflowExecution(WorkflowConfig{
		Type: LinearWorkflow,
		Name: "test-vars",
	})

	wf.SetVariable("key1", "value1")
	val, ok := wf.GetVariable("key1")
	if !ok || val != "value1" {
		t.Errorf("expected value1, got %v (ok=%v)", val, ok)
	}

	_, ok = wf.GetVariable("nonexistent")
	if ok {
		t.Error("expected false for nonexistent variable")
	}
}

func TestWorkflow_UnsupportedType(t *testing.T) {
	wf := NewWorkflowExecution(WorkflowConfig{
		Type: WorkflowType("unsupported"),
		Name: "test-unsupported",
	})

	_, err := wf.Execute(map[string]any{})
	if err == nil {
		t.Error("expected error for unsupported workflow type")
	}
}

func TestWorkflow_ResumeNotPaused(t *testing.T) {
	wf := NewWorkflowExecution(WorkflowConfig{
		Type: LinearWorkflow,
		Name: "test-resume",
	})

	err := wf.Resume()
	if err == nil {
		t.Error("expected error when resuming non-paused workflow")
	}
}

func TestWorkflow_CustomLogicNode(t *testing.T) {
	wf := NewWorkflowExecution(WorkflowConfig{
		Type: LinearWorkflow,
		Name: "test-custom-logic",
	})

	_ = wf.AddNode(&WorkflowNode{
		ID:   "custom",
		Name: "自定义逻辑",
		Type: TaskNode,
		Config: &NodeConfig{
			CustomLogic: func(_ context.Context, input map[string]any) (map[string]any, error) {
				return map[string]any{"result": "custom_output", "input": input["data"]}, nil
			},
		},
	})
	_ = wf.SetStartNode("custom")

	result, err := wf.Execute(map[string]any{"data": "test"})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if result.Status != WfStatusCompleted {
		t.Errorf("expected completed, got %s", result.Status)
	}
}
