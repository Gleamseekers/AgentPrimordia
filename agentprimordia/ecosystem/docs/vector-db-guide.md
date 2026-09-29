# Vector DB 选型指南

AgentPrimordia 提供四种向量存储后端，按场景选择：

## 对比

| 维度 | InMemory | SQLite+FTS5 | Qdrant | Milvus | pgvector |
|------|----------|-------------|--------|--------|----------|
| **适用规模** | <100K | <500K | 100K-1M | >1M | 100K-10M |
| **部署复杂度** | 零 | 零 | 低 | 高 | 低（如已有PG） |
| **持久化** | 否 | 是 | 是 | 是 | 是 |
| **分布式** | 否 | 否 | 可选 | 是 | 否 |
| **全文搜索** | 否 | FTS5 | 否 | 否 | 是（PG原生） |
| **混合检索** | 否 | 否 | 需自建 | 需自建 | 原生支持 |
| **Go SDK** | 内置 | 内置 | REST | REST | database/sql |
| **外部依赖** | 无 | 无 | Qdrant服务 | Milvus集群 | PostgreSQL+pgvector |

## 推荐选择

### <100K 文档 → InMemory
```go
store := ap.NewVectorStore(1536)
```
零依赖，开发测试首选。数据不持久，重启丢失。

### 100K-1M 文档 → Qdrant
```go
client, _ := memory.NewQdrantClient(memory.QdrantConfig{
    Host:       "localhost",
    Port:       6333,
    Collection: "my_docs",
    VectorSize: 1536,
    Distance:   "cosine",
})
```
Go REST 客户端，单节点部署简单，性能优秀。

### >1M 文档 → Milvus
```go
client, _ := memory.NewMilvusClient(memory.MilvusConfig{
    Host:     "localhost",
    Port:     19530,
    Database: "default",
})
```
分布式架构，水平扩展，适合企业级大规模部署。

### 已有 PostgreSQL → pgvector
```go
import pgv "github.com/Gleamseekers/AgentPrimordia/pgvector"

client, _ := pgv.NewClient(pgv.Config{
    Host:       "localhost",
    Port:       5432,
    Database:   "mydb",
    User:       "postgres",
    Password:   "password",
    TableName:  "ap_vectors",
    VectorSize: 1536,
})
```
不引入新基础设施，复用已有 PostgreSQL。支持向量+全文混合查询。

## 安装 pgvector 扩展

```bash
# Docker 方式（推荐）
docker run -d \
  --name pgvector \
  -e POSTGRES_PASSWORD=password \
  -p 5432:5432 \
  pgvector/pgvector:pg16

# macOS
brew install pgvector

# Ubuntu
sudo apt install postgresql-16-pgvector
```

## RAG Pipeline 混合检索

无论选择哪种向量后端，都可以通过 RAG Pipeline 实现全文+向量混合检索：

```go
// FTS 权重 40% + 向量权重 60%
ragStore := memory.NewRAGStore(memory, vectorStore, embedder)
results, _ := ragStore.HybridSearch(ctx, "查询内容", 10)
```
