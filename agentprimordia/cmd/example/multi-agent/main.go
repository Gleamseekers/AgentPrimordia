package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/cmd/example/demo"
	"github.com/Gleamseekers/AgentPrimordia/internal/pool"
	"github.com/Gleamseekers/AgentPrimordia/internal/tools"
	"github.com/Gleamseekers/AgentPrimordia/internal/tools/builtin"
)

func main() {
	fmt.Println("=== AgentPrimordia Level 2: Multi-Agent 并发调度 ===")
	fmt.Println()

	demoLLM := demo.NewDemoLLM(
		"任务1完成: 已分析 main.go 文件结构。",
		"任务2完成: 已列出当前目录内容。",
		"任务3完成: 天气信息获取成功，今日晴朗。",
	)

	p := pool.NewPool(pool.PoolConfig{
		MaxConcurrency: 3,
		Timeout:        30 * time.Second,
		DefaultAgent: pool.ReActAgentConfig{
			SystemPrompt: "You are a helpful assistant. Complete tasks concisely.",
			MaxTurns:     5,
		},
	})
	defer p.Close()

	p.SetModel(demoLLM)

	toolRegistry := tools.NewRegistry()
	fsTool, err := builtin.NewFileSystem(".")
	if err != nil {
		log.Fatal(err)
	}
	_ = toolRegistry.Register(fsTool)
	p.SetToolkit(toolRegistry)

	var wg sync.WaitGroup
	eventCh := p.EventChannel()

	wg.Add(1)
	go func() {
		defer wg.Done()
		fmt.Println("[事件监听] 开始监听 Pool 事件...")
		for event := range eventCh {
			fmt.Printf("[事件] type=%s task_id=%s data=%v\n",
				event.Type, event.TaskID, event.Data)
		}
		fmt.Println("[事件监听] 事件通道已关闭")
	}()

	tasks := []pool.TaskConfig{
		{ID: "task-1", Title: "文件分析", Prompt: "分析文件 main.go"},
		{ID: "task-2", Title: "目录列表", Prompt: "列出当前目录"},
		{ID: "task-3", Title: "天气查询", Prompt: "获取天气信息"},
	}

	fmt.Printf("[调度] 提交 %d 个任务到 Pool (并发度: %d)\n", len(tasks), 3)
	fmt.Println()

	startTime := time.Now()
	results, err := p.Dispatch(context.Background(), tasks)
	if err != nil {
		log.Fatalf("Pool 调度失败: %v", err)
	}

	totalDuration := time.Since(startTime)

	fmt.Println()
	fmt.Println("=== 执行结果 ===")

	successCount := 0
	for i, r := range results {
		statusIcon := "✓"
		if r.Error != nil {
			statusIcon = "✗"
		} else {
			successCount++
		}

		fmt.Printf("[%d] 任务ID: %s\n", i+1, r.TaskID)
		fmt.Printf("    标题:   %s\n", r.Task.Title)
		fmt.Printf("    状态:   %s %s\n", r.Status, statusIcon)
		fmt.Printf("    耗时:   %v\n", r.Duration)
		if r.Response != nil {
			fmt.Printf("    回复:   %s\n", r.Response.Content)
			fmt.Printf("    轮数:   %d\n", r.Response.Metrics.TotalTurns)
		}
		if r.Error != nil {
			fmt.Printf("    错误:   %v\n", r.Error)
		}
		fmt.Println()
	}

	stats := p.Stats()
	fmt.Println("=== Pool 统计 ===")
	fmt.Printf("总任务数:     %d\n", stats.TotalTasks)
	fmt.Printf("成功完成:     %d\n", stats.CompletedTasks)
	fmt.Printf("失败数量:     %d\n", stats.FailedTasks)
	fmt.Printf("最大并发度:   %d\n", stats.MaxConcurrency)
	fmt.Printf("总耗时:       %v\n", totalDuration)
	fmt.Printf("DemoLLM 调用: %d 次\n", demoLLM.CallCount())

	p.Close()
	wg.Wait()

	fmt.Println()
	fmt.Println("--- Level 2 示例运行完成 ---")

	os.Exit(0)
}
