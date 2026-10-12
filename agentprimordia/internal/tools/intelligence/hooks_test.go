// hooks_test.go — 工具智能 ReAct Hook 测试
package intelligence

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// === 桩实现 ===

// stubProfiler 记录调用次数
type stubProfiler struct {
	mu      sync.Mutex
	records []ToolUsageRecord
}

func (p *stubProfiler) Record(_ context.Context, usage ToolUsageRecord) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.records = append(p.records, usage)
	return nil
}

func (p *stubProfiler) Profile(_ context.Context, _ string) (*ToolProfile, error) {
	return &ToolProfile{}, nil
}

func (p *stubProfiler) AllProfiles(_ context.Context) (map[string]*ToolProfile, error) {
	return nil, nil
}

// stubDetector 返回预设缺口
type stubDetector struct {
	gaps []GapCandidate
	err  error
}

func (d *stubDetector) Detect(_ context.Context, _ []ToolCallRecord) ([]GapCandidate, error) {
	return d.gaps, d.err
}

// stubCreator 记录生成调用
type stubCreator struct {
	mu      sync.Mutex
	created []GapCandidate
}

func (c *stubCreator) Create(_ context.Context, gap GapCandidate) (*ToolArtifact, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.created = append(c.created, gap)
	return &ToolArtifact{ID: "auto-" + gap.Key}, nil
}

// === 测试 ===

func TestIntelligenceHook_AfterToolCall_RecordsUsage(t *testing.T) {
	profiler := &stubProfiler{}
	detector := &stubDetector{}
	creator := &stubCreator{}
	hook := NewIntelligenceHook(profiler, detector, creator)

	ctx := context.Background()
	hook.AfterToolCall(ctx, "shell", "ls -la", "file1\nfile2", nil, 50*time.Millisecond)

	// 验证 profiler 记录
	if len(profiler.records) != 1 {
		t.Fatalf("expected 1 profiler record, got %d", len(profiler.records))
	}
	rec := profiler.records[0]
	if rec.ToolName != "shell" {
		t.Errorf("expected tool name 'shell', got %q", rec.ToolName)
	}
	if !rec.Success {
		t.Error("expected success=true")
	}
	if rec.Duration != 50*time.Millisecond {
		t.Errorf("expected duration 50ms, got %v", rec.Duration)
	}

	// 验证轨迹记录
	if hook.TraceLength() != 1 {
		t.Errorf("expected trace length 1, got %d", hook.TraceLength())
	}
}

func TestIntelligenceHook_AfterToolCall_RecordsError(t *testing.T) {
	profiler := &stubProfiler{}
	detector := &stubDetector{}
	creator := &stubCreator{}
	hook := NewIntelligenceHook(profiler, detector, creator)

	ctx := context.Background()
	hook.AfterToolCall(ctx, "web_fetch", "http://example.com", "", errors.New("timeout"), 5*time.Second)

	// 验证 profiler 记录失败
	if len(profiler.records) != 1 {
		t.Fatalf("expected 1 profiler record, got %d", len(profiler.records))
	}
	if profiler.records[0].Success {
		t.Error("expected success=false for error case")
	}
}

func TestIntelligenceHook_OnTurnEnd_DetectsGaps(t *testing.T) {
	profiler := &stubProfiler{}
	expectedGaps := []GapCandidate{
		{Kind: "missing_tool", Key: "csv_parser", Count: 3},
	}
	detector := &stubDetector{gaps: expectedGaps}
	creator := &stubCreator{}
	hook := NewIntelligenceHook(profiler, detector, creator)

	ctx := context.Background()

	// 添加调用记录
	hook.AfterToolCall(ctx, "shell", "cat data.csv", "error: unsupported format", errors.New("unsupported format"), 100*time.Millisecond)

	// 轮次结束，应触发缺口检测
	hook.OnTurnEnd(ctx)

	// 验证工具生成器被调用
	if len(creator.created) != 1 {
		t.Fatalf("expected 1 tool created, got %d", len(creator.created))
	}
	if creator.created[0].Key != "csv_parser" {
		t.Errorf("expected gap key 'csv_parser', got %q", creator.created[0].Key)
	}

	// 验证轨迹已清空
	if hook.TraceLength() != 0 {
		t.Errorf("expected trace to be cleared after OnTurnEnd, got %d", hook.TraceLength())
	}
}

func TestIntelligenceHook_OnTurnEnd_NoGaps(t *testing.T) {
	profiler := &stubProfiler{}
	detector := &stubDetector{gaps: nil} // 无缺口
	creator := &stubCreator{}
	hook := NewIntelligenceHook(profiler, detector, creator)

	ctx := context.Background()
	hook.AfterToolCall(ctx, "shell", "echo hello", "hello", nil, 10*time.Millisecond)

	hook.OnTurnEnd(ctx)

	// 验证未生成工具
	if len(creator.created) != 0 {
		t.Errorf("expected 0 tools created, got %d", len(creator.created))
	}
}

func TestIntelligenceHook_OnTurnEnd_EmptyTrace(t *testing.T) {
	profiler := &stubProfiler{}
	detector := &stubDetector{}
	creator := &stubCreator{}
	hook := NewIntelligenceHook(profiler, detector, creator)

	ctx := context.Background()
	// 无调用记录直接结束轮次，不应 panic
	hook.OnTurnEnd(ctx)

	if len(creator.created) != 0 {
		t.Errorf("expected 0 tools created for empty trace, got %d", len(creator.created))
	}
}

// 验证接口实现
var _ ToolProfiler = (*stubProfiler)(nil)
var _ GapDetector = (*stubDetector)(nil)
var _ ToolCreator = (*stubCreator)(nil)
