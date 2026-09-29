package agent

import (
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm"
)

// TestReActAgent_WithPromptTemplate 验证 PromptTemplate 与 ReActAgent 集成
// （从 prompt 子包测试移回：该测试依赖 NewAgent，属 agent 包集成测试）。
func TestReActAgent_WithPromptTemplate(t *testing.T) {
	tmpl := DefaultSystemPrompt().WithVar("AgentName", "TestAgent").WithScopeRules([]string{"/src/"})

	mockLLM := llm.NewMockLLM(t).WithResponse("done")

	agt, err := NewAgent("TestAgent", "", mockLLM, WithPromptTemplate(tmpl), WithMaxTurns(1))
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	resp, err := agt.Run(t.Context(), UserMessage("hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
}
