package agent

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/internal/memory"
)

// summaryTrackingStore 是带摘要存储追踪的 MemoryStore，
// 用于验证 M2 修复：异步摘要提取后结果被 UpdateSummary 存储，而非只 log 丢弃。
type summaryTrackingStore struct {
	mu       sync.Mutex
	episodes []*memory.Episode
	updates  []summaryUpdate
}

type summaryUpdate struct {
	id      string
	summary string
	topics  string
}

func (s *summaryTrackingStore) Add(_ context.Context, episode *memory.Episode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.episodes = append(s.episodes, episode)
	return nil
}

func (s *summaryTrackingStore) UpdateSummary(_ context.Context, id, summary, topics string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updates = append(s.updates, summaryUpdate{id: id, summary: summary, topics: topics})
	return nil
}

// getUpdates 返回已存储的摘要更新快照
func (s *summaryTrackingStore) getUpdates() []summaryUpdate {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]summaryUpdate, len(s.updates))
	copy(out, s.updates)
	return out
}

// TestReActAgent_AsyncSummaryStored 验证 M2：异步摘要提取成功后，
// 结果通过 UpdateSummary 存储到 MemoryStore，而非只记录日志丢弃。
func TestReActAgent_AsyncSummaryStored(t *testing.T) {
	store := &summaryTrackingStore{}

	// 用一个返回固定摘要的 Summarizer（通过 SummaryExtractor 接口）
	mockLLM := llm.NewMockLLM(t).WithResponse("done")

	agent, err := NewAgent("summary-store-agent", "", mockLLM, WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}

	// 注入 Summarizer：通过 CapabilityAgent 的 WithSummarizer
	cap := agent.WithMemory(store)
	cap.WithSummarizer(&stubSummarizer{summary: "这是摘要", topics: "topic1,topic2"})

	_, err = agent.Run(context.Background(), UserMessage("some content to summarize"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 异步摘要是 goroutine，等一下让它完成
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(store.getUpdates()) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	updates := store.getUpdates()
	if len(updates) == 0 {
		t.Fatal("M2: UpdateSummary 应被调用存储摘要，但未被调用（摘要可能只被 log 丢弃）")
	}
	u := updates[0]
	if u.summary != "这是摘要" {
		t.Errorf("期望 summary='这是摘要'，实际 %q", u.summary)
	}
	if u.topics != "topic1,topic2" {
		t.Errorf("期望 topics='topic1,topic2'，实际 %q", u.topics)
	}
	if !strings.HasPrefix(u.id, "msg_") {
		t.Errorf("期望 id 以 'msg_' 开头，实际 %q", u.id)
	}
}

// stubSummarizer 固定返回预设摘要，实现 memory.SummaryExtractor 接口
type stubSummarizer struct {
	summary string
	topics  string
	err     error
}

func (s *stubSummarizer) ExtractSummary(_ context.Context, content string) (*memory.SummaryResult, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &memory.SummaryResult{Summary: s.summary, Topics: s.topics}, nil
}
