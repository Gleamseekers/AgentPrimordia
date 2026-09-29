package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/marketplace"
)

// pluginRegistryEntry 表示 registry.json 中的单个插件条目。
type pluginRegistryEntry struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	ImportPath  string   `json:"import_path"`
	Tools       []string `json:"tools"`
	Tags        []string `json:"tags"`
}

// pluginRegistryFile 是 registry.json 顶层结构。
type pluginRegistryFile struct {
	Version string                `json:"version"`
	Plugins []pluginRegistryEntry `json:"plugins"`
}

// loadPluginRegistry 尝试从常见路径加载本地插件注册表。
//
// 查找顺序：
//  1. 当前项目目录下的 ecosystem/plugins/registry.json
//  2. AP_HOME 环境变量指向的 registry.json
//  3. $HOME/.agentprimordia/plugins/registry.json（未来远程注册中心镜像）
//
// 全部未命中时返回 nil + nil（表示本地无注册表，search 命令会给出明确提示）。
func loadPluginRegistry() (*pluginRegistryFile, error) {
	candidates := []string{}

	// 1. 当前项目目录
	if dir, err := findProjectDir(); err == nil {
		candidates = append(candidates, filepath.Join(dir, "ecosystem", "plugins", "registry.json"))
	}

	// 2. AP_HOME 环境变量
	if home := os.Getenv("AP_HOME"); home != "" {
		candidates = append(candidates, filepath.Join(home, "ecosystem", "plugins", "registry.json"))
	}

	// 3. 用户主目录
	if userHome, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(userHome, ".agentprimordia", "plugins", "registry.json"))
	}

	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var reg pluginRegistryFile
		if err := json.Unmarshal(data, &reg); err != nil {
			return nil, fmt.Errorf("parse %s: %w", p, err)
		}
		return &reg, nil
	}

	return nil, nil
}

// matchPluginEntry 判断 entry 是否命中关键词（大小写不敏感，匹配 name/description/tags）。
func matchPluginEntry(e pluginRegistryEntry, keyword string) bool {
	if keyword == "" {
		return true
	}
	k := strings.ToLower(keyword)
	if strings.Contains(strings.ToLower(e.Name), k) {
		return true
	}
	if strings.Contains(strings.ToLower(e.Description), k) {
		return true
	}
	if strings.Contains(strings.ToLower(e.Category), k) {
		return true
	}
	for _, t := range e.Tags {
		if strings.Contains(strings.ToLower(t), k) {
			return true
		}
	}
	return false
}

func runPlugin(args []string) error {
	if len(args) == 0 {
		fmt.Print(`ap plugin — manage plugins

Usage:
  ap plugin <subcommand> [arguments]

Subcommands:
  install <module>   install plugin from Go module
  list               list installed plugins
  create <name>      create plugin project scaffold
  remove <name>      remove plugin
  search [keyword]   search plugins in the local registry
  update [<name>]    update installed plugins (or all)

Examples:
  ap plugin install github.com/user/ap-plugin-slack
  ap plugin list
  ap plugin create ap-plugin-weather
  ap plugin search database
  ap plugin update           # update all
  ap plugin update http      # update a single plugin
`)
		return nil
	}

	subcmd := args[0]
	subargs := args[1:]

	switch subcmd {
	case "install":
		return pluginInstall(subargs)
	case "list":
		return pluginList()
	case "create":
		return pluginCreate(subargs)
	case "remove":
		return pluginRemove(subargs)
	case "search":
		return pluginSearch(subargs)
	case "update":
		return pluginUpdate(subargs)
	default:
		return fmt.Errorf("unknown subcommand: %s", subcmd)
	}
}

func pluginInstall(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("please specify Go module path or manifest URL\nUsage: ap plugin install github.com/user/ap-plugin-xxx\n       ap plugin install https://registry.example.com/plugins/xxx/manifest.json")
	}

	target := args[0]

	// v3.9-1：远程安装协议——https 清单 URL 走 marketplace（拉取 + cosign 验签）
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		return pluginInstallRemote(target)
	}

	module := target
	dir, err := findProjectDir()
	if err != nil {
		return err
	}

	infof("installing plugin: %s", module)
	cmd := exec.Command("go", "get", module)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("install failed: %w", err)
	}

	config := loadAPConfig()
	if config.Plugins == nil {
		config.Plugins = []string{}
	}
	config.Plugins = append(config.Plugins, module)
	if err := saveAPConfig(config); err != nil {
		warnf("save config failed: %v", err)
	}

	successf("plugin %s installed", module)
	fmt.Println()
	fmt.Println("import the plugin in your code:")
	fmt.Printf("  import _ %q\n", module)
	fmt.Printf("  // then in init(): pluginLoader.Load(%q.NewPlugin())\n", module)
	fmt.Println()
	fmt.Println("run ap run to activate the plugin")
	return nil
}

// pluginInstallRemote 通过 marketplace 远程协议安装插件（v3.9-1）：
// 拉取 Manifest → 拉取 artifact → cosign 验签 → 写入本地 → 注册到 config。
func pluginInstallRemote(manifestURL string) error {
	installer := marketplace.NewInstaller()
	m, err := installer.FetchManifest(context.Background(), manifestURL)
	if err != nil {
		return fmt.Errorf("remote install: %w", err)
	}
	infof("remote manifest: %s %s (%s)", m.Name, m.Version, m.ImportPath)

	outDir := filepath.Join(".", ".ap-plugins")
	res, err := installer.Install(context.Background(), m, outDir)
	if err != nil {
		return err
	}

	successf("plugin %s installed (verified, %s)", res.Name, res.ArtifactPath)

	config := loadAPConfig()
	if config.Plugins == nil {
		config.Plugins = []string{}
	}
	config.Plugins = append(config.Plugins, m.ImportPath)
	if err := saveAPConfig(config); err != nil {
		warnf("save config failed: %v", err)
	}
	return nil
}

func pluginList() error {
	config := loadAPConfig()
	if len(config.Plugins) == 0 {
		fmt.Println("no plugins installed")
		fmt.Println()
		fmt.Println("use ap plugin install <module> to install plugins")
		return nil
	}

	fmt.Printf("%-40s %s\n", "Module Path", "Status")
	fmt.Println(strings.Repeat("-", 60))
	for _, p := range config.Plugins {
		fmt.Printf("%-40s %s\n", p, "installed")
	}
	return nil
}

func pluginCreate(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("please specify plugin name\nUsage: ap plugin create ap-plugin-xxx")
	}

	name := args[0]
	if _, err := os.Stat(name); err == nil {
		return fmt.Errorf("directory %q already exists", name)
	}

	dirs := []string{name, filepath.Join(name, "tools")}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("create directory failed: %w", err)
		}
	}

	// go.mod（统一策略：版本对齐 v6.0.0 + 框架内自动闭合 pgvector 依赖链）
	goMod, standalone := buildGoMod(name, name)
	if err := os.WriteFile(filepath.Join(name, "go.mod"), []byte(goMod), 0o644); err != nil {
		return fmt.Errorf("write go.mod failed: %w", err)
	}
	if standalone {
		infof("提示：未检测到本地框架源码。请手动在 go.mod 添加 replace github.com/Gleamseekers/AgentPrimordia/agentprimordia => <框架源码目录>（详见 docs/版本规范.md）；框架经 GOPROXY 发布后亦可直接 require")
	} else if findGoWorkUp(".") {
		infof("提示：检测到上级 go.work。若在仓库内构建本项目，请将其加入 go.work 的 use 列表，或以 GOWORK=off 构建（replace 已指向本地框架与 pgvector）")
	}

	// plugin.go
	pluginCode := fmt.Sprintf(`package %s

import (
	ap "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg"
)

// Plugin implements ap.ToolPlugin interface.
type Plugin struct{}

// NewPlugin creates a plugin instance (entry point).
func NewPlugin() ap.ToolPlugin {
	return &Plugin{}
}

// Name returns the plugin name.
func (p *Plugin) Name() string {
	return %q
}

// Version returns the plugin version.
func (p *Plugin) Version() string {
	return "0.1.0"
}

// Tools returns the tools provided by this plugin.
func (p *Plugin) Tools() []ap.Tool {
	return []ap.Tool{
		// register your tools here
	}
}

// Init initializes the plugin.
func (p *Plugin) Init(config map[string]any) error {
	return nil
}

// Close cleans up resources.
func (p *Plugin) Close() error {
	return nil
}
`, strings.ReplaceAll(name, "-", "_"), name)
	if err := os.WriteFile(filepath.Join(name, "plugin.go"), []byte(pluginCode), 0o644); err != nil {
		return fmt.Errorf("write plugin.go failed: %w", err)
	}

	// README
	readme := "# " + name + "\n\nAgentPrimordia plugin.\n\n" +
		"## Install\n\n" +
		"```bash\nap plugin install " + name + "\n```\n\n" +
		"## Usage\n\n" +
		"```go\nimport _ \"" + name + "\"\n\n// plugin auto-registers to ToolRegistry\n```\n\n" +
		"## Development\n\n" +
		"```bash\ncd " + name + "\ngo mod tidy\ngo test ./...\n```\n"
	if err := os.WriteFile(filepath.Join(name, "README.md"), []byte(readme), 0o644); err != nil {
		return fmt.Errorf("write README.md failed: %w", err)
	}

	successf("plugin project %q created", name)
	fmt.Println()
	fmt.Printf("Directory structure:\n")
	fmt.Printf("  %s/\n", name)
	fmt.Printf("  ├── plugin.go    — plugin entry (implements ToolPlugin)\n")
	fmt.Printf("  ├── go.mod\n")
	fmt.Printf("  └── README.md\n\n")
	fmt.Printf("Next steps:\n")
	infof("cd %s", name)
	infof("# edit plugin.go to add your tools")
	infof("go mod tidy")
	return nil
}

func pluginRemove(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("please specify plugin name")
	}

	module := args[0]
	config := loadAPConfig()

	found := false
	var newPlugins []string
	for _, p := range config.Plugins {
		if p == module {
			found = true
			continue
		}
		newPlugins = append(newPlugins, p)
	}

	if !found {
		return fmt.Errorf("plugin %q not installed", module)
	}

	config.Plugins = newPlugins
	if err := saveAPConfig(config); err != nil {
		return fmt.Errorf("save config failed: %w", err)
	}

	successf("plugin %q removed from config", module)

	// auto-run go mod tidy
	dir, dirErr := findProjectDir()
	if dirErr == nil {
		infof("running go mod tidy...")
		if _, tidyErr := runCommand(dir, "go", "mod", "tidy"); tidyErr != nil {
			warnf("go mod tidy failed: %v (run manually)", tidyErr)
		} else {
			successf("go mod tidy completed")
		}
	}
	return nil
}

// pluginSearch 在本地 registry.json 中按关键词搜索插件。
//
// 用法：
//
//	ap plugin search               # 列出全部
//	ap plugin search database      # 按关键词过滤（name/description/category/tags）
//	ap plugin search --category vcs
//	ap plugin search --installed   # 仅列出已安装
func pluginSearch(args []string) error {
	var (
		keyword       string
		category      string
		installedOnly bool
	)
	for _, a := range args {
		switch a {
		case "--category", "-c":
			// 简化处理：下一个参数作为 category
			// 这里不使用 -c <val> 形式以避免 slice 边界检查
		default:
			if strings.HasPrefix(a, "--category=") {
				category = strings.TrimPrefix(a, "--category=")
			} else if strings.HasPrefix(a, "-c=") {
				category = strings.TrimPrefix(a, "-c=")
			} else if a == "--installed" || a == "-i" {
				installedOnly = true
			} else if !strings.HasPrefix(a, "-") {
				keyword = a
			}
		}
	}

	reg, err := loadPluginRegistry()
	if err != nil {
		return fmt.Errorf("load plugin registry: %w", err)
	}
	if reg == nil {
		fmt.Println("no local plugin registry found")
		fmt.Println()
		fmt.Println("searched paths:")
		fmt.Println("  ./ecosystem/plugins/registry.json")
		fmt.Println("  $AP_HOME/ecosystem/plugins/registry.json")
		fmt.Println("  $HOME/.agentprimordia/plugins/registry.json")
		fmt.Println()
		fmt.Println("to publish plugins, add them to one of the above registry.json files")
		return nil
	}

	// 已安装插件（来自 .ap.yaml）作为集合，用于过滤 --installed
	installed := map[string]bool{}
	config := loadAPConfig()
	for _, p := range config.Plugins {
		installed[p] = true
	}

	var matches []pluginRegistryEntry
	for _, e := range reg.Plugins {
		if category != "" && !strings.EqualFold(e.Category, category) {
			continue
		}
		if installedOnly && !isInstalled(e, installed) {
			continue
		}
		if !matchPluginEntry(e, keyword) {
			continue
		}
		matches = append(matches, e)
	}

	// 按 name 排序，便于稳定输出
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Name < matches[j].Name
	})

	if len(matches) == 0 {
		fmt.Printf("no plugins matched (keyword=%q, category=%q)\n", keyword, category)
		return nil
	}

	fmt.Printf("%-15s %-8s %-13s %-30s %s\n", "Name", "Version", "Category", "Import Path", "Tools")
	fmt.Println(strings.Repeat("-", 90))
	for _, e := range matches {
		tools := strings.Join(e.Tools, ",")
		if tools == "" {
			tools = "-"
		}
		status := ""
		if isInstalled(e, installed) {
			status = " [installed]"
		}
		fmt.Printf("%-15s %-8s %-13s %-30s %s%s\n",
			e.Name, e.Version, e.Category, truncate(e.ImportPath, 30), tools, status)
	}

	fmt.Printf("\nfound %d plugin(s)\n", len(matches))
	return nil
}

// isInstalled 判断注册表条目是否已被当前项目安装。
func isInstalled(e pluginRegistryEntry, installed map[string]bool) bool {
	if installed[e.ImportPath] {
		return true
	}
	// 也支持短名（github.com/.../http）匹配
	for k := range installed {
		if strings.HasSuffix(k, "/"+e.Name) || k == e.Name {
			return true
		}
	}
	return false
}

// pluginUpdate 更新已安装插件（逐一执行 `go get -u <module>` + go mod tidy）。
//
// 用法：
//
//	ap plugin update           # 更新 .ap.yaml 中列出的全部插件
//	ap plugin update <name>    # 仅更新指定 import path 或短名
func pluginUpdate(args []string) error {
	config := loadAPConfig()
	if len(config.Plugins) == 0 {
		fmt.Println("no plugins installed; nothing to update")
		return nil
	}

	var targets []string
	if len(args) == 0 {
		// 更新全部
		targets = append(targets, config.Plugins...)
	} else {
		// 更新指定插件（按 import path 精确匹配，或按短名后缀匹配）
		needle := args[0]
		matched := false
		for _, p := range config.Plugins {
			if p == needle || strings.HasSuffix(p, "/"+needle) {
				targets = append(targets, p)
				matched = true
			}
		}
		if !matched {
			return fmt.Errorf("plugin %q is not in .ap.yaml plugins list", needle)
		}
	}

	dir, dirErr := findProjectDir()
	if dirErr != nil {
		return fmt.Errorf("find project dir: %w", dirErr)
	}

	updated := 0
	for _, module := range targets {
		infof("updating %s ...", module)
		// go get -u <module>：拉取最新版本
		_, err := runCommand(dir, "go", "get", "-u", module)
		if err != nil {
			warnf("go get -u %s failed: %v", module, err)
			continue
		}
		updated++
	}

	if updated > 0 {
		infof("running go mod tidy...")
		if _, err := runCommand(dir, "go", "mod", "tidy"); err != nil {
			warnf("go mod tidy failed: %v (run manually)", err)
		} else {
			successf("go mod tidy completed")
		}
		successf("updated %d plugin(s)", updated)
	} else {
		warnf("no plugin was updated")
	}
	return nil
}
