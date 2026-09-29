package main

import (
	"context"
	"fmt"
	"log"
	"time"

	ap "github.com/Gleamseekers/AgentPrimordia/pkg"
	"github.com/Gleamseekers/AgentPrimordia/testutil"
)

func main() {
	fmt.Println("=== AgentPrimordia: 多 Agent 调度示例 ===")
	fmt.Println()

	// 使用 LocalMessageBus 进行 Agent 间通信
	bus := ap.NewLocalMessageBus()
	bus.Register("coordinator", func(ctx context.Context, msg *ap.BusMessage) (*ap.BusMessage, error) {
		fmt.Printf("[协调器] 收到来自 %s 的消息: %s\n", msg.From, msg.Content)
		return &ap.BusMessage{
			ID:        "resp-coord",
			From:      "coordinator",
			Content:   "协调器已确认",
			Timestamp: time.Now(),
		}, nil
	})
	bus.Register("worker", func(ctx context.Context, msg *ap.BusMessage) (*ap.BusMessage, error) {
		fmt.Printf("[工作者] 收到来自 %s 的消息: %s\n", msg.From, msg.Content)
		return &ap.BusMessage{
			ID:        "resp-worker",
			From:      "worker",
			Content:   "工作者已处理",
			Timestamp: time.Now(),
		}, nil
	})

	// 演示 Agent 间消息传递
	resp, err := bus.Send(context.Background(), &ap.BusMessage{
		ID:      "msg-1",
		From:    "coordinator",
		To:      "worker",
		Type:    ap.BusMsgTaskRequest,
		Content: "请处理数据分析任务",
	})
	if err != nil {
		log.Fatalf("消息发送失败: %v", err)
	}
	fmt.Printf("消息回复: %s\n\n", resp.Content)

	// 使用 Session 分组管理任务
	pool := ap.NewPool(ap.PoolConfig{
		MaxConcurrency: 3,
		DefaultAgent: ap.ReActAgentConfig{
			SystemPrompt: "你是一个任务处理助手",
			MaxTurns:     3,
		},
	})
	defer pool.Close()

	mock := testutil.NewMockProvider()
	pool.SetModel(mock)

	tasks := []ap.TaskConfig{
		{ID: "task-1", Title: "数据分析", Prompt: "分析销售数据趋势", SessionID: "session-001"},
		{ID: "task-2", Title: "报告生成", Prompt: "生成月度报告", SessionID: "session-001"},
		{ID: "task-3", Title: "邮件撰写", Prompt: "撰写客户跟进邮件", SessionID: "session-002"},
	}

	results, err := pool.Dispatch(context.Background(), tasks)
	if err != nil {
		log.Fatalf("Pool 调度失败: %v", err)
	}

	for _, r := range results {
		status := "成功"
		if r.Error != nil {
			status = r.Error.Error()
		}
		fmt.Printf("任务 [%s] %s (session=%s): %s (耗时 %v)\n",
			r.TaskID, r.Task.Title, r.Task.SessionID, status, r.Duration)
	}

	stats := pool.Stats()
	fmt.Printf("\n统计: 完成=%d, 失败=%d, 运行中=%d\n",
		stats.CompletedTasks, stats.FailedTasks, stats.RunningTasks)
	fmt.Printf("消息总线注册 Agent: %v\n", bus.ListAgents())
}
