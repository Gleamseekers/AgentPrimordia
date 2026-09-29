// token_budget.go — Token 预算裁剪策略（v5.1 引擎热路径）
//
// 对齐 TS sdk/typescript/src/agent/request-id.ts 的 TokenBudgetStrategy：
// 按 token 预算（charsPerToken 换算为字符上限）裁剪历史消息，
// 始终保留系统消息，从最近消息向前尽量多保留连续后缀。
//
// 相比 DefaultStrategy 的纯计数窗口，token 预算直接约束 LLM 输入规模：
// 计数窗口在「少条数大消息」场景下 token 失控（10 条 × 2000 字符 ≈ 5000 token），
// 本策略将上下文硬约束在预算内 → 同任务集 token 成本降幅可量化。
//
// Go 侧增强（相对 TS）：字符估算同时计入 ToolCalls（name/arguments/id）与
// 多模态文本部分——ReAct 历史以工具调用为主，仅按 Content 估算会严重低估。
package context

import (
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/core"
)

// defaultCharsPerToken 默认字符/token 换算比（与 TS 一致：1 token ≈ 4 字符）
const defaultCharsPerToken = 4

// TokenBudgetStrategy token 预算裁剪策略
type TokenBudgetStrategy struct {
	CharsPerToken int
}

// NewTokenBudgetStrategy 创建 token 预算策略；charsPerToken <= 0 时回退默认值 4
func NewTokenBudgetStrategy(charsPerToken int) *TokenBudgetStrategy {
	if charsPerToken <= 0 {
		charsPerToken = defaultCharsPerToken
	}
	return &TokenBudgetStrategy{CharsPerToken: charsPerToken}
}

// Trim 按 token 预算裁剪历史。
// maxMessages 参数在本策略中解释为 maxTokens（token 上限），与 Strategy 接口兼容；
// 语义与 TS TokenBudgetStrategy.trim(messages, maxTokens) 一致。
func (s *TokenBudgetStrategy) Trim(messages []core.Message, maxTokens int) []core.Message {
	if len(messages) == 0 {
		return messages
	}
	if maxTokens <= 0 {
		// 无有效预算时不裁剪（与计数策略的「<=0 用默认」不同：
		// token 预算无默认上限，交由调用方显式配置）
		return messages
	}
	maxChars := maxTokens * s.CharsPerToken

	// 系统消息始终保留
	var system []core.Message
	var rest []core.Message
	systemChars := 0
	for i := range messages {
		if messages[i].Role == core.RoleSystem {
			system = append(system, messages[i])
			systemChars += messageChars(&messages[i])
		} else {
			rest = append(rest, messages[i])
		}
	}

	budget := maxChars - systemChars
	if budget <= 0 {
		return system
	}

	// 从最近消息向前尽量多保留（连续后缀）
	kept := make([]core.Message, 0, len(rest))
	used := 0
	for i := len(rest) - 1; i >= 0; i-- {
		c := messageChars(&rest[i])
		if used+c > budget {
			continue
		}
		used += c
		kept = append(kept, rest[i])
	}
	// 反转回时间序
	result := make([]core.Message, 0, len(system)+len(kept))
	result = append(result, system...)
	for i := len(kept) - 1; i >= 0; i-- {
		result = append(result, kept[i])
	}
	return result
}

// messageChars 估算消息字符数：Content + 多模态文本 + ToolCalls（Go 侧增强）
func messageChars(m *core.Message) int {
	n := len(m.Content)
	for _, p := range m.ContentParts {
		n += len(p.Text)
	}
	for _, tc := range m.ToolCalls {
		n += len(tc.ID) + len(tc.Name) + len(tc.Args)
	}
	return n
}
