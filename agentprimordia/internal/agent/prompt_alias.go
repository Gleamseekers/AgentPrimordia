// prompt_alias.go — prompt 子包的类型别名与函数转发，保持向后兼容
package agent

import (
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/prompt"
)

// PromptTemplate 支持 {{.Variable}} 格式的系统提示词模板（prompt 子包别名）。
type PromptTemplate = prompt.PromptTemplate

// NewPromptTemplate 创建提示词模板（转发到 prompt 子包）。
func NewPromptTemplate(tmpl string) *PromptTemplate {
	return prompt.NewPromptTemplate(tmpl)
}

// DefaultSystemPrompt 返回默认系统提示词模板（转发）。
func DefaultSystemPrompt() *PromptTemplate {
	return prompt.DefaultSystemPrompt()
}

// CodeAssistantTemplate 构建带文件作用域规则的编程助手模板（转发）。
func CodeAssistantTemplate(taskPrompt string, fileScope []string) *PromptTemplate {
	return prompt.CodeAssistantTemplate(taskPrompt, fileScope)
}

// GeneralAssistantTemplate 通用助手模板（转发）。
func GeneralAssistantTemplate(roleDescription string, constraints []string) *PromptTemplate {
	return prompt.GeneralAssistantTemplate(roleDescription, constraints)
}

// VisionAgentTemplate 视觉 Agent 模板（转发）。
func VisionAgentTemplate(taskDescription string, outputFormat string) *PromptTemplate {
	return prompt.VisionAgentTemplate(taskDescription, outputFormat)
}

// MultiAgentCoordinatorTemplate 多 Agent 协调器模板（转发）。
func MultiAgentCoordinatorTemplate(agents []string, goal string) *PromptTemplate {
	return prompt.MultiAgentCoordinatorTemplate(agents, goal)
}

// FormatToolDescriptions 格式化工具描述（转发）。
func FormatToolDescriptions(toolNames []string, descriptions map[string]string) string {
	return prompt.FormatToolDescriptions(toolNames, descriptions)
}

// FormatRAGContextWithTemplate 用模板格式化 RAG 上下文（转发）。
func FormatRAGContextWithTemplate(tmpl string, docs []*RAGDocument) string {
	return prompt.FormatRAGContextWithTemplate(tmpl, docs)
}
