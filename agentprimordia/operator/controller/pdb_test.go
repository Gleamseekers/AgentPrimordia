// Package controller 测试 - pdb.go
//
// 覆盖：
//   - shouldCreatePDB 单副本 / 多副本 / 显式启用 / 显式禁用 / nil
//   - buildPDB 选择 MinAvailable vs MaxUnavailable vs 默认
//   - ensurePDB 创建 / 更新 / 删除 幂等
//   - pdbSpecEqual / intstrEqualPtr / selectorEqual
package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrlfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	agentv1 "github.com/Gleamseekers/AgentPrimordia/operator/api/v1"
)

// makeAgentForPDB 构造测试用 AgentDeployment
func makeAgentForPDB(name string, replicas int32, db *agentv1.DisruptionBudgetSpec) *agentv1.AgentDeployment {
	return &agentv1.AgentDeployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: agentv1.AgentDeploymentSpec{
			Replicas:         replicas,
			Template:         agentv1.AgentTemplateSpec{Provider: "openai", Model: "gpt-4o"},
			DisruptionBudget: db,
		},
	}
}

// ---- shouldCreatePDB ----

func TestShouldCreatePDB_SingleReplica(t *testing.T) {
	deploy := makeAgentForPDB("single", 1, nil)
	ok, min := shouldCreatePDB(deploy)
	if ok {
		t.Errorf("单副本不应创建 PDB，实际 ok=true")
	}
	if min != nil {
		t.Errorf("min 应为 nil，实际 %v", min)
	}
}

func TestShouldCreatePDB_MultiReplica_Default(t *testing.T) {
	deploy := makeAgentForPDB("multi", 3, nil)
	ok, min := shouldCreatePDB(deploy)
	if !ok {
		t.Fatalf("多副本应创建 PDB")
	}
	if min == nil || min.IntValue() != 1 {
		t.Errorf("min 应为 1，实际 %v", min)
	}
}

func TestShouldCreatePDB_ExplicitlyDisabled(t *testing.T) {
	disabled := false
	deploy := makeAgentForPDB("disabled", 5, &agentv1.DisruptionBudgetSpec{Enabled: &disabled})
	ok, _ := shouldCreatePDB(deploy)
	if ok {
		t.Errorf("Enabled=false 应禁用 PDB")
	}
}

func TestShouldCreatePDB_ExplicitlyEnabled(t *testing.T) {
	enabled := true
	deploy := makeAgentForPDB("enabled", 5, &agentv1.DisruptionBudgetSpec{Enabled: &enabled})
	ok, min := shouldCreatePDB(deploy)
	if !ok {
		t.Fatalf("显式启用应创建 PDB")
	}
	if min == nil || min.IntValue() != 1 {
		t.Errorf("min 默认应为 1，实际 %v", min)
	}
}

func TestShouldCreatePDB_CustomMinAvailable(t *testing.T) {
	min := intstr.FromInt(2)
	deploy := makeAgentForPDB("custom", 4, &agentv1.DisruptionBudgetSpec{MinAvailable: &min})
	ok, got := shouldCreatePDB(deploy)
	if !ok || got == nil || got.IntValue() != 2 {
		t.Errorf("应使用自定义 MinAvailable=2，实际 ok=%v got=%v", ok, got)
	}
}

func TestShouldCreatePDB_CustomMaxUnavailable(t *testing.T) {
	max := intstr.FromInt(2)
	deploy := makeAgentForPDB("max", 5, &agentv1.DisruptionBudgetSpec{MaxUnavailable: &max})
	ok, _ := shouldCreatePDB(deploy)
	if !ok {
		t.Errorf("MaxUnavailable 配置应启用 PDB")
	}
}

func TestShouldCreatePDB_Nil(t *testing.T) {
	ok, _ := shouldCreatePDB(nil)
	if ok {
		t.Errorf("nil deploy 应返回 false")
	}
}

// ---- buildPDB ----

func TestBuildPDB_DefaultMinAvailable(t *testing.T) {
	deploy := makeAgentForPDB("def", 3, nil)
	v := intstr.FromInt(1)
	pdb := buildPDB(deploy, &v)
	if pdb.Spec.MinAvailable == nil || pdb.Spec.MinAvailable.IntValue() != 1 {
		t.Errorf("MinAvailable 应为 1，实际 %v", pdb.Spec.MinAvailable)
	}
	if pdb.Spec.MaxUnavailable != nil {
		t.Errorf("MaxUnavailable 应为 nil")
	}
	if pdb.Name != "def-agent-pdb" {
		t.Errorf("pdb 名 = %s, want def-agent-pdb", pdb.Name)
	}
}

func TestBuildPDB_CustomMinAvailable(t *testing.T) {
	min := intstr.FromInt(2)
	deploy := makeAgentForPDB("c", 4, &agentv1.DisruptionBudgetSpec{MinAvailable: &min})
	v := intstr.FromInt(1)
	pdb := buildPDB(deploy, &v)
	if pdb.Spec.MinAvailable == nil || pdb.Spec.MinAvailable.IntValue() != 2 {
		t.Errorf("MinAvailable 应为 2，实际 %v", pdb.Spec.MinAvailable)
	}
}

func TestBuildPDB_CustomMaxUnavailable(t *testing.T) {
	max := intstr.FromString("30%")
	deploy := makeAgentForPDB("m", 5, &agentv1.DisruptionBudgetSpec{MaxUnavailable: &max})
	pdb := buildPDB(deploy, nil)
	if pdb.Spec.MaxUnavailable == nil || pdb.Spec.MaxUnavailable.StrVal != "30%" {
		t.Errorf("MaxUnavailable 应为 30%%，实际 %v", pdb.Spec.MaxUnavailable)
	}
	if pdb.Spec.MinAvailable != nil {
		t.Errorf("MaxUnavailable 配置时 MinAvailable 应为 nil")
	}
}

func TestBuildPDB_SelectorMatchesDeployment(t *testing.T) {
	deploy := makeAgentForPDB("sel", 2, nil)
	v := intstr.FromInt(1)
	pdb := buildPDB(deploy, &v)
	if pdb.Spec.Selector == nil {
		t.Fatal("Selector 不应为 nil")
	}
	if pdb.Spec.Selector.MatchLabels["app"] != "agentprimordia" {
		t.Errorf("Selector app 标签错: %v", pdb.Spec.Selector.MatchLabels)
	}
	if pdb.Spec.Selector.MatchLabels["agent-deploy"] != "sel" {
		t.Errorf("Selector agent-deploy 标签错: %v", pdb.Spec.Selector.MatchLabels)
	}
}

func TestBuildPDB_OwnerReference(t *testing.T) {
	deploy := makeAgentForPDB("owned", 2, nil)
	deploy.UID = types.UID("owner-uid")
	v := intstr.FromInt(1)
	pdb := buildPDB(deploy, &v)
	if len(pdb.OwnerReferences) != 1 {
		t.Fatalf("应包含 1 个 OwnerRef，实际 %d", len(pdb.OwnerReferences))
	}
	if pdb.OwnerReferences[0].UID != "owner-uid" {
		t.Errorf("OwnerRef UID = %s, want owner-uid", pdb.OwnerReferences[0].UID)
	}
}

// ---- pdbSpecEqual ----

func TestPDBSpecEqual(t *testing.T) {
	a := policyv1.PodDisruptionBudgetSpec{
		MinAvailable: ptrIntOrStr(1),
		Selector:     &metav1.LabelSelector{MatchLabels: map[string]string{"a": "b"}},
	}
	b := policyv1.PodDisruptionBudgetSpec{
		MinAvailable: ptrIntOrStr(1),
		Selector:     &metav1.LabelSelector{MatchLabels: map[string]string{"a": "b"}},
	}
	if !pdbSpecEqual(a, b) {
		t.Errorf("相同 spec 应判等")
	}
}

func TestPDBSpecEqual_DifferentMin(t *testing.T) {
	a := policyv1.PodDisruptionBudgetSpec{MinAvailable: ptrIntOrStr(1)}
	b := policyv1.PodDisruptionBudgetSpec{MinAvailable: ptrIntOrStr(2)}
	if pdbSpecEqual(a, b) {
		t.Errorf("不同 MinAvailable 应判不等")
	}
}

func TestIntStrEqualPtr(t *testing.T) {
	a := intstr.FromInt(1)
	b := intstr.FromInt(1)
	c := intstr.FromInt(2)
	if !intstrEqualPtr(&a, &b) {
		t.Errorf("相同值应判等")
	}
	if intstrEqualPtr(&a, &c) {
		t.Errorf("不同值应判不等")
	}
	if !intstrEqualPtr(nil, nil) {
		t.Errorf("双 nil 应判等")
	}
	if intstrEqualPtr(&a, nil) {
		t.Errorf("一 nil 一非 nil 应判不等")
	}
}

func TestSelectorEqual(t *testing.T) {
	a := &metav1.LabelSelector{MatchLabels: map[string]string{"x": "y"}}
	b := &metav1.LabelSelector{MatchLabels: map[string]string{"x": "y"}}
	c := &metav1.LabelSelector{MatchLabels: map[string]string{"x": "z"}}
	d := &metav1.LabelSelector{MatchLabels: map[string]string{"a": "b"}}
	if !selectorEqual(a, b) {
		t.Errorf("相同 selector 应判等")
	}
	if selectorEqual(a, c) {
		t.Errorf("不同值应判不等")
	}
	if selectorEqual(a, d) {
		t.Errorf("不同 key 应判不等")
	}
	if !selectorEqual(nil, nil) {
		t.Errorf("双 nil 应判等")
	}
}

// ---- ensurePDB 集成测试 ----

func makeSchemeForPDB() (*corev1.PodList, *ctrlfake.ClientBuilder, *runtime.Scheme) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = agentv1.AddToScheme(scheme)
	return &corev1.PodList{}, ctrlfake.NewClientBuilder().WithScheme(scheme), scheme
}

func TestEnsurePDB_CreatesWhenMultiReplica(t *testing.T) {
	_, builder, scheme := makeSchemeForPDB()
	deploy := makeAgentForPDB("newpdb", 3, nil)
	cli := builder.WithObjects(deploy).Build()
	r := &AgentDeploymentReconciler{Client: cli, Scheme: scheme}

	if err := r.ensurePDB(context.Background(), deploy); err != nil {
		t.Fatalf("ensurePDB failed: %v", err)
	}

	var pdb policyv1.PodDisruptionBudget
	if err := cli.Get(context.Background(), types.NamespacedName{Name: "newpdb-agent-pdb", Namespace: "default"}, &pdb); err != nil {
		t.Fatalf("PDB 未创建: %v", err)
	}
	if pdb.Spec.MinAvailable == nil || pdb.Spec.MinAvailable.IntValue() != 1 {
		t.Errorf("MinAvailable = %v, want 1", pdb.Spec.MinAvailable)
	}
}

func TestEnsurePDB_SkipsWhenSingleReplica(t *testing.T) {
	_, builder, scheme := makeSchemeForPDB()
	deploy := makeAgentForPDB("single", 1, nil)
	cli := builder.WithObjects(deploy).Build()
	r := &AgentDeploymentReconciler{Client: cli, Scheme: scheme}

	if err := r.ensurePDB(context.Background(), deploy); err != nil {
		t.Fatalf("ensurePDB failed: %v", err)
	}

	var pdb policyv1.PodDisruptionBudget
	if err := cli.Get(context.Background(), types.NamespacedName{Name: "single-agent-pdb", Namespace: "default"}, &pdb); err == nil {
		t.Errorf("单副本不应创建 PDB，但发现了: %+v", pdb)
	}
}

func TestEnsurePDB_UpdatesWhenSpecChanges(t *testing.T) {
	_, builder, scheme := makeSchemeForPDB()
	deploy := makeAgentForPDB("upd", 4, nil)
	// 预创建 PDB（旧的 MinAvailable=1）
	oldPDB := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{Name: "upd-agent-pdb", Namespace: "default"},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable: ptrIntOrStr(1),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{
				"app":          "agentprimordia",
				"agent-deploy": "upd",
			}},
		},
	}
	cli := builder.WithObjects(deploy, oldPDB).Build()
	r := &AgentDeploymentReconciler{Client: cli, Scheme: scheme}

	// 更新 spec 让 MinAvailable=3
	min3 := intstr.FromInt(3)
	deploy.Spec.DisruptionBudget = &agentv1.DisruptionBudgetSpec{MinAvailable: &min3}

	if err := r.ensurePDB(context.Background(), deploy); err != nil {
		t.Fatalf("ensurePDB failed: %v", err)
	}

	var pdb policyv1.PodDisruptionBudget
	if err := cli.Get(context.Background(), types.NamespacedName{Name: "upd-agent-pdb", Namespace: "default"}, &pdb); err != nil {
		t.Fatalf("PDB 未找到: %v", err)
	}
	if pdb.Spec.MinAvailable == nil || pdb.Spec.MinAvailable.IntValue() != 3 {
		t.Errorf("MinAvailable = %v, want 3", pdb.Spec.MinAvailable)
	}
}

func TestEnsurePDB_DeletesWhenDisabled(t *testing.T) {
	_, builder, scheme := makeSchemeForPDB()
	deploy := makeAgentForPDB("del", 3, nil)
	// 预创建 PDB
	existing := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{Name: "del-agent-pdb", Namespace: "default"},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable: ptrIntOrStr(1),
		},
	}
	cli := builder.WithObjects(deploy, existing).Build()
	r := &AgentDeploymentReconciler{Client: cli, Scheme: scheme}

	// 用户禁用 PDB
	enabled := false
	deploy.Spec.DisruptionBudget = &agentv1.DisruptionBudgetSpec{Enabled: &enabled}

	if err := r.ensurePDB(context.Background(), deploy); err != nil {
		t.Fatalf("ensurePDB failed: %v", err)
	}

	var pdb policyv1.PodDisruptionBudget
	if err := cli.Get(context.Background(), types.NamespacedName{Name: "del-agent-pdb", Namespace: "default"}, &pdb); err == nil {
		t.Errorf("PDB 应已被删除，但仍然存在: %+v", pdb)
	}
}

func TestEnsurePDB_Idempotent(t *testing.T) {
	_, builder, scheme := makeSchemeForPDB()
	deploy := makeAgentForPDB("idem", 3, nil)
	cli := builder.WithObjects(deploy).Build()
	r := &AgentDeploymentReconciler{Client: cli, Scheme: scheme}

	// 多次调用应幂等
	for i := 0; i < 3; i++ {
		if err := r.ensurePDB(context.Background(), deploy); err != nil {
			t.Fatalf("ensurePDB 第 %d 次失败: %v", i+1, err)
		}
	}

	// 只有 1 个 PDB
	var pdbList policyv1.PodDisruptionBudgetList
	if err := cli.List(context.Background(), &pdbList); err != nil {
		t.Fatalf("List failed: %v", err)
	}
	count := 0
	for _, p := range pdbList.Items {
		if p.Labels["agent-deploy"] == "idem" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("应有 1 个 PDB，实际 %d", count)
	}
}

// ---- helpers ----

func ptrIntOrStr(v int) *intstr.IntOrString {
	i := intstr.FromInt(v)
	return &i
}
