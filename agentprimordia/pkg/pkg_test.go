package ap_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent"
	ap "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg"
)

func TestVersion(t *testing.T) {
	t.Parallel()
	if ap.Version == "" {
		t.Fatal("Version should not be empty")
	}
}

func TestErrorSentinels(t *testing.T) {
	t.Parallel()
	sentinelTests := []struct {
		name string
		err  error
		msg  string
	}{
		{"ErrAgentStopped", ap.ErrAgentStopped, "agent is stopped"},
		{"ErrMaxTurnsExceeded", ap.ErrMaxTurnsExceeded, "max turns exceeded"},
		{"ErrInvalidConfig", ap.ErrInvalidConfig, "invalid configuration"},
		{"ErrTimeout", ap.ErrTimeout, "operation timed out"},
		{"ErrTaskNotFound", ap.ErrTaskNotFound, "task not found"},
		{"ErrAgentRunning", ap.ErrAgentRunning, "agent is already running"},
		{"ErrToolNotFound", ap.ErrToolNotFound, "tool not found"},
		{"ErrToolExecution", ap.ErrToolExecution, "tool execution failed"},
		{"ErrLLMCallFailed", ap.ErrLLMCallFailed, "LLM call failed"},
		{"ErrContextCanceled", ap.ErrContextCanceled, "context canceled"},
		{"ErrPoolFull", ap.ErrPoolFull, "pool is at max capacity"},
	}

	for _, tt := range sentinelTests {
		t.Run(tt.name, func(t *testing.T) {
			if !errors.Is(tt.err, errors.New(tt.msg)) {
				if tt.err.Error() != tt.msg {
					t.Errorf("expected error message %q, got %q", tt.msg, tt.err.Error())
				}
			}
		})
	}
}

func TestAgentTypes(t *testing.T) {
	t.Parallel()
	msg := ap.UserMessage("hello")
	if msg.Role != ap.RoleUser {
		t.Errorf("expected RoleUser, got %s", msg.Role)
	}

	sysMsg := ap.SystemMessage("you are a helper")
	if sysMsg.Role != ap.RoleSystem {
		t.Errorf("expected RoleSystem, got %s", sysMsg.Role)
	}
}

func TestHookManager(t *testing.T) {
	t.Parallel()
	hm := ap.NewHookManager()
	if hm == nil {
		t.Fatal("NewHookManager returned nil")
	}
	hm.Register(ap.HookBeforeRun, func(ctx context.Context, hctx *ap.HookContext) error {
		return nil
	})
	if hm.Count(ap.HookBeforeRun) != 1 {
		t.Errorf("expected 1 hook, got %d", hm.Count(ap.HookBeforeRun))
	}
}

func TestLifecycle(t *testing.T) {
	t.Parallel()
	lc := ap.NewLifecycle()
	if lc == nil {
		t.Fatal("NewLifecycle returned nil")
	}
	if lc.Status() != ap.StatusIdle {
		t.Errorf("expected StatusIdle, got %s", lc.Status())
	}
}

func TestToolRegistry(t *testing.T) {
	t.Parallel()
	reg := ap.NewToolRegistry()
	if reg == nil {
		t.Fatal("NewToolRegistry returned nil")
	}
}

func TestMemoryStore(t *testing.T) {
	t.Parallel()
	store, err := ap.WithInMemory()
	if err != nil {
		t.Fatalf("WithInMemory failed: %v", err)
	}
	if store == nil {
		t.Fatal("WithInMemory returned nil")
	}
	store.Close()
}

func TestVectorStore(t *testing.T) {
	t.Parallel()
	vs := ap.NewVectorStore(3)
	if vs == nil {
		t.Fatal("NewVectorStore returned nil")
	}
	if vs.Dimensions() != 3 {
		t.Errorf("expected 3 dimensions, got %d", vs.Dimensions())
	}
}

func TestEventBus(t *testing.T) {
	t.Parallel()
	bus := ap.NewBus(16)
	if bus == nil {
		t.Fatal("NewBus returned nil")
	}
	bus.Close()
}

func TestMetrics(t *testing.T) {
	t.Parallel()
	m := ap.NewMetrics()
	if m == nil {
		t.Fatal("NewMetrics returned nil")
	}
}

func TestSecurity(t *testing.T) {
	t.Parallel()
	acl := ap.NewACL()
	if acl == nil {
		t.Fatal("NewACL returned nil")
	}
	sb := ap.NewSandbox(acl)
	if sb == nil {
		t.Fatal("NewSandbox returned nil")
	}
}

func TestCheckpointStore(t *testing.T) {
	t.Parallel()
	store, err := ap.InMemoryCheckpointStore()
	if err != nil {
		t.Fatalf("InMemoryCheckpointStore failed: %v", err)
	}
	if store == nil {
		t.Fatal("InMemoryCheckpointStore returned nil")
	}
	store.Close()
}

func TestFileLockManager(t *testing.T) {
	t.Parallel()
	flm := ap.NewFileLockManager()
	if flm == nil {
		t.Fatal("NewFileLockManager returned nil")
	}
}

func TestContextWindowStrategy(t *testing.T) {
	t.Parallel()
	strategy := ap.NewDefaultStrategy(5)
	if strategy == nil {
		t.Fatal("NewDefaultStrategy returned nil")
	}
}

func TestResilientConfig(t *testing.T) {
	t.Parallel()
	cfg := ap.DefaultResilientConfig()
	if cfg.MaxRetries <= 0 {
		t.Error("DefaultResilientConfig MaxRetries should be > 0")
	}
}

func TestFileScopePolicy(t *testing.T) {
	t.Parallel()
	policy := ap.NewFileScopePolicy()
	if policy == nil {
		t.Fatal("NewFileScopePolicy returned nil")
	}
}

func TestBuiltinTools(t *testing.T) {
	t.Parallel()
	fs, err := ap.NewFileSystem(".")
	if err != nil {
		t.Fatalf("NewFileSystem error: %v", err)
	}
	if fs == nil {
		t.Fatal("NewFileSystem returned nil")
	}
	sh := ap.NewShell()
	if sh == nil {
		t.Fatal("NewShell returned nil")
	}
	web := ap.NewWeb()
	if web == nil {
		t.Fatal("NewWeb returned nil")
	}
}

func TestErrorCodeMapping(t *testing.T) {
	t.Parallel()
	tests := []struct {
		err  error
		code string
	}{
		{ap.ErrAgentStopped, "AGENT_001"},
		{ap.ErrAgentRunning, "AGENT_002"},
		{ap.ErrMaxTurnsExceeded, "AGENT_003"},
		{ap.ErrNoToolkit, "AGENT_004"},
		{ap.ErrToolNotFound, "TOOL_001"},
		{ap.ErrToolExecution, "TOOL_002"},
		{ap.ErrInvalidConfig, "TOOL_003"},
		{ap.ErrConfirmDenied, "TOOL_004"},
		{ap.ErrLLMCallFailed, "LLM_001"},
		{ap.ErrNotSupported, "LLM_002"},
		{ap.ErrCircuitOpen, "LLM_003"},
		{ap.ErrAPIKeyRequired, "LLM_004"},
		{ap.ErrEmptyResponse, "LLM_005"},
		{ap.ErrResponseParseFailed, "LLM_006"},
		{ap.ErrRetriesExhausted, "LLM_007"},
		{ap.ErrFallbackFailed, "LLM_008"},
		{ap.ErrPoolFull, "POOL_001"},
		{ap.ErrTaskNotFound, "POOL_002"},
		{ap.ErrTimeout, "POOL_003"},
		{ap.ErrContextCanceled, "CTX_001"},
		{ap.ErrEpisodeNotFound, "MEM_001"},
		{ap.ErrInvalidImportance, "MEM_002"},
		{ap.ErrEmptyEpisodeID, "MEM_003"},
		{ap.ErrEmptySessionID, "MEM_004"},
		{ap.ErrEmptyRole, "MEM_005"},
		{ap.ErrEmptyContent, "MEM_006"},
		{ap.ErrDimensionMismatch, "MEM_007"},
		{ap.ErrVectorNotFound, "MEM_008"},
		{ap.ErrCommandBlocked, "SEC_001"},
		{ap.ErrCommandNotAllowed, "SEC_002"},
		{ap.ErrAccessDenied, "SEC_003"},
		{ap.ErrPathTraversal, "SEC_004"},
		{ap.ErrBusClosed, "EVT_001"},
		{ap.ErrCheckpointNotFound, "PST_001"},
		{ap.ErrGlobalWriteConflict, "CON_001"},
		{ap.ErrScopeOverlap, "CON_002"},
	}

	for _, tt := range tests {
		code := ap.GetErrorCode(tt.err)
		if code != tt.code {
			t.Errorf("GetErrorCode(%v) = %q, want %q", tt.err, code, tt.code)
		}
	}
}

func TestErrorCodeUnknown(t *testing.T) {
	t.Parallel()
	code := ap.GetErrorCode(errors.New("some random error"))
	if code != "UNKNOWN" {
		t.Errorf("GetErrorCode(unknown) = %q, want UNKNOWN", code)
	}
}

func TestCodeError(t *testing.T) {
	t.Parallel()
	ce := ap.WithCode("CUSTOM_001", "custom error")
	if ce.Code != "CUSTOM_001" {
		t.Errorf("Code = %q, want CUSTOM_001", ce.Code)
	}
	if ce.Error() != "custom error" {
		t.Errorf("Error() = %q, want 'custom error'", ce.Error())
	}
	if ce.GetCode() != "CUSTOM_001" {
		t.Errorf("GetCode() = %q, want CUSTOM_001", ce.GetCode())
	}
	// 修复评估报告 §四.1-①：GetErrorCode 的 errors.As 路径必须命中 CodeError
	//（修复前 WithCode 构造的错误码返回 "UNKNOWN"）。
	if code := ap.GetErrorCode(ce); code != "CUSTOM_001" {
		t.Errorf("GetErrorCode(CodeError) = %q, want CUSTOM_001", code)
	}
	// %w 包装后同样可命中
	wrapped := fmt.Errorf("outer: %w", ce)
	if code := ap.GetErrorCode(wrapped); code != "CUSTOM_001" {
		t.Errorf("GetErrorCode(wrapped CodeError) = %q, want CUSTOM_001", code)
	}
}

// TestErrOutputBlockedIdentity 验证 pkg.ErrOutputBlocked 与 agent 护栏拦截
// 抛出的 sentinel 是同一错误值（修复评估报告 §四.1-③）。
func TestErrOutputBlockedIdentity(t *testing.T) {
	t.Parallel()
	if ap.ErrOutputBlocked != agent.ErrOutputBlocked {
		t.Fatal("ap.ErrOutputBlocked 与 agent.ErrOutputBlocked 不是同一 sentinel")
	}
	if !errors.Is(agent.ErrOutputBlocked, ap.ErrOutputBlocked) {
		t.Fatal("errors.Is(agent.ErrOutputBlocked, ap.ErrOutputBlocked) 应为 true")
	}
	if code := ap.GetErrorCode(agent.ErrOutputBlocked); code != "GRD_002" {
		t.Fatalf("GetErrorCode(ErrOutputBlocked) = %q, want GRD_002", code)
	}
	if !ap.IsAccessDenied(agent.ErrOutputBlocked) {
		t.Fatal("IsAccessDenied(ErrOutputBlocked) 应为 true")
	}
}
