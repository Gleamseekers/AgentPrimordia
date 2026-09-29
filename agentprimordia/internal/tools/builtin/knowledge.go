package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Gleamseekers/AgentPrimordia/internal/tools"
)

const defaultKnowledgeTopK = 5

// KnowledgeSearcher 是知识库搜索接口，由外部注入 RAG Provider 实现
type KnowledgeSearcher interface {
	SearchKnowledge(ctx context.Context, query string, topK int) ([]*KnowledgeDoc, error)
}

// KnowledgeDoc 是知识库搜索返回的文档
type KnowledgeDoc struct {
	ID      string  `json:"id"`
	Content string  `json:"content"`
	Score   float32 `json:"score"`
	Source  string  `json:"source,omitempty"`
}

// KnowledgeSearch 是一个内置tool，允许 Agent 主动搜索知识库
type KnowledgeSearch struct {
	searcher KnowledgeSearcher
	topK     int
}

// NewKnowledgeSearch 创建知识库搜索tool
func NewKnowledgeSearch(searcher KnowledgeSearcher) *KnowledgeSearch {
	return &KnowledgeSearch{
		searcher: searcher,
		topK:     defaultKnowledgeTopK,
	}
}

// WithTopK 设置默认返回结果数
func (k *KnowledgeSearch) WithTopK(topK int) *KnowledgeSearch {
	k.topK = topK
	return k
}

func (k *KnowledgeSearch) Name() string { return "knowledge_search" }

func (k *KnowledgeSearch) Description() string {
	return "Search the knowledge base for relevant information. Use this tool when you need to look up facts, documentation, or historical context before answering a question."
}

func (k *KnowledgeSearch) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "The search query to find relevant knowledge"},
    "top_k": {"type": "integer", "description": "Number of results to return (default: 5)"}
  },
  "required": ["query"]
}`)
}

func (k *KnowledgeSearch) Execute(ctx context.Context, args json.RawMessage) (*tools.Result, error) {
	if k.searcher == nil {
		return tools.NewErrorResult("knowledge search not configured: no RAG provider available"), nil
	}

	var params struct {
		Query string `json:"query"`
		TopK  int    `json:"top_k"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return tools.NewErrorResult(fmt.Sprintf("invalid arguments: %v", err)), nil
	}

	if params.Query == "" {
		return tools.NewErrorResult("query is required"), nil
	}

	topK := k.topK
	if params.TopK > 0 {
		topK = params.TopK
	}

	docs, err := k.searcher.SearchKnowledge(ctx, params.Query, topK)
	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("knowledge search failed: %v", err)), nil
	}

	if len(docs) == 0 {
		return tools.NewResult("No relevant knowledge found for the given query."), nil
	}

	// 格式化结果
	result := "=== Knowledge Search Results ===\n"
	for i, doc := range docs {
		result += fmt.Sprintf("[%d] (score: %.2f) %s\n", i+1, doc.Score, doc.Content)
	}
	result += "=== End of Results ===\n"

	return tools.NewResult(result), nil
}
