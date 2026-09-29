// failure_server.go — 失败记录与一键重放 HTTP API（v3.4-6）
// 对外暴露失败记录列表/详情/诊断摘要/重放/删除，配合 Inspector 等调试界面使用。
package debugger

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Gleamseekers/AgentPrimordia/internal/persist"
)

// ReplayFunc 失败重放函数签名：返回重放执行的输出内容。
// 通常由 agent 侧适配注入，例如：
//
//	func(ctx, id) (string, error) { resp, err := ag.ReplayFailure(ctx, id); return resp.Content, err }
type ReplayFunc func(ctx context.Context, failureID string) (string, error)

// FailureServerOption FailureServer 可选配置
type FailureServerOption func(*FailureServer)

// WithFailureReplayer 注入失败重放能力；未注入时 replay 接口返回 501
func WithFailureReplayer(fn ReplayFunc) FailureServerOption {
	return func(s *FailureServer) { s.replayer = fn }
}

// FailureServer 失败记录 HTTP API 服务器
type FailureServer struct {
	store    persist.FailureStore
	replayer ReplayFunc
	mux      *http.ServeMux
}

// NewFailureServer 创建失败记录 HTTP API 服务器
func NewFailureServer(store persist.FailureStore, opts ...FailureServerOption) *FailureServer {
	s := &FailureServer{
		store: store,
		mux:   http.NewServeMux(),
	}
	for _, opt := range opts {
		opt(s)
	}

	s.mux.HandleFunc("/api/failures", s.handleList)
	s.mux.HandleFunc("/api/failures/", s.handleItem)

	return s
}

// Handler 返回 HTTP 处理器
func (s *FailureServer) Handler() http.Handler {
	return s.mux
}

// handleList GET /api/failures?agent=<agentID> 列出失败记录（新→旧）
func (s *FailureServer) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	recs, err := s.store.List(r.Context(), r.URL.Query().Get("agent"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(recs)
}

// handleItem 处理 /api/failures/{id}[/(diagnose|replay)] 子路径
func (s *FailureServer) handleItem(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/failures/")
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	sub := ""
	if len(parts) == 2 {
		sub = parts[1]
	}
	if id == "" {
		http.Error(w, "failure id required", http.StatusBadRequest)
		return
	}

	switch {
	case sub == "" && r.Method == http.MethodGet:
		s.handleGet(w, r, id)
	case sub == "" && r.Method == http.MethodDelete:
		s.handleDelete(w, r, id)
	case sub == "diagnose" && r.Method == http.MethodGet:
		s.handleDiagnose(w, r, id)
	case sub == "replay" && r.Method == http.MethodPost:
		s.handleReplay(w, r, id)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleGet 获取单条失败记录（含内嵌检查点）
func (s *FailureServer) handleGet(w http.ResponseWriter, r *http.Request, id string) {
	rec, err := s.store.Get(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rec)
}

// handleDiagnose 返回人类可读的失败诊断摘要（纯文本）
func (s *FailureServer) handleDiagnose(w http.ResponseWriter, r *http.Request, id string) {
	rec, err := s.store.Get(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(rec.Diagnose()))
}

// handleReplay 一键重放失败记录（需注入重放器）
func (s *FailureServer) handleReplay(w http.ResponseWriter, r *http.Request, id string) {
	if s.replayer == nil {
		http.Error(w, "replayer not configured", http.StatusNotImplemented)
		return
	}
	output, err := s.replayer(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"failure_id": id,
		"output":     output,
	})
}

// handleDelete 删除失败记录
func (s *FailureServer) handleDelete(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.store.Delete(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"deleted":true}`))
}
