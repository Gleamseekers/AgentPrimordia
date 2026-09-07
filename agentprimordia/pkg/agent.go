// Package ap 提供 AgentPrimordia 框架的公共 API。
//
// AgentPrimordia 是一个通用 Go Agent 开发框架，核心能力包括：
//   - ReAct 循环引擎（Agent 推理 + tool调用）
//   - 多 Agent 编排（Pipeline / Handoff / Parallel / DAG）
//   - tool系统（注册、权限、作用域）
//   - 记忆存储（SQLite + FTS5 + 向量检索）
//   - LLM 抽象层（OpenAI / Anthropic / Gemini / Ollama / Azure / Cohere / Mistral）
//   - Pool 调度（并发任务分发、重试、会话管理）
//
// 所有类型通过类型别名从 internal 包导出，用户只需导入此包即可使用完整功能。
//
// # 公共 API 稳定性策略
//
// 自 v0.6.0 起，pkg/ 下的 export 按以下四个等级分类，承诺不同的
// 向后兼容策略。每个文件顶部用 `// Stability:` 块标注该文件共享
// 的等级；个别 export 顶部的 godoc 可能覆盖（更严格或不那么严格）。
//
//	Stable:       公共 API，向后兼容。破坏性变更需大版本（v2.0）。
//	              适用：核心 Agent / LLM / Tool / Memory / Pool 类型与构造函数。
//
//	Experimental: 实验性 API，签名可能在 minor 版本内调整。
//	              适用：缓存（CacheManager / CachedProvider）、多模态（Multimodal*）、
//	              插件（ToolPlugin / PluginLoader）等近期新增或仍在快速迭代的能力。
//	              用户应准备升级时调整调用方代码。
//
//	Deprecated:   已废弃，下个 minor 版本起将在编译期 warning，
//	              v2.0 移除。使用链式 API 替代直接字段赋值。
//
//	Internal:     仅供 pkg/ 内部使用，不属于公共 API 承诺范围。
//	              文件内未导出符号默认属于此等级。
//
// 用户应使用 `go doc` 或 IDE 悬浮提示查看每个 export 的稳定性等级。
// 详细治理策略见 agentprimordia/docs/版本规范.md（兼容性承诺与废弃策略）。
package ap

import (
	"agentprimordia/internal/agent"
	"agentprimordia/internal/agent/worldmodel"
	"agentprimordia/internal/config"
	"agentprimordia/internal/health"
	"agentprimordia/internal/memory"
	"os"
)

// Agent 是所有 Agent 实现的核心接口，编排模式和 Pool 均面向此接口编程
type Agent = agent.Agent

// ReActAgent 是基于 ReAct（推理+行动）循环的 Agent 实现
type ReActAgent = agent.ReActAgent

// ReActConfig 是 ReActAgent 的配置结构，包含模型、系统提示词、温度等核心参数
//
// Stability: Stable
type ReActConfig = agent.ReActConfig

// PromptTemplate 支持 {{.Variable}} 格式的系统提示词模板，可自动注入 Agent 名称、权限规则等变量
type PromptTemplate = agent.PromptTemplate

// AgentStatus 表示 Agent 的当前运行状态
type AgentStatus = agent.AgentStatus

// InputGuard 输入端护栏检查函数（v3.4-4）：用户输入进入循环前调用
type InputGuard = agent.InputGuard

// AgentStats 提供 Agent 运行时的统计信息，包括状态、轮次、tool调用次数等
type AgentStats = agent.AgentStats

// Message 表示对话中的一条消息，包含角色、内容和可选的tool调用
type Message = agent.Message

// Role 表示消息发送者的角色（system / user / assistant / tool）
type Role = agent.Role

// ContentPart 是多模态消息的内容片段（文本/图片URL/Base64图片/音频/视频）
type ContentPart = agent.ContentPart

// ToolCall 表示 LLM 发起的tool调用请求，包含调用 ID、tool名称和 JSON 参数
type ToolCall = agent.ToolCall

// Thought 表示 LLM 的推理输出，包含文本内容和可选的tool调用列表
type Thought = agent.Thought

// Response 表示 Agent 的最终响应，包含内容、tool调用、用量和指标
type Response = agent.Response

// AgentUsage 表示 LLM 调用的 Token 用量统计
type AgentUsage = agent.Usage

// AgentMetrics 表示 Agent 运行期间的性能指标，包括轮次、tool数、延迟等
type AgentMetrics = agent.Metrics

// MemoryStore 是 Agent 所需的记忆存储接口，由 memory.Memory 通过适配器实现
type MemoryStore = agent.MemoryStore

// SummaryExtractor 是摘要提取接口，供 Agent 层依赖，由 memory.Summarizer 实现
type SummaryExtractor = memory.SummaryExtractor

// SummaryResult 是摘要提取的结果，包含摘要文本和主题标签
type SummaryResult = memory.SummaryResult

// Summarizer 使用 LLM 从内容中提取摘要和标签
type Summarizer = memory.Summarizer

// EventPublisher 是 Agent 所需的事件发布接口，用于异步发布生命周期事件
type EventPublisher = agent.EventPublisher

// MetricsRecorder 是 Agent 所需的指标记录接口，用于收集 LLM 调用、tool调用等指标
type MetricsRecorder = agent.MetricsRecorder

// StreamEvent 是流式输出的事件，包含事件类型、内容和附加数据
type StreamEvent = agent.StreamEvent

// StreamEventType 标识流式事件的类型（token / thought / tool_call / tool_result / complete / error）
type StreamEventType = agent.StreamEventType

// MessageBus 是统一消息总线接口，支持 Agent 间的点对点消息发送和广播
type MessageBus = agent.MessageBus

// LocalMessageBus 是进程内消息总线实现，支持注册、发送、广播和订阅
type LocalMessageBus = agent.LocalMessageBus

// BusMessage 是统一消息结构，包含发送方、接收方、类型、内容和元数据
type BusMessage = agent.BusMessage

// BusMessageType 是统一消息类型，覆盖任务请求/结果、查询/响应、交接、广播、状态更新、通知
type BusMessageType = agent.BusMessageType

// BusMessageHandler 是消息处理函数类型，接收消息并返回响应
type BusMessageHandler = agent.BusMessageHandler

// RAGDocument 是 RAG 检索返回的文档片段，包含 ID、内容、相关度分数和来源
type RAGDocument = agent.RAGDocument

// RAGProvider 是 Agent 可使用的 RAG 检索接口，由 memory.RAGStore 通过适配器实现
type RAGProvider = agent.RAGProvider

// RAGMode 控制 RAG 在 ReAct Loop 中的注入方式（auto / first / on_demand）
type RAGMode = agent.RAGMode

// RAGConfig 配置 RAG 注入行为，包括提供者、模式、TopK、最低分数和上下文模板
type RAGConfig = agent.RAGConfig

// Transport 是跨进程 Agent 通信传输层接口，定义发送、接收、启动和关闭方法
type Transport = agent.Transport

// HTTPTransport 是基于 HTTP 的跨进程 Agent 通信传输层实现
type HTTPTransport = agent.HTTPTransport

const (
	// RoleSystem 表示系统角色消息
	RoleSystem = agent.RoleSystem
	// RoleUser 表示用户角色消息
	RoleUser = agent.RoleUser
	// RoleAssistant 表示助手角色消息
	RoleAssistant = agent.RoleAssistant
	// RoleTool 表示tool角色消息
	RoleTool = agent.RoleTool

	// StatusIdle 表示 Agent 处于空闲状态
	StatusIdle = agent.StatusIdle
	// StatusRunning 表示 Agent 正在运行
	StatusRunning = agent.StatusRunning
	// StatusPaused 表示 Agent 已暂停
	StatusPaused = agent.StatusPaused
	// StatusCompleted 表示 Agent 已完成
	StatusCompleted = agent.StatusCompleted
	// StatusFailed 表示 Agent 执行失败
	StatusFailed = agent.StatusFailed
	// StatusCancelled 表示 Agent 已取消
	StatusCancelled = agent.StatusCancelled

	// StreamEventToken 表示逐 token 输出事件
	StreamEventToken = agent.StreamEventToken
	// StreamEventThought 表示推理/思考事件
	StreamEventThought = agent.StreamEventThought
	// StreamEventToolCall 表示tool调用开始事件
	StreamEventToolCall = agent.StreamEventToolCall
	// StreamEventToolResult 表示tool执行结果事件
	StreamEventToolResult = agent.StreamEventToolResult
	// StreamEventComplete 表示运行完成事件
	StreamEventComplete = agent.StreamEventComplete
	// StreamEventError 表示错误事件
	StreamEventError = agent.StreamEventError

	// RAGModeAuto 在每轮推理前自动查询知识库并注入上下文
	RAGModeAuto = agent.RAGModeAuto
	// RAGModeFirst 仅在第一轮推理前查询知识库
	RAGModeFirst = agent.RAGModeFirst
	// RAGModeOnDemand 仅当 Agent 主动调用 knowledge_search tool时查询
	RAGModeOnDemand = agent.RAGModeOnDemand

	// BusMsgTaskRequest 表示任务请求消息
	BusMsgTaskRequest = agent.BusMsgTaskRequest
	// BusMsgTaskResult 表示任务结果消息
	BusMsgTaskResult = agent.BusMsgTaskResult
	// BusMsgQuery 表示查询消息
	BusMsgQuery = agent.BusMsgQuery
	// BusMsgResponse 表示查询响应消息
	BusMsgResponse = agent.BusMsgResponse
	// BusMsgHandoff 表示 Agent 交接消息
	BusMsgHandoff = agent.BusMsgHandoff
	// BusMsgBroadcast 表示广播消息
	BusMsgBroadcast = agent.BusMsgBroadcast
	// BusMsgStatusUpdate 表示状态更新消息
	BusMsgStatusUpdate = agent.BusMsgStatusUpdate
	// BusMsgNotify 表示通知消息
	BusMsgNotify = agent.BusMsgNotify
)

var (
	// NewPromptTemplate 创建支持变量注入的提示词模板
	NewPromptTemplate = agent.NewPromptTemplate
	// DefaultSystemPrompt 返回默认系统提示词模板，包含 Agent 名称和权限规则占位符
	DefaultSystemPrompt = agent.DefaultSystemPrompt
	// UserMessage 创建用户角色的消息快捷函数
	UserMessage = agent.UserMessage
	// SystemMessage 创建系统角色的消息快捷函数
	SystemMessage = agent.SystemMessage

	// FormatRAGDocuments 将 RAG 文档列表格式化为可注入 Prompt 的上下文字符串
	FormatRAGDocuments = agent.FormatRAGDocuments
	// NewSummarizer 创建基于 LLM 的摘要提取器（接受 SummarizerLLM 接口）
	NewSummarizer = memory.NewSummarizer
	// DefaultCleanupConfig 返回记忆自动清理的默认配置（30 天过期、24 小时间隔、保留 tool 角色）
	DefaultCleanupConfig = memory.DefaultCleanupConfig
	// NewLocalMessageBus 创建本地进程内消息总线实例
	NewLocalMessageBus = agent.NewLocalMessageBus
	// NewHTTPTransport 创建基于 HTTP 的跨进程传输层实例
	NewHTTPTransport = agent.NewHTTPTransport

	// ===== 请求 ID 关联（可观测性） =====

	// NewRequestID 生成唯一的请求 ID（32 字符 hex）
	NewRequestID = agent.NewRequestID
	// WithRequestID 将请求 ID 注入 context
	WithRequestID = agent.WithRequestID
	// RequestIDFromCtx 从 context 中提取请求 ID
	RequestIDFromCtx = agent.RequestIDFromCtx

	// ===== NewAgent 简化入口（推荐） =====

	// NewAgent 是创建 Agent 的推荐入口（v0.7.0 起）。
	// 暴露核心字段（名称、系统提示词、模型），能力通过 Functional Options 注入。
	// 构造后核心能力不可变，返回 (*CapabilityAgent, error)。
	NewAgent = agent.NewAgent

	// ===== 标量 Option =====

	// WithMaxTurns 设置 ReAct 循环的最大迭代次数（默认 50）
	WithMaxTurns = agent.WithMaxTurns
	// WithTemperature 设置 LLM 温度参数（默认 0.0）
	WithTemperature = agent.WithTemperature
	// WithSessionID 设置会话 ID，用于跨轮记忆关联
	WithSessionID = agent.WithSessionID
	// WithPromptTemplate 设置自定义提示词模板
	WithPromptTemplate = agent.WithPromptTemplate

	// ===== 顶层快捷注入 Option =====

	// WithMemory 注入记忆存储
	WithMemory = agent.WithMemory
	// WithToolkit 注入tool注册表
	WithToolkit = agent.WithToolkit
	// WithHooks 注入生命周期钩子
	WithHooks = agent.WithHooks
	// WithRAG 注入检索增强配置
	WithRAG = agent.WithRAG
	// WithTracer 注入追踪器
	WithTracer = agent.WithTracer
	// WithCostTracker 注入成本追踪器
	WithCostTracker = agent.WithCostTracker
	// WithContextWindow 注入上下文窗口裁剪策略
	WithContextWindow = agent.WithContextWindow
	// WithEvents 注入事件发布器
	WithEvents = agent.WithEvents
	// WithMetrics 注入指标记录器
	WithMetrics = agent.WithMetrics
	// WithInputGuard 注入输入端护栏（v3.4-4）：用户输入进入循环前检查
	WithInputGuard = agent.WithInputGuard
	// WithCheckpointStore 注入检查点存储
	WithCheckpointStore = agent.WithCheckpointStore
	// WithSummarizer 注入摘要提取器
	WithSummarizer = agent.WithSummarizer
	// WithFileScope 注入文件访问范围
	WithFileScope = agent.WithFileScope
	// WithCache 注入 LLM 缓存
	WithCache = agent.WithCache
	// WithHITL 注入 Human-in-the-Loop 配置
	WithHITL = agent.WithHITL
	// WithLearning 注入自适应学习配置（知识蒸馏/能力进化/反馈学习）
	WithLearning = agent.WithLearning
	// WithPlanner 注入任务规划器（首轮自动分解复杂任务为子任务 DAG）
	WithPlanner = agent.WithPlanner
	// WithReflector 注入反思器（完成路径上批评并改进最终输出）
	WithReflector = agent.WithReflector
	// WithReflectionThreshold 设置触发 Reflection 改进的最低严重度
	WithReflectionThreshold = agent.WithReflectionThreshold

	// ===== 分组注入 Option =====

	// WithMemoryConfig 一次性注入记忆相关配置
	WithMemoryConfig = agent.WithMemoryConfig
	// WithObservability 一次性注入可观测性配置
	WithObservability = agent.WithObservability
	// WithResilience 一次性注入容错配置
	WithResilience = agent.WithResilience
	// WithToolsConfig 一次性注入tool配置
	WithToolsConfig = agent.WithToolsConfig
	// WithCognition 一次性注入认知能力配置（Planner / Reflector / 阈值）
	WithCognition = agent.WithCognition
	// WithWorldModel 启用世界模型状态图（v6.1 opt-in，默认关闭；提案 §2.1）
	//
	// Stability: Experimental
	WithWorldModel = agent.WithWorldModel
)

// AgentOption 是 NewAgent 的函数式选项类型（v0.7.0 起等同于 agent.Option）
//
// 注意：pkg/options.go 中另有一个 Option 类型（func(*options)）用于 ApplyOptions，
// 与此处的 AgentOption 不同。NewAgent 接受的是 AgentOption。
type AgentOption = agent.AgentOption

// ===== v0.7.0 分组配置 struct =====

// AgentConfig 是 Agent 的完整配置，构造后不可变。
// 用户不直接构造此 struct，而是通过 NewAgent + Option 函数填充。
type AgentConfig = agent.AgentConfig

// MemoryConfig 记忆能力分组配置（Store / Summarizer / FileScope）
type MemoryConfig = agent.MemoryConfig

// ObservabilityConfig 可观测性分组配置（Hooks / Tracer / Metrics / Events / CostTracker）
type ObservabilityConfig = agent.ObservabilityConfig

// ResilienceConfig 容错分组配置（CheckpointStore / HITL / Cache / ContextWindow）
type ResilienceConfig = agent.ResilienceConfig

// ToolsConfig tool系统分组配置（Registry）
type ToolsConfig = agent.ToolsConfig

// HITLConfig 人机协作配置
type HITLConfig = agent.HITLConfig

// LearningConfig 自适应学习分组配置（Distiller / Evolver / FeedbackLearner）
type LearningConfig = agent.LearningConfig

// CognitionConfig 认知能力分组配置（Planner / Reflector / ReflectionSeverityThreshold / WorldModel）
type CognitionConfig = agent.CognitionConfig

// WorldModelTracker 世界模型跟踪器（v6.1 opt-in）。
//
// Stability: Experimental
//
// v6.1 新增：把任务世界增量维护为结构化状态图（实体/关系/因果算子）。
// 经 ap.WithWorldModel 显式注入，默认关闭（铁律 7：不注入时默认 ReAct
// 行为零变更）；「评价线默认开」「翻默认」分属三段式默认策略第二/三段，
// 见 agentprimordia/docs/提案-世界模型默认策略切换.md，翻默认锁 v7.0。
type WorldModelTracker = worldmodel.WorldModelTracker

// ===== 协议式微内核：Capable 接口 + CapabilityAgent =====

// CapabilityAgent 是可组合能力的 Agent 包装器，通过链式 API 按需注入能力
//
// 使用方式：
//
//	agent, _ := ap.NewAgent("my-agent", "you are helpful", provider,
//	    ap.WithMemory(mem), ap.WithRAG(ap.RAGConfig{...}), ap.WithHooks(hooks))
type CapabilityAgent = agent.CapabilityAgent

// Session 维护多轮对话上下文，自动追加历史到记忆。
//
// 使用方式：
//
//	sess := ap.NewSession(agent, mem)
//	resp, _ := sess.Ask(ctx, "你好")
//	resp2, _ := sess.Ask(ctx, "刚才说的是什么？") // 自动关联上下文
type Session = agent.Session

// SessionOption 是 NewSession 的函数式选项
type SessionOption = agent.SessionOption

// SessWithID 设置自定义会话 ID，不传则自动生成。
var SessWithID = agent.SessWithID

// NewSession 创建新会话。
//
// 如果 mem == nil，使用 agent 已配置的记忆存储。如果都没有，历史消息仅在内存中保留。
var NewSession = agent.NewSession

// MemoryCapable 标识 Agent 具备记忆存储能力，引擎自动保存对话
type MemoryCapable = agent.MemoryCapable

// RAGCapable 标识 Agent 具备 RAG 检索能力，引擎自动注入知识库上下文
type RAGCapable = agent.RAGCapable

// HITLCapable 标识 Agent 具备人机协作能力，tool执行前自动检查人类确认
type HITLCapable = agent.HITLCapable

// HookCapable 标识 Agent 具备 Hook 能力，引擎自动触发注册的 Hook 函数
type HookCapable = agent.HookCapable

// TraceCapable 标识 Agent 具备分布式追踪能力，引擎自动创建 Span
type TraceCapable = agent.TraceCapable

// CostCapable 标识 Agent 具备成本追踪能力，引擎自动记录 LLM Usage
type CostCapable = agent.CostCapable

// ContextWindowCapable 标识 Agent 具备上下文窗口裁剪能力
type ContextWindowCapable = agent.ContextWindowCapable

// EventCapable 标识 Agent 具备事件发布能力
type EventCapable = agent.EventCapable

// MetricsCapable 标识 Agent 具备指标记录能力
type MetricsCapable = agent.MetricsCapable

// CheckpointCapable 标识 Agent 具备检查点持久化能力
type CheckpointCapable = agent.CheckpointCapable

// SummarizerCapable 标识 Agent 具备摘要提取能力
type SummarizerCapable = agent.SummarizerCapable

// FileScopeCapable 标识 Agent 具备文件作用域限制能力
type FileScopeCapable = agent.FileScopeCapable

// CacheCapable 标识 Agent 具备 LLM 缓存能力
type CacheCapable = agent.CacheCapable

// LearningCapable 标识 Agent 具备自适应学习能力，引擎在完成时自动蒸馏知识
type LearningCapable = agent.LearningCapable

// ===== DAG 工作流引擎 =====

// DAGWorkflow 是 DAG 工作流引擎，支持拓扑排序、并行执行、条件分支和重试
type DAGWorkflow = agent.DAGWorkflow

// DAGNode 是 DAG 工作流节点
type DAGNode = agent.DAGNode

// DAGEdge 是 DAG 工作流边（含条件谓词）
type DAGEdge = agent.DAGEdge

// DAGNodeResult 是节点执行结果
type DAGNodeResult = agent.DAGNodeResult

// DAGResult 是 DAG 工作流执行结果
type DAGResult = agent.DAGResult

// DAGMetrics 是 DAG 执行指标
type DAGMetrics = agent.DAGMetrics

// NodeExecutionStats 是单节点执行统计
type NodeExecutionStats = agent.NodeExecutionStats

// DAGBuilder 是声明式 DAG 构建器，提供链式 API
type DAGBuilder = agent.DAGBuilder

// NodeHandler 是节点处理函数类型
type NodeHandler = agent.NodeHandler

// NodePair 是节点定义对（ID + Handler），用于 Sequential/Parallel
type NodePair = agent.NodePair

// ===== 子任务/Agent 委派 =====

// AgentDelegateNode 将 Agent 实例包装为 DAG 节点
type AgentDelegateNode = agent.AgentDelegateNode

// SubWorkflowNode 将子工作流包装为 DAG 节点
type SubWorkflowNode = agent.SubWorkflowNode

var (
	NewDAGWorkflow       = agent.NewDAGWorkflow
	NewDAGBuilder        = agent.NewDAGBuilder
	MakeNode             = agent.MakeNode
	NewAgentDelegateNode = agent.NewAgentDelegateNode
	NewSubWorkflowNode   = agent.NewSubWorkflowNode
	MapFromDependent     = agent.MapFromDependent
	MapConcatAll         = agent.MapConcatAll
	MapPassThrough       = agent.MapPassThrough
	MapTemplate          = agent.MapTemplate
	ConditionOnOutput    = agent.ConditionOnOutput
	ConditionOnError     = agent.ConditionOnError
	ConditionOnSuccess   = agent.ConditionOnSuccess
)

// ===== 工作流执行引擎 (Phase 6) =====
//
// WorkflowExecution supports 5 workflow types (linear/conditional/loop/
// parallel-fork-join/state-machine) with conditional branching, looping,
// error recovery (retry/skip/fail/fallback), sub-workflow composition,
// pause/resume/cancel lifecycle, persistent snapshots, and event streaming.

type (
	WorkflowExecution   = agent.WorkflowExecution
	WorkflowConfig      = agent.WorkflowConfig
	WorkflowType        = agent.WorkflowType
	WorkflowStatus      = agent.WorkflowStatus
	WorkflowNode        = agent.WorkflowNode
	WorkflowEvent       = agent.WorkflowEvent
	WorkflowResult      = agent.WorkflowResult
	WorkflowMetrics     = agent.WorkflowMetrics
	ExecutionRecord     = agent.ExecutionRecord
	NodeType            = agent.NodeType
	NodeCondition       = agent.NodeCondition
	NodeConfig          = agent.NodeConfig
	NodeExecutionStatus = agent.NodeExecutionStatus
	Transition          = agent.Transition
	TransitionCondition = agent.TransitionCondition
	ErrorHandling       = agent.ErrorHandling
	WfRetryPolicy       = agent.WfRetryPolicy
)

const (
	LinearWorkflow      = agent.LinearWorkflow
	ConditionalWorkflow = agent.ConditionalWorkflow
	LoopWorkflow        = agent.LoopWorkflow
	ParallelForkJoinWf  = agent.ParallelForkJoin
	StateMachineWf      = agent.StateMachine

	WfStatusPending   = agent.WfStatusPending
	WfStatusRunning   = agent.WfStatusRunning
	WfStatusPaused    = agent.WfStatusPaused
	WfStatusCompleted = agent.WfStatusCompleted
	WfStatusFailed    = agent.WfStatusFailed
	WfStatusCancelled = agent.WfStatusCancelled

	TaskNode      = agent.TaskNode
	ConditionNode = agent.ConditionNode
	ParallelNode  = agent.ParallelNode
	LoopStartNode = agent.LoopStartNode
	LoopEndNode   = agent.LoopEndNode
	FallbackNode  = agent.FallbackNode
	SubWfNode     = agent.SubWfNode

	NodePending   = agent.NodePending
	NodeRunning   = agent.NodeRunning
	NodeCompleted = agent.NodeCompleted
	NodeSkipped   = agent.NodeSkipped
	NodeFailed    = agent.NodeFailed
)

var NewWorkflowExecution = agent.NewWorkflowExecution

// ===== CostTracker 成本追踪 =====

// CostTracker is the LLM cost tracking engine
type CostTracker = agent.CostTracker

// CostRecord is a single cost record for an LLM call
type CostRecord = agent.CostRecord

// BudgetConfig configures cost budget limits and callback
type BudgetConfig = agent.BudgetConfig

// CostSummary is the aggregated cost summary
type CostSummary = agent.CostSummary

// ModelCost is the per-model cost breakdown
type ModelCost = agent.ModelCost

var (
	// NewCostTracker creates a cost tracker with pricing table and optional budget config
	NewCostTracker = agent.NewCostTracker
)

// ===== SummaryEngine 会话摘要 =====

// SummaryEngine orchestrates summarization with a strategy, summarizer, and memory store
type SummaryEngine = memory.SummaryEngine

// SummaryStrategy is the interface for summarization strategies
type SummaryStrategy = memory.SummaryStrategy

// WindowSummaryStrategy triggers summarization when episode count exceeds window size
type WindowSummaryStrategy = memory.WindowSummaryStrategy

var (
	// NewSummaryEngine creates a summary engine with strategy, summarizer, and memory store
	NewSummaryEngine = memory.NewSummaryEngine
	// NewWindowSummaryStrategy creates a window-based summary strategy
	NewWindowSummaryStrategy = memory.NewWindowSummaryStrategy
)

// ===== 生命周期与上下文窗口策略 =====

// Lifecycle 管理 Agent 的生命周期状态转换（idle / running / paused / stopped / completed / failed / cancelled）
type Lifecycle = agent.Lifecycle

// ContextWindowStrategy 是上下文窗口裁剪策略接口，定义如何裁剪过长的历史消息
type ContextWindowStrategy = agent.ContextWindowStrategy

// DefaultStrategy 是默认的上下文窗口裁剪策略，保留系统消息和最近的对话历史
type DefaultStrategy = agent.DefaultStrategy

// TokenBudgetStrategy 是 token 预算裁剪策略（v5.1）：按 token 预算裁剪历史并
// 始终保留系统消息，对齐 TS SDK 的 TokenBudgetStrategy。相比计数窗口直接约束
// LLM 输入规模，同任务集 token 成本降幅可量化。
//
// Stability: Stable
// Since: 5.1.0
type TokenBudgetStrategy = agent.TokenBudgetStrategy

var (
	// NewLifecycle 创建 Agent 生命周期管理器实例
	NewLifecycle = agent.NewLifecycle
	// NewDefaultStrategy 创建默认上下文窗口裁剪策略
	NewDefaultStrategy = agent.NewDefaultStrategy
	// NewTokenBudgetStrategy 创建 token 预算裁剪策略；charsPerToken <= 0 时用默认值 4
	//
	// Stability: Stable
	// Since: 5.1.0
	NewTokenBudgetStrategy = agent.NewTokenBudgetStrategy
)

// ===== 健康检查 =====

// HealthChecker 聚合健康检查器，处理 /healthz 和 /readyz 请求
type HealthChecker = health.HealthChecker

// HealthCheckable 健康检查接口，各组件实现此接口以注册到聚合检查器
type HealthCheckable = health.Checker

// NewHealthChecker 创建健康检查器
var NewHealthChecker = health.NewChecker

// PProfHandler 返回一个包含 pprof 端点的独立 http.Handler。
// 适用于仅需暴露 profiling 而无需自定义路由的场景。
var PProfHandler = health.PProfHandler

// RegisterPProfSecure 将 pprof 端点注册到给定的 http.ServeMux，
// 并通过 Bearer Token 鉴权保护。
// 若环境变量 PPROF_TOKEN 未设置则无鉴权（开发模式），生产环境必须设置。
var RegisterPProfSecure = health.RegisterPProfSecure

// PProfHandlerSecure 返回一个包含 pprof 端点的独立 http.Handler，
// 并启用 Bearer Token 鉴权。
var PProfHandlerSecure = health.PProfHandlerSecure

// RegisterPProfStrict 将 pprof 端点注册到给定的 http.ServeMux，
// 强制要求环境变量 PPROF_TOKEN 已设置，否则返回 ErrPProfTokenRequired。
// 生产环境推荐使用此版本（fail-fast，不允许开发模式回退）。
var RegisterPProfStrict = health.RegisterPProfStrict

// PProfHandlerStrict 返回一个包含 pprof 端点的独立 http.Handler，
// 强制要求 PPROF_TOKEN 已设置，否则返回错误。
var PProfHandlerStrict = health.PProfHandlerStrict

// ErrPProfTokenRequired 当生产模式下 PPROF_TOKEN 未设置时返回此错误。
var ErrPProfTokenRequired = health.ErrPProfTokenRequired

// ===== 配置加载 =====

// NewConfigLoader 创建统一配置加载器（YAML < ENV < flags）。
// 用法：
//
//	cfg := &MyConfig{}
//	err := ap.NewConfigLoader(cfg, "AP").
//	    LoadYAML(".ap.yaml").
//	    LoadEnv().
//	    LoadFlags().
//	    Validate()
var NewConfigLoader = config.New

// ===== 版本与通用类型 =====

// Version 是 AgentPrimordia 框架的当前版本号
// 与 版本规范.md 和 Release Notes 保持一致（当前 7.1.0）
const Version = "7.1.0"

// Metadata 是消息的元数据，包含时间戳、跟踪 ID 和扩展键值对
type Metadata = agent.Metadata

// ===== 检查点恢复辅助函数 =====
//
// 当 Agent 二进制被 `ap loop resume` 启动时，会通过环境变量注入恢复信息。
// 在 main() 中调用 ShouldResume() 判断是否需要恢复，若是则调用
// agent.ResumeFromCheckpoint(ctx) 即可无缝接续之前的执行状态。
//
// 示例:
//
//	if ap.ShouldResume() {
//	    agentID := ap.ResumeAgentID()
//	    // 按 agentID 找到对应 Agent 实例
//	    if err := myAgent.ResumeFromCheckpoint(ctx); err != nil {
//	        log.Fatal(err)
//	    }
//	    return
//	}

// ShouldResume 检查环境变量 AP_RESUME 是否为 "1"。
// 由 `ap loop resume` 注入，用于 Agent 二进制在启动时判断是否需要从检查点恢复。
func ShouldResume() bool {
	return os.Getenv("AP_RESUME") == "1"
}

// ResumeAgentID 返回需要通过 `ap loop resume` 恢复的 Agent 名称。
// 对应环境变量 AP_RESUME_AGENT。若未设置则返回空字符串。
func ResumeAgentID() string {
	return os.Getenv("AP_RESUME_AGENT")
}

// ResumePrompt 返回 `ap loop resume` 通过 --prompt 参数传入的提示消息。
// 对应环境变量 AP_PROMPT。若未设置则返回空字符串。
func ResumePrompt() string {
	return os.Getenv("AP_PROMPT")
}
