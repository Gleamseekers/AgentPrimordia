package main

import (
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/cmd/example/demo"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/debugger"
	"context"
	"fmt"
	"log"
	"net/http"
	"time"
)

func main() {
	// 创建Inspector实例
	inspector := debugger.NewInspector(1000)

	// 创建Inspector HTTP服务器
	server := debugger.NewInspectorServer(inspector)

	// 启动HTTP服务器（在后台运行）
	go func() {
		log.Println("AP Inspector UI 已启动: http://localhost:6061/inspector")
		if err := http.ListenAndServe(":6061", server.Handler()); err != nil {
			log.Printf("Inspector server error: %v\n", err)
		}
	}()

	// 等待服务器启动
	time.Sleep(100 * time.Millisecond)

	// 创建一个示例Agent
	demoLLM := demo.NewDemoLLM("你好！我是你的AI助手，很高兴为你服务。")

	agentName := "DemoAgent"
	a, err := agent.NewAgent(agentName, "你是一个友好的助手，帮助用户解答问题。", demoLLM, agent.WithMaxTurns(5))
	if err != nil {
		log.Fatalf("Agent 创建失败: %v", err)
	}

	// 模拟Agent执行过程，并记录追踪数据
	ctx := context.Background()
	sessionID := "session-demo-001"

	// 开始Agent级别的Span
	agentSpan, ctx := inspector.StartSpan(ctx, "agent-run", "agent", sessionID)
	inspector.SetAttribute(agentSpan, "agent_name", agentName)
	inspector.SetAttribute(agentSpan, "max_turns", 5)

	// 模拟多轮对话
	messages := []string{
		"你好，请介绍一下自己",
		"你能做什么？",
		"谢谢！",
	}

	for i, msg := range messages {
		// 开始一轮对话的Span
		turnSpan, turnCtx := inspector.StartSpan(ctx, fmt.Sprintf("turn-%d", i+1), "agent", sessionID)
		inspector.SetAttribute(turnSpan, "turn_number", i+1)
		inspector.SetAttribute(turnSpan, "user_message", msg)

		// 模拟LLM调用
		llmSpan, _ := inspector.StartSpan(turnCtx, "llm-call", "llm", sessionID)
		inspector.SetAttribute(llmSpan, "model", "demo-llm")
		inspector.AddEvent(llmSpan, "prompt_sent", map[string]interface{}{
			"message": msg,
		})

		// 模拟LLM响应
		time.Sleep(100 * time.Millisecond)
		llmSpan.PromptTokens = 50 + i*10
		llmSpan.CompletionTokens = 80 + i*15
		llmSpan.TotalTokens = llmSpan.PromptTokens + llmSpan.CompletionTokens
		inspector.EndSpan(llmSpan, nil)

		// 模拟工具调用（第一轮）
		if i == 0 {
			toolSpan, _ := inspector.StartSpan(turnCtx, "tool-call", "tool", sessionID)
			inspector.SetAttribute(toolSpan, "tool_name", "search")
			inspector.AddEvent(toolSpan, "tool_executed", map[string]interface{}{
				"query": "demo query",
			})
			time.Sleep(50 * time.Millisecond)
			inspector.EndSpan(toolSpan, nil)
		}

		// 模拟记忆存储
		memorySpan, _ := inspector.StartSpan(turnCtx, "memory-store", "memory", sessionID)
		inspector.SetAttribute(memorySpan, "operation", "store")
		time.Sleep(20 * time.Millisecond)
		inspector.EndSpan(memorySpan, nil)

		// 结束本轮对话
		inspector.EndSpan(turnSpan, nil)

		fmt.Printf("Turn %d: %s\n", i+1, msg)
	}

	// 实际运行Agent（演示真实调用）
	fmt.Println("\n=== 运行真实Agent ===")
	resp, err := a.Run(ctx, agent.UserMessage("你好！"))
	if err != nil {
		log.Printf("Agent 运行失败: %v", err)
	} else {
		fmt.Printf("Agent 回复: %s\n", resp.Content)
	}

	// 结束Agent级别的Span
	inspector.EndSpan(agentSpan, nil)

	// 打印统计信息
	stats := inspector.GetStats()
	fmt.Printf("\n=== Inspector 统计信息 ===\n")
	fmt.Printf("总追踪数: %d\n", stats.TotalSpans)
	fmt.Printf("总会话数: %d\n", stats.TotalSessions)
	fmt.Printf("总Token数: %d\n", stats.TotalTokens)
	fmt.Printf("按类型统计:\n")
	for kind, count := range stats.SpanByKind {
		fmt.Printf("  %s: %d\n", kind, count)
	}
	fmt.Printf("按状态统计:\n")
	for status, count := range stats.SpanByStatus {
		fmt.Printf("  %s: %d\n", status, count)
	}

	fmt.Printf("\n✅ AP Inspector 正在运行: http://localhost:6061/inspector\n")
	fmt.Printf("按 Ctrl+C 退出...\n")

	// 保持程序运行，让用户查看Web UI
	select {}
}
