// {{.ProjectName}} — Agent with metrics template
// 展示如何为 Agent 接入 Prometheus 指标，暴露 /metrics HTTP 端点。
//
// 前置条件：
//
//	set AP_LLM_API_KEY=sk-xxx
//
// 跑法：
//
//	cd {{.ProjectName}}
//	go mod tidy
//	go run .
//	# 另开终端：
//	curl http://localhost:9090/metrics
package main

import (
	"context"
	"fmt"
	"log"

	ap "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg"
)

func main() {
	cfg := ap.ConfigFromEnv("")
	if cfg.APIKey == "" {
		log.Fatal("set AP_LLM_API_KEY env var")
	}
	provider, err := ap.NewOpenAIProvider(cfg)
	if err != nil {
		log.Fatalf("create provider failed: %v", err)
	}

	// 创建 Prometheus 指标收集器
	metrics := ap.NewMetrics()

	// 启动 metrics HTTP server
	handler := ap.NewPrometheusHandler(metrics, ":9090")
	go func() {
		if err := handler.Start(); err != nil {
			log.Printf("metrics server: %v", err)
		}
	}()
	fmt.Println("📊 Metrics 端点: http://localhost:9090/metrics")

	// 接入 Agent
	agent, err := ap.NewAgent("{{.ProjectName}}", "you are a helpful assistant", provider,
		ap.WithMaxTurns(10),
		ap.WithMetrics(metrics),
	)
	if err != nil {
		log.Fatalf("create agent failed: %v", err)
	}

	// 跑几轮对话
	questions := []string{
		"你好",
		"今天天气如何？",
		"再见",
	}
	for _, q := range questions {
		fmt.Printf("\nQ: %s\n", q)
		resp, err := agent.Run(context.Background(), ap.UserMessage(q))
		if err != nil {
			log.Printf("call failed: %v", err)
			continue
		}
		fmt.Printf("A: %s\n", resp.Content)
	}

	fmt.Println("\nMetrics endpoint: curl http://localhost:9090/metrics")
	fmt.Println("Press Enter to exit...")
	_, _ = fmt.Scanln()
}
