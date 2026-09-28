package llm

import (
	"agentprimordia/internal/jsonutil" // perf-v6 round 6 Task 1
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

const (
	cohereDefaultBaseURL    = "https://api.cohere.com/v2"
	defaultCohereMaxContext = 128000
)

// CohereProvider 实现 Cohere v2 API 模型调用
type CohereProvider struct {
	config Config
	client *http.Client
	// v6.x 评估 §4.2 P1-4：流式专用 client（无整体超时 + ResponseHeaderTimeout）
	streamClient *http.Client
}

// NewCohereProvider 创建 Cohere Provider
func NewCohereProvider(cfg Config) (*CohereProvider, error) {
	if cfg.APIKey == "" {
		return nil, ErrAPIKeyRequired
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = cohereDefaultBaseURL
	}
	cfg.BaseURL = strings.TrimRight(baseURL, "/")

	if cfg.Model == "" {
		cfg.Model = "command-r-plus"
	}

	return &CohereProvider{
		config:       cfg,
		client:       NewDefaultLLMClient(defaultTimeout),
		streamClient: NewDefaultLLMStreamClient(),
	}, nil
}

func (p *CohereProvider) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	model := p.resolveModel(req)

	body := map[string]any{
		"model":    model,
		"messages": p.buildMessages(req.Messages),
	}
	if temp := ResolveTemperature(req.Temperature, p.config.Temperature); temp != nil {
		body["temperature"] = *temp
	}
	if maxTok := p.resolveMaxTokens(req); maxTok > 0 {
		body["max_tokens"] = maxTok
	}
	if req.ResponseFormat != nil {
		p.injectResponseFormat(body, req.ResponseFormat)
	}

	raw, err := p.doRequest(ctx, "/chat", body)
	if err != nil {
		return nil, err
	}

	var resp cohereChatResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrResponseParseFailed, err)
	}

	content := ""
	if len(resp.Message.Content) > 0 {
		content = resp.Message.Content[0].Text
	}

	return &CompletionResponse{
		ID:      resp.ID,
		Model:   model,
		Content: content,
		Role:    resp.Message.Role,
		Usage: Usage{
			PromptTokens:     resp.Usage.Tokens.InputTokens,
			CompletionTokens: resp.Usage.Tokens.OutputTokens,
			TotalTokens:      resp.Usage.Tokens.InputTokens + resp.Usage.Tokens.OutputTokens,
		},
	}, nil
}

func (p *CohereProvider) Stream(ctx context.Context, req *CompletionRequest) (<-chan Chunk, error) {
	model := p.resolveModel(req)

	body := map[string]any{
		"model":    model,
		"messages": p.buildMessages(req.Messages),
		"stream":   true,
	}
	if temp := ResolveTemperature(req.Temperature, p.config.Temperature); temp != nil {
		body["temperature"] = *temp
	}
	if maxTok := p.resolveMaxTokens(req); maxTok > 0 {
		body["max_tokens"] = maxTok
	}
	if req.ResponseFormat != nil {
		p.injectResponseFormat(body, req.ResponseFormat)
	}

	bodyBytes, err := jsonutil.MarshalBody(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.config.BaseURL+"/chat", bytes.NewReader(bodyBytes))
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
		return nil, NewHTTPError("cohere", resp.StatusCode, respBody, resp.Header)
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

			var evt cohereStreamEvent
			// perf-v6 round 8 Task 1：使用 pooled stringReader 避免每条 SSE 消息分配
			if err := jsonutil.DecodeString(data, &evt); err != nil {
				continue
			}

			switch evt.Type {
			case "content-delta":
				if evt.Delta != nil && len(evt.Delta.Message.Content) > 0 {
					select {
					case ch <- Chunk{Content: evt.Delta.Message.Content[0].Text}:
					case <-ctx.Done():
						return
					}
				}
			case "message_end":
				chunk := Chunk{Done: true}
				if evt.Usage != nil {
					chunk.Usage = &Usage{
						PromptTokens:     evt.Usage.Tokens.InputTokens,
						CompletionTokens: evt.Usage.Tokens.OutputTokens,
						TotalTokens:      evt.Usage.Tokens.InputTokens + evt.Usage.Tokens.OutputTokens,
					}
				}
				select {
				case ch <- chunk:
				case <-ctx.Done():
				}
				return
			}
		}
		if err := scanner.Err(); err != nil {
			slog.Warn("Cohere 流式读取错误", "error", err)
		}
		select {
		case ch <- Chunk{Done: true}:
		case <-ctx.Done():
		}
	}()

	return ch, nil
}

func (p *CohereProvider) CallTools(ctx context.Context, req *ToolCallRequest) (*ToolCallResponse, error) {
	model := p.config.Model
	if req.Model != "" {
		model = req.Model
	}

	tools := make([]openaiTool, len(req.Tools)) // perf-v6 Task 3：复用 openai typed struct
	for i, t := range req.Tools {
		var paramsRaw json.RawMessage
		if t.Function.Parameters != nil {
			if b, err := json.Marshal(t.Function.Parameters); err == nil {
				paramsRaw = b
			}
		}
		tools[i] = openaiTool{
			Type: t.Type,
			Function: openaiToolFunction{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  paramsRaw,
			},
		}
	}

	body := map[string]any{
		"model":    model,
		"messages": p.buildMessages(req.Messages),
		"tools":    tools,
	}
	if temp := ResolveTemperature(nil, p.config.Temperature); temp != nil {
		body["temperature"] = *temp
	}
	if p.config.MaxTokens > 0 {
		body["max_tokens"] = p.config.MaxTokens
	}

	raw, err := p.doRequest(ctx, "/chat", body)
	if err != nil {
		return nil, err
	}

	var resp cohereChatResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrResponseParseFailed, err)
	}

	content := ""
	if len(resp.Message.Content) > 0 {
		content = resp.Message.Content[0].Text
	}

	result := &ToolCallResponse{
		Content: content,
		Usage: Usage{
			PromptTokens:     resp.Usage.Tokens.InputTokens,
			CompletionTokens: resp.Usage.Tokens.OutputTokens,
			TotalTokens:      resp.Usage.Tokens.InputTokens + resp.Usage.Tokens.OutputTokens,
		},
	}

	if len(resp.Message.ToolCalls) > 0 {
		result.ToolCalls = make([]FunctionCall, len(resp.Message.ToolCalls))
		for i, tc := range resp.Message.ToolCalls {
			result.ToolCalls[i] = FunctionCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			}
		}
	}

	return result, nil
}

func (p *CohereProvider) Info() ModelInfo {
	contextSizes := map[string]int{
		"command-r-plus": 128000,
		"command-r":      128000,
		"command":        4096,
		"command-light":  4096,
	}
	maxCtx := defaultCohereMaxContext
	if c, ok := contextSizes[p.config.Model]; ok {
		maxCtx = c
	}
	return ModelInfo{
		Name:              p.config.Model,
		Provider:          "cohere",
		MaxContext:        maxCtx,
		SupportsTools:     true,
		SupportsStreaming: true,
	}
}

// ===== 内部方法 =====

func (p *CohereProvider) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	req.Header.Set("User-Agent", userAgent)
}

func (p *CohereProvider) doRequest(ctx context.Context, path string, body any) (json.RawMessage, error) {
	bodyBytes, err := jsonutil.MarshalBody(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.config.BaseURL+path, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	p.setHeaders(httpReq)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	limitedReader := io.LimitReader(resp.Body, maxResponseSize)
	respBody, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error *APIError `json:"error"`
		}
		parsed := json.Unmarshal(respBody, &errResp) == nil && errResp.Error != nil
		// perf-v6 round 8 Task 3：携带 Retry-After + 错误分类
		return nil, NewHTTPErrorOrAPIError("cohere", resp.StatusCode, respBody, resp.Header, errResp.Error, parsed)
	}

	return respBody, nil
}

// buildMessages 将通用 ChatMessage 转换为 Cohere v2 格式
func (p *CohereProvider) buildMessages(msgs []ChatMessage) []map[string]any {
	return BuildOpenAIMessages(msgs)
}

func (p *CohereProvider) resolveModel(req *CompletionRequest) string {
	if req != nil && req.Model != "" {
		return req.Model
	}
	return ResolveModel("", p.config.Model)
}

func (p *CohereProvider) resolveMaxTokens(req *CompletionRequest) int {
	if req != nil && req.MaxTokens > 0 {
		return req.MaxTokens
	}
	return p.config.MaxTokens
}

// ===== Cohere v2 API 响应类型 =====

type cohereChatResponse struct {
	ID      string `json:"id"`
	Message struct {
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		ToolCalls []struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Function struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
	} `json:"message"`
	Usage struct {
		BilledUnits struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"billed_units"`
		Tokens struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"tokens"`
	} `json:"usage"`
	FinishReason string `json:"finish_reason"`
}

type cohereStreamEvent struct {
	Type  string `json:"type"`
	Delta *struct {
		Message struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	} `json:"delta,omitempty"`
	Usage *struct {
		BilledUnits struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"billed_units"`
		Tokens struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"tokens"`
	} `json:"usage,omitempty"`
}

// injectResponseFormat 将 ResponseFormat 注入 Cohere 请求体
// Cohere V2 API 支持 response_format 字段，兼容 OpenAI 格式
func (p *CohereProvider) injectResponseFormat(body map[string]any, rf *ResponseFormat) {
	switch rf.Type {
	case ResponseFormatJSONObject:
		body["response_format"] = map[string]any{"type": "json_object"}
	case ResponseFormatJSONSchema:
		body["response_format"] = buildOpenAIResponseFormat(rf)
	}
}
