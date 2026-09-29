// Stability: Stable — 编排模式（Pipeline / Handoff / Parallel）与文档处理。
package ap

import (
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/memory"
)

// Pipeline 是顺序编排器，前一个 Agent 的输出作为后一个的输入
type Pipeline = agent.Pipeline

// PipelineStep 是 Pipeline 中的一个步骤，包含名称、Agent、输入和条件谓词
type PipelineStep = agent.PipelineStep

// PipelineResult 是 Pipeline 的执行结果，包含各步骤结果、最终输出和总耗时
type PipelineResult = agent.PipelineResult

// StepResult 是 Pipeline 单步骤的执行结果，包含名称、输出、耗时和跳过标记
type StepResult = agent.StepResult

// Handoff 是动态交接编排器，支持 Agent 间根据路由函数自动交接
type Handoff = agent.Handoff

// HandoffConfig 定义 Agent 间的交接规则，包含参与 Agent 列表、路由函数和最大交接次数
type HandoffConfig = agent.HandoffConfig

// HandoffResult 是 Handoff 的执行结果，包含最终 Agent 名称、输出和交接次数
type HandoffResult = agent.HandoffResult

// ParallelResult 是并行执行的结果，包含各 Agent 的结果和总耗时
type ParallelResult = agent.ParallelResult

// AgentResult 是单个 Agent 的执行结果，包含名称、输出、耗时和错误
type AgentResult = agent.AgentResult

var (
	// NewPipeline 创建顺序 Pipeline 编排器
	NewPipeline = agent.NewPipeline
	// NewHandoff 创建动态交接编排器
	NewHandoff = agent.NewHandoff
	// ParallelRun 并行执行多个 Agent 并汇总结果
	ParallelRun = agent.ParallelRun
)

// ===== GroupChat 群组讨论 =====

// GroupChat 是多 Agent 群组讨论管理器，支持轮次发言、终止条件和共识机制
type GroupChat = agent.GroupChat

// GroupChatConfig 是 GroupChat 的配置，包含参与 Agent 列表、最大轮次等
type GroupChatConfig = agent.GroupChatConfig

// GroupChatResult 是 GroupChat 的运行结果，包含消息列表和讨论轮次
type GroupChatResult = agent.GroupChatResult

var (
	// NewGroupChat 创建群组讨论实例
	NewGroupChat = agent.NewGroupChat
)

// Document 是加载后的文档，包含 ID、内容、元数据和来源路径
type Document = memory.Document

// DocumentLoader 是文档加载接口，支持从文件或目录加载文档
type DocumentLoader = memory.DocumentLoader

// TextFileLoader 从文件路径加载文本文档，支持递归加载目录
type TextFileLoader = memory.TextFileLoader

// ReaderLoader 从 io.Reader 加载文本文档
type ReaderLoader = memory.ReaderLoader

// TextSplitter 是文本切分接口，将长文本切分为适合检索的块
type TextSplitter = memory.TextSplitter

// CharacterSplitter 按字符数切分文本，支持重叠和分隔符
type CharacterSplitter = memory.CharacterSplitter

// RecursiveSplitter 递归切分器，按多种分隔符逐级尝试切分
type RecursiveSplitter = memory.RecursiveSplitter

// LineSplitter 按行数切分文本
type LineSplitter = memory.LineSplitter

// DocChunk 是切分后的文本块，包含序号、内容和元数据
type DocChunk = memory.Chunk

// DocumentPipeline 是文档处理管道，串联加载和切分两个阶段
type DocumentPipeline = memory.DocumentPipeline

var (
	// NewTextFileLoader 创建文本文件加载器
	NewTextFileLoader = memory.NewTextFileLoader
	// NewReaderLoader 创建 Reader 加载器
	NewReaderLoader = memory.NewReaderLoader
	// NewCharacterSplitter 创建字符切分器，参数为块大小和重叠大小
	NewCharacterSplitter = memory.NewCharacterSplitter
	// NewRecursiveSplitter 创建递归切分器，参数为块大小和重叠大小
	NewRecursiveSplitter = memory.NewRecursiveSplitter
	// NewLineSplitter 创建行切分器，参数为每块的行数
	NewLineSplitter = memory.NewLineSplitter
	// NewDocumentPipeline 创建文档处理管道，串联加载器和切分器
	NewDocumentPipeline = memory.NewDocumentPipeline
)
