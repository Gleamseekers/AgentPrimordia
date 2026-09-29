package debugger

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent"
	"github.com/Gleamseekers/AgentPrimordia/internal/orchestration"
)

// 默认execution timeout时间
const defaultExecutionTimeout = 5 * time.Minute

// VisualEditor 可视化编排编辑器
type VisualEditor struct {
	mu            sync.RWMutex
	orchestrators map[string]*orchestration.Orchestrator
	configs       map[string]*EditorConfig
	executions    map[string]*ExecutionRecord
	agents        map[string]agent.Agent // 已注册的 Agent 实例（按节点 ID）
}

// EditorConfig 编辑器配置（可序列化的编排配置）
type EditorConfig struct {
	ID          string                         `json:"id"`
	Name        string                         `json:"name"`
	Description string                         `json:"description"`
	Mode        orchestration.OrchestratorMode `json:"mode"`
	Nodes       []WorkflowNode                 `json:"nodes"`
	Edges       []WorkflowEdge                 `json:"edges"`
	CreatedAt   time.Time                      `json:"created_at"`
	UpdatedAt   time.Time                      `json:"updated_at"`
}

// WorkflowNode 工作流节点
type WorkflowNode struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"` // "agent", "start", "end", "condition"
	Name     string                 `json:"name"`
	Position NodePosition           `json:"position"`
	Config   map[string]interface{} `json:"config"`
}

// NodePosition 节点位置
type NodePosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// WorkflowEdge 工作流边
type WorkflowEdge struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Target string `json:"target"`
	Label  string `json:"label,omitempty"`
}

// ExecutionRecord 执行记录
type ExecutionRecord struct {
	ID          string                               `json:"id"`
	ConfigID    string                               `json:"config_id"`
	Status      orchestration.ExecutionStatus        `json:"status"`
	StartTime   time.Time                            `json:"start_time"`
	EndTime     time.Time                            `json:"end_time,omitempty"`
	Duration    time.Duration                        `json:"duration,omitempty"`
	StepResults map[string]*orchestration.StepResult `json:"step_results"`
	FinalOutput map[string]interface{}               `json:"final_output"`
	Error       string                               `json:"error,omitempty"`
}

// NewVisualEditor 创建可视化编辑器
func NewVisualEditor() *VisualEditor {
	return &VisualEditor{
		orchestrators: make(map[string]*orchestration.Orchestrator),
		configs:       make(map[string]*EditorConfig),
		executions:    make(map[string]*ExecutionRecord),
		agents:        make(map[string]agent.Agent),
	}
}

// RegisterAgent 注册 Agent 实例，供编排执行时使用
// nodeID 对应 EditorConfig.Nodes 中 "agent" 类型节点的 ID
func (ve *VisualEditor) RegisterAgent(nodeID string, a agent.Agent) {
	ve.mu.Lock()
	defer ve.mu.Unlock()
	ve.agents[nodeID] = a
}

// buildOrchestrator 从 EditorConfig 构建编排器
func (ve *VisualEditor) buildOrchestrator(cfg *EditorConfig) (*orchestration.Orchestrator, error) {
	orch := orchestration.NewOrchestrator(orchestration.OrchestratorConfig{
		Name:        cfg.Name,
		Description: cfg.Description,
		Mode:        cfg.Mode,
		Timeout:     defaultExecutionTimeout,
	})

	// 将工作流节点转换为编排步骤
	agentCount := 0
	for _, node := range cfg.Nodes {
		if node.Type != "agent" {
			continue // 跳过 start/end/condition 等非 Agent 节点
		}

		step := &orchestration.AgentStep{
			ID:   node.ID,
			Name: node.Name,
		}

		// 从节点配置中提取 prompt
		if prompt, ok := node.Config["prompt"].(string); ok {
			step.Prompt = prompt
		}

		// 解析已注册的 Agent 实例，未注册时使用 echoAgent
		ve.mu.RLock()
		if a, exists := ve.agents[node.ID]; exists {
			step.Agent = a
		} else {
			step.Agent = &echoAgent{name: node.Name}
		}
		ve.mu.RUnlock()

		if err := orch.AddStep(step); err != nil {
			return nil, fmt.Errorf("failed to add step %s: %w", node.ID, err)
		}
		agentCount++
	}

	// 无 agent 节点时拒绝执行
	if agentCount == 0 {
		return nil, fmt.Errorf("config %q has no agent-type node, cannot execute orchestration", cfg.Name)
	}

	// DAG 模式下添加边
	if cfg.Mode == orchestration.DAGMode {
		for _, edge := range cfg.Edges {
			if err := orch.AddEdge(edge.Source, edge.Target); err != nil {
				return nil, fmt.Errorf("failed to add edge %s->%s: %w", edge.Source, edge.Target, err)
			}
		}
	}

	return orch, nil
}

// executeAsync 异步执行编排并更新执行记录
func (ve *VisualEditor) executeAsync(execRecord *ExecutionRecord, cfg *EditorConfig) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultExecutionTimeout)
	defer cancel()

	orch, err := ve.buildOrchestrator(cfg)
	if err != nil {
		ve.mu.Lock()
		execRecord.Status = orchestration.StatusFailed
		execRecord.Error = fmt.Sprintf("构建编排器失败: %v", err)
		execRecord.EndTime = time.Now()
		execRecord.Duration = execRecord.EndTime.Sub(execRecord.StartTime)
		ve.mu.Unlock()
		return
	}

	result, execErr := orch.Execute(ctx, map[string]any{
		"config_id":   cfg.ID,
		"config_name": cfg.Name,
	})

	ve.mu.Lock()
	defer ve.mu.Unlock()

	execRecord.EndTime = time.Now()
	execRecord.Duration = execRecord.EndTime.Sub(execRecord.StartTime)

	if result != nil {
		execRecord.Status = result.Status
		execRecord.StepResults = result.Steps
		execRecord.FinalOutput = result.FinalOutput
		if result.Error != nil {
			execRecord.Error = result.Error.Error()
		}
	} else {
		execRecord.Status = orchestration.StatusFailed
	}

	if execErr != nil && execRecord.Error == "" {
		execRecord.Error = execErr.Error()
	}

	// 如果状态为失败但错误信息仍为空，从步骤结果中提取
	if execRecord.Status == orchestration.StatusFailed && execRecord.Error == "" {
		for _, step := range execRecord.StepResults {
			if step != nil && step.Error != nil {
				execRecord.Error = fmt.Sprintf("步骤 %s 失败: %v", step.StepID, step.Error)
				break
			}
		}
		if execRecord.Error == "" {
			execRecord.Error = "执行失败（未知原因）"
		}
	}
}

// echoAgent 默认回显 Agent，用于未注册真实 Agent 时的占位执行
type echoAgent struct {
	name string
}

func (a *echoAgent) Run(_ context.Context, input agent.Message) (*agent.Response, error) {
	return &agent.Response{
		Content: fmt.Sprintf("[%s] echo: %s", a.name, input.Content),
	}, nil
}

func (a *echoAgent) StreamRun(_ context.Context, input agent.Message) (<-chan agent.StreamEvent, error) {
	ch := make(chan agent.StreamEvent, 1)
	ch <- agent.StreamEvent{Type: agent.StreamEventToken, Content: fmt.Sprintf("[%s] echo: %s", a.name, input.Content)}
	close(ch)
	return ch, nil
}

func (a *echoAgent) Stop()                   {}
func (a *echoAgent) Stats() agent.AgentStats { return agent.AgentStats{} }
func (a *echoAgent) Name() string            { return a.name }

// VisualEditorServer 可视化编辑器HTTP服务器
