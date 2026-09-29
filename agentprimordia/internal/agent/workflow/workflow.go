// Package workflow 提供工作流类型定义、配置、状态结构与基础增删改查 API。
//
// 工作流支持五种类型：线性、条件分支、循环、并行分叉合并、状态机。
// 本包从 internal/agent 父包提取，通过 core 子包引用 Agent 接口，
// 父包通过类型别名保持向后兼容。
package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/core"
)

const (
	defaultMaxIterations      = 100
	defaultWorkflowTimeout    = 10 * time.Minute
	defaultWorkflowMaxRetries = 3
	workflowEventBufferSize   = 200
)

// WorkflowType 工作流类型
type WorkflowType string

const (
	// LinearWorkflow 线性工作流
	LinearWorkflow WorkflowType = "linear"
	// ConditionalWorkflow 条件分支工作流
	ConditionalWorkflow WorkflowType = "conditional"
	// LoopWorkflow 循环工作流
	LoopWorkflow WorkflowType = "loop"
	// ParallelForkJoin 并行分叉合并工作流
	ParallelForkJoin WorkflowType = "parallel_fork_join"
	// StateMachine 状态机工作流
	StateMachine WorkflowType = "state_machine"
)

// WorkflowStatus 工作流状态
type WorkflowStatus string

const (
	WfStatusPending   WorkflowStatus = "pending"
	WfStatusRunning   WorkflowStatus = "running"
	WfStatusPaused    WorkflowStatus = "paused"
	WfStatusCompleted WorkflowStatus = "completed"
	WfStatusFailed    WorkflowStatus = "failed"
	WfStatusCancelled WorkflowStatus = "cancelled"
)

// WfRetryPolicy is the workflow-level retry policy referenced by WorkflowConfig.
// It is distinct from DAG RetryPolicy in dag.go; workflow retries are governed
// at execution time by ErrorHandling.MaxRetries.
type WfRetryPolicy struct {
	MaxAttempts     int           `json:"max_attempts"`
	BackoffInterval time.Duration `json:"backoff_interval"`
}

// WorkflowConfig 工作流配置
type WorkflowConfig struct {
	Type          WorkflowType   `json:"type"`
	Name          string         `json:"name"`
	Description   string         `json:"description,omitempty"`
	MaxIterations int            `json:"max_iterations"`
	Timeout       time.Duration  `json:"timeout"`
	RetryPolicy   *WfRetryPolicy `json:"retry_policy,omitempty"`
	ErrorHandling ErrorHandling  `json:"error_handling"`
	EnableLogging bool           `json:"enable_logging"`
	SaveSnapshot  bool           `json:"save_snapshot"`
}

// ErrorHandling 错误处理策略
type ErrorHandling struct {
	OnError         string `json:"on_error"`
	MaxRetries      int    `json:"max_retries"`
	FallbackStep    string `json:"fallback_step,omitempty"`
	ContinueOnError bool   `json:"continue_on_error"`
}

// WorkflowNode 工作流节点
type WorkflowNode struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Type          NodeType          `json:"type"`
	Agent         core.Agent        `json:"-"`
	Config        *NodeConfig       `json:"config,omitempty"`
	Condition     *NodeCondition    `json:"condition,omitempty"`
	Transitions   []*Transition     `json:"transitions,omitempty"`
	InputMapping  map[string]string `json:"input_mapping,omitempty"`
	OutputMapping map[string]string `json:"output_mapping,omitempty"`
	Metadata      map[string]any    `json:"metadata,omitempty"`
}

// NodeType 节点类型
type NodeType string

const (
	TaskNode      NodeType = "task"
	ConditionNode NodeType = "condition"
	ParallelNode  NodeType = "parallel"
	LoopStartNode NodeType = "loop_start"
	LoopEndNode   NodeType = "loop_end"
	FallbackNode  NodeType = "fallback"
	SubWfNode     NodeType = "sub_workflow"
)

// NodeConfig 节点配置
type NodeConfig struct {
	PromptTemplate string                                                                  `json:"prompt_template,omitempty"`
	Parameters     map[string]any                                                          `json:"parameters,omitempty"`
	Timeout        time.Duration                                                           `json:"timeout,omitempty"`
	RetryCount     int                                                                     `json:"retry_count,omitempty"`
	CustomLogic    func(ctx context.Context, input map[string]any) (map[string]any, error) `json:"-"`
}

// NodeCondition 节点执行条件
type NodeCondition struct {
	Type       string                    `json:"type"`
	Expression string                    `json:"expression,omitempty"`
	CustomFunc func(map[string]any) bool `json:"-"`
	Field      string                    `json:"field,omitempty"`
	Operator   string                    `json:"operator,omitempty"`
	Value      any                       `json:"value,omitempty"`
}

// Transition 转换/边
type Transition struct {
	ID        string               `json:"id"`
	From      string               `json:"from"`
	To        string               `json:"to"`
	Condition *TransitionCondition `json:"condition,omitempty"`
	Weight    float64              `json:"weight"`
}

// TransitionCondition 转换条件
type TransitionCondition struct {
	Type        string  `json:"type"`
	Expression  string  `json:"expression,omitempty"`
	Probability float64 `json:"probability,omitempty"`
	Field       string  `json:"field,omitempty"`
	Operator    string  `json:"operator,omitempty"`
	Value       any     `json:"value,omitempty"`
}

// WorkflowExecution 工作流执行实例
type WorkflowExecution struct {
	mu             sync.RWMutex
	config         WorkflowConfig
	nodes          map[string]*WorkflowNode
	transitions    map[string][]*Transition
	startNodeID    string
	currentNode    *WorkflowNode
	variables      map[string]any
	history        []*ExecutionRecord
	status         WorkflowStatus
	result         *WorkflowResult
	eventCh        chan *WorkflowEvent
	executionCtx   context.Context
	cancelFunc     context.CancelFunc
	pauseCh        chan struct{} // 关闭时恢复执行
	iterationCount int
	nodeExecutions map[string]int
}

// ExecutionRecord 执行记录
type ExecutionRecord struct {
	NodeID    string              `json:"node_id"`
	NodeName  string              `json:"node_name"`
	Status    NodeExecutionStatus `json:"status"`
	Input     map[string]any      `json:"input,omitempty"`
	Output    map[string]any      `json:"output,omitempty"`
	Error     error               `json:"error,omitempty"`
	Duration  time.Duration       `json:"duration"`
	Timestamp time.Time           `json:"timestamp"`
	Iteration int                 `json:"iteration"`
}

// NodeExecutionStatus 节点执行状态
type NodeExecutionStatus string

const (
	NodePending   NodeExecutionStatus = "pending"
	NodeRunning   NodeExecutionStatus = "running"
	NodeCompleted NodeExecutionStatus = "completed"
	NodeSkipped   NodeExecutionStatus = "skipped"
	NodeFailed    NodeExecutionStatus = "failed"
)

// WorkflowResult 工作流结果
type WorkflowResult struct {
	ExecutionID string             `json:"execution_id"`
	Status      WorkflowStatus     `json:"status"`
	Output      map[string]any     `json:"output,omitempty"`
	Variables   map[string]any     `json:"variables,omitempty"`
	Records     []*ExecutionRecord `json:"records,omitempty"`
	Metrics     *WorkflowMetrics   `json:"metrics,omitempty"`
	Error       error              `json:"error,omitempty"`
	StartTime   time.Time          `json:"start_time"`
	EndTime     time.Time          `json:"end_time"`
	Duration    time.Duration      `json:"duration"`
	PathTaken   []string           `json:"path_taken,omitempty"`
}

// WorkflowMetrics 工作流指标
// perf-v6 Task 9：所有 int 计数器改 atomic.Int64（无锁累加），Duration 字段保留
type WorkflowMetrics struct {
	TotalNodes       atomic.Int64  `json:"total_nodes"`
	ExecutedNodes    atomic.Int64  `json:"executed_nodes"`
	FailedNodes      atomic.Int64  `json:"failed_nodes"`
	SkippedNodes     atomic.Int64  `json:"skipped_nodes"`
	TotalDurationNs  atomic.Int64  `json:"-"`
	AvgNodeDuration  time.Duration `json:"avg_node_duration"`
	Iterations       atomic.Int64  `json:"iterations"`
	BranchesTaken    atomic.Int64  `json:"branches_taken"`
	RetriesAttempted atomic.Int64  `json:"retries_attempted"`
}

// WorkflowEvent 工作流事件
type WorkflowEvent struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	NodeID    string    `json:"node_id,omitempty"`
	Data      any       `json:"data,omitempty"`
}

// NewWorkflowExecution 创建新的工作流执行实例
func NewWorkflowExecution(config WorkflowConfig) *WorkflowExecution {
	if config.MaxIterations <= 0 {
		config.MaxIterations = defaultMaxIterations
	}
	if config.Timeout <= 0 {
		config.Timeout = defaultWorkflowTimeout
	}
	if config.ErrorHandling.OnError == "" {
		config.ErrorHandling.OnError = "fail"
	}
	if config.ErrorHandling.MaxRetries <= 0 {
		config.ErrorHandling.MaxRetries = defaultWorkflowMaxRetries
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &WorkflowExecution{
		config:         config,
		nodes:          make(map[string]*WorkflowNode),
		transitions:    make(map[string][]*Transition),
		variables:      make(map[string]any),
		history:        make([]*ExecutionRecord, 0),
		status:         WfStatusPending,
		eventCh:        make(chan *WorkflowEvent, workflowEventBufferSize),
		executionCtx:   ctx,
		cancelFunc:     cancel,
		pauseCh:        make(chan struct{}, 1),
		nodeExecutions: make(map[string]int),
	}
}

// AddNode 添加节点
func (w *WorkflowExecution) AddNode(node *WorkflowNode) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if node.ID == "" || (node.Agent == nil && node.Type == TaskNode) {
		return fmt.Errorf("invalid node")
	}

	w.nodes[node.ID] = node
	return nil
}

// AddTransition 添加转换
func (w *WorkflowExecution) AddTransition(transition *Transition) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if transition.From == "" || transition.To == "" {
		return fmt.Errorf("invalid transition")
	}

	w.transitions[transition.From] = append(w.transitions[transition.From], transition)
	return nil
}

// SetStartNode 设置起始节点
func (w *WorkflowExecution) SetStartNode(nodeID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, exists := w.nodes[nodeID]; !exists {
		return fmt.Errorf("node not found: %s", nodeID)
	}

	w.startNodeID = nodeID
	return nil
}

// SetVariable 设置变量
func (w *WorkflowExecution) SetVariable(key string, value any) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.variables[key] = value
}

// GetVariable 获取变量
func (w *WorkflowExecution) GetVariable(key string) (any, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	val, exists := w.variables[key]
	return val, exists
}

// Export 导出为JSON
func (w *WorkflowExecution) Export() ([]byte, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	data := map[string]any{
		"config":      w.config,
		"nodes":       w.nodes,
		"transitions": w.transitions,
		"variables":   w.variables,
		"status":      w.status,
		"result":      w.result,
	}
	return json.MarshalIndent(data, "", "  ")
}
