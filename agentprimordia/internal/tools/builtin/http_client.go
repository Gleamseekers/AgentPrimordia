package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/tools"
)

const (
	httpClientDefaultTimeout      = 30 * time.Second
	httpClientDefaultMaxBodySize  = 1 * 1024 * 1024 // 1MB
	httpClientDefaultUserAgent    = "AgentPrimordia/1.0 (HTTP Client Tool)"
	httpClientDefaultMaxRedirects = 10

	// httpClientMaxRequestBody 请求体大小上限（10MB）：超大 body 在发起
	// 网络请求前直接拒绝，避免无界内存放大与网络滥用。
	httpClientMaxRequestBody = 10 * 1024 * 1024
)

// HTTPClient 增强型 HTTP 客户端tool，支持多种认证方式和响应处理
type HTTPClient struct {
	timeout      time.Duration
	maxBodySize  int64
	maxRedirects int
	allowPrivate bool // 是否允许访问私有 IP

	transportOnce sync.Once
	transport     *http.Transport
}

// HTTPRequest HTTP 请求参数
type HTTPRequest struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
	Auth    *HTTPAuth         `json:"auth,omitempty"`
	Timeout int               `json:"timeout,omitempty"` // 秒
}

// HTTPAuth 认证信息
type HTTPAuth struct {
	Type     string `json:"type"`      // bearer, basic, apikey
	Token    string `json:"token"`     // bearer token
	Username string `json:"username"`  // basic auth 用户名
	Password string `json:"password"`  // basic auth 密码
	KeyName  string `json:"key_name"`  // apikey header 名称
	KeyValue string `json:"key_value"` // apikey header 值
}

// NewHTTPClient 创建新的 HTTPClient tool实例
func NewHTTPClient() *HTTPClient {
	return &HTTPClient{
		timeout:      httpClientDefaultTimeout,
		maxBodySize:  httpClientDefaultMaxBodySize,
		maxRedirects: httpClientDefaultMaxRedirects,
	}
}

// WithTimeout 设置默认超时时间
func (c *HTTPClient) WithTimeout(d time.Duration) *HTTPClient {
	c.timeout = d
	return c
}

// WithMaxBodySize 设置响应体最大大小
func (c *HTTPClient) WithMaxBodySize(size int64) *HTTPClient {
	c.maxBodySize = size
	return c
}

// WithMaxRedirects 设置最大重定向次数
func (c *HTTPClient) WithMaxRedirects(n int) *HTTPClient {
	c.maxRedirects = n
	return c
}

// WithAllowPrivate 允许访问内网地址（仅用于测试）
func (c *HTTPClient) WithAllowPrivate(allow bool) *HTTPClient {
	c.allowPrivate = allow
	return c
}

// Name 返回tool名称
func (c *HTTPClient) Name() string { return "http_client" }

// Description 返回tool描述
func (c *HTTPClient) Description() string {
	return `增强型 HTTP 客户端tool，支持多种认证方式和智能响应处理。
功能：
- 支持 GET/POST/PUT/DELETE/PATCH 方法
- 自定义请求头，自动设置 Content-Type
- 请求体支持 JSON、form-urlencoded、纯文本
- 认证方式：Bearer Token、Basic Auth、API Key
- 可配置超时（默认 30 秒）
- 响应体大小限制（默认 1MB）
- JSON 响应自动格式化输出
- SSRF 防护（默认阻止内网地址）
- 重定向次数限制（默认 10 次）

参数：
- method: HTTP 方法 [GET|POST|PUT|DELETE|PATCH]
- url: 完整请求 URL
- headers: 请求头（key-value 对象）
- body: 请求体（字符串）
- auth: 认证信息对象
  - type: 认证类型 [bearer|basic|apikey]
  - token: Bearer Token（bearer 类型）
  - username/password: Basic Auth 凭证（basic 类型）
  - key_name/key_value: API Key 头名和值（apikey 类型）
- timeout: 超时秒数（默认 30）`
}

// Parameters 返回 JSON Schema 格式的参数定义
func (c *HTTPClient) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "method": {"type": "string", "enum": ["GET", "POST", "PUT", "DELETE", "PATCH"], "description": "HTTP 方法，默认 GET"},
    "url": {"type": "string", "description": "完整请求 URL"},
    "headers": {"type": "object", "description": "请求头 key-value 对象"},
    "body": {"type": "string", "description": "请求体内容"},
    "auth": {
      "type": "object",
      "description": "认证信息",
      "properties": {
        "type": {"type": "string", "enum": ["bearer", "basic", "apikey"], "description": "认证类型"},
        "token": {"type": "string", "description": "Bearer Token"},
        "username": {"type": "string", "description": "Basic Auth 用户名"},
        "password": {"type": "string", "description": "Basic Auth 密码"},
        "key_name": {"type": "string", "description": "API Key 头名称"},
        "key_value": {"type": "string", "description": "API Key 值"}
      },
      "required": ["type"]
    },
    "timeout": {"type": "number", "description": "超时秒数，默认 30"}
  },
  "required": ["url"]
}`)
}

// Execute 执行 HTTP 请求
func (c *HTTPClient) Execute(ctx context.Context, args json.RawMessage) (*tools.Result, error) {
	var params map[string]json.RawMessage
	if err := json.Unmarshal(args, &params); err != nil {
		return tools.NewErrorResult(fmt.Sprintf("invalid arguments: %v", err)), nil
	}

	// 解析 url（必填）
	rawURL := ""
	if raw, ok := params["url"]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &rawURL); err != nil {
			return tools.NewErrorResult(fmt.Sprintf("invalid parameter 'url': %v", err)), nil
		}
	}
	if strings.TrimSpace(rawURL) == "" {
		return tools.NewErrorResult("url is required"), nil
	}

	// 解析 method
	method := "GET"
	if raw, ok := params["method"]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &method); err != nil {
			return tools.NewErrorResult(fmt.Sprintf("invalid parameter 'method': %v", err)), nil
		}
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = "GET"
	}
	switch method {
	case "GET", "POST", "PUT", "DELETE", "PATCH":
	default:
		return tools.NewErrorResult(fmt.Sprintf("unsupported HTTP method: %s", method)), nil
	}

	// 解析 timeout
	timeoutSec := int(c.timeout.Seconds())
	if raw, ok := params["timeout"]; ok && len(raw) > 0 {
		var v float64
		if err := json.Unmarshal(raw, &v); err != nil {
			return tools.NewErrorResult(fmt.Sprintf("invalid parameter 'timeout': %v", err)), nil
		}
		if v > 0 {
			// P1 修复：clamp 到 [1, 3600] 秒，防 int(v) 大值溢出 time.Duration
			timeoutSec = clampTimeoutSec(v)
		}
	}

	// 解析 headers
	var customHeaders map[string]string
	if raw, ok := params["headers"]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &customHeaders); err != nil {
			return tools.NewErrorResult(fmt.Sprintf("invalid parameter 'headers': %v", err)), nil
		}
	}

	// 解析 body
	var bodyBytes []byte
	if raw, ok := params["body"]; ok && len(raw) > 0 {
		// 先尝试作为字符串解析
		var bodyStr string
		if err := json.Unmarshal(raw, &bodyStr); err == nil {
			bodyBytes = []byte(bodyStr)
		} else {
			// 不是字符串，直接使用原始 JSON 作为 body
			bodyBytes = raw
		}
	}
	// 请求体大小上限：超大 body 在发起网络请求前拒绝，避免无界内存放大
	if len(bodyBytes) > httpClientMaxRequestBody {
		return tools.NewErrorResult(fmt.Sprintf("request body too large: %d bytes exceeds limit %d", len(bodyBytes), httpClientMaxRequestBody)), nil
	}

	// 解析 auth
	var auth HTTPAuth
	if raw, ok := params["auth"]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &auth); err != nil {
			return tools.NewErrorResult(fmt.Sprintf("invalid parameter 'auth': %v", err)), nil
		}
	}

	// 创建带超时的上下文
	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	var bodyReader io.Reader
	if len(bodyBytes) > 0 {
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(reqCtx, method, rawURL, bodyReader)
	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("invalid request: %v", err)), nil
	}

	// 设置默认头
	req.Header.Set("User-Agent", httpClientDefaultUserAgent)
	hasContentType := false
	for k, v := range customHeaders {
		req.Header.Set(k, v)
		if strings.EqualFold(k, "Content-Type") {
			hasContentType = true
		}
	}
	// 有 body 且未指定 Content-Type，默认设为 JSON
	if len(bodyBytes) > 0 && !hasContentType {
		req.Header.Set("Content-Type", "application/json")
	}
	// 自动设置 Accept 头
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json")
	}

	// 应用认证信息
	if err := c.applyAuth(req, &auth); err != nil {
		return tools.NewErrorResult(err.Error()), nil
	}

	// 构建 HTTP 客户端
	client := &http.Client{
		Timeout: time.Duration(timeoutSec) * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= c.maxRedirects {
				return fmt.Errorf("stopped after %d redirects", c.maxRedirects)
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("redirect to non-HTTP scheme blocked: %s", req.URL.Scheme)
			}
			return nil
		},
	}
	// 共享 Transport（懒初始化单例）：旧实现每请求新建 Transport 且从不
	// CloseIdleConnections，连接与 goroutine 随请求数线性泄漏；共享后由
	// 连接池统一复用，IdleConnTimeout 自动回收空闲连接。
	client.Transport = c.httpTransport()

	resp, err := client.Do(req)
	if err != nil {
		if reqCtx.Err() == context.DeadlineExceeded ||
			strings.Contains(err.Error(), "timeout") ||
			strings.Contains(err.Error(), "Client.Timeout") {
			return tools.NewErrorResult(fmt.Sprintf("request timed out after %d seconds", timeoutSec)), nil
		}
		return tools.NewErrorResult(fmt.Sprintf("request failed: %v", err)), nil
	}
	defer resp.Body.Close()

	// 读取响应体，限制大小
	limitedReader := io.LimitReader(resp.Body, c.maxBodySize+1)
	bodyData, err := io.ReadAll(limitedReader)
	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("read response body error: %v", err)), fmt.Errorf("read body: %w", err)
	}

	truncated := false
	if int64(len(bodyData)) > c.maxBodySize {
		bodyData = bodyData[:c.maxBodySize]
		truncated = true
	}

	// 智能处理响应体：JSON 自动格式化
	respBody := c.formatResponseBody(bodyData, resp.Header.Get("Content-Type"))

	// 提取响应头
	respHeaders := make(map[string]string)
	for k, v := range resp.Header {
		if len(v) > 0 {
			respHeaders[k] = v[0]
		}
	}

	// 构建响应结果
	result := map[string]any{
		"status_code":  resp.StatusCode,
		"headers":      respHeaders,
		"body":         respBody,
		"content_type": resp.Header.Get("Content-Type"),
		"truncated":    truncated,
	}

	resultJSON, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("failed to marshal response: %v", err)), err
	}

	if resp.StatusCode >= 400 {
		return tools.NewErrorResult(string(resultJSON)), fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return tools.NewResult(string(resultJSON)), nil
}

// applyAuth 将认证信息应用到请求头
func (c *HTTPClient) applyAuth(req *http.Request, auth *HTTPAuth) error {
	if auth == nil || auth.Type == "" {
		return nil
	}

	switch strings.ToLower(auth.Type) {
	case "bearer":
		if auth.Token == "" {
			return fmt.Errorf("bearer auth requires 'token' field")
		}
		req.Header.Set("Authorization", "Bearer "+auth.Token)
	case "basic":
		if auth.Username == "" {
			return fmt.Errorf("basic auth requires 'username' field")
		}
		req.SetBasicAuth(auth.Username, auth.Password)
	case "apikey":
		if auth.KeyName == "" || auth.KeyValue == "" {
			return fmt.Errorf("apikey auth requires 'key_name' and 'key_value' fields")
		}
		req.Header.Set(auth.KeyName, auth.KeyValue)
	default:
		return fmt.Errorf("unsupported auth type: %s (supported: bearer, basic, apikey)", auth.Type)
	}
	return nil
}

// formatResponseBody 智能格式化响应体，JSON 自动美化输出
func (c *HTTPClient) formatResponseBody(body []byte, contentType string) string {
	// 尝试检测 JSON 内容
	contentTypeLower := strings.ToLower(contentType)
	isJSON := strings.Contains(contentTypeLower, "application/json") ||
		strings.Contains(contentTypeLower, "text/json") ||
		strings.Contains(contentTypeLower, "+json")

	if isJSON {
		var parsed any
		if err := json.Unmarshal(body, &parsed); err == nil {
			formatted, err := json.MarshalIndent(parsed, "", "  ")
			if err == nil {
				return string(formatted)
			}
		}
	}

	// 尝试将 body 作为 JSON 解析（即使 Content-Type 不是 JSON）
	if len(body) > 0 {
		var parsed any
		if err := json.Unmarshal(body, &parsed); err == nil {
			formatted, err := json.MarshalIndent(parsed, "", "  ")
			if err == nil {
				return string(formatted)
			}
		}
	}

	return string(body)
}

// httpTransport 返回本实例共享的安全 HTTP Transport（懒初始化单例）。
// SSRF 防护在 DialContext 中实时校验 IP（默认阻止内网/保留地址，见
// validateIPNotInternal，防止 DNS rebinding）；allowPrivate 为 true 时
// 跳过校验（仅限测试环境）。
func (c *HTTPClient) httpTransport() *http.Transport {
	c.transportOnce.Do(func() {
		dialer := &net.Dialer{Timeout: c.timeout}
		c.transport = &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				if c.allowPrivate {
					// 测试放行模式：直接拨号，不做 IP 校验
					return dialer.DialContext(ctx, network, addr)
				}
				host, port, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, fmt.Errorf("invalid address: %w", err)
				}
				ips, err := net.LookupIP(host)
				if err != nil {
					return nil, fmt.Errorf("DNS lookup failed for %s: %w", host, err)
				}
				for _, ip := range ips {
					if err := validateIPNotInternal(ip); err != nil {
						return nil, err
					}
				}
				return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
			},
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
	})
	return c.transport
}
