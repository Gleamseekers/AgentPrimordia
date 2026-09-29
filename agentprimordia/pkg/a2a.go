// Package ap — A2A（Agent-to-Agent）协议公共 API 导出。
//
// Stability: Stable（gRPC 传输）
//
// 自 v1.x 起，**A2A 的默认传输是 gRPC**（性能更优、二进制更小、内建拦截器链）。
// v4.0.0 已移除旧版 JSON-RPC over HTTP 兼容层（见 版本规范.md「已移除记录」）。
// 开放 Agent2Agent 协议互操作（OpenInteropServer / OpenInteropClient，
// JSON-RPC over HTTP/SSE 承载开放协议）见 pkg/a2a_interop.go。
// 推荐使用：
//
//	srv  := ap.NewA2AGRPCServer(service)
//	cli, _ := ap.NewA2AGRPCClient("localhost:50051")
//
// 本文件将 internal/agent/a2a 包中的核心类型与构造函数通过类型别名导出，
// 用户无需直接 import internal 包。
//
// v4.0-3 评审：gRPC 传输自 v1.x 为默认路径，经生产验证，转正为 Stable。

package ap

import (
	"context"
	"log/slog"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent/a2a"
	"github.com/Gleamseekers/AgentPrimordia/internal/resilience"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// ============================================================================
// Agent Card 与元数据
// ============================================================================

// A2AAgentCard 描述 Agent 的能力、技能和端点信息
type A2AAgentCard = a2a.AgentCard

// A2AAgentSkill 描述 Agent 提供的一项技能
type A2AAgentSkill = a2a.AgentSkill

// A2AAgentCapabilities 描述 Agent 支持的能力（流式、推送等）
type A2AAgentCapabilities = a2a.AgentCapabilities

// A2AAgentEndpoints 描述 Agent 的服务端点
type A2AAgentEndpoints = a2a.AgentEndpoints

// NewA2AAgentCard 创建一张新的 Agent Card
func NewA2AAgentCard(agentID, name string) *A2AAgentCard {
	return a2a.NewAgentCard(agentID, name)
}

// ============================================================================
// Task 管理
// ============================================================================

// A2ATask 表示一个 A2A 任务
type A2ATask = a2a.Task

// A2ATaskState 表示任务状态
type A2ATaskState = a2a.TaskState

// A2ATaskStatus 表示任务状态结构
type A2ATaskStatus = a2a.TaskStatus

// A2ATaskFilter 任务过滤条件
type A2ATaskFilter = a2a.TaskFilter

// A2ATaskManager 任务管理器接口
type A2ATaskManager = a2a.TaskManager

// A2ATaskHandler 自定义任务处理器接口
type A2ATaskHandler = a2a.TaskHandler

// NewA2ATaskManager 创建默认的任务管理器实现
func NewA2ATaskManager() *a2a.TaskManagerImpl {
	return a2a.NewTaskManager()
}

// ============================================================================
// 消息与 Artifact
// ============================================================================

// A2AMessage A2A 协议消息
type A2AMessage = a2a.A2AMessage

// A2AArtifact 任务产物
type A2AArtifact = a2a.Artifact

// A2APart 消息内容片段接口
type A2APart = a2a.Part

// A2ATextPart 文本内容片段
type A2ATextPart = a2a.TextPart

// A2AFilePart 文件内容片段
type A2AFilePart = a2a.FilePart

// A2ADataPart 结构化数据片段
type A2ADataPart = a2a.DataPart

// NewA2ATextPart 创建文本片段
func NewA2ATextPart(text string) A2ATextPart {
	return a2a.NewTextPart(text)
}

// NewA2AFilePartFromURI 从 URI 创建文件片段
func NewA2AFilePartFromURI(uri, mime string) A2AFilePart {
	return a2a.NewFilePartFromURI(uri, mime)
}

// NewA2ADataPart 创建数据片段
func NewA2ADataPart(data []byte) A2ADataPart {
	return a2a.NewDataPart(data)
}

// ============================================================================
// 认证
// ============================================================================

// A2APrincipal 认证主体
type A2APrincipal = a2a.Principal

// A2AAuthenticator 认证器接口
type A2AAuthenticator = a2a.Authenticator

// A2ANoopAuthenticator 空认证器（不校验）
type A2ANoopAuthenticator = a2a.NoopAuthenticator

// A2AAPIKeyAuthenticator API Key 认证器
type A2AAPIKeyAuthenticator = a2a.APIKeyAuthenticator

// A2ABearerTokenAuthenticator Bearer Token 认证器
type A2ABearerTokenAuthenticator = a2a.BearerTokenAuthenticator

// A2ABearerTokenValidator Bearer Token 校验函数类型
type A2ABearerTokenValidator = a2a.BearerTokenValidator

// NewA2ANoopAuthenticator 创建空认证器
func NewA2ANoopAuthenticator() *A2ANoopAuthenticator {
	return a2a.NewNoopAuthenticator()
}

// NewA2AAPIKeyAuthenticator 创建 API Key 认证器
//
// keys: API Key -> 客户端 ID 的映射
// headerName: API Key 所在的 HTTP 头名称（如 "X-API-Key"）
func NewA2AAPIKeyAuthenticator(keys map[string]string, headerName string) *A2AAPIKeyAuthenticator {
	return a2a.NewAPIKeyAuthenticator(keys, headerName)
}

// NewA2ABearerTokenAuthenticator 创建 Bearer Token 认证器
func NewA2ABearerTokenAuthenticator(validate A2ABearerTokenValidator) *A2ABearerTokenAuthenticator {
	return a2a.NewBearerTokenAuthenticator(validate)
}

// ============================================================================
// 服务发现
// ============================================================================

// A2ADiscovery 服务发现接口
type A2ADiscovery = a2a.Discovery

// A2AAgentRegistry Agent 注册表
type A2AAgentRegistry = a2a.AgentRegistry

// A2ADiscoveryEvent 发现事件
type A2ADiscoveryEvent = a2a.DiscoveryEvent

// A2ADiscoveryEventType 发现事件类型
type A2ADiscoveryEventType = a2a.DiscoveryEventType

// A2ALocalDiscovery 本地服务发现实现
type A2ALocalDiscovery = a2a.LocalDiscovery

// NewA2ALocalDiscovery 创建本地服务发现实例
func NewA2ALocalDiscovery() *A2ALocalDiscovery {
	return a2a.NewLocalDiscovery()
}

// ============================================================================
// 桥接
// ============================================================================

// A2AMessageBridge A2A 消息与内部 Message 的桥接器
type A2AMessageBridge = a2a.MessageBridge

// NewA2AMessageBridge 创建消息桥接器
func NewA2AMessageBridge() *A2AMessageBridge {
	return a2a.NewMessageBridge()
}

// ============================================================================
// gRPC Server / Client
// ============================================================================

// A2AService 传输无关的 A2A 业务核心
type A2AService = a2a.A2AService

// A2AServiceOption A2AService 配置选项
type A2AServiceOption = a2a.A2AServiceOption

// NewA2AService 创建 A2A 业务核心
func NewA2AService(card *A2AAgentCard, tm A2ATaskManager, opts ...A2AServiceOption) *A2AService {
	return a2a.NewA2AService(card, tm, opts...)
}

// A2ACreateTaskRequest 创建任务请求
type A2ACreateTaskRequest = a2a.CreateTaskRequest

// A2AGRPCServer A2A gRPC 服务端实现
type A2AGRPCServer = a2a.A2AGRPCServer

// A2AGRPCServerOption gRPC 服务端配置选项
type A2AGRPCServerOption = a2a.GRPCServerOption

// NewA2AGRPCServer 创建 A2A gRPC 服务端实现
func NewA2AGRPCServer(service *A2AService, opts ...A2AGRPCServerOption) *A2AGRPCServer {
	return a2a.NewA2AGRPCServer(service, opts...)
}

// NewA2AGRPCServerWithService 构造并返回已注册 A2A 服务的 *grpc.Server
func NewA2AGRPCServerWithService(service *A2AService, opts ...A2AGRPCServerOption) *grpc.Server {
	return a2a.NewGRPCServer(service, opts...)
}

// A2AGRPCClient A2A gRPC 客户端
type A2AGRPCClient = a2a.A2AGRPCClient

// A2AGRPCClientOption gRPC 客户端配置选项
type A2AGRPCClientOption = a2a.GRPCClientOption

// NewA2AGRPCClient 创建 A2A gRPC 客户端
func NewA2AGRPCClient(target string, opts ...A2AGRPCClientOption) (*A2AGRPCClient, error) {
	return a2a.NewA2AGRPCClient(target, opts...)
}

// NewA2AGRPCClientWithConn 使用已有 gRPC 连接创建客户端
func NewA2AGRPCClientWithConn(conn *grpc.ClientConn, opts ...A2AGRPCClientOption) *A2AGRPCClient {
	return a2a.NewA2AGRPCClientWithConn(conn, opts...)
}

// A2AGRPCAuthFunc gRPC 认证函数
type A2AGRPCAuthFunc = a2a.GRPCAuthFunc

// WithGRPCAuth 设置 gRPC server 认证函数
func WithGRPCAuth(auth A2AGRPCAuthFunc) A2AGRPCServerOption {
	return a2a.WithGRPCAuth(auth)
}

// WithGRPCLogger 设置 gRPC server 日志器
func WithGRPCLogger(logger *slog.Logger) A2AGRPCServerOption {
	return a2a.WithGRPCLogger(logger)
}

// A2AInterceptorMetrics gRPC 拦截器共享的指标收集器
type A2AInterceptorMetrics = a2a.A2AInterceptorMetrics

// A2AMetricsSnapshot 指标快照
type A2AMetricsSnapshot = a2a.A2AMetricsSnapshot

// A2AInterceptorConfig gRPC 拦截器共享配置
type A2AInterceptorConfig = a2a.A2AInterceptorConfig

// NewA2AInterceptorMetrics 创建新的指标收集器
func NewA2AInterceptorMetrics() *A2AInterceptorMetrics {
	return a2a.NewA2AInterceptorMetrics()
}

// WithGRPCMetrics 注入共享的指标收集器
func WithGRPCMetrics(m *A2AInterceptorMetrics) A2AGRPCServerOption {
	return a2a.WithGRPCMetrics(m)
}

// WithGRPCSlowRequestThreshold 设置慢请求日志阈值
func WithGRPCSlowRequestThreshold(d time.Duration) A2AGRPCServerOption {
	return a2a.WithGRPCSlowRequestThreshold(d)
}

// RecoveryInterceptor panic 恢复拦截器（链最外层）
func RecoveryInterceptor() grpc.UnaryServerInterceptor { return a2a.RecoveryInterceptor() }

// StreamRecoveryInterceptor 流式 panic 恢复拦截器
func StreamRecoveryInterceptor() grpc.StreamServerInterceptor { return a2a.StreamRecoveryInterceptor() }

// LoggingInterceptor 日志拦截器
func LoggingInterceptor(cfg A2AInterceptorConfig) grpc.UnaryServerInterceptor {
	return a2a.LoggingInterceptor(cfg)
}

// StreamLoggingInterceptor 流式日志拦截器
func StreamLoggingInterceptor(cfg A2AInterceptorConfig) grpc.StreamServerInterceptor {
	return a2a.StreamLoggingInterceptor(cfg)
}

// MetricsInterceptor 指标拦截器
func MetricsInterceptor(m *A2AInterceptorMetrics) grpc.UnaryServerInterceptor {
	return a2a.MetricsInterceptor(m)
}

// StreamMetricsInterceptor 流式指标拦截器
func StreamMetricsInterceptor(m *A2AInterceptorMetrics) grpc.StreamServerInterceptor {
	return a2a.StreamMetricsInterceptor(m)
}

// ChainUnaryInterceptors 组合多个 unary 拦截器
func ChainUnaryInterceptors(interceptors ...grpc.UnaryServerInterceptor) grpc.UnaryServerInterceptor {
	return a2a.ChainUnaryInterceptors(interceptors...)
}

// ChainStreamInterceptors 组合多个 stream 拦截器
func ChainStreamInterceptors(interceptors ...grpc.StreamServerInterceptor) grpc.StreamServerInterceptor {
	return a2a.ChainStreamInterceptors(interceptors...)
}

// ============================================================================
// Trace Propagation（W3C Trace Context 跨 A2A 调用传播）
// ============================================================================

// A2ATraceContext W3C Trace Context 表示（version-trace_id-span_id-flags）
type A2ATraceContext = a2a.TraceContext

// A2AMetadata gRPC metadata 抽象，便于不直接依赖 google.golang.org/grpc/metadata
type A2AMetadata = a2a.Metadata

// A2AStartTrace 在 client 端启动一条新的 trace 并注入 ctx
//
// 适用于本进程是 trace 起点的场景。
func A2AStartTrace(ctx context.Context) (context.Context, A2ATraceContext) {
	return a2a.GenerateTraceContextInCtx(ctx)
}

// A2AContinueTrace 基于父 TraceContext 创建子 TraceContext 并注入 ctx
//
// 适用于本进程已有父 trace，需要跨 A2A RPC 调用传递到 server 的场景。
func A2AContinueTrace(ctx context.Context, parent A2ATraceContext) (context.Context, A2ATraceContext) {
	return a2a.ContinueTraceInCtx(ctx, parent)
}

// A2AExtractTraceContext 从 ctx 提取当前生效的 TraceContext
//
// server 端在 RPC handler 入口调用本方法获取上游 client 注入的 trace。
func A2AExtractTraceContext(ctx context.Context) (A2ATraceContext, bool) {
	return a2a.TraceContextFromContext(ctx)
}

// A2AInjectTraceToGRPCClient 将 TraceContext 写入 A2AGRPCClient 的 outgoing context
//
// 调用方应在调用 *A2AGRPCClient 的 RPC 方法之前使用本方法包装 ctx。
// 例如：
//
//	tc := ap.A2AGenerateTraceContext()
//	ctx := ap.A2AInjectTraceToGRPCClient(ctx, tc)
//	resp, err := client.GetAgentCard(ctx)
//
// 实际上更推荐使用 client.WithTraceContext(ctx, tc) 形式（同义）。
func A2AInjectTraceToGRPCClient(ctx context.Context, tc A2ATraceContext) context.Context {
	return a2a.WithTraceContext(ctx, tc)
}

// A2AGenerateTraceContext 生成一个新的 W3C TraceContext
func A2AGenerateTraceContext() A2ATraceContext {
	return a2a.GenerateTraceContext()
}

// A2AChildTraceContext 基于父 TraceContext 创建子 TraceContext
//
// 子 span 共享父 trace ID，但 span ID 不同；用于 trace 在同一进程内的延续。
func A2AChildTraceContext(parent A2ATraceContext) A2ATraceContext {
	return a2a.ChildTraceContext(parent)
}

// A2AParseTraceParent 解析 W3C traceparent header（"00-<trace>-<span>-<flags>"）
func A2AParseTraceParent(header string) (A2ATraceContext, error) {
	return a2a.ParseTraceParent(header)
}

// ============================================================================
// mTLS（双向 TLS 配置与证书管理）
// ============================================================================

// A2ATLSConfig 定义 gRPC 双向 TLS 配置
type A2ATLSConfig = a2a.TLSConfig

// A2AServerTLSCredentials 从 TLSConfig 创建服务端 TLS 凭证（含 mTLS 客户端证书验证）
func A2AServerTLSCredentials(config A2ATLSConfig) (credentials.TransportCredentials, error) {
	return a2a.ServerTLSCredentials(config)
}

// A2AClientTLSCredentials 从 TLSConfig 创建客户端 TLS 凭证
func A2AClientTLSCredentials(config A2ATLSConfig) (credentials.TransportCredentials, error) {
	return a2a.ClientTLSCredentials(config)
}

// ============================================================================
// gRPC 客户端配置选项（TLS / 认证 / 断路器）
// ============================================================================

// WithA2AGRPCClientTLS 启用 gRPC 客户端 TLS/mTLS
func WithA2AGRPCClientTLS(config A2ATLSConfig) A2AGRPCClientOption {
	return a2a.WithGRPCClientTLS(config)
}

// WithA2AGRPCClientCredentials 直接设置 gRPC 客户端 TransportCredentials
func WithA2AGRPCClientCredentials(creds credentials.TransportCredentials) A2AGRPCClientOption {
	return a2a.WithGRPCClientCredentials(creds)
}

// WithA2AGRPCClientLogger 设置 gRPC 客户端日志器
func WithA2AGRPCClientLogger(logger *slog.Logger) A2AGRPCClientOption {
	return a2a.WithGRPCClientLogger(logger)
}

// WithA2AGRPCClientAPIKey 设置 gRPC 客户端 API Key
func WithA2AGRPCClientAPIKey(key string) A2AGRPCClientOption {
	return a2a.WithGRPCClientAPIKey(key)
}

// WithA2AGRPCClientBearerToken 设置 gRPC 客户端 Bearer Token
func WithA2AGRPCClientBearerToken(token string) A2AGRPCClientOption {
	return a2a.WithGRPCClientBearerToken(token)
}

// ============================================================================
// gRPC 断路器拦截器
// ============================================================================

// A2ACircuitBreakerInterceptor 基于 断路器的 gRPC 客户端拦截器
type A2ACircuitBreakerInterceptor = a2a.CircuitBreakerInterceptor

// NewA2ACircuitBreakerInterceptor 创建断路器拦截器
func NewA2ACircuitBreakerInterceptor(cfg resilience.Config, logger *slog.Logger) *A2ACircuitBreakerInterceptor {
	return a2a.NewCircuitBreakerInterceptor(cfg, logger)
}

// NewA2ACircuitBreakerInterceptorWithCB 使用已有断路器创建拦截器
func NewA2ACircuitBreakerInterceptorWithCB(cb *resilience.CircuitBreaker, logger *slog.Logger) *A2ACircuitBreakerInterceptor {
	return a2a.NewCircuitBreakerInterceptorWithCB(cb, logger)
}

// WithA2AGRPCCircuitBreaker 为 gRPC 客户端添加断路器
func WithA2AGRPCCircuitBreaker(cb *resilience.CircuitBreaker) A2AGRPCClientOption {
	return a2a.WithGRPCCircuitBreaker(cb)
}
