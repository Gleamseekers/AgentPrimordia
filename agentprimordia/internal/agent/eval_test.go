
package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/eval"
)

func TestExactMatchEvaluator_Match(t *testing.T) {
	e := &ExactMatchEvaluator{}
	result, err := e.Evaluate(context.Background(), EvalInput{
		AgentOutput: &eval.Response{Content: "hello world"},
		Expected:    "hello world",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Error("expected pass for exact match")
	}
	if result.Score != 1.0 {
		t.Errorf("Score = %f, want 1.0", result.Score)
	}
}

func TestExactMatchEvaluator_NoMatch(t *testing.T) {
	e := &ExactMatchEvaluator{}
	result, err := e.Evaluate(context.Background(), EvalInput{
		AgentOutput: &eval.Response{Content: "hello"},
		Expected:    "world",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected fail for no match")
	}
	if result.Score != 0.0 {
		t.Errorf("Score = %f, want 0.0", result.Score)
	}
}

func TestExactMatchEvaluator_CaseInsensitive(t *testing.T) {
	e := &ExactMatchEvaluator{CaseInsensitive: true}
	result, _ := e.Evaluate(context.Background(), EvalInput{
		AgentOutput: &eval.Response{Content: "Hello World"},
		Expected:    "hello world",
	})
	if !result.Passed {
		t.Error("expected pass for case-insensitive match")
	}
}

func TestContainsEvaluator_Found(t *testing.T) {
	e := &ContainsEvaluator{Keywords: []string{"Go", "Agent"}}
	result, err := e.Evaluate(context.Background(), EvalInput{
		AgentOutput: &eval.Response{Content: "Go is great for building Agent systems"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Error("expected pass when all keywords found")
	}
	if result.Score != 1.0 {
		t.Errorf("Score = %f, want 1.0", result.Score)
	}
}

func TestContainsEvaluator_NotFound(t *testing.T) {
	e := &ContainsEvaluator{Keywords: []string{"Python", "Rust"}}
	result, _ := e.Evaluate(context.Background(), EvalInput{
		AgentOutput: &eval.Response{Content: "Go is great for building Agent systems"},
	})
	if result.Passed {
		t.Error("expected fail when keywords not found")
	}
}

func TestContainsEvaluator_PartialMatch(t *testing.T) {
	e := &ContainsEvaluator{Keywords: []string{"Go", "Python"}}
	result, _ := e.Evaluate(context.Background(), EvalInput{
		AgentOutput: &eval.Response{Content: "Go is great for building Agent systems"},
	})
	if result.Passed {
		t.Error("expected fail for partial match")
	}
	if result.Score != 0.5 {
		t.Errorf("Score = %f, want 0.5", result.Score)
	}
}

func TestToolUsageEvaluator_CorrectTool(t *testing.T) {
	e := &ToolUsageEvaluator{ExpectedTools: []string{"calculator", "datetime"}}
	result, err := e.Evaluate(context.Background(), EvalInput{
		AgentOutput: &eval.Response{
			Content: "result is 40",
			ToolCalls: []eval.ToolCall{
				{Name: "calculator", Args: `{"a":15,"b":25}`},
				{Name: "datetime", Args: `{"action":"now"}`},
			},
		},
		Expected: "",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Error("expected pass when all expected tools used")
	}
}

func TestToolUsageEvaluator_WrongTool(t *testing.T) {
	e := &ToolUsageEvaluator{ExpectedTools: []string{"calculator"}}
	result, _ := e.Evaluate(context.Background(), EvalInput{
		AgentOutput: &eval.Response{
			Content:   "done",
			ToolCalls: []eval.ToolCall{{Name: "web_search", Args: `{}`}},
		},
	})
	if result.Passed {
		t.Error("expected fail when wrong tool used")
	}
}

func TestToolUsageEvaluator_NoToolCalls(t *testing.T) {
	e := &ToolUsageEvaluator{ExpectedTools: []string{"calculator"}}
	result, _ := e.Evaluate(context.Background(), EvalInput{
		AgentOutput: &eval.Response{Content: "I don't need tools"},
	})
	if result.Passed {
		t.Error("expected fail when no tool calls but tools expected")
	}
}

func TestCompositeEvaluator_AllPass(t *testing.T) {
	e := &CompositeEvaluator{
		Evaluators: []Evaluator{
			&ExactMatchEvaluator{},
			&ContainsEvaluator{Keywords: []string{"hello"}},
		},
		Mode: CompositeAll,
	}
	result, _ := e.Evaluate(context.Background(), EvalInput{
		AgentOutput: &eval.Response{Content: "hello world"},
		Expected:    "hello world",
	})
	if !result.Passed {
		t.Error("expected pass when all evaluators pass")
	}
}

func TestCompositeEvaluator_AnyPass(t *testing.T) {
	e := &CompositeEvaluator{
		Evaluators: []Evaluator{
			&ExactMatchEvaluator{},
			&ContainsEvaluator{Keywords: []string{"hello"}},
		},
		Mode: CompositeAny,
	}
	result, _ := e.Evaluate(context.Background(), EvalInput{
		AgentOutput: &eval.Response{Content: "hello there"},
		Expected:    "hello world",
	})
	if !result.Passed {
		t.Error("expected pass when any evaluator passes (contains)")
	}
}

func TestCompositeEvaluator_AllFail(t *testing.T) {
	e := &CompositeEvaluator{
		Evaluators: []Evaluator{
			&ExactMatchEvaluator{},
			&ContainsEvaluator{Keywords: []string{"xyz"}},
		},
		Mode: CompositeAll,
	}
	result, _ := e.Evaluate(context.Background(), EvalInput{
		AgentOutput: &eval.Response{Content: "hello world"},
		Expected:    "different",
	})
	if result.Passed {
		t.Error("expected fail when all evaluators fail")
	}
}

func TestEvalRunner_RunSuite(t *testing.T) {
	runner := &EvalRunner{
		evaluators: []Evaluator{&ExactMatchEvaluator{}},
	}

	cases := []EvalCase{
		{Task: "greet", Expected: "hello", Input: "say hello"},
		{Task: "calc", Expected: "42", Input: "calculate 6*7"},
	}

	mockAgent := &mockEvalAgent{
		responses: map[string]string{
			"say hello":     "hello",
			"calculate 6*7": "42",
		},
	}

	suiteResult, err := runner.RunSuite(context.Background(), mockAgent, cases)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if suiteResult.Total != 2 {
		t.Errorf("Total = %d, want 2", suiteResult.Total)
	}
	if suiteResult.Passed != 2 {
		t.Errorf("Passed = %d, want 2", suiteResult.Passed)
	}
	if suiteResult.PassRate != 1.0 {
		t.Errorf("PassRate = %f, want 1.0", suiteResult.PassRate)
	}
}

func TestEvalRunner_RunSuite_PartialPass(t *testing.T) {
	runner := &EvalRunner{
		evaluators: []Evaluator{&ExactMatchEvaluator{}},
	}

	cases := []EvalCase{
		{Task: "greet", Expected: "hello", Input: "say hello"},
		{Task: "calc", Expected: "42", Input: "calculate 6*7"},
	}

	mockAgent := &mockEvalAgent{
		responses: map[string]string{
			"say hello":     "hello",
			"calculate 6*7": "43",
		},
	}

	suiteResult, _ := runner.RunSuite(context.Background(), mockAgent, cases)
	if suiteResult.Passed != 1 {
		t.Errorf("Passed = %d, want 1", suiteResult.Passed)
	}
	if suiteResult.Failed != 1 {
		t.Errorf("Failed = %d, want 1", suiteResult.Failed)
	}
}

type mockEvalAgent struct {
	responses map[string]string
}

func (m *mockEvalAgent) Run(ctx context.Context, input Message) (*Response, error) {
	content := m.responses[input.Content]
	return &Response{Content: content}, nil
}

func (m *mockEvalAgent) StreamRun(ctx context.Context, input Message) (<-chan StreamEvent, error) {
	ch := make(chan StreamEvent, 1)
	ch <- StreamEvent{Type: StreamEventComplete, Content: m.responses[input.Content]}
	close(ch)
	return ch, nil
}

func (m *mockEvalAgent) Stop() {}

func (m *mockEvalAgent) Stats() AgentStats {
	return AgentStats{Status: StatusIdle}
}

func (m *mockEvalAgent) Name() string {
	return "mock-eval-agent"
}

func TestLLMEvaluator_Success(t *testing.T) {
	mockLLM := &evalMockLLM{response: `{"score":0.9,"passed":true,"reason":"good answer"}`}
	e := &LLMEvaluator{Provider: mockLLM}
	result, err := e.Evaluate(context.Background(), EvalInput{
		Task:        "What is 2+2?",
		AgentOutput: &eval.Response{Content: "4"},
		Expected:    "4",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Error("expected pass")
	}
	if result.Score < 0.8 {
		t.Errorf("Score = %f, want >= 0.8", result.Score)
	}
}

func TestLLMEvaluator_Error(t *testing.T) {
	mockLLM := &evalMockLLM{err: ErrAgentStopped}
	e := &LLMEvaluator{Provider: mockLLM}
	_, err := e.Evaluate(context.Background(), EvalInput{
		Task:        "test",
		AgentOutput: &eval.Response{Content: "output"},
	})
	if err == nil {
		t.Error("expected error from LLM evaluator")
	}
}

type evalMockLLM struct {
	response string
	err      error
}

func (m *evalMockLLM) Complete(ctx context.Context, req interface{}) (interface{}, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

func (m *evalMockLLM) Evaluate(ctx context.Context, prompt string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.response, nil
}

func TestContainsEvaluator_EmptyKeywords(t *testing.T) {
	e := &ContainsEvaluator{Keywords: []string{}}
	result, _ := e.Evaluate(context.Background(), EvalInput{
		AgentOutput: &eval.Response{Content: "anything"},
	})
	if !result.Passed {
		t.Error("expected pass when no keywords required")
	}
}

func TestExactMatchEvaluator_NormalizeWhitespace(t *testing.T) {
	e := &ExactMatchEvaluator{NormalizeWhitespace: true}
	result, _ := e.Evaluate(context.Background(), EvalInput{
		AgentOutput: &eval.Response{Content: "hello   world"},
		Expected:    "hello world",
	})
	if !result.Passed {
		t.Error("expected pass with whitespace normalization")
	}
}

func TestEvalRunner_EmptySuite(t *testing.T) {
	runner := &EvalRunner{
		evaluators: []Evaluator{&ExactMatchEvaluator{}},
	}
	result, err := runner.RunSuite(context.Background(), &mockEvalAgent{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Total != 0 {
		t.Errorf("Total = %d, want 0", result.Total)
	}
}

func TestCompositeEvaluator_Weighted(t *testing.T) {
	e := &CompositeEvaluator{
		WeightedEvaluators: []WeightedEvaluator{
			{Evaluator: &ExactMatchEvaluator{}, Weight: 0.6},
			{Evaluator: &ContainsEvaluator{Keywords: []string{"hello"}}, Weight: 0.4},
		},
		Mode: CompositeWeighted,
	}
	result, _ := e.Evaluate(context.Background(), EvalInput{
		AgentOutput: &eval.Response{Content: "hello there"},
		Expected:    "hello world",
	})
	if result.Passed {
		t.Error("expected fail: exact match fails (0.6*0 + 0.4*1 = 0.4 < 0.5)")
	}
	if result.Score < 0.39 || result.Score > 0.41 {
		t.Errorf("Score = %f, want ~0.4", result.Score)
	}
}


func TestToolUsageEvaluator_PartialMatch(t *testing.T) {
	e := &ToolUsageEvaluator{ExpectedTools: []string{"calculator", "datetime"}}
	result, _ := e.Evaluate(context.Background(), EvalInput{
		AgentOutput: &eval.Response{
			Content:   "done",
			ToolCalls: []eval.ToolCall{{Name: "calculator", Args: `{}`}},
		},
	})
	if result.Passed {
		t.Error("expected fail for partial tool match")
	}
	if !strings.Contains(result.Criteria[len(result.Criteria)-1].Reason, "1/2") {
		t.Errorf("Reason should mention partial match, got: %s", result.Criteria[len(result.Criteria)-1].Reason)
	}
}
