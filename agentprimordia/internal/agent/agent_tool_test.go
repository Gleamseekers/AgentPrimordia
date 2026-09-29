package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools"
)

type mockSubAgent struct {
	name    string
	resp    *Response
	respErr error
	called  bool
	lastMsg Message
}

func (m *mockSubAgent) Run(ctx context.Context, input Message) (*Response, error) {
	m.called = true
	m.lastMsg = input
	if m.respErr != nil {
		return nil, m.respErr
	}
	return m.resp, nil
}

func (m *mockSubAgent) StreamRun(ctx context.Context, input Message) (<-chan StreamEvent, error) {
	ch := make(chan StreamEvent, 1)
	go func() {
		defer close(ch)
		if m.resp != nil {
			ch <- StreamEvent{Type: StreamEventComplete, Content: m.resp.Content}
		}
	}()
	return ch, nil
}

func (m *mockSubAgent) Stop() {}

func (m *mockSubAgent) Stats() AgentStats {
	return AgentStats{Status: StatusIdle}
}

func (m *mockSubAgent) Name() string { return m.name }

func TestAgentTool_ImplementsTool(t *testing.T) {
	sub := &mockSubAgent{name: "sub-agent"}
	agentTool := NewAgentTool(sub)

	var _ tools.Tool = agentTool
}

func TestAgentTool_Name(t *testing.T) {
	sub := &mockSubAgent{name: "research-agent"}
	agentTool := NewAgentTool(sub)

	if agentTool.Name() != "agent_research-agent" {
		t.Errorf("Name() = %q, want %q", agentTool.Name(), "agent_research-agent")
	}
}

func TestAgentTool_NameWithCustomPrefix(t *testing.T) {
	sub := &mockSubAgent{name: "research"}
	agentTool := NewAgentTool(sub, AgentToolConfig{Description: "研究助手"})

	if agentTool.Name() != "agent_research" {
		t.Errorf("Name() = %q, want %q", agentTool.Name(), "agent_research")
	}
}

func TestAgentTool_Description(t *testing.T) {
	sub := &mockSubAgent{name: "sub"}
	agentTool := NewAgentTool(sub, AgentToolConfig{Description: "自定义描述"})

	if agentTool.Description() != "自定义描述" {
		t.Errorf("Description() = %q, want %q", agentTool.Description(), "自定义描述")
	}
}

func TestAgentTool_DescriptionDefault(t *testing.T) {
	sub := &mockSubAgent{name: "sub"}
	agentTool := NewAgentTool(sub)

	desc := agentTool.Description()
	if desc == "" {
		t.Error("Description() should not be empty when no custom description provided")
	}
}

func TestAgentTool_Parameters(t *testing.T) {
	sub := &mockSubAgent{name: "sub"}
	agentTool := NewAgentTool(sub)

	params := agentTool.Parameters()
	if len(params) == 0 {
		t.Fatal("Parameters() should not be empty")
	}

	var schema map[string]any
	if err := json.Unmarshal(params, &schema); err != nil {
		t.Fatalf("Parameters() should be valid JSON: %v", err)
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("Schema should have 'properties' field")
	}
	if _, hasInput := props["input"]; !hasInput {
		t.Error("Schema properties should contain 'input' field")
	}
}

func TestAgentTool_Execute(t *testing.T) {
	sub := &mockSubAgent{
		name: "math-agent",
		resp: &Response{Content: "42"},
	}
	agentTool := NewAgentTool(sub)

	args := json.RawMessage(`{"input": "2+2*20"}`)
	result, err := agentTool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Errorf("IsError should be false, got error: %s", result.Content)
	}
	if result.Content != "42" {
		t.Errorf("Content = %q, want %q", result.Content, "42")
	}
	if !sub.called {
		t.Error("Sub-agent should have been called")
	}
	if sub.lastMsg.Content != "2+2*20" {
		t.Errorf("Sub-agent input = %q, want %q", sub.lastMsg.Content, "2+2*20")
	}
}

func TestAgentTool_ExecuteError(t *testing.T) {
	sub := &mockSubAgent{
		name:    "failing-agent",
		respErr: errors.New("agent crashed"),
	}
	agentTool := NewAgentTool(sub)

	args := json.RawMessage(`{"input": "test"}`)
	result, err := agentTool.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !result.IsError {
		t.Error("IsError should be true for failed agent execution")
	}
}

func TestAgentTool_ExecuteInvalidArgs(t *testing.T) {
	sub := &mockSubAgent{name: "sub"}
	agentTool := NewAgentTool(sub)

	args := json.RawMessage(`not valid json`)
	result, err := agentTool.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for invalid JSON args")
	}
	if !result.IsError {
		t.Error("IsError should be true for invalid args")
	}
}

func TestAgentTool_ExecuteMissingInput(t *testing.T) {
	sub := &mockSubAgent{name: "sub"}
	agentTool := NewAgentTool(sub)

	args := json.RawMessage(`{"query": "hello"}`)
	result, err := agentTool.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for missing 'input' field")
	}
	if !result.IsError {
		t.Error("IsError should be true for missing input")
	}
}

func TestAgentTool_RegistryIntegration(t *testing.T) {
	sub := &mockSubAgent{
		name: "search-agent",
		resp: &Response{Content: "found 3 results"},
	}
	agentTool := NewAgentTool(sub)

	registry := tools.NewRegistry()
	_ = registry.Register(agentTool)

	retrieved, exists := registry.Get("agent_search-agent")
	if !exists {
		t.Fatal("AgentTool should be registered in Registry")
	}

	result, err := retrieved.Execute(context.Background(), json.RawMessage(`{"input": "golang"}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.Content != "found 3 results" {
		t.Errorf("Content = %q, want %q", result.Content, "found 3 results")
	}
}

func TestAgentTool_WithContextPassing(t *testing.T) {
	sub := &mockSubAgent{
		name: "ctx-agent",
		resp: &Response{Content: "done"},
	}
	agentTool := NewAgentTool(sub, AgentToolConfig{PassContext: true})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	args := json.RawMessage(`{"input": "hello"}`)
	result, err := agentTool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Errorf("IsError should be false: %s", result.Content)
	}
}

func TestAgentTool_CustomParamSchema(t *testing.T) {
	customSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"input":    map[string]any{"type": "string", "description": "查询文本"},
			"language": map[string]any{"type": "string", "description": "目标语言"},
		},
		"required": []any{"input"},
	}
	schemaBytes, _ := json.Marshal(customSchema)

	sub := &mockSubAgent{name: "translator"}
	agentTool := NewAgentTool(sub, AgentToolConfig{ParamSchema: schemaBytes})

	params := agentTool.Parameters()
	var schema map[string]any
	if err := json.Unmarshal(params, &schema); err != nil {
		t.Fatalf("Parameters() should be valid JSON: %v", err)
	}

	props, _ := schema["properties"].(map[string]any)
	if _, hasLang := props["language"]; !hasLang {
		t.Error("Custom schema should contain 'language' field")
	}
}
