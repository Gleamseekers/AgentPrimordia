// interop_server_race_test.go — OpenInteropServer 共享 task 并发回归。
//
// 背景（2026-09-28 覆盖率补课实测发现）：handleTaskSend/Get/Cancel/Events
// 曾在锁外序列化共享 task，与 executeTask 的锁内写形成数据竞争（-race 红）。
// 修复为"锁内 cloneOpenTask 快照、锁外使用"。本测试以并发 HTTP 负载 +
// executor 常态改写 task 复验——任何回归 -race 即红。
package a2a

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// racingExecutor 常态改写 task 的 executor（放大竞争窗口）。
type racingExecutor struct {
	mu sync.Mutex
	n  int
}

func (e *racingExecutor) Execute(ctx context.Context, task *OpenTask) (*OpenTask, error) {
	e.mu.Lock()
	e.n++
	n := e.n
	e.mu.Unlock()
	// 模拟执行期改写（真实 executor 惯常行为）
	task.Status = OpenTaskStatus{State: OpenTaskWorking, Timestamp: time.Now()}
	task.Messages = append(task.Messages, OpenMessage{Role: "agent", Parts: []OpenPart{{Type: "text", Text: "step"}}})
	task.Metadata = map[string]any{"executed": n}
	done := &OpenTask{
		ID:        task.ID,
		ContextID: task.ContextID,
		Status:    OpenTaskStatus{State: OpenTaskCompleted, Timestamp: time.Now()},
		Messages:  task.Messages,
		Artifacts: []OpenArtifact{{Name: "out", Parts: []OpenPart{{Type: "text", Text: "done"}}}},
		Metadata:  task.Metadata,
	}
	return done, nil
}

func (e *racingExecutor) Cancel(ctx context.Context, taskID string) error { return nil }

func newRacingServer(t *testing.T) (*httptest.Server, *racingExecutor) {
	t.Helper()
	exec := &racingExecutor{}
	srv := NewOpenInteropServer(OpenAgentCard{}, InteropConfig{})
	srv.WithExecutor(exec)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, exec
}

// TestInteropServer_NoRaceUnderConcurrentLoad 并发 send/get/events + executor
// 改写 task：-race 下必须零 DATA RACE。
func TestInteropServer_NoRaceUnderConcurrentLoad(t *testing.T) {
	ts, _ := newRacingServer(t)

	const tasks = 12
	var wg sync.WaitGroup
	for i := 0; i < tasks; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := rpcSendTask(t, ts.URL, i)
			// 并发读（get）
			rpcGetTask(t, ts.URL, id)
			// 并发 SSE 首帧
			resp, err := http.Get(ts.URL + "/a2a/v1/tasks/" + id + "/events")
			if err == nil {
				buf := make([]byte, 256)
				_, _ = resp.Body.Read(buf)
				resp.Body.Close()
			}
		}(i)
	}
	wg.Wait()
}

// rpcSendTask 发一个 tasks/send 并返回 task id。
func rpcSendTask(t *testing.T, baseURL string, seed int) string {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"tasks/send","params":{"id":"race-` +
		strings.Repeat("x", seed%3) + `-` + time.Now().Format("150405.000000000") + `","message":{"role":"user","parts":[{"type":"text","text":"go"}]}}}`
	resp, err := http.Post(baseURL+"/a2a/v1", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	out := string(buf[:n])
	idx := strings.Index(out, `"id":"`)
	if idx < 0 {
		t.Fatalf("响应无 task id: %s", out)
	}
	rest := out[idx+6:]
	end := strings.Index(rest, `"`)
	if end < 0 {
		t.Fatalf("task id 解析失败: %s", out)
	}
	return rest[:end]
}

// rpcGetTask 发一个 tasks/get。
func rpcGetTask(t *testing.T, baseURL, taskID string) {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":2,"method":"tasks/get","params":{"taskId":"` + taskID + `"}}`
	resp, err := http.Post(baseURL+"/a2a/v1", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 8192)
	_, _ = resp.Body.Read(buf)
}
