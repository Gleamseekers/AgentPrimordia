package orchestration

import (
	"strings"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent"
)

// buildLinearWorkflow 构建线性工作流用于测试
func buildLinearWorkflow() *agent.WorkflowExecution {
	wf := agent.NewWorkflowExecution(agent.WorkflowConfig{
		Type: agent.LinearWorkflow,
		Name: "test-linear",
	})

	_ = wf.AddNode(&agent.WorkflowNode{ID: "start", Name: "开始", Type: agent.TaskNode, Agent: newNoopAgent("start")})
	_ = wf.AddNode(&agent.WorkflowNode{ID: "process", Name: "处理", Type: agent.TaskNode, Agent: newNoopAgent("process")})
	_ = wf.AddNode(&agent.WorkflowNode{ID: "end", Name: "结束", Type: agent.TaskNode, Agent: newNoopAgent("end")})

	_ = wf.AddTransition(&agent.Transition{From: "start", To: "process"})
	_ = wf.AddTransition(&agent.Transition{From: "process", To: "end"})

	_ = wf.SetStartNode("start")
	return wf
}

// buildConditionalWorkflow 构建条件分支工作流用于测试
func buildConditionalWorkflow() *agent.WorkflowExecution {
	wf := agent.NewWorkflowExecution(agent.WorkflowConfig{
		Type: agent.ConditionalWorkflow,
		Name: "test-conditional",
	})

	_ = wf.AddNode(&agent.WorkflowNode{ID: "start", Name: "开始", Type: agent.TaskNode, Agent: newNoopAgent("start")})
	_ = wf.AddNode(&agent.WorkflowNode{ID: "check", Name: "检查", Type: agent.ConditionNode})
	_ = wf.AddNode(&agent.WorkflowNode{ID: "yes", Name: "是", Type: agent.TaskNode, Agent: newNoopAgent("yes")})
	_ = wf.AddNode(&agent.WorkflowNode{ID: "no", Name: "否", Type: agent.TaskNode, Agent: newNoopAgent("no")})
	_ = wf.AddNode(&agent.WorkflowNode{ID: "end", Name: "结束", Type: agent.TaskNode, Agent: newNoopAgent("end")})

	_ = wf.AddTransition(&agent.Transition{From: "start", To: "check"})
	_ = wf.AddTransition(&agent.Transition{
		From: "check", To: "yes",
		Condition: &agent.TransitionCondition{Type: "comparison", Field: "result", Operator: "==", Value: true},
	})
	_ = wf.AddTransition(&agent.Transition{
		From: "check", To: "no",
		Condition: &agent.TransitionCondition{Type: "comparison", Field: "result", Operator: "==", Value: false},
	})
	_ = wf.AddTransition(&agent.Transition{From: "yes", To: "end"})
	_ = wf.AddTransition(&agent.Transition{From: "no", To: "end"})

	_ = wf.SetStartNode("start")
	return wf
}

// buildParallelWorkflow 构建并行工作流用于测试
func buildParallelWorkflow() *agent.WorkflowExecution {
	wf := agent.NewWorkflowExecution(agent.WorkflowConfig{
		Type: agent.ParallelForkJoin,
		Name: "test-parallel",
	})

	_ = wf.AddNode(&agent.WorkflowNode{ID: "start", Name: "开始", Type: agent.TaskNode, Agent: newNoopAgent("start")})
	_ = wf.AddNode(&agent.WorkflowNode{ID: "fork", Name: "分叉", Type: agent.ParallelNode})
	_ = wf.AddNode(&agent.WorkflowNode{ID: "task_a", Name: "任务A", Type: agent.TaskNode, Agent: newNoopAgent("task_a")})
	_ = wf.AddNode(&agent.WorkflowNode{ID: "task_b", Name: "任务B", Type: agent.TaskNode, Agent: newNoopAgent("task_b")})
	_ = wf.AddNode(&agent.WorkflowNode{ID: "join", Name: "合并", Type: agent.TaskNode, Agent: newNoopAgent("join")})
	_ = wf.AddNode(&agent.WorkflowNode{ID: "end", Name: "结束", Type: agent.TaskNode, Agent: newNoopAgent("end")})

	_ = wf.AddTransition(&agent.Transition{From: "start", To: "fork"})
	_ = wf.AddTransition(&agent.Transition{From: "fork", To: "task_a"})
	_ = wf.AddTransition(&agent.Transition{From: "fork", To: "task_b"})
	_ = wf.AddTransition(&agent.Transition{From: "task_a", To: "join"})
	_ = wf.AddTransition(&agent.Transition{From: "task_b", To: "join"})
	_ = wf.AddTransition(&agent.Transition{From: "join", To: "end"})

	_ = wf.SetStartNode("start")
	return wf
}

// buildStateMachineWorkflow 构建状态机工作流用于测试
func buildStateMachineWorkflow() *agent.WorkflowExecution {
	wf := agent.NewWorkflowExecution(agent.WorkflowConfig{
		Type: agent.StateMachine,
		Name: "test-state-machine",
	})

	_ = wf.AddNode(&agent.WorkflowNode{ID: "idle", Name: "空闲", Type: agent.TaskNode, Agent: newNoopAgent("idle")})
	_ = wf.AddNode(&agent.WorkflowNode{ID: "running", Name: "运行中", Type: agent.TaskNode, Agent: newNoopAgent("running")})
	_ = wf.AddNode(&agent.WorkflowNode{ID: "paused", Name: "已暂停", Type: agent.TaskNode, Agent: newNoopAgent("paused")})
	_ = wf.AddNode(&agent.WorkflowNode{ID: "done", Name: "完成", Type: agent.TaskNode, Agent: newNoopAgent("done")})

	_ = wf.AddTransition(&agent.Transition{
		From: "idle", To: "running",
		Condition: &agent.TransitionCondition{Type: "comparison", Field: "event", Operator: "==", Value: "start"},
	})
	_ = wf.AddTransition(&agent.Transition{
		From: "running", To: "paused",
		Condition: &agent.TransitionCondition{Type: "comparison", Field: "event", Operator: "==", Value: "pause"},
	})
	_ = wf.AddTransition(&agent.Transition{
		From: "paused", To: "running",
		Condition: &agent.TransitionCondition{Type: "comparison", Field: "event", Operator: "==", Value: "resume"},
	})
	_ = wf.AddTransition(&agent.Transition{
		From: "running", To: "done",
		Condition: &agent.TransitionCondition{Type: "comparison", Field: "event", Operator: "==", Value: "stop"},
	})

	_ = wf.SetStartNode("idle")
	return wf
}

func TestWorkflow_ToMermaid_Linear(t *testing.T) {
	wf := buildLinearWorkflow()
	output := wf.ToMermaid()

	if !strings.HasPrefix(output, "flowchart TD") {
		t.Errorf("期望以 'flowchart TD' 开头，实际: %s", output[:20])
	}

	// 验证节点存在
	if !strings.Contains(output, "start") {
		t.Error("缺少 start 节点")
	}
	if !strings.Contains(output, "process") {
		t.Error("缺少 process 节点")
	}
	if !strings.Contains(output, "end") {
		t.Error("缺少 end 节点")
	}

	// 验证边存在
	if !strings.Contains(output, "start --> process") {
		t.Error("缺少 start -> process 边")
	}
	if !strings.Contains(output, "process --> end") {
		t.Error("缺少 process -> end 边")
	}

	// 验证起始节点高亮
	if !strings.Contains(output, "style start fill:#339af0") {
		t.Error("起始节点未高亮")
	}
}

func TestWorkflow_ToMermaid_Conditional(t *testing.T) {
	wf := buildConditionalWorkflow()
	output := wf.ToMermaid()

	// 验证条件节点使用菱形
	if !strings.Contains(output, "check{") {
		t.Error("条件节点未使用菱形形状")
	}

	// 验证条件边标签
	if !strings.Contains(output, "result == true") {
		t.Error("缺少条件标签 'result == true'")
	}
	if !strings.Contains(output, "result == false") {
		t.Error("缺少条件标签 'result == false'")
	}
}

func TestWorkflow_ToMermaid_Parallel(t *testing.T) {
	wf := buildParallelWorkflow()
	output := wf.ToMermaid()

	// 验证并行节点使用六边形
	if !strings.Contains(output, "fork{{") {
		t.Error("并行节点未使用六边形形状")
	}

	// 验证子图存在
	if !strings.Contains(output, "subgraph fork_parallel") {
		t.Error("缺少并行子图")
	}
}

func TestWorkflow_ToMermaid_StateMachine(t *testing.T) {
	wf := buildStateMachineWorkflow()
	output := wf.ToMermaid()

	// 验证所有状态节点存在
	states := []string{"idle", "running", "paused", "done"}
	for _, state := range states {
		if !strings.Contains(output, state) {
			t.Errorf("缺少状态节点: %s", state)
		}
	}
}

func TestWorkflow_ToDot_Linear(t *testing.T) {
	wf := buildLinearWorkflow()
	output := wf.ToDot()

	if !strings.HasPrefix(output, "digraph workflow {") {
		t.Errorf("期望以 'digraph workflow {' 开头，实际: %s", output[:30])
	}

	if !strings.Contains(output, "\"start\"") {
		t.Error("缺少 start 节点")
	}
	if !strings.Contains(output, "\"start\" -> \"process\"") {
		t.Error("缺少 start -> process 边")
	}
	if !strings.Contains(output, "\"process\" -> \"end\"") {
		t.Error("缺少 process -> end 边")
	}
	if !strings.HasSuffix(strings.TrimSpace(output), "}") {
		t.Error("DOT 图应以 } 结尾")
	}
}

func TestWorkflow_ToDot_Conditional(t *testing.T) {
	wf := buildConditionalWorkflow()
	output := wf.ToDot()

	// 验证条件节点使用菱形
	if !strings.Contains(output, "shape=diamond") {
		t.Error("条件节点未使用 diamond 形状")
	}

	// 验证条件边标签
	if !strings.Contains(output, "result == true") {
		t.Error("缺少条件标签")
	}
}

func TestWorkflow_ToMermaidWithExecution_HighlightPath(t *testing.T) {
	wf := buildLinearWorkflow()

	result := &agent.WorkflowResult{
		PathTaken: []string{"start", "process", "end"},
	}

	output := wf.ToMermaidWithExecution(result)

	// 验证执行路径节点被高亮（绿色）
	if !strings.Contains(output, "fill:#51cf66") {
		t.Error("执行路径节点未被高亮为绿色")
	}
}

func TestWorkflow_ToMermaidWithExecution_FailedNodes(t *testing.T) {
	wf := buildLinearWorkflow()

	result := &agent.WorkflowResult{
		PathTaken: []string{"start", "process"},
		Records: []*agent.ExecutionRecord{
			{NodeID: "process", Status: agent.NodeFailed},
		},
	}

	output := wf.ToMermaidWithExecution(result)

	// 验证失败节点被标记为红色
	if !strings.Contains(output, "fill:#ff6b6b") {
		t.Error("失败节点未被标记为红色")
	}
}

func TestWorkflow_ToDotWithExecution_HighlightPath(t *testing.T) {
	wf := buildLinearWorkflow()

	result := &agent.WorkflowResult{
		PathTaken: []string{"start", "process"},
	}

	output := wf.ToDotWithExecution(result)

	// 验证高亮节点
	if !strings.Contains(output, "fillcolor=\"#51cf66\"") {
		t.Error("执行路径节点未被高亮")
	}

	// 验证高亮边
	if !strings.Contains(output, "color=\"#51cf66\"") {
		t.Error("执行路径边未被高亮")
	}
}

func TestWorkflow_ToMermaid_Direction(t *testing.T) {
	wf := buildLinearWorkflow()

	cfg := DefaultVisualizeConfig()
	cfg.Direction = "LR"

	output := wf.ToMermaidWithConfig(cfg)

	if !strings.HasPrefix(output, "flowchart LR") {
		t.Errorf("期望方向为 LR，实际: %s", output[:15])
	}
}

func TestWorkflow_ToMermaid_NoLabels(t *testing.T) {
	wf := buildConditionalWorkflow()

	cfg := DefaultVisualizeConfig()
	cfg.ShowLabels = false

	output := wf.ToMermaidWithConfig(cfg)

	// 不应包含条件标签
	if strings.Contains(output, "result == true") {
		t.Error("ShowLabels=false 时不应显示条件标签")
	}
}

func TestWorkflow_ToMermaid_EmptyWorkflow(t *testing.T) {
	wf := agent.NewWorkflowExecution(agent.WorkflowConfig{
		Type: agent.LinearWorkflow,
		Name: "empty",
	})

	output := wf.ToMermaid()

	if !strings.HasPrefix(output, "flowchart TD") {
		t.Error("空工作流也应生成基本框架")
	}
}

func TestWorkflow_ToDot_EmptyWorkflow(t *testing.T) {
	wf := agent.NewWorkflowExecution(agent.WorkflowConfig{
		Type: agent.LinearWorkflow,
		Name: "empty",
	})

	output := wf.ToDot()

	if !strings.Contains(output, "digraph workflow {") {
		t.Error("空工作流也应生成 DOT 框架")
	}
	if !strings.Contains(output, "}") {
		t.Error("DOT 图缺少闭合括号")
	}
}

func TestWorkflow_ToMermaid_LoopNodes(t *testing.T) {
	wf := agent.NewWorkflowExecution(agent.WorkflowConfig{
		Type: agent.LoopWorkflow,
		Name: "test-loop",
	})

	_ = wf.AddNode(&agent.WorkflowNode{ID: "loop_start", Name: "循环开始", Type: agent.LoopStartNode})
	_ = wf.AddNode(&agent.WorkflowNode{ID: "loop_body", Name: "循环体", Type: agent.TaskNode, Agent: newNoopAgent("loop_body")})
	_ = wf.AddNode(&agent.WorkflowNode{ID: "loop_end", Name: "循环结束", Type: agent.LoopEndNode})

	_ = wf.AddTransition(&agent.Transition{From: "loop_start", To: "loop_body"})
	_ = wf.AddTransition(&agent.Transition{From: "loop_body", To: "loop_end"})
	_ = wf.AddTransition(&agent.Transition{From: "loop_end", To: "loop_start"})

	_ = wf.SetStartNode("loop_start")

	output := wf.ToMermaid()

	// 验证循环节点使用圆柱形
	if !strings.Contains(output, "loop_start[(") {
		t.Error("循环开始节点未使用圆柱形形状")
	}
	if !strings.Contains(output, "loop_end[(") {
		t.Error("循环结束节点未使用圆柱形形状")
	}
}

func TestWorkflow_ToDot_Parallel(t *testing.T) {
	wf := buildParallelWorkflow()
	output := wf.ToDot()

	// 验证并行节点使用六边形
	if !strings.Contains(output, "shape=hexagon") {
		t.Error("并行节点未使用 hexagon 形状")
	}
}

func TestWorkflow_ToDot_StateMachine(t *testing.T) {
	wf := buildStateMachineWorkflow()
	output := wf.ToDot()

	// 验证所有状态节点存在
	states := []string{"idle", "running", "paused", "done"}
	for _, state := range states {
		if !strings.Contains(output, "\""+state+"\"") {
			t.Errorf("缺少状态节点: %s", state)
		}
	}
}
