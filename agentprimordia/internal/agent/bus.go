// Package agent 提供 ReAct 循环引擎和协议式微内核
// bus.go 提供消息总线的类型别名，保持向后兼容
package agent

import (
	"github.com/Gleamseekers/AgentPrimordia/internal/agent/bus"
)

// BusMessage 是消息总线传递的统一消息类型
// 类型别名保持向后兼容
type BusMessage = bus.BusMessage

// BusMessageType 是消息类型枚举
// 类型别名保持向后兼容
type BusMessageType = bus.BusMessageType

// BusMessageHandler 是消息处理函数类型
// 类型别名保持向后兼容
type BusMessageHandler = bus.BusMessageHandler

// MessageBus 是消息总线接口
// 类型别名保持向后兼容
type MessageBus = bus.MessageBus

// LocalMessageBus 是进程内消息总线实现
// 类型别名保持向后兼容
type LocalMessageBus = bus.LocalMessageBus

// 消息类型常量
const (
	BusMsgTaskRequest  = bus.BusMsgTaskRequest
	BusMsgTaskResult   = bus.BusMsgTaskResult
	BusMsgQuery        = bus.BusMsgQuery
	BusMsgResponse     = bus.BusMsgResponse
	BusMsgHandoff      = bus.BusMsgHandoff
	BusMsgBroadcast    = bus.BusMsgBroadcast
	BusMsgStatusUpdate = bus.BusMsgStatusUpdate
	BusMsgNotify       = bus.BusMsgNotify
)

// NewLocalMessageBus 创建本地消息总线实例
func NewLocalMessageBus() *LocalMessageBus {
	return bus.NewLocalMessageBus()
}
