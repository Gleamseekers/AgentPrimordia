package debugger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent"
	"github.com/Gleamseekers/AgentPrimordia/internal/orchestration"
)

func TestVisualEditor_NewVisualEditor(t *testing.T) {
	editor := NewVisualEditor()
	if editor == nil {
		t.Fatal("Expected non-nil editor")
	}
	if editor.configs == nil {
		t.Error("Expected configs map to be initialized")
	}
	if editor.orchestrators == nil {
		t.Error("Expected orchestrators map to be initialized")
	}
	if editor.executions == nil {
		t.Error("Expected executions map to be initialized")
	}
}

func TestVisualEditorServer_ListConfigs_Empty(t *testing.T) {
	editor := NewVisualEditor()
	server := NewVisualEditorServer(editor)

	req := httptest.NewRequest(http.MethodGet, "/api/editor/configs", nil)
	w := httptest.NewRecorder()

	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var configs []EditorConfig
	if err := json.Unmarshal(w.Body.Bytes(), &configs); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if len(configs) != 0 {
		t.Errorf("Expected 0 configs, got %d", len(configs))
	}
}

func TestVisualEditorServer_CreateConfig(t *testing.T) {
	editor := NewVisualEditor()
	server := NewVisualEditorServer(editor)

	cfg := EditorConfig{
		Name:        "Test Workflow",
		Description: "A test workflow",
		Mode:        orchestration.DAGMode,
		Nodes: []WorkflowNode{
			{
				ID:       "start",
				Type:     "start",
				Name:     "Start",
				Position: NodePosition{X: 100, Y: 100},
			},
			{
				ID:       "agent1",
				Type:     "agent",
				Name:     "Agent 1",
				Position: NodePosition{X: 300, Y: 100},
				Config: map[string]interface{}{
					"prompt": "Hello",
				},
			},
		},
		Edges: []WorkflowEdge{
			{
				ID:     "edge1",
				Source: "start",
				Target: "agent1",
			},
		},
	}

	body, _ := json.Marshal(cfg)
	req := httptest.NewRequest(http.MethodPost, "/api/editor/configs", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected status 201, got %d", w.Code)
	}

	var created EditorConfig
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if created.ID == "" {
		t.Error("Expected ID to be generated")
	}
	if created.Name != cfg.Name {
		t.Errorf("Expected name %s, got %s", cfg.Name, created.Name)
	}
	if created.CreatedAt.IsZero() {
		t.Error("Expected CreatedAt to be set")
	}
	if len(created.Nodes) != 2 {
		t.Errorf("Expected 2 nodes, got %d", len(created.Nodes))
	}
}

func TestVisualEditorServer_GetConfig(t *testing.T) {
	editor := NewVisualEditor()
	server := NewVisualEditorServer(editor)

	// 先创建一个配置
	cfg := EditorConfig{
		Name: "Test Workflow",
		Mode: orchestration.SequentialMode,
	}
	body, _ := json.Marshal(cfg)
	req := httptest.NewRequest(http.MethodPost, "/api/editor/configs", bytes.NewReader(body))
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	var created EditorConfig
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	// 获取配置
	req = httptest.NewRequest(http.MethodGet, "/api/editor/config/"+created.ID, nil)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var fetched EditorConfig
	if err := json.Unmarshal(w.Body.Bytes(), &fetched); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if fetched.ID != created.ID {
		t.Errorf("Expected ID %s, got %s", created.ID, fetched.ID)
	}
}

func TestVisualEditorServer_UpdateConfig(t *testing.T) {
	editor := NewVisualEditor()
	server := NewVisualEditorServer(editor)

	// 创建配置
	cfg := EditorConfig{Name: "Original"}
	body, _ := json.Marshal(cfg)
	req := httptest.NewRequest(http.MethodPost, "/api/editor/configs", bytes.NewReader(body))
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	var created EditorConfig
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	// 更新配置
	updated := EditorConfig{
		Name:        "Updated",
		Description: "Updated description",
		Mode:        orchestration.ParallelMode,
	}
	body, _ = json.Marshal(updated)
	req = httptest.NewRequest(http.MethodPut, "/api/editor/config/"+created.ID, bytes.NewReader(body))
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var result EditorConfig
	_ = json.Unmarshal(w.Body.Bytes(), &result)

	if result.Name != "Updated" {
		t.Errorf("Expected name 'Updated', got %s", result.Name)
	}
	if result.Description != "Updated description" {
		t.Errorf("Expected description 'Updated description', got %s", result.Description)
	}
	if result.Mode != orchestration.ParallelMode {
		t.Errorf("Expected mode %s, got %s", orchestration.ParallelMode, result.Mode)
	}
}

func TestVisualEditorServer_DeleteConfig(t *testing.T) {
	editor := NewVisualEditor()
	server := NewVisualEditorServer(editor)

	// 创建配置
	cfg := EditorConfig{Name: "To Delete"}
	body, _ := json.Marshal(cfg)
	req := httptest.NewRequest(http.MethodPost, "/api/editor/configs", bytes.NewReader(body))
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	var created EditorConfig
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	// 删除配置
	req = httptest.NewRequest(http.MethodDelete, "/api/editor/config/"+created.ID, nil)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("Expected status 204, got %d", w.Code)
	}

	// 验证已删除
	req = httptest.NewRequest(http.MethodGet, "/api/editor/config/"+created.ID, nil)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestVisualEditorServer_ExecuteConfig(t *testing.T) {
	editor := NewVisualEditor()
	server := NewVisualEditorServer(editor)

	// 创建配置
	cfg := EditorConfig{Name: "Execute Test"}
	body, _ := json.Marshal(cfg)
	req := httptest.NewRequest(http.MethodPost, "/api/editor/configs", bytes.NewReader(body))
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	var created EditorConfig
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	// 执行配置
	req = httptest.NewRequest(http.MethodPost, "/api/editor/execute/"+created.ID, nil)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("Expected status 202, got %d", w.Code)
	}

	var result map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &result)

	if result["execution_id"] == "" {
		t.Error("Expected execution_id to be returned")
	}
	if result["status"] != "started" {
		t.Errorf("Expected status 'started', got %s", result["status"])
	}
}

func TestVisualEditorServer_ListExecutions(t *testing.T) {
	editor := NewVisualEditor()
	server := NewVisualEditorServer(editor)

	// 添加一个执行记录
	exec := &ExecutionRecord{
		ID:        "exec1",
		ConfigID:  "cfg1",
		Status:    orchestration.StatusRunning,
		StartTime: time.Now(),
	}
	editor.mu.Lock()
	editor.executions["exec1"] = exec
	editor.mu.Unlock()

	// 列出执行
	req := httptest.NewRequest(http.MethodGet, "/api/editor/executions", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var executions []ExecutionRecord
	if err := json.Unmarshal(w.Body.Bytes(), &executions); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if len(executions) != 1 {
		t.Errorf("Expected 1 execution, got %d", len(executions))
	}
}

func TestVisualEditorServer_GetExecution(t *testing.T) {
	editor := NewVisualEditor()
	server := NewVisualEditorServer(editor)

	// 添加执行记录
	exec := &ExecutionRecord{
		ID:        "exec1",
		ConfigID:  "cfg1",
		Status:    orchestration.StatusCompleted,
		StartTime: time.Now().Add(-time.Minute),
		EndTime:   time.Now(),
		Duration:  time.Minute,
	}
	editor.mu.Lock()
	editor.executions["exec1"] = exec
	editor.mu.Unlock()

	// 获取执行
	req := httptest.NewRequest(http.MethodGet, "/api/editor/execution/exec1", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var result ExecutionRecord
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if result.ID != "exec1" {
		t.Errorf("Expected ID 'exec1', got %s", result.ID)
	}
}

func TestVisualEditorServer_EditorUI(t *testing.T) {
	editor := NewVisualEditor()
	server := NewVisualEditorServer(editor)

	req := httptest.NewRequest(http.MethodGet, "/editor", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	if contentType := w.Header().Get("Content-Type"); contentType != "text/html; charset=utf-8" {
		t.Errorf("Expected Content-Type 'text/html; charset=utf-8', got %s", contentType)
	}

	body := w.Body.String()
	if len(body) == 0 {
		t.Error("Expected non-empty HTML body")
	}
}

func TestVisualEditorServer_MethodNotAllowed(t *testing.T) {
	editor := NewVisualEditor()
	server := NewVisualEditorServer(editor)

	// 尝试用GET方法创建配置
	req := httptest.NewRequest(http.MethodGet, "/api/editor/configs", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	// GET应该是允许的（列出配置），所以不会返回405
	if w.Code == http.StatusMethodNotAllowed {
		t.Error("GET /api/editor/configs should be allowed")
	}
}

func TestVisualEditorServer_NotFound(t *testing.T) {
	editor := NewVisualEditor()
	server := NewVisualEditorServer(editor)

	// 获取不存在的配置
	req := httptest.NewRequest(http.MethodGet, "/api/editor/config/nonexistent", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

// ===== Task A-1: 异步执行编排测试 =====

// TestVisualEditor_AsyncExecution_Completed 验证异步执行正常完成
func TestVisualEditor_AsyncExecution_Completed(t *testing.T) {
	editor := NewVisualEditor()
	server := NewVisualEditorServer(editor)

	// 创建包含 agent 节点的配置
	cfg := EditorConfig{
		Name: "Async Test",
		Mode: orchestration.SequentialMode,
		Nodes: []WorkflowNode{
			{ID: "start", Type: "start", Name: "Start"},
			{ID: "agent1", Type: "agent", Name: "Agent One",
				Config: map[string]interface{}{"prompt": "hello world"}},
		},
		Edges: []WorkflowEdge{{ID: "e1", Source: "start", Target: "agent1"}},
	}
	body, _ := json.Marshal(cfg)
	req := httptest.NewRequest(http.MethodPost, "/api/editor/configs", bytes.NewReader(body))
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	var created EditorConfig
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	// 触发执行
	req = httptest.NewRequest(http.MethodPost, "/api/editor/execute/"+created.ID, nil)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("Expected 202, got %d", w.Code)
	}

	var execResp map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &execResp)
	execID := execResp["execution_id"]

	// 等待异步执行完成
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("异步执行超时（5s）")
		default:
		}

		editor.mu.RLock()
		exec := editor.executions[execID]
		status := exec.Status
		editor.mu.RUnlock()

		if status == orchestration.StatusCompleted {
			break
		}
		if status == orchestration.StatusFailed {
			editor.mu.RLock()
			errMsg := exec.Error
			editor.mu.RUnlock()
			t.Fatalf("执行失败: %s", errMsg)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// 验证执行结果
	editor.mu.RLock()
	exec := editor.executions[execID]
	editor.mu.RUnlock()

	if exec.EndTime.IsZero() {
		t.Error("期望 EndTime 已设置")
	}
	if exec.EndTime.Before(exec.StartTime) {
		t.Error("期望 EndTime 不早于 StartTime")
	}
	if len(exec.StepResults) == 0 {
		t.Error("期望 StepResults 非空")
	}
	// 验证 echoAgent 产生了输出
	if step, ok := exec.StepResults["agent1"]; ok {
		if step.Status != orchestration.StepCompleted {
			t.Errorf("期望步骤状态 completed，得到 %s", step.Status)
		}
	} else {
		t.Error("期望找到 agent1 的步骤结果")
	}
}

// TestVisualEditor_AsyncExecution_WithRegisteredAgent 验证注册真实 Agent 后的执行
func TestVisualEditor_AsyncExecution_WithRegisteredAgent(t *testing.T) {
	editor := NewVisualEditor()
	server := NewVisualEditorServer(editor)

	// 注册自定义 Agent
	editor.RegisterAgent("agent1", &mockRunAgent{
		name:     "custom-agent",
		response: "自定义响应内容",
	})

	cfg := EditorConfig{
		Name: "Registered Agent Test",
		Mode: orchestration.SequentialMode,
		Nodes: []WorkflowNode{
			{ID: "agent1", Type: "agent", Name: "Custom Agent",
				Config: map[string]interface{}{"prompt": "test prompt"}},
		},
	}
	body, _ := json.Marshal(cfg)
	req := httptest.NewRequest(http.MethodPost, "/api/editor/configs", bytes.NewReader(body))
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	var created EditorConfig
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	req = httptest.NewRequest(http.MethodPost, "/api/editor/execute/"+created.ID, nil)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	var execResp map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &execResp)
	execID := execResp["execution_id"]

	// 等待完成
	waitForExecution(t, editor, execID, 5*time.Second)

	editor.mu.RLock()
	exec := editor.executions[execID]
	editor.mu.RUnlock()

	if exec.Status != orchestration.StatusCompleted {
		t.Fatalf("期望 completed，得到 %s (error: %s)", exec.Status, exec.Error)
	}

	step := exec.StepResults["agent1"]
	if step == nil {
		t.Fatal("期望找到 agent1 步骤结果")
	}
	if step.Response == nil || step.Response.Content != "自定义响应内容" {
		t.Errorf("期望自定义响应，得到 %+v", step.Response)
	}
}

// TestVisualEditor_AsyncExecution_Failure 验证执行失败路径
func TestVisualEditor_AsyncExecution_Failure(t *testing.T) {
	editor := NewVisualEditor()
	server := NewVisualEditorServer(editor)

	// 注册一个会返回错误的 Agent
	editor.RegisterAgent("fail-agent", &failingAgent{name: "failer"})

	cfg := EditorConfig{
		Name: "Failure Test",
		Mode: orchestration.SequentialMode,
		Nodes: []WorkflowNode{
			{ID: "fail-agent", Type: "agent", Name: "Failing Agent",
				Config: map[string]interface{}{"prompt": "trigger error"}},
		},
	}
	body, _ := json.Marshal(cfg)
	req := httptest.NewRequest(http.MethodPost, "/api/editor/configs", bytes.NewReader(body))
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	var created EditorConfig
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	req = httptest.NewRequest(http.MethodPost, "/api/editor/execute/"+created.ID, nil)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	var execResp map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &execResp)
	execID := execResp["execution_id"]

	// 等待执行完成（应为失败，重试机制可能耗时较长）
	waitForExecution(t, editor, execID, 15*time.Second)

	editor.mu.RLock()
	exec := editor.executions[execID]
	editor.mu.RUnlock()

	if exec.Status != orchestration.StatusFailed {
		t.Errorf("期望 failed，得到 %s", exec.Status)
	}
	if exec.Error == "" {
		t.Error("期望错误信息非空")
	}
}

// TestVisualEditor_AsyncExecution_ConcurrentQuery 验证执行期间并发查询安全性
func TestVisualEditor_AsyncExecution_ConcurrentQuery(t *testing.T) {
	editor := NewVisualEditor()
	server := NewVisualEditorServer(editor)

	cfg := EditorConfig{
		Name: "Concurrent Query Test",
		Mode: orchestration.SequentialMode,
		Nodes: []WorkflowNode{
			{ID: "a1", Type: "agent", Name: "A1", Config: map[string]interface{}{"prompt": "p1"}},
			{ID: "a2", Type: "agent", Name: "A2", Config: map[string]interface{}{"prompt": "p2"}},
			{ID: "a3", Type: "agent", Name: "A3", Config: map[string]interface{}{"prompt": "p3"}},
		},
	}
	body, _ := json.Marshal(cfg)
	req := httptest.NewRequest(http.MethodPost, "/api/editor/configs", bytes.NewReader(body))
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	var created EditorConfig
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	req = httptest.NewRequest(http.MethodPost, "/api/editor/execute/"+created.ID, nil)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	var execResp map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &execResp)
	execID := execResp["execution_id"]

	// 并发查询执行状态（模拟轮询）
	var wg sync.WaitGroup
	errCh := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				qReq := httptest.NewRequest(http.MethodGet, "/api/editor/execution/"+execID, nil)
				qW := httptest.NewRecorder()
				server.Handler().ServeHTTP(qW, qReq)
				if qW.Code != http.StatusOK {
					errCh <- fmt.Errorf("查询返回 %d", qW.Code)
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
		}()
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("并发查询错误: %v", err)
	}

	// 最终应完成
	waitForExecution(t, editor, execID, 5*time.Second)
}

// TestVisualEditor_AsyncExecution_MultipleExecutions 验证多次执行独立性
func TestVisualEditor_AsyncExecution_MultipleExecutions(t *testing.T) {
	editor := NewVisualEditor()
	server := NewVisualEditorServer(editor)

	cfg := EditorConfig{
		Name: "Multi Exec Test",
		Mode: orchestration.SequentialMode,
		Nodes: []WorkflowNode{
			{ID: "agent1", Type: "agent", Name: "Agent", Config: map[string]interface{}{"prompt": "hi"}},
		},
	}
	body, _ := json.Marshal(cfg)
	req := httptest.NewRequest(http.MethodPost, "/api/editor/configs", bytes.NewReader(body))
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	var created EditorConfig
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	// 触发 3 次执行
	execIDs := make([]string, 3)
	for i := 0; i < 3; i++ {
		req = httptest.NewRequest(http.MethodPost, "/api/editor/execute/"+created.ID, nil)
		w = httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)

		var resp map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		execIDs[i] = resp["execution_id"]
	}

	// 所有执行 ID 应不同
	if execIDs[0] == execIDs[1] || execIDs[1] == execIDs[2] {
		t.Error("执行 ID 应唯一")
	}

	// 等待所有执行完成
	for _, id := range execIDs {
		waitForExecution(t, editor, id, 5*time.Second)
	}

	// 验证所有执行均成功
	editor.mu.RLock()
	defer editor.mu.RUnlock()
	for _, id := range execIDs {
		exec := editor.executions[id]
		if exec.Status != orchestration.StatusCompleted {
			t.Errorf("执行 %s 期望 completed，得到 %s", id, exec.Status)
		}
	}
}

// TestVisualEditor_BuildOrchestrator_NoAgentNodes 验证无 agent 节点时返回错误
func TestVisualEditor_BuildOrchestrator_NoAgentNodes(t *testing.T) {
	editor := NewVisualEditor()

	cfg := &EditorConfig{
		Name: "Empty Workflow",
		Mode: orchestration.SequentialMode,
		Nodes: []WorkflowNode{
			{ID: "start", Type: "start", Name: "Start"},
			{ID: "end", Type: "end", Name: "End"},
		},
	}

	_, err := editor.buildOrchestrator(cfg)
	if err == nil {
		t.Fatal("期望无 agent 节点时返回错误")
	}
}

// ===== 测试辅助 =====

// waitForExecution 等待执行完成（状态不再是 running）
func waitForExecution(t *testing.T, editor *VisualEditor, execID string, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			t.Fatalf("等待执行 %s 超时", execID)
		default:
		}
		editor.mu.RLock()
		exec := editor.executions[execID]
		status := exec.Status
		editor.mu.RUnlock()

		if status != orchestration.StatusRunning {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
}

// mockRunAgent 可自定义响应的测试 Agent
type mockRunAgent struct {
	name     string
	response string
}

func (a *mockRunAgent) Run(_ context.Context, _ agent.Message) (*agent.Response, error) {
	return &agent.Response{Content: a.response}, nil
}
func (a *mockRunAgent) StreamRun(_ context.Context, _ agent.Message) (<-chan agent.StreamEvent, error) {
	ch := make(chan agent.StreamEvent, 1)
	ch <- agent.StreamEvent{Type: agent.StreamEventComplete, Content: a.response}
	close(ch)
	return ch, nil
}
func (a *mockRunAgent) Stop()                   {}
func (a *mockRunAgent) Stats() agent.AgentStats { return agent.AgentStats{} }
func (a *mockRunAgent) Name() string            { return a.name }

// failingAgent 始终返回错误的测试 Agent
type failingAgent struct {
	name string
}

func (a *failingAgent) Run(_ context.Context, _ agent.Message) (*agent.Response, error) {
	return nil, fmt.Errorf("模拟执行失败: %s", a.name)
}
func (a *failingAgent) StreamRun(_ context.Context, _ agent.Message) (<-chan agent.StreamEvent, error) {
	return nil, fmt.Errorf("模拟流式失败")
}
func (a *failingAgent) Stop()                   {}
func (a *failingAgent) Stats() agent.AgentStats { return agent.AgentStats{} }
func (a *failingAgent) Name() string            { return a.name }
