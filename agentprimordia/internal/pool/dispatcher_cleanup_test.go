package pool

import (
	"context"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
)

// TestPool_AutoCleanupTaskMap 验证 M8 修复：当 MaxRetainedTasks > 0 时，
// Dispatch 前会自动清理终态任务，避免 task map 无界增长。
// 此前 Cleanup() 方法存在但从未被自动调用，长期运行内存泄漏。
func TestPool_AutoCleanupTaskMap(t *testing.T) {
	mockLLM := llm.NewMockLLM(t).WithResponse("result")

	// 阈值设为 2：跑 3 批（每批 1 个任务），第 3 批派发前应触发清理
	pool := NewPool(PoolConfig{
		MaxConcurrency:   5,
		Timeout:          30 * time.Second,
		MaxRetainedTasks: 2,
	})
	pool.SetModel(mockLLM)
	defer pool.Close()

	// 第 1 批：跑完留下 1 个终态任务
	_, _ = pool.Dispatch(context.Background(), []TaskConfig{
		{ID: "task-batch-1", Title: "b1", Prompt: "do 1"},
	})

	// 第 2 批：跑完累计 2 个终态任务（== 阈值，不触发）
	_, _ = pool.Dispatch(context.Background(), []TaskConfig{
		{ID: "task-batch-2", Title: "b2", Prompt: "do 2"},
	})
	pool.mu.RLock()
	countAfter2 := len(pool.tasks)
	pool.mu.RUnlock()
	if countAfter2 != 2 {
		t.Logf("note: after batch 2, tasks=%d (may include active)", countAfter2)
	}

	// 第 3 批：派发前 tasks=3 > 阈值 2，应触发清理，删除终态任务
	_, _ = pool.Dispatch(context.Background(), []TaskConfig{
		{ID: "task-batch-3", Title: "b3", Prompt: "do 3"},
	})
	pool.mu.RLock()
	countAfter3 := len(pool.tasks)
	pool.mu.RUnlock()

	// 清理后 + 第 3 批完成，终态任务数应 <= 1（第3批本身），
	// 而非持续累积到 3。具体值取决于清理时机，关键是没无限增长。
	if countAfter3 > 2 {
		t.Errorf("M8: task map 应被自动清理，但 tasks=%d（超过阈值 2，未触发清理）", countAfter3)
	}
}

// TestPool_DefaultRetainedTasks 验证默认 MaxRetainedTasks 被设置为 1000，
// 在未超过阈值时保留终态任务，GetTask 仍可查询。
func TestPool_DefaultRetainedTasks(t *testing.T) {
	mockLLM := llm.NewMockLLM(t).WithResponse("result")

	pool := NewPool(PoolConfig{
		MaxConcurrency: 5,
		Timeout:        30 * time.Second,
		// MaxRetainedTasks 不设置，应使用默认值 1000
	})
	pool.SetModel(mockLLM)
	defer pool.Close()

	if pool.config.MaxRetainedTasks != defaultMaxRetainedTasks {
		t.Fatalf("MaxRetainedTasks default = %d, want %d", pool.config.MaxRetainedTasks, defaultMaxRetainedTasks)
	}

	_, _ = pool.Dispatch(context.Background(), []TaskConfig{
		{ID: "retained-task", Title: "rt", Prompt: "do"},
	})

	// 未达阈值不应清理，任务应仍可查询
	result, found := pool.GetTask("retained-task")
	if !found {
		t.Fatal("默认配置在未超阈值时不应清理，GetTask 应找到任务")
	}
	if result.Status != PoolTaskCompleted {
		t.Errorf("Status = %s, want Completed", result.Status)
	}
}
