// Stability: Experimental — v3.0.0 新增前沿能力，API 可能随使用场景演进而调整。
package ap

import (
	"github.com/Gleamseekers/AgentPrimordia/internal/orchestration"
)

// Debater 辩论参与者接口
type Debater = orchestration.Debater

// Argument 单个论点
type Argument = orchestration.Argument

// DebateResult 辩论结果
type DebateResult = orchestration.DebateResult

// DebateConfig 辩论配置
type DebateConfig = orchestration.DebateConfig

// Debate 辩论管理器
type Debate = orchestration.Debate

// DebateEvent 辩论事件
type DebateEvent = orchestration.DebateEvent

var (
	// NewDebate 创建新的辩论实例
	NewDebate = orchestration.NewDebate
)
