// event_const.go — 事件常量别名，委托到 core 子包
package agent

import (
	"github.com/Gleamseekers/AgentPrimordia/internal/agent/core"
)

// 事件类型常量（委托到 core 子包）
const (
	EventAgentStart  = core.EventAgentStart
	EventAgentStop   = core.EventAgentStop
	EventAgentPanic  = core.EventAgentPanic
	EventAgentError  = core.EventAgentError
	EventAgentResume = core.EventAgentResume
	EventTurnStart   = core.EventTurnStart
	EventTurnEnd     = core.EventTurnEnd
	EventLLMCall     = core.EventLLMCall
	EventLLMResponse = core.EventLLMResponse
	EventToolCall    = core.EventToolCall
	EventToolResult  = core.EventToolResult
)
