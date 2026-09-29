package eval

import (
	"context"
	"sync"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
)

// llmBenchSystemPrompt 面向编码任务基准的系统提示词。
// 要求模型直接给出可评估的输出（代码 / 结论），不返回多余说明。
const llmBenchSystemPrompt = "你是一个严谨的软件工程 Agent。请针对用户的任务直接给出可执行、可验证的输出：" +
	"- 编码任务：给出完整可编译的代码（含函数签名与核心逻辑）；" +
	"- 计划/审查/发布/护栏任务：给出简洁明确的结构化结论；" +
	"- 不要输出与任务无关的解释、不要输出 Markdown 代码围栏之外的多余内容。"

// LLMBenchAgent 真实 LLM 基准 Agent：
// 把基准用例输入交给真实 Provider 生成输出，并累计 token 用量与调用次数。
// 实现 EvalAgent 接口（Run），供 BenchmarkRunner 与 RunLLMBench 复用。
type LLMBenchAgent struct {
	provider  llm.Provider
	model     string
	system    string
	timeout   time.Duration
	maxTokens int

	mu    sync.Mutex
	usage llm.Usage
	calls int
}

// NewLLMBenchAgent 创建真实 LLM 基准 Agent。
// provider 为已配置的真实 Provider；model 指定被测模型（透传给 Complete）。
func NewLLMBenchAgent(provider llm.Provider, model string, opts ...LLMBenchOption) *LLMBenchAgent {
	a := &LLMBenchAgent{
		provider:  provider,
		model:     model,
		system:    llmBenchSystemPrompt,
		timeout:   60 * time.Second,
		maxTokens: 2048,
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// LLMBenchOption 配置项函数。
type LLMBenchOption func(*LLMBenchAgent)

// WithLLMSystemPrompt 覆盖系统提示词。
func WithLLMSystemPrompt(p string) LLMBenchOption {
	return func(a *LLMBenchAgent) { a.system = p }
}

// WithLLMTimeout 覆盖单次调用超时。
func WithLLMTimeout(d time.Duration) LLMBenchOption {
	return func(a *LLMBenchAgent) { a.timeout = d }
}

// Run 执行一次基准调用，返回模型输出并累计用量。
func (a *LLMBenchAgent) Run(ctx context.Context, input string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	resp, err := a.provider.Complete(ctx, &llm.CompletionRequest{
		Messages: []llm.ChatMessage{
			{Role: "system", Content: a.system},
			{Role: "user", Content: input},
		},
		Temperature: llm.Float64Ptr(0),
		MaxTokens:   a.maxTokens,
		Model:       a.model,
	})
	if err != nil {
		return "", err
	}

	a.mu.Lock()
	a.usage.PromptTokens += resp.Usage.PromptTokens
	a.usage.CompletionTokens += resp.Usage.CompletionTokens
	a.usage.TotalTokens += resp.Usage.TotalTokens
	a.calls++
	a.mu.Unlock()

	return resp.Content, nil
}

// Usage 返回累计 token 用量。
func (a *LLMBenchAgent) Usage() llm.Usage {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.usage
}

// CallCount 返回累计调用次数。
func (a *LLMBenchAgent) CallCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls
}

// LLMBenchConfig 真实 LLM 跑分配置。
type LLMBenchConfig struct {
	// Version 被测框架版本号（写入报告）。
	Version string
	// Model 被测模型名。
	Model string
	// ProviderName 被测 Provider 名（openai / anthropic / deepseek ...）。
	ProviderName string
	// Retries 首次失败后的重试次数（用于测量恢复率）。
	Retries int
	// Timeout 单次调用超时。
	Timeout time.Duration
	// Threshold 版本门禁通过率下限。
	Threshold float64
	// Baseline 基线通过率（分数只升不降的对比对象）。
	Baseline float64
}

// LLMBenchCaseResult 单条基准用例执行结果（含恢复信息）。
type LLMBenchCaseResult struct {
	CaseID    string  `json:"case_id"`
	Phase     string  `json:"phase,omitempty"`
	Lang      string  `json:"lang,omitempty"`
	Passed    bool    `json:"passed"`
	Score     float64 `json:"score"`
	Duration  int64   `json:"duration_ms"`
	Attempts  int     `json:"attempts"`
	Recovered bool    `json:"recovered,omitempty"`
	Error     string  `json:"error,omitempty"`
}

// LLMBenchResult 真实 LLM 跑分报告。
type LLMBenchResult struct {
	Version          string               `json:"version"`
	Model            string               `json:"model"`
	Provider         string               `json:"provider"`
	Total            int                  `json:"total"`
	Passed           int                  `json:"passed"`
	Failed           int                  `json:"failed"`
	PassRate         float64              `json:"pass_rate"`
	CostUSD          float64              `json:"cost_usd"`
	LatencyMs        int64                `json:"latency_ms"`
	AvgLatencyMs     int64                `json:"avg_latency_ms"`
	PromptTokens     int                  `json:"prompt_tokens"`
	CompletionTokens int                  `json:"completion_tokens"`
	TotalTokens      int                  `json:"total_tokens"`
	RecoveryRate     float64              `json:"recovery_rate"`
	Threshold        float64              `json:"threshold"`
	Baseline         float64              `json:"baseline"`
	MeetsGate        bool                 `json:"meets_gate"`
	Generated        string               `json:"generated"`
	Cases            []LLMBenchCaseResult `json:"cases"`
}

// RunLLMBench 对给定真实 Agent 运行基准集，产出含成本/耗时/恢复率的跑分报告。
//
// 恢复率定义：首轮失败但在重试（Retries）内成功的用例占比。
// 门禁判定：通过率 ≥ max(Baseline, Threshold) 即视为达标（分数只升不降）。
func RunLLMBench(ctx context.Context, cfg LLMBenchConfig, agent *LLMBenchAgent, cases []EvalCase) (*LLMBenchResult, error) {
	evaluator := &CodeConstructEvaluator{}
	res := &LLMBenchResult{
		Version:   cfg.Version,
		Model:     cfg.Model,
		Provider:  cfg.ProviderName,
		Threshold: cfg.Threshold,
		Baseline:  cfg.Baseline,
		Cases:     make([]LLMBenchCaseResult, 0, len(cases)),
	}
	res.Total = len(cases)

	recoverable := 0 // 首轮失败但可恢复的用例数
	startAll := time.Now()
	for _, c := range cases {
		caseStart := time.Now()
		cr := LLMBenchCaseResult{
			CaseID: c.ID,
			Phase:  c.HarnessPhase,
			Lang:   c.Lang,
		}

		output, err := agent.Run(ctx, c.Input)
		cr.Attempts = 1

		if err == nil {
			score, passed, evalErr := evaluator.Evaluate(ctx, c, output)
			cr.Score = score
			if evalErr != nil {
				err = evalErr
			} else {
				cr.Passed = passed
			}
		}

		// 首轮失败时按 Retries 重试，测量恢复率
		for !cr.Passed && err == nil && cr.Attempts <= cfg.Retries {
			cr.Attempts++
			output, retryErr := agent.Run(ctx, c.Input)
			if retryErr != nil {
				err = retryErr
				break
			}
			score, passed, evalErr := evaluator.Evaluate(ctx, c, output)
			cr.Score = score
			if evalErr != nil {
				err = evalErr
				break
			}
			if passed {
				cr.Passed = true
				cr.Recovered = true
				recoverable++
			}
		}

		if err != nil {
			cr.Error = err.Error()
		}
		cr.Duration = time.Since(caseStart).Milliseconds()

		if cr.Passed {
			res.Passed++
		} else {
			res.Failed++
		}
		res.Cases = append(res.Cases, cr)
	}

	res.LatencyMs = time.Since(startAll).Milliseconds()
	if res.Total > 0 {
		res.PassRate = float64(res.Passed) / float64(res.Total)
		res.AvgLatencyMs = res.LatencyMs / int64(res.Total)
	}
	// 恢复率 = 首轮失败用例中恢复的比例；无失败则视为 1.0
	if res.Failed > 0 {
		res.RecoveryRate = float64(recoverable) / float64(res.Failed+recoverable)
	} else {
		res.RecoveryRate = 1.0
	}

	// 成本估算
	usage := agent.Usage()
	res.PromptTokens = usage.PromptTokens
	res.CompletionTokens = usage.CompletionTokens
	res.TotalTokens = usage.TotalTokens
	res.CostUSD = llm.EstimateCost(cfg.Model, usage, llm.DefaultPricingTable())

	gate := cfg.Baseline
	if cfg.Threshold > gate {
		gate = cfg.Threshold
	}
	res.MeetsGate = res.PassRate >= gate
	res.Generated = time.Now().UTC().Format(time.RFC3339)
	return res, nil
}
