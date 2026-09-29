package a2a

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/tools"
)

// mockToolForLease 是测试用工具
type mockToolForLease struct {
	name     string
	desc     string
	params   json.RawMessage
	execFunc func(ctx context.Context, args json.RawMessage) (*tools.Result, error)
}

func (t *mockToolForLease) Name() string                { return t.name }
func (t *mockToolForLease) Description() string         { return t.desc }
func (t *mockToolForLease) Parameters() json.RawMessage { return t.params }
func (t *mockToolForLease) Execute(ctx context.Context, args json.RawMessage) (*tools.Result, error) {
	if t.execFunc != nil {
		return t.execFunc(ctx, args)
	}
	return tools.NewResult("ok"), nil
}

func newLessorWithTools() (*LessorHandler, *tools.Registry) {
	reg := tools.NewRegistry()
	_ = reg.Register(&mockToolForLease{
		name:   "get_weather",
		desc:   "Get weather info",
		params: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}}}`),
	})
	_ = reg.Register(&mockToolForLease{
		name:   "search_web",
		desc:   "Search the web",
		params: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`),
	})
	return NewLessorHandler(reg, "weather-agent"), reg
}

func TestLessorHandler_GetAvailableTools(t *testing.T) {
	l, _ := newLessorWithTools()
	caps := l.GetAvailableTools()
	if len(caps) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(caps))
	}
	names := make(map[string]bool)
	for _, c := range caps {
		names[c.Name] = true
	}
	if !names["get_weather"] || !names["search_web"] {
		t.Errorf("expected both tools, got names: %v", names)
	}
}

func TestLessorHandler_CreateLease(t *testing.T) {
	l, _ := newLessorWithTools()
	lease, err := l.CreateLease("get_weather", "caller-agent", "localhost:50051", 5*time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lease.Status != LeaseStatusActive {
		t.Errorf("expected status Active, got %v", lease.Status)
	}
	if !lease.IsActive() {
		t.Error("expected lease to be active")
	}
	if lease.LeaseID == "" {
		t.Error("expected lease ID")
	}
}

func TestLessorHandler_CreateLease_ToolNotFound(t *testing.T) {
	l, _ := newLessorWithTools()
	_, err := l.CreateLease("nonexistent", "caller", "localhost:50051", 5*time.Minute)
	if err == nil {
		t.Fatal("expected error for nonexistent tool")
	}
}

func TestLessorHandler_CreateLease_TTLClamped(t *testing.T) {
	l, _ := newLessorWithTools()
	// 请求超过 maxDuration 的 TTL 应该被 clamp
	lease, err := l.CreateLease("get_weather", "caller", "localhost:50051", 2*l.maxDuration)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !lease.CanCall() {
		t.Error("expected lease to be callable")
	}
}

func TestLessorHandler_ReleaseLease(t *testing.T) {
	l, _ := newLessorWithTools()
	lease, _ := l.CreateLease("get_weather", "caller", "localhost:50051", 5*time.Minute)
	if !l.ReleaseLease(lease.LeaseID) {
		t.Error("expected release to succeed")
	}
	if lease.Status != LeaseStatusReleased {
		t.Errorf("expected status Released, got %v", lease.Status)
	}
}

func TestLessorHandler_RecordCall(t *testing.T) {
	l, _ := newLessorWithTools()
	lease, _ := l.CreateLease("get_weather", "caller", "localhost:50051", 5*time.Minute)
	if !l.RecordCall(lease.LeaseID) {
		t.Error("expected call to be recorded")
	}
	if lease.UsedCalls != 1 {
		t.Errorf("expected 1 used call, got %d", lease.UsedCalls)
	}
}

func TestToolLease_CanCall(t *testing.T) {
	lease := &ToolLease{
		Status:    LeaseStatusActive,
		ExpiresAt: time.Now().Add(1 * time.Hour),
		MaxCalls:  2,
	}
	if !lease.CanCall() {
		t.Error("expected CanCall to be true")
	}
	lease.UsedCalls = 2
	if lease.CanCall() {
		t.Error("expected CanCall to be false after MaxCalls exceeded")
	}
}

func TestToolLease_IsActive_Expired(t *testing.T) {
	lease := &ToolLease{
		Status:    LeaseStatusActive,
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	}
	if lease.IsActive() {
		t.Error("expected lease to be expired")
	}
}

func TestLessorHandler_GetLeasedTool(t *testing.T) {
	l, _ := newLessorWithTools()
	lease, _ := l.CreateLease("get_weather", "caller", "localhost:50051", 5*time.Minute)
	got, ok := l.GetLeasedTool(lease.LeaseID)
	if !ok {
		t.Fatal("expected to find lease")
	}
	if got.LeaseID != lease.LeaseID {
		t.Errorf("expected lease %s, got %s", lease.LeaseID, got.LeaseID)
	}
}

func TestLessorHandler_GetLeasedTool_NotFound(t *testing.T) {
	l, _ := newLessorWithTools()
	_, ok := l.GetLeasedTool("nonexistent")
	if ok {
		t.Error("expected not to find lease")
	}
}
