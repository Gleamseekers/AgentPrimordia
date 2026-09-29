// Package controller - PDB 配置管理
//
// 文件：pdb.go
// 作用：为 AgentDeployment 创建/删除 PodDisruptionBudget，确保驱逐/滚动升级时
//
//	至少有部分 Pod 可用，避免完全不可用。
//
// 设计原则：
//   - 默认自动启用：当 replicas >= 2 时自动创建 minAvailable=1 的 PDB
//   - 单副本（replicas=1）不创建 PDB（PDB 至少需要 2 个 Pod 才能生效）
//   - 用户可通过 spec.disruptionBudget.enabled=false 显式禁用
//   - 用户可通过 spec.disruptionBudget.minAvailable/maxUnavailable 自定义阈值
package controller

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/log"

	agentv1 "github.com/Gleamseekers/AgentPrimordia/operator/api/v1"
)

// shouldCreatePDB 判断是否需要创建 PDB
//
// 返回值：
//   - true / 默认 minAvailable
//   - false 表示跳过 PDB（单副本或显式禁用）
func shouldCreatePDB(deploy *agentv1.AgentDeployment) (bool, *intstr.IntOrString) {
	if deploy == nil {
		return false, nil
	}
	// 单副本：PDB 无意义（至少需要 2 个 Pod 才能生效）
	if deploy.Spec.Replicas < 2 {
		return false, nil
	}
	// 用户显式禁用
	if deploy.Spec.DisruptionBudget != nil && deploy.Spec.DisruptionBudget.Enabled != nil && !*deploy.Spec.DisruptionBudget.Enabled {
		return false, nil
	}
	// 用户提供了自定义 minAvailable
	if deploy.Spec.DisruptionBudget != nil && deploy.Spec.DisruptionBudget.MinAvailable != nil {
		return true, deploy.Spec.DisruptionBudget.MinAvailable
	}
	// 用户提供了 maxUnavailable
	if deploy.Spec.DisruptionBudget != nil && deploy.Spec.DisruptionBudget.MaxUnavailable != nil {
		return true, nil // 由 buildPDB 用 MaxUnavailable 填充
	}
	// 默认：minAvailable=1（保证至少 1 个 Pod 可用）
	v := intstr.FromInt(1)
	return true, &v
}

// buildPDB 根据 AgentDeployment 构造 PodDisruptionBudget 对象
func buildPDB(deploy *agentv1.AgentDeployment, defaultMinAvailable *intstr.IntOrString) *policyv1.PodDisruptionBudget {
	deployName := fmt.Sprintf("%s-agent", deploy.Name)
	spec := policyv1.PodDisruptionBudgetSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app":          "agentprimordia",
				"agent-deploy": deploy.Name,
			},
		},
	}

	// 优先级：用户显式 MinAvailable > 用户显式 MaxUnavailable > 默认 MinAvailable=1
	switch {
	case deploy.Spec.DisruptionBudget != nil && deploy.Spec.DisruptionBudget.MinAvailable != nil:
		spec.MinAvailable = deploy.Spec.DisruptionBudget.MinAvailable
	case deploy.Spec.DisruptionBudget != nil && deploy.Spec.DisruptionBudget.MaxUnavailable != nil:
		spec.MaxUnavailable = deploy.Spec.DisruptionBudget.MaxUnavailable
	default:
		spec.MinAvailable = defaultMinAvailable
	}

	return &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deployName + "-pdb",
			Namespace: deploy.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(deploy, agentv1.SchemeGroupVersion.WithKind("AgentDeployment")),
			},
			Labels: map[string]string{
				"app":          "agentprimordia",
				"agent-deploy": deploy.Name,
			},
		},
		Spec: spec,
	}
}

// ensurePDB 创建/更新/删除 PodDisruptionBudget
//
// 三种情况：
//
//  1. 需要 PDB 但不存在：创建
//  2. 需要 PDB 且存在：检查并按需更新
//  3. 不需要 PDB 但存在：删除（用户在 spec 中禁用了 PDB）
//
// 该函数是幂等的，多次调用结果一致。
func (r *AgentDeploymentReconciler) ensurePDB(ctx context.Context, deploy *agentv1.AgentDeployment) error {
	logger := log.FromContext(ctx)
	pdbName := fmt.Sprintf("%s-agent-pdb", deploy.Name)
	needed, defaultMin := shouldCreatePDB(deploy)

	var existing policyv1.PodDisruptionBudget
	err := r.Get(ctx, types.NamespacedName{Name: pdbName, Namespace: deploy.Namespace}, &existing)

	if !needed {
		// 不需要 PDB：若存在则删除（保持幂等）
		if err == nil {
			logger.Info("删除 PDB（spec 禁用或单副本）", "name", pdbName)
			return r.Delete(ctx, &existing)
		}
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("检查 PDB 失败: %w", err)
	}

	// 需要 PDB：若不存在则创建
	if apierrors.IsNotFound(err) {
		pdb := buildPDB(deploy, defaultMin)
		logger.Info("创建 PDB", "name", pdbName, "minAvailable", pdb.Spec.MinAvailable, "maxUnavailable", pdb.Spec.MaxUnavailable)
		return r.Create(ctx, pdb)
	}
	if err != nil {
		return fmt.Errorf("检查 PDB 失败: %w", err)
	}

	// PDB 已存在，检查是否需要更新
	desired := buildPDB(deploy, defaultMin).Spec
	if pdbSpecEqual(existing.Spec, desired) {
		return nil
	}
	logger.Info("更新 PDB", "name", pdbName)
	existing.Spec = desired
	return r.Update(ctx, &existing)
}

// pdbSpecEqual 判断两个 PDB spec 是否一致（MinAvailable / MaxUnavailable / Selector）
func pdbSpecEqual(a, b policyv1.PodDisruptionBudgetSpec) bool {
	if !intstrEqualPtr(a.MinAvailable, b.MinAvailable) {
		return false
	}
	if !intstrEqualPtr(a.MaxUnavailable, b.MaxUnavailable) {
		return false
	}
	if !selectorEqual(a.Selector, b.Selector) {
		return false
	}
	return true
}

func intstrEqualPtr(a, b *intstr.IntOrString) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func selectorEqual(a, b *metav1.LabelSelector) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if len(a.MatchLabels) != len(b.MatchLabels) {
		return false
	}
	for k, v := range a.MatchLabels {
		if bv, ok := b.MatchLabels[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

// 编译期保证类型一致（appv1.Deployment 由 ensureDeployment 创建，标签匹配）
var _ = appsv1.Deployment{}
