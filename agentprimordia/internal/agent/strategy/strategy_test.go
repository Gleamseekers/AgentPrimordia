// strategy_test.go — v5.2 认知引擎策略内核测试（MockLLM 驱动，无真实网络）
package strategy

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/planning"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
)

// fakeEngine 测试用引擎原语：按脚本顺序返回 LLM 响应，记录工具调用
type fakeEngine struct {
	mu        sync.Mutex
	responses []string // LLM 响应队列
	calls     int
	toolCalls [][2]string // (name, args)
	toolFn    func(name, args string) string
}

func (e *fakeEngine) Complete(_ context.Context, _ *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.calls >= len(e.responses) {
		return nil, fmt.Errorf("脚本耗尽")
	}
	r := e.responses[e.calls]
	e.calls++
	usage := llm.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}
	return &llm.CompletionResponse{Content: r, Usage: usage}, nil
}

func (e *fakeEngine) ExecuteTool(_ context.Context, name, args string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.toolCalls = append(e.toolCalls, [2]string{name, args})
	if e.toolFn != nil {
		return e.toolFn(name, args), nil
	}
	return "工具结果:" + name, nil
}

// ===== Registry =====

type stubStrategy struct {
	name string
	out  string
}

func (s *stubStrategy) Name() string { return s.name }
func (s *stubStrategy) Run(_ context.Context, _ Engine, _ Task) (*Result, error) {
	return &Result{Output: s.out}, nil
}

func TestRegistryRegisterGet(t *testing.T) {
	r := NewRegistry()
	r.Register(&stubStrategy{name: "a", out: "A"})
	got, err := r.Get("a")
	if err != nil || got.Name() != "a" {
		t.Fatalf("取用失败: %v", err)
	}
	if _, err := r.Get("missing"); err == nil {
		t.Error("未注册策略应报错")
	}
}

func TestRegistryDefaultHotSwitch(t *testing.T) {
	r := NewRegistry()
	r.Register(&stubStrategy{name: "react", out: "R"})
	r.Register(&stubStrategy{name: "plan", out: "P"})

	if _, err := r.Default(); err == nil {
		t.Fatal("未设默认应报错")
	}
	if err := r.SetDefault("react"); err != nil {
		t.Fatalf("设默认失败: %v", err)
	}
	s1, _ := r.Default()
	if s1.Name() != "react" {
		t.Fatal("默认应为 react")
	}
	// 热切换
	if err := r.SetDefault("plan"); err != nil {
		t.Fatalf("热切换失败: %v", err)
	}
	s2, _ := r.Default()
	if s2.Name() != "plan" {
		t.Fatal("切换后默认应为 plan")
	}
	if err := r.SetDefault("ghost"); err == nil {
		t.Error("未注册策略设默认应报错")
	}
}

func TestRegistryConcurrentHotSwitch(t *testing.T) {
	r := NewRegistry()
	r.Register(&stubStrategy{name: "x", out: "X"})
	r.Register(&stubStrategy{name: "y", out: "Y"})
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(i int) { defer wg.Done(); _ = r.SetDefault([]string{"x", "y"}[i%2]) }(i)
		go func() { defer wg.Done(); _, _ = r.Default() }()
	}
	wg.Wait()
}

// ===== ReActStrategy =====

func TestReActToolThenFinal(t *testing.T) {
	eng := &fakeEngine{
		responses: []string{
			`{"tool": "search", "args": {"q": "go 1.26"}}`,
			`{"final": "答案是 42"}`,
		},
	}
	res, err := (&ReActStrategy{}).Run(context.Background(), eng, Task{Goal: "测试目标"})
	if err != nil {
		t.Fatalf("ReAct 失败: %v", err)
	}
	if res.Output != "答案是 42" {
		t.Errorf("输出错误: %q", res.Output)
	}
	if len(eng.toolCalls) != 1 || eng.toolCalls[0][0] != "search" {
		t.Errorf("工具调用记录错误: %v", eng.toolCalls)
	}
	if res.Turns != 2 {
		t.Errorf("轮数应为 2，得到 %d", res.Turns)
	}
	if res.Usage.TotalTokens != 30 {
		t.Errorf("token 用量累计错误: %d", res.Usage.TotalTokens)
	}
}

func TestReActMaxTurnsExhausted(t *testing.T) {
	eng := &fakeEngine{
		responses: []string{`{"tool": "loop", "args": {}}`, `{"tool": "loop", "args": {}}`},
	}
	_, err := (&ReActStrategy{}).Run(context.Background(), eng, Task{Goal: "死循环", MaxTurns: 2})
	if err == nil || !strings.Contains(err.Error(), "最大轮数") {
		t.Fatalf("应报最大轮数错误，得到 %v", err)
	}
}

// ===== PlanExecuteReflectStrategy =====

func TestPlanReflectPassFirstAttempt(t *testing.T) {
	eng := &fakeEngine{
		responses: []string{
			`[{"id":"1","description":"步骤一"},{"id":"2","description":"步骤二"}]`, // 计划
			`步骤一完成`,            // 执行 1
			`步骤二完成`,            // 执行 2
			`{"passed": true}`, // 反思通过
		},
	}
	res, err := (&PlanExecuteReflectStrategy{}).Run(context.Background(), eng, Task{Goal: "建站"})
	if err != nil {
		t.Fatalf("Plan-Reflect 失败: %v", err)
	}
	if !strings.Contains(res.Output, "步骤二完成") {
		t.Errorf("输出应包含最后一步结果: %q", res.Output)
	}
	if res.Turns != 4 {
		t.Errorf("轮数应为 4（计划+2执行+反思），得到 %d", res.Turns)
	}
}

func TestPlanReflectReplanOnFail(t *testing.T) {
	eng := &fakeEngine{
		responses: []string{
			`[{"id":"1","description":"方案A"}]`,        // 第一次计划
			`无法完成`,                                    // 执行失败信号
			`{"passed": false, "reasons": ["方案不可行"]}`, // 反思不通过
			`[{"id":"1","description":"方案B"}]`,        // 重规划
			`方案B完成`,                                   //
			`{"passed": true}`,                        // 二次反思通过
		},
	}
	res, err := (&PlanExecuteReflectStrategy{}).Run(context.Background(), eng, Task{Goal: "迁移"})
	if err != nil {
		t.Fatalf("重规划路径失败: %v", err)
	}
	if !strings.Contains(res.Output, "方案B完成") {
		t.Errorf("重规划后输出错误: %q", res.Output)
	}
}

// ===== VerificationLoopStrategy =====

func TestVerificationLoopPassAndCorrect(t *testing.T) {
	// 第一版缺关键词 → 修正后通过
	eng := &fakeEngine{
		responses: []string{
			`初版没有要素`,
			`修正版包含 必备要素A 和 必备要素B`,
		},
	}
	v := &KeywordVerifier{Requires: []string{"必备要素A", "必备要素B"}}
	res, err := (&VerificationLoopStrategy{Verifier: v}).Run(context.Background(), eng, Task{Goal: "写摘要"})
	if err != nil {
		t.Fatalf("验证循环失败: %v", err)
	}
	if !res.Verification.Passed || !strings.Contains(res.Output, "必备要素B") {
		t.Errorf("修正后应通过: %+v / %q", res.Verification, res.Output)
	}
}

func TestVerificationLoopExhausted(t *testing.T) {
	eng := &fakeEngine{
		responses: []string{"版本甲", "版本乙", "版本丙"},
	}
	v := &KeywordVerifier{Requires: []string{"必备签章"}}
	_, err := (&VerificationLoopStrategy{Verifier: v, MaxCorrections: 1}).Run(context.Background(), eng, Task{Goal: "任务"})
	if err == nil || !strings.Contains(err.Error(), "校验未通过") {
		t.Fatalf("修正预算耗尽应报错，得到 %v", err)
	}
}

func TestVerificationLoopNilVerifier(t *testing.T) {
	if _, err := (&VerificationLoopStrategy{}).Run(context.Background(), &fakeEngine{}, Task{}); err == nil {
		t.Fatal("nil verifier 应报错")
	}
}

// ===== AdaptiveBudget =====

func TestAdaptiveBudgetShallowForSimple(t *testing.T) {
	b := AdaptiveBudget(TaskSignals{Goal: "修复拼写错误"})
	if b.MaxTurns > 4 || b.MaxCorrections != 0 {
		t.Errorf("简单任务应为浅预算: %+v", b)
	}
}

func TestAdaptiveBudgetDeepForComplex(t *testing.T) {
	b := AdaptiveBudget(TaskSignals{Goal: "设计分布式事务架构并保证兼容性迁移"})
	if b.MaxTurns < 16 {
		t.Errorf("复杂任务应为深预算: %+v", b)
	}
}

func TestAdaptiveBudgetFailureEscalation(t *testing.T) {
	b := AdaptiveBudget(TaskSignals{Goal: "简单任务", FailureCount: 3})
	if b.MaxTurns < 16 {
		t.Errorf("多次失败应升级深预算: %+v", b)
	}
}

// ===== PlanCheckpoint =====

func TestPlanCheckpointSaveResume(t *testing.T) {
	store := NewInMemoryPlanCheckpointStore()
	ctx := context.Background()

	plan := planningPlan()
	// 模拟执行到第 3 步中断：1、2 完成，3 running（中断），4、5 pending
	plan.SubTasks[0].Status = "completed"
	plan.SubTasks[0].Result = "r1"
	plan.SubTasks[1].Status = "completed"
	plan.SubTasks[1].Result = "r2"
	plan.SubTasks[2].Status = "running"

	if err := SavePlanCheckpoint(ctx, store, "sess-1", plan); err != nil {
		t.Fatalf("保存检查点失败: %v", err)
	}

	resumed, next, err := ResumePlan(ctx, store, "sess-1")
	if err != nil {
		t.Fatalf("恢复失败: %v", err)
	}
	if resumed.Goal != plan.Goal {
		t.Error("恢复的目标不一致")
	}
	// 断点续跑：下一批只含依赖就绪的未完成任务（3 的 running 已转 pending）
	if len(next) != 1 || next[0].ID != "3" {
		t.Errorf("断点子任务应为 #3，得到 %+v", next)
	}
	if resumed.SubTasks[0].Result != "r1" {
		t.Error("已完成子任务的结果应保留")
	}
}

func TestPlanCheckpointRespectDependencies(t *testing.T) {
	store := NewInMemoryPlanCheckpointStore()
	ctx := context.Background()
	plan := planningPlan() // 5 依赖 4
	if err := SavePlanCheckpoint(ctx, store, "s2", plan); err != nil {
		t.Fatal(err)
	}
	_, next, err := ResumePlan(ctx, store, "s2")
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, n := range next {
		ids[n.ID] = true
	}
	if ids["5"] {
		t.Error("依赖未完成时 #5 不应进入可执行批次")
	}
	if !ids["1"] && !ids["4"] {
		t.Errorf("无依赖任务应可执行: %v", ids)
	}
}

func TestPlanCheckpointMissing(t *testing.T) {
	_, _, err := ResumePlan(context.Background(), NewInMemoryPlanCheckpointStore(), "none")
	if err == nil {
		t.Fatal("无检查点应报错")
	}
}

// planningPlan 构造 5 步链式依赖计划（i 依赖 i-1）
func planningPlan() *planning.Plan {
	p := &planning.Plan{Goal: "链式任务"}
	for i := 1; i <= 5; i++ {
		st := planning.SubTask{ID: fmt.Sprintf("%d", i), Description: fmt.Sprintf("步骤%d", i), Status: planning.TaskPending}
		if i > 1 {
			st.DependsOn = []string{fmt.Sprintf("%d", i-1)}
		}
		p.SubTasks = append(p.SubTasks, st)
	}
	return p
}

// ===== A/B harness =====

func TestABCompareReport(t *testing.T) {
	react := &ReActStrategy{}
	plan := &PlanExecuteReflectStrategy{}

	tasks := []ABTask{
		{Goal: "任务甲"},
		{Goal: "任务乙"},
	}
	// 引擎工厂：通用三响应脚本——首响应兼作 ReAct 终答与 Plan 的计划输入，
	// 后两响应用于 Plan 的执行与反思（两策略均可完整跑通）
	factory := func(i int) Engine {
		return &fakeEngine{responses: []string{
			`{"final":"完成"}`,
			`子任务结果`,
			`{"passed": true}`,
		}}
	}

	rep, err := ABCompare(context.Background(), react, plan, tasks, factory)
	if err != nil {
		t.Fatalf("A/B 失败: %v", err)
	}
	if rep.Tasks != 2 || rep.A.Strategy != NameReAct || rep.B.Strategy != NamePlanReflect {
		t.Errorf("报告元数据错误: %+v", rep)
	}
	if rep.A.SuccessRate != 1.0 || rep.B.SuccessRate != 1.0 {
		t.Errorf("双策略成功率应为 1.0: A=%.2f B=%.2f", rep.A.SuccessRate, rep.B.SuccessRate)
	}
	if rep.B.AvgTurns <= rep.A.AvgTurns {
		t.Errorf("Plan-Reflect 平均轮数应高于 ReAct 直出: %.1f vs %.1f", rep.B.AvgTurns, rep.A.AvgTurns)
	}
}

func TestABCompareValidation(t *testing.T) {
	if _, err := ABCompare(context.Background(), &ReActStrategy{}, &ReActStrategy{}, nil, func(int) Engine { return &fakeEngine{} }); err == nil {
		t.Error("空任务集应报错")
	}
	if _, err := ABCompare(context.Background(), &ReActStrategy{}, &ReActStrategy{}, []ABTask{{Goal: "x"}}, nil); err == nil {
		t.Error("nil 工厂应报错")
	}
}
