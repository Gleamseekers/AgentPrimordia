package a2a

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"time"

	a2av1 "github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/a2a/proto/a2a/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

// GRPCServerOption gRPC server 配置选项。
type GRPCServerOption func(*A2AGRPCServer)

// WithGRPCLogger 设置 gRPC server 日志器。
func WithGRPCLogger(logger *slog.Logger) GRPCServerOption {
	return func(s *A2AGRPCServer) { s.logger = logger }
}

// WithGRPCAuth 设置 gRPC 认证函数。
func WithGRPCAuth(auth GRPCAuthFunc) GRPCServerOption {
	return func(s *A2AGRPCServer) { s.auth = auth }
}

// A2AGRPCServer gRPC 服务实现。
type A2AGRPCServer struct {
	a2av1.UnimplementedA2AServiceServer
	service *A2AService
	auth    GRPCAuthFunc
	logger  *slog.Logger
	metrics *A2AInterceptorMetrics
	// slowThreshold 慢请求阈值；0 表示使用默认值（1s）
	slowThreshold time.Duration
	// tlsConfig TLS 配置（可选，设置后启用 mTLS）
	tlsConfig *TLSConfig
	// tlsCreds 直接设置的 TLS 凭证（优先级高于 tlsConfig）
	tlsCreds credentials.TransportCredentials
}

// NewA2AGRPCServer 创建 gRPC 服务实现。
func NewA2AGRPCServer(service *A2AService, opts ...GRPCServerOption) *A2AGRPCServer {
	s := &A2AGRPCServer{
		service: service,
		logger:  slog.Default(),
		metrics: &A2AInterceptorMetrics{},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Metrics 返回当前 gRPC 拦截器收集的指标快照。
func (s *A2AGRPCServer) Metrics() A2AMetricsSnapshot {
	if s.metrics == nil {
		return A2AMetricsSnapshot{}
	}
	return s.metrics.Snapshot()
}

// Register 将服务注册到 *grpc.Server。
func (s *A2AGRPCServer) Register(server *grpc.Server) {
	a2av1.RegisterA2AServiceServer(server, s)
}

// GetAgentCard 获取 AgentCard。
func (s *A2AGRPCServer) GetAgentCard(ctx context.Context, _ *a2av1.GetAgentCardRequest) (*a2av1.AgentCard, error) {
	ctx = extractServerTraceContext(ctx)
	card, err := s.service.GetAgentCard(ctx)
	if err != nil {
		return nil, mapServiceError(err)
	}
	return toProtoAgentCard(card), nil
}

// CreateTask 创建任务。
func (s *A2AGRPCServer) CreateTask(ctx context.Context, req *a2av1.CreateTaskRequest) (*a2av1.Task, error) {
	ctx = extractServerTraceContext(ctx)
	task, err := s.service.CreateTask(ctx, &CreateTaskRequest{
		Message:   fromProtoMessage(req.Message),
		TaskID:    req.TaskId,
		SessionID: req.SessionId,
	})
	if err != nil {
		return nil, mapServiceError(err)
	}
	return toProtoTask(task), nil
}

// GetTask 获取任务。
func (s *A2AGRPCServer) GetTask(ctx context.Context, req *a2av1.GetTaskRequest) (*a2av1.Task, error) {
	ctx = extractServerTraceContext(ctx)
	task, err := s.service.GetTask(ctx, req.Id)
	if err != nil {
		return nil, mapServiceError(err)
	}
	return toProtoTask(task), nil
}

// CancelTask 取消任务。
func (s *A2AGRPCServer) CancelTask(ctx context.Context, req *a2av1.CancelTaskRequest) (*a2av1.Task, error) {
	ctx = extractServerTraceContext(ctx)
	task, err := s.service.CancelTask(ctx, req.Id)
	if err != nil {
		return nil, mapServiceError(err)
	}
	return toProtoTask(task), nil
}

// SubscribeTaskEvents 订阅任务事件流。
func (s *A2AGRPCServer) SubscribeTaskEvents(req *a2av1.SubscribeTaskEventsRequest, stream a2av1.A2AService_SubscribeTaskEventsServer) error {
	ctx := stream.Context()
	ctx = extractServerTraceContext(ctx)
	ch, err := s.service.SubscribeTaskEvents(ctx, req.Id)
	if err != nil {
		return mapServiceError(err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-ch:
			if !ok {
				return nil
			}
			if err := stream.Send(toProtoTaskEvent(ev)); err != nil {
				if err == io.EOF {
					return nil
				}
				return err
			}
		}
	}
}

// mapServiceError 将 A2AService 错误映射为 gRPC status。
func mapServiceError(err error) error {
	switch {
	case errors.Is(err, ErrTaskNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, ErrTaskConflict):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, ErrMessageMissing):
		return status.Error(codes.InvalidArgument, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}

// extractServerTraceContext 从 gRPC incoming ctx 提取 trace context 并注入新 ctx
//
// 该函数在每个 RPC handler 入口调用，使得下游 service 能从 ctx.Value 读取 TraceContext。
// 若 metadata 中无 traceparent header，则 ctx 保持不变。
func extractServerTraceContext(ctx context.Context) context.Context {
	enrichedCtx, _ := FromGRPCIncomingContext(ctx)
	return ExtractTraceParent(enrichedCtx)
}

// NewGRPCServer 构造并返回一个 *grpc.Server（已注册 A2A 服务）。
//
// 默认拦截器链（从外到内）：
//  1. Recovery —— panic 恢复
//  2. Logging —— 记录方法、耗时、错误
//  3. Metrics —— 累计调用次数、错误、延迟
//  4. Auth（可选）—— 仅当设置了 GRPCAuthFunc 时启用
func NewGRPCServer(service *A2AService, opts ...GRPCServerOption) *grpc.Server {
	s := NewA2AGRPCServer(service, opts...)
	cfg := A2AInterceptorConfig{
		Logger:               s.logger,
		Metrics:              s.metrics,
		SlowRequestThreshold: s.slowThreshold,
	}
	var unaryInterceptors []grpc.UnaryServerInterceptor
	var streamInterceptors []grpc.StreamServerInterceptor
	// Recovery（最外层，确保 panic 不会让后续拦截器无法处理）
	unaryInterceptors = append(unaryInterceptors, RecoveryInterceptor())
	streamInterceptors = append(streamInterceptors, StreamRecoveryInterceptor())
	// Logging
	unaryInterceptors = append(unaryInterceptors, LoggingInterceptor(cfg))
	streamInterceptors = append(streamInterceptors, StreamLoggingInterceptor(cfg))
	// Metrics
	unaryInterceptors = append(unaryInterceptors, MetricsInterceptor(s.metrics))
	streamInterceptors = append(streamInterceptors, StreamMetricsInterceptor(s.metrics))
	// Auth（可选，最内层，确保其他拦截器先记录）
	if s.auth != nil {
		unaryInterceptors = append(unaryInterceptors, UnaryAuthInterceptor(s.auth))
		streamInterceptors = append(streamInterceptors, StreamAuthInterceptor(s.auth))
	}
	// 构建 gRPC server 选项
	serverOpts := []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(unaryInterceptors...),
		grpc.ChainStreamInterceptor(streamInterceptors...),
	}

	// TLS/mTLS 配置
	switch {
	case s.tlsCreds != nil:
		serverOpts = append(serverOpts, grpc.Creds(s.tlsCreds))
	case s.tlsConfig != nil:
		creds, err := ServerTLSCredentials(*s.tlsConfig)
		if err != nil {
			s.logger.Error("gRPC TLS 配置失败", "error", err)
		} else {
			serverOpts = append(serverOpts, grpc.Creds(creds))
		}
	}

	server := grpc.NewServer(serverOpts...)
	s.Register(server)
	return server
}

// ServeGRPC 在指定 listener 上启动 gRPC server。
func ServeGRPC(server *grpc.Server, lis net.Listener) error {
	return server.Serve(lis)
}

// WithGRPCMetrics 设置拦截器共享的指标收集器。
// 默认情况下每个 server 内部创建一个新的指标实例；通过该选项可注入共享实例。
func WithGRPCMetrics(m *A2AInterceptorMetrics) GRPCServerOption {
	return func(s *A2AGRPCServer) {
		if m != nil {
			s.metrics = m
		}
	}
}

// WithGRPCSlowRequestThreshold 设置慢请求日志阈值。
func WithGRPCSlowRequestThreshold(d time.Duration) GRPCServerOption {
	return func(s *A2AGRPCServer) {
		if d > 0 {
			s.slowThreshold = d
		}
	}
}

// WithGRPCTLS 启用 gRPC TLS（服务端证书 + mTLS 客户端验证）。
func WithGRPCTLS(config TLSConfig) GRPCServerOption {
	return func(s *A2AGRPCServer) {
		s.tlsConfig = &config
	}
}

// WithGRPCCredentials 直接设置 gRPC TransportCredentials。
func WithGRPCCredentials(creds credentials.TransportCredentials) GRPCServerOption {
	return func(s *A2AGRPCServer) {
		s.tlsCreds = creds
	}
}
