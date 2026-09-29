# Memory API

记忆系统 API 参考文档。

## Memory 接口

Memory 是由 7 个子接口组合而成的接口：

```go
type Memory interface {
    MemoryReader
    MemoryWriter
    MemorySearcher
    MemoryLifecycle
    MemoryExporter
    MemoryQuery
    MemoryToolUse
}
```

### MemoryReader

```go
type MemoryReader interface {
    Get(ctx context.Context, id string) (*Episode, error)
    GetBatch(ctx context.Context, ids []string) (map[string]*Episode, error)
    Search(ctx context.Context, query string, opts *SearchOptions) ([]*Episode, error)
    List(ctx context.Context, opts *ListOptions) ([]*Episode, error)
    Count(ctx context.Context, sessionID string) (int64, error)
    Stats(ctx context.Context) (*MemoryStats, error)
}
```

### MemoryWriter

```go
type MemoryWriter interface {
    Add(ctx context.Context, episode *Episode) error
    AddBatch(ctx context.Context, episodes []*Episode) error
    Delete(ctx context.Context, id string) error
    DeleteBatch(ctx context.Context, ids []string) error
    UpdateSummary(ctx context.Context, id, summary, topics string) error
    SetImportance(ctx context.Context, episodeID string, importance float64) error
}
```

### MemorySearcher

```go
type MemorySearcher interface {
    SearchAdvanced(ctx context.Context, opts SearchOptions) ([]*SearchResult, error)
    SearchByTag(ctx context.Context, tag string, opts *SearchOptions) ([]*Episode, error)
    GetImportant(ctx context.Context, threshold float64, limit int) ([]*Episode, error)
    GetTimeline(ctx context.Context, days int) (map[string][]*Episode, error)
}
```

### MemoryLifecycle

```go
type MemoryLifecycle interface {
    Close() error
    CleanupExpired(ctx context.Context, maxAgeDays int) (int64, error)
    ClearAll(ctx context.Context, sessionID string) error
}
```

### MemoryExporter

```go
type MemoryExporter interface {
    ExportMemories(ctx context.Context, sessionID, format string) ([]byte, error)
    ImportMemories(ctx context.Context, data []byte, format string) (int, error)
}
```

### MemoryQuery

```go
type MemoryQuery interface {
    GetMemoriesByTag(ctx context.Context, tag string, limit int) ([]*Episode, error)
    GetMemoriesBySession(ctx context.Context, sessionID string) ([]*Episode, error)
    GetImportantMemories(ctx context.Context, threshold float64, limit int) ([]*Episode, error)
    GetMemoryTimeline(ctx context.Context, days int) ([]*MemoryTimelineGroup, error)
}
```

### MemoryToolUse

```go
type MemoryToolUse interface {
    RecordToolUse(ctx context.Context, sessionID, agentName, toolName, args, result string) error
}
```

## Episode 结构

```go
type Episode struct {
    ID         string            `json:"id"`
    SessionID  string            `json:"session_id"`
    Role       string            `json:"role"`               // user / assistant / system / tool
    Content    string            `json:"content"`
    Summary    string            `json:"summary,omitempty"`
    Topics     string            `json:"topics,omitempty"`
    Importance float64           `json:"importance,omitempty"`
    Metadata   map[string]string `json:"metadata,omitempty"`
    CreatedAt  string            `json:"created_at"`
}
```

## SearchOptions

```go
type SearchOptions struct {
    SessionID   string
    Tags        []string
    MinImportance float64
    Limit       int
    Offset      int
    OrderBy     string  // "created_at" / "importance"
    Desc        bool
}

type SearchResult struct {
    Episode    *Episode
    Score      float64
    Source     string  // "fts" / "vector" / "hybrid"
}
```

## InMemoryStore

内存存储（测试用）：

```go
func WithInMemory() (Memory, error)
```

**示例：**

```go
mem, _ := ap.WithInMemory()
defer mem.Close()

mem.Add(ctx, &memory.Episode{
    SessionID: "session-1",
    Role:      "user",
    Content:   "你好",
})
```

## SQLiteStore

SQLite + FTS5 全文搜索实现：

```go
func NewSQLiteStore(dbPath string) (*SQLiteStore, error)
```

> 注：文件数据库默认启用 WAL 模式（多连接并发吞吐），无需额外配置。过期数据清理使用 `CleanupExpired(ctx, maxAgeDays)` 或 `StartAutoCleanup(cfg)`（见 Summarizer 一节）。

**示例：**

```go
mem, _ := ap.NewSQLiteStore("./data/memory.db")
defer mem.Close()

mem.Add(ctx, &memory.Episode{
    SessionID: "session-1",
    Role:      "user",
    Content:   "AgentPrimordia 是什么？",
})

// FTS5 全文搜索
results, _ := mem.Search(ctx, "Agent 框架", &memory.SearchOptions{Limit: 10})
```

## VectorStore

内存向量存储（余弦相似度）：

```go
func NewVectorStore(dimensions int) *VectorStore

func (vs *VectorStore) Add(ctx context.Context, id string, embedding []float32, metadata map[string]string) error
func (vs *VectorStore) Search(ctx context.Context, query []float32, limit int) ([]*VectorItem, error)
```

**VectorItem 结构：**

```go
type VectorItem struct {
    ID       string
    Score    float64
    Metadata map[string]string
}
```

## RAGStore

混合 RAG 检索（FTS + Vector），支持 RRF 融合：

### 创建 RAGStore

```go
func NewRAGStore(mem Memory, embedder llm.Embedder) *RAGStore
func NewRAGStoreWithFusionConfig(mem Memory, embedder llm.Embedder, fusion HybridFusionMode) *RAGStore
```

### RRF 融合模式

v0.8.0 新增 Reciprocal Rank Fusion（RRF）混合检索算法：

```go
type HybridFusionMode int

const (
    FusionLinear HybridFusionMode = iota  // 基于原始分数加权融合（默认）
    FusionRRF                             // Reciprocal Rank Fusion，基于排名融合，对量纲差异鲁棒
)

type RAGFusionConfig struct {
    FusionMode    HybridFusionMode
    RRFK          int  // RRF 常数，默认 60
    OverFetchSize int  // 预取额外候选数，提升融合质量
}
```

**运行时切换融合模式：**

```go
store, _ := memory.NewRAGStoreWithFusionConfig(mem, embedder, memory.FusionLinear)

// 切换为 RRF 模式
store.SetFusionConfig(memory.RAGFusionConfig{
    FusionMode: memory.FusionRRF,
    RRFK:       60,
})
```

### 检索流程

```
查询 → Embedding → Vector Search → FTS Search → RRF 融合 → Rerank → TopK → 上下文注入
```

## RAG Pipeline

完整 RAG Pipeline 组件：

| 组件 | 文件 | 说明 |
|------|------|------|
| `RAGStore` | `rag.go` | 混合检索（FTS + Vector + RRF） |
| `RAGPipeline` | `rag_pipeline.go` | 完整 Pipeline |
| `Reranker` | `rag_rerank.go` | 重排序 |
| `RetrievalAugmentedGenerator` | `rag_generator.go` | 端到端生成 |

## 外部向量库

Qdrant / Milvus 客户端位于 `internal/memory`，**未经 `pkg/` 导出，外部代码不可直接 import**。需要外部向量库时，推荐使用经公共 API 导出的 pgvector 存储：

```go
store, err := ap.NewPgVectorVectorStore(ctx, ap.PgVectorConfig{
    ConnString: "postgres://user:pass@localhost:5432/ap?sslmode=disable",
    Dimensions: 1536,
})
```

### QdrantClient（internal）

```go
// internal/memory/qdrant_provider.go，未导出至 pkg
func NewQdrantClient(config QdrantConfig) (*QdrantClient, error)
```

### MilvusClient（internal）

```go
// internal/memory/milvus_provider.go，未导出至 pkg
func NewMilvusClient(config MilvusConfig) (*MilvusClient, error)
```

## Summarizer

摘要提取器：

```go
type SummaryExtractor interface {
    Extract(ctx context.Context, content string) (*SummaryResult, error)
}

type SummaryResult struct {
    Summary string
    Topics  string
}
```

## Vector DB 选型

| 规模 | 推荐 | 原因 |
|------|------|------|
| <100K 文档 | InMemory | 零依赖 |
| 100K-1M | Qdrant | Go REST 客户端，性能优 |
| >1M | Milvus | 分布式，水平扩展 |
| 已有 PostgreSQL | pgvector | 不引入新基础设施 |

## 完整示例

=== "Go"

    ```go
    package main

    import (
        "context"
        "fmt"
        "log"
        "os"

        ap "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg"
    )

    func main() {
        // 1. 创建记忆存储
        mem, _ := ap.NewSQLiteStore("./data/memory.db")
        defer mem.Close()

        // 2. 创建 Agent（链式 API）
        provider := ap.NewOpenAIProvider(ap.Config{
            APIKey: os.Getenv("OPENAI_API_KEY"),
            Model:  "gpt-4o",
        })
        agent, _ := ap.NewAgent("rag-agent", "你是助手", provider,
            ap.WithMaxTurns(10),
            ap.WithMemory(mem),
        )

        // 3. 运行
        resp, err := agent.Run(context.Background(), ap.UserMessage("你好！"))
        if err != nil {
            log.Fatal(err)
        }
        fmt.Println(resp.Content)
    }
    ```

=== "TypeScript"

    ```typescript
    import { ReActAgent, OpenAIProvider, SQLiteStore } from '@agentprimordia/sdk';

    const mem = new SQLiteStore({ dbPath: './data/memory.db' });

    const agent = new ReActAgent({
      name: 'rag-agent',
      systemPrompt: '你是助手',
      model: new OpenAIProvider({ apiKey: process.env.OPENAI_API_KEY!, model: 'gpt-4o' }),
      maxTurns: 10,
      memory: mem,
    });

    const resp = await agent.run('你好！');
    console.log(resp.content);
    ```
