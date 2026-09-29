package controller

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	agentv1 "github.com/Gleamseekers/AgentPrimordia/operator/api/v1"
)

func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = agentv1.AddToScheme(s)
	return s
}

func makeAgentDeployment(name, namespace string, replicas int32) *agentv1.AgentDeployment {
	return &agentv1.AgentDeployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: agentv1.AgentDeploymentSpec{
			Replicas: replicas,
			Template: agentv1.AgentTemplateSpec{
				Provider:     "openai",
				Model:        "gpt-4o",
				SystemPrompt: "test prompt",
				MaxTurns:     5,
			},
		},
	}
}

func TestReconcile_CreatesConfigMapAndDeployment(t *testing.T) {
	scheme := newScheme()
	ad := makeAgentDeployment("test-agent", "default", 1)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ad).
		WithStatusSubresource(&agentv1.AgentDeployment{}).
		Build()

	r := &AgentDeploymentReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-agent", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	// Verify ConfigMap was created
	cm := &corev1.ConfigMap{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-agent-config", Namespace: "default"}, cm); err != nil {
		t.Errorf("ConfigMap should be created: %v", err)
	}
	if cm.Data["ap.yaml"] == "" {
		t.Error("ConfigMap should contain ap.yaml key")
	}

	// Verify Deployment was created
	deploy := &appsv1.Deployment{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-agent-agent", Namespace: "default"}, deploy); err != nil {
		t.Errorf("Deployment should be created: %v", err)
	}
	if *deploy.Spec.Replicas != 1 {
		t.Errorf("Deployment replicas = %d, want 1", *deploy.Spec.Replicas)
	}
}

func TestReconcile_HandlesMissingAgentDeployment(t *testing.T) {
	scheme := newScheme()
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	r := &AgentDeploymentReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	result, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "nonexistent", Namespace: "default"},
	})
	if err != nil {
		t.Errorf("Reconcile should not error on missing resource: %v", err)
	}
	if result.Requeue {
		t.Error("Should not requeue for missing resource")
	}
}

func TestReconcile_ConfigMapExistsSkipsCreation(t *testing.T) {
	scheme := newScheme()
	ad := makeAgentDeployment("test-agent", "default", 1)

	existingCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "test-agent-config", Namespace: "default"},
		Data:       map[string]string{"ap.yaml": "existing"},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ad, existingCM).
		WithStatusSubresource(&agentv1.AgentDeployment{}).
		Build()

	r := &AgentDeploymentReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-agent", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	cm := &corev1.ConfigMap{}
	_ = fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-agent-config", Namespace: "default"}, cm)
	if cm.Data["ap.yaml"] != "existing" {
		t.Error("Existing ConfigMap should not be overwritten")
	}
}

func TestReconcile_DeploymentExistsUpdatesReplicas(t *testing.T) {
	scheme := newScheme()
	ad := makeAgentDeployment("test-agent", "default", 3)

	replicas := int32(1)
	existingDeploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "test-agent-agent", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "agentprimordia"}},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ad, existingDeploy).
		WithStatusSubresource(&agentv1.AgentDeployment{}).
		Build()

	r := &AgentDeploymentReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-agent", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	deploy := &appsv1.Deployment{}
	_ = fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-agent-agent", Namespace: "default"}, deploy)
	if *deploy.Spec.Replicas != 3 {
		t.Errorf("Deployment replicas = %d, want 3", *deploy.Spec.Replicas)
	}
}

func TestReconcile_UpdatesStatus(t *testing.T) {
	scheme := newScheme()
	ad := makeAgentDeployment("test-agent", "default", 2)
	ad.ResourceVersion = "1"

	replicas := int32(2)
	existingDeploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "test-agent-agent", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "agentprimordia"}},
		},
	}

	existingCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "test-agent-config", Namespace: "default"},
		Data:       map[string]string{"ap.yaml": "test"},
	}

	// 添加 2 个 Running + Ready 的 Pod
	pod1 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-agent-pod-1", Namespace: "default",
			Labels: map[string]string{"app": "agentprimordia", "agent-deploy": "test-agent"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}
	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-agent-pod-2", Namespace: "default",
			Labels: map[string]string{"app": "agentprimordia", "agent-deploy": "test-agent"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ad, existingDeploy, existingCM, pod1, pod2).
		WithStatusSubresource(&agentv1.AgentDeployment{}).
		Build()

	r := &AgentDeploymentReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-agent", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	updated := &agentv1.AgentDeployment{}
	_ = fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-agent", Namespace: "default"}, updated)
	if updated.Status.ActiveReplicas != 2 {
		t.Errorf("ActiveReplicas = %d, want 2", updated.Status.ActiveReplicas)
	}
	if len(updated.Status.Conditions) != 2 {
		t.Fatalf("Conditions length = %d, want 2", len(updated.Status.Conditions))
	}
	if updated.Status.Conditions[0].Type != "Available" {
		t.Errorf("Condition[0] Type = %s, want Available", updated.Status.Conditions[0].Type)
	}
	if updated.Status.Conditions[0].Status != "True" {
		t.Errorf("Condition[0] Status = %s, want True", updated.Status.Conditions[0].Status)
	}
	if updated.Status.Conditions[1].Type != "Progressing" {
		t.Errorf("Condition[1] Type = %s, want Progressing", updated.Status.Conditions[1].Type)
	}
	if updated.Status.Conditions[1].Status != "True" {
		t.Errorf("Condition[1] Status = %s, want True", updated.Status.Conditions[1].Status)
	}
}

func TestReconcile_StatusUnavailableWhenReplicasBelowTarget(t *testing.T) {
	scheme := newScheme()
	ad := makeAgentDeployment("test-agent", "default", 3)
	ad.ResourceVersion = "1"

	replicas := int32(3)
	readyReplicas := int32(1)
	existingDeploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "test-agent-agent", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "agentprimordia"}},
		},
		Status: appsv1.DeploymentStatus{ReadyReplicas: readyReplicas},
	}

	existingCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "test-agent-config", Namespace: "default"},
		Data:       map[string]string{"ap.yaml": "test"},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ad, existingDeploy, existingCM).
		WithStatusSubresource(&agentv1.AgentDeployment{}).
		Build()

	r := &AgentDeploymentReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-agent", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	updated := &agentv1.AgentDeployment{}
	_ = fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-agent", Namespace: "default"}, updated)
	if updated.Status.Conditions[0].Status != "False" {
		t.Errorf("Condition Status = %s, want False (replicas below target)", updated.Status.Conditions[0].Status)
	}
	if updated.Status.Conditions[0].Reason != "MinimumReplicasUnavailable" {
		t.Errorf("Condition Reason = %s, want MinimumReplicasUnavailable", updated.Status.Conditions[0].Reason)
	}
}

func TestReconcile_APISecretRefInEnv(t *testing.T) {
	scheme := newScheme()
	ad := makeAgentDeployment("secret-agent", "default", 1)
	ad.Spec.Template.APISecretRef = "my-api-secret"

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ad).
		WithStatusSubresource(&agentv1.AgentDeployment{}).
		Build()

	r := &AgentDeploymentReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "secret-agent", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	deploy := &appsv1.Deployment{}
	_ = fakeClient.Get(context.Background(), types.NamespacedName{Name: "secret-agent-agent", Namespace: "default"}, deploy)

	found := false
	for _, env := range deploy.Spec.Template.Spec.Containers[0].Env {
		if env.Name == "OPENAI_API_KEY" && env.ValueFrom != nil && env.ValueFrom.SecretKeyRef.Name == "my-api-secret" {
			found = true
		}
	}
	if !found {
		t.Error("OPENAI_API_KEY env should reference my-api-secret")
	}
}

// Verify OwnerReferences are set on created resources
func TestReconcile_OwnerReferencesSet(t *testing.T) {
	scheme := newScheme()
	ad := makeAgentDeployment("owned", "default", 1)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ad).
		WithStatusSubresource(&agentv1.AgentDeployment{}).
		Build()

	r := &AgentDeploymentReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "owned", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	cm := &corev1.ConfigMap{}
	_ = fakeClient.Get(context.Background(), types.NamespacedName{Name: "owned-config", Namespace: "default"}, cm)
	if len(cm.OwnerReferences) != 1 || cm.OwnerReferences[0].Name != "owned" {
		t.Error("ConfigMap should have OwnerReference to AgentDeployment")
	}

	deploy := &appsv1.Deployment{}
	_ = fakeClient.Get(context.Background(), types.NamespacedName{Name: "owned-agent", Namespace: "default"}, deploy)
	if len(deploy.OwnerReferences) != 1 || deploy.OwnerReferences[0].Name != "owned" {
		t.Error("Deployment should have OwnerReference to AgentDeployment")
	}
}

// Compile-time check: AgentDeploymentReconciler implements client.Object
var _ client.Object = (*agentv1.AgentDeployment)(nil)

func TestReconcile_DefaultProbesWhenNoHealthCheck(t *testing.T) {
	scheme := newScheme()
	ad := makeAgentDeployment("probe-agent", "default", 1)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ad).
		WithStatusSubresource(&agentv1.AgentDeployment{}).
		Build()

	r := &AgentDeploymentReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "probe-agent", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	deploy := &appsv1.Deployment{}
	_ = fakeClient.Get(context.Background(), types.NamespacedName{Name: "probe-agent-agent", Namespace: "default"}, deploy)

	agentContainer := deploy.Spec.Template.Spec.Containers[0]

	// 验证默认 LivenessProbe
	if agentContainer.LivenessProbe == nil {
		t.Fatal("LivenessProbe should be set by default")
	}
	if agentContainer.LivenessProbe.HTTPGet == nil {
		t.Fatal("Default LivenessProbe should use HTTPGet")
	}
	if agentContainer.LivenessProbe.HTTPGet.Path != "/healthz" {
		t.Errorf("Default LivenessProbe path = %s, want /healthz", agentContainer.LivenessProbe.HTTPGet.Path)
	}
	if agentContainer.LivenessProbe.InitialDelaySeconds != 10 {
		t.Errorf("Default LivenessProbe InitialDelaySeconds = %d, want 10", agentContainer.LivenessProbe.InitialDelaySeconds)
	}

	// 验证默认 ReadinessProbe
	if agentContainer.ReadinessProbe == nil {
		t.Fatal("ReadinessProbe should be set by default")
	}
	if agentContainer.ReadinessProbe.HTTPGet == nil {
		t.Fatal("Default ReadinessProbe should use HTTPGet")
	}
	if agentContainer.ReadinessProbe.HTTPGet.Path != "/readyz" {
		t.Errorf("Default ReadinessProbe path = %s, want /readyz", agentContainer.ReadinessProbe.HTTPGet.Path)
	}
	if agentContainer.ReadinessProbe.InitialDelaySeconds != 5 {
		t.Errorf("Default ReadinessProbe InitialDelaySeconds = %d, want 5", agentContainer.ReadinessProbe.InitialDelaySeconds)
	}
}

func TestReconcile_CustomHealthCheckProbes(t *testing.T) {
	scheme := newScheme()
	ad := makeAgentDeployment("custom-probe-agent", "default", 1)
	ad.Spec.HealthCheck = &agentv1.HealthCheckSpec{
		LivenessProbe: &agentv1.ProbeSpec{
			HTTPGet:             &agentv1.HTTPGetAction{Path: "/live", Port: 9090},
			InitialDelaySeconds: 15,
			TimeoutSeconds:      3,
			PeriodSeconds:       20,
			FailureThreshold:    5,
		},
		ReadinessProbe: &agentv1.ProbeSpec{
			HTTPGet:             &agentv1.HTTPGetAction{Path: "/ready", Port: 9090},
			InitialDelaySeconds: 5,
			TimeoutSeconds:      2,
			PeriodSeconds:       10,
			FailureThreshold:    3,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ad).
		WithStatusSubresource(&agentv1.AgentDeployment{}).
		Build()

	r := &AgentDeploymentReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "custom-probe-agent", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	deploy := &appsv1.Deployment{}
	_ = fakeClient.Get(context.Background(), types.NamespacedName{Name: "custom-probe-agent-agent", Namespace: "default"}, deploy)

	agentContainer := deploy.Spec.Template.Spec.Containers[0]

	// 验证自定义 LivenessProbe
	if agentContainer.LivenessProbe == nil {
		t.Fatal("LivenessProbe should be set")
	}
	if agentContainer.LivenessProbe.HTTPGet.Path != "/live" {
		t.Errorf("LivenessProbe path = %s, want /live", agentContainer.LivenessProbe.HTTPGet.Path)
	}
	if agentContainer.LivenessProbe.InitialDelaySeconds != 15 {
		t.Errorf("LivenessProbe InitialDelaySeconds = %d, want 15", agentContainer.LivenessProbe.InitialDelaySeconds)
	}
	if agentContainer.LivenessProbe.FailureThreshold != 5 {
		t.Errorf("LivenessProbe FailureThreshold = %d, want 5", agentContainer.LivenessProbe.FailureThreshold)
	}

	// 验证自定义 ReadinessProbe
	if agentContainer.ReadinessProbe == nil {
		t.Fatal("ReadinessProbe should be set")
	}
	if agentContainer.ReadinessProbe.HTTPGet.Path != "/ready" {
		t.Errorf("ReadinessProbe path = %s, want /ready", agentContainer.ReadinessProbe.HTTPGet.Path)
	}
	if agentContainer.ReadinessProbe.InitialDelaySeconds != 5 {
		t.Errorf("ReadinessProbe InitialDelaySeconds = %d, want 5", agentContainer.ReadinessProbe.InitialDelaySeconds)
	}
}

func TestReconcile_ProgressingConditionWhenBelowTarget(t *testing.T) {
	scheme := newScheme()
	ad := makeAgentDeployment("prog-agent", "default", 3)
	ad.ResourceVersion = "1"

	replicas := int32(3)
	existingDeploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "prog-agent-agent", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "agentprimordia"}},
		},
	}

	existingCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "prog-agent-config", Namespace: "default"},
		Data:       map[string]string{"ap.yaml": "test"},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ad, existingDeploy, existingCM).
		WithStatusSubresource(&agentv1.AgentDeployment{}).
		Build()

	r := &AgentDeploymentReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "prog-agent", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	updated := &agentv1.AgentDeployment{}
	_ = fakeClient.Get(context.Background(), types.NamespacedName{Name: "prog-agent", Namespace: "default"}, updated)

	// 查找 Progressing 条件
	var progressing *agentv1.AgentDeploymentCondition
	for i := range updated.Status.Conditions {
		if updated.Status.Conditions[i].Type == "Progressing" {
			progressing = &updated.Status.Conditions[i]
			break
		}
	}
	if progressing == nil {
		t.Fatal("Progressing condition should exist")
	}
	if progressing.Status != "False" {
		t.Errorf("Progressing Status = %s, want False (no running pods)", progressing.Status)
	}
	if progressing.Reason != "ReplicaSetUpdateInProgress" {
		t.Errorf("Progressing Reason = %s, want ReplicaSetUpdateInProgress", progressing.Reason)
	}
}

func TestBuildProbe(t *testing.T) {
	tests := []struct {
		name     string
		spec     *agentv1.ProbeSpec
		wantPath string
		wantPort int32
		wantInit int32
	}{
		{
			name: "HTTPGet probe",
			spec: &agentv1.ProbeSpec{
				HTTPGet:             &agentv1.HTTPGetAction{Path: "/healthz", Port: 8080},
				InitialDelaySeconds: 10,
				TimeoutSeconds:      5,
				PeriodSeconds:       30,
				FailureThreshold:    3,
			},
			wantPath: "/healthz",
			wantPort: 8080,
			wantInit: 10,
		},
		{
			name: "probe without HTTPGet",
			spec: &agentv1.ProbeSpec{
				InitialDelaySeconds: 5,
				TimeoutSeconds:      3,
				PeriodSeconds:       10,
			},
			wantPath: "",
			wantPort: 0,
			wantInit: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe := buildProbe(tt.spec)
			if probe.InitialDelaySeconds != tt.wantInit {
				t.Errorf("InitialDelaySeconds = %d, want %d", probe.InitialDelaySeconds, tt.wantInit)
			}
			if tt.wantPath != "" {
				if probe.HTTPGet == nil {
					t.Fatal("HTTPGet should not be nil")
				}
				if probe.HTTPGet.Path != tt.wantPath {
					t.Errorf("HTTPGet.Path = %s, want %s", probe.HTTPGet.Path, tt.wantPath)
				}
				if probe.HTTPGet.Port.IntValue() != int(tt.wantPort) {
					t.Errorf("HTTPGet.Port = %d, want %d", probe.HTTPGet.Port.IntValue(), tt.wantPort)
				}
			} else if probe.HTTPGet != nil {
				t.Error("HTTPGet should be nil when not specified")
			}
		})
	}
}

// ===== HPA Tests =====

func TestReconcile_CreatesHPA_WhenAutoscalingConfigured(t *testing.T) {
	scheme := newScheme()
	ad := makeAgentDeployment("hpa-agent", "default", 2)
	ad.Spec.Autoscaling = &agentv1.AutoscalingSpec{
		MinReplicas:           2,
		MaxReplicas:           10,
		TargetConcurrentTasks: 5,
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ad).
		WithStatusSubresource(&agentv1.AgentDeployment{}).
		Build()

	r := &AgentDeploymentReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "hpa-agent", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	// HPA should exist
	var hpa autoscalingv2.HorizontalPodAutoscaler
	err = fakeClient.Get(context.Background(), types.NamespacedName{Name: "hpa-agent-hpa", Namespace: "default"}, &hpa)
	if err != nil {
		t.Fatalf("Expected HPA to be created: %v", err)
	}

	if hpa.Spec.MaxReplicas != 10 {
		t.Errorf("HPA MaxReplicas = %d, want 10", hpa.Spec.MaxReplicas)
	}
	if hpa.Spec.MinReplicas == nil || *hpa.Spec.MinReplicas != 2 {
		t.Errorf("HPA MinReplicas = %v, want 2", hpa.Spec.MinReplicas)
	}
	// Verify the custom metric
	if len(hpa.Spec.Metrics) != 1 {
		t.Fatalf("Expected 1 metric, got %d", len(hpa.Spec.Metrics))
	}
	if hpa.Spec.Metrics[0].Pods == nil {
		t.Fatal("Expected Pods metric")
	}
	if hpa.Spec.Metrics[0].Pods.Metric.Name != "concurrent_tasks_per_pod" {
		t.Errorf("Metric name = %s, want concurrent_tasks_per_pod", hpa.Spec.Metrics[0].Pods.Metric.Name)
	}
	// Verify scale target
	if hpa.Spec.ScaleTargetRef.Name != "hpa-agent-agent" {
		t.Errorf("ScaleTargetRef Name = %s, want hpa-agent-agent", hpa.Spec.ScaleTargetRef.Name)
	}
}

func TestReconcile_NoHPA_WhenAutoscalingNil(t *testing.T) {
	scheme := newScheme()
	ad := makeAgentDeployment("no-hpa-agent", "default", 1)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ad).
		WithStatusSubresource(&agentv1.AgentDeployment{}).
		Build()

	r := &AgentDeploymentReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "no-hpa-agent", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	// HPA should NOT exist
	var hpa autoscalingv2.HorizontalPodAutoscaler
	err = fakeClient.Get(context.Background(), types.NamespacedName{Name: "no-hpa-agent-hpa", Namespace: "default"}, &hpa)
	if err == nil {
		t.Error("HPA should not be created when Autoscaling is nil")
	}
}

func TestReconcile_UpdatesHPA_WhenSpecChanges(t *testing.T) {
	scheme := newScheme()
	ad := makeAgentDeployment("hpa-update", "default", 2)
	ad.Spec.Autoscaling = &agentv1.AutoscalingSpec{
		MinReplicas:           2,
		MaxReplicas:           5,
		TargetConcurrentTasks: 5,
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ad).
		WithStatusSubresource(&agentv1.AgentDeployment{}).
		Build()

	r := &AgentDeploymentReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	// First reconcile creates HPA
	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "hpa-update", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("First reconcile failed: %v", err)
	}

	// Update autoscaling spec
	// 重新从 fake client 读取最新版本，避免 reconcile 触发 Status().Update 后
	// ResourceVersion 不匹配导致 "object was modified" 错误。
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: "hpa-update", Namespace: "default"}, ad); err != nil {
		t.Fatalf("Failed to re-fetch AgentDeployment: %v", err)
	}
	ad.Spec.Autoscaling.MaxReplicas = 20
	ad.Spec.Autoscaling.MinReplicas = 3
	if err := fakeClient.Update(context.Background(), ad); err != nil {
		t.Fatalf("Failed to update AgentDeployment: %v", err)
	}

	// Second reconcile should update HPA
	_, err = r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "hpa-update", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("Second reconcile failed: %v", err)
	}

	var hpa autoscalingv2.HorizontalPodAutoscaler
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: "hpa-update-hpa", Namespace: "default"}, &hpa); err != nil {
		t.Fatalf("Failed to get HPA: %v", err)
	}

	if hpa.Spec.MaxReplicas != 20 {
		t.Errorf("HPA MaxReplicas = %d, want 20", hpa.Spec.MaxReplicas)
	}
	if hpa.Spec.MinReplicas == nil || *hpa.Spec.MinReplicas != 3 {
		t.Errorf("HPA MinReplicas = %v, want 3", hpa.Spec.MinReplicas)
	}
}
