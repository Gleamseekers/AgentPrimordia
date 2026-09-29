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
		ID: "mock-1", Content: "根据知识库信息，Go 1.22 引入了 range over func 特性。",
		Role: "assistant", Usage: ap.Usage{PromptTokens: 10, CompletionTokens: 20},
	}, nil
}
func (m *MockLLM) Stream(ctx context.Context, req *ap.CompletionRequest) (<-chan ap.Chunk, error) {
	ch := make(chan ap.Chunk, 1)
	go func() { defer close(ch); ch <- ap.Chunk{Content: "搜索中...", Done: true} }()
	return ch, nil
}
func (m *MockLLM) CallTools(ctx context.Context, req *ap.ToolCallRequest) (*ap.ToolCallResponse, error) {
	return &ap.ToolCallResponse{Usage: ap.Usage{}}, nil
}
func (m *MockLLM) Info() ap.ModelInfo {
	return ap.ModelInfo{Name: "mock", Provider: "mock", MaxContext: 4096, SupportsTools: true}
}

// mockRAGProvider 是示例用的模拟 RAG 提供者
type mockRAGProvider struct{}

func (m *mockRAGProvider) Search(ctx context.Context, query string, topK int) ([]*ap.RAGDocument, error) {
	return []*ap.RAGDocument{
		{ID: "doc-1", Content: "Go 1.22 新特性：range over func", Score: 0.95, Source: "release-notes"},
		{ID: "doc-2", Content: "Go 1.22 改进了 PGO 优化", Score: 0.82, Source: "performance-guide"},
	}, nil
}

// mockEventPublisher 是示例用的模拟事件发布器
type mockEventPublisher struct{}

func (m *mockEventPublisher) PublishAsync(eventType string, source string, payload any) error {
	fmt.Printf("[Event] %s from %s\n", eventType, source)
	return nil
}

// mockMetricsRecorder 是示例用的模拟指标记录器
type mockMetricsRecorder struct{}

func (m *mockMetricsRecorder) RecordLLMCall(d time.Duration, err error) {
	fmt.Printf("[Metrics] LLM 调用: %v\n", d)
}
func (m *mockMetricsRecorder) RecordToolCall(d time.Duration, err error) {}
func (m *mockMetricsRecorder) RecordTurn(d time.Duration)                {}
func (m *mockMetricsRecorder) RecordTokenUsage(model string, pt, ct int) {}
func (m *mockMetricsRecorder) IncActiveAgents()                          {}
func (m *mockMetricsRecorder) DecActiveAgents()                          {}

func main() {
	fmt.Println("=== 链式 API：RAG + 事件 + 指标 ===")
	fmt.Println()

	// 创建 Agent 并注入多种能力
	agent, err := ap.NewAgent("rag-agent", "你是一个知识库问答助手，根据检索到的信息回答用户问题。", &MockLLM{},
		ap.WithMaxTurns(5),
		ap.WithRAG(ap.RAGConfig{
			Mode:     ap.RAGModeAuto,
			Provider: &mockRAGProvider{},
			TopK:     3,
			MinScore: 0.5,
		}),
		ap.WithEvents(&mockEventPublisher{}),
		ap.WithMetrics(&mockMetricsRecorder{}),
	)
	if err != nil {
		log.Fatalf("创建 Agent 失败: %v", err)
	}

	// 运行
	resp, err := agent.Run(context.Background(), ap.UserMessage("Go 1.22 有什么新特性？"))
	if err != nil {
		log.Fatalf("运行失败: %v", err)
	}

	fmt.Printf("\n回复: %s\n", resp.Content)
	fmt.Printf("轮数: %d\n", resp.Metrics.TotalTurns)

	// 验证 Capable 接口发现
	if _, ok := any(agent).(ap.RAGCapable); ok {
		fmt.Println("✓ RAG 能力已启用")
	}
	if _, ok := any(agent).(ap.EventCapable); ok {
		fmt.Println("✓ 事件发布能力已启用")
	}
	if _, ok := any(agent).(ap.MetricsCapable); ok {
		fmt.Println("✓ 指标记录能力已启用")
	}
}
