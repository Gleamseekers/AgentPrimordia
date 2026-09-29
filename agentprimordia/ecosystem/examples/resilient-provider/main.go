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
	fmt.Println("🛡️  ResilientProvider 弹性调用示例")
	fmt.Println("=" + string(make([]byte, 50)))
	fmt.Println()

	demonstrateBasicResilience()
	demonstrateFallbackChain()
	demonstrateCircuitBreaker()
	demonstrateCustomConfig()
}

func demonstrateBasicResilience() {
	fmt.Println("📌 基础弹性功能演示")
	fmt.Println("-" + string(make([]byte, 40)))

	openaiKey := os.Getenv("OPENAI_API_KEY")
	if openaiKey == "" {
		fmt.Println("⚠️  未设置 OPENAI_API_KEY，使用 Demo 模式")
		demoBasicResilience()
		return
	}

	primary, err := ap.NewOpenAIProvider(ap.Config{
		APIKey: openaiKey,
		Model:  "gpt-4o",
	})
	if err != nil {
		fmt.Printf("❌ 创建 Provider 失败: %v\n", err)
		demoBasicResilience()
		return
	}

	resilient, err := ap.NewResilientProvider(primary, ap.DefaultResilientConfig())
	if err != nil {
		fmt.Printf("❌ 创建 ResilientProvider 失败: %v\n", err)
		return
	}

	testAgent, err := ap.NewAgent("ResilientBot", "你是一个具有弹性能力的 AI 助手", resilient)
	if err != nil {
		log.Fatalf("❌ 创建 Agent 失败: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := testAgent.Run(ctx, ap.UserMessage("你好，请简单介绍一下你自己"))
	if err != nil {
		log.Printf("❌ 调用失败: %v\n", err)
		return
	}

	fmt.Printf("✅ 弹性调用成功\n")
	fmt.Printf("   🤖 回复: %s\n", resp.Content)
	fmt.Println()
}

func demoBasicResilience() {
	mockPrimary := demo.NewDemoLLM("来自主 Provider 的响应")
	resilient, err := ap.NewResilientProvider(mockPrimary, ap.DefaultResilientConfig())
	if err != nil {
		fmt.Printf("❌ 创建 ResilientProvider 失败: %v\n", err)
		return
	}

	ctx := context.Background()

	resp, _ := resilient.Complete(ctx, &ap.CompletionRequest{
		Messages: []ap.ChatMessage{{Role: "user", Content: "测试消息"}},
	})

	fmt.Printf("✅ Demo 模式响应: %s\n", resp.Content)
	fmt.Println()
}

func demonstrateFallbackChain() {
	fmt.Println("🔗 Fallback 降级链演示")
	fmt.Println("-" + string(make([]byte, 40)))

	hasAPIKeys := os.Getenv("OPENAI_API_KEY") != "" && os.Getenv("QWEN_API_KEY") != ""

	if !hasAPIKeys {
		fmt.Println("⚠️  未设置 API Keys，使用 Demo 模式")
		demoFallbackChain()
		return
	}

	primary, _ := ap.NewOpenAIProvider(ap.Config{
		APIKey: os.Getenv("OPENAI_API_KEY"),
		Model:  "gpt-4o",
	})

	fallback1, _ := ap.NewQwenProvider(ap.Config{
		APIKey:  os.Getenv("QWEN_API_KEY"),
		BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
		Model:   "qwen-plus",
	})

	fallback2 := demo.NewDemoLLM("这是最后的兜底响应")

	resilient, err := ap.NewResilientProvider(primary, ap.DefaultResilientConfig())
	if err != nil {
		fmt.Printf("❌ 创建 ResilientProvider 失败: %v\n", err)
		return
	}
	resilient.AddFallback(fallback1)
	resilient.AddFallback(fallback2)

	fmt.Println("✅ 创建降级链:")
	fmt.Println("   1. Primary: OpenAI GPT-4o (主)")
	fmt.Println("   2. Fallback 1: Qwen Plus (备)")
	fmt.Println("   3. Fallback 2: Demo LLM (兜底)")

	ctx := context.Background()
	resp, _ := resilient.Complete(ctx, &ap.CompletionRequest{
		Messages: []ap.ChatMessage{{Role: "user", Content: "测试降级链"}},
	})

	fmt.Printf("\n🤖 响应内容: %s\n", resp.Content)
	fmt.Println()
}

func demoFallbackChain() {
	primary := demo.NewDemoLLM("主 Provider 响应").WithDelay(2 * time.Second)
	fallback1 := demo.NewDemoLLM("备选 Provider 响应")
	fallback2 := demo.NewDemoLLM("兜底 Provider 响应")

	resilient, err := ap.NewResilientProvider(primary, ap.ResilientConfig{
		MaxRetries:   2,
		RetryBackoff: 100 * time.Millisecond,
	})
	if err != nil {
		fmt.Printf("❌ 创建 ResilientProvider 失败: %v\n", err)
		return
	}
	resilient.AddFallback(fallback1)
	resilient.AddFallback(fallback2)

	fmt.Println("📋 降级链配置（模拟）:")
	fmt.Println("   1. Primary: 延迟 2s（模拟超时）")
	fmt.Println("   2. Fallback 1: 即时响应")
	fmt.Println("   3. Fallback 2: 兜底")

	ctx := context.Background()
	startTime := time.Now()
	resp, _ := resilient.Complete(ctx, &ap.CompletionRequest{
		Messages: []ap.ChatMessage{{Role: "user", Content: "测试"}},
	})
	duration := time.Since(startTime)

	fmt.Printf("\n⏱️  总耗时: %v\n", duration)
	fmt.Printf("🤖 实际响应: %s\n", resp.Content)
	fmt.Println()
}

func demonstrateCircuitBreaker() {
	fmt.Println("⚡ 熔断器机制演示")
	fmt.Println("-" + string(make([]byte, 40)))

	config := ap.ResilientConfig{
		MaxRetries:          0,
		CircuitThreshold:    3,
		CircuitRecoverAfter: 5 * time.Second,
	}

	failingProvider := demo.NewDemoLLM("失败响应").WithError(ap.ErrLLMCallFailed)
	resilient, err := ap.NewResilientProvider(failingProvider, config)
	if err != nil {
		fmt.Printf("❌ 创建 ResilientProvider 失败: %v\n", err)
		return
	}

	safeFallback := demo.NewDemoLLM("熔断后的安全响应")
	resilient.AddFallback(safeFallback)

	fmt.Println("📊 熔断器配置:")
	fmt.Println("   - 失败阈值: 3 次")
	fmt.Println("   - 熔断恢复时间: 5 秒")
	fmt.Println("   - 模拟: 所有请求失败，触发熔断后走 Fallback")

	ctx := context.Background()
	for i := 1; i <= 6; i++ {
		resp, err := resilient.Complete(ctx, &ap.CompletionRequest{
			Messages: []ap.ChatMessage{{Role: "user", Content: fmt.Sprintf("请求 %d", i)}},
		})

		status := "✅ 成功"
		if err != nil {
			status = "❌ 失败/熔断中"
		} else if resp != nil && resp.Content == "熔断后的安全响应" {
			status = "⚠️  降级/Fallback"
		}

		fmt.Printf("   请求 %d: %s", i, status)
		if resp != nil {
			fmt.Printf(" → %s", truncate(resp.Content, 30))
		}
		fmt.Println()
	}
	fmt.Println()
}

func demonstrateCustomConfig() {
	fmt.Println("⚙️  自定义配置演示")
	fmt.Println("-" + string(make([]byte, 40)))

	customConfig := ap.ResilientConfig{
		MaxRetries:          5,
		RetryBackoff:        500 * time.Millisecond,
		MaxBackoff:          10 * time.Second,
		CircuitThreshold:    10,
		CircuitRecoverAfter: 60 * time.Second,
	}

	fmt.Println("📋 自定义配置详情:")
	fmt.Printf("   最大重试次数: %d\n", customConfig.MaxRetries)
	fmt.Printf("   退避时间: %v\n", customConfig.RetryBackoff)
	fmt.Printf("   最大退避时间: %v\n", customConfig.MaxBackoff)
	fmt.Printf("   熔断阈值: %d 次\n", customConfig.CircuitThreshold)
	fmt.Printf("   熔断恢复时间: %v\n", customConfig.CircuitRecoverAfter)

	fmt.Println("\n💡 使用建议:")
	fmt.Println("   - 生产环境: MaxRetries=3-5, CircuitThreshold=5-10")
	fmt.Println("   - 测试环境: MaxRetries=1-2, 快速失败")
	fmt.Println("   - 高可用场景: 多个 Fallback + 短退避时间")
	fmt.Println("   - 成本优化: 优先使用便宜的 Provider 作为 Fallback")
	fmt.Println()
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
