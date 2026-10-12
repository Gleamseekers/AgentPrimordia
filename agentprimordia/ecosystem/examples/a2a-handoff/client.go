package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	ap "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg"
)

// pollInterval 轮询间隔；pollTimeout 轮询总超时（防止对端异常时死等）。
const (
	pollInterval = 100 * time.Millisecond
	pollTimeout  = 30 * time.Second
)

// runClient 启动 client 进程：发现 → 委托 → 轮询至终态 → 取回结果 → 取消演示。
//
// 纪律（参照 ecosystem/examples/a2a-interop）：客户端方法失败均返回
// (nil, error)，本进程一律显式检查 err 后 return，禁止用 _ 丢弃错误再解构
// task——那会在对端异常时 nil panic。
func runClient() error {
	addr := envOr("AP_A2A_HANDOFF_ADDR", "127.0.0.1:9797")
	baseURL := "http://" + addr
	client := ap.NewOpenInteropClient(baseURL)
	ctx := context.Background()

	fmt.Println("=== A2A 跨进程委托 Demo（client 进程）===")
	fmt.Printf("目标 server: %s\n\n", baseURL)

	// 1. 发现：经开放协议拉取远端 Agent Card
	card, err := client.FetchAgentCard(ctx)
	if err != nil {
		return fmt.Errorf("发现 Agent Card 失败: %w", err)
	}
	fmt.Printf("[1/5] 发现 Agent Card: name=%s version=%s skills=%d\n",
		card.Name, card.Version, len(card.Skills))

	// 2. 委托：SendTask 返回 send 时刻快照（server 异步执行，故为 working）
	task, err := client.SendTask(ctx, ap.NewTextMessage("user", demoPrompt))
	if err != nil {
		return fmt.Errorf("委托任务失败: %w", err)
	}
	fmt.Printf("[2/5] 委托任务: id=%s state=%s\n", task.ID, task.Status.State)

	// 3. 轮询至终态（开放协议无客户端侧流式订阅面，轮询是规范姿势）
	final, polls, err := pollToTerminal(ctx, client, task.ID)
	if err != nil {
		return err
	}
	fmt.Printf("[3/5] 轮询至终态: state=%s（%d 次轮询）\n", final.Status.State, polls)

	// 4. 取回结果：结果经 Status.Message 回传（见 executor.go 说明）
	result := ""
	if final.Status.Message != nil {
		result = final.Status.Message.TextContent()
	}
	if result == "" {
		return fmt.Errorf("任务 %s 终态 %s 但未取回结果", task.ID, final.Status.State)
	}
	fmt.Printf("[4/5] 取回结果: %d 字符\n", len([]rune(result)))
	for _, line := range strings.Split(strings.TrimSpace(result), "\n") {
		fmt.Printf("      | %s\n", line)
	}

	// 5. 取消面演示：再委托一个任务，待其完成后调用 CancelTask，
	// 验证取消 RPC 可达且状态回读为 canceled（与 a2a-interop 参照范式一致）
	canceled, err := demoCancel(ctx, client)
	if err != nil {
		return err
	}
	fmt.Printf("[5/5] 取消演示: id=%s state=%s\n", canceled.ID, canceled.Status.State)

	// 协议符合性：基于在线发现到的真实 card 生成报告
	report := ap.GenerateInteropReport(*card, ap.DefaultInteropConfig())
	fmt.Printf("\n协议符合性: %.0f%%（%d/%d 项检查通过）\n",
		report.Score*100, len(report.Checks)-len(report.FailedChecks()), len(report.Checks))

	fmt.Println("\n=== A2A 跨进程委托 Demo 完成 ===")
	return nil
}

// pollToTerminal 轮询任务直至终态，返回终态快照与轮询次数。
func pollToTerminal(ctx context.Context, client *ap.OpenInteropClient, taskID string) (*ap.OpenTask, int, error) {
	deadline := time.Now().Add(pollTimeout)
	polls := 0
	for {
		got, err := client.GetTask(ctx, taskID)
		if err != nil {
			return nil, polls, fmt.Errorf("查询任务 %s 失败: %w", taskID, err)
		}
		polls++
		if got.Status.State.IsTerminal() {
			return got, polls, nil
		}
		if time.Now().After(deadline) {
			return nil, polls, fmt.Errorf("轮询超时（%s）：任务 %s 未达终态，最后 state=%s",
				pollTimeout, taskID, got.Status.State)
		}
		time.Sleep(pollInterval)
	}
}

// demoCancel 委托第二个任务并演示 CancelTask：待其完成后取消，回读 canceled。
// 口径说明：对已完成任务调用取消，验证的是取消 RPC 面可达与状态回读
// （a2a-interop 参照 demo 同款情节）；执行中取消会被执行完成后的状态写覆盖，
// 不具备可断言性，故不纳入演示。
func demoCancel(ctx context.Context, client *ap.OpenInteropClient) (*ap.OpenTask, error) {
	task, err := client.SendTask(ctx, ap.NewTextMessage("user", "这是一条将被取消的任务"))
	if err != nil {
		return nil, fmt.Errorf("委托任务失败: %w", err)
	}
	final, _, err := pollToTerminal(ctx, client, task.ID)
	if err != nil {
		return nil, err
	}
	if final.Status.State != ap.OpenTaskCompleted {
		return nil, fmt.Errorf("任务 %s 未完成（state=%s），取消演示中止", task.ID, final.Status.State)
	}
	if err := client.CancelTask(ctx, task.ID); err != nil {
		return nil, fmt.Errorf("取消任务 %s 失败: %w", task.ID, err)
	}
	canceled, err := client.GetTask(ctx, task.ID)
	if err != nil {
		return nil, fmt.Errorf("取消后查询失败: %w", err)
	}
	if canceled.Status.State != ap.OpenTaskCanceled {
		return nil, fmt.Errorf("取消后状态应为 %s，实际 %s", ap.OpenTaskCanceled, canceled.Status.State)
	}
	return canceled, nil
}
