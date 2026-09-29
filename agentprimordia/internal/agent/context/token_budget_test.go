// token_budget_test.go — TokenBudgetStrategy 测试（v5.1 引擎热路径）
//
// 对齐 TS sdk/typescript/src/agent/request-id.ts 的 TokenBudgetStrategy：
// 按 token 预算（charsPerToken 换算为字符）裁剪历史，始终保留系统消息，
// 从最近消息向前尽量多保留。Go 侧增强：估算同时计入 ToolCalls 与多模态文本部分。
package context

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/core"
)

func TestTokenBudget_UnderBudgetUnchanged(t *testing.T) {
	s := NewTokenBudgetStrategy(4)

	messages := []core.Message{
		core.SystemMessage("system prompt"),
		core.UserMessage("hello"),
		{Role: core.RoleAssistant, Content: "hi"},
	}

	result := s.Trim(messages, 1000) // 4000 字符预算，远未超
	if len(result) != 3 {
		t.Fatalf("预算内应原样返回，期望 3 条，得到 %d", len(result))
	}
	for i := range result {
		if result[i].Content != messages[i].Content {
			t.Errorf("消息 %d 内容被改动", i)
		}
	}
}

func TestTokenBudget_OverBudgetKeepsSystemAndRecent(t *testing.T) {
	s := NewTokenBudgetStrategy(4)

	// 构造：1 条系统（40 字符）+ 大量历史；maxTokens=50 → 预算 200 字符
	messages := []core.Message{core.SystemMessage(strings.Repeat("s", 40))}
	for i := 0; i < 20; i++ {
		messages = append(messages, core.UserMessage(strings.Repeat("u", 30)))
	}

	result := s.Trim(messages, 50)
	// 系统消息必须保留
	if result[0].Role != core.RoleSystem {
		t.Fatal("系统消息必须保留在首位")
	}
	// 总字符不得超预算
	total := 0
	for _, m := range result {
		total += len(m.Content)
	}
	if total > 50*4 {
		t.Errorf("裁剪后总字符 %d 超出预算 %d", total, 50*4)
	}
	// 必须是「系统 + 最近连续后缀」：最后一条必须是原始最后一条
	if result[len(result)-1].Content != messages[len(messages)-1].Content {
		t.Error("应从最近消息向前保留，最后一条必须是最新消息")
	}
	// 至少保留了不止系统消息
	if len(result) < 2 {
		t.Error("预算充足时应保留至少一条历史")
	}
}

func TestTokenBudget_SystemExceedsBudget(t *testing.T) {
	s := NewTokenBudgetStrategy(4)

	messages := []core.Message{
		core.SystemMessage(strings.Repeat("s", 500)), // 500 字符 > 200 字符预算
		core.UserMessage("hello"),
	}

	result := s.Trim(messages, 50)
	if len(result) != 1 || result[0].Role != core.RoleSystem {
		t.Fatalf("系统消息超预算时应仅返回系统消息，得到 %d 条", len(result))
	}
}

func TestTokenBudget_SingleMessageTooLarge(t *testing.T) {
	s := NewTokenBudgetStrategy(4)

	messages := []core.Message{
		core.SystemMessage("sys"),
		core.UserMessage(strings.Repeat("x", 1000)),
		core.UserMessage(strings.Repeat("y", 10)),
	}

	result := s.Trim(messages, 50) // 预算 ~392 字符（扣除 sys 后）
	// 1000 字符的消息放不进，但 10 字符的可以
	found := false
	for _, m := range result {
		if m.Content == strings.Repeat("y", 10) {
			found = true
		}
	}
	if !found {
		t.Error("小消息应能放入预算（大消息跳过后不应阻断更早的小消息入选项）")
	}
}

func TestTokenBudget_ToolCallsCounted(t *testing.T) {
	s := NewTokenBudgetStrategy(4)

	bigArgs, _ := json.Marshal(map[string]string{"data": strings.Repeat("a", 400)})
	messages := []core.Message{
		core.SystemMessage("sys"),
		{
			Role: core.RoleAssistant,
			ToolCalls: []core.ToolCall{{
				ID:   "call1",
				Name: "big_tool",
				Args: string(bigArgs),
			}},
		},
	}

	// maxTokens=50 → 200 字符预算；ToolCalls 参数 400+ 字符必然超限
	result := s.Trim(messages, 50)
	total := len(result[0].Content)
	for _, m := range result {
		total += len(m.Content)
		for _, tc := range m.ToolCalls {
			total += len(tc.Name) + len(tc.Args) + len(tc.ID)
		}
	}
	if total > 50*4 {
		t.Errorf("ToolCalls 应计入预算：总字符 %d 超出 %d", total, 50*4)
	}
	if len(result) != 1 {
		t.Errorf("工具调用消息超预算时不应保留，期望 1 条（仅系统），得到 %d", len(result))
	}
}

func TestTokenBudget_Empty(t *testing.T) {
	s := NewTokenBudgetStrategy(4)
	result := s.Trim(nil, 100)
	if len(result) != 0 {
		t.Errorf("空输入应返回空，得到 %d", len(result))
	}
}

func TestTokenBudget_DefaultCharsPerToken(t *testing.T) {
	// charsPerToken <= 0 时应回退默认值 4（与 TS 一致）
	s := NewTokenBudgetStrategy(0)
	if s.CharsPerToken != 4 {
		t.Errorf("默认 charsPerToken 应为 4，得到 %d", s.CharsPerToken)
	}
}

func TestTokenBudget_TokenSavingsVsCountWindow(t *testing.T) {
	// 量化验证：计数窗口在「少条数大消息」场景下 token 失控，
	// TokenBudget 策略将上下文约束在预算内 → token 成本降幅可量化
	countBased := NewDefaultStrategy(10)
	budgetBased := NewTokenBudgetStrategy(4)

	messages := []core.Message{core.SystemMessage(strings.Repeat("s", 100))}
	for i := 0; i < 10; i++ {
		messages = append(messages, core.UserMessage(strings.Repeat("x", 2000))) // 每条 ~500 token
	}

	maxTokens := 1200
	trimmedByBudget := budgetBased.Trim(messages, maxTokens)

	total := 0
	for _, m := range trimmedByBudget {
		total += len(m.Content)
	}
	if total > maxTokens*4 {
		t.Errorf("TokenBudget 裁剪后 %d 字符超出预算 %d", total, maxTokens*4)
	}

	byCount := countBased.Trim(messages, 10)
	countTotal := 0
	for _, m := range byCount {
		countTotal += len(m.Content)
	}
	// 计数窗口保留 10 条 × 2000 字符 ≈ 20000 字符 ≈ 5000 token，远超 1200 token 预算
	if countTotal <= total {
		t.Errorf("计数窗口 (%d 字符) 应显著大于 token 预算窗口 (%d 字符)", countTotal, total)
	}
}
