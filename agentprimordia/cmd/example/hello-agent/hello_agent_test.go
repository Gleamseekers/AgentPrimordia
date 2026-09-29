package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/cmd/example/demo"
	"github.com/Gleamseekers/AgentPrimordia/internal/agent"
	"github.com/Gleamseekers/AgentPrimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/internal/memory"
	"github.com/Gleamseekers/AgentPrimordia/internal/pool"
	"github.com/Gleamseekers/AgentPrimordia/internal/tools"
	"github.com/Gleamseekers/AgentPrimordia/internal/tools/builtin"
)

func TestHelloAgent_BasicRun(t *testing.T) {
	demoLLM := demo.NewDemoLLM("Hello! How can I help you today?")

	a, err := agent.NewAgent("TestHelloAgent", "You are a helpful assistant.", demoLLM, agent.WithMaxTurns(5))
	if err != nil {
		t.Fatalf("创建 Agent 失败: %v", err)
	}

	resp, err := a.Run(context.Background(), agent.UserMessage("Hello!"))
	if err != nil {
		t.Fatalf("BasicRun 失败: %v", err)
	}

	if resp.Content == "" {
		t.Fatal("响应内容不应为空")
	}

	if resp.Content != "Hello! How can I help you today?" {
		t.Errorf("期望响应 'Hello! How can I help you today?'，实际得到 '%s'", resp.Content)
	}

	if resp.Metrics.TotalTurns != 1 {
		t.Errorf("期望 1 轮，实际 %d 轮", resp.Metrics.TotalTurns)
	}

	stats := a.Stats()
	if stats.Status != agent.StatusCompleted {
		t.Errorf("期望状态 completed，实际 %s", stats.Status)
	}

	if demoLLM.CallCount() != 1 {
		t.Errorf("期望 LLM 调用 1 次，实际 %d 次", demoLLM.CallCount())
	}
}

func TestHelloAgent_WithTools(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	_ = os.WriteFile(testFile, []byte("hello from test file"), 0644)

	demoLLM := demo.NewDemoLLM(
		"文件内容已读取完成：hello from test file",
	)

	demoLLM.WithToolCalls(llm.FunctionCall{
		ID:        "call_1",
		Name:      "filesystem",
		Arguments: `{"action":"read","path":"test.txt"}`,
	})

	toolRegistry := tools.NewRegistry()
	fs, err := builtin.NewFileSystem(tmpDir)
	if err != nil {
		t.Fatalf("NewFileSystem error: %v", err)
	}
	_ = toolRegistry.Register(fs)

	a, err := agent.NewAgent("TestToolAgent", "You are a file reading assistant.", demoLLM,
		agent.WithMaxTurns(10),
		agent.WithToolkit(toolRegistry),
	)
	if err != nil {
		t.Fatalf("创建 Agent 失败: %v", err)
	}

	resp, err := a.Run(context.Background(), agent.UserMessage("读取 test.txt 的内容"))
	if err != nil {
		t.Fatalf("WithTools 运行失败: %v", err)
	}

	if resp.Content == "" {
		t.Fatal("工具调用后响应不应为空")
	}

	t.Logf("工具调用后响应: %s", resp.Content)
	t.Logf("总轮数: %d", resp.Metrics.TotalTurns)
	t.Logf("工具调用次数: %d", resp.Metrics.TotalTools)
	t.Logf("LLM 调用次数: %d", demoLLM.CallCount())
}

func TestHelloAgent_MultiTurn(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "data.json")
	_ = os.WriteFile(testFile, []byte(`{"name":"test","value":42}`), 0644)

	multiLLM := demo.NewDemoLLM(
		"我需要先读取文件来了解数据结构",
		"文件内容为 JSON 格式，包含 name 和 value 字段。分析完成。",
	)

	multiLLM.WithToolCalls(llm.FunctionCall{
		ID:        "call_1",
		Name:      "filesystem",
		Arguments: `{"action":"read","path":"data.json"}`,
	})

	toolRegistry := tools.NewRegistry()
	fs, err := builtin.NewFileSystem(tmpDir)
	if err != nil {
		t.Fatalf("NewFileSystem error: %v", err)
	}
	_ = toolRegistry.Register(fs)

	a, err := agent.NewAgent("TestMultiTurnAgent", "You are a data analysis assistant. Read files before answering.", multiLLM,
		agent.WithMaxTurns(10),
		agent.WithToolkit(toolRegistry),
	)
	if err != nil {
		t.Fatalf("创建 Agent 失败: %v", err)
	}

	resp, err := a.Run(context.Background(), agent.UserMessage("分析 data.json 文件"))
	if err != nil {
		t.Fatalf("MultiTurn 运行失败: %v", err)
	}

	if resp.Content == "" {
		t.Fatal("多轮对话最终响应不应为空")
	}

	if resp.Metrics.TotalTurns < 2 {
		t.Errorf("多轮对话应至少 2 轮，实际 %d 轮", resp.Metrics.TotalTurns)
	}

	if demoLLM := multiLLM; demoLLM.CallCount() < 2 {
		t.Errorf("多轮对话应至少 2 次 LLM 调用，实际 %d 次", demoLLM.CallCount())
	}

	t.Logf("多轮对话完成，总轮数: %d, 工具调用: %d",
		resp.Metrics.TotalTurns, resp.Metrics.TotalTools)
}

func TestMultiAgent_PoolDispatch(t *testing.T) {
	poolLLM := demo.NewDemoLLM(
		"任务A 完成",
		"任务B 完成",
		"任务C 完成",
	)

	p := pool.NewPool(pool.PoolConfig{
		MaxConcurrency: 3,
		Timeout:        30 * time.Second,
		DefaultAgent: pool.ReActAgentConfig{
			SystemPrompt: "Complete tasks concisely.",
			MaxTurns:     3,
		},
	})
	defer p.Close()

	p.SetModel(poolLLM)

	tasks := []pool.TaskConfig{
		{ID: "task-a", Title: "任务A", Prompt: "执行任务 A"},
		{ID: "task-b", Title: "任务B", Prompt: "执行任务 B"},
		{ID: "task-c", Title: "任务C", Prompt: "执行任务 C"},
	}

	results, err := p.Dispatch(context.Background(), tasks)
	if err != nil {
		t.Fatalf("Pool Dispatch 失败: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("期望 3 个结果，实际得到 %d 个", len(results))
	}

	successCount := 0
	for i, r := range results {
		if r.TaskID == "" {
			t.Errorf("结果[%d] TaskID 不应为空", i)
		}
		if r.Error != nil {
			t.Errorf("结果[%d] 出错: %v", i, r.Error)
		} else {
			successCount++
		}
		if r.Response == nil {
			t.Errorf("结果[%d] Response 不应为 nil", i)
		} else if r.Response.Content == "" {
			t.Errorf("结果[%d] 响应内容不应为空", i)
		}
	}

	if successCount != 3 {
		t.Errorf("期望 3 个任务全部成功，实际成功 %d 个", successCount)
	}

	stats := p.Stats()
	if stats.CompletedTasks != 3 {
		t.Errorf("Pool 统计显示完成数 %d，期望 3", stats.CompletedTasks)
	}

	if poolLLM.CallCount() != 3 {
		t.Errorf("期望 LLM 被调用 3 次（每任务1次），实际 %d 次", poolLLM.CallCount())
	}

	for _, r := range results {
		t.Logf("[%s] status=%s content=%s duration=%v",
			r.TaskID, r.Status, r.Response.Content, r.Duration)
	}
}

func TestProduction_FullWorkflow(t *testing.T) {
	tmpDir := t.TempDir()

	memStore, err := memory.WithInMemory()
	if err != nil {
		t.Fatalf("Memory 初始化失败: %v", err)
	}
	defer memStore.Close()

	var hookTurns []int
	var hookTools []string
	var hookCompleted bool

	prodLLM := demo.NewDemoLLM(
		"重构建议已完成，请查看详细方案。",
	)

	toolRegistry := tools.NewRegistry()
	fs, err := builtin.NewFileSystem(tmpDir)
	if err != nil {
		t.Fatalf("NewFileSystem error: %v", err)
	}
	_ = toolRegistry.Register(fs)
	_ = toolRegistry.Register(builtin.NewShell())
	_ = toolRegistry.Register(builtin.NewWeb())

	hooks := agent.NewHookManager()
	hooks.Register(agent.HookAfterTurn, func(ctx context.Context, hctx *agent.HookContext) error {
		hookTurns = append(hookTurns, hctx.Turn)
		return nil
	})
	hooks.Register(agent.HookBeforeTool, func(ctx context.Context, hctx *agent.HookContext) error {
		if hctx.ToolCall != nil {
			hookTools = append(hookTools, hctx.ToolCall.Name)
		}
		return nil
	})
	hooks.Register(agent.HookOnComplete, func(ctx context.Context, hctx *agent.HookContext) error {
		hookCompleted = true
		return nil
	})

	prodAgent, err := agent.NewAgent("FullWorkflowAgent", "You are a senior coding assistant.", prodLLM,
		agent.WithMaxTurns(20),
		agent.WithTemperature(0.7),
		agent.WithToolkit(toolRegistry),
		agent.WithMemory(memStore),
		agent.WithHooks(hooks),
	)
	if err != nil {
		t.Fatalf("创建 Agent 失败: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := prodAgent.Run(ctx, agent.UserMessage("帮我重构这个函数"))
	if err != nil {
		t.Fatalf("FullWorkflow 运行失败: %v", err)
	}

	if resp.Content == "" {
		t.Fatal("完整工作流响应不应为空")
	}

	// 检查 Agent 自身状态（lifecycle 是独立创建的，不关联 Agent）
	stats := prodAgent.Stats()
	if stats.Status != agent.StatusCompleted {
		t.Errorf("Agent 最终状态应为 completed，实际 %s", stats.Status)
	}

	if !hookCompleted {
		t.Error("OnComplete Hook 应该被触发")
	}

	count, _ := memStore.Count(ctx, "")
	t.Logf("Memory 记录数: %d", count)

	t.Logf("Agent 状态: %s", stats.Status)
	t.Logf("当前轮次: %d", stats.CurrentTurn)
	t.Logf("消息总数: %d", stats.TotalMessages)
	t.Logf("工具分布: %v", stats.ToolsCalled)

	t.Logf("Metrics: turns=%d tools=%d duration=%v llm_latency=%v tool_latency=%v",
		resp.Metrics.TotalTurns,
		resp.Metrics.TotalTools,
		resp.Metrics.Duration,
		resp.Metrics.LLMLatency,
		resp.Metrics.ToolLatency,
	)

	t.Logf("Hook 触发记录: turns=%v tools=%v completed=%v",
		hookTurns, hookTools, hookCompleted)
}
