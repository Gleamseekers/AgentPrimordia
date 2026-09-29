// tools_test.go — GitHub 工具的真实 API 请求构造验证
//
// 用 httptest server 模拟真实 GitHub API，验证：
//   - URL 拼接（/repos/{owner}/{repo}/...）
//   - GITHUB_TOKEN 存在时附加 Authorization: Bearer 头
//   - add_label 的 POST body（labels JSON）
//   - 非 200 响应的错误透传
package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	ap "github.com/Gleamseekers/AgentPrimordia/agentprimordia/pkg"
)

// capture 记录测试 server 收到的请求
type capture struct {
	mu     sync.Mutex
	auth   string
	method string
	path   string
	body   string
}

func (c *capture) record(r *http.Request) {
	body := ""
	if r.Body != nil {
		b := make([]byte, 1024)
		n, _ := r.Body.Read(b)
		body = string(b[:n])
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.auth = r.Header.Get("Authorization")
	c.method = r.Method
	c.path = r.URL.Path
	c.body = body
}

func (c *capture) snapshot() (auth, method, path, body string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.auth, c.method, c.path, c.body
}

func setupGitHubAPI(t *testing.T, withToken bool) (*capture, func()) {
	t.Helper()
	c := &capture{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.record(r)
		switch {
		case strings.Contains(r.URL.Path, "/labels"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"labels":["bug"]}`))
		case strings.Contains(r.URL.Path, "/issues/") && r.URL.Path != "/repos/owner/repo/issues":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"number":1,"title":"panic","labels":[]}`))
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"number":1,"title":"panic in main loop","state":"open","labels":["bug"]}]`))
		}
	}))

	// 保存并覆盖全局配置
	prevBase, prevToken, prevRepo := apiBase, githubToken, githubRepo
	apiBase = ts.URL
	githubRepo = "owner/repo"
	if withToken {
		githubToken = "ghp_test_token_123"
	} else {
		githubToken = ""
	}

	restore := func() {
		ts.Close()
		apiBase, githubToken, githubRepo = prevBase, prevToken, prevRepo
	}
	return c, restore
}

func TestListIssues_RealAPI_AuthHeader(t *testing.T) {
	c, restore := setupGitHubAPI(t, true)
	defer restore()

	result, err := listIssuesTool{}.Execute(context.Background(), []byte(`{"state":"open"}`))
	if err != nil || result == nil || result.IsError {
		t.Fatalf("list_issues 执行失败: err=%v result=%v", err, result)
	}

	auth, method, path, _ := c.snapshot()
	if auth != "Bearer ghp_test_token_123" {
		t.Errorf("Authorization = %q, want Bearer ghp_test_token_123", auth)
	}
	if method != http.MethodGet {
		t.Errorf("method = %q, want GET", method)
	}
	if path != "/repos/owner/repo/issues" {
		t.Errorf("path = %q, want /repos/owner/repo/issues", path)
	}
}

func TestListIssues_NoToken_NoAuthHeader(t *testing.T) {
	c, restore := setupGitHubAPI(t, false)
	defer restore()

	if _, err := (listIssuesTool{}).Execute(context.Background(), []byte(`{}`)); err != nil {
		t.Fatalf("list_issues 执行失败: %v", err)
	}
	auth, _, _, _ := c.snapshot()
	if auth != "" {
		t.Errorf("未设置 token 时 Authorization = %q, want 空", auth)
	}
}

func TestReadIssue_RealAPI_Path(t *testing.T) {
	c, restore := setupGitHubAPI(t, true)
	defer restore()

	if _, err := (readIssueTool{}).Execute(context.Background(), []byte(`{"issue_number":1}`)); err != nil {
		t.Fatalf("read_issue 执行失败: %v", err)
	}
	_, method, path, _ := c.snapshot()
	if method != http.MethodGet {
		t.Errorf("method = %q, want GET", method)
	}
	if path != "/repos/owner/repo/issues/1" {
		t.Errorf("path = %q, want /repos/owner/repo/issues/1", path)
	}
}

func TestAddLabel_RealAPI_PostBodyAndAuth(t *testing.T) {
	c, restore := setupGitHubAPI(t, true)
	defer restore()

	if _, err := (addLabelTool{}).Execute(context.Background(),
		[]byte(`{"issue_number":1,"labels":["bug","priority:high"]}`)); err != nil {
		t.Fatalf("add_label 执行失败: %v", err)
	}

	auth, method, path, body := c.snapshot()
	if method != http.MethodPost {
		t.Errorf("method = %q, want POST", method)
	}
	if path != "/repos/owner/repo/issues/1/labels" {
		t.Errorf("path = %q, want /repos/owner/repo/issues/1/labels", path)
	}
	if auth != "Bearer ghp_test_token_123" {
		t.Errorf("Authorization = %q, want Bearer ...", auth)
	}
	var payload struct {
		Labels []string `json:"labels"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("POST body 不是合法 JSON: %v (body=%q)", err, body)
	}
	if len(payload.Labels) != 2 || payload.Labels[0] != "bug" || payload.Labels[1] != "priority:high" {
		t.Errorf("POST labels = %v, want [bug priority:high]", payload.Labels)
	}
}

func TestReadIssue_HTTPError_Passthrough(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Resource not accessible by integration"}`))
	}))
	defer ts.Close()

	prevBase, prevToken := apiBase, githubToken
	apiBase = ts.URL
	githubToken = "ghp_test_token_123"
	defer func() { apiBase, githubToken = prevBase, prevToken }()

	result, err := readIssueTool{}.Execute(context.Background(), []byte(`{"issue_number":1}`))
	if err != nil {
		t.Fatalf("read_issue 应返回错误结果而非 error: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("403 应返回 IsError 结果, got %+v", result)
	}
	if !strings.Contains(result.Content, "403") {
		t.Errorf("错误结果应包含 HTTP 状态码, got %q", result.Content)
	}
}

var _ ap.Tool = listIssuesTool{}
