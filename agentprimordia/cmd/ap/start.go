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

	// 步骤 2: 依赖准备
	//
	// 设计约束（v7.3 修正）：
	//   - 生成项目的 go.mod 已通过 replace 自洽，构建阶段改用 GOWORK=off 隔离；
	//   - 绝不调用 `go work use` 改写用户的 go.work（此前会污染用户工作区且不可逆感知）；
	//   - 无本地框架时无法解析依赖（模块路径 agentprimordia 无 /vN 后缀，v2+ 标签
	//     不可经 GOPROXY require），此时明确失败，不制造"看似成功实则编译失败"的假象。
	absTarget, err := filepath.Abs(targetDir)
	if err != nil {
		return fmt.Errorf("获取绝对路径失败: %w", err)
	}
	frameworkDir := os.Getenv("AP_ROOT")
	if frameworkDir == "" {
		frameworkDir = findFrameworkRoot(absTarget)
	}

	if frameworkDir == "" {
		fmt.Println()
		errorf("未检测到本地框架源码，无法解析依赖")
		infof("原因：模块路径 agentprimordia 无 /vN 后缀，v2+ 标签不可经 GOPROXY require（详见 docs/版本规范.md）")
		infof("修复（任选其一）后重试：")
		fmt.Printf("  1) 从仓库源码获取框架：git clone <AgentPrimordia 仓库> && cd agentprimordia && go build -o ap ./cmd/ap/\n")
		fmt.Printf("  2) 在 %s/go.mod 添加：replace agentprimordia => <框架源码目录>，再运行 ap run\n", targetDir)
		return fmt.Errorf("缺少本地框架源码，已创建项目但未启动")
	}

	// 有本地框架：依赖已由生成 go.mod 的 replace 自洽。
	if workspaceDir := findGoWorkspace(absTarget); workspaceDir != "" {
		infof("检测到上级 go.work（%s）；本项目依赖已由 go.mod 的 replace 自洽", workspaceDir)
		infof("构建将以 GOWORK=off 隔离运行，不会改写你的 go.work")
		fmt.Println()
	}

	// 新建项目的 init 已执行过 go mod tidy；仅对既有项目补做。
	if projectExists {
		fmt.Printf("  安装依赖 (go mod tidy) ...\n")
		tidyCmd := exec.Command("go", "mod", "tidy")
		tidyCmd.Dir = targetDir
		tidyCmd.Env = isolatedGoEnv(targetDir)
		tidyCmd.Stdout = os.Stdout
		tidyCmd.Stderr = os.Stderr
		if err := tidyCmd.Run(); err != nil {
			errorf("go mod tidy 失败: %v", err)
			infof("尝试手动执行: cd %s && GOWORK=off go mod tidy", name)
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
	defer func() { _ = os.Chdir(origDir) }()

	return runRun([]string{})
}
