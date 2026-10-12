// budget_test.go — v7.6 规划护栏（D6）测试：PlannerGuard + 带护栏的 EnhancedPlanner。
//
// 背景（v7.2 验收实验报告 docs/v7.2-验收实验报告.md §2.2 / §5.1）：
// EnhancedPlanner 的递归分解策略实测反向显著 -47.8pp（p=0.001，11:0），
// 根因是规划开销 > 规划收益。v7.3 修复项要求：
//   - 规划 turns ≤ 总 turns 上限的 20%；
//   - 递归分解深度 ≤ 2 层；
//   - 超限即停止规划、退回直接执行。
//
// 本文件固化上述三条护栏的默认值与可配置性。
package planning

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm"
)

// ===== 默认值即护栏值 =====

// TestDefaultPlannerBudgetIsGuardrailValue 默认阈值必须就是护栏值（20% / 深度 2）。
func TestDefaultPlannerBudgetIsGuardrailValue(t *testing.T) {
	b := DefaultPlannerBudget()
	if b.MaxPlanTurnRatio != 0.2 {
		t.Errorf("MaxPlanTurnRatio = %v, want 0.2（规划 turns ≤ 总 turns 的 20%%）", b.MaxPlanTurnRatio)
	}
	if b.MaxDecomposeDepth != 2 {
		t.Errorf("MaxDecomposeDepth = %d, want 2（递归分解深度 ≤ 2 层）", b.MaxDecomposeDepth)
	}

	g := NewPlannerGuard(25) // 25 turns 是 v7.2 报告中 ReAct 短程任务的轮次上限
	if got := g.MaxPlanTurns(); got != 5 {
		t.Errorf("MaxPlanTurns(25) = %d, want 5（25 × 20%%）", got)
	}
	if got := g.MaxDecomposeDepth(); got != 2 {
		t.Errorf("MaxDecomposeDepth() = %d, want 2", got)
	}
	if !g.AllowDepth(1) || !g.AllowDepth(2) {
		t.Error("深度 1、2 应在护栏内")
	}
	if g.AllowDepth(3) {
		t.Error("深度 3 超出护栏（≤ 2 层）")
	}
}

// TestPlannerGuardMaxPlanTurnsRounding 规划 turns 上限按总 turns × 比例向下取整。
func TestPlannerGuardMaxPlanTurnsRounding(t *testing.T) {
	cases := []struct {
		totalTurns int
		want       int
	}{
		{totalTurns: 25, want: 5},
		{totalTurns: 10, want: 2},
		{totalTurns: 5, want: 1},
		{totalTurns: 4, want: 0}, // 4 × 0.2 = 0.8 → 0：一个规划 turn 都不允许
		{totalTurns: 0, want: 5}, // 未显式绑定时按默认任务预算 25 计
		{totalTurns: -1, want: 5},
	}
	for _, c := range cases {
		g := NewPlannerGuard(c.totalTurns)
		if got := g.MaxPlanTurns(); got != c.want {
			t.Errorf("NewPlannerGuard(%d).MaxPlanTurns() = %d, want %d", c.totalTurns, got, c.want)
		}
	}
}

// ===== turns 护栏 =====

// TestPlannerGuardTurnsBudget 预算内可规划，超限即熔断（停止规划）。
func TestPlannerGuardTurnsBudget(t *testing.T) {
	g := NewPlannerGuard(10) // 10 × 20% = 2 个规划 turn
	if g.MaxPlanTurns() != 2 {
		t.Fatalf("MaxPlanTurns = %d, want 2", g.MaxPlanTurns())
	}
	if g.Stopped() {
		t.Fatal("初始不应处于熔断状态")
	}

	for i := 1; i <= 2; i++ {
		ok, reason := g.AcquirePlanTurn()
		if !ok {
			t.Fatalf("第 %d 次规划应被放行，原因: %s", i, reason)
		}
	}
	if g.Stopped() {
		t.Fatal("预算刚好用尽时不应提前熔断")
	}
	if got := g.PlanTurns(); got != 2 {
		t.Errorf("已占用规划 turns = %d, want 2", got)
	}

	ok, reason := g.AcquirePlanTurn()
	if ok {
		t.Fatal("超出预算后必须停止规划")
	}
	if reason == "" {
		t.Error("拒绝原因不能为空（调用方需要可读的降级理由）")
	}
	if !strings.Contains(reason, "20%") && !strings.Contains(reason, "预算") {
		t.Errorf("拒绝原因应说明预算口径，got %q", reason)
	}
	if !g.Stopped() {
		t.Error("超限后应置熔断位")
	}
}

// TestPlannerGuardZeroBudgetStopsPlanning 总 turns 过小导致预算为 0 时，一次规划都不允许。
func TestPlannerGuardZeroBudgetStopsPlanning(t *testing.T) {
	g := NewPlannerGuard(4) // 4 × 20% = 0.8 → 0
	ok, reason := g.AcquirePlanTurn()
	if ok {
		t.Fatal("预算为 0 时必须拒绝规划")
	}
	if !strings.Contains(reason, "0") {
		t.Errorf("原因应说明上限为 0，got %q", reason)
	}
	if !g.Stopped() {
		t.Error("应置熔断位")
	}
}

// TestPlannerGuardResetTask 任务边界复位后预算恢复（多任务复用同一 planner 的口径）。
func TestPlannerGuardResetTask(t *testing.T) {
	g := NewPlannerGuard(5) // 1 个规划 turn
	if ok, _ := g.AcquirePlanTurn(); !ok {
		t.Fatal("首次规划应放行")
	}
	if ok, _ := g.AcquirePlanTurn(); ok {
		t.Fatal("用尽后应拒绝")
	}

	g.ResetTask()
	if g.Stopped() {
		t.Error("复位后应清除熔断位")
	}
	if got := g.PlanTurns(); got != 0 {
		t.Errorf("复位后已占用 turns = %d, want 0", got)
	}
	if ok, reason := g.AcquirePlanTurn(); !ok {
		t.Fatalf("复位后应重新放行，原因: %s", reason)
	}
}

// TestPlannerGuardSetTotalTurns 运行期可绑定总 turns 上限（如从 AgentConfig.MaxTurns 注入）。
func TestPlannerGuardSetTotalTurns(t *testing.T) {
	g := NewPlannerGuard(4) // 预算 0
	if ok, _ := g.AcquirePlanTurn(); ok {
		t.Fatal("初始预算为 0 应拒绝")
	}
	g.ResetTask()
	g.SetTotalTurns(50) // 50 × 20% = 10
	if got := g.MaxPlanTurns(); got != 10 {
		t.Errorf("SetTotalTurns(50) 后 MaxPlanTurns = %d, want 10", got)
	}
	if ok, _ := g.AcquirePlanTurn(); !ok {
		t.Error("放宽总 turns 后应放行")
	}
}

// TestPlannerGuardConcurrent 并发守门：占用次数绝不得超过上限（-race 下验证并发安全）。
// AcquirePlanTurn 是「检查 + 记账」的原子操作，50 个goroutine 抢 2 个名额时
// 必须恰好放行 2 个——早期把检查与记账拆成两次调用，实测会超发（TOCTOU）。
func TestPlannerGuardConcurrent(t *testing.T) {
	const goroutines = 50
	g := NewPlannerGuard(10) // 上限 2
	var wg sync.WaitGroup
	var allowed int64
	var mu sync.Mutex
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, _ := g.AcquirePlanTurn(); ok {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if got := g.PlanTurns(); got != g.MaxPlanTurns() {
		t.Errorf("并发放行/占用次数 = %d, want 恰好 %d（上限）", got, g.MaxPlanTurns())
	}
	if int(allowed) != g.MaxPlanTurns() {
		t.Errorf("并发放行次数 = %d, want 恰好 %d（上限）", allowed, g.MaxPlanTurns())
	}
	if !g.Stopped() {
		t.Error("预算耗尽后应置熔断位")
	}
}

// ===== 显式配置可收紧 =====

// TestPlannerGuardOptionsTighten 选项可把阈值收紧到护栏值以下。
func TestPlannerGuardOptionsTighten(t *testing.T) {
	g := NewPlannerGuard(25, WithPlannerTurnRatio(0.1), WithMaxDecomposeDepth(1))
	if got := g.MaxPlanTurns(); got != 2 { // 25 × 10% = 2.5 → 2
		t.Errorf("MaxPlanTurns = %d, want 2", got)
	}
	if !g.AllowDepth(1) {
		t.Error("深度 1 应放行")
	}
	if g.AllowDepth(2) {
		t.Error("收紧到 1 层后深度 2 应拒绝")
	}

	// 比例 0 = 完全关闭规划（合法的"关"开关）
	off := NewPlannerGuard(25, WithPlannerTurnRatio(0))
	if got := off.MaxPlanTurns(); got != 0 {
		t.Errorf("比例 0 时 MaxPlanTurns = %d, want 0", got)
	}
	if ok, _ := off.AcquirePlanTurn(); ok {
		t.Error("比例 0 时必须拒绝规划")
	}

	// 非法负值收敛到 0，比例 >1 收敛到 1
	neg := NewPlannerGuard(25, WithPlannerTurnRatio(-0.5))
	if got := neg.MaxPlanTurns(); got != 0 {
		t.Errorf("负比例应收敛为 0，got %d", got)
	}
	full := NewPlannerGuard(10, WithPlannerTurnRatio(1.5))
	if got := full.MaxPlanTurns(); got != 10 {
		t.Errorf("比例 >1 应收敛为 1（10 × 100%% = 10），got %d", got)
	}
	deep := NewPlannerGuard(25, WithMaxDecomposeDepth(-1))
	if got := deep.MaxDecomposeDepth(); got != 0 {
		t.Errorf("负深度应收敛为 0，got %d", got)
	}
}

// ===== 带护栏的 EnhancedPlanner =====

// subtasksJSON 两步计划（供 MockLLM 返回）。
const subtasksJSON = `[{"id":"1","description":"第一步"},{"id":"2","description":"第二步","depends_on":["1"]}]`

// TestNewEnhancedPlannerWithBudgetAllowsPlanning 预算内正常规划（护栏不误伤）。
func TestNewEnhancedPlannerWithBudgetAllowsPlanning(t *testing.T) {
	mock := llm.NewMockLLM(t).WithResponse(subtasksJSON)
	ep := NewEnhancedPlannerWithBudget(mock, nil, 25)

	g := ep.PlannerGuardOf()
	if g == nil {
		t.Fatal("NewEnhancedPlannerWithBudget 必须启用护栏")
	}
	if got := g.MaxPlanTurns(); got != 5 {
		t.Fatalf("默认护栏 MaxPlanTurns = %d, want 5", got)
	}

	plan, err := ep.GeneratePlan(context.Background(), "复杂任务")
	if err != nil {
		t.Fatalf("预算内规划不应失败: %v", err)
	}
	if len(plan.SubTasks) != 2 {
		t.Errorf("子任务数 = %d, want 2", len(plan.SubTasks))
	}
	if mock.CallCount() != 1 {
		t.Errorf("LLM 调用次数 = %d, want 1", mock.CallCount())
	}
}

// TestNewEnhancedPlannerWithoutBudgetHasNoGuard 旧构造器保持无护栏（向后兼容）。
func TestNewEnhancedPlannerWithoutBudgetHasNoGuard(t *testing.T) {
	mock := llm.NewMockLLM(t).
		WithResponse(subtasksJSON).
		WithResponse(subtasksJSON).
		WithResponse(subtasksJSON)
	ep := NewEnhancedPlanner(mock, nil)
	if g := ep.PlannerGuardOf(); g != nil {
		t.Error("未启用预算的 EnhancedPlanner 不应有护栏")
	}
	// 无护栏时行为与原来一致：可反复规划
	for i := 0; i < 3; i++ {
		if _, err := ep.GeneratePlan(context.Background(), "任务"); err != nil {
			t.Fatalf("第 %d 次规划失败: %v", i+1, err)
		}
	}
}

// TestNewEnhancedPlannerWithBudgetStopsWhenExhausted turns 超限即停止规划，
// 且在调用 LLM 之前拦截（不浪费 token），错误可被 errors.Is 识别。
func TestNewEnhancedPlannerWithBudgetStopsWhenExhausted(t *testing.T) {
	mock := llm.NewMockLLM(t)
	ep := NewEnhancedPlannerWithBudget(mock, nil, 4) // 4 × 20% = 0.8 → 0 个规划 turn

	_, err := ep.GeneratePlan(context.Background(), "复杂任务")
	if err == nil {
		t.Fatal("预算为 0 时必须停止规划")
	}
	if !errors.Is(err, ErrPlanBudgetExceeded) {
		t.Fatalf("错误应可被 errors.Is(ErrPlanBudgetExceeded) 识别，got %v", err)
	}
	if mock.CallCount() != 0 {
		t.Errorf("护栏应在 LLM 调用前拦截，实际调用 %d 次", mock.CallCount())
	}
	if g := ep.PlannerGuardOf(); g == nil || !g.Stopped() {
		t.Error("超限后护栏应处于熔断状态")
	}
}

// TestNewEnhancedPlannerWithBudgetExhaustsAfterMaxPlanTurns 第 N+1 次规划被拦截。
func TestNewEnhancedPlannerWithBudgetExhaustsAfterMaxPlanTurns(t *testing.T) {
	mock := llm.NewMockLLM(t).WithResponse(subtasksJSON).WithResponse(subtasksJSON)
	ep := NewEnhancedPlannerWithBudget(mock, nil, 10) // 10 × 20% = 2 个规划 turn

	for i := 0; i < 2; i++ {
		if _, err := ep.GeneratePlan(context.Background(), "任务"); err != nil {
			t.Fatalf("第 %d 次规划应成功: %v", i+1, err)
		}
	}
	_, err := ep.GeneratePlan(context.Background(), "任务")
	if !errors.Is(err, ErrPlanBudgetExceeded) {
		t.Fatalf("第 3 次规划应被护栏拦截，got %v", err)
	}
	if mock.CallCount() != 2 {
		t.Errorf("LLM 调用次数 = %d, want 2（第 3 次被拦截）", mock.CallCount())
	}
}

// TestEnhancedPlannerDecomposeAtDepthGuard 递归分解深度护栏：
// 层内放行，超层拒绝并提示退回直接执行。
func TestEnhancedPlannerDecomposeAtDepthGuard(t *testing.T) {
	mock := llm.NewMockLLM(t).WithResponse(subtasksJSON)
	ep := NewEnhancedPlannerWithBudget(mock, nil, 25)

	if _, err := ep.DecomposeAt(context.Background(), "任务", 2); err != nil {
		t.Fatalf("深度 2 在护栏内，应放行: %v", err)
	}
	_, err := ep.DecomposeAt(context.Background(), "任务", 3)
	if err == nil {
		t.Fatal("深度 3 超过护栏（≤ 2 层），必须拒绝")
	}
	if !errors.Is(err, ErrPlanDepthExceeded) {
		t.Fatalf("错误应可被 errors.Is(ErrPlanDepthExceeded) 识别，got %v", err)
	}
	if !strings.Contains(err.Error(), "3") {
		t.Errorf("错误应说明超限深度，got %v", err)
	}
}

// TestEnhancedPlannerDecomposeAtTightenedDepth 显式收紧深度后立即生效。
func TestEnhancedPlannerDecomposeAtTightenedDepth(t *testing.T) {
	mock := llm.NewMockLLM(t).WithResponse(subtasksJSON)
	ep := NewEnhancedPlannerWithBudget(mock, nil, 25, WithMaxDecomposeDepth(1))

	if _, err := ep.DecomposeAt(context.Background(), "任务", 1); err != nil {
		t.Fatalf("深度 1 应放行: %v", err)
	}
	if _, err := ep.DecomposeAt(context.Background(), "任务", 2); !errors.Is(err, ErrPlanDepthExceeded) {
		t.Fatalf("收紧到 1 层后深度 2 应被拒绝，got %v", err)
	}
}

// TestPlannerGuardOfNilSafe 护栏访问器对 nil / 未启用护栏均返回 nil。
func TestPlannerGuardOfNilSafe(t *testing.T) {
	var nilEP *EnhancedPlanner
	if g := nilEP.PlannerGuardOf(); g != nil {
		t.Error("nil EnhancedPlanner 的护栏访问应返回 nil")
	}
	ep := NewEnhancedPlanner(llm.NewMockLLM(t), nil)
	if g := ep.PlannerGuardOf(); g != nil {
		t.Error("未启用护栏时应返回 nil")
	}
}

// TestPlannerGuardGuardrailsReplannerAndRecovery 护栏同时覆盖 replan / 恢复路径
// （二者与 Base 共用同一 provider，规划 LLM 调用都记入预算）。
func TestPlannerGuardGuardrailsReplannerAndRecovery(t *testing.T) {
	mock := llm.NewMockLLM(t)
	ep := NewEnhancedPlannerWithBudget(mock, nil, 5) // 1 个规划 turn

	plan := &Plan{Goal: "g", SubTasks: []SubTask{{ID: "1", Description: "s1"}}}
	// ShouldReplan 走同一 provider：首次放行并消耗唯一预算
	_, _ = ep.Replanner.ShouldReplan(context.Background(), plan, "观察")
	if mock.CallCount() != 1 {
		t.Fatalf("ShouldReplan 应消耗 1 个规划 turn，got %d", mock.CallCount())
	}
	// 预算已尽：Replan 必须被护栏拦截
	if _, err := ep.Replanner.Replan(context.Background(), plan, "失败"); !errors.Is(err, ErrPlanBudgetExceeded) {
		t.Fatalf("Replan 应被护栏拦截，got %v", err)
	}
	// Recovery.Recover 同样被拦截
	if _, err := ep.Recovery.Recover(context.Background(), plan, "1"); !errors.Is(err, ErrPlanBudgetExceeded) {
		t.Fatalf("Recovery.Recover 应被护栏拦截，got %v", err)
	}
}
