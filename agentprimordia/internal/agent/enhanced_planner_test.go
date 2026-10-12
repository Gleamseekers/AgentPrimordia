// enhanced_planner_test.go — v7.6 EnhancedPlanner 护栏（D6）在 ReAct 循环层的验收。
//
// 证据锚点：docs/v7.2-验收实验报告.md §2.2——EnhancedPlanner 递归分解策略
// 反向显著 -47.8pp（p=0.001，11:0），根因「规划开销 > 规划收益」。
// v7.3 修复项（§5.1）：规划 turns ≤ 总 turns 的 20%、递归分解深度 ≤ 2 层、
// 超限即停止规划退回直接执行。
//
// 本文件从 Agent 使用者视角验收第三条：护栏超限时请求不失败，而是退回
// 直接执行（ReAct 普通循环）——这正是 v7.2 B 臂 600s 超时全军覆没的解药。
package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/planning"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm"
)

// plannerGuardCapable 提供 planner 能力的最小 Agent 包装（与 self_healing_test.go 同型）。
// 不实现 CheckpointCapable——getCheckpointStore 返回 nil 时保存检查点是 no-op。
type plannerGuardCapable struct {
	planner planning.Planner
}

func (m *plannerGuardCapable) GetPlanner() planning.Planner { return m.planner }
func (m *plannerGuardCapable) Run(_ context.Context, _ Message) (*Response, error) {
	return nil, errors.New("not used")
}
func (m *plannerGuardCapable) StreamRun(_ context.Context, _ Message) (<-chan StreamEvent, error) {
	return nil, errors.New("not used")
}
func (m *plannerGuardCapable) Stop()             {}
func (m *plannerGuardCapable) Stats() AgentStats { return AgentStats{} }
func (m *plannerGuardCapable) Name() string      { return "planner-guard-mock" }

// guardPlanJSON 两步依赖计划（供规划 LLM 返回）。
const guardPlanJSON = `[{"id":"1","description":"第一步"},{"id":"2","description":"第二步","depends_on":["1"]}]`

// TestEnhancedPlannerGuard_PlansWithinBudget 未超限时正常走 plan 路径。
func TestEnhancedPlannerGuard_PlansWithinBudget(t *testing.T) {
	planLLM := llm.NewMockLLM(t).WithResponse(guardPlanJSON)
	// 25 turns × 20% = 5 个规划 turn，远够一次规划
	ep := planning.NewEnhancedPlannerWithBudget(planLLM, nil, 25)

	a := newReActAgent(ReActConfig{
		Name:     "guard-plan",
		MaxTurns: 10,
		Model:    &outputGuardMockProvider{content: "不应走到这里"},
	})
	a.self = &plannerGuardCapable{planner: ep}
	a.subtaskExecutor = func(_ context.Context, task planning.SubTask, _ []Message, _ loopConfig) (*Response, error) {
		return &Response{Content: "done:" + task.ID}, nil
	}

	resp, err := a.Run(context.Background(), Message{Role: RoleUser, Content: "复杂任务"})
	if err != nil {
		t.Fatalf("Run 失败: %v", err)
	}
	if resp.Content != "done:2" {
		t.Errorf("最终输出 = %q, want done:2（来自 plan 路径）", resp.Content)
	}
	if planLLM.CallCount() != 1 {
		t.Errorf("规划 LLM 调用次数 = %d, want 1", planLLM.CallCount())
	}
}

// TestEnhancedPlannerGuard_FallsBackToDirectExecution turns 预算为 0 时，
// 规划被护栏拦截（LLM 都不调用），请求退回直接执行且不报错。
func TestEnhancedPlannerGuard_FallsBackToDirectExecution(t *testing.T) {
	planLLM := llm.NewMockLLM(t).WithResponse(guardPlanJSON)
	// 4 × 20% = 0.8 → 0 个规划 turn：一个都不允许
	ep := planning.NewEnhancedPlannerWithBudget(planLLM, nil, 4)

	a := newReActAgent(ReActConfig{
		Name:     "guard-fallback",
		MaxTurns: 10,
		Model:    &outputGuardMockProvider{content: "直接执行答案"},
	})
	a.self = &plannerGuardCapable{planner: ep}

	resp, err := a.Run(context.Background(), Message{Role: RoleUser, Content: "复杂任务"})
	if err != nil {
		t.Fatalf("护栏超限不应让请求失败，应退回直接执行: %v", err)
	}
	if resp.Content != "直接执行答案" {
		t.Errorf("最终输出 = %q, want 直接执行答案（回退路径）", resp.Content)
	}
	if planLLM.CallCount() != 0 {
		t.Errorf("护栏应在 LLM 调用前拦截，实际规划调用 %d 次", planLLM.CallCount())
	}
	if g := ep.PlannerGuardOf(); g == nil || !g.Stopped() {
		t.Error("护栏应已熔断（停止规划）")
	}
}

// TestEnhancedPlannerGuard_StopsReplanAndDegrades 规划预算 1 turn：
// 首次规划成功 → 子任务失败 → replan 被护栏拦截 → 自愈降级到普通 runLoop。
// 对应 v7.2 的失败模式：规划无限追加直到超时，必须能被预算闸刀切断。
func TestEnhancedPlannerGuard_StopsReplanAndDegrades(t *testing.T) {
	planLLM := llm.NewMockLLM(t).WithResponse(
		`[{"id":"x1","description":"BAD step"},{"id":"x2","description":"x2","depends_on":["x1"]}]`)
	// 5 × 20% = 1 个规划 turn：只够首次规划，replan 必须被拦
	ep := planning.NewEnhancedPlannerWithBudget(planLLM, nil, 5)

	a := newReActAgent(ReActConfig{
		Name:     "guard-degrade",
		MaxTurns: 10,
		Model:    &outputGuardMockProvider{content: "降级成功"},
	})
	a.self = &plannerGuardCapable{planner: ep}
	a.subtaskExecutor = func(_ context.Context, task planning.SubTask, _ []Message, _ loopConfig) (*Response, error) {
		if strings.Contains(task.Description, "BAD") {
			return nil, errors.New("permanent failure")
		}
		return &Response{Content: "done:" + task.ID}, nil
	}

	resp, err := a.Run(context.Background(), Message{Role: RoleUser, Content: "复杂任务"})
	if err != nil {
		t.Fatalf("replan 被护栏拦截后应降级到直接执行: %v", err)
	}
	if resp.Content != "降级成功" {
		t.Errorf("最终输出 = %q, want 降级成功", resp.Content)
	}
	if planLLM.CallCount() != 1 {
		t.Errorf("规划 LLM 调用次数 = %d, want 1（replan 应被护栏拦截）", planLLM.CallCount())
	}

	var degradeFound bool
	for _, r := range a.Stats().PlanRecoveries {
		if r.Method == "degrade" {
			degradeFound = true
		}
	}
	if !degradeFound {
		t.Error("应记录 degrade 自愈动作（replan 被拦 → 降级）")
	}
}

// TestEnhancedPlannerGuard_TightenedBudgetStopsEarlier 显式收紧比例后更早闸断。
func TestEnhancedPlannerGuard_TightenedBudgetStopsEarlier(t *testing.T) {
	planLLM := llm.NewMockLLM(t).WithResponse(guardPlanJSON)
	// 100 turns × 5% = 5 个规划 turn，但收紧到 1 层深度 + 5% 比例
	ep := planning.NewEnhancedPlannerWithBudget(planLLM, nil, 100,
		planning.WithPlannerTurnRatio(0.05))

	g := ep.PlannerGuardOf()
	if g == nil {
		t.Fatal("护栏应已启用")
	}
	if got := g.MaxPlanTurns(); got != 5 {
		t.Fatalf("收紧后 MaxPlanTurns = %d, want 5", got)
	}

	// 预算内：正常规划
	if _, err := ep.GeneratePlan(context.Background(), "任务"); err != nil {
		t.Fatalf("预算内规划应成功: %v", err)
	}
	// 深度护栏收紧到 1 层：第 2 层分解被拒
	epTight := planning.NewEnhancedPlannerWithBudget(
		llm.NewMockLLM(t).WithResponse(guardPlanJSON), nil, 100,
		planning.WithMaxDecomposeDepth(1))
	if _, err := epTight.DecomposeAt(context.Background(), "任务", 1); err != nil {
		t.Fatalf("第 1 层分解应放行: %v", err)
	}
	if _, err := epTight.DecomposeAt(context.Background(), "任务", 2); !errors.Is(err, planning.ErrPlanDepthExceeded) {
		t.Fatalf("收紧到 1 层后第 2 层应被拒，got %v", err)
	}
}
