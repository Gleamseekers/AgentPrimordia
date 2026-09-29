package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent"
)

const (
	defaultMaxRetries   = 3
	defaultOrchTimeout  = 5 * time.Minute
	orchEventBufferSize = 100
	defaultBackoff      = time.Second
)

// OrchestratorMode 编排模式
type OrchestratorMode string

const (
	// SequentialMode 顺序模式：Agent按顺序执行
	SequentialMode OrchestratorMode = "sequential"
	// ParallelMode 并行模式：所有Agent并行执行
	ParallelMode OrchestratorMode = "parallel"
	// DAGMode DAG模式：有向无环图工作流
	DAGMode OrchestratorMode = "dag"
)

// OrchestratorConfig 配置
type OrchestratorConfig struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Mode        OrchestratorMode  `json:"mode"`
	MaxRetries  int               `json:"max_retries"` // 失败重试次数（默认3）
	Timeout     time.Duration     `json:"timeout"`     // 全局超时（默认5分钟）
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// AgentStep 单个Agent步骤定义
type AgentStep struct {
	ID          string        `json:"id"`                     // 唯一标识
	Name        string        `json:"name"`                   // 显示名称
	Agent       agent.Agent   `json:"-"`                      // Agent实例
	Prompt      string        `json:"prompt,omitempty"`       // 初始提示
	InputFrom   []string      `json:"input_from,omitempty"`   // 输入来源（其他步骤ID）
	OutputKey   string        `json:"output_key,omitempty"`   // 输出键名
	Condition   StepCondition `json:"condition,omitempty"`    // 执行条件
	RetryPolicy *RetryPolicy  `json:"retry_policy,omitempty"` // 重写全局重试策略
	Timeout     time.Duration `json:"timeout,omitempty"`      // 步骤超时
	Priority    int           `json:"priority"`               // 优先级（用于并行排序）
}

// StepCondition 步骤执行条件
type StepCondition struct {
	Type     string `json:"type"`               // "always", "on_success", "on_failure", "custom"
	Field    string `json:"field,omitempty"`    // 检查的字段
	Operator string `json:"operator,omitempty"` // "==", "!=", "contains", "empty", "not_empty"
	Value    any    `json:"value,omitempty"`    // 期望值
}

// RetryPolicy 重试策略
type RetryPolicy struct {
	MaxRetries int           `json:"max_retries"`
	Backoff    time.Duration `json:"backoff"` // 退避时间
	Jitter     bool          `json:"jitter"`  // 是否添加随机抖动
}

// DAGEdge DAG边定义
type DAGEdge struct {
	From string `json:"from"` // 源节点ID
	To   string `json:"to"`   // 目标节点ID
}

// ExecutionResult 执行结果
type ExecutionResult struct {
	OrchestratorID string                 `json:"orchestrator_id"`
	Mode           OrchestratorMode       `json:"mode"`
	Status         ExecutionStatus        `json:"status"`
	Duration       time.Duration          `json:"duration"`
	StartTime      time.Time              `json:"start_time"`
	EndTime        time.Time              `json:"end_time"`
	Steps          map[string]*StepResult `json:"steps"` // key=step.ID
	FinalOutput    map[string]any         `json:"final_output"`
	Error          error                  `json:"error,omitempty"`
	Metrics        ExecutionMetrics       `json:"metrics"`
}

// StepResult 单步执行结果
type StepResult struct {
	StepID       string          `json:"step_id"`
	StepName     string          `json:"step_name"`
	Status       StepStatus      `json:"status"`
	Response     *agent.Response `json:"response,omitempty"`
	Output       map[string]any  `json:"output"`
	Error        error           `json:"error,omitempty"`
	Duration     time.Duration   `json:"duration"`
	StartTime    time.Time       `json:"start_time"`
	EndTime      time.Time       `json:"end_time"`
	RetryCount   int             `json:"retry_count"`
	Dependencies []string        `json:"dependencies_completed"`
}

// ExecutionStatus 整体状态
type ExecutionStatus string

const (
	StatusRunning   ExecutionStatus = "running"
	StatusCompleted ExecutionStatus = "completed"
	StatusFailed    ExecutionStatus = "failed"
	StatusPartial   ExecutionStatus = "partial" // 部分成功（DAG/Parallel）
	StatusCancelled ExecutionStatus = "cancelled"
)

// StepStatus 单步状态
type StepStatus string

const (
	StepPending   StepStatus = "pending"
	StepRunning   StepStatus = "running"
	StepCompleted StepStatus = "completed"
	StepFailed    StepStatus = "failed"
	StepSkipped   StepStatus = "skipped"
)

// ExecutionMetrics 执行指标
type ExecutionMetrics struct {
	TotalSteps      int           `json:"total_steps"`
	CompletedSteps  int           `json:"completed_steps"`
	FailedSteps     int           `json:"failed_steps"`
	SkippedSteps    int           `json:"skipped_steps"`
	TotalDuration   time.Duration `json:"total_duration"`
	AvgStepDuration time.Duration `json:"avg_step_duration"`
	MaxStepDuration time.Duration `json:"max_step_duration"`
	MinStepDuration time.Duration `json:"min_step_duration"`
	TotalRetries    int           `json:"total_retries"`
	ConcurrencyUsed int           `json:"concurrency_used"` // 实际并发数
}

// Orchestrator 多Agent编排器
type Orchestrator struct {
	config   OrchestratorConfig
	steps    []*AgentStep
	dagEdges []DAGEdge
	mu       sync.RWMutex
	eventCh  chan *OrchestrationEvent
}

// OrchestrationEvent 编排事件
type OrchestrationEvent struct {
	Type      string    `json:"type"` // step_started, step_completed, step_failed, execution_started, execution_completed
	Timestamp time.Time `json:"timestamp"`
	StepID    string    `json:"step_id,omitempty"`
	Data      any       `json:"data,omitempty"`
}

// NewOrchestrator 创建新的编排器
func NewOrchestrator(config OrchestratorConfig) *Orchestrator {
	if config.MaxRetries <= 0 {
		config.MaxRetries = defaultMaxRetries
	}
	if config.Timeout <= 0 {
		config.Timeout = defaultOrchTimeout
	}
	if config.Mode == "" {
		config.Mode = SequentialMode
	}

	return &Orchestrator{
		config:   config,
		steps:    make([]*AgentStep, 0),
		dagEdges: make([]DAGEdge, 0),
		eventCh:  make(chan *OrchestrationEvent, orchEventBufferSize),
	}
}

// AddStep 添加步骤
func (o *Orchestrator) AddStep(step *AgentStep) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	for _, s := range o.steps {
		if s.ID == step.ID {
			return fmt.Errorf("duplicate step ID: %s", step.ID)
		}
	}

	o.steps = append(o.steps, step)
	return nil
}

// AddEdge 添加DAG边（仅DAG模式有效）
func (o *Orchestrator) AddEdge(from, to string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.config.Mode != DAGMode {
		return fmt.Errorf("edges only supported in DAG mode")
	}

	o.dagEdges = append(o.dagEdges, DAGEdge{From: from, To: to})
	return nil
}

// Execute 执行编排流程
func (o *Orchestrator) Execute(ctx context.Context, initialInput map[string]any) (*ExecutionResult, error) {
	startTime := time.Now()

	result := &ExecutionResult{
		OrchestratorID: o.config.Name,
		Mode:           o.config.Mode,
		Status:         StatusRunning,
		StartTime:      startTime,
		Steps:          make(map[string]*StepResult),
		FinalOutput:    make(map[string]any),
	}

	o.emitEvent("execution_started", "", map[string]any{
		"mode":  o.config.Mode,
		"steps": len(o.steps),
		"input": initialInput,
	})

	maxConcurrency := len(o.steps)
	if maxConcurrency <= 0 {
		maxConcurrency = defaultWorkerMaxConcurrency
	}
	engine := NewExecutionEngine(ExecutionEngineConfig{
		MaxConcurrency: maxConcurrency,
		RetryPolicy:    RetryPolicy{MaxRetries: o.config.MaxRetries, Backoff: defaultBackoff},
		FailFast:       true,
		EventCh:        o.eventCh,
	})

	execResult, err := engine.Run(ctx, o.config.Mode, o.steps, o.dagEdges, initialInput)
	if err != nil && execResult == nil {
		result.Error = err
		result.Status = StatusFailed
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		o.emitEvent("execution_completed", "", map[string]any{
			"status":   result.Status,
			"duration": result.Duration,
			"error":    err,
		})
		return result, err
	}

	*result = *execResult
	result.OrchestratorID = o.config.Name
	result.Error = err

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)
	result.Metrics = calculateMetrics(result)

	o.emitEvent("execution_completed", "", map[string]any{
		"status":   result.Status,
		"duration": result.Duration,
		"error":    err,
	})

	return result, err
}

// 辅助方法

func (o *Orchestrator) emitEvent(eventType, stepID string, data any) {
	select {
	case o.eventCh <- &OrchestrationEvent{
		Type:      eventType,
		Timestamp: time.Now(),
		StepID:    stepID,
		Data:      data,
	}:
	default:
	}
}

// Events 返回事件通道
func (o *Orchestrator) Events() <-chan *OrchestrationEvent {
	return o.eventCh
}

// Stats 返回统计信息
func (o *Orchestrator) Stats() map[string]any {
	o.mu.RLock()
	defer o.mu.RUnlock()

	return map[string]any{
		"name":        o.config.Name,
		"mode":        o.config.Mode,
		"total_steps": len(o.steps),
		"description": o.config.Description,
	}
}

// Export 导出配置为JSON（perf-v5 Task 5：锁内只快照，锁外 marshal）
func (o *Orchestrator) Export() ([]byte, error) {
	o.mu.RLock()
	data := map[string]any{
		"config":    o.config,
		"steps":     o.steps,
		"dag_edges": o.dagEdges,
	}
	o.mu.RUnlock()
	return json.MarshalIndent(data, "", "  ")
}

// ===== tool函数 =====
