// showcase-debate 展示多 Agent 辩论能力
// 运行: AP_LLM_API_KEY=sk-xxx AP_LLM_BASE_URL=https://token.sensenova.cn/v1 AP_LLM_MODEL=sensenova-6.8-flash-lite go run showcase-debate.go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	ap "agentprimordia/pkg"
)

func main() {
	fmt.Println("╔══════════════════════════════════════════════╗")
	fmt.Println("║   AgentPrimordia 多 Agent 辩论展示          ║")
	fmt.Println("║   Multi-Agent Debate Showcase               ║")
	fmt.Println("╚══════════════════════════════════════════════╝")
	fmt.Println()

	apiKey := os.Getenv("AP_LLM_API_KEY")
	if apiKey == "" {
		fmt.Println("请设置 AP_LLM_API_KEY 环境变量")
		fmt.Println("示例: AP_LLM_API_KEY=sk-xxx AP_LLM_BASE_URL=https://token.sensenova.cn/v1 AP_LLM_MODEL=sensenova-6.8-flash-lite go run showcase-debate.go")
		os.Exit(1)
	}

	cfg := ap.ConfigFromEnv("")
	if cfg.Model == "" {
		cfg.Model = "gpt-4o-mini"
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com/v1"
	}

	provider, err := ap.NewOpenAIProvider(cfg)
	if err != nil {
		log.Fatalf("创建 LLM 失败: %v", err)
	}

	ctx := context.Background()

	// 三个不同视角的 Agent
	architect, err := ap.NewAgent("架构师",
		"你是一位注重可扩展性和模块化的高级软件架构师。回答简洁（3句以内），从架构角度分析。",
		provider, ap.WithMaxTurns(3))
	if err != nil {
		log.Fatal(err)
	}

	security, err := ap.NewAgent("安全专家",
		"你是一位注重安全性和合规性的安全专家。回答简洁（3句以内），从安全角度分析。",
		provider, ap.WithMaxTurns(3))
	if err != nil {
		log.Fatal(err)
	}

	pm, err := ap.NewAgent("产品经理",
		"你是一位注重用户体验和交付速度的产品经理。回答简洁（3句以内），从产品和用户角度分析。",
		provider, ap.WithMaxTurns(3))
	if err != nil {
		log.Fatal(err)
	}

	topic := "我们的下一个产品应该采用微服务架构还是单体架构？"

	fmt.Printf("📋 辩论主题: %s\n", topic)
	fmt.Println("👥 参与者: 架构师 | 安全专家 | 产品经理")
	fmt.Println("═══════════════════════════════════════════════")
	fmt.Println()

	agents := []ap.Agent{architect, security, pm}
	rounds := 2

	var previousArgs []string

	for round := 1; round <= rounds; round++ {
		fmt.Printf("── 第 %d 轮 ──\n\n", round)

		for _, agent := range agents {
			var prompt string
			if round == 1 {
				prompt = fmt.Sprintf("请就以下主题从你的专业角度阐述立场（3句以内）:\n\n%s", topic)
			} else {
				prompt = fmt.Sprintf("辩论主题: %s\n\n其他参与者的观点:\n%s\n\n请回应其他观点并强化或调整你的立场（3句以内）。",
					topic, strings.Join(previousArgs, "\n"))
			}

			resp, err := agent.Run(ctx, ap.UserMessage(prompt))
			if err != nil {
				log.Printf("%s 发言失败: %v", agent.Name(), err)
				continue
			}

			fmt.Printf("【%s】\n%s\n\n", agent.Name(), resp.Content)
			previousArgs = append(previousArgs, fmt.Sprintf("%s: %s", agent.Name(), resp.Content))
		}
	}

	// 总结轮
	fmt.Println("── 总结 ──")

	summaryPrompt := fmt.Sprintf("辩论主题: %s\n\n各方立场:\n%s\n\n请用 3-4 句话总结各方共识和分歧，给出推荐方向。",
		topic, strings.Join(previousArgs, "\n"))

	resp, err := architect.Run(ctx, ap.UserMessage(summaryPrompt))
	if err != nil {
		log.Fatalf("总结失败: %v", err)
	}

	fmt.Println(resp.Content)
	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════")
	fmt.Println("展示完成。此能力可用于：技术决策、方案评审、风险评估等场景。")
}
