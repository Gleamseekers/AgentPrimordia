package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/tools"
)

// MarkdownDocument 表示解析后的 Markdown 文档结构
type MarkdownDocument struct {
	Title      string            `json:"title"`
	Sections   []MarkdownSection `json:"sections"`
	CodeBlocks []CodeBlock       `json:"code_blocks"`
	Links      []string          `json:"links"`
	Images     []string          `json:"images"`
	RawContent string            `json:"raw_content"`
}

// MarkdownSection 表示 Markdown 文档中的一个章节
type MarkdownSection struct {
	Level   int    `json:"level"`
	Heading string `json:"heading"`
	Content string `json:"content"`
}

// CodeBlock 表示 Markdown 文档中的代码块
type CodeBlock struct {
	Language string `json:"language"`
	Code     string `json:"code"`
}

// MarkdownLoader 从 Markdown 文件加载结构化内容
type MarkdownLoader struct {
	scopePolicy tools.ScopePolicy
	scopeAgent  string
}

// NewMarkdownLoader 创建 Markdown 加载器
func NewMarkdownLoader() *MarkdownLoader {
	return &MarkdownLoader{}
}

// WithScopePolicy 注入权限策略
func (m *MarkdownLoader) WithScopePolicy(policy tools.ScopePolicy, agentID string) *MarkdownLoader {
	m.scopePolicy = policy
	m.scopeAgent = agentID
	return m
}

func (m *MarkdownLoader) Name() string { return "markdown_loader" }

func (m *MarkdownLoader) Description() string {
	return "Load and parse Markdown files into structured content with sections, code blocks, links, and images metadata."
}

func (m *MarkdownLoader) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "action": {"type": "string", "enum": ["load"], "description": "The operation to perform"},
    "path": {"type": "string", "description": "Path to the Markdown file"}
  },
  "required": ["action", "path"]
}`)
}

func (m *MarkdownLoader) Execute(ctx context.Context, args json.RawMessage) (*tools.Result, error) {
	var params map[string]json.RawMessage
	if err := json.Unmarshal(args, &params); err != nil {
		return tools.NewErrorResult(fmt.Sprintf("invalid arguments: %v", err)), nil
	}

	action := ""
	if err := unmarshalRaw(params["action"], &action); err != nil {
		return tools.NewErrorResult(fmt.Sprintf("invalid parameter 'action': %v", err)), nil
	}

	switch action {
	case "load":
		path := ""
		if err := unmarshalRaw(params["path"], &path); err != nil {
			return tools.NewErrorResult(fmt.Sprintf("invalid parameter 'path': %v", err)), nil
		}
		if path == "" {
			return tools.NewErrorResult("path is required"), nil
		}
		return m.loadFile(path)
	default:
		return tools.NewErrorResult(fmt.Sprintf("unknown action: %s", action)), nil
	}
}

// loadFile 读取并解析 Markdown 文件
func (m *MarkdownLoader) loadFile(path string) (*tools.Result, error) {
	// 权限检查
	if m.scopePolicy != nil && !m.scopePolicy.Allow(m.scopeAgent, path) {
		return tools.NewErrorResult(fmt.Sprintf("access denied: %s", path)), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return tools.NewErrorResult(fmt.Sprintf("file not found: %s", path)), nil
		}
		return tools.NewErrorResult(fmt.Sprintf("read error: %v", err)), nil
	}

	content := string(data)
	doc := parseMarkdown(content)

	output, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("marshal error: %v", err)), nil
	}

	return tools.NewResult(string(output)), nil
}

// headingRe 匹配 Markdown 标题行（# 开头）
var headingRe = regexp.MustCompile(`^(#{1,6})\s+(.+)$`)

// codeBlockRe 匹配代码块（```lang ... ```）
var codeBlockRe = regexp.MustCompile("```(\\w*)\\n([\\s\\S]*?)```")

// linkRe 匹配 Markdown 链接 [text](url)
var linkRe = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)

// imageRe 匹配 Markdown 图片 ![alt](url)
var imageRe = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)

// parseMarkdown 将 Markdown 文本解析为结构化文档
func parseMarkdown(content string) MarkdownDocument {
	doc := MarkdownDocument{
		RawContent: content,
	}

	// 提取代码块（必须在处理标题之前，避免代码块内的 # 被误识别）
	doc.CodeBlocks = extractCodeBlocks(content)

	// 提取链接和图片
	doc.Links = extractLinks(content)
	doc.Images = extractImages(content)

	// 解析标题和章节
	doc.Sections, doc.Title = extractSections(content)

	return doc
}

// extractCodeBlocks 从 Markdown 内容中提取代码块
func extractCodeBlocks(content string) []CodeBlock {
	matches := codeBlockRe.FindAllStringSubmatch(content, -1)
	var blocks []CodeBlock
	for _, match := range matches {
		blocks = append(blocks, CodeBlock{
			Language: match[1],
			Code:     strings.TrimRight(match[2], "\n"),
		})
	}
	return blocks
}

// extractLinks 从 Markdown 内容中提取链接 URL
func extractLinks(content string) []string {
	// 先移除图片语法，避免图片被链接正则误匹配
	cleaned := imageRe.ReplaceAllString(content, "")
	matches := linkRe.FindAllStringSubmatch(cleaned, -1)
	var links []string
	for _, match := range matches {
		links = append(links, match[2])
	}
	return links
}

// extractImages 从 Markdown 内容中提取图片 URL
func extractImages(content string) []string {
	matches := imageRe.FindAllStringSubmatch(content, -1)
	var images []string
	for _, match := range matches {
		images = append(images, match[2])
	}
	return images
}

// extractSections 从 Markdown 内容中提取标题和章节
func extractSections(content string) ([]MarkdownSection, string) {
	// 移除代码块内容，避免代码块内的 # 被误识别为标题
	cleaned := codeBlockRe.ReplaceAllString(content, "")

	lines := strings.Split(cleaned, "\n")

	type headingInfo struct {
		level   int
		heading string
		line    int
	}

	var headings []headingInfo
	for i, line := range lines {
		matches := headingRe.FindStringSubmatch(line)
		if matches != nil {
			headings = append(headings, headingInfo{
				level:   len(matches[1]),
				heading: strings.TrimSpace(matches[2]),
				line:    i,
			})
		}
	}

	var title string
	if len(headings) > 0 && headings[0].level == 1 {
		title = headings[0].heading
	}

	var sections []MarkdownSection
	for i, h := range headings {
		// 章节内容为当前标题行之后到下一个同级或更高级标题之前
		startLine := h.line + 1
		endLine := len(lines)
		for j := i + 1; j < len(headings); j++ {
			if headings[j].level <= h.level {
				endLine = headings[j].line
				break
			}
		}

		var contentLines []string
		for k := startLine; k < endLine && k < len(lines); k++ {
			contentLines = append(contentLines, lines[k])
		}
		sectionContent := strings.TrimSpace(strings.Join(contentLines, "\n"))
		// 跳过第一个 h1 标题（它作为文档标题）
		if i == 0 && h.level == 1 {
			sections = append(sections, MarkdownSection{
				Level:   h.level,
				Heading: h.heading,
				Content: sectionContent,
			})
			continue
		}
		sections = append(sections, MarkdownSection{
			Level:   h.level,
			Heading: h.heading,
			Content: sectionContent,
		})
	}

	return sections, title
}
