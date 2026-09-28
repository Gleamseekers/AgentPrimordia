package autonomy

import (
	"fmt"
	"sync"
	"time"
)

// StepStatus 步骤执行状态
type StepStatus string

const (
	// StepPending 等待执行
	StepPending StepStatus = "pending"
	// StepRunning 正在执行
	StepRunning StepStatus = "running"
	// StepCompleted 执行完成
	StepCompleted StepStatus = "completed"
	// StepFailed 执行失败
	StepFailed StepStatus = "failed"
	// StepSkipped 跳过（依赖失败时）
	StepSkipped StepStatus = "skipped"
)

// StepStrategy 步骤执行策略
type StepStrategy string

const (
	// StepStrategySequential 顺序执行
	StepStrategySequential StepStrategy = "sequential"
	// StepStrategyParallel 并行执行（与同层无依赖步骤并发）
	StepStrategyParallel StepStrategy = "parallel"
	// StepStrategyConditional 条件执行（满足前置条件才执行）
	StepStrategyConditional StepStrategy = "conditional"
)

// PlanStep 计划中的单个执行步骤
type PlanStep struct {
	// ID 步骤唯一标识
	ID string `json:"id"`
	// Description 步骤描述
	Description string `json:"description"`
	// DependsOn 依赖的步骤 ID 列表
	DependsOn []string `json:"depends_on,omitempty"`
	// Strategy 执行策略
	Strategy StepStrategy `json:"strategy"`
	// Status 当前状态
	Status StepStatus `json:"status"`
	// EstimatedCost 预估成本（token 数或时间秒数）
	EstimatedCost float64 `json:"estimated_cost,omitempty"`
	// Result 执行结果
	Result string `json:"result,omitempty"`
	// Error 失败原因
	Error string `json:"error,omitempty"`
	// StartedAt 开始时间
	StartedAt time.Time `json:"started_at,omitempty"`
	// FinishedAt 完成时间
	FinishedAt time.Time `json:"finished_at,omitempty"`
	// RetryCount 步骤级重试次数
	RetryCount int `json:"retry_count,omitempty"`
}

// Duration 返回步骤执行耗时
func (s *PlanStep) Duration() time.Duration {
	if s.StartedAt.IsZero() || s.FinishedAt.IsZero() {
		return 0
	}
	return s.FinishedAt.Sub(s.StartedAt)
}

// GoalPlan 目标执行计划：将目标分解为有序步骤
type GoalPlan struct {
	mu sync.RWMutex

	// GoalID 关联的目标 ID
	GoalID string `json:"goal_id"`
	// Steps 有序步骤列表
	Steps []PlanStep `json:"steps"`
	// Version 计划版本（重规划时递增）
	Version int `json:"version"`
	// CreatedAt 创建时间
	CreatedAt time.Time `json:"created_at"`
	// ReplanReason 最近一次重规划的原因
	ReplanReason string `json:"replan_reason,omitempty"`
}

// NewGoalPlan 创建执行计划
func NewGoalPlan(goalID string, steps []PlanStep) *GoalPlan {
	// 初始化步骤状态
	for i := range steps {
		if steps[i].Status == "" {
			steps[i].Status = StepPending
		}
		if steps[i].Strategy == "" {
			steps[i].Strategy = StepStrategySequential
		}
	}
	return &GoalPlan{
		GoalID:    goalID,
		Steps:     steps,
		Version:   1,
		CreatedAt: time.Now(),
	}
}

// ReadySteps 返回所有依赖已满足且尚未执行的步骤
func (p *GoalPlan) ReadySteps() []PlanStep {
	p.mu.RLock()
	defer p.mu.RUnlock()

	completed := make(map[string]bool)
	for _, s := range p.Steps {
		if s.Status == StepCompleted || s.Status == StepSkipped {
			completed[s.ID] = true
		}
	}

	var ready []PlanStep
	for _, s := range p.Steps {
		if s.Status != StepPending {
			continue
		}
		allDepsReady := true
		for _, dep := range s.DependsOn {
			if !completed[dep] {
				allDepsReady = false
				break
			}
		}
		if allDepsReady {
			ready = append(ready, s)
		}
	}
	return ready
}

// MarkStepCompleted 标记步骤完成
func (p *GoalPlan) MarkStepCompleted(stepID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.Steps {
		if p.Steps[i].ID == stepID {
			p.Steps[i].Status = StepCompleted
			p.Steps[i].FinishedAt = time.Now()
			return
		}
	}
}

// MarkStepFailed 标记步骤失败
func (p *GoalPlan) MarkStepFailed(stepID string, reason string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.Steps {
		if p.Steps[i].ID == stepID {
			p.Steps[i].Status = StepFailed
			p.Steps[i].Error = reason
			p.Steps[i].FinishedAt = time.Now()
			return
		}
	}
}

// MarkStepRunning 标记步骤开始执行
func (p *GoalPlan) MarkStepRunning(stepID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.Steps {
		if p.Steps[i].ID == stepID {
			p.Steps[i].Status = StepRunning
			p.Steps[i].StartedAt = time.Now()
			return
		}
	}
}

// GetStep 获取指定步骤（返回副本）
func (p *GoalPlan) GetStep(stepID string) PlanStep {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, s := range p.Steps {
		if s.ID == stepID {
			return s
		}
	}
	return PlanStep{}
}

// Progress 返回完成进度 [0.0, 1.0]
func (p *GoalPlan) Progress() float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if len(p.Steps) == 0 {
		return 0
	}
	done := 0
	for _, s := range p.Steps {
		if s.Status == StepCompleted || s.Status == StepSkipped {
			done++
		}
	}
	return float64(done) / float64(len(p.Steps))
}

// IsComplete 判断计划是否全部完成
func (p *GoalPlan) IsComplete() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, s := range p.Steps {
		if s.Status != StepCompleted && s.Status != StepSkipped {
			return false
		}
	}
	return len(p.Steps) > 0
}

// RemainingSteps 返回未完成的步骤
func (p *GoalPlan) RemainingSteps() []PlanStep {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var remaining []PlanStep
	for _, s := range p.Steps {
		if s.Status == StepPending || s.Status == StepRunning || s.Status == StepFailed {
			remaining = append(remaining, s)
		}
	}
	return remaining
}

// Clone 返回计划的深拷贝（不含互斥锁）。
// GoalPlan 含 sync.RWMutex，按值拷贝会触发 go vet copylocks 且复制
// 锁状态；恢复检查点等场景需要独立副本时必须走本方法。
func (p *GoalPlan) Clone() *GoalPlan {
	p.mu.RLock()
	defer p.mu.RUnlock()
	steps := make([]PlanStep, len(p.Steps))
	copy(steps, p.Steps)
	return &GoalPlan{
		GoalID:      p.GoalID,
		Steps:       steps,
		Version:     p.Version,
		CreatedAt:   p.CreatedAt,
		ReplanReason: p.ReplanReason,
	}
}

// Validate 校验计划合法性（循环依赖检测 + 非空）
func (p *GoalPlan) Validate() error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.Steps) == 0 {
		return fmt.Errorf("autonomy: 计划不能为空")
	}

	// 构建邻接表检测循环依赖
	stepSet := make(map[string]bool, len(p.Steps))
	for _, s := range p.Steps {
		stepSet[s.ID] = true
	}

	// 检查依赖引用有效性
	for _, s := range p.Steps {
		for _, dep := range s.DependsOn {
			if !stepSet[dep] {
				return fmt.Errorf("autonomy: 步骤 %s 依赖不存在的步骤 %s", s.ID, dep)
			}
		}
	}

	// DFS 检测环
	const (
		white = 0 // 未访问
		gray  = 1 // 访问中
		black = 2 // 已完成
	)
	color := make(map[string]int, len(p.Steps))
	adj := make(map[string][]string, len(p.Steps))
	for _, s := range p.Steps {
		adj[s.ID] = s.DependsOn
	}

	var hasCycle func(id string) bool
	hasCycle = func(id string) bool {
		color[id] = gray
		for _, dep := range adj[id] {
			if color[dep] == gray {
				return true
			}
			if color[dep] == white && hasCycle(dep) {
				return true
			}
		}
		color[id] = black
		return false
	}

	for _, s := range p.Steps {
		if color[s.ID] == white {
			if hasCycle(s.ID) {
				return fmt.Errorf("autonomy: 计划存在循环依赖")
			}
		}
	}
	return nil
}
