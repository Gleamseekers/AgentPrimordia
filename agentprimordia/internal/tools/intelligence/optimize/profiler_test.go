// profiler_test.go — 工具性能画像器测试
package optimize

import (
	"context"
	"testing"
	"time"

	"agentprimordia/internal/tools/intelligence"
)

// TestInMemoryProfiler_RecordAndProfile 测试记录并获取画像
func TestInMemoryProfiler_RecordAndProfile(t *testing.T) {
	ctx := context.Background()
	p := NewInMemoryProfiler()

	// 记录 5 次调用：3 成功，2 失败
	records := []intelligence.ToolUsageRecord{
		{ToolName: "shell", Success: true, Duration: 100 * time.Millisecond, Tokens: 50},
		{ToolName: "shell", Success: true, Duration: 200 * time.Millisecond, Tokens: 60},
		{ToolName: "shell", Success: false, Duration: 50 * time.Millisecond, Tokens: 30},
		{ToolName: "shell", Success: true, Duration: 150 * time.Millisecond, Tokens: 55},
		{ToolName: "shell", Success: false, Duration: 80 * time.Millisecond, Tokens: 40},
	}

	for _, r := range records {
		if err := p.Record(ctx, r); err != nil {
			t.Fatalf("记录失败: %v", err)
		}
	}

	// 获取画像
	profile, err := p.Profile(ctx, "shell")
	if err != nil {
		t.Fatalf("获取画像失败: %v", err)
	}

	// 验证总调用数
	if profile.TotalCalls != 5 {
		t.Errorf("期望 TotalCalls=5，实际=%d", profile.TotalCalls)
	}

	// 验证成功率（3/5 = 0.6）
	expectedRate := 0.6
	if profile.SuccessRate < expectedRate-0.01 || profile.SuccessRate > expectedRate+0.01 {
		t.Errorf("期望 SuccessRate≈%.2f，实际=%.2f", expectedRate, profile.SuccessRate)
	}

	// 验证平均 token（235/5 = 47）
	if profile.AvgTokens != 47 {
		t.Errorf("期望 AvgTokens=47，实际=%d", profile.AvgTokens)
	}

	// 验证 P95 延迟（排序后第 4 个：200ms）
	if profile.P95Duration != 200*time.Millisecond {
		t.Errorf("期望 P95Duration=200ms，实际=%v", profile.P95Duration)
	}
}

// TestInMemoryProfiler_AllProfiles 测试获取所有画像
func TestInMemoryProfiler_AllProfiles(t *testing.T) {
	ctx := context.Background()
	p := NewInMemoryProfiler()

	// 记录两个工具的数据
	_ = p.Record(ctx, intelligence.ToolUsageRecord{ToolName: "shell", Success: true, Duration: 100 * time.Millisecond, Tokens: 50})
	_ = p.Record(ctx, intelligence.ToolUsageRecord{ToolName: "shell", Success: false, Duration: 200 * time.Millisecond, Tokens: 60})
	_ = p.Record(ctx, intelligence.ToolUsageRecord{ToolName: "file", Success: true, Duration: 150 * time.Millisecond, Tokens: 40})

	// 获取所有画像
	profiles, err := p.AllProfiles(ctx)
	if err != nil {
		t.Fatalf("获取所有画像失败: %v", err)
	}

	// 验证数量
	if len(profiles) != 2 {
		t.Errorf("期望 2 个画像，实际=%d", len(profiles))
	}

	// 验证 shell 画像
	shellProfile, ok := profiles["shell"]
	if !ok {
		t.Fatal("期望找到 shell 画像")
	}
	if shellProfile.TotalCalls != 2 {
		t.Errorf("期望 shell TotalCalls=2，实际=%d", shellProfile.TotalCalls)
	}
	if shellProfile.SuccessRate != 0.5 {
		t.Errorf("期望 shell SuccessRate=0.5，实际=%.2f", shellProfile.SuccessRate)
	}

	// 验证 file 画像
	fileProfile, ok := profiles["file"]
	if !ok {
		t.Fatal("期望找到 file 画像")
	}
	if fileProfile.TotalCalls != 1 {
		t.Errorf("期望 file TotalCalls=1，实际=%d", fileProfile.TotalCalls)
	}
	if fileProfile.SuccessRate != 1.0 {
		t.Errorf("期望 file SuccessRate=1.0，实际=%.2f", fileProfile.SuccessRate)
	}
}

// TestInMemoryProfiler_EmptyProfile 测试空画像
func TestInMemoryProfiler_EmptyProfile(t *testing.T) {
	ctx := context.Background()
	p := NewInMemoryProfiler()

	// 获取不存在的工具画像
	profile, err := p.Profile(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("获取画像失败: %v", err)
	}

	// 应返回零值画像
	if profile.TotalCalls != 0 {
		t.Errorf("期望 TotalCalls=0，实际=%d", profile.TotalCalls)
	}
	if profile.SuccessRate != 0 {
		t.Errorf("期望 SuccessRate=0，实际=%.2f", profile.SuccessRate)
	}
}
