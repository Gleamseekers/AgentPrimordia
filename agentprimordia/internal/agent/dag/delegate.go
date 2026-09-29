package dag

import (
	"context"
	"fmt"
	"strings"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/core"
)

// AgentDelegateNode 将 core.Agent 实例包装为 DAG 节点
//
// 支持输入映射：
//   - 直接传递原始输入
//   - 从前置节点输出中提取（通过 inputMapper）
//   - 拼接多个前置节点的输出
type AgentDelegateNode struct {
	agent       core.Agent
	nodeID      string
	inputMapper func(results map[string]*DAGNodeResult) string
	metadata    map[string]string
}

func NewAgentDelegateNode(id string, agent core.Agent) *AgentDelegateNode {
	return &AgentDelegateNode{
		agent:  agent,
		nodeID: id,
	}
}

func (n *AgentDelegateNode) Name() string { return n.nodeID }

func (n *AgentDelegateNode) Stats() core.AgentStats { return n.agent.Stats() }

func (n *AgentDelegateNode) Run(ctx context.Context, msg core.Message) (*core.Response, error) {
	return n.agent.Run(ctx, msg)
}

func (n *AgentDelegateNode) StreamRun(ctx context.Context, msg core.Message) (<-chan core.StreamEvent, error) {
	return n.agent.StreamRun(ctx, msg)
}

func (n *AgentDelegateNode) Stop() { n.agent.Stop() }

// WithInputMapper 设置输入映射函数（从已完成的节点结果中组装输入）
func (n *AgentDelegateNode) WithInputMapper(mapper func(results map[string]*DAGNodeResult) string) *AgentDelegateNode {
	n.inputMapper = mapper
	return n
}

// WithMetadata 设置元数据
func (n *AgentDelegateNode) WithMetadata(kv ...string) *AgentDelegateNode {
	if n.metadata == nil {
		n.metadata = make(map[string]string)
	}
	for i := 0; i < len(kv); i += 2 {
		n.metadata[kv[i]] = kv[i+1]
	}
	return n
}

// GetInputMapper 返回输入映射器（供 DAG 引擎使用）
func (n *AgentDelegateNode) GetInputMapper() func(map[string]*DAGNodeResult) string {
	return n.inputMapper
}

// SubWorkflowNode 将子工作流包装为 DAG 节点
//
// 执行时运行完整的子工作流，并将最终结果作为节点输出。
type SubWorkflowNode struct {
	subWorkflow *DAGWorkflow
	nodeID      string
	metadata    map[string]string
}

func NewSubWorkflowNode(id string, sub *DAGWorkflow) *SubWorkflowNode {
	return &SubWorkflowNode{
		subWorkflow: sub,
		nodeID:      id,
	}
}

func (n *SubWorkflowNode) Name() string { return n.nodeID }

func (n *SubWorkflowNode) Stats() core.AgentStats { return core.AgentStats{} }

func (n *SubWorkflowNode) Run(ctx context.Context, msg core.Message) (*core.Response, error) {
	result, err := n.subWorkflow.Run(ctx, msg.Content)
	if err != nil {
		return nil, err
	}
	var output strings.Builder
	for _, nodeID := range result.Order {
		if nr, ok := result.NodeResults[nodeID]; ok && nr.Output != "" {
			output.WriteString(nr.Output)
			output.WriteString("\n")
		}
	}
	return &core.Response{Content: strings.TrimSpace(output.String())}, nil
}

func (n *SubWorkflowNode) StreamRun(ctx context.Context, msg core.Message) (<-chan core.StreamEvent, error) {
	resp, err := n.Run(ctx, msg)
	if err != nil {
		return nil, err
	}
	ch := make(chan core.StreamEvent, 1)
	ch <- core.StreamEvent{Type: core.StreamEventComplete, Content: resp.Content}
	close(ch)
	return ch, nil
}

func (n *SubWorkflowNode) Stop() {}

// ===== 预定义 Input Mappers =====

// MapFromDependent 从指定依赖节点的输出获取输入
func MapFromDependent(nodeIDs ...string) func(map[string]*DAGNodeResult) string {
	return func(results map[string]*DAGNodeResult) string {
		var parts []string
		for _, id := range nodeIDs {
			if r, ok := results[id]; ok && r.Output != "" {
				parts = append(parts, r.Output)
			}
		}
		return strings.Join(parts, "\n")
	}
}

// MapConcatAll 拼接所有已完成节点的输出
func MapConcatAll() func(map[string]*DAGNodeResult) string {
	return func(results map[string]*DAGNodeResult) string {
		var parts []string
		for _, r := range results {
			if r.Output != "" {
				parts = append(parts, fmt.Sprintf("[%s] %s", r.NodeID, r.Output))
			}
		}
		return strings.Join(parts, "\n")
	}
}

// MapPassThrough 直接透传原始输入（默认行为）
func MapPassThrough(_ map[string]*DAGNodeResult) string {
	return ""
}

// MapTemplate 使用模板格式化多个节点输出
//
// 用法：
//
//	MapTemplate("Summary of {research}:\n{analysis}\n\nConclusion: {conclusion}")
func MapTemplate(template string) func(map[string]*DAGNodeResult) string {
	return func(results map[string]*DAGNodeResult) string {
		result := template
		for nodeID, r := range results {
			if r.Output != "" {
				result = strings.ReplaceAll(result, "{"+nodeID+"}", r.Output)
			}
		}
		return result
	}
}

// ===== DAGBuilder 扩展方法 =====

// DelegateNode 添加 core.Agent 委派节点
func (b *DAGBuilder) DelegateNode(id string, agent core.Agent) *DAGBuilder {
	if b.err != nil {
		return b
	}
	node := NewAgentDelegateNode(id, agent)
	if err := b.workflow.AddNode(&DAGNode{
		ID:       id,
		Agent:    node,
		Metadata: node.metadata,
	}); err != nil {
		b.err = err
		return b
	}
	b.lastNode = id
	return b
}

// SubWorkflowAsNode 将子工作流作为节点添加
func (b *DAGBuilder) SubWorkflowAsNode(id string, sub *DAGWorkflow) *DAGBuilder {
	if b.err != nil {
		return b
	}
	node := NewSubWorkflowNode(id, sub)
	if err := b.workflow.AddNode(&DAGNode{
		ID:       id,
		Agent:    node,
		Metadata: node.metadata,
	}); err != nil {
		b.err = err
		return b
	}
	b.lastNode = id
	return b
}
