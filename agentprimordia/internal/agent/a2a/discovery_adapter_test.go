package a2a

import (
	"context"
	"fmt"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/discovery"
)

// mockDiscovery 模拟 discovery.Discovery 实现
type mockDiscovery struct {
	agents map[string]*discovery.AgentInfo
}

func newMockDiscovery() *mockDiscovery {
	return &mockDiscovery{agents: make(map[string]*discovery.AgentInfo)}
}

func (m *mockDiscovery) Register(_ context.Context, info *discovery.AgentInfo) error {
	m.agents[info.ID] = info
	return nil
}

func (m *mockDiscovery) Unregister(_ context.Context, agentID string) error {
	delete(m.agents, agentID)
	return nil
}

func (m *mockDiscovery) Discover(_ context.Context, agentID string) (*discovery.AgentInfo, error) {
	info, ok := m.agents[agentID]
	if !ok {
		return nil, fmt.Errorf("agent not found: %s", agentID)
	}
	return info, nil
}

func (m *mockDiscovery) ListAgents(_ context.Context) ([]*discovery.AgentInfo, error) {
	out := make([]*discovery.AgentInfo, 0, len(m.agents))
	for _, info := range m.agents {
		out = append(out, info)
	}
	return out, nil
}

func (m *mockDiscovery) Heartbeat(_ context.Context, _ string) error { return nil }
func (m *mockDiscovery) Close() error                                { return nil }

func TestDiscoveryAdapter_RegisterAndResolve(t *testing.T) {
	mock := newMockDiscovery()
	adapter := NewDiscoveryAdapter(mock)

	card := &AgentCard{
		AgentID: "agent-1",
		Name:    "Test Agent",
	}
	card.Endpoints.BaseURL = "http://localhost:8080"
	card.Skills = []AgentSkill{{Name: "search"}}

	if err := adapter.Register(card); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	reg, err := adapter.Resolve("agent-1")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if reg.Card.Name != "Test Agent" {
		t.Errorf("Card.Name = %q, want %q", reg.Card.Name, "Test Agent")
	}
	if reg.Card.Endpoints.BaseURL != "http://localhost:8080" {
		t.Errorf("BaseURL = %q, want %q", reg.Card.Endpoints.BaseURL, "http://localhost:8080")
	}
}

func TestDiscoveryAdapter_Deregister(t *testing.T) {
	mock := newMockDiscovery()
	adapter := NewDiscoveryAdapter(mock)

	card := &AgentCard{AgentID: "agent-1", Name: "Test"}
	_ = adapter.Register(card)

	if err := adapter.Deregister("agent-1"); err != nil {
		t.Fatalf("Deregister() error = %v", err)
	}

	_, err := adapter.Resolve("agent-1")
	if err == nil {
		t.Fatal("Resolve() expected error after deregister")
	}
}

func TestDiscoveryAdapter_List(t *testing.T) {
	mock := newMockDiscovery()
	adapter := NewDiscoveryAdapter(mock)

	_ = adapter.Register(&AgentCard{AgentID: "a1", Name: "Agent 1"})
	_ = adapter.Register(&AgentCard{AgentID: "a2", Name: "Agent 2"})

	list := adapter.List()
	if len(list) != 2 {
		t.Fatalf("List() len = %d, want 2", len(list))
	}
}

func TestDiscoveryAdapter_Watch(t *testing.T) {
	mock := newMockDiscovery()
	adapter := NewDiscoveryAdapter(mock)

	card := &AgentCard{AgentID: "a1", Name: "Agent 1"}
	_ = adapter.Register(card)

	ev := <-adapter.Watch()
	if ev.Type != EventAgentRegistered {
		t.Errorf("event type = %q, want %q", ev.Type, EventAgentRegistered)
	}
	if ev.AgentID != "a1" {
		t.Errorf("event agentID = %q, want %q", ev.AgentID, "a1")
	}
}
