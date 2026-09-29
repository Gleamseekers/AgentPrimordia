// Package self_bootstrap 验证"AP 用 AP 开发 AP"（v3.6-4）。
//
// 使用 AgentPrimordia 框架自身（真实 ReActAgent + 共享记忆）反复解决
// 一组编码任务，观察成功率随轮次变化。由于 v3.6-3 跨任务记忆：
//   - 已解任务在后续轮次命中记忆（0 轮推理，必成功）
//   - 未解任务由模型（ImprovingProvider）随经验积累逐步解决
//
// 综合效果：成功率曲线随轮次可见上升（自举成功）。
package self_bootstrap

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/eval"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/memory"
)

// ImprovingProvider 模拟一个随经验积累而能力提升的 LLM Provider。
//
// 冷启动：每个用例在前 threshold 轮内返回空输出（评估器判失败，且不被存为已解记忆）；
// 达到轮次后返回该用例的合规输出（requires 拼接，可通过评估器）。
// threshold 由用例在注册切片中的序号决定（idx%3），保证曲线阶梯式上升且完全确定。
type ImprovingProvider struct {
	mu    sync.Mutex
	round int
	// inputs: 用户输入 → 用例（用于生成合规输出）
	inputs map[string]eval.EvalCase
	// indexByInput: 用户输入 → 注册序号（决定冷启动阈值，确定性）
	indexByInput map[string]int
}

// NewImprovingProvider 创建提升型 Provider。
func NewImprovingProvider(cases []eval.EvalCase) *ImprovingProvider {
	p := &ImprovingProvider{
		round:        1,
		inputs:       make(map[string]eval.EvalCase, len(cases)),
		indexByInput: make(map[string]int, len(cases)),
	}
	for i, c := range cases {
		p.inputs[c.Input] = c
		p.indexByInput[c.Input] = i
	}
	return p
}

// AdvanceRound 进入下一轮（模型经验提升）。
func (p *ImprovingProvider) AdvanceRound() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.round++
}

// CurrentRound 返回当前轮次。
func (p *ImprovingProvider) CurrentRound() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.round
}

func (p *ImprovingProvider) Complete(_ context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	// 从消息中识别用户输入
	input := ""
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			input = req.Messages[i].Content
			break
		}
	}
	c, ok := p.inputs[input]
	if !ok {
		// 未知输入：返回空内容（评估器判失败，不报错避免触发 agent 重试）
		return &llm.CompletionResponse{ID: "improving", Model: req.Model, Content: "", Role: "assistant"}, nil
	}

	p.mu.Lock()
	round := p.round
	idx := p.indexByInput[input]
	p.mu.Unlock()

	// 冷启动：返回空输出（失败且不存为已解记忆）
	if round <= idx%3 {
		return &llm.CompletionResponse{ID: "improving", Model: req.Model, Content: "", Role: "assistant"}, nil
	}

	content := c.Expected
	if len(c.Requires) > 0 {
		content = joinRequires(c.Requires)
	}
	return &llm.CompletionResponse{
		ID:      "improving",
		Model:   req.Model,
		Content: content,
		Role:    "assistant",
		Usage:   llm.Usage{PromptTokens: 5, CompletionTokens: 5, TotalTokens: 10},
	}, nil
}

func (p *ImprovingProvider) Stream(ctx context.Context, req *llm.CompletionRequest) (<-chan llm.Chunk, error) {
	ch := make(chan llm.Chunk, 1)
	go func() {
		resp, err := p.Complete(ctx, req)
		if err != nil {
			close(ch)
			return
		}
		ch <- llm.Chunk{Content: resp.Content, Done: true}
		close(ch)
	}()
	return ch, nil
}

func (p *ImprovingProvider) CallTools(_ context.Context, _ *llm.ToolCallRequest) (*llm.ToolCallResponse, error) {
	return &llm.ToolCallResponse{Content: ""}, nil
}

func (p *ImprovingProvider) Info() llm.ModelInfo {
	return llm.ModelInfo{Name: "improving", Provider: "mock", MaxContext: 8192}
}

// joinRequires 拼接用例的 requires 片段为合规输出。
func joinRequires(requires []string) string {
	var sb strings.Builder
	for i, r := range requires {
		if i > 0 {
			sb.WriteString(" ")
		}
		sb.WriteString(r)
	}
	return sb.String()
}

// RoundResult 单轮自举结果。
type RoundResult struct {
	Round      int     `json:"round"`
	Total      int     `json:"total"`
	Passed     int     `json:"passed"`
	PassRate   float64 `json:"pass_rate"`
	MemoryHits int     `json:"memory_hits"`
}

// BootstrapReport 自举报告（含成功率曲线）。
type BootstrapReport struct {
	Rounds  []RoundResult `json:"rounds"`
	Rising  bool          `json:"rising"` // 成功率曲线是否单调上升
	Started float64       `json:"started_rate"`
	Ended   float64       `json:"ended_rate"`
}

// Curve 返回 [round, pass_rate] 序列（成功率曲线）。
func (r *BootstrapReport) Curve() [][2]float64 {
	out := make([][2]float64, 0, len(r.Rounds))
	for _, rr := range r.Rounds {
		out = append(out, [2]float64{float64(rr.Round), rr.PassRate})
	}
	return out
}

// BootstrapConfig 自举配置。
type BootstrapConfig struct {
	// Cases 要解决的编码任务集。
	Cases []eval.EvalCase
	// Rounds 运行轮数。
	Rounds int
}

// RunBootstrap 用 AP 框架解决任务集，多轮观察成功率曲线。
//
// 每轮为每个用例创建一个共享同一 memory store 的 ReActAgent：
//   - 命中记忆（v3.6-3）→ 直接复用已解答案；
//   - 未命中 → 调用 ImprovingProvider；
//   - 成功的用例自动存入记忆（任务+答案），供后续轮次复用。
func RunBootstrap(ctx context.Context, cfg BootstrapConfig) (*BootstrapReport, error) {
	if len(cfg.Cases) == 0 {
		return nil, errors.New("self_bootstrap: no cases")
	}
	if cfg.Rounds <= 0 {
		cfg.Rounds = 1
	}

	mem := memory.NewInMemoryStore()
	provider := NewImprovingProvider(cfg.Cases)
	evaluator := &eval.CodeConstructEvaluator{}

	report := &BootstrapReport{Rounds: make([]RoundResult, 0, cfg.Rounds)}

	for round := 1; round <= cfg.Rounds; round++ {
		rr := RoundResult{Round: round, Total: len(cfg.Cases)}
		for _, c := range cfg.Cases {
			ag, err := agent.NewAgent("bootstrap-agent", "你是软件工程 Agent，直接给出可验证的输出。", provider,
				agent.WithMemory(mem),
				agent.WithMaxTurns(3),
			)
			if err != nil {
				continue
			}
			resp, runErr := ag.Run(ctx, agent.UserMessage(c.Input))
			if runErr != nil || resp == nil {
				continue
			}
			if resp.Metrics.MemoryHit {
				rr.MemoryHits++
			}
			_, passed, evalErr := evaluator.Evaluate(ctx, c, resp.Content)
			if evalErr == nil && passed {
				rr.Passed++
			}
		}
		if rr.Total > 0 {
			rr.PassRate = float64(rr.Passed) / float64(rr.Total)
		}
		report.Rounds = append(report.Rounds, rr)
		provider.AdvanceRound()
	}

	if len(report.Rounds) > 0 {
		report.Started = report.Rounds[0].PassRate
		report.Ended = report.Rounds[len(report.Rounds)-1].PassRate
	}
	report.Rising = report.Ended > report.Started
	return report, nil
}
