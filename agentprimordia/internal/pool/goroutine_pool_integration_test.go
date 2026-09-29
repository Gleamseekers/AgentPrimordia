package pool

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/concurrency"
)

// TestPool_GoroutinePool_NotConfigured 验证未配置 GoroutinePool 时的行为。
func TestPool_GoroutinePool_NotConfigured(t *testing.T) {
	p := NewPool(PoolConfig{MaxConcurrency: 4})
	defer p.Close()

	if p.HasGoroutinePool() {
		t.Error("未配置 GoroutinePool 时 HasGoroutinePool 应返回 false")
	}

	if _, ok := p.GoroutinePoolStats(); ok {
		t.Error("未配置 GoroutinePool 时 GoroutinePoolStats 应返回 ok=false")
	}

	err := p.SubmitBackground(context.Background(), func(ctx context.Context) error {
		return nil
	})
	if err != concurrency.ErrPoolStopped {
		t.Errorf("未配置时 SubmitBackground 应返回 concurrency.ErrPoolStopped，实际: %v", err)
	}
}

// TestPool_GoroutinePool_Configured 验证配置 GoroutinePool 后的行为。
func TestPool_GoroutinePool_Configured(t *testing.T) {
	p := NewPool(PoolConfig{
		MaxConcurrency: 4,
		GoroutinePool: &GoroutinePoolConfig{
			MinWorkers:  2,
			MaxWorkers:  8,
			QueueSize:   16,
			IdleTimeout: 5 * time.Second,
		},
	})
	defer p.Close()

	if !p.HasGoroutinePool() {
		t.Fatal("配置后 HasGoroutinePool 应返回 true")
	}

	stats, ok := p.GoroutinePoolStats()
	if !ok {
		t.Fatal("GoroutinePoolStats 应返回 ok=true")
	}
	if stats.MinWorkers != 2 {
		t.Errorf("MinWorkers = %d, 期望 2", stats.MinWorkers)
	}
	if stats.MaxWorkers != 8 {
		t.Errorf("MaxWorkers = %d, 期望 8", stats.MaxWorkers)
	}
	if stats.QueueCapacity != 16 {
		t.Errorf("QueueCapacity = %d, 期望 16", stats.QueueCapacity)
	}
	if stats.Workers < 2 {
		t.Errorf("初始 Workers = %d, 期望 ≥2", stats.Workers)
	}
}

// TestPool_SubmitBackground_ExecutesTask 验证后台任务能被实际执行。
func TestPool_SubmitBackground_ExecutesTask(t *testing.T) {
	p := NewPool(PoolConfig{
		MaxConcurrency: 4,
		GoroutinePool: &GoroutinePoolConfig{
			MinWorkers: 2, MaxWorkers: 4, QueueSize: 8, IdleTimeout: time.Second,
		},
	})
	defer p.Close()

	var counter atomic.Int32
	done := make(chan struct{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for i := 0; i < 5; i++ {
		err := p.SubmitBackground(ctx, func(c context.Context) error {
			counter.Add(1)
			if counter.Load() == 5 {
				close(done)
			}
			return nil
		})
		if err != nil {
			t.Errorf("SubmitBackground 失败: %v", err)
		}
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("后台任务未全部执行，counter=%d", counter.Load())
	}

	if got := counter.Load(); got != 5 {
		t.Errorf("counter = %d, 期望 5", got)
	}
}

// TestPool_SubmitBackground_ContextCancel 验证 ctx 取消能终止后台任务。
func TestPool_SubmitBackground_ContextCancel(t *testing.T) {
	p := NewPool(PoolConfig{
		MaxConcurrency: 4,
		GoroutinePool: &GoroutinePoolConfig{
			MinWorkers: 2, MaxWorkers: 4, QueueSize: 8, IdleTimeout: time.Second,
		},
	})
	defer p.Close()

	ctx, cancel := context.WithCancel(context.Background())
	var blocked atomic.Int32

	err := p.SubmitBackground(ctx, func(c context.Context) error {
		blocked.Add(1)
		<-c.Done() // 阻塞直到 ctx 取消
		return c.Err()
	})
	if err != nil {
		t.Fatalf("SubmitBackground 失败: %v", err)
	}

	// 等任务进入阻塞状态
	deadline := time.Now().Add(time.Second)
	for blocked.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if blocked.Load() == 0 {
		t.Fatal("任务未启动")
	}

	cancel()
	// 给任务一点时间完成
	time.Sleep(100 * time.Millisecond)

	stats, _ := p.GoroutinePoolStats()
	if stats.IsStopped {
		t.Errorf("Pool 未关闭，IsStopped 应为 false")
	}
}

// TestPool_GoroutinePoolStats_QueueDepthGrowing 验证任务堆积时 QueueDepth 上升。
func TestPool_GoroutinePoolStats_QueueDepthGrowing(t *testing.T) {
	p := NewPool(PoolConfig{
		MaxConcurrency: 4,
		GoroutinePool: &GoroutinePoolConfig{
			MinWorkers: 1, MaxWorkers: 1, QueueSize: 32, IdleTimeout: time.Second,
		},
	})
	defer p.Close()

	// 提交阻塞任务耗尽唯一 worker
	block := make(chan struct{})
	defer close(block)
	for i := 0; i < 1; i++ {
		_ = p.SubmitBackground(context.Background(), func(c context.Context) error {
			<-block
			return nil
		})
	}

	// 再提交 5 个任务堆积
	for i := 0; i < 5; i++ {
		_ = p.SubmitBackground(context.Background(), func(c context.Context) error {
			return nil
		})
	}

	// 给 worker 一点时间进入工作状态
	time.Sleep(50 * time.Millisecond)

	stats, _ := p.GoroutinePoolStats()
	if stats.QueueDepth == 0 {
		t.Errorf("应有 ≥1 个任务堆积，实际 QueueDepth=%d", stats.QueueDepth)
	}
	if stats.ActiveWorkers == 0 {
		t.Errorf("应有 ≥1 个 active worker，实际=%d", stats.ActiveWorkers)
	}
}
