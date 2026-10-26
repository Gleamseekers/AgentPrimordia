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

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/jsonutil" // perf-v6 round 6 Task 1
)

// ===== OpenAI 兼容多模态 Provider 的共享实现 =====
//
// 通义千问（Qwen-VL，DashScope）与智谱 GLM（GLM-4V，bigmodel）的
// /chat/completions 请求构造、SSE 流式解析、错误分类、向后兼容适配逻辑
// 曾是两份逐字节相同的代码（约 400 行 × 2）。两份拷贝的危害不是"不好看"，
// 而是**修复只落在一侧**：任一侧的缺陷修复都不会自动同步到另一侧。
//
// 现将共同逻辑上提为本类型，厂商差异收敛到 openAICompatProfile 的 6 个字段
// 与 CallTools 支持性。"两侧行为必须一致"由
// internal/llm/openai_compat_multimodal_test.go 的差分断言钉住：
// 若某一侧被再次单独改动，该测试立刻变红。

// openAICompatProfile 描述一个 OpenAI 兼容多模态 Provider 的厂商差异。
//
// 之所以用 profile 而不是在两侧各写一份常量：这些值正是历史上唯一
// 让两份实现"看起来不同"的东西，集中一处才能一眼看清差异全集。
type openAICompatProfile struct {
	// infoProvider 写入 Info().Provider。
	infoProvider string
	// errorLabel 写入 NewHTTPError 的 provider 参数（HTTP 错误分类用）。
	//
	// 注意它与 infoProvider 在 GLM 侧**故意不同**：GLM 的 Info() 报告厂商
	// "zhipu"，而 HTTP 错误 historically 报告产品 "glm"。两者都有既有调用方
	// 依赖，故拆成两个字段分别保持原值，不做"统一"。
	errorLabel     string
	defaultBaseURL string
	defaultModel   string
	maxContext     int
	maxTokens      int
	maxImageSize   int
}

// qwenProfile 通义千问厂商参数。返回新值而非包级 var，避免可变的全局状态。
func qwenProfile() openAICompatProfile {
	return openAICompatProfile{
		infoProvider:   "qwen",
		errorLabel:     "qwen",
		defaultBaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
		defaultModel:   "qwen-vl-max-latest",
		maxContext:     32768,
		maxTokens:      2000,
		maxImageSize:   10,
	}
}

// glmProfile 智谱 GLM 厂商参数。返回新值而非包级 var，避免可变的全局状态。
func glmProfile() openAICompatProfile {
	return openAICompatProfile{
		infoProvider:   "zhipu",
		errorLabel:     "glm",
		defaultBaseURL: "https://open.bigmodel.cn/api/paas/v4",
		defaultModel:   "glm-4v-flash",
		maxContext:     128000,
		maxTokens:      4096,
		maxImageSize:   20,
	}
}

// openAICompatMultimodal 是 OpenAI 兼容多模态 Provider 的共享实现。
//
// 由 QwenProvider / GLMProvider 内嵌，方法经提升成为两者的公共 API。
// 注意：字段必须保持可被内嵌方直接访问（client / streamClient 在测试中
// 被指向 httptest.Server 的 client），故不做私有化封装。
type openAICompatMultimodal struct {
	profile openAICompatProfile
	config  Config
	client  *http.Client
	// streamClient 流式专用 client（无整体超时 + ResponseHeaderTimeout）——
	// v6.x 评估 §4.2 P1-4。
	streamClient *http.Client
}

// newOpenAICompatMultimodal 按厂商 profile 构造共享实现。
// APIKey 为必填；BaseURL / Model 为空时落到厂商默认值。
func newOpenAICompatMultimodal(profile openAICompatProfile, cfg Config) (openAICompatMultimodal, error) {
	if cfg.APIKey == "" {
		return openAICompatMultimodal{}, ErrAPIKeyRequired
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = profile.defaultBaseURL
	}
	cfg.BaseURL = strings.TrimRight(baseURL, "/")

	if cfg.Model == "" {
		cfg.Model = profile.defaultModel
	}

	return openAICompatMultimodal{
		profile:      profile,
		config:       cfg,
		client:       NewDefaultLLMClient(defaultTimeout),
		streamClient: NewDefaultLLMStreamClient(),
	}, nil
}

// CompleteMultimodal 多模态补全（核心方法）
func (p *openAICompatMultimodal) CompleteMultimodal(ctx context.Context, req *CompletionRequestExt) (*CompletionResponse, error) {
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
func (p *openAICompatMultimodal) StreamMultimodal(ctx context.Context, req *CompletionRequestExt) (<-chan Chunk, error) {
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

	// 流式路径使用无整体超时的 stream client
	resp, err := p.streamClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		limitedReader := io.LimitReader(resp.Body, maxResponseSize)
		respBody, _ := io.ReadAll(limitedReader)
		// perf-v6 round 8 Task 3：携带 Retry-After + 错误分类
		return nil, NewHTTPError(p.profile.errorLabel, resp.StatusCode, respBody, resp.Header)
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
			slog.Warn(p.profile.errorLabel+" 流式读取错误", "error", err)
		}

		select {
		case ch <- Chunk{Done: true}:
		case <-ctx.Done():
		}
	}()

	return ch, nil
}

// buildMultimodalMessages 构建多模态消息列表（OpenAI 兼容格式）
func (p *openAICompatMultimodal) buildMultimodalMessages(msgs []*ChatMessageExt) []map[string]any {
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
			part := p.convertContentPart(c)
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

// convertContentPart 将单个多模态内容片段转换为 OpenAI 兼容格式。
//
// 纯文本与 URL 图片两条路径对 Qwen / GLM 完全相同；base64 图片统一折叠为
// data URL。不支持的类型（当前为音频/视频）返回 nil，由调用方丢弃。
func (p *openAICompatMultimodal) convertContentPart(content *MultimodalContent) map[string]any {
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
		return nil // VL 系列目前主要支持图片，暂不支持音频/视频
	}
}

// resolveModel 解析模型名称
func (p *openAICompatMultimodal) resolveModel(model string) string {
	return ResolveModel(model, p.config.Model)
}

// resolveMaxTokens 解析最大 token 数：请求值 > 配置值 > 厂商默认值
func (p *openAICompatMultimodal) resolveMaxTokens(req *CompletionRequestExt) int {
	if req.MaxTokens > 0 {
		return req.MaxTokens
	}
	if p.config.MaxTokens > 0 {
		return p.config.MaxTokens
	}
	return p.profile.maxTokens
}

// buildMessages 构建标准 OpenAI 格式的消息（CallTools 路径使用）
func (p *openAICompatMultimodal) buildMessages(msgs []ChatMessage) []map[string]any {
	return BuildOpenAIMessages(msgs)
}

// doRequest 发送 HTTP 请求
func (p *openAICompatMultimodal) doRequest(ctx context.Context, endpoint string, body any) ([]byte, error) {
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
		return nil, NewHTTPError(p.profile.errorLabel, resp.StatusCode, respBody, resp.Header)
	}

	limitedReader := io.LimitReader(resp.Body, maxResponseSize)
	responseData, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return responseData, nil
}

// Info 返回模型信息
func (p *openAICompatMultimodal) Info() ModelInfo {
	return ModelInfo{
		Name:              p.config.Model,
		Provider:          p.profile.infoProvider,
		MaxContext:        p.profile.maxContext,
		SupportsTools:     true,
		SupportsStreaming: true,
	}
}

// InfoExt 返回扩展模型信息。
// MaxImagesPerMsg 与 AcceptedMIMETypes 在两侧原本一致，仅 MaxImageSize 不同。
func (p *openAICompatMultimodal) InfoExt() ModelInfoExt {
	return ModelInfoExt{
		ModelInfo:         p.Info(),
		SupportsVision:    true,
		SupportsAudio:     false,
		SupportsVideo:     false,
		MaxImageSize:      p.profile.maxImageSize,
		MaxImagesPerMsg:   8,
		AcceptedMIMETypes: []string{"image/png", "image/jpeg", "image/gif", "image/webp"},
	}
}

// Complete 向后兼容接口：将 CompletionRequest 适配为多模态请求。
func (p *openAICompatMultimodal) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	return p.CompleteMultimodal(ctx, toCompletionRequestExt(req))
}

// Stream 向后兼容接口：将 CompletionRequest 适配为多模态请求。
func (p *openAICompatMultimodal) Stream(ctx context.Context, req *CompletionRequest) (<-chan Chunk, error) {
	return p.StreamMultimodal(ctx, toCompletionRequestExt(req))
}

// toCompletionRequestExt 将旧版 CompletionRequest 转为 CompletionRequestExt。
// 该适配逻辑在 Qwen / GLM 两侧原本逐字节相同，故上提为共享函数。
func toCompletionRequestExt(req *CompletionRequest) *CompletionRequestExt {
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
	return extReq
}

// 注意：QwenProvider 与 GLMProvider 均不实现 Embeddings——文本嵌入请使用
// internal/llm/embedding.go 中的 OpenAIEmbeddingProvider / OllamaEmbeddingProvider。
// 原两份实现末尾各有一条悬空的 "// Embeddings 文本嵌入（暂不支持）" 注释，
// 无对应函数；此处合并为一条有实际指向的说明。
