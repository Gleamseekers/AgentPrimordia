package transport

import (
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/bus"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultAckTimeout    = 10 * time.Second
	defaultMaxRetries    = 3
	defaultRetryInterval = 500 * time.Millisecond
	defaultPoolSize      = 8
	connIdleTimeout      = 60 * time.Second
)

// 安全警告：TCPTransport 以明文传输消息，不适合在生产环境中传输敏感数据
// 生产环境请使用 HTTPTransport + TLS 或自行实现加密传输层

type TCPTransportConfig struct {
	AckTimeout    time.Duration
	MaxRetries    int
	RetryInterval time.Duration
	PoolSize      int
}

func DefaultTCPTransportConfig() TCPTransportConfig {
	return TCPTransportConfig{
		AckTimeout:    defaultAckTimeout,
		MaxRetries:    defaultMaxRetries,
		RetryInterval: defaultRetryInterval,
		PoolSize:      defaultPoolSize,
	}
}

type TCPTransport struct {
	config  TCPTransportConfig
	client  *net.Dialer
	inbound chan *bus.BusMessage
	addr    string
	mu      sync.RWMutex
	started bool
	ln      net.Listener
	logger  *slog.Logger

	pool   *connPool
	ackSeq atomic.Uint64
	acks   map[uint64]chan struct{}
	ackMu  sync.RWMutex
}

func NewTCPTransport() *TCPTransport {
	return NewTCPTransportWithConfig(DefaultTCPTransportConfig())
}

func NewTCPTransportWithConfig(cfg TCPTransportConfig) *TCPTransport {
	if cfg.AckTimeout == 0 {
		cfg.AckTimeout = defaultAckTimeout
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = defaultMaxRetries
	}
	if cfg.RetryInterval == 0 {
		cfg.RetryInterval = defaultRetryInterval
	}
	if cfg.PoolSize == 0 {
		cfg.PoolSize = defaultPoolSize
	}

	return &TCPTransport{
		config:  cfg,
		client:  &net.Dialer{Timeout: 30 * time.Second},
		inbound: make(chan *bus.BusMessage, inboundBufSize),
		logger:  slog.Default(),
		acks:    make(map[uint64]chan struct{}),
	}
}

func (t *TCPTransport) Start(addr string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.started {
		return fmt.Errorf("transport already started on %s", t.addr)
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s failed: %w", addr, err)
	}

	t.ln = ln
	t.addr = ln.Addr().String()
	t.started = true
	t.pool = newConnPool(t.config.PoolSize, t.client)

	go t.acceptLoop()

	t.logger.Info("TCP transport 已启动", "addr", t.addr)
	return nil
}

func (t *TCPTransport) Send(ctx context.Context, target string, msg *bus.BusMessage) error {
	t.mu.RLock()
	started := t.started
	t.mu.RUnlock()

	if !started {
		return fmt.Errorf("transport not started")
	}

	return t.sendWithRetry(ctx, target, msg, t.config.MaxRetries)
}

func (t *TCPTransport) SendWithAck(ctx context.Context, target string, msg *bus.BusMessage) error {
	t.mu.RLock()
	started := t.started
	t.mu.RUnlock()

	if !started {
		return fmt.Errorf("transport not started")
	}

	seq := t.ackSeq.Add(1)
	ackCh := make(chan struct{}, 1)

	t.ackMu.Lock()
	t.acks[seq] = ackCh
	t.ackMu.Unlock()

	defer func() {
		t.ackMu.Lock()
		delete(t.acks, seq)
		t.ackMu.Unlock()
	}()

	ackMsg := *msg
	if ackMsg.Metadata == nil {
		ackMsg.Metadata = make(map[string]string)
	}
	ackMsg.Metadata["_ack_seq"] = fmt.Sprintf("%d", seq)
	ackMsg.Metadata["_ack_required"] = "true"

	if err := t.sendWithRetry(ctx, target, &ackMsg, t.config.MaxRetries); err != nil {
		return err
	}

	select {
	case <-ackCh:
		return nil
	case <-time.After(t.config.AckTimeout):
		return fmt.Errorf("ack timeout for message %s (seq=%d)", msg.ID, seq)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// sendWithRetry 发送消息，使用长连接复用连接池中的连接。
// 消息以 newline-delimited JSON 格式发送，支持连接复用。
func (t *TCPTransport) sendWithRetry(ctx context.Context, target string, msg *bus.BusMessage, maxRetries int) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message failed: %w", err)
	}

	// 添加换行符作为消息分隔符
	data = append(data, '\n')

	needAck := msg.Metadata != nil && msg.Metadata["_ack_required"] == "true"

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(t.config.RetryInterval):
			case <-ctx.Done():
				return ctx.Err()
			}
			t.logger.Debug("TCP 重试发送", "target", target, "attempt", attempt, "msg_id", msg.ID)
		}

		conn, err := t.pool.Get(ctx, target)
		if err != nil {
			lastErr = fmt.Errorf("dial %s failed: %w", target, err)
			continue
		}

		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if _, err := conn.Write(data); err != nil {
			conn.Close()
			lastErr = fmt.Errorf("write to %s failed: %w", target, err)
			continue
		}

		if needAck {
			if err := t.readAckResponse(conn, msg.ID, t.config.AckTimeout); err != nil {
				conn.Close()
				lastErr = fmt.Errorf("read ack from %s failed: %w", target, err)
				continue
			}
		}

		// 归还连接到连接池以供复用
		t.pool.Put(target, conn)
		return nil
	}

	return fmt.Errorf("send failed after %d retries: %w", maxRetries, lastErr)
}

// readAckResponse 从连接中读取ACK响应
func (t *TCPTransport) readAckResponse(conn net.Conn, msgID string, timeout time.Duration) error {
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	defer func() { _ = conn.SetReadDeadline(time.Time{}) }()

	var buf [4096]byte
	n, err := conn.Read(buf[:])
	if err != nil {
		return fmt.Errorf("read ack failed: %w", err)
	}

	var ack struct {
		Type string `json:"type"`
		Seq  uint64 `json:"seq"`
	}
	if err := json.Unmarshal(buf[:n], &ack); err != nil {
		return nil
	}

	if ack.Type == "ack" {
		t.handleAck(ack.Seq)
	}

	return nil
}

func (t *TCPTransport) Receive() <-chan *bus.BusMessage {
	return t.inbound
}

func (t *TCPTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.started {
		return nil
	}

	if err := t.ln.Close(); err != nil {
		return fmt.Errorf("close listener failed: %w", err)
	}

	if t.pool != nil {
		t.pool.CloseAll()
	}

	close(t.inbound)
	t.started = false
	t.logger.Info("TCP transport 已关闭", "addr", t.addr)
	return nil
}

func (t *TCPTransport) Addr() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.addr
}

func (t *TCPTransport) PoolStats() (active, idle int) {
	if t.pool == nil {
		return 0, 0
	}
	return t.pool.Stats()
}

func (t *TCPTransport) acceptLoop() {
	for {
		conn, err := t.ln.Accept()
		if err != nil {
			return
		}
		go t.handleConn(conn)
	}
}

// handleConn 处理入站连接，支持长连接复用。
// 使用 newline-delimited JSON 协议，在同一个连接上读取多条消息。
func (t *TCPTransport) handleConn(conn net.Conn) {
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var msg bus.BusMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			t.logger.Warn("TCP 解码消息失败", "error", err)
			continue
		}

		// 如果需要 ACK，立即回复
		if msg.Metadata != nil {
			if ackSeq, ok := msg.Metadata["_ack_seq"]; ok && msg.Metadata["_ack_required"] == "true" {
				t.sendAck(conn, ackSeq)
			}
		}

		// 修复（评估实测发现）：原实现无锁向 inbound 发送，与 Close 的
		// close(t.inbound) 并发即数据竞争/可能 send on closed channel panic。
		// 发送整体在 RLock 内完成（Close 持写锁），并检查 started：
		// 若 Close 已完成，直接丢弃消息返回，杜绝 close 与 send 并发。
		t.mu.RLock()
		if !t.started {
			t.mu.RUnlock()
			return
		}
		select {
		case t.inbound <- &msg:
		default:
			t.logger.Warn("入站通道已满，丢弃消息", "from", msg.From, "id", msg.ID)
		}
		t.mu.RUnlock()
	}
}

func (t *TCPTransport) sendAck(conn net.Conn, seqStr string) {
	ackData := fmt.Sprintf(`{"type":"ack","seq":%s}`, seqStr)
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, _ = conn.Write([]byte(ackData + "\n"))
}

func (t *TCPTransport) handleAck(seq uint64) {
	t.ackMu.RLock()
	ch, ok := t.acks[seq]
	t.ackMu.RUnlock()

	if ok {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// ===== 连接池（支持连接复用）=====

type connPool struct {
	dialer  *net.Dialer
	maxSize int
	mu      sync.Mutex
	conns   map[string][]*poolConn
	closed  bool
}

type poolConn struct {
	net.Conn
	lastUsed time.Time
}

func newConnPool(maxSize int, dialer *net.Dialer) *connPool {
	return &connPool{
		dialer:  dialer,
		maxSize: maxSize,
		conns:   make(map[string][]*poolConn),
	}
}

// Get 从连接池获取一个空闲连接，如果没有则新建。
// 获取的空闲连接会进行健康检查（1ms 超时读取），确保连接仍然可用。
func (p *connPool) Get(ctx context.Context, target string) (net.Conn, error) {
	p.mu.Lock()

	if p.closed {
		p.mu.Unlock()
		return nil, fmt.Errorf("pool closed")
	}

	// 尝试从空闲连接池中获取一个可用的连接
	for len(p.conns[target]) > 0 {
		pc := p.conns[target][len(p.conns[target])-1]
		p.conns[target] = p.conns[target][:len(p.conns[target])-1]

		// 健康检查：设置极短的超时尝试读取，如果连接已关闭会立即返回错误
		_ = pc.SetReadDeadline(time.Now().Add(1 * time.Millisecond))
		oneByte := make([]byte, 1)
		// 重置 deadline
		_ = pc.SetReadDeadline(time.Time{})

		// 检查连接是否仍然活跃（非阻塞式）
		// 如果连接被对端关闭，Read 会返回错误
		_ = pc.SetReadDeadline(time.Now().Add(1 * time.Millisecond))
		n, err := pc.Read(oneByte)
		_ = pc.SetReadDeadline(time.Time{})

		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() && n == 0 {
				// 超时且没有读到数据 = 连接仍然活跃（没有对端关闭的 EOF）
				p.mu.Unlock()
				return pc.Conn, nil
			}
			// 连接已关闭或其他错误，尝试下一个
			pc.Close()
			continue
		}

		// 如果读到了数据，说明连接状态异常（不应该有数据），关闭并尝试下一个
		pc.Close()
		continue
	}

	p.mu.Unlock()

	// 没有空闲连接，新建一个
	conn, err := p.dialer.DialContext(ctx, "tcp", target)
	if err != nil {
		return nil, err
	}

	return conn, nil
}

// Put 将连接归还到连接池以供复用。
// 如果连接池已满或已关闭，直接关闭连接。
func (p *connPool) Put(target string, conn net.Conn) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		conn.Close()
		return
	}

	// 检查连接池是否已满
	if len(p.conns[target]) >= p.maxSize {
		conn.Close()
		return
	}

	pc := &poolConn{Conn: conn, lastUsed: time.Now()}
	p.conns[target] = append(p.conns[target], pc)

	// 清理过期的空闲连接
	p.evictIdleLocked(target)
}

// evictIdleLocked 清理超过 connIdleTimeout 的空闲连接（调用者需持有锁）
func (p *connPool) evictIdleLocked(target string) {
	now := time.Now()
	var alive []*poolConn
	for _, pc := range p.conns[target] {
		if now.Sub(pc.lastUsed) > connIdleTimeout {
			pc.Close()
		} else {
			alive = append(alive, pc)
		}
	}
	p.conns[target] = alive
}

func (p *connPool) Invalidate(target string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, pc := range p.conns[target] {
		pc.Close()
	}
	delete(p.conns, target)
}

func (p *connPool) Stats() (active, idle int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, conns := range p.conns {
		idle += len(conns)
	}
	return 0, idle
}

func (p *connPool) CloseAll() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.closed = true
	for target, conns := range p.conns {
		for _, pc := range conns {
			pc.Close()
		}
		delete(p.conns, target)
	}
}
