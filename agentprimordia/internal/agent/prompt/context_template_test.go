package prompt

import (
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/core"
	"strings"
	"testing"
)

func TestNewPromptTemplate(t *testing.T) {
	tpl := NewPromptTemplate("基础模板")
	if tpl.template != "基础模板" {
		t.Errorf("template = %q, want 基础模板", tpl.template)
	}
	if tpl.vars == nil {
		t.Error("vars should be initialized")
	}
}

func TestPromptTemplate_WithVar(t *testing.T) {
	tpl := NewPromptTemplate("Hello {{.name}}!")
	tpl.WithVar("name", "World")

	result, err := tpl.Render()
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if result != "Hello World!" {
		t.Errorf("Render() = %q, want 'Hello World!'", result)
	}
}

func TestPromptTemplate_Render(t *testing.T) {
	tpl := NewPromptTemplate("你是一个助手。")
	result, err := tpl.Render()
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if !strings.Contains(result, "你是一个助手。") {
		t.Error("should contain base template")
	}
}

func TestCodeAssistantTemplate(t *testing.T) {
	task := "修复 login.go 中的空指针异常"
	files := []string{"src/auth/login.go", "src/utils/helper.go"}

	tpl := CodeAssistantTemplate(task, files)
	result, err := tpl.Render()
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	if !strings.Contains(result, "代码助手子代理") {
		t.Error("should contain role description")
	}
	if !strings.Contains(result, task) {
		t.Error("should contain task prompt")
	}
	if !strings.Contains(result, "文件范围限制") {
		t.Error("should contain file scope section when files provided")
	}
	for _, f := range files {
		if !strings.Contains(result, f) {
			t.Errorf("should contain file %s in scope", f)
		}
	}
}

func TestCodeAssistantTemplate_NoFileScope(t *testing.T) {
	tpl := CodeAssistantTemplate("完成任务", nil)
	result, err := tpl.Render()
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	if strings.Contains(result, "文件范围限制") {
		t.Error("should not contain file scope section when no files")
	}
}

func TestGeneralAssistantTemplate(t *testing.T) {
	tpl := GeneralAssistantTemplate("数据分析专家", []string{
		"使用中文回答",
		"提供数据可视化建议",
		"解释技术术语",
	})

	result, err := tpl.Render()
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	if !strings.Contains(result, "数据分析专家") {
		t.Error("should contain role description")
	}
	if !strings.Contains(result, "约束与规则") {
		t.Error("should contain constraints section")
	}
	if !strings.Contains(result, "使用中文回答") {
		t.Error("should contain first constraint")
	}
}

func TestVisionAgentTemplate(t *testing.T) {
	tpl := VisionAgentTemplate(
		"分析这张医学影像中的异常区域",
		`JSON格式: {"findings": [...], "confidence": 0.95}`,
	)

	result, err := tpl.Render()
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	if !strings.Contains(result, "视觉分析") {
		t.Error("should contain vision agent description")
	}
	if !strings.Contains(result, "医学影像") {
		t.Error("should contain task description")
	}
	if !strings.Contains(result, "JSON格式") {
		t.Error("should contain output format")
	}
	if !strings.Contains(result, "图片内容识别") {
		t.Error("should contain capabilities")
	}
}

func TestVisionAgentTemplate_NoOutputFormat(t *testing.T) {
	tpl := VisionAgentTemplate("描述这张图片", "")
	result, err := tpl.Render()
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	if strings.Contains(result, "输出格式要求") {
		t.Error("should not contain output format section when empty")
	}
}

func TestMultiAgentCoordinatorTemplate(t *testing.T) {
	agents := []string{"Coder（代码编写）", "Reviewer（代码审查）", "Tester（测试）"}
	goal := "完成用户认证模块的开发和测试"

	tpl := MultiAgentCoordinatorTemplate(agents, goal)
	result, err := tpl.Render()
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	if !strings.Contains(result, "协调者") {
		t.Error("should contain coordinator role")
	}
	if !strings.Contains(result, "可用 Agents") {
		t.Error("should contain agents list section")
	}
	for _, a := range agents {
		if !strings.Contains(result, a) {
			t.Errorf("should contain agent %s", a)
		}
	}
	if !strings.Contains(result, goal) {
		t.Error("should contain collaboration goal")
	}
	if !strings.Contains(result, "协调规则") {
		t.Error("should contain coordination rules")
	}
}

func TestFormatToolDescriptions(t *testing.T) {
	tools := []string{"read_file", "write_file", "search"}
	descs := map[string]string{
		"read_file":  "读取文件内容",
		"write_file": "写入文件内容",
		"search":     "搜索文本",
	}

	result := FormatToolDescriptions(tools, descs)

	if !strings.Contains(result, "## 可用工具") {
		t.Error("should contain tools header")
	}
	if !strings.Contains(result, "**read_file**: 读取文件内容") {
		t.Error("should format tool with description")
	}
}

func TestFormatToolDescriptions_Empty(t *testing.T) {
	result := FormatToolDescriptions(nil, nil)
	if result != "" {
		t.Errorf("empty input should return empty string, got %q", result)
	}
}

func TestFormatToolDescriptions_MissingDescription(t *testing.T) {
	tools := []string{"unknown_tool"}
	result := FormatToolDescriptions(tools, map[string]string{})

	if !strings.Contains(result, "无描述") {
		t.Error("should show '无描述' for missing description")
	}
}

func TestFormatRAGContextWithTemplate(t *testing.T) {
	docs := []*core.RAGDocument{
		{ID: "1", Content: "Go 是一门编程语言", Score: 0.95, Role: "文档"},
		{ID: "2", Content: "AgentPrimordia 是 Go 框架", Score: 0.88, Role: "知识"},
	}

	customTemplate := "📚 参考资料库:\n{{content}}\n💡 请基于以上资料回答"
	result := FormatRAGContextWithTemplate(customTemplate, docs)

	if !strings.Contains(result, "参考资料库") {
		t.Error("should use custom template header")
	}
	if !strings.Contains(result, "Go 是一门编程语言") {
		t.Error("should contain first document")
	}
	if !strings.Contains(result, "相关度: 0.95") {
		t.Error("should contain score of first doc")
	}
	if !strings.Contains(result, "文档") {
		t.Error("should contain role of first doc")
	}
}

func TestFormatRAGContextWithTemplate_DefaultTemplate(t *testing.T) {
	docs := []*core.RAGDocument{
		{Content: "测试内容", Score: 0.9},
	}

	result := FormatRAGContextWithTemplate("", docs)

	if !strings.Contains(result, "=== 相关知识 ===") {
		t.Error("should use default template")
	}
	if !strings.Contains(result, "=== 知识结束 ===") {
		t.Error("should contain default footer")
	}
}

func TestDefaultSystemPrompt(t *testing.T) {
	tpl := DefaultSystemPrompt()
	tpl.WithVar("AgentName", "TestBot")

	result, err := tpl.Render()
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	if !strings.Contains(result, "TestBot") {
		t.Error("should contain agent name")
	}
}

func TestDefaultSystemPrompt_WithScopeRules(t *testing.T) {
	tpl := DefaultSystemPrompt()
	tpl.WithVar("AgentName", "SecureBot")
	tpl.WithScopeRules([]string{"/src/", "/config/"})

	result, err := tpl.Render()
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	if !strings.Contains(result, "文件操作权限") {
		t.Error("should contain scope section when scopes provided")
	}
	if !strings.Contains(result, "/src/") {
		t.Error("should contain first scope path")
	}
}

func TestPromptTemplate_ChainedCalls(t *testing.T) {
	result, err := NewPromptTemplate("{{.role}}").
		WithVar("role", "助手").
		Render()
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	if !strings.Contains(result, "助手") {
		t.Error("should contain role variable")
	}
}
