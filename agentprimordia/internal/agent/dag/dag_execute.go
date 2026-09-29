package dag

// 本文件从 dag.go 拆分而来，包含 DAG 工作流的执行逻辑（Run + 重试）。

import (
	"context"
	"maps"
	"sync"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/core"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/hooks"
)

func (d *DAGWorkflow) Run(ctx context.Context, input string) (*DAGResult, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}

	d.mu.RLock()
	nodes := make(map[string]*DAGNode, len(d.nodes))
	maps.Copy(nodes, d.nodes)
	edges := make([]DAGEdge, len(d.edges))
	copy(edges, d.edges)
	h := d.hooks
	d.mu.RUnlock()

	start := time.Now()

	outgoing := make(map[string][]int)
	incoming := make(map[string][]int)
	for id := range nodes {
		outgoing[id] = nil
		incoming[id] = nil
	}
	for i, edge := range edges {
		outgoing[edge.From] = append(outgoing[edge.From], i)
		incoming[edge.To] = append(incoming[edge.To], i)
	}

	remainingDeps := make(map[string]int)
	for id := range nodes {
		remainingDeps[id] = len(incoming[id])
	}

	result := &DAGResult{
		NodeResults: make(map[string]*DAGNodeResult),
		Order:       make([]string, 0, len(nodes)),
		TotalNodes:  len(nodes),
	}

	var stateMu sync.Mutex

	for len(result.NodeResults) < len(nodes) {
		var ready []string
		for id := range nodes {
			if _, done := result.NodeResults[id]; done {
				continue
			}
			if remainingDeps[id] == 0 {
				ready = append(ready, id)
			}
		}

		if len(ready) == 0 {
			break
		}

		for _, nodeID := range ready {
			result.NodeResults[nodeID] = &DAGNodeResult{NodeID: nodeID}
		}

		var wg sync.WaitGroup
		for _, nodeID := range ready {
			wg.Add(1)
			go func(nid string) {
				defer wg.Done()
				node := nodes[nid]
				nr := result.NodeResults[nid]

				// 统一在 defer 中完成 Order 追加与 remainingDeps 递减，
				// 确保无论执行成功/失败/跳过/panic 都能正确传播
				defer func() {
					stateMu.Lock()
					result.Order = append(result.Order, nid)
					for _, edgeIdx := range outgoing[nid] {
						dst := edges[edgeIdx].To
						if remainingDeps[dst] > 0 {
							remainingDeps[dst]--
						}
					}
					stateMu.Unlock()
				}()

				// 评估条件边：仅统计"活跃"的入边
				// perf-v5 Task 17：先锁内读取 srcResult 快照，锁外评估 edge.Condition 用户回调
				stateMu.Lock()
				type edgeSnapshot struct {
					edge      *DAGEdge
					srcResult *DAGNodeResult
				}
				snapshots := make([]edgeSnapshot, 0, len(incoming[nid]))
				for _, edgeIdx := range incoming[nid] {
					edge := &edges[edgeIdx]
					var src *DAGNodeResult
					if edge.Condition != nil {
						src = result.NodeResults[edge.From]
					}
					snapshots = append(snapshots, edgeSnapshot{edge: edge, srcResult: src})
				}
				hasIncoming := len(incoming[nid]) > 0
				stateMu.Unlock()

				// 锁外评估用户回调
				activeCount := 0
				for _, snap := range snapshots {
					if snap.edge.Condition == nil {
						activeCount++
					} else if snap.srcResult != nil && !snap.srcResult.Skipped && snap.srcResult.Error == nil && snap.edge.Condition(ctx, snap.srcResult) {
						activeCount++
					}
				}

				// 所有活跃条件均为 false → 跳过此节点
				if hasIncoming && activeCount == 0 {
					nr.Skipped = true
					nr.Timestamp = time.Now()
					stateMu.Lock()
					result.Skipped++
					stateMu.Unlock()
					return
				}

				nodeInput := input
				if node.Input != "" {
					nodeInput = node.Input
				}

				if h != nil {
					_ = h.Fire(ctx, &hooks.HookContext{
						Point:    hooks.HookBeforeDAGNode,
						AgentID:  nid,
						Metadata: map[string]any{"node_id": nid},
					})
				}

				nodeStart := time.Now()
				resp, retries, execErr := d.executeWithRetry(ctx, node, nodeInput)

				if h != nil {
					_ = h.Fire(ctx, &hooks.HookContext{
						Point:    hooks.HookAfterDAGNode,
						AgentID:  nid,
						Error:    execErr,
						Metadata: map[string]any{"node_id": nid},
					})
				}

				nr.Retries = retries
				nr.Timestamp = time.Now()
				nr.Duration = time.Since(nodeStart)

				if execErr != nil {
					nr.Error = execErr
					stateMu.Lock()
					result.Failed++
					stateMu.Unlock()
					d.metrics.record(nid, nr.Duration, false, retries)
				} else {
					nr.Output = resp.Content
					stateMu.Lock()
					result.Succeeded++
					stateMu.Unlock()
					d.metrics.record(nid, nr.Duration, true, retries)
				}
			}(nodeID)
		}
		wg.Wait()
	}

	for id := range nodes {
		if _, done := result.NodeResults[id]; !done {
			result.NodeResults[id] = &DAGNodeResult{
				NodeID:    id,
				Skipped:   true,
				Timestamp: time.Now(),
			}
			result.Skipped++
		}
	}

	result.Duration = time.Since(start)
	// perf-v4 Task 3：TotalExecutions / TotalDuration 改为无锁原子累加
	d.metrics.TotalExecutions.Add(1)
	d.metrics.TotalDuration.Add(int64(result.Duration))

	return result, nil
}

// executeWithRetry 带重试的节点执行
func (d *DAGWorkflow) executeWithRetry(ctx context.Context, node *DAGNode, input string) (*core.Response, int, error) {
	policy := node.RetryPolicy
	if policy == nil || policy.MaxRetries <= 0 {
		resp, err := node.Agent.Run(ctx, core.UserMessage(input))
		return resp, 0, err
	}

	var lastErr error
	var resp *core.Response
	delay := policy.Delay

	for attempt := 0; attempt <= policy.MaxRetries; attempt++ {
		if attempt > 0 {
			if policy.OnRetry != nil {
				policy.OnRetry(attempt, lastErr)
			}
			select {
			case <-ctx.Done():
				return nil, attempt - 1, ctx.Err()
			case <-time.After(delay):
			}
			delay = time.Duration(float64(delay) * policy.Backoff)
		}

		resp, lastErr = node.Agent.Run(ctx, core.UserMessage(input))
		if lastErr == nil {
			return resp, attempt, nil
		}
	}

	return resp, policy.MaxRetries, lastErr
}

// Metrics 返回执行指标
