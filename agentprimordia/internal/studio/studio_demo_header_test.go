package studio

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestStudioBasePanelsMarkDemo 回归：基础四面板（chaos/cluster/learning/marketplace）
// 在未注入真实服务时必须标注 X-Data-Source: demo。
// 背景（v7.3 整改）：此前只有 v3.x 面板标注，基础四面板"默认全 demo 却无任何标识"，
// 容易被误读为已接入真实引擎。
func TestStudioBasePanelsMarkDemo(t *testing.T) {
	h := NewStudioHandler()
	paths := []string{
		"/api/v1/chaos/experiments",
		"/api/v1/cluster/status",
		"/api/v1/learning/stats",
		"/api/v1/learning/capabilities",
		"/api/v1/marketplace/templates",
		"/api/v1/marketplace/deployments",
	}
	for _, p := range paths {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, p, nil)
		h.ServeHTTP(rec, req)
		if got := rec.Header().Get("X-Data-Source"); got != "demo" {
			t.Errorf("%s 未注入真实服务时应标 X-Data-Source: demo，got %q", p, got)
		}
	}
}

// TestMarkDemoIfRealSkipsHeader 校验注入真实服务后不再标 demo。
func TestMarkDemoIfRealSkipsHeader(t *testing.T) {
	called := false
	next := func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}

	rec := httptest.NewRecorder()
	markDemoIf(true, next)(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !called {
		t.Fatal("markDemoIf 未调用 next")
	}
	if got := rec.Header().Get("X-Data-Source"); got != "" {
		t.Errorf("真实服务不应标 demo 头，got %q", got)
	}

	// demo 分支必须"先设头再调用 next"，保证 handler 写响应体前头部已就位
	rec = httptest.NewRecorder()
	markDemoIf(false, func(w http.ResponseWriter, r *http.Request) {
		if w.Header().Get("X-Data-Source") != "demo" {
			t.Error("demo 头必须在业务 handler 执行前已设置")
		}
		w.WriteHeader(http.StatusOK)
	})(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := rec.Header().Get("X-Data-Source"); got != "demo" {
		t.Errorf("got %q, want demo", got)
	}
}
