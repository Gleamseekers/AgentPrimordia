package agent

import (
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/multimodal"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
)

// ContentPart 表示消息内容的一部分（文本、图片、音频等）
// 类型别名保持向后兼容
type ContentPart = multimodal.ContentPart

// MultimodalAdapter 多模态消息适配器
// 类型别名保持向后兼容
type MultimodalAdapter = multimodal.MultimodalAdapter

// MultimodalMessage 多模态消息（扩展 Message 支持图片/音频/视频）
// 类型别名保持向后兼容
type MultimodalMessage = multimodal.MultimodalMessage

// MultimodalResponse 多模态响应
// 类型别名保持向后兼容
type MultimodalResponse = multimodal.MultimodalResponse

// UserMultimodalMessage 创建多模态用户消息
func UserMultimodalMessage(parts ...ContentPart) Message {
	mm := multimodal.UserMultimodalMessage(parts...)
	return Message{
		Role:         Role(mm.Role),
		Content:      mm.Content,
		ContentParts: mm.ContentParts,
		Metadata:     Metadata{Timestamp: mm.Metadata.Timestamp, Extra: mm.Metadata.Extra},
	}
}

// UserImageMessage 便捷函数：创建图片用户消息
func UserImageMessage(text, imageURL string) Message {
	mm := multimodal.UserImageMessage(text, imageURL)
	return Message{
		Role:         Role(mm.Role),
		Content:      mm.Content,
		ContentParts: mm.ContentParts,
		Metadata:     Metadata{Timestamp: mm.Metadata.Timestamp, Extra: mm.Metadata.Extra},
	}
}

// NewUserMultimodalMessage 创建用户多模态消息
func NewUserMultimodalMessage(contents ...*llm.MultimodalContent) *MultimodalMessage {
	return multimodal.NewUserMultimodalMessage(contents...)
}

// NewAssistantMultimodalMessage 创建助手多模态消息
func NewAssistantMultimodalMessage(contents ...*llm.MultimodalContent) *MultimodalMessage {
	return multimodal.NewAssistantMultimodalMessage(contents...)
}

// NewSystemMultimodalMessage 创建系统多模态消息
func NewSystemMultimodalMessage(contents ...*llm.MultimodalContent) *MultimodalMessage {
	return multimodal.NewSystemMultimodalMessage(contents...)
}

// UserImageB64Message 创建包含 Base64 图片的用户消息（便捷方法）
func UserImageB64Message(text string, imageBase64 string, mimeType string) *MultimodalMessage {
	return multimodal.UserImageB64Message(text, imageBase64, mimeType)
}

// UserImageURLMessage 创建包含图片 URL 的用户消息（便捷方法）
func UserImageURLMessage(text string, imageURL string) *MultimodalMessage {
	return multimodal.UserImageURLMessage(text, imageURL)
}

// IsMultimodalProvider 检查 LLM Provider 是否支持多模态
func IsMultimodalProvider(p llm.Provider) bool {
	return multimodal.IsMultimodalProvider(p)
}
