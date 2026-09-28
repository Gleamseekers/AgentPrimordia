package tools

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
)

// TestBuildMCPSubprocessEnv_最小白名单 验证子进程环境为最小白名单 + 用户显式配置：
// 宿主敏感变量（如 API Key）不泄露，白名单必要项保留，用户配置可覆盖且去重。
func TestBuildMCPSubprocessEnv_最小白名单(t *testing.T) {
	t.Setenv("AP_MCP_TEST_SECRET", "leak-me")
	t.Setenv("PATH", "/usr/local/bin:/usr/bin:/bin")

	env := buildMCPSubprocessEnv(map[string]string{
		"MCP_CUSTOM": "v1",
		"PATH":       "/custom/bin", // 用户配置覆盖白名单项
	})

	got := make(map[string]string, len(env))
	pathCount := 0
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		got[k] = v
		if k == "PATH" {
			pathCount++
		}
	}

	if _, ok := got["AP_MCP_TEST_SECRET"]; ok {
		t.Error("宿主敏感变量不应传递给 MCP 子进程")
	}
	if got["MCP_CUSTOM"] != "v1" {
		t.Errorf("用户显式配置的环境变量应传递，got %q", got["MCP_CUSTOM"])
	}
	if got["PATH"] != "/custom/bin" {
		t.Errorf("用户配置应覆盖白名单变量，PATH = %q", got["PATH"])
	}
	if pathCount != 1 {
		t.Errorf("PATH 应唯一（去重），实际出现 %d 次", pathCount)
	}
	if home := os.Getenv("HOME"); home != "" && got["HOME"] != home {
		t.Errorf("HOME 应在白名单中保留，got %q, want %q", got["HOME"], home)
	}
}

// TestBuildMCPSubprocessEnv_空配置也过滤宿主环境 验证用户未配置环境变量时，
// 子进程同样只获得白名单变量（旧实现 cmd.Env 为 nil 时全量继承宿主环境）。
func TestBuildMCPSubprocessEnv_空配置也过滤宿主环境(t *testing.T) {
	t.Setenv("AP_MCP_TEST_SECRET", "leak-me")

	env := buildMCPSubprocessEnv(nil)
	for _, kv := range env {
		if strings.HasPrefix(kv, "AP_MCP_TEST_SECRET=") {
			t.Fatal("空配置时也不应继承宿主敏感变量")
		}
	}
}

// TestMCPRegistry_StartList_并发无数据竞争 验证并发 Start/List 无数据竞争
// （-race 下运行）：Start 中修改 entry.Config.Command 必须持锁，
// 否则与 List() 的结构体拷贝读取构成数据竞争。
func TestMCPRegistry_StartList_并发无数据竞争(t *testing.T) {
	r := NewMCPRegistry()
	// 空命令：Start 快速失败（不启动真实子进程），便于高频并发触发竞争窗口
	r.Register(MCPClientConfig{Name: "s1"})

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = r.Start(context.Background(), "s1")
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				entries := r.List() // List 持 RLock 拷贝，安全读取 Config 字段
				for _, e := range entries {
					_ = e.Config.Command
				}
				if _, ok := r.Get("s1"); !ok {
					t.Error("s1 应始终可获取")
				}
			}
		}()
	}
	wg.Wait()
}

// TestMCPRegistry_Start_解析命令持锁更新 验证 Start 后 entry.Config.Command
// 被解析结果更新（Windows npx.cmd 兼容），且 List 能看到一致的解析值。
func TestMCPRegistry_Start_解析命令持锁更新(t *testing.T) {
	r := NewMCPRegistry()
	r.Register(MCPClientConfig{Name: "s1", Command: ""})

	_ = r.Start(context.Background(), "s1")

	entries := r.List()
	if len(entries) != 1 {
		t.Fatalf("期望 1 个条目，实际 %d", len(entries))
	}
	// 空命令解析后仍为空，状态应为 failed
	if entries[0].Status != MCPClientFailed {
		t.Errorf("期望状态 failed, got %s", entries[0].Status)
	}
	if entries[0].Config.Command != "" {
		t.Errorf("解析后命令应为空，got %q", entries[0].Config.Command)
	}
}

// TestMCPRegistry_StartAll_并发Stop 验证 StartAll 与 Stop/StopAll 并发执行
// 时的基本并发安全性（-race 下运行）。
func TestMCPRegistry_StartAll_并发Stop(t *testing.T) {
	r := NewMCPRegistry()
	for i := 0; i < 4; i++ {
		r.Register(MCPClientConfig{Name: string(rune('a' + i))})
	}

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(3)
		go func() { defer wg.Done(); _ = r.StartAll(context.Background()) }()
		go func() { defer wg.Done(); _ = r.List() }()
		go func() { defer wg.Done(); r.StopAll() }()
	}
	wg.Wait()
}
