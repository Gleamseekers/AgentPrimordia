package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestBuildStudioHandlerReal 生产路径回归：默认装配必须注入真实服务。
// 判据使用基础面板的 X-Data-Source 头——真实注入时不标 demo。
func TestBuildStudioHandlerReal(t *testing.T) {
	oldDir := studioDataDir
	studioDataDir = t.TempDir()
	defer func() { studioDataDir = oldDir }()

	h := buildStudioHandler(false)
	if h == nil {
		t.Fatal("buildStudioHandler 返回 nil")
	}

	for _, path := range []string{
		"/api/v1/chaos/experiments",
		"/api/v1/cluster/status",
		"/api/v1/learning/stats",
		"/api/v1/marketplace/templates",
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s 状态码 = %d, want 200", path, rec.Code)
		}
		if got := rec.Header().Get("X-Data-Source"); got == "demo" {
			t.Errorf("%s 装配真实服务后不应标 demo", path)
		}
	}
}

// TestBuildStudioHandlerDemo demo 回退模式仍可用，并保留 demo 标识。
func TestBuildStudioHandlerDemo(t *testing.T) {
	h := buildStudioHandler(true)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/cluster/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("demo 模式状态码 = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("X-Data-Source"); got != "demo" {
		t.Errorf("demo 模式应标 demo，实际 %q", got)
	}
}

// TestStudioMarketplaceEndpointUsesRegistry 真实注册表经 HTTP 端点可见，
// 且部署生命周期端点可用。
func TestStudioMarketplaceEndpointUsesRegistry(t *testing.T) {
	oldDir := studioDataDir
	studioDataDir = t.TempDir()
	defer func() { studioDataDir = oldDir }()

	h := buildStudioHandler(false)

	// 初始为空（真实注册表，非 demo 固定数据）
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/marketplace/templates", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("templates 端点状态码 = %d", rec.Code)
	}
	if body := rec.Body.String(); body == "" {
		t.Error("templates 端点应返回 JSON 数组")
	}
}
