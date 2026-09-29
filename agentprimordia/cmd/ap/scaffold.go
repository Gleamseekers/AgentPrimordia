package main

import (
	"log/slog"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed scaffold/basic scaffold/with-tools scaffold/multi-agent
//go:embed scaffold/agent-with-cache scaffold/agent-with-rag scaffold/agent-with-metrics
//go:embed scaffold/quickstart
//go:embed scaffold/plugin scaffold/plugin/.github scaffold/plugin/.github/workflows
//go:embed scaffold/provider scaffold/provider/.github scaffold/provider/.github/workflows
var scaffoldFS embed.FS

// validTemplates 支持的模板列表（供 init.go 和 scaffold.go 共享）
var validTemplates = map[string]bool{
	"quickstart":         true,
	"basic":              true,
	"with-tools":         true,
	"multi-agent":        true,
	"agent-with-cache":   true,
	"agent-with-rag":     true,
	"agent-with-metrics": true,
	"plugin":             true,
	"provider":           true,
}

// GenerateOptions 定义脚手架生成选项
type GenerateOptions struct {
	Name     string // 项目名称
	Template string // 模板名称
	Type     string // 项目类型: agent | plugin | provider
	DryRun   bool   // 预览模式（不写盘）
}

// Generate 根据模板生成项目文件，返回文件名到内容的映射
func Generate(opts GenerateOptions) (map[string][]byte, error) {
	if opts.Name == "" {
		return nil, fmt.Errorf("project name is required")
	}

	template := opts.Template
	if template == "" {
		template = "basic"
	}

	// 验证模板（使用包级 validTemplates）
	if !validTemplates[template] {
		return nil, fmt.Errorf("unknown template %q", template)
	}

	files := make(map[string][]byte)
	scaffoldDir := "scaffold/" + template

	// 遍历模板文件
	err := fs.WalkDir(scaffoldFS, scaffoldDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}

		relPath := strings.TrimPrefix(path, scaffoldDir+"/")
		if relPath == scaffoldDir {
			return nil
		}

		data, err := scaffoldFS.ReadFile(path)
		if err != nil {
			return err
		}

		// 替换模板变量
		content := strings.ReplaceAll(string(data), "{{.ProjectName}}", opts.Name)
		content = strings.ReplaceAll(content, "{{.ModuleName}}", opts.Name)

		files[relPath] = []byte(content)
		return nil
	})
	if err != nil {
		return nil, err
	}

	// 生成 .ap.yaml
	apConfigYAML := fmt.Sprintf(`# AgentPrimordia 项目配置
name: %s
template: %s

llm:
  provider: openai       # openai | anthropic | gemini | ollama | azure | deepseek | qwen
  model: gpt-4o
  # api_key: "sk-xxx"    # 建议用环境变量 AP_LLM_API_KEY

memory:
  backend: sqlite        # sqlite | memory
  path: ./data/memory.db

agent:
  max_turns: 20
  system_prompt: "you are a helpful assistant"
`, opts.Name, template)
	files[".ap.yaml"] = []byte(apConfigYAML)

	// 生成 .gitignore
	gitignore := `# AgentPrimordia
*.exe
*.exe~
*.dll
*.so
*.dylib
*agent
data/
*.db
.env
`
	files[".gitignore"] = []byte(gitignore)

	// 生成 go.mod（统一走 buildGoMod：版本对齐 + pgvector 依赖链闭合策略）
	// 注意：传入 cwd() 而非项目目录，因为此时项目尚未创建
	goMod, _ := buildGoMod(opts.Name, cwd())
	files["go.mod"] = []byte(goMod)

	return files, nil
}

// apRequirePlaceholder 生成项目 go.mod 的框架 require 占位版本。
//
// 为什么不是 v6.0.0：框架模块路径为无 /vN 后缀的 `github.com/Gleamseekers/AgentPrimordia`，按 Go 语义化导入
// 版本（SIV）规则，require 行不允许出现 v2+ 版本（tidy 直接报 invalid version）。
// 在框架采用 github.com/Gleamseekers/AgentPrimordia/agentprimordia/vN 路径或回落 v1.x 标签之前，replace 场景一律使用
// v0.0.0 占位（replace 后版本号不参与解析）；standalone 场景由调用方提示补 replace。
// 详见 github.com/Gleamseekers/AgentPrimordia/agentprimordia/docs/版本规范.md「模块消费与语义化导入版本限制」。
const apRequirePlaceholder = "v0.0.0"

// frameworkModulePath 框架主模块路径（v7.5 布局修正：go.mod 位于仓库根，
// 模块路径即仓库路径；代码包位于 agentprimordia/ 子目录，import 路径
// 形如 <frameworkModulePath>/agentprimordia/pkg）。
const frameworkModulePath = "github.com/Gleamseekers/AgentPrimordia"

// buildGoMod 生成脚手架项目的 go.mod 内容。
//
// 背景（v6.0 复测发现的断链问题）：框架根模块的
// `replace github.com/Gleamseekers/AgentPrimordia/pgvector => ../pgvector` 不具传递性——生成的独立子项目
// 经 pkg → internal/memory → github.com/Gleamseekers/AgentPrimordia/pgvector 引用链解析该模块时，
// 必须在自己的 go.mod 里自行 require+replace，否则 go mod tidy 直接失败
// （仓库内 workspace 模式会掩盖此问题，独立构建必现）。
//
// 策略：
//   - 从 projectDir 向上探测框架模块（go.mod 声明 module github.com/Gleamseekers/AgentPrimordia）：
//     找到 → 以相对路径 emit replace，并连带 pgvector 的 require+replace；
//   - 未找到（standalone）→ 不 emit replace，依赖 GOPROXY 发布版，
//     返回 standalone=true 供调用方打印提示。
func buildGoMod(projectName, projectDir string) (content string, standalone bool) {
	// 统一转绝对路径：调用方可能传入相对目录（如 init 的 targetDir=name），
	// 混用相对路径会使 filepath.Rel/向上探测产生错误层级
	if abs, err := filepath.Abs(projectDir); err == nil {
		projectDir = abs
	}
	// 解析符号链接：macOS 上 /tmp → /private/tmp、/var → /private/var，
	// 必须在 findFrameworkRoot 之前解析，否则两个路径命名空间不一致
	// 会导致 filepath.Rel 计算出错误的相对路径。
	// 项目目录可能尚未创建（init 流程），此时解析父目录再拼接。
	if real, err := filepath.EvalSymlinks(projectDir); err == nil {
		projectDir = real
	} else if real, err := filepath.EvalSymlinks(filepath.Dir(projectDir)); err == nil {
		projectDir = filepath.Join(real, filepath.Base(projectDir))
	}

	frameworkDir := resolveFrameworkDir(os.Getenv("AP_ROOT"), filepath.Dir(projectDir))
	if frameworkDir == "" {
		// standalone：无本地框架，依赖代理发布版
		return fmt.Sprintf(`module %s

go 1.26

require %s %s
`, projectName, frameworkModulePath, apRequirePlaceholder), true
	}
	if real, err := filepath.EvalSymlinks(frameworkDir); err == nil {
		frameworkDir = real
	}

	// 相对路径：项目目录 → 框架模块 / pgvector 模块
	// （真实仓库布局：pgvector 与框架模块互为兄弟目录，如 <repo>/agentprimordia 与 <repo>/pgvector）
	frameRel, err := filepath.Rel(projectDir, frameworkDir)
	if err != nil {
		frameRel = ".."
	}
	pgvGoMod := filepath.Join(filepath.Dir(frameworkDir), "pgvector", "go.mod")
	hasPgvector := false
	if data, err := os.ReadFile(pgvGoMod); err == nil && strings.Contains(string(data), "module github.com/Gleamseekers/AgentPrimordia/pgvector") {
		hasPgvector = true
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("module %s\n\ngo 1.26\n\n", projectName))
	if hasPgvector {
		pgvRel, err := filepath.Rel(projectDir, filepath.Join(filepath.Dir(frameworkDir), "pgvector"))
		if err != nil || pgvRel == "" {
			pgvRel = filepath.Join(frameRel, "..", "pgvector")
		}
		sb.WriteString(fmt.Sprintf(`require (
	%s %s
	github.com/Gleamseekers/AgentPrimordia/pgvector v0.0.0
)

replace %s => %s
replace github.com/Gleamseekers/AgentPrimordia/pgvector => %s
`, frameworkModulePath, apRequirePlaceholder, frameworkModulePath, frameRel, pgvRel))
	} else {
		sb.WriteString(fmt.Sprintf("require %s %s\n\nreplace %s => %s\n", frameworkModulePath, apRequirePlaceholder, frameworkModulePath, frameRel))
	}
	return sb.String(), false
}

// resolveFrameworkDir 解析框架源码目录：AP_ROOT 显式指定优先，但必须
// 含框架 go.mod（v7.5 布局：go.mod 位于仓库根，声明 module
// github.com/Gleamseekers/AgentPrimordia）；无效（未设置/目录不存在/
// 无 go.mod——如指向旧 agentprimordia/ 子目录的过期值）则回退向上探测。
func resolveFrameworkDir(apRoot, fallbackStart string) string {
	if apRoot != "" {
		if hasFrameworkGoMod(apRoot) {
			return apRoot
		}
		// AP_ROOT 无效：告警并回退探测（不静默信任过期值）
		slog.Warn("AP_ROOT 无效（无框架 go.mod），回退向上探测", "AP_ROOT", apRoot)
	}
	return findFrameworkRoot(fallbackStart)
}

// hasFrameworkGoMod 判断目录是否含框架主模块 go.mod。
func hasFrameworkGoMod(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "module "+frameworkModulePath {
			return true
		}
	}
	return false
}

// findFrameworkRoot 从 start 向上探测框架模块根（go.mod 声明 module github.com/Gleamseekers/AgentPrimordia），
// 最多回溯 6 层；未找到返回空串。
func findFrameworkRoot(start string) string {
	dir := start
	for i := 0; i < 6; i++ {
		// 检查当前目录
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if line == "module github.com/Gleamseekers/AgentPrimordia" {
					return dir
				}
				if strings.HasPrefix(line, "module ") {
					break // 是模块但不是框架，继续向上
				}
			}
		}
		// 检查是否有 agentprimordia 子目录（workspace 场景）
		apSubdir := filepath.Join(dir, "agentprimordia") // 主模块目录名（非模块路径）
		if data, err := os.ReadFile(filepath.Join(apSubdir, "go.mod")); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if line == "module github.com/Gleamseekers/AgentPrimordia" {
					return apSubdir
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// findGoWorkUp 从 start 向上探测 go.work（最多 6 层），命中返回 true。
func findGoWorkUp(start string) bool {
	dir := start
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
	return false
}

// hasLocalFrameworkReplace 判断项目 go.mod 是否以本地路径 replace 框架模块。
//
// 生成项目的 go.mod 已通过 replace 自洽（框架模块与 pgvector 均指向本地源码），
// 因此构建/整理依赖时应以 GOWORK=off 隔离运行：既无需依赖用户的 go.work，
// 也避免为了构建而改写（go work use）用户的 go.work。
func hasLocalFrameworkReplace(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "replace github.com/Gleamseekers/AgentPrimordia ") ||
			strings.HasPrefix(line, "replace github.com/Gleamseekers/AgentPrimordia/") {
			return true
		}
	}
	return false
}

// isolatedGoEnv 返回 go 子命令所需环境。
// 若项目以本地 replace 自洽，则关闭 workspace 模式（GOWORK=off），
// 避免"当前模块不在 go.work 中"导致构建失败，同时不改写用户的 go.work。
func isolatedGoEnv(dir string) []string {
	env := os.Environ()
	if hasLocalFrameworkReplace(dir) {
		env = setEnvVar(env, "GOWORK", "off")
	}
	return env
}

// setEnvVar 在 env 中覆盖或追加 key=value（不产生重复项）。
func setEnvVar(env []string, key, value string) []string {
	prefix := key + "="
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

// cwd 返回当前工作目录（出错时退回 "."）。
func cwd() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	return dir
}
