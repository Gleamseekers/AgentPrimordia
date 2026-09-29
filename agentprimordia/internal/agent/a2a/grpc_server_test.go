package a2a

import (
	"context"
	"net"
	"testing"
	"time"

	a2av1 "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/a2a/proto/a2a/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func startTestGRPCServer(t *testing.T, service *A2AService, opts ...GRPCServerOption) (*grpc.Server, *bufconn.Listener) {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	server := NewGRPCServer(service, opts...)
	go func() {
		if err := server.Serve(lis); err != nil {
			t.Logf("server serve error: %v", err)
		}
	}()
	return server, lis
}

func dialTestGRPC(t *testing.T, lis *bufconn.Listener) *grpc.ClientConn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	//nolint:staticcheck // API 弃用但为兼容保留，NewClient 为惰性连接语义不同
	conn, err := grpc.DialContext(ctx, "passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		//nolint:staticcheck // API 弃用但为兼容保留，NewClient 为惰性连接语义不同
		grpc.WithBlock())
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	return conn
}

func TestGRPCServer_GetAgentCard(t *testing.T) {
	card := NewAgentCard("agent-1", "Test Agent")
	service := NewA2AService(card, NewTaskManager())
	server, lis := startTestGRPCServer(t, service)
	defer server.Stop()

	conn := dialTestGRPC(t, lis)
	defer conn.Close()

	client := a2av1.NewA2AServiceClient(conn)
	resp, err := client.GetAgentCard(context.Background(), &a2av1.GetAgentCardRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.AgentId != "agent-1" {
		t.Errorf("AgentId = %q, want %q", resp.AgentId, "agent-1")
	}
}

func TestGRPCServer_CreateAndGetTask(t *testing.T) {
	card := NewAgentCard("agent-1", "Test Agent")
	service := NewA2AService(card, NewTaskManager())
	server, lis := startTestGRPCServer(t, service)
	defer server.Stop()

	conn := dialTestGRPC(t, lis)
	defer conn.Close()

	client := a2av1.NewA2AServiceClient(conn)
	msg := toProtoMessage(&A2AMessage{Role: "user", Parts: []Part{NewTextPart("hello")}})
	created, err := client.CreateTask(context.Background(), &a2av1.CreateTaskRequest{Message: msg})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	got, err := client.GetTask(context.Background(), &a2av1.GetTaskRequest{Id: created.Id})
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got.Id != created.Id {
		t.Errorf("ID mismatch: %q vs %q", got.Id, created.Id)
	}
}

func TestGRPCServer_GetTask_NotFound(t *testing.T) {
	card := NewAgentCard("agent-1", "Test Agent")
	service := NewA2AService(card, NewTaskManager())
	server, lis := startTestGRPCServer(t, service)
	defer server.Stop()

	conn := dialTestGRPC(t, lis)
	defer conn.Close()

	client := a2av1.NewA2AServiceClient(conn)
	_, err := client.GetTask(context.Background(), &a2av1.GetTaskRequest{Id: "missing"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

func TestGRPCServer_CancelTask(t *testing.T) {
	card := NewAgentCard("agent-1", "Test Agent")
	service := NewA2AService(card, NewTaskManager())
	server, lis := startTestGRPCServer(t, service)
	defer server.Stop()

	conn := dialTestGRPC(t, lis)
	defer conn.Close()

	client := a2av1.NewA2AServiceClient(conn)
	msg := toProtoMessage(&A2AMessage{Role: "user", Parts: []Part{NewTextPart("hello")}})
	created, _ := client.CreateTask(context.Background(), &a2av1.CreateTaskRequest{Message: msg})

	canceled, err := client.CancelTask(context.Background(), &a2av1.CancelTaskRequest{Id: created.Id})
	if err != nil {
		t.Fatalf("cancel failed: %v", err)
	}
	if canceled.State != string(TaskCanceled) {
		t.Errorf("state = %q, want %q", canceled.State, TaskCanceled)
	}
}

func TestGRPCServer_SubscribeTaskEvents(t *testing.T) {
	card := NewAgentCard("agent-1", "Test Agent")
	tm := NewTaskManager()
	service := NewA2AService(card, tm)
	server, lis := startTestGRPCServer(t, service)
	defer server.Stop()

	conn := dialTestGRPC(t, lis)
	defer conn.Close()

	client := a2av1.NewA2AServiceClient(conn)
	msg := toProtoMessage(&A2AMessage{Role: "user", Parts: []Part{NewTextPart("hello")}})
	created, _ := client.CreateTask(context.Background(), &a2av1.CreateTaskRequest{Message: msg})

	streamCtx, streamCancel := context.WithCancel(context.Background())
	defer streamCancel()
	stream, err := client.SubscribeTaskEvents(streamCtx, &a2av1.SubscribeTaskEventsRequest{Id: created.Id})
	if err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}

	// 等待 server-side subscription 就绪，避免事件在订阅建立前发出
	time.Sleep(100 * time.Millisecond)
	_ = tm.Update(created.Id, TaskWorking, nil)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			t.Fatal("timeout waiting for event")
		default:
		}
		ev, err := stream.Recv()
		if err != nil {
			t.Fatalf("recv failed: %v", err)
		}
		if ev.TaskId == created.Id {
			streamCancel()
			return
		}
	}
}
