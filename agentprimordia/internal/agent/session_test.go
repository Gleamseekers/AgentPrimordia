
package agent

import (
	"context"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/session"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/memory"
)

func TestNewSession_AutoID(t *testing.T) {
	t.Parallel()
	mock := llm.NewMockLLM(t).WithResponse("hello")
	agent, err := NewAgent("test", "", mock, WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}

	sess := NewSession(agent, nil)
	if sess.SessionID() == "" {
		t.Fatal("sessionID should not be empty")
	}
	if !hasPrefix(sess.SessionID(), "sess_") {
		t.Fatalf("sessionID should start with sess_, got %s", sess.SessionID())
	}
}

func TestNewSession_WithCustomID(t *testing.T) {
	t.Parallel()
	mock := llm.NewMockLLM(t).WithResponse("hello")
	agent, err := NewAgent("test", "", mock, WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}

	sess := NewSession(agent, nil, SessWithID("custom-123"))
	if sess.SessionID() != "custom-123" {
		t.Fatalf("expected custom-123, got %s", sess.SessionID())
	}
}

func TestSession_Ask(t *testing.T) {
	t.Parallel()
	mock := llm.NewMockLLM(t).WithResponse("Hello, how can I help?")
	agent, err := NewAgent("test", "", mock, WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}

	sess := NewSession(agent, nil)
	resp, err := sess.Ask(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Hello, how can I help?" {
		t.Fatalf("unexpected response: %s", resp.Content)
	}
	if sess.TurnCount() != 1 {
		t.Fatalf("expected turnCount 1, got %d", sess.TurnCount())
	}
}

func TestSession_LastResponse(t *testing.T) {
	t.Parallel()
	mock := llm.NewMockLLM(t).WithResponse("first")
	agent, err := NewAgent("test", "", mock, WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}

	sess := NewSession(agent, nil)
	if sess.LastResponse() != nil {
		t.Fatal("expected nil before any Ask")
	}

	if _, err := sess.Ask(context.Background(), "hi"); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if sess.LastResponse() == nil {
		t.Fatal("expected non-nil after Ask")
	}
	if sess.LastResponse().Content != "first" {
		t.Fatalf("unexpected content: %s", sess.LastResponse().Content)
	}
}

func TestSession_MultiTurn(t *testing.T) {
	t.Parallel()
	mock := llm.NewMockLLM(t).
		WithResponse("response1").
		WithResponse("response2").
		WithResponse("response3")
	agent, err := NewAgent("test", "", mock, WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}

	sess := NewSession(agent, nil)

	expected := []string{"response1", "response2", "response3"}
	for i, exp := range expected {
		resp, err := sess.Ask(context.Background(), "msg")
		if err != nil {
			t.Fatalf("turn %d: unexpected error: %v", i, err)
		}
		if resp.Content != exp {
			t.Fatalf("turn %d: expected %s, got %s", i, exp, resp.Content)
		}
	}

	if sess.TurnCount() != 3 {
		t.Fatalf("expected 3 turns, got %d", sess.TurnCount())
	}
}

func TestSession_History(t *testing.T) {
	t.Parallel()
	mock := llm.NewMockLLM(t).
		WithResponse("resp1").
		WithResponse("resp2")
	agent, err := NewAgent("test", "", mock, WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}

	sess := NewSession(agent, nil)
	if _, err := sess.Ask(context.Background(), "q1"); err != nil {
		t.Fatalf("Ask q1: %v", err)
	}
	if _, err := sess.Ask(context.Background(), "q2"); err != nil {
		t.Fatalf("Ask q2: %v", err)
	}

	h := sess.History()
	if len(h) != 4 { // 2 user + 2 assistant
		t.Fatalf("expected 4 messages, got %d", len(h))
	}
	if h[0].Content != "q1" || h[0].Role != session.RoleUser {
		t.Fatal("first message should be user q1")
	}
	if h[1].Content != "resp1" || h[1].Role != session.RoleAssistant {
		t.Fatal("second message should be assistant resp1")
	}
	if h[2].Content != "q2" || h[2].Role != session.RoleUser {
		t.Fatal("third message should be user q2")
	}
}

func TestSession_Reset(t *testing.T) {
	t.Parallel()
	mock := llm.NewMockLLM(t).WithResponse("resp")
	agent, err := NewAgent("test", "", mock, WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}

	sess := NewSession(agent, nil)
	if _, err := sess.Ask(context.Background(), "q1"); err != nil {
		t.Fatalf("Ask q1: %v", err)
	}

	sess.Reset()

	if sess.TurnCount() != 0 {
		t.Fatalf("expected 0 after reset, got %d", sess.TurnCount())
	}
	if sess.LastResponse() != nil {
		t.Fatal("expected nil after reset")
	}
	if len(sess.History()) != 0 {
		t.Fatal("expected empty history after reset")
	}
	// SessionID should be preserved
	if sess.SessionID() == "" {
		t.Fatal("sessionID should be preserved after reset")
	}
}

func TestSession_WithMemory(t *testing.T) {
	t.Parallel()
	mem := memory.NewInMemoryStore()
	mock := llm.NewMockLLM(t).WithResponse("hello")
	agent, err := NewAgent("test", "", mock, WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}

	sess := NewSession(agent, mem, SessWithID("mem-test"))
	_, err = sess.Ask(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证记忆被持久化
	count, err := mem.Count(context.Background(), "mem-test")
	if err != nil {
		t.Fatalf("count error: %v", err)
	}
	if count != 2 { // user + assistant
		t.Fatalf("expected 2 episodes, got %d", count)
	}
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
