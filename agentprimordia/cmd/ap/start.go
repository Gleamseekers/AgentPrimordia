package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// findGoWorkspace 向上查找 go.work 文件，返回包含 go.work 的目录，未找到返回空
func findGoWorkspace(start string) string {
	dir := start
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func runStart(args []string) error {
	var (
		name     string
		template string
	)
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--template", "-t":
			i++
			if i >= len(args) {
				return fmt.Errorf("--template 需要指定模板名称")
			}
			template = args[i]
		case "--help", "-h":
			fmt.Print(`ap start — create and run an agent in one step

用法:
  ap start <项目名> [--template NAME]

这是 ap init + go mod tidy + ap run 的快捷方式。
如果项目已存在，跳过创建直接启动。

选项:
  --template NAME  使用指定模板 (默认: quickstart)

示例:
  ap start my-agent
  ap start my-agent --template with-tools
`)
			return nil
		default:
			if name == "" {
				name = args[i]
			}
		}
	}

	if name == "" {
		return fmt.Errorf("请指定项目名\n用法: ap start <项目名>")
	}

	targetDir := name
	projectExists := false
	if info, err := os.Stat(targetDir); err == nil && info.IsDir() {
		if _, err := os.Stat(filepath.Join(targetDir, ".ap.yaml")); err == nil {
			projectExists = true
		}
	}

	// 步骤 1: 创建项目（如不存在）
	if !projectExists {
		fmt.Printf("  创建项目 %q ...\n", name)
		initArgs := []string{name}
		if template != "" {
			initArgs = append(initArgs, "--template", template)
		} else {
			initArgs = append(initArgs, "--template", "quickstart")
		}
		if err := runInit(initArgs); err != nil {
			return fmt.Errorf("创建项目失败: %w", err)
		}
		fmt.Println()
	} else {
		infof("项目 %q 已存在，跳过创建", name)
	}

	// 步骤 2: go mod tidy（仅在框架可用且非 workspace 时）
	// 检测是否为 standalone 模式（无本地框架）
	absTarget, err := filepath.Abs(targetDir)
	if err != nil {
		return fmt.Errorf("获取绝对路径失败: %w", err)
	}
	frameworkDir := findFrameworkRoot(absTarget)
	
	// 检测是否在 go.work workspace 内
	inWorkspace := findGoWorkspace(absTarget) != ""
	
	if inWorkspace {
		// workspace 模式：使用 go work use 添加项目
		infof("检测到 Go workspace，正在添加项目...")
		workspaceDir := findGoWorkspace(absTarget)
		// 计算项目相对于 workspace 根的路径
		relPath, err := filepath.Rel(workspaceDir, absTarget)
		if err != nil {
			relPath = targetDir
		}
		workUseCmd := exec.Command("go", "work", "use", "./"+relPath)
		workUseCmd.Dir = workspaceDir
		if err := workUseCmd.Run(); err != nil {
			infof("自动添加到 go.work 失败，请手动运行: cd %s && go work use ./%s", workspaceDir, relPath)
		} else {
			successf("已添加到 go.work")
		}
		
		if frameworkDir != "" {
			// 在 workspace 内且有本地框架，需要添加 replace 指令
			frameRel, _ := filepath.Rel(absTarget, frameworkDir)
			infof("请在 %s/go.mod 添加:", targetDir)
			fmt.Printf("  replace agentprimordia => %s\n", frameRel)
			fmt.Printf("  然后运行: cd %s && go mod tidy\n", targetDir)
		} else {
			infof("请在 %s/go.mod 添加 replace 指令指向框架源码目录", targetDir)
		}
		fmt.Println()
	} else if frameworkDir == "" {
		// standalone 模式：无法 go mod tidy，提供手动指引
		infof("未检测到本地框架源码，跳过 go mod tidy")
		infof("请手动在 %s/go.mod 添加:", targetDir)
		fmt.Printf("  replace agentprimordia => <框架源码目录>\n")
		fmt.Printf("  然后运行: cd %s && go mod tidy\n", targetDir)
		fmt.Println()
	} else {
		fmt.Printf("  安装依赖 (go mod tidy) ...\n")
		tidyCmd := exec.Command("go", "mod", "tidy")
		tidyCmd.Dir = targetDir
		tidyCmd.Stdout = os.Stdout
		tidyCmd.Stderr = os.Stderr
		if err := tidyCmd.Run(); err != nil {
			errorf("go mod tidy 失败: %v", err)
			infof("尝试手动执行: cd %s && go mod tidy", name)
			return fmt.Errorf("依赖安装失败")
		}
		successf("依赖安装完成")
		fmt.Println()
	}

	// 步骤 3: 检查 API key
	apiKey := os.Getenv("AP_LLM_API_KEY")
	if apiKey == "" {
		config := loadAPConfigFromDir(targetDir)
		if config.LLM != nil {
			apiKey = config.LLM.APIKey
		}
	}
	if apiKey == "" {
		infof("未检测到 API key，将使用 Demo 模式")
		infof("提示: 设置 AP_LLM_API_KEY 或运行 ap config set api-key 启用真实 LLM")
		fmt.Println()
	}

	// 步骤 4: 启动 agent
	fmt.Printf("  启动 agent ...\n")
	fmt.Println()

	// 切换到项目目录执行 run
	origDir, _ := os.Getwd()
	if err := os.Chdir(absTarget); err != nil {
		return fmt.Errorf("切换目录失败: %w", err)
	}
	defer os.Chdir(origDir)

	return runRun([]string{})
}
