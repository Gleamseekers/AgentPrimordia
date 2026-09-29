// Package controller 测试 - hpa.go
//
// 覆盖：
//   - defaultHPABehavior / buildHPABehavior 默认值与覆盖逻辑
//   - buildScalingRules 稳定窗口 / policies / selectPolicy
//   - hpaBehaviorEqual / scalingRulesEqual 比较逻辑
//   - ensureHPA 集成 Behavior 字段后写回 HPA
package controller

import (
	"context"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrlfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	agentv1 "github.com/Gleamseekers/AgentPrimordia/operator/api/v1"
)

// ---- defaultHPABehavior ----

func TestDefaultHPABehavior(t *testing.T) {
	b := defaultHPABehavior()
	if b == nil {
		t.Fatal("defaultHPABehavior 不应返回 nil")
	}
	// ScaleDown 5min + 25%/60s
	if b.ScaleDown.StabilizationWindowSeconds == nil || *b.ScaleDown.StabilizationWindowSeconds != 300 {
		t.Errorf("ScaleDown 稳定窗口 = %v, want 300", b.ScaleDown.StabilizationWindowSeconds)
	}
	if len(b.ScaleDown.Policies) != 1 || b.ScaleDown.Policies[0].Value != 25 {
		t.Errorf("ScaleDown policy = %+v, want Value=25", b.ScaleDown.Policies)
	}
	// ScaleUp 30s + 100%/30s
	if b.ScaleUp.StabilizationWindowSeconds == nil || *b.ScaleUp.StabilizationWindowSeconds != 30 {
		t.Errorf("ScaleUp 稳定窗口 = %v, want 30", b.ScaleUp.StabilizationWindowSeconds)
	}
	if len(b.ScaleUp.Policies) != 1 || b.ScaleUp.Policies[0].Value != 100 {
		t.Errorf("ScaleUp policy = %+v, want Value=100", b.ScaleUp.Policies)
	}
}

// ---- buildHPABehavior ----

func TestBuildHPABehavior_Nil(t *testing.T) {
	b := buildHPABehavior(nil)
	if b == nil {
		t.Fatal("nil 输入应返回默认 behavior")
	}
	if b.ScaleDown.StabilizationWindowSeconds == nil || *b.ScaleDown.StabilizationWindowSeconds != 300 {
		t.Errorf("默认 ScaleDown 窗口错")
	}
}

func TestBuildHPABehavior_Partial(t *testing.T) {
	// 用户只配置了 ScaleDown
	window := int32(120)
	spec := &agentv1.HPABehaviorSpec{
		ScaleDown: &agentv1.HPAScalingRulesSpec{
			StabilizationWindowSeconds: &window,
		},
	}
	b := buildHPABehavior(spec)
	if b.ScaleDown.StabilizationWindowSeconds == nil || *b.ScaleDown.StabilizationWindowSeconds != 120 {
		t.Errorf("自定义窗口 = %v, want 120", b.ScaleDown.StabilizationWindowSeconds)
	}
	if b.ScaleUp.StabilizationWindowSeconds == nil || *b.ScaleUp.StabilizationWindowSeconds != 30 {
		t.Errorf("ScaleUp 应回退到默认 30s")
	}
}

// ---- buildScalingRules ----

func TestBuildScalingRules_UserPolicies(t *testing.T) {
	window := int32(600)
	spec := &agentv1.HPAScalingRulesSpec{
		StabilizationWindowSeconds: &window,
		Policies: []agentv1.HPAScalingPolicySpec{
			{Type: "Pods", Value: 2, PeriodSeconds: 30},
			{Type: "Percent", Value: 50, PeriodSeconds: 60},
		},
		SelectPolicy: ptrString("Min"),
	}
	rules := buildScalingRules(spec, 300, 25)
	if rules.StabilizationWindowSeconds == nil || *rules.StabilizationWindowSeconds != 600 {
		t.Errorf("窗口 = %v, want 600", rules.StabilizationWindowSeconds)
	}
	if len(rules.Policies) != 2 {
		t.Fatalf("应有 2 条策略，实际 %d", len(rules.Policies))
	}
	if rules.Policies[0].Type != autoscalingv2.PodsScalingPolicy || rules.Policies[0].Value != 2 {
		t.Errorf("策略 0 错: %+v", rules.Policies[0])
	}
	if rules.Policies[1].Type != autoscalingv2.PercentScalingPolicy || rules.Policies[1].Value != 50 {
		t.Errorf("策略 1 错: %+v", rules.Policies[1])
	}
	if rules.SelectPolicy == nil || *rules.SelectPolicy != autoscalingv2.ScalingPolicySelect("Min") {
		t.Errorf("SelectPolicy = %v, want Min", rules.SelectPolicy)
	}
}

func TestBuildScalingRules_Defaults(t *testing.T) {
	rules := buildScalingRules(nil, 300, 25)
	if rules.StabilizationWindowSeconds == nil || *rules.StabilizationWindowSeconds != 300 {
		t.Errorf("默认窗口 = %v, want 300", rules.StabilizationWindowSeconds)
	}
	if len(rules.Policies) != 1 || rules.Policies[0].Value != 25 {
		t.Errorf("默认 policy 错: %+v", rules.Policies)
	}
}

func TestBuildScalingRules_EmptyPolicies(t *testing.T) {
	// 用户给了空策略列表 -> 应使用默认值（不是禁用）
	rules := buildScalingRules(&agentv1.HPAScalingRulesSpec{}, 300, 25)
	if len(rules.Policies) != 1 || rules.Policies[0].Value != 25 {
		t.Errorf("空策略应回退到默认，实际 %+v", rules.Policies)
	}
}

// ---- hpaBehaviorEqual ----

func TestHPABehaviorEqual_BothNil(t *testing.T) {
	if !hpaBehaviorEqual(nil, nil) {
		t.Errorf("双 nil 应判等")
	}
}

func TestHPABehaviorEqual_DefaultVsNil(t *testing.T) {
	a := defaultHPABehavior()
	if hpaBehaviorEqual(a, nil) {
		t.Errorf("一个非 nil 一个 nil 应判不等")
	}
}

func TestHPABehaviorEqual_Same(t *testing.T) {
	a := defaultHPABehavior()
	b := defaultHPABehavior()
	if !hpaBehaviorEqual(a, b) {
		t.Errorf("两个默认 behavior 应判等")
	}
}

func TestHPABehaviorEqual_DifferentWindow(t *testing.T) {
	a := defaultHPABehavior()
	window := int32(60)
	b := defaultHPABehavior()
	b.ScaleDown.StabilizationWindowSeconds = &window
	if hpaBehaviorEqual(a, b) {
		t.Errorf("不同窗口应判不等")
	}
}

func TestHPABehaviorEqual_DifferentPolicyValue(t *testing.T) {
	a := defaultHPABehavior()
	b := defaultHPABehavior()
	b.ScaleDown.Policies[0].Value = 50
	if hpaBehaviorEqual(a, b) {
		t.Errorf("不同 policy value 应判不等")
	}
}

func TestScalingRulesEqual_DifferentLen(t *testing.T) {
	a := defaultHPABehavior().ScaleDown
	b := &autoscalingv2.HPAScalingRules{
		StabilizationWindowSeconds: a.StabilizationWindowSeconds,
		Policies:                   append([]autoscalingv2.HPAScalingPolicy{}, a.Policies...),
		SelectPolicy:               a.SelectPolicy,
	}
	b.Policies = append(b.Policies, autoscalingv2.HPAScalingPolicy{Type: autoscalingv2.PodsScalingPolicy, Value: 1, PeriodSeconds: 30})
	if scalingRulesEqual(a, b) {
		t.Errorf("不同长度应判不等")
	}
}

// ---- ensureHPA 集成 Behavior ----

func makeAgentForHPA(name string, min, max, target int32, behavior *agentv1.HPABehaviorSpec) *agentv1.AgentDeployment {
	return &agentv1.AgentDeployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", UID: types.UID(name + "-uid")},
		Spec: agentv1.AgentDeploymentSpec{
			Replicas: 2,
			Template: agentv1.AgentTemplateSpec{Provider: "openai", Model: "gpt-4o"},
			Autoscaling: &agentv1.AutoscalingSpec{
				MinReplicas:           min,
				MaxReplicas:           max,
				TargetConcurrentTasks: target,
				Behavior:              behavior,
			},
		},
	}
}

func TestEnsureHPA_WritesBehavior(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = agentv1.AddToScheme(scheme)
	deploy := makeAgentForHPA("bhv", 2, 10, 50, nil)
	cli := ctrlfake.NewClientBuilder().WithScheme(scheme).WithObjects(deploy).Build()
	r := &AgentDeploymentReconciler{Client: cli, Scheme: scheme}

	if err := r.ensureHPA(context.Background(), deploy); err != nil {
		t.Fatalf("ensureHPA failed: %v", err)
	}

	var hpa autoscalingv2.HorizontalPodAutoscaler
	if err := cli.Get(context.Background(), types.NamespacedName{Name: "bhv-hpa", Namespace: "default"}, &hpa); err != nil {
		t.Fatalf("HPA not found: %v", err)
	}
	if hpa.Spec.Behavior == nil {
		t.Fatal("HPA Behavior 应被设置")
	}
	if hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds == nil || *hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds != 300 {
		t.Errorf("默认 ScaleDown 窗口 = %v, want 300", hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds)
	}
}

func TestEnsureHPA_UpdatesBehavior(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = agentv1.AddToScheme(scheme)
	deploy := makeAgentForHPA("bhvu", 2, 10, 50, nil)

	// 预创建 HPA（旧 Behavior）
	existing := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "bhvu-hpa", Namespace: "default"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MinReplicas: ptrInt32(2),
			MaxReplicas: 10,
			Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleDown: &autoscalingv2.HPAScalingRules{
					StabilizationWindowSeconds: ptrInt32(60),
				},
			},
		},
	}
	cli := ctrlfake.NewClientBuilder().WithScheme(scheme).WithObjects(deploy, existing).Build()
	r := &AgentDeploymentReconciler{Client: cli, Scheme: scheme}

	if err := r.ensureHPA(context.Background(), deploy); err != nil {
		t.Fatalf("ensureHPA failed: %v", err)
	}

	var hpa autoscalingv2.HorizontalPodAutoscaler
	if err := cli.Get(context.Background(), types.NamespacedName{Name: "bhvu-hpa", Namespace: "default"}, &hpa); err != nil {
		t.Fatalf("HPA not found: %v", err)
	}
	// Behavior 应被更新为默认 300s
	if hpa.Spec.Behavior == nil || hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds == nil || *hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds != 300 {
		t.Errorf("Behavior 未更新: %+v", hpa.Spec.Behavior)
	}
}

// ---- helpers ----

func ptrString(s string) *string {
	return &s
}

func ptrInt32(v int32) *int32 {
	return &v
}
