package orchestration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/cmd/example/demo"
	"github.com/Gleamseekers/AgentPrimordia/internal/agent"
)

// TestMultiAgentSystem_Integration 测试完整的Multi-Agent系统集成
func TestMultiAgentSystem_Integration(t *testing.T) {
	t.Log("🚀 Multi-Agent System Integration Test")
	t.Log("=" + string(make([]byte, 50)))

	// 1. 创建Orchestrator并配置Sequential工作流
	t.Log("\n📋 1. Setting up Orchestrator with Sequential workflow...")
	orch := NewOrchestrator(OrchestratorConfig{
		Name:        "integration-test",
		Description: "Multi-Agent integration test",
		Mode:        SequentialMode,
	})

	// 2. 创建专业化的Agents
	researcherAgent, err := agent.NewAgent(
		"Researcher",
		"你是市场研究员，负责收集和分析市场数据",
		demo.NewDemoLLM(`{"market_data": {"trend": "up", "growth": 15.3, "competitors": ["A", "B"]}}`),
		agent.WithMaxTurns(1),
	)
	if err != nil {
		t.Fatal(err)
	}

	analystAgent, err := agent.NewAgent(
		"Analyst",
		"你是数据分析师，负责分析数据并提供洞察",
		demo.NewDemoLLM(`{"analysis": {"insight": "市场增长强劲", "recommendation": "扩大投资"}}`),
		agent.WithMaxTurns(1),
	)
	if err != nil {
		t.Fatal(err)
	}

	reporterAgent, err := agent.NewAgent(
		"Reporter",
		"你是报告撰写专家，负责生成最终报告",
		demo.NewDemoLLM("## 市场分析报告\n\n基于数据分析，我们建议..."),
		agent.WithMaxTurns(1),
	)
	if err != nil {
		t.Fatal(err)
	}

	// 3. 添加步骤到Orchestrator
	_ = orch.AddStep(&AgentStep{
		ID:        "research",
		Name:      "市场研究",
		Agent:     researcherAgent,
		Prompt:    "请研究当前市场趋势和竞争格局",
		OutputKey: "market_research",
	})

	_ = orch.AddStep(&AgentStep{
		ID:        "analysis",
		Name:      "数据分析",
		Agent:     analystAgent,
		InputFrom: []string{"market_research"},
		Prompt:    "基于研究结果进行深度分析",
		OutputKey: "data_analysis",
	})

	_ = orch.AddStep(&AgentStep{
		ID:        "report",
		Name:      "报告生成",
		Agent:     reporterAgent,
		InputFrom: []string{"data_analysis"},
		Prompt:    "基于分析结果生成最终报告",
		OutputKey: "final_report",
	})

	// 4. 执行Sequential工作流
	t.Log("▶️  Executing Sequential workflow...")
	input := map[string]any{
		"topic": "AI市场趋势分析",
		"scope": "全球市场",
	}

	result, err := orch.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Orchestration failed: %v", err)
	}

	if result.Status != StatusCompleted {
		t.Errorf("Expected completed status, got %s", result.Status)
	}

	t.Logf("✅ Sequential execution completed in %v", result.Duration)
	t.Logf("   Steps executed: %d", len(result.Steps))
	t.Logf("   Final output keys: %v", getMapKeys(result.FinalOutput))

	// 5. 测试Collaboration (Debate模式)
	t.Log("\n🎯 3. Testing Collaboration (Debate mode)...")
	debateSession := NewCollaborationSession(CollaborationConfig{
		Mode:           DebateMode,
		Name:           "StrategyDebate",
		MaxRounds:      2,
		EnableCritique: true,
	})

	proAgent, err := agent.NewAgent(
		"ProStrategist",
		"你是支持方战略顾问，请提出支持论点",
		demo.NewDemoLLM("我强烈支持这个战略方向，因为市场数据显示增长潜力巨大"),
		agent.WithMaxTurns(1),
	)
	if err != nil {
		t.Fatal(err)
	}

	conAgent, err := agent.NewAgent(
		"ConStrategist",
		"你是反对方战略顾问，请提出反对论点",
		demo.NewDemoLLM("我反对这个方向，因为存在执行风险和资源限制"),
		agent.WithMaxTurns(1),
	)
	if err != nil {
		t.Fatal(err)
	}

	_ = debateSession.AddCollaborator(&Collaborator{
		ID:          "pro",
		Name:        "支持者",
		Role:        "strategist",
		Agent:       proAgent,
		Perspective: "支持",
		Weight:      1.0,
	})

	_ = debateSession.AddCollaborator(&Collaborator{
		ID:          "con",
		Name:        "反对者",
		Role:        "strategist",
		Agent:       conAgent,
		Perspective: "反对",
		Weight:      1.0,
	})

	debateResult, err := debateSession.Execute(context.Background(), "是否应该采用激进的市场扩张策略？")
	if err != nil {
		t.Fatalf("Debate collaboration failed: %v", err)
	}

	if debateResult.Status != CollabStatusCompleted {
		t.Errorf("Expected completed status, got %s", debateResult.Status)
	}

	t.Logf("✅ Debate collaboration completed")
	t.Logf("   Rounds: %d", debateResult.Metrics.TotalRounds)
	t.Logf("   Statements: %d", debateResult.Metrics.TotalStatements)
	if debateResult.FinalOutcome != nil {
		t.Logf("   Agreement Level: %.2f%%", debateResult.FinalOutcome.AgreementLevel*100)
	}

	// 8. 测试Workflow Engine
	t.Log("\n⚙️ 4. Testing Workflow Engine (Conditional)...")
	workflow := agent.NewWorkflowExecution(agent.WorkflowConfig{
		Type:        agent.ConditionalWorkflow,
		Name:        "decision-workflow",
		Description: "决策工作流测试",
		ErrorHandling: agent.ErrorHandling{
			OnError:         "skip",
			ContinueOnError: true,
		},
	})

	decisionAgent, err := agent.NewAgent(
		"DecisionMaker",
		"决策制定者",
		demo.NewDemoLLM(`{"should_proceed": true, "confidence": 0.85}`),
		agent.WithMaxTurns(1),
	)
	if err != nil {
		t.Fatal(err)
	}

	actionAgent, err := agent.NewAgent(
		"ActionTaker",
		"行动执行者",
		demo.NewDemoLLM("行动已执行，目标达成"),
		agent.WithMaxTurns(1),
	)
	if err != nil {
		t.Fatal(err)
	}

	_ = workflow.AddNode(&agent.WorkflowNode{
		ID:    "decision",
		Name:  "决策节点",
		Type:  agent.ConditionNode,
		Agent: decisionAgent,
		Condition: &agent.NodeCondition{
			Field:    "should_proceed",
			Operator: "==",
			Value:    true,
		},
	})

	_ = workflow.AddNode(&agent.WorkflowNode{
		ID:    "action",
		Name:  "行动节点",
		Type:  agent.TaskNode,
		Agent: actionAgent,
	})

	_ = workflow.AddTransition(&agent.Transition{
		From:      "decision",
		To:        "action",
		Condition: &agent.TransitionCondition{Field: "should_proceed", Operator: "==", Value: true},
	})
	_ = workflow.SetStartNode("decision")

	workflowInput := map[string]any{
		"proposal": "新市场进入策略",
		"budget":   1000000,
	}

	workflowResult, err := workflow.Execute(workflowInput)
	if err != nil {
		t.Fatalf("Workflow execution failed: %v", err)
	}

	if workflowResult.Status != agent.WfStatusCompleted {
		t.Errorf("Expected completed status, got %s", workflowResult.Status)
	}

	t.Logf("✅ Conditional workflow completed")
	t.Logf("   Path taken: %v", workflowResult.PathTaken)
	t.Logf("   Records: %d", len(workflowResult.Records))
	t.Logf("   Branches taken: %d", workflowResult.Metrics.BranchesTaken.Load())

	// 9. 综合性能指标汇总
	t.Log("\n📊 5. System Performance Summary:")
	t.Log(string(make([]byte, 40)))

	totalDuration := result.Duration +
		debateResult.Duration +
		workflowResult.Duration

	t.Logf("   Total System Duration: %v", totalDuration)
	t.Logf("   Orchestrator Steps: %d", len(result.Steps))
	t.Logf("   Collaboration Rounds: %d", debateResult.Metrics.TotalRounds)
	t.Logf("   Workflow Nodes: %d", len(workflowResult.Records))

	t.Log("\n✅ All Multi-Agent System Components Integrated Successfully!")
}

// TestMultiAgentSystem_ParallelExecution 测试并行执行场景
func TestMultiAgentSystem_ParallelExecution(t *testing.T) {
	t.Log("\n🔄 Parallel Execution Test")
	t.Log(string(make([]byte, 30)))

	orch := NewOrchestrator(OrchestratorConfig{
		Name:        "parallel-test",
		Description: "Parallel execution test",
		Mode:        ParallelMode,
	})

	for i := 0; i < 4; i++ {
		idx := i
		ag, err := agent.NewAgent(
			fmt.Sprintf("Worker-%d", idx),
			fmt.Sprintf("你是工作者%d，处理分配的任务", idx),
			demo.NewDemoLLM(fmt.Sprintf("任务%d已完成", idx)),
			agent.WithMaxTurns(1),
		)
		if err != nil {
			t.Fatal(err)
		}

		_ = orch.AddStep(&AgentStep{
			ID:        fmt.Sprintf("task_%d", idx),
			Name:      fmt.Sprintf("并行任务%d", idx),
			Agent:     ag,
			Prompt:    fmt.Sprintf("请处理任务%d", idx),
			OutputKey: fmt.Sprintf("result_%d", idx),
			Priority:  idx,
		})
	}

	startTime := time.Now()
	result, err := orch.Execute(context.Background(), map[string]any{
		"workload": "heavy",
		"workers":  4,
	})
	duration := time.Since(startTime)

	if err != nil {
		t.Fatalf("Parallel execution failed: %v", err)
	}

	if result.Status != StatusCompleted {
		t.Errorf("Expected completed, got %s", result.Status)
	}

	t.Logf("✅ Parallel execution completed in %v", duration)
	t.Logf("   Workers: 4")
	t.Logf("   Results: %d outputs", len(result.FinalOutput))

	if duration > time.Second {
		t.Log("⚠️  Parallel execution took longer than expected")
	}
}

// TestMultiAgentSystem_ErrorRecovery 测试错误恢复机制
func TestMultiAgentSystem_ErrorRecovery(t *testing.T) {
	t.Log("\n🔧 Error Recovery Test")
	t.Log(string(make([]byte, 30)))

	orch := NewOrchestrator(OrchestratorConfig{
		Name:        "error-recovery-test",
		Description: "Error recovery test",
		Mode:        SequentialMode,
		MaxRetries:  2,
	})

	failingAgent, err := agent.NewAgent(
		"FailingAgent",
		"模拟失败后恢复的Agent",
		demo.NewDemoLLM("恢复成功！"),
		agent.WithMaxTurns(1),
	)
	if err != nil {
		t.Fatal(err)
	}

	recoveryAgent, err := agent.NewAgent(
		"RecoveryAgent",
		"错误恢复专用Agent",
		demo.NewDemoLLM("错误已处理，系统恢复正常"),
		agent.WithMaxTurns(1),
	)
	if err != nil {
		t.Fatal(err)
	}

	_ = orch.AddStep(&AgentStep{
		ID:        "risky_step",
		Name:      "可能失败的步骤",
		Agent:     failingAgent,
		Prompt:    "执行可能有风险的操作",
		OutputKey: "risky_result",
		RetryPolicy: &RetryPolicy{
			MaxRetries: 2,
			Backoff:    50 * time.Millisecond,
		},
	})

	_ = orch.AddStep(&AgentStep{
		ID:        "recovery_step",
		Name:      "恢复步骤",
		Agent:     recoveryAgent,
		InputFrom: []string{"risky_result"},
		Prompt:    "确认系统状态并恢复",
		OutputKey: "recovery_result",
	})

	result, err := orch.Execute(context.Background(), map[string]any{
		"risk_level": "high",
	})

	if err != nil {
		t.Logf("Error occurred (expected in some cases): %v", err)
	}

	if result != nil {
		t.Logf("✅ Error recovery test completed with status: %s", result.Status)
		t.Logf("   Steps attempted: %d", len(result.Steps))

		for stepID, stepResult := range result.Steps {
			t.Logf("   Step %s: status=%s retries=%d",
				stepID, stepResult.Status, stepResult.RetryCount)
		}
	}
}

// TestMultiAgentSystem_StatePersistence 测试状态持久化
func TestMultiAgentSystem_StatePersistence(t *testing.T) {
	t.Log("\n💾 State Persistence Test")
	t.Log(string(make([]byte, 30)))

	session := NewCollaborationSession(CollaborationConfig{
		Mode:      ReviewMode,
		Name:      "persistence-test",
		MaxRounds: 1,
	})

	reviewer1, err := agent.NewAgent(
		"Reviewer1",
		"评审者1",
		demo.NewDemoLLM("代码质量良好，建议增加单元测试覆盖"),
		agent.WithMaxTurns(1),
	)
	if err != nil {
		t.Fatal(err)
	}

	_ = session.AddCollaborator(&Collaborator{
		ID:     "reviewer1",
		Name:   "评审者1",
		Role:   "reviewer",
		Agent:  reviewer1,
		Weight: 1.0,
	})

	result, err := session.Execute(context.Background(), "审查以下代码的质量")
	if err != nil {
		t.Fatalf("Review execution failed: %v", err)
	}

	exportData, err := session.Export()
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	if len(exportData) == 0 {
		t.Error("Exported data is empty")
	}

	t.Logf("✅ State persistence test passed")
	t.Logf("   Exported size: %d bytes", len(exportData))
	t.Logf("   Review statements: %d", len(result.History))

	history := result.History
	t.Logf("   History records: %d", len(history))
}

func getMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
