// harness_test.go — v3.8-2 Pool × harness 多任务并发执行
// 验证：Pool 并发执行 harness 基准任务，吞吐随并发度近似线性扩展。
package pool

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/eval"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm"
)

// harnessDelayProvider 带延迟的 mock Provider：返回 requires 拼接内容（任务成功），
// 并模拟真实 LLM 耗时以便测量吞吐。
type harnessDelayProvider struct {
	delay time.Duration
}

func (p *harnessDelayProvider) Complete(_ context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	time.Sleep(p.delay)
	content := "func main() {}"
	if len(req.Messages) > 0 {
		content = "completed: " + req.Messages[len(req.Messages)-1].Content
	}
	return &llm.CompletionResponse{ID: "h", Model: req.Model, Content: content, Role: "assistant",
		Usage: llm.Usage{PromptTokens: 5, CompletionTokens: 5, TotalTokens: 10}}, nil
}

func (p *harnessDelayProvider) Stream(ctx context.Context, req *llm.CompletionRequest) (<-chan llm.Chunk, error) {
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

func (p *harnessDelayProvider) CallTools(_ context.Context, _ *llm.ToolCallRequest) (*llm.ToolCallResponse, error) {
	return &llm.ToolCallResponse{Content: ""}, nil
}

func (p *harnessDelayProvider) Info() llm.ModelInfo {
	return llm.ModelInfo{Name: "harness", Provider: "mock", MaxContext: 8192}
}

// runHarnessBatch 用给定并发度跑一批 harness 任务，返回耗时与吞吐。
func runHarnessBatch(t *testing.T, concurrency, taskCount int, delay time.Duration) (throughput float64, passed int) {
	t.Helper()
	p := NewPool(PoolConfig{MaxConcurrency: concurrency, Timeout: 60 * time.Second})
	defer p.Close()

	p.SetModel(&harnessDelayProvider{delay: delay})
	p.SetAgentFactory(func(cfg AgentFactoryConfig) agent.Agent {
		name := cfg.Name
		if name == "" {
			name = "harness-agent"
		}
		ag, err := agent.NewAgent(name, "你是软件工程 Agent", &harnessDelayProvider{delay: delay}, agent.WithMaxTurns(2))
		if err != nil {
			panic(err)
		}
		return ag
	})

	tasks := make([]TaskConfig, 0, taskCount)
	cases := eval.MustBenchmarkCases()
	for i := 0; i < taskCount; i++ {
		c := cases[i%len(cases)]
		tasks = append(tasks, TaskConfig{
			ID:       c.ID + "_" + itoa(i),
			Title:    "task-" + itoa(i),
			Prompt:   c.Input,
			MaxTurns: 2,
		})
	}

	start := time.Now()
	results, err := p.Dispatch(context.Background(), tasks)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Dispatch failed: %v", err)
	}
	for _, r := range results {
		if r.Response != nil && r.Error == nil && strings.HasPrefix(r.Response.Content, "completed:") {
			passed++
		}
	}
	return float64(taskCount) / elapsed.Seconds(), passed
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// TestPoolHarness_ConcurrentThroughputScales 验证并发吞吐近似线性扩展。
func TestPoolHarness_ConcurrentThroughputScales(t *testing.T) {
	const (
		taskCount = 12
		delay     = 30 * time.Millisecond
	)

	seq, passedSeq := runHarnessBatch(t, 1, taskCount, delay)
	if passedSeq != taskCount {
		t.Fatalf("串行通过数 = %d/%d", passedSeq, taskCount)
	}

	conc, passedConc := runHarnessBatch(t, 4, taskCount, delay)
	if passedConc != taskCount {
		t.Fatalf("并发通过数 = %d/%d", passedConc, taskCount)
	}

	// 线性扩展：并发 4 应带来接近 4× 的吞吐（留调度开销余量，要求 ≥2.5×）
	speedup := conc / seq
	if speedup < 2.5 {
		t.Errorf("并发吞吐扩展不足：concurrency=4 speedup=%.2f（应 ≥2.5，线性扩展）", speedup)
	}
	t.Logf("吞吐: seq=%.1f tasks/s, conc=%.1f tasks/s, speedup=%.2f", seq, conc, speedup)
}

// TestPoolHarness_ParallelCorrectness 验证并发执行结果正确性（不丢失任务）。
func TestPoolHarness_ParallelCorrectness(t *testing.T) {
	_, passed := runHarnessBatch(t, 8, 16, 5*time.Millisecond)
	if passed != 16 {
		t.Errorf("并发 8 执行 16 个任务应全部成功, got %d/16", passed)
	}
}
