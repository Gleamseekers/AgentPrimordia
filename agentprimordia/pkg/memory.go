// Stability: Stable — 记忆存储核心接口与实现（除 VectorStore 之外）。
package ap

import (
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/memory"
)

// Memory 是记忆存储的核心接口，提供增删查改、搜索、统计、导入导出等完整能力
type Memory = memory.Memory

// Episode 是一条记忆片段，包含会话 ID、角色、内容、摘要、主题、重要性等
type Episode = memory.Episode

// SearchOptions 是记忆搜索的选项，支持会话过滤、分页和角色过滤
type SearchOptions = memory.SearchOptions

// ListOptions 是记忆列表的选项，支持会话过滤、分页、排序方向
type ListOptions = memory.ListOptions

// MemoryStats 是记忆存储的统计信息，包含总数、会话数、时间范围和存储大小
type MemoryStats = memory.MemoryStats

// SQLiteStore 是基于 SQLite + FTS5 的记忆存储实现，支持全文搜索和自动清理
type SQLiteStore = memory.SQLiteStore

// VectorStore 是内存向量存储，支持余弦相似度搜索
type VectorStore = memory.VectorStore

// VectorEntry 是向量存储中的一条记录，包含 ID、向量和元数据
type VectorEntry = memory.VectorEntry

// VectorSearchResult 是向量搜索的结果，包含 ID、相似度分数和元数据
type VectorSearchResult = memory.VectorSearchResult

// EmbeddingProvider 是向量化接口，由 LLM Provider 适配实现
type EmbeddingProvider = memory.EmbeddingProvider

// RAGStore 封装了 Memory + VectorStore + EmbeddingProvider，提供混合 RAG 检索能力
type RAGStore = memory.RAGStore

// RAGResult 是 RAG 查询的结果，包含匹配的记忆片段、相关度分数和来源（fts / vector）
type RAGResult = memory.RAGResult

// ===== RAG 融合配置 =====

// HybridFusionMode 是 RAG 混合检索的融合模式
type HybridFusionMode = memory.HybridFusionMode

// RAGFusionConfig 是 RAG 检索融合配置，支持运行时调参
type RAGFusionConfig = memory.RAGFusionConfig

const (
	// FusionLinear 线性加权融合（默认）
	FusionLinear = memory.FusionLinear
	// FusionRRF Reciprocal Rank Fusion（推荐用于生产）
	FusionRRF = memory.FusionRRF
)

var (
	// NewRAGStoreWithFusionConfig 创建带融合配置的 RAG 存储实例
	NewRAGStoreWithFusionConfig = memory.NewRAGStoreWithFusionConfig
	// DefaultRAGFusionConfig 返回默认融合配置
	DefaultRAGFusionConfig = memory.DefaultRAGFusionConfig
)

// Reranker 是 RAG 搜索结果重排序接口
type Reranker = memory.Reranker

// MMRReranker 使用最大边际相关性算法进行重排序
type MMRReranker = memory.MMRReranker

// MMRConfig 是 MMR 重排序配置
type MMRConfig = memory.MMRConfig

// Compressor 是记忆压缩器接口
type Compressor = memory.Compressor

// CompressorConfig 是压缩器配置
type CompressorConfig = memory.CompressorConfig

// CompressSummarizer 是压缩摘要接口
type CompressSummarizer = memory.CompressSummarizer

// CompressorSummary 是压缩摘要结果
type CompressorSummary = memory.CompressorSummary

// CleanupConfig 是记忆自动清理配置，包含过期天数、清理间隔和保留角色
type CleanupConfig = memory.CleanupConfig

// HNSWIndex 是 HNSW 向量索引，支持快速近似最近邻搜索
type HNSWIndex = memory.HNSWIndex

// HNSWConfig 是 HNSW 索引配置
type HNSWConfig = memory.HNSWConfig

// HNSWSearchResult 是 HNSW 搜索结果
type HNSWSearchResult = memory.HNSWSearchResult

var (
	// NewSQLiteStore 创建基于 SQLite 的记忆存储实例，参数为数据库文件路径
	NewSQLiteStore = memory.NewSQLiteStore
	// WithInMemory 创建内存模式的 SQLite 记忆存储，适用于测试
	WithInMemory = memory.WithInMemory
	// NewInMemoryStore 创建纯内存记忆存储实例（无持久化）
	NewInMemoryStore = memory.NewInMemoryStore
	// NewMemory 通过工厂函数创建记忆存储实例，根据 Config.Type 选择后端
	NewMemory = memory.NewMemory
	// NewVectorStore 创建内存向量存储实例，参数为向量维度
	NewVectorStore = memory.NewVectorStore
	// NewVectorStoreWithHNSW 创建带 HNSW 索引的向量存储
	NewVectorStoreWithHNSW = memory.NewVectorStoreWithHNSW
	// NewHNSWIndex 创建 HNSW 向量索引
	NewHNSWIndex = memory.NewHNSWIndex
	// FormatRAGContext 将 RAG 检索结果格式化为可注入 Prompt 的上下文字符串
	FormatRAGContext = memory.FormatRAGContext
)

// ===== 记忆存储工厂配置 =====

// BackendType 是记忆存储后端类型（sqlite / memory）
type BackendType = memory.BackendType

// StoreConfig 是记忆存储工厂配置，指定后端类型和路径
type StoreConfig = memory.Config

// InMemoryStore 是纯内存记忆存储实现，适用于测试和临时场景
type InMemoryStore = memory.InMemoryStore

const (
	// BackendSQLite 表示 SQLite 持久化后端
	BackendSQLite = memory.BackendSQLite
	// BackendMemory 表示纯内存后端
	BackendMemory = memory.BackendMemory
)

// ===== RAG 端到端生成 =====

// LLMGenerator 是 LLM 文本生成接口，用于 RAG 端到端生成（与 llm.Provider 解耦）
type LLMGenerator = memory.LLMGenerator

// RetrievalAugmentedGenerator 是 RAG 端到端生成器（检索→上下文组装→LLM 生成）
type RetrievalAugmentedGenerator = memory.RetrievalAugmentedGenerator

// RAGConfig 是 RAG 生成器配置
type RAGGeneratorConfig = memory.RAGConfig

// QueryResult 是 RAG 查询结果（答案 + 来源 + 上下文）
type QueryResult = memory.QueryResult

var (
	NewRetrievalAugmentedGenerator = memory.NewRetrievalAugmentedGenerator
	NewMMRReranker                 = memory.NewMMRReranker
	NewCompressor                  = memory.NewCompressor
)

// ===== pgvector 向量存储（生产级 PostgreSQL + pgvector 后端） =====

// PgVectorVectorStore 基于 PostgreSQL + pgvector 的向量存储实现
//
// 适用于生产环境，支持大规模向量数据（>1M 文档）。
//
// 使用方式：
//
//	store, err := ap.NewPgVectorVectorStore(ctx, ap.PgVectorConfig{
//	    ConnString: "postgres://user:pass@host:5432/dbname",
//	    Dimensions: 1536,
//	})
//	defer store.Close()
//
// Stability: Experimental
type PgVectorVectorStore = memory.PgVectorVectorStore

// PgVectorConfig pgvector 后端配置
type PgVectorConfig = memory.PgVectorConfig

// VectorRecord 向量记录（ID + 向量 + 元数据）
type VectorRecord = memory.VectorRecord

// VectorMatch 向量搜索匹配结果
type VectorMatch = memory.VectorMatch

// VectorSearchOptions 向量搜索选项
type VectorSearchOptions = memory.VectorSearchOptions

var (
	// NewPgVectorVectorStore 创建 pgvector 向量存储实例
	NewPgVectorVectorStore = memory.NewPgVectorVectorStore
)
