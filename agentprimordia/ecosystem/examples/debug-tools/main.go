package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/cmd/example/demo"
	ap "github.com/Gleamseekers/AgentPrimordia/pkg"
)

func main() {
	fmt.Println("🔧 调试可视化工具示例")
	fmt.Println("=" + string(make([]byte, 50)))
	fmt.Println()

	startDebugServer()
	runAgentWithDebugging()
	renderVisualizations()
}

func startDebugServer() {
	fmt.Println("🚀 启动调试 HTTP 服务器...")
	debugServer := ap.NewDebugServer(":8080")

	go func() {
		if err := debugServer.Start(); err != nil {
			log.Printf("⚠️  调试服务器启动失败: %v", err)
		}
	}()

	time.Sleep(1 * time.Second)
	fmt.Println("✅ 调试服务器已启动")
	fmt.Println("   📱 访问地址: http://localhost:8080")
	fmt.Println()
}

func runAgentWithDebugging() {
	fmt.Println("🤖 运行 Agent 并记录调试信息...")

	demoLLM := demo.NewDemoLLM(
		"这是一个测试响应，用于演示调试功能。",
	)

	testAgent, err := ap.NewAgent("DebugTestBot", "你是一个用于测试的 Agent", demoLLM,
		ap.WithMaxTurns(3),
	)
	if err != nil {
		log.Fatalf("❌ 创建 Agent 失败: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	debugServer := ap.NewDebugServer(":8080")

	debugServer.AddEvent("info", "Agent 创建成功")
	debugServer.AddEvent("info", fmt.Sprintf("Agent 名称: %s", testAgent.Name()))

	resp, err := testAgent.Run(ctx, ap.UserMessage("测试消息"))
	if err != nil {
		debugServer.AddEvent("error", fmt.Sprintf("Agent 运行失败: %v", err))
		log.Printf("❌ Agent 运行失败: %v", err)
	} else {
		debugServer.AddEvent("success", "Agent 运行成功")
		fmt.Printf("✅ Agent 回复: %s\n", resp.Content)
	}

	debugServer.AddEvent("info", fmt.Sprintf("总轮数: %d", resp.Metrics.TotalTurns))

	fmt.Println()
}

func renderVisualizations() {
	fmt.Println("📊 渲染可视化数据...")

	visualizer := ap.NewVisualizer()

	snapshot := &ap.MemorySnapshot{
		TotalEpisodes: 150,
		TopSessions: []ap.SessionInfo{
			{SessionID: "session-001", Count: 50},
			{SessionID: "session-002", Count: 30},
			{SessionID: "session-003", Count: 20},
		},
		RecentEvents: []ap.RecentEvent{
			{Time: "16:39:02", Role: "user", Content: "你好"},
			{Time: "16:39:03", Role: "assistant", Content: "你好！有什么可以帮助你的？"},
			{Time: "16:39:05", Role: "user", Content: "解释量子计算"},
		},
	}

	fmt.Println("\n📈 Memory 快照:")
	fmt.Println(visualizer.RenderMemorySnapshot(snapshot))

	lifecycleSteps := []ap.LifecycleStep{
		{State: "CREATED", Timestamp: time.Now(), Message: "Agent 实例创建"},
		{State: "STARTED", Timestamp: time.Now().Add(1 * time.Second), Message: "开始执行任务"},
		{State: "THINKING", Timestamp: time.Now().Add(2 * time.Second), Message: "正在分析用户输入..."},
		{State: "TOOL_CALL", Timestamp: time.Now().Add(3 * time.Second), Message: "调用工具: calculator.add"},
		{State: "OBSERVING", Timestamp: time.Now().Add(4 * time.Second), Message: "观察工具返回结果"},
		{State: "RESPONDING", Timestamp: time.Now().Add(5 * time.Second), Message: "生成最终回复"},
		{State: "COMPLETED", Timestamp: time.Now().Add(6 * time.Second), Message: "任务完成"},
	}

	fmt.Println("\n🔄 Agent 生命周期:")
	fmt.Println(visualizer.RenderAgentLifecycle(lifecycleSteps))

	fmt.Println("\n💡 调试技巧:")
	fmt.Println("   1. 打开浏览器访问 http://localhost:8080 查看实时界面")
	fmt.Println("   2. 使用 AddEvent() 记录关键事件")
	fmt.Println("   3. 使用 RenderMemorySnapshot() 查看 Memory 状态")
	fmt.Println("   4. 使用 RenderAgentLifecycle() 可视化执行流程")
	fmt.Println("   5. 所有数据都支持 JSON 格式导出")
}
