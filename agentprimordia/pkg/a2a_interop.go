// Package ap — A2A 开放协议互操作公共 API 导出（v3.5）。
//
// Stability: Experimental
//
// v3.5 将 JSON-RPC over HTTP/SSE 重新定位为开放 Agent2Agent 协议的标准传输
// （不再标记移除），gRPC 继续作为 ap 内网高性能传输，两者并行。
// 本文件导出开放协议互操作所需的 schema 类型、服务器/客户端与兼容性报告工具。
package ap

import (
	"agentprimordia/internal/agent/a2a"
)

// ===== 开放协议 Schema 类型 =====

// OpenAgentCard 开放规范 Agent Card
type OpenAgentCard = a2a.OpenAgentCard

// OpenCapabilities 开放规范能力声明
type OpenCapabilities = a2a.OpenCapabilities

// OpenSkillDecl 开放规范技能声明
type OpenSkillDecl = a2a.OpenSkillDecl

// OpenMessage 开放规范消息
type OpenMessage = a2a.OpenMessage

// OpenPart 开放规范消息部分
type OpenPart = a2a.OpenPart

// OpenTask 开放规范任务
type OpenTask = a2a.OpenTask

// OpenTaskState 开放规范任务状态
type OpenTaskState = a2a.OpenTaskState

// OpenError 开放规范错误
type OpenError = a2a.OpenError

// OpenInteropServer 开放协议兼容服务器
type OpenInteropServer = a2a.OpenInteropServer

// OpenInteropClient 开放协议客户端
type OpenInteropClient = a2a.OpenInteropClient

// TaskExecutor 任务执行器接口
type TaskExecutor = a2a.TaskExecutor

// EchoTaskExecutor 回声测试执行器
type EchoTaskExecutor = a2a.EchoTaskExecutor

// SimpleTaskExecutor 简单任务执行器
type SimpleTaskExecutor = a2a.SimpleTaskExecutor

// InteropConfig 互操作配置
type InteropConfig = a2a.InteropConfig

// InteropReport 协议符合性报告
type InteropReport = a2a.InteropReport

// InteropCheck 单项符合性检查
type InteropCheck = a2a.InteropCheck

// IOMode 输入输出模式
type IOMode = a2a.IOMode

// IOModeConfig 输入输出模式配置
type IOModeConfig = a2a.IOModeConfig

// ===== 任务状态常量 =====

const (
	// OpenTaskSubmitted 已提交
	OpenTaskSubmitted = a2a.OpenTaskSubmitted
	// OpenTaskWorking 处理中
	OpenTaskWorking = a2a.OpenTaskWorking
	// OpenTaskCompleted 已完成
	OpenTaskCompleted = a2a.OpenTaskCompleted
	// OpenTaskCanceled 已取消
	OpenTaskCanceled = a2a.OpenTaskCanceled
	// OpenTaskFailed 失败
	OpenTaskFailed = a2a.OpenTaskFailed
)

// ===== 互操作模式常量 =====

const (
	// InteropStrict 严格模式：仅开放协议
	InteropStrict = a2a.InteropStrict
	// InteropCompatible 兼容模式：开放 + 私有扩展
	InteropCompatible = a2a.InteropCompatible
)

// ===== 构造器与工具 =====

var (
	// NewTextMessage 创建文本消息
	NewTextMessage = a2a.NewTextMessage
	// NewOpenError 创建开放协议错误
	NewOpenError = a2a.NewOpenError
	// DefaultInteropConfig 默认互操作配置
	DefaultInteropConfig = a2a.DefaultInteropConfig
	// DefaultIOModeConfig 默认输入输出模式配置
	DefaultIOModeConfig = a2a.DefaultIOModeConfig
	// NewOpenInteropServer 创建开放协议服务器
	NewOpenInteropServer = a2a.NewOpenInteropServer
	// NewOpenInteropClient 创建开放协议客户端
	NewOpenInteropClient = a2a.NewOpenInteropClient
	// NewEchoTaskExecutor 创建回声测试执行器
	NewEchoTaskExecutor = a2a.NewEchoTaskExecutor
	// NewSimpleTaskExecutor 创建简单任务执行器
	NewSimpleTaskExecutor = a2a.NewSimpleTaskExecutor
	// GenerateInteropReport 生成协议符合性报告
	GenerateInteropReport = a2a.GenerateInteropReport
)
