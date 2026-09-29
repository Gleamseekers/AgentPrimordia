package orchestration

import (
	"context"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/cmd/example/demo"
	"github.com/Gleamseekers/AgentPrimordia/internal/agent"
)

// slowDemoLLM 创建一个带延迟的 DemoLLM，延迟期间可被 ctx 取消
func slowDemoLLM(name string) *demo.DemoLLM {
	return demo.NewDemoLLM(name + "完成").WithDelay(5 * time.Second)
}

// TestOrchestrator_SequentialCancel 验证顺序模式下 ctx 取消能立即中止执行
func TestOrchestrator_SequentialCancel(t *testing.T) {
	orch := NewOrchestrator(OrchestratorConfig{
		Name:        "cancel-sequential",
		Description: "测试顺序执行取消",
		Mode:        SequentialMode,
	})

	// 第一个步骤使用慢 LLM，给 cancel 留出时间窗口
	step1, err := agent.NewAgent("SlowStep1", "你很慢", slowDemoLLM("slow"), agent.WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}
	step2, err := agent.NewAgent("SlowStep2", "你也很慢", slowDemoLLM("slow"), agent.WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}

	_ = orch.AddStep(&AgentStep{
		ID:     "slow1",
		Name:   "慢步骤1",
		Agent:  step1,
		Prompt: "慢慢处理",
	})
	_ = orch.AddStep(&AgentStep{
		ID:     "slow2",
		Name:   "慢步骤2",
		Agent:  step2,
		Prompt: "慢慢处理2",
	})

	ctx, cancel := context.WithCancel(context.Background())

	// 在短暂延迟后取消上下文
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err = orch.Execute(ctx, map[string]any{"data": "test"})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error from canceled context, got nil")
	}
	if elapsed > 3*time.Second {
		t.Errorf("should have returned quickly after cancel, took %v", elapsed)
	}
	t.Logf("✅ Sequential cancel: err=%v elapsed=%v", err, elapsed)
}

// TestOrchestrator_ParallelCancel 验证并行模式下 ctx 取消能避免启动所有 goroutine
func TestOrchestrator_ParallelCancel(t *testing.T) {
	orch := NewOrchestrator(OrchestratorConfig{
		Name:        "cancel-parallel",
		Description: "测试并行执行取消",
		Mode:        ParallelMode,
		Timeout:     30 * time.Second,
	})

	// 添加 5 个慢步骤，预期 cancel 后不会全部执行
	for i := 0; i < 5; i++ {
		idx := i
		stepAgent, err := agent.NewAgent("SlowParallel", "你很慢", slowDemoLLM("slow"), agent.WithMaxTurns(1))
		if err != nil {
			t.Fatal(err)
		}

		_ = orch.AddStep(&AgentStep{
			ID:        "pslow_" + time.Now().Format("150405") + string(rune('0'+idx)),
			Name:      "慢并行任务",
			Agent:     stepAgent,
			Prompt:    "慢慢并行处理",
			OutputKey: "result",
			Priority:  idx,
		})
	}

	ctx, cancel := context.WithCancel(context.Background())

	// 立即取消
	cancel()

	start := time.Now()
	_, err := orch.Execute(ctx, map[string]any{"task": "cancel-test"})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error from canceled context, got nil")
	}
	if elapsed > 2*time.Second {
		t.Errorf("should return fast with pre-cancelled ctx, took %v", elapsed)
	}
	t.Logf("✅ Parallel cancel: err=%v elapsed=%v", err, elapsed)
}

// TestOrchestrator_DAGCancel 验证 DAG 模式下 ctx 取消能中止拓扑遍历
func TestOrchestrator_DAGCancel(t *testing.T) {
	orch := NewOrchestrator(OrchestratorConfig{
		Name:        "cancel-dag",
		Description: "测试DAG执行取消",
		Mode:        DAGMode,
	})

	// 3 个串行依赖的慢步骤
	step1, err := agent.NewAgent("DAGSlow1", "", slowDemoLLM("dagslow"), agent.WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}
	step2, err := agent.NewAgent("DAGSlow2", "", slowDemoLLM("dagslow"), agent.WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}
	step3, err := agent.NewAgent("DAGSlow3", "", slowDemoLLM("dagslow"), agent.WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}

	_ = orch.AddStep(&AgentStep{ID: "d1", Name: "DAG慢1", Agent: step1, Prompt: "慢"})
	_ = orch.AddStep(&AgentStep{ID: "d2", Name: "DAG慢2", Agent: step2, Prompt: "慢"})
	_ = orch.AddStep(&AgentStep{ID: "d3", Name: "DAG慢3", Agent: step3, Prompt: "慢"})

	_ = orch.AddEdge("d1", "d2")
	_ = orch.AddEdge("d2", "d3")

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err = orch.Execute(ctx, map[string]any{"data": "test"})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error from canceled context, got nil")
	}
	if elapsed > 3*time.Second {
		t.Errorf("should abort DAG traversal on cancel, took %v", elapsed)
	}
	t.Logf("✅ DAG cancel: err=%v elapsed=%v", err, elapsed)
}

// TestOrchestrator_SequentialNoCancel 验证正常执行（无取消）不受影响
func TestOrchestrator_SequentialNoCancel(t *testing.T) {
	orch := NewOrchestrator(OrchestratorConfig{
		Name: "no-cancel-sequential",
		Mode: SequentialMode,
	})

	step1, err := agent.NewAgent("FastStep1", "", demo.NewDemoLLM("fast1"), agent.WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}
	step2, err := agent.NewAgent("FastStep2", "", demo.NewDemoLLM("fast2"), agent.WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}

	_ = orch.AddStep(&AgentStep{ID: "f1", Name: "快步骤1", Agent: step1, Prompt: "快"})
	_ = orch.AddStep(&AgentStep{ID: "f2", Name: "快步骤2", Agent: step2, Prompt: "快"})

	result, err := orch.Execute(context.Background(), map[string]any{"data": "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != StatusCompleted {
		t.Errorf("expected completed, got %s", result.Status)
	}
	if len(result.Steps) != 2 {
		t.Errorf("expected 2 steps, got %d", len(result.Steps))
	}
	t.Logf("✅ Sequential no-cancel: all %d steps completed", len(result.Steps))
}
