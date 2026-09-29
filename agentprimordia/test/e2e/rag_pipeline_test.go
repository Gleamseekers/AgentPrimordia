package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/memory"
)

// TestE2E_RAG_DocumentToChunk 验证文档加载→切分→存储的完整流程
func TestE2E_RAG_DocumentToChunk(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 创建内存记忆存储
	mem := memory.NewInMemoryStore()

	// 添加多个 episode 模拟文档片段
	episodes := []*memory.Episode{
		{ID: "ep1", SessionID: "rag-test", Role: "user", Content: "Go is a statically typed programming language.", CreatedAt: time.Now().UTC().Format(time.RFC3339)},
		{ID: "ep2", SessionID: "rag-test", Role: "assistant", Content: "Yes, Go was designed at Google by Robert Griesemer, Rob Pike, and Ken Thompson.", CreatedAt: time.Now().UTC().Format(time.RFC3339)},
		{ID: "ep3", SessionID: "rag-test", Role: "user", Content: "What makes Go special for concurrent programming?", CreatedAt: time.Now().UTC().Format(time.RFC3339)},
		{ID: "ep4", SessionID: "rag-test", Role: "assistant", Content: "Go has goroutines and channels built into the language, making concurrent programming elegant and efficient.", CreatedAt: time.Now().UTC().Format(time.RFC3339)},
	}

	for _, ep := range episodes {
		if err := mem.Add(ctx, ep); err != nil {
			t.Fatalf("mem.Add(%s) error: %v", ep.ID, err)
		}
	}

	// 验证所有 episode 已存储
	count, err := mem.Count(ctx, "rag-test")
	if err != nil {
		t.Fatalf("mem.Count error: %v", err)
	}
	if count != 4 {
		t.Errorf("count = %d, want 4", count)
	}

	// 搜索相关内容
	results, err := mem.Search(ctx, "concurrent programming goroutines", &memory.SearchOptions{Limit: 3})
	if err != nil {
		t.Fatalf("mem.Search error: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected search results for 'concurrent programming'")
	}
}

// TestE2E_RAG_TextSplitter 验证文本切分器
func TestE2E_RAG_TextSplitter(t *testing.T) {
	splitter := memory.NewCharacterSplitter(50, 10)

	// 使用足够长的文本确保切分
	longText := strings.Repeat("AgentPrimordia is a Go framework. ", 20)

	chunks := splitter.Split(context.Background(), longText)
	if len(chunks) == 0 {
		t.Fatal("expected non-empty chunks from splitter")
	}
	t.Logf("split %d chars into %d chunks", len(longText), len(chunks))
}

// TestE2E_RAG_RecursiveSplitter 验证递归切分器
func TestE2E_RAG_RecursiveSplitter(t *testing.T) {
	splitter := memory.NewRecursiveSplitter(100, 20)

	text := "First paragraph about Go.\n\nSecond paragraph about Rust.\n\nThird paragraph about Python."
	chunks := splitter.Split(context.Background(), text)
	if len(chunks) == 0 {
		t.Fatal("expected non-empty chunks")
	}
}

// TestE2E_Memory_SessionIsolation 验证会话隔离
func TestE2E_Memory_SessionIsolation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mem := memory.NewInMemoryStore()

	// 两个不同 session 的数据
	_ = mem.Add(ctx, &memory.Episode{ID: "s1-ep1", SessionID: "session-1", Role: "user", Content: "session 1 data", CreatedAt: time.Now().UTC().Format(time.RFC3339)})
	_ = mem.Add(ctx, &memory.Episode{ID: "s2-ep1", SessionID: "session-2", Role: "user", Content: "session 2 data", CreatedAt: time.Now().UTC().Format(time.RFC3339)})

	// 按 session 计数
	c1, _ := mem.Count(ctx, "session-1")
	c2, _ := mem.Count(ctx, "session-2")

	if c1 != 1 {
		t.Errorf("session-1 count = %d, want 1", c1)
	}
	if c2 != 1 {
		t.Errorf("session-2 count = %d, want 1", c2)
	}
}

// TestE2E_Memory_CRUD 验证记忆 CRUD 完整流程
func TestE2E_Memory_CRUD(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mem := memory.NewInMemoryStore()

	// Create
	ep := &memory.Episode{
		ID:        "crud-test-1",
		SessionID: "crud-session",
		Role:      "user",
		Content:   "original content",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := mem.Add(ctx, ep); err != nil {
		t.Fatalf("Add error: %v", err)
	}

	// Read
	got, err := mem.Get(ctx, "crud-test-1")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if got.Content != "original content" {
		t.Errorf("Get content = %q, want %q", got.Content, "original content")
	}

	// Delete
	if err := mem.Delete(ctx, "crud-test-1"); err != nil {
		t.Fatalf("Delete error: %v", err)
	}

	// Verify deleted
	_, err = mem.Get(ctx, "crud-test-1")
	if err == nil {
		t.Error("expected error after delete")
	}
}
