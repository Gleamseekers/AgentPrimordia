package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	a2av1 "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/a2a/proto/a2a/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func BenchmarkA2AService_CreateTask(b *testing.B) {
	card := NewAgentCard("agent", "Agent")
	service := NewA2AService(card, NewTaskManager())
	msg := &A2AMessage{Role: "user", Parts: []Part{NewTextPart("hello")}}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := service.CreateTask(ctx, &CreateTaskRequest{Message: msg})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGRPC_CreateTask(b *testing.B) {
	card := NewAgentCard("agent", "Agent")
	service := NewA2AService(card, NewTaskManager())
	server := NewGRPCServer(service)
	lis := bufconn.Listen(1024 * 1024)
	go func() { _ = server.Serve(lis) }()
	defer server.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	//nolint:staticcheck // API 弃用但为兼容保留，NewClient 为惰性连接语义不同
	conn, err := grpc.DialContext(ctx, "passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		//nolint:staticcheck // API 弃用但为兼容保留，NewClient 为惰性连接语义不同
		grpc.WithBlock())
	if err != nil {
		b.Fatal(err)
	}
	defer conn.Close()

	client := a2av1.NewA2AServiceClient(conn)
	msg := toProtoMessage(&A2AMessage{Role: "user", Parts: []Part{NewTextPart("hello")}})
	reqCtx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := client.CreateTask(reqCtx, &a2av1.CreateTaskRequest{Message: msg})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHTTP_CreateTask(b *testing.B) {
	card := NewAgentCard("agent", "Agent")
	server := NewA2AServer(NewTaskManager(), WithCard(card))
	handler := server.Handler()

	params, _ := json.Marshal(map[string]any{
		"message": map[string]string{"role": "user"},
	})
	body, _ := json.Marshal(JSONRPCRequest{JSONRPC: "2.0", ID: 1, Method: "task/create", Params: params})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("status %d", rec.Code)
		}
	}
}
