package a2a

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	a2av1 "github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/a2a/proto/a2a/v1"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/resilience"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// GRPCClientOption gRPC client 配置选项。
type GRPCClientOption func(*A2AGRPCClient)

// WithGRPCClientLogger 设置 gRPC client 日志器。
func WithGRPCClientLogger(logger *slog.Logger) GRPCClientOption {
	return func(c *A2AGRPCClient) { c.logger = logger }
}

// WithGRPCClientAPIKey 设置 API Key。
func WithGRPCClientAPIKey(key string) GRPCClientOption {
	return func(c *A2AGRPCClient) { c.apiKey = key }
}

// WithGRPCClientBearerToken 设置 Bearer Token。
func WithGRPCClientBearerToken(token string) GRPCClientOption {
	return func(c *A2AGRPCClient) { c.bearerToken = token }
}

// WithGRPCClientTLS 启用 gRPC 客户端 TLS/mTLS。
func WithGRPCClientTLS(config TLSConfig) GRPCClientOption {
	return func(c *A2AGRPCClient) { c.tlsConfig = &config }
}

// WithGRPCClientCredentials 直接设置 gRPC 客户端 TransportCredentials。
func WithGRPCClientCredentials(creds credentials.TransportCredentials) GRPCClientOption {
	return func(c *A2AGRPCClient) { c.tlsCreds = creds }
}

// A2AGRPCClient gRPC 客户端。
type A2AGRPCClient struct {
	client         a2av1.A2AServiceClient
	conn           *grpc.ClientConn
	apiKey         string
	bearerToken    string
	logger         *slog.Logger
	tlsConfig      *TLSConfig
	tlsCreds       credentials.TransportCredentials
	circuitBreaker *resilience.CircuitBreaker
}

// NewA2AGRPCClient 创建 gRPC 客户端。
func NewA2AGRPCClient(target string, opts ...GRPCClientOption) (*A2AGRPCClient, error) {
	c := &A2AGRPCClient{logger: slog.Default()}
	for _, opt := range opts {
		opt(c)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 构建 dial 选项
	dialOpts := []grpc.DialOption{}

	// TLS 配置
	switch {
	case c.tlsCreds != nil:
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(c.tlsCreds))
	case c.tlsConfig != nil:
		creds, err := ClientTLSCredentials(*c.tlsConfig)
		if err != nil {
			return nil, fmt.Errorf("mTLS configuration failed: %w", err)
		}
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(creds))
	default:
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	// 断路器拦截器
	if c.circuitBreaker != nil {
		interceptor := NewCircuitBreakerInterceptorWithCB(c.circuitBreaker, nil)
		dialOpts = append(dialOpts,
			grpc.WithUnaryInterceptor(interceptor.UnaryClientInterceptor()),
		)
	}

	//nolint:staticcheck // API 弃用但为兼容保留，NewClient 为惰性连接语义不同
	conn, err := grpc.DialContext(ctx, target, append(dialOpts, grpc.WithBlock())...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to gRPC server: %w", err)
	}

	c.conn = conn
	c.client = a2av1.NewA2AServiceClient(conn)
	return c, nil
}

// NewA2AGRPCClientWithConn 使用已有连接创建客户端（测试常用）。
func NewA2AGRPCClientWithConn(conn *grpc.ClientConn, opts ...GRPCClientOption) *A2AGRPCClient {
	c := &A2AGRPCClient{
		conn:   conn,
		client: a2av1.NewA2AServiceClient(conn),
		logger: slog.Default(),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// ctx 构造 outgoing context：合并认证头 + Trace Context（W3C traceparent）
//
// 调用顺序：
//  1. 注入认证（API Key / Bearer Token）
//  2. 注入 Trace Context（若 ctx 中存在 TraceContext）
//  3. 转换为 gRPC outgoing context（metadata.NewOutgoingContext）
//
// 返回的 ctx 可直接传给 gRPC client 方法。
func (c *A2AGRPCClient) ctx(ctx context.Context) context.Context {
	md := metadata.MD{}
	if c.apiKey != "" {
		md.Set("x-api-key", c.apiKey)
	}
	if c.bearerToken != "" {
		md.Set("authorization", "Bearer "+c.bearerToken)
	}

	// 将 metadata 包装到我们的 outgoing metadata 中，便于 trace 注入层处理
	wrapped := Metadata(md)
	ctx = WithOutgoingMetadata(ctx, wrapped)

	// 注入 W3C trace context
	ctx = InjectTraceParent(ctx)

	// 转为 gRPC outgoing context
	return ToGRPCOutgoingContext(ctx)
}

// FetchAgentCard 获取 AgentCard。
func (c *A2AGRPCClient) FetchAgentCard(ctx context.Context) (*AgentCard, error) {
	resp, err := c.client.GetAgentCard(c.ctx(ctx), &a2av1.GetAgentCardRequest{})
	if err != nil {
		return nil, fmt.Errorf("failed to get AgentCard: %w", err)
	}
	return fromProtoAgentCard(resp), nil
}

// CreateTask 创建任务。
func (c *A2AGRPCClient) CreateTask(ctx context.Context, message *A2AMessage, taskID string) (*Task, error) {
	resp, err := c.client.CreateTask(c.ctx(ctx), &a2av1.CreateTaskRequest{
		Message: toProtoMessage(message),
		TaskId:  taskID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create task: %w", err)
	}
	return fromProtoTask(resp), nil
}

// GetTask 获取任务。
func (c *A2AGRPCClient) GetTask(ctx context.Context, taskID string) (*Task, error) {
	resp, err := c.client.GetTask(c.ctx(ctx), &a2av1.GetTaskRequest{Id: taskID})
	if err != nil {
		return nil, fmt.Errorf("failed to get task: %w", err)
	}
	return fromProtoTask(resp), nil
}

// CancelTask 取消任务。
func (c *A2AGRPCClient) CancelTask(ctx context.Context, taskID string) (*Task, error) {
	resp, err := c.client.CancelTask(c.ctx(ctx), &a2av1.CancelTaskRequest{Id: taskID})
	if err != nil {
		return nil, fmt.Errorf("failed to cancel task: %w", err)
	}
	return fromProtoTask(resp), nil
}

// StreamEvents 订阅任务事件流。
func (c *A2AGRPCClient) StreamEvents(ctx context.Context, taskID string) (<-chan *TaskEvent, error) {
	stream, err := c.client.SubscribeTaskEvents(c.ctx(ctx), &a2av1.SubscribeTaskEventsRequest{Id: taskID})
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe events: %w", err)
	}

	ch := make(chan *TaskEvent, 64)
	go func() {
		defer close(ch)
		for {
			ev, err := stream.Recv()
			if err != nil {
				if err != io.EOF {
					c.logger.Warn("事件流接收错误", "error", err)
				}
				return
			}
			ch <- fromProtoTaskEvent(ev)
		}
	}()

	return ch, nil
}

// Close 关闭连接。
func (c *A2AGRPCClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// WithTraceContext 创建一个新的 ctx，其中携带了指定的 TraceContext
//
// 调用方应在发送 RPC 之前使用本方法包装 ctx。例如：
//
//	tc := a2a.GenerateTraceContext()
//	ctx = client.WithTraceContext(ctx, tc)
//	resp, err := client.CreateTask(ctx, msg, taskID)
//
// 这样下游 server 端就能通过 metadata 中的 traceparent 还原同一条 trace。
func (c *A2AGRPCClient) WithTraceContext(ctx context.Context, tc TraceContext) context.Context {
	return WithTraceContext(ctx, tc)
}

// StartTrace 生成一个新的 TraceContext 并注入 ctx
//
// 适用于 client 端是 trace 起点的场景（无上游 trace）。
func (c *A2AGRPCClient) StartTrace(ctx context.Context) (context.Context, TraceContext) {
	tc := GenerateTraceContext()
	return WithTraceContext(ctx, tc), tc
}

// ContinueTrace 基于父 TraceContext 创建子 TraceContext 并注入 ctx
//
// 适用于 trace 已在 client 进程内创建，需要跨 RPC 调用传播到 server 的场景。
func (c *A2AGRPCClient) ContinueTrace(ctx context.Context, parent TraceContext) (context.Context, TraceContext) {
	tc := ChildTraceContext(parent)
	return WithTraceContext(ctx, tc), tc
}
