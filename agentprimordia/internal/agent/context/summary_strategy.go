// summary_strategy.go — 摘要压缩策略（v3.4-2）
//
// 相比 DefaultStrategy 的纯滑动窗口，本策略在超出窗口时：
//  1. 固定保留系统消息与首个用户目标（防止长任务丢失原始目标）
//  2. 将中间历史折叠为一条「已压缩」摘要标记消息
//  3. 保留最近 N 条
//
// 避免长 pipeline 中 context 无界膨胀的同时不丢失任务意图。
package context

import (
	"fmt"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/core"
)

// SummarizingStrategy 摘要压缩策略
type SummarizingStrategy struct {
	KeepLast int
}

// NewSummarizingStrategy 创建摘要压缩策略
func NewSummarizingStrategy(keepLast int) *SummarizingStrategy {
	if keepLast <= 0 {
		keepLast = 80
	}
	return &SummarizingStrategy{KeepLast: keepLast}
}

// Trim 实现摘要压缩裁剪
func (s *SummarizingStrategy) Trim(messages []core.Message, maxMessages int) []core.Message {
	if len(messages) == 0 {
		return messages
	}
	effectiveMax := maxMessages
	if effectiveMax <= 0 {
		effectiveMax = s.KeepLast
	}
	if len(messages) <= effectiveMax {
		return messages
	}

	// 固定保留项：系统消息 + 首个用户目标
	var sys, goal *core.Message
	for i := range messages {
		if messages[i].Role == core.RoleSystem && sys == nil {
			m := messages[i]
			sys = &m
		}
		if messages[i].Role == core.RoleUser && goal == nil {
			m := messages[i]
			goal = &m
		}
	}
	preserved := 0
	if sys != nil {
		preserved++
	}
	if goal != nil {
		preserved++
	}
	if preserved > effectiveMax {
		preserved = effectiveMax
	}

	result := make([]core.Message, 0, effectiveMax+1)
	// 固定项始终保留（极端小窗口时由末尾截断兜底）
	if sys != nil {
		result = append(result, *sys)
	}
	if goal != nil {
		result = append(result, *goal)
	}

	// 中间历史折叠为摘要标记
	dropped := len(messages) - effectiveMax
	if dropped > 0 {
		result = append(result, core.Message{
			Role:    core.RoleSystem,
			Content: fmt.Sprintf("[上下文已压缩：省略 %d 条历史消息]", dropped),
		})
	}

	// 保留最近 (effectiveMax - preserved) 条（跳过已保留项）
	recent := effectiveMax - preserved
	if recent < 0 {
		recent = 0
	}
	start := len(messages) - recent
	if start < 0 {
		start = 0
	}
	for i := start; i < len(messages); i++ {
		m := messages[i]
		if sys != nil && m.Role == sys.Role && m.Content == sys.Content {
			continue
		}
		if goal != nil && m.Role == goal.Role && m.Content == goal.Content {
			continue
		}
		result = append(result, m)
	}

	if len(result) > effectiveMax {
		result = result[:effectiveMax]
	}
	return result
}
