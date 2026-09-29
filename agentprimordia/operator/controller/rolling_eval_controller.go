// rolling_eval_controller.go — 真实 K8s Canary Rollout 控制器（G2-4 生产实现）
//
// 将 rolling_eval.go 中的纯决策函数升级为完整的 Reconcile 集成：
//   - 状态机管理（CanaryStable → CanaryProgressing → CanaryPromoted / CanaryRolledBack）
//   - 通过 K8s API 调整 Deployment 副本数实现灰度
//   - 通过 Service selector + label 实现流量切分（stable vs canary）
//   - 灰度状态持久化到 AgentDeployment annotation
//   - Eval 结果驱动自动 promote / rollback
//   - 失败回滚到稳定版本（从 annotation 读取稳定 image SHA）
//
// 集成方式：在 AgentDeploymentReconciler.Reconcile() 的步骤 5 之后调用
// CanaryRolloutReconcile()，由它根据 annotation 中的灰度状态决定动作。
package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	agentv1 "github.com/Gleamseekers/AgentPrimordia/agentprimordia/operator/api/v1"
)

// CanaryPhase 灰度发布状态机的阶段。
type CanaryPhase string

const (
	// CanaryStable 稳定状态：所有副本运行 stable 镜像，无灰度。
	CanaryStable CanaryPhase = "Stable"
	// CanaryProgressing 灰度进行中：canary 副本已创建，等待 Eval 结果。
	CanaryProgressing CanaryPhase = "Progressing"
	// CanaryPromoted 灰度通过：canary 已提升为 stable，灰度完成。
	CanaryPromoted CanaryPhase = "Promoted"
	// CanaryRolledBack 灰度回滚：canary 已删除，恢复到 stable。
	CanaryRolledBack CanaryPhase = "RolledBack"
)

// CanaryState 灰度发布运行时状态（序列化到 annotation）。
type CanaryState struct {
	Phase         CanaryPhase `json:"phase"`
	StableImage   string      `json:"stableImage"`
	CanaryImage   string      `json:"canaryImage"`
	CanaryPercent int         `json:"canaryPercent"`
	StartedAt     time.Time   `json:"startedAt"`
	UpdatedAt     time.Time   `json:"updatedAt"`
	EvalResult    *EvalResult `json:"evalResult,omitempty"`
	Decision      string      `json:"decision,omitempty"`
}

const (
	// canaryStateAnnotation 存储 CanaryState JSON。
	canaryStateAnnotation = "agent.primordia.dev/canary-state"
	// canaryLabel 标记 canary Pod。
	canaryLabel = "agent.primordia.dev/canary"
	// canaryRoleLabel 区分 stable / canary 的 role label。
	canaryRoleLabel = "agent.primordia.dev/role"
	// roleStable stable 角色。
	roleStable = "stable"
	// roleCanary canary 角色。
	roleCanary = "canary"
)

// EvalRunner Eval 执行接口。
// 真实实现可以是 bench/eval-ci 中的 Eval 套件。
type EvalRunner interface {
	// RunEval 运行 Eval 套件，返回通过率。
	RunEval(ctx context.Context, agentName, image string) (EvalResult, error)
}

// CanaryRolloutConfig 灰度发布配置。
type CanaryRolloutConfig struct {
	// Canaries 灰度步进百分比序列（如 [10, 25, 50, 100]）。
	Canaries []int
	// EvalWait 灰度后等待 Eval 结果的超时时间。
	EvalWait time.Duration
	// PassThreshold Eval 通过率阈值。
	PassThreshold float64
}

// DefaultCanaryConfig 默认灰度配置。
func DefaultCanaryConfig() CanaryRolloutConfig {
	return CanaryRolloutConfig{
		Canaries:      []int{10, 25, 50, 100},
		EvalWait:      5 * time.Minute,
		PassThreshold: 0.8,
	}
}

// CanaryRolloutReconciler 灰度发布 Reconciler。
// 挂载在 AgentDeploymentReconciler 内部，在 Reconcile 主循环中调用。
type CanaryRolloutReconciler struct {
	client.Client
	Config     CanaryRolloutConfig
	EvalRunner EvalRunner
	metrics    *CanaryMetrics // 可选的可观测性指标
}

// WithMetrics 注入可观测性指标。
func (r *CanaryRolloutReconciler) WithMetrics(metrics *CanaryMetrics) *CanaryRolloutReconciler {
	r.metrics = metrics
	return r
}

// CanaryRolloutReconcile 灰度发布主调谐循环。
//
// 调用时机：在 AgentDeploymentReconciler.Reconcile() 的步骤 5（updateStatus）之后。
// 返回 ctrl.Result：若需要重新排队（等待 Eval 或灰度冷却），返回 RequeueAfter。
func (r *CanaryRolloutReconciler) CanaryRolloutReconcile(
	ctx context.Context,
	agentDeploy *agentv1.AgentDeployment,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	state, err := r.getCanaryState(agentDeploy)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("读取灰度状态失败: %w", err)
	}

	switch state.Phase {
	case CanaryStable:
		// 检查是否需要启动灰度（image SHA 变化）
		return r.handleStable(ctx, agentDeploy, state, logger)

	case CanaryProgressing:
		// 灰度进行中：运行 Eval 并决策
		return r.handleProgressing(ctx, agentDeploy, state, logger)

	case CanaryPromoted:
		// 灰度已完成，清理状态
		return r.handlePromoted(ctx, agentDeploy, state, logger)

	case CanaryRolledBack:
		// 回滚已完成，清理状态
		return r.handleRolledBack(ctx, agentDeploy, state, logger)

	default:
		return ctrl.Result{}, nil
	}
}

// handleStable 稳定状态处理：检测 image 变化，启动灰度。
func (r *CanaryRolloutReconciler) handleStable(
	ctx context.Context,
	agentDeploy *agentv1.AgentDeployment,
	state CanaryState,
	logger interface {
		Info(msg string, keysAndValues ...any)
	},
) (ctrl.Result, error) {
	deployName := fmt.Sprintf("%s-agent", agentDeploy.Name)
	var deploy appsv1.Deployment
	if err := r.Get(ctx, types.NamespacedName{Name: deployName, Namespace: agentDeploy.Namespace}, &deploy); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil // Deployment 尚未创建
		}
		return ctrl.Result{}, fmt.Errorf("获取 Deployment 失败: %w", err)
	}

	currentImage := deploy.Spec.Template.Spec.Containers[0].Image
	if state.StableImage == "" {
		// 首次初始化：记录当前 image 为 stable
		state.StableImage = currentImage
		state.Phase = CanaryStable
		state.UpdatedAt = time.Now()
		return ctrl.Result{}, r.saveCanaryState(ctx, agentDeploy, state)
	}

	if currentImage == state.StableImage {
		return ctrl.Result{}, nil // 无变化
	}

	// Image 变化 → 启动灰度
	logger.Info("检测到 image 变化，启动灰度发布",
		"agent", agentDeploy.Name,
		"stable", state.StableImage,
		"canary", currentImage,
	)

	state.Phase = CanaryProgressing
	state.CanaryImage = currentImage
	state.CanaryPercent = r.Config.Canaries[0]
	state.StartedAt = time.Now()
	state.UpdatedAt = time.Now()
	state.Decision = "灰度启动"

	if r.metrics != nil {
		r.metrics.RecordRolloutStart(agentDeploy.Name, state.CanaryPercent)
	}

	if err := r.saveCanaryState(ctx, agentDeploy, state); err != nil {
		return ctrl.Result{}, err
	}

	// 创建 canary Deployment
	if err := r.createCanaryDeployment(ctx, agentDeploy, &deploy, state); err != nil {
		return ctrl.Result{}, fmt.Errorf("创建 canary Deployment 失败: %w", err)
	}

	// 等待 Eval 结果
	return ctrl.Result{RequeueAfter: r.Config.EvalWait}, nil
}

// handleProgressing 灰度进行中：运行 Eval 并决策。
func (r *CanaryRolloutReconciler) handleProgressing(
	ctx context.Context,
	agentDeploy *agentv1.AgentDeployment,
	state CanaryState,
	logger interface {
		Info(msg string, keysAndValues ...any)
	},
) (ctrl.Result, error) {
	// 超时检查
	if time.Since(state.StartedAt) > r.Config.EvalWait*2 {
		logger.Info("灰度超时，自动回滚",
			"agent", agentDeploy.Name,
			"elapsed", time.Since(state.StartedAt),
		)
		return r.rollbackCanary(ctx, agentDeploy, state, "灰度超时自动回滚", logger)
	}

	// 运行 Eval
	if r.EvalRunner == nil {
		// 无 EvalRunner：直接 promote（用于测试/无 Eval 环境的快速路径）
		return r.promoteCanary(ctx, agentDeploy, state, "无 EvalRunner，直接提升", logger)
	}

	evalResult, err := r.EvalRunner.RunEval(ctx, agentDeploy.Name, state.CanaryImage)
	if err != nil {
		if r.metrics != nil {
			r.metrics.RecordEvalRun(0, err)
		}
		logger.Info("Eval 运行失败，保持灰度等待重试",
			"agent", agentDeploy.Name,
			"error", err,
		)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	if r.metrics != nil {
		r.metrics.RecordEvalRun(evalResult.PassRate, nil)
	}

	state.EvalResult = &evalResult
	decision := DecideRollout(state.CanaryPercent, evalResult)
	state.Decision = decision.Reason

	switch decision.Action {
	case ActionPromote:
		return r.advanceOrPromote(ctx, agentDeploy, state, logger)

	case ActionRollback:
		return r.rollbackCanary(ctx, agentDeploy, state, decision.Reason, logger)

	case ActionHold:
		// Eval 未成功，等待重试
		if err := r.saveCanaryState(ctx, agentDeploy, state); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	return ctrl.Result{}, nil
}

// advanceOrPromote 推进到下一个灰度步进或完成 promote。
func (r *CanaryRolloutReconciler) advanceOrPromote(
	ctx context.Context,
	agentDeploy *agentv1.AgentDeployment,
	state CanaryState,
	logger interface {
		Info(msg string, keysAndValues ...any)
	},
) (ctrl.Result, error) {
	// 查找当前百分比在步进序列中的位置
	currentIdx := -1
	for i, pct := range r.Config.Canaries {
		if pct == state.CanaryPercent {
			currentIdx = i
			break
		}
	}

	if currentIdx < 0 || currentIdx >= len(r.Config.Canaries)-1 {
		// 已到最后一步 → promote
		return r.promoteCanary(ctx, agentDeploy, state, "灰度最终步进通过，提升", logger)
	}

	// 推进到下一步
	nextPercent := r.Config.Canaries[currentIdx+1]
	state.CanaryPercent = nextPercent
	state.UpdatedAt = time.Now()

	logger.Info("灰度步进推进",
		"agent", agentDeploy.Name,
		"from", state.CanaryPercent,
		"to", nextPercent,
	)

	// 更新 canary Deployment 副本数
	if err := r.scaleCanary(ctx, agentDeploy, state); err != nil {
		return ctrl.Result{}, fmt.Errorf("调整 canary 副本失败: %w", err)
	}

	if err := r.saveCanaryState(ctx, agentDeploy, state); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: r.Config.EvalWait}, nil
}

// promoteCanary 将 canary 提升为 stable：更新 stable Deployment image 并删除 canary。
func (r *CanaryRolloutReconciler) promoteCanary(
	ctx context.Context,
	agentDeploy *agentv1.AgentDeployment,
	state CanaryState,
	reason string,
	logger interface {
		Info(msg string, keysAndValues ...any)
	},
) (ctrl.Result, error) {
	logger.Info("提升 canary 为 stable",
		"agent", agentDeploy.Name,
		"image", state.CanaryImage,
		"reason", reason,
	)

	deployName := fmt.Sprintf("%s-agent", agentDeploy.Name)
	var deploy appsv1.Deployment
	if err := r.Get(ctx, types.NamespacedName{Name: deployName, Namespace: agentDeploy.Namespace}, &deploy); err != nil {
		return ctrl.Result{}, fmt.Errorf("获取 stable Deployment 失败: %w", err)
	}

	// 更新 stable Deployment 的 image
	patch := client.MergeFrom(deploy.DeepCopy())
	deploy.Spec.Template.Spec.Containers[0].Image = state.CanaryImage
	if err := r.Patch(ctx, &deploy, patch); err != nil {
		return ctrl.Result{}, fmt.Errorf("更新 stable image 失败: %w", err)
	}

	// 删除 canary Deployment
	canaryName := fmt.Sprintf("%s-canary", agentDeploy.Name)
	r.Delete(ctx, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: canaryName, Namespace: agentDeploy.Namespace}})

	// 更新状态
	state.Phase = CanaryPromoted
	state.StableImage = state.CanaryImage
	state.UpdatedAt = time.Now()
	state.Decision = reason

	if r.metrics != nil {
		r.metrics.RecordPromoted(time.Since(state.StartedAt))
	}

	if err := r.saveCanaryState(ctx, agentDeploy, state); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// rollbackCanary 回滚：删除 canary Deployment，恢复 stable image。
func (r *CanaryRolloutReconciler) rollbackCanary(
	ctx context.Context,
	agentDeploy *agentv1.AgentDeployment,
	state CanaryState,
	reason string,
	logger interface {
		Info(msg string, keysAndValues ...any)
	},
) (ctrl.Result, error) {
	logger.Info("回滚 canary",
		"agent", agentDeploy.Name,
		"reason", reason,
		"stableImage", state.StableImage,
	)

	// 删除 canary Deployment
	canaryName := fmt.Sprintf("%s-canary", agentDeploy.Name)
	if err := r.Delete(ctx, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: canaryName, Namespace: agentDeploy.Namespace}}); err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, fmt.Errorf("删除 canary Deployment 失败: %w", err)
	}

	// 确保 stable Deployment 恢复到 stable image
	deployName := fmt.Sprintf("%s-agent", agentDeploy.Name)
	var deploy appsv1.Deployment
	if err := r.Get(ctx, types.NamespacedName{Name: deployName, Namespace: agentDeploy.Namespace}, &deploy); err == nil {
		if deploy.Spec.Template.Spec.Containers[0].Image != state.StableImage {
			patch := client.MergeFrom(deploy.DeepCopy())
			deploy.Spec.Template.Spec.Containers[0].Image = state.StableImage
			if err := r.Patch(ctx, &deploy, patch); err != nil {
				logger.Info("恢复 stable image 失败（非致命，下次 Reconcile 会重试）", "error", err)
			}
		}
	}

	// 更新状态
	state.Phase = CanaryRolledBack
	state.UpdatedAt = time.Now()
	state.Decision = reason

	if r.metrics != nil {
		r.metrics.RecordRolledBack(time.Since(state.StartedAt))
	}

	if err := r.saveCanaryState(ctx, agentDeploy, state); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// handlePromoted Promoted 状态：清理灰度 annotation，回到 Stable。
func (r *CanaryRolloutReconciler) handlePromoted(
	ctx context.Context,
	agentDeploy *agentv1.AgentDeployment,
	state CanaryState,
	logger interface {
		Info(msg string, keysAndValues ...any)
	},
) (ctrl.Result, error) {
	state.Phase = CanaryStable
	state.CanaryImage = ""
	state.CanaryPercent = 0
	state.EvalResult = nil
	state.Decision = ""
	state.UpdatedAt = time.Now()

	if err := r.saveCanaryState(ctx, agentDeploy, state); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("灰度发布完成，已回到稳定状态", "agent", agentDeploy.Name)
	return ctrl.Result{}, nil
}

// handleRolledBack RolledBack 状态：清理灰度 annotation，回到 Stable。
func (r *CanaryRolloutReconciler) handleRolledBack(
	ctx context.Context,
	agentDeploy *agentv1.AgentDeployment,
	state CanaryState,
	logger interface {
		Info(msg string, keysAndValues ...any)
	},
) (ctrl.Result, error) {
	state.Phase = CanaryStable
	state.CanaryImage = ""
	state.CanaryPercent = 0
	state.EvalResult = nil
	state.Decision = ""
	state.UpdatedAt = time.Now()

	if err := r.saveCanaryState(ctx, agentDeploy, state); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("灰度回滚完成，已回到稳定状态", "agent", agentDeploy.Name)
	return ctrl.Result{}, nil
}

// createCanaryDeployment 创建 canary Deployment（独立于 stable，通过 label 区分）。
func (r *CanaryRolloutReconciler) createCanaryDeployment(
	ctx context.Context,
	agentDeploy *agentv1.AgentDeployment,
	stableDeploy *appsv1.Deployment,
	state CanaryState,
) error {
	canaryName := fmt.Sprintf("%s-canary", agentDeploy.Name)

	// 检查是否已存在
	var existing appsv1.Deployment
	err := r.Get(ctx, types.NamespacedName{Name: canaryName, Namespace: agentDeploy.Namespace}, &existing)
	if err == nil {
		// 已存在，更新副本数即可
		return r.scaleCanary(ctx, agentDeploy, state)
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	// 计算灰度副本数
	canaryReplicas := int32(float64(agentDeploy.Spec.Replicas) * float64(state.CanaryPercent) / 100)
	if canaryReplicas < 1 {
		canaryReplicas = 1
	}

	// 从 stable Deployment 复制，修改 image 和 label
	canaryDeploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      canaryName,
			Namespace: agentDeploy.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(agentDeploy, agentv1.SchemeGroupVersion.WithKind("AgentDeployment")),
			},
			Labels: map[string]string{
				"app":           "agentprimordia",
				"agent-deploy":  agentDeploy.Name,
				canaryLabel:     "true",
				canaryRoleLabel: roleCanary,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &canaryReplicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app":           "agentprimordia",
					"agent-deploy":  agentDeploy.Name,
					canaryRoleLabel: roleCanary,
				},
			},
			Strategy: buildDeploymentStrategy(),
			Template: stableDeploy.Spec.Template,
		},
	}

	// 覆盖 image 和 label
	canaryDeploy.Spec.Template.Spec.Containers[0].Image = state.CanaryImage
	if canaryDeploy.Spec.Template.Labels == nil {
		canaryDeploy.Spec.Template.Labels = map[string]string{}
	}
	canaryDeploy.Spec.Template.Labels[canaryLabel] = "true"
	canaryDeploy.Spec.Template.Labels[canaryRoleLabel] = roleCanary

	return r.Create(ctx, canaryDeploy)
}

// scaleCanary 调整 canary Deployment 副本数。
func (r *CanaryRolloutReconciler) scaleCanary(
	ctx context.Context,
	agentDeploy *agentv1.AgentDeployment,
	state CanaryState,
) error {
	canaryName := fmt.Sprintf("%s-canary", agentDeploy.Name)
	var canary appsv1.Deployment
	if err := r.Get(ctx, types.NamespacedName{Name: canaryName, Namespace: agentDeploy.Namespace}, &canary); err != nil {
		if apierrors.IsNotFound(err) {
			// canary 不存在，重新创建
			deployName := fmt.Sprintf("%s-agent", agentDeploy.Name)
			var stable appsv1.Deployment
			if err := r.Get(ctx, types.NamespacedName{Name: deployName, Namespace: agentDeploy.Namespace}, &stable); err != nil {
				return err
			}
			return r.createCanaryDeployment(ctx, agentDeploy, &stable, state)
		}
		return err
	}

	canaryReplicas := int32(float64(agentDeploy.Spec.Replicas) * float64(state.CanaryPercent) / 100)
	if canaryReplicas < 1 {
		canaryReplicas = 1
	}

	if canary.Spec.Replicas == nil || *canary.Spec.Replicas != canaryReplicas {
		patch := client.MergeFrom(canary.DeepCopy())
		canary.Spec.Replicas = &canaryReplicas
		return r.Patch(ctx, &canary, patch)
	}
	return nil
}

// getCanaryState 从 annotation 读取灰度状态。
func (r *CanaryRolloutReconciler) getCanaryState(agentDeploy *agentv1.AgentDeployment) (CanaryState, error) {
	var state CanaryState
	ann, ok := agentDeploy.Annotations[canaryStateAnnotation]
	if !ok || ann == "" {
		return CanaryState{Phase: CanaryStable}, nil
	}
	if err := json.Unmarshal([]byte(ann), &state); err != nil {
		return CanaryState{Phase: CanaryStable}, nil // 损坏的 annotation 视为 Stable
	}
	return state, nil
}

// saveCanaryState 将灰度状态写入 annotation。
func (r *CanaryRolloutReconciler) saveCanaryState(ctx context.Context, agentDeploy *agentv1.AgentDeployment, state CanaryState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("序列化灰度状态失败: %w", err)
	}

	patch := client.MergeFrom(agentDeploy.DeepCopy())
	if agentDeploy.Annotations == nil {
		agentDeploy.Annotations = map[string]string{}
	}
	agentDeploy.Annotations[canaryStateAnnotation] = string(data)
	return r.Patch(ctx, agentDeploy, patch)
}

// UpdateCanaryService 更新 Service selector 以实现流量切分。
// 稳定状态：Service selector 只选 stable Pod。
// 灰度状态：Service selector 同时选 stable + canary Pod（通过 agent-deploy label）。
// 该函数在 createCanaryDeployment / promoteCanary / rollbackCanary 后调用。
func (r *CanaryRolloutReconciler) UpdateCanaryService(
	ctx context.Context,
	agentDeploy *agentv1.AgentDeployment,
	includeCanary bool,
) error {
	svcName := fmt.Sprintf("%s-metrics", agentDeploy.Name)
	var svc corev1.Service
	if err := r.Get(ctx, types.NamespacedName{Name: svcName, Namespace: agentDeploy.Namespace}, &svc); err != nil {
		return client.IgnoreNotFound(err)
	}

	patch := client.MergeFrom(svc.DeepCopy())
	if includeCanary {
		// 灰度模式：Service 同时选 stable + canary（去掉 role selector）
		delete(svc.Spec.Selector, canaryRoleLabel)
	} else {
		// 稳定模式：Service 只选 stable
		if svc.Spec.Selector == nil {
			svc.Spec.Selector = map[string]string{}
		}
		svc.Spec.Selector[canaryRoleLabel] = roleStable
	}
	return r.Patch(ctx, &svc, patch)
}

// GetCanaryStateForTest 返回当前灰度状态（测试用，暴露包级访问）。
func GetCanaryStateForTest(agentDeploy *agentv1.AgentDeployment) CanaryState {
	r := &CanaryRolloutReconciler{}
	state, _ := r.getCanaryState(agentDeploy)
	return state
}

// EnsureCanaryStableLabel 确保 stable Deployment 有正确的 role label。
// 在首次 Reconcile 时调用，让 Service 能通过 role=stable 精确选到 stable Pod。
func (r *CanaryRolloutReconciler) EnsureCanaryStableLabel(
	ctx context.Context,
	agentDeploy *agentv1.AgentDeployment,
) error {
	deployName := fmt.Sprintf("%s-agent", agentDeploy.Name)
	var deploy appsv1.Deployment
	if err := r.Get(ctx, types.NamespacedName{Name: deployName, Namespace: agentDeploy.Namespace}, &deploy); err != nil {
		return client.IgnoreNotFound(err)
	}

	needsUpdate := false
	if deploy.Spec.Template.Labels == nil {
		deploy.Spec.Template.Labels = map[string]string{}
	}
	if deploy.Spec.Template.Labels[canaryRoleLabel] != roleStable {
		deploy.Spec.Template.Labels[canaryRoleLabel] = roleStable
		needsUpdate = true
	}
	// Selector 也要包含 role=stable（只在首次设置，避免后续变更触发 recreate）
	if deploy.Spec.Selector.MatchLabels == nil {
		deploy.Spec.Selector.MatchLabels = map[string]string{}
	}
	if _, ok := deploy.Spec.Selector.MatchLabels[canaryRoleLabel]; !ok {
		deploy.Spec.Selector.MatchLabels[canaryRoleLabel] = roleStable
		needsUpdate = true
	}

	if needsUpdate {
		patch := client.MergeFrom(deploy.DeepCopy())
		return r.Patch(ctx, &deploy, patch)
	}
	return nil
}

// intstrHelper 辅助创建 IntOrString（避免重复导入 intstr 在多文件中冲突）。
var _ = intstr.FromInt
