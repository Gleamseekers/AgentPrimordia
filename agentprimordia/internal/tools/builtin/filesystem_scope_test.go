package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/concurrency"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools"
)

func TestFileSystem_Write_WithFileLock(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileSystem(dir)
	if err != nil {
		t.Fatal(err)
	}

	fl := concurrency.NewFileLockManager()
	fs.WithFileLock(fl)

	args, _ := json.Marshal(map[string]string{
		"action":  "write",
		"path":    "locked.txt",
		"content": "locked content",
	})

	result, err := fs.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("expected success, got error: %s", result.Content)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "locked.txt"))
	if string(data) != "locked content" {
		t.Errorf("file content = %q, want %q", string(data), "locked content")
	}
}

func TestFileSystem_Write_ScopeDenied(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileSystem(dir)
	if err != nil {
		t.Fatal(err)
	}

	allowedDir := filepath.Join(dir, "allowed")
	os.MkdirAll(allowedDir, 0755)

	policy := tools.NewFileScopePolicy()
	policy.SetScope("agent-1", []string{allowedDir})
	fs.WithScopePolicy(policy, "agent-1")

	outFile := filepath.Join(dir, "denied", "test.txt")
	args, _ := json.Marshal(map[string]string{
		"action":  "write",
		"path":    outFile,
		"content": "should fail",
	})

	result, err := fs.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for scope denied, got nil")
	}
	if result == nil || !result.IsError {
		t.Error("expected error result for scope denied")
	}
}

func TestFileSystem_Edit_WithFileLock(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileSystem(dir)
	if err != nil {
		t.Fatal(err)
	}

	fl := concurrency.NewFileLockManager()
	fs.WithFileLock(fl)

	os.WriteFile(filepath.Join(dir, "edit_locked.txt"), []byte("hello world"), 0644)

	args, _ := json.Marshal(map[string]string{
		"action":  "edit",
		"path":    "edit_locked.txt",
		"old_str": "hello",
		"new_str": "goodbye",
	})

	result, err := fs.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("expected success, got error: %s", result.Content)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "edit_locked.txt"))
	if string(data) != "goodbye world" {
		t.Errorf("file content = %q, want %q", string(data), "goodbye world")
	}
}

func TestFileSystem_Edit_ScopeDenied(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileSystem(dir)
	if err != nil {
		t.Fatal(err)
	}

	allowedDir := filepath.Join(dir, "allowed")
	os.MkdirAll(allowedDir, 0755)

	policy := tools.NewFileScopePolicy()
	policy.SetScope("agent-1", []string{allowedDir})
	fs.WithScopePolicy(policy, "agent-1")

	testFile := filepath.Join(dir, "denied", "edit.txt")
	os.MkdirAll(filepath.Dir(testFile), 0755)
	os.WriteFile(testFile, []byte("hello"), 0644)

	args, _ := json.Marshal(map[string]string{
		"action":  "edit",
		"path":    testFile,
		"old_str": "hello",
		"new_str": "goodbye",
	})

	result, err := fs.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for scope denied, got nil")
	}
	if result == nil || !result.IsError {
		t.Error("expected error result for scope denied")
	}
}

func TestFileSystem_Read_ScopeDenied(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileSystem(dir)
	if err != nil {
		t.Fatal(err)
	}

	allowedDir := filepath.Join(dir, "allowed")
	os.MkdirAll(allowedDir, 0755)

	policy := tools.NewFileScopePolicy()
	policy.SetScope("agent-1", []string{allowedDir})
	fs.WithScopePolicy(policy, "agent-1")

	outFile := filepath.Join(dir, "denied", "secret.txt")
	os.MkdirAll(filepath.Dir(outFile), 0755)
	os.WriteFile(outFile, []byte("secret"), 0644)

	args, _ := json.Marshal(map[string]string{
		"action": "read",
		"path":   outFile,
	})

	result, err := fs.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for scope denied, got nil")
	}
	if result == nil || !result.IsError {
		t.Error("expected error result for scope denied")
	}
}
