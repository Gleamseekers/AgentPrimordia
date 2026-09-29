package suite

// v3.3 自治执行基准测试
//
// 测量自治核心路径的性能指标：
//   - 目标分解（计划创建 + 校验）
//   - 步骤执行吞吐
//   - 检查点保存/恢复耗时
//   - 状态机转换开销

import (
	"context"
	"fmt"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent/autonomy"
)

// benchStepExecutor 基准测试用步骤执行器（无开销）
type benchStepExecutor struct{}

func (b *benchStepExecutor) ExecuteStep(_ context.Context, step autonomy.PlanStep) (string, error) {
	return "ok-" + step.ID, nil
}

// BenchmarkGoalCreation 目标创建开销
func BenchmarkGoalCreation(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = autonomy.NewAgentGoal("基准测试目标", autonomy.GoalConfig{
			AcceptanceCriteria: []string{"标准1", "标准2"},
			Priority:           autonomy.PriorityHigh,
			MaxRetries:         3,
		})
	}
}

// BenchmarkPlanCreation 计划创建 + 校验开销
func BenchmarkPlanCreation(b *testing.B) {
	steps := make([]autonomy.PlanStep, 10)
	for i := range steps {
		steps[i] = autonomy.PlanStep{
			ID:          fmt.Sprintf("s%d", i),
			Description: fmt.Sprintf("步骤 %d", i),
		}
		if i > 0 {
			steps[i].DependsOn = []string{fmt.Sprintf("s%d", i-1)}
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		plan := autonomy.NewGoalPlan("bench-goal", steps)
		_ = plan.Validate()
	}
}

// BenchmarkPlanExecution 计划执行吞吐（10 步骤链式依赖）
func BenchmarkPlanExecution(b *testing.B) {
	exec := autonomy.NewGoalExecutor(autonomy.GoalExecutorConfig{
		StepExecutor: &benchStepExecutor{},
		MaxRetries:   1,
	})

	steps := make([]autonomy.PlanStep, 10)
	for i := range steps {
		steps[i] = autonomy.PlanStep{
			ID:          fmt.Sprintf("s%d", i),
			Description: fmt.Sprintf("步骤 %d", i),
		}
		if i > 0 {
			steps[i].DependsOn = []string{fmt.Sprintf("s%d", i-1)}
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		plan := autonomy.NewGoalPlan("bench-goal", steps)
		_ = exec.Execute(context.Background(), plan)
	}
}

// BenchmarkParallelExecution 并行步骤执行吞吐
func BenchmarkParallelExecution(b *testing.B) {
	exec := autonomy.NewGoalExecutor(autonomy.GoalExecutorConfig{
		StepExecutor: &benchStepExecutor{},
		MaxRetries:   1,
	})

	steps := make([]autonomy.PlanStep, 8)
	for i := range steps {
		steps[i] = autonomy.PlanStep{
			ID:          fmt.Sprintf("p%d", i),
			Description: fmt.Sprintf("并行步骤 %d", i),
			Strategy:    autonomy.StepStrategyParallel,
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		plan := autonomy.NewGoalPlan("bench-parallel", steps)
		_ = exec.Execute(context.Background(), plan)
	}
}

// BenchmarkStateMachineTransition 状态机转换开销
func BenchmarkStateMachineTransition(b *testing.B) {
	sm := autonomy.NewStateMachine()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = sm.Apply(autonomy.GoalCreated, autonomy.GoalPlanned)
	}
}

// BenchmarkCheckpointSaveRestore 检查点保存/恢复开销
func BenchmarkCheckpointSaveRestore(b *testing.B) {
	store := &benchCheckpointStore{data: make(map[string]*autonomy.Checkpoint)}
	rm := autonomy.NewResumeManager(store)
	ctx := context.Background()

	steps := make([]autonomy.PlanStep, 5)
	for i := range steps {
		steps[i] = autonomy.PlanStep{ID: fmt.Sprintf("s%d", i), Description: "x"}
	}
	plan := autonomy.NewGoalPlan("bench-cp", steps)
	plan.MarkStepCompleted("s0")
	plan.MarkStepCompleted("s1")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = rm.SaveCheckpoint(ctx, "bench-cp", "", plan, autonomy.GoalExecuting)
		_, _ = rm.LoadCheckpoint(ctx, "bench-cp")
	}
}

// benchCheckpointStore 基准测试用检查点存储
type benchCheckpointStore struct {
	data map[string]*autonomy.Checkpoint
}

func (s *benchCheckpointStore) SaveCheckpoint(_ context.Context, cp *autonomy.Checkpoint) error {
	s.data[cp.GoalID] = cp
	return nil
}

func (s *benchCheckpointStore) LoadCheckpoint(_ context.Context, goalID string) (*autonomy.Checkpoint, error) {
	return s.data[goalID], nil
}

func (s *benchCheckpointStore) ListIncomplete(_ context.Context) ([]*autonomy.Checkpoint, error) {
	var result []*autonomy.Checkpoint
	for _, cp := range s.data {
		if !cp.Completed {
			result = append(result, cp)
		}
	}
	return result, nil
}
