// budget.go — v7.6 落地 v7.3 修复项 D6：EnhancedPlanner 规划预算护栏。
//
// 证据锚点（docs/v7.2-验收实验报告.md §2.1 / §2.2 / §5.1）：
// EnhancedPlanner 的递归分解策略在 sensenova-6.8-flash-lite 上 A/B 实测
// **反向显著 -47.8pp（A 52.2% vs B 4.3%，McNemar p=0.001，11:0）**——
// 规划器为每个任务生成多层 plan（6 层 × 4-9 subtasks），每层 subtask 又触发
// 新的递归分解，规划本身消耗大量 turns/token，与 ReAct 短程任务
// （25 turns 上限）根本冲突：规划开销 > 规划收益。
//
// 修复口径（v7.3 §5.1，本文件机械化执行）：
//  1. 规划消耗 turns ≤ 总 turns 上限的 20%；
//  2. 递归分解深度 ≤ 2 层；
//  3. 超限即停止规划、退回直接执行（ReAct 循环对 GeneratePlan 失败本就降级
//     到普通 runLoop，见 internal/agent/react_loop_core.go tryPlanExecution）。
//
// 记账口径：以 PlannerGuard 生命周期累计——每次规划 LLM 调用记 1 个规划 turn
// （Base/Replanner/Recovery 共用同一 provider，全部计入）。多任务复用同一
// planner 时，调用方应在每个任务开始前调用 ResetTask()，使 20% 按任务口径生效。
package planning

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm"
)

// ErrPlanBudgetExceeded 规划 turns 预算耗尽。
// 语义：规划消耗的 turns 达到 总 turns 上限 × MaxPlanTurnRatio，调用方必须
// 停止规划并退回直接执行（ReAct 短程循环），不得继续追加规划 LLM 调用。
var ErrPlanBudgetExceeded = errors.New("planning turns budget exceeded")

// ErrPlanDepthExceeded 递归分解深度超限。
// 语义：请求的分解层数超过 MaxDecomposeDepth，调用方必须把该子任务当作
// 原子任务直接执行，不得继续下钻分解。
var ErrPlanDepthExceeded = errors.New("recursive decomposition depth exceeded")

const (
	// defaultPlanTurnRatio 护栏默认值：规划 turns ≤ 总 turns 的 20%（v7.3 §5.1）。
	defaultPlanTurnRatio = 0.2
	// defaultMaxDecomposeDepth 护栏默认值：递归分解深度 ≤ 2 层（v7.3 §5.1）。
	defaultMaxDecomposeDepth = 2
	// defaultTotalTurns 未显式绑定总 turns 上限时的默认任务预算。
	// 锚点：v7.2 报告 §2.2「与 ReAct 短程任务（25 turns 上限）根本冲突」。
	defaultTotalTurns = 25
	// maxPlanTurnRatioUpper 比例上限（100% = 允许规划占满全部 turns）。
	maxPlanTurnRatioUpper = 1.0
	// floatEpsilon 浮点误差补偿，避免 25 × 0.2 = 5.000000000000001 之类
	// 的二进制表示误差把整数上限误判为小一。
	floatEpsilon = 1e-9
)

// PlannerBudget 规划护栏阈值配置。
// 零值不是合法配置——请用 DefaultPlannerBudget() 取得默认值（即护栏值），
// 再用选项收紧。
type PlannerBudget struct {
	// MaxPlanTurnRatio 规划 turns 占总 turns 上限的最大比例，取值 [0, 1]。
	// 默认 0.2（20%）；0 表示完全关闭规划。
	MaxPlanTurnRatio float64
	// MaxDecomposeDepth 递归分解最大层数（第 1 层 = 顶层任务分解）。
	// 默认 2；0 表示不允许任何分解。
	MaxDecomposeDepth int
}

// DefaultPlannerBudget 返回护栏默认值 = v7.3 修复项规定的护栏值（20% / 2 层）。
func DefaultPlannerBudget() PlannerBudget {
	return PlannerBudget{
		MaxPlanTurnRatio:  defaultPlanTurnRatio,
		MaxDecomposeDepth: defaultMaxDecomposeDepth,
	}
}

// PlannerBudgetOption 规划护栏可选配置项（阈值由维护者显式决定，默认即护栏值）。
type PlannerBudgetOption func(*PlannerBudget)

// WithPlannerTurnRatio 设置规划 turns 占比上限（如 0.1 = 10%）。
// 取值收敛到 [0, 1]：0 = 关闭规划，1 = 允许占满全部 turns。
func WithPlannerTurnRatio(ratio float64) PlannerBudgetOption {
	return func(b *PlannerBudget) {
		switch {
		case ratio < 0:
			b.MaxPlanTurnRatio = 0
		case ratio > maxPlanTurnRatioUpper:
			b.MaxPlanTurnRatio = maxPlanTurnRatioUpper
		default:
			b.MaxPlanTurnRatio = ratio
		}
	}
}

// WithMaxDecomposeDepth 设置递归分解最大层数（如 1 = 只允许一层分解）。
// 负值收敛为 0（不允许任何分解）。
func WithMaxDecomposeDepth(depth int) PlannerBudgetOption {
	return func(b *PlannerBudget) {
		if depth < 0 {
			depth = 0
		}
		b.MaxDecomposeDepth = depth
	}
}

// PlannerGuard 规划护栏（执行期线程安全）：turns 预算 + 分解深度双重守门。
// 超限即熔断（停止规划、退回直接执行），任务边界用 ResetTask() 复位。
type PlannerGuard struct {
	mu         sync.Mutex
	budget     PlannerBudget
	totalTurns int  // 任务级总 turns 上限（≤0 时按 defaultTotalTurns 计）
	planTurns  int  // 已消耗的规划 turns
	stopped    bool // 是否已有规划尝试被护栏拒绝（熔断位）
}

// NewPlannerGuard 创建规划护栏。totalTurns 为任务级总 turns 上限
// （如 AgentConfig.MaxTurns）；≤0 时按默认任务预算 defaultTotalTurns 计，
// 保证护栏默认生效（fail-safe，不因漏配而失效）。
func NewPlannerGuard(totalTurns int, opts ...PlannerBudgetOption) *PlannerGuard {
	budget := DefaultPlannerBudget()
	for _, opt := range opts {
		if opt != nil {
			opt(&budget)
		}
	}
	return &PlannerGuard{budget: budget, totalTurns: totalTurns}
}

// MaxPlanTurns 当前生效的规划 turns 上限 = floor(总 turns × MaxPlanTurnRatio)。
func (g *PlannerGuard) MaxPlanTurns() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.maxPlanTurnsLocked()
}

func (g *PlannerGuard) maxPlanTurnsLocked() int {
	return int(math.Floor(float64(g.effectiveTotalTurnsLocked())*g.budget.MaxPlanTurnRatio + floatEpsilon))
}

func (g *PlannerGuard) effectiveTotalTurnsLocked() int {
	if g.totalTurns <= 0 {
		return defaultTotalTurns
	}
	return g.totalTurns
}

// MaxDecomposeDepth 当前生效的递归分解层数上限。
func (g *PlannerGuard) MaxDecomposeDepth() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.budget.MaxDecomposeDepth
}

// PlanTurns 已消耗的规划 turns（观测用）。
func (g *PlannerGuard) PlanTurns() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.planTurns
}

// AcquirePlanTurn 尝试占用一个规划 turn（原子操作：检查 + 记账）。
// 预算内返回 true；超限返回 false 与可读原因，并置熔断位。
//
// 语义：一次规划 LLM 调用 = 1 个规划 turn，调用即占用（v7.2 口径——规划
// 请求本身就消耗 turns/token，失败调用同样已付费，故不做成功后才记账）。
// 调用方拿到 false 时必须停止规划并退回直接执行。
func (g *PlannerGuard) AcquirePlanTurn() (bool, string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	max := g.maxPlanTurnsLocked()
	if g.stopped {
		return false, g.exceededReasonLocked(max)
	}
	if g.planTurns+1 > max {
		g.stopped = true
		return false, g.exceededReasonLocked(max)
	}
	g.planTurns++
	return true, ""
}

func (g *PlannerGuard) exceededReasonLocked(max int) string {
	return fmt.Sprintf("规划 turns 预算耗尽（总 turns %d × %.0f%% = %d，已消耗 %d）："+
		"停止规划并退回直接执行",
		g.effectiveTotalTurnsLocked(), g.budget.MaxPlanTurnRatio*100, max, g.planTurns)
}

// RecordPlanTurn 手动登记一次规划 LLM 调用（= 1 个规划 turn）。
// 仅供自行发起规划调用、未走 AcquirePlanTurn 的调用方补记账使用；
// 常规路径请用 AcquirePlanTurn（检查与记账原子完成，避免并发超发）。
func (g *PlannerGuard) RecordPlanTurn() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.planTurns++
}

// AllowDepth 判断第 level 层分解是否可以执行。
// level 从 1 开始计：1 = 顶层任务分解，2 = 子任务再分解，……
// 护栏默认 MaxDecomposeDepth = 2，即最多 2 层分解（v7.2 报告 §5.1
// 「禁止递归分解超过 2 层」）；level < 1 按 1 计。
func (g *PlannerGuard) AllowDepth(level int) bool {
	if level < 1 {
		level = 1
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return level <= g.budget.MaxDecomposeDepth
}

// Stopped 护栏是否已熔断（已有规划尝试被拒绝）。
func (g *PlannerGuard) Stopped() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.stopped
}

// ResetTask 任务边界复位：清零已消耗规划 turns 与熔断位。
// 多任务复用同一 planner 时，每个任务开始前调用，使 20% 按任务口径生效。
func (g *PlannerGuard) ResetTask() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.planTurns = 0
	g.stopped = false
}

// SetTotalTurns 运行期绑定总 turns 上限（如从 AgentConfig.MaxTurns 注入）。
func (g *PlannerGuard) SetTotalTurns(totalTurns int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.totalTurns = totalTurns
}

// ===== 接入 EnhancedPlanner =====

// budgetedProvider 规划护栏装饰器：包一层 llm.Provider。
// 每次规划 LLM 调用前过护栏；超限即返回包装了 ErrPlanBudgetExceeded 的错误，
// 使上层规划器（Base/Replanner/Recovery 共用该 provider）报错——
// ReAct 循环对 GeneratePlan 失败本就降级到普通 runLoop（直接执行），
// 无需改动循环代码即实现「超限即停止规划、退回直接执行」。
type budgetedProvider struct {
	inner llm.Provider
	guard *PlannerGuard
}

// 编译期断言：装饰器仍是合法 llm.Provider。
var _ llm.Provider = (*budgetedProvider)(nil)

func (p *budgetedProvider) beforePlan() error {
	if ok, reason := p.guard.AcquirePlanTurn(); !ok {
		return fmt.Errorf("%w: %s", ErrPlanBudgetExceeded, reason)
	}
	return nil
}

func (p *budgetedProvider) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if err := p.beforePlan(); err != nil {
		return nil, err
	}
	return p.inner.Complete(ctx, req)
}

func (p *budgetedProvider) CallTools(ctx context.Context, req *llm.ToolCallRequest) (*llm.ToolCallResponse, error) {
	if err := p.beforePlan(); err != nil {
		return nil, err
	}
	return p.inner.CallTools(ctx, req)
}

func (p *budgetedProvider) Stream(ctx context.Context, req *llm.CompletionRequest) (<-chan llm.Chunk, error) {
	if err := p.beforePlan(); err != nil {
		return nil, err
	}
	return p.inner.Stream(ctx, req)
}

func (p *budgetedProvider) Info() llm.ModelInfo { return p.inner.Info() }

// NewEnhancedPlannerWithBudget 创建带规划护栏的增强规划器（v7.6 / D6）。
//
// 返回标准的 *EnhancedPlanner——审批门、死路检测、重规划、恢复全部保留，
// ReAct 循环内 getEnhancedPlannerOrNil() 的接线不变；仅把内部 LLM provider
// 换成带护栏的装饰器，使规划 turns / 分解深度受控。
//
// totalTurns 为任务级总 turns 上限（如 AgentConfig.MaxTurns），≤0 时按默认
// 任务预算 25 turns 计。阈值默认即护栏值（20% / 2 层），可用选项收紧：
//
//	ep := planning.NewEnhancedPlannerWithBudget(llm, actions, cfg.MaxTurns,
//		planning.WithPlannerTurnRatio(0.1), planning.WithMaxDecomposeDepth(1))
func NewEnhancedPlannerWithBudget(provider llm.Provider, approvalActions []string, totalTurns int, opts ...PlannerBudgetOption) *EnhancedPlanner {
	guard := NewPlannerGuard(totalTurns, opts...)
	return NewEnhancedPlanner(&budgetedProvider{inner: provider, guard: guard}, approvalActions)
}

// PlannerGuardOf 取出 EnhancedPlanner 内部生效的规划护栏。
// 未启用预算（用 NewEnhancedPlanner 创建）时返回 nil；对 nil 接收者安全。
func (ep *EnhancedPlanner) PlannerGuardOf() *PlannerGuard {
	if ep == nil || ep.Base == nil {
		return nil
	}
	bp, ok := ep.Base.provider.(*budgetedProvider)
	if !ok {
		return nil
	}
	return bp.guard
}

// DecomposeAt 在指定分解层执行任务分解（深度护栏入口）。
// level 从 1 开始计：1 = 顶层任务分解，2 = 子任务再分解，……
// 超过 MaxDecomposeDepth 时返回包装了 ErrPlanDepthExceeded 的错误，
// 调用方应把该子任务当作原子任务直接执行，不得继续下钻。
func (ep *EnhancedPlanner) DecomposeAt(ctx context.Context, task string, level int) ([]SubTask, error) {
	if g := ep.PlannerGuardOf(); g != nil && !g.AllowDepth(level) {
		return nil, fmt.Errorf("%w: 第 %d 层分解超过上限 %d 层，该子任务应按原子任务直接执行",
			ErrPlanDepthExceeded, level, g.MaxDecomposeDepth())
	}
	return ep.Decompose(ctx, task)
}
