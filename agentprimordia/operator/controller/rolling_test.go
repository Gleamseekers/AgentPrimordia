// Package controller 测试 - rolling.go
//
// 覆盖：
//   - buildDeploymentStrategy 默认 RollingUpdate 配置
//   - applyTerminationLifecycle 注入 grace/preStop
//   - rollingStrategyEqual 比较逻辑
//   - allContainersHavePreStop / int64PtrEqual / hasPreStopHook
//   - ensureDeployment 集成 strategy + lifecycle 后写回 Deployment
package controller

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrlfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	agentv1 "github.com/Gleamseekers/AgentPrimordia/operator/api/v1"
)

// ---- buildDeploymentStrategy ----

func TestBuildDeploymentStrategy_Default(t *testing.T) {
	s := buildDeploymentStrategy()
	if s.Type != appsv1.RollingUpdateDeploymentStrategyType {
		t.Errorf("Type = %v, want RollingUpdate", s.Type)
	}
	if s.RollingUpdate == nil {
		t.Fatal("RollingUpdate 不应为 nil")
	}
	if s.RollingUpdate.MaxUnavailable == nil || s.RollingUpdate.MaxUnavailable.IntValue() != 1 {
		t.Errorf("MaxUnavailable = %v, want 1", s.RollingUpdate.MaxUnavailable)
	}
	if s.RollingUpdate.MaxSurge == nil || s.RollingUpdate.MaxSurge.IntValue() != 1 {
		t.Errorf("MaxSurge = %v, want 1", s.RollingUpdate.MaxSurge)
	}
}

// ---- applyTerminationLifecycle ----

func TestApplyTerminationLifecycle_SetsGraceAndPreStop(t *testing.T) {
	spec := &corev1.PodSpec{
		Containers: []corev1.Container{{Name: "app"}},
	}
	applyTerminationLifecycle(spec)
	if spec.TerminationGracePeriodSeconds == nil || *spec.TerminationGracePeriodSeconds != 30 {
		t.Errorf("Grace = %v, want 30", spec.TerminationGracePeriodSeconds)
	}
	if len(spec.Containers) != 1 {
		t.Fatalf("容器丢失")
	}
	c := spec.Containers[0]
	if c.Lifecycle == nil || c.Lifecycle.PreStop == nil {
		t.Fatal("PreStop 未注入")
	}
	if c.Lifecycle.PreStop.Exec == nil {
		t.Fatal("PreStop.Exec 应非 nil")
	}
}

func TestApplyTerminationLifecycle_PreservesExistingGrace(t *testing.T) {
	spec := &corev1.PodSpec{
		TerminationGracePeriodSeconds: ptrInt64(120),
		Containers:                    []corev1.Container{{Name: "app"}},
	}
	applyTerminationLifecycle(spec)
	if spec.TerminationGracePeriodSeconds == nil || *spec.TerminationGracePeriodSeconds != 120 {
		t.Errorf("已存在的 Grace 应被保留，实际 %v", spec.TerminationGracePeriodSeconds)
	}
}

func TestApplyTerminationLifecycle_PreservesExistingPreStop(t *testing.T) {
	customPreStop := &corev1.LifecycleHandler{Exec: &corev1.ExecAction{Command: []string{"my-hook"}}}
	spec := &corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Name:      "app",
				Lifecycle: &corev1.Lifecycle{PreStop: customPreStop},
			},
		},
	}
	applyTerminationLifecycle(spec)
	got := spec.Containers[0].Lifecycle.PreStop
	if got != customPreStop {
		t.Errorf("自定义 PreStop 应被保留（指针相等），实际被覆盖")
	}
}

func TestApplyTerminationLifecycle_Nil(t *testing.T) {
	applyTerminationLifecycle(nil)
	// 不应 panic
}

func TestApplyTerminationLifecycle_MultipleContainers(t *testing.T) {
	spec := &corev1.PodSpec{
		Containers: []corev1.Container{{Name: "a"}, {Name: "b"}, {Name: "c"}},
	}
	applyTerminationLifecycle(spec)
	for _, c := range spec.Containers {
		if c.Lifecycle == nil || c.Lifecycle.PreStop == nil {
			t.Errorf("容器 %s 未注入 PreStop", c.Name)
		}
	}
}

// ---- rollingStrategyEqual ----

func TestRollingStrategyEqual_Same(t *testing.T) {
	a := buildDeploymentStrategy()
	b := buildDeploymentStrategy()
	if !rollingStrategyEqual(a, b) {
		t.Errorf("相同 strategy 应判等")
	}
}

func TestRollingStrategyEqual_DifferentType(t *testing.T) {
	a := buildDeploymentStrategy()
	b := buildDeploymentStrategy()
	b.Type = appsv1.RecreateDeploymentStrategyType
	if rollingStrategyEqual(a, b) {
		t.Errorf("不同 Type 应判不等")
	}
}

func TestRollingStrategyEqual_DifferentMaxUnavailable(t *testing.T) {
	a := buildDeploymentStrategy()
	b := buildDeploymentStrategy()
	mu := intstr.FromInt(2)
	b.RollingUpdate.MaxUnavailable = &mu
	if rollingStrategyEqual(a, b) {
		t.Errorf("不同 MaxUnavailable 应判不等")
	}
}

func TestRollingStrategyEqual_NilRollingUpdate(t *testing.T) {
	a := appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType}
	b := appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType}
	if !rollingStrategyEqual(a, b) {
		t.Errorf("双 nil RollingUpdate 应判等")
	}
}

// ---- helpers ----

func TestAllContainersHavePreStop(t *testing.T) {
	spec := corev1.PodSpec{Containers: []corev1.Container{
		{Name: "a", Lifecycle: &corev1.Lifecycle{PreStop: &corev1.LifecycleHandler{}}},
		{Name: "b", Lifecycle: &corev1.Lifecycle{PreStop: &corev1.LifecycleHandler{}}},
	}}
	if !allContainersHavePreStop(spec) {
		t.Errorf("所有容器都有 PreStop 应返回 true")
	}
}

func TestAllContainersHavePreStop_Missing(t *testing.T) {
	spec := corev1.PodSpec{Containers: []corev1.Container{
		{Name: "a", Lifecycle: &corev1.Lifecycle{PreStop: &corev1.LifecycleHandler{}}},
		{Name: "b"}, // 缺 PreStop
	}}
	if allContainersHavePreStop(spec) {
		t.Errorf("缺 PreStop 时应返回 false")
	}
}

func TestInt64PtrEqual(t *testing.T) {
	a := int64(10)
	b := int64(10)
	c := int64(20)
	if !int64PtrEqual(&a, &b) {
		t.Errorf("相同值应判等")
	}
	if int64PtrEqual(&a, &c) {
		t.Errorf("不同值应判不等")
	}
	if !int64PtrEqual(nil, nil) {
		t.Errorf("双 nil 应判等")
	}
	if int64PtrEqual(&a, nil) {
		t.Errorf("一 nil 一非 nil 应判不等")
	}
}

func TestHasPreStopHook(t *testing.T) {
	if hasPreStopHook(corev1.Container{}) {
		t.Errorf("空容器应返回 false")
	}
	if !hasPreStopHook(corev1.Container{
		Lifecycle: &corev1.Lifecycle{PreStop: &corev1.LifecycleHandler{}},
	}) {
		t.Errorf("有 PreStop 应返回 true")
	}
}

// ---- ensureDeployment 集成 ----

func TestEnsureDeployment_AppliesStrategyAndLifecycle(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = agentv1.AddToScheme(scheme)
	deploy := &agentv1.AgentDeployment{
		ObjectMeta: metav1.ObjectMeta{Name: "rolling", Namespace: "default", UID: types.UID("roll-uid")},
		Spec: agentv1.AgentDeploymentSpec{
			Replicas: 2,
			Template: agentv1.AgentTemplateSpec{Provider: "openai", Model: "gpt-4o"},
		},
	}
	cli := ctrlfake.NewClientBuilder().WithScheme(scheme).WithObjects(deploy).Build()
	r := &AgentDeploymentReconciler{Client: cli, Scheme: scheme}

	if err := r.ensureDeployment(context.Background(), deploy); err != nil {
		t.Fatalf("ensureDeployment failed: %v", err)
	}

	var got appsv1.Deployment
	if err := cli.Get(context.Background(), types.NamespacedName{Name: "rolling-agent", Namespace: "default"}, &got); err != nil {
		t.Fatalf("Deployment not found: %v", err)
	}
	if got.Spec.Strategy.Type != appsv1.RollingUpdateDeploymentStrategyType {
		t.Errorf("Strategy.Type = %v, want RollingUpdate", got.Spec.Strategy.Type)
	}
	if got.Spec.Strategy.RollingUpdate == nil {
		t.Fatal("RollingUpdate 不应为 nil")
	}
	// Pod 级别
	if got.Spec.Template.Spec.TerminationGracePeriodSeconds == nil || *got.Spec.Template.Spec.TerminationGracePeriodSeconds != 30 {
		t.Errorf("Grace = %v, want 30", got.Spec.Template.Spec.TerminationGracePeriodSeconds)
	}
	// 每个容器应有 preStop
	for _, c := range got.Spec.Template.Spec.Containers {
		if c.Lifecycle == nil || c.Lifecycle.PreStop == nil {
			t.Errorf("容器 %s 缺 PreStop", c.Name)
		}
	}
}

func TestEnsureDeployment_UpdatesMissingLifecycle(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = agentv1.AddToScheme(scheme)
	deploy := &agentv1.AgentDeployment{
		ObjectMeta: metav1.ObjectMeta{Name: "lifecycle-update", Namespace: "default", UID: types.UID("lu-uid")},
		Spec: agentv1.AgentDeploymentSpec{
			Replicas: 2,
			Template: agentv1.AgentTemplateSpec{Provider: "openai", Model: "gpt-4o"},
		},
	}
	// 预创建 Deployment（缺 Strategy 和 lifecycle）
	existing := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "lifecycle-update-agent", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptrInt32(2),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "x"}},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "app"}},
				},
			},
		},
	}
	cli := ctrlfake.NewClientBuilder().WithScheme(scheme).WithObjects(deploy, existing).Build()
	r := &AgentDeploymentReconciler{Client: cli, Scheme: scheme}

	if err := r.ensureDeployment(context.Background(), deploy); err != nil {
		t.Fatalf("ensureDeployment failed: %v", err)
	}

	var got appsv1.Deployment
	if err := cli.Get(context.Background(), types.NamespacedName{Name: "lifecycle-update-agent", Namespace: "default"}, &got); err != nil {
		t.Fatalf("Deployment not found: %v", err)
	}
	if got.Spec.Strategy.Type != appsv1.RollingUpdateDeploymentStrategyType {
		t.Errorf("Strategy 未更新")
	}
	if got.Spec.Template.Spec.TerminationGracePeriodSeconds == nil || *got.Spec.Template.Spec.TerminationGracePeriodSeconds != 30 {
		t.Errorf("Grace 未设置")
	}
	if len(got.Spec.Template.Spec.Containers) == 0 || got.Spec.Template.Spec.Containers[0].Lifecycle == nil ||
		got.Spec.Template.Spec.Containers[0].Lifecycle.PreStop == nil {
		t.Errorf("PreStop 未注入")
	}
}

// ---- helpers ----

func ptrInt64(v int64) *int64 {
	return &v
}
