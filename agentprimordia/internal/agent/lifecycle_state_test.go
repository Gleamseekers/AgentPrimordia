
package agent

import (
	"slices"
	"testing"
	"time"
)

func TestLifecycle_ValidTransitions(t *testing.T) {
	cases := []struct {
		from AgentStatus
		to   AgentStatus
	}{
		{StatusIdle, StatusRunning},
		{StatusRunning, StatusPaused},
		{StatusRunning, StatusCompleted},
		{StatusRunning, StatusFailed},
		{StatusRunning, StatusCancelled},
		{StatusPaused, StatusRunning},
		{StatusCompleted, StatusIdle},
		{StatusFailed, StatusIdle},
		{StatusCancelled, StatusIdle},
	}

	for _, tc := range cases {
		lc := NewLifecycle()
		if tc.from != StatusIdle {
			_ = lc.SetStatus(StatusRunning)
		}
		if tc.from == StatusPaused {
			_ = lc.SetStatus(StatusPaused)
		}
		if tc.from == StatusCompleted {
			_ = lc.SetStatus(StatusCompleted)
		}
		if tc.from == StatusFailed {
			_ = lc.SetStatus(StatusFailed)
		}
		if tc.from == StatusCancelled {
			_ = lc.SetStatus(StatusCancelled)
		}

		if err := lc.SetStatus(tc.to); err != nil {
			t.Errorf("transition from %s to %s should succeed, got error: %v", tc.from, tc.to, err)
		}
		if lc.Status() != tc.to {
			t.Errorf("expected status %s, got %s", tc.to, lc.Status())
		}
	}
}

func TestLifecycle_StateHistory(t *testing.T) {
	lc := NewLifecycle()

	_ = lc.SetStatus(StatusRunning)
	_ = lc.SetStatus(StatusPaused)
	_ = lc.SetStatus(StatusRunning)
	_ = lc.SetStatus(StatusCompleted)

	history := lc.History()
	if len(history) != 4 {
		t.Errorf("expected 4 transitions, got %d", len(history))
	}

	if history[0].From != StatusIdle || history[0].To != StatusRunning {
		t.Errorf("first transition: expected idle->running, got %s->%s", history[0].From, history[0].To)
	}
	if history[2].From != StatusPaused || history[2].To != StatusRunning {
		t.Errorf("third transition: expected paused->running, got %s->%s", history[2].From, history[2].To)
	}
}

func TestLifecycle_RegisterHook(t *testing.T) {
	lc := NewLifecycle()

	var hookCalled bool
	var hookFrom, hookTo AgentStatus

	lc.RegisterHook(StatusCompleted, func(from, to AgentStatus) {
		hookCalled = true
		hookFrom = from
		hookTo = to
	})

	_ = lc.SetStatus(StatusRunning)
	if hookCalled {
		t.Error("hook should not be called for running status")
	}

	_ = lc.SetStatus(StatusCompleted)
	if !hookCalled {
		t.Error("hook should be called for completed status")
	}
	if hookFrom != StatusRunning {
		t.Errorf("hook from: expected running, got %s", hookFrom)
	}
	if hookTo != StatusCompleted {
		t.Errorf("hook to: expected completed, got %s", hookTo)
	}
}

func TestLifecycle_StateDuration(t *testing.T) {
	lc := NewLifecycle()

	initialDur := lc.StateDuration()
	if initialDur < 0 {
		t.Error("state duration should be non-negative")
	}

	_ = lc.SetStatus(StatusRunning)
	runningDur := lc.StateDuration()
	if runningDur < 0 {
		t.Error("state duration should be non-negative")
	}
}

func TestLifecycle_TotalRunningTime(t *testing.T) {
	lc := NewLifecycle()

	_ = lc.SetStatus(StatusRunning)

	total := lc.TotalRunningTime()
	if total < 0 {
		t.Error("total running time should be non-negative")
	}

	_ = lc.SetStatus(StatusPaused)
	pausedTotal := lc.TotalRunningTime()
	if pausedTotal < 0 {
		t.Error("paused total should still be non-negative")
	}

	_ = lc.SetStatus(StatusRunning)
	resumedTotal := lc.TotalRunningTime()
	if resumedTotal < pausedTotal {
		t.Error("resumed total should be >= paused total")
	}
}

func TestLifecycle_TransitionCount(t *testing.T) {
	lc := NewLifecycle()

	if lc.TransitionCount() != 0 {
		t.Errorf("initial count: expected 0, got %d", lc.TransitionCount())
	}

	_ = lc.SetStatus(StatusRunning)
	if lc.TransitionCount() != 1 {
		t.Errorf("after 1 transition: expected 1, got %d", lc.TransitionCount())
	}

	_ = lc.SetStatus(StatusPaused)
	_ = lc.SetStatus(StatusRunning)
	if lc.TransitionCount() != 3 {
		t.Errorf("after 3 transitions: expected 3, got %d", lc.TransitionCount())
	}
}

func TestLifecycle_PausedCanCancel(t *testing.T) {
	lc := NewLifecycle()
	_ = lc.SetStatus(StatusRunning)
	_ = lc.SetStatus(StatusPaused)

	err := lc.SetStatus(StatusCancelled)
	if err != nil {
		t.Errorf("paused->cancelled should succeed, got error: %v", err)
	}
	if lc.Status() != StatusCancelled {
		t.Errorf("expected cancelled, got %s", lc.Status())
	}
}


func TestLifecycle_CanTransitionTo(t *testing.T) {
	lc := NewLifecycle()

	if !lc.CanTransitionTo(StatusRunning) {
		t.Error("idle should be able to transition to running")
	}
	if lc.CanTransitionTo(StatusPaused) {
		t.Error("idle should not be able to transition to paused")
	}
	if lc.CanTransitionTo(StatusCompleted) {
		t.Error("idle should not be able to transition to completed")
	}

	_ = lc.SetStatus(StatusRunning)
	if !lc.CanTransitionTo(StatusPaused) {
		t.Error("running should be able to transition to paused")
	}
	if !lc.CanTransitionTo(StatusCompleted) {
		t.Error("running should be able to transition to completed")
	}
	if lc.CanTransitionTo(StatusIdle) {
		t.Error("running should not be able to transition to idle")
	}

	_ = lc.SetStatus(StatusCompleted)
	if lc.CanTransitionTo(StatusRunning) {
		t.Error("completed should not be able to transition to running directly")
	}
	if !lc.CanTransitionTo(StatusIdle) {
		t.Error("completed should be able to transition to idle (reset)")
	}
}

func TestLifecycle_IsTerminal(t *testing.T) {
	cases := []struct {
		status   AgentStatus
		terminal bool
	}{
		{StatusIdle, false},
		{StatusRunning, false},
		{StatusPaused, false},
		{StatusCompleted, true},
		{StatusFailed, true},
		{StatusCancelled, true},
	}

	for _, tc := range cases {
		lc := NewLifecycle()
		if tc.status != StatusIdle {
			_ = lc.SetStatus(StatusRunning)
		}
		if tc.status == StatusPaused {
			_ = lc.SetStatus(StatusPaused)
		}
		if tc.status == StatusCompleted {
			_ = lc.SetStatus(StatusCompleted)
		}
		if tc.status == StatusFailed {
			_ = lc.SetStatus(StatusFailed)
		}
		if tc.status == StatusCancelled {
			_ = lc.SetStatus(StatusCancelled)
		}

		if lc.IsTerminal() != tc.terminal {
			t.Errorf("status %s: expected IsTerminal=%v, got %v", tc.status, tc.terminal, lc.IsTerminal())
		}
	}
}

func TestLifecycle_AvailableTransitions(t *testing.T) {
	cases := []struct {
		status   AgentStatus
		expected []AgentStatus
	}{
		{StatusIdle, []AgentStatus{StatusRunning}},
		{StatusRunning, []AgentStatus{StatusPaused, StatusWaitingForInput, StatusCompleted, StatusFailed, StatusCancelled}},
		{StatusPaused, []AgentStatus{StatusRunning, StatusCancelled}},
		{StatusCompleted, []AgentStatus{StatusIdle}},
		{StatusFailed, []AgentStatus{StatusIdle}},
		{StatusCancelled, []AgentStatus{StatusIdle}},
	}

	for _, tc := range cases {
		lc := NewLifecycle()
		if tc.status != StatusIdle {
			_ = lc.SetStatus(StatusRunning)
		}
		if tc.status == StatusPaused {
			_ = lc.SetStatus(StatusPaused)
		}
		if tc.status == StatusCompleted {
			_ = lc.SetStatus(StatusCompleted)
		}
		if tc.status == StatusFailed {
			_ = lc.SetStatus(StatusFailed)
		}
		if tc.status == StatusCancelled {
			_ = lc.SetStatus(StatusCancelled)
		}

		got := lc.AvailableTransitions()
		if len(got) != len(tc.expected) {
			t.Errorf("status %s: expected %v, got %v", tc.status, tc.expected, got)
			continue
		}
		for _, exp := range tc.expected {
			if !slices.Contains(got, exp) {
				t.Errorf("status %s: expected transition %s not found in %v", tc.status, exp, got)
			}
		}
	}
}

func TestLifecycle_CompleteStateMachine(t *testing.T) {
	paths := [][]AgentStatus{
		{StatusIdle, StatusRunning, StatusCompleted},
		{StatusIdle, StatusRunning, StatusFailed},
		{StatusIdle, StatusRunning, StatusCancelled},
		{StatusIdle, StatusRunning, StatusPaused, StatusRunning, StatusCompleted},
		{StatusIdle, StatusRunning, StatusPaused, StatusRunning, StatusFailed},
		{StatusIdle, StatusRunning, StatusFailed, StatusIdle, StatusRunning, StatusCompleted},
		{StatusIdle, StatusRunning, StatusCompleted, StatusIdle, StatusRunning, StatusFailed},
		{StatusIdle, StatusRunning, StatusCancelled, StatusIdle, StatusRunning, StatusCompleted},
	}

	for i, path := range paths {
		lc := NewLifecycle()
		for j := 1; j < len(path); j++ {
			if err := lc.SetStatus(path[j]); err != nil {
				t.Errorf("path %d: transition from %s to %s failed: %v", i, path[j-1], path[j], err)
				break
			}
		}
		final := path[len(path)-1]
		if lc.Status() != final {
			t.Errorf("path %d: expected final status %s, got %s", i, final, lc.Status())
		}
	}
}

// ===== 新增增强功能测试 =====

func TestLifecycle_Reset(t *testing.T) {
	terminalStates := []AgentStatus{StatusCompleted, StatusFailed, StatusCancelled}

	for _, terminal := range terminalStates {
		lc := NewLifecycle()
		_ = lc.SetStatus(StatusRunning)
		_ = lc.SetStatus(terminal)

		if err := lc.Reset(); err != nil {
			t.Errorf("reset from %s should succeed, got error: %v", terminal, err)
		}
		if lc.Status() != StatusIdle {
			t.Errorf("after reset from %s: expected idle, got %s", terminal, lc.Status())
		}

		if err := lc.SetStatus(StatusRunning); err != nil {
			t.Errorf("should be able to run again after reset from %s, got error: %v", terminal, err)
		}
	}
}

func TestLifecycle_ResetFromNonTerminal(t *testing.T) {
	lc := NewLifecycle()

	// 现口径：Reset 从非终态返回错误（不再包装 ErrInvalidTransition，
	// 与 lifecycle 包 TestResetNonTerminal 一致）。
	err := lc.Reset()
	if err == nil {
		t.Error("reset from idle should fail")
	}

	_ = lc.SetStatus(StatusRunning)
	err = lc.Reset()
	if err == nil {
		t.Error("reset from running should fail")
	}

	// 终态可 Reset
	_ = lc.SetStatus(StatusFailed)
	if err := lc.Reset(); err != nil {
		t.Errorf("reset from failed should succeed, got %v", err)
	}
}

func TestLifecycle_Retry(t *testing.T) {
	lc := NewLifecycle()
	_ = lc.SetStatus(StatusRunning)
	_ = lc.SetStatus(StatusFailed)

	if err := lc.Retry(); err != nil {
		t.Errorf("retry from failed should succeed, got error: %v", err)
	}
	if lc.Status() != StatusRunning {
		t.Errorf("after retry: expected running, got %s", lc.Status())
	}

	history := lc.History()
	lastTransition := history[len(history)-1]
	if lastTransition.Reason != "retry" {
		t.Errorf("expected reason 'retry', got %q", lastTransition.Reason)
	}
}

func TestLifecycle_RetryFromNonFailed(t *testing.T) {
	lc := NewLifecycle()

	err := lc.Retry()
	if err == nil {
		t.Error("retry from idle should fail")
	}

	_ = lc.SetStatus(StatusRunning)
	_ = lc.SetStatus(StatusCompleted)
	err = lc.Retry()
	if err == nil {
		t.Error("retry from completed should fail (must reset first)")
	}
}

func TestLifecycle_SetStatusWithReason(t *testing.T) {
	lc := NewLifecycle()

	if err := lc.SetStatusWithReason(StatusRunning, "user initiated"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	history := lc.History()
	if len(history) != 1 {
		t.Fatalf("expected 1 transition, got %d", len(history))
	}
	if history[0].Reason != "user initiated" {
		t.Errorf("expected reason 'user initiated', got %q", history[0].Reason)
	}
}

func TestLifecycle_Fail(t *testing.T) {
	lc := NewLifecycle()
	_ = lc.SetStatus(StatusRunning)

	if err := lc.Fail("timeout"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lc.Status() != StatusFailed {
		t.Errorf("expected failed, got %s", lc.Status())
	}

	last, ok := lc.LastTransition()
	if !ok {
		t.Fatal("expected last transition to exist")
	}
	if last.Reason != "timeout" {
		t.Errorf("expected reason 'timeout', got %q", last.Reason)
	}
}

func TestLifecycle_Complete(t *testing.T) {
	lc := NewLifecycle()
	_ = lc.SetStatus(StatusRunning)

	if err := lc.Complete("task done"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lc.Status() != StatusCompleted {
		t.Errorf("expected completed, got %s", lc.Status())
	}

	last, ok := lc.LastTransition()
	if !ok {
		t.Fatal("expected last transition to exist")
	}
	if last.Reason != "task done" {
		t.Errorf("expected reason 'task done', got %q", last.Reason)
	}
}

func TestLifecycle_Cancel(t *testing.T) {
	lc := NewLifecycle()
	_ = lc.SetStatus(StatusRunning)

	if err := lc.Cancel("user abort"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lc.Status() != StatusCancelled {
		t.Errorf("expected cancelled, got %s", lc.Status())
	}

	last, ok := lc.LastTransition()
	if !ok {
		t.Fatal("expected last transition to exist")
	}
	if last.Reason != "user abort" {
		t.Errorf("expected reason 'user abort', got %q", last.Reason)
	}
}

func TestLifecycle_LastTransition(t *testing.T) {
	lc := NewLifecycle()

	_, ok := lc.LastTransition()
	if ok {
		t.Error("empty lifecycle should have no last transition")
	}

	_ = lc.SetStatus(StatusRunning)
	last, ok := lc.LastTransition()
	if !ok {
		t.Fatal("expected last transition to exist")
	}
	if last.From != StatusIdle || last.To != StatusRunning {
		t.Errorf("expected idle->running, got %s->%s", last.From, last.To)
	}
}

func TestLifecycle_StateSince(t *testing.T) {
	lc := NewLifecycle()
	before := time.Now()

	_ = lc.SetStatus(StatusRunning)

	since := lc.StateSince()
	if since.Before(before) {
		t.Error("state since should be after the transition")
	}
}


func TestLifecycle_OnTransition(t *testing.T) {
	lc := NewLifecycle()

	var transitions []AgentStatus
	lc.OnTransition(func(s AgentStatus) {
		transitions = append(transitions, s)
	})

	_ = lc.SetStatus(StatusRunning)
	_ = lc.SetStatus(StatusCompleted)

	if len(transitions) != 2 {
		t.Fatalf("expected 2 transitions, got %d", len(transitions))
	}
	if transitions[0] != StatusRunning {
		t.Errorf("first transition: expected running, got %s", transitions[0])
	}
	if transitions[1] != StatusCompleted {
		t.Errorf("second transition: expected completed, got %s", transitions[1])
	}
}

func TestLifecycle_SetTimeout(t *testing.T) {
	lc := NewLifecycle()
	lc.SetTimeout(50 * time.Millisecond)

	_ = lc.SetStatus(StatusRunning)
	if lc.Status() != StatusRunning {
		t.Fatal("expected running")
	}

	time.Sleep(100 * time.Millisecond)

	if lc.Status() != StatusFailed {
		t.Errorf("expected failed after timeout, got %s", lc.Status())
	}

	last, ok := lc.LastTransition()
	if !ok {
		t.Fatal("expected last transition")
	}
	if last.Reason != "running timeout exceeded" {
		t.Errorf("expected timeout reason, got %q", last.Reason)
	}
}

func TestLifecycle_SetTimeout_CancelBeforeExpiry(t *testing.T) {
	lc := NewLifecycle()
	lc.SetTimeout(200 * time.Millisecond)

	_ = lc.SetStatus(StatusRunning)
	_ = lc.SetStatus(StatusCancelled)

	time.Sleep(300 * time.Millisecond)

	if lc.Status() != StatusCancelled {
		t.Errorf("expected cancelled (timeout should not fire after manual cancel), got %s", lc.Status())
	}
}

func TestLifecycle_PauseResumeWithHooks(t *testing.T) {
	lc := NewLifecycle()

	var hookCalls []string
	lc.RegisterHook(StatusPaused, func(from, to AgentStatus) {
		hookCalls = append(hookCalls, "paused")
	})
	lc.RegisterHook(StatusRunning, func(from, to AgentStatus) {
		hookCalls = append(hookCalls, "running:"+string(from))
	})

	_ = lc.SetStatus(StatusRunning)
	lc.Pause()
	lc.Resume()

	if len(hookCalls) != 3 {
		t.Fatalf("expected 3 hook calls, got %d: %v", len(hookCalls), hookCalls)
	}
	if hookCalls[0] != "running:idle" {
		t.Errorf("first hook: expected 'running:idle', got %q", hookCalls[0])
	}
	if hookCalls[1] != "paused" {
		t.Errorf("second hook: expected 'paused', got %q", hookCalls[1])
	}
	if hookCalls[2] != "running:paused" {
		t.Errorf("third hook: expected 'running:paused', got %q", hookCalls[2])
	}
}

func TestLifecycle_ResetPreservesHistory(t *testing.T) {
	lc := NewLifecycle()
	_ = lc.SetStatus(StatusRunning)
	_ = lc.SetStatus(StatusFailed)

	beforeReset := lc.TransitionCount()
	_ = lc.Reset()

	if lc.TransitionCount() != beforeReset+1 {
		t.Errorf("reset should add one transition, expected %d, got %d", beforeReset+1, lc.TransitionCount())
	}

	history := lc.History()
	last := history[len(history)-1]
	if last.From != StatusFailed || last.To != StatusIdle {
		t.Errorf("last transition should be failed->idle, got %s->%s", last.From, last.To)
	}
	if last.Reason != "reset" {
		t.Errorf("expected reason 'reset', got %q", last.Reason)
	}
}

func TestLifecycle_MultipleResetCycles(t *testing.T) {
	lc := NewLifecycle()

	for i := 0; i < 5; i++ {
		_ = lc.SetStatus(StatusRunning)
		_ = lc.SetStatus(StatusFailed)
		_ = lc.Reset()
	}

	if lc.Status() != StatusIdle {
		t.Errorf("expected idle after 5 cycles, got %s", lc.Status())
	}
	if lc.TransitionCount() != 15 {
		t.Errorf("expected 15 transitions (3 per cycle * 5), got %d", lc.TransitionCount())
	}
}
