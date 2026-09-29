// failure_server_test.go — v3.4-6 失败重放 HTTP API 测试
package debugger

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/persist"
)

// seedRecords 预置两条失败记录（agent-a 两条：一条可重放、一条不可；agent-b 一条）
func seedRecords(t *testing.T, store persist.FailureStore) {
	t.Helper()
	now := time.Now()
	recs := []*persist.FailureRecord{
		{
			ID: "f-1", AgentID: "agent-a", Phase: persist.PhaseRun,
			Error: "llm boom", Turn: 2, Input: "task a", CreatedAt: now,
			State: &persist.AgentState{AgentID: "agent-a", Status: "failed", TurnCount: 1},
		},
		{
			ID: "f-2", AgentID: "agent-a", Phase: persist.PhasePlan,
			Error: "subtask 2 failed: tool broke", SubTaskID: "2", CreatedAt: now,
		},
		{
			ID: "f-3", AgentID: "agent-b", Phase: persist.PhaseRun, Error: "x", CreatedAt: now,
		},
	}
	for _, r := range recs {
		if err := store.Record(context.Background(), r); err != nil {
			t.Fatalf("预置失败记录失败: %v", err)
		}
	}
}

// TestFailureServer_List GET /api/failures 支持按 agent 过滤
func TestFailureServer_List(t *testing.T) {
	store := persist.NewMemoryFailureStore()
	seedRecords(t, store)
	srv := httptest.NewServer(NewFailureServer(store).Handler())
	defer srv.Close()

	// 全量
	var all []*persist.FailureRecord
	getJSON(t, srv.URL+"/api/failures", &all)
	if len(all) != 3 {
		t.Fatalf("全量列表 = %d 条, want 3", len(all))
	}

	// 按 agent 过滤
	var filtered []*persist.FailureRecord
	getJSON(t, srv.URL+"/api/failures?agent=agent-a", &filtered)
	if len(filtered) != 2 {
		t.Fatalf("agent-a 过滤 = %d 条, want 2", len(filtered))
	}
}

// TestFailureServer_Get GET /api/failures/{id} 与 404
func TestFailureServer_Get(t *testing.T) {
	store := persist.NewMemoryFailureStore()
	seedRecords(t, store)
	srv := httptest.NewServer(NewFailureServer(store).Handler())
	defer srv.Close()

	var rec persist.FailureRecord
	getJSON(t, srv.URL+"/api/failures/f-1", &rec)
	if rec.ID != "f-1" || rec.State == nil {
		t.Fatalf("获取记录失败: %+v", rec)
	}

	resp, err := http.Get(srv.URL + "/api/failures/missing")
	if err != nil {
		t.Fatalf("GET 失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("不存在的记录应返回 404, got %d", resp.StatusCode)
	}
}

// TestFailureServer_Diagnose GET /api/failures/{id}/diagnose 返回诊断摘要
func TestFailureServer_Diagnose(t *testing.T) {
	store := persist.NewMemoryFailureStore()
	seedRecords(t, store)
	srv := httptest.NewServer(NewFailureServer(store).Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/failures/f-2/diagnose")
	if err != nil {
		t.Fatalf("GET 失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	buf := make([]byte, 1024)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])
	if !strings.Contains(body, "子任务 2") || !strings.Contains(body, "tool broke") {
		t.Errorf("诊断摘要缺少关键信息:\n%s", body)
	}
}

// TestFailureServer_Replay POST /api/failures/{id}/replay 触发重放
func TestFailureServer_Replay(t *testing.T) {
	store := persist.NewMemoryFailureStore()
	seedRecords(t, store)

	var replayed string
	replayer := func(_ context.Context, id string) (string, error) {
		replayed = id
		return "recovered: " + id, nil
	}
	srv := httptest.NewServer(NewFailureServer(store, WithFailureReplayer(replayer)).Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/failures/f-1/replay", "application/json", nil)
	if err != nil {
		t.Fatalf("POST 失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out struct {
		FailureID string `json:"failure_id"`
		Output    string `json:"output"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if replayed != "f-1" || out.Output != "recovered: f-1" {
		t.Errorf("重放结果 = %+v (replayed=%q)", out, replayed)
	}
}

// TestFailureServer_Replay_Errors 无重放器 → 501；重放失败 → 500
func TestFailureServer_Replay_Errors(t *testing.T) {
	store := persist.NewMemoryFailureStore()
	seedRecords(t, store)

	// 未配置重放器
	srv := httptest.NewServer(NewFailureServer(store).Handler())
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/api/failures/f-1/replay", "application/json", nil)
	if err != nil {
		t.Fatalf("POST 失败: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("未配置重放器应返回 501, got %d", resp.StatusCode)
	}

	// 重放器报错
	failing := func(_ context.Context, _ string) (string, error) { return "", errors.New("still broken") }
	srv2 := httptest.NewServer(NewFailureServer(store, WithFailureReplayer(failing)).Handler())
	defer srv2.Close()
	resp2, err := http.Post(srv2.URL+"/api/failures/f-1/replay", "application/json", nil)
	if err != nil {
		t.Fatalf("POST 失败: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusInternalServerError {
		t.Errorf("重放失败应返回 500, got %d", resp2.StatusCode)
	}
}

// TestFailureServer_Delete DELETE /api/failures/{id}
func TestFailureServer_Delete(t *testing.T) {
	store := persist.NewMemoryFailureStore()
	seedRecords(t, store)
	srv := httptest.NewServer(NewFailureServer(store).Handler())
	defer srv.Close()

	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/failures/f-3", nil)
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE 失败: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if _, err := store.Get(context.Background(), "f-3"); err == nil {
		t.Error("删除后 Get 应报错")
	}
}

// getJSON GET 请求并解析 JSON 响应
func getJSON(t *testing.T, url string, out any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s 失败: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200", url, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("解析 %s 响应失败: %v", url, err)
	}
}

// TestFailureServer_SQLiteBackend 集成4：SQLite 后端读取（持久化失败记录经 /api/failures 可查）。
func TestFailureServer_SQLiteBackend(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "failures.db")
	store, err := persist.NewSQLiteFailureStore(dsn)
	if err != nil {
		t.Fatalf("NewSQLiteFailureStore: %v", err)
	}
	defer store.Close()
	seedRecords(t, store)

	srv := httptest.NewServer(NewFailureServer(store).Handler())
	defer srv.Close()

	var all []*persist.FailureRecord
	getJSON(t, srv.URL+"/api/failures", &all)
	if len(all) != 3 {
		t.Fatalf("SQLite 后端全量列表 = %d 条, want 3", len(all))
	}

	// 单条含可重放检查点
	var one *persist.FailureRecord
	getJSON(t, srv.URL+"/api/failures/f-1", &one)
	if one == nil || one.ID != "f-1" || one.State == nil || one.State.TurnCount != 1 {
		t.Errorf("SQLite 后端 Get = %+v, want f-1 含检查点", one)
	}

	// 删除后不可见
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/failures/f-1", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE status = %d, want 200", resp.StatusCode)
	}
	var after []*persist.FailureRecord
	getJSON(t, srv.URL+"/api/failures", &after)
	if len(after) != 2 {
		t.Errorf("删除后列表 = %d 条, want 2", len(after))
	}
}
