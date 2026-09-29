package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/hitl"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/tools"
)

// noopTool 用于覆盖 tool 调用路径的占位工具
type noopTool struct{}

func (n *noopTool) Name() string        { return "noop_tool" }
func (n *noopTool) Description() string { return "noop" }
func (n *noopTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (n *noopTool) Execute(_ context.Context, _ json.RawMessage) (*tools.Result, error) {
	return tools.NewResult("noop done"), nil
}

// TestAgent_BudgetExceeded 预算超限路径：第二轮触发 ErrBudgetExceeded
// （覆盖 checkBudgetExceeded + recordUsage 完整链路）
func TestAgent_BudgetExceeded(t *testing.T) {
	mock := llm.NewMockLLM(t)
	mock.WithToolResponse([]llm.FunctionCall{{ID: "c1", Name: "noop_tool", Arguments: "{}"}})
	mock.WithResponse("done")

	// 单价巨大 + 预算极小：第一轮 usage 后必然超限
	// （MockLLM.Info() 的模型名为 "mock-model"）
	pricing := map[string]llm.ModelPricing{
		"mock-model": {PromptPricePer1M: 1e9, CompletionPricePer1M: 1e9},
	}
	ct := agent.NewCostTracker(pricing, &agent.BudgetConfig{MaxTotalCostUSD: 1e-9})

	reg := tools.NewRegistry()
	_ = reg.Register(&noopTool{})

	agt, err := agent.NewAgent("budget-test", "you are helpful", mock,
		agent.WithMaxTurns(5), agent.WithCostTracker(ct), agent.WithToolkit(reg))
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err = agt.Run(ctx, agent.UserMessage("do it"))
	if !errors.Is(err, agent.ErrBudgetExceeded) {
		t.Fatalf("期望 ErrBudgetExceeded，实际: %v", err)
	}

	// recordUsage 链路：成本应已被记录
	if len(ct.Records()) == 0 {
		t.Fatal("CostTracker 应有记录（recordUsage 未生效？）")
	}
	t.Logf("✅ 预算超限路径：%d 条成本记录", len(ct.Records()))
}

// TestAgent_HITLReject HITL 拒绝路径：人类拒绝后工具被跳过，Agent 继续完成
func TestAgent_HITLReject(t *testing.T) {
	mock := llm.NewMockLLM(t)
	mock.WithToolResponse([]llm.FunctionCall{{ID: "c1", Name: "noop_tool", Arguments: "{}"}})
	mock.WithResponse("skipped by human")

	// 人类输入通道预填"拒绝"
	humanChan := make(chan *hitl.HumanResponse, 1)
	humanChan <- &hitl.HumanResponse{Approved: false}
	cfg := hitl.HITLConfig{
		InterruptPoints: []hitl.InterruptPoint{{Type: hitl.InterruptToolConfirm, ToolName: ""}}, // 所有工具需确认
		HumanInputChan:  humanChan,
	}

	reg := tools.NewRegistry()
	_ = reg.Register(&noopTool{})

	agt, err := agent.NewAgent("hitl-test", "you are helpful", mock,
		agent.WithMaxTurns(5), agent.WithHITL(&cfg), agent.WithToolkit(reg))
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := agt.Run(ctx, agent.UserMessage("do it"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp.Content != "skipped by human" {
		t.Errorf("最终回答 = %q, want 人类拒绝后的回答", resp.Content)
	}
	t.Logf("✅ HITL 拒绝路径：工具被跳过，最终回答=%q", resp.Content)
}
