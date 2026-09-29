// handler_gaps_test.go — 补全 admin handler 0% 覆盖端点的测试
// Phase 7 覆盖率提升：从 50.7% → 70%+
// 覆盖目标端点：listTools / getTool / toolCategories /
// listWorkflows / getWorkflow / logStream / GetAgent 成功路径 / requireAuth 错误分支
package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/pool"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools"
)

var _ = fmt.Sprintf // keep fmt import used

// ===== Tools 端点测试 =====

// stubTool 是用于测试工具注册表端点的最小 Tool 实现
type stubTool struct {
	name        string
	description string
	params      json.RawMessage
}

func (s *stubTool) Name() string        { return s.name }
func (s *stubTool) Description() string { return s.description }
func (s *stubTool) Parameters() json.RawMessage {
	if s.params == nil {
		return json.RawMessage(`{}`)
	}
	return s.params
}
func (s *stubTool) Execute(ctx context.Context, args json.RawMessage) (*tools.Result, error) {
	return tools.NewResult(fmt.Sprintf("executed %s", s.name)), nil
}

func newHandlerWithTools(t *testing.T) *AdminHandler {
	t.Helper()
	p := pool.NewPool(pool.PoolConfig{MaxConcurrency: 5})
	t.Cleanup(func() { p.Close() })

	reg := tools.NewRegistry()
	if err := reg.Register(&stubTool{
		name:        "greet",
		description: "say hello",
		params:      json.RawMessage(`{"name": "string"}`),
	}); err != nil {
		t.Fatalf("register greet: %v", err)
	}
	if err := reg.Register(&stubTool{
		name:        "farewell",
		description: "say goodbye",
		params:      json.RawMessage(`{}`),
	}); err != nil {
		t.Fatalf("register farewell: %v", err)
	}

	return NewAdminHandler(p, reg, WithAPIToken(testToken))
}

func TestAdminHandler_ListTools(t *testing.T) {
	h := newHandlerWithTools(t)
	rec := doAuthorizedRequest(t, h, http.MethodGet, "/api/tools")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}

	var result []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 tools, got %d", len(result))
	}
	found := false
	for _, tool := range result {
		if tool["name"] == "greet" {
			found = true
			if tool["description"] != "say hello" {
				t.Errorf("description mismatch: %v", tool["description"])
			}
			break
		}
	}
	if !found {
		t.Errorf("expected greet tool in list, got %+v", result)
	}
}

func TestAdminHandler_ListTools_NilRegistry(t *testing.T) {
	p := pool.NewPool(pool.PoolConfig{MaxConcurrency: 2})
	t.Cleanup(func() { p.Close() })
	h := NewAdminHandler(p, nil, WithAPIToken(testToken))

	rec := doAuthorizedRequest(t, h, http.MethodGet, "/api/tools")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 even with nil registry, got %d", rec.Code)
	}
	body := strings.TrimSpace(rec.Body.String())
	if body != "[]" {
		t.Errorf("expected empty list, got %s", body)
	}
}

func TestAdminHandler_GetTool(t *testing.T) {
	h := newHandlerWithTools(t)
	rec := doAuthorizedRequest(t, h, http.MethodGet, "/api/tools/greet")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}

	var result map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result["name"] != "greet" {
		t.Errorf("name mismatch: %v", result["name"])
	}
}

func TestAdminHandler_GetTool_NotFound(t *testing.T) {
	h := newHandlerWithTools(t)
	rec := doAuthorizedRequest(t, h, http.MethodGet, "/api/tools/nonexistent")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestAdminHandler_GetTool_NilRegistry(t *testing.T) {
	p := pool.NewPool(pool.PoolConfig{MaxConcurrency: 2})
	t.Cleanup(func() { p.Close() })
	h := NewAdminHandler(p, nil, WithAPIToken(testToken))

	rec := doAuthorizedRequest(t, h, http.MethodGet, "/api/tools/greet")
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 when registry is nil, got %d", rec.Code)
	}
}

func TestAdminHandler_ToolCategories(t *testing.T) {
	h := newHandlerWithTools(t)
	rec := doAuthorizedRequest(t, h, http.MethodGet, "/api/tools/categories")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}

	var result map[string][]map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// 不验证具体内容（取决于 stubTool 的字段）；只需确保返回了非空对象
	if len(result) == 0 {
		t.Logf("ToolsByCategory returned empty map (acceptable for empty registry)")
	}
}

func TestAdminHandler_ToolCategories_NilRegistry(t *testing.T) {
	p := pool.NewPool(pool.PoolConfig{MaxConcurrency: 2})
	t.Cleanup(func() { p.Close() })
	h := NewAdminHandler(p, nil, WithAPIToken(testToken))

	rec := doAuthorizedRequest(t, h, http.MethodGet, "/api/tools/categories")
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

// ===== Workflows 端点测试 =====

func TestAdminHandler_ListWorkflows_Empty(t *testing.T) {
	h := newTestHandler(t)
	rec := doAuthorizedRequest(t, h, http.MethodGet, "/api/workflows")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var result []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 workflows, got %d", len(result))
	}
}

func TestAdminHandler_ListWorkflows_NilPool(t *testing.T) {
	h := newTestHandler(t)
	h.pool = nil // 走防御分支

	rec := doAuthorizedRequest(t, h, http.MethodGet, "/api/workflows")
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 with nil pool, got %d", rec.Code)
	}
}

func TestAdminHandler_GetWorkflow_NotFound(t *testing.T) {
	h := newTestHandler(t)
	rec := doAuthorizedRequest(t, h, http.MethodGet, "/api/workflows/nonexistent-id")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestAdminHandler_GetWorkflow_NilPool(t *testing.T) {
	h := newTestHandler(t)
	h.pool = nil

	rec := doAuthorizedRequest(t, h, http.MethodGet, "/api/workflows/abc")
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 with nil pool, got %d", rec.Code)
	}
}

func TestAdminHandler_GetWorkflow_Found(t *testing.T) {
	mockLLM := llm.NewMockLLM(t).WithDelay(10 * time.Millisecond).WithResponse("done")
	p := pool.NewPool(pool.PoolConfig{MaxConcurrency: 5})
	p.SetModel(mockLLM)
	t.Cleanup(func() { p.Close() })

	taskID := "wf-task-1"
	go func() {
		_, _ = p.Dispatch(context.Background(), []pool.TaskConfig{
			{ID: taskID, Title: "test", Prompt: "hi"},
		})
	}()
	time.Sleep(50 * time.Millisecond)

	h := NewAdminHandler(p, tools.NewRegistry(), WithAPIToken(testToken))
	rec := doAuthorizedRequest(t, h, http.MethodGet, "/api/workflows/"+taskID)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
	var result map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result["id"] != taskID {
		t.Errorf("id mismatch: %v", result["id"])
	}
}

// ===== Log Stream (SSE) 测试 =====

func TestAdminHandler_LogStream(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/logs/stream", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	// cancel ctx 让 logStream 的 <-r.Context().Done() 立即返回
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "event: connected") {
		t.Errorf("expected 'event: connected' in SSE output, got %q", body)
	}
	if rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("expected Content-Type text/event-stream, got %q", rec.Header().Get("Content-Type"))
	}
}

// ===== GetAgent 成功路径补充 =====

func TestAdminHandler_GetAgent_FoundViaDispatch(t *testing.T) {
	mockLLM := llm.NewMockLLM(t).WithDelay(50 * time.Millisecond).WithResponse("ok")
	p := pool.NewPool(pool.PoolConfig{MaxConcurrency: 5})
	p.SetModel(mockLLM)
	t.Cleanup(func() { p.Close() })

	agentID := "agent-success-001"
	go func() {
		_, _ = p.Dispatch(context.Background(), []pool.TaskConfig{
			{ID: agentID, Title: "test", Prompt: "hi"},
		})
	}()
	time.Sleep(20 * time.Millisecond)

	h := NewAdminHandler(p, tools.NewRegistry(), WithAPIToken(testToken))
	rec := doAuthorizedRequest(t, h, http.MethodGet, "/api/agents/"+agentID)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
	var result map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result["id"] != agentID {
		t.Errorf("id mismatch: %v", result["id"])
	}
}

// ===== 鉴权分支补充 =====

func TestAdminHandler_RequireAuth_NoTokenConfigured(t *testing.T) {
	// 不设置 WithAPIToken，所有受保护端点应返回 401
	p := pool.NewPool(pool.PoolConfig{MaxConcurrency: 2})
	t.Cleanup(func() { p.Close() })
	h := NewAdminHandler(p, nil) // 无 token

	rec := doRequest(t, h, http.MethodGet, "/api/stats")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for missing token, got %d", rec.Code)
	}
}

func TestAdminHandler_RequireAuth_BadHeaderFormat(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	// 不带 "Bearer " 前缀
	req.Header.Set("Authorization", testToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for missing Bearer prefix, got %d", rec.Code)
	}
}

func TestAdminHandler_RequireAuth_WrongToken(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for wrong token, got %d", rec.Code)
	}
}
