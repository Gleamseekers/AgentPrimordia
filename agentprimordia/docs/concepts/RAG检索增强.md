# RAG（检索增强生成）

AgentPrimordia 的 Memory 模块内置向量检索能力，为 Agent 提供 RAG 支持。

## 架构

RAG 能力由 `internal/memory` 包中的向量存储层实现：

- **VectorStore**：向量存储接口，支持 Add / Search / Delete
- **SQLiteStore + FTS5**：基于 SQLite 全文检索的轻量方案
- **pgvector 扩展**：基于 PostgreSQL pgvector 的生产级向量检索（独立模块 `pgvector/`）

## 工作流程

1. **索引阶段**：将文档分块后生成 embedding 向量，存入 VectorStore
2. **检索阶段**：用户查询生成 embedding，通过余弦相似度检索 Top-K 相关片段
3. **增强阶段**：将检索结果注入 Agent 的上下文窗口，辅助 LLM 生成

## 使用示例

```go
import ap "github.com/Gleamseekers/AgentPrimordia/pkg"

// 创建带向量检索的记忆存储
store := ap.NewSQLiteStore("memory.db")

// 添加带向量的记忆
store.Add(ctx, &ap.Episode{
    SessionID: "rag-session",
    Role:      "system",
    Content:   "文档内容片段...",
    Embedding: embeddingVector,
})

// 向量检索
results := store.VectorSearch(ctx, queryEmbedding, 5)
```

## 与 Agent 集成

通过 `WithMemory()` 链式 API 注入 Agent 后，ReAct 循环会自动在每轮推理前检索相关记忆：

```go
agent, err := ap.NewAgent("rag-agent", "你是助手", provider,
    ap.WithMemory(store),
)
```
