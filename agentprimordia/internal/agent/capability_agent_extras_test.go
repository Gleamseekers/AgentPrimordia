package agent

// 本文件补 capability_agent.go 和 chain_api.go 的 0% 覆盖函数。
// 覆盖范围：
//   chain_api.go:        WithToolkit / WithHooks / WithRAG / WithHITL / WithTracer / WithCostTracker
//                        / WithContextWindow / WithEvents / WithMetrics / WithCheckpointStore
//                        / WithSummarizer / WithFileScope / WithCache / Search / WithRAGMemory
//                        / defaultRAGMinScore
//   capability_agent.go: ResumeFromCheckpoint / Pause / Resume / GetCache
//                        / WithHITL / WithCostTracker / WithCheckpointStore
//                        / WithPlanner / GetPlanner / WithReflector / GetReflector
//                        / WithToolLearner / GetToolLearner

import (
	"context"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/lifecycle"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/planning"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/reflection"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/tool_learning"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/memory"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/persist"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/pkg/logger"
)

// stubPlanner 是 planning.Planner 的最小实现，仅用于验证 WithPlanner 注入语义。
type stubPlanner struct{}

func (s *stubPlanner) Decompose(_ context.Context, _ string) ([]planning.SubTask, error) {
	return nil, nil
}
func (s *stubPlanner) GeneratePlan(_ context.Context, _ string) (*planning.Plan, error) {
	return &planning.Plan{}, nil
}

// stubReflector 是 reflection.Reflector 的最小实现。
type stubReflector struct{}

func (s *stubReflector) Reflect(_ context.Context, _, _ string) (*reflection.Reflection, error) {
	return &reflection.Reflection{}, nil
}
func (s *stubReflector) Critique(_ context.Context, _ string) (*reflection.Critique, error) {
	return &reflection.Critique{}, nil
}
func (s *stubReflector) Improve(_ context.Context, _ string, _ *reflection.Critique) (string, error) {
	return "", nil
}

// stubToolLearner 是 tool_learning.ToolLearner 的最小实现。
type stubToolLearner struct{}

func (s *stubToolLearner) RecordSuccess(_ context.Context, _, _, _ string) error { return nil }
func (s *stubToolLearner) RecordFailure(_ context.Context, _, _, _ string) error { return nil }
func (s *stubToolLearner) GetBestPractices(_ context.Context, _ string) ([]tool_learning.BestPractice, error) {
	return nil, nil
}
func (s *stubToolLearner) SuggestImprovement(_ context.Context, _, _ string) (*tool_learning.Suggestion, error) {
	return &tool_learning.Suggestion{}, nil
}
func (s *stubToolLearner) SuggestProcessCorrection(_ context.Context, _, _ string) (*tool_learning.ProcessCorrection, error) {
	return &tool_learning.ProcessCorrection{}, nil
}

// stubLLMCache 是 llm.LLMCache 的空实现。
// a.config.Model 为 nil 时 llm.NewCachedProvider 会失败，但 WithCache 走 err 分支不 panic。
type stubLLMCache struct{}

func (s *stubLLMCache) Get(_ context.Context, _ string, _ float32) (*llm.CompletionResponse, bool) {
	return nil, false
}
func (s *stubLLMCache) Set(_ context.Context, _ string, _ *llm.CompletionResponse) error {
	return nil
}
func (s *stubLLMCache) Stats(_ context.Context) llm.CacheStats       { return llm.CacheStats{} }
func (s *stubLLMCache) Clear(_ context.Context) error                { return nil }
func (s *stubLLMCache) Invalidate(_ context.Context, _ string) error { return nil }

// stubEmbedder 是 memory.EmbeddingProvider 的最小实现。WithRAGMemory 不实际调用 Embed。
type stubEmbedder struct{}

func (s *stubEmbedder) Embed(_ context.Context, _ []string) ([][]float32, error) {
	return nil, nil
}
func (s *stubEmbedder) Dimensions() int { return 8 }

// stubSummaryExtractor 是 memory.SummaryExtractor 的最小实现。
// 该名称避免与 summary_store_test.go 中的 stubSummarizer 冲突。
type stubSummaryExtractor struct{}

func (s *stubSummaryExtractor) ExtractSummary(_ context.Context, content string) (*memory.SummaryResult, error) {
	return &memory.SummaryResult{Summary: "summary: " + content, Topics: ""}, nil
}

// stubCheckpointStore 是 persist.CheckpointStore 的最小实现。
type stubCheckpointStore struct{}

func (s *stubCheckpointStore) Save(_ context.Context, _ *persist.AgentState) error { return nil }
func (s *stubCheckpointStore) Load(_ context.Context, _ string) (*persist.AgentState, error) {
	return nil, nil
}
func (s *stubCheckpointStore) List(_ context.Context, _ string) ([]*persist.AgentState, error) {
	return nil, nil
}
func (s *stubCheckpointStore) Delete(_ context.Context, _ string) error { return nil }

// ===== chain_api.go: ReActAgent.WithXxx 链式入口 =====

// TestChainAPI_ReactAgent_WithToolkit 覆盖 chain_api.go:42 WithToolkit
func TestChainAPI_ReactAgent_WithToolkit(t *testing.T) {
	a := &ReActAgent{}
	cap := a.WithToolkit(nil)
	if cap.GetToolkit() != nil {
		t.Error("WithToolkit(nil) 应允许 GetToolkit 返回 nil 注入")
	}
}

// TestChainAPI_ReactAgent_WithHooks 覆盖 chain_api.go:47 WithHooks
func TestChainAPI_ReactAgent_WithHooks(t *testing.T) {
	a := &ReActAgent{}
	hooks := NewHookManager()
	cap := a.WithHooks(hooks)
	if cap.GetHooks() != hooks {
		t.Error("WithHooks 未注入到 CapabilityAgent")
	}
	if a.hooks != hooks {
		t.Error("WithHooks 应同步设置 ReActAgent.hooks 字段")
	}
}

// TestChainAPI_ReactAgent_WithRAG 覆盖 chain_api.go:53 WithRAG
func TestChainAPI_ReactAgent_WithRAG(t *testing.T) {
	a := &ReActAgent{}
	cfg := RAGConfig{Mode: RAGModeAuto, TopK: 7}
	cap := a.WithRAG(cfg)
	if cap.GetRAGConfig() == nil || cap.GetRAGConfig().TopK != 7 {
		t.Errorf("WithRAG 未正确注入 RAGConfig, got %+v", cap.GetRAGConfig())
	}
}

// TestChainAPI_ReactAgent_WithHITL 覆盖 chain_api.go:58 WithHITL
// HITLConfig 实际字段：InterruptPoints / HumanInputChan / OnInterrupt / AutoApproveTools
func TestChainAPI_ReactAgent_WithHITL(t *testing.T) {
	a := &ReActAgent{}
	approveTools := []string{"shell", "filesystem"}
	cfg := HITLConfig{AutoApproveTools: approveTools}
	cap := a.WithHITL(cfg)
	got := cap.GetHITLConfig()
	if got == nil {
		t.Fatal("WithHITL 未注入 HITLConfig")
	}
	if len(got.AutoApproveTools) != 2 || got.AutoApproveTools[0] != "shell" {
		t.Errorf("HITLConfig.AutoApproveTools 未正确注入, got %v", got.AutoApproveTools)
	}
	if a.hitlMgr == nil {
		t.Error("WithHITL 应在 ReActAgent 内部创建 hitlMgr")
	}
}

// TestChainAPI_ReactAgent_WithTracer 覆盖 chain_api.go:64 WithTracer
// 使用现有的 NewNoopTracer()（tracer.go 中已实现）
func TestChainAPI_ReactAgent_WithTracer(t *testing.T) {
	a := &ReActAgent{}
	tr := NewNoopTracer()
	cap := a.WithTracer(tr)
	if cap.GetTracer() == nil {
		t.Error("WithTracer 未注入")
	}
}

// TestChainAPI_ReactAgent_WithCostTracker 覆盖 chain_api.go:69 WithCostTracker
func TestChainAPI_ReactAgent_WithCostTracker(t *testing.T) {
	a := &ReActAgent{}
	ct := NewCostTracker(nil, nil)
	cap := a.WithCostTracker(ct)
	if cap.GetCostTracker() != ct {
		t.Error("WithCostTracker 未注入")
	}
}

// TestChainAPI_ReactAgent_WithContextWindow 覆盖 chain_api.go:74 WithContextWindow
func TestChainAPI_ReactAgent_WithContextWindow(t *testing.T) {
	a := &ReActAgent{}
	cw := NewDefaultStrategy(20)
	cap := a.WithContextWindow(cw)
	if cap.GetContextWindowStrategy() != cw {
		t.Error("WithContextWindow 未注入")
	}
}

// TestChainAPI_ReactAgent_WithEvents 覆盖 chain_api.go:79 WithEvents
func TestChainAPI_ReactAgent_WithEvents(t *testing.T) {
	a := &ReActAgent{}
	ep := &capTestEventPublisher{}
	cap := a.WithEvents(ep)
	if cap.GetEventPublisher() != ep {
		t.Error("WithEvents 未注入")
	}
}

// TestChainAPI_ReactAgent_WithMetrics 覆盖 chain_api.go:84 WithMetrics
func TestChainAPI_ReactAgent_WithMetrics(t *testing.T) {
	a := &ReActAgent{}
	mr := &capTestMetricsRecorder{}
	cap := a.WithMetrics(mr)
	if cap.GetMetricsRecorder() != mr {
		t.Error("WithMetrics 未注入")
	}
}

// TestChainAPI_ReactAgent_WithCheckpointStore 覆盖 chain_api.go:89 WithCheckpointStore
func TestChainAPI_ReactAgent_WithCheckpointStore(t *testing.T) {
	a := &ReActAgent{}
	cs := &stubCheckpointStore{}
	cap := a.WithCheckpointStore(cs)
	if cap.GetCheckpointStore() != cs {
		t.Error("WithCheckpointStore 未注入")
	}
}

// TestChainAPI_ReactAgent_WithSummarizer 覆盖 chain_api.go:94 WithSummarizer
func TestChainAPI_ReactAgent_WithSummarizer(t *testing.T) {
	a := &ReActAgent{}
	sm := &stubSummaryExtractor{}
	cap := a.WithSummarizer(sm)
	if cap.GetSummarizer() != sm {
		t.Error("WithSummarizer 未注入")
	}
}

// TestChainAPI_ReactAgent_WithFileScope 覆盖 chain_api.go:99 WithFileScope
func TestChainAPI_ReactAgent_WithFileScope(t *testing.T) {
	a := &ReActAgent{}
	scopes := []string{"/data", "/src"}
	cap := a.WithFileScope(scopes)
	got := cap.GetFileScope()
	if len(got) != 2 || got[0] != "/data" {
		t.Errorf("WithFileScope 未正确注入, got %v", got)
	}
}

// TestChainAPI_ReactAgent_WithCache 覆盖 chain_api.go:105 WithCache
func TestChainAPI_ReactAgent_WithCache(t *testing.T) {
	a := &ReActAgent{}
	c := &stubLLMCache{}
	cap := a.WithCache(c)
	if cap.GetCache() != c {
		t.Error("WithCache 未注入到 CapabilityAgent")
	}
}

// TestChainAPI_RAGStoreAdapter_Search 覆盖 chain_api.go:124 ragStoreAdapter.Search
// 通过 WithRAGMemory 间接构造 adapter，再从 CapabilityAgent.GetRAGConfig 取回 Provider 调用 Search
func TestChainAPI_RAGStoreAdapter_Search(t *testing.T) {
	a := &ReActAgent{}
	mem := memory.NewInMemoryStore()
	emb := &stubEmbedder{}
	cap := a.WithRAGMemory(mem, emb)
	provider := cap.GetRAGConfig().Provider
	if provider == nil {
		t.Fatal("WithRAGMemory 未注入 Provider")
	}
	// 验证 Search 在空 memory 时不 panic
	docs, err := provider.Search(context.Background(), "test query", 3)
	if err != nil {
		t.Fatalf("Search 不应失败: %v", err)
	}
	if len(docs) != 0 {
		t.Logf("Search 返回 %d 条结果（空 memory 时应为 0）", len(docs))
	}
}

// TestChainAPI_ReactAgent_WithRAGMemory 覆盖 chain_api.go:151 WithRAGMemory
// 验证 RAGProvider、Mode、TopK、MinScore 字段全部被正确初始化
func TestChainAPI_ReactAgent_WithRAGMemory(t *testing.T) {
	a := &ReActAgent{}
	mem := memory.NewInMemoryStore()
	emb := &stubEmbedder{}
	cap := a.WithRAGMemory(mem, emb)
	cfg := cap.GetRAGConfig()
	if cfg == nil {
		t.Fatal("WithRAGMemory 未注入 RAGConfig")
	}
	if cfg.Mode != RAGModeAuto {
		t.Errorf("期望 Mode=RAGModeAuto, got %v", cfg.Mode)
	}
	if cfg.TopK != 5 {
		t.Errorf("期望 TopK=5, got %d", cfg.TopK)
	}
	if cfg.MinScore != 0.3 {
		t.Errorf("期望 MinScore=0.3 (defaultRAGMinScore), got %f", cfg.MinScore)
	}
}

// TestChainAPI_DefaultRAGMinScore 覆盖 chain_api.go:162 defaultRAGMinScore
func TestChainAPI_DefaultRAGMinScore(t *testing.T) {
	if got := defaultRAGMinScore(); got != 0.3 {
		t.Errorf("defaultRAGMinScore() = %f, 期望 0.3", got)
	}
}

// ===== capability_agent.go: Pause / Resume / ResumeFromCheckpoint =====

// TestCapabilityAgent_Pause 覆盖 capability_agent.go:88 Pause
// 必须先把状态设为 Running 才能 Pause（lifecycle 状态机约束）
// Pause 内部会调 a.logger.Info()，因此 logger 不能为 nil
func TestCapabilityAgent_Pause(t *testing.T) {
	a := &ReActAgent{lifecycle: NewLifecycle(), logger: logger.Default()}
	cap := a.AsCapability()

	if err := a.lifecycle.SetStatus(lifecycle.StatusRunning); err != nil {
		t.Fatalf("SetStatus Running 失败: %v", err)
	}
	cap.Pause()
	if got := a.lifecycle.Status(); got != lifecycle.StatusPaused {
		t.Errorf("Pause 后状态应为 Paused, got %s", got)
	}
}

// TestCapabilityAgent_Resume 覆盖 capability_agent.go:93 Resume
// 必须先 Paused 才能 Resume；Resume 内部会调 a.logger.Info()，因此 logger 不能为 nil
func TestCapabilityAgent_Resume(t *testing.T) {
	a := &ReActAgent{lifecycle: NewLifecycle(), logger: logger.Default()}
	cap := a.AsCapability()

	if err := a.lifecycle.SetStatus(lifecycle.StatusRunning); err != nil {
		t.Fatalf("SetStatus Running 失败: %v", err)
	}
	cap.Pause()
	cap.Resume()
	if got := a.lifecycle.Status(); got != lifecycle.StatusRunning {
		t.Errorf("Resume 后状态应为 Running, got %s", got)
	}
}

// TestCapabilityAgent_ResumeFromCheckpoint_NoStore 覆盖 capability_agent.go:83 ResumeFromCheckpoint
// 无 CheckpointStore 时应返回 error
func TestCapabilityAgent_ResumeFromCheckpoint_NoStore(t *testing.T) {
	a := &ReActAgent{lifecycle: NewLifecycle(), logger: logger.Default()}
	cap := a.AsCapability()

	resp, err := cap.ResumeFromCheckpoint(context.Background())
	if err == nil {
		t.Error("无 CheckpointStore 应返回 error")
	}
	if resp != nil {
		t.Errorf("无 CheckpointStore 时 Response 应为 nil, got %+v", resp)
	}
}

// ===== capability_agent.go: WithPlanner / GetPlanner =====

// TestCapabilityAgent_WithPlanner 覆盖 capability_agent.go:244 WithPlanner + 250 GetPlanner
func TestCapabilityAgent_WithPlanner(t *testing.T) {
	a := &ReActAgent{}
	cap := a.AsCapability()

	if cap.GetPlanner() != nil {
		t.Fatal("初始 GetPlanner 应为 nil")
	}
	p := &stubPlanner{}
	cap.WithPlanner(p)
	if cap.GetPlanner() != p {
		t.Error("WithPlanner 未正确注入")
	}
}

// ===== capability_agent.go: WithReflector / GetReflector =====

// TestCapabilityAgent_WithReflector 覆盖 capability_agent.go:255 WithReflector + 261 GetReflector
func TestCapabilityAgent_WithReflector(t *testing.T) {
	a := &ReActAgent{}
	cap := a.AsCapability()

	if cap.GetReflector() != nil {
		t.Fatal("初始 GetReflector 应为 nil")
	}
	r := &stubReflector{}
	cap.WithReflector(r)
	if cap.GetReflector() != r {
		t.Error("WithReflector 未正确注入")
	}
}

// ===== capability_agent.go: WithToolLearner / GetToolLearner =====

// TestCapabilityAgent_WithToolLearner 覆盖 capability_agent.go:266 WithToolLearner + 272 GetToolLearner
func TestCapabilityAgent_WithToolLearner(t *testing.T) {
	a := &ReActAgent{}
	cap := a.AsCapability()

	if cap.GetToolLearner() != nil {
		t.Fatal("初始 GetToolLearner 应为 nil")
	}
	tl := &stubToolLearner{}
	cap.WithToolLearner(tl)
	if cap.GetToolLearner() != tl {
		t.Error("WithToolLearner 未正确注入")
	}
}

// ===== capability_agent.go: WithCheckpointStore =====

// TestCapabilityAgent_WithCheckpointStore 覆盖 capability_agent.go:206 WithCheckpointStore
func TestCapabilityAgent_WithCheckpointStore(t *testing.T) {
	a := &ReActAgent{}
	cap := a.AsCapability()

	cs := &stubCheckpointStore{}
	cap.WithCheckpointStore(cs)
	if cap.GetCheckpointStore() != cs {
		t.Error("WithCheckpointStore 未注入")
	}
}

// ===== capability_agent.go: WithCostTracker =====

// TestCapabilityAgent_WithCostTracker 覆盖 capability_agent.go:182 WithCostTracker
func TestCapabilityAgent_WithCostTracker(t *testing.T) {
	a := &ReActAgent{}
	cap := a.AsCapability()

	ct := NewCostTracker(nil, nil)
	cap.WithCostTracker(ct)
	if cap.GetCostTracker() != ct {
		t.Error("WithCostTracker 未注入")
	}
}

// ===== capability_agent.go: WithHITL =====

// TestCapabilityAgent_WithHITL 覆盖 capability_agent.go:161 WithHITL
func TestCapabilityAgent_WithHITL(t *testing.T) {
	a := &ReActAgent{}
	cap := a.AsCapability()

	cfg := HITLConfig{AutoApproveTools: []string{"shell"}}
	cap.WithHITL(cfg)
	got := cap.GetHITLConfig()
	if got == nil {
		t.Fatal("WithHITL 未注入")
	}
	if len(got.AutoApproveTools) != 1 || got.AutoApproveTools[0] != "shell" {
		t.Errorf("AutoApproveTools 未正确注入, got %v", got.AutoApproveTools)
	}
	if a.hitlMgr == nil {
		t.Error("WithHITL 应同时设置 inner.hitlMgr")
	}
	// 静默使用 time 包以避免未使用告警（time 也被 HITLConfig 使用，但本测试不直接依赖）
	_ = time.Second
}

// ===== capability_agent.go: GetCache =====

// TestCapabilityAgent_GetCache 覆盖 capability_agent.go:141 GetCache
// GetCache 初始返回 nil；通过 CapabilityAgent.WithCache 注入后能取回
func TestCapabilityAgent_GetCache(t *testing.T) {
	a := &ReActAgent{}
	cap := a.AsCapability()

	if cap.GetCache() != nil {
		t.Fatal("初始 GetCache 应为 nil")
	}
	c := &stubLLMCache{}
	cap.WithCache(c)
	if cap.GetCache() != c {
		t.Error("WithCache 后 GetCache 未返回注入的 cache")
	}
}
