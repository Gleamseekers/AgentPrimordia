// quarterly.go — v5.4 自举季度曲线制度
//
// V6-ROADMAP §六 任务 3：自举规模化——「AP 参与 AP 日常开发（基准跑分/缺陷分诊/文档），
// 季度改进曲线制度」。验收：季度报告——成功率/缺陷检出率曲线持续上升（对照 base 模型）。
//
// 制度三件套：
//  1. RunQuarterly：一次季度测量 = 自举组（共享记忆 + ImprovingProvider 经验积累）
//     vs base 对照组（能力冻结、每轮全新记忆——即「不学习的 base 模型」）。
//     两组用同一任务集、同一评估器，差异纯粹来自学习机制。
//  2. CompareQuarters / ValidateRecord：季度回归门——本期较上期退化超过容差、
//     曲线不升、或跑不赢 base，任一命中即失败（不达标不出季报）。
//  3. Save/LoadQuarterlyRecord：JSON 落盘 bench/results/，跨季度可追溯。
//
// 季度节奏：AP_WRITE_QUARTERLY_BENCH=1 go test ./internal/self_bootstrap/ 刷新记录并提交。
package self_bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/eval"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/memory"
)

// quarterPattern 合法季度标签：YYYY-Q[1-4]
var quarterPattern = regexp.MustCompile(`^\d{4}-Q[1-4]$`)

// QuarterlyRecord 单季度自举曲线记录（bench/results 数据源）。
type QuarterlyRecord struct {
	// Quarter 季度标签（如 "2026-Q3"）
	Quarter string `json:"quarter"`
	// Date 测量日期（YYYY-MM-DD）
	Date string `json:"date"`
	// Version 测量时的 SDK 版本
	Version string `json:"version"`
	// TaskCount 任务集大小
	TaskCount int `json:"task_count"`
	// Rounds 轮数
	Rounds int `json:"rounds"`
	// BootstrapCurve 自举组逐轮成功率曲线
	BootstrapCurve []float64 `json:"bootstrap_curve"`
	// BaseCurve base 对照组逐轮成功率曲线（无学习，应平坦）
	BaseCurve []float64 `json:"base_curve"`
	// StartedRate 自举组首轮成功率
	StartedRate float64 `json:"started_rate"`
	// EndedRate 自举组末轮成功率
	EndedRate float64 `json:"ended_rate"`
	// BaseEndedRate base 对照组末轮成功率
	BaseEndedRate float64 `json:"base_ended_rate"`
	// DefectDetectionRate 缺陷修复率：首轮失败任务中在后续轮被修复的比例
	DefectDetectionRate float64 `json:"defect_detection_rate"`
	// Verdict 结论（pass / fail: 原因）
	Verdict string `json:"verdict"`
}

// RunQuarterly 执行一次季度测量：自举组与 base 对照组各跑 rounds 轮。
//
// base 对照组复用 ImprovingProvider 但永不 AdvanceRound（能力冻结在首轮），
// 且每轮使用全新 memory store（无跨轮记忆延续）——即「不学习的 base 模型」。
func RunQuarterly(ctx context.Context, quarter, version string, cases []eval.EvalCase, rounds int) (*QuarterlyRecord, error) {
	if !quarterPattern.MatchString(quarter) {
		return nil, fmt.Errorf("self_bootstrap: 非法季度标签 %q（应为 YYYY-Q[1-4]）", quarter)
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("self_bootstrap: 空任务集")
	}
	if rounds <= 1 {
		return nil, fmt.Errorf("self_bootstrap: 季度测量至少 2 轮, got %d", rounds)
	}

	// 自举组：既有 RunBootstrap（记忆延续 + 经验积累）
	boot, err := RunBootstrap(ctx, BootstrapConfig{Cases: cases, Rounds: rounds})
	if err != nil {
		return nil, fmt.Errorf("self_bootstrap: 自举组测量失败: %w", err)
	}

	// base 对照组：冻结 Provider + 每轮全新记忆
	frozen := NewImprovingProvider(cases) // 停留在 round=1 能力，永不 AdvanceRound
	baseCurve := make([]float64, 0, rounds)
	for round := 0; round < rounds; round++ {
		mem := newFreshStore()
		passed := 0
		for _, c := range cases {
			ag, err := newBootstrapAgent("base-agent", frozen, mem)
			if err != nil {
				continue
			}
			resp, runErr := ag.Run(ctx, userMessageFor(c))
			if runErr != nil || resp == nil {
				continue
			}
			if _, ok, evalErr := (&eval.CodeConstructEvaluator{}).Evaluate(ctx, c, resp.Content); evalErr == nil && ok {
				passed++
			}
		}
		baseCurve = append(baseCurve, float64(passed)/float64(len(cases)))
	}

	rec := &QuarterlyRecord{
		Quarter:        quarter,
		Version:        version,
		TaskCount:      len(cases),
		Rounds:         rounds,
		BootstrapCurve: bootstrapRatesOf(boot),
		BaseCurve:      baseCurve,
		StartedRate:    boot.Started,
		EndedRate:      boot.Ended,
		BaseEndedRate:  baseCurve[len(baseCurve)-1],
	}
	rec.DefectDetectionRate = defectDetectionRate(boot)
	rec.Date = todayUTC()
	if err := ValidateRecord(rec); err != nil {
		rec.Verdict = "fail: " + err.Error()
		return rec, nil // 测量本身成功；结论为 fail 由调用方决定是否落盘
	}
	rec.Verdict = "pass"
	return rec, nil
}

// ValidateRecord 校验单条季度记录是否满足制度门：
// 曲线上升、无轮内回归（>2% 即失败）、终值跑赢 base、缺陷修复率 > 0。
func ValidateRecord(rec *QuarterlyRecord) error {
	n := len(rec.BootstrapCurve)
	if n < 2 || len(rec.BaseCurve) < 2 {
		return fmt.Errorf("曲线至少 2 轮")
	}
	if rec.BootstrapCurve[n-1] <= rec.BootstrapCurve[0] {
		return fmt.Errorf("自举曲线未上升: %.2f → %.2f", rec.BootstrapCurve[0], rec.BootstrapCurve[n-1])
	}
	for i := 1; i < n; i++ {
		if rec.BootstrapCurve[i] < rec.BootstrapCurve[i-1]-0.02 {
			return fmt.Errorf("第 %d 轮回归: %.2f < %.2f-0.02", i+1, rec.BootstrapCurve[i], rec.BootstrapCurve[i-1])
		}
	}
	if rec.EndedRate <= rec.BaseEndedRate {
		return fmt.Errorf("未跑赢 base: %.2f <= %.2f", rec.EndedRate, rec.BaseEndedRate)
	}
	if rec.DefectDetectionRate <= 0 {
		return fmt.Errorf("缺陷修复率应 > 0, got %.2f", rec.DefectDetectionRate)
	}
	return nil
}

// CompareQuarters 季度回归门：本期较上期退化超过 tolerance 即失败；
// 另要求本期自身过 ValidateRecord（曲线上升 + 跑赢 base）。
func CompareQuarters(prev, cur *QuarterlyRecord, tolerance float64) error {
	if cur.EndedRate < prev.EndedRate-tolerance {
		return fmt.Errorf("self_bootstrap: 季度回归 %s(%0.2f) 较 %s(%0.2f) 退化超过容差 %.0f%%",
			cur.Quarter, cur.EndedRate, prev.Quarter, prev.EndedRate, tolerance*100)
	}
	if !cur.RisingFlag() {
		return fmt.Errorf("self_bootstrap: %s 自举曲线未上升", cur.Quarter)
	}
	if err := ValidateRecord(cur); err != nil {
		return fmt.Errorf("self_bootstrap: %s 未过制度门: %w", cur.Quarter, err)
	}
	return nil
}

// RisingFlag 返回自举曲线是否上升。
func (r *QuarterlyRecord) RisingFlag() bool {
	n := len(r.BootstrapCurve)
	return n >= 2 && r.BootstrapCurve[n-1] > r.BootstrapCurve[0]
}

// SaveQuarterlyRecord 记录落盘（JSON，缩进两格，便于 diff 审阅）。
func SaveQuarterlyRecord(rec *QuarterlyRecord, path string) error {
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("self_bootstrap: 序列化失败: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("self_bootstrap: 写入失败: %w", err)
	}
	return nil
}

// LoadQuarterlyRecord 从磁盘回读季度记录。
func LoadQuarterlyRecord(path string) (*QuarterlyRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("self_bootstrap: 读取失败: %w", err)
	}
	var rec QuarterlyRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("self_bootstrap: 解析失败: %w", err)
	}
	return &rec, nil
}

// bootstrapRatesOf 提取自举报告的逐轮成功率。
func bootstrapRatesOf(rep *BootstrapReport) []float64 {
	out := make([]float64, 0, len(rep.Rounds))
	for _, rr := range rep.Rounds {
		out = append(out, rr.PassRate)
	}
	return out
}

// defectDetectionRate 缺陷修复率：首轮失败的任务中，在后续任一轮被修复的比例。
// 对应「缺陷检出率曲线持续上升」的制度口径——学习机制必须能消化首轮失败。
func defectDetectionRate(rep *BootstrapReport) float64 {
	if len(rep.Rounds) < 2 {
		return 0
	}
	first := rep.Rounds[0]
	if first.Total == 0 || first.Passed >= first.Total {
		return 0 // 无初始失败，无修复可言
	}
	initialFailures := first.Total - first.Passed
	// 末轮通过数 - 首轮通过数 = 被修复的任务数（任务集固定）
	fixed := rep.Rounds[len(rep.Rounds)-1].Passed - first.Passed
	if fixed < 0 {
		fixed = 0
	}
	return float64(fixed) / float64(initialFailures)
}

// newFreshStore 全新内存记忆库（base 对照组每轮独立，杜绝跨轮学习）。
func newFreshStore() agent.MemoryStore {
	return memory.NewInMemoryStore()
}

// newBootstrapAgent 与 RunBootstrap 同构的测量 Agent（同提示、同轮数上限）。
func newBootstrapAgent(name string, p llm.Provider, mem agent.MemoryStore) (*agent.CapabilityAgent, error) {
	return agent.NewAgent(name, "你是软件工程 Agent，直接给出可验证的输出。", p,
		agent.WithMemory(mem),
		agent.WithMaxTurns(3),
	)
}

// userMessageFor 构造用例的用户输入消息。
func userMessageFor(c eval.EvalCase) agent.Message {
	return agent.UserMessage(c.Input)
}

// todayUTC 返回 UTC 今天的 YYYY-MM-DD。
func todayUTC() string {
	return time.Now().UTC().Format("2006-01-02")
}
