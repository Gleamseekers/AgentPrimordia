package ap_test

import (
	"context"
	"fmt"

	ap "github.com/Gleamseekers/AgentPrimordia/pkg"
	"github.com/Gleamseekers/AgentPrimordia/testutil"
)

// ExampleNewAgent 演示创建一个基本的 ReAct Agent 并运行对话。
func ExampleNewAgent() {
	// 使用 MockProvider 模拟 LLM（生产环境替换为真实 Provider）
	mock := testutil.NewMockProvider("Hello! I am your AI assistant.")

	agent, err := ap.NewAgent("demo-bot", "You are a helpful assistant.", mock,
		ap.WithMaxTurns(5),
	)
	if err != nil {
		panic(err)
	}

	resp, err := agent.Run(context.Background(), ap.UserMessage("Hi!"))
	if err != nil {
		panic(err)
	}

	fmt.Println(resp.Content)
	// Output: Hello! I am your AI assistant.
}

// ExampleNewAgent_withTools 演示创建带工具调用能力的 Agent。
func ExampleNewAgent_withTools() {
	mock := testutil.NewMockProvider("I found the file contents for you.")
	registry := ap.NewToolRegistry()

	agent, err := ap.NewAgent("tool-bot", "You can use tools.", mock,
		ap.WithMaxTurns(5),
		ap.WithToolkit(registry),
	)
	if err != nil {
		panic(err)
	}

	resp, err := agent.Run(context.Background(), ap.UserMessage("Read the file"))
	if err != nil {
		panic(err)
	}

	fmt.Println(resp.Content)
	// Output: I found the file contents for you.
}

// ExampleNewAgent_withMemory 演示创建带记忆持久化的 Agent。
func ExampleNewAgent_withMemory() {
	mock := testutil.NewMockProvider("I will remember that.")
	mem := ap.NewInMemoryStore()

	agent, err := ap.NewAgent("memory-bot", "You have memory.", mock,
		ap.WithMemory(mem),
	)
	if err != nil {
		panic(err)
	}

	resp, err := agent.Run(context.Background(), ap.UserMessage("Remember: Go is great"))
	if err != nil {
		panic(err)
	}

	fmt.Println(resp.Content)
	// Output: I will remember that.
}

// ExampleCapabilityAgent_StreamRun 演示流式输出。
func ExampleCapabilityAgent_StreamRun() {
	mock := testutil.NewMockProvider("streaming response")
	agent := testutil.NewTestAgent(testutil.TestAgentConfig{
		Name:      "stream-bot",
		Responses: []string{"streaming response"},
	})
	_ = mock

	ch, err := agent.StreamRun(context.Background(), ap.UserMessage("stream"))
	if err != nil {
		panic(err)
	}

	var content string
	for event := range ch {
		content += event.Content
	}
	fmt.Println(len(content) > 0)
	// Output: true
}

// ExampleGetErrorCode 演示从错误中提取结构化错误码。
func ExampleGetErrorCode() {
	code := ap.GetErrorCode(ap.ErrToolNotFound)
	fmt.Println(code)
	// Output: TOOL_001
}

// ExampleIsRetryable 演示判断错误是否可重试。
func ExampleIsRetryable() {
	fmt.Println(ap.IsRetryable(ap.ErrProviderTimeout))
	fmt.Println(ap.IsRetryable(ap.ErrToolNotFound))
	// Output:
	// true
	// false
}
