// Package controller - HPA 行为配置
//
// 文件：hpa.go
// 作用：将 CRD 中的 HPABehaviorSpec 转换为 autoscalingv2.HorizontalPodAutoscalerBehavior，
//
//	并提供默认值兜底，确保即使用户不显式配置也能获得合理的扩缩容策略。
//
// 设计：
//   - 默认 ScaleDown：5 分钟稳定窗口 + 每 60s 最多缩容 25%
//   - 默认 ScaleUp：30 秒稳定窗口 + 每 30s 最多扩容 100%
//   - 用户配置时按用户值覆盖
package controller

import (
	autoscalingv2 "k8s.io/api/autoscaling/v2"

	agentv1 "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/operator/api/v1"
)

// defaultScaleDownStabilizationSeconds 默认缩容稳定窗口（秒）
const defaultScaleDownStabilizationSeconds int32 = 300

// defaultScaleUpStabilizationSeconds 默认扩容稳定窗口（秒）
const defaultScaleUpStabilizationSeconds int32 = 30

// defaultScaleDownPercent 单次缩容比例（百分比）
const defaultScaleDownPercent int32 = 25

// defaultScaleUpPercent 单次扩容比例（百分比）
const defaultScaleUpPercent int32 = 100

// buildHPABehavior 从 CRD 行为配置构造 K8s HPA Behavior
//
// nil 输入时返回默认值（缩容 5min/25%、扩容 30s/100%）。
func buildHPABehavior(spec *agentv1.HPABehaviorSpec) *autoscalingv2.HorizontalPodAutoscalerBehavior {
	if spec == nil {
		return defaultHPABehavior()
	}
	return &autoscalingv2.HorizontalPodAutoscalerBehavior{
		ScaleDown: buildScalingRules(spec.ScaleDown, defaultScaleDownStabilizationSeconds, defaultScaleDownPercent),
		ScaleUp:   buildScalingRules(spec.ScaleUp, defaultScaleUpStabilizationSeconds, defaultScaleUpPercent),
	}
}

// defaultHPABehavior 返回默认 HPA Behavior（缩容 5min/25%、扩容 30s/100%）
func defaultHPABehavior() *autoscalingv2.HorizontalPodAutoscalerBehavior {
	return &autoscalingv2.HorizontalPodAutoscalerBehavior{
		ScaleDown: buildScalingRules(nil, defaultScaleDownStabilizationSeconds, defaultScaleDownPercent),
		ScaleUp:   buildScalingRules(nil, defaultScaleUpStabilizationSeconds, defaultScaleUpPercent),
	}
}

// buildScalingRules 转换单方向的扩缩规则
//
// 参数：
//   - spec：CRD 中配置（可为 nil）
//   - defaultStabWindow：默认稳定窗口
//   - defaultPercent：默认单次步进百分比
func buildScalingRules(spec *agentv1.HPAScalingRulesSpec, defaultStabWindow, defaultPercent int32) *autoscalingv2.HPAScalingRules {
	rules := &autoscalingv2.HPAScalingRules{}

	// 稳定窗口
	if spec != nil && spec.StabilizationWindowSeconds != nil {
		rules.StabilizationWindowSeconds = spec.StabilizationWindowSeconds
	} else {
		window := defaultStabWindow
		rules.StabilizationWindowSeconds = &window
	}

	// 策略列表
	if spec != nil && len(spec.Policies) > 0 {
		rules.Policies = make([]autoscalingv2.HPAScalingPolicy, 0, len(spec.Policies))
		for _, p := range spec.Policies {
			rules.Policies = append(rules.Policies, autoscalingv2.HPAScalingPolicy{
				Type:          autoscalingv2.HPAScalingPolicyType(p.Type),
				Value:         p.Value,
				PeriodSeconds: p.PeriodSeconds,
			})
		}
	} else {
		// 默认：每次最多缩/扩 defaultPercent%
		ruleType := autoscalingv2.PercentScalingPolicy
		period := int32(60)
		if defaultPercent == defaultScaleUpPercent {
			period = 30
		}
		rules.Policies = []autoscalingv2.HPAScalingPolicy{
			{Type: ruleType, Value: defaultPercent, PeriodSeconds: period},
		}
	}

	// 选择策略
	if spec != nil && spec.SelectPolicy != nil {
		sp := autoscalingv2.ScalingPolicySelect(*spec.SelectPolicy)
		rules.SelectPolicy = &sp
	}

	return rules
}

// hpaBehaviorEqual 判断两个 HPA Behavior 是否等价
//
// HPA controller 比较时通过此函数避免无意义的 update。
func hpaBehaviorEqual(a, b *autoscalingv2.HorizontalPodAutoscalerBehavior) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if !scalingRulesEqual(a.ScaleDown, b.ScaleDown) {
		return false
	}
	if !scalingRulesEqual(a.ScaleUp, b.ScaleUp) {
		return false
	}
	return true
}

func scalingRulesEqual(a, b *autoscalingv2.HPAScalingRules) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if !int32PtrEqual(a.StabilizationWindowSeconds, b.StabilizationWindowSeconds) {
		return false
	}
	if !selectPolicyEqual(a.SelectPolicy, b.SelectPolicy) {
		return false
	}
	if len(a.Policies) != len(b.Policies) {
		return false
	}
	for i := range a.Policies {
		if a.Policies[i] != b.Policies[i] {
			return false
		}
	}
	return true
}

func int32PtrEqual(a, b *int32) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func selectPolicyEqual(a, b *autoscalingv2.ScalingPolicySelect) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}
