// data-analysis-agent 示例演示一个数据分析 agent 的使用场景。
//
// 运行方式（从 monorepo 根目录）:
//   go run ./ecosystem/examples/data-analysis-agent/
//
// 本示例：
//   1. 使用 DemoProvider 演示数据分析功能
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
	fmt.Println("数据分析 Agent — 使用示例")
	fmt.Println("=========================")
	fmt.Println()

	// 步骤 1: 创建 LLM provider
	fmt.Println("[步骤 1] 创建 LLM provider...")
	ctx := context.Background()

	// 使用 DemoProvider 演示（无需 API key）
	provider := ap.NewDemoProvider()
	fmt.Println("✓ 使用 DemoProvider（演示模式）")
	fmt.Println()

	// 步骤 2: 模拟数据分析对话
	fmt.Println("[步骤 2] 进行数据分析对话...")
	fmt.Println()

	analyses := []struct {
		title       string
		description string
	}{
		{
			title:       "CSV 数据概览",
			description: "分析一个包含销售数据的 CSV 文件，包含日期、产品、销售额、数量等字段",
		},
		{
			title:       "SQL 查询优化",
			description: "优化一个慢查询：SELECT * FROM orders WHERE created_at > '2024-01-01' ORDER BY total DESC",
		},
		{
			title:       "数据可视化建议",
			description: "为月度销售趋势数据推荐合适的图表类型和展示方式",
		},
		{
			title:       "异常值检测",
			description: "检查数据集中的异常值：[10, 12, 11, 13, 12, 100, 11, 12, 10]",
		},
	}

	for i, analysis := range analyses {
		fmt.Printf("分析 #%d: %s\n", i+1, analysis.title)
		fmt.Printf("任务: %s\n", analysis.description)

		// 调用 provider 进行分析
		req := &ap.CompletionRequest{
			Messages: []ap.ChatMessage{
				{Role: "user", Content: fmt.Sprintf("请帮我分析: %s", analysis.description)},
			},
		}

		resp, err := provider.Complete(ctx, req)
		if err != nil {
			log.Printf("分析失败: %v", err)
			continue
		}

		// 输出分析结果
		fmt.Printf("分析结果:\n%s\n", resp.Content)
		fmt.Println(strings.Repeat("-", 60))
		fmt.Println()
	}

	// 步骤 3: 说明学习闭环
	fmt.Println("[步骤 3] 学习闭环说明")
	fmt.Println()
	fmt.Println("在真实使用场景中：")
	fmt.Println("  1. 每次分析后，SelfModel 会记录：")
	fmt.Println("     - 分析领域（csv-analysis, sql-optimization 等）")
	fmt.Println("     - 分析结果（成功/部分成功/失败）")
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
	fmt.Println("  3. 添加数据分析工具：")
	fmt.Println("     - CSV 解析工具")
	fmt.Println("     - SQL 执行工具")
	fmt.Println("     - 统计分析工具")
	fmt.Println("     - 可视化工具")
	fmt.Println()

	// 步骤 5: 总结
	fmt.Println("[总结]")
	fmt.Println("✓ 数据分析 agent 完成 4 次分析演示")
	fmt.Println("✓ DemoProvider 提供基础响应能力")
	fmt.Println("✓ 真实场景中会自动记录学习数据")
	fmt.Println("✓ 使用 'ap profile' 查看成长报告")
	fmt.Println()
	fmt.Println("下一步:")
	fmt.Println("  - 设置 AP_LLM_API_KEY 使用真实 LLM")
	fmt.Println("  - 添加数据分析工具（CSV、SQL、统计）")
	fmt.Println("  - 集成数据库连接")
	fmt.Println("  - 运行 'ap profile' 查看详细报告")
	fmt.Println("  - 启动 Studio 面板查看可视化数据")
}
