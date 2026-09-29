// dag_builder.go — dag 子包 builder 的类型别名，保持向后兼容
package agent

import (
	"context"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent/dag"
)

// ===== 类型别名 =====

// NodeHandler 节点处理函数类型
type NodeHandler = dag.NodeHandler

// NodeHandlerFunc 将普通函数包装为 Agent 接口
type NodeHandlerFunc = dag.NodeHandlerFunc

// DAGBuilder 声明式 DAG 构建器
type DAGBuilder = dag.DAGBuilder

// NodePair 节点定义对
type NodePair = dag.NodePair

// ===== 函数委托 =====

// NewDAGBuilder 创建 DAG 构建器
func NewDAGBuilder(name string) *DAGBuilder {
	return dag.NewDAGBuilder(name)
}

// MakeNode 创建节点对
func MakeNode(id string, handler NodeHandler) NodePair {
	return dag.MakeNode(id, handler)
}

// ===== 条件谓词别名 =====

// ConditionOnOutput 输出包含指定文本时为 true
func ConditionOnOutput(substr string) func(ctx context.Context, result *DAGNodeResult) bool {
	return dag.ConditionOnOutput(substr)
}

// ConditionOnError 出错时走此分支
func ConditionOnError() func(ctx context.Context, result *DAGNodeResult) bool {
	return dag.ConditionOnError()
}

// ConditionOnSuccess 成功时走此分支
func ConditionOnSuccess() func(ctx context.Context, result *DAGNodeResult) bool {
	return dag.ConditionOnSuccess()
}
