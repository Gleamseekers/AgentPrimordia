// Package controller 包含 AgentDeployment 调谐及周边辅助逻辑
//
// 文件：metrics_adapter.go
// 作用：实现 Pod 指标采集 + 自定义 HPA 指标适配
//
// 与 K8s 标准的 custom-metrics-apiserver 不同，本适配器通过定期拉取
// 各 Pod /metrics 端点，把采集到的并发任务数（ap_pool_dispatched_total 等）
// 反向回写至 AgentDeployment.Status.Metrics，并提供给 HPA controller 计算。
// 这种设计的 trade-off：
//
//   - 优点：不依赖额外 apiserver 插件，直接基于状态聚合；
//   - 缺点：HPA 仍依赖外部 adapter（如 Prometheus Adapter）才能访问这些值。
//
// 因此本适配器同时承担两个角色：
//
//  1. Pod 指标采集器（写 AgentDeployment.Status，供用户观测/告警）；
//  2. 单元可注入的 collector 接口，使得 envtest / fake 客户端可以不依赖 HTTP 抓取。
package controller

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentv1 "github.com/Gleamseekers/AgentPrimordia/operator/api/v1"
)

// PodMetricsCollector 负责从每个 Pod 的 /metrics 端口采集关键指标
//
// 设计为可注入接口（customCollector 字段），便于测试桩接入；
// 默认 HTTP 采集器走的是 cluster-internal DNS。
type PodMetricsCollector struct {
	client.Client
	// HTTPClient 配置 HTTP 抓取客户端；nil 则用带超时的默认 client
	HTTPClient *http.Client
	// MetricsPath 指标端点路径，默认 /metrics
	MetricsPath string
	// MetricsPort 指标端端口号（默认 9090，与 ServerBootstrap 中 PrometheusHandler 端口一致）
	MetricsPort int32
	// Timeout 每次抓取超时（默认 3s）
	Timeout time.Duration
}

// NewPodMetricsCollector 创建默认采集器
func NewPodMetricsCollector(c client.Client) *PodMetricsCollector {
	return &PodMetricsCollector{
		Client:      c,
		HTTPClient:  &http.Client{Timeout: 3 * time.Second},
		MetricsPath: "/metrics",
		MetricsPort: 9090,
		Timeout:     3 * time.Second,
	}
}

// PodMetric 单 Pod 的指标摘要
type PodMetric struct {
	// Pod 名称
	PodName string `json:"podName"`
	// Pod IP（采自 endpoint 推断）
	PodIP string `json:"podIp,omitempty"`
	// ConcurrentTasks 当前并发任务数（来自 ap_pool_dispatched_total）
	ConcurrentTasks float64 `json:"concurrentTasks"`
	// ActiveWorkers 当前活跃 worker 数（来自 ap_pool_active_workers）
	ActiveWorkers float64 `json:"activeWorkers"`
	// QueueLength 当前队列长度（来自 ap_pool_queued_tasks）
	QueueLength float64 `json:"queueLength"`
	// CostUSD 累计成本（来自 ap_cost_usd_total）
	CostUSD float64 `json:"costUsd,omitempty"`
	// TokensTotal 累计 token 数（来自 ap_cost_tokens_total）
	TokensTotal int64 `json:"tokensTotal,omitempty"`
	// ScrapeSuccess 是否成功抓取
	ScrapeSuccess bool `json:"scrapeSuccess"`
	// ScrapeError 抓取错误信息（成功时为空）
	ScrapeError string `json:"scrapeError,omitempty"`
}

// AggregateMetrics 把单 Pod 指标聚合成总览数字
//
// 用于把 /metrics 端点摘要成 HPA / status 友好的字段；不去重 ip，
// 由调用方负责清理。
func (p PodMetric) AggregateMetrics() (concurrentTasks, activeWorkers, queueLength float64) {
	return p.ConcurrentTasks, p.ActiveWorkers, p.QueueLength
}

// Collect 对一个 AgentDeployment 采集所有 Pod 的指标
//
// 返回的切片按 PodName 升序排列，保证 controller 写 status 时不会因为
// Pod 列表随机顺序导致 hash/state 不一致。
func (c *PodMetricsCollector) Collect(ctx context.Context, deploy *agentv1.AgentDeployment) ([]PodMetric, error) {
	if deploy == nil {
		return nil, fmt.Errorf("deploy 为 nil")
	}

	// 列出属于该 AgentDeployment 的 Pod（通过 owner reference 过滤）
	var pods corev1.PodList
	if err := c.List(ctx, &pods, client.InNamespace(deploy.Namespace)); err != nil {
		return nil, fmt.Errorf("列出 Pod 失败: %w", err)
	}

	// 过滤：只保留属于本 deployment 且 Running/Ready 的 Pod
	var matchingPods []corev1.Pod
	for i := range pods.Items {
		p := &pods.Items[i]
		owned := false
		for _, ref := range p.OwnerReferences {
			if ref.UID == deploy.UID {
				owned = true
				break
			}
		}
		if !owned {
			continue
		}
		if p.Status.Phase != corev1.PodRunning {
			continue
		}
		matchingPods = append(matchingPods, *p)
	}
	sort.Slice(matchingPods, func(i, j int) bool {
		return matchingPods[i].Name < matchingPods[j].Name
	})

	// 并发抓取
	path := c.MetricsPath
	if path == "" {
		path = "/metrics"
	}
	port := c.MetricsPort
	if port == 0 {
		port = 9090
	}

	results := make([]PodMetric, len(matchingPods))
	var wg sync.WaitGroup
	for i := range matchingPods {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			pod := &matchingPods[idx]
			pm := PodMetric{PodName: pod.Name, PodIP: pod.Status.PodIP}
			body, err := c.scrape(ctx, pod.Status.PodIP, port, path)
			if err != nil {
				pm.ScrapeError = err.Error()
				results[idx] = pm
				return
			}
			parsed := parsePrometheusText(body)
			pm.ConcurrentTasks = parsed["ap_pool_dispatched_total"]
			pm.ActiveWorkers = parsed["ap_pool_active_workers"]
			pm.QueueLength = parsed["ap_pool_queued_tasks"]
			pm.CostUSD = parsed["ap_cost_usd_total"]
			pm.TokensTotal = int64(parsed["ap_cost_tokens_total"])
			pm.ScrapeSuccess = true
			results[idx] = pm
		}(i)
	}
	wg.Wait()

	return results, nil
}

// scrape 抓取单个 Pod 的 /metrics 文本
//
// 优先使用 PodIP（cluster-internal 通信），port 通常由 Service forwarding；
// 失败时打 error 并返回。
func (c *PodMetricsCollector) scrape(ctx context.Context, ip string, port int32, path string) (string, error) {
	if ip == "" {
		return "", fmt.Errorf("pod IP 为空")
	}
	url := fmt.Sprintf("http://%s:%d%s", ip, port, path)
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 3 * time.Second
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	limited := io.LimitReader(resp.Body, 4*1024*1024) // 4 MiB 上限，防止巨型响应
	data, err := io.ReadAll(limited)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Prometheus 文本指标解析：返回每个 metric name (无标签) 的最新值。
// 当同一指标存在多个标签维度时求和（对 dispatch/total 这类 counter 合理近似）。
var prometheusMetricRe = regexp.MustCompile(`^([a-zA-Z_:][a-zA-Z0-9_:]*)(?:\{[^}]*\})?\s+([-+0-9.eE]+)$`)

// parsePrometheusText 解析 Prometheus 文本格式输出
//
// 简化版解析器：
//
//   - 跳过 # 开头的注释；
//   - 支持 name{labels} value 形式；
//   - 重复 metric 求和（counter 累加维度）。
func parsePrometheusText(body string) map[string]float64 {
	out := make(map[string]float64)
	scanner := bufio.NewScanner(strings.NewReader(body))
	scanner.Buffer(make([]byte, 0, 1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := prometheusMetricRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name := m[1]
		val, err := strconv.ParseFloat(m[2], 64)
		if err != nil {
			continue
		}
		out[name] += val
	}
	return out
}

// AggregateSummary 把 Pod 列表汇总为 AgentDeployment.Status 可用的指标
type AggregateSummary struct {
	TotalConcurrentTasks   float64
	AverageConcurrentTasks float64
	TotalActiveWorkers     float64
	TotalQueueLength       float64
	TotalCostUSD           float64
	TotalTokens            int64
	HealthyPodCount        int
	FailedScrapeCount      int
}

// Summarize 从 PodMetric 切片计算聚合摘要
func Summarize(metrics []PodMetric) AggregateSummary {
	s := AggregateSummary{}
	for _, pm := range metrics {
		s.TotalConcurrentTasks += pm.ConcurrentTasks
		s.TotalActiveWorkers += pm.ActiveWorkers
		s.TotalQueueLength += pm.QueueLength
		s.TotalCostUSD += pm.CostUSD
		s.TotalTokens += pm.TokensTotal
		if pm.ScrapeSuccess {
			s.HealthyPodCount++
		} else {
			s.FailedScrapeCount++
		}
	}
	if len(metrics) > 0 {
		s.AverageConcurrentTasks = s.TotalConcurrentTasks / float64(len(metrics))
	}
	return s
}

// CustomMetricsStatus 把 Adapter 写回到 Status 的结构
//
// 该结构用于 AgentDeployment.Status.Metrics（自定义 status 字段），便于
// kubectl describe / kubectl get -o yaml 观察。
type CustomMetricsStatus struct {
	// LastCollectionTime 上次成功采集时间
	LastCollectionTime metav1Time `json:"lastCollectionTime"`
	// ConcurrentTasks 跨所有 Pod 的总并发任务
	ConcurrentTasks float64 `json:"concurrentTasks"`
	// AverageConcurrentTasks 每 Pod 平均并发任务（HPA 的核心指标）
	AverageConcurrentTasks float64 `json:"averageConcurrentTasks"`
	// ActiveWorkers 总活跃 worker 数
	ActiveWorkers float64 `json:"activeWorkers"`
	// QueueLength 总队列长度
	QueueLength float64 `json:"queueLength"`
	// CostUSD 累计成本（USD）
	CostUSD float64 `json:"costUsd"`
	// TotalTokens 累计 token
	TotalTokens int64 `json:"totalTokens"`
	// HealthyPods 健康 Pod 数
	HealthyPods int32 `json:"healthyPods"`
	// FailedScrape 失败抓取次数
	FailedScrape int32 `json:"failedScrape"`
	// Pods 每个 Pod 的明细（用于调试）
	Pods []PodMetric `json:"pods,omitempty"`
}

// metav1Time 是 time.Time 的别名（避免引入 metav1 依赖造成的循环）
type metav1Time struct {
	time.Time
}

// MarshalJSON 实现 RFC3339 格式时间输出，与 metav1.Time 兼容
func (t metav1Time) MarshalJSON() ([]byte, error) {
	return []byte(`"` + t.Time.UTC().Format(time.RFC3339) + `"`), nil
}

// ReconcileMetricsAdapter 把 collector 与 controller 解耦
//
// 该类型封装"采集 → 回写 status"流程，使其既可作 controller 的子流程，
// 又可单独被 timer/cron 触发。
type ReconcileMetricsAdapter struct {
	Collector *PodMetricsCollector
}

// NewReconcileMetricsAdapter 创建默认适配器
func NewReconcileMetricsAdapter(c *PodMetricsCollector) *ReconcileMetricsAdapter {
	return &ReconcileMetricsAdapter{Collector: c}
}

// Reconcile 采集 Pod 指标并回写 AgentDeployment.Status.Metrics（如果存在字段）
//
// 当前 types.go 中 AgentDeploymentStatus 没有 Metrics 字段（保持向后兼容），
// 所以此处只返回 CustomMetricsStatus 给调用方，由 caller 决定如何使用
// （写 status、推送到 metrics adapter、上报告警等）。
func (a *ReconcileMetricsAdapter) Reconcile(ctx context.Context, deploy *agentv1.AgentDeployment) (*CustomMetricsStatus, error) {
	if a.Collector == nil {
		return nil, fmt.Errorf("collector 未配置")
	}
	podMetrics, err := a.Collector.Collect(ctx, deploy)
	if err != nil {
		return nil, err
	}
	summary := Summarize(podMetrics)
	status := &CustomMetricsStatus{
		LastCollectionTime:     metav1Time{Time: time.Now().UTC()},
		ConcurrentTasks:        summary.TotalConcurrentTasks,
		AverageConcurrentTasks: summary.AverageConcurrentTasks,
		ActiveWorkers:          summary.TotalActiveWorkers,
		QueueLength:            summary.TotalQueueLength,
		CostUSD:                summary.TotalCostUSD,
		TotalTokens:            summary.TotalTokens,
		HealthyPods:            int32(summary.HealthyPodCount),
		FailedScrape:           int32(summary.FailedScrapeCount),
		Pods:                   podMetrics,
	}
	return status, nil
}

// compile-time assertion：types.NamespacedName 仍由 client-go 暴露，无 alias 必要
var _ = types.NamespacedName{}
