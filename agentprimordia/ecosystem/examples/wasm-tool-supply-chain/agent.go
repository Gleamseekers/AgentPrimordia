// agent.go — 零凭据脚本化 provider + ReActAgent 运行时调用 WASM 工具。
//
// 口径说明（诚实边界，务必读）：
//
//   - 框架自带的 DemoProvider（internal/llm/demo_provider.go）在 CallTools 里
//     只返回「工具调用已模拟」的提示文本，并不真正发起工具调用。若直接用它，
//     本 demo 的「agent 调用 WASM 工具」一步就只剩文案、没有真实执行。
//   - 因此这里实现一个零凭据的脚本化 provider：固定发起一次指定工具的调用，
//     随后收尾。它不发任何网络请求、不需要 API key，与 DemoProvider 同属
//     「无外部 LLM 依赖」的演示路径，只是把「模型决定调用哪个工具」这一步
//     写成确定性的，便于 CI 复跑与断言。
//   - 工具执行本身走的是框架生产路径：tools.Registry → tools.Tool.Execute →
//     WASMToolAdapter.ExecuteTool → Sandbox.ExecuteWithMemory（wazero）。
//     这与 ReActAgent 运行时的工具调用是同一代码路径
//     （对照 wasm/runtime_e2e_test.go TestWASM_ToolRegistryIntegration）。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	ap "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg"
)

// scriptedProvider 零凭据脚本化 provider：首轮发起一次 WASM 工具调用，
// 之后轮次返回纯文本收尾。
type scriptedProvider struct {
	toolName string
	toolArgs json.RawMessage

	mu    sync.Mutex
	calls int
}

// 编译期契约：scriptedProvider 必须满足 pkg 公共 Provider 接口。
var _ ap.Provider = (*scriptedProvider)(nil)

// newScriptedProvider 构造脚本化 provider。
func newScriptedProvider(toolName string, toolArgs json.RawMessage) *scriptedProvider {
	return &scriptedProvider{toolName: toolName, toolArgs: toolArgs}
}

// Complete 纯文本补全（收尾轮次走这里；本 demo 的收尾内容确定）。
func (p *scriptedProvider) Complete(_ context.Context, _ *ap.CompletionRequest) (*ap.CompletionResponse, error) {
	return &ap.CompletionResponse{
		Content: "已完成 WASM 工具调用，结果取自 wazero 沙箱执行输出。",
		Role:    "assistant",
	}, nil
}

// Stream 流式补全：单 chunk 返回（本 demo 不走流式，仅为满足接口）。
func (p *scriptedProvider) Stream(_ context.Context, _ *ap.CompletionRequest) (<-chan ap.Chunk, error) {
	ch := make(chan ap.Chunk, 1)
	ch <- ap.Chunk{Content: "已完成 WASM 工具调用，结果取自 wazero 沙箱执行输出。", Done: true}
	close(ch)
	return ch, nil
}

// CallTools 工具调用：首次返回一次 WASM 工具调用，其后返回空调用集收尾。
func (p *scriptedProvider) CallTools(_ context.Context, _ *ap.ToolCallRequest) (*ap.ToolCallResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++

	if p.calls == 1 {
		return &ap.ToolCallResponse{
			ToolCalls: []ap.FunctionCall{{
				ID:        "wasm-call-1",
				Name:      p.toolName,
				Arguments: string(p.toolArgs),
			}},
		}, nil
	}
	return &ap.ToolCallResponse{Content: "已完成 WASM 工具调用，结果取自 wazero 沙箱执行输出。"}, nil
}

// Info 模型信息（名称含 demo 字样，表明非真实 LLM）。
func (p *scriptedProvider) Info() ap.ModelInfo {
	return ap.ModelInfo{
		Name:          "wasm-supply-chain-scripted",
		Provider:      "demo-scripted",
		MaxContext:    4096,
		SupportsTools: true,
	}
}

// toolCallCount 已发生的 CallTools 次数（断言用）。
func (p *scriptedProvider) toolCallCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// runAgentWithWASMTool 构造 ReActAgent（toolkit 内含已注册的 WASM 工具），
// 运行一次并返回响应。
func runAgentWithWASMTool(toolName string, registry *ap.ToolRegistry, provider ap.Provider, prompt string) (*ap.Response, error) {
	agent, err := ap.NewAgent(
		"wasm-supply-chain-demo",
		"你是 WASM 工具安全供应链演示的助手；需要计算时调用已注册的 WASM 工具。",
		provider,
		ap.WithToolkit(registry),
		ap.WithMaxTurns(6),
	)
	if err != nil {
		return nil, fmt.Errorf("创建 Agent 失败: %w", err)
	}
	return agent.Run(context.Background(), ap.UserMessage(prompt))
}
