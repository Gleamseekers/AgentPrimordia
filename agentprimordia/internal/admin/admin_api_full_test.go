package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/pool"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/tools"
)

// doRequestWithPool 使用指定 pool 创建 handler 并发起授权请求
func doRequestWithPool(t *testing.T, p *pool.Pool, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	handler := NewAdminHandler(p, tools.NewRegistry(), WithAPIToken(testToken))
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// 功能验证：GetAgent 查询正在运行的 agent 应返回 200 和正确数据
func TestAdminAPI_GetAgent_Existing(t *testing.T) {
	mockLLM := llm.NewMockLLM(t).WithDelay(200 * time.Millisecond).WithResponse("任务完成")
	p := pool.NewPool(pool.PoolConfig{MaxConcurrency: 5})
	p.SetModel(mockLLM)
	t.Cleanup(func() { p.Close() })

	agentID := "test-agent-001"

	go func() {
		tasks := []pool.TaskConfig{
			{ID: agentID, Title: "存在性验证任务", Prompt: "执行任务"},
		}
		if _, err := p.Dispatch(context.Background(), tasks); err != nil {
			t.Logf("Dispatch failed (acceptable in test): %v", err)
		}
	}()

	time.Sleep(50 * time.Millisecond)

	handler := NewAdminHandler(p, tools.NewRegistry(), WithAPIToken(testToken))
	rec := doAuthorizedRequest(t, handler, http.MethodGet, "/api/agents/"+agentID)

	if rec.Code != http.StatusOK {
		t.Fatalf("期望状态码 200，实际 %d，响应体: %s", rec.Code, rec.Body.String())
	}

	var result map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("解析 JSON 失败: %v", err)
	}

	if result["id"] != agentID {
		t.Errorf("id = %v，期望 %s", result["id"], agentID)
	}
	if result["status"] == nil {
		t.Error("期望包含 status 字段")
	}
}

// 边界条件：Health 响应字段验证 - RFC3339 时间戳、字段类型
func TestAdminAPI_Health_ResponseFields(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t)
	rec := doRequest(t, handler, http.MethodGet, "/api/health")

	if rec.Code != http.StatusOK {
		t.Fatalf("期望状态码 200，实际 %d", rec.Code)
	}

	var result map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("解析 JSON 失败: %v", err)
	}

	if result["status"] != "healthy" {
		t.Errorf("status = %v，期望 healthy", result["status"])
	}

	timestampStr, ok := result["timestamp"].(string)
	if !ok {
		t.Fatalf("timestamp 类型错误，期望 string，实际 %T", result["timestamp"])
	}

	_, err := time.Parse(time.RFC3339, timestampStr)
	if err != nil {
		t.Errorf("timestamp 不是有效的 RFC3339 格式: %v", err)
	}

	tasksVal, ok := result["tasks"].(float64)
	if !ok {
		t.Fatalf("tasks 类型错误，期望 float64(JSON number)，实际 %T", result["tasks"])
	}
	if tasksVal < 0 {
		t.Errorf("tasks = %v，期望 >= 0", tasksVal)
	}

	runningVal, ok := result["running"].(float64)
	if !ok {
		t.Fatalf("running 类型错误，期望 float64(JSON number)，实际 %T", result["running"])
	}
	if runningVal < 0 {
		t.Errorf("running = %v，期望 >= 0", runningVal)
	}
}

// 边界条件：SystemInfo 响应字段类型验证
func TestAdminAPI_SystemInfo_FieldTypes(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t)
	rec := doAuthorizedRequest(t, handler, http.MethodGet, "/api/system")

	if rec.Code != http.StatusOK {
		t.Fatalf("期望状态码 200，实际 %d", rec.Code)
	}

	var result map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("解析 JSON 失败: %v", err)
	}

	if _, ok := result["go_version"].(string); !ok {
		t.Errorf("go_version 类型错误，期望 string，实际 %T", result["go_version"])
	}

	if _, ok := result["goroutines"].(float64); !ok {
		t.Errorf("goroutines 类型错误，期望 float64，实际 %T", result["goroutines"])
	}

	if _, ok := result["cpu_count"].(float64); !ok {
		t.Errorf("cpu_count 类型错误，期望 float64，实际 %T", result["cpu_count"])
	}

	if _, ok := result["mem_alloc_mb"].(float64); !ok {
		t.Errorf("mem_alloc_mb 类型错误，期望 float64，实际 %T", result["mem_alloc_mb"])
	}

	if _, ok := result["mem_sys_mb"].(float64); !ok {
		t.Errorf("mem_sys_mb 类型错误，期望 float64，实际 %T", result["mem_sys_mb"])
	}

	if _, ok := result["gc_count"].(float64); !ok {
		t.Errorf("gc_count 类型错误，期望 float64，实际 %T", result["gc_count"])
	}

	if _, ok := result["heap_objects"].(float64); !ok {
		t.Errorf("heap_objects 类型错误，期望 float64，实际 %T", result["heap_objects"])
	}

	if _, ok := result["stack_use_mb"].(float64); !ok {
		t.Errorf("stack_use_mb 类型错误，期望 float64，实际 %T", result["stack_use_mb"])
	}

	if result["goroutines"].(float64) < 1 {
		t.Error("goroutines 应 >= 1")
	}
	if result["cpu_count"].(float64) < 1 {
		t.Error("cpu_count 应 >= 1")
	}
	if result["mem_alloc_mb"].(float64) < 0 {
		t.Error("mem_alloc_mb 应 >= 0")
	}
}

// 错误处理：各 GET 端点对 POST/PUT/DELETE 方法应返回 405
func TestAdminAPI_MethodNotAllowed(t *testing.T) {
	t.Parallel()

	endpoints := []string{
		"/api/agents",
		"/api/agents/some-id",
		"/api/stats",
		"/api/tasks",
		"/api/health",
		"/api/system",
	}

	methods := []string{http.MethodPost, http.MethodPut, http.MethodDelete}

	for _, endpoint := range endpoints {
		for _, method := range methods {
			ep := endpoint
			mt := method
			t.Run(fmt.Sprintf("%s_%s", mt, ep), func(t *testing.T) {
				t.Parallel()
				handler := newTestHandler(t)
				rec := doRequest(t, handler, mt, ep)

				if rec.Code != http.StatusMethodNotAllowed {
					t.Errorf("%s %s: 期望状态码 405，实际 %d", mt, ep, rec.Code)
				}
			})
		}
	}
}

// 错误处理：各种无效路径应返回 404
func TestAdminAPI_InvalidPaths(t *testing.T) {
	t.Parallel()

	invalidPaths := []struct {
		path                string
		allowRedirectStatus bool
	}{
		{"/api", false},
		{"/api/", false},
		{"/api/agents/", false},
		{"/api/agent", false},
		{"/api/stat", false},
		{"/api/task", false},
		{"/api/healthz", false},
		{"/api/systems", false},
		{"/v1/api/agents", false},
		{"/api/agents/../../etc/passwd", true},
	}

	for _, tc := range invalidPaths {
		p := tc.path
		allowRedir := tc.allowRedirectStatus
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			handler := newTestHandler(t)
			rec := doRequest(t, handler, http.MethodGet, p)

			if allowRedir {
				isRedirect := rec.Code >= 300 && rec.Code <= 399
				if rec.Code != http.StatusNotFound && !isRedirect {
					t.Errorf("路径 %s: 期望状态码 404 或 3xx，实际 %d", p, rec.Code)
				}
			} else {
				if rec.Code != http.StatusNotFound {
					t.Errorf("路径 %s: 期望状态码 404，实际 %d", p, rec.Code)
				}
			}
		})
	}
}

// 安全性：API 端点应返回 application/json Content-Type
func TestAdminAPI_ContentType_JSON(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t)

	jsonEndpoints := []struct {
		name   string
		method string
		path   string
	}{
		{"listAgents", http.MethodGet, "/api/agents"},
		{"stats", http.MethodGet, "/api/stats"},
		{"tasks", http.MethodGet, "/api/tasks"},
		{"health", http.MethodGet, "/api/health"},
		{"systemInfo", http.MethodGet, "/api/system"},
		{"getAgent_NotFound", http.MethodGet, "/api/agents/nonexistent"},
	}

	for _, ep := range jsonEndpoints {
		tc := ep
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := doRequest(t, handler, tc.method, tc.path)

			ct := rec.Header().Get("Content-Type")
			if ct != "application/json; charset=utf-8" {
				t.Errorf("%s: Content-Type = %q，期望 %q", tc.name, ct, "application/json; charset=utf-8")
			}
		})
	}
}

// 安全性：Index 端点应返回 text/html Content-Type
func TestAdminAPI_ContentType_HTML(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t)
	rec := doRequest(t, handler, http.MethodGet, "/")

	ct := rec.Header().Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q，期望 %q", ct, "text/html; charset=utf-8")
	}
}

// 安全性：XSS 在 agent ID 中不应被执行
func TestAdminAPI_XSS_InAgentID(t *testing.T) {
	t.Parallel()

	xssPayloads := []struct {
		name    string
		payload string
	}{
		{"script_tag", "<script>alert('xss')</script>"},
		{"img_onerror", "<img src=x onerror=alert(1)>"},
		{"script_cookie", "'><script>document.cookie</script>"},
		{"svg_onload", "<svg/onload=alert(1)>"},
	}

	for _, tc := range xssPayloads {
		name := tc.name
		payload := tc.payload
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			handler := newTestHandler(t)

			encodedID := url.PathEscape(payload)
			req := httptest.NewRequest(http.MethodGet, "/api/agents/"+encodedID, nil)
			req.Header.Set("Authorization", "Bearer "+testToken)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			body := rec.Body.String()

			if !json.Valid([]byte(body)) {
				t.Fatalf("响应不是有效的 JSON: %s", body)
			}

			if findSubstring(body, "<script>") {
				t.Errorf("响应体包含未转义的 <script> 标签")
			}
			if findSubstring(body, "onerror=") {
				t.Errorf("响应体包含未转义的 onerror 事件")
			}
			if findSubstring(body, "onload=") {
				t.Errorf("响应体包含未转义的 onload 事件")
			}
		})
	}
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// 并发安全：多个 goroutine 同时请求 /api/health
func TestAdminAPI_ConcurrentRequests(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t)
	concurrency := 20
	var wg sync.WaitGroup
	errCh := make(chan error, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			rec := doRequest(t, handler, http.MethodGet, "/api/health")

			if rec.Code != http.StatusOK {
				errCh <- fmt.Errorf("goroutine %d: 期望状态码 200，实际 %d", idx, rec.Code)
				return
			}

			var result map[string]any
			if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
				errCh <- fmt.Errorf("goroutine %d: 解析 JSON 失败: %v", idx, err)
				return
			}

			if result["status"] != "healthy" {
				errCh <- fmt.Errorf("goroutine %d: status = %v，期望 healthy", idx, result["status"])
				return
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Error(err)
	}
}

// 兼容性：验证各端点 JSON 响应结构符合预期格式
func TestAdminAPI_ResponseFormat(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t)

	t.Run("listAgents_返回map", func(t *testing.T) {
		t.Parallel()
		rec := doAuthorizedRequest(t, handler, http.MethodGet, "/api/agents")

		var result map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
			t.Fatalf("解析 JSON 失败: %v", err)
		}
	})

	t.Run("stats_包含所有PoolStats字段", func(t *testing.T) {
		t.Parallel()
		rec := doAuthorizedRequest(t, handler, http.MethodGet, "/api/stats")

		var result map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
			t.Fatalf("解析 JSON 失败: %v", err)
		}

		expectedKeys := []string{
			"total_tasks", "completed_tasks", "failed_tasks",
			"running_tasks", "queued_tasks", "max_concurrency", "active_concurrency",
		}
		for _, key := range expectedKeys {
			if _, exists := result[key]; !exists {
				t.Errorf("stats 响应缺少字段 %q", key)
			}
		}
	})

	t.Run("tasks_返回数组", func(t *testing.T) {
		t.Parallel()
		rec := doAuthorizedRequest(t, handler, http.MethodGet, "/api/tasks")

		var result []map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
			t.Fatalf("解析 JSON 失败: %v", err)
		}
	})

	t.Run("health_包含必要字段", func(t *testing.T) {
		t.Parallel()
		rec := doRequest(t, handler, http.MethodGet, "/api/health")

		var result map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
			t.Fatalf("解析 JSON 失败: %v", err)
		}

		expectedKeys := []string{"status", "timestamp", "tasks", "running"}
		for _, key := range expectedKeys {
			if _, exists := result[key]; !exists {
				t.Errorf("health 响应缺少字段 %q", key)
			}
		}
	})

	t.Run("systemInfo_包含所有字段", func(t *testing.T) {
		t.Parallel()
		rec := doAuthorizedRequest(t, handler, http.MethodGet, "/api/system")

		var result map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
			t.Fatalf("解析 JSON 失败: %v", err)
		}

		expectedKeys := []string{
			"go_version", "goroutines", "cpu_count",
			"mem_alloc_mb", "mem_sys_mb", "gc_count",
			"heap_objects", "stack_use_mb",
		}
		for _, key := range expectedKeys {
			if _, exists := result[key]; !exists {
				t.Errorf("systemInfo 响应缺少字段 %q", key)
			}
		}
	})

	t.Run("getAgent_NotFound_包含error字段", func(t *testing.T) {
		t.Parallel()
		rec := doAuthorizedRequest(t, handler, http.MethodGet, "/api/agents/nonexistent")

		if rec.Code != http.StatusNotFound {
			t.Fatalf("期望状态码 404，实际 %d", rec.Code)
		}

		var result map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
			t.Fatalf("解析 JSON 失败: %v", err)
		}

		if _, exists := result["error"]; !exists {
			t.Error("404 响应应包含 error 字段")
		}
	})
}

// 性能：快速连续发送 100 个请求到 /api/health
func TestAdminAPI_RapidRequests(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t)
	totalRequests := 100
	failures := 0

	for i := 0; i < totalRequests; i++ {
		rec := doRequest(t, handler, http.MethodGet, "/api/health")

		if rec.Code != http.StatusOK {
			failures++
			continue
		}

		var result map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
			failures++
			continue
		}

		if result["status"] != "healthy" {
			failures++
		}
	}

	if failures > 0 {
		t.Errorf("%d/%d 请求失败", failures, totalRequests)
	}
}

// 并发安全：并发请求多个不同端点
func TestAdminAPI_ConcurrentMixedEndpoints(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t)
	endpoints := []struct {
		path   string
		status int
	}{
		{"/api/agents", http.StatusOK},
		{"/api/stats", http.StatusOK},
		{"/api/tasks", http.StatusOK},
		{"/api/health", http.StatusOK},
		{"/api/system", http.StatusOK},
	}

	concurrency := 10
	var wg sync.WaitGroup
	errCh := make(chan error, concurrency*len(endpoints))

	for i := 0; i < concurrency; i++ {
		for _, ep := range endpoints {
			wg.Add(1)
			go func(path string, expectedStatus int) {
				defer wg.Done()
				req := httptest.NewRequest(http.MethodGet, path, nil)
				if path != "/api/health" {
					req.Header.Set("Authorization", "Bearer "+testToken)
				}
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				if rec.Code != expectedStatus {
					errCh <- fmt.Errorf("%s: 期望状态码 %d，实际 %d", path, expectedStatus, rec.Code)
				}
			}(ep.path, ep.status)
		}
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Error(err)
	}
}

// 安全性：非根路径通过 index handler 应返回 404 JSON
func TestAdminAPI_Index_NonRootPath(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t)
	nonRootPaths := []string{"/dashboard", "/admin", "/panel"}

	for _, path := range nonRootPaths {
		p := path
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			rec := doRequest(t, handler, http.MethodGet, p)

			if rec.Code != http.StatusNotFound {
				t.Errorf("路径 %s: 期望状态码 404，实际 %d", p, rec.Code)
			}

			ct := rec.Header().Get("Content-Type")
			if ct != "application/json; charset=utf-8" {
				t.Errorf("路径 %s: Content-Type = %q，期望 application/json", p, ct)
			}

			var result map[string]any
			if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
				t.Fatalf("路径 %s: 解析 JSON 失败: %v", p, err)
			}

			if _, exists := result["error"]; !exists {
				t.Errorf("路径 %s: 404 响应应包含 error 字段", p)
			}
		})
	}
}

// 兼容性：Stats 响应数值字段应为非负整数
func TestAdminAPI_Stats_NonNegativeValues(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t)
	rec := doAuthorizedRequest(t, handler, http.MethodGet, "/api/stats")

	if rec.Code != http.StatusOK {
		t.Fatalf("期望状态码 200，实际 %d", rec.Code)
	}

	var result map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("解析 JSON 失败: %v", err)
	}

	numericFields := []string{
		"total_tasks", "completed_tasks", "failed_tasks",
		"running_tasks", "queued_tasks", "max_concurrency", "active_concurrency",
	}

	for _, field := range numericFields {
		val, ok := result[field].(float64)
		if !ok {
			t.Errorf("字段 %s 类型错误，期望 float64，实际 %T", field, result[field])
			continue
		}
		if val < 0 {
			t.Errorf("字段 %s = %v，期望 >= 0", field, val)
		}
	}
}

// 功能验证：Dispatch 任务后 Stats 和 Tasks 端点反映正确状态
func TestAdminAPI_AfterDispatch_StatsAndTasks(t *testing.T) {
	mockLLM := llm.NewMockLLM(t).WithResponse("完成")
	p := pool.NewPool(pool.PoolConfig{MaxConcurrency: 5})
	p.SetModel(mockLLM)
	t.Cleanup(func() { p.Close() })

	tasks := []pool.TaskConfig{
		{ID: "dispatch-1", Title: "调度测试1", Prompt: "执行"},
		{ID: "dispatch-2", Title: "调度测试2", Prompt: "执行"},
	}

	results, err := p.Dispatch(context.Background(), tasks)
	if err != nil {
		t.Fatalf("Dispatch 失败: %v", err)
	}
	_ = results

	t.Run("stats_反映已完成任务", func(t *testing.T) {
		rec := doRequestWithPool(t, p, http.MethodGet, "/api/stats")

		var stats map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&stats); err != nil {
			t.Fatalf("解析 JSON 失败: %v", err)
		}

		if stats["total_tasks"].(float64) != 2 {
			t.Errorf("total_tasks = %v，期望 2", stats["total_tasks"])
		}
		if stats["completed_tasks"].(float64) != 2 {
			t.Errorf("completed_tasks = %v，期望 2", stats["completed_tasks"])
		}
	})

	t.Run("tasks_返回已完成的任务列表", func(t *testing.T) {
		rec := doRequestWithPool(t, p, http.MethodGet, "/api/tasks")

		var taskList []map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&taskList); err != nil {
			t.Fatalf("解析 JSON 失败: %v", err)
		}

		if len(taskList) < 2 {
			t.Errorf("任务列表长度 = %d，期望 >= 2", len(taskList))
		}
	})
}

// 边界条件：Health 时间戳应为当前时间附近
func TestAdminAPI_Health_TimestampNearNow(t *testing.T) {
	t.Parallel()

	before := time.Now().UTC().Add(-5 * time.Second)
	handler := newTestHandler(t)
	rec := doRequest(t, handler, http.MethodGet, "/api/health")
	after := time.Now().UTC().Add(5 * time.Second)

	var result map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("解析 JSON 失败: %v", err)
	}

	tsStr, ok := result["timestamp"].(string)
	if !ok {
		t.Fatalf("timestamp 类型错误")
	}

	ts, err := time.Parse(time.RFC3339, tsStr)
	if err != nil {
		t.Fatalf("解析时间戳失败: %v", err)
	}

	if ts.Before(before) || ts.After(after) {
		t.Errorf("时间戳 %v 不在预期范围 [%v, %v] 内", ts, before, after)
	}
}

// 功能验证：GetAgent 查询未注册的 agent ID 应返回 404
func TestAdminAPI_GetAgent_NotRegistered(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t)
	rec := doAuthorizedRequest(t, handler, http.MethodGet, "/api/agents/nonexistent-agent")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("期望状态码 404，实际 %d", rec.Code)
	}

	var result map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("解析 JSON 失败: %v", err)
	}

	if _, exists := result["error"]; !exists {
		t.Error("404 响应应包含 error 字段")
	}
}

// 安全性：XSS payload 在 agent ID 中应被安全处理（JSON 编码天然转义）
func TestAdminAPI_XSS_JSONEncodingSafe(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t)
	xssID := `<script>alert(1)</script>`
	encodedID := url.PathEscape(xssID)
	req := httptest.NewRequest(http.MethodGet, "/api/agents/"+encodedID, nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Logf("XSS ID 返回状态码 %d（非 200 即安全）", rec.Code)
	}

	body := rec.Body.String()

	if !json.Valid([]byte(body)) {
		t.Fatalf("响应不是有效的 JSON: %s", body)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("解析 JSON 失败: %v", err)
	}
}

// 并发安全：并发读写 - 同时 Dispatch 任务和请求 API
func TestAdminAPI_ConcurrentDispatchAndQuery(t *testing.T) {
	mockLLM := llm.NewMockLLM(t).WithResponse("并发结果")
	p := pool.NewPool(pool.PoolConfig{MaxConcurrency: 5})
	p.SetModel(mockLLM)
	t.Cleanup(func() { p.Close() })

	handler := NewAdminHandler(p, tools.NewRegistry(), WithAPIToken(testToken))
	var wg sync.WaitGroup
	errCh := make(chan error, 30)

	wg.Add(1)
	go func() {
		defer wg.Done()
		tasks := []pool.TaskConfig{
			{ID: "concurrent-1", Title: "并发任务1", Prompt: "执行"},
		}
		if _, err := p.Dispatch(context.Background(), tasks); err != nil {
			t.Logf("Dispatch failed (acceptable in test): %v", err)
		}
	}()

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			endpoints := []string{"/api/health", "/api/stats", "/api/agents"}
			for _, ep := range endpoints {
				req := httptest.NewRequest(http.MethodGet, ep, nil)
				if ep != "/api/health" {
					req.Header.Set("Authorization", "Bearer "+testToken)
				}
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					errCh <- fmt.Errorf("goroutine %d: %s 返回 %d", idx, ep, rec.Code)
				}
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Error(err)
	}
}

// 兼容性：Index 页面包含关键 HTML 结构
func TestAdminAPI_Index_HTMLStructure(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t)
	rec := doRequest(t, handler, http.MethodGet, "/")

	body := rec.Body.String()

	expectedSubstrings := []string{
		"<!DOCTYPE html>",
		"<html",
		"</html>",
		"<head>",
		"</head>",
		"<body>",
		"</body>",
		"AgentPrimordia",
		"/api/stats",
		"/api/tasks",
	}

	for _, sub := range expectedSubstrings {
		if !findSubstring(body, sub) {
			t.Errorf("HTML 缺少子串 %q", sub)
		}
	}
}
