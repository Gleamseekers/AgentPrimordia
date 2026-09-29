//go:build go1.23

package agent

import (
	"context"
	"errors"
	"iter"

	"github.com/Gleamseekers/AgentPrimordia/internal/llm"
)

// StreamSeq 返回流式输出迭代器（Go 1.23+ 风格）。
//
// 用法:
//
//	for chunk, err := range agent.StreamSeq(ctx, UserMessage("hi")) {
//	    if err != nil { return err }
//	    fmt.Print(chunk.Content)
//	}
//
// 与 StreamRun（返回 <-chan StreamEvent）功能等价，
// 但使用 Go 1.23 的 range-over-func 语法，
// 错误处理更自然，无需手动检查 channel 关闭。
//
// 原有的 StreamRun 方法仍然保留，向后兼容。
func (a *ReActAgent) StreamSeq(ctx context.Context, input Message) iter.Seq2[llm.Chunk, error] {
	return func(yield func(llm.Chunk, error) bool) {
		ch, err := a.StreamRun(ctx, input)
		if err != nil {
			yield(llm.Chunk{}, err)
			return
		}
		for event := range ch {
			switch event.Type {
			case StreamEventError:
				yield(llm.Chunk{}, errors.New(event.Content))
				return
			case StreamEventComplete:
				yield(llm.Chunk{Done: true}, nil)
				return
			case StreamEventToken, StreamEventThought:
				if !yield(llm.Chunk{Content: event.Content}, nil) {
					return
				}
			}
			// tool调用等事件不暴露给 Chunk 迭代器
		}
	}
}
