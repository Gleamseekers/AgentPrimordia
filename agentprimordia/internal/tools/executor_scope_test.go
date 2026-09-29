package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/concurrency"
)

func TestExecutor_WithScopePolicy_Allowed(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(&scopeTestTool{name: "allowed_tool"})

	policy := NewFileScopePolicy()
	policy.SetScope("agent-1", []string{"/src/"})

	exec := NewExecutor(reg).WithScopePolicy(policy, "agent-1")

	result, err := exec.Execute(context.Background(), &FunctionCall{
		ID:   "1",
		Name: "allowed_tool",
		Args: `{"path": "/src/main.go"}`,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("expected success, got error: %s", result.Content)
	}
}

func TestExecutor_WithScopePolicy_Denied(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(&scopeTestTool{name: "denied_tool"})

	policy := NewFileScopePolicy()
	policy.SetScope("agent-1", []string{"/src/"})

	exec := NewExecutor(reg).WithScopePolicy(policy, "agent-1")

	result, err := exec.Execute(context.Background(), &FunctionCall{
		ID:   "1",
		Name: "denied_tool",
		Args: `{"path": "/etc/passwd"}`,
	})

	if err == nil {
		t.Fatal("expected error for scope denied, got nil")
	}
	if result == nil || !result.IsError {
		t.Error("expected error result for scope denied")
	}
}

func TestExecutor_WithScopePolicy_Nil(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(&scopeTestTool{name: "no_policy_tool"})

	exec := NewExecutor(reg)

	result, err := exec.Execute(context.Background(), &FunctionCall{
		ID:   "1",
		Name: "no_policy_tool",
		Args: `{"path": "/any/path"}`,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("expected success without policy, got error: %s", result.Content)
	}
}

func TestExecutor_WithFileLock(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(&scopeTestTool{name: "lock_tool"})

	fl := concurrency.NewFileLockManager()

	exec := NewExecutor(reg).WithFileLock(fl)

	result, err := exec.Execute(context.Background(), &FunctionCall{
		ID:   "1",
		Name: "lock_tool",
		Args: `{"path": "/tmp/test.txt"}`,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("expected success, got error: %s", result.Content)
	}
}

type scopeTestTool struct {
	name string
}

func (t *scopeTestTool) Name() string                { return t.name }
func (t *scopeTestTool) Description() string         { return "test" }
func (t *scopeTestTool) Parameters() json.RawMessage { return json.RawMessage(`{}`) }
func (t *scopeTestTool) Execute(ctx context.Context, args json.RawMessage) (*Result, error) {
	return NewResult("ok"), nil
}
