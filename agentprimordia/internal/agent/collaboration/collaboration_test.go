package collaboration

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent/bus"
)

// ===== mockAgent =====

type mockAgent struct {
	name    string
	respFn  func(ctx context.Context, msg Message) (Message, error)
	callCnt atomic.Int64
}

func (m *mockAgent) Name() string { return m.name }
func (m *mockAgent) Run(ctx context.Context, msg Message) (Message, error) {
	m.callCnt.Add(1)
	if m.respFn != nil {
		return m.respFn(ctx, msg)
	}
	return Message{Role: "assistant", Content: "response from " + m.name}, nil
}

// ===== Collaborator tests =====

func TestCollaborator_Sequential(t *testing.T) {
	b := bus.NewLocalMessageBus()
	b.Register("agent-1", func(ctx context.Context, msg *bus.BusMessage) (*bus.BusMessage, error) {
		return &bus.BusMessage{Content: msg.Content + " -> step1"}, nil
	})
	b.Register("agent-2", func(ctx context.Context, msg *bus.BusMessage) (*bus.BusMessage, error) {
		return &bus.BusMessage{Content: msg.Content + " -> step2"}, nil
	})

	c := NewCollaborator(b)
	result, err := c.Run(context.Background(), CollaborationConfig{
		Pattern:      CollabSequential,
		Participants: []string{"agent-1", "agent-2"},
	}, "start")

	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result.Pattern != CollabSequential {
		t.Errorf("Pattern = %s, want %s", result.Pattern, CollabSequential)
	}
	if result.Rounds != 2 {
		t.Errorf("Rounds = %d, want 2", result.Rounds)
	}
	if result.Outputs["agent-1"] != "start -> step1" {
		t.Errorf("agent-1 output = %q", result.Outputs["agent-1"])
	}
	if result.Outputs["agent-2"] != "start -> step1 -> step2" {
		t.Errorf("agent-2 output = %q", result.Outputs["agent-2"])
	}
}

func TestCollaborator_Parallel(t *testing.T) {
	b := bus.NewLocalMessageBus()
	b.Register("agent-1", func(ctx context.Context, msg *bus.BusMessage) (*bus.BusMessage, error) {
		return &bus.BusMessage{Content: "resp1"}, nil
	})
	b.Register("agent-2", func(ctx context.Context, msg *bus.BusMessage) (*bus.BusMessage, error) {
		return &bus.BusMessage{Content: "resp2"}, nil
	})

	c := NewCollaborator(b)
	result, err := c.Run(context.Background(), CollaborationConfig{
		Pattern:      CollabParallel,
		Participants: []string{"agent-1", "agent-2"},
	}, "input")

	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result.Rounds != 1 {
		t.Errorf("Rounds = %d, want 1", result.Rounds)
	}
	if result.Outputs["agent-1"] != "resp1" || result.Outputs["agent-2"] != "resp2" {
		t.Errorf("Outputs = %v", result.Outputs)
	}
}

func TestCollaborator_Parallel_PartialError(t *testing.T) {
	b := bus.NewLocalMessageBus()
	b.Register("agent-1", func(ctx context.Context, msg *bus.BusMessage) (*bus.BusMessage, error) {
		return &bus.BusMessage{Content: "ok"}, nil
	})
	b.Register("agent-2", func(ctx context.Context, msg *bus.BusMessage) (*bus.BusMessage, error) {
		return nil, errors.New("agent-2 failure")
	})

	c := NewCollaborator(b)
	result, err := c.Run(context.Background(), CollaborationConfig{
		Pattern:      CollabParallel,
		Participants: []string{"agent-1", "agent-2"},
	}, "input")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if result.Outputs["agent-1"] != "ok" {
		t.Errorf("agent-1 should still have output, got %q", result.Outputs["agent-1"])
	}
}

func TestCollaborator_Debate(t *testing.T) {
	b := bus.NewLocalMessageBus()
	b.Register("agent-a", func(ctx context.Context, msg *bus.BusMessage) (*bus.BusMessage, error) {
		return &bus.BusMessage{Content: "argA-" + msg.Content}, nil
	})
	b.Register("agent-b", func(ctx context.Context, msg *bus.BusMessage) (*bus.BusMessage, error) {
		return &bus.BusMessage{Content: "argB-" + msg.Content}, nil
	})

	c := NewCollaborator(b)
	result, err := c.Run(context.Background(), CollaborationConfig{
		Pattern:      CollabDebate,
		Participants: []string{"agent-a", "agent-b"},
		MaxRounds:    2,
	}, "topic")

	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result.Rounds != 2 {
		t.Errorf("Rounds = %d, want 2", result.Rounds)
	}
	if result.Winner != "agent-b" {
		t.Errorf("Winner = %q, want agent-b", result.Winner)
	}
}

func TestCollaborator_Debate_DefaultRounds(t *testing.T) {
	b := bus.NewLocalMessageBus()
	b.Register("agent-a", func(ctx context.Context, msg *bus.BusMessage) (*bus.BusMessage, error) {
		return &bus.BusMessage{Content: "a"}, nil
	})
	b.Register("agent-b", func(ctx context.Context, msg *bus.BusMessage) (*bus.BusMessage, error) {
		return &bus.BusMessage{Content: "b"}, nil
	})

	c := NewCollaborator(b)
	result, err := c.Run(context.Background(), CollaborationConfig{
		Pattern:      CollabDebate,
		Participants: []string{"agent-a", "agent-b"},
		MaxRounds:    0, // should default to 3
	}, "topic")

	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result.Rounds != 3 {
		t.Errorf("Rounds = %d, want 3 (default)", result.Rounds)
	}
}

func TestCollaborator_Debate_InsufficientParticipants(t *testing.T) {
	b := bus.NewLocalMessageBus()
	c := NewCollaborator(b)
	_, err := c.Run(context.Background(), CollaborationConfig{
		Pattern:      CollabDebate,
		Participants: []string{"only-one"},
	}, "topic")
	if !errors.Is(err, ErrDebateParticipants) {
		t.Errorf("err = %v, want ErrDebateParticipants", err)
	}
}

func TestCollaborator_Review(t *testing.T) {
	b := bus.NewLocalMessageBus()
	b.Register("author", func(ctx context.Context, msg *bus.BusMessage) (*bus.BusMessage, error) {
		return &bus.BusMessage{Content: "draft"}, nil
	})
	b.Register("reviewer-1", func(ctx context.Context, msg *bus.BusMessage) (*bus.BusMessage, error) {
		return &bus.BusMessage{Content: "review1"}, nil
	})
	b.Register("reviewer-2", func(ctx context.Context, msg *bus.BusMessage) (*bus.BusMessage, error) {
		return &bus.BusMessage{Content: "review2"}, nil
	})

	c := NewCollaborator(b)
	result, err := c.Run(context.Background(), CollaborationConfig{
		Pattern:      CollabReview,
		Participants: []string{"author", "reviewer-1", "reviewer-2"},
	}, "write something")

	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result.Outputs["author"] != "draft" {
		t.Errorf("author output = %q", result.Outputs["author"])
	}
	if result.Outputs["reviewer-1"] != "review1" {
		t.Errorf("reviewer-1 output = %q", result.Outputs["reviewer-1"])
	}
}

func TestCollaborator_Review_InsufficientParticipants(t *testing.T) {
	b := bus.NewLocalMessageBus()
	c := NewCollaborator(b)
	_, err := c.Run(context.Background(), CollaborationConfig{
		Pattern:      CollabReview,
		Participants: []string{"only-one"},
	}, "topic")
	if !errors.Is(err, ErrReviewParticipants) {
		t.Errorf("err = %v, want ErrReviewParticipants", err)
	}
}

func TestCollaborator_UnknownPattern(t *testing.T) {
	b := bus.NewLocalMessageBus()
	c := NewCollaborator(b)
	_, err := c.Run(context.Background(), CollaborationConfig{
		Pattern: "unknown",
	}, "topic")
	if err == nil {
		t.Fatal("expected error for unknown pattern")
	}
}

func TestCollaborator_Sequential_Error(t *testing.T) {
	b := bus.NewLocalMessageBus()
	b.Register("agent-1", func(ctx context.Context, msg *bus.BusMessage) (*bus.BusMessage, error) {
		return nil, errors.New("step1 failed")
	})

	c := NewCollaborator(b)
	_, err := c.Run(context.Background(), CollaborationConfig{
		Pattern:      CollabSequential,
		Participants: []string{"agent-1"},
	}, "start")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCollaborator_Review_ReviewerError(t *testing.T) {
	b := bus.NewLocalMessageBus()
	b.Register("author", func(ctx context.Context, msg *bus.BusMessage) (*bus.BusMessage, error) {
		return &bus.BusMessage{Content: "draft"}, nil
	})
	b.Register("reviewer-1", func(ctx context.Context, msg *bus.BusMessage) (*bus.BusMessage, error) {
		return nil, errors.New("reviewer error")
	})
	b.Register("reviewer-2", func(ctx context.Context, msg *bus.BusMessage) (*bus.BusMessage, error) {
		return &bus.BusMessage{Content: "review2"}, nil
	})

	c := NewCollaborator(b)
	result, err := c.Run(context.Background(), CollaborationConfig{
		Pattern:      CollabReview,
		Participants: []string{"author", "reviewer-1", "reviewer-2"},
	}, "write something")

	// Review should succeed even if one reviewer fails (logs warning, continues)
	if err != nil {
		t.Fatalf("Run should not fail on reviewer error: %v", err)
	}
	if result.Outputs["reviewer-2"] != "review2" {
		t.Errorf("reviewer-2 output = %q", result.Outputs["reviewer-2"])
	}
}

func TestCollaborator_Debate_RoundError(t *testing.T) {
	b := bus.NewLocalMessageBus()
	b.Register("agent-a", func(ctx context.Context, msg *bus.BusMessage) (*bus.BusMessage, error) {
		return &bus.BusMessage{Content: "argA"}, nil
	})
	b.Register("agent-b", func(ctx context.Context, msg *bus.BusMessage) (*bus.BusMessage, error) {
		return nil, errors.New("agent-b failure")
	})

	c := NewCollaborator(b)
	_, err := c.Run(context.Background(), CollaborationConfig{
		Pattern:      CollabDebate,
		Participants: []string{"agent-a", "agent-b"},
		MaxRounds:    3,
	}, "topic")
	if err == nil {
		t.Fatal("expected error from debate round failure")
	}
}

// ===== GroupChat tests =====

func TestNewGroupChat_Validation(t *testing.T) {
	t.Run("too few agents", func(t *testing.T) {
		_, err := NewGroupChat(GroupChatConfig{
			Agents:    []Agent{&mockAgent{name: "a"}},
			MaxRounds: 3,
		})
		if err == nil {
			t.Fatal("expected error for < 2 agents")
		}
	})
	t.Run("invalid maxRounds", func(t *testing.T) {
		_, err := NewGroupChat(GroupChatConfig{
			Agents:    []Agent{&mockAgent{name: "a"}, &mockAgent{name: "b"}},
			MaxRounds: 0,
		})
		if err == nil {
			t.Fatal("expected error for maxRounds <= 0")
		}
	})
}

func TestGroupChat_Run_RoundRobin(t *testing.T) {
	agent1 := &mockAgent{name: "agent-1"}
	agent2 := &mockAgent{name: "agent-2"}

	gc, err := NewGroupChat(GroupChatConfig{
		Agents:        []Agent{agent1, agent2},
		MaxRounds:     3,
		SelectSpeaker: RoundRobinSelector(),
	})
	if err != nil {
		t.Fatalf("NewGroupChat failed: %v", err)
	}

	result, err := gc.Run(context.Background(), Message{
		Role:    "user",
		Content: "hello",
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result.Rounds != 3 {
		t.Errorf("Rounds = %d, want 3", result.Rounds)
	}
	if len(result.Messages) != 4 { // initial + 3 rounds
		t.Errorf("Messages length = %d, want 4", len(result.Messages))
	}
	if result.Terminated {
		t.Error("should not be terminated")
	}
}

func TestGroupChat_Run_Terminate(t *testing.T) {
	agent1 := &mockAgent{name: "agent-1", respFn: func(ctx context.Context, msg Message) (Message, error) {
		return Message{Role: "assistant", Content: "TERMINATE"}, nil
	}}
	agent2 := &mockAgent{name: "agent-2"}

	gc, _ := NewGroupChat(GroupChatConfig{
		Agents:        []Agent{agent1, agent2},
		MaxRounds:     5,
		SelectSpeaker: RoundRobinSelector(),
	})

	result, err := gc.Run(context.Background(), Message{Role: "user", Content: "hello"})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !result.Terminated {
		t.Error("should be terminated")
	}
	if result.Rounds != 1 {
		t.Errorf("Rounds = %d, want 1", result.Rounds)
	}
}

func TestGroupChat_Run_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancel

	agent1 := &mockAgent{name: "agent-1"}
	agent2 := &mockAgent{name: "agent-2"}

	gc, _ := NewGroupChat(GroupChatConfig{
		Agents:        []Agent{agent1, agent2},
		MaxRounds:     3,
		SelectSpeaker: RoundRobinSelector(),
	})

	_, err := gc.Run(ctx, Message{Role: "user", Content: "hello"})
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
}

func TestGroupChat_Run_DefaultSelector(t *testing.T) {
	agent1 := &mockAgent{name: "agent-1"}
	agent2 := &mockAgent{name: "agent-2"}

	gc, err := NewGroupChat(GroupChatConfig{
		Agents:    []Agent{agent1, agent2},
		MaxRounds: 2,
		// no SelectSpeaker - should default to RoundRobin
	})
	if err != nil {
		t.Fatalf("NewGroupChat failed: %v", err)
	}

	result, err := gc.Run(context.Background(), Message{Role: "user", Content: "hello"})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result.Rounds != 2 {
		t.Errorf("Rounds = %d, want 2", result.Rounds)
	}
}

func TestGroupChat_Run_WithBus(t *testing.T) {
	b := bus.NewLocalMessageBus()
	agent1 := &mockAgent{name: "agent-1"}
	agent2 := &mockAgent{name: "agent-2"}

	gc, _ := NewGroupChat(GroupChatConfig{
		Agents:        []Agent{agent1, agent2},
		MaxRounds:     2,
		SelectSpeaker: RoundRobinSelector(),
		Bus:           b,
	})

	_, err := gc.Run(context.Background(), Message{Role: "user", Content: "hello"})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
}

func TestGroupChat_Run_SpeakerError(t *testing.T) {
	agent1 := &mockAgent{name: "agent-1", respFn: func(ctx context.Context, msg Message) (Message, error) {
		return Message{}, errors.New("agent failure")
	}}
	agent2 := &mockAgent{name: "agent-2"}

	gc, _ := NewGroupChat(GroupChatConfig{
		Agents:        []Agent{agent1, agent2},
		MaxRounds:     3,
		SelectSpeaker: RoundRobinSelector(),
	})

	_, err := gc.Run(context.Background(), Message{Role: "user", Content: "hello"})
	if err == nil {
		t.Fatal("expected error from speaker failure")
	}
}

// ===== Selector tests =====

func TestRoundRobinSelector(t *testing.T) {
	agents := []Agent{
		&mockAgent{name: "a"},
		&mockAgent{name: "b"},
		&mockAgent{name: "c"},
	}
	sel := RoundRobinSelector()

	s1, err := sel(context.Background(), nil, agents)
	if err != nil || s1.Name() != "a" {
		t.Errorf("first select = %v, err=%v", s1, err)
	}
	s2, _ := sel(context.Background(), nil, agents)
	if s2.Name() != "b" {
		t.Errorf("second select = %s, want b", s2.Name())
	}
	s3, _ := sel(context.Background(), nil, agents)
	if s3.Name() != "c" {
		t.Errorf("third select = %s, want c", s3.Name())
	}
	s4, _ := sel(context.Background(), nil, agents)
	if s4.Name() != "a" {
		t.Errorf("fourth select = %s, want a (wrap around)", s4.Name())
	}
}

func TestRoundRobinSelector_EmptyAgents(t *testing.T) {
	sel := RoundRobinSelector()
	_, err := sel(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("expected error for empty agents")
	}
}

func TestRandomSelector(t *testing.T) {
	agents := []Agent{
		&mockAgent{name: "a"},
		&mockAgent{name: "b"},
	}
	sel := RandomSelector()
	s, err := sel(context.Background(), nil, agents)
	if err != nil {
		t.Fatalf("RandomSelector failed: %v", err)
	}
	if s.Name() != "a" && s.Name() != "b" {
		t.Errorf("unexpected agent: %s", s.Name())
	}
}

func TestRandomSelector_EmptyAgents(t *testing.T) {
	sel := RandomSelector()
	_, err := sel(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("expected error for empty agents")
	}
}

func TestLastSpeakerSelector(t *testing.T) {
	agents := []Agent{
		&mockAgent{name: "a"},
		&mockAgent{name: "b"},
	}
	sel := LastSpeakerSelector()

	// First call with no previous messages
	s1, err := sel(context.Background(), []Message{}, agents)
	if err != nil {
		t.Fatalf("first select failed: %v", err)
	}
	if s1 == nil {
		t.Fatal("first select returned nil")
	}

	// Second call with messages that have metadata
	msgs := []Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi", Metadata: map[string]interface{}{"agent": "a"}},
	}
	s2, err := sel(context.Background(), msgs, agents)
	if err != nil {
		t.Fatalf("second select failed: %v", err)
	}
	if s2.Name() != "b" {
		t.Errorf("second select = %s, want b (next after a)", s2.Name())
	}
}

func TestLastSpeakerSelector_EmptyAgents(t *testing.T) {
	sel := LastSpeakerSelector()
	_, err := sel(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("expected error for empty agents")
	}
}

func TestRoleBasedSelector(t *testing.T) {
	agents := []Agent{
		&mockAgent{name: "coder"},
		&mockAgent{name: "reviewer"},
	}
	cfg := RoleBasedConfig{
		Roles: map[string]AgentRole{
			"coder":    {Name: "coder", Keywords: []string{"code", "implement", "write"}, Priority: 1},
			"reviewer": {Name: "reviewer", Keywords: []string{"review", "check", "test"}, Priority: 2},
		},
		DefaultRole:  "coder",
		FallbackMode: "round_robin",
	}
	sel := RoleBasedSelector(cfg)

	// Message about coding should select coder
	msgs := []Message{{Role: "user", Content: "Please write code for the feature"}}
	s, err := sel(context.Background(), msgs, agents)
	if err != nil {
		t.Fatalf("select failed: %v", err)
	}
	if s.Name() != "coder" {
		t.Errorf("select = %s, want coder", s.Name())
	}

	// Message about review should select reviewer
	msgs = []Message{{Role: "user", Content: "Please review this PR"}}
	s, err = sel(context.Background(), msgs, agents)
	if err != nil {
		t.Fatalf("select failed: %v", err)
	}
	if s.Name() != "reviewer" {
		t.Errorf("select = %s, want reviewer", s.Name())
	}
}

func TestRoleBasedSelector_EmptyAgents(t *testing.T) {
	sel := RoleBasedSelector(RoleBasedConfig{})
	_, err := sel(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("expected error for empty agents")
	}
}

func TestRoleBasedSelector_EmptyMessages(t *testing.T) {
	agents := []Agent{&mockAgent{name: "a"}, &mockAgent{name: "b"}}
	sel := RoleBasedSelector(RoleBasedConfig{})
	s, err := sel(context.Background(), nil, agents)
	if err != nil {
		t.Fatalf("select failed: %v", err)
	}
	if s.Name() != "a" {
		t.Errorf("select = %s, want a (first agent for empty messages)", s.Name())
	}
}

func TestRoleBasedSelector_FallbackRandom(t *testing.T) {
	agents := []Agent{
		&mockAgent{name: "a"},
		&mockAgent{name: "b"},
	}
	cfg := RoleBasedConfig{
		Roles: map[string]AgentRole{
			"a": {Name: "a", Keywords: []string{"special"}},
		},
		FallbackMode: "random",
	}
	sel := RoleBasedSelector(cfg)
	// No keyword match -> fallback to random
	s, err := sel(context.Background(), []Message{{Role: "user", Content: "no match"}}, agents)
	if err != nil {
		t.Fatalf("select failed: %v", err)
	}
	if s == nil {
		t.Fatal("select returned nil")
	}
}

// ===== Consensus tests =====

func TestGroupChat_RunConsensus(t *testing.T) {
	agent1 := &mockAgent{name: "agent-1", respFn: func(ctx context.Context, msg Message) (Message, error) {
		return Message{Role: "assistant", Content: "option-a"}, nil
	}}
	agent2 := &mockAgent{name: "agent-2", respFn: func(ctx context.Context, msg Message) (Message, error) {
		return Message{Role: "assistant", Content: "option-a"}, nil
	}}

	gc, _ := NewGroupChat(GroupChatConfig{
		Agents:    []Agent{agent1, agent2},
		MaxRounds: 1,
	})

	result, err := gc.RunConsensus(context.Background(), Message{
		Role:    "user",
		Content: "Which option?",
	})
	if err != nil {
		t.Fatalf("RunConsensus failed: %v", err)
	}
	if result.Decision != "option-a" {
		t.Errorf("Decision = %q, want option-a", result.Decision)
	}
	if !result.Unanimous {
		t.Error("should be unanimous")
	}
}

func TestGroupChat_RunConsensus_NoVotes(t *testing.T) {
	agent1 := &mockAgent{name: "agent-1", respFn: func(ctx context.Context, msg Message) (Message, error) {
		return Message{}, errors.New("failure")
	}}
	agent2 := &mockAgent{name: "agent-2", respFn: func(ctx context.Context, msg Message) (Message, error) {
		return Message{}, errors.New("failure")
	}}

	gc, _ := NewGroupChat(GroupChatConfig{
		Agents:    []Agent{agent1, agent2},
		MaxRounds: 1,
	})

	_, err := gc.RunConsensus(context.Background(), Message{Role: "user", Content: "question"})
	if err == nil {
		t.Fatal("expected error when no votes received")
	}
}

func TestGroupChat_RunConsensus_NotUnanimous(t *testing.T) {
	agent1 := &mockAgent{name: "agent-1", respFn: func(ctx context.Context, msg Message) (Message, error) {
		return Message{Content: "option-a"}, nil
	}}
	agent2 := &mockAgent{name: "agent-2", respFn: func(ctx context.Context, msg Message) (Message, error) {
		return Message{Content: "option-b"}, nil
	}}

	gc, _ := NewGroupChat(GroupChatConfig{
		Agents:    []Agent{agent1, agent2},
		MaxRounds: 1,
	})

	result, err := gc.RunConsensus(context.Background(), Message{Content: "question"})
	if err != nil {
		t.Fatalf("RunConsensus failed: %v", err)
	}
	if result.Unanimous {
		t.Error("should not be unanimous")
	}
}

// ===== Integration: Collaborator with timeout =====

func TestCollaborator_WithTimeout(t *testing.T) {
	b := bus.NewLocalMessageBus()
	b.Register("agent-1", func(ctx context.Context, msg *bus.BusMessage) (*bus.BusMessage, error) {
		select {
		case <-time.After(200 * time.Millisecond):
			return &bus.BusMessage{Content: "slow response"}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})

	c := NewCollaborator(b)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := c.Run(ctx, CollaborationConfig{
		Pattern:      CollabSequential,
		Participants: []string{"agent-1"},
	}, "start")
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

// ===== RoleBasedSelector 补充用例（2026-09-28 自 agent 包 group_chat_test.go 迁移）=====
// 原文件被 //go:build ignore 禁用而腐烂：其测试的 RoleBasedSelector API
// 已迁入本包。迁移时适配 collaboration.Message/Agent 类型，保留原覆盖意图。

func TestRoleBasedSelector_KeywordMatchZH(t *testing.T) {
	agents := []Agent{
		&mockAgent{name: "coder"},
		&mockAgent{name: "designer"},
	}
	cfg := RoleBasedConfig{
		Roles: map[string]AgentRole{
			"coder": {
				Name:        "代码专家",
				Description: "负责编码实现",
				Keywords:    []string{"code", "implement", "function", "API", "bug"},
				Priority:    1,
			},
			"designer": {
				Name:        "设计专家",
				Description: "负责 UI/UX 设计",
				Keywords:    []string{"UI", "design", "color", "layout", "interface"},
				Priority:    2,
			},
		},
		FallbackMode: "round_robin",
	}
	sel := RoleBasedSelector(cfg)

	selected, err := sel(context.Background(),
		[]Message{{Role: "user", Content: "Please implement the login API function"}}, agents)
	if err != nil {
		t.Fatalf("selector error = %v", err)
	}
	if selected.Name() != "coder" {
		t.Errorf("expected coder agent, got %s", selected.Name())
	}

	selected2, err := sel(context.Background(),
		[]Message{{Role: "user", Content: "Design a beautiful UI layout"}}, agents)
	if err != nil {
		t.Fatalf("selector error = %v", err)
	}
	if selected2.Name() != "designer" {
		t.Errorf("expected designer agent, got %s", selected2.Name())
	}
}

func TestRoleBasedSelector_FallbackModes(t *testing.T) {
	agents := []Agent{
		&mockAgent{name: "agent-1"},
		&mockAgent{name: "agent-2"},
	}
	cfg := RoleBasedConfig{
		Roles: map[string]AgentRole{
			"agent-1": {Name: "A1", Keywords: []string{"special"}},
		},
		FallbackMode: "random",
	}
	sel := RoleBasedSelector(cfg)
	msg := []Message{{Role: "user", Content: "generic message without keywords"}}

	for i := 0; i < 5; i++ {
		selected, err := sel(context.Background(), msg, agents)
		if err != nil {
			t.Fatalf("selector error = %v", err)
		}
		if selected == nil {
			t.Error("expected non-nil agent")
		}
	}
}
