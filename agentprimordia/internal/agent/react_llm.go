// react_llm.go — LLM 调用 + tool执行
// 包含tool执行、LLM 调用重试、错误处理等底层操作
package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/tools"
)

// ErrMaxTurnsExceeded is returned when the agent exceeds max turn limit
var ErrMaxTurnsExceeded = errors.New("max turns exceeded")

var ErrBudgetExceeded = errors.New("budget exceeded")

var ErrNoToolkit = errors.New("no toolkit configured")

// ErrInputBlocked 表示用户输入被输入端护栏拒绝（v3.4-4）
var ErrInputBlocked = errors.New("input blocked by guardrail")

// ErrOutputBlocked 表示 LLM 输出被输出端护栏拦截。
// 与 ErrInputBlocked 对称，供 pkg 层 re-export；guardrail 拦截路径必须
// 抛出本 sentinel，保证 errors.Is(err, ap.ErrOutputBlocked) 可命中
// （修复评估报告 §四.1-③：此前 react_loop_core.go 用裸 fmt.Errorf，
// 与 pkg.ErrOutputBlocked 消息不同，errors.Is 永不匹配）。
var ErrOutputBlocked = errors.New("output blocked by guardrail")

// getOrInitExecutor 懒加载返回缓存的 Executor。优化（Task 1.5）：避免每轮tool调用都 NewExecutor。
func (a *ReActAgent) getOrInitExecutor() *tools.Executor {
	a.toolExecutorOnce.Do(func() {
		tk := a.getToolkit()
		if tk == nil {
			return
		}
		a.toolExecutor = tools.NewExecutor(tk)
	})
	return a.toolExecutor
}

// executeTool runs a single tool call
// 优化（perf-v3）：优先使用 capCache 缓存的 toolkit，避免每次tool调用都做类型断言
func (a *ReActAgent) executeTool(ctx context.Context, tc ToolCall) (*ToolResult, error) {
	var tk *tools.Registry
	if a.capCache != nil {
		tk = a.capCache.toolkit
	} else {
		tk = a.getToolkit()
	}
	if tk == nil {
		return &ToolResult{
			ToolCallID: tc.ID,
			Content:    "error: no toolkit configured",
			IsError:    true,
		}, ErrNoToolkit
	}

	executor := a.getOrInitExecutor()
	if executor == nil {
		return &ToolResult{
			ToolCallID: tc.ID,
			Content:    "error: failed to initialize tool executor",
			IsError:    true,
		}, ErrNoToolkit
	}

	fc := tools.FunctionCall{
		ID:   tc.ID,
		Name: tc.Name,
		Args: tc.Args,
	}

	// v3.4-4：tool 执行层失败自动重试（网络抖动/瞬时错误），最多 defaultToolRetries 次额外尝试。
	// 注意：业务级拒绝（result.IsError）不触发重试，仅执行层 error 重试。
	var lastErr error
	for attempt := 0; attempt <= defaultToolRetries; attempt++ {
		if ctx.Err() != nil {
			return &ToolResult{
				ToolCallID: tc.ID,
				Content:    "tool execution cancelled",
				IsError:    true,
			}, ctx.Err()
		}
		result, err := executor.Execute(ctx, &fc)
		if err == nil {
			return &ToolResult{
				ToolCallID: tc.ID,
				Content:    result.Content,
				IsError:    result.IsError,
			}, nil
		}
		lastErr = err
		a.logger.Debug("tool 执行失败，准备重试",
			"tool", tc.Name,
			"attempt", attempt+1,
			"error", err,
		)
	}

	return &ToolResult{
		ToolCallID: tc.ID,
		Content:    lastErr.Error(),
		IsError:    true,
	}, lastErr
}

// defaultToolRetries tool 执行层失败时的额外重试次数（v3.4-4）
const defaultToolRetries = 1

// callToolsWithRetry calls LLM with function calling support, with retry on transient errors
// 优化（Task 2.5）：直接接受 []llm.ToolDefinition 而非 []map[string]any，
// 由调用方使用 convertToolDefsToLLMDefinitions 一次性转换，避免每轮重复反解。
func (a *ReActAgent) callToolsWithRetry(ctx context.Context, messages []llm.ChatMessage, definitions []llm.ToolDefinition) (*llm.ToolCallResponse, error) {
	req := &llm.ToolCallRequest{
		Messages: messages,
		Tools:    definitions,
	}

	const maxRetries = 2
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		resp, err := a.config.Model.CallTools(ctx, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		a.logger.Warn("CallTools 失败，将重试", "attempt", attempt+1, "error", err)
	}
	return nil, fmt.Errorf("callTools failed after %d retries: %w", maxRetries, lastErr)
}

// completeWithRetry calls LLM for simple completion, with retry on transient errors
func (a *ReActAgent) completeWithRetry(ctx context.Context, messages []llm.ChatMessage) (*llm.CompletionResponse, error) {
	req := &llm.CompletionRequest{
		Messages:    messages,
		Temperature: llm.Float64Ptr(a.config.Temperature),
	}

	const maxRetries = 2
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		resp, err := a.config.Model.Complete(ctx, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		a.logger.Warn("Complete 失败，将重试", "attempt", attempt+1, "error", err)
	}
	return nil, fmt.Errorf("complete failed after %d retries: %w", maxRetries, lastErr)
}

// handleOnError triggers error hook
func (a *ReActAgent) handleOnError(_ context.Context, err error) {
	_ = a.fireHook(HookOnError, &HookContext{Error: err})
}
