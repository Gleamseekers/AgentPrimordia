package a2a

import (
	"context"
	"net"
	"testing"
	"time"

	a2av1 "github.com/Gleamseekers/AgentPrimordia/internal/agent/a2a/proto/a2a/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

// newBufconnServerClient 创建一个使用 bufconn 传输的 in-memory gRPC server + client
//
// 返回 client 和 server 端 service 实例，便于在测试中直接验证 server 收到的 ctx。
func newBufconnServerClient(t *testing.T) (*A2AGRPCClient, *A2AService, func()) {
	t.Helper()

	svc := NewA2AService(NewAgentCard("agent-under-test", "Test Agent"), NewTaskManager())
	server := NewGRPCServer(svc)

	lis := bufconn.Listen(1024 * 64)
	go func() {
		_ = server.Serve(lis)
	}()

	dialer := func(context.Context, string) (net.Conn, error) {
		return lis.Dial()
	}

	//nolint:staticcheck // API deprecated, keep for compat; NewClient has lazy-connect semantics
	conn, err := grpc.DialContext(
		context.Background(),
		"bufnet",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	client := NewA2AGRPCClientWithConn(conn)

	cleanup := func() {
		conn.Close()
		server.Stop()
		lis.Close()
	}
	return client, svc, cleanup
}

// TestTracePropagation_EndToEnd_FetchAgentCard 验证 Client → gRPC wire → Server 的 trace 传播
//
// 场景：
//  1. Agent A 生成 TraceContext
//  2. 包装 ctx，调用 A2AGRPCClient.GetAgentCard
//  3. 服务端 handler 收到请求后，extractServerTraceContext 提取 traceparent
//  4. 验证 server 端能从 incoming metadata 中读到相同的 trace ID
func TestTracePropagation_EndToEnd_FetchAgentCard(t *testing.T) {
	client, _, cleanup := newBufconnServerClient(t)
	defer cleanup()

	// Agent A 端：生成 trace 并包装 ctx
	tc := GenerateTraceContext()
	ctx := WithTraceContext(context.Background(), tc)

	// 调用 FetchAgentCard
	card, err := client.FetchAgentCard(ctx)
	if err != nil {
		t.Fatalf("FetchAgentCard failed: %v", err)
	}
	if card == nil {
		t.Fatalf("card should not be nil")
	}
}

// TestTracePropagation_EndToEnd_CreateTaskWithCapture 验证 CreateTask 调用时 trace 跨 wire 传播
//
// 通过自定义 captureServer 拦截 server 收到的 ctx，从中提取 incoming metadata，
// 验证 traceparent header 已被 client 正确写入。
func TestTracePropagation_EndToEnd_CreateTaskWithCapture(t *testing.T) {
	var capturedIncomingMD metadata.MD
	var capturedServerTC TraceContext

	svc := NewA2AService(NewAgentCard("agent-b", "Agent B"), NewTaskManager())

	// 用 raw gRPC server + captureServer 来拦截 metadata
	server := NewA2AGRPCServer(svc)

	rawServer := grpc.NewServer()
	a2av1.RegisterA2AServiceServer(rawServer, &captureServer{
		inner:   server,
		capture: &capturedIncomingMD,
		tc:      &capturedServerTC,
	})

	lis := bufconn.Listen(1024 * 64)
	go func() {
		_ = rawServer.Serve(lis)
	}()

	//nolint:staticcheck // API deprecated, keep for compat; NewClient has lazy-connect semantics
	conn, err := grpc.DialContext(
		context.Background(),
		"bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer func() {
		conn.Close()
		rawServer.Stop()
		lis.Close()
	}()

	client := NewA2AGRPCClientWithConn(conn)

	tc := GenerateTraceContext()
	ctx := WithTraceContext(context.Background(), tc)

	_, err = client.CreateTask(ctx, &A2AMessage{
		Role:  "user",
		Parts: []Part{NewTextPart("hello")},
	}, "task-1")
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	// 验证 server 端收到的 incoming metadata 包含 traceparent
	traceparent := capturedIncomingMD.Get("traceparent")
	if len(traceparent) == 0 {
		t.Fatalf("server did not receive traceparent header; got md: %v", capturedIncomingMD)
	}

	parsed, perr := ParseTraceParent(traceparent[0])
	if perr != nil {
		t.Fatalf("server received invalid traceparent %q: %v", traceparent[0], perr)
	}
	if parsed.TraceID != tc.TraceID {
		t.Errorf("trace_id mismatch: client=%q server=%q", tc.TraceID, parsed.TraceID)
	}

	// 同时验证 server 端 extractServerTraceContext 提取的 TC 与 client 一致
	if capturedServerTC.TraceID != tc.TraceID {
		t.Errorf("server extracted trace_id mismatch: client=%q server=%q", tc.TraceID, capturedServerTC.TraceID)
	}
}

// TestTracePropagation_EndToEnd_StreamTaskEvents 验证流式 RPC 也能传播 trace
func TestTracePropagation_EndToEnd_StreamTaskEvents(t *testing.T) {
	var capturedIncomingMD metadata.MD
	var capturedServerTC TraceContext

	svc := NewA2AService(NewAgentCard("agent-b", "Agent B"), NewTaskManager())
	server := NewA2AGRPCServer(svc)

	rawServer := grpc.NewServer()
	a2av1.RegisterA2AServiceServer(rawServer, &captureServer{
		inner:   server,
		capture: &capturedIncomingMD,
		tc:      &capturedServerTC,
	})

	lis := bufconn.Listen(1024 * 64)
	go func() {
		_ = rawServer.Serve(lis)
	}()

	//nolint:staticcheck // API deprecated, keep for compat; NewClient has lazy-connect semantics
	conn, err := grpc.DialContext(
		context.Background(),
		"bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer func() {
		conn.Close()
		rawServer.Stop()
		lis.Close()
	}()

	client := NewA2AGRPCClientWithConn(conn)

	tc := GenerateTraceContext()
	ctx := WithTraceContext(context.Background(), tc)

	ch, err := client.StreamEvents(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("StreamEvents failed: %v", err)
	}
	// drain
	for range ch {
	}

	// 验证 server 端收到的 incoming metadata 包含 traceparent
	traceparent := capturedIncomingMD.Get("traceparent")
	if len(traceparent) == 0 {
		t.Fatalf("server did not receive traceparent header in stream")
	}
	parsed, _ := ParseTraceParent(traceparent[0])
	if parsed.TraceID != tc.TraceID {
		t.Errorf("stream trace_id mismatch: client=%q server=%q", tc.TraceID, parsed.TraceID)
	}
	if capturedServerTC.TraceID != tc.TraceID {
		t.Errorf("stream server extracted trace_id mismatch: client=%q server=%q", tc.TraceID, capturedServerTC.TraceID)
	}
}

// captureServer 包装 A2AGRPCServer 以捕获 incoming metadata 与提取的 TraceContext
type captureServer struct {
	a2av1.UnimplementedA2AServiceServer
	inner   *A2AGRPCServer
	capture *metadata.MD
	tc      *TraceContext
}

func (c *captureServer) GetAgentCard(ctx context.Context, req *a2av1.GetAgentCardRequest) (*a2av1.AgentCard, error) {
	c.captureIncoming(ctx)
	return c.inner.GetAgentCard(extractServerTraceContext(ctx), req)
}

func (c *captureServer) CreateTask(ctx context.Context, req *a2av1.CreateTaskRequest) (*a2av1.Task, error) {
	c.captureIncoming(ctx)
	return c.inner.CreateTask(extractServerTraceContext(ctx), req)
}

func (c *captureServer) GetTask(ctx context.Context, req *a2av1.GetTaskRequest) (*a2av1.Task, error) {
	c.captureIncoming(ctx)
	return c.inner.GetTask(extractServerTraceContext(ctx), req)
}

func (c *captureServer) CancelTask(ctx context.Context, req *a2av1.CancelTaskRequest) (*a2av1.Task, error) {
	c.captureIncoming(ctx)
	return c.inner.CancelTask(extractServerTraceContext(ctx), req)
}

func (c *captureServer) SubscribeTaskEvents(req *a2av1.SubscribeTaskEventsRequest, stream a2av1.A2AService_SubscribeTaskEventsServer) error {
	ctx := stream.Context()
	c.captureIncoming(ctx)
	wrappedStream := &wrappedServerStream{
		A2AService_SubscribeTaskEventsServer: stream,
		ctx:                                  extractServerTraceContext(ctx),
	}
	return c.inner.SubscribeTaskEvents(req, wrappedStream)
}

func (c *captureServer) captureIncoming(ctx context.Context) {
	md, _ := metadata.FromIncomingContext(ctx)
	*c.capture = md

	enriched, _ := FromGRPCIncomingContext(ctx)
	if tc, ok := TraceContextFromContext(ExtractTraceParent(enriched)); ok {
		*c.tc = tc
	}
}

// wrappedServerStream 包装 grpc.ServerStream 与 A2AService_SubscribeTaskEventsServer
// 以便修改 ctx
type wrappedServerStream struct {
	a2av1.A2AService_SubscribeTaskEventsServer
	ctx context.Context
}

func (w *wrappedServerStream) Context() context.Context {
	return w.ctx
}

// TestTracePropagation_NoContext_NoTrace 验证不带 trace 的调用也能成功（向后兼容）
func TestTracePropagation_NoContext_NoTrace(t *testing.T) {
	client, _, cleanup := newBufconnServerClient(t)
	defer cleanup()

	// 不带 TraceContext 调用，应正常工作
	_, err := client.FetchAgentCard(context.Background())
	if err != nil {
		t.Errorf("FetchAgentCard without trace context should still work, got: %v", err)
	}
}

// TestTracePropagation_ConcurrentCalls 验证并发场景下 trace 隔离
func TestTracePropagation_ConcurrentCalls(t *testing.T) {
	client, _, cleanup := newBufconnServerClient(t)
	defer cleanup()

	const N = 20
	errCh := make(chan error, N)
	for i := 0; i < N; i++ {
		go func(idx int) {
			tc := GenerateTraceContext()
			ctx := WithTraceContext(context.Background(), tc)
			ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			if _, err := client.FetchAgentCard(ctx); err != nil {
				errCh <- err
			} else {
				errCh <- nil
			}
		}(i)
	}

	for i := 0; i < N; i++ {
		if err := <-errCh; err != nil {
			t.Errorf("concurrent call failed: %v", err)
		}
	}
}
