// Package multi_agent 提供多 Agent 分工编排（v3.8-1）。
//
// 核心思想：单 Agent 能力有限，大任务拆分为子任务后分配给专业分工的
// Specialist Agent 并行/串行执行，显著扩大可处理的任务规模。
// 验收：任务规模 ×N 时成功率不降。
package multi_agent

import (
	"context"
	"strings"
	"sync"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent"
)

// Specialist 一个专业分工的 Agent。
type Specialist struct {
	// Name 专业名称（如 implementer / reviewer / tester）。
	Name string
	// Keywords 子任务描述命中任一关键词即路由到本 Agent。
	Keywords []string
	// Agent 实际执行 Agent（实现 core.Agent）。
	Agent agent.Agent
}

// Swarm 专业分工 Agent 组。
type Swarm struct {
	// Specialists 专业 Agent 列表（按声明顺序匹配）。
	Specialists []Specialist
	// Generalist 兜底通用 Agent（无专业命中时使用）。
	Generalist agent.Agent
}

// NewSwarm 创建 Swarm。
func NewSwarm(specialists []Specialist, generalist agent.Agent) *Swarm {
	return &Swarm{Specialists: specialists, Generalist: generalist}
}

// route 按子任务描述路由到专业 Agent，无命中时回退通用 Agent。
func (s *Swarm) route(task string) agent.Agent {
	for _, sp := range s.Specialists {
		for _, kw := range sp.Keywords {
			if strings.Contains(task, kw) {
				return sp.Agent
			}
		}
	}
	return s.Generalist
}

// SwarmResult 分工执行结果。
type SwarmResult struct {
	Total    int            `json:"total"`
	Passed   int            `json:"passed"`
	Failed   int            `json:"failed"`
	PassRate float64        `json:"pass_rate"`
	ByAgent  map[string]int `json:"by_agent"` // agent 名 → 通过数
}

// Execute 将一组子任务路由给对应 Specialist 并执行。
// taskPass 判定单个子任务是否成功（由调用方注入，支持 mock）。
func (s *Swarm) Execute(ctx context.Context, tasks []string, taskPass func(content string) bool) *SwarmResult {
	res := &SwarmResult{Total: len(tasks), ByAgent: make(map[string]int)}

	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, t := range tasks {
		wg.Add(1)
		go func(task string) {
			defer wg.Done()
			ag := s.route(task)
			resp, err := ag.Run(ctx, agent.Message{Role: agent.RoleUser, Content: task})
			ok := err == nil && resp != nil && taskPass(resp.Content)
			mu.Lock()
			defer mu.Unlock()
			if ok {
				res.Passed++
				res.ByAgent[agentName(ag)]++
			} else {
				res.Failed++
			}
		}(t)
	}
	wg.Wait()

	if res.Total > 0 {
		res.PassRate = float64(res.Passed) / float64(res.Total)
	}
	return res
}

// ExecuteSequential 串行版本（结果一致，便于确定性断言）。
func (s *Swarm) ExecuteSequential(ctx context.Context, tasks []string, taskPass func(content string) bool) *SwarmResult {
	res := &SwarmResult{Total: len(tasks), ByAgent: make(map[string]int)}
	for _, t := range tasks {
		ag := s.route(t)
		resp, err := ag.Run(ctx, agent.Message{Role: agent.RoleUser, Content: t})
		ok := err == nil && resp != nil && taskPass(resp.Content)
		if ok {
			res.Passed++
			res.ByAgent[agentName(ag)]++
		} else {
			res.Failed++
		}
	}
	if res.Total > 0 {
		res.PassRate = float64(res.Passed) / float64(res.Total)
	}
	return res
}

func agentName(a agent.Agent) string {
	if a == nil {
		return "unknown"
	}
	return a.Name()
}
