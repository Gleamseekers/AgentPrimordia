package hooks

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/core"
)

func TestHookManager_RegisterAndFire(t *testing.T) {
	t.Parallel()
	m := NewHookManager()
	var called int32
	m.Register(HookBeforeRun, func(_ context.Context, _ *HookContext) error {
		atomic.AddInt32(&called, 1)
		return nil
	})

	err := m.Fire(context.Background(), &HookContext{Point: HookBeforeRun})
	if err != nil {
		t.Fatalf("Fire 失败: %v", err)
	}
	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("钩子应被调用 1 次，实际 %d", called)
	}
}

func TestHookManager_PriorityOrdering(t *testing.T) {
	t.Parallel()
	m := NewHookManager()
	order := make([]int, 0)

	m.RegisterWithPriority(HookAfterTurn, func(_ context.Context, _ *HookContext) error {
		order = append(order, 3)
		return nil
	}, 1)

	m.RegisterWithPriority(HookAfterTurn, func(_ context.Context, _ *HookContext) error {
		order = append(order, 1)
		return nil
	}, -1)

	m.RegisterWithPriority(HookAfterTurn, func(_ context.Context, _ *HookContext) error {
		order = append(order, 2)
		return nil
	}, 0)

	_ = m.Fire(context.Background(), &HookContext{Point: HookAfterTurn})

	if len(order) != 3 || order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Errorf("优先级顺序错误，得到 %v", order)
	}
}

func TestHookManager_ConditionalExecution(t *testing.T) {
	t.Parallel()
	m := NewHookManager()
	var turn3Called bool
	var alwaysCalled bool

	m.RegisterConditional(HookAfterTurn, func(_ context.Context, _ *HookContext) error {
		turn3Called = true
		return nil
	}, 10, OnTurn(3), "turn-3-hook")

	m.RegisterConditional(HookAfterTurn, func(_ context.Context, _ *HookContext) error {
		alwaysCalled = true
		return nil
	}, 5, Always, "always-hook")

	_ = m.Fire(context.Background(), &HookContext{Point: HookAfterTurn, Turn: 2})
	if turn3Called {
		t.Error("turn=3 条件钩子不应在 turn=2 时触发")
	}
	if !alwaysCalled {
		t.Error("Always 条件钩子应始终触发")
	}

	err := m.Fire(context.Background(), &HookContext{Point: HookAfterTurn, Turn: 3})
	if err != nil {
		t.Fatalf("Fire 失败: %v", err)
	}
	if !turn3Called {
		t.Error("turn=3 条件钩子应在 turn=3 时触发")
	}
}

func TestHookManager_OnTurnsGreaterCondition(t *testing.T) {
	t.Parallel()
	m := NewHookManager()
	var called int32
	m.RegisterConditional(HookAfterTurn, func(_ context.Context, _ *HookContext) error {
		atomic.AddInt32(&called, 1)
		return nil
	}, 0, OnTurnsGreater(5), "")

	for i := 1; i <= 7; i++ {
		_ = m.Fire(context.Background(), &HookContext{Point: HookAfterTurn, Turn: i})
	}

	if atomic.LoadInt32(&called) != 2 {
		t.Errorf("OnTurnsGreater(5) 应触发 2 次（turn 6,7），实际 %d", called)
	}
}

func TestHookManager_OnErrorCondition(t *testing.T) {
	t.Parallel()
	m := NewHookManager()
	var errorHookCalled bool

	m.RegisterConditional(HookOnError, func(_ context.Context, hctx *HookContext) error {
		errorHookCalled = true
		if hctx.Error == nil {
			t.Error("OnError 条件下应有 Error 字段")
		}
		return nil
	}, 0, OnError(), "")

	_ = m.Fire(context.Background(), &HookContext{Point: HookOnError})
	if errorHookCalled {
		t.Error("无错误时 OnError 条件钩子不应触发")
	}

	testErr := errors.New("test error")
	_ = m.Fire(context.Background(), &HookContext{Point: HookOnError, Error: testErr})
	if !errorHookCalled {
		t.Error("有错误时 OnError 条件钩子应触发")
	}
}

func TestHookManager_StateTransitionCondition(t *testing.T) {
	t.Parallel()
	m := NewHookManager()
	var transitionCaught bool

	m.RegisterConditional(HookOnStateChange, func(_ context.Context, hctx *HookContext) error {
		transitionCaught = true
		if hctx.OldState != "running" || hctx.NewState != "completed" {
			t.Errorf("状态转换不匹配: %s -> %s", hctx.OldState, hctx.NewState)
		}
		return nil
	}, 0, OnStateTransition("running", "completed"), "")

	_ = m.Fire(context.Background(), &HookContext{
		Point:    HookOnStateChange,
		OldState: "idle",
		NewState: "running",
	})
	if transitionCaught {
		t.Error("idle->running 不应匹配 running->completed 条件")
	}

	_ = m.Fire(context.Background(), &HookContext{
		Point:    HookOnStateChange,
		OldState: "running",
		NewState: "completed",
	})
	if !transitionCaught {
		t.Error("running->completed 应匹配条件")
	}
}

func TestHookManager_RemoveByID(t *testing.T) {
	t.Parallel()
	m := NewHookManager()
	var called int32

	m.RegisterConditional(HookBeforeRun, func(_ context.Context, _ *HookContext) error {
		atomic.AddInt32(&called, 1)
		return nil
	}, 0, Always, "hook-a")

	m.RegisterConditional(HookBeforeRun, func(_ context.Context, _ *HookContext) error {
		atomic.AddInt32(&called, 1)
		return nil
	}, 0, Always, "hook-b")

	_ = m.Fire(context.Background(), &HookContext{Point: HookBeforeRun})
	if atomic.LoadInt32(&called) != 2 {
		t.Errorf("移除前应有 2 个钩子，实际调用 %d", called)
	}

	m.RemoveByID("hook-a")

	atomic.StoreInt32(&called, 0)
	_ = m.Fire(context.Background(), &HookContext{Point: HookBeforeRun})
	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("移除后应有 1 个钩子，实际调用 %d", called)
	}
}

func TestHookManager_Stats(t *testing.T) {
	t.Parallel()
	m := NewHookManager()

	m.Register(HookBeforeRun, func(_ context.Context, _ *HookContext) error { return nil })
	m.Register(HookAfterTurn, func(_ context.Context, _ *HookContext) error { return errors.New("fail") })

	_ = m.Fire(context.Background(), &HookContext{Point: HookBeforeRun})
	_ = m.Fire(context.Background(), &HookContext{Point: HookAfterTurn})
	_ = m.Fire(context.Background(), &HookContext{Point: HookAfterTurn})

	stats := m.Stats().Snapshot()
	totalFired := stats["total_fired"].(int64)
	totalErrors := stats["total_errors"].(int64)

	if totalFired != 3 {
		t.Errorf("TotalFired 应为 3，实际 %d", totalFired)
	}
	if totalErrors != 2 {
		t.Errorf("TotalErrors 应为 2，实际 %d", totalErrors)
	}
}

func TestHookManager_TotalCount(t *testing.T) {
	t.Parallel()
	m := NewHookManager()

	m.Register(HookBeforeRun, func(_ context.Context, _ *HookContext) error { return nil })
	m.Register(HookAfterRun, func(_ context.Context, _ *HookContext) error { return nil })
	m.Register(HookBeforeTool, func(_ context.Context, _ *HookContext) error { return nil })

	if m.TotalCount() != 3 {
		t.Errorf("TotalCount 应为 3，实际 %d", m.TotalCount())
	}
	if m.Count(HookBeforeRun) != 1 {
		t.Errorf("Count(BeforeRun) 应为 1，实际 %d", m.Count(HookBeforeRun))
	}
}

func TestHookManager_RegisteredPoints(t *testing.T) {
	t.Parallel()
	m := NewHookManager()
	m.Register(HookOnStream, func(_ context.Context, _ *HookContext) error { return nil })
	m.Register(HookOnStateChange, func(_ context.Context, _ *HookContext) error { return nil })

	points := m.RegisteredPoints()
	if len(points) != 2 {
		t.Errorf("应有 2 个注册点，实际 %d", len(points))
	}
}

func TestHookMiddleware_Chain(t *testing.T) {
	t.Parallel()
	m := NewHookManager()
	var beforeOrder []string
	var afterOrder []string

	m.Use(&HookMiddlewareFunc{
		BeforeFn: func(_ context.Context, _ *HookContext) error {
			beforeOrder = append(beforeOrder, "mid1-before")
			return nil
		},
		AfterFn: func(_ context.Context, _ *HookContext, err error) error {
			afterOrder = append(afterOrder, "mid1-after")
			return err
		},
	})

	m.Use(&HookMiddlewareFunc{
		BeforeFn: func(_ context.Context, _ *HookContext) error {
			beforeOrder = append(beforeOrder, "mid2-before")
			return nil
		},
		AfterFn: func(_ context.Context, _ *HookContext, err error) error {
			afterOrder = append(afterOrder, "mid2-after")
			return err
		},
	})

	m.Register(HookBeforeRun, func(_ context.Context, _ *HookContext) error { return nil })

	_ = m.Fire(context.Background(), &HookContext{Point: HookBeforeRun})

	if len(beforeOrder) != 2 || beforeOrder[0] != "mid1-before" || beforeOrder[1] != "mid2-before" {
		t.Errorf("中间件 Before 顺序错误: %v", beforeOrder)
	}
	if len(afterOrder) != 2 || afterOrder[0] != "mid2-after" || afterOrder[1] != "mid1-after" {
		t.Errorf("中间件 After 顺序错误（应逆序）: %v", afterOrder)
	}
}

func TestHookMiddleware_BeforeBlocks(t *testing.T) {
	t.Parallel()
	m := NewHookManager()
	var hookCalled bool

	blockErr := errors.New("blocked by middleware")
	m.Use(&HookMiddlewareFunc{
		BeforeFn: func(_ context.Context, _ *HookContext) error { return blockErr },
		AfterFn:  func(_ context.Context, _ *HookContext, err error) error { return err },
	})

	m.Register(HookBeforeRun, func(_ context.Context, _ *HookContext) error {
		hookCalled = true
		return nil
	})

	err := m.Fire(context.Background(), &HookContext{Point: HookBeforeRun})
	if !errors.Is(err, blockErr) {
		t.Errorf("应返回中间件阻断错误，得到: %v", err)
	}
	if hookCalled {
		t.Error("中间件阻断后钩子不应执行")
	}
}

func TestErrorRecoveryMiddleware(t *testing.T) {
	t.Parallel()
	m := NewHookManager()
	var recoveredErrors []string

	m.Use(ErrorRecoveryMiddleware(func(p HookPoint, err error) {
		recoveredErrors = append(recoveredErrors, string(p)+": "+err.Error())
	}))

	hookErr := errors.New("hook failed")
	m.Register(HookBeforeRun, func(_ context.Context, _ *HookContext) error { return hookErr })
	m.Register(HookAfterRun, func(_ context.Context, _ *HookContext) error { return nil })

	err := m.Fire(context.Background(), &HookContext{Point: HookBeforeRun})
	if err != nil {
		t.Errorf("ErrorRecovery 不应传播错误，得到: %v", err)
	}
	if len(recoveredErrors) != 1 || recoveredErrors[0] != "before_run: hook failed" {
		t.Errorf("恢复记录错误: %v", recoveredErrors)
	}
}

func TestAllHookPoints(t *testing.T) {
	t.Parallel()
	points := AllHookPoints()
	if len(points) < 30 {
		t.Errorf("AllHookPoints 应返回至少 30 个点，实际 %d", len(points))
	}

	pointSet := make(map[HookPoint]bool)
	for _, p := range points {
		if pointSet[p] {
			t.Errorf("重复的 HookPoint: %s", p)
		}
		pointSet[p] = true
	}
}

func TestHookPointCategory(t *testing.T) {
	t.Parallel()
	tests := []struct {
		point    HookPoint
		expected string
	}{
		{HookBeforeRun, "lifecycle"},
		{HookAfterLLM, "llm"},
		{HookBeforeTool, "tool"},
		{HookBeforeRAG, "memory"},
		{HookOnStream, "stream"},
		{HookContextWindowUpdate, "context"},
		{HookBeforeDAGNode, "orchestration"},
		{HookOnMetricsCollect, "observability"},
		{HookOnStateChange, "execution"},
		{HookBeforeShutdown, "lifecycle"},
		{HookBeforeMemoryRead, "memory"},
		{HookOnStreamStart, "stream"},
	}

	for _, tt := range tests {
		cat := HookPointCategory(tt.point)
		if cat != tt.expected {
			t.Errorf("%s 分类应为 %s，实际 %s", tt.point, tt.expected, cat)
		}
	}
}

func TestHookConvenienceMethods(t *testing.T) {
	t.Parallel()
	m := NewHookManager()
	var streamCalled, stateCalled, memoryCalled bool

	m.Register(HookOnStream, func(_ context.Context, _ *HookContext) error {
		streamCalled = true
		return nil
	})

	m.Register(HookOnStateChange, func(_ context.Context, _ *HookContext) error {
		stateCalled = true
		return nil
	})

	m.Register(HookAfterMemoryRead, func(_ context.Context, _ *HookContext) error {
		memoryCalled = true
		return nil
	})

	m.OnStream(context.Background(), &core.StreamEvent{Type: core.StreamEventToken})
	if !streamCalled {
		t.Error("OnStream 应触发 on_stream 钩子")
	}

	m.OnStateChange(context.Background(), "agent-1", "idle", "running", "start")
	if !stateCalled {
		t.Error("OnStateChange 应触发 on_state_change 钩子")
	}

	m.OnMemoryRead(context.Background(), "test query", "result data")
	if !memoryCalled {
		t.Error("OnMemoryRead 应触发 after_memory_read 钩子")
	}
}

func TestHookContext_Fields(t *testing.T) {
	t.Parallel()
	hctx := &HookContext{
		AgentID:            "agent-1",
		SessionID:          "sess-1",
		Point:              HookOnStateChange,
		Turn:               5,
		OldState:           "running",
		NewState:           "completed",
		Reason:             "task done",
		MemoryQuery:        "find X",
		ContextWindowUsage: 0.85,
		ContextWindowLimit: 4096,
		Duration:           time.Second,
		StreamChunk:        &core.StreamEvent{Type: core.StreamEventToken},
	}

	if hctx.AgentID != "agent-1" {
		t.Error("AgentID 不正确")
	}
	if hctx.OldState != "running" {
		t.Error("OldState 不正确")
	}
	if hctx.ContextWindowUsage != 0.85 {
		t.Error("ContextWindowUsage 不正确")
	}
	if hctx.StreamChunk == nil {
		t.Error("StreamChunk 不应为 nil")
	}
}

func TestHookManager_Clear(t *testing.T) {
	t.Parallel()
	m := NewHookManager()
	m.Register(HookBeforeRun, func(_ context.Context, _ *HookContext) error { return nil })
	m.Register(HookAfterRun, func(_ context.Context, _ *HookContext) error { return nil })

	if m.TotalCount() != 2 {
		t.Errorf("清除前应有 2 个钩子")
	}

	m.Clear()
	if m.TotalCount() != 0 {
		t.Errorf("清除后应为 0 个钩子，实际 %d", m.TotalCount())
	}
}

func TestHookManager_FireEmptyPoint(t *testing.T) {
	t.Parallel()
	m := NewHookManager()
	err := m.Fire(context.Background(), &HookContext{Point: "nonexistent"})
	if err != nil {
		t.Errorf("空钩子点的 Fire 不应返回错误: %v", err)
	}
}

func TestHookStats_Snapshot(t *testing.T) {
	t.Parallel()
	s := NewHookStats()
	s.Record(HookBeforeRun, nil)
	s.Record(HookAfterTurn, errors.New("err"))
	s.Record(HookAfterTurn, errors.New("err"))

	snap := s.Snapshot()
	if snap["total_fired"].(int64) != 3 {
		t.Errorf("total_fired 应为 3")
	}
	if snap["total_errors"].(int64) != 2 {
		t.Errorf("total_errors 应为 2")
	}
}

// perf-v5 Task 11 验证：HookStats.Record 并发安全（atomic.Int64 数组）
func TestHookStats_ConcurrentRecord(t *testing.T) {
	t.Parallel()
	s := NewHookStats()
	const goroutines = 50
	const perGoroutine = 100

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				s.Record(HookBeforeLLM, nil)
			}
		}()
	}
	wg.Wait()

	snap := s.Snapshot()
	totalFired := snap["total_fired"].(int64)
	expected := int64(goroutines * perGoroutine)
	if totalFired != expected {
		t.Errorf("并发 Record 后 total_fired 失真: got=%d, want=%d", totalFired, expected)
	}
	if snap["total_errors"].(int64) != 0 {
		t.Errorf("total_errors 应为 0，got=%d", snap["total_errors"].(int64))
	}
}

func TestHookPhase_ValidationStopsExecution(t *testing.T) {
	t.Parallel()
	mgr := NewHookManager()
	var executionOrder []string

	mgr.RegisterInPhase(PhaseExecution, HookBeforeLLM, func(_ context.Context, _ *HookContext) error {
		executionOrder = append(executionOrder, "execution")
		return nil
	})
	mgr.RegisterInPhase(PhaseValidation, HookBeforeLLM, func(_ context.Context, _ *HookContext) error {
		executionOrder = append(executionOrder, "validation")
		return errors.New("blocked")
	})

	err := mgr.Fire(context.Background(), &HookContext{Point: HookBeforeLLM})
	if err == nil {
		t.Error("should return error from validation phase")
	}
	if len(executionOrder) != 1 || executionOrder[0] != "validation" {
		t.Errorf("execution should stop at validation, got %v", executionOrder)
	}
}

func TestHookPhase_ExecutionOrder(t *testing.T) {
	t.Parallel()
	mgr := NewHookManager()
	var order []string

	mgr.RegisterInPhase(PhaseValidation, HookBeforeLLM, func(_ context.Context, _ *HookContext) error {
		order = append(order, "validation")
		return nil
	})
	mgr.RegisterInPhase(PhasePreProcessing, HookBeforeLLM, func(_ context.Context, _ *HookContext) error {
		order = append(order, "pre")
		return nil
	})
	mgr.RegisterInPhase(PhaseExecution, HookBeforeLLM, func(_ context.Context, _ *HookContext) error {
		order = append(order, "exec")
		return nil
	})
	mgr.RegisterInPhase(PhasePostProcessing, HookBeforeLLM, func(_ context.Context, _ *HookContext) error {
		order = append(order, "post")
		return nil
	})

	err := mgr.Fire(context.Background(), &HookContext{Point: HookBeforeLLM})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []string{"validation", "pre", "exec", "post"}
	if len(order) != len(expected) {
		t.Fatalf("order = %v, want %v", order, expected)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("order[%d] = %q, want %q", i, order[i], v)
		}
	}
}

func TestHookPhase_RegisterDefaultIsExecution(t *testing.T) {
	t.Parallel()
	mgr := NewHookManager()
	var called bool
	mgr.Register(HookBeforeLLM, func(_ context.Context, _ *HookContext) error {
		called = true
		return nil
	})
	err := mgr.Fire(context.Background(), &HookContext{Point: HookBeforeLLM})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("Register default should use PhaseExecution")
	}
}

func TestHookPhase_MultipleInSamePhase(t *testing.T) {
	t.Parallel()
	mgr := NewHookManager()
	var order []string

	mgr.RegisterInPhase(PhaseValidation, HookBeforeLLM, func(_ context.Context, _ *HookContext) error {
		order = append(order, "v1")
		return nil
	})
	mgr.RegisterInPhase(PhaseValidation, HookBeforeLLM, func(_ context.Context, _ *HookContext) error {
		order = append(order, "v2")
		return nil
	})

	err := mgr.Fire(context.Background(), &HookContext{Point: HookBeforeLLM})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(order) != 2 {
		t.Errorf("expected 2 calls, got %d", len(order))
	}
}

func TestLoggingMiddleware(t *testing.T) {
	t.Parallel()
	mw := LoggingMiddleware()
	m := NewHookManager()
	m.Use(mw)

	m.Register(HookBeforeRun, func(_ context.Context, _ *HookContext) error { return nil })
	err := m.Fire(context.Background(), &HookContext{Point: HookBeforeRun})
	if err != nil {
		t.Errorf("LoggingMiddleware should not block: %v", err)
	}
}

func TestMetricsCollectionMiddleware(t *testing.T) {
	t.Parallel()
	stats := NewHookStats()
	mw := MetricsCollectionMiddleware(stats)
	m := NewHookManager()
	m.Use(mw)

	m.Register(HookBeforeRun, func(_ context.Context, _ *HookContext) error { return nil })
	m.Register(HookAfterRun, func(_ context.Context, _ *HookContext) error {
		return errors.New("test error")
	})

	_ = m.Fire(context.Background(), &HookContext{Point: HookBeforeRun})
	_ = m.Fire(context.Background(), &HookContext{Point: HookAfterRun})

	snap := stats.Snapshot()
	if snap["total_fired"].(int64) != 2 {
		t.Errorf("expected 2 fired, got %v", snap["total_fired"])
	}
	if snap["total_errors"].(int64) != 1 {
		t.Errorf("expected 1 error, got %v", snap["total_errors"])
	}
}

func TestTimeoutMiddleware(t *testing.T) {
	t.Parallel()
	mw := TimeoutMiddleware(5 * time.Second)
	m := NewHookManager()
	m.Use(mw)

	m.Register(HookBeforeRun, func(_ context.Context, _ *HookContext) error { return nil })
	err := m.Fire(context.Background(), &HookContext{Point: HookBeforeRun})
	if err != nil {
		t.Errorf("TimeoutMiddleware should not block normal execution: %v", err)
	}
}

// ===== Pool 测试 =====

func TestAcquireReleaseHookContext(t *testing.T) {
	hctx := AcquireHookContext()
	hctx.AgentID = "test-agent"
	hctx.Turn = 5

	ReleaseHookContext(hctx)

	// 再次获取应已重置
	hctx2 := AcquireHookContext()
	if hctx2.AgentID != "" {
		t.Errorf("Reset 后 AgentID 应为空，实际 %q", hctx2.AgentID)
	}
	if hctx2.Turn != 0 {
		t.Errorf("Reset 后 Turn 应为 0，实际 %d", hctx2.Turn)
	}
	ReleaseHookContext(hctx2)
}

func TestReleaseHookContext_Nil(t *testing.T) {
	// 不应 panic
	ReleaseHookContext(nil)
}

func TestHookContext_Reset_WithMetadata(t *testing.T) {
	hctx := &HookContext{
		Metadata: map[string]any{"key1": "val1", "key2": "val2"},
	}
	hctx.Reset()

	if len(hctx.Metadata) != 0 {
		t.Errorf("Reset 后 Metadata 应为空，实际 %d 项", len(hctx.Metadata))
	}
}

func TestHookManager_Remove(t *testing.T) {
	t.Parallel()
	m := NewHookManager()
	var called int32

	m.Register(HookBeforeRun, func(_ context.Context, _ *HookContext) error {
		atomic.AddInt32(&called, 1)
		return nil
	})

	_ = m.Fire(context.Background(), &HookContext{Point: HookBeforeRun})
	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("Remove 前应调用 1 次，实际 %d", called)
	}

	m.Remove(HookBeforeRun)

	atomic.StoreInt32(&called, 0)
	_ = m.Fire(context.Background(), &HookContext{Point: HookBeforeRun})
	if atomic.LoadInt32(&called) != 0 {
		t.Errorf("Remove 后不应调用，实际 %d", called)
	}
}

func TestOnMetadataKeyCondition(t *testing.T) {
	t.Parallel()
	m := NewHookManager()
	var called bool

	m.RegisterConditional(HookBeforeRun, func(_ context.Context, _ *HookContext) error {
		called = true
		return nil
	}, 0, OnMetadataKey("trigger"), "")

	// 无 metadata 时不触发
	_ = m.Fire(context.Background(), &HookContext{Point: HookBeforeRun})
	if called {
		t.Error("无 metadata 时不应触发")
	}

	// 有 metadata 但无目标 key 时不触发
	_ = m.Fire(context.Background(), &HookContext{
		Point:    HookBeforeRun,
		Metadata: map[string]any{"other": "value"},
	})
	if called {
		t.Error("无目标 key 时不应触发")
	}

	// 有目标 key 时触发
	_ = m.Fire(context.Background(), &HookContext{
		Point:    HookBeforeRun,
		Metadata: map[string]any{"trigger": "go"},
	})
	if !called {
		t.Error("有目标 key 时应触发")
	}
}

func TestHookManager_ConvenienceOnComplete(t *testing.T) {
	t.Parallel()
	m := NewHookManager()
	var called bool

	m.Register(HookOnComplete, func(_ context.Context, _ *HookContext) error {
		called = true
		return nil
	})

	m.OnComplete(context.Background(), &core.Response{Content: "done"})
	if !called {
		t.Error("OnComplete 应触发 on_complete 钩子")
	}
}

func TestHookManager_ConvenienceOnToolUse(t *testing.T) {
	t.Parallel()
	m := NewHookManager()
	var called bool

	m.Register(HookBeforeTool, func(_ context.Context, _ *HookContext) error {
		called = true
		return nil
	})

	m.OnToolUse(context.Background(), &core.ToolCall{ID: "tc1", Name: "search"})
	if !called {
		t.Error("OnToolUse 应触发 before_tool 钩子")
	}
}

func TestHookManager_ConvenienceOnShutdown(t *testing.T) {
	t.Parallel()
	m := NewHookManager()
	var called bool

	m.Register(HookBeforeShutdown, func(_ context.Context, _ *HookContext) error {
		called = true
		return nil
	})

	m.OnShutdown(context.Background())
	if !called {
		t.Error("OnShutdown 应触发 before_shutdown 钩子")
	}
}
