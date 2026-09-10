package a2a

import (
	"context"
	"time"
)

// TaskExecutor 任务执行器接口。
// 将 A2A 开放协议的任务请求桥接到实际的 agent 执行层。
type TaskExecutor interface {
	// Execute 执行任务，返回更新后的任务状态。
	Execute(ctx context.Context, task *OpenTask) (*OpenTask, error)
	// Cancel 取消正在执行的任务。
	Cancel(ctx context.Context, taskID string) error
}

// SimpleTaskExecutor 基于回调函数的简单执行器实现。
type SimpleTaskExecutor struct {
	handler func(ctx context.Context, task *OpenTask) (*OpenTask, error)
	cancel  func(ctx context.Context, taskID string) error
}

// NewSimpleTaskExecutor 创建简单任务执行器。
func NewSimpleTaskExecutor(
	handler func(ctx context.Context, task *OpenTask) (*OpenTask, error),
) *SimpleTaskExecutor {
	return &SimpleTaskExecutor{handler: handler}
}

// WithCancel 设置取消回调。
func (e *SimpleTaskExecutor) WithCancel(cancel func(ctx context.Context, taskID string) error) *SimpleTaskExecutor {
	e.cancel = cancel
	return e
}

// Execute 执行任务。
func (e *SimpleTaskExecutor) Execute(ctx context.Context, task *OpenTask) (*OpenTask, error) {
	if e.handler == nil {
		task.Status = OpenTaskStatus{State: OpenTaskFailed, Timestamp: time.Now()}
		return task, nil
	}
	return e.handler(ctx, task)
}

// Cancel 取消任务。
func (e *SimpleTaskExecutor) Cancel(ctx context.Context, taskID string) error {
	if e.cancel != nil {
		return e.cancel(ctx, taskID)
	}
	return nil
}

// EchoTaskExecutor 回声测试执行器，直接将输入消息作为输出返回。
type EchoTaskExecutor struct{}

// NewEchoTaskExecutor 创建回声测试执行器。
func NewEchoTaskExecutor() *EchoTaskExecutor {
	return &EchoTaskExecutor{}
}

// Execute 回声执行：将输入消息原样返回。
func (e *EchoTaskExecutor) Execute(ctx context.Context, task *OpenTask) (*OpenTask, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// 提取输入文本
	var inputText string
	for _, msg := range task.Messages {
		if msg.Role == "user" {
			for _, part := range msg.Parts {
				if part.Type == "text" {
					inputText += part.Text
				}
			}
		}
	}

	// 构建响应
	task.Status = OpenTaskStatus{State: OpenTaskCompleted, Timestamp: time.Now()}
	task.Artifacts = []OpenArtifact{
		{
			Name:  "response",
			Parts: []OpenPart{{Type: "text", Text: "Echo: " + inputText}},
			Index: 0,
		},
	}
	return task, nil
}

// Cancel 回声执行器不支持取消。
func (e *EchoTaskExecutor) Cancel(ctx context.Context, taskID string) error {
	return nil
}
