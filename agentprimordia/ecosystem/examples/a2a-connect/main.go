// a2a-connect 示例演示两个独立 agent 通过 A2A 开放协议进行通信。
//
// 运行方式（从 monorepo 根目录）:
//   go run ./ecosystem/examples/a2a-connect/
//
// 本示例：
//   1. 启动 agent-a（数学计算服务），暴露 A2A 端点
//   2. 启动 agent-b（编排者），通过 A2A 协议发现并调用 agent-a
//   3. 展示完整的任务创建、执行、结果获取流程
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	ap "agentprimordia/pkg"
)

func main() {
	fmt.Println("A2A Connect — 两个 Agent 通过开放协议通信")
	fmt.Println("==========================================")
	fmt.Println()

	// 步骤 1: 创建 agent-a（数学计算服务）
	fmt.Println("[Agent A] 启动数学计算服务...")
	cardA := ap.OpenAgentCard{
		Name:        "math-agent",
		Description: "提供数学计算能力的 Agent",
		URL:         "http://localhost:9100",
		Version:     "1.0.0",
		Capabilities: ap.OpenCapabilities{
			Streaming: false,
		},
		Skills: []ap.OpenSkillDecl{
			{
				ID:          "math-calc",
				Name:        "数学计算",
				Description: "执行基本数学运算",
				Tags:        []string{"math", "calculation"},
			},
		},
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
	}

	cfgA := ap.DefaultInteropConfig()
	serverA := ap.NewOpenInteropServer(cardA, cfgA)
	serverA.WithExecutor(ap.NewEchoTaskExecutor())

	httpServerA := &http.Server{
		Addr:    ":9100",
		Handler: serverA.Handler(),
	}
	go func() {
		if err := httpServerA.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Agent A server error: %v", err)
		}
	}()
	fmt.Println("[Agent A] 监听 :9100")
	fmt.Println()

	// 等待服务启动
	time.Sleep(100 * time.Millisecond)

	// 步骤 2: agent-b 发现 agent-a
	fmt.Println("[Agent B] 发现 Agent A...")
	clientB := ap.NewOpenInteropClient("http://localhost:9100")

	ctx := context.Background()
	discoveredCard, err := clientB.FetchAgentCard(ctx)
	if err != nil {
		log.Fatalf("发现 Agent A 失败: %v", err)
	}
	fmt.Printf("[Agent B] 发现: %s (%s)\n", discoveredCard.Name, discoveredCard.Description)
	fmt.Printf("[Agent B] 技能: ")
	for i, skill := range discoveredCard.Skills {
		if i > 0 {
			fmt.Print(", ")
		}
		fmt.Printf("%s", skill.Name)
	}
	fmt.Println()
	fmt.Println()

	// 步骤 3: agent-b 向 agent-a 发送任务
	fmt.Println("[Agent B] 发送计算任务...")
	msg := ap.NewTextMessage("user", "计算 2 + 3 * 4")
	task, err := clientB.SendTask(ctx, msg)
	if err != nil {
		log.Fatalf("发送任务失败: %v", err)
	}
	fmt.Printf("[Agent B] 任务已创建: %s (状态: %s)\n", task.ID, task.Status.State)

	// 步骤 4: 轮询任务状态
	fmt.Println("[Agent B] 等待任务完成...")
	for i := 0; i < 10; i++ {
		time.Sleep(200 * time.Millisecond)
		updatedTask, err := clientB.GetTask(ctx, task.ID)
		if err != nil {
			log.Printf("查询任务状态失败: %v", err)
			continue
		}
		fmt.Printf("[Agent B] 任务状态: %s\n", updatedTask.Status.State)

		if updatedTask.Status.State == ap.OpenTaskCompleted {
			fmt.Println()
			fmt.Println("[Agent B] 任务完成! 结果:")
			for _, artifact := range updatedTask.Artifacts {
				for _, part := range artifact.Parts {
					if part.Type == "text" {
						fmt.Printf("  %s\n", part.Text)
					}
				}
			}
			break
		}
	}

	fmt.Println()

	// 步骤 5: 生成互操作报告
	fmt.Println("[Agent B] 生成互操作合规报告...")
	report := ap.GenerateInteropReport(*discoveredCard, cfgA)
	fmt.Printf("[Agent B] 合规评分: %.0f%%\n", report.Score*100)
	for _, check := range report.Checks {
		status := "PASS"
		if !check.Passed {
			status = "FAIL"
		}
		fmt.Printf("  [%s] %s\n", status, check.Name)
	}

	fmt.Println()
	fmt.Println("A2A 通信演示完成!")

	// 清理
	ctxShutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = httpServerA.Shutdown(ctxShutdown)
}
