
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"agentprimordia/internal/agent/discovery"
)

// newAuthServer 创建带 API Key 的测试服务器，返回服务器和基础 URL
func newAuthServer(apiKey string) (*httptest.Server, *LocalDiscovery) {
	local := NewLocalDiscovery()
	server := NewDiscoveryServer(local, "")
	if apiKey != "" {
		server.WithAPIKey(apiKey)
	}
	ts := httptest.NewServer(server.Handler())
	return ts, local
}

// doRequest 发送 HTTP 请求并返回响应状态码和响应体
func doRequest(method, url, body string, headers map[string]string) (*http.Response, string) {
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		panic(fmt.Sprintf("创建请求失败: %v", err))
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(fmt.Sprintf("发送请求失败: %v", err))
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, string(data)
}

// ===== 1. 认证 - API Key 保护写操作 =====

func TestAPIKeyAuth_RegisterWithoutAuth(t *testing.T) {
	ts, _ := newAuthServer("secret123")
	defer ts.Close()

	resp, _ := doRequest("POST", ts.URL+"/api/discovery/register",
		`{"id":"a1","name":"worker","address":"localhost:8080"}`, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("无认证注册: 状态码 = %d, 期望 %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestAPIKeyAuth_RegisterWithWrongBearer(t *testing.T) {
	ts, _ := newAuthServer("secret123")
	defer ts.Close()

	resp, _ := doRequest("POST", ts.URL+"/api/discovery/register",
		`{"id":"a1","name":"worker","address":"localhost:8080"}`,
		map[string]string{"Authorization": "Bearer wrong-key"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("错误 Bearer 注册: 状态码 = %d, 期望 %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestAPIKeyAuth_RegisterWithCorrectBearer(t *testing.T) {
	ts, _ := newAuthServer("secret123")
	defer ts.Close()

	resp, _ := doRequest("POST", ts.URL+"/api/discovery/register",
		`{"id":"a1","name":"worker","address":"localhost:8080"}`,
		map[string]string{"Authorization": "Bearer secret123"})
	if resp.StatusCode != http.StatusOK {
		t.Errorf("正确 Bearer 注册: 状态码 = %d, 期望 %d", resp.StatusCode, http.StatusOK)
	}
}

func TestAPIKeyAuth_UnregisterWithoutAuth(t *testing.T) {
	ts, _ := newAuthServer("secret123")
	defer ts.Close()

	// 先用正确认证注册一个 Agent
	doRequest("POST", ts.URL+"/api/discovery/register",
		`{"id":"a1","name":"worker","address":"localhost:8080"}`,
		map[string]string{"Authorization": "Bearer secret123"})

	// 无认证注销应返回 401
	resp, _ := doRequest("DELETE", ts.URL+"/api/discovery/a1", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("无认证注销: 状态码 = %d, 期望 %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestAPIKeyAuth_HeartbeatWithoutAuth(t *testing.T) {
	ts, _ := newAuthServer("secret123")
	defer ts.Close()

	// 先注册
	doRequest("POST", ts.URL+"/api/discovery/register",
		`{"id":"a1","name":"worker","address":"localhost:8080"}`,
		map[string]string{"Authorization": "Bearer secret123"})

	// 无认证心跳应返回 401
	resp, _ := doRequest("POST", ts.URL+"/api/discovery/a1/heartbeat", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("无认证心跳: 状态码 = %d, 期望 %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestAPIKeyAuth_ListAgentsWithoutAuth(t *testing.T) {
	ts, _ := newAuthServer("secret123")
	defer ts.Close()

	// 先注册
	doRequest("POST", ts.URL+"/api/discovery/register",
		`{"id":"a1","name":"worker","address":"localhost:8080"}`,
		map[string]string{"Authorization": "Bearer secret123"})

	// 读操作无需认证
	resp, body := doRequest("GET", ts.URL+"/api/discovery/agents", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("无认证列出 Agent: 状态码 = %d, 期望 %d", resp.StatusCode, http.StatusOK)
	}
	var agents []*AgentInfo
	if err := json.Unmarshal([]byte(body), &agents); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if len(agents) != 1 {
		t.Errorf("Agent 数量 = %d, 期望 1", len(agents))
	}
}

func TestAPIKeyAuth_DiscoverWithoutAuth(t *testing.T) {
	ts, _ := newAuthServer("secret123")
	defer ts.Close()

	// 先注册
	doRequest("POST", ts.URL+"/api/discovery/register",
		`{"id":"a1","name":"worker","address":"localhost:8080"}`,
		map[string]string{"Authorization": "Bearer secret123"})

	// 读操作无需认证
	resp, body := doRequest("GET", ts.URL+"/api/discovery/a1", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("无认证发现: 状态码 = %d, 期望 %d", resp.StatusCode, http.StatusOK)
	}
	var info AgentInfo
	if err := json.Unmarshal([]byte(body), &info); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if info.ID != "a1" {
		t.Errorf("ID = %q, 期望 %q", info.ID, "a1")
	}
}

// ===== 2. 边界条件 - 注册验证 =====

func TestRegisterValidation_EmptyJSONBody(t *testing.T) {
	ts, _ := newAuthServer("")
	defer ts.Close()

	resp, _ := doRequest("POST", ts.URL+"/api/discovery/register", "{}", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("空 JSON 体: 状态码 = %d, 期望 %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestRegisterValidation_MissingID(t *testing.T) {
	ts, _ := newAuthServer("")
	defer ts.Close()

	resp, body := doRequest("POST", ts.URL+"/api/discovery/register",
		`{"name":"worker","address":"localhost:8080"}`, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("缺少 ID: 状态码 = %d, 期望 %d", resp.StatusCode, http.StatusBadRequest)
	}
	if !strings.Contains(body, "agent id must be between 1 and 256 characters") {
		t.Errorf("缺少 ID 错误信息不匹配: %q", body)
	}
}

func TestRegisterValidation_EmptyID(t *testing.T) {
	ts, _ := newAuthServer("")
	defer ts.Close()

	resp, body := doRequest("POST", ts.URL+"/api/discovery/register",
		`{"id":"","name":"worker","address":"localhost:8080"}`, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("空 ID: 状态码 = %d, 期望 %d", resp.StatusCode, http.StatusBadRequest)
	}
	if !strings.Contains(body, "agent id must be between 1 and 256 characters") {
		t.Errorf("空 ID 错误信息不匹配: %q", body)
	}
}

func TestRegisterValidation_IDTooLong(t *testing.T) {
	ts, _ := newAuthServer("")
	defer ts.Close()

	longID := strings.Repeat("a", 257)
	resp, body := doRequest("POST", ts.URL+"/api/discovery/register",
		fmt.Sprintf(`{"id":"%s","name":"worker","address":"localhost:8080"}`, longID), nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("超长 ID: 状态码 = %d, 期望 %d", resp.StatusCode, http.StatusBadRequest)
	}
	if !strings.Contains(body, "agent id must be between 1 and 256 characters") {
		t.Errorf("超长 ID 错误信息不匹配: %q", body)
	}
}

func TestRegisterValidation_MissingName(t *testing.T) {
	ts, _ := newAuthServer("")
	defer ts.Close()

	resp, body := doRequest("POST", ts.URL+"/api/discovery/register",
		`{"id":"a1","address":"localhost:8080"}`, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("缺少 Name: 状态码 = %d, 期望 %d", resp.StatusCode, http.StatusBadRequest)
	}
	if !strings.Contains(body, "agent name must be between 1 and 256 characters") {
		t.Errorf("缺少 Name 错误信息不匹配: %q", body)
	}
}

func TestRegisterValidation_EmptyName(t *testing.T) {
	ts, _ := newAuthServer("")
	defer ts.Close()

	resp, body := doRequest("POST", ts.URL+"/api/discovery/register",
		`{"id":"a1","name":"","address":"localhost:8080"}`, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("空 Name: 状态码 = %d, 期望 %d", resp.StatusCode, http.StatusBadRequest)
	}
	if !strings.Contains(body, "agent name must be between 1 and 256 characters") {
		t.Errorf("空 Name 错误信息不匹配: %q", body)
	}
}

func TestRegisterValidation_NameTooLong(t *testing.T) {
	ts, _ := newAuthServer("")
	defer ts.Close()

	longName := strings.Repeat("b", 257)
	resp, body := doRequest("POST", ts.URL+"/api/discovery/register",
		fmt.Sprintf(`{"id":"a1","name":"%s","address":"localhost:8080"}`, longName), nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("超长 Name: 状态码 = %d, 期望 %d", resp.StatusCode, http.StatusBadRequest)
	}
	if !strings.Contains(body, "agent name must be between 1 and 256 characters") {
		t.Errorf("超长 Name 错误信息不匹配: %q", body)
	}
}

func TestRegisterValidation_AddressTooLong(t *testing.T) {
	ts, _ := newAuthServer("")
	defer ts.Close()

	longAddr := strings.Repeat("c", 1025)
	resp, body := doRequest("POST", ts.URL+"/api/discovery/register",
		fmt.Sprintf(`{"id":"a1","name":"worker","address":"%s"}`, longAddr), nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("超长 Address: 状态码 = %d, 期望 %d", resp.StatusCode, http.StatusBadRequest)
	}
	if !strings.Contains(body, "agent address must not exceed 1024 characters") {
		t.Errorf("超长 Address 错误信息不匹配: %q", body)
	}
}

func TestRegisterValidation_ValidFullFields(t *testing.T) {
	ts, _ := newAuthServer("")
	defer ts.Close()

	resp, _ := doRequest("POST", ts.URL+"/api/discovery/register",
		`{"id":"a1","name":"worker","address":"localhost:8080","capabilities":["search","compute"],"metadata":{"version":"1.0"}}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("完整字段注册: 状态码 = %d, 期望 %d", resp.StatusCode, http.StatusOK)
	}

	// 验证注册后可发现
	resp2, body := doRequest("GET", ts.URL+"/api/discovery/a1", "", nil)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("发现已注册 Agent 失败: 状态码 = %d", resp2.StatusCode)
	}
	var info AgentInfo
	_ = json.Unmarshal([]byte(body), &info)
	if info.ID != "a1" || info.Name != "worker" {
		t.Errorf("注册数据不匹配: ID=%q Name=%q", info.ID, info.Name)
	}
	if len(info.Capabilities) != 2 {
		t.Errorf("Capabilities 长度 = %d, 期望 2", len(info.Capabilities))
	}
	if info.Metadata["version"] != "1.0" {
		t.Errorf("Metadata version = %q, 期望 %q", info.Metadata["version"], "1.0")
	}
}

// ===== 3. 边界条件 - 发现不存在 Agent =====

func TestDiscover_NonexistentAgent(t *testing.T) {
	ts, _ := newAuthServer("")
	defer ts.Close()

	resp, _ := doRequest("GET", ts.URL+"/api/discovery/nonexistent", "", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("发现不存在 Agent: 状态码 = %d, 期望 %d", resp.StatusCode, http.StatusNotFound)
	}
}

// ===== 4. 边界条件 - 心跳不存在 Agent =====

func TestHeartbeat_NonexistentAgent(t *testing.T) {
	ts, _ := newAuthServer("")
	defer ts.Close()

	resp, _ := doRequest("POST", ts.URL+"/api/discovery/nonexistent/heartbeat", "", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("心跳不存在 Agent: 状态码 = %d, 期望 %d", resp.StatusCode, http.StatusNotFound)
	}
}

// ===== 5. 边界条件 - 列出空 Agent 列表 =====

func TestListAgents_EmptyList(t *testing.T) {
	ts, _ := newAuthServer("")
	defer ts.Close()

	resp, body := doRequest("GET", ts.URL+"/api/discovery/agents", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("空列表: 状态码 = %d, 期望 %d", resp.StatusCode, http.StatusOK)
	}
	var agents []*AgentInfo
	if err := json.Unmarshal([]byte(body), &agents); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if agents == nil || len(agents) != 0 {
		t.Errorf("空列表: agents = %v, 期望空数组", agents)
	}
}

// ===== 6. 错误处理 - 无效 JSON 请求体 =====

func TestRegister_InvalidJSON(t *testing.T) {
	ts, _ := newAuthServer("")
	defer ts.Close()

	resp, body := doRequest("POST", ts.URL+"/api/discovery/register",
		`{invalid json!!!}`, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("无效 JSON: 状态码 = %d, 期望 %d", resp.StatusCode, http.StatusBadRequest)
	}
	if !strings.Contains(body, "invalid request body") {
		t.Errorf("无效 JSON 错误信息不匹配: %q", body)
	}
}

// ===== 7. 并发安全 - 并发注册 =====

func TestConcurrent_Register(t *testing.T) {
	ts, _ := newAuthServer("")
	defer ts.Close()

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)

	errCh := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			body := fmt.Sprintf(`{"id":"agent-%d","name":"worker-%d","address":"localhost:%d"}`, idx, idx, 8080+idx)
			resp, _ := doRequest("POST", ts.URL+"/api/discovery/register", body, nil)
			if resp.StatusCode != http.StatusOK {
				errCh <- fmt.Errorf("agent-%d 注册失败: 状态码 %d", idx, resp.StatusCode)
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("并发注册错误: %v", err)
	}

	// 验证所有 Agent 均已注册
	resp, body := doRequest("GET", ts.URL+"/api/discovery/agents", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("列出 Agent 失败: 状态码 %d", resp.StatusCode)
	}
	var agents []*AgentInfo
	_ = json.Unmarshal([]byte(body), &agents)
	if len(agents) != goroutines {
		t.Errorf("并发注册后 Agent 数量 = %d, 期望 %d", len(agents), goroutines)
	}
}

// ===== 8. 安全性 - 请求体大小限制 =====

func TestRegister_BodyTooLarge(t *testing.T) {
	ts, _ := newAuthServer("")
	defer ts.Close()

	// 构造超过 1MB 的请求体
	largeBody := `{"id":"a1","name":"worker","address":"` + strings.Repeat("x", 1<<20) + `"}`

	resp, _ := doRequest("POST", ts.URL+"/api/discovery/register", largeBody, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("超大请求体: 状态码 = %d, 期望 %d", resp.StatusCode, http.StatusBadRequest)
	}
}

// ===== 9. 兼容性 - 完整 CRUD 流程 =====

func TestCRUD_FullFlow(t *testing.T) {
	ts, _ := newAuthServer("")
	defer ts.Close()

	// 步骤1: 注册
	resp, _ := doRequest("POST", ts.URL+"/api/discovery/register",
		`{"id":"crud-1","name":"crud-worker","address":"localhost:9000","capabilities":["read","write"],"metadata":{"env":"test"}}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("CRUD 注册: 状态码 = %d, 期望 %d", resp.StatusCode, http.StatusOK)
	}

	// 步骤2: 列出 Agent
	resp, body := doRequest("GET", ts.URL+"/api/discovery/agents", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("CRUD 列出: 状态码 = %d", resp.StatusCode)
	}
	var agents []*AgentInfo
	_ = json.Unmarshal([]byte(body), &agents)
	if len(agents) != 1 || agents[0].ID != "crud-1" {
		t.Errorf("CRUD 列出: agents = %v, 期望包含 crud-1", agents)
	}

	// 步骤3: 发现指定 Agent
	resp, body = doRequest("GET", ts.URL+"/api/discovery/crud-1", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("CRUD 发现: 状态码 = %d", resp.StatusCode)
	}
	var info AgentInfo
	_ = json.Unmarshal([]byte(body), &info)
	if info.Name != "crud-worker" {
		t.Errorf("CRUD 发现: Name = %q, 期望 crud-worker", info.Name)
	}
	if len(info.Capabilities) != 2 {
		t.Errorf("CRUD 发现: Capabilities 长度 = %d, 期望 2", len(info.Capabilities))
	}

	// 步骤4: 心跳
	resp, _ = doRequest("POST", ts.URL+"/api/discovery/crud-1/heartbeat", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("CRUD 心跳: 状态码 = %d", resp.StatusCode)
	}

	// 步骤5: 注销
	resp, _ = doRequest("DELETE", ts.URL+"/api/discovery/crud-1", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("CRUD 注销: 状态码 = %d", resp.StatusCode)
	}

	// 步骤6: 再次发现应返回 404
	resp, _ = doRequest("GET", ts.URL+"/api/discovery/crud-1", "", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("CRUD 注销后发现: 状态码 = %d, 期望 %d", resp.StatusCode, http.StatusNotFound)
	}
}

// ===== 10. DiscoveryServer 双重启动 =====

// TestDiscoveryServer_Lifecycle 服务器启停生命周期。
// 2026-09-28：原 TestDiscoveryServer_DoubleStart 基于旧的非阻塞 Start +
// "already started" 守卫契约；现 Start() 即 http.ListenAndServe（阻塞，
// 供调用方以 goroutine 启动），双重启动守卫已随 API 重构移除。
// 服务器行为由 discovery 子包 15+ 测试覆盖，此处验证 agent 门面的
// 启停装配：goroutine 启动 → 真实服务 → Close 优雅退出。
func TestDiscoveryServer_Lifecycle(t *testing.T) {
	local := NewLocalDiscovery()
	server := NewDiscoveryServer(local, "127.0.0.1:0")

	startErr := make(chan error, 1)
	go func() { startErr <- server.Start() }()

	// 等待真实绑定地址就绪（Start 在 goroutine 中异步 Listen；
	// 就绪前 Addr() 返回配置值 "127.0.0.1:0"）
	deadline := time.Now().Add(5 * time.Second)
	const configured = "127.0.0.1:0"
	var addr string
	for time.Now().Before(deadline) {
		if addr = server.Addr(); addr != configured {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if addr == configured {
		t.Fatal("Addr() 未在超时内返回真实绑定地址")
	}
	client := NewHTTPDiscoveryClient("http://" + addr)
	var registered bool
	var lastErr error
	for time.Now().Before(deadline) {
		if err := client.Register(context.Background(), &discovery.AgentInfo{ID: "lc-1", Name: "LC", Address: addr}); err == nil {
			registered = true
			break
		} else {
			lastErr = err
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !registered {
		t.Fatalf("服务器未在超时内就绪 (addr=%s): %v", addr, lastErr)
	}

	if err := server.Close(); err != nil {
		t.Errorf("Close 失败: %v", err)
	}
	// ListenAndServe 关闭后应返回 http.ErrServerClosed（非阻塞错误即通过）。
	select {
	case err := <-startErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("Start 返回了意外错误: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("Close 后 Start 未返回")
	}
}
