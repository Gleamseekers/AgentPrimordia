// react_reasoning.go — LLM 推理阶段
// 包含同步推理和流式推理两种模式的实现
package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/llm"
)

// ===== 流式 tool_calls 增量协议（P2 修复）=====
//
// 背景：流式模式下若配了 toolDefs，旧实现先流式取内容、再发一次非流式
// callToolsWithRetry 取 tool calls——每轮两次 LLM 调用，费用/延迟翻倍；
// 且流式路径不记录 Usage，成本追踪失效。
//
// 修复：provider 实现 StreamingToolCallProvider 时，从单次流式请求中按
// OpenAI 流式协议累积 tool_calls（index/id/name/arguments delta 拼接），
// 不再发第二次非流式请求。llm.Chunk 本身不携带 tool_calls 字段（且
// llm 包不在本修复的改动范围内），故增量协议在本包内定义；不实现该
// 接口的 provider 自动回退原路径，行为与修复前一致。

// ToolCallDelta 是 OpenAI 流式协议中的单次 tool_call 增量片段。
// 同一 Index 的多个 delta 按序拼接：首片携带 ID/Name，后续片段仅
// 追加 Arguments 字符串。
type ToolCallDelta struct {
	Index     int
	ID        string
	Name      string
	Arguments string
}

// StreamToolCallChunk 是携带 tool_calls 增量的流式输出块。
// 最终块以 Done 标记流结束，可携带 provider 给出的 Usage。
type StreamToolCallChunk struct {
	Content   string
	Done      bool
	Usage     *llm.Usage
	ToolCalls []ToolCallDelta
}

// StreamingToolCallProvider 是 llm.Provider 的可选扩展接口：实现它的
// provider 能在单次流式请求中直接产出 tool_calls 增量，从而消除
// "先流式、再非流式取 tool calls"的双倍调用。未实现该接口的 provider
// 自动回退原路径（兼容不支持流式 tool_calls 的 provider）。
type StreamingToolCallProvider interface {
	StreamToolCalls(ctx context.Context, req *llm.ToolCallRequest) (<-chan StreamToolCallChunk, error)
}

// errStreamToolCallsUnavailable 标记流式 tool_calls 通道不可用，
// 调用方据此回退原路径（区别于 ctx 取消等真实错误）。
var errStreamToolCallsUnavailable = errors.New("streaming tool calls unavailable")

// streamToolCallAccumulator 按 OpenAI 流式协议累积 tool_calls delta：
// 按 Index 归并（首片定 ID/Name，后续追加 Arguments），按首片出现顺序
// 输出；对不使用 Index（恒为 0 但 ID 不同）的 provider 亦能正确分段。
type streamToolCallAccumulator struct {
	byIndex map[int]*llm.FunctionCall
	order   []int
}

func newStreamToolCallAccumulator() *streamToolCallAccumulator {
	return &streamToolCallAccumulator{byIndex: make(map[int]*llm.FunctionCall)}
}

// add 合入单个 delta，返回该 delta 是否开启了新的 tool call。
func (a *streamToolCallAccumulator) add(d ToolCallDelta) {
	call, ok := a.byIndex[d.Index]
	if !ok {
		call = &llm.FunctionCall{}
		a.byIndex[d.Index] = call
		a.order = append(a.order, d.Index)
	} else if d.ID != "" && call.ID != "" && d.ID != call.ID {
		// 同一 index 出现新 ID：视为新调用（兼容不按 index 递增的 provider）
		call = &llm.FunctionCall{}
		a.byIndex[d.Index] = call
		a.order = append(a.order, d.Index)
	}
	if d.ID != "" {
		call.ID = d.ID
	}
	if d.Name != "" {
		call.Name = d.Name
	}
	call.Arguments += d.Arguments
}

// result 按首片出现顺序输出累积完成的 tool calls。
func (a *streamToolCallAccumulator) result() []llm.FunctionCall {
	calls := make([]llm.FunctionCall, 0, len(a.order))
	for _, idx := range a.order {
		calls = append(calls, *a.byIndex[idx])
	}
	return calls
}

// estimateStreamUsage 在 provider 未给出 Usage 时按字符数估算：
// completion ≈ 输出文本字符数/4，prompt ≈ 输入消息字符数/4。
// 估算仅用于成本追踪的量级参考，非精确计费；无任何文本时如实置零。
func estimateStreamUsage(messages []llm.ChatMessage, output string) llm.Usage {
	inChars := 0
	for _, m := range messages {
		inChars += len(m.Content)
	}
	prompt := inChars / 4
	completion := len(output) / 4
	total := prompt + completion
	if total <= 0 {
		return llm.Usage{}
	}
	return llm.Usage{PromptTokens: prompt, CompletionTokens: completion, TotalTokens: total}
}

// syncReasoning 非流式推理阶段
// 优化（Task 2.5）：toolDefs 一次性转换为 []llm.ToolDefinition 并在内部复用，
// 避免每轮 LLM 调用都进行 map 反解。
func (a *ReActAgent) syncReasoning(ctx context.Context, llmMessages []llm.ChatMessage, toolDefs []llm.ToolDefinition, llmStart time.Time) (Thought, error) {
	var thought Thought

	if len(toolDefs) > 0 {
		resp, err := a.callToolsWithRetry(ctx, llmMessages, toolDefs)
		if err != nil {
			a.recordLLM(time.Since(llmStart), err)
			return Thought{}, err
		}
		if len(resp.ToolCalls) == 0 && resp.Content == "" {
			completeResp, completeErr := a.completeWithRetry(ctx, llmMessages)
			if completeErr != nil {
				a.recordLLM(time.Since(llmStart), completeErr)
				return Thought{}, completeErr
			}
			thought = Thought{Content: completeResp.Content, Usage: completeResp.Usage}
		} else {
			thought = Thought{
				Content:   resp.Content,
				ToolCalls: convertToToolCalls(resp.ToolCalls),
				Usage:     resp.Usage,
			}
		}
		a.recordLLM(time.Since(llmStart), nil)
	} else {
		resp, err := a.completeWithRetry(ctx, llmMessages)
		if err != nil {
			a.recordLLM(time.Since(llmStart), err)
			return Thought{}, err
		}
		thought = Thought{Content: resp.Content, Usage: resp.Usage}
		a.recordLLM(time.Since(llmStart), nil)
	}

	return thought, nil
}

// streamReasoning 流式推理阶段
// 先尝试 Stream 接口，失败则回退到非流式调用。
// 优化（Task 1.5 / Task 2.5 / P2）：
//   - 流式拼接改用 strings.Builder，避免 O(n^2) 的字符串拼接
//   - toolDefs 一次性转换，复用传入的 []llm.ToolDefinition，不再重复 tk.Definitions()
//   - P2：provider 支持流式 tool_calls（StreamingToolCallProvider）时，从单次
//     流式请求中累积 tool_calls，不再发第二次非流式请求；Usage 取流式最终
//     chunk 或按字符数估算，成本追踪不再丢失。不支持的 provider 回退原路径。
func (a *ReActAgent) streamReasoning(ctx context.Context, cfg loopConfig, llmMessages []llm.ChatMessage, toolDefs []llm.ToolDefinition, llmStart time.Time) (Thought, error) {
	// P2：单次流式请求直接获取 tool_calls（消除双倍 LLM 调用）
	if len(toolDefs) > 0 {
		if sp, ok := a.config.Model.(StreamingToolCallProvider); ok {
			thought, err := a.streamReasoningToolCalls(ctx, cfg, sp, llmMessages, toolDefs)
			if err == nil {
				a.recordLLM(time.Since(llmStart), nil)
				return thought, nil
			}
			// ctx 取消等真实错误不回退；仅"流式 tool_calls 不可用"时回退原路径
			if !errors.Is(err, errStreamToolCallsUnavailable) {
				return Thought{}, err
			}
		}
	}

	streamCh, streamErr := a.config.Model.Stream(ctx, &llm.CompletionRequest{
		Messages:    llmMessages,
		Temperature: llm.Float64Ptr(a.config.Temperature),
	})

	if streamErr == nil {
		// 优化（Task 1.5）：使用 strings.Builder 拼接流式内容，O(n) 复杂度
		var contentBuilder strings.Builder
		// 预分配容量以减少 realloc（典型 4K 流式响应）
		contentBuilder.Grow(4096)

		for chunk := range streamCh {
			if ctx.Err() != nil {
				_ = a.lifecycle.SetStatus(StatusCancelled)
				a.emitStream(cfg, StreamEvent{Type: StreamEventError, Content: ctx.Err().Error()})
				return Thought{}, ctx.Err()
			}
			if chunk.Content != "" {
				contentBuilder.WriteString(chunk.Content)
				a.emitStream(cfg, StreamEvent{Type: StreamEventToken, Content: chunk.Content})
			}
			if chunk.Done {
				break
			}
		}
		// 注意：llm.Chunk 不携带 tool_calls 与逐 chunk Usage，此处无法从
		// 流式增量中恢复 tool 调用——这正是原路径需要第二次非流式请求的原因。
		// 该路径的 Usage 如实置零（provider 未在 Chunk 中给出）。
		thought := Thought{Content: contentBuilder.String()}

		// 优化（Task 2.5）：直接复用外层传入的 toolDefs，不再调用 tk.Definitions()
		if len(toolDefs) > 0 {
			resp, err := a.callToolsWithRetry(ctx, llmMessages, toolDefs)
			if err == nil && len(resp.ToolCalls) > 0 {
				thought = Thought{
					Content:   resp.Content,
					ToolCalls: convertToToolCalls(resp.ToolCalls),
					Usage:     resp.Usage,
				}
			}
		}

		a.recordLLM(time.Since(llmStart), nil)
		return thought, nil
	}

	// Fallback: 非流式调用
	if len(toolDefs) > 0 {
		resp, err := a.callToolsWithRetry(ctx, llmMessages, toolDefs)
		if err != nil {
			a.recordLLM(time.Since(llmStart), err)
			_ = a.lifecycle.SetStatus(StatusFailed)
			a.emitStream(cfg, StreamEvent{Type: StreamEventError, Content: err.Error()})
			return Thought{}, err
		}
		if len(resp.ToolCalls) == 0 && resp.Content == "" {
			completeResp, completeErr := a.completeWithRetry(ctx, llmMessages)
			if completeErr != nil {
				a.recordLLM(time.Since(llmStart), completeErr)
				_ = a.lifecycle.SetStatus(StatusFailed)
				a.emitStream(cfg, StreamEvent{Type: StreamEventError, Content: completeErr.Error()})
				return Thought{}, completeErr
			}
			a.emitStream(cfg, StreamEvent{Type: StreamEventThought, Content: completeResp.Content})
			a.recordLLM(time.Since(llmStart), nil)
			return Thought{Content: completeResp.Content, Usage: completeResp.Usage}, nil
		}
		a.emitStream(cfg, StreamEvent{Type: StreamEventThought, Content: resp.Content})
		a.recordLLM(time.Since(llmStart), nil)
		return Thought{
			Content:   resp.Content,
			ToolCalls: convertToToolCalls(resp.ToolCalls),
			Usage:     resp.Usage,
		}, nil
	}

	resp, err := a.completeWithRetry(ctx, llmMessages)
	if err != nil {
		a.recordLLM(time.Since(llmStart), err)
		_ = a.lifecycle.SetStatus(StatusFailed)
		a.emitStream(cfg, StreamEvent{Type: StreamEventError, Content: err.Error()})
		return Thought{}, err
	}
	a.emitStream(cfg, StreamEvent{Type: StreamEventThought, Content: resp.Content})
	a.recordLLM(time.Since(llmStart), nil)
	return Thought{Content: resp.Content, Usage: resp.Usage}, nil
}

// streamReasoningToolCalls 单次流式请求获取内容 + tool_calls（P2 修复）。
// 从 StreamToolCallChunk 增量中累积 tool_calls（OpenAI 流式协议 delta
// 拼接），不再发送第二次非流式请求。Usage 优先取流式最终 chunk；
// provider 未给出时按字符数估算填充；无任何文本时如实置零。
func (a *ReActAgent) streamReasoningToolCalls(ctx context.Context, cfg loopConfig, sp StreamingToolCallProvider, llmMessages []llm.ChatMessage, toolDefs []llm.ToolDefinition) (Thought, error) {
	streamCh, err := sp.StreamToolCalls(ctx, &llm.ToolCallRequest{
		Messages: llmMessages,
		Tools:    toolDefs,
	})
	if err != nil {
		// 通道不可用：回退原路径（普通流式 + 非流式 tool calls）
		return Thought{}, fmt.Errorf("%w: %v", errStreamToolCallsUnavailable, err)
	}

	var contentBuilder strings.Builder
	contentBuilder.Grow(4096)
	acc := newStreamToolCallAccumulator()
	var usage *llm.Usage

	for chunk := range streamCh {
		if ctx.Err() != nil {
			_ = a.lifecycle.SetStatus(StatusCancelled)
			a.emitStream(cfg, StreamEvent{Type: StreamEventError, Content: ctx.Err().Error()})
			return Thought{}, ctx.Err()
		}
		if chunk.Content != "" {
			contentBuilder.WriteString(chunk.Content)
			a.emitStream(cfg, StreamEvent{Type: StreamEventToken, Content: chunk.Content})
		}
		for _, d := range chunk.ToolCalls {
			acc.add(d)
		}
		if chunk.Usage != nil {
			// 最终 chunk 通常携带 Usage；后续若还有则以后者为准
			u := *chunk.Usage
			usage = &u
		}
		if chunk.Done {
			break
		}
	}

	thought := Thought{Content: contentBuilder.String()}
	if calls := acc.result(); len(calls) > 0 {
		thought.ToolCalls = convertToToolCalls(calls)
	}
	switch {
	case usage != nil:
		// provider 给出了真实 Usage，原样记录
		thought.Usage = *usage
	default:
		// 拿不到时按字符数估算（含 tool 参数），无任何文本则如实置零
		output := thought.Content
		for _, c := range thought.ToolCalls {
			output += c.Name + c.Args
		}
		thought.Usage = estimateStreamUsage(llmMessages, output)
	}
	return thought, nil
}
