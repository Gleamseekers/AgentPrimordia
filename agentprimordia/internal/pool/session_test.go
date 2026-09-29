package pool

import (
	"context"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent"
	"github.com/Gleamseekers/AgentPrimordia/internal/llm"
)

func TestPool_GetTasksBySession(t *testing.T) {
	mockLLM := llm.NewMockLLM(t).
		WithResponse("Result A").
		WithResponse("Result B").
		WithResponse("Result C")

	pool := NewPool(PoolConfig{
		MaxConcurrency: 5,
		Timeout:        30 * time.Second,
	})
	pool.SetModel(mockLLM)
	defer pool.Close()

	tasks := []TaskConfig{
		{ID: "s1-t1", Title: "Session1 Task1", Prompt: "Do A", SessionID: "session-1"},
		{ID: "s1-t2", Title: "Session1 Task2", Prompt: "Do B", SessionID: "session-1"},
		{ID: "s2-t1", Title: "Session2 Task1", Prompt: "Do C", SessionID: "session-2"},
	}

	_, err := pool.Dispatch(context.Background(), tasks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	results := pool.GetTasksBySession("session-1")
	if len(results) != 2 {
		t.Fatalf("expected 2 results for session-1, got %d", len(results))
	}

	for _, r := range results {
		if r.Task.SessionID != "session-1" {
			t.Errorf("task %s has SessionID %q, want %q", r.TaskID, r.Task.SessionID, "session-1")
		}
		if r.Status != PoolTaskCompleted {
			t.Errorf("task %s status = %s, want Completed", r.TaskID, r.Status)
		}
	}

	results2 := pool.GetTasksBySession("session-2")
	if len(results2) != 1 {
		t.Fatalf("expected 1 result for session-2, got %d", len(results2))
	}
}

func TestPool_GetTasksBySession_Empty(t *testing.T) {
	mockLLM := llm.NewMockLLM(t).WithResponse("Done")

	pool := NewPool(PoolConfig{
		MaxConcurrency: 5,
		Timeout:        30 * time.Second,
	})
	pool.SetModel(mockLLM)
	defer pool.Close()

	tasks := []TaskConfig{
		{ID: "t1", Title: "Task", Prompt: "Do", SessionID: "session-1"},
	}
	_, _ = pool.Dispatch(context.Background(), tasks)

	results := pool.GetTasksBySession("nonexistent-session")
	if len(results) != 0 {
		t.Errorf("expected 0 results for nonexistent session, got %d", len(results))
	}
}

func TestPool_CancelBySession(t *testing.T) {
	mockLLM := llm.NewMockLLM(t).WithDelay(2 * time.Second).
		WithResponse("Done").
		WithResponse("Done").
		WithResponse("Done")

	pool := NewPool(PoolConfig{
		MaxConcurrency: 5,
		Timeout:        10 * time.Second,
	})
	pool.SetModel(mockLLM)
	defer pool.Close()

	tasks := []TaskConfig{
		{ID: "s1-cancel-1", Title: "Cancel1", Prompt: "Long", SessionID: "cancel-session"},
		{ID: "s1-cancel-2", Title: "Cancel2", Prompt: "Long", SessionID: "cancel-session"},
		{ID: "s2-keep", Title: "Keep", Prompt: "Long", SessionID: "keep-session"},
	}

	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = pool.CancelBySession("cancel-session")
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	results, _ := pool.Dispatch(ctx, tasks)

	cancelledInSession := 0
	for _, r := range results {
		if r.Task.SessionID == "cancel-session" && r.Status == PoolTaskCancelled {
			cancelledInSession++
		}
	}

	if cancelledInSession != 2 {
		t.Errorf("expected 2 cancelled tasks in cancel-session, got %d", cancelledInSession)
	}
}

func TestPool_CancelBySession_NoTasks(t *testing.T) {
	pool := NewPool(PoolConfig{
		MaxConcurrency: 5,
		Timeout:        30 * time.Second,
	})
	defer pool.Close()

	err := pool.CancelBySession("nonexistent-session")
	if err != nil {
		t.Errorf("expected nil error for nonexistent session, got %v", err)
	}
}

func TestPool_GetTask(t *testing.T) {
	mockLLM := llm.NewMockLLM(t).WithResponse("Task result")

	pool := NewPool(PoolConfig{
		MaxConcurrency: 5,
		Timeout:        30 * time.Second,
	})
	pool.SetModel(mockLLM)
	defer pool.Close()

	tasks := []TaskConfig{
		{ID: "get-task-1", Title: "GetTask", Prompt: "Do something", SessionID: "session-x"},
	}

	_, _ = pool.Dispatch(context.Background(), tasks)

	result, found := pool.GetTask("get-task-1")
	if !found {
		t.Fatal("expected to find task get-task-1")
	}
	if result.TaskID != "get-task-1" {
		t.Errorf("TaskID = %q, want %q", result.TaskID, "get-task-1")
	}
	if result.Status != PoolTaskCompleted {
		t.Errorf("Status = %s, want Completed", result.Status)
	}
	if result.Task.SessionID != "session-x" {
		t.Errorf("SessionID = %q, want %q", result.Task.SessionID, "session-x")
	}
}

func TestPool_GetTask_NotFound(t *testing.T) {
	pool := NewPool(PoolConfig{
		MaxConcurrency: 5,
		Timeout:        30 * time.Second,
	})
	defer pool.Close()

	_, found := pool.GetTask("nonexistent-id")
	if found {
		t.Error("expected not found for nonexistent task ID")
	}
}

func TestPool_Dispatch_WithSessionID(t *testing.T) {
	mockLLM := llm.NewMockLLM(t).WithResponse("Session response")

	var receivedSessionID string
	factory := func(config AgentFactoryConfig) agent.Agent {
		receivedSessionID = config.SessionID
		a, err := agent.NewAgent(config.Name, config.SystemPrompt, mockLLM, agent.WithMaxTurns(config.MaxTurns))
		if err != nil {
			t.Fatalf("failed to create agent: %v", err)
		}
		return a
	}

	pool := NewPool(PoolConfig{
		MaxConcurrency: 5,
		Timeout:        30 * time.Second,
	})
	pool.SetAgentFactory(factory)
	defer pool.Close()

	tasks := []TaskConfig{
		{ID: "session-dispatch-1", Title: "Session Task", Prompt: "Do", SessionID: "dispatch-session"},
	}

	results, err := pool.Dispatch(context.Background(), tasks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if results[0].Status != PoolTaskCompleted {
		t.Errorf("Status = %s, want Completed", results[0].Status)
	}

	if receivedSessionID != "dispatch-session" {
		t.Errorf("AgentFactory received SessionID = %q, want %q", receivedSessionID, "dispatch-session")
	}
}
