// Package controller 测试 - metrics_adapter.go
//
// 覆盖：
//   - parsePrometheusText 文本解析
//   - Summarize 聚合
//   - PodMetricsCollector.Collect 通过 httptest + fake client 端到端
//   - ReconcileMetricsAdapter.Reconcile 串联
package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	agentv1 "github.com/Gleamseekers/AgentPrimordia/agentprimordia/operator/api/v1"
)

// ---- parsePrometheusText 纯函数测试 ----

func TestParsePrometheusText_Basic(t *testing.T) {
	body := `# HELP foo Some metric
# TYPE foo counter
foo 12.5
bar 3
`
	got := parsePrometheusText(body)
	if got["foo"] != 12.5 {
		t.Errorf("foo = %v, want 12.5", got["foo"])
	}
	if got["bar"] != 3 {
		t.Errorf("bar = %v, want 3", got["bar"])
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}

func TestParsePrometheusText_Labeled(t *testing.T) {
	body := `
ap_pool_dispatched_total{agent="x"} 7
ap_pool_dispatched_total{agent="y"} 5
ap_pool_active_workers 3
`
	got := parsePrometheusText(body)
	// 重复 metric 求和
	if got["ap_pool_dispatched_total"] != 12 {
		t.Errorf("sum = %v, want 12", got["ap_pool_dispatched_total"])
	}
	if got["ap_pool_active_workers"] != 3 {
		t.Errorf("active = %v, want 3", got["ap_pool_active_workers"])
	}
}

func TestParsePrometheusText_CommentsAndEmpty(t *testing.T) {
	body := `# HELP x
# TYPE x gauge

   # 空行+注释
y -1.5e2
bad_line_no_value
`
	got := parsePrometheusText(body)
	if got["y"] != -150 {
		t.Errorf("y = %v, want -150", got["y"])
	}
	if _, ok := got["bad_line_no_value"]; ok {
		t.Errorf("bad_line_no_value 应被忽略")
	}
}

func TestParsePrometheusText_LargeBuffer(t *testing.T) {
	// 单行很长的指标不应触发 bufio 默认 64K 限制之外的 panic
	var sb strings.Builder
	for i := 0; i < 1000; i++ {
		sb.WriteString(fmt.Sprintf("metric_a %d\nmetric_b %d\n", i, i*2))
	}
	got := parsePrometheusText(sb.String())
	// 重复 metric 求和：0+1+...+999 = 999*1000/2 = 499500
	if got["metric_a"] != 499500 {
		t.Errorf("metric_a = %v, want 499500", got["metric_a"])
	}
	// metric_b = 0+2+4+...+1998 = 1000*1998/2 = 999000
	if got["metric_b"] != 999000 {
		t.Errorf("metric_b = %v, want 999000", got["metric_b"])
	}
}

// ---- Summarize 测试 ----

func TestSummarize(t *testing.T) {
	pms := []PodMetric{
		{PodName: "p1", ConcurrentTasks: 5, ActiveWorkers: 2, QueueLength: 1, CostUSD: 0.5, TokensTotal: 100, ScrapeSuccess: true},
		{PodName: "p2", ConcurrentTasks: 3, ActiveWorkers: 1, QueueLength: 0, CostUSD: 0.3, TokensTotal: 200, ScrapeSuccess: true},
		{PodName: "p3", ConcurrentTasks: 0, ActiveWorkers: 0, QueueLength: 0, CostUSD: 0, TokensTotal: 0, ScrapeSuccess: false},
	}
	s := Summarize(pms)
	if s.TotalConcurrentTasks != 8 {
		t.Errorf("TotalConcurrentTasks = %v, want 8", s.TotalConcurrentTasks)
	}
	if s.AverageConcurrentTasks != 8.0/3.0 {
		t.Errorf("AverageConcurrentTasks = %v, want %v", s.AverageConcurrentTasks, 8.0/3.0)
	}
	if s.TotalActiveWorkers != 3 {
		t.Errorf("TotalActiveWorkers = %v, want 3", s.TotalActiveWorkers)
	}
	if s.TotalQueueLength != 1 {
		t.Errorf("TotalQueueLength = %v, want 1", s.TotalQueueLength)
	}
	if s.TotalCostUSD != 0.8 {
		t.Errorf("TotalCostUSD = %v, want 0.8", s.TotalCostUSD)
	}
	if s.TotalTokens != 300 {
		t.Errorf("TotalTokens = %v, want 300", s.TotalTokens)
	}
	if s.HealthyPodCount != 2 {
		t.Errorf("HealthyPodCount = %v, want 2", s.HealthyPodCount)
	}
	if s.FailedScrapeCount != 1 {
		t.Errorf("FailedScrapeCount = %v, want 1", s.FailedScrapeCount)
	}
}

func TestSummarize_Empty(t *testing.T) {
	s := Summarize(nil)
	if s.AverageConcurrentTasks != 0 {
		t.Errorf("空切片平均应 = 0，实际 %v", s.AverageConcurrentTasks)
	}
	if s.HealthyPodCount != 0 || s.FailedScrapeCount != 0 {
		t.Errorf("计数应都为 0")
	}
}

// ---- PodMetricsCollector 端到端 ----

// makeFakeClientWithPods 构造 fake client + 包含若干 Pod 的环境
func makeFakeClientWithPods(deploy *agentv1.AgentDeployment, pods []corev1.Pod) (*runtime.Scheme, *ctrlfake.ClientBuilder) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = agentv1.AddToScheme(scheme)
	builder := ctrlfake.NewClientBuilder().WithScheme(scheme).WithObjects(deploy)
	objs := make([]client.Object, 0, len(pods))
	for i := range pods {
		objs = append(objs, &pods[i])
	}
	builder = builder.WithObjects(objs...)
	return scheme, builder
}

func TestPodMetricsCollector_Collect_NilDeploy(t *testing.T) {
	c := NewPodMetricsCollector(nil)
	if _, err := c.Collect(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil deploy")
	}
}

func TestPodMetricsCollector_Collect_EndToEnd(t *testing.T) {
	// 启动 httptest server 作为"metrics sidecar"
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`
# HELP ap_pool_dispatched_total Dispatched tasks
# TYPE ap_pool_dispatched_total counter
ap_pool_dispatched_total 42
# HELP ap_pool_active_workers Active workers
ap_pool_active_workers 7
ap_pool_queued_tasks 3
ap_cost_usd_total 0.1234
ap_cost_tokens_total 5000
`))
	}))
	defer server.Close()

	// 从 server URL 提取 host:port
	addr := strings.TrimPrefix(server.URL, "http://")
	host, port := splitHostPort(t, addr)

	deploy := &agentv1.AgentDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "myagent",
			Namespace: "default",
			UID:       types.UID("deploy-uid-1"),
		},
		Spec: agentv1.AgentDeploymentSpec{
			Replicas: 2,
			Template: agentv1.AgentTemplateSpec{
				Provider: "openai",
				Model:    "gpt-4o-mini",
			},
		},
	}
	owned := func(name string) corev1.Pod {
		return corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "default",
				OwnerReferences: []metav1.OwnerReference{
					{UID: deploy.UID, Kind: "AgentDeployment", Name: deploy.Name},
				},
				Labels: map[string]string{"app": "agentprimordia", "agent-deploy": deploy.Name},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				PodIP: host,
				Conditions: []corev1.PodCondition{
					{Type: corev1.PodReady, Status: corev1.ConditionTrue},
				},
			},
		}
	}

	unowned := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unowned",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{UID: types.UID("someone-else"), Kind: "AgentDeployment", Name: "other"},
			},
			Labels: map[string]string{"app": "agentprimordia", "agent-deploy": "other"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			PodIP: host,
		},
	}

	pending := owned("pending-pod")
	pending.Status.Phase = corev1.PodPending

	_, builder := makeFakeClientWithPods(deploy, []corev1.Pod{
		owned("pod-a"),
		owned("pod-b"),
		unowned,
		pending,
	})
	client := builder.Build()

	collector := NewPodMetricsCollector(client)
	collector.MetricsPath = "/metrics"
	collector.MetricsPort = int32(port)
	collector.HTTPClient = server.Client()

	results, err := collector.Collect(context.Background(), deploy)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	// 期望 2 个 owned Running pod，pending/unowned 被过滤
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	// 排序后顺序应为 pod-a, pod-b
	if results[0].PodName != "pod-a" || results[1].PodName != "pod-b" {
		t.Errorf("顺序错误: %s, %s", results[0].PodName, results[1].PodName)
	}
	// 每个 Pod 都应成功抓取
	for _, pm := range results {
		if !pm.ScrapeSuccess {
			t.Errorf("pod %s 抓取失败: %s", pm.PodName, pm.ScrapeError)
		}
		if pm.ConcurrentTasks != 42 {
			t.Errorf("pod %s ConcurrentTasks = %v, want 42", pm.PodName, pm.ConcurrentTasks)
		}
		if pm.ActiveWorkers != 7 {
			t.Errorf("pod %s ActiveWorkers = %v, want 7", pm.PodName, pm.ActiveWorkers)
		}
		if pm.QueueLength != 3 {
			t.Errorf("pod %s QueueLength = %v, want 3", pm.PodName, pm.QueueLength)
		}
		if pm.CostUSD != 0.1234 {
			t.Errorf("pod %s CostUSD = %v, want 0.1234", pm.PodName, pm.CostUSD)
		}
		if pm.TokensTotal != 5000 {
			t.Errorf("pod %s TokensTotal = %v, want 5000", pm.PodName, pm.TokensTotal)
		}
	}
	if atomic.LoadInt32(&hits) != 2 {
		t.Errorf("HTTP 抓取次数 = %d, want 2", hits)
	}

	// Summarize 一致性
	s := Summarize(results)
	if s.HealthyPodCount != 2 {
		t.Errorf("HealthyPodCount = %v, want 2", s.HealthyPodCount)
	}
	if s.TotalConcurrentTasks != 84 {
		t.Errorf("TotalConcurrentTasks = %v, want 84", s.TotalConcurrentTasks)
	}
}

func TestPodMetricsCollector_Collect_ScrapeFailure(t *testing.T) {
	// 启动会返回 500 的 server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	addr := strings.TrimPrefix(server.URL, "http://")
	host, port := splitHostPort(t, addr)

	deploy := &agentv1.AgentDeployment{
		ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "default", UID: types.UID("x-uid")},
		Spec:       agentv1.AgentDeploymentSpec{Replicas: 1, Template: agentv1.AgentTemplateSpec{Provider: "openai", Model: "gpt-4o"}},
	}
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "broken",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{UID: deploy.UID, Kind: "AgentDeployment", Name: deploy.Name},
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: host},
	}
	_, builder := makeFakeClientWithPods(deploy, []corev1.Pod{pod})
	c := NewPodMetricsCollector(builder.Build())
	c.MetricsPort = int32(port)
	c.HTTPClient = server.Client()

	results, err := c.Collect(context.Background(), deploy)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len = %d, want 1", len(results))
	}
	if results[0].ScrapeSuccess {
		t.Errorf("ScrapeSuccess = true, want false")
	}
	if results[0].ScrapeError == "" {
		t.Errorf("ScrapeError 应非空")
	}
}

func TestPodMetricsCollector_Collect_EmptyPodIP(t *testing.T) {
	deploy := &agentv1.AgentDeployment{
		ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "default", UID: types.UID("x-uid")},
		Spec:       agentv1.AgentDeploymentSpec{Replicas: 1, Template: agentv1.AgentTemplateSpec{Provider: "openai", Model: "gpt-4o"}},
	}
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "no-ip",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{UID: deploy.UID, Kind: "AgentDeployment", Name: deploy.Name},
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: ""},
	}
	_, builder := makeFakeClientWithPods(deploy, []corev1.Pod{pod})
	c := NewPodMetricsCollector(builder.Build())

	results, err := c.Collect(context.Background(), deploy)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}
	if results[0].ScrapeSuccess {
		t.Errorf("ScrapeSuccess = true, want false")
	}
}

// ---- ReconcileMetricsAdapter ----

func TestReconcileMetricsAdapter_NilCollector(t *testing.T) {
	a := &ReconcileMetricsAdapter{}
	if _, err := a.Reconcile(context.Background(), &agentv1.AgentDeployment{}); err == nil {
		t.Fatal("expected error for nil collector")
	}
}

func TestReconcileMetricsAdapter_Full(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ap_pool_dispatched_total 11\nap_pool_active_workers 4\nap_pool_queued_tasks 2\nap_cost_usd_total 0.07\nap_cost_tokens_total 1500\n"))
	}))
	defer server.Close()
	addr := strings.TrimPrefix(server.URL, "http://")
	host, port := splitHostPort(t, addr)

	deploy := &agentv1.AgentDeployment{
		ObjectMeta: metav1.ObjectMeta{Name: "y", Namespace: "default", UID: types.UID("y-uid")},
		Spec:       agentv1.AgentDeploymentSpec{Replicas: 1, Template: agentv1.AgentTemplateSpec{Provider: "openai", Model: "gpt-4o"}},
	}
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "y-0",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{UID: deploy.UID, Kind: "AgentDeployment", Name: deploy.Name},
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: host},
	}
	_, builder := makeFakeClientWithPods(deploy, []corev1.Pod{pod})
	c := NewPodMetricsCollector(builder.Build())
	c.MetricsPort = int32(port)
	c.HTTPClient = server.Client()
	a := NewReconcileMetricsAdapter(c)

	status, err := a.Reconcile(context.Background(), deploy)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}
	if status.ConcurrentTasks != 11 {
		t.Errorf("ConcurrentTasks = %v, want 11", status.ConcurrentTasks)
	}
	if status.AverageConcurrentTasks != 11 {
		t.Errorf("AverageConcurrentTasks = %v, want 11", status.AverageConcurrentTasks)
	}
	if status.ActiveWorkers != 4 {
		t.Errorf("ActiveWorkers = %v, want 4", status.ActiveWorkers)
	}
	if status.QueueLength != 2 {
		t.Errorf("QueueLength = %v, want 2", status.QueueLength)
	}
	if status.CostUSD != 0.07 {
		t.Errorf("CostUSD = %v, want 0.07", status.CostUSD)
	}
	if status.TotalTokens != 1500 {
		t.Errorf("TotalTokens = %v, want 1500", status.TotalTokens)
	}
	if status.HealthyPods != 1 {
		t.Errorf("HealthyPods = %v, want 1", status.HealthyPods)
	}
	if status.LastCollectionTime.Time.IsZero() {
		t.Errorf("LastCollectionTime 应非零")
	}

	// JSON 输出验证
	raw, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if !strings.Contains(string(raw), `"concurrentTasks":11`) {
		t.Errorf("JSON 缺少 concurrentTasks 字段: %s", raw)
	}
}

// ---- helpers ----

func splitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	idx := strings.LastIndex(addr, ":")
	if idx < 0 {
		t.Fatalf("invalid addr: %s", addr)
	}
	var port int
	if _, err := fmt.Sscanf(addr[idx+1:], "%d", &port); err != nil {
		t.Fatalf("parse port: %v", err)
	}
	return addr[:idx], port
}

// ---- 集成测试：MetricsAdapter 与 Reconcile 流程联动 ----

// TestReconcile_IntegratesMetricsAdapter 验证 controller 集成 MetricsAdapter 后，
// 能成功把 Pod /metrics 中的 cost/token 写回 AgentDeployment.Status
func TestReconcile_IntegratesMetricsAdapter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ap_pool_dispatched_total 9\nap_pool_active_workers 3\nap_pool_queued_tasks 1\nap_cost_usd_total 1.2345\nap_cost_tokens_total 9000\n"))
	}))
	defer server.Close()
	addr := strings.TrimPrefix(server.URL, "http://")
	host, port := splitHostPort(t, addr)

	deploy := &agentv1.AgentDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "with-metrics",
			Namespace: "default",
			UID:       types.UID("wmuid"),
		},
		Spec: agentv1.AgentDeploymentSpec{
			Replicas: 1,
			Template: agentv1.AgentTemplateSpec{
				Provider: "openai",
				Model:    "gpt-4o",
			},
		},
	}
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "with-metrics-agent-abc123",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{UID: deploy.UID, Kind: "AgentDeployment", Name: deploy.Name},
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: host},
	}
	scheme, builder := makeFakeClientWithPods(deploy, []corev1.Pod{pod})
	client := builder.WithStatusSubresource(&agentv1.AgentDeployment{}).Build()

	collector := NewPodMetricsCollector(client)
	collector.MetricsPort = int32(port)
	collector.HTTPClient = server.Client()

	r := &AgentDeploymentReconciler{
		Client:         client,
		Scheme:         scheme,
		MetricsAdapter: NewReconcileMetricsAdapter(collector),
	}

	if _, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: "with-metrics", Namespace: "default"}}); err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	// 验证 status 已写回
	var updated agentv1.AgentDeployment
	if err := client.Get(context.Background(), types.NamespacedName{Name: "with-metrics", Namespace: "default"}, &updated); err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if updated.Status.EstimatedCostUSD != 1.2345 {
		t.Errorf("Status.EstimatedCostUSD = %v, want 1.2345", updated.Status.EstimatedCostUSD)
	}
	if updated.Status.TotalTokens != 9000 {
		t.Errorf("Status.TotalTokens = %v, want 9000", updated.Status.TotalTokens)
	}
}

// TestReconcile_MetricsAdapter_FailureDoesNotBlock 验证 collector 失败时 Reconcile 仍能成功
func TestReconcile_MetricsAdapter_FailureDoesNotBlock(t *testing.T) {
	// 故意指向不可达端口（1 端口通常关闭）
	deploy := &agentv1.AgentDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "metrics-fail",
			Namespace: "default",
			UID:       types.UID("mfuid"),
		},
		Spec: agentv1.AgentDeploymentSpec{
			Replicas: 1,
			Template: agentv1.AgentTemplateSpec{
				Provider: "openai",
				Model:    "gpt-4o",
			},
		},
	}
	scheme, builder := makeFakeClientWithPods(deploy, nil)
	client := builder.WithStatusSubresource(&agentv1.AgentDeployment{}).Build()

	collector := NewPodMetricsCollector(client)
	collector.MetricsPort = 1 // 通常不可达

	r := &AgentDeploymentReconciler{
		Client:         client,
		Scheme:         scheme,
		MetricsAdapter: NewReconcileMetricsAdapter(collector),
	}

	// 即便 metrics 抓取失败，Reconcile 仍应成功
	if _, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: "metrics-fail", Namespace: "default"}}); err != nil {
		t.Fatalf("Reconcile 应容忍 metrics 抓取失败: %v", err)
	}

	// 验证 status 仍正常更新
	var updated agentv1.AgentDeployment
	if err := client.Get(context.Background(), types.NamespacedName{Name: "metrics-fail", Namespace: "default"}, &updated); err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if len(updated.Status.Conditions) == 0 {
		t.Errorf("Conditions 应仍被填充")
	}
}
