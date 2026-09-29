package llm

import (
	"github.com/Gleamseekers/AgentPrimordia/internal/jsonutil" // perf-v6 round 6 Task 1
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
	mistralDefaultBaseURL    = "https://api.mistral.ai/v1"
	defaultMistralMaxContext = 32768
	defaultMistralEmbedModel = "mistral-embed"
)

// MistralProvider 实现 Mistral AI 模型调用
// Mistral API 与 OpenAI 兼容，复用 openaiChatResponse 响应类型
type MistralProvider struct {
	config Config
	client *http.Client
	// v6.x 评估 §4.2 P1-4：流式专用 client（无整体超时 + ResponseHeaderTimeout）
	streamClient *http.Client
}

// NewMistralProvider 创建 Mistral Provider
func NewMistralProvider(cfg Config) (*MistralProvider, error) {
	if cfg.APIKey == "" {
		return nil, ErrAPIKeyRequired
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = mistralDefaultBaseURL
	}
	cfg.BaseURL = strings.TrimRight(baseURL, "/")

	if cfg.Model == "" {
		cfg.Model = "mistral-large-latest"
	}

	return &MistralProvider{
		config:       cfg,
		client:       NewDefaultLLMClient(defaultTimeout),
		streamClient: NewDefaultLLMStreamClient(),
	}, nil
}

func (p *MistralProvider) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	model := p.resolveModel(req)

	body := map[string]any{
		"model":    model,
		"messages": p.buildMessages(req.Messages),
	}
	if temp := ResolveTemperature(req.Temperature, p.config.Temperature); temp != nil {
		body["temperature"] = *temp
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	} else if p.config.MaxTokens > 0 {
		body["max_tokens"] = p.config.MaxTokens
	}
	if req.ResponseFormat != nil {
		body["response_format"] = buildOpenAIResponseFormat(req.ResponseFormat)
	}

	raw, err := p.doRequest(ctx, "/chat/completions", body)
	if err != nil {
		return nil, err
	}

	var resp openaiChatResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrResponseParseFailed, err)
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	if len(resp.Choices) == 0 {
		return nil, ErrEmptyResponse
	}

	choice := resp.Choices[0]
	return &CompletionResponse{
		ID:      resp.ID,
		Model:   model,
		Content: choice.Message.Content,
		Role:    choice.Message.Role,
		Usage: Usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}, nil
}

func (p *MistralProvider) Stream(ctx context.Context, req *CompletionRequest) (<-chan Chunk, error) {
	model := p.resolveModel(req)

	body := map[string]any{
		"model":    model,
		"messages": p.buildMessages(req.Messages),
		"stream":   true,
	}
	if temp := ResolveTemperature(req.Temperature, p.config.Temperature); temp != nil {
		body["temperature"] = *temp
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	} else if p.config.MaxTokens > 0 {
		body["max_tokens"] = p.config.MaxTokens
	}
	if req.ResponseFormat != nil {
		body["response_format"] = buildOpenAIResponseFormat(req.ResponseFormat)
	}

	bodyBytes, err := jsonutil.MarshalBody(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.config.BaseURL+"/chat/completions", bytes.NewReader(bodyBytes))
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
		limitedReader := io.LimitReader(resp.Body, maxResponseSize)
		respBody, _ := io.ReadAll(limitedReader)
		var apiErr APIError
		parsed := json.Unmarshal(respBody, &apiErr) == nil && apiErr.Message != ""
		// perf-v6 round 8 Task 3：携带 Retry-After + 错误分类
		return nil, NewHTTPErrorOrAPIError("mistral", resp.StatusCode, respBody, resp.Header, &apiErr, parsed)
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
			if data == "[DONE]" {
				select {
				case ch <- Chunk{Done: true}:
				case <-ctx.Done():
				}
				return
			}

			var sseResp openaiChatResponse
			// perf-v6 round 8 Task 1：使用 pooled stringReader 避免每条 SSE 消息分配
			if err := jsonutil.DecodeString(data, &sseResp); err != nil {
				continue
			}
			if sseResp.Error != nil {
				continue
			}
			if len(sseResp.Choices) == 0 {
				continue
			}

			chunk := Chunk{
				Content: sseResp.Choices[0].Delta.Content,
			}
			if sseResp.Choices[0].FinishReason == "stop" {
				chunk.Done = true
				chunk.Usage = &Usage{
					PromptTokens:     sseResp.Usage.PromptTokens,
					CompletionTokens: sseResp.Usage.CompletionTokens,
					TotalTokens:      sseResp.Usage.TotalTokens,
				}
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
			slog.Warn("Mistral 流式读取错误", "error", err)
		}
		select {
		case ch <- Chunk{Done: true}:
		case <-ctx.Done():
		}
	}()

	return ch, nil
}

func (p *MistralProvider) CallTools(ctx context.Context, req *ToolCallRequest) (*ToolCallResponse, error) {
	model := p.config.Model
	if req.Model != "" {
		model = req.Model
	}

	tools := make([]map[string]any, len(req.Tools))
	for i, t := range req.Tools {
		tools[i] = map[string]any{
			"type": t.Type,
			"function": map[string]any{
				"name":        t.Function.Name,
				"description": t.Function.Description,
				"parameters":  t.Function.Parameters,
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

	raw, err := p.doRequest(ctx, "/chat/completions", body)
	if err != nil {
		return nil, err
	}

	var resp openaiChatResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrResponseParseFailed, err)
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	if len(resp.Choices) == 0 {
		return nil, ErrEmptyResponse
	}

	choice := resp.Choices[0]
	result := &ToolCallResponse{
		Content: choice.Message.Content,
		Usage: Usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}

	if len(choice.Message.ToolCalls) > 0 {
		result.ToolCalls = make([]FunctionCall, len(choice.Message.ToolCalls))
		for i, tc := range choice.Message.ToolCalls {
			result.ToolCalls[i] = FunctionCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			}
		}
	}

	return result, nil
}

func (p *MistralProvider) Embeddings(ctx context.Context, texts []string) ([][]float32, error) {
	model := p.config.Model
	if !strings.Contains(model, "embed") {
		model = defaultMistralEmbedModel
	}

	body := map[string]any{
		"model": model,
		"input": texts,
	}

	raw, err := p.doRequest(ctx, "/embeddings", body)
	if err != nil {
		return nil, err
	}

	var resp openaiEmbedResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrResponseParseFailed, err)
	}

	result := make([][]float32, len(resp.Data))
	for i, d := range resp.Data {
		result[i] = d.Embedding
	}
	return result, nil
}

func (p *MistralProvider) Info() ModelInfo {
	contextSizes := map[string]int{
		"mistral-large-latest":  128000,
		"mistral-medium-latest": 32768,
		"mistral-small-latest":  32768,
		"mistral-embed":         8192,
		"open-mistral-7b":       32768,
		"open-mixtral-8x7b":     32768,
		"open-mixtral-8x22b":    65536,
	}
	maxCtx := defaultMistralMaxContext
	if c, ok := contextSizes[p.config.Model]; ok {
		maxCtx = c
	}
	return ModelInfo{
		Name:              p.config.Model,
		Provider:          "mistral",
		MaxContext:        maxCtx,
		SupportsTools:     true,
		SupportsStreaming: true,
	}
}

// ===== 内部方法 =====

func (p *MistralProvider) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	req.Header.Set("User-Agent", userAgent)
}

func (p *MistralProvider) doRequest(ctx context.Context, path string, body any) (json.RawMessage, error) {
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
		return nil, NewHTTPErrorOrAPIError("mistral", resp.StatusCode, respBody, resp.Header, errResp.Error, parsed)
	}

	return respBody, nil
}

// buildMessages 将通用 ChatMessage 转换为 Mistral（OpenAI 兼容）格式
func (p *MistralProvider) buildMessages(msgs []ChatMessage) []map[string]any {
	return BuildOpenAIMessages(msgs)
}

func (p *MistralProvider) resolveModel(req *CompletionRequest) string {
	if req != nil && req.Model != "" {
		return req.Model
	}
	return ResolveModel("", p.config.Model)
}
