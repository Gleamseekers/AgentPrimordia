// Stability: Stable — 状态检查点持久化。
package ap

import (
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/persist"
)

// CheckpointStore 是状态持久化接口，支持 Agent 状态的保存和恢复
type CheckpointStore = persist.CheckpointStore

// AgentState 是 Agent 的持久化状态，包含消息历史、轮次和状态信息
type AgentState = persist.AgentState

// SQLiteCheckpointStore 是基于 SQLite 的检查点存储实现
type SQLiteCheckpointStore = persist.SQLiteCheckpointStore

var (
	// NewSQLiteCheckpointStore 创建基于 SQLite 的检查点存储实例
	NewSQLiteCheckpointStore = persist.NewSQLiteCheckpointStore
	// InMemoryCheckpointStore 创建内存模式的检查点存储，适用于测试
	InMemoryCheckpointStore = persist.InMemoryCheckpointStore
	// UnmarshalAgentState 从 JSON 字节反序列化 Agent 状态
	UnmarshalAgentState = persist.UnmarshalAgentState
)
