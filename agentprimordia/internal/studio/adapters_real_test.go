package studio

import (
	"context"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/cluster"
	agentmarket "github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/marketplace"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/chaos"
)

// TestClusterAdapterStatus 真实集群管理器 → Cluster 面板状态映射。
func TestClusterAdapterStatus(t *testing.T) {
	mgr := cluster.NewClusterManager(cluster.ClusterConfig{NodeID: "n1", ListenAddr: "127.0.0.1:0"})
	ad := NewClusterServiceAdapter(mgr)

	st, err := ad.Status(context.Background())
	if err != nil {
		t.Fatalf("Status 失败: %v", err)
	}
	if len(st.Nodes) != 1 {
		t.Fatalf("节点数 = %d, want 1", len(st.Nodes))
	}
	if st.Nodes[0].ID != "n1" {
		t.Errorf("节点 ID = %q, want n1", st.Nodes[0].ID)
	}
	if st.Nodes[0].Status != "online" {
		t.Errorf("节点状态 = %q, want online", st.Nodes[0].Status)
	}
}

// TestMarketplaceAdapterSearchAndDeployLifecycle 真实模板注册表 + 部署生命周期。
func TestMarketplaceAdapterSearchAndDeployLifecycle(t *testing.T) {
	reg := agentmarket.NewTemplateRegistry()
	tmpl := &agentmarket.AgentTemplate{
		ID: "t1", Name: "T1", Version: "1.0.0", Author: "a",
		SystemPrompt: "p", Category: "coding",
	}
	if err := reg.Register(tmpl); err != nil {
		t.Fatalf("注册模板失败: %v", err)
	}
	ad := NewMarketplaceServiceAdapter(reg)
	ctx := context.Background()

	all, err := ad.SearchTemplates(ctx, "", "")
	if err != nil {
		t.Fatalf("SearchTemplates 失败: %v", err)
	}
	if len(all) != 1 || all[0].ID != "t1" {
		t.Fatalf("模板列表 = %+v, want 1 条 t1", all)
	}
	filtered, _ := ad.SearchTemplates(ctx, "", "research")
	if len(filtered) != 0 {
		t.Errorf("分类过滤应返回 0 条，实际 %d", len(filtered))
	}

	dep, err := ad.Deploy(ctx, "t1")
	if err != nil {
		t.Fatalf("Deploy 失败: %v", err)
	}
	if dep.Status != "running" || dep.TemplateID != "t1" {
		t.Errorf("部署记录异常: %+v", dep)
	}
	if err := ad.StopDeployment(ctx, dep.ID); err != nil {
		t.Fatalf("StopDeployment 失败: %v", err)
	}
	deps, _ := ad.ListDeployments(ctx)
	if len(deps) != 1 || deps[0].Status != "stopped" {
		t.Errorf("停止后状态 = %+v", deps)
	}
	if err := ad.StartDeployment(ctx, dep.ID); err != nil {
		t.Fatalf("StartDeployment 失败: %v", err)
	}
	deps, _ = ad.ListDeployments(ctx)
	if deps[0].Status != "running" {
		t.Errorf("启动后状态 = %q, want running", deps[0].Status)
	}
	if _, err := ad.Deploy(ctx, "missing"); err == nil {
		t.Error("不存在模板应报错")
	}
}

// TestChaosAdapterRunsRealEngine 真实 ChaosEngine 流水线被调用并记录结果。
func TestChaosAdapterRunsRealEngine(t *testing.T) {
	ad := NewChaosServiceAdapter(chaos.NewEngine())
	ctx := context.Background()

	if err := ad.CreateExperiment(ctx, CreateExperimentRequest{
		Name: "exp-1", Hypothesis: "无副作用实验", FaultType: "latency",
	}); err != nil {
		t.Fatalf("CreateExperiment 失败: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	var results []ExperimentResult
	for time.Now().Before(deadline) {
		results, _ = ad.ListExperiments(ctx)
		if len(results) == 1 && results[0].Experiment.Status != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(results) != 1 {
		t.Fatalf("实验记录数 = %d, want 1", len(results))
	}
	if results[0].Experiment.Status == "" || results[0].Experiment.Status == "running" {
		t.Errorf("实验应已结束，实际状态 %q", results[0].Experiment.Status)
	}
	if results[0].Experiment.Hypothesis != "无副作用实验" {
		t.Errorf("假设字段丢失: %+v", results[0].Experiment)
	}
}

// TestChaosAdapterValidationAndAbort 必填校验与中止语义。
func TestChaosAdapterValidationAndAbort(t *testing.T) {
	ad := NewChaosServiceAdapter(chaos.NewEngine())
	ctx := context.Background()

	if err := ad.CreateExperiment(ctx, CreateExperimentRequest{Name: "", Hypothesis: "h"}); err == nil {
		t.Error("缺少 name 应报错")
	}
	if err := ad.CreateExperiment(ctx, CreateExperimentRequest{Name: "e", Hypothesis: ""}); err == nil {
		t.Error("缺少 hypothesis 应报错")
	}
	if err := ad.AbortExperiment(ctx, "不存在的实验"); err == nil {
		t.Error("中止不存在实验应报错")
	}
}
