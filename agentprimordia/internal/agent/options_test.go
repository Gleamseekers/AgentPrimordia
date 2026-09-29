// options_test.go — Functional Options 单元测试
// v0.7.0 API 稳定化：覆盖所有 WithXxx Option 函数（4 标量 + 14 顶层快捷注入 + 4 分组注入）
package agent

import (
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/internal/memory"
	"github.com/Gleamseekers/AgentPrimordia/internal/persist"
	"github.com/Gleamseekers/AgentPrimordia/internal/tools"
)

// ===== 4 个标量 Option 测试 =====

func TestWithMaxTurns(t *testing.T) {
	cfg := defaultConfig()
	WithMaxTurns(15)(&cfg)
	if cfg.MaxTurns != 15 {
		t.Errorf("MaxTurns = %d, want 15", cfg.MaxTurns)
	}
}

func TestWithTemperature(t *testing.T) {
	cfg := defaultConfig()
	WithTemperature(0.7)(&cfg)
	if cfg.Temperature != 0.7 {
		t.Errorf("Temperature = %v, want 0.7", cfg.Temperature)
	}
}

func TestWithSessionID(t *testing.T) {
	cfg := defaultConfig()
	WithSessionID("session-xyz")(&cfg)
	if cfg.SessionID != "session-xyz" {
		t.Errorf("SessionID = %q, want %q", cfg.SessionID, "session-xyz")
	}
}

func TestWithPromptTemplate(t *testing.T) {
	cfg := defaultConfig()
	tmpl := NewPromptTemplate("hello {{.Name}}")
	WithPromptTemplate(tmpl)(&cfg)
	if cfg.PromptTemplate != tmpl {
		t.Error("PromptTemplate 未被正确设置")
	}
}

// ===== 14 个顶层快捷注入 Option 测试 =====

func TestWithMemory(t *testing.T) {
	cfg := defaultConfig()
	// 传 nil 验证不 panic
	WithMemory(nil)(&cfg)
	if cfg.Memory.Store != nil {
		t.Error("Memory.Store 应为 nil")
	}
}

func TestWithToolkit(t *testing.T) {
	cfg := defaultConfig()
	r := tools.NewRegistry()
	WithToolkit(r)(&cfg)
	if cfg.Tools.Registry != r {
		t.Error("Tools.Registry 未被正确设置")
	}
}

func TestWithHooks(t *testing.T) {
	cfg := defaultConfig()
	h := NewHookManager()
	WithHooks(h)(&cfg)
	if cfg.Observability.Hooks != h {
		t.Error("Observability.Hooks 未被正确设置")
	}
}

func TestWithRAG(t *testing.T) {
	cfg := defaultConfig()
	ragCfg := RAGConfig{
		Mode:     RAGModeAuto,
		TopK:     3,
		MinScore: 0.5,
	}
	WithRAG(ragCfg)(&cfg)
	if cfg.RAG.TopK != 3 {
		t.Errorf("RAG.TopK = %d, want 3", cfg.RAG.TopK)
	}
	if cfg.RAG.Mode != RAGModeAuto {
		t.Errorf("RAG.Mode = %q, want %q", cfg.RAG.Mode, RAGModeAuto)
	}
	if cfg.RAG.MinScore != 0.5 {
		t.Errorf("RAG.MinScore = %v, want 0.5", cfg.RAG.MinScore)
	}
}

func TestWithTracer(t *testing.T) {
	cfg := defaultConfig()
	// 传 nil 验证不 panic
	WithTracer(nil)(&cfg)
	if cfg.Observability.Tracer != nil {
		t.Error("Observability.Tracer 应为 nil")
	}
}

func TestWithCostTracker(t *testing.T) {
	cfg := defaultConfig()
	ct := NewCostTracker(nil, nil)
	WithCostTracker(ct)(&cfg)
	if cfg.Observability.CostTracker != ct {
		t.Error("Observability.CostTracker 未被正确设置")
	}
}

func TestWithContextWindow(t *testing.T) {
	cfg := defaultConfig()
	// 传 nil 验证不 panic
	WithContextWindow(nil)(&cfg)
	if cfg.Resilience.ContextWindow != nil {
		t.Error("Resilience.ContextWindow 应为 nil")
	}
}

func TestWithEvents(t *testing.T) {
	cfg := defaultConfig()
	// 传 nil 验证不 panic
	WithEvents(nil)(&cfg)
	if cfg.Observability.Events != nil {
		t.Error("Observability.Events 应为 nil")
	}
}

func TestWithMetrics(t *testing.T) {
	cfg := defaultConfig()
	// 传 nil 验证不 panic
	WithMetrics(nil)(&cfg)
	if cfg.Observability.Metrics != nil {
		t.Error("Observability.Metrics 应为 nil")
	}
}

func TestWithCheckpointStore(t *testing.T) {
	cfg := defaultConfig()
	// 传 nil 验证不 panic
	WithCheckpointStore(nil)(&cfg)
	if cfg.Resilience.CheckpointStore != nil {
		t.Error("Resilience.CheckpointStore 应为 nil")
	}
}

func TestWithSummarizer(t *testing.T) {
	cfg := defaultConfig()
	// 传 nil 验证不 panic
	WithSummarizer(nil)(&cfg)
	if cfg.Memory.Summarizer != nil {
		t.Error("Memory.Summarizer 应为 nil")
	}
}

func TestWithFileScope(t *testing.T) {
	cfg := defaultConfig()
	scopes := []string{"/tmp/a", "/tmp/b"}
	WithFileScope(scopes)(&cfg)
	if len(cfg.Memory.FileScope) != 2 {
		t.Fatalf("FileScope 长度 = %d, want 2", len(cfg.Memory.FileScope))
	}
	if cfg.Memory.FileScope[0] != "/tmp/a" || cfg.Memory.FileScope[1] != "/tmp/b" {
		t.Errorf("FileScope = %v, want %v", cfg.Memory.FileScope, scopes)
	}
}

func TestWithCache(t *testing.T) {
	cfg := defaultConfig()
	// 传 nil 验证不 panic
	WithCache(nil)(&cfg)
	if cfg.Resilience.Cache != nil {
		t.Error("Resilience.Cache 应为 nil")
	}
}

func TestWithHITL(t *testing.T) {
	cfg := defaultConfig()
	h := &HITLConfig{
		AutoApproveTools: []string{"safe_tool"},
	}
	WithHITL(h)(&cfg)
	if cfg.Resilience.HITL != h {
		t.Error("Resilience.HITL 未被正确设置")
	}
}

// ===== 4 个分组注入 Option 测试 =====

func TestWithMemoryConfig(t *testing.T) {
	cfg := defaultConfig()
	mc := MemoryConfig{
		FileScope: []string{"/data"},
	}
	WithMemoryConfig(mc)(&cfg)
	if len(cfg.Memory.FileScope) != 1 || cfg.Memory.FileScope[0] != "/data" {
		t.Errorf("Memory.FileScope = %v, want [/data]", cfg.Memory.FileScope)
	}
}

func TestWithObservability(t *testing.T) {
	cfg := defaultConfig()
	oc := ObservabilityConfig{
		Tracer: NewNoopTracer(),
	}
	WithObservability(oc)(&cfg)
	if cfg.Observability.Tracer == nil {
		t.Error("Observability.Tracer 不应为 nil")
	}
}

func TestWithResilience(t *testing.T) {
	cfg := defaultConfig()
	rc := ResilienceConfig{
		ContextWindow: NewDefaultStrategy(100),
	}
	WithResilience(rc)(&cfg)
	if cfg.Resilience.ContextWindow == nil {
		t.Error("Resilience.ContextWindow 不应为 nil")
	}
}

func TestWithToolsConfig(t *testing.T) {
	cfg := defaultConfig()
	tc := ToolsConfig{
		Registry: tools.NewRegistry(),
	}
	WithToolsConfig(tc)(&cfg)
	if cfg.Tools.Registry == nil {
		t.Error("Tools.Registry 不应为 nil")
	}
}

// ===== 组合使用测试 =====

func TestOptionsCombination(t *testing.T) {
	cfg := defaultConfig()
	opts := []Option{
		WithMaxTurns(10),
		WithTemperature(0.5),
		WithSessionID("combo-session"),
		WithTracer(NewNoopTracer()),
		WithHooks(NewHookManager()),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.MaxTurns != 10 {
		t.Errorf("MaxTurns = %d, want 10", cfg.MaxTurns)
	}
	if cfg.Temperature != 0.5 {
		t.Errorf("Temperature = %v, want 0.5", cfg.Temperature)
	}
	if cfg.SessionID != "combo-session" {
		t.Errorf("SessionID = %q, want %q", cfg.SessionID, "combo-session")
	}
	if cfg.Observability.Tracer == nil {
		t.Error("Tracer 不应为 nil")
	}
	if cfg.Observability.Hooks == nil {
		t.Error("Hooks 不应为 nil")
	}
}

// 确保未使用的 import 不会导致编译失败（这些 import 在实现阶段会用到）
var _ llm.LLMCache = (llm.LLMCache)(nil)
var _ memory.SummaryExtractor = (memory.SummaryExtractor)(nil)
var _ persist.CheckpointStore = (persist.CheckpointStore)(nil)
