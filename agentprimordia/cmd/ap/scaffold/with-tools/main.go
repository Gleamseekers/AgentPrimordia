package main

import (
	"context"
	"fmt"
	"log"
	"os"

	ap "github.com/Gleamseekers/AgentPrimordia/agentprimordia/pkg"
)

func main() {
	// 从环境变量读取 LLM 配置
	cfg := ap.ConfigFromEnv("")
	if cfg.APIKey == "" {
		log.Fatal("set AP_LLM_API_KEY env var, e.g.: set AP_LLM_API_KEY=sk-xxx")
	}

	provider, err := ap.NewOpenAIProvider(cfg)
	if err != nil {
		log.Fatalf("create provider failed: %v", err)
	}

	// 配置工具集
	registry, err := ap.DefaultToolkit(ap.ToolkitConfig{
		RootDir:     ".",
		EnableFS:    true,
		EnableShell: true,
		EnableWeb:   true,
	})
	if err != nil {
		log.Fatalf("create toolkit failed: %v", err)
	}

	// 配置记忆存储
	memory, err := ap.WithInMemory()
	if err != nil {
		log.Fatalf("create memory store failed: %v", err)
	}
	defer memory.Close()

	agent, err := ap.NewAgent("{{.ProjectName}}", "you are an assistant that can read/write files, execute commands, and browse the web.",
		provider,
		ap.WithMaxTurns(20),
		ap.WithToolkit(registry),
		ap.WithMemory(memory),
	)
	if err != nil {
		log.Fatalf("create agent failed: %v", err)
	}

	prompt := "list files in the current directory"
	if envPrompt := os.Getenv("AP_PROMPT"); envPrompt != "" {
		prompt = envPrompt
	}

	resp, err := agent.Run(context.Background(), ap.UserMessage(prompt))
	if err != nil {
		log.Fatalf("agent run failed: %v", err)
	}

	fmt.Printf("Reply: %s\n", resp.Content)
	fmt.Printf("Tool calls: %d\n", resp.Metrics.TotalTools)
}
