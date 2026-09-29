package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent/planning"
	"github.com/Gleamseekers/AgentPrimordia/internal/persist"
)

// ===== mock 辅助 =====

// mockSelfHealPlanner 依次返回预设计划（每次 GeneratePlan 取下一个）。
type mockSelfHealPlanner struct {
	plans []*planning.Plan
	calls int
}

func (m *mockSelfHealPlanner) Decompose(_ context.Context, _ string) ([]planning.SubTask, error) {
	return nil, errors.New("not used")
}

func (m *mockSelfHealPlanner) GeneratePlan(_ context.Context, _ string) (*planning.Plan, error) {
	idx := m.calls
	m.calls++
	if idx < len(m.plans) {
		return m.plans[idx], nil
	}
	return &planning.Plan{SubTasks: []planning.SubTask{{ID: "fallback", Description: "fallback"}}}, nil
}

// selfHealMock 实现 PlanningCapable + CheckpointCapable + Agent。
type selfHealMock struct {
	planner planning.Planner
	store   persist.CheckpointStore
}

func (m *selfHealMock) GetPlanner() planning.Planner                { return m.planner }
func (m *selfHealMock) GetCheckpointStore() persist.CheckpointStore { return m.store }
func (m *selfHealMock) Run(_ context.Context, _ Message) (*Response, error) {
	return nil, errors.New("not used")
}
func (m *selfHealMock) StreamRun(_ context.Context, _ Message) (<-chan StreamEvent, error) {
	return nil, errors.New("not used")
}
func (m *selfHealMock) Stop()             {}
func (m *selfHealMock) Stats() AgentStats { return AgentStats{} }
func (m *selfHealMock) Name() string      { return "self-heal-mock" }

// ===== TestPlanSubtaskRetry_FeedbackHint =====
// 验证子任务重试会携带失败反馈（换方案提示）。

func TestPlanSubtaskRetry_FeedbackHint(t *testing.T) {
	a := newReActAgent(ReActConfig{Name: "feedback-hint", MaxTurns: 5, PlanSubtaskRetries: 1})
	a.subtaskExecutor = func(_ context.Context, task planning.SubTask, _ []Message, _ loopConfig) (*Response, error) {
		return nil, errors.New("boom")
	}

	// 直接调用 executeSubTask 的底层反馈注入不可行（需要 LLM），
	// 改为验证 runSubtaskWithRetry 在重试时把失败原因传给 run(feedback)。
	var feedbacks []string
	run := func(feedback string) (*Response, error) {
		feedbacks = append(feedbacks, feedback)
		if len(feedbacks) == 1 {
			return nil, errors.New("first attempt failed")
		}
		return &Response{Content: "recovered"}, nil
	}
	resp, err := runSubtaskWithRetry(context.Background(), run, 1)
	if err != nil {
		t.Fatalf("重试应成功: %v", err)
	}
	if resp.Content != "recovered" {
		t.Errorf("Content = %q", resp.Content)
	}
	// 第二次调用的 feedback 应包含第一次失败原因（换方案提示）
	if len(feedbacks) != 2 {
		t.Fatalf("调用次数 = %d, want 2", len(feedbacks))
	}
	if !strings.Contains(feedbacks[1], "first attempt failed") {
		t.Errorf("重试 feedback 应携带失败原因, got %q", feedbacks[1])
	}
	if !strings.Contains(feedbacks[1], "更换") {
		t.Errorf("feedback 应提示换方案, got %q", feedbacks[1])
	}
}

// ===== TestSelfHealing_Replan =====
// plan 失败 → 自动 replan 换路径 → 成功。

func TestSelfHealing_Replan(t *testing.T) {
	store, err := persist.InMemoryCheckpointStore()
	if err != nil {
		t.Fatalf("checkpoint store: %v", err)
	}
	defer store.Close()

	// planA 含标记 BAD 的子任务（必然失败）；planB 全部可成功
	planA := &planning.Plan{Goal: "goal", SubTasks: []planning.SubTask{
		{ID: "a1", Description: "BAD step", DependsOn: []string{}},
		{ID: "a2", Description: "a2", DependsOn: []string{"a1"}},
	}}
	planB := &planning.Plan{Goal: "goal", SubTasks: []planning.SubTask{
		{ID: "b1", Description: "good step 1", DependsOn: []string{}},
		{ID: "b2", Description: "good step 2", DependsOn: []string{"b1"}},
	}}

	planner := &mockSelfHealPlanner{plans: []*planning.Plan{planA, planB}}

	a := newReActAgent(ReActConfig{Name: "heal-replan", MaxTurns: 10, Model: &outputGuardMockProvider{content: "fallback"}})
	a.self = &selfHealMock{planner: planner, store: store}
	// 子任务执行器：含 "BAD" 的任务必失败（重试也失败），其余成功
	a.subtaskExecutor = func(_ context.Context, task planning.SubTask, _ []Message, _ loopConfig) (*Response, error) {
		if strings.Contains(task.Description, "BAD") {
			return nil, errors.New("permanent failure")
		}
		return &Response{Content: "done:" + task.ID}, nil
	}

	resp, err := a.Run(context.Background(), Message{Role: RoleUser, Content: "复杂任务"})
	if err != nil {
		t.Fatalf("Run 应通过自愈完成: %v", err)
	}
	if resp.Content != "done:b2" {
		t.Errorf("最终输出 = %q, want done:b2（来自 replan 的计划）", resp.Content)
	}

	stats := a.Stats()
	if len(stats.PlanRecoveries) == 0 {
		t.Fatal("应记录自愈动作")
	}
	if stats.PlanRecoveries[0].Method != "replan" {
		t.Errorf("自愈方式 = %q, want replan", stats.PlanRecoveries[0].Method)
	}
	if !stats.PlanRecoveries[0].Success {
		t.Error("replan 自愈应成功")
	}
}

// ===== TestSelfHealing_Degrade =====
// replan 仍失败 → 降级到普通 runLoop → 请求不中断。

func TestSelfHealing_Degrade(t *testing.T) {
	store, err := persist.InMemoryCheckpointStore()
	if err != nil {
		t.Fatalf("checkpoint store: %v", err)
	}
	defer store.Close()

	// 两次 GeneratePlan 都返回含 BAD 的计划（replan 也失败）
	failingPlan := &planning.Plan{Goal: "goal", SubTasks: []planning.SubTask{
		{ID: "x1", Description: "BAD step", DependsOn: []string{}},
		{ID: "x2", Description: "x2", DependsOn: []string{"x1"}},
	}}
	planner := &mockSelfHealPlanner{plans: []*planning.Plan{failingPlan, failingPlan}}

	a := newReActAgent(ReActConfig{Name: "heal-degrade", MaxTurns: 10, Model: &outputGuardMockProvider{content: "降级成功"}})
	a.self = &selfHealMock{planner: planner, store: store}
	a.subtaskExecutor = func(_ context.Context, task planning.SubTask, _ []Message, _ loopConfig) (*Response, error) {
		if strings.Contains(task.Description, "BAD") {
			return nil, errors.New("permanent failure")
		}
		return &Response{Content: "done"}, nil
	}

	resp, err := a.Run(context.Background(), Message{Role: RoleUser, Content: "复杂任务"})
	if err != nil {
		t.Fatalf("降级后应正常完成: %v", err)
	}
	if resp.Content != "降级成功" {
		t.Errorf("降级输出 = %q, want 降级成功（来自普通 runLoop）", resp.Content)
	}

	stats := a.Stats()
	found := false
	for _, rec := range stats.PlanRecoveries {
		if rec.Method == "degrade" {
			found = true
			if !rec.Success {
				t.Error("degrade 自愈应成功")
			}
		}
	}
	if !found {
		t.Error("应记录 degrade 自愈动作")
	}
}

// ===== TestSelfHealing_Disabled =====
// PlanRecoveryMode=off 时计划失败直接返回错误，不做自愈。

func TestSelfHealing_Disabled(t *testing.T) {
	store, err := persist.InMemoryCheckpointStore()
	if err != nil {
		t.Fatalf("checkpoint store: %v", err)
	}
	defer store.Close()

	failingPlan := &planning.Plan{Goal: "goal", SubTasks: []planning.SubTask{
		{ID: "z1", Description: "BAD step", DependsOn: []string{}},
		{ID: "z2", Description: "z2", DependsOn: []string{"z1"}},
	}}
	planner := &mockSelfHealPlanner{plans: []*planning.Plan{failingPlan}}

	a := newReActAgent(ReActConfig{Name: "heal-off", MaxTurns: 10, Model: &outputGuardMockProvider{content: "x"}, PlanRecoveryMode: "off"})
	a.self = &selfHealMock{planner: planner, store: store}
	a.subtaskExecutor = func(_ context.Context, task planning.SubTask, _ []Message, _ loopConfig) (*Response, error) {
		if strings.Contains(task.Description, "BAD") {
			return nil, errors.New("permanent failure")
		}
		return &Response{Content: "done"}, nil
	}

	_, err = a.Run(context.Background(), Message{Role: RoleUser, Content: "复杂任务"})
	if err == nil {
		t.Fatal("关闭自愈后计划失败应返回错误")
	}
	if len(a.Stats().PlanRecoveries) != 0 {
		t.Error("关闭自愈不应记录恢复动作")
	}
}
