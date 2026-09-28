// timeout_clamp_test.go — 用户可控 timeout 参数 clamp 测试（P1 安全修复）
//
// 背景：web/api/http_client/code_execution 的 timeout 参数直接 int(v) 后
// 乘以 time.Second，超大值（如 1e18）会溢出 time.Duration（int64 纳秒），
// 产生负超时——行为未定义（可能立即取消或永不过期）。修复：统一 clamp 到
// [minTimeoutSec, maxTimeoutSec]（参照 builtin/shell.go maxTimeoutSec 模式）。
package builtin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestClampTimeoutSec 验证 clamp 边界与溢出防护
func TestClampTimeoutSec(t *testing.T) {
	cases := []struct {
		name string
		in   float64
		want int
	}{
		{"低于下限 clamp 到 1", 0.5, minTimeoutSec},
		{"下限边界", 1, 1},
		{"常规值不变", 30, 30},
		{"上限边界", 3600, maxTimeoutSec},
		{"高于上限 clamp 到 3600", 3601, maxTimeoutSec},
		{"超大值防溢出", 1e18, maxTimeoutSec},
		{"int64 溢出值", 9.3e18, maxTimeoutSec},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := clampTimeoutSec(tc.in); got != tc.want {
				t.Errorf("clampTimeoutSec(%v) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

// TestClampTimeoutSec_NoDurationOverflow 验证 clamp 后的值绝不会使
// time.Duration 乘法溢出（负数超时）
func TestClampTimeoutSec_NoDurationOverflow(t *testing.T) {
	for _, v := range []float64{1e18, 9.3e18, 1e300} {
		sec := clampTimeoutSec(v)
		d := time.Duration(sec) * time.Second
		if d <= 0 {
			t.Errorf("clampTimeoutSec(%v) = %d → Duration %d 非正", v, sec, d)
		}
	}
}

// TestWeb_Execute_TimeoutOverflowClamped 行为测试：超大 timeout 参数
// 不得因 Duration 溢出导致请求失败（修复前 IsError）
func TestWeb_Execute_TimeoutOverflowClamped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	w := NewWeb().WithAllowPrivate(true)
	args := json.RawMessage(`{"action":"fetch","url":"` + srv.URL + `","timeout":1e18}`)
	result, err := w.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute 不应返回错误: %v", err)
	}
	if result.IsError {
		t.Fatalf("超大 timeout 应被 clamp 而非溢出失败: %s", result.Content)
	}
}

// TestAPI_Execute_TimeoutOverflowClamped 行为测试（api 工具）
func TestAPI_Execute_TimeoutOverflowClamped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	a := NewAPI().WithAllowPrivate(true)
	args := json.RawMessage(`{"method":"GET","url":"` + srv.URL + `","timeout":1e18}`)
	result, err := a.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute 不应返回错误: %v", err)
	}
	if result.IsError {
		t.Fatalf("超大 timeout 应被 clamp 而非溢出失败: %s", result.Content)
	}
}

// TestHTTPClient_Execute_TimeoutOverflowClamped 行为测试（http_client 工具）
func TestHTTPClient_Execute_TimeoutOverflowClamped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := NewHTTPClient().WithAllowPrivate(true)
	args := json.RawMessage(`{"method":"GET","url":"` + srv.URL + `","timeout":1e18}`)
	result, err := c.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute 不应返回错误: %v", err)
	}
	if result.IsError {
		t.Fatalf("超大 timeout 应被 clamp 而非溢出失败: %s", result.Content)
	}
}

// TestCodeExecution_Execute_TimeoutOverflowClamped 行为测试（code_execution 工具）
func TestCodeExecution_Execute_TimeoutOverflowClamped(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 不可用，跳过")
	}
	t.Setenv("AP_ALLOW_CODE_EXECUTION", "true")

	c := NewCodeExecution()
	args := json.RawMessage(`{"language":"python","code":"print(1+1)","timeout":1e18}`)
	result, err := c.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute 不应返回错误: %v", err)
	}
	if result.IsError {
		t.Fatalf("超大 timeout 应被 clamp 而非溢出失败: %s", result.Content)
	}
	// 输出应包含 2
	if !json.Valid([]byte(result.Content)) {
		t.Fatalf("结果不是有效 JSON: %s", result.Content)
	}
	var out struct {
		Output string `json:"output"`
	}
	_ = json.Unmarshal([]byte(result.Content), &out)
	if strings.TrimSpace(out.Output) != "2" {
		t.Errorf("输出 = %q, want %q", out.Output, "2")
	}
}
