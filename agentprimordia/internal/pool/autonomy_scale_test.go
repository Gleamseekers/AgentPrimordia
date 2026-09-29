// autonomy_scale_test.go — v4.2-2 Pool × autonomy 100+ 并发目标
//
// 验收标准：成功率/恢复率与 1 目标持平。
// 场景：100+ 自治目标经 Pool 并发调度（MaxConcurrency=100+），
// 目标执行复用真实 AutonomyRuntime（SubmitGoal → SetPlan → ExecuteGoal → CompleteGoal），
// 步骤执行器可注入瞬时故障测量恢复率。
package pool

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/autonomy"
)

// scaleStepExecutor 确定性步骤执行器：failOnce 置位时首个 fix 步骤失败一次后恢复。
type scaleStepExecutor struct {
	failOnce atomic.Bool
}

func (e *scaleStepExecutor) ExecuteStep(_ context.Context, step autonomy.PlanStep) (string, error) {
	if step.ID == "fix" && e.failOnce.CompareAndSwap(true, false) {
		return "", fmt.Errorf("模拟瞬时故障")
	}
	return "ok:" + step.ID, nil
}

// goalRunAgent 将一次自治目标执行包装为 pool 任务（实现 agent.Agent 接口）。
type goalRunAgent struct {
	rt       *autonomy.AutonomyRuntime
	executor *scaleStepExecutor
}

func newGoalPlan(id string) *autonomy.GoalPlan {
	return autonomy.NewGoalPlan(id, []autonomy.PlanStep{
		{ID: "collect", Description: "采集数据", Strategy: autonomy.StepStrategySequential},
		{ID: "fix", Description: "修复异常", DependsOn: []string{"collect"}, Strategy: autonomy.StepStrategySequential},
		{ID: "verify", Description: "验证结果", DependsOn: []string{"fix"}, Strategy: autonomy.StepStrategySequential},
	})
}

func (a *goalRunAgent) Run(ctx context.Context, input agent.Message) (*agent.Response, error) {
	goal := a.rt.SubmitGoal(input.Content, autonomy.GoalConfig{MaxRetries: 2})
	plan := newGoalPlan(goal.ID)
	if err := a.rt.SetPlan(goal.ID, plan); err != nil {
		return nil, err
	}
	if err := a.rt.ExecuteGoal(ctx, goal.ID); err != nil {
		return nil, err
	}
	if err := a.rt.CompleteGoal(goal.ID); err != nil {
		return nil, err
	}
	g, _ := a.rt.GetGoal(goal.ID)
	if g.State != autonomy.GoalDone {
		return nil, fmt.Errorf("goal %s 未完成: %s", goal.ID, g.State)
	}
	return &agent.Response{RequestID: goal.ID, Content: "goal done"}, nil
}

func (a *goalRunAgent) StreamRun(ctx context.Context, input agent.Message) (<-chan agent.StreamEvent, error) {
	resp, err := a.Run(ctx, input)
	ch := make(chan agent.StreamEvent, 1)
	if err == nil {
		ch <- agent.StreamEvent{Type: agent.StreamEventComplete, Content: resp.Content}
	}
	close(ch)
	return ch, err
}

func (a *goalRunAgent) Stop()                   {}
func (a *goalRunAgent) Stats() agent.AgentStats { return agent.AgentStats{} }
func (a *goalRunAgent) Name() string            { return "goal-run" }

// runGoalsThroughPool 经 Pool 并发执行 n 个自治目标，返回完成数与失败原因。
func runGoalsThroughPool(t *testing.T, n, concurrency int, exec *scaleStepExecutor, retries int) (completed int, failures []error) {
	t.Helper()
	rt := autonomy.NewAutonomyRuntime(autonomy.RuntimeConfig{StepExecutor: exec})
	p := NewPool(PoolConfig{
		MaxConcurrency: concurrency,
		Timeout:        60 * time.Second,
		RetryPolicy:    RetryPolicy{MaxRetries: retries, Backoff: 5 * time.Millisecond},
	})
	t.Cleanup(p.Close)
	p.SetAgentFactory(func(AgentFactoryConfig) agent.Agent {
		return &goalRunAgent{rt: rt, executor: exec}
	})

	ctx := context.Background()
	tasks := make([]TaskConfig, 0, n)
	for i := range n {
		tasks = append(tasks, TaskConfig{
			ID:     fmt.Sprintf("goal-%d", i),
			Title:  fmt.Sprintf("目标 %d", i),
			Prompt: fmt.Sprintf("目标任务 %d：采集→修复→验证", i),
		})
	}
	results, err := p.Dispatch(ctx, tasks)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	for _, r := range results {
		if r.Error != nil {
			failures = append(failures, r.Error)
			continue
		}
		if r.Response == nil || r.Response.Content != "goal done" {
			failures = append(failures, fmt.Errorf("goal %s 未正常完成", r.TaskID))
			continue
		}
		completed++
	}
	return completed, failures
}

// TestPoolAutonomy_100ConcurrentGoals 100+ 并发目标全部完成（成功率与单目标持平）。
func TestPoolAutonomy_100ConcurrentGoals(t *testing.T) {
	// 基线：单目标直跑（不经 Pool）
	baselineExec := &scaleStepExecutor{}
	rt := autonomy.NewAutonomyRuntime(autonomy.RuntimeConfig{StepExecutor: baselineExec})
	ctx := context.Background()
	for range 5 {
		g := rt.SubmitGoal("基线目标", autonomy.GoalConfig{MaxRetries: 2})
		if err := rt.SetPlan(g.ID, newGoalPlan(g.ID)); err != nil {
			t.Fatal(err)
		}
		if err := rt.ExecuteGoal(ctx, g.ID); err != nil {
			t.Fatalf("基线目标执行失败: %v", err)
		}
		if err := rt.CompleteGoal(g.ID); err != nil {
			t.Fatal(err)
		}
	}

	// 规模化：100 并发目标经 Pool
	exec := &scaleStepExecutor{}
	completed, failures := runGoalsThroughPool(t, 100, 100, exec, 0)
	if len(failures) > 0 {
		t.Fatalf("100 并发目标失败 %d 个：%v", len(failures), failures[0])
	}
	if completed != 100 {
		t.Fatalf("completed = %d, want 100（成功率与 1 目标持平）", completed)
	}
}

// TestPoolAutonomy_RecoveryParity 故障注入下恢复率与 1 目标持平。
func TestPoolAutonomy_RecoveryParity(t *testing.T) {
	// 单目标基线：瞬时故障经重试恢复
	baselineExec := &scaleStepExecutor{}
	baselineExec.failOnce.Store(true)
	rt := autonomy.NewAutonomyRuntime(autonomy.RuntimeConfig{StepExecutor: baselineExec})
	ctx := context.Background()
	g := rt.SubmitGoal("恢复基线", autonomy.GoalConfig{MaxRetries: 2})
	if err := rt.SetPlan(g.ID, newGoalPlan(g.ID)); err != nil {
		t.Fatal(err)
	}
	if err := rt.ExecuteGoal(ctx, g.ID); err != nil {
		t.Fatalf("单目标瞬时故障应经重试恢复: %v", err)
	}
	_ = rt.CompleteGoal(g.ID)

	// 规模化：100 并发目标，注入瞬时故障，Pool 重试恢复
	exec := &scaleStepExecutor{}
	exec.failOnce.Store(true)
	completed, failures := runGoalsThroughPool(t, 100, 100, exec, 1)
	if len(failures) > 0 {
		t.Fatalf("故障注入下失败 %d 个：%v", len(failures), failures[0])
	}
	if completed != 100 {
		t.Fatalf("recovered = %d, want 100（恢复率与 1 目标持平）", completed)
	}
}
