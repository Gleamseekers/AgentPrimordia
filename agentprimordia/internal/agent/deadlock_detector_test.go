// deadlock_detector_test.go — DeadlockDetector 接入 ReAct 循环的集成测试
// 验证：当 EnhancedPlanner 配置了 DeadlockDetector 且连续失败超过阈值时，
// 死路被检测并触发自动恢复（fire-and-forget）。
package agent

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"agentprimordia/internal/agent/planning"
	"agentprimordia/internal/llm"
)

// ===== mock 辅助 =====

// deadlockMockProvider 模拟 LLM Provider，跟踪 Complete 调用并返回预设 JSON 响应。
// 用于测试 Recovery.Recover 的 LLM 调用路径。
type deadlockMockProvider struct {
	mu          sync.Mutex
	content     string
	callCount   int32
	recoveryCh  chan struct{} // 首次 Recover 调用时关闭，用于测试同步
}

func (p *deadlockMockProvider) Complete(_ context.Context, _ *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	atomic.AddInt32(&p.callCount, 1)
	p.mu.Lock()
	ch := p.recoveryCh
	p.mu.Unlock()
	if ch != nil {
		select {
		case <-ch:
		default:
			close(ch)
		}
	}
	return &llm.CompletionResponse{
		Content: p.content,
		Usage:   llm.Usage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30},
	}, nil
}

func (p *deadlockMockProvider) Stream(_ context.Context, _ *llm.CompletionRequest) (<-chan llm.Chunk, error) {
	return nil, errors.New("not implemented")
}

func (p *deadlockMockProvider) CallTools(_ context.Context, _ *llm.ToolCallRequest) (*llm.ToolCallResponse, error) {
	return nil, errors.New("not implemented")
}

func (p *deadlockMockProvider) Info() llm.ModelInfo {
	return llm.ModelInfo{Name: "deadlock-mock", Provider: "test"}
}

// ===== 测试用例 =====

// TestDeadlockDetector_ConsecutiveFailures 验证：
// - EnhancedPlanner 配置 DeadlockDetector（阈值=1）
// - 子任务失败后，死路被检测（连续失败达到阈值）
// - tryDeadlockRecovery 被触发（Recovery.Recover 被调用）
//
// 说明：executePlanWithState 中每个子任务通过 runPlanSubtask 执行（内部带重试），
// 但 RecordFailure 在子任务最终失败时调用一次。使用阈值=1 确保首次子任务失败即触发死路检测。
func TestDeadlockDetector_ConsecutiveFailures(t *testing.T) {
	// 恢复 JSON：有效的 SubTask 数组
	recoveryJSON := `[{"id":"recovery-1","description":"替代方案"}]`
	mockProvider := &deadlockMockProvider{
		content:    recoveryJSON,
		recoveryCh: make(chan struct{}),
	}

	// 创建 EnhancedPlanner，DeadlockDetector 阈值=1（首次失败即触发）
	detector := planning.NewDeadlockDetector(1)
	ep := &planning.EnhancedPlanner{
		Recovery: planning.NewLLMRecoveryStrategy(mockProvider, detector),
		Deadlock: detector,
	}

	// 创建 Agent 并注入 EnhancedPlanner（通过 capCache）
	// 注意：PlanSubtaskRetries=0 会被 newReActAgent 默认为 1（runSubtaskWithRetry 做 2 次尝试），
	// 但 RecordFailure 在 executePlanWithState 中仅调用一次（runPlanSubtask 返回后），
	// 因此阈值=1 确保首次子任务失败即触发死路检测。
	a := newReActAgent(ReActConfig{
		Name: "deadlock-test",
		MaxTurns: 10,
	})
	a.capCache = &capabilityCache{planner: ep}

	// 子任务执行器：始终失败
	a.subtaskExecutor = func(_ context.Context, _ planning.SubTask, _ []Message, _ loopConfig) (*Response, error) {
		return nil, errors.New("permanent failure")
	}

	// 构建测试计划：两个子任务，第二个依赖第一个
	plan := &planning.Plan{
		Goal: "测试死路检测",
		SubTasks: []planning.SubTask{
			{ID: "s1", Description: "必失败步骤"},
			{ID: "s2", Description: "后续步骤", DependsOn: []string{"s1"}},
		},
	}

	// 执行计划（预期失败，因为子任务始终失败）
	_, err := a.executePlan(context.Background(), nil, plan, loopConfig{}, time.Now(), 0, 0, 0)
	if err == nil {
		t.Fatal("计划应失败")
	}

	// 等待 tryDeadlockRecovery 的 goroutine 触发 Recovery.Recover
	select {
	case <-mockProvider.recoveryCh:
		// Recovery.Recover 被调用
	case <-time.After(3 * time.Second):
		t.Fatal("超时：Recovery.Recover 未被调用，死路检测可能未触发")
	}

	// 验证 Recovery.Recover 至少被调用一次
	calls := atomic.LoadInt32(&mockProvider.callCount)
	if calls < 1 {
		t.Errorf("Recovery.Recover 应至少被调用 1 次，实际 %d 次", calls)
	}

	// 等待 goroutine 完成记录恢复动作
	time.Sleep(50 * time.Millisecond)

	// 验证 stats 中记录了 deadlock_recovery 恢复动作
	stats := a.Stats()
	found := false
	for _, rec := range stats.PlanRecoveries {
		if rec.Method == "deadlock_recovery" {
			found = true
			if !rec.Success {
				t.Error("deadlock_recovery 应标记为成功")
			}
		}
	}
	if !found {
		t.Error("应记录 deadlock_recovery 自愈动作")
	}
}

// TestDeadlockDetector_OptIn 验证：
// 当 planner 不是 *planning.EnhancedPlanner 时，死路检测不触发（opt-in 语义）。
func TestDeadlockDetector_OptIn(t *testing.T) {
	// 使用普通 mock planner（非 EnhancedPlanner）
	planner := &mockSelfHealPlanner{
		plans: []*planning.Plan{
			{Goal: "test", SubTasks: []planning.SubTask{
				{ID: "s1", Description: "step1"},
				{ID: "s2", Description: "step2", DependsOn: []string{"s1"}},
			}},
		},
	}

	a := newReActAgent(ReActConfig{
		Name:             "no-deadlock-test",
		MaxTurns:         10,
		PlanRecoveryMode: "off", // 关闭自愈以避免 replan 干扰
	})
	a.self = &selfHealMock{planner: planner}

	// 子任务执行器：始终失败
	a.subtaskExecutor = func(_ context.Context, _ planning.SubTask, _ []Message, _ loopConfig) (*Response, error) {
		return nil, errors.New("fail")
	}

	plan := &planning.Plan{
		Goal: "test",
		SubTasks: []planning.SubTask{
			{ID: "s1", Description: "step1"},
			{ID: "s2", Description: "step2", DependsOn: []string{"s1"}},
		},
	}

	// 执行计划——应失败但不触发 deadlock recovery
	_, err := a.executePlan(context.Background(), nil, plan, loopConfig{}, time.Now(), 0, 0, 0)
	if err == nil {
		t.Fatal("计划应失败")
	}

	// 验证：没有 deadlock_recovery 恢复动作
	stats := a.Stats()
	for _, rec := range stats.PlanRecoveries {
		if rec.Method == "deadlock_recovery" {
			t.Error("非 EnhancedPlanner 不应触发 deadlock_recovery")
		}
	}
}

// TestDeadlockDetector_SuccessResets 验证：
// 子任务成功后，连续失败计数器被重置，不会误触发死路检测。
func TestDeadlockDetector_SuccessResets(t *testing.T) {
	detector := planning.NewDeadlockDetector(3) // 阈值=3

	// 第 1 次失败：计数=1，未达阈值
	if detector.RecordFailure("s1") {
		t.Error("第 1 次失败不应触发死路")
	}
	// 第 2 次失败：计数=2，未达阈值
	if detector.RecordFailure("s1") {
		t.Error("第 2 次失败不应触发死路")
	}
	// 成功：重置计数
	detector.RecordSuccess("s1")
	// 再次失败：计数=1（重新开始），未达阈值
	if detector.RecordFailure("s1") {
		t.Error("重置后第 1 次失败不应触发死路")
	}
}

// TestGetEnhancedPlannerOrNil 验证类型断言的正确性
func TestGetEnhancedPlannerOrNil(t *testing.T) {
	// 情况 1：无 planner
	a := newReActAgent(ReActConfig{Name: "no-planner"})
	if ep := a.getEnhancedPlannerOrNil(); ep != nil {
		t.Error("无 planner 时应返回 nil")
	}

	// 情况 2：普通 planner（非 EnhancedPlanner）
	a2 := newReActAgent(ReActConfig{Name: "plain-planner"})
	a2.capCache = &capabilityCache{planner: &mockSelfHealPlanner{}}
	if ep := a2.getEnhancedPlannerOrNil(); ep != nil {
		t.Error("普通 planner 应返回 nil")
	}

	// 情况 3：EnhancedPlanner
	a3 := newReActAgent(ReActConfig{Name: "enhanced-planner"})
	ep := &planning.EnhancedPlanner{
		Deadlock: planning.NewDeadlockDetector(3),
	}
	a3.capCache = &capabilityCache{planner: ep}
	result := a3.getEnhancedPlannerOrNil()
	if result == nil {
		t.Fatal("EnhancedPlanner 应返回非 nil")
	}
	if result.Deadlock == nil {
		t.Error("EnhancedPlanner.Deadlock 应非 nil")
	}
}
