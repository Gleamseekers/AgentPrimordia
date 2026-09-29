package pool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm"
)

// testCodeError 用于测试 errors.As 的自定义错误类型
type testCodeError struct {
	code    string
	message string
}

func (e *testCodeError) Error() string { return e.code + ": " + e.message }
func (e *testCodeError) Code() string  { return e.code }

func TestPool_DispatchSingleTask(t *testing.T) {
	t.Parallel()
	mockLLM := llm.NewMockLLM(t).WithResponse("Task completed")

	pool := NewPool(PoolConfig{
		MaxConcurrency: 5,
		Timeout:        30 * time.Second,
	})
	pool.SetModel(mockLLM)
	defer pool.Close()

	tasks := []TaskConfig{
		{ID: "task-1", Title: "Test Task", Prompt: "Do something"},
	}

	results, err := pool.Dispatch(context.Background(), tasks)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	result := results[0]
	if result.Status != PoolTaskCompleted {
		t.Errorf("expected Completed, got %s", result.Status)
	}
	if result.Error != nil {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestPool_DispatchMultipleTasks(t *testing.T) {
	t.Parallel()
	mockLLM := llm.NewMockLLM(t).
		WithResponse("Result A").
		WithResponse("Result B").
		WithResponse("Result C")

	pool := NewPool(PoolConfig{
		MaxConcurrency: 3,
		Timeout:        30 * time.Second,
	})
	pool.SetModel(mockLLM)
	defer pool.Close()

	tasks := []TaskConfig{
		{ID: "task-a", Title: "Task A", Prompt: "Do A"},
		{ID: "task-b", Title: "Task B", Prompt: "Do B"},
		{ID: "task-c", Title: "Task C", Prompt: "Do C"},
	}

	results, err := pool.Dispatch(context.Background(), tasks)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	completed := 0
	for _, r := range results {
		if r.Status == PoolTaskCompleted {
			completed++
		}
	}
	if completed != 3 {
		t.Errorf("expected 3 completed, got %d", completed)
	}
}

func TestPool_ConcurrencyLimit(t *testing.T) {
	t.Parallel()
	mockLLM := llm.NewMockLLM(t).WithDelay(50 * time.Millisecond).
		WithResponse("Done").
		WithResponse("Done").
		WithResponse("Done")

	pool := NewPool(PoolConfig{
		MaxConcurrency: 1,
		Timeout:        10 * time.Second,
	})
	pool.SetModel(mockLLM)
	defer pool.Close()

	tasks := []TaskConfig{
		{ID: "slow-1", Title: "Slow 1", Prompt: "Wait"},
		{ID: "slow-2", Title: "Slow 2", Prompt: "Wait"},
		{ID: "slow-3", Title: "Slow 3", Prompt: "Wait"},
	}

	start := time.Now()
	results, _ := pool.Dispatch(context.Background(), tasks)
	elapsed := time.Since(start)

	if elapsed < 140*time.Millisecond {
		t.Errorf("expected ~150ms+ with concurrency=1, got %v", elapsed)
	}

	_ = results
}

func TestPool_CancelTask(t *testing.T) {
	t.Parallel()
	mockLLM := llm.NewMockLLM(t).WithDelay(2 * time.Second).WithResponse("Done")

	pool := NewPool(PoolConfig{
		MaxConcurrency: 5,
		Timeout:        10 * time.Second,
	})
	pool.SetModel(mockLLM)
	defer pool.Close()

	tasks := []TaskConfig{
		{ID: "cancel-me", Title: "Cancellable", Prompt: "Long task"},
	}

	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = pool.Cancel("cancel-me")
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	results, _ := pool.Dispatch(ctx, tasks)

	result := results[0]
	if result.Status != PoolTaskCancelled {
		t.Errorf("expected Cancelled, got %s", result.Status)
	}
}

func TestPool_CancelAll(t *testing.T) {
	t.Parallel()
	mockLLM := llm.NewMockLLM(t).WithDelay(2 * time.Second).
		WithResponse("Done").
		WithResponse("Done")

	pool := NewPool(PoolConfig{
		MaxConcurrency: 5,
		Timeout:        10 * time.Second,
	})
	pool.SetModel(mockLLM)
	defer pool.Close()

	tasks := []TaskConfig{
		{ID: "all-1", Title: "All 1", Prompt: "Long 1"},
		{ID: "all-2", Title: "All 2", Prompt: "Long 2"},
	}

	go func() {
		time.Sleep(100 * time.Millisecond)
		pool.CancelAll()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	results, _ := pool.Dispatch(ctx, tasks)

	cancelled := 0
	for _, r := range results {
		if r.Status == PoolTaskCancelled {
			cancelled++
		}
	}
	if cancelled != 2 {
		t.Errorf("expected 2 cancelled, got %d", cancelled)
	}
}

func TestPool_Timeout(t *testing.T) {
	t.Parallel()
	mockLLM := llm.NewMockLLM(t).WithDelay(5 * time.Second).WithResponse("Done")

	pool := NewPool(PoolConfig{
		MaxConcurrency: 1,
		Timeout:        100 * time.Millisecond,
	})
	pool.SetModel(mockLLM)
	defer pool.Close()

	tasks := []TaskConfig{
		{ID: "timeout-task", Title: "Timeout", Prompt: "Too slow"},
	}

	results, _ := pool.Dispatch(context.Background(), tasks)

	result := results[0]
	if result.Status != PoolTaskFailed {
		t.Errorf("expected Failed, got %s", result.Status)
	}
}

func TestPool_EmptyTasks(t *testing.T) {
	t.Parallel()
	pool := NewPool(PoolConfig{})
	defer pool.Close()

	results, err := pool.Dispatch(context.Background(), []TaskConfig{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestPool_Stats(t *testing.T) {
	t.Parallel()
	mockLLM := llm.NewMockLLM(t).WithResponse("Done")

	pool := NewPool(PoolConfig{
		MaxConcurrency: 3,
	})
	pool.SetModel(mockLLM)
	defer pool.Close()

	stats := pool.Stats()
	if stats.TotalTasks != 0 {
		t.Errorf("expected 0 total before dispatch")
	}

	tasks := []TaskConfig{
		{ID: "stat-1", Title: "Stat Task", Prompt: "Stat me"},
	}

	_, _ = pool.Dispatch(context.Background(), tasks)

	stats = pool.Stats()
	if stats.TotalTasks != 1 {
		t.Errorf("expected 1 total, got %d", stats.TotalTasks)
	}
	if stats.CompletedTasks != 1 {
		t.Errorf("expected 1 completed, got %d", stats.CompletedTasks)
	}
	if stats.MaxConcurrency != 3 {
		t.Errorf("expected max_concurrency 3, got %d", stats.MaxConcurrency)
	}
}

func TestPool_EventChannel(t *testing.T) {
	t.Parallel()
	mockLLM := llm.NewMockLLM(t).WithResponse("Event test")

	pool := NewPool(PoolConfig{
		MaxConcurrency: 5,
	})
	pool.SetModel(mockLLM)
	defer pool.Close()

	receivedEvents := []PoolEvent{}
	done := make(chan struct{})

	go func() {
		for event := range pool.EventChannel() {
			receivedEvents = append(receivedEvents, event)
			if len(receivedEvents) >= 2 {
				break
			}
		}
		close(done)
	}()

	tasks := []TaskConfig{
		{ID: "event-task", Title: "Event Task", Prompt: "Emit events"},
	}

	_, _ = pool.Dispatch(context.Background(), tasks)

	select {
	case <-done:
	case <-time.After(time.Second):
	}

	time.Sleep(50 * time.Millisecond)

	if len(receivedEvents) == 0 {
		t.Error("should have received at least one event")
	}
}

func TestPool_RetryPolicy(t *testing.T) {
	t.Parallel()
	mockLLM := llm.NewMockLLM(t).WithDelay(10 * time.Millisecond).
		WithError(errors.New("rate_limit_exceeded"))

	pool := NewPool(PoolConfig{
		MaxConcurrency: 5,
		Timeout:        10 * time.Second,
		RetryPolicy: RetryPolicy{
			MaxRetries:      1,
			Backoff:         20 * time.Millisecond,
			RetryableErrors: []string{"rate_limit"},
		},
	})
	pool.SetModel(mockLLM)
	defer pool.Close()

	tasks := []TaskConfig{
		{ID: "retry-task", Title: "Retry Task", Prompt: "Retry me"},
	}

	start := time.Now()
	results, err := pool.Dispatch(context.Background(), tasks)
	elapsed := time.Since(start)

	result := results[0]
	if result.Status != PoolTaskFailed {
		t.Errorf("expected Failed (all retries exhausted), got %s", result.Status)
	}

	if elapsed < 40*time.Millisecond {
		t.Errorf("expected delay due to retry backoff, got %v", elapsed)
	}

	_ = err
}

func TestPool_DefaultConfig(t *testing.T) {
	t.Parallel()
	pool := NewPool(PoolConfig{})
	defer pool.Close()

	if pool.config.MaxConcurrency != 16 {
		t.Errorf("default max_concurrency should be 16, got %d", pool.config.MaxConcurrency)
	}
	if pool.config.Timeout != 5*time.Minute {
		t.Errorf("default timeout should be 5min, got %v", pool.config.Timeout)
	}
}

func TestEventBus_SubscribeUnsubscribe(t *testing.T) {
	t.Parallel()
	bus := NewEventBus()
	defer bus.Close()

	ch := make(chan PoolEvent, 10)
	unsub := bus.Subscribe(ch)

	go func() {
		time.Sleep(10 * time.Millisecond)
		bus.Publish(PoolEvent{Type: "test_event"})
	}()

	select {
	case event := <-ch:
		if event.Type != "test_event" {
			t.Errorf("expected 'test_event', got '%s'", event.Type)
		}
	// 100ms 在 -race 并行下偶发不足（macOS CI 实测），放宽到 1s
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for event")
	}

	unsub()

	go func() {
		time.Sleep(10 * time.Millisecond)
		bus.Publish(PoolEvent{Type: "after_unsub"})
	}()

	select {
	case <-ch:
		t.Error("should not receive event after unsubscribe")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestEventBus_Watch(t *testing.T) {
	t.Parallel()
	bus := NewEventBus()
	defer bus.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go func() {
		for i := 0; i < 5; i++ {
			bus.Publish(PoolEvent{Type: "test", TaskID: "task-id"})
			time.Sleep(20 * time.Millisecond)
		}
	}()

	events, err := bus.Watch(ctx, func(e PoolEvent) bool {
		return e.Type == "task_completed" || e.Type == "test"
	})

	if err != nil && err != context.DeadlineExceeded {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(events) == 0 {
		t.Error("should have collected some events")
	}
}

func TestEventCollector_CollectUntilCondition(t *testing.T) {
	t.Parallel()
	bus := NewEventBus()
	defer bus.Close()

	collector := NewEventCollector(bus, func(events []PoolEvent) bool {
		return len(events) >= 3
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done, _ := collector.Start(ctx)

	time.Sleep(10 * time.Millisecond)

	for i := 0; i < 5; i++ {
		bus.Publish(PoolEvent{Type: "collected", TaskID: "id"})
		time.Sleep(20 * time.Millisecond)
	}

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
	}

	events := collector.Events()
	if len(events) < 3 {
		t.Errorf("expected at least 3 collected events, got %d", len(events))
	}
}

// ===== AggregatedError 错误聚合测试 =====

func TestAggregatedError_Error(t *testing.T) {
	t.Parallel()

	// 单个错误
	ae := &AggregatedError{
		TaskErrors: []TaskError{
			{TaskID: "task-1", Error: errors.New("something went wrong")},
		},
	}
	expected := "task task-1 failed: something went wrong"
	if ae.Error() != expected {
		t.Errorf("expected %q, got %q", expected, ae.Error())
	}

	// 多个错误
	ae = &AggregatedError{
		TaskErrors: []TaskError{
			{TaskID: "task-1", Error: errors.New("err1")},
			{TaskID: "task-2", Error: errors.New("err2")},
		},
	}
	errStr := ae.Error()
	if !strings.Contains(errStr, "2 tasks failed") {
		t.Errorf("expected '2 tasks failed', got %q", errStr)
	}
	if !strings.Contains(errStr, "task-1: err1") {
		t.Errorf("expected task-1 detail, got %q", errStr)
	}
	if !strings.Contains(errStr, "task-2: err2") {
		t.Errorf("expected task-2 detail, got %q", errStr)
	}
}

func TestAggregatedError_Is(t *testing.T) {
	t.Parallel()

	baseErr := errors.New("base error")
	ae := &AggregatedError{
		TaskErrors: []TaskError{
			{TaskID: "task-1", Error: baseErr},
			{TaskID: "task-2", Error: errors.New("other error")},
		},
	}

	if !errors.Is(ae, baseErr) {
		t.Error("AggregatedError.Is should find baseErr")
	}
	if errors.Is(ae, errors.New("nonexistent")) {
		t.Error("AggregatedError.Is should not find nonexistent error")
	}
}

func TestAggregatedError_Unwrap(t *testing.T) {
	t.Parallel()

	e1 := errors.New("error1")
	e2 := errors.New("error2")
	ae := &AggregatedError{
		TaskErrors: []TaskError{
			{TaskID: "task-1", Error: e1},
			{TaskID: "task-2", Error: e2},
		},
	}

	errs := ae.Unwrap()
	if len(errs) != 2 {
		t.Fatalf("expected 2 unwrapped errors, got %d", len(errs))
	}
	if errs[0] != e1 || errs[1] != e2 {
		t.Error("Unwrap returned wrong errors")
	}
}

func TestAggregatedError_Empty(t *testing.T) {
	t.Parallel()

	ae := &AggregatedError{}
	if ae.Error() != "no errors" {
		t.Errorf("expected 'no errors', got %q", ae.Error())
	}
}

func TestAggregatedError_As(t *testing.T) {
	t.Parallel()

	ce := &testCodeError{code: "ERR_001", message: "custom error"}
	ae := &AggregatedError{
		TaskErrors: []TaskError{
			{TaskID: "task-1", Error: errors.New("plain")},
			{TaskID: "task-2", Error: ce},
		},
	}

	var target *testCodeError
	if !errors.As(ae, &target) {
		t.Error("AggregatedError.As should find testCodeError")
	}
	if target.code != "ERR_001" {
		t.Errorf("expected code ERR_001, got %q", target.code)
	}
}

func TestAggregatedError_WrappedIs(t *testing.T) {
	t.Parallel()

	baseErr := errors.New("base")
	wrapped := fmt.Errorf("wrapped: %w", baseErr)
	ae := &AggregatedError{
		TaskErrors: []TaskError{
			{TaskID: "task-1", Error: wrapped},
		},
	}

	if !errors.Is(ae, baseErr) {
		t.Error("AggregatedError.Is should find wrapped baseErr")
	}
}

func TestAggregatedError_AllSameError(t *testing.T) {
	t.Parallel()

	timeoutErr := errors.New("timeout")
	ae := &AggregatedError{
		TaskErrors: []TaskError{
			{TaskID: "task-1", Error: timeoutErr},
			{TaskID: "task-2", Error: timeoutErr},
			{TaskID: "task-3", Error: timeoutErr},
		},
	}

	if !errors.Is(ae, timeoutErr) {
		t.Error("AggregatedError.Is should find timeoutErr when all tasks have same error")
	}
}

// TestTaskResult_MarshalJSON 验证 error 字段的 JSON 序列化行为。
// 修复前：error 接口直接序列化输出 {}，外部消费者拿不到错误信息；
// 修复后：序列化为 Error() 字符串，无错误时省略字段。
func TestTaskResult_MarshalJSON(t *testing.T) {
	// 有错误：序列化为字符串
	tr := TaskResult{TaskID: "t1", Status: PoolTaskFailed, Error: fmt.Errorf("boom")}
	data, err := json.Marshal(tr)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, `"error":"boom"`) {
		t.Errorf("error 应序列化为字符串，实际: %s", got)
	}
	if strings.Contains(got, "{}") {
		t.Errorf("error 不得序列化为空对象: %s", got)
	}

	// 无错误：error 字段省略
	tr2 := TaskResult{TaskID: "t2", Status: PoolTaskCompleted}
	data2, _ := json.Marshal(tr2)
	if strings.Contains(string(data2), "error") {
		t.Errorf("无错误时不应输出 error 字段: %s", data2)
	}
}
