// tool_retry_guard_test.go — v3.4-4 tool 重试 / 并行 recover / 输入端护栏测试
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/tools"
)

// retryTool 第一次执行返回瞬时错误，之后成功
type retryTool struct {
	attempts atomic.Int64
}

func (t *retryTool) Name() string        { return "retry_tool" }
func (t *retryTool) Description() string { return "retry test tool" }
func (t *retryTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (t *retryTool) Execute(_ context.Context, _ json.RawMessage) (*tools.Result, error) {
	if t.attempts.Add(1) == 1 {
		return nil, errors.New("transient failure")
	}
	return &tools.Result{Content: "ok"}, nil
}

// panicTool 执行时直接 panic（验证并行 recover 不击穿循环）
type panicTool struct{}

func (t *panicTool) Name() string        { return "panic_tool" }
func (t *panicTool) Description() string { return "panic test tool" }
func (t *panicTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (t *panicTool) Execute(_ context.Context, _ json.RawMessage) (*tools.Result, error) {
	panic("boom")
}

// TestExecuteTool_RetryThenSuccess 验证 tool 执行层瞬时失败自动重试
func TestExecuteTool_RetryThenSuccess(t *testing.T) {
	mock := llm.NewMockLLM(t)
	ag, err := NewAgent("tool-retry", "助手", mock, WithToolkit(func() *tools.Registry {
		r := tools.NewRegistry()
		_ = r.Register(&retryTool{})
		return r
	}()))
	if err != nil {
		t.Fatalf("NewAgent 失败: %v", err)
	}

	// 直接调用内部 executeTool 验证重试
	inner := ag.Inner()
	result, err := inner.executeTool(context.Background(), ToolCall{ID: "c1", Name: "retry_tool", Args: "{}"})
	if err != nil {
		t.Fatalf("executeTool 应重试成功: %v", err)
	}
	if result.Content != "ok" {
		t.Fatalf("Content = %q, want ok", result.Content)
	}
}

// TestExecuteToolCallsParallel_PanicRecover 验证并行 tool 执行时单个 tool panic
// 被 recover 捕获转为错误结果，循环不被击穿。
func TestExecuteToolCallsParallel_PanicRecover(t *testing.T) {
	mock := llm.NewMockLLM(t)
	ag, err := NewAgent("tool-panic", "助手", mock,
		WithToolkit(func() *tools.Registry {
			r := tools.NewRegistry()
			_ = r.Register(&panicTool{})
			_ = r.Register(&retryTool{})
			return r
		}()),
	)
	if err != nil {
		t.Fatalf("NewAgent 失败: %v", err)
	}
	// 强制并行执行
	inner := ag.Inner()
	inner.config.ParallelToolExecution = true
	inner.config.MaxParallelTools = 2

	calls := []ToolCall{
		{ID: "p1", Name: "panic_tool", Args: "{}"},
		{ID: "p2", Name: "retry_tool", Args: "{}"},
	}
	history, _, _ := inner.executeToolCalls(context.Background(), []Message{}, calls, 0, loopConfig{}, nil, nil, 0, 0)
	// panic 工具应产生错误结果，但不 panic 出循环
	foundErr := false
	for _, m := range history {
		if strings.Contains(m.Content, "panic") {
			foundErr = true
		}
	}
	if !foundErr {
		t.Errorf("应记录 panic 工具的错误结果, history: %+v", history)
	}
}

// TestRun_InputGuard_Sanitize 验证输入端护栏在 sanitize 动作时替换输入
func TestRun_InputGuard_Sanitize(t *testing.T) {
	mock := llm.NewMockLLM(t)
	mock.WithResponse("已处理")

	guard := InputGuard(func(content string) (string, bool, error) {
		return strings.ReplaceAll(content, "坏词", "好词"), false, nil
	})

	ag, err := NewAgent("input-guard", "助手", mock, WithInputGuard(guard))
	if err != nil {
		t.Fatalf("NewAgent 失败: %v", err)
	}

	resp, err := ag.Run(context.Background(), Message{Role: RoleUser, Content: "这段包含坏词"})
	if err != nil {
		t.Fatalf("Run 失败: %v", err)
	}
	if resp.Content == "" {
		t.Fatal("响应为空")
	}
	// 断言 LLM 收到的是脱敏后的输入
	last := mock.LastRequest()
	req, ok := last.(*llm.CompletionRequest)
	if !ok {
		t.Fatalf("LastRequest 类型 = %T", last)
	}
	all := ""
	for _, m := range req.Messages {
		all += m.Content + "\n"
	}
	if strings.Contains(all, "坏词") {
		t.Errorf("LLM 不应收到原始输入（应脱敏）: %q", all)
	}
	if !strings.Contains(all, "好词") {
		t.Errorf("LLM 应收到脱敏后输入: %q", all)
	}
}

// TestRun_InputGuard_Block 验证输入端护栏在 block 动作时拒绝输入
func TestRun_InputGuard_Block(t *testing.T) {
	mock := llm.NewMockLLM(t)

	guard := InputGuard(func(_ string) (string, bool, error) {
		return "", true, nil
	})

	ag, err := NewAgent("input-block", "助手", mock, WithInputGuard(guard))
	if err != nil {
		t.Fatalf("NewAgent 失败: %v", err)
	}

	_, err = ag.Run(context.Background(), Message{Role: RoleUser, Content: "被拒绝的输入"})
	if err == nil {
		t.Fatal("block 输入应返回错误")
	}
}
