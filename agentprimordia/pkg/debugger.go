// Stability: Experimental — 调试器与可视化tool，API 可能随使用场景调整。
package ap

import (
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/debugger"
)

// DebugServer 是调试 HTTP 服务器，提供实时事件记录和查询
// Deprecated: 无外部采用证据的周边能力（调试 / 可视化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type DebugServer = debugger.DebugServer

// Visualizer 是调试可视化tool，支持渲染 Memory 快照和 Agent 生命周期
// Deprecated: 无外部采用证据的周边能力（调试 / 可视化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type Visualizer = debugger.Visualizer

// MemorySnapshot 是 Memory 状态快照，包含总条目数、热门会话和最近事件
// Deprecated: 无外部采用证据的周边能力（调试 / 可视化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type MemorySnapshot = debugger.MemorySnapshot

// SessionInfo 是会话信息，包含会话 ID 和条目数
// Deprecated: 无外部采用证据的周边能力（调试 / 可视化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type SessionInfo = debugger.SessionInfo

// RecentEvent 是最近事件记录，包含时间、角色和内容
// Deprecated: 无外部采用证据的周边能力（调试 / 可视化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type RecentEvent = debugger.RecentEvent

// LifecycleStep 是 Agent 生命周期步骤，包含状态、时间戳和消息
// Deprecated: 无外部采用证据的周边能力（调试 / 可视化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type LifecycleStep = debugger.LifecycleStep

var (
	// NewDebugServer 创建调试 HTTP 服务器实例
	// Deprecated: 无外部采用证据的周边能力（调试 / 可视化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewDebugServer = debugger.NewDebugServer
	// NewVisualizer 创建可视化tool实例
	// Deprecated: 无外部采用证据的周边能力（调试 / 可视化），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewVisualizer = debugger.NewVisualizer
)
