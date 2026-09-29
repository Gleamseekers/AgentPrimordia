package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/memory"
)

// capTestMemoryStore 是 CapabilityAgent 测试专用的 MemoryStore 实现
type capTestMemoryStore struct {
	mu       sync.Mutex
	episodes []*memory.Episode
}

// Add 生产代码会从多个 goroutine 并发调用（异步 memoryWriter + saveSolutionMemory），
// 测试 mock 须与真实 MemoryStore 一样线程安全，否则 -race 下必现数据竞争。
func (m *capTestMemoryStore) Add(ctx context.Context, episode *memory.Episode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.episodes = append(m.episodes, episode)
	return nil
}

func (m *capTestMemoryStore) UpdateSummary(_ context.Context, _, _, _ string) error {
	return nil // stub: 测试不验证摘要存储
}

// capTestEventPublisher 是 CapabilityAgent 测试专用的 EventPublisher 实现
type capTestEventPublisher struct {
	events []struct {
		eventType string
		source    string
	}
}

func (m *capTestEventPublisher) PublishAsync(eventType string, source string, payload any) error {
	m.events = append(m.events, struct {
		eventType string
		source    string
	}{eventType, source})
	return nil
}

// capTestMetricsRecorder 是 CapabilityAgent 测试专用的 MetricsRecorder 实现
type capTestMetricsRecorder struct {
	llmCalls  int
	toolCalls int
	turns     int
}

func (m *capTestMetricsRecorder) RecordLLMCall(duration time.Duration, err error)                   { m.llmCalls++ }
func (m *capTestMetricsRecorder) RecordToolCall(duration time.Duration, err error)                  { m.toolCalls++ }
func (m *capTestMetricsRecorder) RecordTurn(duration time.Duration)                                 { m.turns++ }
func (m *capTestMetricsRecorder) RecordTokenUsage(model string, promptTokens, completionTokens int) {}
func (m *capTestMetricsRecorder) IncActiveAgents()                                                  {}
func (m *capTestMetricsRecorder) DecActiveAgents()                                                  {}

// ===== 测试：Capable 接口类型断言 =====

func TestCapabilityAgent_ImplementsMemoryCapable(t *testing.T) {
	mockLLM := llm.NewMockLLM(t)
	mockLLM.WithResponse("test response")

	a, err := NewAgent("test-agent", "", mockLLM, WithMaxTurns(5))
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}

	mem := &capTestMemoryStore{}
	capAgent := a.WithMemory(mem)

	// 验证 CapabilityAgent 实现了 MemoryCapable 接口
	var _ MemoryCapable = capAgent

	// 验证接口发现
	if c, ok := any(capAgent).(MemoryCapable); !ok {
		t.Error("CapabilityAgent 应实现 MemoryCapable 接口")
	} else if c.GetMemoryStore() != MemoryStore(mem) {
		t.Error("GetMemoryStore 应返回注入的 MemoryStore")
	}
}

func TestCapabilityAgent_ImplementsRAGCapable(t *testing.T) {
	mockLLM := llm.NewMockLLM(t)
	mockLLM.WithResponse("test response")

	a, err := NewAgent("test-agent", "", mockLLM, WithMaxTurns(5))
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}

	ragCfg := RAGConfig{Mode: RAGModeAuto, TopK: 3}
	capAgent := a.WithRAG(ragCfg)

	var _ RAGCapable = capAgent

	if c, ok := any(capAgent).(RAGCapable); !ok {
		t.Error("CapabilityAgent 应实现 RAGCapable 接口")
	} else if c.GetRAGConfig().TopK != 3 {
		t.Error("GetRAGConfig 应返回注入的 RAGConfig")
	}
}

func TestCapabilityAgent_ImplementsEventCapable(t *testing.T) {
	mockLLM := llm.NewMockLLM(t)
	mockLLM.WithResponse("test response")

	a, err := NewAgent("test-agent", "", mockLLM, WithMaxTurns(5))
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}

	ep := &capTestEventPublisher{}
	capAgent := a.WithEvents(ep)

	var _ EventCapable = capAgent

	if c, ok := any(capAgent).(EventCapable); !ok {
		t.Error("CapabilityAgent 应实现 EventCapable 接口")
	} else if c.GetEventPublisher() != ep {
		t.Error("GetEventPublisher 应返回注入的 EventPublisher")
	}
}

func TestCapabilityAgent_ImplementsMetricsCapable(t *testing.T) {
	mockLLM := llm.NewMockLLM(t)
	mockLLM.WithResponse("test response")

	a, err := NewAgent("test-agent", "", mockLLM, WithMaxTurns(5))
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}

	mr := &capTestMetricsRecorder{}
	capAgent := a.WithMetrics(mr)

	var _ MetricsCapable = capAgent

	if c, ok := any(capAgent).(MetricsCapable); !ok {
		t.Error("CapabilityAgent 应实现 MetricsCapable 接口")
	} else if c.GetMetricsRecorder() != mr {
		t.Error("GetMetricsRecorder 应返回注入的 MetricsRecorder")
	}
}

// ===== 测试：链式 API =====

func TestChainAPI_MultipleCapabilities(t *testing.T) {
	mockLLM := llm.NewMockLLM(t)
	mockLLM.WithResponse("test response")

	mem := &capTestMemoryStore{}
	ep := &capTestEventPublisher{}
	mr := &capTestMetricsRecorder{}

	capAgent, err := NewAgent("chain-agent", "", mockLLM, WithMaxTurns(5))
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}
	capAgent = capAgent.WithMemory(mem).WithEvents(ep).WithMetrics(mr)

	// 验证已注入的能力可发现且非 nil
	if c, ok := any(capAgent).(MemoryCapable); !ok {
		t.Error("应实现 MemoryCapable")
	} else if c.GetMemoryStore() == nil {
		t.Error("MemoryStore 不应为 nil")
	}
	if c, ok := any(capAgent).(EventCapable); !ok {
		t.Error("应实现 EventCapable")
	} else if c.GetEventPublisher() == nil {
		t.Error("EventPublisher 不应为 nil")
	}
	if c, ok := any(capAgent).(MetricsCapable); !ok {
		t.Error("应实现 MetricsCapable")
	} else if c.GetMetricsRecorder() == nil {
		t.Error("MetricsRecorder 不应为 nil")
	}

	// 验证未注入的能力返回 nil（Go 接口满足性：方法存在但返回 nil）
	// 引擎通过 if c, ok := agent.(XxxCapable); ok && c.GetXxx() != nil 检测
	if c, ok := any(capAgent).(RAGCapable); !ok {
		t.Error("CapabilityAgent 应满足 RAGCapable 接口（方法存在）")
	} else if c.GetRAGConfig() != nil {
		t.Error("未注入 RAG 时 GetRAGConfig 应返回 nil")
	}
	if c, ok := any(capAgent).(HITLCapable); !ok {
		t.Error("CapabilityAgent 应满足 HITLCapable 接口（方法存在）")
	} else if c.GetHITLConfig() != nil {
		t.Error("未注入 HITL 时 GetHITLConfig 应返回 nil")
	}
}

func TestChainAPI_SyncsToInnerConfig(t *testing.T) {
	mockLLM := llm.NewMockLLM(t)
	mockLLM.WithResponse("test response")

	mem := &capTestMemoryStore{}
	mr := &capTestMetricsRecorder{}

	capAgent, err := NewAgent("sync-agent", "", mockLLM, WithMaxTurns(5))
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}

	capAgent = capAgent.WithMemory(mem).WithMetrics(mr)

	// 验证通过 CapabilityAgent 接口能获取注入的能力
	if capAgent.GetMemoryStore() != MemoryStore(mem) {
		t.Error("GetMemoryStore 应返回注入的 MemoryStore")
	}
	if capAgent.GetMetricsRecorder() != mr {
		t.Error("GetMetricsRecorder 应返回注入的 MetricsRecorder")
	}

	// 验证内部 ReActAgent 通过接口发现也能获取能力
	inner := capAgent.Inner()
	if innerMem := inner.getMemoryStore(); innerMem != MemoryStore(mem) {
		t.Error("内部 ReActAgent 应通过接口发现获取 MemoryStore")
	}
	if innerMr := inner.getMetricsRecorder(); innerMr != mr {
		t.Error("内部 ReActAgent 应通过接口发现获取 MetricsRecorder")
	}
}

// ===== 测试：Agent 接口委托 =====

func TestCapabilityAgent_DelegatesAgentInterface(t *testing.T) {
	mockLLM := llm.NewMockLLM(t)
	mockLLM.WithResponse("delegated response")

	capAgent, err := NewAgent("delegate-agent", "", mockLLM, WithMaxTurns(5))
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}

	mem := &capTestMemoryStore{}
	capAgent = capAgent.WithMemory(mem)

	// 验证 Agent 接口
	var _ Agent = capAgent

	// 验证 Name 委托
	if capAgent.Name() != "delegate-agent" {
		t.Errorf("Name 应为 delegate-agent，实际为 %s", capAgent.Name())
	}

	// 验证 Run 委托
	resp, err := capAgent.Run(context.Background(), UserMessage("hello"))
	if err != nil {
		t.Errorf("Run 不应返回错误: %v", err)
	}
	if resp.Content != "delegated response" {
		t.Errorf("Response 内容应为 'delegated response'，实际为 %s", resp.Content)
	}

	// 验证记忆已保存
	mem.mu.Lock()
	epCount := len(mem.episodes)
	mem.mu.Unlock()
	if epCount == 0 {
		t.Error("记忆应已保存")
	}
}

func TestCapabilityAgent_InnerMethod(t *testing.T) {
	mockLLM := llm.NewMockLLM(t)
	mockLLM.WithResponse("test")

	capAgent, err := NewAgent("inner-agent", "", mockLLM, WithMaxTurns(5))
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}

	capAgent = capAgent.WithMemory(&capTestMemoryStore{})

	// 验证 Inner 返回非 nil 的 ReActAgent
	if capAgent.Inner() == nil {
		t.Error("Inner 不应返回 nil")
	}
}

// ===== 测试：ReActAgent 未包装时不实现 Capable =====

func TestReActAgent_DoesNotImplementCapable(t *testing.T) {
	mockLLM := llm.NewMockLLM(t)

	capAgent, err := NewAgent("bare-agent", "", mockLLM, WithMaxTurns(5))
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}

	// 通过 Inner() 获取原始 ReActAgent，验证其不实现 Capable 接口
	inner := capAgent.Inner()

	// ReActAgent 本身不实现 Capable 接口（没有 GetXxx 方法）
	// 这是协议式微内核的关键：只有通过 WithXxx 包装后才具备 Capable 接口
	if _, ok := any(inner).(MemoryCapable); ok {
		t.Error("ReActAgent 不应满足 MemoryCapable 接口（无 GetMemoryStore 方法）")
	}
	if _, ok := any(inner).(RAGCapable); ok {
		t.Error("ReActAgent 不应满足 RAGCapable 接口（无 GetRAGConfig 方法）")
	}
}

// ===== 测试：CapabilityAgent 上继续链式注入 =====

func TestChainAPI_ContinueOnCapabilityAgent(t *testing.T) {
	mockLLM := llm.NewMockLLM(t)
	mockLLM.WithResponse("test response")

	mem := &capTestMemoryStore{}
	ep := &capTestEventPublisher{}

	// 先用 NewAgent 创建 CapabilityAgent，再注入 Memory
	capAgent, err := NewAgent("continue-agent", "", mockLLM, WithMaxTurns(5))
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}
	capAgent = capAgent.WithMemory(mem)

	// 在 CapabilityAgent 上继续注入
	capAgent = capAgent.WithEvents(ep)

	// 验证两个能力都存在
	if capAgent.GetMemoryStore() != MemoryStore(mem) {
		t.Error("Memory 应保留")
	}
	if capAgent.GetEventPublisher() != ep {
		t.Error("EventPublisher 应已注入")
	}
}

// ===== 测试：ContextWindowCapable =====

func TestCapabilityAgent_ImplementsContextWindowCapable(t *testing.T) {
	mockLLM := llm.NewMockLLM(t)
	mockLLM.WithResponse("test")

	capAgent, err := NewAgent("ctx-agent", "", mockLLM, WithMaxTurns(5))
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}

	cw := NewDefaultStrategy(50)
	capAgent = capAgent.WithContextWindow(cw)

	var _ ContextWindowCapable = capAgent

	if c, ok := any(capAgent).(ContextWindowCapable); !ok {
		t.Error("CapabilityAgent 应实现 ContextWindowCapable")
	} else if c.GetContextWindowStrategy() != cw {
		t.Error("GetContextWindowStrategy 应返回注入的策略")
	}
}

// ===== 测试：HookCapable =====

func TestCapabilityAgent_ImplementsHookCapable(t *testing.T) {
	mockLLM := llm.NewMockLLM(t)
	mockLLM.WithResponse("test")

	capAgent, err := NewAgent("hook-agent", "", mockLLM, WithMaxTurns(5))
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}

	hooks := NewHookManager()
	capAgent = capAgent.WithHooks(hooks)

	var _ HookCapable = capAgent

	if c, ok := any(capAgent).(HookCapable); !ok {
		t.Error("CapabilityAgent 应实现 HookCapable")
	} else if c.GetHooks() != hooks {
		t.Error("GetHooks 应返回注入的 Hooks")
	}
}

// ===== 测试：FileScopeCapable =====

func TestCapabilityAgent_ImplementsFileScopeCapable(t *testing.T) {
	mockLLM := llm.NewMockLLM(t)
	mockLLM.WithResponse("test")

	capAgent, err := NewAgent("scope-agent", "", mockLLM, WithMaxTurns(5))
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}

	scopes := []string{"/workspace/data", "/workspace/src"}
	capAgent = capAgent.WithFileScope(scopes)

	var _ FileScopeCapable = capAgent

	if c, ok := any(capAgent).(FileScopeCapable); !ok {
		t.Error("CapabilityAgent 应实现 FileScopeCapable")
	} else {
		fs := c.GetFileScope()
		if len(fs) != 2 || fs[0] != "/workspace/data" {
			t.Errorf("GetFileScope 应返回注入的作用域，实际: %v", fs)
		}
	}
}

// TestAgent_CapCacheLifecycle 验证 capCache 生命周期契约（评估修复）：
// Run 结束后 capCache 保留（非 nil）——异步 goroutine（知识蒸馏等）
// 读取不会 panic；每次 Run 在锁内重新填充，不会误用旧引用。
func TestAgent_CapCacheLifecycle(t *testing.T) {
	mock := llm.NewMockLLM(t).WithResponse("ok")
	agt, err := NewAgent("capcache-test", "you are helpful", mock, WithMaxTurns(1))
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	inner := agt.Inner()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := agt.Run(ctx, UserMessage("hi")); err != nil {
		t.Fatalf("Run #1: %v", err)
	}

	// Run 结束后 capCache 必须保留（旧实现 defer 置 nil，异步读取会 panic）
	if inner.capCache == nil {
		t.Fatal("Run 后 capCache 不应被置 nil（异步 goroutine 可能读取）")
	}
	firstReqID := inner.capCache.requestID
	if firstReqID == "" {
		t.Fatal("capCache.requestID 应为本次请求 ID")
	}

	// 第二次 Run：锁内重新填充（新请求 ID），旧值被覆盖而非误用
	if _, err := agt.Run(ctx, UserMessage("hi again")); err != nil {
		t.Fatalf("Run #2: %v", err)
	}
	if inner.capCache == nil {
		t.Fatal("第二次 Run 后 capCache 应已重新填充")
	}
	if inner.capCache.requestID == firstReqID {
		t.Fatal("第二次 Run 应使用新的请求 ID（重新填充）")
	}
}
