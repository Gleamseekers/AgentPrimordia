package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestGenerate_Basic 验证 basic 模板渲染出预期文件清单。
func TestGenerate_Basic(t *testing.T) {
	files, err := Generate(GenerateOptions{
		Name:     "demo",
		Template: "basic",
	})
	if err != nil {
		t.Fatalf("Generate 失败: %v", err)
	}
	want := []string{".ap.yaml", ".gitignore", "go.mod", "main.go"}
	got := make([]string, 0, len(files))
	for k := range files {
		got = append(got, k)
	}
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("文件清单不匹配: got %v, want %v", got, want)
	}
}

// TestGenerate_ProjectName 验证 {{.ProjectName}} 已被替换。
func TestGenerate_ProjectName(t *testing.T) {
	files, err := Generate(GenerateOptions{
		Name:     "hello-world",
		Template: "basic",
	})
	if err != nil {
		t.Fatalf("Generate 失败: %v", err)
	}
	main, ok := files["main.go"]
	if !ok {
		t.Fatal("缺 main.go")
	}
	if !strings.Contains(string(main), `"hello-world"`) {
		t.Error("main.go 未包含替换后的 ProjectName")
	}
	if strings.Contains(string(main), "{{.ProjectName}}") {
		t.Error("main.go 仍含未替换的 {{.ProjectName}}")
	}
}

// TestGenerate_UnknownTemplate 验证未知模板返回错误。
func TestGenerate_UnknownTemplate(t *testing.T) {
	_, err := Generate(GenerateOptions{
		Name:     "demo",
		Template: "does-not-exist",
	})
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
}

// TestGenerate_EmptyName 验证空 name 返回错误。
func TestGenerate_EmptyName(t *testing.T) {
	_, err := Generate(GenerateOptions{
		Name:     "",
		Template: "basic",
	})
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
}

// TestGenerate_DryRunFlag 验证 DryRun 选项不会让 Generate 失败（行为与正式模式相同，
// 实际"不写盘"是 runInit 包装层的事，Generate 本身只生成文件树）。
func TestGenerate_DryRunFlag(t *testing.T) {
	files, err := Generate(GenerateOptions{
		Name:     "demo",
		Template: "basic",
		DryRun:   true,
	})
	if err != nil {
		t.Fatalf("Generate 失败: %v", err)
	}
	if len(files) == 0 {
		t.Error("DryRun 也应返回文件清单")
	}
}

// TestGenerate_ApYamlContent 验证 .ap.yaml 关键字段存在。
func TestGenerate_ApYamlContent(t *testing.T) {
	files, err := Generate(GenerateOptions{
		Name:     "demo",
		Template: "basic",
	})
	if err != nil {
		t.Fatalf("Generate 失败: %v", err)
	}
	ap, ok := files[".ap.yaml"]
	if !ok {
		t.Fatal("缺 .ap.yaml")
	}
	s := string(ap)
	if !strings.Contains(s, "name: demo") {
		t.Error(".ap.yaml 缺 name: demo")
	}
	if !strings.Contains(s, "template: basic") {
		t.Error(".ap.yaml 缺 template: basic")
	}
}

// ===== v7.6：standalone go.mod 版本修复 + 提示文案修正（脚手架修复任务）=====
//
// 背景：standalone 场景（探测不到本地框架源码）生成的 go.mod 曾把框架 require
// 写死为旧占位 v7.0.0，外部用户 go mod tidy 后锁死在旧版而非当前发布版；
// 且 init 的 replace 提示文案漏写 /v7（用户照抄后 tidy 报 unknown module）。
// 本组测试锁定修复行为：Version 注入优先、dev 回退占位、init 自动 go get @latest、
// 提示文案带 /v7；网络部分经 goGetFrameworkLatest 注入替身，测试不触网。

// TestFrameworkRequireVersion_Injected Version 经 ldflags 注入合法 semver 时，
// require 直接写当前发布版（发布二进制离线也准确）。
func TestFrameworkRequireVersion_Injected(t *testing.T) {
	orig := Version
	defer func() { Version = orig }()
	cases := map[string]string{
		"7.5.1":     "v7.5.1",
		"v7.5.1":    "v7.5.1",
		"7.6.0-rc1": "v7.6.0-rc1",
	}
	for in, want := range cases {
		Version = in
		if got := frameworkRequireVersion(); got != want {
			t.Errorf("Version=%q: 期望 %q，实际 %q", in, want, got)
		}
	}
}

// TestFrameworkRequireVersion_DevFallback dev 构建（Version 非 semver，如 dev /
// ci-<sha>）回退占位版本，由 init 的 go get @latest 升级到代理当前发布版。
func TestFrameworkRequireVersion_DevFallback(t *testing.T) {
	orig := Version
	defer func() { Version = orig }()
	for _, in := range []string{"dev", "", "ci-abc123", "v7", "7.5", "latest"} {
		Version = in
		if got := frameworkRequireVersion(); got != apRequirePlaceholder {
			t.Errorf("Version=%q: 期望回退占位 %q，实际 %q", in, apRequirePlaceholder, got)
		}
	}
}

// TestBuildGoMod_StandalonePlaceholder standalone 分支生成的 go.mod：
// require 落在 frameworkRequireVersion()、不含 replace。
func TestBuildGoMod_StandalonePlaceholder(t *testing.T) {
	t.Setenv("AP_ROOT", "")
	content, standalone := buildGoMod("my-agent", filepath.Join(t.TempDir(), "my-agent"))
	if !standalone {
		t.Fatal("空目录应判定为 standalone")
	}
	want := "github.com/Gleamseekers/AgentPrimordia/v7 " + frameworkRequireVersion()
	if !strings.Contains(content, want) {
		t.Errorf("standalone go.mod 应包含 require %s:\n%s", want, content)
	}
	if strings.Contains(content, "replace") {
		t.Errorf("standalone 场景不应包含 replace:\n%s", content)
	}
}

// TestBuildGoMod_StandaloneInjectedVersion Version 注入时，standalone require
// 用注入的当前版本而非旧占位 v7.0.0。
func TestBuildGoMod_StandaloneInjectedVersion(t *testing.T) {
	orig := Version
	defer func() { Version = orig }()
	t.Setenv("AP_ROOT", "")
	Version = "7.6.0"
	content, _ := buildGoMod("my-agent", filepath.Join(t.TempDir(), "my-agent"))
	if !strings.Contains(content, "github.com/Gleamseekers/AgentPrimordia/v7 v7.6.0") {
		t.Errorf("standalone go.mod 应包含注入版本 v7.6.0:\n%s", content)
	}
	if strings.Contains(content, "v7.0.0") {
		t.Errorf("standalone go.mod 不应再写死旧占位 v7.0.0:\n%s", content)
	}
}

// TestStandaloneHint_ModulePathHasV7 standalone 提示文案的 replace 模块路径
// 必须带 /v7（v7.6 修复：曾漏写 /v7，用户照抄后 tidy 报 unknown module）。
// 覆盖两个调用方变体：ap init（自动解析）与 ap plugin create（手动解析）。
func TestStandaloneHint_ModulePathHasV7(t *testing.T) {
	for _, autoResolve := range []bool{true, false} {
		hint := standaloneHint(autoResolve)
		if !strings.Contains(hint, "replace github.com/Gleamseekers/AgentPrimordia/v7 =>") {
			t.Errorf("autoResolve=%v: 提示文案的 replace 路径应带 /v7:\n%s", autoResolve, hint)
		}
		if strings.Contains(hint, "AgentPrimordia/agentprimordia") {
			t.Errorf("autoResolve=%v: 提示文案不应再出现缺 /v7 的旧路径:\n%s", autoResolve, hint)
		}
		if !strings.Contains(hint, "go get github.com/Gleamseekers/AgentPrimordia/v7@latest") {
			t.Errorf("autoResolve=%v: 提示文案应指引 go get @latest 解析当前发布版:\n%s", autoResolve, hint)
		}
	}
}

// TestRunInit_Standalone_GoGetLatest standalone + 未 --no-tidy：init 应先移除
// 占位 require（自愈设计：占位未必已发布），再触发 go get @latest（hook 注入
// 替身返回错误，避免 tidy 触网）；失败时须打印可执行的手动提示。
func TestRunInit_Standalone_GoGetLatest(t *testing.T) {
	origHook := goGetFrameworkLatest
	defer func() { goGetFrameworkLatest = origHook }()
	t.Setenv("AP_ROOT", "")

	var calls int
	goGetFrameworkLatest = func(dir string) error {
		calls++
		if dir == "" {
			t.Error("go get 的目录不应为空")
		}
		return fmt.Errorf("模拟代理不可用（测试替身，不触网）")
	}

	tmp := t.TempDir()
	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir 失败: %v", err)
	}

	out := captureStdout(t, func() {
		if err := runInit([]string{"demo"}); err != nil {
			t.Errorf("runInit 失败: %v", err)
		}
	})

	if calls != 1 {
		t.Errorf("standalone 模式应触发一次 go get @latest，实际 %d 次", calls)
	}
	if !strings.Contains(out, "go get github.com/Gleamseekers/AgentPrimordia/v7@latest") {
		t.Errorf("stdout 应包含手动提示 go get ...@latest:\n%s", out)
	}
	mod, err := os.ReadFile(filepath.Join(tmp, "demo", "go.mod"))
	if err != nil {
		t.Fatalf("读取 go.mod 失败: %v", err)
	}
	s := string(mod)
	if !strings.Contains(s, "module demo") {
		t.Errorf("go.mod 应正常生成:\n%s", s)
	}
	// 自愈设计：占位 require 已被移除，用户随后手动 go mod tidy 也能落到 @latest
	if strings.Contains(s, "require github.com/Gleamseekers/AgentPrimordia/v7") {
		t.Errorf("standalone 失败路径应已移除占位 require（交由 go get @latest 重建）:\n%s", s)
	}
}

// TestRunInit_Standalone_NoTidy_SkipsGet --no-tidy 时不做任何网络/依赖操作，
// go.mod 保留占位 require 并打印 standalone 提示。
func TestRunInit_Standalone_NoTidy_SkipsGet(t *testing.T) {
	origHook := goGetFrameworkLatest
	defer func() { goGetFrameworkLatest = origHook }()
	t.Setenv("AP_ROOT", "")

	called := false
	goGetFrameworkLatest = func(dir string) error {
		called = true
		return nil
	}

	tmp := t.TempDir()
	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir 失败: %v", err)
	}

	out := captureStdout(t, func() {
		if err := runInit([]string{"demo", "--no-tidy"}); err != nil {
			t.Errorf("runInit 失败: %v", err)
		}
	})

	if called {
		t.Error("--no-tidy 不应触发 go get @latest")
	}
	if !strings.Contains(out, "replace github.com/Gleamseekers/AgentPrimordia/v7 =>") {
		t.Errorf("--no-tidy 的 standalone 场景仍应打印带 /v7 的提示:\n%s", out)
	}
}

// captureStdout 在 fn 执行期间捕获 os.Stdout 输出（infof/Printf 均写 os.Stdout）。
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("创建管道失败: %v", err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		var sb strings.Builder
		_, _ = io.Copy(&sb, r)
		done <- sb.String()
	}()
	fn()
	_ = w.Close()
	os.Stdout = old
	return <-done
}
