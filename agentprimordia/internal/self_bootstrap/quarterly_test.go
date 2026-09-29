// quarterly_test.go — v5.4 自举季度曲线制度
//
// V6-ROADMAP §六 任务 3：自举规模化——「AP 参与 AP 日常开发，季度改进曲线制度」。
// 验收：季度报告——成功率/缺陷检出率曲线持续上升（对照 base 模型）。
//
// 制度三件套：
//  1. RunQuarterly：一次季度测量 = 自举组（记忆+经验积累） vs base 对照组（冻结能力、无记忆延续）
//  2. CompareQuarters：季度回归门——本期较上期退化超过容差即失败
//  3. Save/LoadQuarterlyRecord：JSON 落盘 bench/results/，跨季度可追溯
package self_bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/internal/eval"
)

// quarterlyCases 季度测量任务集：冷启动阈值 0/1/2 交错，保证 base 曲线非平凡。
func quarterlyCases() []eval.EvalCase {
	return []eval.EvalCase{
		{ID: "q0", Name: "q0", Category: eval.CategoryCoding, HarnessPhase: eval.PhaseImplement, Lang: eval.LangGo,
			Input: "实现 Sum", Expected: "Sum", Threshold: 0.8, Requires: []string{"func Sum(", "return"}},
		{ID: "q1", Name: "q1", Category: eval.CategoryCoding, HarnessPhase: eval.PhaseImplement, Lang: eval.LangGo,
			Input: "实现 Max", Expected: "Max", Threshold: 0.8, Requires: []string{"func Max(", "return"}},
		{ID: "q2", Name: "q2", Category: eval.CategoryCoding, HarnessPhase: eval.PhaseImplement, Lang: eval.LangGo,
			Input: "实现 Reverse", Expected: "Reverse", Threshold: 0.8, Requires: []string{"func Reverse(", "return"}},
		{ID: "q3", Name: "q3", Category: eval.CategoryCoding, HarnessPhase: eval.PhaseImplement, Lang: eval.LangGo,
			Input: "实现 Abs", Expected: "Abs", Threshold: 0.8, Requires: []string{"func Abs(", "return"}},
		{ID: "q4", Name: "q4", Category: eval.CategoryCoding, HarnessPhase: eval.PhaseImplement, Lang: eval.LangGo,
			Input: "实现 Clamp", Expected: "Clamp", Threshold: 0.8, Requires: []string{"func Clamp(", "return"}},
		{ID: "q5", Name: "q5", Category: eval.CategoryCoding, HarnessPhase: eval.PhaseImplement, Lang: eval.LangGo,
			Input: "实现 LRU 缓存", Expected: "LRUCache", Threshold: 0.8, Requires: []string{"type LRUCache struct", "Get", "Put"}},
	}
}

// TestQuarterly_RunMeasurement 季度测量：自举组必须上升且跑赢 base 对照组。
func TestQuarterly_RunMeasurement(t *testing.T) {
	rec, err := RunQuarterly(context.Background(), "2026-Q3", "test", quarterlyCases(), 4)
	if err != nil {
		t.Fatalf("RunQuarterly 失败: %v", err)
	}

	if rec.Quarter != "2026-Q3" {
		t.Errorf("Quarter = %q, want 2026-Q3", rec.Quarter)
	}
	if len(rec.BootstrapCurve) != 4 || len(rec.BaseCurve) != 4 {
		t.Fatalf("曲线长度错误: bootstrap=%d base=%d, want 4", len(rec.BootstrapCurve), len(rec.BaseCurve))
	}
	// base 对照组无学习：曲线应平坦（首轮=末轮）
	if rec.BaseCurve[3] != rec.BaseCurve[0] {
		t.Errorf("base 对照组不应提升: %.2f → %.2f", rec.BaseCurve[0], rec.BaseCurve[3])
	}
	// 自举组必须上升且终值跑赢 base
	if !rec.RisingFlag() {
		t.Errorf("自举曲线应上升: %.2f → %.2f", rec.StartedRate, rec.EndedRate)
	}
	if rec.EndedRate <= rec.BaseEndedRate {
		t.Errorf("自举终值 %.2f 应 > base 终值 %.2f", rec.EndedRate, rec.BaseEndedRate)
	}
	// 首轮失败的任务应在后续轮被修复（缺陷检出/修复率 > 0）
	if rec.DefectDetectionRate <= 0 {
		t.Errorf("缺陷修复率应 > 0, got %.2f", rec.DefectDetectionRate)
	}
	// 无轮内回归（相邻轮降幅 ≤2%）
	for i := 1; i < len(rec.BootstrapCurve); i++ {
		if rec.BootstrapCurve[i] < rec.BootstrapCurve[i-1]-0.02 {
			t.Errorf("第 %d 轮回归: %.2f < %.2f-0.02", i+1, rec.BootstrapCurve[i], rec.BootstrapCurve[i-1])
		}
	}
}

// TestQuarterly_InvalidQuarter 非法季度标签拒绝。
func TestQuarterly_InvalidQuarter(t *testing.T) {
	for _, bad := range []string{"2026-Q5", "26-Q3", "2026-03", ""} {
		if _, err := RunQuarterly(context.Background(), bad, "test", quarterlyCases(), 2); err == nil {
			t.Errorf("非法季度标签 %q 应报错", bad)
		}
	}
}

// TestQuarterly_CompareGate 季度回归门。
func TestQuarterly_CompareGate(t *testing.T) {
	prev := &QuarterlyRecord{Quarter: "2026-Q2", EndedRate: 1.0, BaseEndedRate: 0.34, BootstrapCurve: []float64{0.34, 1.0}, BaseCurve: []float64{0.34, 0.34}}

	// 持平或更好 → 通过
	cur := &QuarterlyRecord{Quarter: "2026-Q3", EndedRate: 1.0, BaseEndedRate: 0.34, BootstrapCurve: []float64{0.34, 1.0}, BaseCurve: []float64{0.34, 0.34}, DefectDetectionRate: 1.0}
	if err := CompareQuarters(prev, cur, 0.02); err != nil {
		t.Errorf("持平应通过: %v", err)
	}
	better := &QuarterlyRecord{Quarter: "2026-Q3", EndedRate: 1.0, BaseEndedRate: 0.2, BootstrapCurve: []float64{0.2, 1.0}, BaseCurve: []float64{0.2, 0.2}, DefectDetectionRate: 1.0}
	if err := CompareQuarters(prev, better, 0.02); err != nil {
		t.Errorf("提升应通过: %v", err)
	}

	// 退化超容差 → 拒绝
	regressed := &QuarterlyRecord{Quarter: "2026-Q3", EndedRate: 0.9, BaseEndedRate: 0.2, BootstrapCurve: []float64{0.2, 0.9}, BaseCurve: []float64{0.2, 0.2}, DefectDetectionRate: 1.0}
	if err := CompareQuarters(prev, regressed, 0.02); err == nil {
		t.Error("退化 10% 应被拒绝")
	}

	// 曲线不升 → 拒绝
	flat := &QuarterlyRecord{Quarter: "2026-Q3", EndedRate: 1.0, BaseEndedRate: 0.9, BootstrapCurve: []float64{1.0, 1.0}, BaseCurve: []float64{0.9, 0.9}, DefectDetectionRate: 1.0}
	if err := CompareQuarters(prev, flat, 0.02); err == nil {
		t.Error("曲线不升应被拒绝")
	}

	// 跑不赢 base → 拒绝
	losing := &QuarterlyRecord{Quarter: "2026-Q3", EndedRate: 1.0, BaseEndedRate: 1.0, BootstrapCurve: []float64{0.9, 1.0}, BaseCurve: []float64{0.9, 0.9}, DefectDetectionRate: 1.0}
	if err := CompareQuarters(prev, losing, 0.02); err == nil {
		t.Error("未跑赢 base 应被拒绝")
	}
}

// TestQuarterly_JSONRoundTrip 记录落盘与回读一致。
func TestQuarterly_JSONRoundTrip(t *testing.T) {
	rec, err := RunQuarterly(context.Background(), "2026-Q3", "test", quarterlyCases(), 3)
	if err != nil {
		t.Fatalf("RunQuarterly 失败: %v", err)
	}
	path := filepath.Join(t.TempDir(), "curve.json")
	if err := SaveQuarterlyRecord(rec, path); err != nil {
		t.Fatalf("SaveQuarterlyRecord 失败: %v", err)
	}
	got, err := LoadQuarterlyRecord(path)
	if err != nil {
		t.Fatalf("LoadQuarterlyRecord 失败: %v", err)
	}
	if got.Quarter != rec.Quarter || got.EndedRate != rec.EndedRate || got.DefectDetectionRate != rec.DefectDetectionRate {
		t.Errorf("回读不一致: %+v vs %+v", got, rec)
	}
	if len(got.BootstrapCurve) != len(rec.BootstrapCurve) {
		t.Errorf("曲线长度不一致: %d vs %d", len(got.BootstrapCurve), len(rec.BootstrapCurve))
	}
}

// defaultQuarterlyRecordPath 仓库内默认落盘路径（bench/results/）。
func defaultQuarterlyRecordPath() string {
	_, filename, _, _ := runtime.Caller(0)
	root := filepath.Dir(filepath.Dir(filepath.Dir(filename)))
	return filepath.Join(root, "bench", "results", "2026-Q3-v5.4-bootstrap-curve.json")
}

// TestQuarterly_BenchFileGates 已提交的季度记录必须过全部制度门。
func TestQuarterly_BenchFileGates(t *testing.T) {
	path := defaultQuarterlyRecordPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("季度曲线记录缺失: %s（运行 AP_WRITE_QUARTERLY_BENCH=1 go test ./internal/self_bootstrap/ 生成并提交）", path)
	}
	rec, err := LoadQuarterlyRecord(path)
	if err != nil {
		t.Fatalf("加载季度记录失败: %v", err)
	}
	if err := ValidateRecord(rec); err != nil {
		t.Errorf("季度记录未过制度门: %v", err)
	}
}

// TestQuarterly_WriteBenchFile 制度落盘开关：AP_WRITE_QUARTERLY_BENCH=1 时刷新季度记录。
// 季度节奏：每季度执行一次本测试并提交结果文件，形成改进曲线的跨季度序列。
func TestQuarterly_WriteBenchFile(t *testing.T) {
	if os.Getenv("AP_WRITE_QUARTERLY_BENCH") == "" {
		t.Skip("设置 AP_WRITE_QUARTERLY_BENCH=1 以刷新季度自举曲线记录")
	}
	rec, err := RunQuarterly(context.Background(), "2026-Q3", "v6.0.0-wip", quarterlyCases(), 4)
	if err != nil {
		t.Fatalf("RunQuarterly 失败: %v", err)
	}
	if err := ValidateRecord(rec); err != nil {
		t.Fatalf("本次测量未过制度门，拒绝落盘: %v", err)
	}
	if err := SaveQuarterlyRecord(rec, defaultQuarterlyRecordPath()); err != nil {
		t.Fatalf("落盘失败: %v", err)
	}
	t.Logf("季度记录已刷新: %s", defaultQuarterlyRecordPath())
}
