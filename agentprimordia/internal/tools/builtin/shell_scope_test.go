package builtin

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/tools"
)

func TestShell_Execute_ScopeDenied(t *testing.T) {
	shell := NewShell().WithBlacklist()

	policy := tools.NewFileScopePolicy()
	policy.SetScope("agent-1", []string{"/home/user/project"})
	shell.WithScopePolicy(policy, "agent-1")

	args, _ := json.Marshal(map[string]string{
		"action":  "execute",
		"command": "ls",
		"workdir": "/etc",
	})

	result, err := shell.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for scope denied, got nil")
	}
	if result == nil || !result.IsError {
		t.Error("expected error result for scope denied")
	}
}

func TestShell_Execute_ScopeAllowed(t *testing.T) {
	shell := NewShell().WithBlacklist()

	dir := t.TempDir()
	policy := tools.NewFileScopePolicy()
	policy.SetScope("agent-1", []string{dir})
	shell.WithScopePolicy(policy, "agent-1")

	args, _ := json.Marshal(map[string]string{
		"action":  "execute",
		"command": "go env GOROOT",
		"workdir": dir,
	})

	result, err := shell.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("expected success, got error: %s", result.Content)
	}
}

func TestShell_Execute_NoScopePolicy(t *testing.T) {
	shell := NewShell().WithBlacklist()

	dir := t.TempDir()
	args, _ := json.Marshal(map[string]string{
		"action":  "execute",
		"command": "go env GOROOT",
		"workdir": dir,
	})

	result, err := shell.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("expected success without policy, got error: %s", result.Content)
	}
}
