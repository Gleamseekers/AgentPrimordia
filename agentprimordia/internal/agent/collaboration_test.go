
package agent

import (
	"context"
	"fmt"
	"testing"
)

// mockAgentForOrch 编排测试用的桩 Agent 构造函数。
// 本文件的协作模式测试早于 newStubAgent 存在，为最小化改动保留原命名，
// 实现委托 newStubAgent（orchestration_pipeline_test.go）。
func mockAgentForOrch(name, output string) *stubAgent {
	return newStubAgent(name, output)
}

func TestHandoff_NoMatchingAgent(t *testing.T) {
	agent1 := mockAgentForOrch("agent-1", "handled")

	handoff := NewHandoff(HandoffConfig{
		Agents: []Agent{agent1},
		Router: func(_ context.Context, _ string) int {
			return -1
		},
	})

	_, err := handoff.Run(context.Background(), "nobody can handle this")
	if err == nil {
		t.Error("expected error when no agent matches")
	}
}

func TestHandoff_MultipleTransfers(t *testing.T) {
	agent1 := mockAgentForOrch("agent-1", "step1 done")
	agent2 := mockAgentForOrch("agent-2", "step2 done")
	agent3 := mockAgentForOrch("agent-3", "final result")

	routeCall := 0
	handoff := NewHandoff(HandoffConfig{
		Agents: []Agent{agent1, agent2, agent3},
		Router: func(_ context.Context, _ string) int {
			routeCall++
			switch routeCall {
			case 1:
				return 0
			case 2:
				return 1
			case 3:
				return 1
			case 4:
				return 2
			case 5:
				return 2
			default:
				return -1
			}
		},
		MaxHandoffs: 10,
	})

	result, err := handoff.Run(context.Background(), "multi-step task")
	if err != nil {
		t.Fatalf("Handoff.Run() error = %v", err)
	}

	if result.Output != "final result" {
		t.Errorf("output = %q, want %q", result.Output, "final result")
	}
	if result.Handoffs != 3 {
		t.Errorf("handoffs = %d, want 3", result.Handoffs)
	}
}


func TestHandoff_AgentFails(t *testing.T) {
	failAgent := &mockFailAgent{name: "fail-agent"}

	handoff := NewHandoff(HandoffConfig{
		Agents: []Agent{failAgent},
		Router: func(_ context.Context, _ string) int {
			return 0
		},
	})

	_, err := handoff.Run(context.Background(), "cause failure")
	if err == nil {
		t.Error("expected error when agent fails")
	}
}


func TestHandoff_SameAgentStops(t *testing.T) {
	agent1 := mockAgentForOrch("agent-1", "same agent result")

	handoff := NewHandoff(HandoffConfig{
		Agents: []Agent{agent1},
		Router: func(_ context.Context, _ string) int {
			return 0
		},
	})

	result, err := handoff.Run(context.Background(), "test same agent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Handoffs != 1 {
		t.Errorf("handoffs = %d, want 1 (same agent should stop)", result.Handoffs)
	}
}

func TestCollaborator_Sequential(t *testing.T) {
	bus := NewLocalMessageBus()

	bus.Register("agent-a", func(_ context.Context, msg *BusMessage) (*BusMessage, error) {
		return &BusMessage{
			From:    msg.To,
			To:      msg.From,
			Type:    BusMsgResponse,
			Content: msg.Content + " -> A",
		}, nil
	})

	bus.Register("agent-b", func(_ context.Context, msg *BusMessage) (*BusMessage, error) {
		return &BusMessage{
			From:    msg.To,
			To:      msg.From,
			Type:    BusMsgResponse,
			Content: msg.Content + " -> B",
		}, nil
	})

	collab := NewCollaborator(bus)
	result, err := collab.Run(context.Background(), CollaborationConfig{
		Pattern:      CollabSequential,
		Participants: []string{"agent-a", "agent-b"},
	}, "start")

	if err != nil {
		t.Fatalf("Collaborator.Run() error = %v", err)
	}
	if result.Outputs["agent-a"] != "start -> A" {
		t.Errorf("agent-a output = %q, want %q", result.Outputs["agent-a"], "start -> A")
	}
	if result.Outputs["agent-b"] != "start -> A -> B" {
		t.Errorf("agent-b output = %q, want %q", result.Outputs["agent-b"], "start -> A -> B")
	}
	if result.Rounds != 2 {
		t.Errorf("rounds = %d, want 2", result.Rounds)
	}
}

func TestCollaborator_Parallel(t *testing.T) {
	bus := NewLocalMessageBus()

	bus.Register("agent-x", func(_ context.Context, msg *BusMessage) (*BusMessage, error) {
		return &BusMessage{
			From:    msg.To,
			To:      msg.From,
			Type:    BusMsgResponse,
			Content: "parallel-x",
		}, nil
	})

	bus.Register("agent-y", func(_ context.Context, msg *BusMessage) (*BusMessage, error) {
		return &BusMessage{
			From:    msg.To,
			To:      msg.From,
			Type:    BusMsgResponse,
			Content: "parallel-y",
		}, nil
	})

	collab := NewCollaborator(bus)
	result, err := collab.Run(context.Background(), CollaborationConfig{
		Pattern:      CollabParallel,
		Participants: []string{"agent-x", "agent-y"},
	}, "input")

	if err != nil {
		t.Fatalf("Collaborator.Run() error = %v", err)
	}
	if result.Outputs["agent-x"] != "parallel-x" {
		t.Errorf("agent-x output = %q", result.Outputs["agent-x"])
	}
	if result.Outputs["agent-y"] != "parallel-y" {
		t.Errorf("agent-y output = %q", result.Outputs["agent-y"])
	}
}

func TestCollaborator_Debate(t *testing.T) {
	bus := NewLocalMessageBus()

	bus.Register("pro", func(_ context.Context, msg *BusMessage) (*BusMessage, error) {
		return &BusMessage{From: "pro", Type: BusMsgResponse, Content: "pro: " + msg.Content}, nil
	})

	bus.Register("con", func(_ context.Context, msg *BusMessage) (*BusMessage, error) {
		return &BusMessage{From: "con", Type: BusMsgResponse, Content: "con: " + msg.Content}, nil
	})

	collab := NewCollaborator(bus)
	result, err := collab.Run(context.Background(), CollaborationConfig{
		Pattern:      CollabDebate,
		Participants: []string{"pro", "con"},
		MaxRounds:    2,
	}, "topic")

	if err != nil {
		t.Fatalf("Collaborator.Run() error = %v", err)
	}
	if result.Rounds != 2 {
		t.Errorf("rounds = %d, want 2", result.Rounds)
	}
	if result.Winner != "con" {
		t.Errorf("winner = %q, want %q", result.Winner, "con")
	}
}

func TestCollaborator_Debate_TooFewParticipants(t *testing.T) {
	bus := NewLocalMessageBus()
	collab := NewCollaborator(bus)

	_, err := collab.Run(context.Background(), CollaborationConfig{
		Pattern:      CollabDebate,
		Participants: []string{"only-one"},
	}, "topic")

	if err == nil {
		t.Error("expected error for debate with < 2 participants")
	}
}

func TestCollaborator_Review(t *testing.T) {
	bus := NewLocalMessageBus()

	bus.Register("author", func(_ context.Context, msg *BusMessage) (*BusMessage, error) {
		return &BusMessage{From: "author", Type: BusMsgResponse, Content: "draft content"}, nil
	})

	bus.Register("reviewer", func(_ context.Context, msg *BusMessage) (*BusMessage, error) {
		return &BusMessage{From: "reviewer", Type: BusMsgResponse, Content: "review: looks good"}, nil
	})

	collab := NewCollaborator(bus)
	result, err := collab.Run(context.Background(), CollaborationConfig{
		Pattern:      CollabReview,
		Participants: []string{"author", "reviewer"},
	}, "write something")

	if err != nil {
		t.Fatalf("Collaborator.Run() error = %v", err)
	}
	if result.Outputs["author"] != "draft content" {
		t.Errorf("author output = %q", result.Outputs["author"])
	}
	if result.Outputs["reviewer"] != "review: looks good" {
		t.Errorf("reviewer output = %q", result.Outputs["reviewer"])
	}
}

func TestCollaborator_Review_TooFewParticipants(t *testing.T) {
	bus := NewLocalMessageBus()
	collab := NewCollaborator(bus)

	_, err := collab.Run(context.Background(), CollaborationConfig{
		Pattern:      CollabReview,
		Participants: []string{"only-author"},
	}, "topic")

	if err == nil {
		t.Error("expected error for review with < 2 participants")
	}
}

func TestCollaborator_UnknownPattern(t *testing.T) {
	bus := NewLocalMessageBus()
	collab := NewCollaborator(bus)

	_, err := collab.Run(context.Background(), CollaborationConfig{
		Pattern:      CollaborationPattern("unknown"),
		Participants: []string{"a"},
	}, "topic")

	if err == nil {
		t.Error("expected error for unknown collaboration pattern")
	}
}

// mockFailAgent 总是返回错误的 Agent
type mockFailAgent struct {
	name string
}

func (a *mockFailAgent) Run(_ context.Context, _ Message) (*Response, error) {
	return nil, fmt.Errorf("agent %s failed", a.name)
}

func (a *mockFailAgent) StreamRun(_ context.Context, _ Message) (<-chan StreamEvent, error) {
	ch := make(chan StreamEvent, 1)
	ch <- StreamEvent{Type: StreamEventError, Content: "failed"}
	close(ch)
	return ch, nil
}

func (a *mockFailAgent) Stop()             {}
func (a *mockFailAgent) Stats() AgentStats { return AgentStats{Status: StatusFailed} }
func (a *mockFailAgent) Name() string      { return a.name }
