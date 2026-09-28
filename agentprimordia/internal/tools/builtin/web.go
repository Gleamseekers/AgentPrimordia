package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"agentprimordia/internal/tools"
)

const (
	defaultMaxBodySize  int64 = 10 * 1024 * 1024
	defaultTimeout            = 30 * time.Second
	defaultUserAgent          = "AgentPrimordia/1.0 (Web Tool)"
	defaultMaxRedirects       = 10
)

type Web struct {
	timeout        time.Duration
	maxBodySize    int64
	maxContentSize int
	allowPrivate   bool
}

const defaultMaxContentSize = 50000

func NewWeb() *Web {
	return &Web{
		timeout:        defaultTimeout,
		maxBodySize:    defaultMaxBodySize,
		maxContentSize: defaultMaxContentSize,
	}
}

func (w *Web) WithTimeout(d time.Duration) *Web {
	w.timeout = d
	return w
}

func (w *Web) WithMaxBodySize(size int64) *Web {
	w.maxBodySize = size
	return w
}

// WithAllowPrivate 允许访问内网/私有地址（仅用于测试环境）
func (w *Web) WithAllowPrivate(allow bool) *Web {
	w.allowPrivate = allow
	return w
}

func (w *Web) Name() string { return "web" }

func (w *Web) Description() string {
	return "Web tool for making HTTP requests. Supports GET/POST methods, custom headers, response body truncation, and configurable timeouts."
}

func (w *Web) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "action": {"type": "string", "enum": ["fetch"], "description": "The operation to perform"},
    "url": {"type": "string", "description": "URL to fetch"},
    "method": {"type": "string", "enum": ["GET", "POST", "PUT", "DELETE", "PATCH"], "description": "HTTP method (default: GET)"},
    "headers": {"type": "object", "description": "Custom HTTP headers as key-value pairs"},
    "body": {"type": "string", "description": "Request body for POST/PUT/PATCH requests"},
    "timeout": {"type": "number", "description": "Timeout in seconds (default: 30)"}
  },
  "required": ["action", "url"]
}`)
}

func (w *Web) Execute(ctx context.Context, args json.RawMessage) (*tools.Result, error) {
	var params map[string]json.RawMessage
	if err := json.Unmarshal(args, &params); err != nil {
		return tools.NewErrorResult(fmt.Sprintf("invalid arguments: %v", err)), nil
	}

	action := ""
	if err := unmarshalRaw(params["action"], &action); err != nil {
		return tools.NewErrorResult(fmt.Sprintf("invalid parameter 'action': %v", err)), nil
	}

	if action != "fetch" {
		return tools.NewErrorResult(fmt.Sprintf("unknown action: %s", action)), nil
	}

	rawURL := ""
	if raw, ok := params["url"]; ok && len(raw) > 0 {
		if err := unmarshalRaw(raw, &rawURL); err != nil {
			return tools.NewErrorResult(fmt.Sprintf("invalid parameter 'url': %v", err)), nil
		}
	}
	if strings.TrimSpace(rawURL) == "" {
		return tools.NewErrorResult("url is required"), nil
	}

	// SSRF 防护在 Transport.DialContext 中实现，每次 TCP 连接时实时校验 IP
	// 不再使用预验证，避免 DNS rebinding 攻击

	method := "GET"
	if raw, ok := params["method"]; ok && len(raw) > 0 {
		if err := unmarshalRaw(raw, &method); err != nil {
			return tools.NewErrorResult(fmt.Sprintf("invalid parameter 'method': %v", err)), nil
		}
	}
	if method == "" {
		method = "GET"
	}

	timeoutSec := int(w.timeout.Seconds())
	if raw, ok := params["timeout"]; ok && len(raw) > 0 {
		var v float64
		if err := unmarshalRaw(raw, &v); err != nil {
			return tools.NewErrorResult(fmt.Sprintf("invalid parameter 'timeout': %v", err)), nil
		}
		if v > 0 {
			// P1 修复：clamp 到 [1, 3600] 秒，防 int(v) 大值溢出 time.Duration
			timeoutSec = clampTimeoutSec(v)
		}
	}

	var customHeaders map[string]string
	if raw, ok := params["headers"]; ok && len(raw) > 0 {
		if err := unmarshalRaw(raw, &customHeaders); err != nil {
			return tools.NewErrorResult(fmt.Sprintf("invalid parameter 'headers': %v", err)), nil
		}
	}

	bodyStr := ""
	if raw, ok := params["body"]; ok && len(raw) > 0 {
		if err := unmarshalRaw(raw, &bodyStr); err != nil {
			return tools.NewErrorResult(fmt.Sprintf("invalid parameter 'body': %v", err)), nil
		}
	}

	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	var bodyReader io.Reader
	if bodyStr != "" {
		bodyReader = strings.NewReader(bodyStr)
	}

	req, err := http.NewRequestWithContext(reqCtx, method, rawURL, bodyReader)
	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("invalid request: %v", err)), nil
	}

	req.Header.Set("User-Agent", defaultUserAgent)
	for k, v := range customHeaders {
		req.Header.Set(k, v)
	}

	client := &http.Client{
		Timeout: time.Duration(timeoutSec) * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= defaultMaxRedirects {
				return fmt.Errorf("stopped after %d redirects", defaultMaxRedirects)
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("redirect to non-HTTP scheme blocked: %s", req.URL.Scheme)
			}
			return nil
		},
	}
	if !w.allowPrivate {
		client.Transport = w.newSecureTransport()
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

	respHeaderMap := make(map[string]string)
	for k, v := range resp.Header {
		if len(v) > 0 {
			respHeaderMap[k] = v[0]
		}
	}

	limitedReader := io.LimitReader(resp.Body, w.maxBodySize+1)
	bodyData, err := io.ReadAll(limitedReader)
	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("read response body error: %v", err)), fmt.Errorf("read body: %w", err)
	}

	truncated := false
	if int64(len(bodyData)) > w.maxBodySize {
		bodyData = bodyData[:w.maxBodySize]
		truncated = true
	}

	respBody := string(bodyData)
	if w.maxContentSize > 0 && len(respBody) > w.maxContentSize {
		respBody = respBody[:w.maxContentSize] + "\n... [内容已截断，总长度超过限制]"
		truncated = true
	}

	result := map[string]any{
		"status_code":  resp.StatusCode,
		"headers":      respHeaderMap,
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

// validateIPNotInternal 检查 IP 地址是否为内网/私有地址
func validateIPNotInternal(ip net.IP) error {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() {
		return fmt.Errorf("target resolves to internal/private address %s", ip)
	}
	// 检测 IPv4-mapped IPv6 地址（如 ::ffff:127.0.0.1）
	if v4 := ip.To4(); v4 != nil {
		if v4.IsLoopback() || v4.IsPrivate() || v4.IsLinkLocalUnicast() || v4.IsMulticast() || v4.IsUnspecified() {
			return fmt.Errorf("target resolves to internal/private address %s", ip)
		}
	}
	return nil
}

// newSecureTransport 创建安全 HTTP Transport，在 TCP 连接时实时校验 IP，防止 DNS rebinding
func (w *Web) newSecureTransport() *http.Transport {
	dialer := &net.Dialer{Timeout: w.timeout}
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
