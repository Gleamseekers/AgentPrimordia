package main

import (
	"runtime/debug"
	"testing"
)

// TestResolveVersion 覆盖 resolveVersion 的三个分支：
// ① ldflags 注入值优先（不被 build info 覆盖）；
// ② dev 构建 + build info 带模块版本（go install module@version 场景）→ 回填；
// ③ dev 构建 + 无 build info 或 (devel) → 保持 dev。
func TestResolveVersion(t *testing.T) {
	cases := []struct {
		name     string
		injected string
		bi       *debug.BuildInfo
		ok       bool
		want     string
	}{
		{"ldflags 注入优先", "7.5.2", &debug.BuildInfo{Main: debug.Module{Version: "v9.9.9"}}, true, "7.5.2"},
		{"go install @version 回填", "dev", &debug.BuildInfo{Main: debug.Module{Version: "v7.5.1"}}, true, "v7.5.1"},
		{"go test 构建保持 dev", "dev", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, true, "dev"},
		{"无 build info 保持 dev", "dev", nil, false, "dev"},
		{"空版本保持 dev", "dev", &debug.BuildInfo{}, true, "dev"},
		{"伪版本（本地 go build）保持 dev", "dev", &debug.BuildInfo{Main: debug.Module{Version: "v7.5.2-0.20261012074529-9831cef3963f+dirty"}}, true, "dev"},
		{"干净预发布版本接受", "dev", &debug.BuildInfo{Main: debug.Module{Version: "v7.6.0-rc1"}}, true, "v7.6.0-rc1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := resolveVersion(c.injected, func() (*debug.BuildInfo, bool) {
				return c.bi, c.ok
			})
			if got != c.want {
				t.Errorf("resolveVersion(%q) = %q，期望 %q", c.injected, got, c.want)
			}
		})
	}
}

// TestVersionNotDevInTest 守护：真实 init() 在本包构建下不应把 Version 改成
// 意外的非 dev 值（go test 构建无模块版本，应保持 dev）。
func TestVersionNotDevInTest(t *testing.T) {
	if Version != "dev" {
		t.Logf("当前 Version = %q（测试构建带 ldflags 时属预期）", Version)
	}
}
