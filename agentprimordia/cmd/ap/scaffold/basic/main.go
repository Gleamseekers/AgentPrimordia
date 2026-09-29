package main

import (
	"context"
	"fmt"
	"log"

	ap "github.com/Gleamseekers/AgentPrimordia/pkg"
)

func main() {
	// 从环境变量读取 LLM 配置（AP_LLM_API_KEY, AP_LLM_MODEL 等）
	cfg := ap.ConfigFromEnv("")
	if cfg.APIKey == "" {
		log.Fatal("set AP_LLM_API_KEY env var, e.g.: set AP_LLM_API_KEY=sk-xxx")
	}

	provider, err := ap.NewOpenAIProvider(cfg)
	if err != nil {
		log.Fatalf("create provider failed: %v", err)
	}

	agent, err := ap.NewAgent("{{.ProjectName}}", "you are a helpful assistant.", provider, ap.WithMaxTurns(10))
	if err != nil {
		log.Fatalf("create agent failed: %v", err)
	}

	prompt := "Hello!"
	resp, err := agent.Run(context.Background(), ap.UserMessage(prompt))
	if err != nil {
		log.Fatalf("agent run failed: %v", err)
	}

	fmt.Printf("Reply: %s\n", resp.Content)
	fmt.Printf("Turns: %d\n", resp.Metrics.TotalTurns)
}
