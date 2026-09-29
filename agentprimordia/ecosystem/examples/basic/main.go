package main

import (
	"context"
	"fmt"
	"log"

	ap "github.com/Gleamseekers/AgentPrimordia/pkg"
	"github.com/Gleamseekers/AgentPrimordia/testutil"
)

func main() {
	fmt.Println("=== AgentPrimordia: 基础示例 ===")
	fmt.Println()

	// 使用 PromptTemplate 构建系统提示词
	tmpl := ap.NewPromptTemplate("你是一个{{.Role}}助手，专注于{{.Domain}}领域。")
	tmpl.WithVar("Role", "Go开发").WithVar("Domain", "Agent框架")
	systemPrompt, err := tmpl.Render()
	if err != nil {
		log.Fatalf("渲染模板失败: %v", err)
	}
	fmt.Printf("系统提示词: %s\n\n", systemPrompt)

	mockLLM := testutil.NewMockProvider("你好！我是 AgentPrimordia 助手，有什么可以帮你的？")

	agent, err := ap.NewAgent("BasicAgent", systemPrompt, mockLLM, ap.WithMaxTurns(3))
	if err != nil {
		log.Fatalf("创建 Agent 失败: %v", err)
	}

	ctx := context.Background()

	// 检查点恢复：当被 `ap loop resume` 启动时，AP_RESUME=1 会被注入
	if ap.ShouldResume() {
		agentID := ap.ResumeAgentID()
		if agentID == "" || agentID == "BasicAgent" {
			fmt.Printf("[info] 从检查点恢复 Agent %q...\n", agentID)
			if _, err := agent.ResumeFromCheckpoint(ctx); err != nil {
				log.Fatalf("恢复失败: %v", err)
			}
			return
		}
	}

	// 正常启动：使用 --prompt 参数或默认问候
	prompt := ap.ResumePrompt()
	if prompt == "" {
		prompt = "你好！"
	}

	resp, err := agent.Run(ctx, ap.UserMessage(prompt))
	if err != nil {
		log.Fatalf("Agent 运行失败: %v", err)
	}

	fmt.Printf("回复: %s\n", resp.Content)
	fmt.Printf("轮数: %d\n", resp.Metrics.TotalTurns)
	fmt.Printf("版本: %s\n", ap.Version)
}
