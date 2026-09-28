// adapters_real.go — Studio 基础四面板的真实引擎适配器（v7.4 接线）
//
// 背景：cmd/studio 此前调用 NewStudioHandler() 不传任何 With*，
// chaos/cluster/learning/marketplace 四面板全部走 demo 数据（仅有 X-Data-Source 标识）。
// 本文件补齐 cluster / marketplace / chaos 的真实适配器（learning 见 real_learning.go，
// autonomy/skills/realtime 见 adapters_v3x.go），使 Studio 可装配真实引擎。
//
// 安全边界（chaos）：Studio HTTP 契约只带 FaultType 字符串，缺少注入目标/参数，
// 因此适配器**不执行宿主机级故障注入**，而是用 NoopFault + AlwaysMet 稳态
// 真实跑通 ChaosEngine 流水线（校验/登记/稳态检查/结果记录/中止）。真正的高危注入
// 需以完整 Experiment 规格在 SDK 中显式构造。
package studio

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"agentprimordia/internal/agent/cluster"
	agentmarket "agentprimordia/internal/agent/marketplace"
	"agentprimordia/internal/chaos"
)

// ===== Cluster =====

type clusterAdapter struct {
	mgr *cluster.ClusterManager
}

// NewClusterServiceAdapter 用真实集群管理器适配 Cluster 面板（只读状态）。
func NewClusterServiceAdapter(mgr *cluster.ClusterManager) ClusterService {
	return &clusterAdapter{mgr: mgr}
}

func (a *clusterAdapter) Status(_ context.Context) (*ClusterStatus, error) {
	if a == nil || a.mgr == nil {
		return &ClusterStatus{}, nil
	}
	nodes := a.mgr.ListNodes()
	out := &ClusterStatus{
		Nodes:    make([]NodeInfo, 0, len(nodes)),
		LeaderID: a.mgr.GetLeader(),
	}
	for _, n := range nodes {
		out.Nodes = append(out.Nodes, NodeInfo{
			ID:           n.ID,
			Address:      n.Address,
			Role:         string(n.Role),
			Status:       string(n.Status),
			Capabilities: n.Capabilities,
		})
	}
	return out, nil
}

// ===== Marketplace =====

type marketplaceAdapter struct {
	reg *agentmarket.TemplateRegistry

	mu          sync.Mutex
	deployments map[string]Deployment
	order       []string
	seq         int
}

// NewMarketplaceServiceAdapter 用真实模板注册表适配 Marketplace 面板。
// 模板检索来自注册表（可为持久化注册表）；部署记录由适配器跟踪。
func NewMarketplaceServiceAdapter(reg *agentmarket.TemplateRegistry) MarketplaceService {
	return &marketplaceAdapter{
		reg:         reg,
		deployments: make(map[string]Deployment),
	}
}

func (a *marketplaceAdapter) SearchTemplates(_ context.Context, query, category string) ([]AgentTemplate, error) {
	if a == nil || a.reg == nil {
		return []AgentTemplate{}, nil
	}
	var tags []string
	if query == "" {
		// 空查询时按分类列出全部
		all := a.reg.List()
		out := make([]AgentTemplate, 0, len(all))
		for _, t := range all {
			if category != "" && t.Category != category {
				continue
			}
			out = append(out, toStudioTemplate(t))
		}
		return out, nil
	}
	found := a.reg.Search(query, category, tags)
	out := make([]AgentTemplate, 0, len(found))
	for _, t := range found {
		out = append(out, toStudioTemplate(t))
	}
	return out, nil
}

func toStudioTemplate(t *agentmarket.AgentTemplate) AgentTemplate {
	if t == nil {
		return AgentTemplate{}
	}
	return AgentTemplate{
		ID:          t.ID,
		Name:        t.Name,
		Description: t.Description,
		Version:     t.Version,
		Author:      t.Author,
		Category:    t.Category,
		Tags:        t.Tags,
		Rating:      t.Rating,
		Downloads:   t.Downloads,
	}
}

func (a *marketplaceAdapter) Deploy(_ context.Context, templateID string) (Deployment, error) {
	if a == nil || a.reg == nil {
		return Deployment{}, fmt.Errorf("studio: 模板注册表未配置")
	}
	tmpl, ok := a.reg.Get(templateID)
	if !ok {
		return Deployment{}, fmt.Errorf("studio: 模板 %q 不存在", templateID)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.seq++
	dep := Deployment{
		ID:         fmt.Sprintf("dep-%s-%d", templateID, a.seq),
		TemplateID: templateID,
		Name:       tmpl.Name,
		Version:    tmpl.Version,
		Category:   tmpl.Category,
		Status:     "running",
		DeployedAt: time.Now().Format(time.RFC3339),
	}
	a.deployments[dep.ID] = dep
	a.order = append(a.order, dep.ID)
	return dep, nil
}

func (a *marketplaceAdapter) ListDeployments(_ context.Context) ([]Deployment, error) {
	if a == nil {
		return []Deployment{}, nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]Deployment, 0, len(a.order))
	for _, id := range a.order {
		if dep, ok := a.deployments[id]; ok {
			out = append(out, dep)
		}
	}
	return out, nil
}

func (a *marketplaceAdapter) StopDeployment(_ context.Context, id string) error {
	return a.setDeploymentStatus(id, "stopped")
}

func (a *marketplaceAdapter) StartDeployment(_ context.Context, id string) error {
	return a.setDeploymentStatus(id, "running")
}

func (a *marketplaceAdapter) setDeploymentStatus(id, status string) error {
	if a == nil {
		return fmt.Errorf("studio: 适配器未初始化")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	dep, ok := a.deployments[id]
	if !ok {
		return fmt.Errorf("studio: 部署 %q 不存在", id)
	}
	dep.Status = status
	a.deployments[id] = dep
	return nil
}

// ===== Chaos =====

type chaosAdapter struct {
	engine *chaos.ChaosEngine

	mu      sync.Mutex
	results map[string]ExperimentResult
	order   []string
}

// NewChaosServiceAdapter 用真实 ChaosEngine 适配 Chaos 面板。
//
// 说明：Studio 契约不含注入目标/参数，故实验以 NoopFault + AlwaysMet 稳态执行——
// 真实跑通引擎流水线但不触碰宿主机。高危注入需在 SDK 中以完整 Experiment 规格发起。
func NewChaosServiceAdapter(engine *chaos.ChaosEngine) ChaosService {
	return &chaosAdapter{
		engine:  engine,
		results: make(map[string]ExperimentResult),
	}
}

func (a *chaosAdapter) CreateExperiment(_ context.Context, req CreateExperimentRequest) error {
	if a == nil || a.engine == nil {
		return fmt.Errorf("studio: 混沌引擎未配置")
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Hypothesis) == "" {
		return fmt.Errorf("studio: 实验 name 与 hypothesis 均为必填")
	}

	exp := chaos.Experiment{
		Name:        req.Name,
		Description: req.FaultType,
		Hypothesis:  req.Hypothesis,
		Faults:      []chaos.Fault{chaos.NewNoopFault(req.FaultType)},
		SteadyState: chaos.NewAlwaysMetSteadyState(),
		Duration:    2 * time.Second,
	}

	a.mu.Lock()
	a.order = removeString(a.order, req.Name)
	a.order = append(a.order, req.Name)
	a.mu.Unlock()

	// 异步执行，避免阻塞 HTTP 请求（实验含稳态检查与持续时间）
	go func() {
		result, err := a.engine.Run(context.Background(), exp)
		a.mu.Lock()
		defer a.mu.Unlock()
		if err != nil {
			a.results[req.Name] = ExperimentResult{
				Experiment: Experiment{
					Name:       req.Name,
					Hypothesis: req.Hypothesis,
					Status:     "failed",
				},
			}
			return
		}
		a.results[req.Name] = toStudioExperimentResult(result)
	}()
	return nil
}

func (a *chaosAdapter) ListExperiments(_ context.Context) ([]ExperimentResult, error) {
	if a == nil {
		return []ExperimentResult{}, nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]ExperimentResult, 0, len(a.order))
	for _, name := range a.order {
		if r, ok := a.results[name]; ok {
			out = append(out, r)
		}
	}
	return out, nil
}

func (a *chaosAdapter) AbortExperiment(_ context.Context, name string) error {
	if a == nil || a.engine == nil {
		return fmt.Errorf("studio: 混沌引擎未配置")
	}
	if !a.engine.Abort(name) {
		return fmt.Errorf("studio: 实验 %q 不在运行中", name)
	}
	return nil
}

func toStudioExperimentResult(r *chaos.ExperimentResult) ExperimentResult {
	if r == nil {
		return ExperimentResult{}
	}
	faults := make([]Fault, 0, len(r.Experiment.Faults))
	for _, f := range r.Experiment.Faults {
		if f == nil {
			continue
		}
		faults = append(faults, Fault{Type: f.Type()})
	}
	return ExperimentResult{
		Experiment: Experiment{
			Name:                r.Experiment.Name,
			Description:         r.Experiment.Description,
			Hypothesis:          r.Experiment.Hypothesis,
			Status:              string(r.Status),
			Duration:            r.Duration.String(),
			Faults:              faults,
			HypothesisValidated: r.HypothesisValidated,
		},
		StartTime: r.StartTime.Format(time.RFC3339),
		EndTime:   r.EndTime.Format(time.RFC3339),
	}
}

func removeString(list []string, target string) []string {
	out := list[:0]
	for _, s := range list {
		if s != target {
			out = append(out, s)
		}
	}
	return out
}
