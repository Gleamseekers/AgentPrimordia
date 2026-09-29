// dag_delegate.go — dag 子包 delegate 的类型别名，保持向后兼容
package agent

import (
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/dag"
)

// ===== 类型别名 =====

// AgentDelegateNode 将 Agent 实例包装为 DAG 节点
type AgentDelegateNode = dag.AgentDelegateNode

// SubWorkflowNode 将子工作流包装为 DAG 节点
type SubWorkflowNode = dag.SubWorkflowNode

// ===== 函数委托 =====

// NewAgentDelegateNode 创建 Agent 委派节点
func NewAgentDelegateNode(id string, agent Agent) *AgentDelegateNode {
	return dag.NewAgentDelegateNode(id, agent)
}

// NewSubWorkflowNode 创建子工作流节点
func NewSubWorkflowNode(id string, sub *DAGWorkflow) *SubWorkflowNode {
	return dag.NewSubWorkflowNode(id, sub)
}

// ===== 预定义 Input Mappers =====

// MapFromDependent 从指定依赖节点的输出获取输入
func MapFromDependent(nodeIDs ...string) func(map[string]*DAGNodeResult) string {
	return dag.MapFromDependent(nodeIDs...)
}

// MapConcatAll 拼接所有已完成节点的输出
func MapConcatAll() func(map[string]*DAGNodeResult) string {
	return dag.MapConcatAll()
}

// MapPassThrough 直接透传原始输入
func MapPassThrough(_ map[string]*DAGNodeResult) string {
	return dag.MapPassThrough(nil)
}

// MapTemplate 使用模板格式化多个节点输出
func MapTemplate(template string) func(map[string]*DAGNodeResult) string {
	return dag.MapTemplate(template)
}
