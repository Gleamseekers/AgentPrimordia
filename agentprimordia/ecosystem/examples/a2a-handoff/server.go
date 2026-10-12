package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	ap "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg"
)

// runServer 启动开放协议 server 进程。
//
// 生命周期：监听（支持 :0 随机端口）→ 打印实际监听地址（run.sh 解析）→
// 服务至收到 SIGINT/SIGTERM → 优雅关闭。
func runServer() error {
	addr := envOr("AP_A2A_HANDOFF_ADDR", "127.0.0.1:9797")

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("监听 %s 失败: %w", addr, err)
	}

	exec, err := newDemoExecutor()
	if err != nil {
		return err
	}

	card := buildCard(lis.Addr().String())
	server := ap.NewOpenInteropServer(card, ap.DefaultInteropConfig()).WithExecutor(exec)

	httpServer := &http.Server{Handler: server.Handler()}

	// 结构化就绪行：run.sh / CI 据此获取实际监听地址（:0 时端口由内核分配）
	fmt.Printf("A2A_HANDOFF_SERVER_ADDR=%s\n", lis.Addr().String())
	fmt.Printf("[server] 开放协议端点已启动: %s（Agent Card: %s%s）\n",
		card.URL, card.URL, ap.DefaultInteropConfig().AgentCardPath)
	fmt.Printf("[server] Agent: %s v%s | skills=%d | LLM 模式: %s\n",
		card.Name, card.Version, len(card.Skills), llmMode())

	// 优雅关闭：SIGINT/SIGTERM 后停止接收新连接并等待在途请求
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		if err := httpServer.Serve(lis); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
		fmt.Println("[server] 收到关闭信号，正在优雅退出...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("优雅关闭失败: %w", err)
		}
		fmt.Println("[server] 已关闭")
		return nil
	}
}

// envOr 读取环境变量，为空时返回默认值。
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
