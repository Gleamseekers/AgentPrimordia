package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/jsonutil" // perf-v6 round 6 Task 1
)

// AnthropicVisionProvider Anthropic Claude 视觉多模态 Provider
type AnthropicVisionProvider struct {
	config Config
	client *http.Client
	// v6.x 评估 §4.2 P1-4：流式专用 client（无整体超时 + ResponseHeaderTimeout）
	streamClient *http.Client
}

// NewAnthropicVisionProvider 创建 Anthropic Vision Provider
func NewAnthropicVisionProvider(cfg Config) (*AnthropicVisionProvider, error) {
	if cfg.APIKey == "" {
		return nil, ErrAPIKeyRequired
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = anthropicDefaultBaseURL
	}
	cfg.BaseURL = strings.TrimRight(baseURL, "/")

	if cfg.Model == "" {
		cfg.Model = "claude-sonnet-4-20250514" // 默认支持视觉的模型
	}

	return &AnthropicVisionProvider{
		config:       cfg,
		client:       NewDefaultLLMClient(defaultTimeout),
		streamClient: NewDefaultLLMStreamClient(),
	}, nil
}

// CompleteMultimodal 多模态补全
func (p *AnthropicVisionProvider) CompleteMultimodal(ctx context.Context, req *CompletionRequestExt) (*CompletionResponse, error) {
	model := p.resolveModel(req.Model)
	messages, systemMsg, err := p.buildVisionMessages(req.Messages)
	if err != nil {
		return nil, err
	}

	body := map[string]any{
		"model":      model,
		"messages":   messages,
		"max_tokens": p.resolveMaxTokens(req),
	}
	if systemMsg != "" {
		body["system"] = systemMsg
	}
	if temp := ResolveTemperature(req.Temperature, p.config.Temperature); temp != nil {
		body["temperature"] = *temp
	}

	raw, err := p.doRequest(ctx, body)
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

// StreamMultimodal 流式多模态补全
func (p *AnthropicVisionProvider) StreamMultimodal(ctx context.Context, req *CompletionRequestExt) (<-chan Chunk, error) {
	model := p.resolveModel(req.Model)
	messages, systemMsg, err := p.buildVisionMessages(req.Messages)
	if err != nil {
		return nil, err
	}

	body := map[string]any{
		"model":      model,
		"messages":   messages,
		"max_tokens": p.resolveMaxTokens(req),
		"stream":     true,
	}
	if systemMsg != "" {
		body["system"] = systemMsg
	}
	if temp := ResolveTemperature(req.Temperature, p.config.Temperature); temp != nil {
		body["temperature"] = *temp
	}

	bodyBytes, err := jsonutil.MarshalBody(body) // perf-v6 round 6 Task 1
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
		limitedReader := io.LimitReader(resp.Body, maxResponseSize)
		respBody, _ := io.ReadAll(limitedReader)
		// perf-v6 round 8 Task 3：携带 Retry-After + 错误分类
		return nil, NewHTTPError("anthropic_vision", resp.StatusCode, respBody, resp.Header)
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

			var event struct {
				Type  string `json:"type"`
				Delta struct {
					Text       string `json:"text,omitempty"`
					StopReason string `json:"stop_reason,omitempty"`
				} `json:"delta,omitempty"`
				Message struct {
					Usage struct {
						InputTokens  int `json:"input_tokens"`
						OutputTokens int `json:"output_tokens"`
					} `json:"usage"`
				} `json:"message,omitempty"`
			}
			// perf-v6 round 8 Task 1：使用 pooled stringReader 避免每条 SSE 消息分配
			if err := jsonutil.DecodeString(data, &event); err != nil {
				continue
			}

			if event.Type == "content_block_delta" && event.Delta.Text != "" {
				select {
				case ch <- Chunk{Content: event.Delta.Text}:
				case <-ctx.Done():
					return
				}
			} else if event.Type == "message_stop" || event.Delta.StopReason == "end_turn" {
				select {
				case ch <- Chunk{Done: true}:
				case <-ctx.Done():
				}
				return
			} else if event.Type == "message_start" && event.Message.Usage.OutputTokens > 0 {
				select {
				case ch <- Chunk{
					Done: true,
					Usage: &Usage{
						PromptTokens:     event.Message.Usage.InputTokens,
						CompletionTokens: event.Message.Usage.OutputTokens,
						TotalTokens:      event.Message.Usage.InputTokens + event.Message.Usage.OutputTokens,
					},
				}:
				case <-ctx.Done():
					return
				}
			}
		}
		if err := scanner.Err(); err != nil {
			select {
			case ch <- Chunk{Done: true}:
			case <-ctx.Done():
			}
			return
		}
		select {
		case ch <- Chunk{Done: true}:
		case <-ctx.Done():
		}
	}()

	return ch, nil
}

// buildVisionMessages 构建视觉消息列表
func (p *AnthropicVisionProvider) buildVisionMessages(msgs []*ChatMessageExt) ([]map[string]any, string, error) {
	var systemMsg string
	var result []map[string]any

	for _, m := range msgs {
		if m.Role == "system" {
			systemMsg = m.ExtractText()
			continue
		}

		if !m.HasNonTextContent() {
			msg := map[string]any{
				"role": m.Role,
				"content": []map[string]any{
					{"type": "text", "text": m.ExtractText()},
				},
			}
			result = append(result, msg)
			continue
		}

		contentParts := make([]map[string]any, 0, len(m.Contents))
		for _, c := range m.Contents {
			part, err := p.convertToAnthropicFormat(c)
			if err != nil {
				return nil, "", err
			}
			if part != nil {
				contentParts = append(contentParts, part)
			}
		}

		msg := map[string]any{
			"role":    m.Role,
			"content": contentParts,
		}
		result = append(result, msg)
	}

	return result, systemMsg, nil
}

// convertToAnthropicFormat 转换为 Anthropic API 格式
func (p *AnthropicVisionProvider) convertToAnthropicFormat(content *MultimodalContent) (map[string]any, error) {
	switch content.Type {
	case ContentTypeText:
		if content.Text == "" {
			return nil, nil
		}
		return map[string]any{
			"type": "text",
			"text": content.Text,
		}, nil

	case ContentTypeImageURL:
		return nil, fmt.Errorf("Anthropic does not support direct image URLs; download the image and provide it as base64")

	case ContentTypeImageB64:
		if content.Data == "" || content.MIME == "" {
			return nil, nil
		}
		return map[string]any{
			"type": "image",
			"source": map[string]any{
				"type":       "base64",
				"media_type": content.MIME,
				"data":       content.Data,
			},
		}, nil

	case ContentTypeAudio:
		return nil, nil

	case ContentTypeVideo:
		return nil, nil

	default:
		return nil, nil
	}
}

// resolveModel 解析模型名称
func (p *AnthropicVisionProvider) resolveModel(model string) string {
	if model != "" {
		return model
	}
	return p.config.Model
}

// resolveMaxTokens 解析最大 token 数
func (p *AnthropicVisionProvider) resolveMaxTokens(req *CompletionRequestExt) int {
	if req.MaxTokens > 0 {
		return req.MaxTokens
	}
	if p.config.MaxTokens > 0 {
		return p.config.MaxTokens
	}
	return defaultAnthropicMaxTokens
}

// setHeaders 设置请求头
func (p *AnthropicVisionProvider) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.config.APIKey)
	req.Header.Set("anthropic-version", anthropicAPIVersion)
	req.Header.Set("User-Agent", userAgent)
}

// doRequest 发送 HTTP 请求
func (p *AnthropicVisionProvider) doRequest(ctx context.Context, body any) ([]byte, error) {
	bodyBytes, err := jsonutil.MarshalBody(body) // perf-v6 round 6 Task 1
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	url := p.config.BaseURL + "/v1/messages"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	p.setHeaders(req)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		limitedReader := io.LimitReader(resp.Body, maxResponseSize)
		respBody, _ := io.ReadAll(limitedReader)
		// perf-v6 round 8 Task 3：携带 Retry-After + 错误分类
		return nil, NewHTTPError("anthropic_vision", resp.StatusCode, respBody, resp.Header)
	}

	limitedReader := io.LimitReader(resp.Body, maxResponseSize)
	responseData, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return responseData, nil
}

// Info 返回模型信息
func (p *AnthropicVisionProvider) Info() ModelInfo {
	return ModelInfo{
		Name:              p.config.Model,
		Provider:          "anthropic",
		MaxContext:        200000,
		SupportsTools:     true,
		SupportsStreaming: true,
	}
}

func (p *AnthropicVisionProvider) InfoExt() ModelInfoExt {
	return ModelInfoExt{
		ModelInfo:         p.Info(),
		SupportsVision:    true,
		SupportsAudio:     false,
		SupportsVideo:     false,
		MaxImageSize:      20,
		MaxImagesPerMsg:   20,
		AcceptedMIMETypes: []string{"image/jpeg", "image/png", "image/gif", "image/webp"},
	}
}

// Complete 向后兼容接口
func (p *AnthropicVisionProvider) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	extReq := &CompletionRequestExt{
		Messages:    make([]*ChatMessageExt, len(req.Messages)),
		Model:       req.Model,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}
	for i, m := range req.Messages {
		extReq.Messages[i] = NewUserTextMessage(m.Content)
		extReq.Messages[i].Role = m.Role
		extReq.Messages[i].ToolCalls = m.ToolCalls
		extReq.Messages[i].ToolCallID = m.ToolCallID
		extReq.Messages[i].IsToolError = m.IsToolError
	}
	return p.CompleteMultimodal(ctx, extReq)
}

// Stream 向后兼容接口
func (p *AnthropicVisionProvider) Stream(ctx context.Context, req *CompletionRequest) (<-chan Chunk, error) {
	extReq := &CompletionRequestExt{
		Messages:    make([]*ChatMessageExt, len(req.Messages)),
		Model:       req.Model,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}
	for i, m := range req.Messages {
		extReq.Messages[i] = NewUserTextMessage(m.Content)
		extReq.Messages[i].Role = m.Role
		extReq.Messages[i].ToolCalls = m.ToolCalls
		extReq.Messages[i].ToolCallID = m.ToolCallID
		extReq.Messages[i].IsToolError = m.IsToolError
	}
	return p.StreamMultimodal(ctx, extReq)
}

// CallTools tool调用（暂不支持）
func (p *AnthropicVisionProvider) CallTools(ctx context.Context, req *ToolCallRequest) (*ToolCallResponse, error) {
	return nil, ErrNotSupported
}

// Embeddings 文本嵌入（不支持）
