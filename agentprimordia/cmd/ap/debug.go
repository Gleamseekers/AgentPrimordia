package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/debugger"
)

func runDebug(args []string) error {
	var port string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--port", "-p":
			i++
			if i >= len(args) {
				return fmt.Errorf("--port requires a value")
			}
			port = args[i]
		case "--help", "-h":
			fmt.Print(`ap debug — start debug server

用法:
  ap debug [--port 8080]

选项:
  --port, -p   debug server port (default: 6060)

说明:
  Start HTTP debug server to view agent events,
  memory snapshots and runtime status in the browser.

示例:
  ap debug
  ap debug --port 3000
`)
			return nil
		}
	}
	if port == "" {
		port = "6060"
	}

	// 检查项目目录
	if _, err := findProjectDir(); err != nil {
		warnf("project directory not found, debug server starts with limited features")
	}

	addr := "localhost:" + port
	server := debugger.NewDebugServer(addr)

	successf("调试服务器启动: http://%s", addr)
	infof("run %s in another terminal to start the agent", bold("ap run"))
	infof("press Ctrl+C to stop")
	fmt.Println()

	if err := server.Start(); err != nil {
		return fmt.Errorf("server start failed: %w", err)
	}

	// 等待中断信号以优雅退出
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	fmt.Println("\n正在关闭调试服务器...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Stop(ctx); err != nil {
		errorf("server shutdown error: %v", err)
	}
	fmt.Println("调试服务器已退出")
	return nil
}
