package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"agentprimordia/internal/jsonutil" // perf-v6 round 5 Task 1：JSON 池化
)

const (
	defaultBaseURL  = "https://api.openai.com/v1"
	maxResponseSize = 10 * 1024 * 1024 // 10MB
	defaultTimeout  = 120 * time.Second
	userAgent       = "AgentPrimordia/1.0"

	defaultOpenAIMaxContext = 128000
)

var (
	openaiContextSizes = map[string]int{
		"gpt-4o":        128000,
		"gpt-4o-mini":   128000,
		"gpt-4-turbo":   128000,
		"gpt-4":         8192,
		"gpt-4-32k":     32768,
		"gpt-3.5-turbo": 16385,
		"o1":            200000,
		"o1-mini":       128000,
		"o3-mini":       200000,
	}
)

var (
	ErrNotSupported        = errors.New("not supported")
	ErrAPIKeyRequired      = errors.New("API key is required")
	ErrEmptyResponse       = errors.New("empty choices in response")
	ErrResponseParseFailed = errors.New("failed to parse LLM response")
	ErrLLMCallFailed       = errors.New("LLM call failed")

	// ErrTemplateNotImplemented 当调用 NewTemplateProvider 时返回。
	// TemplateProvider 是新 LLM Provider 的代码模板，自身不是真实可用
	// 的 Provider。任何误用都会得到此错误而非运行时崩溃，便于早期
	// 发现问题。贡献者应复制 provider_template.go 到新文件并实现
	// 所有方法。详见 ecosystem/contributing/PROVIDER.md。
	ErrTemplateNotImplemented = errors.New(
		"TemplateProvider is a code template, not a real Provider. " +
			"Copy internal/llm/provider_template.go to a new file and implement it. " +
			"See internal/llm/provider_template.go and ecosystem/contributing/PROVIDER.md.")
)

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Type    string `json:"type"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API error: %s (%s)", e.Message, e.Type)
}

type OpenAIProvider struct {
	config Config
	client *http.Client
}

func NewOpenAIProvider(cfg Config) (*OpenAIProvider, error) {
	if cfg.APIKey == "" {
		return nil, ErrAPIKeyRequired
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	cfg.BaseURL = strings.TrimRight(baseURL, "/")

	if cfg.Model == "" {
		cfg.Model = "gpt-4o-mini"
	}

	return &OpenAIProvider{
		config: cfg,
		client: NewDefaultLLMClient(defaultTimeout),
	}, nil
}

func (p *OpenAIProvider) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	model := p.resolveModel(req.Model)

	// perf-v6 round 8 Task 4: typed request struct 减少反射
	oaiReq := openaiChatRequest{
		Model:    model,
		Messages: p.buildMessages(req.Messages),
	}
	if temp := ResolveTemperature(req.Temperature, p.config.Temperature); temp != nil {
		oaiReq.Temperature = temp
	}
	if req.MaxTokens > 0 {
		oaiReq.MaxTokens = req.MaxTokens
	} else if p.config.MaxTokens > 0 {
		oaiReq.MaxTokens = p.config.MaxTokens
	}
	if req.ResponseFormat != nil {
		oaiReq.ResponseFormat = convertResponseFormatToTyped(req.ResponseFormat)
	}

	raw, err := p.doRequest(ctx, "/chat/completions", oaiReq)
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
		Content: strings.TrimSpace(choice.Message.Content),
		Role:    choice.Message.Role,
		Usage: Usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}, nil
}

func (p *OpenAIProvider) Stream(ctx context.Context, req *CompletionRequest) (<-chan Chunk, error) {
	model := p.resolveModel(req.Model)

	// perf-v6 round 8 Task 4: typed request struct 减少反射
	oaiReq := openaiChatRequest{
		Model:    model,
		Messages: p.buildMessages(req.Messages),
		Stream:   true,
	}
	if temp := ResolveTemperature(req.Temperature, p.config.Temperature); temp != nil {
		oaiReq.Temperature = temp
	}
	if req.MaxTokens > 0 {
		oaiReq.MaxTokens = req.MaxTokens
	}
	if p.config.MaxTokens > 0 && req.MaxTokens == 0 {
		oaiReq.MaxTokens = p.config.MaxTokens
	}
	if req.ResponseFormat != nil {
		oaiReq.ResponseFormat = convertResponseFormatToTyped(req.ResponseFormat)
	}

	bodyBytes, err := jsonutil.Marshal(oaiReq) // perf-v6 round 5 Task 1
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.config.BaseURL+"/chat/completions", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	httpReq.Header.Set("User-Agent", userAgent)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		limitedReader := io.LimitReader(resp.Body, maxResponseSize)
		// 错误响应通常较小，io.ReadAll 在内部已经做了优化
		respBody, _ := io.ReadAll(limitedReader)
		var apiErr APIError
		parsed := json.Unmarshal(respBody, &apiErr) == nil && apiErr.Message != ""
		// perf-v6 round 8 Task 3：携带 Retry-After + 错误分类
		return nil, NewHTTPErrorOrAPIError("openai", resp.StatusCode, respBody, resp.Header, &apiErr, parsed)
	}

	ch := make(chan Chunk, 32)

	// 背压超时（perf-v11 stage-3）：如果消费者在 streamBackpressureTimeout 内未读取，
	// 跳过当前 chunk 而非无限阻塞。防止慢消费者拖垮 LLM 流的 goroutine 生命周期。
	const streamBackpressureTimeout = 5 * time.Second
	droppedChunks := 0
	const maxDroppedChunks = 10 // 连续丢弃 N 个 chunk 后中断流（消费者可能已崩溃）

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

			// perf-v6 round 8 Task 1：使用 pooled *bytes.Reader 避免每条 SSE 消息分配
			var sseResp openaiChatResponse
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
			// 背压控制：限时发送，超时则丢弃并计数
			timer := time.NewTimer(streamBackpressureTimeout)
			select {
			case ch <- chunk:
				if !timer.Stop() {
					<-timer.C
				}
				droppedChunks = 0 // 成功发送，重置计数器
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				// 消费者慢：丢弃当前 chunk
				droppedChunks++
				if droppedChunks >= maxDroppedChunks {
					// 连续丢弃过多，放弃整个流
					slog.Warn("OpenAI 流式背压超时，消费者可能已停止读取",
						"dropped", droppedChunks, "timeout", streamBackpressureTimeout)
					return
				}
			}
			if chunk.Done {
				return
			}
		}

		if err := scanner.Err(); err != nil {
			slog.Warn("OpenAI 流式读取错误", "error", err)
		}
		select {
		case ch <- Chunk{Done: true}:
		case <-ctx.Done():
		}
	}()

	return ch, nil
}

func (p *OpenAIProvider) CallTools(ctx context.Context, req *ToolCallRequest) (*ToolCallResponse, error) {
	model := p.resolveModel(req.Model)

	// perf-v6 Task C：typed struct 减少反射
	tools := make([]openaiTool, len(req.Tools))
	for i, t := range req.Tools {
		// 将 map[string]any 转 json.RawMessage
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

	oaiReq := openaiChatRequest{
		Model:    model,
		Messages: p.buildMessages(req.Messages),
		Tools:    tools,
	}
	if temp := ResolveTemperature(nil, p.config.Temperature); temp != nil {
		oaiReq.Temperature = temp
	}
	if p.config.MaxTokens > 0 {
		oaiReq.MaxTokens = p.config.MaxTokens
	}

	raw, err := p.doRequest(ctx, "/chat/completions", oaiReq)
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
		Content: strings.TrimSpace(choice.Message.Content),
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

const defaultOpenAIEmbedModel = "text-embedding-3-small"

func (p *OpenAIProvider) Embeddings(ctx context.Context, texts []string) ([][]float32, error) {
	embedModel := p.config.Model
	if embedModel == "" || embedModel == "gpt-4o-mini" {
		embedModel = defaultOpenAIEmbedModel
	}
	embedReq := openaiEmbedRequest{
		Model: embedModel,
		Input: texts,
	}

	raw, err := p.doRequest(ctx, "/embeddings", embedReq)
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

func (p *OpenAIProvider) Info() ModelInfo {
	maxCtx := defaultOpenAIMaxContext
	if c, ok := openaiContextSizes[p.config.Model]; ok {
		maxCtx = c
	}
	return ModelInfo{
		Name:              p.config.Model,
		Provider:          "openai",
		MaxContext:        maxCtx,
		SupportsTools:     true,
		SupportsStreaming: true,
	}
}

func (p *OpenAIProvider) doRequest(ctx context.Context, path string, body any) (json.RawMessage, error) {
	bodyBytes, err := jsonutil.Marshal(body) // perf-v6 round 5 Task 1
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.config.BaseURL+path, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	httpReq.Header.Set("User-Agent", userAgent)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	limitedReader := io.LimitReader(resp.Body, maxResponseSize)
	// 优化（perf-v11 stage-2）：成功响应使用 io.ReadAll（标准库已优化），
	// 避免 sync.Pool 在并行测试中可能引入的字节级竞态。错误响应（短）走池化路径。
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
		return nil, NewHTTPErrorOrAPIError("openai", resp.StatusCode, respBody, resp.Header, errResp.Error, parsed)
	}

	return respBody, nil
}

func (p *OpenAIProvider) resolveModel(reqModel string) string {
	return ResolveModel(reqModel, p.config.Model)
}

// requestBodyPool 复用 LLM 请求体 []byte buffer（perf-v6 Task D）
func (p *OpenAIProvider) buildMessages(msgs []ChatMessage) []map[string]any {
	return BuildOpenAIMessages(msgs)
}

// buildOpenAIResponseFormat 将通用 ResponseFormat 转换为 OpenAI 兼容格式
// Qwen/GLM/Mistral 等 OpenAI 兼容 API 均使用此格式
func buildOpenAIResponseFormat(rf *ResponseFormat) map[string]any {
	result := map[string]any{
		"type": string(rf.Type),
	}
	if rf.Type == ResponseFormatJSONSchema && rf.JSONSchema != nil {
		schemaDef := map[string]any{
			"name":   rf.JSONSchema.Name,
			"schema": rf.JSONSchema.Schema,
		}
		if rf.JSONSchema.Description != "" {
			schemaDef["description"] = rf.JSONSchema.Description
		}
		if rf.JSONSchema.Strict {
			schemaDef["strict"] = true
		}
		result["json_schema"] = schemaDef
	}
	return result
}

type openaiEmbedResponse struct {
	Object string `json:"object"`
	Data   []struct {
		Object    string    `json:"object"`
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Model string `json:"model"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

// openaiTool tool定义 typed struct（perf-v6 Task C：减少反射）
type openaiTool struct {
	Type     string             `json:"type"`
	Function openaiToolFunction `json:"function"`
}

// openaiToolFunction tool函数定义
type openaiToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type openaiChatResponse struct {
	ID      string `json:"id"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *APIError `json:"error,omitempty"`
}
