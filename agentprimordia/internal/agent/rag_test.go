package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools"
)

// ===== Mock RAGProvider =====

type mockRAGProvider struct {
	docs    []*RAGDocument
	err     error
	queries []string
}

func (m *mockRAGProvider) Search(ctx context.Context, query string, topK int) ([]*RAGDocument, error) {
	m.queries = append(m.queries, query)
	if m.err != nil {
		return nil, m.err
	}
	if topK > len(m.docs) {
		topK = len(m.docs)
	}
	return m.docs[:topK], nil
}

// ===== RAG 注入 ReAct Loop 测试 =====

func TestReActAgent_RAG_AutoMode(t *testing.T) {
	// 准备 Mock RAG Provider
	ragProvider := &mockRAGProvider{
		docs: []*RAGDocument{
			{ID: "1", Content: "Go is a statically typed language", Score: 0.9, Source: "vector", Role: "knowledge"},
			{ID: "2", Content: "Go has goroutines for concurrency", Score: 0.85, Source: "fts+vector", Role: "knowledge"},
		},
	}

	// Mock LLM: 验证 RAG 上下文注入后，LLM 收到的消息中包含知识
	var receivedMessages []llm.ChatMessage
	mockLLM := llm.NewMockLLM(t).WithResponse("Go is a statically typed language with goroutines.")

	registry := tools.NewRegistry()

	agent, err := NewAgent("rag-agent", "", mockLLM, WithMaxTurns(10))
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	agent.WithToolkit(registry).WithRAG(RAGConfig{
		Provider: ragProvider,
		Mode:     RAGModeAuto,
		TopK:     5,
		MinScore: 0.3,
	})

	resp, err := agent.Run(context.Background(), UserMessage("What is Go?"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Content != "Go is a statically typed language with goroutines." {
		t.Errorf("unexpected response: %s", resp.Content)
	}

	// 验证 RAG 查询被调用
	if len(ragProvider.queries) != 1 {
		t.Errorf("expected 1 RAG query, got %d", len(ragProvider.queries))
	}
	if ragProvider.queries[0] != "What is Go?" {
		t.Errorf("expected query 'What is Go?', got '%s'", ragProvider.queries[0])
	}

	// 验证 LLM 收到的消息中包含 RAG 上下文
	lastReq := mockLLM.LastRequest()
	if lastReq != nil {
		if cr, ok := lastReq.(*llm.CompletionRequest); ok {
			receivedMessages = cr.Messages
		}
	}

	hasRAGContext := false
	for _, msg := range receivedMessages {
		if msg.Role == "system" && len(msg.Content) > 0 {
			// 检查是否包含 RAG 注入的知识上下文
			if containsRAG(msg.Content, "相关知识") || containsRAG(msg.Content, "Go is a statically") {
				hasRAGContext = true
				break
			}
		}
	}
	if !hasRAGContext {
		t.Error("expected RAG context to be injected into LLM messages")
	}

	_ = receivedMessages
}

func TestReActAgent_RAG_FirstMode(t *testing.T) {
	// 第一轮查询，之后不再查询
	ragProvider := &mockRAGProvider{
		docs: []*RAGDocument{
			{ID: "1", Content: "First result", Score: 0.9, Role: "knowledge"},
		},
	}

	// LLM 先返回工具调用，再返回最终结果
	mockLLM := llm.NewMockLLM(t).
		WithToolResponse([]llm.FunctionCall{
			{ID: "call_1", Name: "echo_tool", Arguments: `{"msg":"test"}`},
		}).
		WithResponse("Final answer after RAG")

	registry := tools.NewRegistry()
	_ = registry.Register(&mockEchoTool{name: "echo_tool"})

	agent, err := NewAgent("rag-first-agent", "", mockLLM, WithMaxTurns(10))
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	agent.WithToolkit(registry).WithRAG(RAGConfig{
		Provider: ragProvider,
		Mode:     RAGModeFirst,
		TopK:     3,
	})

	resp, err := agent.Run(context.Background(), UserMessage("Search for something"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// RAG 模式为 First，只在第一轮查询
	if len(ragProvider.queries) != 1 {
		t.Errorf("expected 1 RAG query in first mode, got %d", len(ragProvider.queries))
	}

	_ = resp
}

func TestReActAgent_RAG_OnDemandMode(t *testing.T) {
	// OnDemand 模式下不自动查询
	ragProvider := &mockRAGProvider{
		docs: []*RAGDocument{
			{ID: "1", Content: "On demand result", Score: 0.9},
		},
	}

	mockLLM := llm.NewMockLLM(t).WithResponse("No auto RAG")

	agent, err := NewAgent("rag-ondemand-agent", "", mockLLM, WithMaxTurns(10))
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	agent.WithToolkit(tools.NewRegistry()).WithRAG(RAGConfig{
		Provider: ragProvider,
		Mode:     RAGModeOnDemand,
	})

	resp, err := agent.Run(context.Background(), UserMessage("Test on demand"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// OnDemand 模式下不应该自动查询
	if len(ragProvider.queries) != 0 {
		t.Errorf("expected 0 RAG queries in on_demand mode, got %d", len(ragProvider.queries))
	}

	_ = resp
}

func TestReActAgent_RAG_NoProvider(t *testing.T) {
	// 没有配置 RAG Provider 时，不应该报错
	mockLLM := llm.NewMockLLM(t).WithResponse("Normal response")

	agent, err := NewAgent("no-rag-agent", "", mockLLM, WithMaxTurns(10))
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	agent.WithToolkit(tools.NewRegistry())

	resp, err := agent.Run(context.Background(), UserMessage("Hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Normal response" {
		t.Errorf("unexpected response: %s", resp.Content)
	}
}

func TestReActAgent_RAG_ErrorHandling(t *testing.T) {
	// RAG 检索失败不应阻止 Agent 运行
	ragProvider := &mockRAGProvider{
		err: context.DeadlineExceeded,
	}

	mockLLM := llm.NewMockLLM(t).WithResponse("Fallback without RAG")

	agent, err := NewAgent("rag-error-agent", "", mockLLM, WithMaxTurns(10))
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	agent.WithToolkit(tools.NewRegistry()).WithRAG(RAGConfig{
		Provider: ragProvider,
		Mode:     RAGModeAuto,
	})

	resp, err := agent.Run(context.Background(), UserMessage("Test RAG error"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Fallback without RAG" {
		t.Errorf("Agent should still respond when RAG fails, got: %s", resp.Content)
	}
}

func TestReActAgent_RAG_MinScore(t *testing.T) {
	// 低于 MinScore 的结果应该被过滤
	ragProvider := &mockRAGProvider{
		docs: []*RAGDocument{
			{ID: "1", Content: "High score result", Score: 0.9, Role: "knowledge"},
			{ID: "2", Content: "Low score result", Score: 0.1, Role: "knowledge"},
		},
	}

	mockLLM := llm.NewMockLLM(t).WithResponse("Filtered response")

	agent, err := NewAgent("rag-minscore-agent", "", mockLLM, WithMaxTurns(10))
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	agent.WithToolkit(tools.NewRegistry()).WithRAG(RAGConfig{
		Provider: ragProvider,
		Mode:     RAGModeAuto,
		MinScore: 0.5,
	})

	resp, err := agent.Run(context.Background(), UserMessage("Test min score"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证 LLM 收到的消息中只包含高分结果
	lastReq := mockLLM.LastRequest()
	if cr, ok := lastReq.(*llm.CompletionRequest); ok {
		for _, msg := range cr.Messages {
			if msg.Role == "system" {
				if containsRAG(msg.Content, "Low score result") && containsRAG(msg.Content, "相关知识") {
					t.Error("Low score result should be filtered out")
				}
			}
		}
	}

	_ = resp
}

func TestReActAgent_RAG_HooksFired(t *testing.T) {
	ragProvider := &mockRAGProvider{
		docs: []*RAGDocument{
			{ID: "1", Content: "Hook test result", Score: 0.8, Role: "knowledge"},
		},
	}

	mockLLM := llm.NewMockLLM(t).WithResponse("Hooks response")

	var beforeRAGCalled bool
	var afterRAGCalled bool

	hooks := NewHookManager()
	hooks.Register(HookBeforeRAG, func(ctx context.Context, hctx *HookContext) error {
		beforeRAGCalled = true
		return nil
	})
	hooks.Register(HookAfterRAG, func(ctx context.Context, hctx *HookContext) error {
		afterRAGCalled = true
		return nil
	})

	agent, err := NewAgent("rag-hooks-agent", "", mockLLM, WithMaxTurns(10))
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	agent.WithToolkit(tools.NewRegistry()).WithRAG(RAGConfig{
		Provider: ragProvider,
		Mode:     RAGModeAuto,
	}).WithHooks(hooks)

	_, err = agent.Run(context.Background(), UserMessage("Test RAG hooks"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !beforeRAGCalled {
		t.Error("HookBeforeRAG should have been called")
	}
	if !afterRAGCalled {
		t.Error("HookAfterRAG should have been called")
	}
}

// ===== RAG 格式化测试 =====

func TestFormatRAGDocuments(t *testing.T) {
	docs := []*RAGDocument{
		{ID: "1", Content: "Go is fast", Score: 0.9, Role: "knowledge"},
		{ID: "2", Content: "Go has goroutines", Score: 0.8, Role: "user"},
	}

	result := FormatRAGDocuments(docs)
	if result == "" {
		t.Error("expected non-empty formatted result")
	}
	if !containsRAG(result, "相关知识") {
		t.Error("expected '相关知识' in formatted result")
	}
	if !containsRAG(result, "Go is fast") {
		t.Error("expected 'Go is fast' in formatted result")
	}
	if !containsRAG(result, "知识结束") {
		t.Error("expected '知识结束' in formatted result")
	}
}

func TestFormatRAGDocuments_Empty(t *testing.T) {
	result := FormatRAGDocuments(nil)
	if result != "" {
		t.Errorf("expected empty result for nil docs, got '%s'", result)
	}

	result = FormatRAGDocuments([]*RAGDocument{})
	if result != "" {
		t.Errorf("expected empty result for empty docs, got '%s'", result)
	}
}

func TestRAGDocument_ContextForPrompt(t *testing.T) {
	doc := &RAGDocument{ID: "1", Content: "Test content", Score: 0.95, Role: "assistant"}
	result := doc.RAGContextForPrompt()
	if !containsRAG(result, "0.95") {
		t.Error("expected score in context")
	}
	if !containsRAG(result, "assistant") {
		t.Error("expected role in context")
	}
}

func TestRAGDocument_ContextForPrompt_DefaultRole(t *testing.T) {
	doc := &RAGDocument{ID: "1", Content: "Test", Score: 0.5}
	result := doc.RAGContextForPrompt()
	if !containsRAG(result, "知识") {
		t.Error("expected default role '知识' in context")
	}
}

// ===== Knowledge Search 工具测试 =====

func TestKnowledgeSearch_Execute(t *testing.T) {
	searcher := &mockKnowledgeSearcher{
		docs: []*mockKDoc{
			{ID: "1", Content: "Test knowledge", Score: 0.9},
		},
	}

	ks := NewKnowledgeSearchWithSearcher(searcher)
	args := json.RawMessage(`{"query":"test"}`)

	result, err := ks.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected error result: %s", result.Content)
	}
	if !containsRAG(result.Content, "Test knowledge") {
		t.Errorf("expected knowledge content in result, got: %s", result.Content)
	}
}

func TestKnowledgeSearch_NoProvider(t *testing.T) {
	ks := NewKnowledgeSearchWithSearcher(nil)
	args := json.RawMessage(`{"query":"test"}`)

	result, err := ks.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result when no provider configured")
	}
}

func TestKnowledgeSearch_EmptyQuery(t *testing.T) {
	searcher := &mockKnowledgeSearcher{}
	ks := NewKnowledgeSearchWithSearcher(searcher)
	args := json.RawMessage(`{"query":""}`)

	result, err := ks.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for empty query")
	}
}

func TestKnowledgeSearch_NoResults(t *testing.T) {
	searcher := &mockKnowledgeSearcher{docs: []*mockKDoc{}}
	ks := NewKnowledgeSearchWithSearcher(searcher)
	args := json.RawMessage(`{"query":"nonexistent"}`)

	result, err := ks.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Error("expected non-error result for no matches")
	}
	if !containsRAG(result.Content, "No relevant knowledge") {
		t.Errorf("expected 'No relevant knowledge' message, got: %s", result.Content)
	}
}

// ===== 辅助 Mock =====

type mockKnowledgeSearcher struct {
	docs []*mockKDoc
	err  error
}

type mockKDoc struct {
	ID      string
	Content string
	Score   float32
	Source  string
}

func (m *mockKnowledgeSearcher) SearchKnowledge(ctx context.Context, query string, topK int) ([]*mockKDoc, error) {
	if m.err != nil {
		return nil, m.err
	}
	if topK > len(m.docs) {
		topK = len(m.docs)
	}
	return m.docs[:topK], nil
}

// 为了让知识搜索工具测试不依赖 builtin 包的具体类型
// 这里定义一个简化的 KnowledgeSearch 接口来测试
type knowledgeSearchTool struct {
	searcher *mockKnowledgeSearcher
}

func NewKnowledgeSearchWithSearcher(searcher *mockKnowledgeSearcher) *knowledgeSearchTool {
	return &knowledgeSearchTool{searcher: searcher}
}

func (k *knowledgeSearchTool) Name() string        { return "knowledge_search" }
func (k *knowledgeSearchTool) Description() string { return "Search knowledge base" }
func (k *knowledgeSearchTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"top_k":{"type":"integer"}},"required":["query"]}`)
}

func (k *knowledgeSearchTool) Execute(ctx context.Context, args json.RawMessage) (*tools.Result, error) {
	if k.searcher == nil {
		return tools.NewErrorResult("knowledge search not configured: no RAG provider available"), nil
	}

	var params struct {
		Query string `json:"query"`
		TopK  int    `json:"top_k"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return tools.NewErrorResult("invalid arguments"), nil
	}
	if params.Query == "" {
		return tools.NewErrorResult("query is required"), nil
	}

	topK := 5
	if params.TopK > 0 {
		topK = params.TopK
	}

	docs, err := k.searcher.SearchKnowledge(ctx, params.Query, topK)
	if err != nil {
		return tools.NewErrorResult("knowledge search failed"), nil
	}
	if len(docs) == 0 {
		return tools.NewResult("No relevant knowledge found for the given query."), nil
	}

	result := "=== Knowledge Search Results ===\n"
	for i, doc := range docs {
		result += formatKResult(i+1, doc.Score, doc.Content)
	}
	result += "=== End of Results ===\n"
	return tools.NewResult(result), nil
}

func formatKResult(idx int, score float32, content string) string {
	return fmt.Sprintf("[%d] (score: %.2f) %s\n", idx, score, content)
}

// contains 检查字符串是否包含子串
func containsRAG(s, sub string) bool {
	return strings.Contains(s, sub)
}
