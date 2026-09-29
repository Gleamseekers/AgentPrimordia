package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/cmd/example/demo"
	ap "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg"
)

func main() {
	fmt.Println("🛠️  内置工具集示例")
	fmt.Println("=" + string(make([]byte, 50)))
	fmt.Println()

	demonstrateCalculatorTool()
	demonstrateDateTimeTool()
	demonstrateAgentWithTools()
}

func demonstrateCalculatorTool() {
	fmt.Println("🔢 Calculator 计算器工具演示")
	fmt.Println("-" + string(make([]byte, 40)))

	calc := ap.NewCalculator()

	testCases := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "加法",
			args: map[string]interface{}{
				"operation": "add",
				"a":         float64(10),
				"b":         float64(20),
			},
		},
		{
			name: "减法",
			args: map[string]interface{}{
				"operation": "subtract",
				"a":         float64(100),
				"b":         float64(30),
			},
		},
		{
			name: "乘法",
			args: map[string]interface{}{
				"operation": "multiply",
				"a":         float64(7),
				"b":         float64(8),
			},
		},
		{
			name: "除法",
			args: map[string]interface{}{
				"operation": "divide",
				"a":         float64(100),
				"b":         float64(4),
			},
		},
	}

	ctx := context.Background()
	for _, tc := range testCases {
		argsJSON, _ := json.Marshal(tc.args)
		result, err := calc.Execute(ctx, argsJSON)
		if err != nil {
			log.Printf("❌ %s 失败: %v", tc.name, err)
			continue
		}
		fmt.Printf("✅ %-8s → 结果: %s\n", tc.name, result.Content)
	}

	divZeroArgs, _ := json.Marshal(map[string]interface{}{
		"operation": "divide",
		"a":         float64(10),
		"b":         float64(0),
	})
	result, _ := calc.Execute(ctx, divZeroArgs)
	fmt.Printf("⚠️  除零测试   → 错误: %s\n", result.Content)
	fmt.Println()
}

func demonstrateDateTimeTool() {
	fmt.Println("📅 DateTime 日期时间工具演示")
	fmt.Println("-" + string(make([]byte, 40)))

	dt := ap.NewDateTime()

	testCases := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "默认格式 (RFC3339)",
			args: map[string]interface{}{
				"action": "now",
			},
		},
		{
			name: "ISO8601 格式",
			args: map[string]interface{}{
				"action": "now",
				"format": "ISO8601",
			},
		},
		{
			name: "简单日期时间",
			args: map[string]interface{}{
				"action": "now",
				"format": "simple",
			},
		},
		{
			name: "仅日期",
			args: map[string]interface{}{
				"action": "now",
				"format": "date",
			},
		},
		{
			name: "仅时间",
			args: map[string]interface{}{
				"action": "now",
				"format": "time",
			},
		},
	}

	ctx := context.Background()
	for _, tc := range testCases {
		argsJSON, _ := json.Marshal(tc.args)
		result, err := dt.Execute(ctx, argsJSON)
		if err != nil {
			log.Printf("❌ %s 失败: %v", tc.name, err)
			continue
		}
		fmt.Printf("✅ %-20s → %s\n", tc.name, result.Content)
	}
	fmt.Println()
}

func demonstrateAgentWithTools() {
	fmt.Println("🤖 Agent 集成工具示例")
	fmt.Println("-" + string(make([]byte, 40)))

	registry, err := ap.DefaultToolkit(ap.ToolkitConfig{
		RootDir:     "./workspace",
		EnableFS:    false,
		EnableShell: false,
		EnableWeb:   false,
		EnableUtils: true,
	})
	if err != nil {
		log.Fatalf("❌ 创建工具集失败: %v", err)
	}

	demoLLM := demo.NewDemoLLM(
		"我使用了计算器工具计算 15+25=40，并获取了当前时间。",
	).WithToolCalls(
		ap.FunctionCall{
			ID:        "call_1",
			Name:      "calculator",
			Arguments: `{"operation":"add","a":15,"b":25}`,
		},
		ap.FunctionCall{
			ID:        "call_2",
			Name:      "datetime",
			Arguments: `{"action":"now","format":"simple"}`,
		},
	)

	toolAgent, err := ap.NewAgent("ToolBot", "你可以使用计算器和日期时间工具来帮助用户完成任务。", demoLLM,
		ap.WithMaxTurns(5),
		ap.WithToolkit(registry),
	)
	if err != nil {
		log.Fatalf("❌ 创建 Agent 失败: %v", err)
	}

	ctx := context.Background()

	fmt.Println("📝 测试1: 数学计算")
	resp1, _ := toolAgent.Run(ctx, ap.UserMessage("请帮我计算 15 + 25 的结果"))
	fmt.Printf("   🤖 回复: %s\n", resp1.Content)

	fmt.Println("\n📝 测试2: 获取当前时间")
	resp2, _ := toolAgent.Run(ctx, ap.UserMessage("现在几点了？请用简单的格式告诉我"))
	fmt.Printf("   🤖 回复: %s\n", resp2.Content)

	fmt.Println("\n📝 测试3: 组合任务")
	resp3, _ := toolAgent.Run(ctx, ap.UserMessage(
		"先计算 123 * 456，然后告诉我当前时间，最后总结一下结果",
	))
	fmt.Printf("   🤖 回复: %s\n", resp3.Content)

	fmt.Println("\n💡 工具使用统计:")
	fmt.Printf("   📊 总轮数: %d\n", resp3.Metrics.TotalTurns)
	if len(resp3.ToolCalls) > 0 {
		fmt.Println("   🔧 调用的工具:")
		for i, tc := range resp3.ToolCalls {
			fmt.Printf("      %d. %s(%s\n", i+1, tc.Name, truncateString(tc.Args, 50))
		}
	}
	fmt.Println()
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
