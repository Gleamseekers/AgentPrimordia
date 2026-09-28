package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"agentprimordia/internal/jsonutil"
)

// ===== 泛型 Provider 基础设施（v2.0 #3） =====
//
// BaseProvider 封装所有 Provider 共用的 HTTP 基础设施与公共方法。
// 具体 Provider 通过嵌入 BaseProvider 复用这些公共方法，减少模板代码。

// BaseProvider 封装 HTTP 客户端与共享配置（APIKey / BaseURL / 模型解析等）。
// v6.x 评估 §4.2 P1-4：client（非流式，带整体超时）与 streamClient（流式，
// 无整体超时 + ResponseHeaderTimeout）分离，避免长流被 Client.Timeout 掐断。
type BaseProvider struct {
	client       *http.Client
	streamClient *http.Client
}

// NewBaseProvider 创建 BaseProvider 实例。
func NewBaseProvider(timeout time.Duration) *BaseProvider {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &BaseProvider{
		client:       NewDefaultLLMClient(timeout),
		streamClient: NewDefaultLLMStreamClient(),
	}
}

// Client 返回 HTTP 客户端（非流式）。
func (p *BaseProvider) Client() *http.Client {
	return p.client
}

// StreamClient 返回流式请求专用 HTTP 客户端（v6.x 评估 §4.2 P1-4）。
// 无 Client.Timeout 整体超时；响应头超时由 transport 的
// ResponseHeaderTimeout 约束；流式总时长由调用方 ctx deadline 控制。
func (p *BaseProvider) StreamClient() *http.Client {
	return p.streamClient
}

// DoRequest 发送 HTTP POST 请求，返回原始 JSON 响应字节。
// 错误统一使用 NewHTTPError 包装，包含状态码和响应体。
//
// 参数：
//   - authHeader: 完整的 Authorization 头值（如 "Bearer xxx"）
//   - targetProvider: 用于错误信息的 Provider 标识
func (p *BaseProvider) DoRequest(ctx context.Context, baseURL, path, authHeader string, body any, targetProvider string) ([]byte, error) {
	bodyBytes, err := jsonutil.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	baseURL = strings.TrimRight(baseURL, "/")
	httpReq, err := http.NewRequestWithContext(ctx, "POST", baseURL+path, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", authHeader)
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
		return nil, NewHTTPError(targetProvider, resp.StatusCode, respBody, resp.Header)
	}

	return respBody, nil
}

// DoRequestCustom 发送 HTTP POST 请求并支持自定义请求头集。
//
// v6.x（评估报告 §五.1）：Anthropic（x-api-key + anthropic-version）与
// Azure（api-key + api-version query）无法复用 DoRequest 的 Authorization
// 单头签名。此方法让它们共享同一套 HTTP 底座（client 复用 + 限流响应体
// 读取 + 统一错误包装），消除各 Provider 重复的 doRequest 实现。
func (p *BaseProvider) DoRequestCustom(ctx context.Context, url, method string, headers map[string]string, body any, targetProvider string) ([]byte, error) {
	bodyBytes, err := jsonutil.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	var bodyReader io.Reader
	if bodyBytes != nil {
		bodyReader = bytes.NewReader(bodyBytes)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", userAgent)
	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}

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
		// 尝试解析 `{"error": {...}}` 结构，提供更丰富的错误分类
		//（与旧 Azure/Anthropic doRequest 行为一致，携带 APIError + Retry-After）。
		var errResp struct {
			Error *APIError `json:"error"`
		}
		parsed := json.Unmarshal(respBody, &errResp) == nil && errResp.Error != nil
		if parsed {
			return nil, NewHTTPErrorOrAPIError(targetProvider, resp.StatusCode, respBody, resp.Header, errResp.Error, true)
		}
		return nil, NewHTTPError(targetProvider, resp.StatusCode, respBody, resp.Header)
	}
	return respBody, nil
}

// ResolveModel 解析模型名称：优先使用请求中的模型，否则回退到配置默认值。
func ResolveModelName(reqModel, defaultModel string) string {
	return ResolveModel(reqModel, defaultModel)
}
