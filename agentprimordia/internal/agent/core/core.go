// Package core 提供 Agent 包的核心共享类型。
// 这些类型被 agent 父包及其所有子包（react, hooks, dag, workflow 等）共用，
// 放在独立子包中可避免子包导入父包造成的循环依赖。
package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/lifecycle"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/multimodal"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm"
)

// ===== 请求 ID 关联 =====

// requestIDKey 是 context 中存储请求 ID 的 key
type requestIDKey struct{}

// NewRequestID 生成唯一的请求 ID（16 字节随机 hex）
func NewRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// WithRequestID 将请求 ID 注入 context
func WithRequestID(ctx context.Context, reqID string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, reqID)
}

// RequestIDFromCtx 从 context 中提取请求 ID，若不存在返回空字符串
func RequestIDFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(requestIDKey{}).(string)
	return v
}

// ===== RAG 接口 =====

// RAGDocument 是 RAG 检索返回的文档片段
type RAGDocument struct {
	ID      string  `json:"id"`
	Content string  `json:"content"`
	Score   float32 `json:"score"`
	Source  string  `json:"source,omitempty"` // "fts" 和/或 "vector"
	Role    string  `json:"role,omitempty"`   // 原始角色 (user/assistant)
}

// RAGProvider 是 Agent 可使用的 RAG 检索接口
// 由 memory.RAGStore 通过 pkg/adapters 适配实现
type RAGProvider interface {
	// Search 执行 RAG 检索，query 为查询文本，topK 为返回结果数
	Search(ctx context.Context, query string, topK int) ([]*RAGDocument, error)
}

// RAGMode 控制 RAG 在 ReAct Loop 中的注入方式
type RAGMode string

const (
	// RAGModeAuto 在每轮推理前自动查询知识库并注入上下文
	RAGModeAuto RAGMode = "auto"
	// RAGModeFirst 仅在第一轮推理前查询知识库
	RAGModeFirst RAGMode = "first"
	// RAGModeOnDemand 仅当 Agent 主动调用 knowledge_search tool时查询
	RAGModeOnDemand RAGMode = "on_demand"
)

// RAGConfig 配置 RAG 注入行为
type RAGConfig struct {
	// Provider RAG 检索提供者
	Provider RAGProvider

	// Mode 注入模式，默认 auto
	Mode RAGMode

	// TopK 每次检索返回的最大文档数，默认 5
	TopK int

	// MinScore 最低相关度阈值，低于此值的结果将被过滤，默认 0.3
	MinScore float32

	// ContextTemplate 上下文注入模板，默认使用 FormatRAGContext
	// 可用占位符: {{context}}
	ContextTemplate string
}

// FormatRAGDocuments 将 RAG 检索结果格式化为可注入 Prompt 的上下文文本
func FormatRAGDocuments(docs []*RAGDocument) string {
	if len(docs) == 0 {
		return ""
	}
	result := "=== 相关知识 ===\n"
	for i, doc := range docs {
		role := doc.Role
		if role == "" {
			role = "知识"
		}
		result += fmt.Sprintf("[%d | 相关度: %.2f | %s] %s\n", i+1, doc.Score, role, doc.Content)
	}
	result += "=== 知识结束 ===\n"
	return result
}

// RAGContextForPrompt 将单个 RAGDocument 格式化为 Prompt 上下文
func (d *RAGDocument) RAGContextForPrompt() string {
	role := d.Role
	if role == "" {
		role = "知识"
	}
	return fmt.Sprintf("[相关度: %.2f | %s] %s", d.Score, role, d.Content)
}

// ===== Agent 核心接口 =====

// Agent 是所有 Agent 实现的核心接口
// 编排模式（Pipeline/Handoff/Parallel）和 Pool 均面向此接口编程
type Agent interface {
	// Run 执行同步推理，接收 Message 输入并返回 Response
	Run(ctx context.Context, input Message) (*Response, error)
	// StreamRun 执行流式推理，返回 StreamEvent 通道
	StreamRun(ctx context.Context, input Message) (<-chan StreamEvent, error)
	// Stop 停止当前运行
	Stop()
	// Stats 返回运行统计
	Stats() AgentStats
	// Name 返回 Agent 名称
	Name() string
}

// Role represents the role of a message sender
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message represents a single message in the conversation
type Message struct {
	Role         Role                     `json:"role"`
	Content      string                   `json:"content"`
	ContentParts []multimodal.ContentPart `json:"content_parts,omitempty"`
	ToolCalls    []ToolCall               `json:"tool_calls,omitempty"`
	Metadata     Metadata                 `json:"metadata,omitempty"`
}

// HasMultimodal 判断消息是否包含多模态内容
func (m *Message) HasMultimodal() bool {
	for _, p := range m.ContentParts {
		if p.Type != "text" {
			return true
		}
	}
	return false
}

// TextContent 提取纯文本内容
func (m *Message) TextContent() string {
	if m.Content != "" && len(m.ContentParts) == 0 {
		return m.Content
	}
	result := ""
	for _, p := range m.ContentParts {
		if p.Type == "text" && p.Text != "" {
			if result != "" {
				result += " "
			}
			result += p.Text
		}
	}
	if result == "" {
		return m.Content
	}
	return result
}

// Metadata carries additional message information
type Metadata struct {
	SessionID string            `json:"session_id,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
	Extra     map[string]string `json:"extra,omitempty"`
}

// UserMessage creates a user message helper
func UserMessage(content string) Message {
	return Message{
		Role:     RoleUser,
		Content:  content,
		Metadata: Metadata{Timestamp: time.Now()},
	}
}

// SystemMessage creates a system message helper
func SystemMessage(content string) Message {
	return Message{
		Role:     RoleSystem,
		Content:  content,
		Metadata: Metadata{Timestamp: time.Now()},
	}
}

// ToolCall represents a function call request from LLM
type ToolCall struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Args string `json:"args"` // JSON-encoded arguments
}

// ToolResult represents the result of executing a tool
type ToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Content    string `json:"content"`
	IsError    bool   `json:"is_error"`
}

// ToMessage converts ToolResult to a Message with RoleTool
func (tr *ToolResult) ToMessage() Message {
	extra := map[string]string{"tool_call_id": tr.ToolCallID}
	if tr.IsError {
		extra["is_error"] = "true"
	}
	return Message{
		Role:    RoleTool,
		Content: tr.Content,
		Metadata: Metadata{
			Extra: extra,
		},
	}
}

// Thought represents the LLM's reasoning output
type Thought struct {
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	Usage     llm.Usage  `json:"usage,omitempty"`
}

// Response represents the final response from an Agent
type Response struct {
	RequestID string     `json:"request_id"`
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	Usage     Usage      `json:"usage"`
	Metrics   Metrics    `json:"metrics"`
	Error     error      `json:"-"`
}

// ErrorCode 返回结构化错误码，若无错误返回空字符串
func (r *Response) ErrorCode() string {
	if r.Error == nil {
		return ""
	}
	type coded interface{ Code() string }
	var c coded
	if errors.As(r.Error, &c) {
		return c.Code()
	}
	return "UNKNOWN"
}

// Usage tracks token usage
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Metrics tracks performance metrics
type Metrics struct {
	TotalTurns  int           `json:"total_turns"`
	TotalTools  int           `json:"total_tools_called"`
	Duration    time.Duration `json:"duration"`
	LLMLatency  time.Duration `json:"llm_latency_ms"`
	ToolLatency time.Duration `json:"tool_latency_ms"`
	// v3.6-3：本次运行是否命中跨任务记忆 fast-path
	MemoryHit bool `json:"memory_hit"`
}

// AgentStats provides runtime statistics about an agent
type AgentStats struct {
	Status        lifecycle.Status `json:"status"`
	RequestID     string           `json:"request_id,omitempty"`
	CurrentTurn   int              `json:"current_turn"`
	TotalMessages int              `json:"total_messages"`
	ToolsCalled   map[string]int   `json:"tools_called"`
	StartTime     time.Time        `json:"start_time"`
	// v3.6-1：自愈记录——plan 失败自动换路径/降级的明细
	PlanRecoveries []PlanRecovery `json:"plan_recoveries,omitempty"`
	// v3.6-2：流程修正——命中高频失败模式被自动规避的 tool 调用次数
	ProcessCorrections int `json:"process_corrections"`
	// v3.6-3：跨任务记忆命中——相似任务直接复用已解答案（fast-path）的次数
	MemoryHits int `json:"memory_hits"`
}

// PlanRecovery 记录一次自愈动作（v3.6-1）。
type PlanRecovery struct {
	// SubtaskID 触发自愈的子任务 ID（plan 级降级时为空）。
	SubtaskID string `json:"subtask_id,omitempty"`
	// Method 自愈方式：replan / degrade。
	Method string `json:"method"`
	// Success 自愈是否成功（换路径后请求正常完成）。
	Success bool `json:"success"`
	// Error 触发自愈的错误。
	Error string `json:"error,omitempty"`
	// Timestamp 自愈发生时间。
	Timestamp time.Time `json:"timestamp"`
}

// ===== 流式事件 =====

// StreamEventType 标识流式事件的类型
type StreamEventType string

const (
	StreamEventToken      StreamEventType = "token"       // 逐 token 输出
	StreamEventThought    StreamEventType = "thought"     // 思考/推理
	StreamEventToolCall   StreamEventType = "tool_call"   // tool调用开始
	StreamEventToolResult StreamEventType = "tool_result" // tool执行结果
	StreamEventComplete   StreamEventType = "complete"    // 运行完成
	StreamEventError      StreamEventType = "error"       // 错误
)

// StreamEvent 是流式输出的事件
type StreamEvent struct {
	Type      StreamEventType `json:"type"`
	RequestID string          `json:"request_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	Data      any             `json:"data,omitempty"`
}

// ===== 事件常量 =====

const (
	EventAgentStart  = "agent.start"
	EventAgentStop   = "agent.stop"
	EventAgentPanic  = "agent.panic"
	EventAgentError  = "agent.error"
	EventAgentResume = "agent.resume"
	EventTurnStart   = "turn.start"
	EventTurnEnd     = "turn.end"
	EventLLMCall     = "llm.call"
	EventLLMResponse = "llm.response"
	EventToolCall    = "tool.call"
	EventToolResult  = "tool.result"
)
