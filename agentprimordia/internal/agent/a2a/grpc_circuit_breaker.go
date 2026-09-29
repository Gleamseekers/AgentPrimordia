package a2a

import (
	"context"
	"log/slog"

	"github.com/Gleamseekers/AgentPrimordia/internal/resilience"
	"google.golang.org/grpc"
)

// CircuitBreakerInterceptor 是基于断路器的 gRPC 客户端拦截器。
// 当连续失败次数达到阈值时，快速失败并返回 ErrCircuitOpen。
type CircuitBreakerInterceptor struct {
	cb     *resilience.CircuitBreaker
	logger *slog.Logger
}

// NewCircuitBreakerInterceptor 创建断路器拦截器。
func NewCircuitBreakerInterceptor(cfg resilience.Config, logger *slog.Logger) *CircuitBreakerInterceptor {
	if logger == nil {
		logger = slog.Default()
	}
	return &CircuitBreakerInterceptor{
		cb:     resilience.NewCircuitBreaker(cfg),
		logger: logger,
	}
}

// NewCircuitBreakerInterceptorWithCB 使用已有断路器创建拦截器。
func NewCircuitBreakerInterceptorWithCB(cb *resilience.CircuitBreaker, logger *slog.Logger) *CircuitBreakerInterceptor {
	if logger == nil {
		logger = slog.Default()
	}
	return &CircuitBreakerInterceptor{
		cb:     cb,
		logger: logger,
	}
}

// UnaryClientInterceptor 返回 gRPC 一元客户端拦截器。
func (c *CircuitBreakerInterceptor) UnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		return c.cb.Execute(ctx, func(ctx context.Context) error {
			return invoker(ctx, method, req, reply, cc, opts...)
		})
	}
}

// StreamClientInterceptor 返回 gRPC 流式客户端拦截器。
func (c *CircuitBreakerInterceptor) StreamClientInterceptor() grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		var stream grpc.ClientStream
		err := c.cb.Execute(ctx, func(ctx context.Context) error {
			var err error
			stream, err = streamer(ctx, desc, cc, method, opts...)
			return err
		})
		return stream, err
	}
}

// State 返回断路器当前状态。
func (c *CircuitBreakerInterceptor) State() resilience.State {
	return c.cb.State()
}

// Reset 手动重置断路器。
func (c *CircuitBreakerInterceptor) Reset() {
	c.cb.Reset()
}

// WithGRPCCircuitBreaker 为 gRPC 客户端添加断路器（通过 WithGRPCClientCredentials 配合使用）。
func WithGRPCCircuitBreaker(cb *resilience.CircuitBreaker) GRPCClientOption {
	return func(c *A2AGRPCClient) {
		c.circuitBreaker = cb
	}
}
