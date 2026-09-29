package llm

import (
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/jsonutil" // perf-v6 round 6 Task 1
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
	qwenDefaultBaseURL    = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	defaultQwenMaxContext = 32768
	defaultQwenMaxTokens  = 2000
)

// QwenProvider 通义千问多模态 Provider（支持 Qwen-VL 系列）
type QwenProvider struct {
	config Config
	client *http.Client
	// v6.x 评估 §4.2 P1-4：流式专用 client（无整体超时 + ResponseHeaderTimeout）
	streamClient *http.Client
}

// NewQwenProvider 创建通义千问 Provider
func NewQwenProvider(cfg Config) (*QwenProvider, error) {
	if cfg.APIKey == "" {
		return nil, ErrAPIKeyRequired
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = qwenDefaultBaseURL
	}
	cfg.BaseURL = strings.TrimRight(baseURL, "/")

	if cfg.Model == "" {
		cfg.Model = "qwen-vl-max-latest" // 默认使用最新的视觉模型
	}

	return &QwenProvider{
		config:       cfg,
		client:       NewDefaultLLMClient(defaultTimeout),
		streamClient: NewDefaultLLMStreamClient(),
	}, nil
}

// CompleteMultimodal 多模态补全（核心方法）
func (p *QwenProvider) CompleteMultimodal(ctx context.Context, req *CompletionRequestExt) (*CompletionResponse, error) {
	model := p.resolveModel(req.Model)
	messages := p.buildMultimodalMessages(req.Messages)

	body := map[string]any{
		"model":    model,
		"messages": messages,
	}
	if temp := ResolveTemperature(req.Temperature, p.config.Temperature); temp != nil && *temp > 0 {
		body["temperature"] = temp
	}
	if maxTok := p.resolveMaxTokens(req); maxTok > 0 {
		body["max_tokens"] = maxTok
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

// StreamMultimodal 流式多模态补全
func (p *QwenProvider) StreamMultimodal(ctx context.Context, req *CompletionRequestExt) (<-chan Chunk, error) {
	model := p.resolveModel(req.Model)
	messages := p.buildMultimodalMessages(req.Messages)

	body := map[string]any{
		"model":    model,
		"messages": messages,
		"stream":   true,
		"stream_options": map[string]bool{
			"include_usage": true,
		},
	}
	if temp := ResolveTemperature(req.Temperature, p.config.Temperature); temp != nil && *temp > 0 {
		body["temperature"] = temp
	}
	if maxTok := p.resolveMaxTokens(req); maxTok > 0 {
		body["max_tokens"] = maxTok
	}
	if req.ResponseFormat != nil {
		body["response_format"] = buildOpenAIResponseFormat(req.ResponseFormat)
	}

	url := fmt.Sprintf("%s/chat/completions", p.config.BaseURL)
	bodyBytes, err := jsonutil.MarshalBody(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.config.APIKey))
	httpReq.Header.Set("User-Agent", userAgent)

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
		return nil, NewHTTPError("qwen", resp.StatusCode, respBody, resp.Header)
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
			if !strings.HasPrefix(line, "data: ") || line == "data: [DONE]" {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			var streamResp openaiChatResponse
			// perf-v6 round 8 Task 1：使用 pooled stringReader 避免每条 SSE 消息分配
			if err := jsonutil.DecodeString(data, &streamResp); err != nil {
				continue
			}

			if len(streamResp.Choices) > 0 {
				delta := streamResp.Choices[0].Delta
				select {
				case ch <- Chunk{Content: delta.Content}:
				case <-ctx.Done():
					return
				}
			}

			if streamResp.Usage.TotalTokens > 0 {
				select {
				case ch <- Chunk{
					Done: true,
					Usage: &Usage{
						PromptTokens:     streamResp.Usage.PromptTokens,
						CompletionTokens: streamResp.Usage.CompletionTokens,
						TotalTokens:      streamResp.Usage.TotalTokens,
					},
				}:
				case <-ctx.Done():
					return
				}
			}
		}

		if err := scanner.Err(); err != nil {
			slog.Warn("Qwen 流式读取错误", "error", err)
		}

		select {
		case ch <- Chunk{Done: true}:
		case <-ctx.Done():
		}
	}()

	return ch, nil
}

// buildMultimodalMessages 构建多模态消息列表（OpenAI 兼容格式）
func (p *QwenProvider) buildMultimodalMessages(msgs []*ChatMessageExt) []map[string]any {
	result := make([]map[string]any, 0, len(msgs))

	for _, m := range msgs {
		if !m.HasNonTextContent() {
			msg := map[string]any{
				"role":    m.Role,
				"content": m.ExtractText(),
			}
			result = append(result, msg)
			continue
		}

		contentParts := make([]map[string]any, 0, len(m.Contents))
		for _, c := range m.Contents {
			part := p.convertToQwenFormat(c)
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

	return result
}

// convertToQwenFormat 转换为通义千问 API 格式（OpenAI 兼容）
func (p *QwenProvider) convertToQwenFormat(content *MultimodalContent) map[string]any {
	switch content.Type {
	case ContentTypeText:
		if content.Text == "" {
			return nil
		}
		return map[string]any{
			"type": "text",
			"text": content.Text,
		}

	case ContentTypeImageURL:
		return map[string]any{
			"type": "image_url",
			"image_url": map[string]string{
				"url": content.URL,
			},
		}

	case ContentTypeImageB64:
		if content.Data == "" || content.MIME == "" {
			return nil
		}
		dataURL := fmt.Sprintf("data:%s;base64,%s", content.MIME, content.Data)
		return map[string]any{
			"type": "image_url",
			"image_url": map[string]string{
				"url": dataURL,
			},
		}

	default:
		return nil // 通义千问 VL 目前主要支持图片，暂不支持音频/视频
	}
}

// resolveModel 解析模型名称
func (p *QwenProvider) resolveModel(model string) string {
	return ResolveModel(model, p.config.Model)
}

// resolveMaxTokens 解析最大 token 数
func (p *QwenProvider) resolveMaxTokens(req *CompletionRequestExt) int {
	if req.MaxTokens > 0 {
		return req.MaxTokens
	}
	if p.config.MaxTokens > 0 {
		return p.config.MaxTokens
	}
	return defaultQwenMaxTokens
}

// doRequest 发送 HTTP 请求
func (p *QwenProvider) doRequest(ctx context.Context, endpoint string, body any) ([]byte, error) {
	url := fmt.Sprintf("%s%s", p.config.BaseURL, endpoint)

	bodyBytes, err := jsonutil.MarshalBody(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.config.APIKey))
	req.Header.Set("User-Agent", userAgent)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		limitedReader := io.LimitReader(resp.Body, maxResponseSize)
		respBody, _ := io.ReadAll(limitedReader)
		// perf-v6 round 8 Task 3：携带 Retry-After + 错误分类
		return nil, NewHTTPError("qwen", resp.StatusCode, respBody, resp.Header)
	}

	limitedReader := io.LimitReader(resp.Body, maxResponseSize)
	responseData, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return responseData, nil
}

// Info 返回模型信息
func (p *QwenProvider) Info() ModelInfo {
	return ModelInfo{
		Name:              p.config.Model,
		Provider:          "qwen",
		MaxContext:        defaultQwenMaxContext,
		SupportsTools:     true,
		SupportsStreaming: true,
	}
}

func (p *QwenProvider) InfoExt() ModelInfoExt {
	return ModelInfoExt{
		ModelInfo:         p.Info(),
		SupportsVision:    true,
		SupportsAudio:     false,
		SupportsVideo:     false,
		MaxImageSize:      10,
		MaxImagesPerMsg:   8,
		AcceptedMIMETypes: []string{"image/png", "image/jpeg", "image/gif", "image/webp"},
	}
}

// Complete 向后兼容接口
func (p *QwenProvider) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	extReq := &CompletionRequestExt{
		Messages:       make([]*ChatMessageExt, len(req.Messages)),
		Model:          req.Model,
		Temperature:    req.Temperature,
		MaxTokens:      req.MaxTokens,
		ResponseFormat: req.ResponseFormat,
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
func (p *QwenProvider) Stream(ctx context.Context, req *CompletionRequest) (<-chan Chunk, error) {
	extReq := &CompletionRequestExt{
		Messages:       make([]*ChatMessageExt, len(req.Messages)),
		Model:          req.Model,
		Temperature:    req.Temperature,
		MaxTokens:      req.MaxTokens,
		ResponseFormat: req.ResponseFormat,
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

// CallTools tool调用
func (p *QwenProvider) CallTools(ctx context.Context, req *ToolCallRequest) (*ToolCallResponse, error) {
	model := p.resolveModel(req.Model)

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

// Embeddings 文本嵌入（暂不支持）
// buildMessages 构建标准 OpenAI 格式的消息
func (p *QwenProvider) buildMessages(msgs []ChatMessage) []map[string]any {
	return BuildOpenAIMessages(msgs)
}
