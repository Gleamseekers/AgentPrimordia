package llm

import (
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/jsonutil" // perf-v6 round 6 Task 1
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
)

// OpenAIMultimodalProvider OpenAI 多模态 Provider（支持 GPT-4o 视觉能力）
type OpenAIMultimodalProvider struct {
	config Config
	client *http.Client
	// v6.x 评估 §4.2 P1-4：流式专用 client（无整体超时 + ResponseHeaderTimeout）
	streamClient *http.Client
}

// NewOpenAIMultimodalProvider 创建 OpenAI 多模态 Provider
func NewOpenAIMultimodalProvider(cfg Config) (*OpenAIMultimodalProvider, error) {
	if cfg.APIKey == "" {
		return nil, ErrAPIKeyRequired
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	cfg.BaseURL = strings.TrimRight(baseURL, "/")

	if cfg.Model == "" {
		cfg.Model = "gpt-4o" // 默认使用支持视觉的模型
	}

	return &OpenAIMultimodalProvider{
		config:       cfg,
		client:       NewDefaultLLMClient(defaultTimeout),
		streamClient: NewDefaultLLMStreamClient(),
	}, nil
}

// CompleteMultimodal 多模态补全（核心方法）
func (p *OpenAIMultimodalProvider) CompleteMultimodal(ctx context.Context, req *CompletionRequestExt) (*CompletionResponse, error) {
	model := p.resolveModel(req.Model)

	messages := p.buildMultimodalMessages(req.Messages)

	body := map[string]any{
		"model":    model,
		"messages": messages,
	}
	if temp := ResolveTemperature(req.Temperature, p.config.Temperature); temp != nil {
		body["temperature"] = *temp
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	} else if p.config.MaxTokens > 0 {
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

// StreamMultimodal 多模态流式补全
func (p *OpenAIMultimodalProvider) StreamMultimodal(ctx context.Context, req *CompletionRequestExt) (<-chan Chunk, error) {
	model := p.resolveModel(req.Model)

	messages := p.buildMultimodalMessages(req.Messages)

	body := map[string]any{
		"model":    model,
		"messages": messages,
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

	bodyBytes, err := jsonutil.MarshalBody(body)
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
		return nil, NewHTTPErrorOrAPIError("openai_multimodal", resp.StatusCode, respBody, resp.Header, &apiErr, parsed)
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
			slog.Warn("OpenAI Multimodal 流式读取错误", "error", err)
		}
		select {
		case ch <- Chunk{Done: true}:
		case <-ctx.Done():
		}
	}()

	return ch, nil
}

// buildMultimodalMessages 构建多模态消息列表（核心转换逻辑）
func (p *OpenAIMultimodalProvider) buildMultimodalMessages(msgs []*ChatMessageExt) []map[string]any {
	result := make([]map[string]any, 0, len(msgs))

	for _, m := range msgs {
		if !m.HasNonTextContent() {
			// 纯文本消息，直接使用简单格式
			msg := map[string]any{
				"role":    m.Role,
				"content": m.ExtractText(),
			}
			if m.Role == "assistant" && len(m.ToolCalls) > 0 {
				msg["tool_calls"] = p.buildToolCalls(m.ToolCalls)
			}
			if m.Role == "tool" {
				msg["tool_call_id"] = m.ToolCallID
			}
			result = append(result, msg)
			continue
		}

		// 多模态内容，构建 content 数组格式
		contentParts := make([]map[string]any, 0, len(m.Contents))
		for _, c := range m.Contents {
			part := p.convertContentPart(c)
			if part != nil {
				contentParts = append(contentParts, part)
			}
		}

		msg := map[string]any{
			"role":    m.Role,
			"content": contentParts,
		}
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			msg["tool_calls"] = p.buildToolCalls(m.ToolCalls)
		}
		if m.Role == "tool" {
			msg["tool_call_id"] = m.ToolCallID
		}
		result = append(result, msg)
	}

	return result
}

// convertContentPart 将 MultimodalContent 转换为 OpenAI API 格式
func (p *OpenAIMultimodalProvider) convertContentPart(content *MultimodalContent) map[string]any {
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
		imageURL := map[string]any{
			"url": content.URL,
		}
		if content.Detail != "" && content.Detail != "auto" {
			imageURL["detail"] = content.Detail
		}
		return map[string]any{
			"type":      "image_url",
			"image_url": imageURL,
		}

	case ContentTypeImageB64:
		if content.Data == "" || content.MIME == "" {
			return nil
		}
		url := fmt.Sprintf("data:%s;base64,%s", content.MIME, content.Data)
		imageURL := map[string]any{
			"url": url,
		}
		if content.Detail != "" && content.Detail != "auto" {
			imageURL["detail"] = content.Detail
		}
		return map[string]any{
			"type":      "image_url",
			"image_url": imageURL,
		}

	case ContentTypeAudio:
		// OpenAI GPT-4o 音频支持有限，转为 base64 URL 格式
		if content.Data == "" || content.MIME == "" {
			return nil
		}
		inputAudio := map[string]any{
			"data":   content.Data,
			"format": strings.Split(content.MIME, "/")[1], // 提取格式如 "mp3", "wav"
		}
		return map[string]any{
			"type":        "input_audio",
			"input_audio": inputAudio,
		}

	case ContentTypeVideo:
		// OpenAI 目前不直接支持视频输入，记录警告并跳过
		// 未来可考虑先提取关键帧作为图片发送
		return nil

	default:
		return nil
	}
}

// buildToolCalls 构建 tool_calls 字段
func (p *OpenAIMultimodalProvider) buildToolCalls(toolCalls []FunctionCall) []map[string]any {
	result := make([]map[string]any, len(toolCalls))
	for i, tc := range toolCalls {
		result[i] = map[string]any{
			"id":   tc.ID,
			"type": "function",
			"function": map[string]any{
				"name":      tc.Name,
				"arguments": tc.Arguments,
			},
		}
	}
	return result
}

// resolveModel 解析模型名称
func (p *OpenAIMultimodalProvider) resolveModel(model string) string {
	if model != "" {
		return model
	}
	return p.config.Model
}

// doRequest 发送 HTTP 请求
func (p *OpenAIMultimodalProvider) doRequest(ctx context.Context, endpoint string, body any) ([]byte, error) {
	bodyBytes, err := jsonutil.MarshalBody(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	url := p.config.BaseURL + endpoint
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	req.Header.Set("User-Agent", userAgent)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		limitedReader := io.LimitReader(resp.Body, maxResponseSize)
		respBody, _ := io.ReadAll(limitedReader)

		var apiErr APIError
		parsed := json.Unmarshal(respBody, &apiErr) == nil && apiErr.Message != ""
		// perf-v6 round 8 Task 3：携带 Retry-After + 错误分类
		return nil, NewHTTPErrorOrAPIError("openai_multimodal", resp.StatusCode, respBody, resp.Header, &apiErr, parsed)
	}

	limitedReader := io.LimitReader(resp.Body, maxResponseSize)
	responseData, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return responseData, nil
}

// Info 返回模型信息（包含多模态能力）
func (p *OpenAIMultimodalProvider) Info() ModelInfo {
	return ModelInfo{
		Name:              p.config.Model,
		Provider:          "openai",
		MaxContext:        128000,
		SupportsTools:     true,
		SupportsStreaming: true,
	}
}

func (p *OpenAIMultimodalProvider) InfoExt() ModelInfoExt {
	return ModelInfoExt{
		ModelInfo:         p.Info(),
		SupportsVision:    true,
		SupportsAudio:     true,
		SupportsVideo:     false,
		MaxImageSize:      20,
		MaxImagesPerMsg:   10,
		AcceptedMIMETypes: []string{"image/png", "image/jpeg", "image/gif", "image/webp"},
	}
}

var _ error = (*APIError)(nil)

// ErrMultimodalNotSupported 多模态不支持错误
var ErrMultimodalNotSupported = errors.New("multimodal not supported for this provider")

// Complete 向后兼容的标准接口实现
func (p *OpenAIMultimodalProvider) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	extReq := &CompletionRequestExt{
		Messages:    make([]*ChatMessageExt, len(req.Messages)),
		Model:       req.Model,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Stream:      req.Stream,
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

// Stream 向后兼容的标准接口实现
func (p *OpenAIMultimodalProvider) Stream(ctx context.Context, req *CompletionRequest) (<-chan Chunk, error) {
	extReq := &CompletionRequestExt{
		Messages:    make([]*ChatMessageExt, len(req.Messages)),
		Model:       req.Model,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Stream:      true,
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

// CallTools tool调用（暂不支持多模态，降级为标准调用）
func (p *OpenAIMultimodalProvider) CallTools(ctx context.Context, req *ToolCallRequest) (*ToolCallResponse, error) {
	return nil, ErrNotSupported
}

// Embeddings 文本嵌入（不支持）
