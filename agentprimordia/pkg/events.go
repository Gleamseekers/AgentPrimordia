// Stability: Stable — 事件总线与订阅。
package ap

import (
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/events"
)

// Bus 是事件总线，支持发布/订阅模式的事件分发
type Bus = events.Bus

// Event 是事件结构，包含类型、来源、时间戳和负载
type Event = events.Event

// EventType 是事件类型标识
type EventType = events.EventType

// Subscriber 是事件订阅者，接收匹配类型的事件
type Subscriber = events.Subscriber

const (
	// EventAgentStart 表示 Agent 启动事件
	EventAgentStart = events.EventAgentStart
	// EventAgentStop 表示 Agent 停止事件
	EventAgentStop = events.EventAgentStop
	// EventAgentPanic 表示 Agent panic 恢复事件
	EventAgentPanic = events.EventAgentPanic
	// EventAgentError 表示 Agent 错误事件
	EventAgentError = events.EventAgentError
	// EventAgentResume 表示 Agent 从检查点恢复事件
	EventAgentResume = events.EventAgentResume
	// EventTurnStart 表示推理轮次开始事件
	EventTurnStart = events.EventTurnStart
	// EventTurnEnd 表示推理轮次结束事件
	EventTurnEnd = events.EventTurnEnd
	// EventToolCall 表示tool调用事件
	EventToolCall = events.EventToolCall
	// EventToolResult 表示tool结果事件
	EventToolResult = events.EventToolResult
	// EventLLMCall 表示 LLM 调用事件
	EventLLMCall = events.EventLLMCall
	// EventLLMResponse 表示 LLM 响应事件
	EventLLMResponse = events.EventLLMResponse
	// EventPoolDispatch 表示 Pool 任务分发事件
	EventPoolDispatch = events.EventPoolDispatch
	// EventPoolComplete 表示 Pool 任务完成事件
	EventPoolComplete = events.EventPoolComplete
)

// NewBus 创建事件总线实例
var NewBus = events.NewBus
