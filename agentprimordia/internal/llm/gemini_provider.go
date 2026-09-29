package llm

import (
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/jsonutil" // perf-v6 round 6 Task 1
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
	geminiDefaultBaseURL        = "https://generativelanguage.googleapis.com"
	defaultGeminiMaxContext     = 1048576
	defaultGeminiEmbedModel     = "text-embedding-004"
	geminiEmptyContentsFallback = "Hello"
)

// GeminiProvider 实现 Google Gemini 系列模型调用
type GeminiProvider struct {
	config Config
	client *http.Client
	// v6.x 评估 §4.2 P1-4：流式专用 client（无整体超时 + ResponseHeaderTimeout）
	streamClient *http.Client
}

// NewGeminiProvider 创建 Google Gemini Provider
func NewGeminiProvider(cfg Config) (*GeminiProvider, error) {
	if cfg.APIKey == "" {
		return nil, ErrAPIKeyRequired
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = geminiDefaultBaseURL
	}
	cfg.BaseURL = strings.TrimRight(baseURL, "/")

	if cfg.Model == "" {
		cfg.Model = "gemini-2.0-flash"
	}

	return &GeminiProvider{
		config:       cfg,
		client:       NewDefaultLLMClient(defaultTimeout),
		streamClient: NewDefaultLLMStreamClient(),
	}, nil
}

func (p *GeminiProvider) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	model := p.resolveModel(req)
	contents := p.buildContents(req.Messages)

	body := map[string]any{
		"contents": contents,
	}
	if sysParts := p.buildSystemInstruction(req.Messages); len(sysParts) > 0 {
		body["systemInstruction"] = map[string]any{
			"parts": sysParts,
		}
	}
	generationConfig := map[string]any{}
	if temp := ResolveTemperature(req.Temperature, p.config.Temperature); temp != nil && *temp > 0 {
		generationConfig["temperature"] = temp
	}
	if maxTok := p.resolveMaxTokens(req); maxTok > 0 {
		generationConfig["maxOutputTokens"] = maxTok
	}
	if req.ResponseFormat != nil {
		p.injectResponseFormat(generationConfig, req.ResponseFormat)
	}
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

func (p *GeminiProvider) Stream(ctx context.Context, req *CompletionRequest) (<-chan Chunk, error) {
	model := p.resolveModel(req)
	contents := p.buildContents(req.Messages)

	body := map[string]any{
		"contents": contents,
	}
	if sysParts := p.buildSystemInstruction(req.Messages); len(sysParts) > 0 {
		body["systemInstruction"] = map[string]any{
			"parts": sysParts,
		}
	}
	generationConfig := map[string]any{}
	if temp := ResolveTemperature(req.Temperature, p.config.Temperature); temp != nil && *temp > 0 {
		generationConfig["temperature"] = temp
	}
	if maxTok := p.resolveMaxTokens(req); maxTok > 0 {
		generationConfig["maxOutputTokens"] = maxTok
	}
	if req.ResponseFormat != nil {
		p.injectResponseFormat(generationConfig, req.ResponseFormat)
	}
	if len(generationConfig) > 0 {
		body["generationConfig"] = generationConfig
	}

	bodyBytes, err := jsonutil.MarshalBody(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?alt=sse&key=%s",
		p.config.BaseURL, model, p.config.APIKey)

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
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
		// perf-v6 round 8 Task 3：携带 Retry-After + 错误分类
		return nil, NewHTTPError("gemini", resp.StatusCode, respBody, resp.Header)
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

			var sseResp geminiResponse
			// perf-v6 round 8 Task 1：使用 pooled stringReader 避免每条 SSE 消息分配
			if err := jsonutil.DecodeString(data, &sseResp); err != nil {
				continue
			}

			if len(sseResp.Candidates) > 0 && len(sseResp.Candidates[0].Content.Parts) > 0 {
				chunk := Chunk{
					Content: sseResp.Candidates[0].Content.Parts[0].Text,
				}
				if sseResp.Candidates[0].FinishReason == "STOP" {
					chunk.Done = true
					chunk.Usage = &Usage{
						PromptTokens:     sseResp.UsageMetadata.PromptTokenCount,
						CompletionTokens: sseResp.UsageMetadata.CandidatesTokenCount,
						TotalTokens:      sseResp.UsageMetadata.TotalTokenCount,
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
		}
		if err := scanner.Err(); err != nil {
			slog.Warn("Gemini 流式读取错误", "error", err)
		}
		select {
		case ch <- Chunk{Done: true}:
		case <-ctx.Done():
		}
	}()

	return ch, nil
}

func (p *GeminiProvider) CallTools(ctx context.Context, req *ToolCallRequest) (*ToolCallResponse, error) {
	model := p.config.Model
	if req.Model != "" {
		model = req.Model
	}

	contents := p.buildContents(req.Messages)

	declarations := make([]geminiFunctionDeclaration, len(req.Tools))
	for i, t := range req.Tools {
		declarations[i] = geminiFunctionDeclaration{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			Parameters:  t.Function.Parameters,
		}
	}

	body := map[string]any{
		"contents": contents,
		"tools": []geminiToolContainer{
			{FunctionDeclarations: declarations},
		},
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

	if len(resp.Candidates) > 0 {
		for _, part := range resp.Candidates[0].Content.Parts {
			if part.Text != "" {
				result.Content = part.Text
			}
			if part.FunctionCall.Name != "" {
				argsJSON, err := json.Marshal(part.FunctionCall.Args)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal Gemini function call args: %w", err)
				}
				result.ToolCalls = append(result.ToolCalls, FunctionCall{
					ID:        fmt.Sprintf("gemini_fc_%s", part.FunctionCall.Name),
					Name:      part.FunctionCall.Name,
					Arguments: string(argsJSON),
				})
			}
		}
	}

	return result, nil
}

func (p *GeminiProvider) Embeddings(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	model := defaultGeminiEmbedModel

	requests := make([]map[string]any, len(texts))
	for i, t := range texts {
		requests[i] = map[string]any{
			"model": "models/" + model,
			"content": map[string]any{
				"parts": []map[string]any{
					{"text": t},
				},
			},
		}
	}

	body := map[string]any{
		"requests": requests,
	}

	raw, err := p.doRequest(ctx, model, ":batchEmbedContents", body)
	if err != nil {
		return nil, fmt.Errorf("batch embedding failed: %w", err)
	}

	var resp struct {
		Embeddings []struct {
			Values []float32 `json:"values"`
		} `json:"embeddings"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("%w: batch embed response: %v", ErrResponseParseFailed, err)
	}

	result := make([][]float32, len(resp.Embeddings))
	for i, emb := range resp.Embeddings {
		vec := make([]float32, len(emb.Values))
		copy(vec, emb.Values)
		result[i] = vec
	}

	return result, nil
}

func (p *GeminiProvider) Info() ModelInfo {
	contextSizes := map[string]int{
		"gemini-2.5-pro":        1000000,
		"gemini-2.5-flash":      1000000,
		"gemini-2.0-flash":      1048576,
		"gemini-2.0-flash-lite": 1048576,
		"gemini-1.5-pro":        2097152,
		"gemini-1.5-flash":      1048576,
	}
	maxCtx := defaultGeminiMaxContext
	if c, ok := contextSizes[p.config.Model]; ok {
		maxCtx = c
	}
	return ModelInfo{
		Name:              p.config.Model,
		Provider:          "google",
		MaxContext:        maxCtx,
		SupportsTools:     true,
		SupportsStreaming: true,
	}
}

// ===== 内部方法 =====

func (p *GeminiProvider) doRequest(ctx context.Context, model, action string, body any) (json.RawMessage, error) {
	bodyBytes, err := jsonutil.MarshalBody(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v1beta/models/%s%s?key=%s",
		p.config.BaseURL, model, action, p.config.APIKey)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", userAgent)

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
		return nil, NewHTTPErrorOrAPIError("gemini", resp.StatusCode, respBody, resp.Header, errResp.Error, parsed)
	}

	return respBody, nil
}

// buildContents 将通用 ChatMessage 转换为 Gemini contents 格式
func (p *GeminiProvider) buildContents(msgs []ChatMessage) []map[string]any {
	var result []map[string]any
	for _, m := range msgs {
		if m.Role == "system" {
			continue // system 通过 systemInstruction 传递
		}
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		result = append(result, map[string]any{
			"role": role,
			"parts": []map[string]any{
				{"text": m.Content},
			},
		})
	}
	if len(result) == 0 {
		result = []map[string]any{
			{"role": "user", "parts": []map[string]any{{"text": geminiEmptyContentsFallback}}},
		}
	}
	return result
}

// buildSystemInstruction 提取 system 消息
func (p *GeminiProvider) buildSystemInstruction(msgs []ChatMessage) []map[string]any {
	var parts []map[string]any
	for _, m := range msgs {
		if m.Role == "system" {
			parts = append(parts, map[string]any{"text": m.Content})
		}
	}
	return parts
}

func (p *GeminiProvider) resolveModel(req *CompletionRequest) string {
	if req != nil && req.Model != "" {
		return req.Model
	}
	return p.config.Model
}

func (p *GeminiProvider) resolveMaxTokens(req *CompletionRequest) int {
	if req != nil && req.MaxTokens > 0 {
		return req.MaxTokens
	}
	return p.config.MaxTokens
}

// ===== Gemini API 响应类型 =====

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text         string `json:"text,omitempty"`
				FunctionCall struct {
					Name string         `json:"name,omitempty"`
					Args map[string]any `json:"args,omitempty"`
				} `json:"functionCall,omitempty"`
			} `json:"parts"`
			Role string `json:"role"`
		} `json:"content"`
		FinishReason string `json:"finishReason,omitempty"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

type geminiFunctionDeclaration struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// geminiToolContainer Gemini tool容器（perf-v6 Task 4：typed struct 替代 map）
type geminiToolContainer struct {
	FunctionDeclarations []geminiFunctionDeclaration `json:"function_declarations,omitempty"`
}

// injectResponseFormat 将 ResponseFormat 注入 Gemini generationConfig
func (p *GeminiProvider) injectResponseFormat(config map[string]any, rf *ResponseFormat) {
	switch rf.Type {
	case ResponseFormatJSONObject:
		config["responseMimeType"] = "application/json"
	case ResponseFormatJSONSchema:
		config["responseMimeType"] = "application/json"
		if rf.JSONSchema != nil {
			config["responseSchema"] = rf.JSONSchema.Schema
		}
	}
}
