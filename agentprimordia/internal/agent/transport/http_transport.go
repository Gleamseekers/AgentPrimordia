package transport

import (
	"github.com/Gleamseekers/AgentPrimordia/internal/agent/bus"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

// HTTPTransport 基于 HTTP 的跨进程 Agent 传输层实现
// 安全警告：未配置 TLS 时以明文传输消息，不适合在生产环境中传输敏感数据
// 生产环境请使用 WithTLS() 配置 TLS 加密
type HTTPTransport struct {
	client    *http.Client
	server    *http.Server
	inbound   chan *bus.BusMessage
	addr      string
	mu        sync.RWMutex
	started   bool
	logger    *slog.Logger
	tlsConfig *tls.Config
	pool      *ConnPool
}

const (
	messageEndpoint    = "/api/message"
	inboundBufSize     = 64
	serverReadTimeout  = 5 * time.Second
	serverWriteTimeout = 10 * time.Second
	serverIdleTimeout  = 60 * time.Second
	shutdownTimeout    = 5 * time.Second
)

// NewHTTPTransport 创建 HTTP 传输层实例
func NewHTTPTransport() *HTTPTransport {
	return &HTTPTransport{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		inbound: make(chan *bus.BusMessage, inboundBufSize),
		logger:  slog.Default(),
	}
}

// WithConnPool 配置连接池，启用 Transport 级别连接复用
func (t *HTTPTransport) WithConnPool(pool *ConnPool) *HTTPTransport {
	t.pool = pool
	t.client.Transport = pool.Transport()
	return t
}

// WithTLS 配置 TLS 加密，启用后传输层使用 HTTPS
func (t *HTTPTransport) WithTLS(cfg *tls.Config) *HTTPTransport {
	t.tlsConfig = cfg
	transport := &http.Transport{
		TLSClientConfig: cfg,
	}
	if t.pool != nil {
		poolTransport := t.pool.Transport()
		poolTransport.TLSClientConfig = cfg
		t.client.Transport = poolTransport
	} else {
		t.client.Transport = transport
	}
	return t
}

// Start 在指定地址启动 HTTP 服务
func (t *HTTPTransport) Start(addr string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.started {
		return fmt.Errorf("transport already started on %s", t.addr)
	}

	mux := http.NewServeMux()
	mux.HandleFunc(messageEndpoint, t.handleMessage)

	t.server = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  serverReadTimeout,
		WriteTimeout: serverWriteTimeout,
		IdleTimeout:  serverIdleTimeout,
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s failed: %w", addr, err)
	}

	// 配置 TLS 时使用 TLS 监听
	if t.tlsConfig != nil {
		ln = tls.NewListener(ln, t.tlsConfig)
	}

	t.addr = ln.Addr().String()
	t.started = true

	go func() {
		if err := t.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			t.logger.Error("HTTP transport server error", "error", err)
		}
	}()

	t.logger.Info("HTTP transport 已启动", "addr", t.addr)
	return nil
}

// Send 向目标地址发送消息
func (t *HTTPTransport) Send(ctx context.Context, target string, msg *bus.BusMessage) error {
	t.mu.RLock()
	started := t.started
	t.mu.RUnlock()

	if !started {
		return fmt.Errorf("transport not started")
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message failed: %w", err)
	}

	scheme := "http"
	if t.tlsConfig != nil {
		scheme = "https"
	}
	url := fmt.Sprintf("%s://%s%s", scheme, target, messageEndpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("send message to %s failed: %w", target, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("send message to %s returned status %d: %s", target, resp.StatusCode, string(body))
	}

	return nil
}

// Receive 返回入站消息通道
func (t *HTTPTransport) Receive() <-chan *bus.BusMessage {
	return t.inbound
}

// Close 优雅关闭传输服务
func (t *HTTPTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.started {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	err := t.server.Shutdown(ctx)
	if err != nil {
		return fmt.Errorf("shutdown server failed: %w", err)
	}

	close(t.inbound)
	t.started = false
	t.logger.Info("HTTP transport 已关闭", "addr", t.addr)
	return nil
}

// Addr 返回实际监听地址（启动后可用）
func (t *HTTPTransport) Addr() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.addr
}

// handleMessage 处理入站 HTTP 消息请求
func (t *HTTPTransport) handleMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body failed", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var msg bus.BusMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		http.Error(w, "invalid message format", http.StatusBadRequest)
		return
	}

	select {
	case t.inbound <- &msg:
		w.WriteHeader(http.StatusOK)
	default:
		t.logger.Warn("入站通道已满，丢弃消息", "from", msg.From, "id", msg.ID)
		http.Error(w, "inbound channel full", http.StatusServiceUnavailable)
	}
}
