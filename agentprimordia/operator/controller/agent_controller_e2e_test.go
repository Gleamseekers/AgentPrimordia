//go:build envtest

package controller

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	agentv1 "github.com/Gleamseekers/AgentPrimordia/operator/api/v1"
)

func TestAgentControllerE2E(t *testing.T) {
	// Skip if KUBEBUILDER_ASSETS is not set (e.g., local dev without envtest)
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set; run 'setup-envtest use -p path' to set up envtest binaries")
	}

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := agentv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	// 复制 CRD 到临时目录：manifest/ 下混有非 CRD 的部署清单（controller.yaml），
	// envtest 的 CRDDirectoryPaths 会解析目录内所有 yaml，混入非 CRD 会报错。
	crdDir := t.TempDir()
	crdBytes, err := os.ReadFile(filepath.Join("..", "manifest", "crd.yaml"))
	if err != nil {
		t.Fatalf("读取 CRD 清单失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(crdDir, "crd.yaml"), crdBytes, 0o644); err != nil {
		t.Fatalf("写入临时 CRD 失败: %v", err)
	}

	testEnv := &envtest.Environment{
		BinaryAssetsDirectory: os.Getenv("KUBEBUILDER_ASSETS"),
		// 安装 AgentDeployment CRD，否则创建自定义资源会 404
		CRDDirectoryPaths: []string{crdDir},
	}

	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatalf("Failed to start envtest: %v", err)
	}
	defer testEnv.Stop()

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme,
		Metrics: server.Options{
			BindAddress: "0",
		},
	})
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	if err := (&AgentDeploymentReconciler{
		Client: mgr.GetClient(),
		Scheme: scheme,
	}).SetupWithManager(mgr); err != nil {
		t.Fatalf("Failed to setup controller: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go mgr.Start(ctx)

	// Wait for manager to be ready
	time.Sleep(2 * time.Second)

	k8sClient := mgr.GetClient()

	ad := &agentv1.AgentDeployment{
		ObjectMeta: metav1.ObjectMeta{Name: "e2e-test", Namespace: "default"},
		Spec: agentv1.AgentDeploymentSpec{
			Replicas: 1,
			Template: agentv1.AgentTemplateSpec{
				Provider:     "openai",
				Model:        "gpt-4o",
				SystemPrompt: "e2e test",
				MaxTurns:     3,
			},
		},
	}

	if err := k8sClient.Create(ctx, ad); err != nil {
		t.Fatalf("Failed to create AgentDeployment: %v", err)
	}

	// Poll for ConfigMap
	assertEventually(t, func() error {
		cm := &corev1.ConfigMap{}
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: "e2e-test-config", Namespace: "default"}, cm); err != nil {
			return fmt.Errorf("ConfigMap not found: %w", err)
		}
		return nil
	}, 10*time.Second, "ConfigMap should be created")

	// Poll for Deployment
	assertEventually(t, func() error {
		deploy := &appsv1.Deployment{}
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: "e2e-test-agent", Namespace: "default"}, deploy); err != nil {
			return fmt.Errorf("Deployment not found: %w", err)
		}
		return nil
	}, 10*time.Second, "Deployment should be created")

	// Cleanup
	k8sClient.Delete(ctx, ad)
}

func assertEventually(t *testing.T, fn func() error, timeout time.Duration, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := fn(); err == nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("%s: condition not met within %v", msg, timeout)
}
