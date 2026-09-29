package reflection

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
)

// Reflector 定义自我反思和纠错接口
type Reflector interface {
	// Reflect 对执行结果进行反思
	Reflect(ctx context.Context, input, output string) (*Reflection, error)
	// Critique 对输出进行批评和纠错
	Critique(ctx context.Context, output string) (*Critique, error)
	// Improve 基于反思结果改进输出
	Improve(ctx context.Context, output string, feedback *Critique) (string, error)
}

// Reflection 反思结果
type Reflection struct {
	Strengths   []string `json:"strengths"`
	Weaknesses  []string `json:"weaknesses"`
	Suggestions []string `json:"suggestions"`
	Confidence  float64  `json:"confidence"`
}

// Critique 批评结果
type Critique struct {
	Issues      []Issue      `json:"issues"`
	Severity    Severity     `json:"severity"`
	Corrections []Correction `json:"corrections"`
}

// Issue 问题描述
type Issue struct {
	Description string   `json:"description"`
	Location    string   `json:"location,omitempty"`
	Severity    Severity `json:"severity"`
}

// Severity 严重程度
type Severity string

const (
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// Correction 纠正建议
type Correction struct {
	Original  string `json:"original"`
	Corrected string `json:"corrected"`
	Reason    string `json:"reason"`
}

// LLMReflector 使用 LLM 进行自我反思
type LLMReflector struct {
	provider llm.Provider
}

// NewLLMReflector 创建 LLMReflector 实例
func NewLLMReflector(provider llm.Provider) *LLMReflector {
	return &LLMReflector{
		provider: provider,
	}
}

// Reflect 对输入输出进行反思
func (r *LLMReflector) Reflect(ctx context.Context, input, output string) (*Reflection, error) {
	prompt := fmt.Sprintf(`请对以下对话进行反思分析。

输入：%s
输出：%s

请以 JSON 格式返回反思结果，包含：
- strengths: 优点列表（字符串数组）
- weaknesses: 缺点列表（字符串数组）
- suggestions: 改进建议列表（字符串数组）
- confidence: 整体置信度（0-1 之间的浮点数）

示例格式：
{
  "strengths": ["准确理解了问题", "回答结构清晰"],
  "weaknesses": ["缺少具体示例", "部分解释不够深入"],
  "suggestions": ["添加代码示例", "补充背景知识"],
  "confidence": 0.85
}

请只返回 JSON，不要其他内容。`, input, output)

	req := &llm.CompletionRequest{
		Messages: []llm.ChatMessage{
			{Role: "user", Content: prompt},
		},
	}

	response, err := r.provider.Complete(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("LLM complete failed: %w", err)
	}

	var reflection Reflection
	if err := json.Unmarshal([]byte(response.Content), &reflection); err != nil {
		return nil, fmt.Errorf("parse reflection failed: %w", err)
	}

	return &reflection, nil
}

// Critique 对输出进行批评
func (r *LLMReflector) Critique(ctx context.Context, output string) (*Critique, error) {
	prompt := fmt.Sprintf(`请对以下输出进行严格批评和纠错。

输出：%s

请以 JSON 格式返回批评结果，包含：
- issues: 问题列表，每个问题包含 description（描述）、location（位置，可选）、severity（严重程度：low/medium/high/critical）
- severity: 整体严重程度（low/medium/high/critical）
- corrections: 纠正建议列表，每个建议包含 original（原文）、corrected（修正）、reason（原因）

示例格式：
{
  "issues": [
    {"description": "事实错误", "location": "第2段", "severity": "high"}
  ],
  "severity": "medium",
  "corrections": [
    {"original": "错误内容", "corrected": "正确内容", "reason": "事实更正"}
  ]
}

请只返回 JSON，不要其他内容。`, output)

	req := &llm.CompletionRequest{
		Messages: []llm.ChatMessage{
			{Role: "user", Content: prompt},
		},
	}

	response, err := r.provider.Complete(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("LLM complete failed: %w", err)
	}

	var critique Critique
	if err := json.Unmarshal([]byte(response.Content), &critique); err != nil {
		return nil, fmt.Errorf("parse critique failed: %w", err)
	}

	return &critique, nil
}

// Improve 基于批评改进输出
func (r *LLMReflector) Improve(ctx context.Context, output string, feedback *Critique) (string, error) {
	if feedback == nil || len(feedback.Corrections) == 0 {
		return output, nil
	}

	corrections := make([]string, 0, len(feedback.Corrections))
	for _, c := range feedback.Corrections {
		corrections = append(corrections, fmt.Sprintf("- 原文「%s」改为「%s」，原因：%s", c.Original, c.Corrected, c.Reason))
	}

	prompt := fmt.Sprintf(`请根据以下批评意见改进输出。

原始输出：%s

改进意见：
%s

请返回改进后的完整输出，保持原有格式和风格。`, output, strings.Join(corrections, "\n"))

	req := &llm.CompletionRequest{
		Messages: []llm.ChatMessage{
			{Role: "user", Content: prompt},
		},
	}

	response, err := r.provider.Complete(ctx, req)
	if err != nil {
		return "", fmt.Errorf("LLM complete failed: %w", err)
	}

	return response.Content, nil
}
