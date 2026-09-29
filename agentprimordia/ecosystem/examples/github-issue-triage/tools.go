// Phase 18: GitHub Issue Triage Demo - Tools
//
// 实现 ap.Tool 接口的 3 个 GitHub 工具：
//   - listIssuesTool:  列出 issues
//   - readIssueTool:   读取 issue 详情
//   - addLabelTool:    给 issue 添加 label
//
// 双模式：
//   - mock 模式（默认）：main 将 apiBase 注入为内存 mock server 地址
//   - 真实 API 模式：设置 GITHUB_TOKEN + GH_REPO 后访问 https://api.github.com，
//     工具自动附加 Authorization: Bearer 鉴权头
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	ap "github.com/Gleamseekers/AgentPrimordia/pkg"
)

// apiBase 是 GitHub API 基础 URL。
// 默认 https://api.github.com（真实 API）；mock 模式由 main 注入 mock server 地址。
var apiBase = "https://api.github.com"

// githubToken 是可选的真实 GitHub API 访问令牌（来自 GITHUB_TOKEN 环境变量）。
// 非空时所有工具请求都会附带 Authorization: Bearer 头。
var githubToken string

// githubRepo 是目标仓库 "owner/repo"（来自 GH_REPO 环境变量，默认 "owner/repo"）。
var githubRepo = "owner/repo"

// httpClient 共享给所有工具，避免每次都新建 client
var httpClient = &http.Client{Timeout: 10 * time.Second}

// newGitHubRequest 构造带鉴权的 GitHub API 请求。
// 设置 githubToken 时自动附加 Authorization: Bearer 头；有 body 时附加 JSON Content-Type。
func newGitHubRequest(ctx context.Context, method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	if githubToken != "" {
		req.Header.Set("Authorization", "Bearer "+githubToken)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// ===== listIssuesTool =====

type listIssuesTool struct{}

func (listIssuesTool) Name() string { return "list_issues" }

func (listIssuesTool) Description() string {
	return `列出 GitHub repo 的 issues。

输入参数:
  state: "open" | "closed" | "all"（可选，默认 "open"）

返回: JSON 数组，每项包含 number, title, state, labels, author, created_at
   示例: [{"number": 1, "title": "panic in main loop", "state": "open", "labels": ["bug"]}]`
}

func (listIssuesTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"state": {
				"type": "string",
				"enum": ["open", "closed", "all"],
				"description": "Issue 状态过滤",
				"default": "open"
			}
		}
	}`)
}

func (listIssuesTool) Execute(ctx context.Context, args json.RawMessage) (*ap.ToolResult, error) {
	var params struct {
		State string `json:"state"`
	}
	if len(args) > 0 {
		_ = json.Unmarshal(args, &params)
	}
	if params.State == "" {
		params.State = "open"
	}

	url := fmt.Sprintf("%s/repos/%s/issues?state=%s", apiBase, githubRepo, params.State)
	req, err := newGitHubRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ap.NewToolErrorResult(fmt.Sprintf("list_issues build request: %v", err)), nil
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return ap.NewToolErrorResult(fmt.Sprintf("list_issues http error: %v", err)), nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return ap.NewToolErrorResult(fmt.Sprintf("list_issues status=%d body=%s", resp.StatusCode, string(body))), nil
	}
	return ap.NewToolResult(string(body)), nil
}

// ===== readIssueTool =====

type readIssueTool struct{}

func (readIssueTool) Name() string { return "read_issue" }

func (readIssueTool) Description() string {
	return `读取单个 GitHub issue 的完整详情。

输入参数:
  issue_number: 整数，必填

返回: JSON 对象，包含 number, title, body, labels, author, created_at
   示例: {"number": 1, "title": "...", "body": "...", "labels": [], "author": "alice"}`
}

func (readIssueTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"issue_number": {
				"type": "integer",
				"description": "Issue 编号"
			}
		},
		"required": ["issue_number"]
	}`)
}

func (readIssueTool) Execute(ctx context.Context, args json.RawMessage) (*ap.ToolResult, error) {
	var params struct {
		IssueNumber int `json:"issue_number"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return ap.NewToolErrorResult(fmt.Sprintf("read_issue invalid args: %v", err)), nil
	}
	if params.IssueNumber == 0 {
		return ap.NewToolErrorResult("read_issue requires issue_number"), nil
	}

	url := fmt.Sprintf("%s/repos/%s/issues/%d", apiBase, githubRepo, params.IssueNumber)
	req, err := newGitHubRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ap.NewToolErrorResult(fmt.Sprintf("read_issue build request: %v", err)), nil
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return ap.NewToolErrorResult(fmt.Sprintf("read_issue http error: %v", err)), nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return ap.NewToolErrorResult(fmt.Sprintf("read_issue status=%d body=%s", resp.StatusCode, string(body))), nil
	}
	return ap.NewToolResult(string(body)), nil
}

// ===== addLabelTool =====

type addLabelTool struct{}

func (addLabelTool) Name() string { return "add_label" }

func (addLabelTool) Description() string {
	return `给 GitHub issue 添加一个或多个 label。

输入参数:
  issue_number: 整数，必填
  labels: 字符串数组，必填，例如 ["bug", "priority:high"]

返回: JSON 对象 {ok, issue_number, labels_applied, current_labels}`
}

func (addLabelTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"issue_number": {
				"type": "integer",
				"description": "Issue 编号"
			},
			"labels": {
				"type": "array",
				"items": {"type": "string"},
				"description": "要添加的 label 列表"
			}
		},
		"required": ["issue_number", "labels"]
	}`)
}

func (addLabelTool) Execute(ctx context.Context, args json.RawMessage) (*ap.ToolResult, error) {
	var params struct {
		IssueNumber int      `json:"issue_number"`
		Labels      []string `json:"labels"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return ap.NewToolErrorResult(fmt.Sprintf("add_label invalid args: %v", err)), nil
	}
	if params.IssueNumber == 0 {
		return ap.NewToolErrorResult("add_label requires issue_number"), nil
	}
	if len(params.Labels) == 0 {
		return ap.NewToolErrorResult("add_label requires at least one label"), nil
	}

	body, _ := json.Marshal(map[string]any{"labels": params.Labels})
	url := fmt.Sprintf("%s/repos/%s/issues/%d/labels", apiBase, githubRepo, params.IssueNumber)
	req, err := newGitHubRequest(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return ap.NewToolErrorResult(fmt.Sprintf("add_label build request: %v", err)), nil
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return ap.NewToolErrorResult(fmt.Sprintf("add_label http error: %v", err)), nil
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return ap.NewToolErrorResult(fmt.Sprintf("add_label status=%d body=%s", resp.StatusCode, string(respBody))), nil
	}
	return ap.NewToolResult(string(respBody)), nil
}

// registryFromTools 用最简方式注册工具到 ap.ToolRegistry
// 不使用 DefaultToolkit 因为本 demo 只需要自定义工具，不需要 FS/Shell/Web
func registryFromTools(tools ...ap.Tool) (*ap.ToolRegistry, error) {
	registry := ap.NewToolRegistry()
	for _, t := range tools {
		if err := registry.Register(t); err != nil {
			return nil, fmt.Errorf("failed to register tool %s: %w", t.Name(), err)
		}
	}
	return registry, nil
}

// formatIssueBrief 把 Issue 简化成单行字符串（用于最终报告输出）
func formatIssueBrief(iss Issue) string {
	return fmt.Sprintf("#%-3d %-12s | labels=%-30s | %s",
		iss.Number,
		firstLabel(iss.Labels),
		strings.Join(iss.Labels, ","),
		truncate(iss.Title, 50),
	)
}

func firstLabel(labels []string) string {
	if len(labels) == 0 {
		return "(none)"
	}
	return labels[0]
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
