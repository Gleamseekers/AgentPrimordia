package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Gleamseekers/AgentPrimordia/internal/jsonutil" // perf-v6 round 6 Task 1：统一 JSON 序列化
)

const (
	anthropicDefaultBaseURL    = "https://api.anthropic.com"
	anthropicAPIVersion        = "2023-06-01"
	defaultAnthropicMaxContext = 200000
	defaultAnthropicMaxTokens  = 4096
)

// AnthropicProvider 实现 Claude 系列模型调用
//
// v6.x（评估报告 §五.1）：嵌入 BaseProvider 复用共享 HTTP 底座
// （连接池 + 限流响应体 + 统一错误包装），消除重复的 doRequest。
type AnthropicProvider struct {
	config Config
	*BaseProvider
}

// NewAnthropicProvider 创建 Anthropic Claude Provider
func NewAnthropicProvider(cfg Config) (*AnthropicProvider, error) {
	if cfg.APIKey == "" {
		return nil, ErrAPIKeyRequired
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = anthropicDefaultBaseURL
	}
	cfg.BaseURL = strings.TrimRight(baseURL, "/")

	if cfg.Model == "" {
		cfg.Model = "claude-sonnet-4-20250514"
	}

	return &AnthropicProvider{
		config: cfg,
		// perf-v5 Task 6：共享 transport（连接池复用 + HTTP/2），
		// 统一由 BaseProvider 持有。
		BaseProvider: NewBaseProvider(defaultTimeout),
	}, nil
}

func (p *AnthropicProvider) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	model := p.resolveModel(req)
	messages, systemMsg := p.convertMessages(req.Messages)

	// perf-v6 round 4 Task 1：typed struct 减少反射
	anthReq := anthropicRequest{
		Model:     model,
		Messages:  messages,
		MaxTokens: p.resolveMaxTokens(req),
		System:    systemMsg,
	}
	if temp := ResolveTemperature(req.Temperature, p.config.Temperature); temp != nil {
		f32 := float32(*temp)
		anthReq.Temperature = &f32
	}

	// 结构化输出：Anthropic 通过 tool_choice + 单tool注入实现。
	// buildStructuredOutputSafe 对全部 ResponseFormat 形态安全（P0 修复：
	// json_object 无 schema 时走通用兜底 tool，不再 nil deref panic）。
	if req.ResponseFormat != nil {
		tool, choice, ok, err := p.buildStructuredOutputSafe(req.ResponseFormat)
		if err != nil {
			return nil, err
		}
		if ok {
			anthReq.Tools = append(anthReq.Tools, tool)
			anthReq.ToolChoice = &choice
		}
	}

	raw, err := p.doRequest(ctx, "/v1/messages", anthReq)
	if err != nil {
		return nil, err
	}

	var resp anthropicMessagesResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrResponseParseFailed, err)
	}

	content := ""
	if len(resp.Content) > 0 {
		content = resp.Content[0].Text
	}

	return &CompletionResponse{
		ID:      resp.ID,
		Model:   model,
		Content: content,
		Role:    "assistant",
		Usage: Usage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
	}, nil
}

func (p *AnthropicProvider) Stream(ctx context.Context, req *CompletionRequest) (<-chan Chunk, error) {
	model := p.resolveModel(req)
	messages, systemMsg := p.convertMessages(req.Messages)

	// perf-v6 round 4 Task 1：typed struct 减少反射
	anthReq := anthropicRequest{
		Model:     model,
		Messages:  messages,
		MaxTokens: p.resolveMaxTokens(req),
		System:    systemMsg,
		Stream:    true,
	}
	if temp := ResolveTemperature(req.Temperature, p.config.Temperature); temp != nil {
		f32 := float32(*temp)
		anthReq.Temperature = &f32
	}

	bodyBytes, err := jsonutil.MarshalBody(anthReq) // perf-v6 round 6 Task 1
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.config.BaseURL+"/v1/messages", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	p.setHeaders(httpReq)

	// v6.x 评估 §4.2 P1-4：流式路径使用无整体超时的 stream client
	resp, err := p.streamClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
		// perf-v6 round 8 Task 3：携带 Retry-After + 错误分类
		return nil, NewHTTPError("anthropic", resp.StatusCode, respBody, resp.Header)
	}

	ch := make(chan Chunk, 32)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")

			var sseEvt anthropicSSEEvent
			// perf-v6 round 8 Task 1：使用 pooled stringReader 避免每条 SSE 消息分配
			if err := jsonutil.DecodeString(data, &sseEvt); err != nil {
				continue
			}

			var chunk Chunk
			switch sseEvt.Type {
			case "content_block_delta":
				if sseEvt.Delta != nil {
					chunk = Chunk{Content: sseEvt.Delta.Text}
				}
			case "message_delta":
				if sseEvt.Usage != nil {
					chunk = Chunk{
						Usage: &Usage{
							CompletionTokens: sseEvt.Usage.OutputTokens,
						},
					}
				}
			case "message_stop":
				chunk = Chunk{Done: true}
			case "error":
				chunk = Chunk{Done: true}
			}

			select {
			case ch <- chunk:
			case <-ctx.Done():
				return
			}

			if chunk.Done {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			slog.Warn("Anthropic 流式读取错误", "error", err)
		}
		select {
		case ch <- Chunk{Done: true}:
		case <-ctx.Done():
		}
	}()

	return ch, nil
}

func (p *AnthropicProvider) CallTools(ctx context.Context, req *ToolCallRequest) (*ToolCallResponse, error) {
	model := p.config.Model
	if req.Model != "" {
		model = req.Model
	}
	messages, systemMsg := p.convertMessages(req.Messages)

	tools := make([]anthropicTool, len(req.Tools))
	for i, t := range req.Tools {
		tools[i] = anthropicTool{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			InputSchema: t.Function.Parameters,
		}
	}

	body := map[string]any{
		"model":      model,
		"messages":   messages,
		"max_tokens": p.resolveMaxTokens(nil),
		"tools":      tools,
	}
	if systemMsg != "" {
		body["system"] = systemMsg
	}

	raw, err := p.doRequest(ctx, "/v1/messages", body)
	if err != nil {
		return nil, err
	}

	var resp anthropicMessagesResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrResponseParseFailed, err)
	}

	result := &ToolCallResponse{
		Usage: Usage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
	}

	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			result.Content = block.Text
		case "tool_use":
			result.ToolCalls = append(result.ToolCalls, FunctionCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: block.Input,
			})
		}
	}

	return result, nil
}

func (p *AnthropicProvider) Info() ModelInfo {
	contextSizes := map[string]int{
		"claude-sonnet-4-20250514":   200000,
		"claude-3-5-sonnet-20241022": 200000,
		"claude-3-5-haiku-20241022":  200000,
		"claude-3-opus-20240229":     200000,
		"claude-3-haiku-20240307":    200000,
	}
	maxCtx := defaultAnthropicMaxContext
	if c, ok := contextSizes[p.config.Model]; ok {
		maxCtx = c
	}
	return ModelInfo{
		Name:              p.config.Model,
		Provider:          "anthropic",
		MaxContext:        maxCtx,
		SupportsTools:     true,
		SupportsStreaming: true,
	}
}

// ===== 内部方法 =====

func (p *AnthropicProvider) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.config.APIKey)
	req.Header.Set("anthropic-version", anthropicAPIVersion)
	req.Header.Set("User-Agent", userAgent)
}

// doRequest 委托 BaseProvider.DoRequestCustom 复用共享 HTTP 底座。
//
// v6.x：错误封装行为保持不变（NewHTTPError 统一包装），但 HTTP client /
// 响应体限流读取逻辑不再重复实现。
func (p *AnthropicProvider) doRequest(ctx context.Context, path string, body any) (json.RawMessage, error) {
	headers := map[string]string{
		"x-api-key":         p.config.APIKey,
		"anthropic-version": anthropicAPIVersion,
	}
	raw, err := p.BaseProvider.DoRequestCustom(ctx, p.config.BaseURL+path, "POST", headers, body, "anthropic")
	if err != nil {
		return nil, err
	}
	return raw, nil
}

// convertMessages 将通用 ChatMessage 转换为 Anthropic 格式
// Anthropic 要求 system 消息不在 messages 数组中，而是单独的 system 参数
// convertMessages 转换 ChatMessage 列表为 Anthropic 消息格式（perf-v6 round 5 Task 2）
// 返回 typed []anthropicMessage 而非 []map[string]any
func (p *AnthropicProvider) convertMessages(msgs []ChatMessage) ([]anthropicMessage, string) {
	var systemMsg string
	result := make([]anthropicMessage, 0, len(msgs))

	for _, m := range msgs {
		if m.Role == "system" {
			systemMsg = m.Content
			continue
		}
		result = append(result, anthropicMessage{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	return result, systemMsg
}

func (p *AnthropicProvider) resolveModel(req *CompletionRequest) string {
	if req != nil && req.Model != "" {
		return req.Model
	}
	return p.config.Model
}

func (p *AnthropicProvider) resolveMaxTokens(req *CompletionRequest) int {
	if req != nil && req.MaxTokens > 0 {
		return req.MaxTokens
	}
	if p.config.MaxTokens > 0 {
		return p.config.MaxTokens
	}
	return defaultAnthropicMaxTokens
}

// buildStructuredOutputSafe 构建结构化输出 tool（perf-v6 round 4 Task 1），
// 对所有 ResponseFormat 形态安全（P0 修复）。
//
// 语义矩阵：
//   - rf == nil 或 Type == text：不注入（ok=false, err=nil）；
//   - Type == json_object 且 JSONSchema == nil：Anthropic 无原生 json_object
//     模式，注入通用兜底 tool（宽松 object schema）保证结构化意图不丢失，
//     不再像旧实现那样 nil deref panic；
//   - Type == json_schema 且 JSONSchema == nil：调用方错误，返回明确 error；
//   - JSONSchema != nil：按 schema 构建（原行为）。
func (p *AnthropicProvider) buildStructuredOutputSafe(rf *ResponseFormat) (tool anthropicTool, choice anthropicToolChoice, ok bool, err error) {
	if rf == nil || rf.Type == ResponseFormatText {
		return anthropicTool{}, anthropicToolChoice{}, false, nil
	}
	if rf.JSONSchema == nil {
		if rf.Type == ResponseFormatJSONObject {
			// 通用兜底：宽松 object schema + 明确描述，引导模型返回 JSON。
			tool = anthropicTool{
				Name:        "structured_output",
				Description: "Return the response as a single JSON object.",
				InputSchema: map[string]any{"type": "object"},
			}
			choice = anthropicToolChoice{Type: "tool", Name: tool.Name}
			return tool, choice, true, nil
		}
		// json_schema 模式缺少 schema 定义属无效请求。
		return anthropicTool{}, anthropicToolChoice{}, false,
			fmt.Errorf("llm: anthropic structured output requires JSONSchema when ResponseFormat.Type=%q", rf.Type)
	}
	schemaName := rf.JSONSchema.Name
	if schemaName == "" {
		schemaName = "structured_output"
	}
	tool = anthropicTool{
		Name:        schemaName,
		Description: rf.JSONSchema.Description,
		InputSchema: rf.JSONSchema.Schema,
	}
	choice = anthropicToolChoice{
		Type: "tool",
		Name: schemaName,
	}
	return tool, choice, true, nil
}

// ===== Anthropic API 响应类型 =====

type anthropicMessagesResponse struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content []struct {
		Type  string `json:"type"`
		Text  string `json:"text,omitempty"`
		ID    string `json:"id,omitempty"`
		Name  string `json:"name,omitempty"`
		Input string `json:"input,omitempty"`
	} `json:"content"`
	Model string `json:"model"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	StopReason string `json:"stop_reason,omitempty"`
}

type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

// anthropicMessage 单条消息 typed struct（perf-v6 round 5 Task 2）
type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// anthropicRequest 完整请求 typed struct（perf-v6 round 4 Task 1）
// 替代 map[string]any 反射序列化
type anthropicRequest struct {
	Model       string               `json:"model"`
	Messages    []anthropicMessage   `json:"messages"`
	MaxTokens   int                  `json:"max_tokens"`
	System      string               `json:"system,omitempty"`
	Temperature *float32             `json:"temperature,omitempty"`
	Stream      bool                 `json:"stream,omitempty"`
	Tools       []anthropicTool      `json:"tools,omitempty"`
	ToolChoice  *anthropicToolChoice `json:"tool_choice,omitempty"`
}

type anthropicToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

type anthropicSSEEvent struct {
	Type  string `json:"type"`
	Delta *struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta,omitempty"`
	Usage *struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage,omitempty"`
}
