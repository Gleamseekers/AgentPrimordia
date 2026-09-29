// Stability: Experimental — v3.0.0 新增前沿能力，API 可能随使用场景演进而调整。
package ap

import (
	"github.com/Gleamseekers/AgentPrimordia/internal/agent/tool_learning"
)

// ToolLearner 定义tool学习能力接口
type ToolLearner = tool_learning.ToolLearner

// Episode 是tool学习使用的记忆条目
type TLEpisode = tool_learning.Episode

// BestPractice 最佳实践
type BestPractice = tool_learning.BestPractice

// Suggestion 改进建议
type Suggestion = tool_learning.Suggestion

// ToolUsageRecord tool使用记录
type ToolUsageRecord = tool_learning.ToolUsageRecord

// MemoryToolLearner 基于 MemoryStore 的tool学习器
type MemoryToolLearner = tool_learning.MemoryToolLearner

// ToolLearningMemoryStore 定义tool学习所需的最小记忆存储接口
type ToolLearningMemoryStore = tool_learning.MemoryStore

var (
	// NewMemoryToolLearner 创建 MemoryToolLearner 实例
	NewMemoryToolLearner = tool_learning.NewMemoryToolLearner
)
