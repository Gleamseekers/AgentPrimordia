// workflow_executor.go — 节点级执行器
//   - executeNode：调度节点类型的分发 + 错误处理（retry/skip/fallback）
//   - executeTaskNode / executeConditionNode / executeFallbackNode / executeParallelNode
//   - retryNodeExecution + parallelResult
//
// 注意：本文件依赖 workflow_evaluator.go 中的 evaluate* / compareValues / toFloat64 /
// renderTemplate / buildPrompt / applyInputMapping / applyOutputMapping。
package workflow

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/core"
)

// executeNode 执行单个节点（任务/条件/回退），并按 ErrorHandling 策略处理错误
func (w *WorkflowExecution) executeNode(ctx context.Context, node *WorkflowNode, input map[string]any) (map[string]any, error) {
	startTime := time.Now()
	w.mu.Lock()
	w.currentNode = node
	w.mu.Unlock()
	w.recordPath(node.ID)

	w.emitEvent("node_started", node.ID, map[string]any{
		"type": node.Type,
		"name": node.Name,
	})

	mappedInput := w.applyInputMapping(input, node.InputMapping)

	var output map[string]any
	var err error

	switch node.Type {
	case TaskNode:
		output, err = w.executeTaskNode(ctx, node, mappedInput)
	case ConditionNode:
		output, err = w.executeConditionNode(ctx, node, mappedInput)
	case FallbackNode:
		output, err = w.executeFallbackNode(ctx, node, mappedInput)
	default:
		output, err = w.executeTaskNode(ctx, node, mappedInput)
	}

	duration := time.Since(startTime)
	status := NodeCompleted
	if err != nil {
		status = NodeFailed

		switch w.config.ErrorHandling.OnError {
		case "retry":
			output, err = w.retryNodeExecution(ctx, node, mappedInput)
		case "skip":
			status = NodeSkipped
			err = nil
			output = input
		case "fallback":
			if w.config.ErrorHandling.FallbackStep != "" {
				fallbackNode, exists := w.nodes[w.config.ErrorHandling.FallbackStep]
				if exists {
					output, err = w.executeNode(ctx, fallbackNode, input)
				}
			}
		}
	}

	// 修复（-race 实测发现）：并行节点由多个 goroutine 并发执行 executeNode，
	// nodeExecutions 递增必须加锁
	w.mu.Lock()
	w.nodeExecutions[node.ID]++
	w.mu.Unlock()

	record := &ExecutionRecord{
		NodeID:    node.ID,
		NodeName:  node.Name,
		Status:    status,
		Input:     mappedInput,
		Output:    output,
		Error:     err,
		Duration:  duration,
		Timestamp: time.Now(),
		Iteration: w.iterationCount,
	}

	w.addToHistory(record)

	finalOutput := w.applyOutputMapping(output, node.OutputMapping)

	w.mu.Lock()
	if w.result != nil && finalOutput != nil {
		if len(node.OutputMapping) > 0 {
			for k, v := range finalOutput {
				w.result.Output[k] = v
			}
		} else {
			for k, v := range finalOutput {
				w.result.Output[node.ID+"_"+k] = v
			}
		}
	}
	w.mu.Unlock()

	w.emitEvent("node_completed", node.ID, map[string]any{
		"status":   status,
		"duration": duration,
	})

	return finalOutput, err
}

// executeTaskNode 执行任务节点：优先 CustomLogic，否则调用 Agent.Run
func (w *WorkflowExecution) executeTaskNode(ctx context.Context, node *WorkflowNode, input map[string]any) (map[string]any, error) {
	if node.Config != nil && node.Config.CustomLogic != nil {
		return node.Config.CustomLogic(ctx, input)
	}

	prompt := w.buildPrompt(node, input)
	resp, err := node.Agent.Run(ctx, core.UserMessage(prompt))
	if err != nil {
		return nil, err
	}

	output := make(map[string]any)
	output["content"] = resp.Content
	output["turns"] = resp.Metrics.TotalTurns

	var parsed map[string]any
	if err := json.Unmarshal([]byte(resp.Content), &parsed); err == nil {
		for k, v := range parsed {
			output[k] = v
		}
	}

	return output, nil
}

// executeConditionNode 执行条件节点：仅评估 condition，结果写入 output
func (w *WorkflowExecution) executeConditionNode(ctx context.Context, node *WorkflowNode, input map[string]any) (map[string]any, error) {
	result := false

	if node.Condition != nil {
		result = w.evaluateNodeCondition(node.Condition, input)
	}

	output := make(map[string]any)
	output["condition_result"] = result
	output["matched"] = result

	return output, nil
}

// executeFallbackNode 执行回退节点：直接转交 executeTaskNode
func (w *WorkflowExecution) executeFallbackNode(ctx context.Context, node *WorkflowNode, input map[string]any) (map[string]any, error) {
	w.emitEvent("fallback_executed", node.ID, nil)
	return w.executeTaskNode(ctx, node, input)
}

// executeParallelNode 并行执行所有子节点，结果以 "{nodeID}_{key}" 为前缀合并
func (w *WorkflowExecution) executeParallelNode(ctx context.Context, node *WorkflowNode, input map[string]any) (map[string]any, error) {
	childTransitions := w.transitions[node.ID]
	if len(childTransitions) == 0 {
		return input, nil
	}

	var wg sync.WaitGroup
	resultsCh := make(chan *parallelResult, len(childTransitions))

	for _, trans := range childTransitions {
		childNode, exists := w.nodes[trans.To]
		if !exists {
			continue
		}

		wg.Add(1)
		go func(cn *WorkflowNode) {
			defer wg.Done()

			output, err := w.executeNode(ctx, cn, input)
			resultsCh <- &parallelResult{
				nodeID: cn.ID,
				output: output,
				error:  err,
			}
		}(childNode)
	}

	wg.Wait()
	close(resultsCh)

	mergedOutput := make(map[string]any)
	for result := range resultsCh {
		if result.error != nil {
			mergedOutput[result.nodeID+"_error"] = result.error.Error()
		}
		if result.output != nil {
			for k, v := range result.output {
				mergedOutput[result.nodeID+"_"+k] = v
			}
		}
	}

	return mergedOutput, nil
}

// parallelResult 是并行执行的子节点结果（私有类型，不导出）
type parallelResult struct {
	nodeID string
	output map[string]any
	error  error
}

// retryNodeExecution 按 MaxRetries 次数重试任务节点（线性退避）
func (w *WorkflowExecution) retryNodeExecution(ctx context.Context, node *WorkflowNode, input map[string]any) (map[string]any, error) {
	maxRetries := w.config.ErrorHandling.MaxRetries
	var lastErr error

	for i := 0; i < maxRetries; i++ {
		w.emitEvent("node_retry", node.ID, map[string]any{"attempt": i + 1})

		output, err := w.executeTaskNode(ctx, node, input)
		if err == nil {
			w.result.Metrics.RetriesAttempted.Add(int64(i + 1))
			return output, nil
		}
		lastErr = err

		select {
		case <-time.After(time.Duration(i+1) * 100 * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	w.result.Metrics.RetriesAttempted.Add(int64(maxRetries))
	return nil, lastErr
}
