package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/tools"
)

// Chunk 表示文本切分后的一个块
type Chunk struct {
	Content  string            `json:"content"`
	Metadata map[string]string `json:"metadata,omitempty"`
	Index    int               `json:"index"`
}

// TextSplitter 文本切分器，按指定分隔符和重叠长度切分
type TextSplitter struct {
	ChunkSize    int
	ChunkOverlap int
	Separator    string
}

// NewTextSplitter 创建文本切分器
func NewTextSplitter(chunkSize, chunkOverlap int, separator string) *TextSplitter {
	if separator == "" {
		separator = "\n\n"
	}
	if chunkSize <= 0 {
		chunkSize = 1000
	}
	if chunkOverlap < 0 {
		chunkOverlap = 0
	}
	if chunkOverlap >= chunkSize {
		chunkOverlap = chunkSize / 2
	}
	return &TextSplitter{
		ChunkSize:    chunkSize,
		ChunkOverlap: chunkOverlap,
		Separator:    separator,
	}
}

// Split 将文本切分为多个块
func (s *TextSplitter) Split(text string) []Chunk {
	if text == "" {
		return nil
	}

	var chunks []Chunk
	parts := strings.Split(text, s.Separator)

	current := ""
	idx := 0

	for _, part := range parts {
		// 如果当前块 + 分隔符 + 新部分超过大小限制
		candidate := current
		if candidate != "" {
			candidate += s.Separator
		}
		candidate += part

		if len(candidate) > s.ChunkSize && current != "" {
			chunks = append(chunks, Chunk{
				Content: current,
				Index:   idx,
			})
			idx++

			// 计算重叠：保留当前块的尾部
			if s.ChunkOverlap > 0 && len(current) > s.ChunkOverlap {
				overlapText := current[len(current)-s.ChunkOverlap:]
				current = overlapText + s.Separator + part
			} else {
				current = part
			}
		} else {
			current = candidate
		}

		// 如果单个部分就超过 chunkSize，需要强制切分
		if len(current) > s.ChunkSize {
			chunks = append(chunks, Chunk{
				Content: current[:s.ChunkSize],
				Index:   idx,
			})
			idx++
			remaining := current[s.ChunkSize:]
			if s.ChunkOverlap > 0 && len(current[:s.ChunkSize]) > s.ChunkOverlap {
				overlapText := current[:s.ChunkSize][len(current[:s.ChunkSize])-s.ChunkOverlap:]
				current = overlapText + remaining
			} else {
				current = remaining
			}
		}
	}

	if current != "" {
		chunks = append(chunks, Chunk{
			Content: current,
			Index:   idx,
		})
	}

	return chunks
}

// RecursiveTextSplitter 递归文本切分器（按多种分隔符层级切分）
type RecursiveTextSplitter struct {
	ChunkSize    int
	ChunkOverlap int
	Separators   []string // 按优先级尝试，如 ["\n\n", "\n", " ", ""]
}

// NewRecursiveTextSplitter 创建递归文本切分器
func NewRecursiveTextSplitter(chunkSize, chunkOverlap int, separators []string) *RecursiveTextSplitter {
	if len(separators) == 0 {
		separators = []string{"\n\n", "\n", " ", ""}
	}
	if chunkSize <= 0 {
		chunkSize = 1000
	}
	if chunkOverlap < 0 {
		chunkOverlap = 0
	}
	if chunkOverlap >= chunkSize {
		chunkOverlap = chunkSize / 2
	}
	return &RecursiveTextSplitter{
		ChunkSize:    chunkSize,
		ChunkOverlap: chunkOverlap,
		Separators:   separators,
	}
}

// Split 递归切分文本
func (s *RecursiveTextSplitter) Split(text string) []Chunk {
	if text == "" {
		return nil
	}
	rawChunks := s.recursiveSplit(text, s.Separators)
	var chunks []Chunk
	for i, c := range rawChunks {
		chunks = append(chunks, Chunk{
			Content: c,
			Index:   i,
		})
	}
	return chunks
}

// recursiveSplit 递归地按分隔符层级切分文本
func (s *RecursiveTextSplitter) recursiveSplit(text string, separators []string) []string {
	if len(text) <= s.ChunkSize {
		return []string{text}
	}

	if len(separators) == 0 {
		// 没有更多分隔符，强制按字符切分
		return s.forceSplit(text)
	}

	sep := separators[0]
	remainingSeps := separators[1:]

	if sep == "" {
		// 空分隔符表示按字符强制切分
		return s.forceSplit(text)
	}

	parts := strings.Split(text, sep)
	var result []string
	current := ""

	for _, part := range parts {
		candidate := current
		if candidate != "" {
			candidate += sep
		}
		candidate += part

		if len(candidate) > s.ChunkSize && current != "" {
			// 当前块已完成
			result = append(result, current)
			// 重叠处理
			if s.ChunkOverlap > 0 && len(current) > s.ChunkOverlap {
				current = current[len(current)-s.ChunkOverlap:] + sep + part
			} else {
				current = part
			}
		} else {
			current = candidate
		}

		// 如果单个部分仍然太大，递归使用下一级分隔符
		if len(current) > s.ChunkSize {
			subChunks := s.recursiveSplit(current, remainingSeps)
			if len(subChunks) > 1 {
				result = append(result, subChunks[:len(subChunks)-1]...)
				current = subChunks[len(subChunks)-1]
			}
		}
	}

	if current != "" {
		result = append(result, current)
	}

	return result
}

// forceSplit 强制按字符切分
func (s *RecursiveTextSplitter) forceSplit(text string) []string {
	var result []string
	for i := 0; i < len(text); i += s.ChunkSize - s.ChunkOverlap {
		end := i + s.ChunkSize
		if end > len(text) {
			end = len(text)
		}
		result = append(result, text[i:end])
		if end >= len(text) {
			break
		}
	}
	return result
}

// FixedLengthSplitter 固定长度切分器，在精确字符边界处切分
type FixedLengthSplitter struct {
	ChunkSize    int
	ChunkOverlap int
}

// NewFixedLengthSplitter 创建固定长度切分器
func NewFixedLengthSplitter(chunkSize, chunkOverlap int) *FixedLengthSplitter {
	if chunkSize <= 0 {
		chunkSize = 1000
	}
	if chunkOverlap < 0 {
		chunkOverlap = 0
	}
	if chunkOverlap >= chunkSize {
		chunkOverlap = chunkSize / 2
	}
	return &FixedLengthSplitter{
		ChunkSize:    chunkSize,
		ChunkOverlap: chunkOverlap,
	}
}

// Split 将文本按固定长度切分
func (s *FixedLengthSplitter) Split(text string) []Chunk {
	if text == "" {
		return nil
	}

	var chunks []Chunk
	step := s.ChunkSize - s.ChunkOverlap
	if step <= 0 {
		step = 1
	}

	idx := 0
	for i := 0; i < len(text); i += step {
		end := i + s.ChunkSize
		if end > len(text) {
			end = len(text)
		}
		chunks = append(chunks, Chunk{
			Content: text[i:end],
			Index:   idx,
		})
		idx++
		if end >= len(text) {
			break
		}
	}

	return chunks
}

// SentenceSplitter 句子切分器，在句子边界处切分
type SentenceSplitter struct {
	ChunkSize    int
	ChunkOverlap int
}

// NewSentenceSplitter 创建句子切分器
func NewSentenceSplitter(chunkSize, chunkOverlap int) *SentenceSplitter {
	if chunkSize <= 0 {
		chunkSize = 1000
	}
	if chunkOverlap < 0 {
		chunkOverlap = 0
	}
	if chunkOverlap >= chunkSize {
		chunkOverlap = chunkSize / 2
	}
	return &SentenceSplitter{
		ChunkSize:    chunkSize,
		ChunkOverlap: chunkOverlap,
	}
}

// Split 将文本按句子边界切分
func (s *SentenceSplitter) Split(text string) []Chunk {
	if text == "" {
		return nil
	}

	sentences := splitSentences(text)
	var chunks []Chunk
	current := ""
	idx := 0

	for _, sentence := range sentences {
		candidate := current + sentence

		if len(candidate) > s.ChunkSize && current != "" {
			chunks = append(chunks, Chunk{
				Content: current,
				Index:   idx,
			})
			idx++

			// 重叠：保留最后几个句子
			if s.ChunkOverlap > 0 {
				overlapText := getOverlapSentences(current, s.ChunkOverlap)
				current = overlapText + sentence
			} else {
				current = sentence
			}
		} else {
			current = candidate
		}
	}

	if current != "" {
		chunks = append(chunks, Chunk{
			Content: current,
			Index:   idx,
		})
	}

	return chunks
}

// splitSentences 使用简单启发式规则切分句子
// 识别句号、问号、感叹号后跟空格或文本末尾的位置
// 中文标点（。？！）后不需要空格即可视为句子边界
func splitSentences(text string) []string {
	var sentences []string
	var current strings.Builder

	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		current.WriteRune(runes[i])

		// 检查是否为句子结束标记
		if isSentenceEndRune(runes[i]) {
			// 中文标点后直接视为句子边界（无需空格）
			// 英文标点后需要空格或文本结束
			isChinesePunct := runes[i] == '。' || runes[i] == '？' || runes[i] == '！'
			if isChinesePunct || i+1 >= len(runes) || runes[i+1] == ' ' || runes[i+1] == '\n' || runes[i+1] == '\r' {
				sentence := strings.TrimSpace(current.String())
				if sentence != "" {
					sentences = append(sentences, sentence)
				}
				current.Reset()
			}
		}
	}

	// 处理剩余文本
	remaining := strings.TrimSpace(current.String())
	if remaining != "" {
		sentences = append(sentences, remaining)
	}

	return sentences
}

// isSentenceEndRune 判断 rune 是否为句子结束标记
func isSentenceEndRune(ch rune) bool {
	return ch == '.' || ch == '?' || ch == '!' || ch == '。' || ch == '？' || ch == '！'
}

// getOverlapSentences 从文本尾部获取不超过 maxLen 的重叠内容
func getOverlapSentences(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[len(text)-maxLen:]
}

// --- TextSplitterTool tool封装 ---

// TextSplitterTool 文本切分tool，实现 tools.Tool 接口
type TextSplitterTool struct{}

// NewTextSplitterTool 创建文本切分tool
func NewTextSplitterTool() *TextSplitterTool {
	return &TextSplitterTool{}
}

func (t *TextSplitterTool) Name() string { return "text_splitter" }

func (t *TextSplitterTool) Description() string {
	return "Split text into chunks using various strategies: simple (by separator), recursive (by multiple separators), fixed_length (exact character boundaries), or sentence (sentence boundaries)."
}

func (t *TextSplitterTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "action": {"type": "string", "enum": ["split"], "description": "The operation to perform"},
    "text": {"type": "string", "description": "The text to split"},
    "strategy": {"type": "string", "enum": ["simple", "recursive", "fixed_length", "sentence"], "description": "Splitting strategy (default: simple)"},
    "chunk_size": {"type": "integer", "description": "Maximum chunk size in characters (default: 1000)"},
    "chunk_overlap": {"type": "integer", "description": "Overlap between chunks in characters (default: 0)"},
    "separator": {"type": "string", "description": "Separator for simple strategy (default: double newline)"},
    "separators": {"type": "array", "items": {"type": "string"}, "description": "Ordered separators for recursive strategy (default: [double newline, newline, space])"}
  },
  "required": ["action", "text"]
}`)
}

func (t *TextSplitterTool) Execute(ctx context.Context, args json.RawMessage) (*tools.Result, error) {
	var params struct {
		Action       string   `json:"action"`
		Text         string   `json:"text"`
		Strategy     string   `json:"strategy"`
		ChunkSize    int      `json:"chunk_size"`
		ChunkOverlap int      `json:"chunk_overlap"`
		Separator    string   `json:"separator"`
		Separators   []string `json:"separators"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return tools.NewErrorResult(fmt.Sprintf("invalid arguments: %v", err)), nil
	}

	if params.Action != "split" {
		return tools.NewErrorResult(fmt.Sprintf("unknown action: %s", params.Action)), nil
	}
	if params.Text == "" {
		return tools.NewErrorResult("text is required"), nil
	}

	chunkSize := params.ChunkSize
	if chunkSize <= 0 {
		chunkSize = 1000
	}
	chunkOverlap := params.ChunkOverlap
	if chunkOverlap < 0 {
		chunkOverlap = 0
	}

	var chunks []Chunk

	switch params.Strategy {
	case "simple", "":
		sep := params.Separator
		if sep == "" {
			sep = "\n\n"
		}
		splitter := NewTextSplitter(chunkSize, chunkOverlap, sep)
		chunks = splitter.Split(params.Text)
	case "recursive":
		seps := params.Separators
		if len(seps) == 0 {
			seps = []string{"\n\n", "\n", " ", ""}
		}
		splitter := NewRecursiveTextSplitter(chunkSize, chunkOverlap, seps)
		chunks = splitter.Split(params.Text)
	case "fixed_length":
		splitter := NewFixedLengthSplitter(chunkSize, chunkOverlap)
		chunks = splitter.Split(params.Text)
	case "sentence":
		splitter := NewSentenceSplitter(chunkSize, chunkOverlap)
		chunks = splitter.Split(params.Text)
	default:
		return tools.NewErrorResult(fmt.Sprintf("unknown strategy: %s", params.Strategy)), nil
	}

	output, err := json.MarshalIndent(chunks, "", "  ")
	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("marshal error: %v", err)), nil
	}

	return tools.NewResult(string(output)), nil
}
