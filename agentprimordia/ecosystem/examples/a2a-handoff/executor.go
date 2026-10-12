package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	ap "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg"
)

// demoExecutor 把开放协议任务桥接到 ap Agent（Demo 模式 provider）。
//
// 设计口径：
//   - 入口只读共享 task（ID/Messages 在 send 后无并发写者），执行结果构造
//     新 OpenTask 返回，由 OpenInteropServer.executeTask 在锁内拷回——
//     与框架 racingExecutor 回归测试（interop_server_race_test.go）同款模式，
//     避免 executor 在锁外写共享 task；
//   - pkg 公共面未导出 OpenArtifact 构造器（API 黄金快照冻结新增导出），
//     故任务结果经 Status.Message（已导出的 OpenMessage）回传，client 侧
//     经 TextContent() 取回——与 EchoTaskExecutor 的 Artifacts 通道并存；
//   - Demo Agent 非为并发执行设计，本 executor 用互斥锁串行化任务。
type demoExecutor struct {
	agent *ap.CapabilityAgent
	delay time.Duration
	mu    sync.Mutex
}

// newDemoExecutor 构造 Demo 模式任务执行器。
// provider 选择与 cmd/ap/scaffold/quickstart 一致：未设 AP_LLM_API_KEY 时
// 走关键词匹配 provider（零凭据）；设置后走真实 LLM（本 demo 不依赖）。
func newDemoExecutor() (*demoExecutor, error) {
	var provider ap.Provider = ap.NewDemoProvider()
	if os.Getenv("AP_LLM_API_KEY") != "" {
		cfg := ap.ConfigFromEnv("")
		if cfg.Model == "" {
			cfg.Model = "gpt-4o-mini"
		}
		if cfg.BaseURL == "" {
			cfg.BaseURL = "https://api.openai.com/v1"
		}
		realProvider, err := ap.NewOpenAIProvider(cfg)
		if err != nil {
			return nil, fmt.Errorf("创建 LLM provider 失败: %w", err)
		}
		provider = realProvider
	}

	agent, err := ap.NewAgent(
		demoAgentName,
		"你是 A2A 开放协议演示服务端的助手，用简洁的中文回答问题。",
		provider,
		ap.WithMaxTurns(6),
	)
	if err != nil {
		return nil, fmt.Errorf("创建 Agent 失败: %w", err)
	}

	return &demoExecutor{agent: agent, delay: execDelayFromEnv()}, nil
}

// execDelayFromEnv 读取模拟处理耗时（默认 400ms）。
// 作用：让 client 的轮询循环能观察到 working → completed 的状态迁移；
// 设为 0 时任务立即完成（仍可跑通，只是少了轮询观察窗口）。
func execDelayFromEnv() time.Duration {
	ms := 400
	if v := os.Getenv("AP_A2A_HANDOFF_EXEC_DELAY_MS"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil {
			fmt.Fprintf(os.Stderr, "警告: AP_A2A_HANDOFF_EXEC_DELAY_MS=%q 非法，使用默认 400ms\n", v)
		} else if parsed >= 0 {
			ms = parsed
		}
	}
	return time.Duration(ms) * time.Millisecond
}

// Execute 执行任务：提取 user 消息文本 → 调 ap Agent → 结果写入 Status.Message。
func (e *demoExecutor) Execute(ctx context.Context, task *ap.OpenTask) (*ap.OpenTask, error) {
	text := userTextOf(task)

	// 串行化：Demo Agent 不保证并发 Run 安全
	e.mu.Lock()
	defer e.mu.Unlock()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(e.delay):
	}

	resp, err := e.agent.Run(ctx, ap.UserMessage(text))
	if err != nil {
		return nil, fmt.Errorf("agent 执行失败: %w", err)
	}

	// 构造新任务返回（不写共享 task，锁内拷回由 server 完成）
	out := &ap.OpenTask{
		ID:        task.ID,
		ContextID: task.ContextID,
		Messages:  task.Messages,
	}
	out.Status.State = ap.OpenTaskCompleted
	out.Status.Timestamp = time.Now()
	resultMsg := ap.NewTextMessage("agent", resp.Content)
	out.Status.Message = &resultMsg
	out.Metadata = map[string]any{
		"agent_name": demoAgentName,
		"llm_mode":   llmMode(),
		"turns":      resp.Metrics.TotalTurns,
	}
	return out, nil
}

// Cancel 取消任务：Demo 执行器无真实取消点（执行即秒完），仅记录可达。
func (e *demoExecutor) Cancel(ctx context.Context, taskID string) error {
	fmt.Printf("[server] 收到取消请求: task=%s\n", taskID)
	return nil
}

// userTextOf 提取任务消息中的 user 文本（取第一条 user 消息的全部 text part）。
func userTextOf(task *ap.OpenTask) string {
	for _, msg := range task.Messages {
		if msg.Role != "user" {
			continue
		}
		text := msg.TextContent()
		if text != "" {
			return text
		}
	}
	return ""
}

// llmMode 返回当前 LLM 模式标识（写入任务 metadata，便于对账）。
func llmMode() string {
	if os.Getenv("AP_LLM_API_KEY") != "" {
		return "real-llm"
	}
	return "demo-keyword-matching"
}
