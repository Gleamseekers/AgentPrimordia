// react_reflect.go — R1.4 Reflection 接入（G1-2）
// 在 ReAct Agent 完成路径（无tool调用）前，对最终输出进行反思与改进。
package agent

import (
	"context"
	"strings"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/reflection"
)

// reflectionSeverityOrder 严重度排序（高 → 低）
// 用于阈值比较：实际严重度 idx <= 阈值 idx 时触发改进
var reflectionSeverityOrder = map[reflection.Severity]int{
	reflection.SeverityLow:      1,
	reflection.SeverityMedium:   2,
	reflection.SeverityHigh:     3,
	reflection.SeverityCritical: 4,
}

// reflectAndImprove 对最终输出进行反思，必要时改进。
//
// 行为契约：
//   - 当 reflector == nil 或 content 为空时直接返回 content, nil（无副作用）
//   - 仅当 Critique.Severity >= ReflectionSeverityThreshold 时才触发 Improve
//   - 任何一步出错都仅记录日志并返回原 content，不阻断 Agent 完成
//
// 这是 R1.4 G1-2 闭环的接入点。在 runLoop "无tool调用" 分支前调用。
func (a *ReActAgent) reflectAndImprove(ctx context.Context, content string) (string, error) {
	reflector := a.getReflectorOrNil()
	if reflector == nil || content == "" {
		return content, nil
	}

	critique, err := reflector.Critique(ctx, content)
	if err != nil {
		a.logger.Warn("Reflection critique 失败，使用原内容", "error", err)
		return content, nil
	}

	threshold := a.config.ReflectionSeverityThreshold
	if threshold == "" {
		threshold = string(reflection.SeverityHigh)
	}
	if !shouldImprove(critique.Severity, reflection.Severity(threshold)) {
		return content, nil
	}

	improved, err := reflector.Improve(ctx, content, critique)
	if err != nil {
		a.logger.Warn("Reflection improve 失败，使用原内容", "error", err)
		return content, nil
	}
	if strings.TrimSpace(improved) == "" {
		return content, nil
	}
	a.logger.Debug("Reflection 改进了输出",
		"severity", critique.Severity,
		"original_len", len(content),
		"improved_len", len(improved),
	)
	return improved, nil
}

// getReflectorOrNil 通过 capCache 获取 reflector（nil-safe）
func (a *ReActAgent) getReflectorOrNil() reflection.Reflector {
	if a.capCache == nil {
		return a.getReflector()
	}
	return a.capCache.reflector
}

// shouldImprove 判断 critique 严重度是否达到触发改进的阈值
func shouldImprove(actual, threshold reflection.Severity) bool {
	actualRank, ok1 := reflectionSeverityOrder[actual]
	thresholdRank, ok2 := reflectionSeverityOrder[threshold]
	if !ok1 || !ok2 {
		// 未知严重度保守处理：仅 critical 触发
		return actual == reflection.SeverityCritical
	}
	return actualRank >= thresholdRank
}
