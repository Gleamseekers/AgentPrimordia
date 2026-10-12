// Stability: Experimental — v3.0.0 新增前沿能力，API 可能随使用场景演进而调整。
package ap

import (
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/tool_learning"
)

// ToolLearner 定义tool学习能力接口
// Deprecated: 无外部采用证据的周边能力（工具学习 re-export），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type ToolLearner = tool_learning.ToolLearner

// Episode 是tool学习使用的记忆条目
// Deprecated: 无外部采用证据的周边能力（工具学习 re-export），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type TLEpisode = tool_learning.Episode

// BestPractice 最佳实践
// Deprecated: 无外部采用证据的周边能力（工具学习 re-export），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type BestPractice = tool_learning.BestPractice

// Suggestion 改进建议
// Deprecated: 无外部采用证据的周边能力（工具学习 re-export），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type Suggestion = tool_learning.Suggestion

// ToolUsageRecord tool使用记录
// Deprecated: 无外部采用证据的周边能力（工具学习 re-export），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type ToolUsageRecord = tool_learning.ToolUsageRecord

// MemoryToolLearner 基于 MemoryStore 的tool学习器
// Deprecated: 无外部采用证据的周边能力（工具学习 re-export），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type MemoryToolLearner = tool_learning.MemoryToolLearner

// ToolLearningMemoryStore 定义tool学习所需的最小记忆存储接口
// Deprecated: 无外部采用证据的周边能力（工具学习 re-export），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type ToolLearningMemoryStore = tool_learning.MemoryStore

var (
	// NewMemoryToolLearner 创建 MemoryToolLearner 实例
	// Deprecated: 无外部采用证据的周边能力（工具学习 re-export），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewMemoryToolLearner = tool_learning.NewMemoryToolLearner
)
