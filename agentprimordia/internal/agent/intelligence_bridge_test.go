// intelligence_bridge_test.go — IntelligenceHook 桥接 HookManager 测试
//
// 覆盖：
//   - 配置为 nil 时桥接不生效（铁律 7：默认路径零变化）
//   - resolveCapabilities 创建 IntelligenceHook 实例
//   - HookAfterTool 桥接 → AfterToolCall（画像记录 + 轨迹追加）
//   - HookAfterTurn 桥接 → OnTurnEnd（缺口检测 + 工具创建）
//   - 端到端：通过 HookManager.Fire 触发完整桥接链路
package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"agentprimordia/internal/agent/hooks"
	"agentprimordia/internal/llm"
	"agentprimordia/internal/tools/intelligence"
)

// ===== 桩实现 =====

// bridgeStubProfiler 记录工具使用画像
type bridgeStubProfiler struct {
	mu      sync.Mutex
	records []intelligence.ToolUsageRecord
}

func (p *bridgeStubProfiler) Record(_ context.Context, usage intelligence.ToolUsageRecord) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.records = append(p.records, usage)
	return nil
}

func (p *bridgeStubProfiler) Profile(_ context.Context, _ string) (*intelligence.ToolProfile, error) {
	return &intelligence.ToolProfile{}, nil
}

func (p *bridgeStubProfiler) AllProfiles(_ context.Context) (map[string]*intelligence.ToolProfile, error) {
	return nil, nil
}

// bridgeStubDetector 返回预设缺口
type bridgeStubDetector struct {
	gaps []intelligence.GapCandidate
	err  error
}

func (d *bridgeStubDetector) Detect(_ context.Context, _ []intelligence.ToolCallRecord) ([]intelligence.GapCandidate, error) {
	return d.gaps, d.err
}

// bridgeStubCreator 记录工具生成调用
type bridgeStubCreator struct {
	mu      sync.Mutex
	created []intelligence.GapCandidate
}

func (c *bridgeStubCreator) Create(_ context.Context, gap intelligence.GapCandidate) (*intelligence.ToolArtifact, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.created = append(c.created, gap)
	return &intelligence.ToolArtifact{ID: "auto-" + gap.Key}, nil
}

// 接口实现验证
var _ intelligence.ToolProfiler = (*bridgeStubProfiler)(nil)
var _ intelligence.GapDetector = (*bridgeStubDetector)(nil)
var _ intelligence.ToolCreator = (*bridgeStubCreator)(nil)

// ===== 测试 =====

// TestIntelligenceBridge_NilConfig 验证 ToolIntelligence 为 nil 时桥接不生效
func TestIntelligenceBridge_NilConfig(t *testing.T) {
	a := newReActAgent(ReActConfig{
		Name:     "bridge-nil",
		MaxTurns: 1,
	})
	// 不设置 ToolIntelligence
	hm := hooks.NewHookManager()
	a.hooks = hm

	a.setupIntelligenceBridge()

	// HookManager 不应注册任何桥接 Hook
	if hm.Count(HookAfterTool) != 0 {
		t.Errorf("expected 0 HookAfterTool hooks for nil config, got %d", hm.Count(HookAfterTool))
	}
	if hm.Count(HookAfterTurn) != 0 {
		t.Errorf("expected 0 HookAfterTurn hooks for nil config, got %d", hm.Count(HookAfterTurn))
	}
}

// TestIntelligenceBridge_ResolveCapabilities 验证 resolveCapabilities 创建 IntelligenceHook
func TestIntelligenceBridge_ResolveCapabilities(t *testing.T) {
	profiler := &bridgeStubProfiler{}
	detector := &bridgeStubDetector{}
	creator := &bridgeStubCreator{}

	a := newReActAgent(ReActConfig{
		Name:     "bridge-resolve",
		MaxTurns: 1,
		Model:    llm.NewMockLLM(t),
		ToolIntelligence: &ToolIntelligenceConfig{
			Profiler: profiler,
			Detector: detector,
			Creator:  creator,
		},
	})

	cap := a.resolveCapabilities("req-1")
	if cap.intelligenceHook == nil {
		t.Fatal("expected intelligenceHook to be created in resolveCapabilities")
	}
}

// TestIntelligenceBridge_AfterToolCall 验证 HookAfterTool 桥接 → AfterToolCall
func TestIntelligenceBridge_AfterToolCall(t *testing.T) {
	profiler := &bridgeStubProfiler{}
	detector := &bridgeStubDetector{}
	creator := &bridgeStubCreator{}

	a := newReActAgent(ReActConfig{
		Name:     "bridge-tool",
		MaxTurns: 1,
		Model:    llm.NewMockLLM(t),
		ToolIntelligence: &ToolIntelligenceConfig{
			Profiler: profiler,
			Detector: detector,
			Creator:  creator,
		},
	})

	hm := hooks.NewHookManager()
	a.hooks = hm

	// 模拟 resolveCapabilities 创建 IntelligenceHook
	a.capCache = &capabilityCache{
		intelligenceHook: intelligence.NewIntelligenceHook(profiler, detector, creator),
	}

	// 注册桥接
	a.setupIntelligenceBridge()

	// 验证 Hook 已注册
	if hm.Count(HookAfterTool) != 1 {
		t.Fatalf("expected 1 HookAfterTool hook, got %d", hm.Count(HookAfterTool))
	}

	// 触发 HookAfterTool
	hctx := &hooks.HookContext{
		Point: HookAfterTool,
		ToolCall: &ToolCall{
			ID:   "call-1",
			Name: "shell",
			Args: "ls -la",
		},
		ToolResult: &ToolResult{
			ToolCallID: "call-1",
			Content:    "file1\nfile2",
		},
		Duration: 50 * time.Millisecond,
	}
	if err := hm.Fire(context.Background(), hctx); err != nil {
		t.Fatalf("fire HookAfterTool failed: %v", err)
	}

	// 验证 profiler 记录
	if len(profiler.records) != 1 {
		t.Fatalf("expected 1 profiler record, got %d", len(profiler.records))
	}
	if profiler.records[0].ToolName != "shell" {
		t.Errorf("expected tool name 'shell', got %q", profiler.records[0].ToolName)
	}
	if !profiler.records[0].Success {
		t.Error("expected success=true")
	}
	if profiler.records[0].Duration != 50*time.Millisecond {
		t.Errorf("expected duration 50ms, got %v", profiler.records[0].Duration)
	}

	// 验证 IntelligenceHook 轨迹
	if a.capCache.intelligenceHook.TraceLength() != 1 {
		t.Errorf("expected trace length 1, got %d", a.capCache.intelligenceHook.TraceLength())
	}
}

// TestIntelligenceBridge_AfterToolCall_Error 验证错误场景的桥接
func TestIntelligenceBridge_AfterToolCall_Error(t *testing.T) {
	profiler := &bridgeStubProfiler{}
	detector := &bridgeStubDetector{}
	creator := &bridgeStubCreator{}

	a := newReActAgent(ReActConfig{
		Name:     "bridge-tool-err",
		MaxTurns: 1,
		Model:    llm.NewMockLLM(t),
		ToolIntelligence: &ToolIntelligenceConfig{
			Profiler: profiler,
			Detector: detector,
			Creator:  creator,
		},
	})

	hm := hooks.NewHookManager()
	a.hooks = hm
	a.capCache = &capabilityCache{
		intelligenceHook: intelligence.NewIntelligenceHook(profiler, detector, creator),
	}
	a.setupIntelligenceBridge()

	hctx := &hooks.HookContext{
		Point: HookAfterTool,
		ToolCall: &ToolCall{
			ID:   "call-2",
			Name: "web_fetch",
			Args: "http://example.com",
		},
		ToolResult: &ToolResult{
			ToolCallID: "call-2",
			Content:    "",
			IsError:    true,
		},
		Error:    context.DeadlineExceeded,
		Duration: 5 * time.Second,
	}
	if err := hm.Fire(context.Background(), hctx); err != nil {
		t.Fatalf("fire HookAfterTool failed: %v", err)
	}

	// 验证 profiler 记录了失败
	if len(profiler.records) != 1 {
		t.Fatalf("expected 1 profiler record, got %d", len(profiler.records))
	}
	if profiler.records[0].Success {
		t.Error("expected success=false for error case")
	}
}

// TestIntelligenceBridge_OnTurnEnd 验证 HookAfterTurn 桥接 → OnTurnEnd
func TestIntelligenceBridge_OnTurnEnd(t *testing.T) {
	profiler := &bridgeStubProfiler{}
	expectedGaps := []intelligence.GapCandidate{
		{Kind: "missing_tool", Key: "csv_parser", Count: 3},
	}
	detector := &bridgeStubDetector{gaps: expectedGaps}
	creator := &bridgeStubCreator{}

	a := newReActAgent(ReActConfig{
		Name:     "bridge-turn",
		MaxTurns: 1,
		Model:    llm.NewMockLLM(t),
		ToolIntelligence: &ToolIntelligenceConfig{
			Profiler: profiler,
			Detector: detector,
			Creator:  creator,
		},
	})

	hm := hooks.NewHookManager()
	a.hooks = hm
	a.capCache = &capabilityCache{
		intelligenceHook: intelligence.NewIntelligenceHook(profiler, detector, creator),
	}
	a.setupIntelligenceBridge()

	// 先触发一次工具调用（填充轨迹）
	toolCtx := &hooks.HookContext{
		Point: HookAfterTool,
		ToolCall: &ToolCall{
			ID:   "call-3",
			Name: "shell",
			Args: "cat data.csv",
		},
		ToolResult: &ToolResult{
			ToolCallID: "call-3",
			Content:    "error: unsupported",
		},
		Duration: 100 * time.Millisecond,
	}
	_ = hm.Fire(context.Background(), toolCtx)

	// 触发轮次结束
	turnCtx := &hooks.HookContext{
		Point: HookAfterTurn,
		Turn:  1,
	}
	if err := hm.Fire(context.Background(), turnCtx); err != nil {
		t.Fatalf("fire HookAfterTurn failed: %v", err)
	}

	// 验证工具生成器被调用
	if len(creator.created) != 1 {
		t.Fatalf("expected 1 tool created, got %d", len(creator.created))
	}
	if creator.created[0].Key != "csv_parser" {
		t.Errorf("expected gap key 'csv_parser', got %q", creator.created[0].Key)
	}

	// 验证轨迹已清空
	if a.capCache.intelligenceHook.TraceLength() != 0 {
		t.Errorf("expected trace to be cleared after OnTurnEnd, got %d",
			a.capCache.intelligenceHook.TraceLength())
	}
}

// TestIntelligenceBridge_NilCapCache 验证 capCache 为 nil 时桥接函数安全跳过
func TestIntelligenceBridge_NilCapCache(t *testing.T) {
	a := newReActAgent(ReActConfig{
		Name:     "bridge-nil-cap",
		MaxTurns: 1,
		ToolIntelligence: &ToolIntelligenceConfig{
			Profiler: &bridgeStubProfiler{},
			Detector: &bridgeStubDetector{},
			Creator:  &bridgeStubCreator{},
		},
	})

	hm := hooks.NewHookManager()
	a.hooks = hm
	// 不设置 capCache

	a.setupIntelligenceBridge()

	// 触发 Hook 不应 panic
	hctx := &hooks.HookContext{
		Point: HookAfterTool,
		ToolCall: &ToolCall{
			ID:   "call-x",
			Name: "test",
		},
	}
	if err := hm.Fire(context.Background(), hctx); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	turnCtx := &hooks.HookContext{Point: HookAfterTurn, Turn: 1}
	if err := hm.Fire(context.Background(), turnCtx); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

// TestIntelligenceBridge_Idempotent 验证多次调用 setupIntelligenceBridge 不会重复注册
func TestIntelligenceBridge_Idempotent(t *testing.T) {
	profiler := &bridgeStubProfiler{}
	a := newReActAgent(ReActConfig{
		Name:     "bridge-idem",
		MaxTurns: 1,
		ToolIntelligence: &ToolIntelligenceConfig{
			Profiler: profiler,
			Detector: &bridgeStubDetector{},
			Creator:  &bridgeStubCreator{},
		},
	})

	hm := hooks.NewHookManager()
	a.hooks = hm
	a.capCache = &capabilityCache{
		intelligenceHook: intelligence.NewIntelligenceHook(profiler, &bridgeStubDetector{}, &bridgeStubCreator{}),
	}

	// 多次调用
	a.setupIntelligenceBridge()
	a.setupIntelligenceBridge()
	a.setupIntelligenceBridge()

	// 验证只注册了一次
	if hm.Count(HookAfterTool) != 1 {
		t.Errorf("expected 1 HookAfterTool hook after idempotent setup, got %d", hm.Count(HookAfterTool))
	}
	if hm.Count(HookAfterTurn) != 1 {
		t.Errorf("expected 1 HookAfterTurn hook after idempotent setup, got %d", hm.Count(HookAfterTurn))
	}
}
