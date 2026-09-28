
package agent

import (
	"context"
	"fmt"
	"testing"
)

// ===== Mock Agent for GroupChat tests =====

type mockGroupChatAgent struct {
	name   string
	output string
}

func (m *mockGroupChatAgent) Run(_ context.Context, input Message) (*Response, error) {
	return &Response{Content: m.output}, nil
}

func (m *mockGroupChatAgent) StreamRun(_ context.Context, _ Message) (<-chan StreamEvent, error) {
	ch := make(chan StreamEvent, 1)
	ch <- StreamEvent{Type: StreamEventComplete, Content: m.output}
	close(ch)
	return ch, nil
}

func (m *mockGroupChatAgent) Stop() {}

func (m *mockGroupChatAgent) Stats() AgentStats {
	return AgentStats{Status: StatusIdle}
}

func (m *mockGroupChatAgent) Name() string { return m.name }

// ===== Tests =====

func TestGroupChat_BasicConversation(t *testing.T) {
	agent1 := &mockGroupChatAgent{name: "agent-1", output: "hello from agent-1"}
	agent2 := &mockGroupChatAgent{name: "agent-2", output: "hello from agent-2"}

	gc, err := NewGroupChat(GroupChatConfig{
		Agents:        []Agent{agent1, agent2},
		MaxRounds:     3,
		SelectSpeaker: RoundRobinSelector(),
	})
	if err != nil {
		t.Fatalf("NewGroupChat() error = %v", err)
	}

	result, err := gc.Run(context.Background(), UserMessage("start"))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if result.Rounds < 1 {
		t.Errorf("rounds = %d, want >= 1", result.Rounds)
	}
	if len(result.Messages) < 2 {
		t.Errorf("messages = %d, want >= 2", len(result.Messages))
	}
	if len(result.AgentOrder) < 1 {
		t.Errorf("agentOrder = %d, want >= 1", len(result.AgentOrder))
	}
}

func TestGroupChat_RoundRobin(t *testing.T) {
	agent1 := &mockGroupChatAgent{name: "agent-1", output: "msg-1"}
	agent2 := &mockGroupChatAgent{name: "agent-2", output: "msg-2"}
	agent3 := &mockGroupChatAgent{name: "agent-3", output: "msg-3"}

	gc, err := NewGroupChat(GroupChatConfig{
		Agents:        []Agent{agent1, agent2, agent3},
		MaxRounds:     2,
		SelectSpeaker: RoundRobinSelector(),
	})
	if err != nil {
		t.Fatalf("NewGroupChat() error = %v", err)
	}

	result, err := gc.Run(context.Background(), UserMessage("start"))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(result.AgentOrder) < 2 {
		t.Errorf("agentOrder = %d, want >= 2", len(result.AgentOrder))
	}
	if result.AgentOrder[0] != "agent-1" {
		t.Errorf("first speaker = %q, want %q", result.AgentOrder[0], "agent-1")
	}
	if result.AgentOrder[1] != "agent-2" {
		t.Errorf("second speaker = %q, want %q", result.AgentOrder[1], "agent-2")
	}
}

func TestGroupChat_MaxRounds(t *testing.T) {
	agent1 := &mockGroupChatAgent{name: "agent-1", output: "msg-1"}
	agent2 := &mockGroupChatAgent{name: "agent-2", output: "msg-2"}

	gc, err := NewGroupChat(GroupChatConfig{
		Agents:        []Agent{agent1, agent2},
		MaxRounds:     2,
		SelectSpeaker: RoundRobinSelector(),
	})
	if err != nil {
		t.Fatalf("NewGroupChat() error = %v", err)
	}

	result, err := gc.Run(context.Background(), UserMessage("start"))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if result.Rounds != 2 {
		t.Errorf("rounds = %d, want 2", result.Rounds)
	}
	if result.Terminated {
		t.Error("expected not terminated")
	}
}

func TestGroupChat_Termination(t *testing.T) {
	agent1 := &mockGroupChatAgent{name: "agent-1", output: "TERMINATE"}
	agent2 := &mockGroupChatAgent{name: "agent-2", output: "msg-2"}

	gc, err := NewGroupChat(GroupChatConfig{
		Agents:        []Agent{agent1, agent2},
		MaxRounds:     5,
		SelectSpeaker: RoundRobinSelector(),
	})
	if err != nil {
		t.Fatalf("NewGroupChat() error = %v", err)
	}

	result, err := gc.Run(context.Background(), UserMessage("start"))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !result.Terminated {
		t.Error("expected terminated")
	}
	if result.Rounds != 1 {
		t.Errorf("rounds = %d, want 1", result.Rounds)
	}
}

func TestGroupChat_SingleAgentError(t *testing.T) {
	agent1 := &mockGroupChatAgent{name: "agent-1", output: "msg-1"}

	_, err := NewGroupChat(GroupChatConfig{
		Agents:        []Agent{agent1},
		MaxRounds:     3,
		SelectSpeaker: RoundRobinSelector(),
	})
	if err == nil {
		t.Error("expected error with single agent")
	}
}

func TestGroupChat_EmptyAgents(t *testing.T) {
	_, err := NewGroupChat(GroupChatConfig{
		Agents:        []Agent{},
		MaxRounds:     3,
		SelectSpeaker: RoundRobinSelector(),
	})
	if err == nil {
		t.Error("expected error with empty agents")
	}
}

func TestGroupChat_Broadcast(t *testing.T) {
	agent1 := &mockGroupChatAgent{name: "agent-1", output: "msg-1"}
	agent2 := &mockGroupChatAgent{name: "agent-2", output: "msg-2"}

	bus := NewLocalMessageBus()

	var broadcasted []string
	bus.Register("agent-1", func(_ context.Context, msg *BusMessage) (*BusMessage, error) {
		broadcasted = append(broadcasted, msg.Content)
		return nil, nil
	})
	bus.Register("agent-2", func(_ context.Context, msg *BusMessage) (*BusMessage, error) {
		broadcasted = append(broadcasted, msg.Content)
		return nil, nil
	})

	gc, err := NewGroupChat(GroupChatConfig{
		Agents:        []Agent{agent1, agent2},
		MaxRounds:     2,
		SelectSpeaker: RoundRobinSelector(),
		Bus:           bus,
	})
	if err != nil {
		t.Fatalf("NewGroupChat() error = %v", err)
	}

	_, err = gc.Run(context.Background(), UserMessage("start"))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(broadcasted) == 0 {
		t.Error("expected broadcast messages")
	}
}

// ===== Mock Agent with dynamic output =====

type mockDynamicAgent struct {
	name      string
	callCount int
	outputs   []string
}

func (m *mockDynamicAgent) Run(_ context.Context, _ Message) (*Response, error) {
	output := m.outputs[m.callCount%len(m.outputs)]
	m.callCount++
	return &Response{Content: output}, nil
}

func (m *mockDynamicAgent) StreamRun(_ context.Context, _ Message) (<-chan StreamEvent, error) {
	ch := make(chan StreamEvent, 1)
	ch <- StreamEvent{Type: StreamEventComplete, Content: m.outputs[0]}
	close(ch)
	return ch, nil
}

func (m *mockDynamicAgent) Stop() {}

func (m *mockDynamicAgent) Stats() AgentStats {
	return AgentStats{Status: StatusIdle}
}

func (m *mockDynamicAgent) Name() string { return m.name }

func TestGroupChat_RandomSelector(t *testing.T) {
	agent1 := &mockDynamicAgent{name: "agent-1", outputs: []string{"msg-1"}}
	agent2 := &mockDynamicAgent{name: "agent-2", outputs: []string{"msg-2"}}

	gc, err := NewGroupChat(GroupChatConfig{
		Agents:        []Agent{agent1, agent2},
		MaxRounds:     3,
		SelectSpeaker: RandomSelector(),
	})
	if err != nil {
		t.Fatalf("NewGroupChat() error = %v", err)
	}

	result, err := gc.Run(context.Background(), UserMessage("start"))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if result.Rounds != 3 {
		t.Errorf("rounds = %d, want 3", result.Rounds)
	}
	if len(result.AgentOrder) != 3 {
		t.Errorf("agentOrder = %d, want 3", len(result.AgentOrder))
	}
}

func TestGroupChat_LastSpeakerSelector(t *testing.T) {
	agent1 := &mockGroupChatAgent{name: "agent-1", output: "msg-1"}
	agent2 := &mockGroupChatAgent{name: "agent-2", output: "msg-2"}

	gc, err := NewGroupChat(GroupChatConfig{
		Agents:        []Agent{agent1, agent2},
		MaxRounds:     3,
		SelectSpeaker: LastSpeakerSelector(),
	})
	if err != nil {
		t.Fatalf("NewGroupChat() error = %v", err)
	}

	result, err := gc.Run(context.Background(), UserMessage("start"))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if result.Rounds != 3 {
		t.Errorf("rounds = %d, want 3", result.Rounds)
	}
	if len(result.AgentOrder) != 3 {
		t.Errorf("agentOrder = %d, want 3", len(result.AgentOrder))
	}
}

// ===== Mock fail agent for error propagation =====

type mockGroupChatFailAgent struct {
	name string
}

func (m *mockGroupChatFailAgent) Run(_ context.Context, _ Message) (*Response, error) {
	return nil, fmt.Errorf("agent %s failed", m.name)
}

func (m *mockGroupChatFailAgent) StreamRun(_ context.Context, _ Message) (<-chan StreamEvent, error) {
	ch := make(chan StreamEvent, 1)
	ch <- StreamEvent{Type: StreamEventError, Content: "failed"}
	close(ch)
	return ch, nil
}

func (m *mockGroupChatFailAgent) Stop() {}

func (m *mockGroupChatFailAgent) Stats() AgentStats {
	return AgentStats{Status: StatusFailed}
}

func (m *mockGroupChatFailAgent) Name() string { return m.name }

func TestGroupChat_AgentRunError(t *testing.T) {
	agent1 := &mockGroupChatAgent{name: "agent-1", output: "msg-1"}
	failAgent := &mockGroupChatFailAgent{name: "fail-agent"}

	gc, err := NewGroupChat(GroupChatConfig{
		Agents:        []Agent{agent1, failAgent},
		MaxRounds:     3,
		SelectSpeaker: RoundRobinSelector(),
	})
	if err != nil {
		t.Fatalf("NewGroupChat() error = %v", err)
	}

	_, err = gc.Run(context.Background(), UserMessage("start"))
	if err == nil {
		t.Error("expected error when agent fails")
	}
}

// ===== RoleBasedSelector Tests =====

// ===== Consensus Tests =====

func TestGroupChat_Consensus_Unanimous(t *testing.T) {
	agent1 := &mockGroupChatAgent{name: "agent-1", output: "option A"}
	agent2 := &mockGroupChatAgent{name: "agent-2", output: "option A"}
	agent3 := &mockGroupChatAgent{name: "agent-3", output: "option A"}

	gc, err := NewGroupChat(GroupChatConfig{
		Agents:    []Agent{agent1, agent2, agent3},
		MaxRounds: 3,
	})
	if err != nil {
		t.Fatalf("NewGroupChat() error = %v", err)
	}

	result, err := gc.RunConsensus(context.Background(), UserMessage("Choose A or B"))
	if err != nil {
		t.Fatalf("RunConsensus() error = %v", err)
	}

	if !result.Unanimous {
		t.Error("expected unanimous decision")
	}
	if result.Decision != "option A" {
		t.Errorf("decision = %q, want %q", result.Decision, "option A")
	}
	if len(result.Votes) != 3 {
		t.Errorf("votes count = %d, want 3", len(result.Votes))
	}
}

func TestGroupChat_Consensus_Majority(t *testing.T) {
	agent1 := &mockGroupChatAgent{name: "agent-1", output: "option A"}
	agent2 := &mockGroupChatAgent{name: "agent-2", output: "option B"}
	agent3 := &mockGroupChatAgent{name: "agent-3", output: "option A"}

	gc, err := NewGroupChat(GroupChatConfig{
		Agents:    []Agent{agent1, agent2, agent3},
		MaxRounds: 3,
	})
	if err != nil {
		t.Fatalf("NewGroupChat() error = %v", err)
	}

	result, err := gc.RunConsensus(context.Background(), UserMessage("Choose A or B"))
	if err != nil {
		t.Fatalf("RunConsensus() error = %v", err)
	}

	if result.Unanimous {
		t.Error("expected not unanimous")
	}
	if result.Decision != "option A" {
		t.Errorf("decision = %q, want %q (majority)", result.Decision, "option A")
	}
}
