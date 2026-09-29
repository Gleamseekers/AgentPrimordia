# pgvector Provider for AgentPrimordia

[PostgreSQL](https://www.postgresql.org/) + [pgvector](https://github.com/pgvector/pgvector) 向量存储提供者。

## 功能

- 向量 CRUD（增删改查）
- KNN 搜索（余弦距离 / L2 距离 / 内积）
- JSONB 元数据存储
- 多租户命名空间隔离
- HNSW 与 IVFFlat 索引支持

## 前置条件

1. PostgreSQL 13+ 已安装
2. 启用 pgvector 扩展：`CREATE EXTENSION IF NOT EXISTS vector;`
3. 连接字符串：`postgres://user:password@host:5432/dbname`

## 使用方式

```go
import "github.com/Gleamseekers/AgentPrimordia/pgvector"

cfg := pgvector.Config{
    ConnString:    "postgres://localhost:5432/ap",
    TableName:     "agent_memory",
    Dimensions:    1536,     // OpenAI text-embedding-3-large
    Distance:      pgvector.CosineDistance,
    IndexType:     pgvector.HNSWIndex,
    MaxConnections: 10,
}

store, err := pgvector.New(context.Background(), cfg)
if err != nil { log.Fatal(err) }
defer store.Close()

// 添加向量
err = store.Add(ctx, "ep_1", []float32{0.1, 0.2, ...}, map[string]string{"tenant": "t1"})

// 搜索
results, err := store.Search(ctx, queryVec, 10, map[string]string{"tenant": "t1"})
```

## 与 memory 向量体系集成

`pgvector.Store` 不直接读取环境变量，全部配置经 `pgvector.Config{...}` 结构体传入 `pgvector.New(ctx, cfg)`。

框架侧接入路径：`internal/memory/pgvector_store.go` 中的 `PgVectorVectorStore` 适配器实现了 memory 的 `VectorStore` 接口（Insert/Delete/Search/CreateCollection/DropCollection），公共入口为 `ap.NewPgVectorVectorStore(ctx, ap.PgVectorConfig{...})`。RAG 混合检索与 HNSW 内存向量库的公共构造器：

```go
import ap "github.com/Gleamseekers/AgentPrimordia/agentprimordia/pkg"

// RAG 混合检索（Memory + Embedding + 向量通道）
embedder := ap.NewEmbeddingAdapter(provider, 1536) // llm.Provider 适配为 EmbeddingProvider
rag := ap.NewRAGStore(memStore, embedder)

// HNSW 内存向量库（pgvector 之外的默认实现）
vec := ap.NewVectorStoreWithHNSW(1536, ap.HNSWConfig{...})

// pgvector 后端（经适配器接入 memory.VectorStore 接口）
pv, err := ap.NewPgVectorVectorStore(ctx, ap.PgVectorConfig{
    ConnString: "postgres://localhost:5432/ap",
    TableName:  "agent_vectors",
    Dimensions: 1536,
})
```

> 注意：`ap.NewRAGStore` 内部使用内置向量通道，pgvector 后端经 `internal/memory/pgvector_store.go`
> 的适配接入，两者维度需与 EmbeddingProvider 保持一致。

## 配置

配置仅经 `pgvector.Config` 结构体传入（`New(ctx, cfg)`），不读取任何环境变量：

- `ConnString` — PostgreSQL 连接字符串（必填）
- `TableName` — 表名（默认 `agent_vectors`）
- `Dimensions` — 向量维度（默认 16，按需显式指定，如 OpenAI text-embedding-3-large 为 1536）
- `Distance` — 距离度量（默认 cosine）
- `IndexType` — 索引类型（默认 hnsw，可选 ivfflat）
- `MaxConnections` — 连接池大小（默认 5）
