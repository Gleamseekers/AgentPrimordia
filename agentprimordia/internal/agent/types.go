// types.go — core 子包的类型别名，保持向后兼容
package agent

import (
	"context"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/core"
)

// ===== 请求 ID 关联 =====

// NewRequestID 生成唯一的请求 ID
// 委托到 core 子包，保持向后兼容
func NewRequestID() string {
	return core.NewRequestID()
}

// WithRequestID 将请求 ID 注入 context
// 委托到 core 子包，保持向后兼容
func WithRequestID(ctx context.Context, reqID string) context.Context {
	return core.WithRequestID(ctx, reqID)
}

// RequestIDFromCtx 从 context 中提取请求 ID
// 委托到 core 子包，保持向后兼容
func RequestIDFromCtx(ctx context.Context) string {
	return core.RequestIDFromCtx(ctx)
}

// ===== RAG 接口 =====

// RAGDocument 是 RAG 检索返回的文档片段
// 类型别名保持向后兼容
type RAGDocument = core.RAGDocument

// RAGProvider 是 Agent 可使用的 RAG 检索接口
// 类型别名保持向后兼容
type RAGProvider = core.RAGProvider

// RAGMode 控制 RAG 在 ReAct Loop 中的注入方式
// 类型别名保持向后兼容
type RAGMode = core.RAGMode

// RAG 模式常量
const (
	RAGModeAuto     = core.RAGModeAuto
	RAGModeFirst    = core.RAGModeFirst
	RAGModeOnDemand = core.RAGModeOnDemand
)

// RAGConfig 配置 RAG 注入行为
// 类型别名保持向后兼容
type RAGConfig = core.RAGConfig

// FormatRAGDocuments 将 RAG 检索结果格式化为可注入 Prompt 的上下文文本
// 委托到 core 子包，保持向后兼容
func FormatRAGDocuments(docs []*RAGDocument) string {
	return core.FormatRAGDocuments(docs)
}

// ===== Agent 核心接口 =====

// Agent 是所有 Agent 实现的核心接口
// 类型别名保持向后兼容
type Agent = core.Agent

// Role represents the role of a message sender
// 类型别名保持向后兼容
type Role = core.Role

// 角色常量
const (
	RoleSystem    = core.RoleSystem
	RoleUser      = core.RoleUser
	RoleAssistant = core.RoleAssistant
	RoleTool      = core.RoleTool
)

// Message represents a single message in the conversation
// 类型别名保持向后兼容
type Message = core.Message

// Metadata carries additional message information
// 类型别名保持向后兼容
type Metadata = core.Metadata

// UserMessage creates a user message helper
// 委托到 core 子包，保持向后兼容
func UserMessage(content string) Message {
	return core.UserMessage(content)
}

// SystemMessage creates a system message helper
// 委托到 core 子包，保持向后兼容
func SystemMessage(content string) Message {
	return core.SystemMessage(content)
}

// ToolCall represents a function call request from LLM
// 类型别名保持向后兼容
type ToolCall = core.ToolCall

// ToolResult represents the result of executing a tool
// 类型别名保持向后兼容
type ToolResult = core.ToolResult

// Thought represents the LLM's reasoning output
// 类型别名保持向后兼容
type Thought = core.Thought

// Response represents the final response from an Agent
// 类型别名保持向后兼容
type Response = core.Response

// Usage tracks token usage
// 类型别名保持向后兼容
type Usage = core.Usage

// Metrics tracks performance metrics
// 类型别名保持向后兼容
type Metrics = core.Metrics

// AgentStats provides runtime statistics about an agent
// 类型别名保持向后兼容
type AgentStats = core.AgentStats

// PlanRecovery 记录一次自愈动作（v3.6-1，agent 包别名）。
type PlanRecovery = core.PlanRecovery

// ===== 流式事件 =====

// StreamEventType 标识流式事件的类型
// 类型别名保持向后兼容
type StreamEventType = core.StreamEventType

// 流式事件类型常量
const (
	StreamEventToken      = core.StreamEventToken
	StreamEventThought    = core.StreamEventThought
	StreamEventToolCall   = core.StreamEventToolCall
	StreamEventToolResult = core.StreamEventToolResult
	StreamEventComplete   = core.StreamEventComplete
	StreamEventError      = core.StreamEventError
)

// StreamEvent 是流式输出的事件
// 类型别名保持向后兼容
type StreamEvent = core.StreamEvent
