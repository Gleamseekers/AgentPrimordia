package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools"
)

// silentToolLogs 把 tools 包全局 slog 重定向到 io.Discard，benchmark 结束后恢复。
//
// Phase 4 Task 10 起，工具执行器已统一使用 slog，不再有 log.Printf 输出。
// 此函数保留是为了在 benchmark 期间仍然抑制 slog（信息级别 + 调试级别），
// 避免大量 IO 拖慢结果。
func silentToolLogs(b *testing.B) func() {
	b.Helper()
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	return func() { slog.SetDefault(prev) }
}

// benchMockTool 用于基准测试的轻量 Mock 工具
type benchMockTool struct {
	name string
}

func (m *benchMockTool) Name() string        { return m.name }
func (m *benchMockTool) Description() string { return "bench mock tool" }
func (m *benchMockTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"required":[]}`)
}
func (m *benchMockTool) Execute(_ context.Context, _ json.RawMessage) (*tools.Result, error) {
	return tools.NewResult("ok"), nil
}

// BenchmarkReActAgent_SimpleCompletion 测试无工具的单轮完成性能
func BenchmarkReActAgent_SimpleCompletion(b *testing.B) {
	defer silentToolLogs(b)()
	b.ReportAllocs()

	mockLLM := llm.NewMockLLM(nil).WithResponse("done")

	agent, err := NewAgent("bench-simple", "", mockLLM, WithMaxTurns(10))
	if err != nil {
		b.Fatal(err)
	}

	ctx := context.Background()
	input := UserMessage("hello")

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := agent.Run(ctx, input)
		if err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
		// 重置 Lifecycle 以支持重复 Run
		_ = agent.Inner().lifecycle.SetStatus(StatusIdle)
	}
}

// BenchmarkReActAgent_SingleToolCall 测试单次工具调用场景性能
func BenchmarkReActAgent_SingleToolCall(b *testing.B) {
	defer silentToolLogs(b)()
	b.ReportAllocs()

	registry := tools.NewRegistry()
	_ = registry.Register(&benchMockTool{name: "bench_tool"})

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// 每次 iteration 创建新的 MockLLM，因为响应队列会被消耗
		mockLLM := llm.NewMockLLM(nil).
			WithToolResponse([]llm.FunctionCall{
				{ID: "call_1", Name: "bench_tool", Arguments: "{}"},
			}).
			WithResponse("task done")

		agent, err := NewAgent("bench-tool", "", mockLLM, WithMaxTurns(10))
		if err != nil {
			b.Fatal(err)
		}
		agent = agent.WithToolkit(registry)

		_, err = agent.Run(context.Background(), UserMessage("use tool"))
		if err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}

// BenchmarkReActAgent_MaxTurns 测试多轮次运行性能（5 轮工具调用循环）
func BenchmarkReActAgent_MaxTurns(b *testing.B) {
	defer silentToolLogs(b)()
	b.ReportAllocs()

	registry := tools.NewRegistry()
	_ = registry.Register(&benchMockTool{name: "loop_tool"})

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		mockLLM := llm.NewMockLLM(nil)
		for j := 0; j < 4; j++ {
			mockLLM.WithToolResponse([]llm.FunctionCall{
				{ID: "call_x", Name: "loop_tool", Arguments: "{}"},
			})
		}
		mockLLM.WithResponse("finally done")

		agent, err := NewAgent("bench-maxturns", "", mockLLM, WithMaxTurns(10))
		if err != nil {
			b.Fatal(err)
		}
		agent = agent.WithToolkit(registry)

		_, err = agent.Run(context.Background(), UserMessage("multi-turn"))
		if err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}

// BenchmarkConvertToLLMMessages 测试消息转换的内存分配
func BenchmarkConvertToLLMMessages(b *testing.B) {
	history := []Message{
		{Role: RoleSystem, Content: "You are a helpful assistant."},
		{Role: RoleUser, Content: "Hello"},
		{Role: RoleAssistant, Content: "Hi", ToolCalls: []ToolCall{
			{ID: "call_1", Name: "search", Args: `{"q":"go"}`},
		}},
		{Role: RoleTool, Content: "results", Metadata: Metadata{Extra: map[string]string{"tool_call_id": "call_1"}}},
		{Role: RoleAssistant, Content: "Done"},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = convertToLLMMessages(history)
	}
}
