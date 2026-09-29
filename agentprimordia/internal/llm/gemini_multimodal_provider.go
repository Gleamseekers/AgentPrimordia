package llm

import (
	"github.com/Gleamseekers/AgentPrimordia/internal/jsonutil" // perf-v6 round 6 Task 1
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
)

const defaultGeminiMultimodalMaxTokens = 8192

// GeminiMultimodalProvider Google Gemini 多模态 Provider（支持 Gemini Pro Vision）
type GeminiMultimodalProvider struct {
	config Config
	client *http.Client
	// v6.x 评估 §4.2 P1-4：流式专用 client（无整体超时 + ResponseHeaderTimeout）
	streamClient *http.Client
}

// NewGeminiMultimodalProvider 创建 Gemini 多模态 Provider
func NewGeminiMultimodalProvider(cfg Config) (*GeminiMultimodalProvider, error) {
	if cfg.APIKey == "" {
		return nil, ErrAPIKeyRequired
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = geminiDefaultBaseURL
	}
	cfg.BaseURL = strings.TrimRight(baseURL, "/")

	if cfg.Model == "" {
		cfg.Model = "gemini-2.0-flash" // 默认支持视觉的模型
	}

	return &GeminiMultimodalProvider{
		config:       cfg,
		client:       NewDefaultLLMClient(defaultTimeout),
		streamClient: NewDefaultLLMStreamClient(),
	}, nil
}

// CompleteMultimodal 多模态补全（核心方法）
func (p *GeminiMultimodalProvider) CompleteMultimodal(ctx context.Context, req *CompletionRequestExt) (*CompletionResponse, error) {
	model := p.resolveModel(req.Model)
	contents, systemInstruction := p.buildMultimodalContents(req.Messages)

	body := map[string]any{
		"contents": contents,
	}
	if len(systemInstruction) > 0 {
		body["systemInstruction"] = map[string]any{
			"parts": systemInstruction,
		}
	}
	generationConfig := p.buildGenerationConfig(req)
	if len(generationConfig) > 0 {
		body["generationConfig"] = generationConfig
	}

	raw, err := p.doRequest(ctx, model, ":generateContent", body)
	if err != nil {
		return nil, err
	}

	var resp geminiResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrResponseParseFailed, err)
	}

	content := ""
	if len(resp.Candidates) > 0 && len(resp.Candidates[0].Content.Parts) > 0 {
		content = resp.Candidates[0].Content.Parts[0].Text
	}

	return &CompletionResponse{
		ID:      fmt.Sprintf("gemini-%d", resp.UsageMetadata.TotalTokenCount),
		Model:   model,
		Content: content,
		Role:    "assistant",
		Usage: Usage{
			PromptTokens:     resp.UsageMetadata.PromptTokenCount,
			CompletionTokens: resp.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      resp.UsageMetadata.TotalTokenCount,
		},
	}, nil
}

// StreamMultimodal 流式多模态补全
func (p *GeminiMultimodalProvider) StreamMultimodal(ctx context.Context, req *CompletionRequestExt) (<-chan Chunk, error) {
	model := p.resolveModel(req.Model)
	contents, systemInstruction := p.buildMultimodalContents(req.Messages)

	body := map[string]any{
		"contents": contents,
	}
	if len(systemInstruction) > 0 {
		body["systemInstruction"] = map[string]any{
			"parts": systemInstruction,
		}
	}
	generationConfig := p.buildGenerationConfig(req)
	if len(generationConfig) > 0 {
		body["generationConfig"] = generationConfig
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?key=%s",
		p.config.BaseURL, model, p.config.APIKey)

	bodyBytes, err := jsonutil.MarshalBody(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
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
		return nil, NewHTTPError("gemini_multimodal", resp.StatusCode, respBody, resp.Header)
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
			if line == "" || !strings.HasPrefix(line, "{") {
				continue
			}

			var streamResp geminiStreamResponse
			// perf-v6 round 8 Task 1：使用 pooled stringReader 避免每条 SSE 消息分配 + []byte 拷贝
			if err := jsonutil.DecodeString(line, &streamResp); err != nil {
				continue
			}

			if len(streamResp.Candidates) > 0 && len(streamResp.Candidates[0].Content.Parts) > 0 {
				text := streamResp.Candidates[0].Content.Parts[0].Text
				if text != "" {
					select {
					case ch <- Chunk{Content: text}:
					case <-ctx.Done():
						return
					}
				}
			}

			if streamResp.UsageMetadata.TotalTokenCount > 0 {
				select {
				case ch <- Chunk{
					Done: true,
					Usage: &Usage{
						PromptTokens:     streamResp.UsageMetadata.PromptTokenCount,
						CompletionTokens: streamResp.UsageMetadata.CandidatesTokenCount,
						TotalTokens:      streamResp.UsageMetadata.TotalTokenCount,
					},
				}:
				case <-ctx.Done():
					return
				}
			}
		}

		if err := scanner.Err(); err != nil {
			slog.Warn("Gemini Multimodal 流式读取错误", "error", err)
		}

		select {
		case ch <- Chunk{Done: true}:
		case <-ctx.Done():
		}
	}()

	return ch, nil
}

// buildMultimodalContents 构建多模态内容列表
func (p *GeminiMultimodalProvider) buildMultimodalContents(msgs []*ChatMessageExt) ([]map[string]any, []map[string]any) {
	var contents []map[string]any
	var systemParts []map[string]any

	for _, m := range msgs {
		if m.Role == "system" {
			systemParts = append(systemParts, map[string]any{
				"text": m.ExtractText(),
			})
			continue
		}

		parts := make([]map[string]any, 0, len(m.Contents))

		for _, c := range m.Contents {
			part := p.convertToGeminiFormat(c)
			if part != nil {
				parts = append(parts, part)
			}
		}

		if len(parts) == 0 {
			continue
		}

		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}

		content := map[string]any{
			"role":  role,
			"parts": parts,
		}

		contents = append(contents, content)
	}

	return contents, systemParts
}

// convertToGeminiFormat 转换为 Gemini API 格式
func (p *GeminiMultimodalProvider) convertToGeminiFormat(content *MultimodalContent) map[string]any {
	switch content.Type {
	case ContentTypeText:
		if content.Text == "" {
			return nil
		}
		return map[string]any{
			"text": content.Text,
		}

	case ContentTypeImageURL:
		imageURL := content.URL
		if imageURL == "" {
			return nil
		}

		// perf-v5 Task 6：复用 Provider 共享 client（连接池 + Transport 调优）
		resp, err := p.client.Get(imageURL)
		if err != nil {
			slog.Warn("下载图片失败", "url", imageURL, "error", err)
			return nil
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			slog.Warn("图片下载返回非200状态", "url", imageURL, "status", resp.StatusCode)
			return nil
		}

		data, err := io.ReadAll(resp.Body)
		if err != nil {
			slog.Warn("读取图片数据失败", "error", err)
			return nil
		}

		mediaType := detectMediaType(resp.Header.Get("Content-Type"), imageURL)

		return map[string]any{
			"inlineData": map[string]any{
				"mimeType": mediaType,
				"data":     base64.StdEncoding.EncodeToString(data),
			},
		}

	case ContentTypeImageB64:
		if content.Data == "" || content.MIME == "" {
			return nil
		}
		return map[string]any{
			"inlineData": map[string]any{
				"mimeType": content.MIME,
				"data":     content.Data,
			},
		}

	case ContentTypeAudio:
		if content.Data == "" || content.MIME == "" {
			return nil
		}
		return map[string]any{
			"inlineData": map[string]any{
				"mimeType": content.MIME,
				"data":     content.Data,
			},
		}

	case ContentTypeVideo:
		if content.URL != "" {
			return map[string]any{
				"fileData": map[string]any{
					"mimeType": content.MIME,
					"fileUri":  content.URL,
				},
			}
		}
		if content.Data != "" && content.MIME != "" {
			return map[string]any{
				"inlineData": map[string]any{
					"mimeType": content.MIME,
					"data":     content.Data,
				},
			}
		}
		return nil

	default:
		return nil
	}
}

// buildGenerationConfig 构建生成配置
func (p *GeminiMultimodalProvider) buildGenerationConfig(req *CompletionRequestExt) map[string]any {
	config := map[string]any{}

	if temp := ResolveTemperature(req.Temperature, p.config.Temperature); temp != nil && *temp > 0 {
		config["temperature"] = temp
	}

	maxTok := p.resolveMaxTokens(req)
	if maxTok > 0 {
		config["maxOutputTokens"] = maxTok
	}

	// 结构化输出：Gemini 通过 responseMimeType + responseSchema 实现
	if req.ResponseFormat != nil {
		switch req.ResponseFormat.Type {
		case ResponseFormatJSONObject:
			config["responseMimeType"] = "application/json"
		case ResponseFormatJSONSchema:
			config["responseMimeType"] = "application/json"
			if req.ResponseFormat.JSONSchema != nil {
				config["responseSchema"] = req.ResponseFormat.JSONSchema.Schema
			}
		}
	}

	return config
}

// resolveModel 解析模型名称
func (p *GeminiMultimodalProvider) resolveModel(model string) string {
	if model != "" {
		return model
	}
	return p.config.Model
}

// resolveMaxTokens 解析最大 token 数
func (p *GeminiMultimodalProvider) resolveMaxTokens(req *CompletionRequestExt) int {
	if req.MaxTokens > 0 {
		return req.MaxTokens
	}
	if p.config.MaxTokens > 0 {
		return p.config.MaxTokens
	}
	return defaultGeminiMultimodalMaxTokens
}

// doRequest 发送 HTTP 请求
func (p *GeminiMultimodalProvider) doRequest(ctx context.Context, model, endpoint string, body any) ([]byte, error) {
	url := fmt.Sprintf("%s/v1beta/models/%s%s?key=%s",
		p.config.BaseURL, model, endpoint, p.config.APIKey)

	bodyBytes, err := jsonutil.MarshalBody(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
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
		return nil, NewHTTPError("gemini_multimodal", resp.StatusCode, respBody, resp.Header)
	}

	limitedReader := io.LimitReader(resp.Body, maxResponseSize)
	responseData, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return responseData, nil
}

// Info 返回模型信息
func (p *GeminiMultimodalProvider) Info() ModelInfo {
	return ModelInfo{
		Name:              p.config.Model,
		Provider:          "google",
		MaxContext:        1000000,
		SupportsTools:     true,
		SupportsStreaming: true,
	}
}

func (p *GeminiMultimodalProvider) InfoExt() ModelInfoExt {
	return ModelInfoExt{
		ModelInfo:         p.Info(),
		SupportsVision:    true,
		SupportsAudio:     true,
		SupportsVideo:     true,
		MaxImageSize:      20,
		MaxImagesPerMsg:   16,
		AcceptedMIMETypes: []string{"image/png", "image/jpeg", "image/gif", "image/webp", "audio/mp3", "audio/wav", "video/mp4"},
	}
}

// Complete 向后兼容接口
func (p *GeminiMultimodalProvider) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
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
func (p *GeminiMultimodalProvider) Stream(ctx context.Context, req *CompletionRequest) (<-chan Chunk, error) {
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

// CallTools tool调用
func (p *GeminiMultimodalProvider) CallTools(ctx context.Context, req *ToolCallRequest) (*ToolCallResponse, error) {
	model := p.resolveModel(req.Model)

	// 转换消息为 Gemini 格式
	contents, systemParts := p.buildChatContents(req.Messages)

	// 构建tool声明
	var tools map[string]any
	if len(req.Tools) > 0 {
		functions := make([]map[string]any, len(req.Tools))
		for i, t := range req.Tools {
			functions[i] = map[string]any{
				"name":        t.Function.Name,
				"description": t.Function.Description,
				"parameters":  t.Function.Parameters,
			}
		}
		tools = map[string]any{
			"functionDeclarations": functions,
		}
	}

	body := map[string]any{
		"contents": contents,
	}
	if len(systemParts) > 0 {
		body["systemInstruction"] = map[string]any{
			"parts": systemParts,
		}
	}
	if tools != nil {
		body["tools"] = tools
	}

	generationConfig := p.buildGenerationConfig(&CompletionRequestExt{
		Model:       req.Model,
		MaxTokens:   0,
		Temperature: nil,
	})
	if len(generationConfig) > 0 {
		body["generationConfig"] = generationConfig
	}

	raw, err := p.doRequest(ctx, model, ":generateContent", body)
	if err != nil {
		return nil, err
	}

	var resp geminiResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrResponseParseFailed, err)
	}

	result := &ToolCallResponse{
		Usage: Usage{
			PromptTokens:     resp.UsageMetadata.PromptTokenCount,
			CompletionTokens: resp.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      resp.UsageMetadata.TotalTokenCount,
		},
	}

	if len(resp.Candidates) > 0 && len(resp.Candidates[0].Content.Parts) > 0 {
		// 解析内容和tool调用
		for _, part := range resp.Candidates[0].Content.Parts {
			if part.Text != "" {
				result.Content += part.Text
			}
			if part.FunctionCall.Name != "" {
				argsJSON, _ := json.Marshal(part.FunctionCall.Args)
				result.ToolCalls = append(result.ToolCalls, FunctionCall{
					ID:        part.FunctionCall.Name, // Gemini 没有独立的 ID，用函数名代替
					Name:      part.FunctionCall.Name,
					Arguments: string(argsJSON),
				})
			}
		}
	}

	return result, nil
}

// Embeddings 文本嵌入（不支持）
// buildChatContents 构建普通聊天内容
func (p *GeminiMultimodalProvider) buildChatContents(msgs []ChatMessage) ([]map[string]any, []map[string]any) {
	var contents []map[string]any
	var systemParts []map[string]any

	for _, m := range msgs {
		if m.Role == "system" {
			systemParts = append(systemParts, map[string]any{
				"text": m.Content,
			})
			continue
		}

		parts := []map[string]any{
			{"text": m.Content},
		}

		// 处理tool调用和tool响应
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			// Gemini 的 function call 处理
			for _, tc := range m.ToolCalls {
				args := map[string]any{}
				if err := json.Unmarshal([]byte(tc.Arguments), &args); err != nil {
					// tool调用参数解析失败，使用原始字符串作为参数
					args = map[string]any{"raw": tc.Arguments}
				}
				parts = append(parts, map[string]any{
					"functionCall": map[string]any{
						"name": tc.Name,
						"args": args,
					},
				})
			}
		}

		if m.Role == "tool" && m.ToolCallID != "" {
			// tool响应
			var respData any
			err := json.Unmarshal([]byte(m.Content), &respData)
			if err != nil {
				respData = m.Content
			}
			parts = append(parts, map[string]any{
				"functionResponse": map[string]any{
					"name": m.ToolCallID,
					"response": map[string]any{
						"name":    m.ToolCallID,
						"content": respData,
					},
				},
			})
		}

		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}

		content := map[string]any{
			"role":  role,
			"parts": parts,
		}

		contents = append(contents, content)
	}

	return contents, systemParts
}

// geminiStreamResponse Gemini 流式响应结构
type geminiStreamResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text,omitempty"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

// detectMediaType 根据响应头或 URL 扩展名检测媒体类型
func detectMediaType(contentType, url string) string {
	if contentType != "" && contentType != "application/octet-stream" {
		return contentType
	}

	ext := strings.ToLower(filepath.Ext(url))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	default:
		return "image/jpeg"
	}
}
