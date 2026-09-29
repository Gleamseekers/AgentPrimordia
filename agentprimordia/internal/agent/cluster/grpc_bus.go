// grpc_bus.go — gRPC 跨节点消息总线（V3.1 Phase 1）
//
// 为 RemoteNode 提供 gRPC 传输模式，替代纯 HTTP 转发。
// 相比 HTTP，gRPC 提供：
//   - 双向流式通信（适合高频消息交换）
//   - 连接复用（减少 TCP 握手开销）
//   - 内置超时/取消传播
//
// 设计：
//   - 使用 gRPC 通用 codec（JSON 编码），避免在 a2a/ 外引入 protobuf 依赖
//   - RemoteNode 支持 HTTP + gRPC 双模式，通过配置切换
//   - 复用 internal/agent/a2a/ 的 gRPC 基础设施模式（拦截器、认证）
package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/bus"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// ===== gRPC 消息定义（JSON 编码，避免 protobuf 依赖） =====

// ClusterMessage 集群消息（gRPC 传输）
type ClusterMessage struct {
	ID        string            `json:"id"`
	From      string            `json:"from"`
	To        string            `json:"to"`
	Type      string            `json:"type"`
	Content   string            `json:"content"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Timestamp int64             `json:"timestamp"` // Unix 毫秒
}

// ClusterReply 集群消息回复
type ClusterReply struct {
	ID        string `json:"id"`
	InReplyTo string `json:"in_reply_to"`
	From      string `json:"from"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error"`
	Timestamp int64  `json:"timestamp"`
}

// ===== gRPC 服务定义 =====

const (
	// clusterServiceName gRPC 服务名
	clusterServiceName = "agentprimordia.cluster.v1.ClusterBus"
	// methodSendMessage 单播方法
	methodSendMessage = "/agentprimordia.cluster.v1.ClusterBus/SendMessage"
	// methodHealthCheck 健康检查方法
	methodHealthCheck = "/agentprimordia.cluster.v1.ClusterBus/HealthCheck"
)

// ===== gRPC 远程节点 =====

// GRPCRemoteNode gRPC 远程节点连接
//
// 与 HTTP RemoteNode 对等，提供基于 gRPC 的跨节点消息传输。
type GRPCRemoteNode struct {
	ID      string
	Address string // 例如 "10.0.0.2:9090"（不含 scheme）
	conn    *grpc.ClientConn
	logger  *slog.Logger
	mu      sync.Mutex
	closed  bool
}

// GRPCRemoteNodeConfig gRPC 远程节点配置
type GRPCRemoteNodeConfig struct {
	// ID 节点 ID
	ID string
	// Address gRPC 地址（host:port）
	Address string
	// DialTimeout 连接超时（默认 5s）
	DialTimeout time.Duration
	// Logger 日志器
	Logger *slog.Logger
}

// NewGRPCRemoteNode 创建 gRPC 远程节点
func NewGRPCRemoteNode(cfg GRPCRemoteNodeConfig) (*GRPCRemoteNode, error) {
	if cfg.Address == "" {
		return nil, fmt.Errorf("grpc_bus: address is required")
	}
	if cfg.DialTimeout == 0 {
		cfg.DialTimeout = 5 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	// 建立 gRPC 连接
	ctx, cancel := context.WithTimeout(context.Background(), cfg.DialTimeout)
	defer cancel()

	//nolint:staticcheck // API 弃用但为兼容保留，NewClient 为惰性连接语义不同
	conn, err := grpc.DialContext(ctx, cfg.Address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		//nolint:staticcheck // API 弃用但为兼容保留，NewClient 为惰性连接语义不同
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("grpc_bus: dial %s: %w", cfg.Address, err)
	}

	cfg.Logger.Info("gRPC 远程节点已连接", "id", cfg.ID, "address", cfg.Address)

	return &GRPCRemoteNode{
		ID:      cfg.ID,
		Address: cfg.Address,
		conn:    conn,
		logger:  cfg.Logger,
	}, nil
}

// SendMessage 通过 gRPC 发送消息到远程节点
func (n *GRPCRemoteNode) SendMessage(ctx context.Context, msg *bus.BusMessage) (*bus.BusMessage, error) {
	n.mu.Lock()
	if n.closed {
		n.mu.Unlock()
		return nil, fmt.Errorf("grpc_bus: node %s is closed", n.ID)
	}
	n.mu.Unlock()

	// 转换为集群消息
	clusterMsg := busMessageToCluster(msg)

	// 编码请求
	reqBytes, err := json.Marshal(clusterMsg)
	if err != nil {
		return nil, fmt.Errorf("grpc_bus: marshal request: %w", err)
	}

	// 通过 gRPC 通用调用发送
	// 注意：必须保留对响应 rawMessage 的引用，因为 gRPC 通过
	// Unmarshal 修改的是 rawMessage.data 字段，而非外部变量。
	resp := &rawMessage{}
	err = n.conn.Invoke(ctx, methodSendMessage, &rawMessage{data: reqBytes}, resp)
	if err != nil {
		return nil, fmt.Errorf("grpc_bus: send to %s: %w", n.ID, err)
	}

	// 解码回复
	var reply ClusterReply
	if err := json.Unmarshal(resp.data, &reply); err != nil {
		return nil, fmt.Errorf("grpc_bus: unmarshal reply from %s: %w", n.ID, err)
	}

	if reply.IsError {
		return nil, fmt.Errorf("grpc_bus: remote error from %s: %s", n.ID, reply.Content)
	}

	return clusterReplyToBus(&reply), nil
}

// HealthCheck 检查远程节点健康状态
func (n *GRPCRemoteNode) HealthCheck(ctx context.Context) error {
	n.mu.Lock()
	if n.closed {
		n.mu.Unlock()
		return fmt.Errorf("grpc_bus: node %s is closed", n.ID)
	}
	n.mu.Unlock()

	req := &ClusterMessage{
		ID:   fmt.Sprintf("health_%d", time.Now().UnixNano()),
		From: "health_checker",
		Type: "health_check",
	}
	reqBytes, _ := json.Marshal(req)

	resp := &rawMessage{}
	err := n.conn.Invoke(ctx, methodHealthCheck, &rawMessage{data: reqBytes}, resp)
	if err != nil {
		return fmt.Errorf("grpc_bus: health check %s: %w", n.ID, err)
	}
	return nil
}

// Close 关闭 gRPC 连接
func (n *GRPCRemoteNode) Close() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.closed {
		return nil
	}
	n.closed = true

	n.logger.Info("gRPC 远程节点已断开", "id", n.ID)
	return n.conn.Close()
}

// ===== gRPC 集群服务器 =====

// GRPCClusterServer gRPC 集群消息服务器
//
// 接收来自其他节点的 gRPC 消息，转发到本地 MessageBus。
type GRPCClusterServer struct {
	nodeID   string
	bus      bus.MessageBus
	logger   *slog.Logger
	server   *grpc.Server
	listener net.Listener
	mu       sync.Mutex
	running  bool
}

// GRPCClusterServerConfig gRPC 集群服务器配置
type GRPCClusterServerConfig struct {
	// NodeID 本节点 ID
	NodeID string
	// Bus 本地消息总线
	Bus bus.MessageBus
	// ListenAddr 监听地址（默认 ":9090"）
	ListenAddr string
	// Logger 日志器
	Logger *slog.Logger
}

// NewGRPCClusterServer 创建 gRPC 集群服务器
func NewGRPCClusterServer(cfg GRPCClusterServerConfig) *GRPCClusterServer {
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":9090"
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	return &GRPCClusterServer{
		nodeID: cfg.NodeID,
		bus:    cfg.Bus,
		logger: cfg.Logger,
	}
}

// Start 启动 gRPC 服务器
func (s *GRPCClusterServer) Start(addr string) error {
	if addr == "" {
		addr = ":9090"
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("grpc_bus: server already running")
	}
	s.mu.Unlock()

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("grpc_bus: listen: %w", err)
	}

	s.server = grpc.NewServer()
	s.listener = ln

	// 注册通用服务处理器
	s.server.RegisterService(&grpc.ServiceDesc{
		ServiceName: clusterServiceName,
		HandlerType: (*any)(nil),
		Methods: []grpc.MethodDesc{
			{
				MethodName: "SendMessage",
				Handler:    s.handleSendMessage,
			},
			{
				MethodName: "Broadcast",
				Handler:    s.handleBroadcast,
			},
			{
				MethodName: "HealthCheck",
				Handler:    s.handleHealthCheck,
			},
		},
		Streams: []grpc.StreamDesc{},
	}, s)

	s.mu.Lock()
	s.running = true
	s.mu.Unlock()

	s.logger.Info("gRPC 集群服务器已启动", "node_id", s.nodeID, "addr", addr)

	go func() {
		if err := s.server.Serve(ln); err != nil {
			s.logger.Error("gRPC 服务器异常", "error", err)
		}
	}()

	return nil
}

// Stop 停止 gRPC 服务器
func (s *GRPCClusterServer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}
	s.running = false

	if s.server != nil {
		s.server.GracefulStop()
	}
	s.logger.Info("gRPC 集群服务器已停止", "node_id", s.nodeID)
}

// ===== gRPC 处理函数 =====

func (s *GRPCClusterServer) handleSendMessage(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	req := &rawMessage{}
	if err := dec(req); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "decode request: %v", err)
	}

	var msg ClusterMessage
	if err := json.Unmarshal(req.data, &msg); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal message: %v", err)
	}

	// 转换为 BusMessage 并发送到本地总线
	busMsg := clusterToBusMessage(&msg)
	reply, err := s.bus.Send(ctx, busMsg)
	if err != nil {
		replyMsg := &ClusterReply{
			ID:        fmt.Sprintf("reply_%d", time.Now().UnixNano()),
			InReplyTo: msg.ID,
			From:      s.nodeID,
			Content:   err.Error(),
			IsError:   true,
			Timestamp: time.Now().UnixMilli(),
		}
		data, _ := json.Marshal(replyMsg)
		return &rawMessage{data: data}, nil
	}

	// 构造回复
	replyMsg := busReplyToCluster(reply)
	data, _ := json.Marshal(replyMsg)
	return &rawMessage{data: data}, nil
}

func (s *GRPCClusterServer) handleBroadcast(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	req := &rawMessage{}
	if err := dec(req); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "decode request: %v", err)
	}

	var msg ClusterMessage
	if err := json.Unmarshal(req.data, &msg); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal message: %v", err)
	}

	busMsg := clusterToBusMessage(&msg)
	s.bus.Broadcast(ctx, busMsg)

	reply := &ClusterReply{
		ID:        fmt.Sprintf("bcast_reply_%d", time.Now().UnixNano()),
		InReplyTo: msg.ID,
		From:      s.nodeID,
		Content:   "broadcast received",
		Timestamp: time.Now().UnixMilli(),
	}
	data, _ := json.Marshal(reply)
	return &rawMessage{data: data}, nil
}

func (s *GRPCClusterServer) handleHealthCheck(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	reply := &ClusterReply{
		ID:        fmt.Sprintf("health_reply_%d", time.Now().UnixNano()),
		From:      s.nodeID,
		Content:   "healthy",
		Timestamp: time.Now().UnixMilli(),
	}
	data, _ := json.Marshal(reply)
	return &rawMessage{data: data}, nil
}

// ===== 消息转换 =====

func busMessageToCluster(msg *bus.BusMessage) *ClusterMessage {
	return &ClusterMessage{
		ID:        msg.ID,
		From:      msg.From,
		To:        msg.To,
		Type:      string(msg.Type),
		Content:   msg.Content,
		Metadata:  msg.Metadata,
		Timestamp: msg.Timestamp.UnixMilli(),
	}
}

func clusterToBusMessage(msg *ClusterMessage) *bus.BusMessage {
	return &bus.BusMessage{
		ID:        msg.ID,
		From:      msg.From,
		To:        msg.To,
		Type:      bus.BusMessageType(msg.Type),
		Content:   msg.Content,
		Metadata:  msg.Metadata,
		Timestamp: time.UnixMilli(msg.Timestamp),
	}
}

func clusterReplyToBus(reply *ClusterReply) *bus.BusMessage {
	return &bus.BusMessage{
		ID:        reply.ID,
		From:      reply.From,
		Type:      bus.BusMsgResponse,
		Content:   reply.Content,
		Timestamp: time.UnixMilli(reply.Timestamp),
	}
}

func busReplyToCluster(msg *bus.BusMessage) *ClusterReply {
	return &ClusterReply{
		ID:        msg.ID,
		InReplyTo: msg.ID,
		From:      msg.From,
		Content:   msg.Content,
		Timestamp: msg.Timestamp.UnixMilli(),
	}
}

// ===== rawMessage gRPC 通用编解码 =====

// rawMessage 原始字节消息（用于 gRPC 通用调用）
type rawMessage struct {
	data []byte
}

func (m *rawMessage) Marshal() ([]byte, error) {
	return m.data, nil
}

func (m *rawMessage) Unmarshal(data []byte) error {
	m.data = data
	return nil
}

func (m *rawMessage) ProtoMessage() {}

func (m *rawMessage) Reset() { m.data = nil }

func (m *rawMessage) String() string { return string(m.data) }
