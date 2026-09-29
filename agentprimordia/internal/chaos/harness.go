package chaos

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/eval"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
)

// FaultInjectingProvider 包装 llm.Provider，按故障序列或概率注入调用失败。
//
// 用于 harness 混沌对比：同一组基准用例，分别在健康 Provider 与
// 故障 Provider 上运行，量化注入故障后的成功率下降。
// 故障序列（FailAt）从 1 开始计数；若未设置 FailAt，则按 Rate 概率失败。
type FaultInjectingProvider struct {
	base   llm.Provider
	failAt []int
	rate   float64
	seq    int
	failed int
	mu     sync.Mutex
}

// NewFaultInjectingProvider 创建故障注入 Provider。
// failAt 为从 1 计数的失败调用序号；rate 为概率失败（failAt 非空时忽略）。
func NewFaultInjectingProvider(base llm.Provider, failAt []int, rate float64) *FaultInjectingProvider {
	return &FaultInjectingProvider{base: base, failAt: failAt, rate: rate}
}

// InjectedFailures 返回已注入的失败次数。
func (f *FaultInjectingProvider) InjectedFailures() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.failed
}

// shouldFail 判定本次调用是否注入失败。
// 故障序列（failAt）命中时必定失败；否则按 rate 概率（周期化，确定性）失败。
func (f *FaultInjectingProvider) shouldFail() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	if len(f.failAt) > 0 {
		for _, n := range f.failAt {
			if n == f.seq {
				f.failed++
				return true
			}
		}
		return false
	}
	// 周期化失败：rate=0.5 → 每 2 次失败 1 次；rate=0.25 → 每 4 次失败 1 次
	if f.rate >= 1 {
		f.failed++
		return true
	}
	if f.rate <= 0 {
		return false
	}
	period := int(1.0 / f.rate)
	if period <= 1 {
		period = 1
	}
	if f.seq%period == 0 {
		f.failed++
		return true
	}
	return false
}

// Complete 执行一次调用；命中故障时返回注入错误。
func (f *FaultInjectingProvider) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if f.shouldFail() {
		return nil, fmt.Errorf("chaos: injected LLM failure (seq=%d)", f.seq)
	}
	return f.base.Complete(ctx, req)
}

// Stream 委托给基础 Provider（注入故障时直接返回错误 channel）。
func (f *FaultInjectingProvider) Stream(ctx context.Context, req *llm.CompletionRequest) (<-chan llm.Chunk, error) {
	if f.shouldFail() {
		ch := make(chan llm.Chunk, 1)
		close(ch)
		return ch, nil
	}
	return f.base.Stream(ctx, req)
}

// CallTools 委托给基础 Provider。
func (f *FaultInjectingProvider) CallTools(ctx context.Context, req *llm.ToolCallRequest) (*llm.ToolCallResponse, error) {
	return f.base.CallTools(ctx, req)
}

// Info 委托给基础 Provider。
func (f *FaultInjectingProvider) Info() llm.ModelInfo { return f.base.Info() }

// HarnessChaosConfig harness 混沌对比配置。
type HarnessChaosConfig struct {
	// Cases 要运行的基准用例（复用 v3.5-1 真实基准集）。
	Cases []eval.EvalCase
	// BaselineProvider 健康基线 Provider。
	BaselineProvider llm.Provider
	// FaultProvider 注入故障的 Provider（应基于同一 base 包装）。
	FaultProvider *FaultInjectingProvider
	// Timeout 单用例超时。
	Timeout time.Duration
}

// HarnessChaosReport 注入故障下的成功率量化报告。
type HarnessChaosReport struct {
	Total            int     `json:"total"`
	BaselinePassed   int     `json:"baseline_passed"`
	FaultPassed      int     `json:"fault_passed"`
	BaselinePassRate float64 `json:"baseline_pass_rate"`
	FaultPassRate    float64 `json:"fault_pass_rate"`
	Degradation      float64 `json:"degradation"`       // 成功率绝对下降
	DegradationPct   float64 `json:"degradation_pct"`   // 相对下降百分比
	InjectedFailures int     `json:"injected_failures"` // 实际注入的故障数
	DurationMs       int64   `json:"duration_ms"`
}

// RunHarnessChaos 运行 harness 混沌对比：基线 vs 注入故障，量化成功率下降。
//
// 流程：对同一组基准用例分别用健康 Provider 与故障 Provider 各跑一遍，
// 用 eval.CodeConstructEvaluator 判分，汇总通过率并计算下降量。
func RunHarnessChaos(ctx context.Context, cfg HarnessChaosConfig) (*HarnessChaosReport, error) {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if len(cfg.Cases) == 0 {
		return nil, fmt.Errorf("chaos: no cases provided")
	}
	if cfg.BaselineProvider == nil || cfg.FaultProvider == nil {
		return nil, fmt.Errorf("chaos: baseline and fault providers are required")
	}

	evaluator := &eval.CodeConstructEvaluator{}
	report := &HarnessChaosReport{Total: len(cfg.Cases)}

	start := time.Now()
	for _, c := range cfg.Cases {
		// 基线
		if ok, err := runOne(ctx, cfg.BaselineProvider, evaluator, c, cfg.Timeout); err == nil && ok {
			report.BaselinePassed++
		}
		// 故障
		if ok, err := runOne(ctx, cfg.FaultProvider, evaluator, c, cfg.Timeout); err == nil && ok {
			report.FaultPassed++
		}
	}
	report.DurationMs = time.Since(start).Milliseconds()

	if report.Total > 0 {
		report.BaselinePassRate = float64(report.BaselinePassed) / float64(report.Total)
		report.FaultPassRate = float64(report.FaultPassed) / float64(report.Total)
	}
	report.Degradation = report.BaselinePassRate - report.FaultPassRate
	if report.BaselinePassRate > 0 {
		report.DegradationPct = report.Degradation / report.BaselinePassRate * 100
	}
	report.InjectedFailures = cfg.FaultProvider.InjectedFailures()
	return report, nil
}

// runOne 对单条用例跑一次 Provider 调用并判分。
func runOne(ctx context.Context, p llm.Provider, evaluator *eval.CodeConstructEvaluator, c eval.EvalCase, timeout time.Duration) (bool, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := p.Complete(cctx, &llm.CompletionRequest{
		Messages: []llm.ChatMessage{
			{Role: "system", Content: "你是软件工程 Agent，直接给出可验证的输出。 case=" + c.ID + " "},
			{Role: "user", Content: c.Input},
		},
		Temperature: llm.Float64Ptr(0),
		MaxTokens:   1024,
	})
	if err != nil {
		return false, err
	}
	_, passed, err := evaluator.Evaluate(cctx, c, resp.Content)
	return passed, err
}
