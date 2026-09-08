// Stability: 混合 —
//
//	Provider / Config / 9 家 Provider 实现（OpenAI / Anthropic / Gemini / Ollama
//	/ Azure / Cohere / Mistral / Qwen / GLM）/ ResilientProvider: Stable。
//	LLM 缓存（CachedProvider / CacheManager / HybridCache / InMemoryCache）: Experimental。
//	多模态（MultimodalProvider / MultimodalAdapter / CompletionRequestExt）: Experimental。
//	结构化输出（SchemaFromStruct / NewStructuredExtractor）: Stable。
package ap

import (
	"agentprimordia/internal/llm"
	"agentprimordia/internal/resilience"
)

// Provider 是 LLM 提供者的核心接口，定义补全、流式、tool调用和嵌入等方法
type Provider = llm.Provider

// Config 是 LLM 提供者的通用配置，包含 API Key、Base URL、模型名、温度和最大 Token 数
type Config = llm.Config

// CompletionRequest 是补全请求，包含消息列表、模型、温度和最大 Token 数
type CompletionRequest = llm.CompletionRequest

// CompletionResponse 是补全响应，包含 ID、内容、角色和用量
type CompletionResponse = llm.CompletionResponse

// ChatMessage 是对话消息，包含角色、内容、tool调用和tool调用 ID
type ChatMessage = llm.ChatMessage

// ToolCallRequest 是tool调用请求，包含消息列表和tool定义列表
type ToolCallRequest = llm.ToolCallRequest

// ToolCallResponse 是tool调用响应，包含内容、tool调用列表和用量
type ToolCallResponse = llm.ToolCallResponse

// Chunk 是流式输出的一个片段，包含内容和完成标记
type Chunk = llm.Chunk

// ModelInfo 是模型信息，包含名称、提供者、上下文窗口大小和能力标记
type ModelInfo = llm.ModelInfo

// Usage 是 Token 用量统计，包含输入、输出和总 Token 数
type Usage = llm.Usage

// FunctionCall 表示 LLM 发起的函数调用，包含 ID、名称和 JSON 参数
type FunctionCall = llm.FunctionCall

// FunctionDefinition 是函数定义，包含名称、描述和参数 JSON Schema
type FunctionDefinition = llm.FunctionDefinition

// ToolDefinition 是tool定义，包含类型和函数定义
type ToolDefinition = llm.ToolDefinition

// APIError 是 LLM API 返回的错误，包含错误码、消息和类型
type APIError = llm.APIError

// OpenAIProvider 实现 OpenAI 系列模型调用（GPT-4o 等）
type OpenAIProvider = llm.OpenAIProvider

// AnthropicProvider 实现 Claude 系列模型调用
type AnthropicProvider = llm.AnthropicProvider

// GeminiProvider 实现 Google Gemini 系列模型调用
type GeminiProvider = llm.GeminiProvider

// OllamaProvider 实现本地 Ollama 模型调用
type OllamaProvider = llm.OllamaProvider

// AzureOpenAIProvider 实现 Azure OpenAI 模型调用
type AzureOpenAIProvider = llm.AzureOpenAIProvider

// AzureConfig 是 Azure OpenAI 的专有配置，包含资源名称、部署名称和 API 版本
type AzureConfig = llm.AzureConfig

// ResilientProvider 是弹性 LLM 提供者，支持重试、熔断和降级
type ResilientProvider = llm.ResilientProvider

// ResilientConfig 是弹性提供者的配置，包含最大重试次数、退避时间和熔断阈值
type ResilientConfig = llm.ResilientConfig

// CohereProvider 实现 Cohere v2 API 模型调用
type CohereProvider = llm.CohereProvider

// MistralProvider 实现 Mistral AI 模型调用
type MistralProvider = llm.MistralProvider

// GeminiMultimodalProvider 实现 Google Gemini 多模态模型调用（文本+图片+音频+视频）
type GeminiMultimodalProvider = llm.GeminiMultimodalProvider

// DemoProvider 无需 API key 的演示提供者，基于关键词匹配产生响应
type DemoProvider = llm.DemoProvider

var (
	// NewOpenAIProvider 创建 OpenAI 提供者实例
	NewOpenAIProvider = llm.NewOpenAIProvider
	// NewAnthropicProvider 创建 Anthropic 提供者实例
	NewAnthropicProvider = llm.NewAnthropicProvider
	// NewGeminiProvider 创建 Gemini 提供者实例
	NewGeminiProvider = llm.NewGeminiProvider
	// NewGeminiMultimodalProvider 创建 Gemini 多模态提供者实例
	NewGeminiMultimodalProvider = llm.NewGeminiMultimodalProvider
	// NewOpenAIMultimodalProvider 创建 OpenAI 多模态提供者实例（支持图片/音频输入）
	NewOpenAIMultimodalProvider = llm.NewOpenAIMultimodalProvider
	// NewAnthropicVisionProvider 创建 Anthropic 视觉提供者实例（支持图片输入）
	NewAnthropicVisionProvider = llm.NewAnthropicVisionProvider
	// NewOllamaProvider 创建 Ollama 提供者实例
	NewOllamaProvider = llm.NewOllamaProvider
	// NewAzureOpenAIProvider 创建 Azure OpenAI 提供者实例
	NewAzureOpenAIProvider = llm.NewAzureOpenAIProvider
	// NewResilientProvider 创建弹性提供者实例，包装主提供者并支持降级
	NewResilientProvider = llm.NewResilientProvider
	// DefaultResilientConfig 返回弹性提供者的默认配置（3 次重试、500ms 退避、5 次熔断阈值）
	DefaultResilientConfig = llm.DefaultResilientConfig
	// NewCohereProvider 创建 Cohere 提供者实例
	NewCohereProvider = llm.NewCohereProvider
	// NewMistralProvider 创建 Mistral 提供者实例
	NewMistralProvider = llm.NewMistralProvider
	// NewDemoProvider 创建无需 API key 的演示提供者
	NewDemoProvider = llm.NewDemoProvider
	// NewQwenProvider 创建通义千问提供者实例（DashScope OpenAI 兼容模式）
	NewQwenProvider = llm.NewQwenProvider
	// NewGLMProvider 创建智谱 GLM 提供者实例（OpenAI 兼容模式）
	//
	// 注意: GLMProvider 的 CallTools 当前返回 ErrNotSupported（智谱 OpenAI 兼容层对 tool_calls
	// 协议支持有限）。如需tool调用，请使用 OpenAI/Anthropic/Gemini/Qwen。
	NewGLMProvider = llm.NewGLMProvider
	// ConfigFromEnv 从环境变量读取 LLM 配置（AP_LLM_API_KEY, AP_LLM_BASE_URL, AP_LLM_MODEL 等）
	ConfigFromEnv = llm.ConfigFromEnv
)

// LLMCache 是 LLM 响应缓存接口，支持 Get/Set/Stats/Clear/Invalidate
// Stability: Experimental — 缓存策略与一致性语义在 v0.x 演进。
type LLMCache = llm.LLMCache

// CacheStats 是缓存统计信息
type CacheStats = llm.CacheStats

// CacheEntry 是缓存条目
type CacheEntry = llm.CacheEntry

// CachedProvider 是带 LLM 缓存的 Provider 装饰器
type CachedProvider = llm.CachedProvider

// InMemoryCache 是基于内存的向量相似度缓存
type InMemoryCache = llm.InMemoryCache

// InMemoryCacheFullConfig 是内存缓存的完整配置
type InMemoryCacheFullConfig = llm.InMemoryCacheFullConfig

// FingerprintCache 是基于 Prompt 指纹的精确匹配缓存
type FingerprintCache = llm.FingerprintCache

// HybridCache 是混合缓存，先精确匹配再语义匹配
type HybridCache = llm.HybridCache

// CacheManager 是缓存管理器，统一管理缓存实例和 Tracer 联动
type CacheManager = llm.CacheManager

// CacheManagerConfig 是缓存管理器配置
type CacheManagerConfig = llm.CacheManagerConfig

// NoopCache 是空缓存实现，用于禁用缓存
type NoopCache = llm.NoopCache

// EmbeddingFunc 是文本向量化函数类型
type EmbeddingFunc = llm.EmbeddingFunc

var (
	// NewInMemoryCache 创建内存缓存实例
	NewInMemoryCache = llm.NewInMemoryCache
	// NewInMemoryCacheWithFullConfig 创建带完整配置的内存缓存实例
	NewInMemoryCacheWithFullConfig = llm.NewInMemoryCacheWithFullConfig
	// NewCachedProvider 创建带缓存的 Provider 装饰器
	NewCachedProvider = llm.NewCachedProvider
	// NewCachedProviderWithManager 创建带 CacheManager 的缓存 Provider
	NewCachedProviderWithManager = llm.NewCachedProviderWithManager
	// NewFingerprintCache 创建指纹缓存实例
	NewFingerprintCache = llm.NewFingerprintCache
	// NewHybridCache 创建混合缓存实例
	NewHybridCache = llm.NewHybridCache
	// NewCacheManager 创建缓存管理器实例
	NewCacheManager = llm.NewCacheManager
	// PromptFingerprint 生成 Prompt 的指纹哈希
	PromptFingerprint = llm.PromptFingerprint
)

// SchemaDef 是 JSON Schema 定义，用于结构化输出
type SchemaDef = llm.SchemaDef

// ResponseFormat 是 LLM 响应格式控制
type ResponseFormat = llm.ResponseFormat

// ResponseFormatType 是响应格式类型
type ResponseFormatType = llm.ResponseFormatType

// ValidationError 是 Schema 验证错误
type ValidationError = llm.ValidationError

// StructuredExtractor 是结构化数据提取器
type StructuredExtractor = llm.StructuredExtractor

// ExtractorConfig 是提取器配置
type ExtractorConfig = llm.ExtractorConfig

// SchemaOption 是 Schema 生成选项函数
type SchemaOption = llm.SchemaOption

var (
	ResponseFormatText       = llm.ResponseFormatText
	ResponseFormatJSONObject = llm.ResponseFormatJSONObject
	ResponseFormatJSONSchema = llm.ResponseFormatJSONSchema
)

var (
	// NewStructuredExtractor 创建结构化提取器
	NewStructuredExtractor = llm.NewStructuredExtractor
	// NewStructuredExtractorWithConfig 创建带配置的结构化提取器
	NewStructuredExtractorWithConfig = llm.NewStructuredExtractorWithConfig
	// SchemaFromStruct 从 Go struct 生成 JSON Schema
	SchemaFromStruct = llm.SchemaFromStruct
	// WithSchemaName 设置 Schema 名称
	WithSchemaName = llm.WithSchemaName
	// WithStrictMode 启用严格模式
	WithStrictMode = llm.WithStrictMode
	// ValidateAgainstSchema 验证 JSON 数据是否符合 Schema
	ValidateAgainstSchema = llm.ValidateAgainstSchema
	// 预定义 Schema 模板
	SentimentSchema                = llm.SentimentSchema
	SentimentDetailSchema          = llm.SentimentDetailSchema
	NERSchema                      = llm.NERSchema
	ClassificationSchema           = llm.ClassificationSchema
	MultiLabelClassificationSchema = llm.MultiLabelClassificationSchema
	SummarySchema                  = llm.SummarySchema
	ExtractiveSummarySchema        = llm.ExtractiveSummarySchema
	CodeAnalysisSchema             = llm.CodeAnalysisSchema
	DocumentExtractionSchema       = llm.DocumentExtractionSchema
	APIResponseAnalysisSchema      = llm.APIResponseAnalysisSchema
)

// 预定义模板输出类型
type SentimentOutput = llm.SentimentOutput
type SentimentDetailOutput = llm.SentimentDetailOutput
type AspectSentiment = llm.AspectSentiment
type NEROutput = llm.NEROutput
type Entity = llm.Entity
type ClassificationOutput = llm.ClassificationOutput
type MultiLabelClassificationOutput = llm.MultiLabelClassificationOutput
type LabelScore = llm.LabelScore
type SummaryOutput = llm.SummaryOutput
type ExtractiveSummaryOutput = llm.ExtractiveSummaryOutput

// ===== 多模态统一抽象 =====
// Stability: Experimental — API 形状在 v0.x 仍可能调整。

// MultimodalCapability 是多模态能力标记（CapText/CapVision/CapAudio/CapVideo）
type MultimodalCapability = llm.MultimodalCapability

// MultimodalProvider 是多模态 LLM 提供者接口，扩展标准 Provider 以支持多模态输入
type MultimodalProvider = llm.MultimodalProvider

// MultimodalAdapter 是多模态适配器，将不同 Provider 统一为 MultimodalProvider 接口
type MultimodalAdapter = llm.MultimodalAdapter

// CompletionRequestExt 是扩展的补全请求，支持多模态内容块
type CompletionRequestExt = llm.CompletionRequestExt

// MultimodalContent 是多模态内容块抽象（Text/Image/Audio/Video）
type MultimodalContent = llm.MultimodalContent

// ChatMessageExt 是扩展的聊天消息，支持多模态内容
type ChatMessageExt = llm.ChatMessageExt

const (
	CapText   = llm.CapText
	CapVision = llm.CapVision
	CapAudio  = llm.CapAudio
	CapVideo  = llm.CapVideo

	ContentTypeText     = llm.ContentTypeText
	ContentTypeImageURL = llm.ContentTypeImageURL
	ContentTypeImageB64 = llm.ContentTypeImageB64
	ContentTypeAudio    = llm.ContentTypeAudio
	ContentTypeVideo    = llm.ContentTypeVideo
)

var (
	NewMultimodalProvider    = llm.NewMultimodalProvider
	NewMultimodalAdapter     = llm.NewMultimodalAdapter
	NewTextContent           = llm.NewTextContent
	NewImageURLContent       = llm.NewImageURLContent
	NewImageB64Content       = llm.NewImageB64Content
	NewAudioContent          = llm.NewAudioContent
	NewVideoContent          = llm.NewVideoContent
	NewUserTextMessage       = llm.NewUserTextMessage
	NewUserMultimodalMessage = llm.NewUserMultimodalMessage
)

// ModelPricing 定义按模型计费的价格信息，用于成本估算
type ModelPricing = llm.ModelPricing

var (
	// DefaultPricingTable 返回主流模型的默认价格表
	DefaultPricingTable = llm.DefaultPricingTable
	// EstimateCost 估算单次 LLM 调用的成本
	EstimateCost = llm.EstimateCost
)

// ===== 断路器（LLM 故障转移） =====

// CircuitBreaker 断路器，用于 LLM Provider 故障转移
type CircuitBreaker = resilience.CircuitBreaker

// CircuitBreakerConfig 断路器配置
type CircuitBreakerConfig = resilience.Config

// CircuitBreakerState 断路器状态
type CircuitBreakerState = resilience.State

// NewCircuitBreaker 创建断路器
var NewCircuitBreaker = resilience.NewCircuitBreaker

const (
	// CircuitClosed 断路器关闭（正常）
	CircuitClosed = resilience.StateClosed
	// CircuitOpen 断路器打开（断路）
	CircuitOpen = resilience.StateOpen
	// CircuitHalfOpen 断路器半开（试探）
	CircuitHalfOpen = resilience.StateHalfOpen
)
