package builtin

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/tools"
)

// mockSearcher 模拟知识库搜索
type mockSearcher struct {
	docs []*KnowledgeDoc
	err  error
}

func (m *mockSearcher) SearchKnowledge(ctx context.Context, query string, topK int) ([]*KnowledgeDoc, error) {
	if m.err != nil {
		return nil, m.err
	}
	if topK > len(m.docs) {
		topK = len(m.docs)
	}
	return m.docs[:topK], nil
}

func TestKnowledge_Search_Basic(t *testing.T) {
	searcher := &mockSearcher{
		docs: []*KnowledgeDoc{
			{ID: "1", Content: "Go is a statically typed language", Score: 0.95, Source: "go-docs"},
			{ID: "2", Content: "Go supports concurrency with goroutines", Score: 0.85, Source: "go-docs"},
		},
	}

	ks := NewKnowledgeSearch(searcher)
	args, _ := json.Marshal(map[string]any{
		"query": "Go language",
	})

	result, err := ks.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("should not be error: %s", result.Content)
	}
	if !contains(result.Content, "Go is a statically typed language") {
		t.Errorf("should contain first doc content, got: %s", result.Content)
	}
	if !contains(result.Content, "0.95") {
		t.Errorf("should contain score, got: %s", result.Content)
	}
	if !contains(result.Content, "Knowledge Search Results") {
		t.Errorf("should contain header, got: %s", result.Content)
	}
}

func TestKnowledge_Search_NoResults(t *testing.T) {
	searcher := &mockSearcher{
		docs: []*KnowledgeDoc{},
	}

	ks := NewKnowledgeSearch(searcher)
	args, _ := json.Marshal(map[string]any{
		"query": "nonexistent topic",
	})

	result, err := ks.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("should not be error for no results: %s", result.Content)
	}
	if !contains(result.Content, "No relevant knowledge found") {
		t.Errorf("should report no results, got: %s", result.Content)
	}
}

func TestKnowledge_Search_EmptyQuery(t *testing.T) {
	searcher := &mockSearcher{}
	ks := NewKnowledgeSearch(searcher)

	args, _ := json.Marshal(map[string]any{
		"query": "",
	})

	result, err := ks.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("empty query should return error")
	}
	if !contains(result.Content, "query is required") {
		t.Errorf("should mention query required, got: %s", result.Content)
	}
}

func TestKnowledge_Search_InvalidArgs(t *testing.T) {
	searcher := &mockSearcher{}
	ks := NewKnowledgeSearch(searcher)

	result, err := ks.Execute(context.Background(), json.RawMessage(`not json`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("invalid args should return error")
	}
	if !contains(result.Content, "invalid arguments") {
		t.Errorf("should mention invalid arguments, got: %s", result.Content)
	}
}

func TestKnowledge_Search_NilSearcher(t *testing.T) {
	ks := NewKnowledgeSearch(nil)

	args, _ := json.Marshal(map[string]any{
		"query": "test",
	})

	result, err := ks.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("nil searcher should return error")
	}
	if !contains(result.Content, "not configured") {
		t.Errorf("should mention not configured, got: %s", result.Content)
	}
}

func TestKnowledge_Search_SearcherError(t *testing.T) {
	searcher := &mockSearcher{
		err: context.DeadlineExceeded,
	}

	ks := NewKnowledgeSearch(searcher)
	args, _ := json.Marshal(map[string]any{
		"query": "test",
	})

	result, err := ks.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("searcher error should return error result")
	}
	if !contains(result.Content, "knowledge search failed") {
		t.Errorf("should mention search failed, got: %s", result.Content)
	}
}

func TestKnowledge_Search_CustomTopK(t *testing.T) {
	docs := make([]*KnowledgeDoc, 10)
	for i := range docs {
		docs[i] = &KnowledgeDoc{
			ID:      string(rune('0' + i)),
			Content: "result",
			Score:   float32(10-i) / 10.0,
		}
	}

	searcher := &mockSearcher{docs: docs}
	ks := NewKnowledgeSearch(searcher)

	args, _ := json.Marshal(map[string]any{
		"query": "test",
		"top_k": float64(3),
	})

	result, err := ks.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("should not be error: %s", result.Content)
	}
	if !contains(result.Content, "[1]") {
		t.Errorf("should contain numbered results, got: %s", result.Content)
	}
}

func TestKnowledge_Search_DefaultTopK(t *testing.T) {
	docs := make([]*KnowledgeDoc, 10)
	for i := range docs {
		docs[i] = &KnowledgeDoc{
			ID:      string(rune('0' + i)),
			Content: "result",
			Score:   0.5,
		}
	}

	searcher := &mockSearcher{docs: docs}
	ks := NewKnowledgeSearch(searcher).WithTopK(2)

	args, _ := json.Marshal(map[string]any{
		"query": "test",
	})

	result, err := ks.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("should not be error: %s", result.Content)
	}
	// WithTopK 设置默认 topK=2，应只返回 2 条
	if !contains(result.Content, "[1]") || !contains(result.Content, "[2]") {
		t.Errorf("should contain 2 results, got: %s", result.Content)
	}
}

func TestKnowledge_Name(t *testing.T) {
	ks := NewKnowledgeSearch(nil)
	if ks.Name() != "knowledge_search" {
		t.Errorf("expected 'knowledge_search', got '%s'", ks.Name())
	}
}

func TestKnowledge_Description(t *testing.T) {
	ks := NewKnowledgeSearch(nil)
	desc := ks.Description()
	if desc == "" {
		t.Error("description should not be empty")
	}
}

func TestKnowledge_Parameters(t *testing.T) {
	ks := NewKnowledgeSearch(nil)
	params := ks.Parameters()
	if params == nil {
		t.Fatal("parameters should not be nil")
	}
	var schema map[string]any
	if err := json.Unmarshal(params, &schema); err != nil {
		t.Fatalf("invalid JSON schema: %v", err)
	}
	if schema["type"] != "object" {
		t.Errorf("expected type object, got %v", schema["type"])
	}
}

func TestKnowledge_Search_WithTopK(t *testing.T) {
	ks := NewKnowledgeSearch(nil).WithTopK(10)
	if ks.topK != 10 {
		t.Errorf("expected topK 10, got %d", ks.topK)
	}
}

func TestKnowledge_Search_Registry(t *testing.T) {
	searcher := &mockSearcher{
		docs: []*KnowledgeDoc{
			{ID: "1", Content: "test doc", Score: 0.9},
		},
	}

	ks := NewKnowledgeSearch(searcher)

	reg := tools.NewRegistry()
	if err := reg.Register(ks); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tool, exists := reg.Get("knowledge_search")
	if !exists {
		t.Fatal("knowledge_search should be registered")
	}
	if tool.Name() != "knowledge_search" {
		t.Errorf("expected 'knowledge_search', got '%s'", tool.Name())
	}
}
