package a2a

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// v3.5 互操作集成测试：模拟第三方 Agent 完整委托链路
//
// 启动 OpenInteropServer（httptest），用 OpenInteropClient 调用，
// 验证 FetchAgentCard → SendTask → GetTask → CancelTask 全链路。

func newTestInteropServer(t *testing.T) (*httptest.Server, OpenAgentCard) {
	t.Helper()
	card := OpenAgentCard{
		Name:               "interop-test",
		Description:        "integration test agent",
		URL:                "http://test",
		Version:            "1.0.0",
		Capabilities:       OpenCapabilities{Streaming: true},
		Skills:             []OpenSkillDecl{{ID: "s1", Name: "echo"}},
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
	}
	srv := NewOpenInteropServer(card, DefaultInteropConfig())
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, card
}

func TestInteropFullDelegation(t *testing.T) {
	ts, card := newTestInteropServer(t)
	client := NewOpenInteropClient(ts.URL)
	ctx := context.Background()

	// 1. 获取 Agent Card
	got, err := client.FetchAgentCard(ctx)
	if err != nil {
		t.Fatalf("fetch card: %v", err)
	}
	if got.Name != card.Name {
		t.Errorf("card name = %q, want %q", got.Name, card.Name)
	}
	if len(got.Skills) != 1 {
		t.Errorf("skills = %d", len(got.Skills))
	}

	// 2. 发送任务
	task, err := client.SendTask(ctx, NewTextMessage("user", "delegate this"))
	if err != nil {
		t.Fatalf("send task: %v", err)
	}
	if task.ID == "" {
		t.Fatal("task id empty")
	}
	// 异步执行：任务可能已进入 working 状态
	if task.Status.State != OpenTaskSubmitted && task.Status.State != OpenTaskWorking {
		t.Errorf("state = %q, want submitted or working", task.Status.State)
	}

	// 3. 查询任务
	fetched, err := client.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if fetched.ID != task.ID {
		t.Errorf("fetched id = %q", fetched.ID)
	}

	// 4. 取消任务
	if err := client.CancelTask(ctx, task.ID); err != nil {
		t.Fatalf("cancel task: %v", err)
	}
	canceled, _ := client.GetTask(ctx, task.ID)
	if canceled.Status.State != OpenTaskCanceled {
		t.Errorf("after cancel state = %q, want canceled", canceled.Status.State)
	}
}

func TestInteropGetUnknownTask(t *testing.T) {
	ts, _ := newTestInteropServer(t)
	client := NewOpenInteropClient(ts.URL)

	_, err := client.GetTask(context.Background(), "nonexist")
	if err == nil {
		t.Fatal("expected error for unknown task")
	}
	oe, ok := err.(*OpenError)
	if !ok {
		t.Fatalf("error type = %T, want *OpenError", err)
	}
	if oe.Code != OpenErrTaskNotFound {
		t.Errorf("code = %d, want %d", oe.Code, OpenErrTaskNotFound)
	}
}

func TestInteropAgentCardEndpoint(t *testing.T) {
	ts, _ := newTestInteropServer(t)
	resp, err := http.Get(ts.URL + "/.well-known/agent.json")
	if err != nil {
		t.Fatalf("get card endpoint: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestInteropMethodNotAllowed(t *testing.T) {
	ts, _ := newTestInteropServer(t)
	// GET 到 JSON-RPC 端点应 405
	resp, err := http.Get(ts.URL + "/a2a/v1")
	if err != nil {
		t.Fatalf("get rpc: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}
