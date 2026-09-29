// multi_agent.go — 多 Agent 分工编排公共 API 导出
//
// Stability: Experimental
//
// 说明（评估报告 §2.1-24）：internal/multi_agent.Swarm 实现完整（专业路由 +
// 泛化兜底，规模 1/2/4/8 成功率不降），但此前零生产消费方——未通过公共 API
// 暴露，ROADMAP「已接入」叙事不成立。本文件将其以 Experimental 级别导出，
// 使能力可经 pkg 使用并接受生态验证；转正需等真实消费方与外部基准。
package ap

import "github.com/Gleamseekers/AgentPrimordia/internal/multi_agent"

// Specialist 专业分工的 Agent。
// Stability: Experimental
type Specialist = multi_agent.Specialist

// Swarm 专业分工 Agent 组（多 Agent 编排）。
// 方法与字段随别名直接可用：NewSwarm → Execute/ExecuteSequential。
// Stability: Experimental
type Swarm = multi_agent.Swarm

// SwarmResult 分工执行结果。
// Stability: Experimental
type SwarmResult = multi_agent.SwarmResult

// NewSwarm 创建 Swarm：专业 Specialist 按关键词路由，无命中回退 generalist。
// Stability: Experimental
func NewSwarm(specialists []Specialist, generalist Agent) *Swarm {
	return multi_agent.NewSwarm(specialists, generalist)
}
