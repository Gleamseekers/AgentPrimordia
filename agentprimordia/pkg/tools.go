// Stability: 混合 —
//
//	内置tool（Tool / ToolRegistry / FileSystem / Shell / Web / KnowledgeSearch）
//	与 ScopePolicy: Stable。
//	MCP 客户端（MCPClient / MCPRegistry）: Experimental（协议仍在演进）。
//	插件（ToolPlugin / PluginLoader / PluginInfo）: Experimental。
package ap

import (
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/concurrency"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/tools"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/tools/builtin"
)

// Tool 是所有tool必须实现的接口，定义名称、描述、参数和执行方法
type Tool = tools.Tool

// ToolResult 是tool执行的结果，包含内容、错误标记和元数据
type ToolResult = tools.Result

// ToolRegistry 是tool注册中心，管理tool的注册、查找和权限配置
type ToolRegistry = tools.Registry

// ToolPermission 定义tool的访问控制规则，包括允许角色、禁止路径和确认回调
type ToolPermission = tools.Permission

// ToolExecutor 是tool执行器，处理tool调用、权限检查、文件锁和超时
type ToolExecutor = tools.Executor

// ToolFunctionCall 表示tool调用请求，包含调用 ID、tool名称和 JSON 参数
type ToolFunctionCall = tools.FunctionCall

// ScopePolicy 是作用域权限策略接口，定义 Agent 对资源的访问控制
type ScopePolicy = tools.ScopePolicy

// FileScopePolicy 是基于文件路径的作用域权限策略实现，按 Agent 分配可访问目录
type FileScopePolicy = tools.FileScopePolicy

// FileSystem 是文件系统操作tool，支持读写、搜索目录等操作，可注入权限策略和文件锁
type FileSystem = builtin.FileSystem

// Shell 是命令行执行tool，支持超时、白名单和作用域限制
type Shell = builtin.Shell

// Web 是 HTTP 请求tool，支持 GET/POST 等方法
type Web = builtin.Web

// KnowledgeSearcher 是知识库搜索接口，由外部注入 RAG Provider 实现
type KnowledgeSearcher = builtin.KnowledgeSearcher

// KnowledgeDoc 是知识库搜索返回的文档，包含 ID、内容、分数和来源
type KnowledgeDoc = builtin.KnowledgeDoc

// KnowledgeSearch 是内置知识库搜索tool，允许 Agent 主动查询知识库
type KnowledgeSearch = builtin.KnowledgeSearch

// ToolkitConfig 是tool包配置，包含根目录、启用的tool类型、权限策略和文件锁
type ToolkitConfig = builtin.ToolkitConfig

var (
	// NewToolRegistry 创建tool注册中心实例
	NewToolRegistry = tools.NewRegistry
	// NewToolExecutor 创建tool执行器实例
	NewToolExecutor = tools.NewExecutor
	// NewToolResult 创建tool执行成功结果
	NewToolResult = tools.NewResult
	// NewToolErrorResult 创建tool执行错误结果
	NewToolErrorResult = tools.NewErrorResult
	// NewFileScopePolicy 创建基于文件路径的作用域权限策略
	NewFileScopePolicy = tools.NewFileScopePolicy
	// NewScopeDeniedError 创建作用域拒绝错误
	NewScopeDeniedError = tools.NewScopeDeniedError
	// NewFileSystem 创建文件系统操作tool
	NewFileSystem = builtin.NewFileSystem
	// NewShell 创建命令行执行tool
	NewShell = builtin.NewShell
	// NewWeb 创建 HTTP 请求tool
	NewWeb = builtin.NewWeb
	// NewKnowledgeSearch 创建知识库搜索tool
	NewKnowledgeSearch = builtin.NewKnowledgeSearch
	// DefaultToolkit 创建默认tool包（文件系统 + Shell + Web），需指定根目录
	DefaultToolkit = builtin.DefaultToolkit
	// MinimalToolkit 创建最小tool包（文件系统 + Shell），需指定根目录
	MinimalToolkit = builtin.MinimalToolkit

	// NewMCPClient 创建 MCP 客户端
	NewMCPClient = tools.NewMCPClient
	// NewMCPRegistry 创建 MCP Server 注册中心
	NewMCPRegistry = tools.NewMCPRegistry
	// NewPluginLoader 创建插件加载器
	NewPluginLoader = tools.NewPluginLoader
)

// MCPClient 是 MCP 协议客户端，用于连接外部 MCP Server
// Stability: Experimental — MCP 协议规范仍在演进。
type MCPClient = tools.MCPClient

// MCPRegistry 管理 MCP Server 的注册和生命周期
type MCPRegistry = tools.MCPRegistry

// MCPClientConfig 描述一个外部 MCP Server 的连接配置
type MCPClientConfig = tools.MCPClientConfig

// MCPToolDefinition 描述 MCP 服务端提供的tool
type MCPToolDefinition = tools.MCPToolDefinition

// ToolPlugin 是tool插件接口，支持批量注册tool
// Stability: Experimental — 接口形状可能随插件生态需求调整。
type ToolPlugin = tools.ToolPlugin

// PluginLoader 管理插件的加载、卸载和查询
type PluginLoader = tools.PluginLoader

// PluginInfo 描述已加载插件的信息
type PluginInfo = tools.PluginInfo

// ===== 数据处理tool =====
// Stability: Experimental — 数据处理tool（JSON / CSV / Git / SQLite），API 可能随使用场景调整。

// JSONTool 是 JSON 数据处理tool，支持查询、提取、转换等操作
type JSONTool = tools.JSONTool

// CSVTool 是 CSV 数据处理tool，支持解析、查询、转换等操作
type CSVTool = tools.CSVTool

// SQLiteTool 是 SQLite 数据库tool，支持 query 和 execute 操作
type SQLiteTool = tools.SQLiteTool

// CalculatorTool 是计算器tool，支持加减乘除等基本运算
type CalculatorTool = builtin.CalculatorTool

// DateTimeTool 是日期时间tool，支持获取当前时间和格式化
type DateTimeTool = builtin.DateTimeTool

var (
	// NewJSONTool 创建 JSON 数据处理tool
	NewJSONTool = tools.NewJSONTool
	// NewCSVTool 创建 CSV 数据处理tool
	NewCSVTool = tools.NewCSVTool
	// NewSQLiteTool 创建 SQLite 数据库tool
	NewSQLiteTool = tools.NewSQLiteTool
	// NewCalculator 创建计算器tool
	NewCalculator = builtin.NewCalculator
	// NewDateTime 创建日期时间tool
	NewDateTime = builtin.NewDateTime
)

// ===== v3.0.0 前沿能力：内置tool扩展 =====
// Stability: Experimental — v3.0.0 新增，API 可能随使用场景演进而调整。

// API 是 REST API 调用tool，支持 GET/POST/PUT/DELETE/PATCH 方法，内置 SSRF 防护
type API = builtin.API

// CodeExecution 代码执行tool，支持在安全沙箱中执行 Python、JavaScript、Go 代码
// 注意：默认禁用，需设置 AP_ALLOW_CODE_EXECUTION=true 环境变量启用
type CodeExecution = builtin.CodeExecution

// Database 是 SQLite 数据库tool，支持 query 和 execute 操作
type Database = builtin.Database

// DatabaseOption 是 Database 的可选配置
type DatabaseOption = builtin.DatabaseOption

var (
	// NewAPI 创建新的 API tool实例
	NewAPI = builtin.NewAPI
	// NewCodeExecution 创建代码执行tool实例
	NewCodeExecution = builtin.NewCodeExecution
	// NewDatabase 创建 SQLite 数据库tool
	// dbPath 为数据库文件路径，传 ":memory:" 使用内存数据库
	NewDatabase = builtin.NewDatabase
	// WithReadOnly 设置数据库只读模式
	WithReadOnly = builtin.WithReadOnly
	// WithMaxRows 设置数据库查询结果最大行数
	WithMaxRows = builtin.WithMaxRows
	// WithQueryTimeout 设置数据库查询超时时间
	WithQueryTimeout = builtin.WithQueryTimeout
)

// ===== 文件锁与作用域校验 =====

// FileLockManager 是文件锁管理器，提供跨 Agent 的文件级互斥访问控制
type FileLockManager = concurrency.FileLockManager

var (
	// NewFileLockManager 创建文件锁管理器实例
	NewFileLockManager = concurrency.NewFileLockManager
	// ValidateScopes 验证 Agent 的文件作用域是否存在重叠冲突
	ValidateScopes = concurrency.ValidateScopes
)
