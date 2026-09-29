// Package multimodal 提供多模态消息处理功能
package multimodal

import (
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm"
	"time"
)

// ContentPart 表示消息内容的一部分（文本、图片、音频等）
type ContentPart struct {
	Type   string `json:"type"`             // 内容类型：text, image_url, image_b64, audio, video
	Text   string `json:"text,omitempty"`   // 文本内容
	URL    string `json:"url,omitempty"`    // 图片/视频 URL
	Data   string `json:"data,omitempty"`   // Base64 编码数据
	MIME   string `json:"mime,omitempty"`   // MIME 类型
	Detail string `json:"detail,omitempty"` // 图片细节级别（low/high/auto）
}

// Role 消息角色
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
	RoleTool      Role = "tool"
)

// Message 标准消息结构
type Message struct {
	Role         Role          `json:"role"`
	Content      string        `json:"content"`
	ContentParts []ContentPart `json:"content_parts,omitempty"`
	ToolCalls    []ToolCall    `json:"tool_calls,omitempty"`
	Metadata     Metadata      `json:"metadata,omitempty"`
}

// Metadata 消息元数据
type Metadata struct {
	Timestamp time.Time         `json:"timestamp"`
	Extra     map[string]string `json:"extra,omitempty"`
}

// ToolCall tool调用
type ToolCall struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Args string `json:"args"`
}

// HasMultimodal 检查消息是否包含多模态内容
func (m Message) HasMultimodal() bool {
	return len(m.ContentParts) > 0
}

// MultimodalAdapter 多模态消息适配器
// 将 multimodal.ContentPart 转换为 llm.MultimodalContent
type MultimodalAdapter struct{}

// ToLLMContents 将 multimodal ContentParts 转换为 llm.MultimodalContent 列表
func (a *MultimodalAdapter) ToLLMContents(parts []ContentPart) []*llm.MultimodalContent {
	if len(parts) == 0 {
		return nil
	}

	result := make([]*llm.MultimodalContent, len(parts))
	for i, p := range parts {
		c := &llm.MultimodalContent{}

		switch p.Type {
		case "text":
			c.Type = llm.ContentTypeText
			c.Text = p.Text
		case "image_url":
			c.Type = llm.ContentTypeImageURL
			c.URL = p.URL
			if p.Detail != "" {
				c.Detail = p.Detail
			} else {
				c.Detail = "auto"
			}
		case "image_b64":
			c.Type = llm.ContentTypeImageB64
			c.Data = p.Data
			c.MIME = p.MIME
			if p.Detail != "" {
				c.Detail = p.Detail
			} else {
				c.Detail = "auto"
			}
		case "audio":
			c.Type = llm.ContentTypeAudio
			c.Data = p.Data
			c.MIME = p.MIME
		case "video":
			c.Type = llm.ContentTypeVideo
			c.Data = p.Data
			c.MIME = p.MIME
		default:
			c.Type = llm.ContentTypeText
			c.Text = p.Text
		}

		result[i] = c
	}
	return result
}

// HistoryHasMultimodal 检查历史消息中是否包含多模态内容
func (a *MultimodalAdapter) HistoryHasMultimodal(history []Message) bool {
	for _, m := range history {
		if m.HasMultimodal() {
			return true
		}
	}
	return false
}

// ConvertHistoryToExt 将历史消息转换为 LLM ChatMessageExt 格式
// 如果消息包含 ContentParts，使用多模态格式；否则降级为纯文本
func (a *MultimodalAdapter) ConvertHistoryToExt(history []Message) []*llm.ChatMessageExt {
	msgs := make([]*llm.ChatMessageExt, 0, len(history))
	for _, m := range history {
		ext := &llm.ChatMessageExt{
			Role: string(m.Role),
		}

		if len(m.ContentParts) > 0 {
			ext.Contents = a.ToLLMContents(m.ContentParts)
		} else {
			ext.Contents = []*llm.MultimodalContent{
				llm.NewTextContent(m.Content),
			}
		}

		if m.Role == RoleAssistant && len(m.ToolCalls) > 0 {
			ext.ToolCalls = make([]llm.FunctionCall, len(m.ToolCalls))
			for j, tc := range m.ToolCalls {
				ext.ToolCalls[j] = llm.FunctionCall{
					ID:        tc.ID,
					Name:      tc.Name,
					Arguments: tc.Args,
				}
			}
		}

		if m.Role == RoleTool {
			if id, ok := m.Metadata.Extra["tool_call_id"]; ok {
				ext.ToolCallID = id
			}
			if isError, ok := m.Metadata.Extra["is_error"]; ok && isError == "true" {
				ext.IsToolError = true
			}
		}

		msgs = append(msgs, ext)
	}
	return msgs
}

// UserMultimodalMessage 创建多模态用户消息
func UserMultimodalMessage(parts ...ContentPart) Message {
	return Message{
		Role:         RoleUser,
		ContentParts: parts,
		Metadata:     Metadata{Timestamp: time.Now()},
	}
}

// UserImageMessage 便捷函数：创建图片用户消息
func UserImageMessage(text, imageURL string) Message {
	return Message{
		Role: RoleUser,
		ContentParts: []ContentPart{
			{Type: "text", Text: text},
			{Type: "image_url", URL: imageURL, Detail: "auto"},
		},
		Metadata: Metadata{Timestamp: time.Now()},
	}
}

// MultimodalMessage 多模态消息（扩展 Message 支持图片/音频/视频）
type MultimodalMessage struct {
	Role      Role                     `json:"role"`
	Contents  []*llm.MultimodalContent `json:"contents,omitempty"` // 多模态内容列表
	Content   string                   `json:"content,omitempty"`  // 纯文本内容（向后兼容）
	ToolCalls []ToolCall               `json:"tool_calls,omitempty"`
	Metadata  Metadata                 `json:"metadata,omitempty"`
}

// HasNonTextContent 是否包含非文本内容
func (m *MultimodalMessage) HasNonTextContent() bool {
	for _, c := range m.Contents {
		if c.Type != llm.ContentTypeText {
			return true
		}
	}
	return false
}

// ExtractText 提取所有文本内容
func (m *MultimodalMessage) ExtractText() string {
	if m.Content != "" {
		return m.Content
	}
	result := ""
	for _, c := range m.Contents {
		if c.Text != "" {
			result += c.Text
		}
	}
	return result
}

// ToChatMessageExt 转换为 LLM 多模态消息格式
func (m *MultimodalMessage) ToChatMessageExt() *llm.ChatMessageExt {
	ext := &llm.ChatMessageExt{
		Role:        string(m.Role),
		Contents:    m.Contents,
		ToolCalls:   make([]llm.FunctionCall, len(m.ToolCalls)),
		IsToolError: false,
	}

	if len(m.Contents) == 0 && m.Content != "" {
		ext.Contents = []*llm.MultimodalContent{
			{Type: llm.ContentTypeText, Text: m.Content},
		}
	}

	for i, tc := range m.ToolCalls {
		ext.ToolCalls[i] = llm.FunctionCall{
			ID:        tc.ID,
			Name:      tc.Name,
			Arguments: tc.Args,
		}
	}

	return ext
}

// NewUserMultimodalMessage 创建用户多模态消息
func NewUserMultimodalMessage(contents ...*llm.MultimodalContent) *MultimodalMessage {
	return &MultimodalMessage{
		Role:     RoleUser,
		Contents: contents,
		Metadata: Metadata{Timestamp: time.Now()},
	}
}

// NewAssistantMultimodalMessage 创建助手多模态消息
func NewAssistantMultimodalMessage(contents ...*llm.MultimodalContent) *MultimodalMessage {
	return &MultimodalMessage{
		Role:     RoleAssistant,
		Contents: contents,
		Metadata: Metadata{Timestamp: time.Now()},
	}
}

// NewSystemMultimodalMessage 创建系统多模态消息
func NewSystemMultimodalMessage(contents ...*llm.MultimodalContent) *MultimodalMessage {
	return &MultimodalMessage{
		Role:     RoleSystem,
		Contents: contents,
		Metadata: Metadata{Timestamp: time.Now()},
	}
}

// UserImageB64Message 创建包含 Base64 图片的用户消息（便捷方法）
func UserImageB64Message(text string, imageBase64 string, mimeType string) *MultimodalMessage {
	return NewUserMultimodalMessage(
		&llm.MultimodalContent{Type: llm.ContentTypeText, Text: text},
		&llm.MultimodalContent{Type: llm.ContentTypeImageB64, Data: imageBase64, MIME: mimeType},
	)
}

// UserImageURLMessage 创建包含图片 URL 的用户消息（便捷方法）
func UserImageURLMessage(text string, imageURL string) *MultimodalMessage {
	return NewUserMultimodalMessage(
		&llm.MultimodalContent{Type: llm.ContentTypeText, Text: text},
		&llm.MultimodalContent{Type: llm.ContentTypeImageURL, URL: imageURL},
	)
}

// ToMessage 转换为标准 Message（降级）
func (m *MultimodalMessage) ToMessage() Message {
	msg := Message{
		Role:      m.Role,
		Content:   m.ExtractText(),
		ToolCalls: m.ToolCalls,
		Metadata:  m.Metadata,
	}
	return msg
}

// MultimodalResponse 多模态响应
type MultimodalResponse struct {
	Response
	MultimodalContent *llm.CompletionResponse // 原始多模态响应
}

// Response 标准响应结构
type Response struct {
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	Usage     Usage      `json:"usage,omitempty"`
}

// Usage 令牌使用情况
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// IsMultimodalProvider 检查 LLM Provider 是否支持多模态
func IsMultimodalProvider(p llm.Provider) bool {
	if mp, ok := p.(interface {
		CompleteMultimodal(ctx interface{}, req *llm.CompletionRequestExt) (*llm.CompletionResponse, error)
	}); ok {
		_ = mp
		return true
	}
	return false
}
