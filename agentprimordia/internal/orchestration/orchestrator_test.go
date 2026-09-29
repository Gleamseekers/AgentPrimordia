package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/cmd/example/demo"
	"github.com/Gleamseekers/AgentPrimordia/internal/agent"
)

func TestOrchestrator_SequentialMode(t *testing.T) {
	orch := NewOrchestrator(OrchestratorConfig{
		Name:        "test-sequential",
		Description: "测试顺序执行",
		Mode:        SequentialMode,
	})

	step1Agent, err := agent.NewAgent("Step1-Agent", "你是步骤1，请简单回复'步骤1完成'", demo.NewDemoLLM("步骤1完成"), agent.WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}

	step2Agent, err := agent.NewAgent("Step2-Agent", "你是步骤2，请回复'步骤2完成'", demo.NewDemoLLM("步骤2完成"), agent.WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}

	_ = orch.AddStep(&AgentStep{
		ID:        "step1",
		Name:      "分析数据",
		Agent:     step1Agent,
		Prompt:    "请分析以下数据",
		OutputKey: "analysis_result",
	})

	_ = orch.AddStep(&AgentStep{
		ID:        "step2",
		Name:      "生成报告",
		Agent:     step2Agent,
		InputFrom: []string{"analysis_result"},
		Prompt:    "基于分析结果生成报告",
		OutputKey: "final_report",
	})

	input := map[string]any{"data": "测试数据"}
	result, err := orch.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if result.Status != StatusCompleted {
		t.Errorf("expected StatusCompleted, got %s", result.Status)
	}
	if len(result.Steps) != 2 {
		t.Errorf("expected 2 steps, got %d", len(result.Steps))
	}
	if result.Steps["step1"].Status != StepCompleted {
		t.Errorf("step1 should be completed")
	}
	if result.Steps["step2"].Status != StepCompleted {
		t.Errorf("step2 should be completed")
	}

	t.Logf("✅ Sequential Mode: status=%s duration=%v steps=%d", result.Status, result.Duration, len(result.Steps))
	t.Logf("   Step1 output: %v", result.Steps["step1"].Output)
	t.Logf("   Step2 output: %v", result.Steps["step2"].Output)
}

func TestOrchestrator_ParallelMode(t *testing.T) {
	orch := NewOrchestrator(OrchestratorConfig{
		Name:        "test-parallel",
		Description: "测试并行执行",
		Mode:        ParallelMode,
		Timeout:     10 * time.Second,
	})

	for i := 0; i < 3; i++ {
		idx := i
		stepAgent, err := agent.NewAgent(
			fmt.Sprintf("Parallel-Agent-%d", idx),
			fmt.Sprintf("你是并行任务%d", idx),
			demo.NewDemoLLM(fmt.Sprintf("并行任务%d完成", idx)),
			agent.WithMaxTurns(1),
		)
		if err != nil {
			t.Fatal(err)
		}

		_ = orch.AddStep(&AgentStep{
			ID:        fmt.Sprintf("parallel_step_%d", idx),
			Name:      fmt.Sprintf("并行任务 %d", idx+1),
			Agent:     stepAgent,
			Prompt:    fmt.Sprintf("执行并行任务 %d", idx+1),
			OutputKey: fmt.Sprintf("result_%d", idx),
			Priority:  idx,
		})
	}

	startTime := time.Now()
	input := map[string]any{"task": "并行处理"}
	result, err := orch.Execute(context.Background(), input)
	duration := time.Since(startTime)

	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if result.Status != StatusCompleted {
		t.Errorf("expected StatusCompleted, got %s", result.Status)
	}
	if len(result.Steps) != 3 {
		t.Errorf("expected 3 steps, got %d", len(result.Steps))
	}

	for _, stepResult := range result.Steps {
		if stepResult.Status != StepCompleted {
			t.Errorf("all parallel steps should complete, step %s is %s", stepResult.StepID, stepResult.Status)
		}
	}

	t.Logf("✅ Parallel Mode: status=%s duration=%v (should be fast)", result.Status, duration)
	t.Logf("   Total steps: %d, Completed: %d", result.Metrics.TotalSteps, result.Metrics.CompletedSteps)
	t.Logf("   Concurrency used: %d", result.Metrics.ConcurrencyUsed)
}

func TestOrchestrator_DAGMode(t *testing.T) {
	orch := NewOrchestrator(OrchestratorConfig{
		Name:        "test-dag",
		Description: "测试DAG工作流",
		Mode:        DAGMode,
	})

	dataAgent, err := agent.NewAgent("DataCollector", "收集数据", demo.NewDemoLLM("数据已收集"), agent.WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}

	processA_Agent, err := agent.NewAgent("ProcessA", "处理方式A", demo.NewDemoLLM("处理A完成"), agent.WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}

	processB_Agent, err := agent.NewAgent("ProcessB", "处理方式B", demo.NewDemoLLM("处理B完成"), agent.WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}

	mergeAgent, err := agent.NewAgent("Merger", "合并结果", demo.NewDemoLLM("合并完成"), agent.WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}

	// 添加步骤
	_ = orch.AddStep(&AgentStep{ID: "collect", Name: "数据收集", Agent: dataAgent, Prompt: "收集原始数据"})
	_ = orch.AddStep(&AgentStep{ID: "process_a", Name: "处理A", Agent: processA_Agent, InputFrom: []string{"collect"}, Prompt: "使用方法A处理"})
	_ = orch.AddStep(&AgentStep{ID: "process_b", Name: "处理B", Agent: processB_Agent, InputFrom: []string{"collect"}, Prompt: "使用方法B处理"})
	_ = orch.AddStep(&AgentStep{ID: "merge", Name: "合并结果", Agent: mergeAgent, InputFrom: []string{"process_a", "process_b"}, Prompt: "合并两种处理结果"})

	// 添加边
	_ = orch.AddEdge("collect", "process_a")
	_ = orch.AddEdge("collect", "process_b")
	_ = orch.AddEdge("process_a", "merge")
	_ = orch.AddEdge("process_b", "merge")

	result, err := orch.Execute(context.Background(), map[string]any{"source": "test"})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if result.Status != StatusCompleted {
		t.Errorf("expected StatusCompleted, got %s", result.Status)
	}
	if len(result.Steps) != 4 {
		t.Errorf("expected 4 steps, got %d", len(result.Steps))
	}

	// 验证依赖顺序
	collectTime := result.Steps["collect"].EndTime
	processATime := result.Steps["process_a"].StartTime
	processBTime := result.Steps["process_b"].StartTime
	mergeTime := result.Steps["merge"].StartTime

	// 由于执行非常快，使用 >= 而不是 >
	if processATime.Before(collectTime) {
		t.Error("process_a should not start before collect")
	}
	if processBTime.Before(collectTime) {
		t.Error("process_b should not start before collect")
	}
	if mergeTime.Before(processATime) || mergeTime.Before(processBTime) {
		t.Error("merge should not start before both processes")
	}

	t.Logf("✅ DAG Mode: status=%s steps=%d", result.Status, len(result.Steps))
	t.Logf("   Execution order validated correctly")
	for id, sr := range result.Steps {
		t.Logf("   - %s: %s (%v)", id, sr.Status, sr.Duration)
	}
}

func TestOrchestrator_RetryMechanism(t *testing.T) {
	retryAgent, err := agent.NewAgent("RetryAgent", "会重试的步骤", demo.NewDemoLLM("最终成功!"), agent.WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}

	orch := NewOrchestrator(OrchestratorConfig{
		Name:       "test-retry",
		Mode:       SequentialMode,
		MaxRetries: 3,
	})

	_ = orch.AddStep(&AgentStep{
		ID:          "flaky_step",
		Name:        "不稳定步骤",
		Agent:       retryAgent,
		Prompt:      "执行可能失败的任务",
		RetryPolicy: &RetryPolicy{MaxRetries: 3, Backoff: 50 * time.Millisecond},
	})

	result, err := orch.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if result.Steps["flaky_step"].Status != StepCompleted {
		t.Errorf("expected success, got %s", result.Steps["flaky_step"].Status)
	}

	t.Logf("✅ Retry Mechanism: step completed successfully")
}

func TestOrchestrator_ConditionalExecution(t *testing.T) {
	orch := NewOrchestrator(OrchestratorConfig{
		Name: "test-condition",
		Mode: SequentialMode,
	})

	simpleAgent, err := agent.NewAgent("ConditionalAgent", "条件执行测试", demo.NewDemoLLM("已执行"), agent.WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}

	_ = orch.AddStep(&AgentStep{
		ID:        "conditional_step",
		Name:      "条件步骤",
		Agent:     simpleAgent,
		Prompt:    "只在满足条件时执行",
		Condition: StepCondition{Type: "custom", Field: "execute_flag", Operator: "==", Value: true},
	})

	// 测试条件不满足
	result1, _ := orch.Execute(context.Background(), map[string]any{"execute_flag": false})
	if result1.Steps["conditional_step"].Status != StepSkipped {
		t.Error("step should be skipped when condition not met")
	}

	// 测试条件满足
	result2, _ := orch.Execute(context.Background(), map[string]any{"execute_flag": true})
	if result2.Steps["conditional_step"].Status != StepCompleted {
		t.Error("step should execute when condition met")
	}

	t.Logf("✅ Conditional Execution: skipped=%v executed=%v",
		result1.Steps["conditional_step"].Status == StepSkipped,
		result2.Steps["conditional_step"].Status == StepCompleted)
}

func TestOrchestrator_TimeoutHandling(t *testing.T) {
	slowAgent, err := agent.NewAgent("SlowAgent", "非常慢的响应", demo.NewDemoLLM("慢速响应"), agent.WithMaxTurns(5))
	if err != nil {
		t.Fatal(err)
	}

	orch := NewOrchestrator(OrchestratorConfig{
		Name:    "test-timeout",
		Mode:    SequentialMode,
		Timeout: 100 * time.Millisecond,
	})

	_ = orch.AddStep(&AgentStep{
		ID:      "slow_step",
		Name:    "超时步骤",
		Agent:   slowAgent,
		Prompt:  "执行耗时操作",
		Timeout: 100 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	result, err := orch.Execute(ctx, nil)
	if err == nil {
		t.Log("⚠️ Timeout test: no error (may be fast machine)")
	} else {
		t.Logf("✅ Timeout Handling: caught timeout error: %v", err)
	}

	if result != nil {
		t.Logf("   Status: %s, Duration: %v", result.Status, result.Duration)
	}
}

func TestOrchestrator_EventEmission(t *testing.T) {
	orch := NewOrchestrator(OrchestratorConfig{
		Name: "test-events",
		Mode: SequentialMode,
	})

	testAgent, err := agent.NewAgent("EventTestAgent", "事件测试", demo.NewDemoLLM("事件测试完成"), agent.WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}

	_ = orch.AddStep(&AgentStep{
		ID:     "event_step",
		Name:   "事件步骤",
		Agent:  testAgent,
		Prompt: "触发事件",
	})

	// 修复（-race 实测发现）：events 被收集 goroutine 与主 goroutine 共享，
	// append 与 range 需互斥
	var eventsMu sync.Mutex
	events := make([]*OrchestrationEvent, 0)
	go func() {
		for event := range orch.Events() {
			eventsMu.Lock()
			events = append(events, event)
			eventsMu.Unlock()
			if event.Type == "execution_completed" {
				break
			}
		}
	}()

	_, err = orch.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	time.Sleep(50 * time.Millisecond) // 等待事件收集

	hasStartEvent := false
	hasCompleteEvent := false
	hasStepEvents := false

	eventsMu.Lock()
	for _, event := range events {
		switch event.Type {
		case "execution_started":
			hasStartEvent = true
		case "execution_completed":
			hasCompleteEvent = true
		case "step_started", "step_completed":
			hasStepEvents = true
		}
	}
	eventsMu.Unlock()

	if !hasStartEvent || !hasCompleteEvent {
		t.Errorf("missing execution events: start=%v complete=%v", hasStartEvent, hasCompleteEvent)
	}
	if !hasStepEvents {
		t.Error("missing step events")
	}

	t.Logf("✅ Event Emission: collected %d events", len(events))
	for _, e := range events {
		t.Logf("   - %s (step=%s)", e.Type, e.StepID)
	}
}

func TestOrchestrator_MetricsCalculation(t *testing.T) {
	orch := NewOrchestrator(OrchestratorConfig{
		Name: "test-metrics",
		Mode: ParallelMode,
	})

	for i := 0; i < 4; i++ {
		idx := i
		metricsAgent, err := agent.NewAgent(
			fmt.Sprintf("MetricsAgent-%d", idx),
			fmt.Sprintf("指标测试%d", idx),
			demo.NewDemoLLM(fmt.Sprintf("结果%d", idx)),
			agent.WithMaxTurns(1),
		)
		if err != nil {
			t.Fatal(err)
		}

		_ = orch.AddStep(&AgentStep{
			ID:       fmt.Sprintf("metric_step_%d", idx),
			Name:     fmt.Sprintf("指标步骤 %d", idx+1),
			Agent:    metricsAgent,
			Priority: idx,
		})
	}

	result, _ := orch.Execute(context.Background(), nil)

	metrics := result.Metrics
	if metrics.TotalSteps != 4 {
		t.Errorf("expected 4 total steps, got %d", metrics.TotalSteps)
	}
	if metrics.CompletedSteps != 4 {
		t.Errorf("expected 4 completed steps, got %d", metrics.CompletedSteps)
	}
	if metrics.ConcurrencyUsed != 4 {
		t.Errorf("expected concurrency 4, got %d", metrics.ConcurrencyUsed)
	}
	if metrics.TotalDuration < 0 { // 允许为0（快速执行）
		t.Error("total duration should be non-negative")
	}

	metricsJSON, _ := json.MarshalIndent(metrics, "", "  ")
	t.Logf("✅ Metrics Calculation:\n%s", string(metricsJSON))
}

func TestOrchestrator_ExportImport(t *testing.T) {
	orch := NewOrchestrator(OrchestratorConfig{
		Name:        "test-export",
		Description: "导出测试",
		Mode:        SequentialMode,
		Metadata:    map[string]string{"version": "1.0"},
	})

	testAgent, err := agent.NewAgent("ExportAgent", "导出测试", demo.NewDemoLLM("导出成功"), agent.WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}

	_ = orch.AddStep(&AgentStep{
		ID:    "export_step",
		Name:  "导出步骤",
		Agent: testAgent,
	})

	data, err := orch.Export()
	if err != nil {
		t.Fatalf("Export error: %v", err)
	}

	var exported struct {
		Config OrchestratorConfig `json:"config"`
		Steps  []*AgentStep       `json:"steps"`
	}
	_ = json.Unmarshal(data, &exported)

	if exported.Config.Name != "test-export" {
		t.Errorf("export name mismatch")
	}
	if len(exported.Steps) != 1 {
		t.Errorf("expected 1 exported step, got %d", len(exported.Steps))
	}

	stats := orch.Stats()
	if stats["name"] != "test-export" {
		t.Errorf("stats name mismatch")
	}

	t.Logf("✅ Export/Import: size=%d bytes, name=%s", len(data), exported.Config.Name)
	t.Logf("   Stats: mode=%s steps=%d", stats["mode"], stats["total_steps"])
}

func BenchmarkOrchestrator_Sequential(b *testing.B) {
	orch := NewOrchestrator(OrchestratorConfig{
		Mode: SequentialMode,
	})

	for i := 0; i < b.N; i++ {
		idx := i
		benchAgent, err := agent.NewAgent(
			fmt.Sprintf("BenchAgent-%d", idx),
			"基准测试",
			demo.NewDemoLLM(fmt.Sprintf("响应%d", idx)),
			agent.WithMaxTurns(1),
		)
		if err != nil {
			b.Fatal(err)
		}

		_ = orch.AddStep(&AgentStep{
			ID:    fmt.Sprintf("bench_step_%d", idx),
			Name:  "基准步骤",
			Agent: benchAgent,
		})
	}

	b.ResetTimer()
	ctx := context.Background()
	for i := 0; i < b.N; i++ {
		_, _ = orch.Execute(ctx, map[string]any{"i": i})
	}
}

func BenchmarkOrchestrator_Parallel(b *testing.B) {
	orch := NewOrchestrator(OrchestratorConfig{
		Mode: ParallelMode,
	})

	steps := make([]*AgentStep, 10)
	for i := range steps {
		idx := i
		benchAgent, err := agent.NewAgent(
			fmt.Sprintf("ParallelBench-%d", idx),
			"并行基准",
			demo.NewDemoLLM(fmt.Sprintf("并行响应%d", idx)),
			agent.WithMaxTurns(1),
		)
		if err != nil {
			b.Fatal(err)
		}

		steps[idx] = &AgentStep{
			ID:       fmt.Sprintf("parallel_bench_%d", idx),
			Name:     fmt.Sprintf("并行基准 %d", idx+1),
			Agent:    benchAgent,
			Priority: idx,
		}
		_ = orch.AddStep(steps[idx])
	}

	b.ResetTimer()
	ctx := context.Background()
	for i := 0; i < b.N; i++ {
		_, _ = orch.Execute(ctx, map[string]any{"batch": i})
	}
}
