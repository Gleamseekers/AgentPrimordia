package prompt

import (
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/core"
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// PromptTemplate 支持 {{.Variable}} 格式的系统提示词模板
// 可自动注入 Agent 名称、权限规则、任务描述等变量
type PromptTemplate struct {
	template string
	vars     map[string]string
}

// NewPromptTemplate 创建提示词模板
func NewPromptTemplate(tmpl string) *PromptTemplate {
	return &PromptTemplate{
		template: tmpl,
		vars:     make(map[string]string),
	}
}

// WithVar 设置模板变量
func (t *PromptTemplate) WithVar(key, value string) *PromptTemplate {
	t.vars[key] = value
	return t
}

// WithScopeRules 自动注入 FilesScope 权限规则文本
// 将路径列表格式化为人类可读的规则描述
func (t *PromptTemplate) WithScopeRules(scopes []string) *PromptTemplate {
	if len(scopes) == 0 {
		return t
	}
	var b strings.Builder
	for i, s := range scopes {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("- ")
		b.WriteString(s)
	}
	t.vars["ScopeRules"] = b.String()
	return t
}

// Render 渲染模板，返回最终的提示词文本
// 使用 text/template 标准库，缺失变量输出空字符串
func (t *PromptTemplate) Render() (string, error) {
	if t.template == "" {
		return "", nil
	}

	tmpl, err := template.New("prompt").Option("missingkey=zero").Parse(t.template)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, t.vars); err != nil {
		return "", fmt.Errorf("failed to render template: %w", err)
	}

	return strings.TrimSpace(buf.String()), nil
}

// DefaultSystemPrompt 返回内置的默认系统提示词模板
func DefaultSystemPrompt() *PromptTemplate {
	return NewPromptTemplate(`你是一个 AI 助手，名为 {{.AgentName}}。
{{if .ScopeRules}}
## 文件操作权限
你只能操作以下文件或目录：
{{.ScopeRules}}

如果用户要求你操作范围外的文件，请拒绝并说明原因。
{{end}}
{{if .TaskDescription}}
## 任务描述
{{.TaskDescription}}
{{end}}
请逐步思考和行动，使用可用的tool来完成任务。`)
}

// ===== 预定义模板工厂函数 =====

// CodeAssistantTemplate 代码助手系统提示词模板
//
// 从 CodeCast-desktop/agent_engine.go:24 提取，适用于代码编写、调试、重构场景。
func CodeAssistantTemplate(taskPrompt string, fileScope []string) *PromptTemplate {
	tmpl := `你是一个代码助手子代理（Sub-Agent），负责独立执行分配给你的任务。

## 任务

` + taskPrompt + `

## 规则

1. 专注于分配给你的任务，不要偏离主题。
2. 使用提供的tool完成任务，每次只调用必要的tool。
3. 完成任务后，输出简洁的结果摘要。
4. 如果遇到无法解决的错误，清晰地描述问题并停止。
5. 不要请求用户输入，你需要独立完成任务。
{{if .HasFileScope}}

## 文件范围限制

你只能操作以下文件或目录：
{{.FileScopeList}}

不要修改范围之外的文件。
{{end}}`

	t := NewPromptTemplate(tmpl)
	if len(fileScope) > 0 {
		var b strings.Builder
		for _, f := range fileScope {
			b.WriteString("- ")
			b.WriteString(f)
			b.WriteString("\n")
		}
		t.WithVar("HasFileScope", "true")
		t.WithVar("FileScopeList", strings.TrimSpace(b.String()))
	}
	return t
}

// GeneralAssistantTemplate 通用助手系统提示词模板
//
// 适用于问答、分析、创作等通用场景。
func GeneralAssistantTemplate(roleDescription string, constraints []string) *PromptTemplate {
	baseTmpl := fmt.Sprintf(`你是一个%s。`, roleDescription)

	if len(constraints) > 0 {
		baseTmpl += "\n\n## 约束与规则\n\n"
		for i, c := range constraints {
			baseTmpl += fmt.Sprintf("%d. %s\n", i+1, c)
		}
	}

	baseTmpl += `
## 输出要求

请提供清晰、准确、有帮助的回答。如果信息不足，请明确说明。`

	return NewPromptTemplate(baseTmpl)
}

// VisionAgentTemplate 视觉分析 Agent 模板
//
// 适用于图片理解、图像分析、OCR 等多模态场景。
func VisionAgentTemplate(taskDescription string, outputFormat string) *PromptTemplate {
	tmpl := `你是一个专业的视觉分析 AI 助手，擅长图像理解和内容描述。

## 任务

` + taskDescription + `

## 能力范围

- 图片内容识别与描述
- 颜色、构图、风格分析
- 文字提取（OCR）
- 对象检测与计数
- 场景理解与上下文推断
{{if .OutputFormat}}

## 输出格式要求

{{.OutputFormat}}
{{end}}

## 注意事项

- 如果图片模糊或质量较差，请明确说明
- 对于不确定的内容，给出合理的推测并标注置信度
- 使用中文回答（除非用户指定其他语言）`

	t := NewPromptTemplate(tmpl)
	if outputFormat != "" {
		t.WithVar("OutputFormat", outputFormat)
	}
	return t
}

// MultiAgentCoordinatorTemplate 多 Agent 协调者模板
//
// 用于 Pool/GroupChat 中的主控 Agent。
func MultiAgentCoordinatorTemplate(agents []string, goal string) *PromptTemplate {
	var agentList strings.Builder
	for i, a := range agents {
		agentList.WriteString(fmt.Sprintf("- Agent %d: %s\n", i+1, a))
	}

	tmpl := fmt.Sprintf(`你是多 Agent 协作系统的协调者，负责分配任务、整合结果。

## 可用 Agents

%s
## 协作目标

%s

## 协调规则

1. 根据任务特点选择最合适的 Agent
2. 将复杂任务分解为子任务并分配
3. 整合各 Agent 的结果，提供统一输出
4. 处理 Agent 间的冲突和依赖关系
5. 确保最终结果符合用户需求`, agentList.String(), goal)

	return NewPromptTemplate(tmpl)
}

// ===== tool函数 =====

// FormatToolDescriptions 格式化tool描述列表
//
// 将tool注册表中的tool转换为可读的描述文本，用于注入系统提示词。
func FormatToolDescriptions(toolNames []string, descriptions map[string]string) string {
	if len(toolNames) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## 可用工具\n\n")

	for _, name := range toolNames {
		desc, ok := descriptions[name]
		if !ok {
			desc = "无描述"
		}
		sb.WriteString(fmt.Sprintf("- **%s**: %s\n", name, desc))
	}

	return sb.String()
}

// FormatRAGContextWithTemplate 使用自定义模板格式化 RAG 上下文
//
// 扩展 RAGConfig.ContextTemplate 的功能，支持更灵活的模板定制。
func FormatRAGContextWithTemplate(tmpl string, docs []*core.RAGDocument) string {
	if tmpl == "" {
		tmpl = "=== 相关知识 ===\n{{content}}\n=== 知识结束 ==="
	}

	var sb strings.Builder
	for i, doc := range docs {
		role := doc.Role
		if role == "" {
			role = "知识"
		}
		sb.WriteString(fmt.Sprintf("[%d | 相关度: %.2f | %s] %s\n", i+1, doc.Score, role, doc.Content))
	}

	content := strings.TrimSpace(sb.String())
	result := strings.ReplaceAll(tmpl, "{{content}}", content)
	return result
}
