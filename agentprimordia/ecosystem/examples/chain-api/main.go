package main

import (
	"context"
	"fmt"
	"log"

	ap "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/testutil"
)

func main() {
	fmt.Println("=== 链式 API：最简 Agent ===")
	fmt.Println()

	mock := testutil.NewMockProvider("你好！我是链式 API 创建的 Agent，有什么可以帮你的？")

	// 只需 3 个必填参数即可创建 Agent
	agent, err := ap.NewAgent("hello-agent", "", mock, ap.WithMaxTurns(3))
	if err != nil {
		log.Fatalf("创建 Agent 失败: %v", err)
	}

	resp, err := agent.Run(context.Background(), ap.UserMessage("你好！"))
	if err != nil {
		log.Fatalf("运行失败: %v", err)
	}

	fmt.Printf("回复: %s\n", resp.Content)
	fmt.Printf("轮数: %d\n", resp.Metrics.TotalTurns)
}
