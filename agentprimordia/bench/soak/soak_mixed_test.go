// Package soak — v4.2-1 24h+ Soak（真实接线系统混合流量 + chaos 注入）
//
// 验收标准：无泄漏、无 panic、恢复率 ≥99%。
// 混合流量：QA 轮与工具轮交替（filesystem 真实工具调用）；
// chaos 期间注入：测试进行 1/3 处武装故障窗口（20% LLM 调用失败），
// 2/3 处解除，由 ResilientProvider 重试恢复，量化恢复率。
//
// CI 冒烟：默认 15s（SOAK_DURATION 覆盖）；24h 正式运行：
//
//	SOAK_DURATION=24h SOAK_RPS=10 go test -run TestSoak_MixedTraffic -v ./bench/soak/
package soak

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm/soak"
	ap "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg"
)

// mixedProvider 确定性混合流量 Provider：Complete 给结论（QA 轮收尾），
// CallTools 返回一次 filesystem 写入调用（工具轮）。
type mixedProvider struct {
	seq atomic.Int64
}

func (m *mixedProvider) Complete(_ context.Context, _ *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return &llm.CompletionResponse{
		ID: "soak-mixed", Model: "soak", Content: "任务已完成：总结就绪/文件已写入。", Role: "assistant",
		Usage: llm.Usage{PromptTokens: 20, CompletionTokens: 30, TotalTokens: 50},
	}, nil
}

func (m *mixedProvider) Stream(ctx context.Context, req *llm.CompletionRequest) (<-chan llm.Chunk, error) {
	ch := make(chan llm.Chunk, 1)
	resp, err := m.Complete(ctx, req)
	if err != nil {
		close(ch)
		return ch, err
	}
	ch <- llm.Chunk{Content: resp.Content, Done: true}
	close(ch)
	return ch, nil
}

func (m *mixedProvider) CallTools(_ context.Context, _ *llm.ToolCallRequest) (*llm.ToolCallResponse, error) {
	// 交替：偶数调用返回一次 filesystem 写入（工具轮），奇数调用返回空（收尾结论轮）
	if m.seq.Add(1)%2 == 0 {
		return &llm.ToolCallResponse{
			ToolCalls: []llm.FunctionCall{{
				ID:   "call_write",
				Name: "filesystem",
				Arguments: `{"action":"write","path":"hello.txt","content":"soak-` +
					strconv.FormatInt(m.seq.Load(), 10) + `"}`,
			}},
		}, nil
	}
	return &llm.ToolCallResponse{ToolCalls: nil}, nil
}

func (m *mixedProvider) Info() llm.ModelInfo {
	return llm.ModelInfo{Name: "soak-mixed", Provider: "mock", MaxContext: 8192, SupportsTools: true}
}

// windowFaultProvider 时间窗口故障注入：armed 期间按 20% 周期注入 LLM 调用失败。
type windowFaultProvider struct {
	base   llm.Provider
	armed  atomic.Bool
	seq    atomic.Int64
	faults atomic.Int64
}

// maybeFault 故障窗口判定：armed 期间按 20% 周期注入失败。
func (f *windowFaultProvider) maybeFault() error {
	if f.armed.Load() && f.seq.Add(1)%5 == 0 {
		f.faults.Add(1)
		return fmt.Errorf("chaos: injected soak fault")
	}
	return nil
}

func (f *windowFaultProvider) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if err := f.maybeFault(); err != nil {
		return nil, err
	}
	return f.base.Complete(ctx, req)
}

func (f *windowFaultProvider) Stream(ctx context.Context, req *llm.CompletionRequest) (<-chan llm.Chunk, error) {
	if err := f.maybeFault(); err != nil {
		ch := make(chan llm.Chunk, 1)
		close(ch)
		return ch, nil
	}
	return f.base.Stream(ctx, req)
}

func (f *windowFaultProvider) CallTools(ctx context.Context, req *llm.ToolCallRequest) (*llm.ToolCallResponse, error) {
	if err := f.maybeFault(); err != nil {
		return nil, err
	}
	return f.base.CallTools(ctx, req)
}

func (f *windowFaultProvider) Info() llm.ModelInfo { return f.base.Info() }

// TestSoak_MixedTraffic 混合流量 + chaos 注入 Soak：
// 无 panic、无 goroutine 泄漏、故障窗口内恢复率 ≥99%。
func TestSoak_MixedTraffic(t *testing.T) {
	duration := envDuration("SOAK_DURATION", 15*time.Second)
	rps := envInt("SOAK_RPS", 5)
	if testing.Short() {
		duration = 5 * time.Second
	}

	// 装配：混合 Provider → 故障窗口包装 → ResilientProvider（重试恢复）
	fault := &windowFaultProvider{base: &mixedProvider{}}
	resilient, err := llm.NewResilientProvider(fault, llm.ResilientConfig{
		MaxRetries:   3,
		RetryBackoff: 10 * time.Millisecond,
		MaxBackoff:   50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewResilientProvider: %v", err)
	}

	workdir := t.TempDir()
	registry := ap.NewToolRegistry()
	fsTool, err := ap.NewFileSystem(workdir)
	if err != nil {
		t.Fatalf("NewFileSystem: %v", err)
	}
	if err := registry.Register(fsTool); err != nil {
		t.Fatalf("Register: %v", err)
	}
	agent, err := ap.NewAgent("soak-agent", "你是 Soak 测试 Agent。", resilient,
		ap.WithMaxTurns(4), ap.WithToolkit(registry))
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	// 故障窗口：1/3 处武装，2/3 处解除
	windowStart := duration / 3
	windowLen := duration / 3
	time.AfterFunc(windowStart, func() { fault.armed.Store(true) })
	time.AfterFunc(windowStart+windowLen, func() { fault.armed.Store(false) })

	// 混合流量请求函数：QA 轮与工具轮交替
	var reqSeq atomic.Int64
	var agentErrs atomic.Int64
	before := runtime.NumGoroutine()

	runner := soak.NewRunner(soak.RunnerConfig{
		Duration:         duration,
		Pattern:          soak.ConstantPattern(rps),
		SamplingInterval: 2 * time.Second,
		RequestFn: func(ctx context.Context) (*soak.Response, error) {
			start := time.Now()
			var prompt string
			if reqSeq.Add(1)%2 == 0 {
				prompt = "请写入文件 hello.txt（工具轮）"
			} else {
				prompt = "请总结 A、B、C 三点（QA 轮）"
			}
			resp, runErr := agent.Run(ctx, ap.UserMessage(prompt))
			ok := runErr == nil && resp != nil && resp.Error == nil
			if !ok {
				agentErrs.Add(1)
			}
			return &soak.Response{Latency: time.Since(start), Success: ok, Error: runErr}, nil
		},
	})

	result := runner.Run(context.Background())
	t.Logf("Soak 结果: 请求=%d 错误=%d 错误率=%.4f 平均延迟=%s",
		result.TotalRequests, result.TotalErrors, result.ErrorRate(), result.AvgLatency())
	t.Logf("注入故障=%d 请求层失败=%d 恢复率=%.4f",
		fault.faults.Load(), agentErrs.Load(), recoveryRate(fault.faults.Load(), agentErrs.Load()))

	// 验收 1：无 panic（测试未崩即满足）
	// 验收 2：chaos 真实注入（窗口内必须有故障发生）
	if fault.faults.Load() == 0 {
		t.Fatal("故障窗口内未注入任何故障（chaos 未生效）")
	}
	// 验收 3：恢复率 ≥99%（注入故障经重试恢复，请求层失败趋近 0）
	if rate := recoveryRate(fault.faults.Load(), agentErrs.Load()); rate < 0.99 {
		t.Fatalf("恢复率 = %.4f, want ≥0.99", rate)
	}
	// 验收 4：无 goroutine 泄漏（settle 后增量有界）
	time.Sleep(2 * time.Second)
	after := runtime.NumGoroutine()
	if leaked := after - before; leaked > 8 {
		t.Fatalf("goroutine 泄漏: before=%d after=%d (delta=%d)", before, after, leaked)
	}
}

// recoveryRate 恢复率 = 1 - 请求层失败 / 注入故障。
func recoveryRate(faults, agentErrs int64) float64 {
	if faults <= 0 {
		return 1.0
	}
	return 1 - float64(agentErrs)/float64(faults)
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
