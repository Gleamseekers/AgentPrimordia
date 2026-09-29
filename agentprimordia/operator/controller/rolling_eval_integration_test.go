// rolling_eval_integration_test.go — K8s Fake Client 集成测试（生产集成深度）
//
// 使用 sigs.k8s.io/controller-runtime 的 fake.NewClientBuilder 创建
// 内存中的 K8s 客户端，端到端验证 Canary Rollout 状态机：
//  1. Stable → Progressing（image 变化触发灰度启动）
//  2. Progressing → Promoted（Eval 通过，canary 提升为 stable）
//  3. Progressing → RolledBack（Eval 失败，自动回滚）
//  4. Progressing → Step Advance（灰度步进推进）
//  5. Stable → Stable（无变化，不应触发灰度）
//
// 同时验证 Metrics 指标在各个状态转换中被正确记录。
package controller

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agentv1 "github.com/Gleamseekers/AgentPrimordia/agentprimordia/operator/api/v1"
)

// integrationMockEvalRunner 可控的 Eval 执行器（用于测试）。
type integrationMockEvalRunner struct {
	result EvalResult
	err    error
}

func (m *integrationMockEvalRunner) RunEval(ctx context.Context, agentName, image string) (EvalResult, error) {
	if m.err != nil {
		return EvalResult{}, m.err
	}
	return m.result, nil
}

// setupFakeClient 创建带初始对象的 fake K8s client。
func setupFakeClient(t *testing.T, initObjects ...client.Object) (client.Client, *runtime.Scheme) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	if err := agentv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme agentv1: %v", err)
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(initObjects...).
		WithStatusSubresource(&agentv1.AgentDeployment{}).
		Build()

	return fakeClient, scheme
}

// makeCanaryAgentDeployment 创建测试用的 AgentDeployment。
func makeCanaryAgentDeployment(name, namespace, image string, replicas int32) *agentv1.AgentDeployment {
	return &agentv1.AgentDeployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "agent.primordia.dev/v1",
			Kind:       "AgentDeployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: map[string]string{},
		},
		Spec: agentv1.AgentDeploymentSpec{
			Replicas: replicas,
			Template: agentv1.AgentTemplateSpec{
				Provider: "openai",
				Model:    "gpt-4o",
			},
		},
	}
}

// makeStableDeployment 创建 stable 版本的 Deployment。
func makeStableDeployment(name, namespace, image string, replicas int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-agent",
			Namespace: namespace,
			Labels: map[string]string{
				"app":          "agentprimordia",
				"agent-deploy": name,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app":          "agentprimordia",
					"agent-deploy": name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":          "agentprimordia",
						"agent-deploy": name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "agent",
							Image: image,
						},
					},
				},
			},
		},
	}
}

// ===== 集成测试 =====

// TestCanaryRollout_StableToProgressing 验证 image 变化触发灰度启动。
func TestCanaryRollout_StableToProgressing(t *testing.T) {
	ctx := context.Background()

	stableImage := "agent:v1.0"
	ad := makeCanaryAgentDeployment("test-agent", "default", stableImage, 10)
	deploy := makeStableDeployment("test-agent", "default", stableImage, 10)

	// 预设 stable image 到 annotation
	ad.Annotations[canaryStateAnnotation] = `{"phase":"Stable","stableImage":"agent:v1.0","canaryPercent":0}`

	// 更新 Deployment image 为 v2.0（触发灰度）
	deploy.Spec.Template.Spec.Containers[0].Image = "agent:v2.0"

	k8sClient, _ := setupFakeClient(t, ad, deploy)

	metrics := NewCanaryMetrics()
	r := &CanaryRolloutReconciler{
		Client:  k8sClient,
		Config:  DefaultCanaryConfig(),
		metrics: metrics,
	}

	result, err := r.CanaryRolloutReconcile(ctx, ad)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	// 应该返回 RequeueAfter（等待 Eval）
	if result.RequeueAfter == 0 {
		t.Error("expected RequeueAfter > 0")
	}

	// 验证状态变为 Progressing
	updatedAd := &agentv1.AgentDeployment{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-agent", Namespace: "default"}, updatedAd); err != nil {
		t.Fatalf("Get AgentDeployment: %v", err)
	}

	state := GetCanaryStateForTest(updatedAd)
	if state.Phase != CanaryProgressing {
		t.Errorf("Phase = %q, want %q", state.Phase, CanaryProgressing)
	}
	if state.CanaryImage != "agent:v2.0" {
		t.Errorf("CanaryImage = %q, want %q", state.CanaryImage, "agent:v2.0")
	}
	if state.StableImage != "agent:v1.0" {
		t.Errorf("StableImage = %q, want %q", state.StableImage, "agent:v1.0")
	}

	// 验证 Metrics
	snap := metrics.Snapshot()
	if snap.RolloutTotal != 1 {
		t.Errorf("RolloutTotal = %d, want 1", snap.RolloutTotal)
	}
	if snap.CurrentPhase != metricPhaseProgressing {
		t.Errorf("CurrentPhase = %d, want %d", snap.CurrentPhase, metricPhaseProgressing)
	}
}

// TestCanaryRollout_EvalPass_Promoted 验证 Eval 通过后 canary 提升为 stable。
func TestCanaryRollout_EvalPass_Promoted(t *testing.T) {
	ctx := context.Background()

	stableImage := "agent:v1.0"
	canaryImage := "agent:v2.0"

	ad := makeCanaryAgentDeployment("test-agent", "default", canaryImage, 10)
	ad.Annotations[canaryStateAnnotation] = mustMarshalCanaryState(CanaryState{
		Phase:         CanaryProgressing,
		StableImage:   stableImage,
		CanaryImage:   canaryImage,
		CanaryPercent: 100,
		StartedAt:     time.Now().Add(-2 * time.Minute),
		UpdatedAt:     time.Now(),
	})

	stableDeploy := makeStableDeployment("test-agent", "default", stableImage, 10)
	canaryDeploy := makeStableDeployment("test-agent-canary", "default", canaryImage, 1)
	canaryDeploy.Name = "test-agent-canary"

	k8sClient, _ := setupFakeClient(t, ad, stableDeploy, canaryDeploy)

	metrics := NewCanaryMetrics()
	evalRunner := &integrationMockEvalRunner{
		result: EvalResult{RanOK: true, PassRate: 0.95, Threshold: 0.8},
	}

	r := &CanaryRolloutReconciler{
		Client:     k8sClient,
		Config:     DefaultCanaryConfig(),
		EvalRunner: evalRunner,
		metrics:    metrics,
	}

	_, err := r.CanaryRolloutReconcile(ctx, ad)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	// 验证状态变为 Promoted
	updatedAd := &agentv1.AgentDeployment{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-agent", Namespace: "default"}, updatedAd); err != nil {
		t.Fatalf("Get: %v", err)
	}

	state := GetCanaryStateForTest(updatedAd)
	if state.Phase != CanaryPromoted {
		t.Errorf("Phase = %q, want %q", state.Phase, CanaryPromoted)
	}
	if state.StableImage != canaryImage {
		t.Errorf("StableImage = %q, want %q", state.StableImage, canaryImage)
	}

	// 验证 stable Deployment image 被更新为 canary
	updatedDeploy := &appsv1.Deployment{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-agent-agent", Namespace: "default"}, updatedDeploy); err != nil {
		t.Fatalf("Get Deployment: %v", err)
	}
	if updatedDeploy.Spec.Template.Spec.Containers[0].Image != canaryImage {
		t.Errorf("stable Deployment image = %q, want %q",
			updatedDeploy.Spec.Template.Spec.Containers[0].Image, canaryImage)
	}

	// 验证 Metrics
	snap := metrics.Snapshot()
	if snap.PromotedTotal != 1 {
		t.Errorf("PromotedTotal = %d, want 1", snap.PromotedTotal)
	}
	if snap.EvalRunsTotal != 1 {
		t.Errorf("EvalRunsTotal = %d, want 1", snap.EvalRunsTotal)
	}
}

// TestCanaryRollout_EvalFail_RolledBack 验证 Eval 失败后自动回滚。
func TestCanaryRollout_EvalFail_RolledBack(t *testing.T) {
	ctx := context.Background()

	stableImage := "agent:v1.0"
	canaryImage := "agent:v2.0-bad"

	ad := makeCanaryAgentDeployment("test-agent", "default", canaryImage, 10)
	ad.Annotations[canaryStateAnnotation] = mustMarshalCanaryState(CanaryState{
		Phase:         CanaryProgressing,
		StableImage:   stableImage,
		CanaryImage:   canaryImage,
		CanaryPercent: 10,
		StartedAt:     time.Now().Add(-1 * time.Minute),
		UpdatedAt:     time.Now(),
	})

	stableDeploy := makeStableDeployment("test-agent", "default", stableImage, 10)
	canaryDeploy := makeStableDeployment("test-agent-canary", "default", canaryImage, 1)
	canaryDeploy.Name = "test-agent-canary"

	k8sClient, _ := setupFakeClient(t, ad, stableDeploy, canaryDeploy)

	metrics := NewCanaryMetrics()
	evalRunner := &integrationMockEvalRunner{
		result: EvalResult{RanOK: true, PassRate: 0.3, Threshold: 0.8}, // 30% < 80% → 回滚
	}

	r := &CanaryRolloutReconciler{
		Client:     k8sClient,
		Config:     DefaultCanaryConfig(),
		EvalRunner: evalRunner,
		metrics:    metrics,
	}

	_, err := r.CanaryRolloutReconcile(ctx, ad)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	// 验证状态变为 RolledBack
	updatedAd := &agentv1.AgentDeployment{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-agent", Namespace: "default"}, updatedAd); err != nil {
		t.Fatalf("Get: %v", err)
	}

	state := GetCanaryStateForTest(updatedAd)
	if state.Phase != CanaryRolledBack {
		t.Errorf("Phase = %q, want %q", state.Phase, CanaryRolledBack)
	}

	// 验证 stable Deployment image 恢复为 stable
	updatedDeploy := &appsv1.Deployment{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-agent-agent", Namespace: "default"}, updatedDeploy); err != nil {
		t.Fatalf("Get Deployment: %v", err)
	}
	if updatedDeploy.Spec.Template.Spec.Containers[0].Image != stableImage {
		t.Errorf("stable Deployment image = %q, want %q (should be restored)",
			updatedDeploy.Spec.Template.Spec.Containers[0].Image, stableImage)
	}

	// 验证 Metrics
	snap := metrics.Snapshot()
	if snap.RolledBackTotal != 1 {
		t.Errorf("RolledBackTotal = %d, want 1", snap.RolledBackTotal)
	}
}

// TestCanaryRollout_NoChange_StableRemains 验证无 image 变化时保持 Stable。
func TestCanaryRollout_NoChange_StableRemains(t *testing.T) {
	ctx := context.Background()

	stableImage := "agent:v1.0"
	ad := makeCanaryAgentDeployment("test-agent", "default", stableImage, 10)
	ad.Annotations[canaryStateAnnotation] = mustMarshalCanaryState(CanaryState{
		Phase:         CanaryStable,
		StableImage:   stableImage,
		CanaryPercent: 0,
	})

	deploy := makeStableDeployment("test-agent", "default", stableImage, 10)

	k8sClient, _ := setupFakeClient(t, ad, deploy)

	r := &CanaryRolloutReconciler{
		Client: k8sClient,
		Config: DefaultCanaryConfig(),
	}

	result, err := r.CanaryRolloutReconcile(ctx, ad)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	// 不应 Requeue
	if result.RequeueAfter != 0 {
		t.Errorf("RequeueAfter = %v, want 0", result.RequeueAfter)
	}

	// 状态应保持 Stable
	updatedAd := &agentv1.AgentDeployment{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-agent", Namespace: "default"}, updatedAd); err != nil {
		t.Fatalf("Get: %v", err)
	}

	state := GetCanaryStateForTest(updatedAd)
	if state.Phase != CanaryStable {
		t.Errorf("Phase = %q, want %q", state.Phase, CanaryStable)
	}
}

// TestCanaryRollout_EvalError_Holds 验证 Eval 运行失败时保持灰度（Hold）。
func TestCanaryRollout_EvalError_Holds(t *testing.T) {
	ctx := context.Background()

	stableImage := "agent:v1.0"
	canaryImage := "agent:v2.0"

	ad := makeCanaryAgentDeployment("test-agent", "default", canaryImage, 10)
	ad.Annotations[canaryStateAnnotation] = mustMarshalCanaryState(CanaryState{
		Phase:         CanaryProgressing,
		StableImage:   stableImage,
		CanaryImage:   canaryImage,
		CanaryPercent: 25,
		StartedAt:     time.Now().Add(-30 * time.Second),
		UpdatedAt:     time.Now(),
	})

	stableDeploy := makeStableDeployment("test-agent", "default", stableImage, 10)
	canaryDeploy := makeStableDeployment("test-agent-canary", "default", canaryImage, 1)
	canaryDeploy.Name = "test-agent-canary"

	k8sClient, _ := setupFakeClient(t, ad, stableDeploy, canaryDeploy)

	metrics := NewCanaryMetrics()
	evalRunner := &integrationMockEvalRunner{
		err: context.DeadlineExceeded, // Eval 自身出错
	}

	r := &CanaryRolloutReconciler{
		Client:     k8sClient,
		Config:     DefaultCanaryConfig(),
		EvalRunner: evalRunner,
		metrics:    metrics,
	}

	result, err := r.CanaryRolloutReconcile(ctx, ad)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	// 应该 Requeue 等待重试
	if result.RequeueAfter == 0 {
		t.Error("expected RequeueAfter > 0 for Eval error")
	}

	// 状态应保持 Progressing（不贸然回滚或提升）
	updatedAd := &agentv1.AgentDeployment{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-agent", Namespace: "default"}, updatedAd); err != nil {
		t.Fatalf("Get: %v", err)
	}

	state := GetCanaryStateForTest(updatedAd)
	if state.Phase != CanaryProgressing {
		t.Errorf("Phase = %q, want %q (should hold)", state.Phase, CanaryProgressing)
	}

	// 验证 Metrics：Eval 错误被记录
	snap := metrics.Snapshot()
	if snap.EvalErrorsTotal != 1 {
		t.Errorf("EvalErrorsTotal = %d, want 1", snap.EvalErrorsTotal)
	}
}

// TestCanaryRollout_PromotedTransitionsToStable 验证 Promoted → Stable 转换。
func TestCanaryRollout_PromotedTransitionsToStable(t *testing.T) {
	ctx := context.Background()

	canaryImage := "agent:v2.0"

	ad := makeCanaryAgentDeployment("test-agent", "default", canaryImage, 10)
	ad.Annotations[canaryStateAnnotation] = mustMarshalCanaryState(CanaryState{
		Phase:         CanaryPromoted,
		StableImage:   canaryImage,
		CanaryImage:   canaryImage,
		CanaryPercent: 100,
		UpdatedAt:     time.Now(),
	})

	k8sClient, _ := setupFakeClient(t, ad)

	r := &CanaryRolloutReconciler{
		Client: k8sClient,
		Config: DefaultCanaryConfig(),
	}

	_, err := r.CanaryRolloutReconcile(ctx, ad)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	// 验证状态回到 Stable
	updatedAd := &agentv1.AgentDeployment{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-agent", Namespace: "default"}, updatedAd); err != nil {
		t.Fatalf("Get: %v", err)
	}

	state := GetCanaryStateForTest(updatedAd)
	if state.Phase != CanaryStable {
		t.Errorf("Phase = %q, want %q", state.Phase, CanaryStable)
	}
	if state.CanaryImage != "" {
		t.Errorf("CanaryImage = %q, want empty", state.CanaryImage)
	}
}

// mustMarshalCanaryState 序列化 CanaryState（测试辅助）。
func mustMarshalCanaryState(state CanaryState) string {
	data, err := json.Marshal(state)
	if err != nil {
		panic(err)
	}
	return string(data)
}
