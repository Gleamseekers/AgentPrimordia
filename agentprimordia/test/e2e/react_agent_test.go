// Package e2e 提供 AgentPrimordia 框架的端到端集成测试。
//
// 这些测试验证完整的用户场景：从创建 Agent 到获得最终响应，
// 包括工具调用、记忆持久化、多 Agent 编排、RAG 管道等。
//
// 所有测试使用 testutil.MockProvider 模拟 LLM，不需要真实 API 密钥。
// 每个测试应在 5 秒内完成。
package e2e

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/memory"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/tools"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/testutil"
)

// TestE2E_ReActAgent_SimpleConversation 验证最基本的 Agent 对话流程：
// 用户输入 → LLM 推理 → 返回响应
func TestE2E_ReActAgent_SimpleConversation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a := testutil.NewTestAgent(testutil.TestAgentConfig{
		Name:      "echo-bot",
		Responses: []string{"Hello! I am echo-bot. How can I help you?"},
	})

	resp, err := a.Run(ctx, agent.UserMessage("Hi there"))
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if resp.Content != "Hello! I am echo-bot. How can I help you?" {
		t.Errorf("unexpected content: %q", resp.Content)
	}
}

// TestE2E_ReActAgent_WithToolCall 验证 Agent 工具调用完整流程：
// 用户输入 → LLM 决定调用工具 → 执行工具 → LLM 生成最终响应
func TestE2E_ReActAgent_WithToolCall(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	registry := tools.NewRegistry()
	_ = registry.Register(&echoTool{})

	// 第一轮返回工具调用，第二轮返回最终答案
	mock := testutil.NewMockProvider("The echo tool said: hello world")
	mock.WithToolCalls(llm.FunctionCall{
		Name:      "echo",
		Arguments: `{"message":"hello world"}`,
	})

	a, err := agent.NewAgent("tool-bot", "you are a helpful assistant.", mock,
		agent.WithMaxTurns(5),
		agent.WithToolkit(registry),
	)
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}

	resp, err := a.Run(ctx, agent.UserMessage("Please echo 'hello world'"))
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if resp.Content == "" {
		t.Fatal("expected non-empty response after tool call")
	}
}

// TestE2E_ReActAgent_WithMemory 验证 Agent + 记忆持久化：
// 对话内容被保存到 Memory Store
func TestE2E_ReActAgent_WithMemory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mem := memory.NewInMemoryStore()

	mock := testutil.NewMockProvider("I will remember this.", "Yes, I recall.")
	a, err := agent.NewAgent("memory-bot", "you are a helpful assistant.", mock,
		agent.WithMaxTurns(5),
		agent.WithMemory(mem),
	)
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}

	resp, err := a.Run(ctx, agent.UserMessage("Remember: the sky is blue"))
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if resp.Content == "" {
		t.Fatal("expected non-empty response")
	}

	// 验证记忆中有数据
	count, err := mem.Count(ctx, "")
	if err != nil {
		t.Fatalf("mem.Count error: %v", err)
	}
	if count == 0 {
		t.Error("expected memory to contain episodes after conversation")
	}
}

// TestE2E_ReActAgent_MultiTurn 验证多轮对话
func TestE2E_ReActAgent_MultiTurn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a := testutil.NewTestAgent(testutil.TestAgentConfig{
		Name: "multi-turn-bot",
		Responses: []string{
			"First response",
			"Second response",
			"Third response",
		},
	})

	expected := []string{"First response", "Second response", "Third response"}
	for i, want := range expected {
		resp, err := a.Run(ctx, agent.UserMessage("message"))
		if err != nil {
			t.Fatalf("Run() turn %d error: %v", i+1, err)
		}
		if resp.Content != want {
			t.Errorf("turn %d: got %q, want %q", i+1, resp.Content, want)
		}
	}
}

// TestE2E_ReActAgent_ContextCancellation 验证 ctx 取消能正确终止 Agent
func TestE2E_ReActAgent_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	mock := testutil.NewMockProvider("slow response").WithDelay(10 * time.Second)
	a, err := agent.NewAgent("slow-bot", "test", mock, agent.WithMaxTurns(5))
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}

	cancel() // 立即取消

	_, err = a.Run(ctx, agent.UserMessage("hello"))
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

// TestE2E_ReActAgent_StreamRun 验证流式输出
func TestE2E_ReActAgent_StreamRun(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a := testutil.NewTestAgent(testutil.TestAgentConfig{
		Name:      "stream-bot",
		Responses: []string{"streaming response content"},
	})

	ch, err := a.StreamRun(ctx, agent.UserMessage("stream please"))
	if err != nil {
		t.Fatalf("StreamRun() error: %v", err)
	}

	var gotContent string
	for ev := range ch {
		if ev.Content != "" {
			gotContent += ev.Content
		}
	}
	if gotContent == "" {
		t.Error("expected non-empty streamed content")
	}
}

// TestE2E_ReActAgent_ErrorPropagation 验证 LLM 错误正确传播
func TestE2E_ReActAgent_ErrorPropagation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mock := testutil.NewMockProvider().WithError(context.DeadlineExceeded)
	a, err := agent.NewAgent("error-bot", "test", mock, agent.WithMaxTurns(3))
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}

	_, err = a.Run(ctx, agent.UserMessage("hello"))
	if err == nil {
		t.Fatal("expected error from LLM failure")
	}
}

// === 辅助工具 ===

// echoTool 是一个简单的回显工具
type echoTool struct{}

func (t *echoTool) Name() string        { return "echo" }
func (t *echoTool) Description() string { return "Echoes the input message" }
func (t *echoTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"message":{"type":"string","description":"message to echo"}},"required":["message"]}`)
}

func (t *echoTool) Execute(_ context.Context, args json.RawMessage) (*tools.Result, error) {
	var params struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}
	return tools.NewResult("echo: " + params.Message), nil
}
