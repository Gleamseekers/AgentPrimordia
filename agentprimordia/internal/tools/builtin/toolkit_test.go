package builtin

import (
	"strings"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/concurrency"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools"
)

func TestDefaultToolkit_AllTools(t *testing.T) {
	tmpDir := t.TempDir()
	reg, err := DefaultToolkit(ToolkitConfig{
		RootDir:      tmpDir,
		EnableFS:     true,
		EnableShell:  true,
		EnableWeb:    true,
		EnableSearch: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reg.Count() != 3 {
		t.Errorf("expected 3 tools, got %d", reg.Count())
	}
	names := reg.List()
	hasFS := false
	hasShell := false
	hasWeb := false
	for _, n := range names {
		switch n {
		case "filesystem":
			hasFS = true
		case "shell":
			hasShell = true
		case "web":
			hasWeb = true
		}
	}
	if !hasFS {
		t.Error("filesystem tool should be registered")
	}
	if !hasShell {
		t.Error("shell tool should be registered")
	}
	if !hasWeb {
		t.Error("web tool should be registered")
	}
}

func TestDefaultToolkit_WithScopePolicy(t *testing.T) {
	tmpDir := t.TempDir()
	policy := tools.NewFileScopePolicy()
	policy.SetScope("agent-1", []string{tmpDir})

	reg, err := DefaultToolkit(ToolkitConfig{
		RootDir:     tmpDir,
		EnableFS:    true,
		EnableShell: true,
		EnableWeb:   false,
		ScopePolicy: policy,
		ScopeAgent:  "agent-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reg.Count() != 2 {
		t.Errorf("expected 2 tools, got %d", reg.Count())
	}

	fsTool, ok := reg.Get("filesystem")
	if !ok {
		t.Fatal("filesystem tool should exist")
	}
	fs, ok := fsTool.(*FileSystem)
	if !ok {
		t.Fatal("should be *FileSystem")
	}
	if fs.scopePolicy == nil {
		t.Error("scopePolicy should be injected")
	}
}

func TestDefaultToolkit_WithFileLock(t *testing.T) {
	tmpDir := t.TempDir()
	fl := concurrency.NewFileLockManager()

	reg, err := DefaultToolkit(ToolkitConfig{
		RootDir:  tmpDir,
		EnableFS: true,
		FileLock: fl,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fsTool, ok := reg.Get("filesystem")
	if !ok {
		t.Fatal("filesystem tool should exist")
	}
	fs, ok := fsTool.(*FileSystem)
	if !ok {
		t.Fatal("should be *FileSystem")
	}
	if fs.fileLock == nil {
		t.Error("fileLock should be injected")
	}
}

func TestDefaultToolkit_EmptyRootDir(t *testing.T) {
	_, err := DefaultToolkit(ToolkitConfig{
		RootDir: "",
	})
	if err == nil {
		t.Fatal("empty rootDir should return error")
	}
	if !strings.Contains(err.Error(), "rootDir") {
		t.Errorf("error should mention rootDir, got: %v", err)
	}
}

func TestMinimalToolkit(t *testing.T) {
	tmpDir := t.TempDir()
	reg, err := MinimalToolkit(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reg.Count() != 2 {
		t.Errorf("expected 2 tools, got %d", reg.Count())
	}
	names := reg.List()
	hasFS := false
	hasShell := false
	for _, n := range names {
		switch n {
		case "filesystem":
			hasFS = true
		case "shell":
			hasShell = true
		}
	}
	if !hasFS {
		t.Error("filesystem tool should be registered")
	}
	if !hasShell {
		t.Error("shell tool should be registered")
	}
	_, hasWeb := reg.Get("web")
	if hasWeb {
		t.Error("web tool should NOT be registered in minimal toolkit")
	}
}

func TestMinimalToolkit_EmptyRootDir(t *testing.T) {
	_, err := MinimalToolkit("")
	if err == nil {
		t.Fatal("empty rootDir should return error")
	}
}
