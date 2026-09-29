package reflection

import (
	"context"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/internal/llm"
)

// TestReflectorInterface 验证 Reflector 接口定义
func TestReflectorInterface(t *testing.T) {
	var _ Reflector = (*LLMReflector)(nil)
}

// TestReflectionCreation 测试反思结果创建
func TestReflectionCreation(t *testing.T) {
	reflection := Reflection{
		Strengths:   []string{"准确理解问题"},
		Weaknesses:  []string{"缺少示例"},
		Suggestions: []string{"添加代码示例"},
		Confidence:  0.85,
	}

	if len(reflection.Strengths) != 1 {
		t.Errorf("Expected 1 strength, got %d", len(reflection.Strengths))
	}
	if reflection.Confidence < 0 || reflection.Confidence > 1 {
		t.Errorf("Confidence should be between 0 and 1, got %f", reflection.Confidence)
	}
}

// TestCritiqueCreation 测试批评结果创建
func TestCritiqueCreation(t *testing.T) {
	critique := Critique{
		Issues: []Issue{
			{Description: "事实错误", Location: "第2段", Severity: SeverityHigh},
		},
		Severity: SeverityMedium,
		Corrections: []Correction{
			{Original: "错误内容", Corrected: "正确内容", Reason: "事实更正"},
		},
	}

	if len(critique.Issues) != 1 {
		t.Errorf("Expected 1 issue, got %d", len(critique.Issues))
	}
	if critique.Severity != SeverityMedium {
		t.Errorf("Expected severity medium, got %v", critique.Severity)
	}
}

// TestSeverityValues 测试严重程度值
func TestSeverityValues(t *testing.T) {
	severities := []Severity{SeverityLow, SeverityMedium, SeverityHigh, SeverityCritical}

	for _, severity := range severities {
		if severity == "" {
			t.Errorf("Severity should not be empty")
		}
	}
}

// TestLLMReflectorReflect 测试 LLMReflector 的反思功能
func TestLLMReflectorReflect(t *testing.T) {
	mockProvider := &mockLLMProvider{
		completeFunc: func(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
			return &llm.CompletionResponse{
				Content: `{
					"strengths": ["准确理解问题"],
					"weaknesses": ["缺少示例"],
					"suggestions": ["添加代码示例"],
					"confidence": 0.85
				}`,
			}, nil
		},
	}

	reflector := NewLLMReflector(mockProvider)

	reflection, err := reflector.Reflect(context.Background(), "测试输入", "测试输出")
	if err != nil {
		t.Fatalf("Reflect failed: %v", err)
	}

	if len(reflection.Strengths) == 0 {
		t.Error("Expected at least one strength")
	}
}

// TestLLMReflectorCritique 测试 LLMReflector 的批评功能
func TestLLMReflectorCritique(t *testing.T) {
	mockProvider := &mockLLMProvider{
		completeFunc: func(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
			return &llm.CompletionResponse{
				Content: `{
					"issues": [{"description": "事实错误", "severity": "high"}],
					"severity": "medium",
					"corrections": [{"original": "错误内容", "corrected": "正确内容", "reason": "事实更正"}]
				}`,
			}, nil
		},
	}

	reflector := NewLLMReflector(mockProvider)

	critique, err := reflector.Critique(context.Background(), "测试输出")
	if err != nil {
		t.Fatalf("Critique failed: %v", err)
	}

	if len(critique.Issues) == 0 {
		t.Error("Expected at least one issue")
	}
}

// TestLLMReflectorImprove 测试 LLMReflector 的改进功能
func TestLLMReflectorImprove(t *testing.T) {
	callCount := 0
	mockProvider := &mockLLMProvider{
		completeFunc: func(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
			callCount++
			return &llm.CompletionResponse{
				Content: "改进后的输出",
			}, nil
		},
	}

	reflector := NewLLMReflector(mockProvider)

	feedback := &Critique{
		Corrections: []Correction{
			{Original: "错误内容", Corrected: "正确内容", Reason: "事实更正"},
		},
	}

	improved, err := reflector.Improve(context.Background(), "原始输出", feedback)
	if err != nil {
		t.Fatalf("Improve failed: %v", err)
	}

	if improved != "改进后的输出" {
		t.Errorf("Expected improved output '改进后的输出', got '%s'", improved)
	}
	if callCount != 1 {
		t.Errorf("Expected 1 LLM call, got %d", callCount)
	}
}

// TestLLMReflectorImproveNoFeedback 测试无反馈时的改进功能
func TestLLMReflectorImproveNoFeedback(t *testing.T) {
	mockProvider := &mockLLMProvider{}

	reflector := NewLLMReflector(mockProvider)

	output, err := reflector.Improve(context.Background(), "原始输出", nil)
	if err != nil {
		t.Fatalf("Improve with nil feedback failed: %v", err)
	}

	if output != "原始输出" {
		t.Errorf("Expected original output '%s', got '%s'", "原始输出", output)
	}
}

// mockLLMProvider 模拟 LLM Provider
type mockLLMProvider struct {
	completeFunc func(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error)
}

func (m *mockLLMProvider) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if m.completeFunc != nil {
		return m.completeFunc(ctx, req)
	}
	return &llm.CompletionResponse{Content: ""}, nil
}

func (m *mockLLMProvider) Stream(ctx context.Context, req *llm.CompletionRequest) (<-chan llm.Chunk, error) {
	return nil, nil
}

func (m *mockLLMProvider) CallTools(ctx context.Context, req *llm.ToolCallRequest) (*llm.ToolCallResponse, error) {
	return &llm.ToolCallResponse{}, nil
}

func (m *mockLLMProvider) Info() llm.ModelInfo {
	return llm.ModelInfo{}
}
