package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/tools"
)

// HTMLDocument 表示解析后的 HTML 文档结构
type HTMLDocument struct {
	Title    string            `json:"title"`
	Content  string            `json:"content"`
	Links    []HTMLLink        `json:"links"`
	Images   []HTMLImage       `json:"images"`
	Meta     map[string]string `json:"meta"`
	Headings []HTMLHeading     `json:"headings"`
}

const (
	maxHTMLResponseSize = 10 * 1024 * 1024 // 10MB：HTML 响应体上限，防止 OOM
)

// HTMLLink 表示 HTML 中的超链接
type HTMLLink struct {
	Text string `json:"text"`
	Href string `json:"href"`
}

// HTMLImage 表示 HTML 中的图片
type HTMLImage struct {
	Alt string `json:"alt"`
	Src string `json:"src"`
}

// HTMLHeading 表示 HTML 中的标题
type HTMLHeading struct {
	Level   int    `json:"level"`
	Content string `json:"content"`
}

// HTMLLoader 从 HTML 文件/URL 加载结构化内容
type HTMLLoader struct {
	scopePolicy tools.ScopePolicy
	scopeAgent  string
}

// NewHTMLLoader 创建 HTML 加载器
func NewHTMLLoader() *HTMLLoader {
	return &HTMLLoader{}
}

// WithScopePolicy 注入权限策略
func (h *HTMLLoader) WithScopePolicy(policy tools.ScopePolicy, agentID string) *HTMLLoader {
	h.scopePolicy = policy
	h.scopeAgent = agentID
	return h
}

func (h *HTMLLoader) Name() string { return "html_loader" }

func (h *HTMLLoader) Description() string {
	return "Load and parse HTML files or URLs into structured content with title, text, links, images, metadata, and headings."
}

func (h *HTMLLoader) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "action": {"type": "string", "enum": ["load"], "description": "The operation to perform"},
    "path": {"type": "string", "description": "Path to the HTML file or URL (http/https)"},
    "timeout": {"type": "integer", "description": "HTTP request timeout in seconds (default: 30, only for URL loading)"}
  },
  "required": ["action", "path"]
}`)
}

func (h *HTMLLoader) Execute(ctx context.Context, args json.RawMessage) (*tools.Result, error) {
	var params struct {
		Action  string `json:"action"`
		Path    string `json:"path"`
		Timeout int    `json:"timeout"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return tools.NewErrorResult(fmt.Sprintf("invalid arguments: %v", err)), nil
	}

	if params.Action == "" {
		return tools.NewErrorResult("action is required"), nil
	}

	switch params.Action {
	case "load":
		if params.Path == "" {
			return tools.NewErrorResult("path is required"), nil
		}
		return h.load(params.Path, params.Timeout)
	default:
		return tools.NewErrorResult(fmt.Sprintf("unknown action: %s", params.Action)), nil
	}
}

// load 从文件或 URL 加载 HTML 内容
func (h *HTMLLoader) load(path string, timeout int) (*tools.Result, error) {
	var data []byte
	var err error

	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		// 从 URL 加载
		data, err = h.fetchURL(path, timeout)
	} else {
		// 权限检查（仅本地文件）
		if h.scopePolicy != nil && !h.scopePolicy.Allow(h.scopeAgent, path) {
			return tools.NewErrorResult(fmt.Sprintf("access denied: %s", path)), nil
		}
		data, err = os.ReadFile(path)
		if err != nil && os.IsNotExist(err) {
			return tools.NewErrorResult(fmt.Sprintf("file not found: %s", path)), nil
		}
	}

	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("read error: %v", err)), nil
	}

	if len(data) == 0 {
		return tools.NewErrorResult("empty content"), nil
	}

	doc := parseHTML(string(data))

	output, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("marshal error: %v", err)), nil
	}

	return tools.NewResult(string(output)), nil
}

// fetchURL 从 URL 获取 HTML 内容
func (h *HTMLLoader) fetchURL(url string, timeout int) ([]byte, error) {
	if timeout <= 0 {
		timeout = 30
	}
	client := &http.Client{}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP status %d: %s", resp.StatusCode, resp.Status)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxHTMLResponseSize))
	if err != nil {
		return nil, fmt.Errorf("read response body failed: %w", err)
	}
	return data, nil
}

// --- HTML 解析正则表达式 ---

// titleRe 匹配 <title> 标签内容
var titleRe = regexp.MustCompile(`(?i)<title[^>]*>(.*?)</title>`)

// metaRe 匹配 <meta name="..." content="..."> 标签
var metaRe = regexp.MustCompile(`(?i)<meta\s+[^>]*name\s*=\s*["']([^"']+)["'][^>]*content\s*=\s*["']([^"']+)["'][^>]*/?>`)

// metaReAlt 匹配 <meta content="..." name="..."> 标签（属性顺序不同）
var metaReAlt = regexp.MustCompile(`(?i)<meta\s+[^>]*content\s*=\s*["']([^"']+)["'][^>]*name\s*=\s*["']([^"']+)["'][^>]*/?>`)

// headingReHTML 匹配 <h1> 到 <h6> 标签
var headingReHTML = regexp.MustCompile(`(?i)<h([1-6])[^>]*>(.*?)</h[1-6]>`)

// linkReHTML 匹配 <a href="..."> 标签
var linkReHTML = regexp.MustCompile(`(?i)<a\s+[^>]*href\s*=\s*["']([^"']*)["'][^>]*>(.*?)</a>`)

// imgTagRe 匹配 <img> 标签整体
var imgTagRe = regexp.MustCompile(`(?i)<img\s+[^>]+/?>`)

// imgSrcRe 从 img 标签中提取 src 属性
var imgSrcRe = regexp.MustCompile(`(?i)src\s*=\s*["']([^"']*)["']`)

// imgAltRe 从 img 标签中提取 alt 属性
var imgAltRe = regexp.MustCompile(`(?i)alt\s*=\s*["']([^"']*)["']`)

// tagRe 匹配所有 HTML 标签，用于去除标签获取纯文本
var tagRe = regexp.MustCompile(`<[^>]+>`)

// scriptStyleRe 匹配 script 标签及其内容
var scriptRe = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)

// styleRe 匹配 style 标签及其内容
var styleRe = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)

// parseHTML 将 HTML 文本解析为结构化文档
func parseHTML(content string) HTMLDocument {
	doc := HTMLDocument{
		Meta: make(map[string]string),
	}

	// 提取标题
	if matches := titleRe.FindStringSubmatch(content); len(matches) > 1 {
		doc.Title = decodeHTMLEntities(strings.TrimSpace(matches[1]))
	}

	// 提取 meta 标签
	metaMatches := metaRe.FindAllStringSubmatch(content, -1)
	for _, m := range metaMatches {
		if len(m) >= 3 {
			doc.Meta[strings.ToLower(m[1])] = decodeHTMLEntities(m[2])
		}
	}
	metaAltMatches := metaReAlt.FindAllStringSubmatch(content, -1)
	for _, m := range metaAltMatches {
		if len(m) >= 3 {
			doc.Meta[strings.ToLower(m[2])] = decodeHTMLEntities(m[1])
		}
	}

	// 提取标题层级
	headingMatches := headingReHTML.FindAllStringSubmatch(content, -1)
	for _, m := range headingMatches {
		if len(m) >= 3 {
			level := 0
			fmt.Sscanf(m[1], "%d", &level)
			text := stripTags(m[2])
			doc.Headings = append(doc.Headings, HTMLHeading{
				Level:   level,
				Content: strings.TrimSpace(decodeHTMLEntities(text)),
			})
		}
	}

	// 提取链接
	linkMatches := linkReHTML.FindAllStringSubmatch(content, -1)
	for _, m := range linkMatches {
		if len(m) >= 3 {
			text := stripTags(m[2])
			doc.Links = append(doc.Links, HTMLLink{
				Text: strings.TrimSpace(decodeHTMLEntities(text)),
				Href: m[1],
			})
		}
	}

	// 提取图片：先匹配整个 img 标签，再从中提取 src 和 alt 属性
	imgTagMatches := imgTagRe.FindAllString(content, -1)
	for _, tag := range imgTagMatches {
		src := ""
		alt := ""
		if m := imgSrcRe.FindStringSubmatch(tag); len(m) > 1 {
			src = m[1]
		}
		if m := imgAltRe.FindStringSubmatch(tag); len(m) > 1 {
			alt = m[1]
		}
		if src != "" {
			doc.Images = append(doc.Images, HTMLImage{
				Src: src,
				Alt: decodeHTMLEntities(alt),
			})
		}
	}

	// 提取纯文本内容
	// 先移除 script 和 style 标签
	cleaned := scriptRe.ReplaceAllString(content, "")
	cleaned = styleRe.ReplaceAllString(cleaned, "")
	// 移除所有 HTML 标签
	plainText := stripTags(cleaned)
	plainText = decodeHTMLEntities(plainText)
	// 清理多余空白
	plainText = collapseWhitespace(plainText)
	doc.Content = strings.TrimSpace(plainText)

	return doc
}

// stripTags 移除所有 HTML 标签，返回纯文本
func stripTags(html string) string {
	return tagRe.ReplaceAllString(html, "")
}

// decodeHTMLEntities 解码常见 HTML 实体
func decodeHTMLEntities(s string) string {
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", `"`)
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.ReplaceAll(s, "&apos;", "'")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	return s
}

// collapseWhitespace 将连续空白压缩为单个空格
func collapseWhitespace(s string) string {
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' || r == ' ' {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
		} else {
			b.WriteRune(r)
			prevSpace = false
		}
	}
	return b.String()
}
