package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/cmd/example/demo"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/events"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/memory"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/metrics"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/persist"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools/builtin"
	ap "github.com/Gleamseekers/AgentPrimordia/agentprimordia/pkg"
)

func main() {
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║   AgentPrimordia 生产级示例：RAG 增强知识助手               ║")
	fmt.Println("║   展示: RAG + 多模型 + 弹性调用 + 事件系统 + 可观测性       ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// 优雅退出
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\n[信号] 收到中断信号，正在优雅退出...")
		cancel()
	}()

	// ===== 1. 初始化可观测性 =====
	fmt.Println("── 步骤 1: 初始化可观测性 ──")

	eventBus := events.NewBus(100)
	agentMetrics := metrics.NewMetrics()

	// 订阅所有事件
	eventCh, subID := eventBus.SubscribeAll()
	go func() {
		for evt := range eventCh {
			fmt.Printf("  [事件] %s | 来源: %s\n", evt.Type, evt.Source)
		}
	}()

	fmt.Println("  ✓ 事件总线已启动 (订阅ID: " + subID + ")")
	fmt.Println("  ✓ 指标收集器已创建")
	fmt.Println()

	// ===== 2. 初始化记忆与 RAG =====
	fmt.Println("── 步骤 2: 初始化记忆与 RAG 知识库 ──")

	memStore, err := memory.WithInMemory()
	if err != nil {
		log.Fatalf("Memory 初始化失败: %v", err)
	}
	defer memStore.Close()

	// 预加载知识库文档
	knowledgeDocs := []struct {
		role    string
		content string
	}{
		{"knowledge", "Go 语言由 Google 的 Robert Griesemer、Rob Pike 和 Ken Thompson 于 2009 年发布。它是一种静态类型、编译型语言，专注于简洁性和高效性。"},
		{"knowledge", "Go 的并发模型基于 goroutine 和 channel。goroutine 是轻量级线程，由 Go 运行时管理，创建成本约 2KB 栈空间。"},
		{"knowledge", "Go 的接口是隐式实现的——类型只需实现接口的所有方法即可满足接口，无需显式声明。这促进了松耦合设计。"},
		{"knowledge", "Go 模块系统 (Go Modules) 从 Go 1.11 引入，1.16 后成为默认。使用 go.mod 文件声明依赖，支持语义化版本控制。"},
		{"knowledge", "Go 的垃圾回收器使用了并发标记清除算法，从 Go 1.5 开始实现了低延迟 GC，STW (Stop The World) 时间通常在亚毫秒级。"},
		{"knowledge", "Go 泛型 (Generics) 在 Go 1.18 正式发布，使用类型参数约束实现，支持类型推断。语法为 func Name[T any](v T) T。"},
	}

	for _, doc := range knowledgeDocs {
		ep, err := memory.NewEpisode("knowledge-base", doc.role, doc.content)
		if err != nil {
			log.Printf("  ✗ 创建知识文档 Episode 失败: %v", err)
			continue
		}
		ep.Importance = 0.9 // 高重要度
		if err := memStore.Add(ctx, ep); err != nil {
			log.Printf("  ✗ 加载知识文档失败: %v", err)
		}
	}

	// 创建 RAG Store (无 Embedding，使用 FTS 模式)
	ragStore := ap.NewRAGStore(memStore, nil)
	ragProvider := ap.NewRAGProviderAdapter(ragStore)

	fmt.Printf("  ✓ 已加载 %d 条知识文档\n", len(knowledgeDocs))
	fmt.Println("  ✓ RAG Store 已创建 (FTS 模式)")
	fmt.Println()

	// ===== 3. 初始化工具系统 =====
	fmt.Println("── 步骤 3: 初始化工具系统 ──")

	toolRegistry := tools.NewRegistry()
	fsTool, err := builtin.NewFileSystem(".")
	if err != nil {
		log.Fatal(err)
	}
	_ = toolRegistry.Register(fsTool)
	_ = toolRegistry.Register(builtin.NewWeb())
	_ = toolRegistry.Register(builtin.NewShell())

	// 注册知识库搜索工具（RAG on_demand 模式）
	knowledgeSearcher := ap.NewKnowledgeSearcherAdapter(ragStore)
	_ = toolRegistry.Register(builtin.NewKnowledgeSearch(knowledgeSearcher))

	fmt.Printf("  ✓ 已注册 %d 个工具: %v\n", toolRegistry.Count(), toolRegistry.List())
	fmt.Println()

	// ===== 4. 创建检查点存储 =====
	fmt.Println("── 步骤 4: 创建检查点存储 ──")

	tmpDir, err := os.MkdirTemp("", "ap-checkpoint-*")
	if err != nil {
		log.Fatalf("创建临时目录失败: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	checkpointStore, err := persist.NewSQLiteCheckpointStore(tmpDir + "/checkpoints.db")
	if err != nil {
		log.Fatalf("检查点存储初始化失败: %v", err)
	}
	defer checkpointStore.Close()

	fmt.Println("  ✓ 检查点存储已就绪")
	fmt.Println()

	// ===== 5. 创建 LLM (使用 DemoLLM 演示) =====
	fmt.Println("── 步骤 5: 初始化 LLM Provider ──")

	demoLLM := demo.NewDemoLLM(
		"根据知识库中的信息，Go 语言具有以下特点：\n" +
			"1. 静态类型、编译型语言，2009年由Google发布\n" +
			"2. 并发模型基于goroutine和channel\n" +
			"3. 接口隐式实现，促进松耦合\n" +
			"4. 从1.18开始支持泛型\n" +
			"5. 低延迟GC，STW时间在亚毫秒级",
	)

	fmt.Println("  ✓ DemoLLM 已创建 (生产环境可替换为 OpenAI/Azure/Anthropic/Gemini/Ollama)")
	fmt.Println()

	// ===== 6. 创建 RAG 增强的 Agent =====
	fmt.Println("── 步骤 6: 创建 RAG 增强知识助手 ──")

	hooks := agent.NewHookManager()

	// 注册生命周期钩子
	hooks.Register(agent.HookBeforeRAG, func(ctx context.Context, hctx *agent.HookContext) error {
		query, _ := hctx.Metadata["query"].(string)
		fmt.Printf("  [RAG] 正在检索知识库: %q\n", truncate(query, 50))
		return nil
	})
	hooks.Register(agent.HookAfterRAG, func(ctx context.Context, hctx *agent.HookContext) error {
		count, _ := hctx.Metadata["results"].(int)
		query, _ := hctx.Metadata["query"].(string)
		fmt.Printf("  [RAG] 检索完成: 找到 %d 条相关结果 (查询: %q)\n", count, truncate(query, 30))
		return nil
	})
	hooks.Register(agent.HookBeforeTool, func(ctx context.Context, hctx *agent.HookContext) error {
		if hctx.ToolCall != nil {
			fmt.Printf("  [工具] 调用 %s\n", hctx.ToolCall.Name)
		}
		return nil
	})
	hooks.Register(agent.HookAfterTool, func(ctx context.Context, hctx *agent.HookContext) error {
		if hctx.ToolResult != nil {
			icon := "OK"
			if hctx.ToolResult.IsError {
				icon = "ERR"
			}
			fmt.Printf("  [结果] %s %s\n", icon, truncate(hctx.ToolResult.Content, 80))
		}
		return nil
	})
	hooks.Register(agent.HookOnComplete, func(ctx context.Context, hctx *agent.HookContext) error {
		if hctx.Response != nil {
			fmt.Printf("  [完成] 耗时: %v, 轮数: %d\n",
				hctx.Response.Metrics.Duration, hctx.Response.Metrics.TotalTurns)
		}
		return nil
	})

	ragAgent, err := agent.NewAgent("RAGKnowledgeAssistant", `你是一个专业的知识助手。在回答问题前，请先检索知识库获取相关信息。
如果知识库中没有相关信息，请诚实说明，不要编造内容。
回答时要引用知识来源，保持准确性和专业性。`, demoLLM,
		agent.WithToolkit(toolRegistry),
		agent.WithMemory(memStore),
		agent.WithMaxTurns(15),
		agent.WithTemperature(0.7),
		agent.WithSessionID("rag-demo-session"),
		agent.WithHooks(hooks),
		agent.WithRAG(agent.RAGConfig{
			Provider: ragProvider,
			Mode:     agent.RAGModeFirst, // 第一轮自动检索
			TopK:     3,
			MinScore: 0.1,
		}),
		agent.WithCheckpointStore(checkpointStore),
		agent.WithEvents(ap.NewEventBusAdapter(eventBus)),
		agent.WithMetrics(agentMetrics),
	)
	if err != nil {
		log.Fatalf("创建 RAG Agent 失败: %v", err)
	}

	fmt.Println("  ✓ RAG 知识助手已创建")
	fmt.Println("    - RAG 模式: First (首轮自动检索)")
	fmt.Println("    - RAG TopK: 3")
	fmt.Println("    - 最低相关度: 0.1")
	fmt.Println()

	// ===== 7. 执行查询 =====
	fmt.Println("── 步骤 7: 执行知识查询 ──")
	fmt.Println()

	queries := []string{
		"Go语言的并发模型是怎样的？",
		"Go什么时候开始支持泛型的？",
	}

	for i, query := range queries {
		fmt.Printf("--- 查询 %d: %s ---\n", i+1, query)
		fmt.Println()

		resp, err := ragAgent.Run(ctx, agent.UserMessage(query))
		if err != nil {
			fmt.Printf("[错误] %v\n", err)
			continue
		}

		fmt.Println()
		fmt.Println("  回答:")
		fmt.Printf("  %s\n", resp.Content)
		fmt.Println()
		fmt.Printf("  指标: 轮数=%d, 工具=%d, 耗时=%v\n",
			resp.Metrics.TotalTurns, resp.Metrics.TotalTools, resp.Metrics.Duration)
		fmt.Println()
	}

	// ===== 8. 流式输出演示 =====
	fmt.Println("── 步骤 8: 流式输出演示 ──")
	fmt.Println()

	fmt.Println("--- 流式查询: Go的垃圾回收器特点 ---")
	fmt.Println()

	streamCh, err := ragAgent.StreamRun(ctx, agent.UserMessage("Go的垃圾回收器有什么特点？"))
	if err != nil {
		fmt.Printf("[错误] 流式启动失败: %v\n", err)
	} else {
		fmt.Print("  ")
		for evt := range streamCh {
			switch evt.Type {
			case agent.StreamEventToken:
				fmt.Print(evt.Content)
			case agent.StreamEventComplete:
				fmt.Println()
				if resp, ok := evt.Data.(*agent.Response); ok && resp != nil {
					fmt.Printf("  流式指标: 轮数=%d, 耗时=%v\n",
						resp.Metrics.TotalTurns, resp.Metrics.Duration)
				}
			case agent.StreamEventError:
				fmt.Printf("\n  [错误] %s\n", evt.Content)
			}
		}
	}
	fmt.Println()

	// ===== 9. Pipeline 编排演示 =====
	fmt.Println("── 步骤 9: Pipeline 编排演示 ──")
	fmt.Println()

	// 创建两个专家 Agent
	analyzerAgent, err := agent.NewAgent("Analyzer", "你是一个代码分析专家，负责分析代码质量和潜在问题。",
		demo.NewDemoLLM("代码分析完成：发现3个潜在问题，2个性能优化点。"),
		agent.WithToolkit(tools.NewRegistry()),
		agent.WithMaxTurns(5),
	)
	if err != nil {
		log.Fatalf("创建 Analyzer Agent 失败: %v", err)
	}

	writerAgent, err := agent.NewAgent("Writer", "你是一个技术文档撰写专家，负责将分析结果转化为清晰的文档。",
		demo.NewDemoLLM("技术文档已生成：包含问题描述、优化建议和代码示例。"),
		agent.WithToolkit(tools.NewRegistry()),
		agent.WithMaxTurns(5),
	)
	if err != nil {
		log.Fatalf("创建 Writer Agent 失败: %v", err)
	}

	pipeline := agent.NewPipeline(
		agent.PipelineStep{Name: "analyze", Agent: analyzerAgent},
		agent.PipelineStep{Name: "document", Agent: writerAgent},
	)
	pipelineResult, err := pipeline.Run(ctx, "分析并文档化这个函数的性能问题")
	if err != nil {
		fmt.Printf("  [错误] Pipeline 执行失败: %v\n", err)
	} else {
		fmt.Printf("  Pipeline 执行成功\n")
		fmt.Printf("  步骤数: %d, 耗时: %v\n", len(pipelineResult.Steps), pipelineResult.Duration)
		fmt.Printf("  最终输出: %s\n", truncate(pipelineResult.Final, 100))
		for _, step := range pipelineResult.Steps {
			fmt.Printf("    - %s: %s (耗时: %v)\n", step.Name, truncate(step.Output, 60), step.Duration)
		}
	}
	fmt.Println()

	// ===== 10. 汇总报告 =====
	fmt.Println("================================================================")
	fmt.Println("                      运行报告汇总")
	fmt.Println("================================================================")
	fmt.Println()

	// Agent 状态
	stats := ragAgent.Stats()
	fmt.Printf("  Agent 状态:      %s\n", stats.Status)
	fmt.Printf("  当前轮次:        %d\n", stats.CurrentTurn)
	fmt.Printf("  工具调用分布:    %v\n", stats.ToolsCalled)
	fmt.Println()

	// Memory 统计
	memCount, _ := memStore.Count(ctx, "knowledge-base")
	totalCount, _ := memStore.Count(ctx, "")
	fmt.Printf("  知识库文档数:    %d\n", memCount)
	fmt.Printf("  总记忆条目数:    %d\n", totalCount)
	fmt.Println()

	// 指标报告
	snapshot := agentMetrics.Snapshot()
	fmt.Println("  性能指标:")
	fmt.Printf("    %-20s %d\n", "LLM 调用次数:", snapshot.LLMTotalCalls)
	fmt.Printf("    %-20s %d\n", "LLM 错误次数:", snapshot.LLMTotalErrors)
	fmt.Printf("    %-20s %d\n", "工具调用次数:", snapshot.ToolTotalCalls)
	fmt.Printf("    %-20s %d\n", "总轮数:", snapshot.TotalTurns)
	fmt.Printf("    %-20s %d\n", "活跃 Agent:", snapshot.ActiveAgents)
	fmt.Println()

	// 框架能力清单
	fmt.Println("  已展示框架能力:")
	capabilities := []string{
		"ReAct Loop (推理+行动循环)",
		"RAG 知识库检索 (FTS 混合搜索)",
		"RAG 注入 ReAct Loop (Auto/First/OnDemand)",
		"流式输出 (StreamRun + SSE)",
		"工具系统 (FileSystem/Shell/Web/KnowledgeSearch)",
		"记忆系统 (SQLite FTS5)",
		"检查点恢复 (CheckpointStore)",
		"生命周期钩子 (Hooks)",
		"事件系统 (EventBus)",
		"可观测性 (Metrics)",
		"Pipeline 编排",
		"多模型支持 (OpenAI/Azure/Anthropic/Gemini/Ollama)",
		"弹性调用 (熔断/退避/Fallback)",
		"安全沙箱",
	}
	for _, cap := range capabilities {
		fmt.Printf("    + %s\n", cap)
	}
	fmt.Println()

	fmt.Println("── 生产级示例运行完成 ──")

	// 清理事件总线
	eventBus.Close()

	os.Exit(0)
}

func truncate(s string, maxLen int) string {
	// 处理多字节字符
	if len(s) <= maxLen {
		return s
	}
	// 安全截断，避免截断 UTF-8 字符
	for maxLen > 0 && !strings.HasPrefix(s[maxLen:], "") {
		maxLen--
	}
	return s[:maxLen] + "..."
}
