package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/cmd/example/demo"
	ap "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg"
)

func main() {
	fmt.Println("🤝 多 Agent 协作示例")
	fmt.Println("=" + string(make([]byte, 60)))
	fmt.Println()

	ctx := context.Background()

	// ===== 场景：软件开发团队协作 =====
	fmt.Println("📋 场景：模拟软件开发团队协作流程")
	fmt.Println()

	// 创建不同角色的 Agent
	researcher, err := ap.NewAgent("研究员", "你是需求分析师，负责收集和分析用户需求。", demo.NewDemoLLM("需求分析完成：用户需要一个登录功能。"),
		ap.WithMaxTurns(3),
	)
	if err != nil {
		log.Fatalf("❌ 创建研究员失败: %v", err)
	}

	coder, err := ap.NewAgent("开发者", "你是后端开发工程师，负责实现API接口。", demo.NewDemoLLM("代码开发完成：已实现 /api/login 接口。"),
		ap.WithMaxTurns(3),
	)
	if err != nil {
		log.Fatalf("❌ 创建开发者失败: %v", err)
	}

	tester, err := ap.NewAgent("测试员", "你是QA工程师，负责编写测试用例。", demo.NewDemoLLM("测试完成：所有用例通过，覆盖率达95%。"),
		ap.WithMaxTurns(3),
	)
	if err != nil {
		log.Fatalf("❌ 创建测试员失败: %v", err)
	}

	// ===== 模式1: Pipeline 流水线 =====
	fmt.Println("🔄 模式1: Pipeline 流水线（顺序协作）")
	fmt.Println("-" + string(make([]byte, 40)))

	pipeline := ap.NewPipeline(
		ap.PipelineStep{Name: "需求分析", Agent: researcher},
		ap.PipelineStep{Name: "编码实现", Agent: coder},
		ap.PipelineStep{Name: "质量测试", Agent: tester},
	)

	pipelineResult, err := pipeline.Run(ctx, "开发一个用户登录功能")
	if err != nil {
		log.Fatalf("Pipeline 执行失败: %v", err)
	}

	fmt.Printf("⏱️  总耗时: %v\n", pipelineResult.Duration)
	for i, step := range pipelineResult.Steps {
		status := "✅"
		if step.Error != nil {
			status = "❌"
		}
		if step.Skipped {
			status = "⏭️ "
		}
		fmt.Printf("  [%d] %s %s (%v)\n", i+1, status, step.Name, step.Duration)
	}
	fmt.Printf("📝 最终输出: %s\n\n", pipelineResult.Final)

	// ===== 模式2: Pool 并发调度 =====
	fmt.Println("🚀 模式2: Pool 并发调度（并行协作）")
	fmt.Println("-" + string(make([]byte, 40)))

	p := ap.NewPool(ap.PoolConfig{
		MaxConcurrency: 3,
		Timeout:        30 * time.Second,
	})
	defer p.Close()

	demoLLM := demo.NewDemoLLM(
		"任务1完成: 已分析需求文档。",
		"任务2完成: 已开发API接口。",
		"任务3完成: 测试用例全部通过。",
	)
	p.SetModel(demoLLM)

	tasks := []ap.TaskConfig{
		{ID: "t1", Title: "分析需求A", Prompt: "分析登录功能需求"},
		{ID: "t2", Title: "开发模块B", Prompt: "开发认证模块"},
		{ID: "t3", Title: "测试接口C", Prompt: "测试API接口"},
	}

	poolStart := time.Now()
	results, err := p.Dispatch(ctx, tasks)
	if err != nil {
		log.Fatalf("Pool 调度失败: %v", err)
	}

	poolDuration := time.Since(poolStart)
	successCount := 0
	for _, r := range results {
		icon := "✅"
		if r.Error != nil {
			icon = "❌"
		} else {
			successCount++
		}
		fmt.Printf("  %s [%s] %s (%v)\n", icon, r.TaskID, r.Task.Title, r.Duration)
	}
	fmt.Printf("⏱️  总耗时: %v (3个任务并行)\n", poolDuration)
	fmt.Printf("✅ 成功率: %d/%d\n\n", successCount, len(tasks))

	// ===== 模式3: Handoff 动态交接 =====
	fmt.Println("🎯 模式3: Handoff 动态交接（智能路由）")
	fmt.Println("-" + string(make([]byte, 40)))

	handoff := ap.NewHandoff(ap.HandoffConfig{
		Agents: []ap.Agent{researcher, coder, tester},
		Router: func(ctx context.Context, input string) int {
			if containsAny(input, "bug", "错误", "问题") {
				return 2 // 路由到测试员
			}
			if containsAny(input, "代码", "实现", "开发") {
				return 1 // 路由到开发者
			}
			return 0 // 默认给研究员
		},
		MaxHandoffs: 3,
	})

	handoffStart := time.Now()
	handoffResult, err := handoff.Run(ctx, "发现一个登录 bug")
	if err != nil {
		log.Fatalf("Handoff 执行失败: %v", err)
	}

	fmt.Printf("🎯 最终由 [%s] 处理\n", handoffResult.AgentName)
	fmt.Printf("📝 输出: %s\n", handoffResult.Output)
	fmt.Printf("🔄 交接次数: %d\n", handoffResult.Handoffs)
	fmt.Printf("⏱️  耗时: %v\n\n", time.Since(handoffStart))

	// ===== 模式4: GroupChat 群组讨论 =====
	fmt.Println("💬 模式4: GroupChat 群组讨论（团队会议）")
	fmt.Println("-" + string(make([]byte, 40)))

	group, err := ap.NewGroupChat(ap.GroupChatConfig{
		Agents:    []ap.Agent{researcher, coder, tester},
		MaxRounds: 3,
	})
	if err != nil {
		log.Fatalf("创建 GroupChat 失败: %v", err)
	}

	groupStart := time.Now()
	groupResult, err := group.Run(ctx, ap.UserMessage("讨论如何优化登录性能"))
	if err != nil {
		log.Fatalf("GroupChat 执行失败: %v", err)
	}

	fmt.Printf("👥 参与者: %d 个 Agent\n", 3)
	if len(groupResult.Messages) > 0 {
		lastMsg := groupResult.Messages[len(groupResult.Messages)-1]
		fmt.Printf("📝 最后发言: %s\n", lastMsg.Content)
	}
	fmt.Printf("🔄 讨论轮次: %d\n", groupResult.Rounds)
	fmt.Printf("⏱️  耗时: %v\n\n", time.Since(groupStart))

	// ===== 总结 =====
	fmt.Println("=" + string(make([]byte, 60)))
	fmt.Println("🎉 所有协作模式演示完成！")
	fmt.Println()
	fmt.Println("📊 对比总结:")
	fmt.Printf("  Pipeline (顺序):    %v\n", pipelineResult.Duration)
	fmt.Printf("  Pool (并行):       %v  ← 最快！\n", poolDuration)
	fmt.Printf("  Handoff (路由):    %v\n", time.Since(handoffStart))
	fmt.Printf("  GroupChat (讨论):  %v\n", time.Since(groupStart))
	fmt.Println()
	fmt.Println("💡 选择建议:")
	fmt.Println("  • 依赖关系强 → Pipeline")
	fmt.Println("  • 任务独立   → Pool (推荐)")
	fmt.Println("  • 需要分类   → Handoff")
	fmt.Println("  • 团队决策   → GroupChat")
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if contains(s, sub) {
			return true
		}
	}
	return false
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
