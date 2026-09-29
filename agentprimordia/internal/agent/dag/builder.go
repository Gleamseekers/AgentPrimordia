package dag

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/core"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/hooks"
)

// NodeHandler 节点处理函数类型
type NodeHandler func(ctx context.Context, input string) (string, error)

// NodeHandlerFunc 将普通函数包装为 core.Agent 接口（用于 DSL 构建器）
type NodeHandlerFunc struct {
	handler NodeHandler
	nodeID  string
}

func (n *NodeHandlerFunc) Name() string {
	return n.nodeID
}

func (n *NodeHandlerFunc) Stats() core.AgentStats {
	return core.AgentStats{}
}

func (n *NodeHandlerFunc) StreamRun(ctx context.Context, input core.Message) (<-chan core.StreamEvent, error) {
	resp, err := n.Run(ctx, input)
	if err != nil {
		return nil, err
	}
	ch := make(chan core.StreamEvent, 1)
	ch <- core.StreamEvent{Type: core.StreamEventComplete, Content: resp.Content}
	close(ch)
	return ch, nil
}

func (n *NodeHandlerFunc) Stop() {}

func (n *NodeHandlerFunc) Run(ctx context.Context, msg core.Message) (*core.Response, error) {
	result, err := n.handler(ctx, msg.Content)
	if err != nil {
		return nil, err
	}
	return &core.Response{Content: result}, nil
}

// DAGBuilder 声明式 DAG 构建器
//
// 提供链式 API 用于快速构建复杂工作流：
//
//	dag := NewDAGBuilder("research-workflow").
//	    Node("search", searchHandler).Label("Web Search").
//	    Node("extract", extractHandler).Label("Extract Info").
//	    Node("summarize", summarizeHandler).Label("Summarize").
//	    Edge("search", "extract").
//	    Edge("extract", "summarize").
//	    Build()
type DAGBuilder struct {
	workflow     *DAGWorkflow
	err          error
	lastNode     string
	pendingEdges []DAGEdge
}

// NewDAGBuilder 创建 DAG 构建器
func NewDAGBuilder(name string) *DAGBuilder {
	return &DAGBuilder{
		workflow:     NewDAGWorkflow().WithName(name),
		pendingEdges: make([]DAGEdge, 0),
	}
}

// Build 返回构建完成的 DAG 工作流
func (b *DAGBuilder) Build() (*DAGWorkflow, error) {
	if b.err != nil {
		return nil, b.err
	}
	for _, edge := range b.pendingEdges {
		if err := b.workflow.AddEdge(edge); err != nil {
			return nil, err
		}
	}
	b.pendingEdges = nil
	return b.workflow, nil
}

// MustBuild 构建 DAG，panic on error。
// 生产建议：使用 Build() 并处理 error。
func (b *DAGBuilder) MustBuild() *DAGWorkflow {
	dag, err := b.Build()
	if err != nil {
		slog.Error("dag MustBuild 失败", "error", err)
		panic(fmt.Errorf("dag build failed: %w", err))
	}
	return dag
}

// Node 添加节点（使用 Handler 函数）
func (b *DAGBuilder) Node(id string, handler NodeHandler) *DAGBuilder {
	if b.err != nil {
		return b
	}
	if id == "" {
		b.err = fmt.Errorf("dag builder: node ID cannot be empty")
		return b
	}
	node := &DAGNode{
		ID:    id,
		Agent: &NodeHandlerFunc{handler: handler, nodeID: id},
	}
	if err := b.workflow.AddNode(node); err != nil {
		b.err = err
		return b
	}
	b.lastNode = id
	return b
}

// NodeWithAgent 添加节点（使用已有的 core.Agent 实例）
func (b *DAGBuilder) NodeWithAgent(id string, agent core.Agent) *DAGBuilder {
	if b.err != nil {
		return b
	}
	node := &DAGNode{
		ID:    id,
		Agent: agent,
	}
	if err := b.workflow.AddNode(node); err != nil {
		b.err = err
		return b
	}
	b.lastNode = id
	return b
}

// Label 为上一个添加的节点设置标签
func (b *DAGBuilder) Label(label string) *DAGBuilder {
	if b.err != nil || b.lastNode == "" {
		return b
	}
	b.workflow.mu.RLock()
	node := b.workflow.nodes[b.lastNode]
	b.workflow.mu.RUnlock()
	if node != nil {
		if node.Metadata == nil {
			node.Metadata = make(map[string]string)
		}
		node.Metadata["label"] = label
	}
	return b
}

// Metadata 为最后一个节点设置元数据
func (b *DAGBuilder) Metadata(kv ...string) *DAGBuilder {
	if b.err != nil || b.lastNode == "" || len(kv)%2 != 0 {
		return b
	}
	b.workflow.mu.RLock()
	node := b.workflow.nodes[b.lastNode]
	b.workflow.mu.RUnlock()
	if node != nil {
		if node.Metadata == nil {
			node.Metadata = make(map[string]string)
		}
		for i := 0; i < len(kv); i += 2 {
			node.Metadata[kv[i]] = kv[i+1]
		}
	}
	return b
}

// Edge 添加边 from → to（支持前向引用）
func (b *DAGBuilder) Edge(from, to string) *DAGBuilder {
	if b.err != nil {
		return b
	}
	b.addEdgeOrPending(DAGEdge{From: from, To: to})
	return b
}

func (b *DAGBuilder) addEdgeOrPending(edge DAGEdge) {
	if err := b.workflow.AddEdge(edge); err != nil {
		b.pendingEdges = append(b.pendingEdges, edge)
	}
}

// EdgeWithCondition 添加条件边
func (b *DAGBuilder) EdgeWithCondition(from, to string, cond func(ctx context.Context, result *DAGNodeResult) bool) *DAGBuilder {
	if b.err != nil {
		return b
	}
	b.addEdgeOrPending(DAGEdge{From: from, To: to, Condition: cond})
	return b
}

// LabeledEdge 添加带标签的边
func (b *DAGBuilder) LabeledEdge(from, to, label string) *DAGBuilder {
	if b.err != nil {
		return b
	}
	b.addEdgeOrPending(DAGEdge{From: from, To: to, Label: label})
	return b
}

// Chain 串联节点：A → B → C → ...
func (b *DAGBuilder) Chain(nodeIDs ...string) *DAGBuilder {
	if b.err != nil {
		return b
	}
	for i := 0; i < len(nodeIDs)-1; i++ {
		b.addEdgeOrPending(DAGEdge{From: nodeIDs[i], To: nodeIDs[i+1]})
	}
	return b
}

// FanOut 从源节点扇出到多个目标节点
func (b *DAGBuilder) FanOut(from string, targets ...string) *DAGBuilder {
	if b.err != nil {
		return b
	}
	for _, target := range targets {
		b.addEdgeOrPending(DAGEdge{From: from, To: target})
	}
	return b
}

// FanIn 多个源节点汇聚到目标节点
func (b *DAGBuilder) FanIn(to string, sources ...string) *DAGBuilder {
	if b.err != nil {
		return b
	}
	for _, source := range sources {
		b.addEdgeOrPending(DAGEdge{From: source, To: to})
	}
	return b
}

// LinkTo 连接最后一个节点到指定节点
func (b *DAGBuilder) LinkTo(to string) *DAGBuilder {
	if b.err != nil || b.lastNode == "" {
		return b
	}
	return b.Edge(b.lastNode, to)
}

// NodePair 节点定义对（ID + Handler）
type NodePair struct {
	ID      string
	Handler NodeHandler
}

// Node 创建节点对（用于 Sequential/Parallel）
func MakeNode(id string, handler NodeHandler) NodePair {
	return NodePair{ID: id, Handler: handler}
}

// Sequential 创建顺序执行链：每个节点按顺序连接
//
// 用法：
//
//	builder.Sequential(
//	    MakeNode("step1", step1Handler),
//	    MakeNode("step2", step2Handler),
//	    MakeNode("step3", step3Handler),
//	)
func (b *DAGBuilder) Sequential(pairs ...NodePair) *DAGBuilder {
	if b.err != nil {
		return b
	}
	var ids []string
	for _, pair := range pairs {
		b.Node(pair.ID, pair.Handler)
		ids = append(ids, pair.ID)
	}
	if len(ids) > 1 {
		b.Chain(ids...)
	}
	return b
}

// Parallel 创建并行执行模式：fan-out → [nodes] → fan-in
//
// 用法：
//
//	builder.Parallel("split", splitHandler, "merge", mergeHandler,
//	    MakeNode("task_a", taskAHandler),
//	    MakeNode("task_b", taskBHandler),
//	)
func (b *DAGBuilder) Parallel(fanOutID string, fanOutHandler NodeHandler, fanInID string, fanInHandler NodeHandler, tasks ...NodePair) *DAGBuilder {
	if b.err != nil {
		return b
	}

	b.Node(fanOutID, fanOutHandler)
	b.Node(fanInID, fanInHandler)

	var taskIDs []string
	for _, task := range tasks {
		b.Node(task.ID, task.Handler)
		taskIDs = append(taskIDs, task.ID)
	}

	b.FanOut(fanOutID, taskIDs...)
	b.FanIn(fanInID, taskIDs...)

	return b
}

// Conditional 创建条件分支：source → {condition: trueTarget, falseTarget}
func (b *DAGBuilder) Conditional(source, trueTarget, falseTarget string, condition func(ctx context.Context, result *DAGNodeResult) bool) *DAGBuilder {
	if b.err != nil {
		return b
	}
	b.EdgeWithCondition(source, trueTarget, condition)
	b.EdgeWithCondition(source, falseTarget, func(ctx context.Context, result *DAGNodeResult) bool {
		return !condition(ctx, result)
	})
	return b
}

// WithRetry 为最后一个节点设置重试策略
func (b *DAGBuilder) WithRetry(maxRetries int, delayMs int) *DAGBuilder {
	if b.err != nil || b.lastNode == "" {
		return b
	}
	b.workflow.mu.RLock()
	node := b.workflow.nodes[b.lastNode]
	b.workflow.mu.RUnlock()
	if node != nil {
		node.RetryPolicy = &RetryPolicy{
			MaxRetries: maxRetries,
			Delay:      time.Duration(delayMs) * time.Millisecond,
			Backoff:    2.0,
		}
	}
	return b
}

// WithHooks 设置工作流钩子
func (b *DAGBuilder) WithHooks(h hooks.Hooks) *DAGBuilder {
	if b.err != nil {
		return b
	}
	b.workflow.SetHooks(h)
	return b
}

// SubDAG 嵌入子工作流
func (b *DAGBuilder) SubDAG(name string, sub *DAGWorkflow) *DAGBuilder {
	if b.err != nil {
		return b
	}
	b.err = b.workflow.AddSubDAG(name, sub)
	return b
}

// ===== 常用条件谓词 =====

// ConditionOnOutput 输出包含指定文本时为 true
func ConditionOnOutput(substr string) func(ctx context.Context, result *DAGNodeResult) bool {
	return func(_ context.Context, result *DAGNodeResult) bool {
		if result == nil {
			return false
		}
		return containsString(result.Output, substr)
	}
}

// ConditionOnError 出错时走此分支
func ConditionOnError() func(ctx context.Context, result *DAGNodeResult) bool {
	return func(_ context.Context, result *DAGNodeResult) bool {
		return result != nil && result.Error != nil
	}
}

// ConditionOnSuccess 成功时走此分支
func ConditionOnSuccess() func(ctx context.Context, result *DAGNodeResult) bool {
	return func(_ context.Context, result *DAGNodeResult) bool {
		return result != nil && result.Error == nil
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
