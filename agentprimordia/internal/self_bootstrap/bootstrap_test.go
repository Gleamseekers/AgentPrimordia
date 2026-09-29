// bootstrap_test.go — v3.6-4 自举测试
// 验证 AP 用 AP 开发 AP：成功率曲线随轮次可见上升。
package self_bootstrap

import (
	"context"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/eval"
)

// bootstrapCases 构造 3 个用例：冷启动阈值分别为 0/1/2 轮。
func bootstrapCases() []eval.EvalCase {
	return []eval.EvalCase{
		{ID: "c0", Name: "c0", Category: eval.CategoryCoding, HarnessPhase: eval.PhaseImplement, Lang: eval.LangGo,
			Input: "实现 Fibonacci", Expected: "Fibonacci", Threshold: 0.8, Requires: []string{"func Fibonacci(", "return"}},
		{ID: "c1", Name: "c1", Category: eval.CategoryCoding, HarnessPhase: eval.PhaseImplement, Lang: eval.LangGo,
			Input: "实现 IsPalindrome", Expected: "IsPalindrome", Threshold: 0.8, Requires: []string{"func IsPalindrome(", "return"}},
		{ID: "c2", Name: "c2", Category: eval.CategoryCoding, HarnessPhase: eval.PhaseImplement, Lang: eval.LangGo,
			Input: "实现 LRU 缓存", Expected: "LRUCache", Threshold: 0.8, Requires: []string{"type LRUCache struct", "Get", "Put"}},
	}
}

// TestBootstrap_SuccessRateCurveRises 验证成功率曲线随轮次上升。
func TestBootstrap_SuccessRateCurveRises(t *testing.T) {
	ctx := context.Background()
	report, err := RunBootstrap(ctx, BootstrapConfig{Cases: bootstrapCases(), Rounds: 4})
	if err != nil {
		t.Fatalf("RunBootstrap failed: %v", err)
	}

	if len(report.Rounds) != 4 {
		t.Fatalf("Rounds = %d, want 4", len(report.Rounds))
	}
	// 冷启动阈值 0/1/2 → round1=1/3, round2=2/3, round3=3/3, round4=3/3
	rates := report.Curve()
	if rates[0][1] != 1.0/3.0 {
		t.Errorf("round1 pass_rate = %f, want 1/3", rates[0][1])
	}
	if rates[1][1] != 2.0/3.0 {
		t.Errorf("round2 pass_rate = %f, want 2/3", rates[1][1])
	}
	if rates[2][1] != 1.0 {
		t.Errorf("round3 pass_rate = %f, want 1.0", rates[2][1])
	}
	// 曲线上升
	if !report.Rising {
		t.Errorf("成功率曲线应上升: started=%f ended=%f", report.Started, report.Ended)
	}
	// 后期轮次应有记忆命中（跨任务记忆起作用）
	if report.Rounds[3].MemoryHits == 0 {
		t.Errorf("后期轮次应有记忆命中, got %d", report.Rounds[3].MemoryHits)
	}
	// 后期轮次 pass_rate 稳定在 1.0
	if report.Rounds[3].PassRate != 1.0 {
		t.Errorf("round4 pass_rate = %f, want 1.0", report.Rounds[3].PassRate)
	}
}

// TestBootstrap_CurveFormat 验证曲线序列格式。
func TestBootstrap_CurveFormat(t *testing.T) {
	report, err := RunBootstrap(context.Background(), BootstrapConfig{Cases: bootstrapCases(), Rounds: 2})
	if err != nil {
		t.Fatalf("RunBootstrap failed: %v", err)
	}
	curve := report.Curve()
	if len(curve) != 2 {
		t.Fatalf("curve len = %d, want 2", len(curve))
	}
	if curve[0][0] != 1 || curve[1][0] != 2 {
		t.Errorf("curve round 序列错误: %v", curve)
	}
}

// TestBootstrap_EmptyCases 无用例报错。
func TestBootstrap_EmptyCases(t *testing.T) {
	_, err := RunBootstrap(context.Background(), BootstrapConfig{Cases: nil, Rounds: 1})
	if err == nil {
		t.Error("空用例应报错")
	}
}
