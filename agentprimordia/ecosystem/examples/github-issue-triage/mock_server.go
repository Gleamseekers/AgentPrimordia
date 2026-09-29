// Phase 18: GitHub Issue Triage Demo - Mock Server
//
// 本文件用 httptest 模拟 GitHub REST API 的 Issue 相关端点：
//
//	GET    /repos/{owner}/{repo}/issues        -> ListIssues
//	GET    /repos/{owner}/{repo}/issues/{n}   -> ReadIssue
//	POST   /repos/{owner}/{repo}/issues/{n}/labels -> AddLabel
//
// 所有数据存于内存（sync.Mutex 保护），重启后丢失。
// 这是 demo 用 mock server，生产场景应替换为真实 GitHub API 调用。
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"
)

// Issue 表示 GitHub Issue 的简化模型
type Issue struct {
	Number    int      `json:"number"`
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	State     string   `json:"state"`
	Labels    []string `json:"labels"`
	Author    string   `json:"author"`
	CreatedAt string   `json:"created_at"`
}

// seedIssues 返回演示用的预置 Issue 列表
// 涵盖 4 种分类：bug / feature / question / duplicate
func seedIssues() []Issue {
	now := time.Now().UTC().Format(time.RFC3339)
	return []Issue{
		{
			Number:    1,
			Title:     "panic in main loop when context is nil",
			Body:      "Steps to reproduce:\n1. Call agent.Run(ctx, msg) with ctx == nil\n2. Agent panics with nil pointer dereference\n\nExpected: should return an error instead of panicking.\n\nStack trace:\n```\npanic: runtime error: invalid memory address or nil pointer dereference\n```",
			State:     "open",
			Labels:    []string{},
			Author:    "alice",
			CreatedAt: now,
		},
		{
			Number:    2,
			Title:     "Feature request: dark mode for CLI",
			Body:      "It would be great if the ap CLI supported a dark mode color scheme.\n\nMany users work in low-light environments and the default white-on-black is hard to read.\n\nThanks!",
			State:     "open",
			Labels:    []string{},
			Author:    "bob",
			CreatedAt: now,
		},
		{
			Number:    3,
			Title:     "How to configure OAuth provider?",
			Body:      "Hi, I'm trying to use OAuth with my custom provider but I can't find documentation.\n\nWhere do I put the client_id and client_secret? Is there an env var convention?\n\nAny pointer would be appreciated.",
			State:     "open",
			Labels:    []string{},
			Author:    "charlie",
			CreatedAt: now,
		},
		{
			Number:    4,
			Title:     "Build fails on Windows with CGO error",
			Body:      "Trying to build on Windows 11 with Go 1.22:\n\n```\n# github.com/Gleamseekers/AgentPrimordia/internal/memory\nC:\\Users\\me\\go\\pkg\\mod\\modernc.org\\sqlite@v1.28.0\\...\\sqlite.c:15:10: fatal error: stdio.h: No such file or directory\n```\n\nIt says Zero CGO but the build is failing with CGO. What's wrong?",
			State:     "open",
			Labels:    []string{},
			Author:    "diana",
			CreatedAt: now,
		},
		{
			Number:    5,
			Title:     "Same as #2 - dark mode request",
			Body:      "Just adding my +1 to the dark mode feature request from #2.\n\nWould love to see this implemented soon!",
			State:     "open",
			Labels:    []string{},
			Author:    "eve",
			CreatedAt: now,
		},
	}
}

// mockGitHubServer 是 GitHub API 模拟器的 HTTP handler
// 数据存于内存，对外暴露 issues 字段供 main 读取最终状态
type mockGitHubServer struct {
	mu     sync.Mutex
	issues []Issue
}

func newMockGitHubServer() *mockGitHubServer {
	return &mockGitHubServer{
		issues: seedIssues(),
	}
}

// handler 路由分发
func (s *mockGitHubServer) handler() http.Handler {
	mux := http.NewServeMux()

	// GET /repos/owner/repo/issues - 列出 issues
	mux.HandleFunc("/repos/owner/repo/issues", s.handleList)

	// GET/POST /repos/owner/repo/issues/{number}[/labels]
	mux.HandleFunc("/repos/owner/repo/issues/", s.handleIssue)

	return mux
}

func (s *mockGitHubServer) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// 支持 state 过滤：open/closed/all
	state := r.URL.Query().Get("state")
	filtered := make([]Issue, 0, len(s.issues))
	for _, iss := range s.issues {
		if state == "" || state == "all" || iss.State == state {
			filtered = append(filtered, iss)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(filtered)
}

func (s *mockGitHubServer) handleIssue(w http.ResponseWriter, r *http.Request) {
	// 解析 path: /repos/owner/repo/issues/{number}[/labels]
	path := strings.TrimPrefix(r.URL.Path, "/repos/owner/repo/issues/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "missing issue number", http.StatusBadRequest)
		return
	}

	var number int
	if _, err := fmt.Sscanf(parts[0], "%d", &number); err != nil {
		http.Error(w, "invalid issue number", http.StatusBadRequest)
		return
	}

	// POST /repos/owner/repo/issues/{n}/labels
	if len(parts) == 2 && parts[1] == "labels" && r.Method == http.MethodPost {
		s.handleAddLabel(w, r, number)
		return
	}

	// GET /repos/owner/repo/issues/{n}
	if len(parts) == 1 && r.Method == http.MethodGet {
		s.handleRead(w, number)
		return
	}

	http.Error(w, "not found", http.StatusNotFound)
}

func (s *mockGitHubServer) handleRead(w http.ResponseWriter, number int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, iss := range s.issues {
		if iss.Number == number {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(iss)
			return
		}
	}
	http.Error(w, "issue not found", http.StatusNotFound)
}

type addLabelRequest struct {
	Labels []string `json:"labels"`
}

type addLabelResponse struct {
	OK            bool     `json:"ok"`
	IssueNumber   int      `json:"issue_number"`
	LabelsApplied []string `json:"labels_applied"`
	CurrentLabels []string `json:"current_labels"`
}

func (s *mockGitHubServer) handleAddLabel(w http.ResponseWriter, r *http.Request, number int) {
	var req addLabelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.issues {
		if s.issues[i].Number == number {
			// 合并 label（去重）
			labelSet := make(map[string]bool)
			for _, l := range s.issues[i].Labels {
				labelSet[l] = true
			}
			for _, l := range req.Labels {
				labelSet[l] = true
			}
			merged := make([]string, 0, len(labelSet))
			for l := range labelSet {
				merged = append(merged, l)
			}
			s.issues[i].Labels = merged

			resp := addLabelResponse{
				OK:            true,
				IssueNumber:   number,
				LabelsApplied: req.Labels,
				CurrentLabels: s.issues[i].Labels,
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
	}
	http.Error(w, "issue not found", http.StatusNotFound)
}

// start 启动 HTTP mock server 并返回 URL + 关闭函数
func (s *mockGitHubServer) start() (string, func()) {
	server := httptest.NewServer(s.handler())
	return server.URL, server.Close
}

// snapshot 返回当前所有 issue 的快照（用于报告输出）
func (s *mockGitHubServer) snapshot() []Issue {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Issue, len(s.issues))
	copy(out, s.issues)
	return out
}
