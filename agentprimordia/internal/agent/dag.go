// dag.go — dag 子包的类型别名，保持向后兼容
package agent

import (
	"github.com/Gleamseekers/AgentPrimordia/internal/agent/dag"
)

// ===== 错误变量别名 =====

var (
	ErrDagNodeNil         = dag.ErrDagNodeNil
	ErrDagNodeIDEmpty     = dag.ErrDagNodeIDEmpty
	ErrDagEdgeEmpty       = dag.ErrDagEdgeEmpty
	ErrDagSubDAGNameEmpty = dag.ErrDagSubDAGNameEmpty
	ErrDagSubDAGNil       = dag.ErrDagSubDAGNil
	ErrDagCycle           = dag.ErrDagCycle
	ErrDagCycleTopo       = dag.ErrDagCycleTopo
)

// ===== 类型别名 =====

// DAGNode DAG 工作流节点
type DAGNode = dag.DAGNode

// RetryPolicy 节点重试策略
type RetryPolicy = dag.RetryPolicy

// DAGEdge DAG 工作流边
type DAGEdge = dag.DAGEdge

// DAGNodeResult 节点执行结果
type DAGNodeResult = dag.DAGNodeResult

// DAGResult DAG 工作流执行结果
type DAGResult = dag.DAGResult

// DAGWorkflow DAG 工作流引擎
type DAGWorkflow = dag.DAGWorkflow

// DAGMetrics DAG 执行指标
type DAGMetrics = dag.DAGMetrics

// NodeExecutionStats 单节点执行统计
type NodeExecutionStats = dag.NodeExecutionStats

// ===== 函数委托 =====

// NewDAGWorkflow 创建 DAG 工作流
func NewDAGWorkflow() *DAGWorkflow {
	return dag.NewDAGWorkflow()
}
