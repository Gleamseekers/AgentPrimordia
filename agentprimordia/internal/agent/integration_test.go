//go:build integration
// +build integration

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools"
)

// ===== Shared helpers =====

func getAPIKey(t *testing.T) string {
	t.Helper()
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		t.Skip("OPENAI_API_KEY not set, skipping integration test")
	}
	return key
}

func getBaseURL() string {
	if url := os.Getenv("OPENAI_BASE_URL"); url != "" {
		return url
	}
	return ""
}

func newTestProvider(t *testing.T) llm.Provider {
	t.Helper()
	apiKey := getAPIKey(t)
	provider, err := llm.NewOpenAIProvider(llm.Config{
		APIKey:  apiKey,
		BaseURL: getBaseURL(),
		Model:   "gpt-4o-mini",
	})
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
	}
	return provider
}

// calculatorTool is a simple tool that adds two numbers for integration testing.
type calculatorTool struct{}

func (calculatorTool) Name() string {
	return "calculator"
}

func (calculatorTool) Description() string {
	return "Add two numbers together. Provide a and b as parameters."
}

func (calculatorTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"a": {"type": "number", "description": "First number"},
			"b": {"type": "number", "description": "Second number"}
		},
		"required": ["a", "b"]
	}`)
}

func (calculatorTool) Execute(_ context.Context, args json.RawMessage) (*tools.Result, error) {
	var params struct {
		A float64 `json:"a"`
		B float64 `json:"b"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return tools.NewErrorResult(fmt.Sprintf("invalid arguments: %v", err)), nil
	}
	result := params.A + params.B
	return tools.NewResult(fmt.Sprintf("%.0f", result)), nil
}

// ===== Integration tests =====

func TestIntegration_ReActAgent_SimpleCompletion(t *testing.T) {
	provider := newTestProvider(t)

	agent, err := NewAgent("test-simple", "You are a helpful assistant. Follow instructions exactly.", provider,
		WithMaxTurns(3),
		WithTemperature(0),
	)
	if err != nil {
		t.Fatalf("NewAgent() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := agent.Run(ctx, UserMessage("Say 'hello' and nothing else."))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if resp.Content == "" {
		t.Error("expected non-empty response content")
	}

	t.Logf("Response: %s", resp.Content)
	t.Logf("Metrics: turns=%d tools=%d duration=%v",
		resp.Metrics.TotalTurns, resp.Metrics.TotalTools, resp.Metrics.Duration)

	// A simple "say hello" should complete in 1 turn without tool calls
	if resp.Metrics.TotalTools > 0 {
		t.Logf("Note: agent made %d tool calls (expected 0 for simple completion)", resp.Metrics.TotalTools)
	}
}

func TestIntegration_ReActAgent_ToolCall(t *testing.T) {
	provider := newTestProvider(t)

	registry := tools.NewRegistry()
	if err := registry.Register(calculatorTool{}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	agent, err := NewAgent("test-tool", "You are a helpful assistant. Use the calculator tool when asked to perform arithmetic.", provider,
		WithMaxTurns(5),
		WithTemperature(0),
		WithToolkit(registry),
	)
	if err != nil {
		t.Fatalf("NewAgent() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := agent.Run(ctx, UserMessage("What is 2 plus 3? Use the calculator tool to compute the answer."))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if resp.Content == "" {
		t.Error("expected non-empty response content")
	}

	t.Logf("Response: %s", resp.Content)
	t.Logf("Metrics: turns=%d tools=%d duration=%v",
		resp.Metrics.TotalTurns, resp.Metrics.TotalTools, resp.Metrics.Duration)

	// The agent should have used the calculator tool
	stats := agent.Stats()
	if _, ok := stats.ToolsCalled["calculator"]; !ok {
		t.Error("expected calculator tool to be called")
	} else {
		t.Logf("Calculator tool called %d time(s)", stats.ToolsCalled["calculator"])
	}

	// The response should mention the result 5
	if !strings.Contains(resp.Content, "5") {
		t.Errorf("expected response to contain '5', got: %s", resp.Content)
	}
}

func TestIntegration_ResilientProvider(t *testing.T) {
	provider := newTestProvider(t)

	// Create a ResilientProvider with real OpenAI as primary and mock as fallback.
	// Since the primary is healthy, all calls should go through OpenAI.
	mockFallback := llm.NewMockLLM(t).WithResponse("fallback response")

	resilient, err := llm.NewResilientProvider(provider, llm.DefaultResilientConfig())
	if err != nil {
		t.Fatalf("NewResilientProvider() error = %v", err)
	}
	resilient.AddFallback(mockFallback)

	agent, err := NewAgent("test-resilient", "You are a helpful assistant. Follow instructions exactly.", resilient,
		WithMaxTurns(3),
		WithTemperature(0),
	)
	if err != nil {
		t.Fatalf("NewAgent() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := agent.Run(ctx, UserMessage("Say 'hello from resilient' and nothing else."))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if resp.Content == "" {
		t.Error("expected non-empty response content")
	}

	t.Logf("Response: %s", resp.Content)

	// Verify the primary (OpenAI) was used, not the fallback mock.
	// A real LLM response should be longer and different from "fallback response".
	if resp.Content == "fallback response" {
		t.Error("expected primary provider (OpenAI) to handle the request, but got mock fallback response")
	}
}

func TestIntegration_ResilientProvider_FallbackUsed(t *testing.T) {
	// Test that when the primary provider fails, the fallback is used.
	// We use a mock that always errors as primary, and a working mock as fallback.
	failingPrimary := llm.NewMockLLM(t).WithError(fmt.Errorf("primary unavailable"))
	fallback := llm.NewMockLLM(t).WithResponse("fallback worked")

	cfg := llm.DefaultResilientConfig()
	cfg.MaxRetries = 1 // reduce retries to speed up the test

	resilient, err := llm.NewResilientProvider(failingPrimary, cfg)
	if err != nil {
		t.Fatalf("NewResilientProvider() error = %v", err)
	}
	resilient.AddFallback(fallback)

	agent, err := NewAgent("test-resilient-fallback", "You are a helpful assistant.", resilient,
		WithMaxTurns(3),
		WithTemperature(0),
	)
	if err != nil {
		t.Fatalf("NewAgent() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := agent.Run(ctx, UserMessage("Say hello."))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if resp.Content == "" {
		t.Error("expected non-empty response content from fallback")
	}

	t.Logf("Fallback response: %s", resp.Content)

	if resp.Content != "fallback worked" {
		t.Errorf("expected fallback response, got: %s", resp.Content)
	}
}

func TestIntegration_ReActAgent_StreamRun(t *testing.T) {
	provider := newTestProvider(t)

	agent, err := NewAgent("test-stream", "You are a helpful assistant. Follow instructions exactly.", provider,
		WithMaxTurns(3),
		WithTemperature(0),
	)
	if err != nil {
		t.Fatalf("NewAgent() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ch, err := agent.StreamRun(ctx, UserMessage("Count from 1 to 3."))
	if err != nil {
		t.Fatalf("StreamRun() error = %v", err)
	}

	var tokens []string
	var gotComplete bool
	var gotError bool

	for event := range ch {
		switch event.Type {
		case StreamEventToken:
			tokens = append(tokens, event.Content)
		case StreamEventComplete:
			gotComplete = true
			if event.Content == "" {
				t.Error("expected non-empty content in complete event")
			}
			t.Logf("Complete event content: %s", event.Content)
		case StreamEventThought:
			t.Logf("Thought: %s", event.Content)
		case StreamEventError:
			gotError = true
			t.Errorf("Error event: %s", event.Content)
		case StreamEventToolCall:
			t.Logf("Tool call: %s", event.Content)
		case StreamEventToolResult:
			t.Logf("Tool result: %s", event.Content)
		}
	}

	if gotError && !gotComplete {
		t.Error("stream ended with error and no completion")
	}

	if !gotComplete {
		t.Error("expected StreamEventComplete event")
	}

	t.Logf("Stream collected %d token events", len(tokens))
}
