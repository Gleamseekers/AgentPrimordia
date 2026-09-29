package agent

import (
	"github.com/Gleamseekers/AgentPrimordia/internal/agent/eval"
	"context"
)

// Evaluator 评估器接口
type Evaluator = eval.Evaluator

// EvalInput 评估输入
type EvalInput = eval.EvalInput

// EvalResult 评估结果
type EvalResult = eval.EvalResult

// CriterionResult 标准评估结果
type CriterionResult = eval.CriterionResult

// EvalCase 评估用例
type EvalCase = eval.EvalCase

// EvalSuiteResult 评估套件结果
type EvalSuiteResult = eval.EvalSuiteResult

// CaseResult 用例结果
type CaseResult = eval.CaseResult

// ExactMatchEvaluator 精确匹配评估器
type ExactMatchEvaluator = eval.ExactMatchEvaluator

// ContainsEvaluator 包含关键词评估器
type ContainsEvaluator = eval.ContainsEvaluator

// ToolUsageEvaluator tool使用评估器
type ToolUsageEvaluator = eval.ToolUsageEvaluator

// CompositeMode 组合模式
type CompositeMode = eval.CompositeMode

// WeightedEvaluator 加权评估器
type WeightedEvaluator = eval.WeightedEvaluator

// CompositeEvaluator 组合评估器
type CompositeEvaluator = eval.CompositeEvaluator

// LLMEvaluator LLM 评估器
type LLMEvaluator = eval.LLMEvaluator

// 组合模式常量
const (
	CompositeAll      = eval.CompositeAll
	CompositeAny      = eval.CompositeAny
	CompositeWeighted = eval.CompositeWeighted
)

// evalAgentAdapter 将 agent.Agent 适配为 eval.Agent 接口
type evalAgentAdapter struct {
	a Agent
}

func (w *evalAgentAdapter) Run(ctx context.Context, input string) (*eval.Response, error) {
	msg := UserMessage(input)
	resp, err := w.a.Run(ctx, msg)
	if err != nil {
		return nil, err
	}

	// 转换 ToolCalls
	var toolCalls []eval.ToolCall
	for _, tc := range resp.ToolCalls {
		toolCalls = append(toolCalls, eval.ToolCall{
			Name: tc.Name,
			Args: tc.Args,
		})
	}

	return &eval.Response{
		Content:   resp.Content,
		ToolCalls: toolCalls,
	}, nil
}

// EvalRunner 评估运行器
type EvalRunner struct {
	evaluators []Evaluator
}

// NewEvalRunner 创建评估运行器
func NewEvalRunner(evaluators ...Evaluator) *EvalRunner {
	return &EvalRunner{
		evaluators: evaluators,
	}
}

// RunSuite 运行评估套件
func (r *EvalRunner) RunSuite(ctx context.Context, agent Agent, cases []EvalCase) (*EvalSuiteResult, error) {
	// 使用适配器将 agent.Agent 转换为 eval.Agent
	adapter := &evalAgentAdapter{a: agent}

	// 创建 eval.EvalRunner 并委托执行
	runner := eval.NewEvalRunner(r.evaluators...)
	return runner.RunSuite(ctx, adapter, cases)
}
