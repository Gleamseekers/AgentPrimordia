package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/cmd/example/demo"
	ap "github.com/Gleamseekers/AgentPrimordia/agentprimordia/pkg"
)

func main() {
	fmt.Println("🌸 通义千问 (Qwen) Provider 示例")
	fmt.Println("=" + string(make([]byte, 50)))
	fmt.Println()

	apiKey := os.Getenv("QWEN_API_KEY")
	if apiKey == "" {
		fmt.Println("⚠️  未设置 QWEN_API_KEY 环境变量")
		fmt.Println("   请运行: export QWEN_API_KEY='your-api-key'")
		fmt.Println("   或使用 Demo 模式运行")
		fmt.Println()
		runDemoMode()
		return
	}

	runQwenProvider(apiKey)
}

func runQwenProvider(apiKey string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	provider, err := ap.NewQwenProvider(ap.Config{
		APIKey:  apiKey,
		BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
		Model:   "qwen-plus",
	})
	if err != nil {
		log.Fatalf("❌ 创建 Qwen Provider 失败: %v", err)
	}

	fmt.Println("✅ Qwen Provider 创建成功")
	fmt.Println("   Model: qwen-plus (阿里云通义千问)")
	fmt.Println()

	qwenAgent, err := ap.NewAgent("QwenBot", "你是一个使用阿里云通义千问的 AI 助手，请用中文回答。", provider,
		ap.WithMaxTurns(5),
	)
	if err != nil {
		log.Fatalf("❌ 创建 Agent 失败: %v", err)
	}

	testCases := []struct {
		name    string
		message string
	}{
		{"基础对话", "你好！请用一句话介绍你自己"},
		{"代码生成", "用 Go 语言写一个计算斐波那契数列的函数"},
		{"知识问答", "请解释什么是量子计算，用简单的语言"},
	}

	for i, tc := range testCases {
		fmt.Printf("📝 测试%d: %s\n", i+1, tc.name)
		fmt.Printf("   用户输入: %s\n", tc.message)

		startTime := time.Now()
		resp, err := qwenAgent.Run(ctx, ap.UserMessage(tc.message))
		if err != nil {
			log.Printf("❌ 测试失败: %v", err)
			continue
		}
		duration := time.Since(startTime)

		fmt.Printf("   🤖 Qwen 回复: %s\n", resp.Content)
		fmt.Printf("   ⏱️  耗时: %v\n", duration)
		fmt.Printf("   📊 Token 使用: Prompt=%d Completion=%d Total=%d\n",
			resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
		fmt.Println()
	}

	fmt.Println("🎉 通义千问 Provider 示例完成!")
	fmt.Println()
	fmt.Println("💡 提示:")
	fmt.Println("   - 通义千问是阿里云的大语言模型")
	fmt.Println("   - 支持多种模型：qwen-turbo(快速)、qwen-plus(均衡)、qwen-max(最强)")
	fmt.Println("   - 特别适合中文场景和国内用户")
	fmt.Println()
	fmt.Println("📚 可用的模型选项:")
	fmt.Println("   - qwen-turbo: 快速响应，适合简单任务")
	fmt.Println("   - qwen-plus: 性价比之选，适合大多数场景")
	fmt.Println("   - qwen-max: 最强性能，适合复杂推理")
	fmt.Println("   - qwen-long: 长文本支持（100K+ tokens）")
}

func runDemoMode() {
	fmt.Println("🔄 切换到 Demo 模式...")

	demoLLM := demo.NewDemoLLM(
		"你好！我是基于阿里云通义千问的 AI 助手。我可以帮助你回答问题、生成代码和分析内容。作为国产大模型，我在中文理解方面表现出色。",
	)

	demoAgent, err := ap.NewAgent("DemoQwenBot", "模拟通义千问的响应", demoLLM)
	if err != nil {
		log.Fatalf("❌ 创建 Agent 失败: %v", err)
	}

	resp, _ := demoAgent.Run(context.Background(), ap.UserMessage("你好"))
	fmt.Printf("   🤖 Demo 回复: %s\n", resp.Content)
	fmt.Println()
	fmt.Println("ℹ️  要使用真实的通义千问 API，请设置 QWEN_API_KEY 环境变量")
}
