package main

import (
	"context"
	"fmt"
	"log"
	"time"

	ap "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg"
)

// MockLLM 是示例用的模拟 LLM
type MockLLM struct{}

func (m *MockLLM) Complete(ctx context.Context, req *ap.CompletionRequest) (*ap.CompletionResponse, error) {
	return &ap.CompletionResponse{
		ID: "mock-1", Content: "代码审查完成，发现 3 个问题：1) 未处理错误 2) 缺少注释 3) 变量命名不规范。",
		Role: "assistant", Usage: ap.Usage{PromptTokens: 10, CompletionTokens: 20},
	}, nil
}
func (m *MockLLM) Stream(ctx context.Context, req *ap.CompletionRequest) (<-chan ap.Chunk, error) {
	ch := make(chan ap.Chunk, 1)
	go func() { defer close(ch); ch <- ap.Chunk{Content: "审查中...", Done: true} }()
	return ch, nil
}
func (m *MockLLM) CallTools(ctx context.Context, req *ap.ToolCallRequest) (*ap.ToolCallResponse, error) {
	return &ap.ToolCallResponse{Usage: ap.Usage{}}, nil
}
func (m *MockLLM) Info() ap.ModelInfo {
	return ap.ModelInfo{Name: "mock", Provider: "mock", MaxContext: 4096, SupportsTools: true}
}

// mockEventPublisher 事件发布器
type mockEventPublisher struct{}

func (m *mockEventPublisher) PublishAsync(eventType string, source string, payload any) error {
	fmt.Printf("[Event] %s\n", eventType)
	return nil
}

// mockMetricsRecorder 指标记录器
type mockMetricsRecorder struct{}

func (m *mockMetricsRecorder) RecordLLMCall(d time.Duration, err error)  {}
func (m *mockMetricsRecorder) RecordToolCall(d time.Duration, err error) {}
func (m *mockMetricsRecorder) RecordTurn(d time.Duration)                {}
func (m *mockMetricsRecorder) RecordTokenUsage(model string, pt, ct int) {}
func (m *mockMetricsRecorder) IncActiveAgents()                          {}
func (m *mockMetricsRecorder) DecActiveAgents()                          {}

func main() {
	fmt.Println("=== 链式 API：生产级 Agent ===")
	fmt.Println()

	// 创建工具集
	registry, err := ap.DefaultToolkit(ap.ToolkitConfig{
		RootDir:     ".",
		EnableFS:    true,
		EnableShell: true,
		EnableWeb:   false,
	})
	if err != nil {
		log.Fatalf("创建工具集失败: %v", err)
	}

	// 创建记忆存储
	mem, err := ap.WithInMemory()
	if err != nil {
		log.Fatalf("创建记忆存储失败: %v", err)
	}
	defer mem.Close()

	// 创建 Hook 管理器
	hooks := ap.NewHookManager()
	hooks.Register(ap.HookBeforeRun, func(ctx context.Context, hctx *ap.HookContext) error {
		fmt.Printf("[Hook] Agent %s 开始运行\n", hctx.AgentID)
		return nil
	})
	hooks.Register(ap.HookAfterRun, func(ctx context.Context, hctx *ap.HookContext) error {
		fmt.Printf("[Hook] Agent %s 运行完成\n", hctx.AgentID)
		return nil
	})

	// 创建生产级 Agent：一次性注入全部能力
	agent, err := ap.NewAgent("production-agent", "你是一个代码审查助手，负责检查代码质量并提出改进建议。", &MockLLM{},
		ap.WithMaxTurns(10),
		ap.WithSessionID("prod-session-001"),
		ap.WithToolkit(registry),
		ap.WithMemory(mem),
		ap.WithHooks(hooks),
		ap.WithEvents(&mockEventPublisher{}),
		ap.WithMetrics(&mockMetricsRecorder{}),
		ap.WithFileScope([]string{"./src", "./test"}),
	)
	if err != nil {
		log.Fatalf("创建 Agent 失败: %v", err)
	}

	// 运行
	resp, err := agent.Run(context.Background(), ap.UserMessage("审查 src/main.go 的代码质量"))
	if err != nil {
		log.Fatalf("运行失败: %v", err)
	}

	fmt.Printf("\n回复: %s\n", resp.Content)
	fmt.Printf("轮数: %d, 工具调用: %d\n", resp.Metrics.TotalTurns, resp.Metrics.TotalTools)

	// 验证所有能力
	fmt.Println("\n能力检查:")
	if _, ok := any(agent).(ap.MemoryCapable); ok {
		fmt.Println("  ✓ 记忆存储")
	}
	if _, ok := any(agent).(ap.HookCapable); ok {
		fmt.Println("  ✓ Hook 系统")
	}
	if _, ok := any(agent).(ap.EventCapable); ok {
		fmt.Println("  ✓ 事件发布")
	}
	if _, ok := any(agent).(ap.MetricsCapable); ok {
		fmt.Println("  ✓ 指标记录")
	}
	if _, ok := any(agent).(ap.FileScopeCapable); ok {
		fmt.Println("  ✓ 文件作用域")
	}
}
