package suite

import (
	"context"
	"fmt"
	"testing"

	ap "github.com/Gleamseekers/AgentPrimordia/pkg"
)

// BenchmarkToolCalling 基准：工具调用准确率
// 使用 MockLLM 模拟 LLM 返回工具调用请求，验证工具选择和执行正确性
func BenchmarkToolCalling(b *testing.B) {
	registry := ap.NewToolRegistry()
	fs, _ := ap.NewFileSystem(".")
	_ = registry.Register(fs)

	agent, err := ap.NewAgent("BenchAgent", "你是一个助手，使用工具完成任务。", &benchMockLLM{}, ap.WithMaxTurns(5), ap.WithToolkit(registry))
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = agent.Run(context.Background(), ap.UserMessage("读取文件"))
	}
}

// BenchmarkAgentRun 基准：单次 Agent 运行吞吐量
func BenchmarkAgentRun(b *testing.B) {
	agent, err := ap.NewAgent("ThroughputAgent", "你是助手", &benchMockLLM{}, ap.WithMaxTurns(3))
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = agent.Run(context.Background(), ap.UserMessage("hello"))
	}
}

// BenchmarkMemoryStore 基准：记忆存储写入和搜索
func BenchmarkMemoryStore(b *testing.B) {
	memory, _ := ap.WithInMemory()
	defer memory.Close()
	ctx := context.Background()

	b.Run("Add", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = memory.Add(ctx, &ap.Episode{
				ID:      fmt.Sprintf("bench-%d", i),
				Content: "benchmark test episode",
				Role:    "user",
			})
		}
	})

	b.Run("Search", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = memory.Search(ctx, "benchmark", nil)
		}
	})
}

// benchMockLLM 基准测试用 Mock LLM
type benchMockLLM struct{}

func (m *benchMockLLM) Complete(ctx context.Context, req *ap.CompletionRequest) (*ap.CompletionResponse, error) {
	return &ap.CompletionResponse{
		ID:      "bench-mock",
		Content: "模拟回复",
		Role:    "assistant",
		Usage:   ap.Usage{PromptTokens: 10, CompletionTokens: 10},
	}, nil
}

func (m *benchMockLLM) Stream(ctx context.Context, req *ap.CompletionRequest) (<-chan ap.Chunk, error) {
	ch := make(chan ap.Chunk, 1)
	go func() {
		defer close(ch)
		ch <- ap.Chunk{Content: "模拟回复", Done: true}
	}()
	return ch, nil
}

func (m *benchMockLLM) CallTools(ctx context.Context, req *ap.ToolCallRequest) (*ap.ToolCallResponse, error) {
	return &ap.ToolCallResponse{Usage: ap.Usage{}}, nil
}

func (m *benchMockLLM) Embeddings(ctx context.Context, texts []string) ([][]float32, error) {
	return make([][]float32, len(texts)), nil
}

func (m *benchMockLLM) Info() ap.ModelInfo {
	return ap.ModelInfo{Name: "bench-mock", Provider: "mock", MaxContext: 4096, SupportsTools: true}
}
