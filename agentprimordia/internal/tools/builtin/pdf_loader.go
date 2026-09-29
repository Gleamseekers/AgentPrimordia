package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"unicode/utf16"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools"
)

// PDFDocument 表示解析后的 PDF 文档结构
type PDFDocument struct {
	Pages     []PDFPage         `json:"pages"`
	PageCount int               `json:"page_count"`
	Metadata  map[string]string `json:"metadata"`
}

// PDFPage 表示 PDF 中的单页
type PDFPage struct {
	PageNumber int    `json:"page_number"`
	Content    string `json:"content"`
}

// PDFLoader 从 PDF 文件提取文本内容
// 注意：此为纯 Go 标准库实现，仅支持简单文本型 PDF。
// 复杂 PDF（扫描件、加密、压缩流）可能无法正确提取。
type PDFLoader struct {
	scopePolicy tools.ScopePolicy
	scopeAgent  string
}

// NewPDFLoader 创建 PDF 加载器
func NewPDFLoader() *PDFLoader {
	return &PDFLoader{}
}

// WithScopePolicy 注入权限策略
func (p *PDFLoader) WithScopePolicy(policy tools.ScopePolicy, agentID string) *PDFLoader {
	p.scopePolicy = policy
	p.scopeAgent = agentID
	return p
}

func (p *PDFLoader) Name() string { return "pdf_loader" }

func (p *PDFLoader) Description() string {
	return "Load and extract text content from PDF files. Supports simple text-based PDFs (not scanned/encrypted)."
}

func (p *PDFLoader) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "action": {"type": "string", "enum": ["load"], "description": "The operation to perform"},
    "path": {"type": "string", "description": "Path to the PDF file"}
  },
  "required": ["action", "path"]
}`)
}

func (p *PDFLoader) Execute(ctx context.Context, args json.RawMessage) (*tools.Result, error) {
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
		return p.loadFile(params.Path)
	default:
		return tools.NewErrorResult(fmt.Sprintf("unknown action: %s", params.Action)), nil
	}
}

// loadFile 读取并解析 PDF 文件
func (p *PDFLoader) loadFile(path string) (*tools.Result, error) {
	// 权限检查
	if p.scopePolicy != nil && !p.scopePolicy.Allow(p.scopeAgent, path) {
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

	doc, err := parsePDF(data)
	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("parse error: %v", err)), nil
	}

	output, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("marshal error: %v", err)), nil
	}

	return tools.NewResult(string(output)), nil
}

// --- PDF 解析正则表达式 ---

// pdfHeaderRe 匹配 PDF 文件头
var pdfHeaderRe = regexp.MustCompile(`%PDF-(\d+\.\d+)`)

// pdfTrailerRe 匹配 PDF trailer 中的 Info 引用
var pdfTrailerRe = regexp.MustCompile(`/Info\s+(\d+)\s+(\d+)\s+R`)

// pdfInfoRe 匹配 Info 字典中的键值对
var pdfInfoRe = regexp.MustCompile(`/([A-Za-z]+)\s*\(([^)]*)\)`)

// pdfPageRe 匹配 /Type /Page（非 /Pages）
var pdfPageRe = regexp.MustCompile(`/Type\s*/Page\b`)

// pdfStringLiteralRe 匹配 PDF 字符串字面量 (...)
var pdfStringLiteralRe = regexp.MustCompile(`\(([^)]*)\)`)

// pdfHexStringRe 匹配 PDF 十六进制字符串 <...>
var pdfHexStringRe = regexp.MustCompile(`<([0-9A-Fa-f]+)>`)

// pdfTjRe 匹配 Tj 操作符（显示字符串）
var pdfTjRe = regexp.MustCompile(`\(([^)]*)\)\s*Tj`)

// pdfTJRe 匹配 TJ 操作符（显示字符串数组）
var pdfTJRe = regexp.MustCompile(`\[([^\]]*)\]\s*TJ`)

// pdfTJStringRe 匹配 TJ 数组中的字符串元素
var pdfTJStringRe = regexp.MustCompile(`\(([^)]*)\)`)

// parsePDF 解析 PDF 二进制数据
func parsePDF(data []byte) (*PDFDocument, error) {
	doc := &PDFDocument{
		Metadata: make(map[string]string),
	}

	// 验证 PDF 头
	header := pdfHeaderRe.FindSubmatch(data)
	if header == nil {
		return nil, fmt.Errorf("not a valid PDF file: missing %%PDF header")
	}
	doc.Metadata["pdf_version"] = string(header[1])

	// 提取 Info 字典中的元数据
	extractPDFMetadata(data, doc)

	// 计算页数并提取文本
	pages := extractPDFPages(data)
	doc.Pages = pages
	doc.PageCount = len(pages)

	return doc, nil
}

// extractPDFMetadata 从 PDF 数据中提取元数据
func extractPDFMetadata(data []byte, doc *PDFDocument) {
	// 查找 Info 字典
	// 在 trailer 中找到 /Info 引用后，定位 Info 对象
	// 简化实现：直接在整个文件中搜索 Info 字典内容

	// 搜索常见元数据字段
	metadataFields := []string{"Title", "Author", "Subject", "Creator", "Producer"}
	for _, field := range metadataFields {
		// 匹配 /Field (value) 模式
		pattern := regexp.MustCompile(`/` + field + `\s*\(([^)]*)\)`)
		if match := pattern.FindSubmatch(data); len(match) > 1 {
			value := decodePDFString(match[1])
			if value != "" {
				doc.Metadata[strings.ToLower(field)] = value
			}
		}
	}
}

// extractPDFPages 从 PDF 数据中提取各页文本
func extractPDFPages(data []byte) []PDFPage {
	var pages []PDFPage

	// 查找所有页面对象
	// 简化实现：将整个文件按页分割
	// 在简单 PDF 中，每页的内容流包含 BT...ET 文本块

	// 找到所有页面的内容流区域
	// 先找所有 /Type /Page 的位置
	pageMatches := pdfPageRe.FindAllIndex(data, -1)

	if len(pageMatches) == 0 {
		// 如果没有找到明确的页面标记，尝试将整个文件作为一个页面处理
		text := extractTextFromPDFData(data)
		if text != "" {
			pages = append(pages, PDFPage{
				PageNumber: 1,
				Content:    text,
			})
		}
		return pages
	}

	// 对每个页面区域提取文本
	for i, match := range pageMatches {
		start := match[0]
		end := len(data)
		if i+1 < len(pageMatches) {
			end = pageMatches[i+1][0]
		}

		// 在页面区域中查找内容流
		// 查找 stream...endstream 块
		pageData := data[start:end]
		text := extractTextFromPDFData(pageData)

		pages = append(pages, PDFPage{
			PageNumber: i + 1,
			Content:    strings.TrimSpace(text),
		})
	}

	return pages
}

// extractTextFromPDFData 从 PDF 数据块中提取文本
func extractTextFromPDFData(data []byte) string {
	var texts []string

	// 查找 stream...endstream 块中的文本
	streamRe := regexp.MustCompile(`stream\r?\n(.*?)endstream`)
	streamMatches := streamRe.FindAllSubmatch(data, -1)

	for _, streamMatch := range streamMatches {
		if len(streamMatch) < 2 {
			continue
		}
		streamData := streamMatch[1]

		// 从流数据中提取文本操作符
		text := extractTextFromStream(streamData)
		if text != "" {
			texts = append(texts, text)
		}
	}

	// 如果没有找到 stream 块，尝试直接在整个数据中搜索文本操作符
	if len(texts) == 0 {
		text := extractTextFromStream(data)
		if text != "" {
			texts = append(texts, text)
		}
	}

	return strings.Join(texts, "\n")
}

// extractTextFromStream 从 PDF 内容流中提取文本
func extractTextFromStream(data []byte) string {
	var texts []string

	// 提取 Tj 操作符的文本: (text) Tj
	tjMatches := pdfTjRe.FindAllSubmatch(data, -1)
	for _, match := range tjMatches {
		if len(match) > 1 {
			text := decodePDFString(match[1])
			if text != "" {
				texts = append(texts, text)
			}
		}
	}

	// 提取 TJ 操作符的文本: [(text1) num (text2)] TJ
	tjArrayMatches := pdfTJRe.FindAllSubmatch(data, -1)
	for _, arrMatch := range tjArrayMatches {
		if len(arrMatch) > 1 {
			// 在 TJ 数组中提取所有字符串
			strMatches := pdfTJStringRe.FindAllSubmatch(arrMatch[1], -1)
			var pageTexts []string
			for _, strMatch := range strMatches {
				if len(strMatch) > 1 {
					text := decodePDFString(strMatch[1])
					if text != "" {
						pageTexts = append(pageTexts, text)
					}
				}
			}
			if len(pageTexts) > 0 {
				texts = append(texts, strings.Join(pageTexts, ""))
			}
		}
	}

	return strings.Join(texts, " ")
}

// decodePDFString 解码 PDF 字符串
// 支持普通字符串和 UTF-16BE 编码字符串
func decodePDFString(data []byte) string {
	if len(data) == 0 {
		return ""
	}

	// 检查是否为 UTF-16BE 编码（以 BOM 0xFEFF 开头）
	if len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF {
		return decodeUTF16BE(data[2:])
	}

	// 处理 PDFDocEncoding / Latin-1
	// 处理转义序列
	result := make([]byte, 0, len(data))
	i := 0
	for i < len(data) {
		if data[i] == '\\' && i+1 < len(data) {
			i++
			switch data[i] {
			case 'n':
				result = append(result, '\n')
			case 'r':
				result = append(result, '\r')
			case 't':
				result = append(result, '\t')
			case 'b':
				result = append(result, '\b')
			case 'f':
				result = append(result, '\f')
			case '(':
				result = append(result, '(')
			case ')':
				result = append(result, ')')
			case '\\':
				result = append(result, '\\')
			default:
				// 八进制转义 \nnn
				if data[i] >= '0' && data[i] <= '7' {
					var octal byte
					count := 0
					for i < len(data) && data[i] >= '0' && data[i] <= '7' && count < 3 {
						octal = octal*8 + (data[i] - '0')
						i++
						count++
					}
					result = append(result, octal)
					continue
				}
				result = append(result, data[i])
			}
		} else {
			result = append(result, data[i])
		}
		i++
	}

	return string(result)
}

// decodeUTF16BE 解码 UTF-16BE 编码的字节
func decodeUTF16BE(data []byte) string {
	if len(data) < 2 {
		return ""
	}

	// 确保偶数长度
	if len(data)%2 != 0 {
		data = data[:len(data)-1]
	}

	// 转换为 []uint16
	u16s := make([]uint16, len(data)/2)
	for i := range u16s {
		u16s[i] = uint16(data[i*2])<<8 | uint16(data[i*2+1])
	}

	// 使用 utf16 包解码
	runes := utf16.Decode(u16s)
	return string(runes)
}

// buildMinimalPDF 构建一个最小化的有效 PDF 文件用于测试
// 包含两页，每页有一些文本
func buildMinimalPDF(pages []string) []byte {
	var buf bytes.Buffer

	// PDF 头
	buf.WriteString("%PDF-1.4\n")
	// 二进制注释，标记为二进制 PDF
	buf.WriteString("%\xe2\xe3\xcf\xd3\n")

	// 记录各对象偏移
	offsets := make(map[int]int)

	// 对象 1: Catalog
	offsets[1] = buf.Len()
	buf.WriteString("1 0 obj\n")
	buf.WriteString("<< /Type /Catalog /Pages 2 0 R >>\n")
	buf.WriteString("endobj\n")

	// 对象 2: Pages
	offsets[2] = buf.Len()
	buf.WriteString("2 0 obj\n")
	pageRefs := ""
	for i := 0; i < len(pages); i++ {
		pageRefs += fmt.Sprintf(" %d 0 R", 3+i*2)
	}
	buf.WriteString(fmt.Sprintf("<< /Type /Pages /Kids [%s ] /Count %d >>\n", pageRefs, len(pages)))
	buf.WriteString("endobj\n")

	// 为每页创建 Page 和 ContentStream 对象
	for i, pageText := range pages {
		// Page 对象
		objNum := 3 + i*2
		streamObjNum := 4 + i*2

		offsets[objNum] = buf.Len()
		buf.WriteString(fmt.Sprintf("%d 0 obj\n", objNum))
		buf.WriteString(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents %d 0 R >>\n", streamObjNum))
		buf.WriteString("endobj\n")

		// Content Stream 对象
		streamContent := fmt.Sprintf("BT /F1 12 Tf 100 700 Td (%s) Tj ET", escapePDFString(pageText))
		streamLen := len(streamContent)

		offsets[streamObjNum] = buf.Len()
		buf.WriteString(fmt.Sprintf("%d 0 obj\n", streamObjNum))
		buf.WriteString(fmt.Sprintf("<< /Length %d >>\n", streamLen))
		buf.WriteString("stream\n")
		buf.WriteString(streamContent)
		buf.WriteString("\nendstream\n")
		buf.WriteString("endobj\n")
	}

	// 对象: Font (简化)
	fontObjNum := 3 + len(pages)*2
	offsets[fontObjNum] = buf.Len()
	buf.WriteString(fmt.Sprintf("%d 0 obj\n", fontObjNum))
	buf.WriteString("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\n")
	buf.WriteString("endobj\n")

	// Info 对象
	infoObjNum := fontObjNum + 1
	offsets[infoObjNum] = buf.Len()
	buf.WriteString(fmt.Sprintf("%d 0 obj\n", infoObjNum))
	buf.WriteString("<< /Title (Test PDF) /Author (Test Author) /Creator (PDFLoader Test) >>\n")
	buf.WriteString("endobj\n")

	// xref 表
	xrefOffset := buf.Len()
	buf.WriteString("xref\n")
	buf.WriteString(fmt.Sprintf("0 %d\n", infoObjNum+1))
	buf.WriteString("0000000000 65535 f \n")
	for i := 1; i <= infoObjNum; i++ {
		if off, ok := offsets[i]; ok {
			buf.WriteString(fmt.Sprintf("%010d 00000 n \n", off))
		} else {
			buf.WriteString("0000000000 00000 f \n")
		}
	}

	// Trailer
	buf.WriteString("trailer\n")
	buf.WriteString(fmt.Sprintf("<< /Size %d /Root 1 0 R /Info %d 0 R >>\n", infoObjNum+1, infoObjNum))
	buf.WriteString("startxref\n")
	buf.WriteString(fmt.Sprintf("%d\n", xrefOffset))
	buf.WriteString("%%EOF\n")

	return buf.Bytes()
}

// escapePDFString 转义 PDF 字符串中的特殊字符
func escapePDFString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `(`, `\(`)
	s = strings.ReplaceAll(s, `)`, `\)`)
	return s
}
