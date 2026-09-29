# Memory API 参考

> `package memory` — 记忆存储接口与实现。

## Memory 接口

Memory 是由 7 个子接口组合而成的复合接口：

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

## Episode 类型

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
    Query          string
    SessionID      string
    Limit          int     // 分页限制：与 Offset 配合实现分页查询
    Offset         int
    RoleFilter     string
    Tags           []string
    MinScore       float64
    MaxResults     int     // 最大结果数：限制搜索返回的总结果数
    UseSemantic    bool
    SemanticWeight float64
    TopK           int
    Threshold      float32
    Filter         map[string]any
}
```

## SearchResult

```go
type SearchResult struct {
    Episode       *Episode
    KeywordScore  float64
    SemanticScore float64
    CombinedScore float64
}
```

## ListOptions

```go
type ListOptions struct {
    SessionID string
    Limit     int
    Offset    int
    OrderBy   string  // "created_at" / "importance"
    Ascending bool
}
```

## 后端实现

### InMemoryStore

内存存储（测试用）：

```go
// ap.WithInMemory() 创建内存数据库（底层 SQLite :memory:），返回 *SQLiteStore
func WithInMemory() (*SQLiteStore, error)
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

### SQLiteStore

SQLite + FTS5 全文搜索实现（WAL 模式默认开启，内部 PRAGMA 配置）：

```go
func NewSQLiteStore(dbPath string) (*SQLiteStore, error)
```

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

## SimpleVectorStore

内存向量存储（余弦相似度），支持可选 HNSW 索引：

```go
func NewVectorStore(dimensions int) *SimpleVectorStore
func NewVectorStoreWithHNSW(dimensions int, cfg HNSWConfig) *SimpleVectorStore

func (vs *SimpleVectorStore) Add(ctx context.Context, id string, vector []float32, metadata map[string]string) error
func (vs *SimpleVectorStore) Search(ctx context.Context, query []float32, topK int) ([]*VectorSearchResult, error)
func (vs *SimpleVectorStore) Delete(ctx context.Context, id string) error
```

**VectorSearchResult 结构：**

```go
type VectorSearchResult struct {
    ID       string
    Score    float32
    Metadata map[string]string
}
```

**HNSWConfig：**

```go
type HNSWConfig struct {
    Dimensions     int
    MaxConnections int // 每个节点的最大连接数 M（默认 16）
    EfConstruction int // 构建时的搜索范围（默认 200）
    EfSearch       int // 搜索时的搜索范围（默认 50）
}
```

## RAGStore

混合 RAG 检索（FTS + Vector），支持 RRF 融合：

```go
func NewRAGStore(mem Memory, embedder EmbeddingProvider) *RAGStore
func NewRAGStoreWithFusionConfig(mem Memory, embedder EmbeddingProvider, cfg RAGFusionConfig) *RAGStore
```

### RRF 融合模式

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

## 嵌入接口

```go
// EmbeddingProvider 嵌入接口（定义在 memory 包中）
type EmbeddingProvider interface {
    Embed(ctx context.Context, texts []string) ([][]float32, error)
    Dimensions() int
}

// 通过 ap.NewEmbeddingAdapter(provider, dimensions) 将 llm.Provider 适配为 EmbeddingProvider
```

## 外部向量库

Qdrant / Milvus 客户端位于 `internal/memory`（不 经 pkg re-export，属内部实现细节）：

```go
func NewQdrantClient(config QdrantConfig) (*QdrantClient, error)   // internal/memory
func NewMilvusClient(config MilvusConfig) (*MilvusClient, error)   // internal/memory
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
        provider, err := ap.NewOpenAIProvider(ap.Config{
            APIKey: os.Getenv("OPENAI_API_KEY"),
            Model:  "gpt-4o",
        })
        if err != nil {
            log.Fatal(err)
        }
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
    import { ReActAgent, OpenAIProvider, SqliteStore } from '@agentprimordia/sdk';

    const mem = new SqliteStore({ dbPath: './data/memory.db' });

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
