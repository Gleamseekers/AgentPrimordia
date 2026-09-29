// hitl.go — hitl 子包的类型别名，保持向后兼容
package agent

import (
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/hitl"
)

// ErrHumanChannelClosed human input channel closed错误
var ErrHumanChannelClosed = hitl.ErrHumanChannelClosed

// InterruptReason 中断原因
// 类型别名保持向后兼容
type InterruptReason = hitl.InterruptReason

// 中断原因常量
const (
	InterruptToolConfirm   = hitl.InterruptToolConfirm
	InterruptDecisionPoint = hitl.InterruptDecisionPoint
	InterruptBudgetExceed  = hitl.InterruptBudgetExceed
	InterruptCustom        = hitl.InterruptCustom
)

// InterruptPoint 中断点配置
// 类型别名保持向后兼容
type InterruptPoint = hitl.InterruptPoint

// InterruptRequest 中断请求（Agent 发出）
// 类型别名保持向后兼容
type InterruptRequest = hitl.InterruptRequest

// HumanResponse 人类响应
// 类型别名保持向后兼容
type HumanResponse = hitl.HumanResponse

// HITLConfig 人机协作配置
// 类型别名保持向后兼容
type HITLConfig = hitl.HITLConfig

// HITLManager 人机协作管理器
// 类型别名保持向后兼容
type HITLManager = hitl.HITLManager

// NewHITLManager 创建人机协作管理器
// 委托到 hitl 子包，保持向后兼容
func NewHITLManager(config HITLConfig) *HITLManager {
	return hitl.NewHITLManager(config)
}
