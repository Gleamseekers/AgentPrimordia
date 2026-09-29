// swarm_test.go — v3.8-1 多 Agent 分工规模测试
// 验证：大任务拆分为子任务交给专业 Agent 后，任务规模 ×N 成功率不降，
// 且优于单 Agent 泛化执行。
package multi_agent

import (
	"context"
	"strings"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
)

// contentProvider 按输入关键词返回内容的 mock Provider。
type contentProvider struct {
	content func(input string) string
}

func (p *contentProvider) Complete(_ context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	input := ""
	if len(req.Messages) > 0 {
		input = req.Messages[len(req.Messages)-1].Content
	}
	return &llm.CompletionResponse{
		ID:      "swarm-mock",
		Model:   req.Model,
		Content: p.content(input),
		Role:    "assistant",
		Usage:   llm.Usage{PromptTokens: 5, CompletionTokens: 5, TotalTokens: 10},
	}, nil
}

func (p *contentProvider) Stream(ctx context.Context, req *llm.CompletionRequest) (<-chan llm.Chunk, error) {
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

func (p *contentProvider) CallTools(_ context.Context, _ *llm.ToolCallRequest) (*llm.ToolCallResponse, error) {
	return &llm.ToolCallResponse{Content: ""}, nil
}

func (p *contentProvider) Info() llm.ModelInfo {
	return llm.ModelInfo{Name: "swarm-mock", Provider: "mock", MaxContext: 8192}
}

func newContentAgent(name string, content func(string) string) agent.Agent {
	ag, err := agent.NewAgent(name, "你是一名专业 Agent", &contentProvider{content: content}, agent.WithMaxTurns(2))
	if err != nil {
		panic(err)
	}
	return ag
}

// buildSwarm 构建 implementer/reviewer/tester 三个专业 Agent + 泛化兜底。
func buildSwarm() *Swarm {
	implementer := newContentAgent("implementer", func(input string) string {
		if strings.Contains(input, "编写") {
			return "OK 代码已编写"
		}
		return "不支持"
	})
	reviewer := newContentAgent("reviewer", func(input string) string {
		if strings.Contains(input, "审查") {
			return "OK 代码已审查"
		}
		return "不支持"
	})
	tester := newContentAgent("tester", func(input string) string {
		if strings.Contains(input, "测试") {
			return "OK 测试已编写"
		}
		return "不支持"
	})
	// 泛化兜底：仅能处理"基础"任务（模拟单 Agent 能力有限）
	generalist := newContentAgent("generalist", func(input string) string {
		if strings.Contains(input, "基础") {
			return "OK 基础任务完成"
		}
		return ""
	})

	return NewSwarm([]Specialist{
		{Name: "implementer", Keywords: []string{"编写"}, Agent: implementer},
		{Name: "reviewer", Keywords: []string{"审查"}, Agent: reviewer},
		{Name: "tester", Keywords: []string{"测试"}, Agent: tester},
	}, generalist)
}

// tasksOfSize 生成 N 个子任务（混合三专业域）。
func tasksOfSize(n int) []string {
	tasks := make([]string, 0, n)
	for i := 0; i < n; i++ {
		switch i % 3 {
		case 0:
			tasks = append(tasks, "编写第"+strings.Repeat("I", i+1)+"个模块代码")
		case 1:
			tasks = append(tasks, "审查第"+strings.Repeat("V", i+1)+"次代码变更")
		default:
			tasks = append(tasks, "测试第"+strings.Repeat("T", i+1)+"个用例")
		}
	}
	return tasks
}

func passCheck(content string) bool {
	return strings.HasPrefix(content, "OK")
}

// TestSwarm_ScaleDoesNotDegrade 验证规模 ×N 成功率不降。
func TestSwarm_ScaleDoesNotDegrade(t *testing.T) {
	swarm := buildSwarm()
	ctx := context.Background()

	var prevRate float64
	for _, size := range []int{1, 2, 4, 8} {
		tasks := tasksOfSize(size)
		res := swarm.ExecuteSequential(ctx, tasks, passCheck)
		if res.PassRate < 0.9 {
			t.Errorf("规模 %d 成功率 = %f, 应 ≥0.9（任务规模×N 成功率不降）", size, res.PassRate)
		}
		if prevRate > 0 && res.PassRate < prevRate {
			t.Errorf("规模 %d 成功率 %f < 上一规模 %f", size, res.PassRate, prevRate)
		}
		prevRate = res.PassRate
	}
}

// TestSwarm_BeatsSingleAgent 验证分工优于单 Agent 泛化执行。
func TestSwarm_BeatsSingleAgent(t *testing.T) {
	ctx := context.Background()
	tasks := tasksOfSize(4) // 4 个跨专业域子任务

	// 单 Agent：仅能处理"基础"任务，其余失败
	single := newContentAgent("single", func(input string) string {
		if strings.Contains(input, "基础") {
			return "OK"
		}
		return ""
	})
	singleRes := &SwarmResult{Total: len(tasks)}
	for _, t := range tasks {
		resp, err := single.Run(ctx, agent.Message{Role: agent.RoleUser, Content: t})
		if err == nil && resp != nil && passCheck(resp.Content) {
			singleRes.Passed++
		}
	}
	singleRes.Failed = singleRes.Total - singleRes.Passed
	singleRes.PassRate = float64(singleRes.Passed) / float64(singleRes.Total)

	// Swarm：每个子任务路由到专业 Agent，全部成功
	swarmRes := buildSwarm().ExecuteSequential(ctx, tasks, passCheck)

	if swarmRes.PassRate <= singleRes.PassRate {
		t.Errorf("Swarm 成功率 %f 应高于单 Agent %f", swarmRes.PassRate, singleRes.PassRate)
	}
	if swarmRes.PassRate != 1.0 {
		t.Errorf("Swarm 全专业域应全过, got %f", swarmRes.PassRate)
	}
	// 分工：至少 3 个 Agent 都贡献了通过
	if len(swarmRes.ByAgent) < 3 {
		t.Errorf("应至少 3 个专业 Agent 参与, got %d", len(swarmRes.ByAgent))
	}
}

// TestSwarm_Concurrent 验证并发执行安全（-race 覆盖）。
func TestSwarm_Concurrent(t *testing.T) {
	swarm := buildSwarm()
	res := swarm.Execute(context.Background(), tasksOfSize(8), passCheck)
	if res.PassRate < 0.9 {
		t.Errorf("并发成功率 = %f, 应 ≥0.9", res.PassRate)
	}
}

// TestSwarm_GeneralistFallback 无专业命中时回退泛化 Agent。
func TestSwarm_GeneralistFallback(t *testing.T) {
	swarm := buildSwarm()
	res := swarm.ExecuteSequential(context.Background(), []string{"基础日常任务"}, passCheck)
	if res.Passed != 1 {
		t.Errorf("基础任务应回退泛化 Agent 成功, got %d/%d", res.Passed, res.Total)
	}
}
