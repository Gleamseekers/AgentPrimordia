// a2a_coverage_test.go — A2A 协议栈覆盖率补强（开放协议互操作面）
//
// 目标（只新增测试，不改生产代码）：
//   - interop_executor/interop_sse/interop_modes/interop_report/interop_integration：
//     开放协议执行器与 SSE 写入器、IO 模式、符合性报告、跨组件集成；
//   - interop_server：异步执行失败路径、SSE 订阅端点、协议错误分支；
//   - interop_client / client.go：RPC 错误路径、SSE 流读取（含畸形帧）；
//   - registry_file.go：文件注册表全生命周期与损坏文件处理；
//   - types.go：A2AMessage 反序列化的 part 类型分派与回退。
package a2a

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// slogDiscard 返回丢弃全部输出的 logger（测试用）。
func slogDiscard() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// ===== interop_executor.go =====

// TestCoverSimpleTaskExecutor_NilHandler 未注入 handler 时执行标记失败。
func TestCoverSimpleTaskExecutor_NilHandler(t *testing.T) {
	t.Parallel()
	e := NewSimpleTaskExecutor(nil)
	task := &OpenTask{ID: "t1"}
	got, err := e.Execute(context.Background(), task)
	if err != nil {
		t.Fatalf("nil handler 不应返回错误: %v", err)
	}
	if got.Status.State != OpenTaskFailed {
		t.Errorf("State = %q, want failed", got.Status.State)
	}
}

// TestCoverSimpleTaskExecutor_HandlerAndCancel handler 与取消回调的分派。
func TestCoverSimpleTaskExecutor_HandlerAndCancel(t *testing.T) {
	t.Parallel()
	e := NewSimpleTaskExecutor(func(_ context.Context, task *OpenTask) (*OpenTask, error) {
		task.Status = OpenTaskStatus{State: OpenTaskCompleted}
		return task, nil
	})
	got, err := e.Execute(context.Background(), &OpenTask{ID: "t2"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status.State != OpenTaskCompleted {
		t.Errorf("State = %q, want completed", got.Status.State)
	}

	// 未设置取消回调 → nil
	if err := e.Cancel(context.Background(), "t2"); err != nil {
		t.Errorf("无取消回调应返回 nil: %v", err)
	}

	// WithCancel 链式设置后回调被调用
	var canceledID string
	e2 := e.WithCancel(func(_ context.Context, taskID string) error {
		canceledID = taskID
		return nil
	})
	if e2 != e {
		t.Error("WithCancel 应返回同一实例（链式）")
	}
	if err := e.Cancel(context.Background(), "t3"); err != nil {
		t.Fatal(err)
	}
	if canceledID != "t3" {
		t.Errorf("取消回调未收到 taskID: %q", canceledID)
	}
}

// TestCoverEchoTaskExecutor 回声执行器：文本拼接 / 非 user 角色忽略 / ctx 取消。
func TestCoverEchoTaskExecutor(t *testing.T) {
	t.Parallel()
	e := NewEchoTaskExecutor()

	task := &OpenTask{
		ID: "echo-1",
		Messages: []OpenMessage{
			{Role: "agent", Parts: []OpenPart{{Type: "text", Text: "ignored"}}},
			{Role: "user", Parts: []OpenPart{
				{Type: "text", Text: "hello "},
				{Type: "file", Text: "not-text"},
				{Type: "text", Text: "world"},
			}},
		},
	}
	got, err := e.Execute(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status.State != OpenTaskCompleted {
		t.Errorf("State = %q, want completed", got.Status.State)
	}
	if len(got.Artifacts) != 1 {
		t.Fatalf("应有 1 个 artifact, got %d", len(got.Artifacts))
	}
	if got.Artifacts[0].Parts[0].Text != "Echo: hello world" {
		t.Errorf("回显文本错误: %q", got.Artifacts[0].Parts[0].Text)
	}

	// ctx 已取消 → 返回错误
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.Execute(ctx, task); err == nil {
		t.Error("已取消的 ctx 应返回错误")
	}
	// Cancel 恒 nil
	if err := e.Cancel(context.Background(), "echo-1"); err != nil {
		t.Errorf("Echo Cancel 应返回 nil: %v", err)
	}
}

// ===== interop_sse.go =====

// nonFlushWriter 是不实现 http.Flusher 的 ResponseWriter。
type nonFlushWriter struct{ h http.Header }

func (n *nonFlushWriter) Header() http.Header {
	if n.h == nil {
		n.h = make(http.Header)
	}
	return n.h
}
func (n *nonFlushWriter) Write(b []byte) (int, error) { return len(b), nil }
func (n *nonFlushWriter) WriteHeader(int)             {}

// TestCoverOpenSSEWriter_NonFlusher 不支持 Flushing 的 Writer → 构造错误。
func TestCoverOpenSSEWriter_NonFlusher(t *testing.T) {
	t.Parallel()
	if _, err := NewOpenSSEWriter(&nonFlushWriter{}); err == nil {
		t.Fatal("不支持 Flush 的 ResponseWriter 应返回错误")
	}
}

// TestCoverOpenSSEWriter_WriteEvents SSE 头设置与三种事件写入格式。
func TestCoverOpenSSEWriter_WriteEvents(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	sw, err := NewOpenSSEWriter(rec)
	if err != nil {
		t.Fatal(err)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q", cc)
	}

	if err := sw.WriteEvent(SSEMessageDelta, `{"delta":"hi"}`); err != nil {
		t.Fatal(err)
	}
	if err := sw.WriteMessageDelta("task-1", "hello"); err != nil {
		t.Fatal(err)
	}
	if err := sw.WriteTaskStatus("task-1", OpenTaskCompleted); err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: message.delta\ndata: ") {
		t.Errorf("WriteEvent 输出格式错误: %q", body)
	}
	if !strings.Contains(body, `"delta":"hello"`) {
		t.Errorf("WriteMessageDelta 载荷错误: %q", body)
	}
	if !strings.Contains(body, "event: task.status_update") || !strings.Contains(body, `"state":"completed"`) {
		t.Errorf("WriteTaskStatus 载荷错误: %q", body)
	}
}

// ===== interop_modes.go =====

// TestCoverIOModeConfig_SupportsOutput 输出模式判定真假两路。
func TestCoverIOModeConfig_SupportsOutput(t *testing.T) {
	t.Parallel()
	cfg := IOModeConfig{
		InputModes:  []IOMode{IOModeText, IOModeImage},
		OutputModes: []IOMode{IOModeText},
	}
	if !cfg.SupportsOutput(IOModeText) {
		t.Error("应支持 text 输出")
	}
	if cfg.SupportsOutput(IOModeImage) {
		t.Error("不应支持 image 输出")
	}
	if !cfg.SupportsInput(IOModeImage) || cfg.SupportsInput(IOModeVideo) {
		t.Error("输入模式判定异常")
	}
}

// ===== interop_report.go =====

// TestCoverGenerateInteropReport 符合性报告：全通过/缺字段/FailedChecks。
func TestCoverGenerateInteropReport(t *testing.T) {
	t.Parallel()
	full := OpenAgentCard{
		Name: "a", URL: "http://x", Version: "1.0",
		DefaultInputModes: []string{"text"}, DefaultOutputModes: []string{"text"},
	}
	cfg := InteropConfig{Mode: InteropStrict, ExposeAgentCard: true, IOModes: DefaultIOModeConfig()}
	rep := GenerateInteropReport(full, cfg)
	if rep.Score != 1.0 {
		t.Errorf("完整声明应得 1.0, got %v（失败项: %+v）", rep.Score, rep.FailedChecks())
	}
	if rep.Mode != "strict" {
		t.Errorf("Mode = %q", rep.Mode)
	}
	if len(rep.FailedChecks()) != 0 {
		t.Errorf("不应有失败项: %+v", rep.FailedChecks())
	}

	// 缺 name/url/version/modes + 未暴露 card + 无 text IO → 7 项失败
	empty := GenerateInteropReport(OpenAgentCard{}, InteropConfig{})
	failed := empty.FailedChecks()
	if len(failed) != 7 {
		t.Errorf("空声明应有 7 项失败, got %d: %+v", len(failed), failed)
	}
	if empty.Score <= 0 || empty.Score >= 1 {
		t.Errorf("部分通过得分应在 (0,1): %v", empty.Score)
	}
}

// ===== interop_integration.go =====

// stubInteropAuth 实现 InteropAuthenticator。
type stubInteropAuth struct {
	subject string
	err     error
}

func (s stubInteropAuth) Authenticate(_ context.Context, _ string) (string, error) {
	return s.subject, s.err
}

// stubInteropRegistry 实现 InteropRegistry。
type stubInteropRegistry struct {
	cards map[string]OpenAgentCard
}

func (s *stubInteropRegistry) Register(card OpenAgentCard) error {
	s.cards[card.Name] = card
	return nil
}
func (s *stubInteropRegistry) Deregister(name string) error {
	delete(s.cards, name)
	return nil
}

// stubTracer 实现 InteropTracer。
type stubTracer struct{ extracted bool }

func (s *stubTracer) Inject(_ context.Context, _ map[string]string) {}
func (s *stubTracer) Extract(ctx context.Context, _ map[string]string) context.Context {
	s.extracted = true
	return ctx
}

// stubLimiter 实现 InteropRateLimiter。
type stubLimiter struct{ allow bool }

func (s stubLimiter) Allow(string) bool { return s.allow }

// stubSkillSource 实现 InteropSkillSource。
type stubSkillSource struct{ decls []OpenSkillDecl }

func (s stubSkillSource) Declarations() []OpenSkillDecl { return s.decls }

// TestCoverInteropIntegrations 五类跨组件集成的成功/失败路径。
func TestCoverInteropIntegrations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// 认证集成
	okAuth := NewAuthIntegration(stubInteropAuth{subject: "svc-a"})
	if sub, err := okAuth.Check(ctx, "cred"); err != nil || sub != "svc-a" {
		t.Errorf("Check = %q, %v", sub, err)
	}
	badAuth := NewAuthIntegration(stubInteropAuth{err: fmt.Errorf("bad cred")})
	if _, err := badAuth.Check(ctx, "cred"); err == nil {
		t.Error("认证失败应返回错误")
	}

	// 发现集成
	reg := &stubInteropRegistry{cards: map[string]OpenAgentCard{}}
	disc := NewDiscoveryIntegration(reg)
	if err := disc.RegisterCard(OpenAgentCard{Name: "n", URL: "http://n"}); err != nil {
		t.Fatal(err)
	}
	if len(reg.cards) != 1 {
		t.Error("卡片应已注册")
	}
	if err := disc.RegisterCard(OpenAgentCard{Name: "", URL: "http://n"}); err == nil {
		t.Error("缺 name 应报错")
	}
	if err := disc.RegisterCard(OpenAgentCard{Name: "n2"}); err == nil {
		t.Error("缺 url 应报错")
	}

	// 追踪集成
	tr := &stubTracer{}
	trace := NewTraceIntegration(tr)
	if got := trace.Propagate(ctx, map[string]string{"traceparent": "00-x"}); got == nil {
		t.Fatal("Propagate 不应返回 nil ctx")
	}
	if !tr.extracted {
		t.Error("Extract 应被调用")
	}

	// 限流集成
	allow := NewRateLimitIntegration(stubLimiter{allow: true})
	if err := allow.CheckQuota("svc-a"); err != nil {
		t.Errorf("允许的调用不应报错: %v", err)
	}
	deny := NewRateLimitIntegration(stubLimiter{allow: false})
	err := deny.CheckQuota("svc-b")
	if err == nil {
		t.Fatal("超限应报错")
	}
	if oe, ok := err.(*OpenError); !ok || oe.Code != OpenErrUnsupportedOperation {
		t.Errorf("错误类型/码错误: %T %v", err, err)
	}

	// 技能集成
	skill := NewSkillIntegration(stubSkillSource{decls: []OpenSkillDecl{{ID: "s1", Name: "echo"}}})
	card := &OpenAgentCard{Name: "n"}
	skill.EnrichCard(card)
	if len(card.Skills) != 1 || card.Skills[0].ID != "s1" {
		t.Errorf("EnrichCard 未填充技能: %+v", card.Skills)
	}
}

// ===== interop_router.go =====

// TestCoverInteropRouter_EndpointsAndPolling 端点视图 + GetTask/CancelTask 轮询。
func TestCoverInteropRouter_EndpointsAndPolling(t *testing.T) {
	t.Parallel()
	alive := interopTestServer(t, true)
	defer alive.Close()
	router := NewOpenInteropRouter(InteropRouterConfig{Endpoints: []string{alive.URL}})

	eps := router.Endpoints()
	if len(eps) != 1 || eps[0] != alive.URL {
		t.Errorf("Endpoints = %v", eps)
	}
	task, err := router.GetTask(context.Background(), "task-1")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.ID != "task-1" {
		t.Errorf("task = %+v", task)
	}
	if err := router.CancelTask(context.Background(), "task-1"); err != nil {
		t.Fatalf("CancelTask: %v", err)
	}
}

// TestCoverInteropRouter_AllDownGetCancel 全部端点故障时 Get/Cancel 均报错。
func TestCoverInteropRouter_AllDownGetCancel(t *testing.T) {
	t.Parallel()
	dead := interopTestServer(t, false)
	defer dead.Close()
	router := NewOpenInteropRouter(InteropRouterConfig{
		Endpoints: []string{dead.URL}, FailureThreshold: 1, CircuitTimeout: 100 * time.Millisecond,
	})
	if _, err := router.GetTask(context.Background(), "t"); err == nil {
		t.Error("全部故障时 GetTask 应报错")
	}
	if err := router.CancelTask(context.Background(), "t"); err == nil {
		t.Error("全部故障时 CancelTask 应报错")
	}
}

// ===== interop_server.go =====

// TestCoverInteropServer_ExecutorError 执行器返回错误 → 任务标记 failed。
//
// 注意：直接调用 executeTask（而非经 HTTP tasks/send）。经 HTTP 触发会命中
// 生产数据竞争（handleTaskSend 在锁外序列化 task，interop_server.go:132，
// 与 executeTask 的锁内写 interop_server.go:143/145 冲突）——该竞争已在
// 报告中记录，本测试按其修复前的形态绕过响应序列化路径。
func TestCoverInteropServer_ExecutorError(t *testing.T) {
	t.Parallel()
	card := OpenAgentCard{Name: "n", URL: "http://x", Version: "1.0"}
	srv := NewOpenInteropServer(card, DefaultInteropConfig()).
		WithExecutor(NewSimpleTaskExecutor(func(_ context.Context, task *OpenTask) (*OpenTask, error) {
			return nil, fmt.Errorf("boom")
		}))
	if srv.executor == nil {
		t.Fatal("WithExecutor 应注入执行器")
	}

	task := &OpenTask{ID: "t-err", Status: OpenTaskStatus{State: OpenTaskWorking}}
	srv.mu.Lock()
	srv.tasks[task.ID] = task
	srv.mu.Unlock()

	srv.executeTask(task) // 同步执行：错误 → failed 终态

	srv.mu.RLock()
	got := srv.tasks[task.ID]
	srv.mu.RUnlock()
	if got.Status.State != OpenTaskFailed {
		t.Errorf("执行器错误应将任务标记 failed, got %q", got.Status.State)
	}
}

// TestCoverInteropServer_EchoExecutorFullFlow Echo 执行器完整流转：
// 执行完成 → completed + artifact；SSE 端点对终态任务立即返回并关流。
func TestCoverInteropServer_EchoExecutorFullFlow(t *testing.T) {
	t.Parallel()
	card := OpenAgentCard{Name: "echo", URL: "http://x", Version: "1.0"}
	srv := NewOpenInteropServer(card, DefaultInteropConfig()).WithExecutor(NewEchoTaskExecutor())

	// 1. 同步执行 Echo（不经过 tasks/send，避开上述生产竞争的响应序列化路径）
	task := &OpenTask{
		ID:       "echo-1",
		Status:   OpenTaskStatus{State: OpenTaskWorking},
		Messages: []OpenMessage{NewTextMessage("user", "ping")},
	}
	srv.mu.Lock()
	srv.tasks[task.ID] = task
	srv.mu.Unlock()
	srv.executeTask(task)

	srv.mu.RLock()
	done := srv.tasks[task.ID]
	srv.mu.RUnlock()
	if done.Status.State != OpenTaskCompleted {
		t.Fatalf("Echo 应完成, got %q", done.Status.State)
	}
	if len(done.Artifacts) != 1 || done.Artifacts[0].Parts[0].Text != "Echo: ping" {
		t.Fatalf("Echo 产出物错误: %+v", done.Artifacts)
	}

	// 2. SSE 端点：终态任务应立即返回当前状态并关闭
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/a2a/v1/tasks/"+task.ID+"/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("Content-Type = %q", resp.Header.Get("Content-Type"))
	}
	scanner := bufio.NewScanner(resp.Body)
	var frame strings.Builder
	for scanner.Scan() {
		frame.WriteString(scanner.Text())
		frame.WriteString("\n")
	}
	if !strings.Contains(frame.String(), "event: task") {
		t.Errorf("SSE 应推送当前任务状态: %q", frame.String())
	}
}

// TestCoverInteropServer_SSESubscribeLive 非终态任务订阅 → 广播事件送达订阅者。
//
// 广播经 srv.broadcastTaskEvent 直接触发（subMu 保护；与 SSE handler 的
// 初始读/通道等待正确同步）。若改经 executeTask 触发，会命中生产竞争：
// handleTaskEvents 在 RUnlock 后读 task（interop_server.go:241）与
// executeTask 锁内写（interop_server.go:145）冲突——详见最终报告。
func TestCoverInteropServer_SSESubscribeLive(t *testing.T) {
	t.Parallel()
	card := OpenAgentCard{Name: "live", URL: "http://x", Version: "1.0"}
	srv := NewOpenInteropServer(card, DefaultInteropConfig())

	// 预置非终态任务
	task := &OpenTask{
		ID:       "live-1",
		Status:   OpenTaskStatus{State: OpenTaskWorking},
		Messages: []OpenMessage{NewTextMessage("user", "x")},
	}
	srv.mu.Lock()
	srv.tasks[task.ID] = task
	srv.mu.Unlock()

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/a2a/v1/tasks/live-1/events", nil)
	sseResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer sseResp.Body.Close()

	// 第一帧：订阅时下发的当前状态（working）。SSE 帧以空行分隔，跳过空行。
	scanner := bufio.NewScanner(sseResp.Body)
	if !scanFrameLine(scanner, "event: task") {
		t.Fatalf("应收到当前状态帧（err=%v）", scanner.Err())
	}
	if !scanFrameLine(scanner, `"state":"working"`) {
		t.Fatalf("初始帧应含 working 状态（err=%v）", scanner.Err())
	}

	// 广播一个携带完成态的独立 task 对象（同 ID；不触碰 srv.tasks 中的共享
	// 对象——handler 在 RUnlock 后读 task，见 interop_server.go:241，任何
	// 对共享 task 的并发写都是生产竞争，详见最终报告）
	completed := &OpenTask{
		ID:       "live-1",
		Status:   OpenTaskStatus{State: OpenTaskCompleted},
		Messages: []OpenMessage{NewTextMessage("user", "x")},
	}
	srv.broadcastTaskEvent(completed)

	// 第二帧：广播内容（completed）
	if !scanFrameLine(scanner, "event: task") {
		t.Fatalf("应收到广播帧（err=%v）", scanner.Err())
	}
	if !scanFrameLine(scanner, "completed") {
		t.Errorf("广播帧应含 completed 状态（err=%v）", scanner.Err())
	}
}

// scanFrameLine 扫描下一非空行并断言包含 want（SSE 帧间空行跳过）。
func scanFrameLine(scanner *bufio.Scanner, want string) bool {
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue // 帧分隔空行
		}
		return strings.Contains(line, want)
	}
	return false
}

// TestCoverInteropServer_ProtocolErrors JSON-RPC 协议错误分支表驱动。
func TestCoverInteropServer_ProtocolErrors(t *testing.T) {
	t.Parallel()
	ts, _ := newTestInteropServer(t)
	post := func(payload string) map[string]any {
		resp, err := http.Post(ts.URL+"/a2a/v1", "application/json", strings.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var m map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
			t.Fatal(err)
		}
		return m
	}

	// 坏 JSON → Parse error
	m := post(`{broken`)
	if errObj, ok := m["error"].(map[string]any); !ok || errObj["code"].(float64) != -32700 {
		t.Errorf("坏 JSON 应返回 Parse error: %v", m)
	}
	// 未知方法 → Method not found（-32601）
	m = post(`{"jsonrpc":"2.0","id":7,"method":"tasks/unknown"}`)
	if errObj, ok := m["error"].(map[string]any); !ok || errObj["code"].(float64) != -32601 {
		t.Errorf("未知方法应返回 -32601: %v", m)
	}
	// 取消不存在任务 → Task not found（-32001）
	m = post(`{"jsonrpc":"2.0","id":8,"method":"tasks/cancel","params":{"taskId":"nope"}}`)
	if errObj, ok := m["error"].(map[string]any); !ok || errObj["code"].(float64) != -32001 {
		t.Errorf("取消不存在任务应返回 -32001: %v", m)
	}
	// 查询不存在任务 → Task not found
	m = post(`{"jsonrpc":"2.0","id":9,"method":"tasks/get","params":{"taskId":"nope"}}`)
	if errObj, ok := m["error"].(map[string]any); !ok || errObj["code"].(float64) != -32001 {
		t.Errorf("查询不存在任务应返回 -32001: %v", m)
	}
	// Agent Card 端点非 GET → 405
	resp, err := http.Post(ts.URL+"/.well-known/agent.json", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("非 GET 应 405, got %d", resp.StatusCode)
	}
}

// TestCoverInteropServer_AgentCardNotExposed 未暴露 card 时端点 404。
func TestCoverInteropServer_AgentCardNotExposed(t *testing.T) {
	t.Parallel()
	srv := NewOpenInteropServer(OpenAgentCard{Name: "hidden"}, InteropConfig{ExposeAgentCard: false})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	resp, err := http.Get(ts.URL + "/.well-known/agent.json")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("未暴露时应 404, got %d", resp.StatusCode)
	}
}

// ===== interop_errors.go =====

// TestCoverStandardErrorMessage_AllCodes 全部标准错误码消息与默认分支。
func TestCoverStandardErrorMessage_AllCodes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		code OpenErrorCode
		want string
	}{
		{OpenErrParseError, "Parse error"},
		{OpenErrInvalidRequest, "Invalid Request"},
		{OpenErrMethodNotFound, "Method not found"},
		{OpenErrInvalidParams, "Invalid params"},
		{OpenErrInternal, "Internal error"},
		{OpenErrTaskNotFound, "Task not found"},
		{OpenErrTaskAlreadyCanceled, "Task already canceled"},
		{OpenErrPushNotSupported, "Push notification not supported"},
		{OpenErrUnsupportedOperation, "Unsupported operation"},
		{OpenErrorCode(-99999), "Unknown error"},
	}
	for _, tt := range tests {
		if got := StandardErrorMessage(tt.code); got != tt.want {
			t.Errorf("StandardErrorMessage(%d) = %q, want %q", tt.code, got, tt.want)
		}
	}
}

// ===== interop_client.go =====

// TestCoverInteropClient_ErrorPaths 客户端错误路径：连接拒绝 / 非 JSON 卡片 /
// RPC 错误对象 / 结果类型不匹配。
func TestCoverInteropClient_ErrorPaths(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// 连接拒绝（监听一个端口后立即关闭）
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()
	deadClient := NewOpenInteropClient(deadURL)
	if _, err := deadClient.FetchAgentCard(ctx); err == nil {
		t.Error("连接拒绝应报错")
	}

	// 200 但卡片非 JSON
	badCard := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer badCard.Close()
	if _, err := NewOpenInteropClient(badCard.URL).FetchAgentCard(ctx); err == nil {
		t.Error("卡片非 JSON 应报错")
	}

	// 卡片端点非 200
	notOK := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer notOK.Close()
	if _, err := NewOpenInteropClient(notOK.URL).FetchAgentCard(ctx); err == nil {
		t.Error("非 200 应报错")
	}

	// RPC 返回错误对象 → *OpenError
	rpcErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","error":{"code":-32001,"message":"nope"}}`))
	}))
	defer rpcErr.Close()
	if _, err := NewOpenInteropClient(rpcErr.URL).GetTask(ctx, "t"); err == nil {
		t.Error("RPC 错误对象应报错")
	}

	// result 反序列化失败（id 为数字而非字符串）
	badResult := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":{"id":123}}`))
	}))
	defer badResult.Close()
	if _, err := NewOpenInteropClient(badResult.URL).GetTask(ctx, "t"); err == nil {
		t.Error("result 类型不匹配应报错")
	}
}

// ===== client.go（SSE 流） =====

// TestCoverClient_Options 客户端选项注入（logger/auth/apiKey/bearerToken）。
func TestCoverClient_Options(t *testing.T) {
	t.Parallel()
	logger := slogDiscard()
	auth := NewNoopAuthenticator()
	c := NewA2AClient("http://x/", WithClientLogger(logger), WithClientAuth(auth),
		WithClientAPIKey("k1"), WithClientBearerToken("tok"))
	if c.logger != logger {
		t.Error("logger 未注入")
	}
	if c.auth != auth {
		t.Error("auth 未注入")
	}
	if c.apiKey != "k1" || c.bearerToken != "tok" {
		t.Errorf("凭据未注入: %q %q", c.apiKey, c.bearerToken)
	}
	if c.baseURL != "http://x" {
		t.Errorf("baseURL 应去除尾斜杠: %q", c.baseURL)
	}
}

// TestCoverClient_StreamEvents SSE 流：多帧顺序到达 + 畸形帧跳过 + 通道关闭。
func TestCoverClient_StreamEvents(t *testing.T) {
	t.Parallel()
	frames := []string{
		`{"type":"state_change","task_id":"t1","state":"working"}`,
		`{this is not json`, // 畸形帧：readSSEStream 静默跳过
		`{"type":"artifact","task_id":"t1"}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tasks/t1/events" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		for _, f := range frames {
			fmt.Fprintf(w, "data: %s\n\n", f)
			if fl != nil {
				fl.Flush()
			}
		}
	}))
	defer srv.Close()

	c := NewA2AClient(srv.URL, WithClientAPIKey("key"), WithClientBearerToken("tok"))
	ch, err := c.StreamEvents("t1")
	if err != nil {
		t.Fatalf("StreamEvents: %v", err)
	}
	var got []TaskEventType
	timeout := time.After(3 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				goto done // 流关闭
			}
			got = append(got, ev.Type)
		case <-timeout:
			t.Fatal("SSE 流读取超时")
		}
	}
done:
	if len(got) != 2 {
		t.Fatalf("应收到 2 个有效事件（畸形帧被跳过）, got %v", got)
	}
	if got[0] != EventStateChange || got[1] != EventArtifact {
		t.Errorf("事件顺序/类型错误: %v", got)
	}
}

// TestCoverClient_StreamEvents_NonOK 非 200 状态 → 连接错误（body 已关闭）。
func TestCoverClient_StreamEvents_NonOK(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	c := NewA2AClient(srv.URL)
	if _, err := c.StreamEvents("t1"); err == nil {
		t.Fatal("非 200 应返回错误")
	}
}

// TestCoverClient_ReadSSEStream_MultiLineData 多行 data 拼接为单帧载荷。
func TestCoverClient_ReadSSEStream_MultiLineData(t *testing.T) {
	t.Parallel()
	c := NewA2AClient("http://unused")
	ch := make(chan *TaskEvent, 4)
	// 两行 data 拼成 {"type":"error",...} 后被空行分隔成帧
	stream := "data: {\"type\":\"error\",\n" +
		"data: \"task_id\":\"t9\",\"error\":\"x\"}\n\n"
	c.readSSEStream(strings.NewReader(stream), ch)
	close(ch)
	select {
	case ev := <-ch:
		if ev.Type != EventError || ev.TaskID != "t9" {
			t.Errorf("多行 data 拼接错误: %+v", ev)
		}
	default:
		t.Fatal("应解析出 1 个事件")
	}
}

// ===== registry_file.go =====

// TestCoverFileRegistry_Lifecycle 文件注册表全生命周期：
// 注册/查询/列表/注销通知/重新加载/损坏文件/关闭。
func TestCoverFileRegistry_Lifecycle(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "registry.json")

	r, err := NewFileRegistry(path)
	if err != nil {
		t.Fatalf("NewFileRegistry: %v", err)
	}
	// 空表查询
	if _, err := r.Resolve("ghost"); err == nil {
		t.Error("查询不存在 agent 应报错")
	}
	// 注册 + Watch 订阅
	watch := r.Watch()
	card := NewAgentCard("agent-1", "Agent One")
	card.Endpoints = AgentEndpoints{BaseURL: "http://a1"}
	if err := r.Register(card); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got, err := r.Resolve("agent-1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Card.Name != "Agent One" {
		t.Errorf("解析结果错误: %+v", got)
	}
	// 注意：FileRegistry.Register 只填 Card/SeenAt，不填 Endpoints 字段
	// （registry_file.go:59-62，与 LocalDiscovery.Register 行为不一致，
	// save/load 后 AgentRegistry.Endpoints 丢失——Card.Endpoints 仍保留）。
	// 此处按现状断言 Card 内端点。
	if got.Card.Endpoints.BaseURL != "http://a1" {
		t.Errorf("Card.Endpoints 应保留: %+v", got.Card.Endpoints)
	}
	if len(r.List()) != 1 {
		t.Errorf("List = %d, want 1", len(r.List()))
	}
	// 注销 → 观察者收到 deregistered 事件
	if err := r.Deregister("agent-1"); err != nil {
		t.Fatalf("Deregister: %v", err)
	}
	select {
	case ev := <-watch:
		if ev.Type != EventAgentDeregistered || ev.AgentID != "agent-1" {
			t.Errorf("事件错误: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("未收到注销事件")
	}
	// 关闭
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	// 第二个实例从同一文件加载（load 路径）
	card2 := NewAgentCard("agent-2", "Agent Two")
	if err := r2Register(path, card2); err != nil {
		t.Fatal(err)
	}
	r2, err := NewFileRegistry(path)
	if err != nil {
		t.Fatalf("二次加载: %v", err)
	}
	if _, err := r2.Resolve("agent-2"); err != nil {
		t.Errorf("应从文件恢复 agent-2: %v", err)
	}
	_ = r2.Close()
}

// r2Register 辅助：独立注册一个 agent 并持久化。
func r2Register(path string, card *AgentCard) error {
	r, err := NewFileRegistry(path)
	if err != nil {
		return err
	}
	defer r.Close()
	return r.Register(card)
}

// TestCoverFileRegistry_CorruptFile 注册表文件损坏 → 构造报错。
func TestCoverFileRegistry_CorruptFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "registry.json")
	if err := os.WriteFile(path, []byte("{corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileRegistry(path); err == nil {
		t.Fatal("损坏的注册表文件应报错")
	}
}

// ===== types.go =====

// TestCoverA2AMessage_UnmarshalPartTypes part 类型分派与回退路径。
func TestCoverA2AMessage_UnmarshalPartTypes(t *testing.T) {
	t.Parallel()
	raw := `{"role":"user","parts":[
		{"type":"text","text":"hi"},
		{"type":"file","file":{"name":"a.bin","mime_type":"application/octet-stream","bytes":"eHg="}},
		{"type":"file","file_uri":{"uri":"https://x/a","mime_type":"text/plain"}},
		{"type":"data","data":{"k":1}},
		{"type":"unknown-kind","text":"fallback"},
		123,
		{"type":"text","text":123}
	]}`
	var msg A2AMessage
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if len(msg.Parts) != 7 {
		t.Fatalf("parts = %d, want 7", len(msg.Parts))
	}
	if tp, ok := msg.Parts[0].(TextPart); !ok || tp.Text != "hi" {
		t.Errorf("text part 错误: %+v", msg.Parts[0])
	}
	fp, ok := msg.Parts[1].(FilePart)
	if !ok || fp.File == nil || fp.File.Name != "a.bin" || fp.File.Bytes != "eHg=" {
		t.Errorf("file(bytes) part 错误: %+v", msg.Parts[1])
	}
	fp2, ok := msg.Parts[2].(FilePart)
	if !ok || fp2.FileURI == nil || fp2.FileURI.URI != "https://x/a" {
		t.Errorf("file(uri) part 错误: %+v", msg.Parts[2])
	}
	if dp, ok := msg.Parts[3].(DataPart); !ok || string(dp.Data) != `{"k":1}` {
		t.Errorf("data part 错误: %+v", msg.Parts[3])
	}
	// 未知类型 → 回退 TextPart
	if _, ok := msg.Parts[4].(TextPart); !ok {
		t.Errorf("未知类型应回退 TextPart: %T", msg.Parts[4])
	}
	// 非对象 part（数字）→ 类型提示解析失败 → 回退 TextPart（零值）
	if _, ok := msg.Parts[5].(TextPart); !ok {
		t.Errorf("非对象 part 应回退 TextPart: %T", msg.Parts[5])
	}
	// text part 字段类型错误 → 回退 TextPart{TypeField:"text"}
	if tp, ok := msg.Parts[6].(TextPart); !ok || tp.TypeField != "text" {
		t.Errorf("畸形 text part 应回退 TextPart: %T %+v", msg.Parts[6], msg.Parts[6])
	}

	// 整体非法 JSON → 错误
	var bad A2AMessage
	if err := json.Unmarshal([]byte("{oops"), &bad); err == nil {
		t.Error("非法 JSON 应报错")
	}
}

// TestCoverNewFilePartFromURI 与 FilePart.Type 访问器。
func TestCoverNewFilePartFromURI(t *testing.T) {
	t.Parallel()
	fp := NewFilePartFromURI("https://x/f", "text/plain")
	if fp.Type() != "file" {
		t.Errorf("Type() = %q", fp.Type())
	}
	if fp.FileURI == nil || fp.FileURI.URI != "https://x/f" || fp.FileURI.MimeType != "text/plain" {
		t.Errorf("FileURI 错误: %+v", fp.FileURI)
	}
	if NewTextPart("x").Type() != "text" || NewDataPart(nil).Type() != "data" {
		t.Error("Part Type 访问器错误")
	}
}

// TestCoverIsValidTransition_UnknownStates 未知状态的转移判定（双假）。
func TestCoverIsValidTransition_UnknownStates(t *testing.T) {
	t.Parallel()
	if IsValidTransition(TaskState("bogus"), TaskWorking) {
		t.Error("未知 from 状态不应有合法转移")
	}
	if IsValidTransition(TaskSubmitted, TaskState("bogus")) {
		t.Error("未知 to 状态不应合法")
	}
	if !IsValidTransition(TaskSubmitted, TaskWorking) {
		t.Error("submitted→working 应合法")
	}
}
