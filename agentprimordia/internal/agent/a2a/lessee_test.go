package a2a

import (
	"context"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/tools"
)

func TestLesseeClient_LeaseTool(t *testing.T) {
	reg := tools.NewRegistry()
	client := NewLesseeClient(reg)
	ctx := context.Background()

	lease, err := client.LeaseTool(ctx, "weather-agent", "get_weather", "localhost:50051")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lease.Status != LeaseStatusActive {
		t.Errorf("expected status Active, got %v", lease.Status)
	}
	if !lease.IsActive() {
		t.Error("expected lease to be active")
	}
	if lease.ToolName != "get_weather" {
		t.Errorf("expected tool name 'get_weather', got %q", lease.ToolName)
	}
}

func TestLesseeClient_ReleaseLease(t *testing.T) {
	reg := tools.NewRegistry()
	client := NewLesseeClient(reg)
	ctx := context.Background()

	lease, _ := client.LeaseTool(ctx, "agent-b", "tool-x", "localhost:50051")
	if !client.ReleaseLease(lease.LeaseID) {
		t.Error("expected release to succeed")
	}
	if lease.Status != LeaseStatusReleased {
		t.Errorf("expected status Released, got %v", lease.Status)
	}
}

func TestLesseeClient_ActiveLeaseCount(t *testing.T) {
	reg := tools.NewRegistry()
	client := NewLesseeClient(reg)
	ctx := context.Background()

	_, _ = client.LeaseTool(ctx, "agent-a", "tool-1", "localhost:50051")
	_, _ = client.LeaseTool(ctx, "agent-b", "tool-2", "localhost:50052")
	if client.ActiveLeaseCount() != 2 {
		t.Errorf("expected 2 active leases, got %d", client.ActiveLeaseCount())
	}
}

func TestLesseeClient_GetLease(t *testing.T) {
	reg := tools.NewRegistry()
	client := NewLesseeClient(reg)
	ctx := context.Background()

	lease, _ := client.LeaseTool(ctx, "agent-b", "tool-x", "localhost:50051")
	got, ok := client.GetLease(lease.LeaseID)
	if !ok {
		t.Fatal("expected to find lease")
	}
	if got.LeaseID != lease.LeaseID {
		t.Errorf("expected lease %s, got %s", lease.LeaseID, got.LeaseID)
	}
}

func TestLesseeClient_LeaseTool_MaxCallsEnforced(t *testing.T) {
	lease := &ToolLease{
		Status:    LeaseStatusActive,
		ExpiresAt: time.Now().Add(1 * time.Hour),
		MaxCalls:  1,
		UsedCalls: 1,
	}
	if lease.CanCall() {
		t.Error("expected CanCall to be false after MaxCalls reached")
	}
}
