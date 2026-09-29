package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/cmd/example/demo"
	ap "github.com/Gleamseekers/AgentPrimordia/pkg"
)

func main() {
	fmt.Println("🌟 Google Gemini Provider 示例")
	fmt.Println("=" + string(make([]byte, 50)))
	fmt.Println()

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		fmt.Println("⚠️  未设置 GEMINI_API_KEY 环境变量")
		fmt.Println("   请运行: export GEMINI_API_KEY='your-api-key'")
		fmt.Println("   或使用 Demo 模式运行")
		fmt.Println()
		runDemoMode()
		return
	}

	runGeminiProvider(apiKey)
}

func runGeminiProvider(apiKey string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	provider, err := ap.NewGeminiMultimodalProvider(ap.Config{
		APIKey: apiKey,
		Model:  "gemini-2.0-flash",
	})
	if err != nil {
		log.Fatalf("❌ 创建 Gemini Provider 失败: %v", err)
	}

	fmt.Println("✅ Gemini Provider 创建成功")
	fmt.Println("   Model: gemini-2.0-flash")
	fmt.Println()

	geminiAgent, err := ap.NewAgent("GeminiBot", "你是一个使用 Google Gemini 的 AI 助手，请用中文回答。", provider,
		ap.WithMaxTurns(5),
	)
	if err != nil {
		log.Fatalf("❌ 创建 Agent 失败: %v", err)
	}

	fmt.Println("📝 测试1: 基础对话")
	fmt.Println("   用户输入: 请用一句话介绍你自己")

	startTime := time.Now()
	resp, err := geminiAgent.Run(ctx, ap.UserMessage("请用一句话介绍你自己"))
	if err != nil {
		log.Fatalf("❌ 运行失败: %v", err)
	}
	duration := time.Since(startTime)

	fmt.Printf("   🤖 Gemini 回复: %s\n", resp.Content)
	fmt.Printf("   ⏱️  耗时: %v\n", duration)
	fmt.Printf("   📊 Token 使用: Prompt=%d Completion=%d\n",
		resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
	fmt.Println()

	fmt.Println("📝 测试2: 数学计算")
	fmt.Println("   用户输入: 计算 123 * 456 的结果")

	resp2, _ := geminiAgent.Run(ctx, ap.UserMessage("计算 123 * 456 的结果，只输出数字"))
	fmt.Printf("   🤖 回复: %s\n", resp2.Content)
	fmt.Println()

	fmt.Println("🎉 Gemini Provider 示例完成!")
	fmt.Println()
	fmt.Println("💡 提示:")
	fmt.Println("   - Gemini 支持多模态输入（文本+图片）")
	fmt.Println("   - 可以处理长上下文（100K+ tokens）")
	fmt.Println("   - 具有强大的推理能力")
}

func runDemoMode() {
	fmt.Println("🔄 切换到 Demo 模式...")

	demoLLM := demo.NewDemoLLM(
		"你好！我是基于 Google Gemini 的 AI 助手。我可以帮助你回答问题、分析内容和进行各种任务。",
	)

	demoAgent, err := ap.NewAgent("DemoGeminiBot", "模拟 Google Gemini 的响应", demoLLM)
	if err != nil {
		log.Fatalf("❌ 创建 Agent 失败: %v", err)
	}

	resp, _ := demoAgent.Run(context.Background(), ap.UserMessage("你好"))
	fmt.Printf("   🤖 Demo 回复: %s\n", resp.Content)
	fmt.Println()
	fmt.Println("ℹ️  要使用真实的 Gemini API，请设置 GEMINI_API_KEY 环境变量")
}
