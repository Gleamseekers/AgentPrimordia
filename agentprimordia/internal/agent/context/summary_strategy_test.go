// summary_strategy_test.go — 摘要压缩策略测试（v3.4-2）
package context

import (
	"strings"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent/core"
)

func msgs(roles []core.Role) []core.Message {
	out := make([]core.Message, 0, len(roles))
	for i, r := range roles {
		out = append(out, core.Message{Role: r, Content: "msg-" + string(r) + "-" + string(rune('a'+i))})
	}
	return out
}

// TestSummarizingStrategy_TrimsWithGoal 验证超出窗口时：
// 保留系统消息与首个用户目标，中间历史折叠为摘要标记，并保留最近 N 条。
func TestSummarizingStrategy_TrimsWithGoal(t *testing.T) {
	s := NewSummarizingStrategy(6)
	history := append(
		[]core.Message{{Role: core.RoleSystem, Content: "system"}},
		msgs([]core.Role{core.RoleUser, core.RoleAssistant, core.RoleUser, core.RoleTool, core.RoleAssistant, core.RoleUser, core.RoleTool, core.RoleUser})...,
	)

	got := s.Trim(history, 6)
	if len(got) > 6 {
		t.Fatalf("裁剪后 len = %d, want <= 6", len(got))
	}
	// 系统消息保留
	if got[0].Role != core.RoleSystem {
		t.Errorf("应保留系统消息, got %+v", got[0])
	}
	// 目标（首条 user 消息）保留
	foundGoal := false
	for _, m := range got {
		if m.Role == core.RoleUser && strings.Contains(m.Content, "msg-user-a") {
			foundGoal = true
		}
	}
	if !foundGoal {
		t.Errorf("应保留首个用户目标 msg-user-a, got %+v", got)
	}
	// 压缩标记存在
	foundSummary := false
	for _, m := range got {
		if strings.Contains(m.Content, "已压缩") {
			foundSummary = true
		}
	}
	if !foundSummary {
		t.Errorf("应包含压缩标记, got %+v", got)
	}
}

// TestSummarizingStrategy_WithinLimit 验证未超限时原样返回。
func TestSummarizingStrategy_WithinLimit(t *testing.T) {
	s := NewSummarizingStrategy(10)
	history := msgs([]core.Role{core.RoleUser, core.RoleAssistant, core.RoleUser})
	got := s.Trim(history, 10)
	if len(got) != len(history) {
		t.Fatalf("未超限不应裁剪, len = %d, want %d", len(got), len(history))
	}
}

// TestSummarizingStrategy_NoSystem 验证无系统消息时仅保留目标+最近。
func TestSummarizingStrategy_NoSystem(t *testing.T) {
	s := NewSummarizingStrategy(4)
	history := msgs([]core.Role{core.RoleUser, core.RoleAssistant, core.RoleUser, core.RoleAssistant, core.RoleUser, core.RoleTool, core.RoleUser})
	got := s.Trim(history, 4)
	if len(got) > 4 {
		t.Fatalf("裁剪后 len = %d, want <= 4", len(got))
	}
	foundGoal := false
	for _, m := range got {
		if m.Role == core.RoleUser && strings.Contains(m.Content, "msg-user-a") {
			foundGoal = true
		}
	}
	if !foundGoal {
		t.Errorf("无系统消息时也应保留目标, got %+v", got)
	}
}
