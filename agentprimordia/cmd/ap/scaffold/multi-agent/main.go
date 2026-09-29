package main

import (
	"context"
	"fmt"
	"log"

	ap "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg"
)

func main() {
	fmt.Println("=== {{.ProjectName}}: 多 Agent 协作 ===")

	// 从环境变量读取 LLM 配置
	cfg := ap.ConfigFromEnv("")
	if cfg.APIKey == "" {
		log.Fatal("set AP_LLM_API_KEY env var, e.g.: set AP_LLM_API_KEY=sk-xxx")
	}

	provider, err := ap.NewOpenAIProvider(cfg)
	if err != nil {
		log.Fatalf("create provider failed: %v", err)
	}

	// 使用 Pool 进行多 Agent 调度
	pool := ap.NewPool(ap.PoolConfig{
		MaxConcurrency: 3,
		DefaultAgent: ap.ReActAgentConfig{
			SystemPrompt: "you are a task processing assistant",
			MaxTurns:     5,
		},
	})
	defer pool.Close()

	pool.SetModel(provider)

	tasks := []ap.TaskConfig{
		{ID: "task-1", Title: "data collection", Prompt: "collect relevant data", SessionID: "session-001"},
		{ID: "task-2", Title: "analysis", Prompt: "analyze collected data", SessionID: "session-001"},
		{ID: "task-3", Title: "report generation", Prompt: "generate analysis report", SessionID: "session-001"},
	}

	results, err := pool.Dispatch(context.Background(), tasks)
	if err != nil {
		log.Fatalf("pool dispatch failed: %v", err)
	}

	for _, r := range results {
		status := "成功"
		if r.Error != nil {
			status = r.Error.Error()
		}
		fmt.Printf("任务 [%s] %s: %s (耗时 %v)\n", r.TaskID, r.Task.Title, status, r.Duration)
	}

	stats := pool.Stats()
	fmt.Printf("\nStats: completed=%d, failed=%d\n", stats.CompletedTasks, stats.FailedTasks)
}
