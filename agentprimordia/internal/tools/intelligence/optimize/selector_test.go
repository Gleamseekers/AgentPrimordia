// selector_test.go — 历史成功率工具选择器测试
package optimize

import (
	"context"
	"testing"
)

// TestHistorySelector_SelectBest 测试选择成功率最高的工具
func TestHistorySelector_SelectBest(t *testing.T) {
	ctx := context.Background()
	selector := NewHistorySelector()

	// 记录工具调用结果
	_ = selector.RecordOutcome(ctx, "shell", true)
	_ = selector.RecordOutcome(ctx, "shell", true)
	_ = selector.RecordOutcome(ctx, "shell", false) // 2/3 = 0.667

	_ = selector.RecordOutcome(ctx, "file", true)
	_ = selector.RecordOutcome(ctx, "file", true) // 2/2 = 1.0

	_ = selector.RecordOutcome(ctx, "web", false)
	_ = selector.RecordOutcome(ctx, "web", false) // 0/2 = 0.0

	// 选择
	selected, err := selector.Select(ctx, "task", []string{"shell", "file", "web"})
	if err != nil {
		t.Fatalf("选择失败: %v", err)
	}

	// 应选 file（成功率最高 1.0）
	if selected != "file" {
		t.Errorf("期望选择 file，实际=%s", selected)
	}
}

// TestHistorySelector_NoHistory 测试无历史记录
func TestHistorySelector_NoHistory(t *testing.T) {
	ctx := context.Background()
	selector := NewHistorySelector()

	// 无历史记录时返回第一个
	selected, err := selector.Select(ctx, "task", []string{"shell", "file"})
	if err != nil {
		t.Fatalf("选择失败: %v", err)
	}

	if selected != "shell" {
		t.Errorf("无历史记录时应返回第一个候选，实际=%s", selected)
	}
}

// TestHistorySelector_EmptyCandidates 测试空候选列表
func TestHistorySelector_EmptyCandidates(t *testing.T) {
	ctx := context.Background()
	selector := NewHistorySelector()

	_, err := selector.Select(ctx, "task", []string{})
	if err == nil {
		t.Error("期望空候选列表返回错误，实际为 nil")
	}
}

// TestHistorySelector_RecordOutcome 测试记录结果
func TestHistorySelector_RecordOutcome(t *testing.T) {
	ctx := context.Background()
	selector := NewHistorySelector()

	// 记录成功
	_ = selector.RecordOutcome(ctx, "shell", true)
	_ = selector.RecordOutcome(ctx, "shell", true)

	// 记录失败
	_ = selector.RecordOutcome(ctx, "shell", false)

	// 验证统计
	success, total := selector.GetStats("shell")
	if success != 2 {
		t.Errorf("期望 success=2，实际=%d", success)
	}
	if total != 3 {
		t.Errorf("期望 total=3，实际=%d", total)
	}
}

// TestHistorySelector_GetStatsNonexistent 测试获取不存在工具的统计
func TestHistorySelector_GetStatsNonexistent(t *testing.T) {
	selector := NewHistorySelector()

	success, total := selector.GetStats("nonexistent")
	if success != 0 || total != 0 {
		t.Errorf("期望 (0, 0)，实际=(%d, %d)", success, total)
	}
}
