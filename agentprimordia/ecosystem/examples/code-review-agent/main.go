// code-review-agent 示例演示一个代码审查 agent 的使用场景。
//
// 运行方式（从 monorepo 根目录）:
//   go run ./ecosystem/examples/code-review-agent/
//
// 本示例：
//   1. 使用 DemoProvider 演示代码审查功能
//   2. 展示如何与 agent 进行多轮对话
//   3. 说明如何接入真实 LLM 和学习系统
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	ap "agentprimordia/pkg"
)

func main() {
	fmt.Println("代码审查 Agent — 使用示例")
	fmt.Println("=========================")
	fmt.Println()

	// 步骤 1: 创建 LLM provider
	fmt.Println("[步骤 1] 创建 LLM provider...")
	ctx := context.Background()

	// 使用 DemoProvider 演示（无需 API key）
	provider := ap.NewDemoProvider()
	fmt.Println("✓ 使用 DemoProvider（演示模式）")
	fmt.Println()

	// 步骤 2: 模拟代码审查对话
	fmt.Println("[步骤 2] 进行代码审查对话...")
	fmt.Println()

	reviews := []struct {
		title string
		code  string
	}{
		{
			title: "基础函数审查",
			code: `func add(a, b int) int {
    return a + b
}`,
		},
		{
			title: "边界条件检查",
			code: `func processData(data []int) {
    for i := 0; i <= len(data); i++ {
        fmt.Println(data[i])
    }
}`,
		},
		{
			title: "并发安全审查",
			code: `var mu sync.Mutex
var cache map[string]int

func get(key string) int {
    mu.Lock()
    defer mu.Unlock()
    return cache[key]
}`,
		},
	}

	for i, review := range reviews {
		fmt.Printf("审查 #%d: %s\n", i+1, review.title)
		fmt.Printf("代码:\n%s\n", review.code)

		// 调用 provider 进行审查
		req := &ap.CompletionRequest{
			Messages: []ap.ChatMessage{
				{Role: "user", Content: fmt.Sprintf("请审查这段 Go 代码:\n\n%s", review.code)},
			},
		}

		resp, err := provider.Complete(ctx, req)
		if err != nil {
			log.Printf("审查失败: %v", err)
			continue
		}

		// 输出审查结果
		fmt.Printf("审查结果:\n%s\n", resp.Content)
		fmt.Println(strings.Repeat("-", 60))
		fmt.Println()
	}

	// 步骤 3: 说明学习闭环
	fmt.Println("[步骤 3] 学习闭环说明")
	fmt.Println()
	fmt.Println("在真实使用场景中：")
	fmt.Println("  1. 每次审查后，SelfModel 会记录：")
	fmt.Println("     - 审查的领域（go-basics, go-concurrency 等）")
	fmt.Println("     - 审查结果（成功/部分成功/失败）")
	fmt.Println("     - 消耗的轮数")
	fmt.Println()
	fmt.Println("  2. 运行 'ap profile' 查看成长报告：")
	fmt.Println("     - 总体成功率")
	fmt.Println("     - 各领域表现排名")
	fmt.Println("     - 弱项领域和改进建议")
	fmt.Println("     - 成长事件时间线")
	fmt.Println()
	fmt.Println("  3. 启动 Studio 面板查看可视化数据：")
	fmt.Println("     - 能力雷达图")
	fmt.Println("     - 成长曲线")
	fmt.Println("     - 工具使用热力图")
	fmt.Println()

	// 步骤 4: 接入真实 LLM
	fmt.Println("[步骤 4] 接入真实 LLM")
	fmt.Println()
	fmt.Println("要使用真实 LLM 替代 DemoProvider：")
	fmt.Println()
	fmt.Println("  1. 设置环境变量：")
	fmt.Println("     export AP_LLM_API_KEY=sk-your-key-here")
	fmt.Println()
	fmt.Println("  2. 修改代码：")
	fmt.Println("     provider := ap.NewOpenAIProvider(")
	fmt.Println("         \"https://api.openai.com/v1\",")
	fmt.Println("         os.Getenv(\"AP_LLM_API_KEY\"),")
	fmt.Println("         \"gpt-4o\",")
	fmt.Println("     )")
	fmt.Println()
	fmt.Println("  3. 支持的 provider：")
	fmt.Println("     - OpenAI (GPT-4, GPT-3.5)")
	fmt.Println("     - Anthropic (Claude)")
	fmt.Println("     - Gemini (Google)")
	fmt.Println("     - Ollama (本地模型)")
	fmt.Println("     - Azure OpenAI")
	fmt.Println("     - 通义千问、智谱 GLM 等国内模型")
	fmt.Println()

	// 步骤 5: 总结
	fmt.Println("[总结]")
	fmt.Println("✓ 代码审查 agent 完成 3 次审查演示")
	fmt.Println("✓ DemoProvider 提供基础响应能力")
	fmt.Println("✓ 真实场景中会自动记录学习数据")
	fmt.Println("✓ 使用 'ap profile' 查看成长报告")
	fmt.Println()
	fmt.Println("下一步:")
	fmt.Println("  - 设置 AP_LLM_API_KEY 使用真实 LLM")
	fmt.Println("  - 添加代码分析工具（AST 解析、静态分析）")
	fmt.Println("  - 集成到 CI/CD 流程中")
	fmt.Println("  - 运行 'ap profile' 查看详细报告")
	fmt.Println("  - 启动 Studio 面板查看可视化数据")
}
