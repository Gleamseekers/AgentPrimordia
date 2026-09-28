package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"agentprimordia/internal/memory"
)

// memoryCapableMock 实现 MemoryCapable + Agent。
type memoryCapableMock struct {
	store MemoryStore
}

func (m *memoryCapableMock) GetMemoryStore() MemoryStore { return m.store }
func (m *memoryCapableMock) Run(_ context.Context, _ Message) (*Response, error) {
	return nil, errors.New("not used")
}
func (m *memoryCapableMock) StreamRun(_ context.Context, _ Message) (<-chan StreamEvent, error) {
	return nil, errors.New("not used")
}
func (m *memoryCapableMock) Stop()             {}
func (m *memoryCapableMock) Stats() AgentStats { return AgentStats{} }
func (m *memoryCapableMock) Name() string      { return "memory-capable-mock" }

// TestMemoryReadback_FastPath 验证相似任务第二次命中已解记忆（0 轮推理，显著更快）。
func TestMemoryReadback_FastPath(t *testing.T) {
	mem := memory.NewInMemoryStore()
	ctx := context.Background()

	ag := newReActAgent(ReActConfig{Name: "mem-fast", MaxTurns: 5, Model: &outputGuardMockProvider{content: "慢速推理答案"}})
	ag.self = &memoryCapableMock{store: mem}

	// 第一次：无已解记忆，走正常推理（LLM 返回），并自动把答案存为已解记忆
	r1, err := ag.Run(ctx, UserMessage("用 Go 实现 Fibonacci，负数返回 -1"))
	if err != nil {
		t.Fatalf("第一次 Run error: %v", err)
	}
	if r1.Metrics.MemoryHit {
		t.Error("第一次运行不应命中记忆")
	}
	if r1.Content != "慢速推理答案" {
		t.Errorf("第一次应走正常推理, got %q", r1.Content)
	}

	// 等待已解记忆的异步落库（saveSolutionMemory 为 fire-and-forget
	// 设计，见 memory_readback.go；直接读存在竞态窗口，需显式同步）。
	waitSolutionMemory(t, mem, "Fibonacci", 5*time.Second)

	// 第二次：命中自动保存的已解记忆，直接复用（0 轮推理）——显著更快
	r2, err := ag.Run(ctx, UserMessage("用 Go 实现 Fibonacci，负数返回 -1"))
	if err != nil {
		t.Fatalf("第二次 Run error: %v", err)
	}
	if !r2.Metrics.MemoryHit {
		t.Fatal("第二次应命中跨任务记忆")
	}
	if r2.Metrics.TotalTurns != 0 {
		t.Errorf("fast-path 应为 0 轮推理, got %d", r2.Metrics.TotalTurns)
	}
	if r2.Content != "慢速推理答案" {
		t.Errorf("应直接复用记忆答案, got %q", r2.Content)
	}

	if got := ag.Stats().MemoryHits; got != 1 {
		t.Errorf("MemoryHits = %d, want 1", got)
	}

	// 第三次（不同任务）：不命中，正常推理
	r3, err := ag.Run(ctx, UserMessage("实现一个 LRU 缓存"))
	if err != nil {
		t.Fatalf("第三次 Run error: %v", err)
	}
	if r3.Metrics.MemoryHit {
		t.Error("无关任务不应命中已解记忆")
	}
}

// waitSolutionMemory 轮询等待已解记忆落库（消除 fire-and-forget
// 异步写入与测试读取之间的竞态——该竞态曾导致本测试 ~1% flaky）。
func waitSolutionMemory(t *testing.T, mem *memory.InMemoryStore, keyword string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		eps, err := mem.Search(context.Background(), keyword, nil)
		if err == nil {
			for _, ep := range eps {
				if ep.Metadata["solved"] == "true" {
					return
				}
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("已解记忆未在 %v 内落库（keyword=%q）", timeout, keyword)
}

// TestMemoryReadback_NoSolvedEpisode 无 solved 标记时不走 fast-path。
func TestMemoryReadback_NoSolvedEpisode(t *testing.T) {
	mem := memory.NewInMemoryStore()
	ctx := context.Background()
	_ = mem.Add(ctx, &memory.Episode{
		ID:        "plain-ep",
		SessionID: "s",
		Role:      "episode",
		Content:   "func Fibonacci(...) 参考实现",
		CreatedAt: time.Now().Format(time.RFC3339),
	})

	ag := newReActAgent(ReActConfig{Name: "mem-nosolved", MaxTurns: 5, Model: &outputGuardMockProvider{content: "正常"}})
	ag.self = &memoryCapableMock{store: mem}

	resp, err := ag.Run(ctx, UserMessage("用 Go 实现 Fibonacci"))
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if resp.Metrics.MemoryHit {
		t.Error("未标记 solved 的记忆不应触发 fast-path")
	}
	if resp.Content != "正常" {
		t.Errorf("应走正常推理, got %q", resp.Content)
	}
	if got := ag.Stats().MemoryHits; got != 0 {
		t.Errorf("MemoryHits = %d, want 0", got)
	}
}
