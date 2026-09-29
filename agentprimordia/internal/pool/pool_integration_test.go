package pool

import (
	"context"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm"
)

// TestPool_Stats_Fields 验证 PoolStats 字段完整性
func TestPool_Stats_Fields(t *testing.T) {
	mockLLM := llm.NewMockLLM(t).WithResponse("ok")
	pool := NewPool(PoolConfig{
		MaxConcurrency: 5,
		Timeout:        30 * time.Second,
	})
	pool.SetModel(mockLLM)
	defer pool.Close()

	stats := pool.Stats()
	if stats.MaxConcurrency != 5 {
		t.Errorf("MaxConcurrency = %d, 期望 5", stats.MaxConcurrency)
	}
	if stats.ActiveConcurrency != 0 {
		t.Errorf("初始 ActiveConcurrency = %d, 期望 0", stats.ActiveConcurrency)
	}
}

// TestPool_AutoScaler_StaticCalculate 测试 AutoScaler 静态计算（不实际跑定时器）
func TestPool_AutoScaler_StaticCalculate(t *testing.T) {
	cfg := AutoScalerConfig{
		MinConcurrency:     2,
		MaxConcurrency:     20,
		ScaleUpThreshold:   0.8,
		ScaleDownThreshold: 0.2,
		CoolDownPeriod:     100 * time.Millisecond,
		CheckInterval:      1 * time.Second,
	}
	as := NewAutoScaler(cfg)

	// 初始：扩容场景
	newConc := as.Calculate(8, 0, 10)
	if newConc <= 10 {
		t.Errorf("高负载下应扩容，实际: %d", newConc)
	}
	if newConc > 20 {
		t.Errorf("扩容超过最大值 20: %d", newConc)
	}

	// 等待冷却期过后再缩容
	time.Sleep(150 * time.Millisecond)
	newConc = as.Calculate(1, 0, 20)
	if newConc >= 20 {
		t.Errorf("低负载下应缩容，实际: %d", newConc)
	}
	if newConc < 2 {
		t.Errorf("缩容低于最小值 2: %d", newConc)
	}
}

// TestPool_AutoScaler_Cooldown 测试冷却期保护
func TestPool_AutoScaler_Cooldown(t *testing.T) {
	cfg := AutoScalerConfig{
		MinConcurrency:     2,
		MaxConcurrency:     100,
		ScaleUpThreshold:   0.5,
		ScaleDownThreshold: 0.1,
		CoolDownPeriod:     5 * time.Second, // 长冷却
		CheckInterval:      1 * time.Second,
	}
	as := NewAutoScaler(cfg)

	// 第一次扩容：以 10 为起点扩容
	first := as.Calculate(50, 0, 10)
	if first <= 10 {
		t.Errorf("首次应扩容: %d", first)
	}
	// 冷却期内使用 first 作为 current 再计算应保持不变
	second := as.Calculate(50, 0, first)
	if second != first {
		t.Errorf("冷却期内不应变化: first=%d, second=%d", first, second)
	}
}

// TestPool_AutoScaler_Bounds 测试扩缩容边界
func TestPool_AutoScaler_Bounds(t *testing.T) {
	cfg := AutoScalerConfig{
		MinConcurrency:     2,
		MaxConcurrency:     8,
		ScaleUpThreshold:   0.5,
		ScaleDownThreshold: 0.1,
		CoolDownPeriod:     10 * time.Millisecond,
		CheckInterval:      1 * time.Second,
	}
	as := NewAutoScaler(cfg)

	// 多次扩容不能超过 MaxConcurrency
	cur := 2
	for i := 0; i < 10; i++ {
		time.Sleep(15 * time.Millisecond)
		cur = as.Calculate(100, 0, cur)
		if cur > 8 {
			t.Fatalf("超过 MaxConcurrency: %d", cur)
		}
	}
	// 多次缩容不能低于 MinConcurrency
	cur = 8
	for i := 0; i < 10; i++ {
		time.Sleep(15 * time.Millisecond)
		cur = as.Calculate(0, 0, cur)
		if cur < 2 {
			t.Fatalf("低于 MinConcurrency: %d", cur)
		}
	}
}

// TestPool_AutoScaler_ZeroProtection 测试除零保护
func TestPool_AutoScaler_ZeroProtection(t *testing.T) {
	cfg := AutoScalerConfig{
		MinConcurrency: 3,
		MaxConcurrency: 20,
	}
	as := NewAutoScaler(cfg)
	// current=0 时应使用 MinConcurrency
	cur := as.Calculate(0, 0, 0)
	if cur != 3 {
		t.Errorf("current=0 应使用 MinConcurrency=3, 实际: %d", cur)
	}
}

// TestPool_Stats_DuringExecution 测试执行中的统计更新
func TestPool_Stats_DuringExecution(t *testing.T) {
	mockLLM := llm.NewMockLLM(t).
		WithResponse("a").
		WithResponse("b").
		WithResponse("c")
	pool := NewPool(PoolConfig{
		MaxConcurrency: 3,
		Timeout:        30 * time.Second,
	})
	pool.SetModel(mockLLM)
	defer pool.Close()

	tasks := []TaskConfig{
		{ID: "t1", Title: "A", Prompt: "a"},
		{ID: "t2", Title: "B", Prompt: "b"},
		{ID: "t3", Title: "C", Prompt: "c"},
	}

	results, err := pool.Dispatch(context.Background(), tasks)
	if err != nil {
		t.Fatalf("Dispatch 失败: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("期望 3 个结果，实际 %d", len(results))
	}
	// 完成后统计
	stats := pool.Stats()
	if stats.CompletedTasks != 3 {
		t.Errorf("CompletedTasks = %d, 期望 3", stats.CompletedTasks)
	}
	if stats.TotalTasks != 3 {
		t.Errorf("TotalTasks = %d, 期望 3", stats.TotalTasks)
	}
	if stats.FailedTasks != 0 {
		t.Errorf("FailedTasks = %d, 期望 0", stats.FailedTasks)
	}
}

// TestPool_DynamicConcurrency_Semaphore 测试动态并发信号量
func TestPool_DynamicConcurrency_Semaphore(t *testing.T) {
	mockLLM := llm.NewMockLLM(t)
	pool := NewPool(PoolConfig{
		MaxConcurrency: 3,
		Timeout:        30 * time.Second,
	})
	pool.SetModel(mockLLM)
	defer pool.Close()

	// 初始并发度
	if got := pool.dynamicConcurrency.Load(); got != 3 {
		t.Errorf("初始 dynamicConcurrency = %d, 期望 3", got)
	}

	// 直接修改 dynamicConcurrency
	pool.dynamicConcurrency.Store(10)
	if got := pool.dynamicConcurrency.Load(); got != 10 {
		t.Errorf("更新后 dynamicConcurrency = %d, 期望 10", got)
	}

	// 验证 acquireSlot 反映新限制
	// 占用 10 个槽位
	for i := 0; i < 10; i++ {
		if err := pool.acquireSlot(context.Background()); err != nil {
			t.Fatalf("acquireSlot[%d] 失败: %v", i, err)
		}
	}
	// 第 11 个应超时
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := pool.acquireSlot(ctx); err == nil {
		t.Error("第 11 个 acquireSlot 应超时，但未超时")
	}
	// 释放
	for i := 0; i < 10; i++ {
		pool.releaseSlot()
	}
}

// TestPool_DynamicConcurrency_Reset 重置并发度
func TestPool_DynamicConcurrency_Reset(t *testing.T) {
	mockLLM := llm.NewMockLLM(t)
	pool := NewPool(PoolConfig{MaxConcurrency: 2})
	pool.SetModel(mockLLM)
	defer pool.Close()

	pool.dynamicConcurrency.Store(100)
	pool.dynamicConcurrency.Store(2) // 模拟缩容

	// 应能正常获取
	if err := pool.acquireSlot(context.Background()); err != nil {
		t.Errorf("获取槽位失败: %v", err)
	}
	pool.releaseSlot()
}
