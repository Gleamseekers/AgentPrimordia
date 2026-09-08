package main

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func runInit(args []string) error {
	var (
		name        string
		template    string
		projectType string
		dryRun      bool
		interactive bool
		noTidy      bool
	)
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--template", "-t":
			i++
			if i >= len(args) {
				return fmt.Errorf("--template 需要指定模板名称")
			}
			template = args[i]
		case "--type":
			i++
			if i >= len(args) {
				return fmt.Errorf("--type 需要指定类型")
			}
			projectType = args[i]
		case "--dry-run":
			dryRun = true
		case "--interactive", "-i":
			interactive = true
		case "--no-tidy":
			noTidy = true
		case "--help", "-h":
			fmt.Print(`ap init — create a new agent / plugin / provider project

Usage:
  ap init <项目名> [--template NAME] [--type TYPE] [--dry-run] [--interactive]
  ap init <项目名> --type plugin      # 生成插件项目
  ap init <项目名> --type provider    # 生成 LLM Provider 项目

Templates (--type=agent 时使用):
  quickstart         5分钟快速入门 (推荐新手)
  basic              minimal agent (default)
  with-tools         agent with tools (filesystem + shell + web)
  multi-agent        multi-agent collaboration
  agent-with-cache   agent + LLM response cache
  agent-with-rag     agent + knowledge retrieval (RAG)
  agent-with-metrics agent + Prometheus metrics

Types (--type 时使用):
  agent              标准 Agent 项目（默认）
  plugin             AgentPrimordia 插件项目（ap.Plugin 接口）
  provider           LLM Provider 项目（ap.Provider 接口）

Options:
  --type TYPE        项目类型: agent | plugin | provider
  --template NAME    Agent 模板名称（仅 type=agent 时有效）
  --dry-run          preview files without creating
  --interactive, -i  interactive wizard mode
  --no-tidy          skip automatic go mod tidy

Examples:
  ap init my-agent
  ap init my-agent --template with-tools
  ap init my-agent --type plugin
  ap init my-provider --type provider
  ap init --interactive
`)
			return nil
		default:
			if name == "" {
				name = args[i]
			}
		}
	}

	// 交互式向导模式
	if interactive {
		wizard := NewWizard(os.Stdin, os.Stdout)
		opts, err := wizard.Run()
		if err != nil {
			return err
		}
		name = opts.Name
		template = opts.Template
		if opts.Type != "" {
			projectType = opts.Type
		}
	}

	if name == "" {
		return fmt.Errorf("please specify project name\nUsage: ap init <name>")
	}

	// 推断 project type
	if projectType == "" {
		projectType = "agent"
	}
	switch projectType {
	case "agent":
		// template 适用
	case "plugin":
		if template == "" {
			template = "plugin"
		}
		if template != "plugin" {
			return fmt.Errorf("--type=plugin 时只能使用 --template=plugin")
		}
	case "provider":
		if template == "" {
			template = "provider"
		}
		if template != "provider" {
			return fmt.Errorf("--type=provider 时只能使用 --template=provider")
		}
	default:
		return fmt.Errorf("unknown --type %q (支持: agent | plugin | provider)", projectType)
	}

	if template == "" {
		template = "basic"
	}

	// 验证模板（validTemplates 定义在 scaffold.go）
	if !validTemplates[template] {
		return fmt.Errorf("unknown template %q, supported: quickstart, basic, with-tools, multi-agent, agent-with-cache, agent-with-rag, agent-with-metrics, plugin, provider", template)
	}

	// dry-run 模式：只预览
	if dryRun {
		fmt.Printf("%s preview mode — files to be created:\n\n", bold("DRY RUN"))
		scaffoldDir := "scaffold/" + template
		if err := fs.WalkDir(scaffoldFS, scaffoldDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			relPath := strings.TrimPrefix(path, scaffoldDir+"/")
			if relPath == scaffoldDir {
				return nil
			}
			infof("%s/%s", name, relPath)
			return nil
		}); err != nil {
			return fmt.Errorf("dry-run 预览模板失败: %w", err)
		}
		infof("%s/.ap.yaml", name)
		infof("%s/.gitignore", name)
		infof("%s/go.mod", name)
		fmt.Printf("\nTemplates: %s\n", template)
		return nil
	}

	// 检查目标目录
	targetDir := name
	if _, err := os.Stat(targetDir); err == nil {
		return fmt.Errorf("directory %q already exists", targetDir)
	}

	// 创建目录
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("create directory failed: %w", err)
	}

	// 复制模板文件
	scaffoldDir := "scaffold/" + template
	err := fs.WalkDir(scaffoldFS, scaffoldDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relPath := strings.TrimPrefix(path, scaffoldDir+"/")
		if relPath == scaffoldDir {
			return nil
		}
		targetPath := filepath.Join(targetDir, relPath)

		if d.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}

		data, err := scaffoldFS.ReadFile(path)
		if err != nil {
			return err
		}

		content := strings.ReplaceAll(string(data), "{{.ProjectName}}", name)
		content = strings.ReplaceAll(content, "{{.ModuleName}}", name)

		return os.WriteFile(targetPath, []byte(content), 0o644)
	})
	if err != nil {
		return fmt.Errorf("create project failed: %w", err)
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
`, name, template)
	if err := os.WriteFile(filepath.Join(targetDir, ".ap.yaml"), []byte(apConfigYAML), 0o644); err != nil {
		return fmt.Errorf("write .ap.yaml failed: %w", err)
	}

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
	if err := os.WriteFile(filepath.Join(targetDir, ".gitignore"), []byte(gitignore), 0o644); err != nil {
		return fmt.Errorf("write .gitignore failed: %w", err)
	}

	// 生成 go.mod（统一策略：版本对齐 v6.0.0 + 框架内自动闭合 pgvector 依赖链）
	goMod, standalone := buildGoMod(name, targetDir)
	if err := os.WriteFile(filepath.Join(targetDir, "go.mod"), []byte(goMod), 0o644); err != nil {
		return fmt.Errorf("write go.mod failed: %w", err)
	}
	if standalone {
		infof("提示：未检测到本地框架源码。受 Go 语义化导入版本限制，框架 v2+ 标签暂不可经 GOPROXY require；请手动在 go.mod 添加 replace agentprimordia => <框架源码目录>（详见 docs/版本规范.md）")
	} else if findGoWorkUp(".") {
		infof("提示：检测到上级 go.work。若在仓库内构建本项目，请将其加入 go.work 的 use 列表，或以 GOWORK=off 构建（replace 已指向本地框架与 pgvector）")
	}

	// 自动执行 go mod tidy（除非 --no-tidy 或 standalone 模式）
	tidyRan := false
	if !noTidy && !standalone {
		fmt.Printf("  运行 go mod tidy ...\n")
		tidyCmd := exec.Command("go", "mod", "tidy")
		tidyCmd.Dir = targetDir
		if output, err := tidyCmd.CombinedOutput(); err != nil {
			errorf("go mod tidy 失败: %s", strings.TrimSpace(string(output)))
			infof("请手动执行: cd %s && go mod tidy", name)
		} else {
			tidyRan = true
			successf("依赖安装完成")
		}
	}

	successf("项目 %q 已创建 (Templates: %s)", name, template)
	fmt.Println()
	fmt.Printf("Next steps:\n")
	infof("cd %s", name)
	if !tidyRan {
		infof("go mod tidy")
	}
	infof("set AP_LLM_API_KEY=sk-xxx")
	infof("ap run")
	return nil
}
