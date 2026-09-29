// verifier.go — v5.2 验证循环原生化：verifier 为引擎一等公民。
//
// 可配置校验器：自校验（LLM 判断）/ 关键词校验（确定性 requires 语义，
// 对齐 eval 用例的 requires 字段）。验证失败驱动修正轮（见 verification_loop.go）。
package strategy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm"
)

// VerificationReport 验证报告
type VerificationReport struct {
	Passed  bool     `json:"passed"`
	Reasons []string `json:"reasons,omitempty"` // 失败原因（供修正轮反馈）
}

// Verifier 校验器接口（v5.2 冻结点）
type Verifier interface {
	Name() string
	Verify(ctx context.Context, eng Engine, task Task, output string) (*VerificationReport, error)
}

// KeywordVerifier 确定性关键词校验：输出须包含全部 requires 关键词
// （任一缺失即失败，语义对齐 eval 基准用例的 requires 字段）。
type KeywordVerifier struct {
	Requires []string
}

// Name 实现 Verifier
func (v *KeywordVerifier) Name() string { return "keyword" }

// Verify 实现 Verifier
func (v *KeywordVerifier) Verify(_ context.Context, _ Engine, _ Task, output string) (*VerificationReport, error) {
	rep := &VerificationReport{Passed: true}
	for _, kw := range v.Requires {
		if !strings.Contains(output, kw) {
			rep.Passed = false
			rep.Reasons = append(rep.Reasons, fmt.Sprintf("缺少关键要素 %q", kw))
		}
	}
	return rep, nil
}

// selfCheckResponse 自校验 LLM 结构化响应
type selfCheckResponse struct {
	Passed  bool     `json:"passed"`
	Reasons []string `json:"reasons"`
}

// SelfCheckVerifier 自校验器：让 LLM 对照任务目标判断输出是否合格，
// 返回结构化 pass/reasons。maxTokens 限制判断成本。
type SelfCheckVerifier struct{}

// Name 实现 Verifier
func (v *SelfCheckVerifier) Name() string { return "self_check" }

// Verify 实现 Verifier
func (v *SelfCheckVerifier) Verify(ctx context.Context, eng Engine, task Task, output string) (*VerificationReport, error) {
	prompt := fmt.Sprintf(
		"任务目标：%s\n\n候选回答：\n%s\n\n请判断候选回答是否达成了任务目标。"+
			"只返回 JSON：{\"passed\": true/false, \"reasons\": [\"失败原因\"...]}。",
		task.Goal, output)
	resp, err := eng.Complete(ctx, &llm.CompletionRequest{
		Model: "",
		Messages: []llm.ChatMessage{
			{Role: "system", Content: "你是严格的结果校验器。只输出 JSON，不输出其他内容。"},
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("strategy: 自校验 LLM 调用失败: %w", err)
	}
	var sc selfCheckResponse
	if err := json.Unmarshal([]byte(extractJSON(resp.Content)), &sc); err != nil {
		return nil, fmt.Errorf("strategy: 自校验响应非 JSON: %w（原文 %.100s）", err, resp.Content)
	}
	return &VerificationReport{Passed: sc.Passed, Reasons: sc.Reasons}, nil
}

// extractJSON 提取文本中首个 {...} JSON 片段（容忍模型输出围栏/前后缀）
func extractJSON(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return s
	}
	return s[start : end+1]
}
