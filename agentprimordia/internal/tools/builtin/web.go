package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools"
)

const (
	defaultMaxBodySize  int64 = 10 * 1024 * 1024
	defaultTimeout            = 30 * time.Second
	defaultUserAgent          = "AgentPrimordia/1.0 (Web Tool)"
	defaultMaxRedirects       = 10

	// webMaxRequestBody 请求体大小上限（10MB）：超大 body 在发起网络请求前
	// 直接拒绝，避免无界内存放大与网络滥用。
	webMaxRequestBody = 10 * 1024 * 1024
)

// reservedCIDRs SSRF 防护保留网段：除环回/私有/链路本地（由 net.IP 方法判定）
// 之外的特殊用途地址——CGNAT、IETF 协议赋值、基准测试、文档段、6to4、
// NAT64、保留段等。这些地址同样不应作为 fetch 目标：可被用于绕过内网检测
// 触及云元数据/中间盒，或造成路由异常。
var reservedCIDRs = mustParseCIDRs(
	"0.0.0.0/8",       // "本网络"（RFC 1122）
	"100.64.0.0/10",   // CGNAT 运营商级 NAT（RFC 6598）
	"192.0.0.0/24",    // IETF 协议赋值（RFC 6890）
	"192.0.2.0/24",    // TEST-NET-1 文档段（RFC 5737）
	"192.88.99.0/24",  // 6to4 中继任播（RFC 7526，已废弃）
	"198.18.0.0/15",   // 基准测试（RFC 2544）
	"198.51.100.0/24", // TEST-NET-2 文档段（RFC 5737）
	"203.0.113.0/24",  // TEST-NET-3 文档段（RFC 5737）
	"240.0.0.0/4",     // 保留段（含 255.255.255.255，RFC 1112）
	"2002::/16",       // 6to4（RFC 3056）
	"64:ff9b::/96",    // NAT64 知名前缀（RFC 6052）
	"64:ff9b:1::/48",  // NAT64 本地使用前缀（RFC 6052）
	"2001::/32",       // Teredo（RFC 4380）
	"2001:db8::/32",   // IPv6 文档段（RFC 3849）
	"100::/64",        // discard-only（RFC 6666）
)

// mustParseCIDRs 解析保留网段列表，非法 CIDR 直接 panic（编程错误，启动即暴露）
func mustParseCIDRs(cidrs ...string) []*net.IPNet {
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic(fmt.Sprintf("invalid reserved CIDR %q: %v", c, err))
		}
		nets = append(nets, n)
	}
	return nets
}

type Web struct {
	timeout        time.Duration
	maxBodySize    int64
	maxContentSize int
	allowPrivate   bool

	transportOnce sync.Once
	transport     *http.Transport
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
	// 请求体大小上限：超大 body 在发起网络请求前拒绝，避免无界内存放大
	if len(bodyStr) > webMaxRequestBody {
		return tools.NewErrorResult(fmt.Sprintf("request body too large: %d bytes exceeds limit %d", len(bodyStr), webMaxRequestBody)), nil
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
	// 共享 Transport（懒初始化单例）：旧实现每请求新建 Transport 且从不
	// CloseIdleConnections，连接与 goroutine 随请求数线性泄漏；共享后由
	// 连接池统一复用，IdleConnTimeout 自动回收空闲连接。
	client.Transport = w.httpTransport()

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

// validateIPNotInternal 检查 IP 地址是否为内网/私有/保留地址（SSRF 防护）。
// 覆盖：环回、私有、链路本地、组播、未指定地址，以及保留网段
// （CGNAT 100.64.0.0/10、192.0.0.0/24、198.18.0.0/15、240.0.0.0/4、
// 6to4 2002::/16、NAT64 64:ff9b::/96、文档段等，见 reservedCIDRs）。
// IPv4-mapped IPv6（::ffff:x.x.x.x）解包后按 IPv4 规则复核，防止绕过。
func validateIPNotInternal(ip net.IP) error {
	if ip == nil {
		return fmt.Errorf("target resolves to internal/private address <nil>")
	}
	if err := checkIPBlocked(ip); err != nil {
		return err
	}
	// IPv4-mapped IPv6（如 ::ffff:100.64.0.1）需解包后按 IPv4 保留段复核，
	// 否则 ::ffff:x.x.x.x 形式可绕过 IPv4 网段判定
	if v4 := ip.To4(); v4 != nil {
		if err := checkIPBlocked(v4); err != nil {
			return err
		}
	}
	return nil
}

// checkIPBlocked 判定单个 IP 是否命中内网/私有/保留地址
func checkIPBlocked(ip net.IP) error {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() || ip.IsInterfaceLocalMulticast() {
		return fmt.Errorf("target resolves to internal/private address %s", ip)
	}
	for _, cidr := range reservedCIDRs {
		if cidr.Contains(ip) {
			return fmt.Errorf("target resolves to reserved address %s (%s)", ip, cidr)
		}
	}
	return nil
}

// httpTransport 返回本实例共享的安全 HTTP Transport（懒初始化单例）。
// SSRF 校验在 DialContext 中实时进行（默认阻止内网/保留地址，防 DNS
// rebinding）；allowPrivate 为 true 时跳过校验（仅限测试环境）。
func (w *Web) httpTransport() *http.Transport {
	w.transportOnce.Do(func() {
		dialer := &net.Dialer{Timeout: w.timeout}
		w.transport = &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				if w.allowPrivate {
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
	return w.transport
}
