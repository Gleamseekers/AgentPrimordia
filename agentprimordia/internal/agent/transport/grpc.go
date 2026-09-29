package transport

import (
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/bus"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// ===== #9 gRPC 单播传输层（真实实现） =====
//
// 历史：早期为 EXPERIMENTAL 占位（Send 不投递消息）。v6.x 补全真实单播实现：
//   - 服务名 agentprimordia.transport.v1.TransportBus / SendMessage
//   - JSON 通用 codec（rawMessage），与 cluster/grpc_bus.go 同模式，无需 protobuf 生成代码
//   - 客户端懒建立连接池（peers），复用 gRPC 连接
//   - 服务端接收消息推入 inbox，推送与 Close 互斥（写锁保护 close(inbox)）

// grpcServiceName gRPC 服务名
const grpcServiceName = "agentprimordia.transport.v1.TransportBus"

// methodSendMessage 单播发送方法
const methodSendMessage = "/agentprimordia.transport.v1.TransportBus/SendMessage"

// grpcRawMessage gRPC 通用消息 codec（JSON 编码）。
// 实现 proto.Message 接口（Marshal/Unmarshal/ProtoMessage/Reset），
// 使 gRPC 可直接传输任意 JSON 载荷而无需生成代码。
type grpcRawMessage struct {
	data []byte
}

func (m *grpcRawMessage) Marshal() ([]byte, error) { return m.data, nil }
func (m *grpcRawMessage) Unmarshal(data []byte) error {
	m.data = data
	return nil
}
func (m *grpcRawMessage) ProtoMessage() {}
func (m *grpcRawMessage) Reset()        { m.data = nil }

// String 实现旧式 proto.Message v1 接口（Reset/String/ProtoMessage），
// 新版 protobuf 经 protoadapt 自动适配为 MessageV2（与 cluster rawMessage 一致）
func (m *grpcRawMessage) String() string { return string(m.data) }

// grpcAck 服务端接收确认
type grpcAck struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// GrpcTransport 基于 gRPC 的跨进程 Agent 通信传输层。
type GrpcTransport struct {
	mu        sync.RWMutex
	listener  net.Listener
	server    *grpc.Server
	inbox     chan *bus.BusMessage
	peers     map[string]*grpc.ClientConn
	timeout   time.Duration
	localAddr string
	started   bool
	logger    *slog.Logger
}

// grpcMessage JSON 编码的消息封装。
type grpcMessage struct {
	ID        string            `json:"id"`
	From      string            `json:"from"`
	To        string            `json:"to"`
	Type      string            `json:"type"`
	Content   string            `json:"content"`
	Timestamp int64             `json:"timestamp"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

func toGrpcMsg(msg *bus.BusMessage) *grpcMessage {
	return &grpcMessage{
		ID:        msg.ID,
		From:      msg.From,
		To:        msg.To,
		Type:      string(msg.Type),
		Content:   msg.Content,
		Timestamp: msg.Timestamp.UnixNano(),
		Metadata:  msg.Metadata,
	}
}

func fromGrpcMsg(gm *grpcMessage) *bus.BusMessage {
	return &bus.BusMessage{
		ID:        gm.ID,
		From:      gm.From,
		To:        gm.To,
		Type:      bus.BusMessageType(gm.Type),
		Content:   gm.Content,
		Metadata:  gm.Metadata,
		Timestamp: time.Unix(0, gm.Timestamp),
	}
}

// NewGrpcTransport 创建 gRPC 传输层实例。
func NewGrpcTransport() *GrpcTransport {
	return &GrpcTransport{
		inbox:   make(chan *bus.BusMessage, 256),
		peers:   make(map[string]*grpc.ClientConn),
		timeout: 30 * time.Second,
		logger:  slog.Default(),
	}
}

// WithGrpcLogger 注入结构化日志器。
func (t *GrpcTransport) WithGrpcLogger(l *slog.Logger) *GrpcTransport {
	t.logger = l
	return t
}

// Addr 返回实际监听地址（Start 传入 ":0" 时可获取系统分配端口）。
func (t *GrpcTransport) Addr() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.listener != nil {
		return t.listener.Addr().String()
	}
	return t.localAddr
}

// Send 向目标地址发送消息（真实 gRPC 单播）。target 为对端地址（host:port）。
func (t *GrpcTransport) Send(ctx context.Context, target string, msg *bus.BusMessage) error {
	if msg == nil {
		return fmt.Errorf("grpc transport: nil message")
	}
	conn, err := t.peerConn(target)
	if err != nil {
		return err
	}

	gm := toGrpcMsg(msg)
	data, err := json.Marshal(gm)
	if err != nil {
		return fmt.Errorf("grpc transport: marshal message: %w", err)
	}

	resp := &grpcRawMessage{}
	callCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	if err := conn.Invoke(callCtx, methodSendMessage, &grpcRawMessage{data: data}, resp); err != nil {
		return fmt.Errorf("grpc transport: send to %s: %w", target, err)
	}

	var ack grpcAck
	if err := json.Unmarshal(resp.data, &ack); err != nil {
		return fmt.Errorf("grpc transport: decode ack from %s: %w", target, err)
	}
	if !ack.OK {
		return fmt.Errorf("grpc transport: remote error from %s: %s", target, ack.Error)
	}
	return nil
}

// peerConn 获取（或懒建立）到目标的 gRPC 连接。
func (t *GrpcTransport) peerConn(target string) (*grpc.ClientConn, error) {
	t.mu.RLock()
	conn, ok := t.peers[target]
	t.mu.RUnlock()
	if ok {
		return conn, nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	// double-check：并发首次连接时避免重复建立
	if conn, ok := t.peers[target]; ok {
		return conn, nil
	}
	// 发送方无需 Start（Start 仅启动服务端角色），直接懒建立连接

	dialCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	//nolint:staticcheck // DialContext 保留：惰性连接语义（NewClient 行为不同）
	conn, err := grpc.DialContext(dialCtx, target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("grpc transport: dial %s: %w", target, err)
	}
	t.peers[target] = conn
	return conn, nil
}

// Receive 入站消息通道。
func (t *GrpcTransport) Receive() <-chan *bus.BusMessage {
	return t.inbox
}

// Start 启动 gRPC 服务。
func (t *GrpcTransport) Start(addr string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.started {
		return fmt.Errorf("grpc transport: already started")
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("grpc transport: listen %s: %w", addr, err)
	}
	t.listener = lis
	t.localAddr = addr

	srv := grpc.NewServer()
	srv.RegisterService(&grpc.ServiceDesc{
		ServiceName: grpcServiceName,
		HandlerType: (*any)(nil),
		Methods: []grpc.MethodDesc{
			{MethodName: "SendMessage", Handler: t.handleSendMessage},
		},
		Streams: []grpc.StreamDesc{},
	}, t)
	t.server = srv
	t.started = true

	go func() {
		if err := srv.Serve(lis); err != nil {
			t.logger.Warn("gRPC 传输服务停止", "addr", addr, "error", err)
		}
	}()

	t.logger.Info("gRPC 传输服务已启动", "addr", addr)
	return nil
}

// handleSendMessage 服务端单播处理器：解码消息推入 inbox。
// 推送在 RLock 内完成并与 Close 的写锁互斥，避免 close(inbox) 与发送并发。
func (t *GrpcTransport) handleSendMessage(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	req := &grpcRawMessage{}
	if err := dec(req); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "decode request: %v", err)
	}

	var gm grpcMessage
	if err := json.Unmarshal(req.data, &gm); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal message: %v", err)
	}
	msg := fromGrpcMsg(&gm)

	t.mu.RLock()
	if !t.started {
		t.mu.RUnlock()
		data, _ := json.Marshal(&grpcAck{OK: false, Error: "grpc transport closed"})
		return &grpcRawMessage{data: data}, nil
	}
	select {
	case t.inbox <- msg:
	default:
		t.logger.Warn("gRPC 入站通道已满，丢弃消息", "from", msg.From, "id", msg.ID)
	}
	t.mu.RUnlock()

	data, _ := json.Marshal(&grpcAck{OK: true})
	return &grpcRawMessage{data: data}, nil
}

// Close 关闭传输服务。
func (t *GrpcTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.started {
		return nil
	}

	if t.server != nil {
		t.server.Stop()
	}
	if t.listener != nil {
		_ = t.listener.Close()
	}
	for target, conn := range t.peers {
		_ = conn.Close()
		delete(t.peers, target)
	}
	close(t.inbox)
	t.started = false
	t.logger.Info("gRPC 传输服务已关闭", "addr", t.localAddr)
	return nil
}

var _ Transport = (*GrpcTransport)(nil)
