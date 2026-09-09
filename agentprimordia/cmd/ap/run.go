package main

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func runRun(args []string) error {
	var watch bool
	prompt := ""

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--watch", "-w":
			watch = true
		case "--prompt", "-p":
			i++
			if i >= len(args) {
				return fmt.Errorf("--prompt requires a value")
			}
			prompt = args[i]
		case "--help", "-h":
			fmt.Print(`ap run — build and run the current project

用法:
  ap run [--watch] [--prompt "消息"]

选项:
  --watch, -w        auto-rebuild on file changes
  --prompt, -p       send a message to the agent

环境变量:
  AP_LLM_API_KEY     LLM API key (from .ap.yaml or env)
  AP_LLM_MODEL       model name
  AP_LLM_BASE_URL    API base URL
  AP_DEBUG=1         enable debug logging

示例:
  ap run
  ap run --prompt "分析这段代码"
  ap run --watch
`)
			return nil
		}
	}

	// 调试模式：AP_DEBUG=1 开启 verbose 日志
	if os.Getenv("AP_DEBUG") == "1" {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))
		slog.Debug("调试模式已启用")
	}

	// 查找项目目录
	dir, err := findProjectDir()
	if err != nil {
		return err
	}

	binaryName := filepath.Base(dir) + "-agent"

	if watch {
		infof("watch mode: auto-rebuild on file changes (Ctrl+C to exit)")
		fmt.Println()
		if err := watchAndRun(dir, binaryName, prompt); err != nil {
			return err
		}
		return nil
	}

	// 编译（带 spinner）
	spinner := newSpinner(fmt.Sprintf("编译 %s", binaryName))
	buildCmd := exec.Command("go", "build", "-o", binaryName, ".")
	buildCmd.Dir = dir
	buildOutput, buildErr := buildCmd.CombinedOutput()
	spinner.Stop()

	if buildErr != nil {
		return fmt.Errorf("build failed: %s\n  hint: run %s for details", strings.TrimSpace(string(buildOutput)), bold("go build ."))
	}
	defer os.Remove(filepath.Join(dir, binaryName))

	// 运行
	successf("编译完成，启动 %s", binaryName)
	fmt.Println()
	runCmd := exec.Command(filepath.Join(dir, binaryName))
	runCmd.Dir = dir
	runCmd.Stdin = os.Stdin
	runCmd.Stdout = os.Stdout
	runCmd.Stderr = os.Stderr

	// 从 .ap.yaml 注入配置到环境变量
	env := os.Environ()
	env = appendConfigEnv(env, dir)
	if prompt != "" {
		env = append(env, "AP_PROMPT="+prompt)
	}
	runCmd.Env = env

	if err := runCmd.Run(); err != nil {
		return fmt.Errorf("run failed: %w", err)
	}
	return nil
}

// appendConfigEnv 读取 .ap.yaml 的 llm 配置并注入为环境变量。
// 环境变量优先级：已有环境变量 > .ap.yaml 配置。
// 优化（perf-v3）：直接使用已有目录加载配置，避免冗余的 findProjectDir() 调用
func appendConfigEnv(env []string, dir string) []string {
	config := loadAPConfigFromDir(dir)
	if config.LLM == nil {
		return env
	}

	// 检查环境变量是否已设置（不覆盖）
	hasKey := func(key string) bool {
		prefix := key + "="
		for _, e := range env {
			if strings.HasPrefix(e, prefix) {
				return true
			}
		}
		return false
	}

	llm := config.LLM
	if llm.Provider != "" && !hasKey("AP_LLM_PROVIDER") {
		env = append(env, "AP_LLM_PROVIDER="+llm.Provider)
	}
	if llm.Model != "" && !hasKey("AP_LLM_MODEL") {
		env = append(env, "AP_LLM_MODEL="+llm.Model)
	}
	if llm.APIKey != "" && !hasKey("AP_LLM_API_KEY") {
		env = append(env, "AP_LLM_API_KEY="+llm.APIKey)
	}
	if llm.BaseURL != "" && !hasKey("AP_LLM_BASE_URL") {
		env = append(env, "AP_LLM_BASE_URL="+llm.BaseURL)
	}

	return env
}

func findProjectDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	// 向上查找包含 .ap.yaml 或 go.mod 的目录
	for {
		if _, err := os.Stat(filepath.Join(dir, ".ap.yaml")); err == nil {
			return dir, nil
		}
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("project directory not found (missing .ap.yaml or go.mod)")
}

func watchAndRun(dir, binaryName, prompt string) error {
	lastHash := ""
	var runCmd *exec.Cmd

	// 监听中断信号，优雅退出
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)

	// cleanup 杀掉正在运行的子进程
	cleanup := func() {
		if runCmd != nil && runCmd.Process != nil {
			_ = runCmd.Process.Kill()
			_ = runCmd.Wait()
			runCmd = nil
		}
	}
	defer cleanup()

	for {
		currentHash, err := getFileHash(dir)
		if err != nil {
			return err
		}

		if currentHash != lastHash {
			if lastHash != "" {
				fmt.Println()
				infof("file changed, rebuilding...")

				if runCmd != nil && runCmd.Process != nil {
					_ = runCmd.Process.Kill()
					_ = runCmd.Wait()
					runCmd = nil
				}
			}
			lastHash = currentHash

			spinner := newSpinner(fmt.Sprintf("编译 %s", binaryName))
			buildCmd := exec.Command("go", "build", "-o", binaryName, ".")
			buildCmd.Dir = dir
			output, err := buildCmd.CombinedOutput()
			spinner.Stop()

			if err != nil {
				errorf("build failed: %s", strings.TrimSpace(string(output)))
				time.Sleep(500 * time.Millisecond)
				continue
			}

			successf("编译完成")
			runCmd = exec.Command(filepath.Join(".", binaryName))
			runCmd.Dir = dir

			env := os.Environ()
			env = appendConfigEnv(env, dir)
			if prompt != "" {
				env = append(env, "AP_PROMPT="+prompt)
			}
			runCmd.Env = env

			runCmd.Stdout = os.Stdout
			runCmd.Stderr = os.Stderr
			if err := runCmd.Start(); err != nil {
				errorf("start failed: %v", err)
			}
		}

		// 等待信号或定时轮询
		select {
		case <-quit:
			fmt.Println()
			infof("收到退出信号，正在停止...")
			cleanup()
			fmt.Println("已退出")
			return nil
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// getFileHash 扫描目录下所有 .go 文件并拼接 (path:modtime;)+ 作为指纹。
//
// perf-v12 (2026-07-03) 优化：
//  1. strings.Builder 预分配 64KB（典型项目 < 1000 文件时减少 builder 扩容）
//  2. 跳过 go.sum（每次 go build 自动更新，避免伪触发重建）
//  3. 跳过常见构建产物目录 bin/dist/build（缩短 walk 时间）
func getFileHash(dir string) (string, error) {
	var sb strings.Builder
	sb.Grow(64 * 1024) // 典型 1000 个 .go 文件约 30-50KB 字符串

	// 优化（perf-v3）：使用 WalkDir 替代 Walk，避免每个文件的 Lstat 调用
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			// 跳过隐藏目录、vendor、node_modules、.git、testdata 等
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules" || name == "testdata" {
				return filepath.SkipDir
			}
			// perf-v12：跳过构建产物目录（避免扫描 bin/dist/build 下的二进制）
			if name == "bin" || name == "dist" || name == "build" {
				return filepath.SkipDir
			}
			return nil
		}
		// 只关注 .go 文件
		if strings.HasSuffix(path, ".go") {
			// perf-v12：跳过 go.sum（每次 go build 自动更新，会导致伪触发）
			if strings.HasSuffix(path, ".sum") {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			sb.WriteString(path)
			sb.WriteByte(':')
			sb.WriteString(strconv.FormatInt(info.ModTime().UnixNano(), 10))
			sb.WriteByte(';')
		}
		return nil
	})
	return sb.String(), err
}
