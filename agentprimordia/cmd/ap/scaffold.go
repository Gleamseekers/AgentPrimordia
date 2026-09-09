package main

import (
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
// 为什么不是 v6.0.0：框架模块路径为无 /vN 后缀的 `agentprimordia`，按 Go 语义化导入
// 版本（SIV）规则，require 行不允许出现 v2+ 版本（tidy 直接报 invalid version）。
// 在框架采用 agentprimordia/vN 路径或回落 v1.x 标签之前，replace 场景一律使用
// v0.0.0 占位（replace 后版本号不参与解析）；standalone 场景由调用方提示补 replace。
// 详见 agentprimordia/docs/版本规范.md「模块消费与语义化导入版本限制」。
const apRequirePlaceholder = "v0.0.0"

// buildGoMod 生成脚手架项目的 go.mod 内容。
//
// 背景（v6.0 复测发现的断链问题）：框架根模块的
// `replace agentprimordia/pgvector => ../pgvector` 不具传递性——生成的独立子项目
// 经 pkg → internal/memory → agentprimordia/pgvector 引用链解析该模块时，
// 必须在自己的 go.mod 里自行 require+replace，否则 go mod tidy 直接失败
// （仓库内 workspace 模式会掩盖此问题，独立构建必现）。
//
// 策略：
//   - 从 projectDir 向上探测框架模块（go.mod 声明 module agentprimordia）：
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

	frameworkDir := os.Getenv("AP_ROOT")
	if frameworkDir == "" {
		frameworkDir = findFrameworkRoot(filepath.Dir(projectDir))
	}
	if frameworkDir == "" {
		// standalone：无本地框架，依赖代理发布版
		return fmt.Sprintf(`module %s

go 1.26

require agentprimordia %s
`, projectName, apRequirePlaceholder), true
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
	if data, err := os.ReadFile(pgvGoMod); err == nil && strings.Contains(string(data), "module agentprimordia/pgvector") {
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
	agentprimordia %s
	agentprimordia/pgvector v0.0.0
)

replace agentprimordia => %s
replace agentprimordia/pgvector => %s
`, apRequirePlaceholder, frameRel, pgvRel))
	} else {
		sb.WriteString(fmt.Sprintf("require agentprimordia %s\n\nreplace agentprimordia => %s\n", apRequirePlaceholder, frameRel))
	}
	return sb.String(), false
}

// findFrameworkRoot 从 start 向上探测框架模块根（go.mod 声明 module agentprimordia），
// 最多回溯 6 层；未找到返回空串。
func findFrameworkRoot(start string) string {
	dir := start
	for i := 0; i < 6; i++ {
		// 检查当前目录
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if line == "module agentprimordia" {
					return dir
				}
				if strings.HasPrefix(line, "module ") {
					break // 是模块但不是框架，继续向上
				}
			}
		}
		// 检查是否有 agentprimordia 子目录（workspace 场景）
		apSubdir := filepath.Join(dir, "agentprimordia")
		if data, err := os.ReadFile(filepath.Join(apSubdir, "go.mod")); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if line == "module agentprimordia" {
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

// cwd 返回当前工作目录（出错时退回 "."）。
func cwd() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	return dir
}
