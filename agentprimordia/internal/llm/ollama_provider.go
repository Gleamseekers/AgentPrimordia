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
	"time"
)

const (
	ollamaDefaultBaseURL    = "http://localhost:11434"
	defaultOllamaMaxContext = 8192
	defaultOllamaEmbedModel = "nomic-embed-text"
)

const defaultOllamaTimeout = 300 * time.Second

// OllamaProvider 实现本地 Ollama 模型调用
type OllamaProvider struct {
	config Config
	client *http.Client
	// v6.x 评估 §4.2 P1-4：流式专用 client（无整体超时 + ResponseHeaderTimeout）
	streamClient *http.Client
}

// NewOllamaProvider 创建 Ollama 本地模型 Provider
func NewOllamaProvider(cfg Config) (*OllamaProvider, error) {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = ollamaDefaultBaseURL
	}
	cfg.BaseURL = strings.TrimRight(baseURL, "/")

	if cfg.Model == "" {
		cfg.Model = "llama3"
	}

	return &OllamaProvider{
		config:       cfg,
		client:       NewDefaultLLMClient(defaultOllamaTimeout), // 本地模型可能更慢
		streamClient: NewDefaultLLMStreamClient(),
	}, nil
}

func (p *OllamaProvider) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	model := p.resolveModel(req)

	body := map[string]any{
		"model":    model,
		"messages": p.buildMessages(req.Messages),
		"stream":   false,
	}
	if temp := ResolveTemperature(req.Temperature, p.config.Temperature); temp != nil && *temp > 0 {
		body["options"] = map[string]any{"temperature": temp}
	}
	if maxTok := p.resolveMaxTokens(req); maxTok > 0 {
		if opts, ok := body["options"].(map[string]any); ok {
			opts["num_predict"] = maxTok
		} else {
			body["options"] = map[string]any{"num_predict": maxTok}
		}
	}
	if req.ResponseFormat != nil {
		p.injectResponseFormat(body, req.ResponseFormat)
	}

	raw, err := p.doRequest(ctx, "/api/chat", body)
	if err != nil {
		return nil, err
	}

	var resp ollamaChatResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrResponseParseFailed, err)
	}

	return &CompletionResponse{
		ID:      fmt.Sprintf("ollama-%s", model),
		Model:   model,
		Content: resp.Message.Content,
		Role:    "assistant",
		Usage: Usage{
			PromptTokens:     resp.PromptEvalCount,
			CompletionTokens: resp.EvalCount,
			TotalTokens:      resp.PromptEvalCount + resp.EvalCount,
		},
	}, nil
}

func (p *OllamaProvider) Stream(ctx context.Context, req *CompletionRequest) (<-chan Chunk, error) {
	model := p.resolveModel(req)

	body := map[string]any{
		"model":    model,
		"messages": p.buildMessages(req.Messages),
		"stream":   true,
	}
	if temp := ResolveTemperature(req.Temperature, p.config.Temperature); temp != nil && *temp > 0 {
		body["options"] = map[string]any{"temperature": temp}
	}
	if req.ResponseFormat != nil {
		p.injectResponseFormat(body, req.ResponseFormat)
	}

	bodyBytes, err := jsonutil.MarshalBody(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.config.BaseURL+"/api/chat", bytes.NewReader(bodyBytes))
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
		return nil, NewHTTPError("ollama", resp.StatusCode, respBody, resp.Header)
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
			if line == "" {
				continue
			}

			var chunkResp ollamaChatResponse
			// perf-v6 round 8 Task 1：使用 pooled stringReader 避免每条 SSE 消息分配 + []byte 拷贝
			if err := jsonutil.DecodeString(line, &chunkResp); err != nil {
				continue
			}

			chunk := Chunk{
				Content: chunkResp.Message.Content,
				Done:    chunkResp.Done,
			}
			select {
			case ch <- chunk:
			case <-ctx.Done():
				return
			}
			if chunkResp.Done {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			slog.Warn("Ollama 流式读取错误", "error", err)
		}
		select {
		case ch <- Chunk{Done: true}:
		case <-ctx.Done():
		}
	}()

	return ch, nil
}

func (p *OllamaProvider) CallTools(ctx context.Context, req *ToolCallRequest) (*ToolCallResponse, error) {
	// Ollama 的tool调用支持取决于模型能力
	// 这里实现 Ollama 原生tool调用格式
	model := p.config.Model
	if req.Model != "" {
		model = req.Model
	}

	tools := make([]ollamaTool, len(req.Tools))
	for i, t := range req.Tools {
		tools[i] = ollamaTool{
			Type: "function",
			Function: ollamaFunction{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  t.Function.Parameters,
			},
		}
	}

	body := map[string]any{
		"model":    model,
		"messages": p.buildMessages(req.Messages),
		"tools":    tools,
		"stream":   false,
	}

	raw, err := p.doRequest(ctx, "/api/chat", body)
	if err != nil {
		return nil, err
	}

	var resp ollamaChatResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrResponseParseFailed, err)
	}

	result := &ToolCallResponse{
		Content: resp.Message.Content,
		Usage: Usage{
			PromptTokens:     resp.PromptEvalCount,
			CompletionTokens: resp.EvalCount,
			TotalTokens:      resp.PromptEvalCount + resp.EvalCount,
		},
	}

	for _, tc := range resp.Message.ToolCalls {
		argsJSON, err := json.Marshal(tc.Function.Arguments)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal Ollama function call args: %w", err)
		}
		result.ToolCalls = append(result.ToolCalls, FunctionCall{
			ID:        fmt.Sprintf("ollama_%s_%d", tc.Function.Name, tc.ID),
			Name:      tc.Function.Name,
			Arguments: string(argsJSON),
		})
	}

	return result, nil
}

func (p *OllamaProvider) Embeddings(ctx context.Context, texts []string) ([][]float32, error) {
	model := p.config.Model
	// 使用专门的 embedding 模型
	if !strings.Contains(model, "embed") {
		model = defaultOllamaEmbedModel
	}

	results := make([][]float32, len(texts))
	for i, text := range texts {
		body := map[string]any{
			"model":  model,
			"prompt": text,
		}

		raw, err := p.doRequest(ctx, "/api/embeddings", body)
		if err != nil {
			return nil, err
		}

		var resp ollamaEmbedResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrResponseParseFailed, err)
		}
		results[i] = resp.Embedding
	}
	return results, nil
}

func (p *OllamaProvider) Info() ModelInfo {
	contextSizes := map[string]int{
		"llama3":         8192,
		"llama3.1":       131072,
		"llama3.2":       131072,
		"mistral":        32768,
		"mixtral":        32768,
		"codellama":      16384,
		"qwen2":          32768,
		"gemma2":         8192,
		"deepseek-coder": 16384,
	}
	maxCtx := defaultOllamaMaxContext
	if c, ok := contextSizes[p.config.Model]; ok {
		maxCtx = c
	}
	return ModelInfo{
		Name:              p.config.Model,
		Provider:          "ollama",
		MaxContext:        maxCtx,
		SupportsTools:     true,
		SupportsStreaming: true,
	}
}

// ===== 内部方法 =====

func (p *OllamaProvider) doRequest(ctx context.Context, path string, body any) (json.RawMessage, error) {
	bodyBytes, err := jsonutil.MarshalBody(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.config.BaseURL+path, bytes.NewReader(bodyBytes))
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
		// perf-v6 round 8 Task 3：携带 Retry-After + 错误分类
		return nil, NewHTTPError("ollama", resp.StatusCode, respBody, resp.Header)
	}

	return respBody, nil
}

func (p *OllamaProvider) buildMessages(msgs []ChatMessage) []map[string]any {
	result := make([]map[string]any, len(msgs))
	for i, m := range msgs {
		result[i] = map[string]any{
			"role":    m.Role,
			"content": m.Content,
		}
	}
	return result
}

func (p *OllamaProvider) resolveModel(req *CompletionRequest) string {
	if req != nil && req.Model != "" {
		return req.Model
	}
	return p.config.Model
}

func (p *OllamaProvider) resolveMaxTokens(req *CompletionRequest) int {
	if req != nil && req.MaxTokens > 0 {
		return req.MaxTokens
	}
	return p.config.MaxTokens
}

// ===== Ollama API 响应类型 =====

type ollamaChatResponse struct {
	Model     string `json:"model"`
	CreatedAt string `json:"created_at"`
	Message   struct {
		Role      string `json:"role"`
		Content   string `json:"content"`
		ToolCalls []struct {
			ID       int `json:"id"`
			Function struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls,omitempty"`
	} `json:"message"`
	Done            bool   `json:"done"`
	DoneReason      string `json:"done_reason,omitempty"`
	PromptEvalCount int    `json:"prompt_eval_count"`
	EvalCount       int    `json:"eval_count"`
}

type ollamaTool struct {
	Type     string         `json:"type"`
	Function ollamaFunction `json:"function"`
}

type ollamaFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type ollamaEmbedResponse struct {
	Embedding []float32 `json:"embedding"`
}

// injectResponseFormat 将 ResponseFormat 注入 Ollama 请求体
func (p *OllamaProvider) injectResponseFormat(body map[string]any, rf *ResponseFormat) {
	switch rf.Type {
	case ResponseFormatJSONObject:
		body["format"] = "json"
	case ResponseFormatJSONSchema:
		if rf.JSONSchema != nil {
			body["format"] = rf.JSONSchema.Schema
		} else {
			body["format"] = "json"
		}
	}
}

type OllamaModelInfo struct {
	Name       string `json:"name"`
	ModifiedAt string `json:"modified_at"`
	Size       int64  `json:"size"`
	Digest     string `json:"digest"`
	Details    struct {
		ParentModel       string `json:"parent_model"`
		Format            string `json:"format"`
		Family            string `json:"family"`
		Families          string `json:"families"`
		ParameterSize     string `json:"parameter_size"`
		QuantizationLevel string `json:"quantization_level"`
	} `json:"details"`
}

func (p *OllamaProvider) ListModels(ctx context.Context) ([]OllamaModelInfo, error) {
	raw, err := p.doGet(ctx, "/api/tags")
	if err != nil {
		return nil, err
	}

	var resp struct {
		Models []OllamaModelInfo `json:"models"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrResponseParseFailed, err)
	}
	return resp.Models, nil
}

func (p *OllamaProvider) PullModel(ctx context.Context, modelName string) error {
	body := map[string]any{
		"name":   modelName,
		"stream": false,
	}
	_, err := p.doRequest(ctx, "/api/pull", body)
	return err
}

func (p *OllamaProvider) DeleteModel(ctx context.Context, modelName string) error {
	bodyBytes, err := jsonutil.MarshalBody(map[string]any{"name": modelName})
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "DELETE", p.config.BaseURL+"/api/delete", bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
		// perf-v6 round 8 Task 3：携带 Retry-After + 错误分类
		return NewHTTPError("ollama", resp.StatusCode, respBody, resp.Header)
	}
	return nil
}

func (p *OllamaProvider) Ping(ctx context.Context) error {
	raw, err := p.doGet(ctx, "/api/tags")
	if err != nil {
		return fmt.Errorf("ollama not reachable: %w", err)
	}
	if len(raw) == 0 {
		return fmt.Errorf("ollama returned empty response")
	}
	return nil
}

func (p *OllamaProvider) ModelExists(ctx context.Context, modelName string) (bool, error) {
	models, err := p.ListModels(ctx)
	if err != nil {
		return false, err
	}
	for _, m := range models {
		if m.Name == modelName || strings.HasPrefix(m.Name, modelName+":") {
			return true, nil
		}
	}
	return false, nil
}

func (p *OllamaProvider) doGet(ctx context.Context, path string) (json.RawMessage, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", p.config.BaseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
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
		// perf-v6 round 8 Task 3：携带 Retry-After + 错误分类
		return nil, NewHTTPError("ollama", resp.StatusCode, respBody, resp.Header)
	}

	return respBody, nil
}
