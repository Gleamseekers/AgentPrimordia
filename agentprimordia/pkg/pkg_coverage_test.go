package ap

import (
	"context"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/memory"
)

func TestAdaptMemoryStore(t *testing.T) {
	store, err := WithInMemory()
	if err != nil {
		t.Fatalf("WithInMemory failed: %v", err)
	}
	defer store.Close()

	adapter := store
	ctx := context.Background()

	ep := &memory.Episode{
		ID:        "ep-test-1",
		SessionID: "session-1",
		Role:      "user",
		Content:   "Hello, world!",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	if err := adapter.Add(ctx, ep); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	count, err := adapter.Count(ctx, "session-1")
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}
	if count != 1 {
		t.Errorf("Count() = %d, want %d", count, 1)
	}

	got, err := adapter.Get(ctx, ep.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Content != ep.Content {
		t.Errorf("Get() Content = %q, want %q", got.Content, ep.Content)
	}

	list, err := adapter.List(ctx, &memory.ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 1 {
		t.Errorf("List() length = %d, want %d", len(list), 1)
	}

	search, err := adapter.Search(ctx, "world", &memory.SearchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(search) != 1 {
		t.Errorf("Search() length = %d, want %d", len(search), 1)
	}

	if err := adapter.UpdateSummary(ctx, ep.ID, "Summary", "tag1,tag2"); err != nil {
		t.Fatalf("UpdateSummary() error = %v", err)
	}

	if err := adapter.SetImportance(ctx, ep.ID, 0.8); err != nil {
		t.Fatalf("SetImportance() error = %v", err)
	}

	if err := adapter.Delete(ctx, ep.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	_, err = adapter.Get(ctx, ep.ID)
	if err == nil {
		t.Error("expected error after Delete()")
	}
}

func TestAdaptMemoryStore_Operations(t *testing.T) {
	store, err := WithInMemory()
	if err != nil {
		t.Fatalf("WithInMemory failed: %v", err)
	}
	defer store.Close()

	adapter := store
	ctx := context.Background()

	if err := adapter.RecordToolUse(ctx, "session-1", "agent-1", "shell", "ls", "result"); err != nil {
		t.Fatalf("RecordToolUse() error = %v", err)
	}

	count, _ := adapter.Count(ctx, "session-1")
	if count != 1 {
		t.Errorf("after RecordToolUse, Count() = %d, want %d", count, 1)
	}

	exported, err := adapter.ExportMemories(ctx, "session-1", "json")
	if err != nil {
		t.Fatalf("ExportMemories() error = %v", err)
	}
	if len(exported) == 0 {
		t.Error("ExportMemories() returned empty data")
	}

	exportedMD, err := adapter.ExportMemories(ctx, "session-1", "markdown")
	if err != nil {
		t.Fatalf("ExportMemories(markdown) error = %v", err)
	}
	if len(exportedMD) == 0 {
		t.Error("ExportMemories(markdown) returned empty data")
	}

	// 先清空再导入，避免 ID 冲突
	if err := adapter.ClearAll(ctx, "session-1"); err != nil {
		t.Fatalf("ClearAll() error = %v", err)
	}

	if err := adapter.ClearAll(ctx, "session-1"); err != nil {
		t.Fatalf("ClearAll() error = %v", err)
	}

	imported, err := adapter.ImportMemories(ctx, exported, "json")
	if err != nil {
		t.Fatalf("ImportMemories() error = %v", err)
	}
	if imported != 1 {
		t.Errorf("ImportMemories() = %d, want %d", imported, 1)
	}

	count, _ = adapter.Count(ctx, "session-1")
	if count != 1 {
		t.Errorf("after ImportMemories, Count() = %d, want %d", count, 1)
	}

	if err := adapter.ClearAll(ctx, "session-1"); err != nil {
		t.Fatalf("ClearAll() error = %v", err)
	}

	count, _ = adapter.Count(ctx, "session-1")
	if count != 0 {
		t.Errorf("after ClearAll, Count() = %d, want %d", count, 0)
	}
}

func TestAdaptMemoryStore_Timeline(t *testing.T) {
	store, err := WithInMemory()
	if err != nil {
		t.Fatalf("WithInMemory failed: %v", err)
	}
	defer store.Close()

	adapter := store
	ctx := context.Background()

	ep1 := &memory.Episode{
		ID:         "ep-1",
		SessionID:  "session-1",
		Role:       "user",
		Content:    "Go programming",
		Topics:     "go,programming",
		Importance: 0.9,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	ep2 := &memory.Episode{
		ID:         "ep-2",
		SessionID:  "session-1",
		Role:       "assistant",
		Content:    "Python programming",
		Topics:     "python,programming",
		Importance: 0.5,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
	}

	_ = adapter.Add(ctx, ep1)
	_ = adapter.Add(ctx, ep2)

	byTag, err := adapter.GetMemoriesByTag(ctx, "go", 10)
	if err != nil {
		t.Fatalf("GetMemoriesByTag() error = %v", err)
	}
	if len(byTag) != 1 {
		t.Errorf("GetMemoriesByTag() length = %d, want %d", len(byTag), 1)
	}

	important, err := adapter.GetImportantMemories(ctx, 0.6, 10)
	if err != nil {
		t.Fatalf("GetImportantMemories() error = %v", err)
	}
	if len(important) != 1 {
		t.Errorf("GetImportantMemories() length = %d, want %d", len(important), 1)
	}
	if important[0].Importance != 0.9 {
		t.Errorf("GetImportantMemories()[0].Importance = %f, want %f", important[0].Importance, 0.9)
	}

	timeline, err := adapter.GetMemoryTimeline(ctx, 7)
	if err != nil {
		t.Fatalf("GetMemoryTimeline() error = %v", err)
	}
	if len(timeline) == 0 {
		t.Error("GetMemoryTimeline() returned empty timeline")
	}

	bySession, err := adapter.GetMemoriesBySession(ctx, "session-1")
	if err != nil {
		t.Fatalf("GetMemoriesBySession() error = %v", err)
	}
	if len(bySession) != 2 {
		t.Errorf("GetMemoriesBySession() length = %d, want %d", len(bySession), 2)
	}

	stats, err := adapter.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats() error = %v", err)
	}
	if stats.TotalEpisodes != 2 {
		t.Errorf("Stats().TotalEpisodes = %d, want %d", stats.TotalEpisodes, 2)
	}
}

func TestAdaptMemoryStore_SearchByTagAndGetImportant(t *testing.T) {
	store, err := WithInMemory()
	if err != nil {
		t.Fatalf("WithInMemory failed: %v", err)
	}
	defer store.Close()

	adapter := store
	ctx := context.Background()

	ep := &memory.Episode{
		ID:         "ep-1",
		SessionID:  "session-1",
		Role:       "user",
		Content:    "Test content",
		Topics:     "test,unit",
		Importance: 0.95,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	_ = adapter.Add(ctx, ep)

	byTag, err := adapter.SearchByTag(ctx, "test", &memory.SearchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("SearchByTag() error = %v", err)
	}
	if len(byTag) != 1 {
		t.Errorf("SearchByTag() length = %d, want %d", len(byTag), 1)
	}

	important, err := adapter.GetImportant(ctx, 0.9, 10)
	if err != nil {
		t.Fatalf("GetImportant() error = %v", err)
	}
	if len(important) != 1 {
		t.Errorf("GetImportant() length = %d, want %d", len(important), 1)
	}

	timeline, err := adapter.GetTimeline(ctx, 7)
	if err != nil {
		t.Fatalf("GetTimeline() error = %v", err)
	}
	if len(timeline) == 0 {
		t.Error("GetTimeline() returned empty timeline")
	}
}

func TestAdaptMemoryStore_CleanupExpired(t *testing.T) {
	store, err := WithInMemory()
	if err != nil {
		t.Fatalf("WithInMemory failed: %v", err)
	}
	defer store.Close()

	adapter := store
	ctx := context.Background()

	ep := &memory.Episode{
		ID:        "ep-1",
		SessionID: "session-1",
		Role:      "user",
		Content:   "Test",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	_ = adapter.Add(ctx, ep)

	deleted, err := adapter.CleanupExpired(ctx, 30)
	if err != nil {
		t.Fatalf("CleanupExpired() error = %v", err)
	}
	if deleted != 0 {
		t.Errorf("CleanupExpired() = %d, want %d", deleted, 0)
	}
}

func TestAdaptMemoryStore_Close(t *testing.T) {
	store, err := WithInMemory()
	if err != nil {
		t.Fatalf("WithInMemory failed: %v", err)
	}

	adapter := store
	if err := adapter.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestNewEmbeddingAdapter(t *testing.T) {
	t.Parallel()
	mockLLM := &integrationMockLLM{response: "test"}

	adapter := NewEmbeddingAdapter(mockLLM, 768)
	if adapter.Dimensions() != 768 {
		t.Errorf("Dimensions() = %d, want %d", adapter.Dimensions(), 768)
	}

	ctx := context.Background()
	vecs, err := adapter.Embed(ctx, []string{"hello", "world"})
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if len(vecs) != 2 {
		t.Errorf("Embed() returned %d vectors, want %d", len(vecs), 2)
	}
}

func TestNewEmbeddingAdapter_DefaultDim(t *testing.T) {
	t.Parallel()
	mockLLM := &integrationMockLLM{response: "test"}
	adapter := NewEmbeddingAdapter(mockLLM, 0)
	if adapter.Dimensions() != 1536 {
		t.Errorf("Dimensions() = %d, want %d", adapter.Dimensions(), 1536)
	}
}

func TestNewRAGStore(t *testing.T) {
	memStore, err := WithInMemory()
	if err != nil {
		t.Fatalf("WithInMemory failed: %v", err)
	}
	defer memStore.Close()

	mockLLM := &integrationMockLLM{response: "test"}
	embedder := NewEmbeddingAdapter(mockLLM, 128)

	rag := NewRAGStore(memStore, embedder)
	if rag == nil {
		t.Fatal("NewRAGStore() returned nil")
	}
}

func TestNewRAGProviderAdapter(t *testing.T) {
	memStore, err := WithInMemory()
	if err != nil {
		t.Fatalf("WithInMemory failed: %v", err)
	}
	defer memStore.Close()

	mockLLM := &integrationMockLLM{response: "test"}
	embedder := NewEmbeddingAdapter(mockLLM, 128)
	rag := NewRAGStore(memStore, embedder)

	provider := NewRAGProviderAdapter(rag)
	ctx := context.Background()

	docs, err := provider.Search(ctx, "test", 5)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(docs) != 0 {
		t.Errorf("Search() returned %d docs, want 0 for empty store", len(docs))
	}
}

func TestNewKnowledgeSearcherAdapter(t *testing.T) {
	memStore, err := WithInMemory()
	if err != nil {
		t.Fatalf("WithInMemory failed: %v", err)
	}
	defer memStore.Close()

	mockLLM := &integrationMockLLM{response: "test"}
	embedder := NewEmbeddingAdapter(mockLLM, 128)
	rag := NewRAGStore(memStore, embedder)

	searcher := NewKnowledgeSearcherAdapter(rag)
	ctx := context.Background()

	docs, err := searcher.SearchKnowledge(ctx, "test", 5)
	if err != nil {
		t.Fatalf("SearchKnowledge() error = %v", err)
	}
	if len(docs) != 0 {
		t.Errorf("SearchKnowledge() returned %d docs, want 0 for empty store", len(docs))
	}
}

type integrationMockLLM struct {
	response string
}

func (m *integrationMockLLM) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return &llm.CompletionResponse{
		ID:      "mock-integration-id",
		Content: m.response,
		Role:    "assistant",
		Usage:   llm.Usage{PromptTokens: 10, CompletionTokens: 20},
	}, nil
}

func (m *integrationMockLLM) Stream(ctx context.Context, req *llm.CompletionRequest) (<-chan llm.Chunk, error) {
	ch := make(chan llm.Chunk, 1)
	go func() {
		defer close(ch)
		ch <- llm.Chunk{Content: m.response, Done: true}
	}()
	return ch, nil
}

func (m *integrationMockLLM) CallTools(ctx context.Context, req *llm.ToolCallRequest) (*llm.ToolCallResponse, error) {
	return &llm.ToolCallResponse{
		Content:   m.response,
		ToolCalls: []llm.FunctionCall{},
		Usage:     llm.Usage{},
	}, nil
}

func (m *integrationMockLLM) Embeddings(ctx context.Context, texts []string) ([][]float32, error) {
	return make([][]float32, len(texts)), nil
}

func (m *integrationMockLLM) Info() llm.ModelInfo {
	return llm.ModelInfo{
		Name:              "integration-mock",
		Provider:          "mock",
		MaxContext:        4096,
		SupportsTools:     true,
		SupportsStreaming: true,
	}
}

// ===== options.go 覆盖率测试 =====

func TestWithTimeout(t *testing.T) {
	t.Parallel()
	opt := WithTimeout(5 * time.Second)
	o := ApplyOptions(opt)
	if o.timeout != 5*time.Second {
		t.Errorf("timeout = %v, want %v", o.timeout, 5*time.Second)
	}
}

func TestWithMaxIterations(t *testing.T) {
	t.Parallel()
	opt := WithMaxIterations(10)
	o := ApplyOptions(opt)
	if o.maxTurns != 10 {
		t.Errorf("maxTurns = %d, want 10", o.maxTurns)
	}
}

func TestWithTemp(t *testing.T) {
	t.Parallel()
	opt := WithTemp(0.7)
	o := ApplyOptions(opt)
	if o.temperature != 0.7 {
		t.Errorf("temperature = %f, want 0.7", o.temperature)
	}
}

func TestWithCheckpoint(t *testing.T) {
	t.Parallel()
	opt := WithCheckpoint("/tmp/checkpoint")
	o := ApplyOptions(opt)
	if o.checkpointDir != "/tmp/checkpoint" {
		t.Errorf("checkpointDir = %q, want /tmp/checkpoint", o.checkpointDir)
	}
}

func TestWithStreaming(t *testing.T) {
	t.Parallel()
	called := false
	fn := func(chunk string) { called = true }
	opt := WithStreaming(fn)
	o := ApplyOptions(opt)
	if o.streamingFn == nil {
		t.Fatal("streamingFn should not be nil")
	}
	o.streamingFn("test")
	if !called {
		t.Error("streamingFn should have been called")
	}
}

func TestWithMetadata(t *testing.T) {
	t.Parallel()
	md := Metadata{SessionID: "sess-1", Extra: map[string]string{"key": "value"}}
	opt := WithMetadata(md)
	o := ApplyOptions(opt)
	if o.metadata.SessionID != "sess-1" {
		t.Errorf("metadata.SessionID = %q, want sess-1", o.metadata.SessionID)
	}
}

func TestApplyOptions(t *testing.T) {
	t.Parallel()
	o := ApplyOptions(
		WithTimeout(3*time.Second),
		WithMaxIterations(5),
		WithTemp(0.5),
	)
	if o.timeout != 3*time.Second {
		t.Errorf("timeout = %v, want 3s", o.timeout)
	}
	if o.maxTurns != 5 {
		t.Errorf("maxTurns = %d, want 5", o.maxTurns)
	}
	if o.temperature != 0.5 {
		t.Errorf("temperature = %f, want 0.5", o.temperature)
	}
}

func TestApplyOptions_Empty(t *testing.T) {
	t.Parallel()
	o := ApplyOptions()
	if o.timeout != 0 || o.maxTurns != 0 || o.temperature != 0 {
		t.Error("empty ApplyOptions should yield zero-value options")
	}
}
