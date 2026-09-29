package agent

// react_reasoning_stream_tools_test.go — 流式推理单次请求获取 tool_calls
// （P2 修复，TDD：先红后绿）。
//
// 背景：流式模式下若配了 toolDefs，旧实现先在流式请求中取内容、再发一次
// 非流式 callToolsWithRetry 取 tool calls——每轮两次 LLM 调用，费用/延迟
// 翻倍；且流式路径不记录 thought.Usage，成本追踪失效。
// 修复：provider 实现 StreamingToolCallProvider（流式 tool_calls 增量）时，
// 从单次流式请求中按 OpenAI 流式协议累积 tool_calls（index/id/name/
// arguments delta 拼接），Usage 取最终 chunk 或按字符数估算；
// 不支持的 provider 自动回退原路径。

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools"
)

// ===== 支持流式 tool_calls 的 Mock =====

// streamToolCallsMockLLM 实现 llm.Provider + StreamingToolCallProvider。
// 每次 StreamToolCalls 调用弹出一组预置 chunk；同时统计各类调用次数，
// 用于断言"单次请求、无双倍调用"。
type streamToolCallsMockLLM struct {
	mu sync.Mutex

	// turns 为每一轮流式响应预置的 chunk 序列（按调用顺序弹出）
	turns [][]StreamToolCallChunk

	streamToolCallsCalls int
	streamCalls          int
	callToolsCalls       int
	completeCalls        int

	// streamToolCallsErr 非空时 StreamToolCalls 返回该错误（测回退）
	streamToolCallsErr error

	// toolCallUsed 首轮 CallTools 返回 tool call，之后返回空（终止循环）
	toolCallUsed bool
}

func (m *streamToolCallsMockLLM) nextTurn() []StreamToolCallChunk {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.turns) == 0 {
		return nil
	}
	turn := m.turns[0]
	m.turns = m.turns[1:]
	return turn
}

func (m *streamToolCallsMockLLM) Complete(_ context.Context, _ *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	m.mu.Lock()
	m.completeCalls++
	m.mu.Unlock()
	return &llm.CompletionResponse{
		ID:      "mock-complete",
		Content: "non-stream fallback",
		Role:    "assistant",
		Usage:   llm.Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2},
	}, nil
}

func (m *streamToolCallsMockLLM) Stream(_ context.Context, _ *llm.CompletionRequest) (<-chan llm.Chunk, error) {
	m.mu.Lock()
	m.streamCalls++
	m.mu.Unlock()
	ch := make(chan llm.Chunk, 2)
	go func() {
		defer close(ch)
		ch <- llm.Chunk{Content: "plain stream"}
		ch <- llm.Chunk{Done: true, Usage: &llm.Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2}}
	}()
	return ch, nil
}

func (m *streamToolCallsMockLLM) CallTools(_ context.Context, _ *llm.ToolCallRequest) (*llm.ToolCallResponse, error) {
	m.mu.Lock()
	m.callToolsCalls++
	used := m.toolCallUsed
	m.toolCallUsed = true
	m.mu.Unlock()
	if used {
		return &llm.ToolCallResponse{
			Content:   "done",
			ToolCalls: nil,
			Usage:     llm.Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2},
		}, nil
	}
	return &llm.ToolCallResponse{
		Content:   "",
		ToolCalls: []llm.FunctionCall{{ID: "fallback_call", Name: "get_time", Arguments: "{}"}},
		Usage:     llm.Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2},
	}, nil
}

func (m *streamToolCallsMockLLM) StreamToolCalls(_ context.Context, _ *llm.ToolCallRequest) (<-chan StreamToolCallChunk, error) {
	m.mu.Lock()
	m.streamToolCallsCalls++
	err := m.streamToolCallsErr
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	turn := m.nextTurn()
	ch := make(chan StreamToolCallChunk, len(turn)+1)
	go func() {
		defer close(ch)
		for _, c := range turn {
			ch <- c
		}
	}()
	return ch, nil
}

func (m *streamToolCallsMockLLM) Embeddings(_ context.Context, _ []string) ([][]float32, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *streamToolCallsMockLLM) Info() llm.ModelInfo {
	return llm.ModelInfo{
		Name:              "stream-tool-calls-mock",
		Provider:          "mock",
		MaxContext:        4096,
		SupportsTools:     true,
		SupportsStreaming: true,
	}
}

func (m *streamToolCallsMockLLM) counts() (streamToolCalls, stream, callTools, complete int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.streamToolCallsCalls, m.streamCalls, m.callToolsCalls, m.completeCalls
}

// newStreamToolCallsAgent 创建带 get_time 工具的流式 Agent（CapabilityAgent）
func newStreamToolCallsAgent(t *testing.T, mock llm.Provider, ct *CostTracker) *CapabilityAgent {
	t.Helper()
	registry := tools.NewRegistry()
	if err := registry.Register(&mockTimeTool{name: "get_time"}); err != nil {
		t.Fatalf("register get_time error: %v", err)
	}
	agent, err := NewAgent("stream-tool-calls", "", mock, WithMaxTurns(10))
	if err != nil {
		t.Fatalf("NewAgent() error = %v", err)
	}
	cap := agent.WithToolkit(registry)
	if ct != nil {
		cap = cap.WithCostTracker(ct)
	}
	return cap
}

// ===== 测试用例 =====

// TestStreamReasoning_StreamToolCalls_SingleRequestPerTurn 验证：
// provider 支持流式 tool_calls 时，每轮只发一次 LLM 请求（无第二次
// 非流式 callToolsWithRetry），tool 正确执行，Usage 有记录。
func TestStreamReasoning_StreamToolCalls_SingleRequestPerTurn(t *testing.T) {
	mock := &streamToolCallsMockLLM{
		turns: [][]StreamToolCallChunk{
			{
				{Content: "Let me "},
				{Content: "check"},
				{ToolCalls: []ToolCallDelta{
					{Index: 0, ID: "call_1", Name: "get_time"},
				}},
				{ToolCalls: []ToolCallDelta{
					{Index: 0, Arguments: `{"tz":`},
				}},
				{ToolCalls: []ToolCallDelta{
					{Index: 0, Arguments: `"UTC"}`},
				}},
				{Done: true, Usage: &llm.Usage{PromptTokens: 12, CompletionTokens: 8, TotalTokens: 20}},
			},
			{
				{Content: "The time is 12:00 PM"},
				{Done: true, Usage: &llm.Usage{PromptTokens: 15, CompletionTokens: 6, TotalTokens: 21}},
			},
		},
	}

	ct := NewCostTracker(nil, nil)
	cap := newStreamToolCallsAgent(t, mock, ct)

	ch, err := cap.StreamRun(context.Background(), UserMessage("What time?"))
	if err != nil {
		t.Fatalf("StreamRun() error = %v", err)
	}

	var toolCallEvents, toolResultEvents int
	var completeContent string
	for evt := range ch {
		switch evt.Type {
		case StreamEventToolCall:
			toolCallEvents++
		case StreamEventToolResult:
			toolResultEvents++
		case StreamEventComplete:
			if resp, ok := evt.Data.(*Response); ok {
				completeContent = resp.Content
			}
		case StreamEventError:
			t.Errorf("unexpected error event: %s", evt.Content)
		}
	}

	// 每轮恰好一次流式请求；不得出现第二次非流式调用
	stCalls, streamCalls, callToolsCalls, completeCalls := mock.counts()
	if stCalls != 2 {
		t.Errorf("StreamToolCalls calls = %d, want 2 (one per turn)", stCalls)
	}
	if streamCalls != 0 {
		t.Errorf("plain Stream calls = %d, want 0 (single-request path must not fall back)", streamCalls)
	}
	if callToolsCalls != 0 {
		t.Errorf("CallTools calls = %d, want 0 (double LLM call eliminated)", callToolsCalls)
	}
	if completeCalls != 0 {
		t.Errorf("Complete calls = %d, want 0", completeCalls)
	}

	if toolCallEvents != 1 {
		t.Errorf("tool call events = %d, want 1", toolCallEvents)
	}
	if toolResultEvents != 1 {
		t.Errorf("tool result events = %d, want 1", toolResultEvents)
	}
	if completeContent != "The time is 12:00 PM" {
		t.Errorf("complete content = %q, want %q", completeContent, "The time is 12:00 PM")
	}

	// Usage 有记录（两轮流式各自记账）
	records := ct.Records()
	if len(records) != 2 {
		t.Fatalf("cost records = %d, want 2", len(records))
	}
	if records[0].TotalTokens != 20 || records[0].PromptTokens != 12 {
		t.Errorf("turn 1 usage = %+v, want prompt=12 total=20", records[0])
	}
	if records[1].TotalTokens != 21 {
		t.Errorf("turn 2 usage = %+v, want total=21", records[1])
	}
}

// TestStreamReasoning_StreamToolCalls_DeltaAccumulation 直接单测
// streamReasoning：OpenAI 流式协议 tool_calls delta（index/id/name/
// arguments 增量、多 index 交错）正确拼接，Usage 取自最终 chunk。
func TestStreamReasoning_StreamToolCalls_DeltaAccumulation(t *testing.T) {
	mock := &streamToolCallsMockLLM{
		turns: [][]StreamToolCallChunk{
			{
				{Content: "thinking "},
				{ToolCalls: []ToolCallDelta{
					{Index: 0, ID: "call_a", Name: "get_time"},
					{Index: 1, ID: "call_b", Name: "echo"},
				}},
				{ToolCalls: []ToolCallDelta{
					{Index: 0, Arguments: `{"a":`},
					{Index: 1, Arguments: `{"b":`},
				}},
				{ToolCalls: []ToolCallDelta{
					{Index: 0, Arguments: `1}`},
					{Index: 1, Arguments: `2}`},
				}},
				{Done: true, Usage: &llm.Usage{PromptTokens: 7, CompletionTokens: 9, TotalTokens: 16}},
			},
		},
	}

	agent := newReActAgent(ReActConfig{
		Name:     "delta-accum",
		Model:    mock,
		MaxTurns: 2,
	})

	toolDefs := []llm.ToolDefinition{
		{Type: "function", Function: llm.FunctionDefinition{Name: "get_time"}},
		{Type: "function", Function: llm.FunctionDefinition{Name: "echo"}},
	}
	thought, err := agent.streamReasoning(
		context.Background(),
		loopConfig{},
		[]llm.ChatMessage{{Role: "user", Content: "hi"}},
		toolDefs,
		time.Now(),
	)
	if err != nil {
		t.Fatalf("streamReasoning error: %v", err)
	}

	if thought.Content != "thinking " {
		t.Errorf("content = %q, want %q", thought.Content, "thinking ")
	}
	if len(thought.ToolCalls) != 2 {
		t.Fatalf("tool calls = %d, want 2", len(thought.ToolCalls))
	}
	// index 顺序保持：call_a 在前、call_b 在后
	if thought.ToolCalls[0].ID != "call_a" || thought.ToolCalls[0].Name != "get_time" {
		t.Errorf("tool call 0 = %+v, want id=call_a name=get_time", thought.ToolCalls[0])
	}
	if thought.ToolCalls[0].Args != `{"a":1}` {
		t.Errorf("tool call 0 args = %q, want %q", thought.ToolCalls[0].Args, `{"a":1}`)
	}
	if thought.ToolCalls[1].ID != "call_b" || thought.ToolCalls[1].Name != "echo" {
		t.Errorf("tool call 1 = %+v, want id=call_b name=echo", thought.ToolCalls[1])
	}
	if thought.ToolCalls[1].Args != `{"b":2}` {
		t.Errorf("tool call 1 args = %q, want %q", thought.ToolCalls[1].Args, `{"b":2}`)
	}
	// Usage 取自最终 chunk
	if thought.Usage.TotalTokens != 16 || thought.Usage.PromptTokens != 7 {
		t.Errorf("usage = %+v, want prompt=7 total=16", thought.Usage)
	}
}

// TestStreamReasoning_StreamToolCalls_UsageEstimatedWhenAbsent 验证
// provider 未给 Usage 时按字符数估算填充（成本追踪不丢）。
func TestStreamReasoning_StreamToolCalls_UsageEstimatedWhenAbsent(t *testing.T) {
	longContent := "This is a sufficiently long streamed answer for estimation."
	mock := &streamToolCallsMockLLM{
		turns: [][]StreamToolCallChunk{
			{
				{Content: longContent},
				{Done: true},
			},
		},
	}

	agent := newReActAgent(ReActConfig{
		Name:     "usage-estimate",
		Model:    mock,
		MaxTurns: 2,
	})

	thought, err := agent.streamReasoning(
		context.Background(),
		loopConfig{},
		[]llm.ChatMessage{{Role: "user", Content: "hi"}},
		[]llm.ToolDefinition{{Type: "function", Function: llm.FunctionDefinition{Name: "get_time"}}},
		time.Now(),
	)
	if err != nil {
		t.Fatalf("streamReasoning error: %v", err)
	}

	if thought.Usage.TotalTokens <= 0 {
		t.Errorf("expected estimated usage > 0, got %+v", thought.Usage)
	}
	if thought.Usage.CompletionTokens != len(longContent)/4 {
		t.Errorf("completion tokens = %d, want %d (len/4)", thought.Usage.CompletionTokens, len(longContent)/4)
	}
}

// TestStreamReasoning_StreamToolCalls_NoTextUsageZero 验证无任何输出
// （无文本、无 tool 调用）且 provider 未给 Usage 时如实置零（不伪造计费）。
func TestStreamReasoning_StreamToolCalls_NoTextUsageZero(t *testing.T) {
	mock := &streamToolCallsMockLLM{
		turns: [][]StreamToolCallChunk{
			{
				{Done: true},
			},
		},
	}

	agent := newReActAgent(ReActConfig{
		Name:     "usage-zero",
		Model:    mock,
		MaxTurns: 2,
	})

	thought, err := agent.streamReasoning(
		context.Background(),
		loopConfig{},
		nil,
		[]llm.ToolDefinition{{Type: "function", Function: llm.FunctionDefinition{Name: "get_time"}}},
		time.Now(),
	)
	if err != nil {
		t.Fatalf("streamReasoning error: %v", err)
	}
	if thought.Usage.TotalTokens != 0 {
		t.Errorf("expected zero usage when no output and no provider usage, got %+v", thought.Usage)
	}
}

// TestStreamReasoning_StreamToolCalls_ToolArgsCountedInEstimate 验证
// 仅有 tool 调用（无文本）时，参数文本计入 Usage 估算（成本追踪不丢）。
func TestStreamReasoning_StreamToolCalls_ToolArgsCountedInEstimate(t *testing.T) {
	mock := &streamToolCallsMockLLM{
		turns: [][]StreamToolCallChunk{
			{
				{ToolCalls: []ToolCallDelta{{Index: 0, ID: "c1", Name: "get_time", Arguments: `{"timezone":"UTC"}`}}},
				{Done: true},
			},
		},
	}

	agent := newReActAgent(ReActConfig{
		Name:     "usage-args",
		Model:    mock,
		MaxTurns: 2,
	})

	thought, err := agent.streamReasoning(
		context.Background(),
		loopConfig{},
		nil,
		[]llm.ToolDefinition{{Type: "function", Function: llm.FunctionDefinition{Name: "get_time"}}},
		time.Now(),
	)
	if err != nil {
		t.Fatalf("streamReasoning error: %v", err)
	}
	if thought.Usage.TotalTokens <= 0 {
		t.Errorf("expected estimated usage > 0 from tool args, got %+v", thought.Usage)
	}
}

// TestStreamReasoning_StreamToolCalls_FallbackOnStreamError 验证
// StreamToolCalls 不可用（返回错误）时回退原路径（普通流式 + 非流式
// tool calls），行为与修复前一致。
func TestStreamReasoning_StreamToolCalls_FallbackOnStreamError(t *testing.T) {
	mock := &streamToolCallsMockLLM{
		streamToolCallsErr: errors.New("streaming tool calls unsupported"),
		turns:              [][]StreamToolCallChunk{{}},
	}

	cap := newStreamToolCallsAgent(t, mock, nil)

	ch, err := cap.StreamRun(context.Background(), UserMessage("What time?"))
	if err != nil {
		t.Fatalf("StreamRun() error = %v", err)
	}

	var toolResultEvents int
	for evt := range ch {
		switch evt.Type {
		case StreamEventToolResult:
			toolResultEvents++
		case StreamEventError:
			t.Errorf("unexpected error event: %s", evt.Content)
		}
	}

	// 回退原路径：每轮 StreamToolCalls 失败一次 → 普通流式 + CallTools 接管
	stCalls, streamCalls, callToolsCalls, _ := mock.counts()
	if stCalls != 2 {
		t.Errorf("StreamToolCalls calls = %d, want 2 (attempted once per turn, both failed)", stCalls)
	}
	if streamCalls != 2 {
		t.Errorf("plain Stream calls = %d, want 2 (fallback path)", streamCalls)
	}
	if callToolsCalls != 2 {
		t.Errorf("CallTools calls = %d, want 2 (original double-call path)", callToolsCalls)
	}
	if toolResultEvents != 1 {
		t.Errorf("tool result events = %d, want 1 (fallback path still executes tools)", toolResultEvents)
	}
}

// TestStreamReasoning_FallbackWhenProviderLacksStreamToolCalls 验证
// 不实现 StreamingToolCallProvider 的 provider 走原路径（兼容性）。
func TestStreamReasoning_FallbackWhenProviderLacksStreamToolCalls(t *testing.T) {
	// streamMockLLM（react_loop_stream_test.go）不实现 StreamToolCalls
	mock := &streamMockLLM{
		chunks: []string{"Let me", " check"},
		toolCalls: []llm.FunctionCall{
			{ID: "call_1", Name: "get_time", Arguments: "{}"},
		},
		finalResp: "The current time is 12:00 PM.",
	}

	cap := newStreamToolCallsAgent(t, mock, nil)

	ch, err := cap.StreamRun(context.Background(), UserMessage("What time?"))
	if err != nil {
		t.Fatalf("StreamRun() error = %v", err)
	}

	var toolResultEvents int
	for evt := range ch {
		if evt.Type == StreamEventToolResult {
			toolResultEvents++
		}
	}
	if toolResultEvents != 1 {
		t.Errorf("tool result events = %d, want 1 (original path)", toolResultEvents)
	}
	if !mock.toolCallUsed {
		t.Error("expected original path to use CallTools")
	}
}

// TestStreamReasoning_StreamToolCalls_ContextCancelled 验证流式中途
// ctx 取消时返回错误、不继续消费。
func TestStreamReasoning_StreamToolCalls_ContextCancelled(t *testing.T) {
	mock := &streamToolCallsMockLLM{
		turns: [][]StreamToolCallChunk{
			{
				{Content: "partial"},
				{Done: true},
			},
		},
	}

	agent := newReActAgent(ReActConfig{
		Name:     "stream-cancel-tools",
		Model:    mock,
		MaxTurns: 2,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := agent.streamReasoning(
		ctx,
		loopConfig{},
		[]llm.ChatMessage{{Role: "user", Content: "hi"}},
		[]llm.ToolDefinition{{Type: "function", Function: llm.FunctionDefinition{Name: "get_time"}}},
		time.Now(),
	)
	if err == nil {
		t.Error("expected error on cancelled context")
	}
}

// TestEstimateStreamUsage 单测 Usage 估算辅助函数。
func TestEstimateStreamUsage(t *testing.T) {
	// 有文本：按字符数估算（completion≈输出/4，prompt≈输入/4）
	usage := estimateStreamUsage(
		[]llm.ChatMessage{{Role: "user", Content: "12345678"}}, // 8 chars → 2
		"abcdefghij", // 10 chars → 2 (10/4=2)
	)
	if usage.PromptTokens != 2 || usage.CompletionTokens != 2 || usage.TotalTokens != 4 {
		t.Errorf("usage = %+v, want prompt=2 completion=2 total=4", usage)
	}

	// 无任何文本：如实置零
	zero := estimateStreamUsage(nil, "")
	if zero.TotalTokens != 0 || zero.PromptTokens != 0 || zero.CompletionTokens != 0 {
		t.Errorf("expected zero usage for empty input, got %+v", zero)
	}
}
