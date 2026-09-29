package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHasLocalFrameworkReplace 校验"本地 replace 自洽"判定的边界。
func TestHasLocalFrameworkReplace(t *testing.T) {
	cases := []struct {
		name    string
		goMod   string
		want    bool
		noGoMod bool
	}{
		{
			name:  "框架 replace（生成项目形态）",
			goMod: "module demo\n\nrequire github.com/Gleamseekers/AgentPrimordia v0.0.0\n\nreplace github.com/Gleamseekers/AgentPrimordia => ../../agentprimordia\n",
			want:  true,
		},
		{
			name:  "仅 pgvector replace",
			goMod: "module demo\n\nrequire github.com/Gleamseekers/AgentPrimordia/pgvector v0.0.0\n\nreplace github.com/Gleamseekers/AgentPrimordia/pgvector => ../../pgvector\n",
			want:  true,
		},
		{
			name:  "standalone：无 replace",
			goMod: "module demo\n\nrequire github.com/Gleamseekers/AgentPrimordia v0.0.0\n",
			want:  false,
		},
		{
			name:  "代理版本 require（非本地）",
			goMod: "module demo\n\nrequire github.com/Gleamseekers/AgentPrimordia v1.2.3\n",
			want:  false,
		},
		{
			name:    "无 go.mod",
			noGoMod: true,
			want:    false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if !c.noGoMod {
				if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(c.goMod), 0o644); err != nil {
					t.Fatalf("写入 go.mod 失败: %v", err)
				}
			}
			if got := hasLocalFrameworkReplace(dir); got != c.want {
				t.Errorf("hasLocalFrameworkReplace = %v, want %v", got, c.want)
			}
		})
	}
}

// TestIsolatedGoEnv 校验本地 replace 项目会以 GOWORK=off 隔离构建。
func TestIsolatedGoEnv(t *testing.T) {
	dir := t.TempDir()
	goMod := "module demo\n\nrequire github.com/Gleamseekers/AgentPrimordia v0.0.0\n\nreplace github.com/Gleamseekers/AgentPrimordia => ../../agentprimordia\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatalf("写入 go.mod 失败: %v", err)
	}

	env := isolatedGoEnv(dir)
	if !hasEnvEntry(env, "GOWORK", "off") {
		t.Errorf("本地 replace 项目应设置 GOWORK=off，实际环境未命中")
	}
	// 不应产生重复的 GOWORK 项
	if n := countEnvKey(env, "GOWORK"); n != 1 {
		t.Errorf("GOWORK 项数 = %d, want 1", n)
	}

	// standalone 项目不改动 GOWORK
	plain := t.TempDir()
	if err := os.WriteFile(filepath.Join(plain, "go.mod"), []byte("module demo\n"), 0o644); err != nil {
		t.Fatalf("写入 go.mod 失败: %v", err)
	}
	if hasEnvEntry(isolatedGoEnv(plain), "GOWORK", "off") {
		t.Errorf("standalone 项目不应设置 GOWORK=off")
	}
}

// TestStartNeverMutatesUserGoWork 是安全不变式守卫：
// `ap start` 绝不允许调用 `go work use` 改写用户的 go.work。
// 该不变式曾因历史实现被破坏（在仓库内运行 ap start 会静默改写根 go.work）。
//
// 只匹配"实际构造该命令"的实参形态（exec.Command("go", "work", "use", ...)），
// 不匹配提示文案或注释中对 `go work use` 的文字引用。
func TestStartNeverMutatesUserGoWork(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("读取 cmd/ap 目录失败: %v", err)
	}
	forbidden := []string{`"work", "use"`, `"work","use"`}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", name, err)
		}
		content := string(data)
		for _, f := range forbidden {
			if strings.Contains(content, f) {
				t.Errorf("%s 含被禁止的 go.work 改写调用 %q——ap 不得改写用户 go.work", name, f)
			}
		}
	}
}

func hasEnvEntry(env []string, key, value string) bool {
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) && strings.TrimPrefix(e, prefix) == value {
			return true
		}
	}
	return false
}

func countEnvKey(env []string, key string) int {
	prefix := key + "="
	n := 0
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			n++
		}
	}
	return n
}
