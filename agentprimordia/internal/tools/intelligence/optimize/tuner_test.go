// tuner_test.go — 数据驱动调优器测试
package optimize

import (
	"context"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/tools/intelligence"
)

// TestDataDrivenTuner_LowSuccessRate 测试低成功率建议重试
func TestDataDrivenTuner_LowSuccessRate(t *testing.T) {
	ctx := context.Background()
	tuner := NewDataDrivenTuner()

	profile := &intelligence.ToolProfile{
		ToolName:    "shell",
		TotalCalls:  10,
		SuccessRate: 0.3, // 低于 0.7 阈值
		AvgDuration: 100 * time.Millisecond,
	}

	suggestion, err := tuner.SuggestTuning(ctx, "shell", profile)
	if err != nil {
		t.Fatalf("生成建议失败: %v", err)
	}

	if suggestion == nil {
		t.Fatal("期望获得调优建议，实际为 nil")
	}
	if suggestion.Parameter != "retry" {
		t.Errorf("期望 Parameter=retry，实际=%s", suggestion.Parameter)
	}
	if suggestion.SuggestedVal != "2" {
		t.Errorf("期望 SuggestedVal=2，实际=%s", suggestion.SuggestedVal)
	}
}

// TestDataDrivenTuner_HighLatency 测试高延迟建议增大超时
func TestDataDrivenTuner_HighLatency(t *testing.T) {
	ctx := context.Background()
	tuner := NewDataDrivenTuner()

	profile := &intelligence.ToolProfile{
		ToolName:    "web",
		TotalCalls:  10,
		SuccessRate: 0.8, // 高于阈值
		AvgDuration: 10 * time.Second, // 超过 5s 阈值
	}

	suggestion, err := tuner.SuggestTuning(ctx, "web", profile)
	if err != nil {
		t.Fatalf("生成建议失败: %v", err)
	}

	if suggestion == nil {
		t.Fatal("期望获得调优建议，实际为 nil")
	}
	if suggestion.Parameter != "timeout" {
		t.Errorf("期望 Parameter=timeout，实际=%s", suggestion.Parameter)
	}
}

// TestDataDrivenTuner_GoodPerformance 测试良好表现无需调优
func TestDataDrivenTuner_GoodPerformance(t *testing.T) {
	ctx := context.Background()
	tuner := NewDataDrivenTuner()

	profile := &intelligence.ToolProfile{
		ToolName:    "file",
		TotalCalls:  10,
		SuccessRate: 0.9, // 高于阈值
		AvgDuration: 500 * time.Millisecond, // 低于阈值
	}

	suggestion, err := tuner.SuggestTuning(ctx, "file", profile)
	if err != nil {
		t.Fatalf("生成建议失败: %v", err)
	}

	if suggestion != nil {
		t.Errorf("期望无需调优（返回 nil），实际获得建议: %+v", suggestion)
	}
}

// TestDataDrivenTuner_NilProfile 测试空画像报错
func TestDataDrivenTuner_NilProfile(t *testing.T) {
	ctx := context.Background()
	tuner := NewDataDrivenTuner()

	_, err := tuner.SuggestTuning(ctx, "tool", nil)
	if err == nil {
		t.Error("期望空画像返回错误，实际为 nil")
	}
}
