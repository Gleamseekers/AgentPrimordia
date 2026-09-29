// Stability: Experimental — 调试器与可视化tool，API 可能随使用场景调整。
package ap

import (
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/debugger"
)

// DebugServer 是调试 HTTP 服务器，提供实时事件记录和查询
type DebugServer = debugger.DebugServer

// Visualizer 是调试可视化tool，支持渲染 Memory 快照和 Agent 生命周期
type Visualizer = debugger.Visualizer

// MemorySnapshot 是 Memory 状态快照，包含总条目数、热门会话和最近事件
type MemorySnapshot = debugger.MemorySnapshot

// SessionInfo 是会话信息，包含会话 ID 和条目数
type SessionInfo = debugger.SessionInfo

// RecentEvent 是最近事件记录，包含时间、角色和内容
type RecentEvent = debugger.RecentEvent

// LifecycleStep 是 Agent 生命周期步骤，包含状态、时间戳和消息
type LifecycleStep = debugger.LifecycleStep

var (
	// NewDebugServer 创建调试 HTTP 服务器实例
	NewDebugServer = debugger.NewDebugServer
	// NewVisualizer 创建可视化tool实例
	NewVisualizer = debugger.NewVisualizer
)
