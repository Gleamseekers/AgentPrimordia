// tuner.go — 数据驱动参数调优器
package optimize

import (
	"context"
	"fmt"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/tools/intelligence"
)

// DataDrivenTuner 数据驱动调优器（基于画像提出参数调整建议）
type DataDrivenTuner struct {
	// 阈值配置
	MinSuccessRate float64       // 成功率阈值（低于此值建议重试）
	MaxAvgDuration time.Duration // 平均延迟阈值（高于此值建议增大超时）
}

// NewDataDrivenTuner 创建调优器（默认阈值）
func NewDataDrivenTuner() *DataDrivenTuner {
	return &DataDrivenTuner{
		MinSuccessRate: 0.7,
		MaxAvgDuration: 5 * time.Second,
	}
}

// SuggestTuning 基于画像生成调优建议
// 低成功率 → 建议重试；高延迟 → 建议增大超时；
// 成功率 >= MinSuccessRate 且延迟正常时返回 nil
func (t *DataDrivenTuner) SuggestTuning(_ context.Context, toolName string, profile *intelligence.ToolProfile) (*intelligence.TuningSuggestion, error) {
	if profile == nil {
		return nil, fmt.Errorf("画像为空")
	}

	// 成功率低于阈值 → 建议重试
	if profile.SuccessRate < t.MinSuccessRate {
		return &intelligence.TuningSuggestion{
			ToolName:     toolName,
			Parameter:    "retry",
			CurrentVal:   "0",
			SuggestedVal: "2",
			Confidence:   1.0 - profile.SuccessRate,
			Reason:       fmt.Sprintf("成功率 %.1f%% 低于阈值 %.1f%%，建议启用重试", profile.SuccessRate*100, t.MinSuccessRate*100),
		}, nil
	}

	// 平均延迟超过阈值 → 建议增大超时
	if profile.AvgDuration > t.MaxAvgDuration {
		suggestedTimeout := profile.AvgDuration * 2
		return &intelligence.TuningSuggestion{
			ToolName:     toolName,
			Parameter:    "timeout",
			CurrentVal:   profile.AvgDuration.String(),
			SuggestedVal: suggestedTimeout.String(),
			Confidence:   0.8,
			Reason:       fmt.Sprintf("平均延迟 %v 超过阈值 %v，建议增大超时", profile.AvgDuration, t.MaxAvgDuration),
		}, nil
	}

	// 表现良好，无需调优
	return nil, nil
}

// ApplyTuning 应用调优建议（当前为空操作，实际应注册到工具配置）
func (t *DataDrivenTuner) ApplyTuning(_ context.Context, _ string, _ *intelligence.TuningSuggestion) error {
	// 当前实现为空操作，后续可接入配置热加载
	return nil
}
