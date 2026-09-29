package demo

import (
	"context"
	"sync"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
)

type DemoLLM struct {
	mu            sync.Mutex
	responses     []*llm.CompletionResponse
	toolResponses []*llm.ToolCallResponse
	callCount     int
	delay         time.Duration
	err           error
}

func NewDemoLLM(responses ...string) *DemoLLM {
	d := &DemoLLM{
		responses: make([]*llm.CompletionResponse, 0, len(responses)),
	}
	for _, r := range responses {
		d.responses = append(d.responses, &llm.CompletionResponse{
			ID:      "demo-id",
			Content: r,
			Role:    "assistant",
			Usage:   llm.Usage{PromptTokens: 10, CompletionTokens: len(r) / 4},
		})
	}
	return d
}

func (d *DemoLLM) WithToolCalls(calls ...llm.FunctionCall) *DemoLLM {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.toolResponses = append(d.toolResponses, &llm.ToolCallResponse{
		Content:   "",
		ToolCalls: calls,
		Usage:     llm.Usage{PromptTokens: 20, CompletionTokens: 30},
	})
	return d
}

// WithResponse 向 Complete 队列追加一条响应（脚本化多轮对话）
func (d *DemoLLM) WithResponse(content string) *DemoLLM {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.responses = append(d.responses, &llm.CompletionResponse{
		ID:      "demo-id",
		Content: content,
		Role:    "assistant",
	})
	return d
}

// WithToolResponse 向 CallTools 队列追加一条响应；
// calls 为空表示本轮无工具调用（引擎将回退到 Complete 队列）
func (d *DemoLLM) WithToolResponse(calls []llm.FunctionCall) *DemoLLM {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.toolResponses = append(d.toolResponses, &llm.ToolCallResponse{
		Content:   "",
		ToolCalls: calls,
	})
	return d
}

func (d *DemoLLM) WithDelay(delay time.Duration) *DemoLLM {
	d.delay = delay
	return d
}

func (d *DemoLLM) WithError(err error) *DemoLLM {
	d.err = err
	return d
}

func (d *DemoLLM) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.callCount++

	if d.delay > 0 {
		select {
		case <-time.After(d.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if d.err != nil {
		return nil, d.err
	}

	if len(d.responses) == 0 {
		return &llm.CompletionResponse{
			ID:      "demo-default",
			Content: "This is a default demo response.",
			Role:    "assistant",
		}, nil
	}

	resp := d.responses[0]
	d.responses = d.responses[1:]
	return resp, nil
}

func (d *DemoLLM) Stream(ctx context.Context, req *llm.CompletionRequest) (<-chan llm.Chunk, error) {
	ch := make(chan llm.Chunk, 10)

	go func() {
		defer close(ch)
		resp, err := d.Complete(ctx, req)
		if err != nil {
			return
		}
		ch <- llm.Chunk{Content: resp.Content, Done: true, Usage: &resp.Usage}
	}()

	return ch, nil
}

func (d *DemoLLM) CallTools(ctx context.Context, req *llm.ToolCallRequest) (*llm.ToolCallResponse, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.callCount++

	if d.err != nil {
		return nil, d.err
	}

	if len(d.toolResponses) == 0 {
		return &llm.ToolCallResponse{
			Content:   "",
			ToolCalls: []llm.FunctionCall{},
			Usage:     llm.Usage{},
		}, nil
	}

	resp := d.toolResponses[0]
	d.toolResponses = d.toolResponses[1:]
	return resp, nil
}

func (d *DemoLLM) Embeddings(ctx context.Context, texts []string) ([][]float32, error) {
	embeddings := make([][]float32, len(texts))
	for i := range texts {
		embeddings[i] = make([]float32, 16)
	}
	return embeddings, nil
}

func (d *DemoLLM) Info() llm.ModelInfo {
	return llm.ModelInfo{
		Name:              "demo-model",
		Provider:          "demo",
		MaxContext:        4096,
		SupportsTools:     true,
		SupportsStreaming: true,
	}
}

func (d *DemoLLM) CallCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.callCount
}
