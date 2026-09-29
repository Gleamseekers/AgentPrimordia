package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/cmd/example/demo"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/metrics"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools"
)

// echoTool 用于测试工具调用标签的简单 echo 工具
type echoTool struct{}

func (e echoTool) Name() string        { return "echo" }
func (e echoTool) Description() string { return "Echo back the input" }
func (e echoTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`)
}
func (e echoTool) Execute(_ context.Context, args json.RawMessage) (*tools.Result, error) {
	var m map[string]string
	if err := json.Unmarshal(args, &m); err != nil {
		return tools.NewErrorResult(err.Error()), nil
	}
	return tools.NewResult(m["text"]), nil
}

// TestReActAgent_LabeledMetricsOutput 验证 Agent 运行后 metrics 输出包含 provider/model 和 agent_name 标签
func TestReActAgent_LabeledMetricsOutput(t *testing.T) {
	m := metrics.NewMetrics()

	agent, err := NewAgent("TestAgent", "你是测试Agent", demo.NewDemoLLM("测试回复").WithDelay(10*time.Millisecond), WithMaxTurns(2))
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}
	agent = agent.WithMetrics(m)

	_, err = agent.Run(context.Background(), UserMessage("测试"))
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	output := m.String()

	// 验证基础计数器存在
	if !strings.Contains(output, "ap_llm_total_calls ") {
		t.Error("missing ap_llm_total_calls")
	}
	if !strings.Contains(output, "ap_total_turns ") {
		t.Error("missing ap_total_turns")
	}

	// 验证标签维度：provider + model
	if !strings.Contains(output, `ap_llm_calls_by_provider{provider="demo",model="demo-model"}`) {
		t.Error("missing labeled LLM call metrics with provider/model")
	}
	// 验证标签维度：agent_name
	if !strings.Contains(output, `ap_turns{agent_name="TestAgent"}`) {
		t.Error("missing labeled turn metrics with agent_name")
	}

	t.Logf("✅ Labeled metrics output:\n%s", output)
}

// TestReActAgent_LabeledMetricsWithToolCall 验证工具调用后输出 tool_name 标签
func TestReActAgent_LabeledMetricsWithToolCall(t *testing.T) {
	m := metrics.NewMetrics()

	registry := tools.NewRegistry()
	_ = registry.Register(echoTool{})

	// DemoLLM 先返回 tool call（通过 CallTools），再返回纯文本
	llm := demo.NewDemoLLM(`echo完成`).WithToolCalls(
		llm.FunctionCall{Name: "echo", Arguments: `{"text":"hello"}`},
	)

	agent, err := NewAgent("ToolAgent", "你使用工具", llm, WithMaxTurns(3), WithToolkit(registry))
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}
	agent = agent.WithMetrics(m)

	_, err = agent.Run(context.Background(), UserMessage("用 echo 工具说 hello"))
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	output := m.String()

	// 验证工具标签维度
	if !strings.Contains(output, `ap_tool_calls{tool_name="echo"}`) {
		t.Error("missing labeled tool call metrics with tool_name")
	}
	if !strings.Contains(output, "ap_tool_total_calls ") {
		t.Error("missing ap_tool_total_calls")
	}

	t.Logf("✅ Tool labeled metrics output:\n%s", output)
}

// TestReActAgent_LabeledMetricsNilRecorder 验证无 metrics recorder 时不 panic
func TestReActAgent_LabeledMetricsNilRecorder(t *testing.T) {
	agent, err := NewAgent("NoMetricsAgent", "测试", demo.NewDemoLLM("ok"), WithMaxTurns(1))
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}
	// 不注入 Metrics

	_, err = agent.Run(context.Background(), UserMessage("测试"))
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	t.Logf("✅ No panic with nil metrics recorder")
}

// TestReActAgent_LabeledMetricsOnCapabilityAgent 验证通过 CapabilityAgent 注入的标签维度
func TestReActAgent_LabeledMetricsOnCapabilityAgent(t *testing.T) {
	m := metrics.NewMetrics()

	capAgent, err := NewAgent("CapAgent", "测试", demo.NewDemoLLM("ok"), WithMaxTurns(1))
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}
	capAgent = capAgent.WithMetrics(m)

	_, err = capAgent.Run(context.Background(), UserMessage("测试"))
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	output := m.String()
	if !strings.Contains(output, `ap_turns{agent_name="CapAgent"}`) {
		t.Error("missing labeled turn via CapabilityAgent")
	}
	if !strings.Contains(output, `ap_llm_calls_by_provider{provider="demo",model="demo-model"}`) {
		t.Error("missing labeled LLM call via CapabilityAgent")
	}

	t.Logf("✅ CapabilityAgent labeled metrics:\n%s", output)
}
