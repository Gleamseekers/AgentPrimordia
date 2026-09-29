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
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/tools"
)

const (
	apiDefaultTimeout      = 30 * time.Second
	apiDefaultMaxBodySize  = 1 * 1024 * 1024 // 1MB
	apiDefaultUserAgent    = "AgentPrimordia/1.0 (API Tool)"
	apiDefaultMaxRedirects = 10
)

// API 是 REST API 调用tool，支持 GET/POST/PUT/DELETE/PATCH 方法
type API struct {
	timeout      time.Duration
	maxBodySize  int64
	allowPrivate bool
}

// NewAPI 创建新的 API tool实例
func NewAPI() *API {
	return &API{
		timeout:     apiDefaultTimeout,
		maxBodySize: apiDefaultMaxBodySize,
	}
}

// WithTimeout 设置默认超时时间
func (a *API) WithTimeout(d time.Duration) *API {
	a.timeout = d
	return a
}

// WithMaxBodySize 设置响应体最大大小
func (a *API) WithMaxBodySize(size int64) *API {
	a.maxBodySize = size
	return a
}

// WithAllowPrivate 允许访问内网地址（仅用于测试）
func (a *API) WithAllowPrivate(allow bool) *API {
	a.allowPrivate = allow
	return a
}

func (a *API) Name() string { return "api" }

func (a *API) Description() string {
	return `REST API 调用tool，支持 GET/POST/PUT/DELETE/PATCH 方法。
功能：
- 自动 JSON 请求/响应处理
- 自定义请求头
- 超时控制（默认 30 秒）
- 响应体大小限制（默认 1MB）
- SSRF 防护（默认阻止内网地址）

参数：
- method: HTTP 方法 [GET|POST|PUT|DELETE|PATCH]
- url: 完整请求 URL
- headers: 请求头（key-value 对象）
- body: 请求体（字符串或 JSON 对象）
- timeout: 超时秒数（默认 30）`
}

func (a *API) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "method": {"type": "string", "enum": ["GET", "POST", "PUT", "DELETE", "PATCH"], "description": "HTTP 方法，默认 GET"},
    "url": {"type": "string", "description": "完整请求 URL"},
    "headers": {"type": "object", "description": "请求头 key-value 对象"},
    "body": {"type": ["string", "object"], "description": "请求体，支持字符串或 JSON 对象"},
    "timeout": {"type": "number", "description": "超时秒数，默认 30"}
  },
  "required": ["url"]
}`)
}

func (a *API) Execute(ctx context.Context, args json.RawMessage) (*tools.Result, error) {
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
	timeoutSec := int(a.timeout.Seconds())
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

	// 自动设置默认头
	req.Header.Set("User-Agent", apiDefaultUserAgent)
	hasContentType := false
	for k, v := range customHeaders {
		req.Header.Set(k, v)
		if strings.EqualFold(k, "Content-Type") {
			hasContentType = true
		}
	}
	// 如果有 body 且未指定 Content-Type，自动设为 JSON
	if len(bodyBytes) > 0 && !hasContentType {
		req.Header.Set("Content-Type", "application/json")
	}
	// 自动设置 Accept 头
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json")
	}

	// 构建 HTTP 客户端
	client := &http.Client{
		Timeout: time.Duration(timeoutSec) * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= apiDefaultMaxRedirects {
				return fmt.Errorf("stopped after %d redirects", apiDefaultMaxRedirects)
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("redirect to non-HTTP scheme blocked: %s", req.URL.Scheme)
			}
			return nil
		},
	}
	if !a.allowPrivate {
		client.Transport = a.newSecureTransport()
	}

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
	limitedReader := io.LimitReader(resp.Body, a.maxBodySize+1)
	bodyData, err := io.ReadAll(limitedReader)
	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("read response body error: %v", err)), fmt.Errorf("read body: %w", err)
	}

	truncated := false
	if int64(len(bodyData)) > a.maxBodySize {
		bodyData = bodyData[:a.maxBodySize]
		truncated = true
	}

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
		"body":         string(bodyData),
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

// newSecureTransport 创建安全 HTTP Transport，在 TCP 连接时实时校验 IP，防止 SSRF
func (a *API) newSecureTransport() *http.Transport {
	dialer := &net.Dialer{Timeout: a.timeout}
	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
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
	}
}
