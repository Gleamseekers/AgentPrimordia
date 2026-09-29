package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent"
)

// StepExecutor 执行单个 step。
type StepExecutor interface {
	Execute(ctx context.Context, step *AgentStep, input map[string]any) *StepResult
}

type defaultStepExecutor struct {
	eventCh chan<- *OrchestrationEvent
}

// NewDefaultStepExecutor 创建默认 step 执行器。
// eventCh 可为 nil，表示不发送事件。
func NewDefaultStepExecutor(eventCh chan<- *OrchestrationEvent) StepExecutor {
	return &defaultStepExecutor{eventCh: eventCh}
}

func (e *defaultStepExecutor) Execute(ctx context.Context, step *AgentStep, input map[string]any) *StepResult {
	startTime := time.Now()
	result := &StepResult{
		StepID:    step.ID,
		StepName:  step.Name,
		Status:    StepRunning,
		StartTime: startTime,
		Output:    make(map[string]any),
	}

	if step.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, step.Timeout)
		defer cancel()
	}

	e.emitEvent("step_started", step.ID, map[string]any{"name": step.Name})

	if !stepConditionSatisfied(step, input) {
		result.Status = StepSkipped
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(startTime)
		return result
	}

	prompt := step.Prompt
	if prompt == "" {
		prompt = buildPromptFromInputs(input, step.InputFrom)
	}

	resp, err := step.Agent.Run(ctx, agent.UserMessage(prompt))

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(startTime)
	result.Response = resp

	if err != nil {
		result.Status = StepFailed
		result.Error = err
		e.emitEvent("step_failed", step.ID, map[string]any{"error": err.Error()})
		return result
	}

	result.Status = StepCompleted
	if resp.Content != "" {
		result.Output["content"] = resp.Content
	}
	if step.OutputKey != "" {
		result.Output[step.OutputKey] = resp.Content
	}
	if resp.Metrics.TotalTurns > 0 {
		result.Output["turns"] = resp.Metrics.TotalTurns
	}

	e.emitEvent("step_completed", step.ID, map[string]any{
		"duration": result.Duration,
		"turns":    resp.Metrics.TotalTurns,
	})
	return result
}

func (e *defaultStepExecutor) emitEvent(typ, stepID string, data any) {
	if e.eventCh == nil {
		return
	}
	select {
	case e.eventCh <- &OrchestrationEvent{Type: typ, Timestamp: time.Now(), StepID: stepID, Data: data}:
	default:
	}
}

// stepConditionSatisfied 检查 step 执行条件。
func stepConditionSatisfied(step *AgentStep, input map[string]any) bool {
	if step.Condition.Type == "" || step.Condition.Type == "always" {
		return true
	}

	value, exists := input[step.Condition.Field]
	if !exists {
		return false
	}

	switch step.Condition.Operator {
	case "==":
		return fmt.Sprintf("%v", value) == fmt.Sprintf("%v", step.Condition.Value)
	case "!=":
		return fmt.Sprintf("%v", value) != fmt.Sprintf("%v", step.Condition.Value)
	case "contains":
		strValue := fmt.Sprintf("%v", value)
		return strings.Contains(strings.ToLower(strValue), strings.ToLower(fmt.Sprintf("%v", step.Condition.Value)))
	case "empty":
		return value == nil || fmt.Sprintf("%v", value) == ""
	case "not_empty":
		return value != nil && fmt.Sprintf("%v", value) != ""
	default:
		return true
	}
}

// buildPromptFromInputs 从输入构建提示词。
func buildPromptFromInputs(input map[string]any, inputKeys []string) string {
	if len(inputKeys) == 0 {
		data, _ := json.MarshalIndent(input, "", "  ")
		return fmt.Sprintf("请基于以下上下文信息进行处理:\n\n%s", string(data))
	}

	var parts []string
	for _, key := range inputKeys {
		if val, ok := input[key]; ok {
			parts = append(parts, fmt.Sprintf("[%s]:\n%v", key, val))
		}
	}

	return fmt.Sprintf("请基于以下信息进行处理:\n\n%s", strings.Join(parts, "\n\n"))
}
