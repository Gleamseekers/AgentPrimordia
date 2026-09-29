package builtin

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools"
)

// DOCXDocument 表示解析后的 DOCX 文档结构
type DOCXDocument struct {
	Paragraphs []DOCXParagraph `json:"paragraphs"`
	Metadata   DOCXMetadata    `json:"metadata"`
}

// DOCXParagraph 表示 DOCX 中的一个段落
type DOCXParagraph struct {
	Text      string `json:"text"`
	Style     string `json:"style,omitempty"`
	IsHeading bool   `json:"is_heading,omitempty"`
}

// DOCXMetadata 表示 DOCX 文档的元数据
type DOCXMetadata struct {
	Title    string `json:"title,omitempty"`
	Author   string `json:"author,omitempty"`
	Created  string `json:"created,omitempty"`
	Modified string `json:"modified,omitempty"`
}

// DOCXLoader 从 DOCX 文件提取文本内容
// DOCX 格式本质是 ZIP 包内含 XML 文件
type DOCXLoader struct {
	scopePolicy tools.ScopePolicy
	scopeAgent  string
}

// NewDOCXLoader 创建 DOCX 加载器
func NewDOCXLoader() *DOCXLoader {
	return &DOCXLoader{}
}

// WithScopePolicy 注入权限策略
func (d *DOCXLoader) WithScopePolicy(policy tools.ScopePolicy, agentID string) *DOCXLoader {
	d.scopePolicy = policy
	d.scopeAgent = agentID
	return d
}

func (d *DOCXLoader) Name() string { return "docx_loader" }

func (d *DOCXLoader) Description() string {
	return "Load and extract text content from DOCX files. Parses paragraphs, headings, and metadata from the ZIP/XML structure."
}

func (d *DOCXLoader) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "action": {"type": "string", "enum": ["load"], "description": "The operation to perform"},
    "path": {"type": "string", "description": "Path to the DOCX file"}
  },
  "required": ["action", "path"]
}`)
}

func (d *DOCXLoader) Execute(ctx context.Context, args json.RawMessage) (*tools.Result, error) {
	var params struct {
		Action string `json:"action"`
		Path   string `json:"path"`
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
		return d.loadFile(params.Path)
	default:
		return tools.NewErrorResult(fmt.Sprintf("unknown action: %s", params.Action)), nil
	}
}

// loadFile 读取并解析 DOCX 文件
func (d *DOCXLoader) loadFile(path string) (*tools.Result, error) {
	// 权限检查
	if d.scopePolicy != nil && !d.scopePolicy.Allow(d.scopeAgent, path) {
		return tools.NewErrorResult(fmt.Sprintf("access denied: %s", path)), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return tools.NewErrorResult(fmt.Sprintf("file not found: %s", path)), nil
		}
		return tools.NewErrorResult(fmt.Sprintf("read error: %v", err)), nil
	}

	if len(data) == 0 {
		return tools.NewErrorResult("file is empty"), nil
	}

	doc, err := parseDOCX(data)
	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("parse error: %v", err)), nil
	}

	output, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("marshal error: %v", err)), nil
	}

	return tools.NewResult(string(output)), nil
}

// --- DOCX XML 结构体定义 ---

// wDocument 表示 word/document.xml 的根结构
type wDocument struct {
	XMLName xml.Name `xml:"document"`
	Body    wBody    `xml:"body"`
}

// wBody 表示文档 body
type wBody struct {
	Paragraphs []wParagraph `xml:"p"`
}

// wParagraph 表示段落 <w:p>
type wParagraph struct {
	Properties wParagraphProperties `xml:"pPr"`
	Runs       []wRun               `xml:"r"`
}

// wParagraphProperties 表示段落属性 <w:pPr>
type wParagraphProperties struct {
	Style wParagraphStyle `xml:"pStyle"`
}

// wParagraphStyle 表示段落样式 <w:pStyle>
type wParagraphStyle struct {
	Val string `xml:"val,attr"`
}

// wRun 表示文本运行 <w:r>
type wRun struct {
	Text wText `xml:"t"`
}

// wText 表示文本内容 <w:t>
type wText struct {
	Content string `xml:",chardata"`
}

// cpCoreProperties 表示 docProps/core.xml 的根结构
type cpCoreProperties struct {
	XMLName  xml.Name `xml:"coreProperties"`
	Title    string   `xml:"title"`
	Creator  string   `xml:"creator"`
	Created  string   `xml:"created"`
	Modified string   `xml:"modified"`
}

// parseDOCX 解析 DOCX 二进制数据
func parseDOCX(data []byte) (*DOCXDocument, error) {
	doc := &DOCXDocument{}

	// 打开 ZIP 文件
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("failed to open DOCX as ZIP: %w", err)
	}

	// 解析文档内容
	var documentXML []byte
	var coreXML []byte

	for _, file := range reader.File {
		switch file.Name {
		case "word/document.xml":
			documentXML, err = readZipFile(file)
			if err != nil {
				return nil, fmt.Errorf("failed to read document.xml: %w", err)
			}
		case "docProps/core.xml":
			coreXML, err = readZipFile(file)
			if err != nil {
				return nil, fmt.Errorf("failed to read core.xml: %w", err)
			}
		}
	}

	if documentXML == nil {
		return nil, fmt.Errorf("invalid DOCX: missing word/document.xml")
	}

	// 解析文档段落
	paragraphs, err := parseDocumentXML(documentXML)
	if err != nil {
		return nil, fmt.Errorf("failed to parse document.xml: %w", err)
	}
	doc.Paragraphs = paragraphs

	// 解析元数据
	if coreXML != nil {
		doc.Metadata = parseCoreXML(coreXML)
	}

	return doc, nil
}

// readZipFile 读取 ZIP 中的文件内容
func readZipFile(file *zip.File) ([]byte, error) {
	rc, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// parseDocumentXML 解析 word/document.xml
func parseDocumentXML(data []byte) ([]DOCXParagraph, error) {
	var wDoc wDocument
	if err := xml.Unmarshal(data, &wDoc); err != nil {
		return nil, fmt.Errorf("XML unmarshal error: %w", err)
	}

	var paragraphs []DOCXParagraph
	for _, p := range wDoc.Body.Paragraphs {
		// 收集段落文本
		var textParts []string
		for _, run := range p.Runs {
			if run.Text.Content != "" {
				textParts = append(textParts, run.Text.Content)
			}
		}
		text := strings.Join(textParts, "")

		// 判断是否为标题
		style := p.Properties.Style.Val
		isHeading := strings.HasPrefix(style, "Heading") || strings.HasPrefix(style, "heading")

		// 跳过完全空白的段落
		if text == "" && style == "" {
			continue
		}

		paragraphs = append(paragraphs, DOCXParagraph{
			Text:      text,
			Style:     style,
			IsHeading: isHeading,
		})
	}

	return paragraphs, nil
}

// parseCoreXML 解析 docProps/core.xml
func parseCoreXML(data []byte) DOCXMetadata {
	// 使用 XML 命名空间感知解析
	// core.xml 使用 Dublin Core 命名空间
	var core cpCoreProperties

	// 尝试直接解析（某些 DOCX 的命名空间前缀可能不同）
	if err := xml.Unmarshal(data, &core); err != nil {
		// 如果直接解析失败，尝试用正则提取
		return extractCoreMetadataWithRegex(data)
	}

	return DOCXMetadata{
		Title:    core.Title,
		Author:   core.Creator,
		Created:  core.Created,
		Modified: core.Modified,
	}
}

// extractCoreMetadataWithRegex 使用正则从 core.xml 提取元数据
// 作为 XML 解析失败的降级方案
func extractCoreMetadataWithRegex(data []byte) DOCXMetadata {
	meta := DOCXMetadata{}
	content := string(data)

	// 匹配 <dc:title>...</dc:title> 或 <title>...</title>
	if match := extractXMLTag(content, "dc:title"); match != "" {
		meta.Title = match
	} else if match := extractXMLTag(content, "title"); match != "" {
		meta.Title = match
	}

	// 匹配 <dc:creator>...</dc:creator>
	if match := extractXMLTag(content, "dc:creator"); match != "" {
		meta.Author = match
	} else if match := extractXMLTag(content, "creator"); match != "" {
		meta.Author = match
	}

	// 匹配 <dcterms:created>...</dcterms:created>
	if match := extractXMLTag(content, "dcterms:created"); match != "" {
		meta.Created = match
	} else if match := extractXMLTag(content, "created"); match != "" {
		meta.Created = match
	}

	// 匹配 <dcterms:modified>...</dcterms:modified>
	if match := extractXMLTag(content, "dcterms:modified"); match != "" {
		meta.Modified = match
	} else if match := extractXMLTag(content, "modified"); match != "" {
		meta.Modified = match
	}

	return meta
}

// extractXMLTag 从 XML 内容中提取指定标签的文本内容
func extractXMLTag(content, tag string) string {
	// 简单字符串搜索实现，避免引入 regexp
	openTag := "<" + tag
	closeTag := "</" + tag + ">"

	start := strings.Index(content, openTag)
	if start == -1 {
		return ""
	}

	// 找到开始标签的结束位置 >
	tagEnd := strings.Index(content[start:], ">")
	if tagEnd == -1 {
		return ""
	}
	contentStart := start + tagEnd + 1

	// 找到结束标签
	end := strings.Index(content[contentStart:], closeTag)
	if end == -1 {
		return ""
	}

	return strings.TrimSpace(content[contentStart : contentStart+end])
}

// buildMinimalDOCX 构建一个最小化的有效 DOCX 文件用于测试
func buildMinimalDOCX(paragraphs []DOCXParagraph, meta DOCXMetadata) []byte {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	// [Content_Types].xml
	contentTypes := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
  <Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>
</Types>`
	addZipFile(w, "[Content_Types].xml", contentTypes)

	// _rels/.rels
	rels := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>
</Relationships>`
	addZipFile(w, "_rels/.rels", rels)

	// word/_rels/document.xml.rels
	wordRels := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
</Relationships>`
	addZipFile(w, "word/_rels/document.xml.rels", wordRels)

	// word/document.xml
	var paraXML strings.Builder
	for _, p := range paragraphs {
		styleAttr := ""
		if p.Style != "" {
			styleAttr = fmt.Sprintf(`<w:pStyle w:val="%s"/>`, p.Style)
		}
		paraXML.WriteString(fmt.Sprintf(`<w:p><w:pPr>%s</w:pPr><w:r><w:t>%s</w:t></w:r></w:p>`, styleAttr, escapeXML(p.Text)))
	}

	documentXML := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>%s</w:body>
</w:document>`, paraXML.String())
	addZipFile(w, "word/document.xml", documentXML)

	// docProps/core.xml
	coreXML := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties"
  xmlns:dc="http://purl.org/dc/elements/1.1/"
  xmlns:dcterms="http://purl.org/dc/terms/"
  xmlns:dcmitype="http://purl.org/dc/dcmitype/"
  xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
  <dc:title>%s</dc:title>
  <dc:creator>%s</dc:creator>
  <dcterms:created xsi:type="dcterms:W3CDTF">%s</dcterms:created>
  <dcterms:modified xsi:type="dcterms:W3CDTF">%s</dcterms:modified>
</cp:coreProperties>`,
		escapeXML(meta.Title), escapeXML(meta.Author), escapeXML(meta.Created), escapeXML(meta.Modified))
	addZipFile(w, "docProps/core.xml", coreXML)

	w.Close()
	return buf.Bytes()
}

// addZipFile 向 ZIP 写入器添加一个文件
func addZipFile(w *zip.Writer, name, content string) {
	f, _ := w.Create(name)
	f.Write([]byte(content))
}

// escapeXML 转义 XML 特殊字符
func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}
