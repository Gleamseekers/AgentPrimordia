// Package testutil 提供 Agent 测试辅助工具，包括 MockProvider 和快捷构造器。
//
// MockProvider 实现 llm.Provider + llm.Embedder 接口，
// 支持预设响应序列、工具调用、延迟注入和错误注入，适合单元测试和示例代码。
package testutil

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
)

// MockProvider 是用于测试的 LLM 提供者。
// 支持预设响应序列、工具调用、延迟和错误注入。
type MockProvider struct {
	mu            sync.Mutex
	responses     []*llm.CompletionResponse
	toolResponses []*llm.ToolCallResponse
	callCount     int
	delay         time.Duration
	err           error
}

// NewMockProvider 创建预设响应的 MockProvider。
// responses 参数指定 Complete 方法依次返回的响应内容。
func NewMockProvider(responses ...string) *MockProvider {
	d := &MockProvider{
		responses: make([]*llm.CompletionResponse, 0, len(responses)),
	}
	for _, r := range responses {
		d.responses = append(d.responses, &llm.CompletionResponse{
			ID:      "mock-id",
			Content: r,
			Role:    "assistant",
			Usage:   llm.Usage{PromptTokens: 10, CompletionTokens: len(r) / 4},
		})
	}
	return d
}

// WithToolCalls 预设工具调用响应序列，每次 CallTools 按序返回一个。
func (m *MockProvider) WithToolCalls(calls ...llm.FunctionCall) *MockProvider {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.toolResponses = append(m.toolResponses, &llm.ToolCallResponse{
		Content:   "",
		ToolCalls: calls,
		Usage:     llm.Usage{PromptTokens: 20, CompletionTokens: 30},
	})
	return m
}

// WithDelay 设置每次调用的模拟延迟。
func (m *MockProvider) WithDelay(delay time.Duration) *MockProvider {
	m.delay = delay
	return m
}

// WithError 设置错误注入，之后的调用将返回该错误。
func (m *MockProvider) WithError(err error) *MockProvider {
	m.err = err
	return m
}

// Complete 实现 llm.Provider.Complete。
func (m *MockProvider) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.callCount++

	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if m.err != nil {
		return nil, m.err
	}

	if len(m.responses) == 0 {
		return &llm.CompletionResponse{
			ID:      "mock-default",
			Content: "This is a default mock response.",
			Role:    "assistant",
		}, nil
	}

	resp := m.responses[0]
	m.responses = m.responses[1:]
	return resp, nil
}

// Stream 实现 llm.Provider.Stream。
func (m *MockProvider) Stream(ctx context.Context, req *llm.CompletionRequest) (<-chan llm.Chunk, error) {
	ch := make(chan llm.Chunk, 10)

	go func() {
		defer close(ch)
		resp, err := m.Complete(ctx, req)
		if err != nil {
			return
		}
		ch <- llm.Chunk{Content: resp.Content, Done: true, Usage: &resp.Usage}
	}()

	return ch, nil
}

// CallTools 实现 llm.Provider.CallTools。
func (m *MockProvider) CallTools(ctx context.Context, req *llm.ToolCallRequest) (*llm.ToolCallResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.callCount++

	if m.err != nil {
		return nil, m.err
	}

	if len(m.toolResponses) == 0 {
		return &llm.ToolCallResponse{
			Content:   "",
			ToolCalls: []llm.FunctionCall{},
			Usage:     llm.Usage{},
		}, nil
	}

	resp := m.toolResponses[0]
	m.toolResponses = m.toolResponses[1:]
	return resp, nil
}

// Embeddings 实现 llm.Embedder.Embeddings，返回随机向量。
func (m *MockProvider) Embeddings(ctx context.Context, texts []string) ([][]float32, error) {
	embeddings := make([][]float32, len(texts))
	for i := range texts {
		embeddings[i] = make([]float32, 16)
	}
	return embeddings, nil
}

// Info 实现 llm.Provider.Info。
func (m *MockProvider) Info() llm.ModelInfo {
	return llm.ModelInfo{
		Name:              "mock-model",
		Provider:          "mock",
		MaxContext:        4096,
		SupportsTools:     true,
		SupportsStreaming: true,
	}
}

// CallCount 返回 Complete + CallTools 的总调用次数。
func (m *MockProvider) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

// ===== NewTestAgent 快捷构造器 =====

// TestAgentConfig 是 NewTestAgent 的配置。
type TestAgentConfig struct {
	// Name Agent 名称（默认 "test-agent"）
	Name string
	// SystemPrompt 系统提示词（默认 "you are a helpful assistant."）
	SystemPrompt string
	// Responses Complete 方法依次返回的响应内容（默认单个通用响应）
	Responses []string
	// MaxTurns ReAct 最大轮数（默认 10）
	MaxTurns int
}

// NewTestAgent 创建预配置了 MockProvider 的测试用 Agent。
// 返回 CapabilityAgent，支持链式注入 Memory/RAG/Hooks 等能力。
//
// 使用方式：
//
//	agent := testutil.NewTestAgent(testutil.TestAgentConfig{
//	    Name: "test-bot",
//	    Responses: []string{"Hello!", "Goodbye!"},
//	})
//	resp, _ := agent.Run(ctx, agent.UserMessage("hi"))
func NewTestAgent(cfg TestAgentConfig) *agent.CapabilityAgent {
	if cfg.Name == "" {
		cfg.Name = "test-agent"
	}
	if cfg.SystemPrompt == "" {
		cfg.SystemPrompt = "you are a helpful assistant."
	}
	if cfg.MaxTurns <= 0 {
		cfg.MaxTurns = 10
	}
	if len(cfg.Responses) == 0 {
		cfg.Responses = []string{"This is a test response."}
	}

	mock := NewMockProvider(cfg.Responses...)

	a, err := agent.NewAgent(cfg.Name, cfg.SystemPrompt, mock, agent.WithMaxTurns(cfg.MaxTurns))
	if err != nil {
		panic(fmt.Sprintf("NewTestAgent: failed to create agent: %v", err))
	}

	// 返回 CapabilityAgent 支持链式注入
	return a
}

// ensure MockProvider implements interfaces
var (
	_ llm.Provider = (*MockProvider)(nil)
	_ llm.Embedder = (*MockProvider)(nil)
)
