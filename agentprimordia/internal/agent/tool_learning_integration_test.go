package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/tool_learning"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/tools"
)

// ===== mock =====

// processCorrectionLearner 可配置流程修正的 ToolLearner mock。
type processCorrectionLearner struct {
	avoid           bool
	alternativeArgs string
	confidence      float64
}

func (l *processCorrectionLearner) RecordSuccess(_ context.Context, _, _, _ string) error { return nil }
func (l *processCorrectionLearner) RecordFailure(_ context.Context, _, _, _ string) error { return nil }
func (l *processCorrectionLearner) GetBestPractices(_ context.Context, _ string) ([]tool_learning.BestPractice, error) {
	return nil, nil
}
func (l *processCorrectionLearner) SuggestImprovement(_ context.Context, _, _ string) (*tool_learning.Suggestion, error) {
	return &tool_learning.Suggestion{Confidence: 0}, nil
}
func (l *processCorrectionLearner) SuggestProcessCorrection(_ context.Context, _, _ string) (*tool_learning.ProcessCorrection, error) {
	return &tool_learning.ProcessCorrection{
		ToolName:        "tool",
		Avoid:           l.avoid,
		Reason:          "参数组合已失败 3 次，应规避",
		Confidence:      l.confidence,
		AlternativeArgs: l.alternativeArgs,
		ErrorPattern:    "permission denied",
		Frequency:       3,
	}, nil
}

// toolLearningMock 实现 ToolLearningCapable + ToolkitCapable + Agent。
type toolLearningMock struct {
	learner tool_learning.ToolLearner
	toolkit *tools.Registry
}

func (m *toolLearningMock) GetToolLearner() tool_learning.ToolLearner { return m.learner }
func (m *toolLearningMock) GetToolkit() *tools.Registry               { return m.toolkit }
func (m *toolLearningMock) Run(_ context.Context, _ Message) (*Response, error) {
	return nil, errors.New("not used")
}
func (m *toolLearningMock) StreamRun(_ context.Context, _ Message) (<-chan StreamEvent, error) {
	return nil, errors.New("not used")
}
func (m *toolLearningMock) Stop()             {}
func (m *toolLearningMock) Stats() AgentStats { return AgentStats{} }
func (m *toolLearningMock) Name() string      { return "tool-learning-mock" }

// countingTool 统计执行次数的 Tool（验证规避后不执行）。
type countingTool struct {
	calls atomic.Int64
}

func (c *countingTool) Name() string                { return "counting_tool" }
func (c *countingTool) Description() string         { return "counts calls" }
func (c *countingTool) Parameters() json.RawMessage { return nil }
func (c *countingTool) Execute(_ context.Context, _ json.RawMessage) (*tools.Result, error) {
	c.calls.Add(1)
	return &tools.Result{Content: "ok"}, nil
}

// newToolRegistryWith 创建含单工具的注册表。
func newToolRegistryWith(tool tools.Tool) *tools.Registry {
	reg := tools.NewRegistry()
	if err := reg.Register(tool); err != nil {
		panic(err)
	}
	return reg
}

func containsSubstr(s, sub string) bool { return strings.Contains(s, sub) }

// ===== TestToolLearning_ProcessCorrection_Skip =====
// 命中高频失败模式且无替代参数 → 跳过执行（失败模式被自动规避）。

func TestToolLearning_ProcessCorrection_Skip(t *testing.T) {
	learner := &processCorrectionLearner{avoid: true, confidence: 0.95}
	tool := &countingTool{}

	a := newReActAgent(ReActConfig{Name: "tl-skip", MaxTurns: 5, ToolLearningConfidenceThreshold: 0.7})
	a.self = &toolLearningMock{learner: learner, toolkit: newToolRegistryWith(tool)}

	tc := &ToolCall{ID: "c1", Name: "counting_tool", Args: `{"x":"1"}`}
	result, err, _ := a.executeSingleTool(context.Background(), tc, 0, loopConfig{}, nil, &NoopSpan{})
	if err != nil {
		t.Fatalf("executeSingleTool 不应报错: %v", err)
	}
	if tool.calls.Load() != 0 {
		t.Errorf("工具应被跳过（执行次数 = %d）", tool.calls.Load())
	}
	if result == nil || !containsSubstr(result.Content, "已规避") {
		t.Errorf("结果应含规避提示, got %q", result.Content)
	}
	if a.Stats().ProcessCorrections != 1 {
		t.Errorf("ProcessCorrections = %d, want 1", a.Stats().ProcessCorrections)
	}
}

// ===== TestToolLearning_ProcessCorrection_Alternative =====
// 命中失败模式且有替代参数 → 用替代参数执行（流程修正）。

func TestToolLearning_ProcessCorrection_Alternative(t *testing.T) {
	learner := &processCorrectionLearner{avoid: true, confidence: 0.95, alternativeArgs: `{"x":"safe"}`}
	tool := &countingTool{}

	a := newReActAgent(ReActConfig{Name: "tl-alt", MaxTurns: 5, ToolLearningConfidenceThreshold: 0.7})
	a.self = &toolLearningMock{learner: learner, toolkit: newToolRegistryWith(tool)}

	tc := &ToolCall{ID: "c2", Name: "counting_tool", Args: `{"x":"dangerous"}`}
	result, err, _ := a.executeSingleTool(context.Background(), tc, 0, loopConfig{}, nil, &NoopSpan{})
	if err != nil {
		t.Fatalf("executeSingleTool 不应报错: %v", err)
	}
	// 替代参数路径应执行工具（结果正常）
	if tool.calls.Load() != 1 {
		t.Errorf("替代参数路径应执行工具（执行次数 = %d）", tool.calls.Load())
	}
	if result == nil || !containsSubstr(result.Content, "ok") {
		t.Errorf("结果应为工具输出, got %q", result.Content)
	}
	if a.Stats().ProcessCorrections != 1 {
		t.Errorf("ProcessCorrections = %d, want 1", a.Stats().ProcessCorrections)
	}
}

// ===== TestToolLearning_ProcessCorrection_LowConfidence =====
// 置信度低于阈值 → 不规避，正常执行。

func TestToolLearning_ProcessCorrection_LowConfidence(t *testing.T) {
	learner := &processCorrectionLearner{avoid: true, confidence: 0.3}
	tool := &countingTool{}

	a := newReActAgent(ReActConfig{Name: "tl-low", MaxTurns: 5, ToolLearningConfidenceThreshold: 0.7})
	a.self = &toolLearningMock{learner: learner, toolkit: newToolRegistryWith(tool)}

	tc := &ToolCall{ID: "c3", Name: "counting_tool", Args: `{"x":"1"}`}
	_, err, _ := a.executeSingleTool(context.Background(), tc, 0, loopConfig{}, nil, &NoopSpan{})
	if err != nil {
		t.Fatalf("executeSingleTool 不应报错: %v", err)
	}
	if tool.calls.Load() != 1 {
		t.Errorf("低置信度不应规避（执行次数 = %d）", tool.calls.Load())
	}
	if a.Stats().ProcessCorrections != 0 {
		t.Errorf("ProcessCorrections = %d, want 0", a.Stats().ProcessCorrections)
	}
}
