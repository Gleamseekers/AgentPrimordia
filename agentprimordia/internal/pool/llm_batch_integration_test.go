package pool

import (
	"context"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/internal/llm"
)

// mockProvider 是最简的 llm.Provider 实现，仅用于 Phase 3 Task 6 集成测试。
type mockProvider struct{}

func (m *mockProvider) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return &llm.CompletionResponse{Content: "ok"}, nil
}
func (m *mockProvider) Stream(ctx context.Context, req *llm.CompletionRequest) (<-chan llm.Chunk, error) {
	ch := make(chan llm.Chunk)
	close(ch)
	return ch, nil
}
func (m *mockProvider) CallTools(ctx context.Context, req *llm.ToolCallRequest) (*llm.ToolCallResponse, error) {
	return &llm.ToolCallResponse{}, nil
}
func (m *mockProvider) Info() llm.ModelInfo { return llm.ModelInfo{Name: "mock"} }

// TestPool_SetLLMBatchProcessor_ReplacesModel 验证 SetLLMBatchProcessor 替换 model。
func TestPool_SetLLMBatchProcessor_ReplacesModel(t *testing.T) {
	p := NewPool(PoolConfig{MaxConcurrency: 4})
	defer p.Close()

	p.SetModel(&mockProvider{})

	bp := llm.NewBatchProcessor(&mockProvider{}, llm.DefaultBatchConfig())
	defer bp.Close()
	p.SetLLMBatchProcessor(bp)

	stats, ok := p.LLMBatchStats()
	if !ok {
		t.Fatal("LLMBatchStats 应返回 ok=true")
	}
	if !stats.Enabled {
		t.Errorf("stats.Enabled = false, 期望 true")
	}
	if !stats.HasModel {
		t.Errorf("stats.HasModel = false, 期望 true")
	}
}

// TestPool_LLMBatchStats_NotConfigured 验证未配置 BatchProcessor 时返回 ok=false。
func TestPool_LLMBatchStats_NotConfigured(t *testing.T) {
	p := NewPool(PoolConfig{MaxConcurrency: 4})
	defer p.Close()

	_, ok := p.LLMBatchStats()
	if ok {
		t.Errorf("未配置 BatchProcessor 时 LLMBatchStats 应返回 ok=false")
	}
}

// TestPool_RunBatchFlushLoop_NoGoroutinePool 验证未配置 GoroutinePool 时返回错误。
func TestPool_RunBatchFlushLoop_NoGoroutinePool(t *testing.T) {
	p := NewPool(PoolConfig{MaxConcurrency: 4})
	defer p.Close()

	err := p.RunBatchFlushLoop(context.Background())
	if err == nil {
		t.Error("未配置 GoroutinePool 时 RunBatchFlushLoop 应返回错误")
	}
}

// TestPool_RunBatchFlushLoop_NoBatchProcessor 验证未配置 BatchProcessor 时直接成功。
func TestPool_RunBatchFlushLoop_NoBatchProcessor(t *testing.T) {
	p := NewPool(PoolConfig{
		MaxConcurrency: 4,
		GoroutinePool: &GoroutinePoolConfig{
			MinWorkers: 2, MaxWorkers: 4, QueueSize: 8, IdleTimeout: 1,
		},
	})
	defer p.Close()

	if err := p.RunBatchFlushLoop(context.Background()); err != nil {
		t.Errorf("仅配置 GoroutinePool 时 RunBatchFlushLoop 应返回 nil，实际: %v", err)
	}
}

// TestPool_RunBatchFlushLoop_WithBoth 验证同时配置 GoroutinePool + BatchProcessor 时 flush 任务被调度。
func TestPool_RunBatchFlushLoop_WithBoth(t *testing.T) {
	p := NewPool(PoolConfig{
		MaxConcurrency: 4,
		GoroutinePool: &GoroutinePoolConfig{
			MinWorkers: 2, MaxWorkers: 4, QueueSize: 8, IdleTimeout: 1,
		},
	})
	defer p.Close()

	bp := llm.NewBatchProcessor(&mockProvider{}, llm.DefaultBatchConfig())
	defer bp.Close()
	p.SetLLMBatchProcessor(bp)

	if err := p.RunBatchFlushLoop(context.Background()); err != nil {
		t.Errorf("RunBatchFlushLoop 应成功，实际: %v", err)
	}
}
