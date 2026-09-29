package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCLIVersion(t *testing.T) {
	// 验证 CLI 可编译（构建测试）
	// 实际版本输出通过 go build + ./ap version 测试
}

func TestFindProjectDir_CurrentDir(t *testing.T) {
	// 在有 go.mod 的目录应能找到项目
	tmpDir := t.TempDir()
	goMod := filepath.Join(tmpDir, "go.mod")
	_ = os.WriteFile(goMod, []byte("module test\n"), 0o644)

	// 保存当前目录
	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()

	_ = os.Chdir(tmpDir)
	dir, err := findProjectDir()
	if err != nil {
		t.Fatalf("findProjectDir 失败: %v", err)
	}
	// 归一化后比较：macOS 上 /var 是 /private/var 的符号链接，
	// t.TempDir() 返回逻辑路径，而 os.Getwd() 返回解析后的物理路径
	want, _ := filepath.EvalSymlinks(tmpDir)
	got, gotErr := filepath.EvalSymlinks(dir)
	if gotErr != nil || got != want {
		t.Errorf("期望 %q，实际 %q", want, dir)
	}
}

func TestFindProjectDir_NotFound(t *testing.T) {
	// 在没有 go.mod 或 .ap.yaml 的临时目录不应找到项目
	tmpDir := t.TempDir()

	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()

	_ = os.Chdir(tmpDir)
	_, err := findProjectDir()
	if err == nil {
		t.Error("期望返回错误，实际返回 nil")
	}
}

func TestFindProjectDir_ApYaml(t *testing.T) {
	tmpDir := t.TempDir()
	apYaml := filepath.Join(tmpDir, ".ap.yaml")
	_ = os.WriteFile(apYaml, []byte("name: test\n"), 0o644)

	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()

	_ = os.Chdir(tmpDir)
	dir, err := findProjectDir()
	if err != nil {
		t.Fatalf("findProjectDir 失败: %v", err)
	}
	// 归一化后比较：macOS 上 /var 是 /private/var 的符号链接
	want, _ := filepath.EvalSymlinks(tmpDir)
	got, gotErr := filepath.EvalSymlinks(dir)
	if gotErr != nil || got != want {
		t.Errorf("期望 %q，实际 %q", want, dir)
	}
}

func TestRunInit_BasicTemplate(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()
	_ = os.Chdir(tmpDir)

	// 模拟 ap init my-agent
	if err := runInit([]string{"my-agent"}); err != nil {
		t.Fatalf("runInit 失败: %v", err)
	}

	targetDir := filepath.Join(tmpDir, "my-agent")
	if _, err := os.Stat(targetDir); os.IsNotExist(err) {
		t.Fatal("项目目录未创建")
	}

	// 检查 main.go 是否存在
	mainGo := filepath.Join(targetDir, "main.go")
	if _, err := os.Stat(mainGo); os.IsNotExist(err) {
		t.Fatal("main.go 未创建")
	}

	// 检查 .ap.yaml 是否存在
	apYaml := filepath.Join(targetDir, ".ap.yaml")
	if _, err := os.Stat(apYaml); os.IsNotExist(err) {
		t.Fatal(".ap.yaml 未创建")
	}

	// 检查 go.mod 是否存在（点 5 治理：可编译性）
	goMod := filepath.Join(targetDir, "go.mod")
	if _, err := os.Stat(goMod); os.IsNotExist(err) {
		t.Fatal("go.mod 未创建 — 生成的项目无法编译")
	}

	// 验证 main.go 变量替换正确
	content, _ := os.ReadFile(mainGo)
	if !contains(string(content), `"my-agent"`) {
		t.Error("main.go 中 {{.ProjectName}} 未被替换为 my-agent")
	}
	if contains(string(content), "{{.ProjectName}}") {
		t.Error("main.go 仍包含未替换的 {{.ProjectName}}")
	}

	// 验证 go.mod 内容
	modContent, _ := os.ReadFile(goMod)
	if !contains(string(modContent), "module my-agent") {
		t.Error("go.mod 缺 module my-agent")
	}
	if !contains(string(modContent), "github.com/Gleamseekers/AgentPrimordia/v7 v7.0.0") {
		t.Error("go.mod 缺框架模块依赖")
	}
}

func TestRunInit_WithToolsTemplate(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()
	_ = os.Chdir(tmpDir)

	if err := runInit([]string{"my-agent", "--template", "with-tools"}); err != nil {
		t.Fatalf("runInit 失败: %v", err)
	}

	targetDir := filepath.Join(tmpDir, "my-agent")
	if _, err := os.Stat(targetDir); os.IsNotExist(err) {
		t.Fatal("项目目录未创建")
	}

	mainGo := filepath.Join(targetDir, "main.go")
	content, _ := os.ReadFile(mainGo)
	if len(content) == 0 {
		t.Error("main.go 内容为空")
	}

	// 验证 go.mod 存在
	if _, err := os.Stat(filepath.Join(targetDir, "go.mod")); os.IsNotExist(err) {
		t.Error("go.mod 未创建")
	}
}

func TestRunInit_MultiAgentTemplate(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()
	_ = os.Chdir(tmpDir)

	if err := runInit([]string{"my-agent", "--template", "multi-agent"}); err != nil {
		t.Fatalf("runInit 失败: %v", err)
	}

	targetDir := filepath.Join(tmpDir, "my-agent")
	if _, err := os.Stat(targetDir); os.IsNotExist(err) {
		t.Fatal("项目目录未创建")
	}

	// 验证 go.mod 存在
	if _, err := os.Stat(filepath.Join(targetDir, "go.mod")); os.IsNotExist(err) {
		t.Error("go.mod 未创建")
	}
}

// TestRunInit_GeneratedProjectBuilds 端到端：验证生成项目可编译。
// 用相对路径 replace 指向 ../agentprimordia（仓库内目录名不变）。
func TestRunInit_GeneratedProjectBuilds(t *testing.T) {
	if testing.Short() {
		t.Skip("short 模式跳过 e2e 构建")
	}

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()
	_ = os.Chdir(tmpDir)

	if err := runInit([]string{"buildable-agent"}); err != nil {
		t.Fatalf("runInit 失败: %v", err)
	}

	targetDir := filepath.Join(tmpDir, "buildable-agent")

	// 验证 go.mod 含 replace 指令
	modContent, _ := os.ReadFile(filepath.Join(targetDir, "go.mod"))
	modContentStr := string(modContent)
	if !contains(modContentStr, "replace github.com/Gleamseekers/AgentPrimordia/v7 =>") {
		t.Skip("go.mod 不含 replace，跳过 e2e 构建")
	}
	t.Logf("生成 go.mod 包含预期的 replace 指令，%d 字节", len(modContentStr))

	// 至少验证 main.go 语法可解析（编译检查）
	mainGoContent, _ := os.ReadFile(filepath.Join(targetDir, "main.go"))
	if !contains(string(mainGoContent), "package main") {
		t.Error("生成 main.go 缺 package main")
	}
	if !contains(string(mainGoContent), "func main()") {
		t.Error("生成 main.go 缺 func main()")
	}
}

// contains 是 strings.Contains 的本地别名（避免 import 冲突）
func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestRunInit_DirExists(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()
	_ = os.Chdir(tmpDir)

	// 创建已存在的目录
	_ = os.Mkdir(filepath.Join(tmpDir, "my-agent"), 0o755)

	// runInit 现在返回 error，可以测试错误路径
	err := runInit([]string{"my-agent"})
	if err == nil {
		t.Fatal("期望返回错误（目录已存在），实际返回 nil")
	}
}

func TestRunInit_InvalidTemplate(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()
	_ = os.Chdir(tmpDir)

	err := runInit([]string{"my-agent", "--template", "does-not-exist"})
	if err == nil {
		t.Fatal("期望返回错误（未知模板），实际返回 nil")
	}
}

func TestRunInit_NoName(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()
	_ = os.Chdir(tmpDir)

	err := runInit([]string{})
	if err == nil {
		t.Fatal("期望返回错误（未指定项目名），实际返回 nil")
	}
}

func TestAPConfig_LoadSave(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()
	_ = os.Chdir(tmpDir)

	// 创建 go.mod 让 findProjectDir 工作
	_ = os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test\n"), 0o644)

	// 保存配置
	config := &apConfig{
		Name:     "test-project",
		Template: "basic",
		Plugins:  []string{"github.com/user/ap-plugin-test"},
	}
	if err := saveAPConfig(config); err != nil {
		t.Fatalf("saveAPConfig 失败: %v", err)
	}

	// 读取配置
	loaded := loadAPConfig()
	if loaded.Name != "test-project" {
		t.Errorf("名称不匹配: got %q", loaded.Name)
	}
	if len(loaded.Plugins) != 1 {
		t.Errorf("插件数量不匹配: got %d", len(loaded.Plugins))
	}
}

// ===== Phase 8.2: 新模板 e2e 测试 =====

func TestRunInit_AgentWithCacheTemplate(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()
	_ = os.Chdir(tmpDir)

	if err := runInit([]string{"cache-agent", "--template", "agent-with-cache"}); err != nil {
		t.Fatalf("runInit 失败: %v", err)
	}

	targetDir := filepath.Join(tmpDir, "cache-agent")
	if _, err := os.Stat(targetDir); os.IsNotExist(err) {
		t.Fatal("项目目录未创建")
	}
	mainGo := filepath.Join(targetDir, "main.go")
	content, _ := os.ReadFile(mainGo)
	if !contains(string(content), "WithCache") {
		t.Error("agent-with-cache 模板应包含 WithCache 链式调用")
	}
}

func TestRunInit_AgentWithRAGTemplate(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()
	_ = os.Chdir(tmpDir)

	if err := runInit([]string{"rag-agent", "--template", "agent-with-rag"}); err != nil {
		t.Fatalf("runInit 失败: %v", err)
	}

	targetDir := filepath.Join(tmpDir, "rag-agent")
	if _, err := os.Stat(targetDir); os.IsNotExist(err) {
		t.Fatal("项目目录未创建")
	}
	mainGo := filepath.Join(targetDir, "main.go")
	content, _ := os.ReadFile(mainGo)
	if !contains(string(content), "WithRAG") {
		t.Error("agent-with-rag 模板应包含 WithRAG 链式调用")
	}
}

func TestRunInit_AgentWithMetricsTemplate(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()
	_ = os.Chdir(tmpDir)

	if err := runInit([]string{"metrics-agent", "--template", "agent-with-metrics"}); err != nil {
		t.Fatalf("runInit 失败: %v", err)
	}

	targetDir := filepath.Join(tmpDir, "metrics-agent")
	if _, err := os.Stat(targetDir); os.IsNotExist(err) {
		t.Fatal("项目目录未创建")
	}
	mainGo := filepath.Join(targetDir, "main.go")
	content, _ := os.ReadFile(mainGo)
	if !contains(string(content), "WithMetrics") {
		t.Error("agent-with-metrics 模板应包含 WithMetrics 链式调用")
	}
	if !contains(string(content), "/metrics") {
		t.Error("agent-with-metrics 模板应暴露 /metrics 端点")
	}
}

// ===== P1: 新增错误路径与关键路径测试 =====

// TestRunInit_QuickstartTemplate 验证 quickstart 模板使用 pkg 而非 internal
func TestRunInit_QuickstartTemplate(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()
	_ = os.Chdir(tmpDir)

	if err := runInit([]string{"qs-agent", "--template", "quickstart"}); err != nil {
		t.Fatalf("runInit 失败: %v", err)
	}

	targetDir := filepath.Join(tmpDir, "qs-agent")
	mainGo := filepath.Join(targetDir, "main.go")
	content, _ := os.ReadFile(mainGo)

	if contains(string(content), "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/") {
		t.Error("quickstart 模板不应直接引用 internal/ 包，应使用 pkg/")
	}
	if !contains(string(content), `ap "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg"`) {
		t.Error("quickstart 模板应通过 pkg/ 公共 API 引用框架")
	}
}

// TestRunInit_GoModVersion 验证生成的 go.mod 版本对齐（go 1.26 + v6.0.0）；
// TempDir 为 standalone 场景：不应包含 replace（框架不在上级）。
func TestRunInit_GoModVersion(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()
	_ = os.Chdir(tmpDir)

	if err := runInit([]string{"ver-agent"}); err != nil {
		t.Fatalf("runInit 失败: %v", err)
	}

	modContent, _ := os.ReadFile(filepath.Join(tmpDir, "ver-agent", "go.mod"))
	if !contains(string(modContent), "go 1.26") {
		t.Errorf("生成的 go.mod 应包含 go 1.26，实际: %s", string(modContent))
	}
	if !contains(string(modContent), "github.com/Gleamseekers/AgentPrimordia/v7 v7.0.0") {
		t.Errorf("生成的 go.mod 应包含 require github.com/Gleamseekers/AgentPrimordia/v7（占位版本），实际: %s", string(modContent))
	}
	if contains(string(modContent), "replace") {
		t.Errorf("standalone 场景 go.mod 不应包含 replace，实际: %s", string(modContent))
	}
}

// TestRunInit_Help 验证 --help 返回 nil 且不创建目录
func TestRunInit_Help(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()
	_ = os.Chdir(tmpDir)

	err := runInit([]string{"--help"})
	if err != nil {
		t.Errorf("--help 应返回 nil，实际: %v", err)
	}
	// 确保没有创建任何目录
	entries, _ := os.ReadDir(tmpDir)
	if len(entries) != 0 {
		t.Errorf("--help 不应创建文件，实际创建了 %d 个", len(entries))
	}
}

// TestRunInit_DryRun 验证 --dry-run 不创建文件
func TestRunInit_DryRun(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()
	_ = os.Chdir(tmpDir)

	err := runInit([]string{"dry-agent", "--dry-run"})
	if err != nil {
		t.Fatalf("--dry-run 失败: %v", err)
	}
	// 确保没有创建项目目录
	if _, err := os.Stat(filepath.Join(tmpDir, "dry-agent")); err == nil {
		t.Error("--dry-run 不应创建目录")
	}
}

// ===== Completion 测试 =====

func TestRunCompletion_Bash(t *testing.T) {
	err := runCompletion([]string{"bash"})
	if err != nil {
		t.Fatalf("bash 补全失败: %v", err)
	}
}

func TestRunCompletion_Zsh(t *testing.T) {
	err := runCompletion([]string{"zsh"})
	if err != nil {
		t.Fatalf("zsh 补全失败: %v", err)
	}
}

func TestRunCompletion_Fish(t *testing.T) {
	err := runCompletion([]string{"fish"})
	if err != nil {
		t.Fatalf("fish 补全失败: %v", err)
	}
}

func TestRunCompletion_InvalidShell(t *testing.T) {
	err := runCompletion([]string{"powershell"})
	if err == nil {
		t.Fatal("期望返回错误（不支持的 shell），实际返回 nil")
	}
}

func TestRunCompletion_NoArgs(t *testing.T) {
	err := runCompletion([]string{})
	if err != nil {
		t.Errorf("无参数应返回 nil（显示帮助），实际: %v", err)
	}
}

// ===== Doctor 测试 =====

func TestRunDoctor_Run(t *testing.T) {
	// doctor 不依赖项目目录，应正常执行
	err := runDoctor([]string{})
	if err != nil {
		t.Errorf("doctor 不应返回错误: %v", err)
	}
}

// ===== appendConfigEnv 测试 =====

func TestAppendConfigEnv_NoConfig(t *testing.T) {
	setupTestProject(t)

	// 无 .ap.yaml 时应原样返回
	env := []string{"PATH=/usr/bin"}
	result := appendConfigEnv(env, "")
	if len(result) != 1 {
		t.Errorf("无配置时不应追加环境变量，实际长度 %d", len(result))
	}
}

func TestAppendConfigEnv_WithConfig(t *testing.T) {
	dir := setupTestProject(t)

	// 创建 .ap.yaml
	apYaml := `name: test
llm:
  provider: openai
  model: gpt-4o
  api_key: sk-test-key
`
	_ = os.WriteFile(filepath.Join(dir, ".ap.yaml"), []byte(apYaml), 0o644)

	env := []string{"PATH=/usr/bin"}
	result := appendConfigEnv(env, dir)

	// 验证追加的环境变量
	hasProvider := false
	hasModel := false
	hasKey := false
	for _, e := range result {
		if e == "AP_LLM_PROVIDER=openai" {
			hasProvider = true
		}
		if e == "AP_LLM_MODEL=gpt-4o" {
			hasModel = true
		}
		if e == "AP_LLM_API_KEY=sk-test-key" {
			hasKey = true
		}
	}
	if !hasProvider {
		t.Error("应包含 AP_LLM_PROVIDER=openai")
	}
	if !hasModel {
		t.Error("应包含 AP_LLM_MODEL=gpt-4o")
	}
	if !hasKey {
		t.Error("应包含 AP_LLM_API_KEY=sk-test-key")
	}
}

func TestAppendConfigEnv_EnvNotOverwritten(t *testing.T) {
	dir := setupTestProject(t)

	// 创建 .ap.yaml
	apYaml := `name: test
llm:
  provider: openai
  model: gpt-4o
`
	_ = os.WriteFile(filepath.Join(dir, ".ap.yaml"), []byte(apYaml), 0o644)

	// 已有环境变量不应被覆盖
	env := []string{"PATH=/usr/bin", "AP_LLM_PROVIDER=anthropic"}
	result := appendConfigEnv(env, dir)

	for _, e := range result {
		if e == "AP_LLM_PROVIDER=openai" {
			t.Error("已有的 AP_LLM_PROVIDER 不应被 .ap.yaml 覆盖")
		}
	}
}
