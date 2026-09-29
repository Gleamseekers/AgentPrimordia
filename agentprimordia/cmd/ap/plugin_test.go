package main

import (
	"os"
	"path/filepath"
	"testing"
)

// ===== Plugin list 测试 =====

func TestPluginList_Empty(t *testing.T) {
	setupTestProject(t)

	err := pluginList()
	if err != nil {
		t.Fatalf("pluginList 失败: %v", err)
	}
}

func TestPluginList_WithPlugins(t *testing.T) {
	dir := setupTestProject(t)

	apYaml := `name: test
plugins:
  - github.com/user/ap-plugin-slack
  - github.com/user/ap-plugin-weather
`
	_ = os.WriteFile(filepath.Join(dir, ".ap.yaml"), []byte(apYaml), 0o644)

	err := pluginList()
	if err != nil {
		t.Fatalf("pluginList 失败: %v", err)
	}
}

// ===== Plugin create 测试 =====

func TestPluginCreate_Success(t *testing.T) {
	dir := setupTestProject(t)

	err := pluginCreate([]string{"ap-plugin-test"})
	if err != nil {
		t.Fatalf("pluginCreate 失败: %v", err)
	}

	// 验证目录结构
	pluginDir := filepath.Join(dir, "ap-plugin-test")
	if _, err := os.Stat(pluginDir); os.IsNotExist(err) {
		t.Fatal("插件目录未创建")
	}
	if _, err := os.Stat(filepath.Join(pluginDir, "plugin.go")); os.IsNotExist(err) {
		t.Error("plugin.go 未创建")
	}
	if _, err := os.Stat(filepath.Join(pluginDir, "go.mod")); os.IsNotExist(err) {
		t.Error("go.mod 未创建")
	}
	if _, err := os.Stat(filepath.Join(pluginDir, "README.md")); os.IsNotExist(err) {
		t.Error("README.md 未创建")
	}

	// 验证 go.mod 版本
	modContent, _ := os.ReadFile(filepath.Join(pluginDir, "go.mod"))
	if !contains(string(modContent), "go 1.26") {
		t.Error("plugin go.mod 应包含 go 1.26")
	}
	if !contains(string(modContent), "github.com/Gleamseekers/AgentPrimordia v0.0.0") {
		t.Error("plugin go.mod 应包含 require github.com/Gleamseekers/AgentPrimordia（占位版本）")
	}

	// 验证 plugin.go 内容
	pluginCode, _ := os.ReadFile(filepath.Join(pluginDir, "plugin.go"))
	if !contains(string(pluginCode), "package ap_plugin_test") {
		t.Error("plugin.go 应包含 package ap_plugin_test")
	}
	if !contains(string(pluginCode), "ap-plugin-test") {
		t.Error("plugin.go 应包含插件名称 ap-plugin-test")
	}
}

func TestPluginCreate_DirExists(t *testing.T) {
	dir := setupTestProject(t)

	// 创建已存在的目录
	_ = os.Mkdir(filepath.Join(dir, "existing-plugin"), 0o755)

	err := pluginCreate([]string{"existing-plugin"})
	if err == nil {
		t.Fatal("期望返回错误（目录已存在），实际返回 nil")
	}
}

func TestPluginCreate_NoName(t *testing.T) {
	setupTestProject(t)

	err := pluginCreate([]string{})
	if err == nil {
		t.Fatal("期望返回错误（未指定名称），实际返回 nil")
	}
}

// ===== Plugin remove 测试 =====

func TestPluginRemove_NotInstalled(t *testing.T) {
	setupTestProject(t)

	err := pluginRemove([]string{"github.com/nonexistent/plugin"})
	if err == nil {
		t.Fatal("期望返回错误（插件未安装），实际返回 nil")
	}
}

func TestPluginRemove_NoName(t *testing.T) {
	setupTestProject(t)

	err := pluginRemove([]string{})
	if err == nil {
		t.Fatal("期望返回错误（未指定名称），实际返回 nil")
	}
}

func TestPluginRemove_Success(t *testing.T) {
	dir := setupTestProject(t)

	// 写入含插件的配置
	apYaml := `name: test
plugins:
  - github.com/user/ap-plugin-test
`
	_ = os.WriteFile(filepath.Join(dir, ".ap.yaml"), []byte(apYaml), 0o644)

	err := pluginRemove([]string{"github.com/user/ap-plugin-test"})
	if err != nil {
		t.Fatalf("pluginRemove 失败: %v", err)
	}

	// 验证已从配置中移除
	config := loadAPConfig()
	for _, p := range config.Plugins {
		if p == "github.com/user/ap-plugin-test" {
			t.Error("插件应已从配置中移除")
		}
	}
}

// ===== Plugin dispatch 测试 =====

func TestRunPlugin_NoArgs(t *testing.T) {
	err := runPlugin([]string{})
	if err != nil {
		t.Errorf("无参数应返回 nil（显示帮助），实际: %v", err)
	}
}

func TestRunPlugin_UnknownSubcommand(t *testing.T) {
	err := runPlugin([]string{"unknown"})
	if err == nil {
		t.Fatal("期望返回错误（未知子命令），实际返回 nil")
	}
}

func TestRunPlugin_DispatchesSearchAndUpdate(t *testing.T) {
	// 仅验证分发逻辑；具体子命令的语义测试见 TestPluginSearch_* / TestPluginUpdate_*
	if err := runPlugin([]string{"search"}); err != nil {
		t.Errorf("search 子命令分发不应返回错误（无注册表时返回 nil），实际: %v", err)
	}
	// update 没有插件时应返回 nil
	dir := setupTestProject(t)
	_ = dir
	if err := runPlugin([]string{"update"}); err != nil {
		t.Errorf("update 子命令（无插件）分发不应返回错误，实际: %v", err)
	}
}

// ===== pluginRegistry / matchPluginEntry 单元测试 =====

func TestLoadPluginRegistry_NotFound(t *testing.T) {
	dir := setupTestProject(t)
	_ = dir
	reg, err := loadPluginRegistry()
	if err != nil {
		t.Fatalf("无注册表应返回 nil error，实际: %v", err)
	}
	if reg != nil {
		t.Errorf("无注册表应返回 nil reg，实际: %+v", reg)
	}
}

func TestLoadPluginRegistry_ValidFile(t *testing.T) {
	dir := setupTestProject(t)

	// 写入临时 registry.json
	regDir := filepath.Join(dir, "ecosystem", "plugins")
	_ = os.MkdirAll(regDir, 0o755)
	content := `{
  "version": "1.0",
  "plugins": [
    {"name": "alpha", "version": "1.0.0", "description": "alpha plugin", "category": "data", "import_path": "example.com/alpha", "tools": ["t1"], "tags": ["alpha","data"]},
    {"name": "beta",  "version": "0.5.0", "description": "beta plugin",  "category": "vcs",  "import_path": "example.com/beta",  "tools": ["t2"], "tags": ["beta","vcs"]}
  ]
}`
	if err := os.WriteFile(filepath.Join(regDir, "registry.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("写注册表失败: %v", err)
	}

	reg, err := loadPluginRegistry()
	if err != nil {
		t.Fatalf("loadPluginRegistry 失败: %v", err)
	}
	if reg == nil {
		t.Fatal("loadPluginRegistry 应返回非 nil")
	}
	if len(reg.Plugins) != 2 {
		t.Fatalf("期望 2 个插件，实际 %d", len(reg.Plugins))
	}
	if reg.Plugins[0].Name != "alpha" {
		t.Errorf("第一个插件名 = %q, 期望 alpha", reg.Plugins[0].Name)
	}
}

func TestLoadPluginRegistry_InvalidJSON(t *testing.T) {
	dir := setupTestProject(t)

	regDir := filepath.Join(dir, "ecosystem", "plugins")
	_ = os.MkdirAll(regDir, 0o755)
	if err := os.WriteFile(filepath.Join(regDir, "registry.json"), []byte("{invalid json"), 0o644); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}

	_, err := loadPluginRegistry()
	if err == nil {
		t.Fatal("非法 JSON 应返回错误，实际 nil")
	}
}

func TestMatchPluginEntry(t *testing.T) {
	e := pluginRegistryEntry{
		Name:        "http-client",
		Description: "REST API 调用工具",
		Category:    "network",
		Tags:        []string{"http", "rest", "api"},
	}

	cases := []struct {
		keyword string
		want    bool
	}{
		{"", true},
		{"http", true},    // 匹配 name
		{"HTTP", true},    // 大小写不敏感
		{"rest", true},    // 匹配 tag
		{"API", true},     // 匹配 tag
		{"工具", true},      // 中文 substring 也匹配（strings.Contains 跨语言）
		{"调用", true},      // 同上
		{"CLIENT", true},  // 匹配 name（大写）
		{"NETWORK", true}, // 匹配 category
		{"grpc", false},   // 不命中
		{"xyz", false},    // 完全不命中
	}
	for _, c := range cases {
		got := matchPluginEntry(e, c.keyword)
		if got != c.want {
			t.Errorf("matchPluginEntry(keyword=%q) = %v, 期望 %v", c.keyword, got, c.want)
		}
	}
}

// ===== pluginSearch 集成测试 =====

// writeTestRegistry 在 dir 下写入测试用的 ecosystem/plugins/registry.json。
func writeTestRegistry(t *testing.T, dir string, content string) {
	t.Helper()
	regDir := filepath.Join(dir, "ecosystem", "plugins")
	if err := os.MkdirAll(regDir, 0o755); err != nil {
		t.Fatalf("创建注册表目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(regDir, "registry.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("写注册表失败: %v", err)
	}
}

func TestPluginSearch_NoRegistry(t *testing.T) {
	setupTestProject(t)
	if err := pluginSearch([]string{}); err != nil {
		t.Errorf("无注册表应返回 nil，实际: %v", err)
	}
}

func TestPluginSearch_ListAll(t *testing.T) {
	dir := setupTestProject(t)
	writeTestRegistry(t, dir, `{
  "version": "1.0",
  "plugins": [
    {"name": "alpha", "version": "1.0.0", "description": "alpha plugin", "category": "data", "import_path": "example.com/alpha", "tools": ["t1"], "tags": ["alpha"]},
    {"name": "beta",  "version": "0.5.0", "description": "beta plugin",  "category": "vcs",  "import_path": "example.com/beta",  "tools": ["t2"], "tags": ["beta"]}
  ]
}`)

	if err := pluginSearch([]string{}); err != nil {
		t.Fatalf("pluginSearch 失败: %v", err)
	}
}

func TestPluginSearch_ByKeyword(t *testing.T) {
	dir := setupTestProject(t)
	writeTestRegistry(t, dir, `{
  "version": "1.0",
  "plugins": [
    {"name": "alpha", "version": "1.0.0", "description": "alpha plugin", "category": "data", "import_path": "example.com/alpha", "tools": ["t1"], "tags": ["alpha","data"]},
    {"name": "beta",  "version": "0.5.0", "description": "beta plugin",  "category": "vcs",  "import_path": "example.com/beta",  "tools": ["t2"], "tags": ["beta","vcs"]}
  ]
}`)

	if err := pluginSearch([]string{"alpha"}); err != nil {
		t.Fatalf("pluginSearch(alpha) 失败: %v", err)
	}
	if err := pluginSearch([]string{"DATA"}); err != nil {
		t.Fatalf("pluginSearch(DATA) 失败: %v", err)
	}
}

func TestPluginSearch_NoMatch(t *testing.T) {
	dir := setupTestProject(t)
	writeTestRegistry(t, dir, `{
  "version": "1.0",
  "plugins": [
    {"name": "alpha", "version": "1.0.0", "description": "alpha plugin", "category": "data", "import_path": "example.com/alpha", "tools": ["t1"], "tags": ["alpha"]}
  ]
}`)

	if err := pluginSearch([]string{"zzz-not-exist"}); err != nil {
		t.Errorf("pluginSearch 无匹配应返回 nil，实际: %v", err)
	}
}

func TestPluginSearch_ByCategory(t *testing.T) {
	dir := setupTestProject(t)
	writeTestRegistry(t, dir, `{
  "version": "1.0",
  "plugins": [
    {"name": "alpha", "version": "1.0.0", "description": "alpha plugin", "category": "data", "import_path": "example.com/alpha", "tools": ["t1"], "tags": ["alpha"]},
    {"name": "beta",  "version": "0.5.0", "description": "beta plugin",  "category": "vcs",  "import_path": "example.com/beta",  "tools": ["t2"], "tags": ["beta"]}
  ]
}`)

	if err := pluginSearch([]string{"--category=vcs"}); err != nil {
		t.Errorf("pluginSearch --category=vcs 失败: %v", err)
	}
	if err := pluginSearch([]string{"-c=data"}); err != nil {
		t.Errorf("pluginSearch -c=data 失败: %v", err)
	}
}

func TestPluginSearch_InstalledOnly(t *testing.T) {
	dir := setupTestProject(t)
	writeTestRegistry(t, dir, `{
  "version": "1.0",
  "plugins": [
    {"name": "alpha", "version": "1.0.0", "description": "alpha plugin", "category": "data", "import_path": "example.com/alpha", "tools": ["t1"], "tags": ["alpha"]},
    {"name": "beta",  "version": "0.5.0", "description": "beta plugin",  "category": "vcs",  "import_path": "example.com/beta",  "tools": ["t2"], "tags": ["beta"]}
  ]
}`)

	// 标记 alpha 为已安装
	_ = os.WriteFile(filepath.Join(dir, ".ap.yaml"), []byte(`name: t
plugins:
  - example.com/alpha
`), 0o644)

	if err := pluginSearch([]string{"--installed"}); err != nil {
		t.Errorf("pluginSearch --installed 失败: %v", err)
	}
}

// ===== pluginUpdate 测试 =====

func TestPluginUpdate_NoPlugins(t *testing.T) {
	setupTestProject(t)
	if err := pluginUpdate([]string{}); err != nil {
		t.Errorf("无插件时 pluginUpdate 应返回 nil，实际: %v", err)
	}
}

func TestPluginUpdate_NotInList(t *testing.T) {
	dir := setupTestProject(t)
	_ = os.WriteFile(filepath.Join(dir, ".ap.yaml"), []byte(`name: t
plugins:
  - example.com/alpha
`), 0o644)

	err := pluginUpdate([]string{"unknown-plugin"})
	if err == nil {
		t.Fatal("未在 .ap.yaml 中的插件应返回错误")
	}
}

func TestIsInstalled(t *testing.T) {
	installed := map[string]bool{
		"example.com/alpha": true,
		"github.com/x/beta": true,
	}
	cases := []struct {
		e    pluginRegistryEntry
		want bool
	}{
		{pluginRegistryEntry{Name: "gamma", ImportPath: "example.com/gamma"}, false},
		{pluginRegistryEntry{Name: "alpha", ImportPath: "example.com/alpha"}, true},
		{pluginRegistryEntry{Name: "beta", ImportPath: "github.com/y/beta"}, true}, // 短名匹配
		{pluginRegistryEntry{Name: "beta", ImportPath: "beta"}, true},              // 完全相等
	}
	for _, c := range cases {
		got := isInstalled(c.e, installed)
		if got != c.want {
			t.Errorf("isInstalled(%s) = %v, 期望 %v", c.e.ImportPath, got, c.want)
		}
	}
}
