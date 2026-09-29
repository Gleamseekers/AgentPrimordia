package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/pool"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/tools"
)

const testToken = "test-admin-token"

func newTestHandler(t *testing.T) *AdminHandler {
	t.Helper()
	p := pool.NewPool(pool.PoolConfig{MaxConcurrency: 5})
	t.Cleanup(func() { p.Close() })
	return NewAdminHandler(p, tools.NewRegistry(), WithAPIToken(testToken))
}

func doRequest(t *testing.T, handler http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func doAuthorizedRequest(t *testing.T, handler http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestAdminHandler_ListAgents(t *testing.T) {
	handler := newTestHandler(t)
	rec := doAuthorizedRequest(t, handler, http.MethodGet, "/api/agents")

	if rec.Code != http.StatusOK {
		t.Fatalf("期望状态码 200，实际 %d", rec.Code)
	}

	var result map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("解析 JSON 失败: %v", err)
	}
}

func TestAdminHandler_GetAgent_NotFound(t *testing.T) {
	handler := newTestHandler(t)
	rec := doAuthorizedRequest(t, handler, http.MethodGet, "/api/agents/nonexistent")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("期望状态码 404，实际 %d", rec.Code)
	}

	var result map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("解析 JSON 失败: %v", err)
	}
	if result["error"] == "" {
		t.Fatal("期望返回错误信息")
	}
}

func TestAdminHandler_Stats(t *testing.T) {
	handler := newTestHandler(t)
	rec := doAuthorizedRequest(t, handler, http.MethodGet, "/api/stats")

	if rec.Code != http.StatusOK {
		t.Fatalf("期望状态码 200，实际 %d", rec.Code)
	}

	var stats pool.PoolStats
	if err := json.NewDecoder(rec.Body).Decode(&stats); err != nil {
		t.Fatalf("解析 JSON 失败: %v", err)
	}
	if stats.MaxConcurrency != 5 {
		t.Fatalf("期望 MaxConcurrency=5，实际 %d", stats.MaxConcurrency)
	}
}

func TestAdminHandler_Tasks(t *testing.T) {
	handler := newTestHandler(t)
	rec := doAuthorizedRequest(t, handler, http.MethodGet, "/api/tasks")

	if rec.Code != http.StatusOK {
		t.Fatalf("期望状态码 200，实际 %d", rec.Code)
	}

	var tasks []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&tasks); err != nil {
		t.Fatalf("解析 JSON 失败: %v", err)
	}
}

func TestAdminHandler_Index(t *testing.T) {
	handler := newTestHandler(t)
	rec := doRequest(t, handler, http.MethodGet, "/")

	if rec.Code != http.StatusOK {
		t.Fatalf("期望状态码 200，实际 %d", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Fatalf("期望 Content-Type text/html，实际 %s", ct)
	}

	body := rec.Body.String()
	if len(body) == 0 {
		t.Fatal("期望返回 HTML 内容")
	}
}

func TestAdminHandler_NotFound(t *testing.T) {
	handler := newTestHandler(t)
	rec := doRequest(t, handler, http.MethodGet, "/unknown")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("期望状态码 404，实际 %d", rec.Code)
	}
}

func TestSanitizeHTML(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"<script>alert('xss')</script>", "&lt;script&gt;alert(&#39;xss&#39;)&lt;/script&gt;"},
		{"Hello & World", "Hello &amp; World"},
		{`"quoted"`, "&#34;quoted&#34;"},
		{"normal text", "normal text"},
		{"", ""},
	}

	for _, tt := range tests {
		result := sanitizeHTML(tt.input)
		if result != tt.expected {
			t.Errorf("sanitizeHTML(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestTaskResultToJSON(t *testing.T) {
	tests := []struct {
		name     string
		result   pool.TaskResult
		checkKey string
	}{
		{
			name: "成功结果",
			result: pool.TaskResult{
				TaskID:   "task-1",
				Task:     pool.TaskConfig{Title: "测试任务"},
				Status:   "completed",
				Duration: 1000000000,
			},
			checkKey: "duration_ms",
		},
		{
			name: "带错误的结果",
			result: pool.TaskResult{
				TaskID: "task-2",
				Task:   pool.TaskConfig{Title: "失败任务"},
				Status: "failed",
				Error:  fmt.Errorf("模拟错误"),
			},
			checkKey: "error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := taskResultToJSON(tt.result)
			if m["task_id"] != tt.result.TaskID {
				t.Errorf("task_id = %v, want %v", m["task_id"], tt.result.TaskID)
			}
			if m["status"] != tt.result.Status {
				t.Errorf("status = %v, want %v", m["status"], tt.result.Status)
			}
			if _, exists := m[tt.checkKey]; !exists {
				t.Errorf("期望包含 key %s", tt.checkKey)
			}
		})
	}
}

func TestAdminHandler_Health(t *testing.T) {
	handler := newTestHandler(t)
	rec := doRequest(t, handler, http.MethodGet, "/api/health")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var result map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&result)

	if result["status"] != "healthy" {
		t.Errorf("status = %v, want healthy", result["status"])
	}
	if _, ok := result["timestamp"]; !ok {
		t.Error("expected timestamp field")
	}
	if _, ok := result["tasks"]; !ok {
		t.Error("expected tasks field")
	}
}

func TestAdminHandler_SystemInfo(t *testing.T) {
	handler := newTestHandler(t)
	rec := doAuthorizedRequest(t, handler, http.MethodGet, "/api/system")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var result map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&result)

	if _, ok := result["go_version"]; !ok {
		t.Error("expected go_version field")
	}
	if _, ok := result["goroutines"]; !ok {
		t.Error("expected goroutines field")
	}
	if _, ok := result["cpu_count"]; !ok {
		t.Error("expected cpu_count field")
	}
	if _, ok := result["mem_alloc_mb"]; !ok {
		t.Error("expected mem_alloc_mb field")
	}
}

func TestAdminHandler_AuthMissing(t *testing.T) {
	handler := newTestHandler(t)
	rec := doRequest(t, handler, http.MethodGet, "/api/agents")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAdminHandler_AuthInvalid(t *testing.T) {
	handler := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAdminHandler_AuthNoTokenConfigured(t *testing.T) {
	p := pool.NewPool(pool.PoolConfig{MaxConcurrency: 5})
	t.Cleanup(func() { p.Close() })
	handler := NewAdminHandler(p, tools.NewRegistry())

	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAdminHandler_HealthPublic(t *testing.T) {
	handler := newTestHandler(t)
	rec := doRequest(t, handler, http.MethodGet, "/api/health")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}
