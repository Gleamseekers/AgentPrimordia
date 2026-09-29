// Stability: 混合 —
//
//	适配器主接口（AgentAdapter / LLMAdapter / MemoryAdapter / ToolAdapter）: Stable。
//	适配器实现（OpenAI / Anthropic / Gemini 等）: Stable。
//	高阶组合（MultiAgentAdapter / PipelineAdapter）: Experimental。
package ap

import (
	"context"
	"fmt"
	"strings"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent"
	"github.com/Gleamseekers/AgentPrimordia/internal/events"
	"github.com/Gleamseekers/AgentPrimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/internal/memory"
	"github.com/Gleamseekers/AgentPrimordia/internal/tools/builtin"
)

// ===== EventPublisher 适配器 =====

// eventBusAdapter 将 events.Bus 适配为 agent.EventPublisher
type eventBusAdapter struct {
	bus *events.Bus
}

// NewEventBusAdapter 将 events.Bus 适配为 agent.EventPublisher，用于注入到 Agent 的事件发布器
func NewEventBusAdapter(bus *events.Bus) agent.EventPublisher {
	return &eventBusAdapter{bus: bus}
}

func (a *eventBusAdapter) PublishAsync(eventType string, source string, payload any) error {
	evt := events.Event{
		Type:    events.EventType(eventType),
		Source:  source,
		Payload: payload,
	}
	return a.bus.PublishAsync(evt)
}

// ===== EmbeddingProvider 适配器 =====

// embeddingAdapter 将 llm.Provider 适配为 memory.EmbeddingProvider
type embeddingAdapter struct {
	provider llm.Provider
	dim      int
}

const defaultEmbeddingDimensions = 1536 // OpenAI text-embedding-3-small 默认维度

// NewEmbeddingAdapter 将 llm.Provider 适配为 memory.EmbeddingProvider，dimensions 指定向量维度（默认 1536）
func NewEmbeddingAdapter(provider llm.Provider, dimensions int) memory.EmbeddingProvider {
	if dimensions <= 0 {
		dimensions = defaultEmbeddingDimensions
	}
	return &embeddingAdapter{provider: provider, dim: dimensions}
}

func (a *embeddingAdapter) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if embedder, ok := a.provider.(llm.Embedder); ok {
		return embedder.Embeddings(ctx, texts)
	}
	return nil, fmt.Errorf("embeddings not supported by provider %T", a.provider)
}

func (a *embeddingAdapter) Dimensions() int {
	return a.dim
}

// ===== RAGStore 工厂 =====

// NewRAGStore 创建 RAG 存储实例，集成 Memory + Embedding + Vector，提供混合检索能力
func NewRAGStore(memStore memory.Memory, embedder memory.EmbeddingProvider) *memory.RAGStore {
	return memory.NewRAGStore(memStore, embedder)
}

// ===== RAGProvider 适配器 =====

// ragProviderAdapter 将 memory.RAGStore 适配为 agent.RAGProvider
type ragProviderAdapter struct {
	store *memory.RAGStore
}

// NewRAGProviderAdapter 将 memory.RAGStore 适配为 agent.RAGProvider，用于注入到 Agent 的 RAG 配置
func NewRAGProviderAdapter(store *memory.RAGStore) agent.RAGProvider {
	return &ragProviderAdapter{store: store}
}

func (a *ragProviderAdapter) Search(ctx context.Context, query string, topK int) ([]*agent.RAGDocument, error) {
	results, err := a.store.HybridSearch(ctx, query, topK)
	if err != nil {
		return nil, err
	}

	docs := make([]*agent.RAGDocument, 0, len(results))
	for _, r := range results {
		docs = append(docs, &agent.RAGDocument{
			ID:      r.Episode.ID,
			Content: r.Episode.Content,
			Score:   r.Score,
			Source:  formatSources(r.Sources),
			Role:    r.Episode.Role,
		})
	}
	return docs, nil
}

// ===== KnowledgeSearcher 适配器 =====

// knowledgeSearcherAdapter 将 memory.RAGStore 适配为 builtin.KnowledgeSearcher
type knowledgeSearcherAdapter struct {
	store *memory.RAGStore
}

// NewKnowledgeSearcherAdapter 将 memory.RAGStore 适配为 builtin.KnowledgeSearcher，用于注入到 ToolkitConfig
func NewKnowledgeSearcherAdapter(store *memory.RAGStore) builtin.KnowledgeSearcher {
	return &knowledgeSearcherAdapter{store: store}
}

func (a *knowledgeSearcherAdapter) SearchKnowledge(ctx context.Context, query string, topK int) ([]*builtin.KnowledgeDoc, error) {
	results, err := a.store.HybridSearch(ctx, query, topK)
	if err != nil {
		return nil, err
	}

	docs := make([]*builtin.KnowledgeDoc, 0, len(results))
	for _, r := range results {
		docs = append(docs, &builtin.KnowledgeDoc{
			ID:      r.Episode.ID,
			Content: r.Episode.Content,
			Score:   r.Score,
			Source:  formatSources(r.Sources),
		})
	}
	return docs, nil
}

// formatSources 将来源切片格式化为字符串
func formatSources(sources []string) string {
	return strings.Join(sources, "+")
}

// ===== SummarizerLLM 适配器 =====

// summarizerLLMAdapter 将 llm.Provider 适配为 memory.SummarizerLLM，解耦 memory→llm 依赖
type summarizerLLMAdapter struct {
	provider llm.Provider
}

// NewSummarizerLLMAdapter 将 llm.Provider 适配为 memory.SummarizerLLM，用于创建 Summarizer
func NewSummarizerLLMAdapter(provider llm.Provider) memory.SummarizerLLM {
	return &summarizerLLMAdapter{provider: provider}
}

func (a *summarizerLLMAdapter) Complete(ctx context.Context, messages []memory.ChatMessageForSummary, model string) (string, error) {
	chatMsgs := make([]llm.ChatMessage, len(messages))
	for i, m := range messages {
		chatMsgs[i] = llm.ChatMessage{Role: m.Role, Content: m.Content}
	}

	req := &llm.CompletionRequest{
		Messages: chatMsgs,
	}
	if model != "" {
		req.Model = model
	}

	resp, err := a.provider.Complete(ctx, req)
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}
