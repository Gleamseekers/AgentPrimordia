// session_history_test.go — 会话历史回读注入测试（v6.0.1 修复多轮记忆失效）
//
// 真实 LLM 复测发现：agent.Run 构建 LLM 请求时只放 [system, input]，
// 从不回读本 SessionID 下已持久化的历史消息，导致多轮对话记忆完全失效。
package agent

import (
	"context"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/memory"
)

// TestRun_SessionHistoryReadback 验证同一 SessionID 的历史消息被回读并注入 LLM 请求，
// 且历史位于当前输入之前（保持时间顺序）。
func TestRun_SessionHistoryReadback(t *testing.T) {
	mock := llm.NewMockLLM(t)
	mock.WithResponse("Nice to meet you, Alice")

	store := memory.NewInMemoryStore()
	ctx := context.Background()
	// 预置上一轮会话历史（同一 session）
	if err := store.Add(ctx, &memory.Episode{ID: "m1", SessionID: "sess-x", Role: "user", Content: "My name is Alice.", CreatedAt: "2026-01-01T00:00:01Z"}); err != nil {
		t.Fatalf("seed m1: %v", err)
	}
	if err := store.Add(ctx, &memory.Episode{ID: "m2", SessionID: "sess-x", Role: "assistant", Content: "Hello Alice!", CreatedAt: "2026-01-01T00:00:02Z"}); err != nil {
		t.Fatalf("seed m2: %v", err)
	}
	// 其他 session 的消息不应泄漏
	if err := store.Add(ctx, &memory.Episode{ID: "m9", SessionID: "sess-other", Role: "user", Content: "SECRET-OTHER-SESSION", CreatedAt: "2026-01-01T00:00:03Z"}); err != nil {
		t.Fatalf("seed m9: %v", err)
	}

	ag, err := NewAgent("hist-read", "助手", mock, WithSessionID("sess-x"))
	if err != nil {
		t.Fatalf("NewAgent 失败: %v", err)
	}
	ag.WithMemory(store)

	if _, err := ag.Run(ctx, UserMessage("What is my name?")); err != nil {
		t.Fatalf("Run 失败: %v", err)
	}

	req, ok := mock.LastRequest().(*llm.CompletionRequest)
	if !ok {
		t.Fatalf("LastRequest 类型 = %T", mock.LastRequest())
	}

	idxHistoryUser, idxHistoryAssistant, idxCurrent := -1, -1, -1
	for i, m := range req.Messages {
		switch {
		case m.Role == "user" && m.Content == "My name is Alice.":
			idxHistoryUser = i
		case m.Role == "assistant" && m.Content == "Hello Alice!":
			idxHistoryAssistant = i
		case m.Role == "user" && m.Content == "What is my name?":
			idxCurrent = i
		}
		if m.Content == "SECRET-OTHER-SESSION" {
			t.Errorf("LLM 请求泄漏了其他 session 的消息")
		}
	}
	if idxHistoryUser < 0 || idxHistoryAssistant < 0 {
		t.Fatalf("LLM 请求缺少会话历史消息, messages=%+v", req.Messages)
	}
	if idxCurrent < 0 {
		t.Fatalf("LLM 请求缺少当前输入, messages=%+v", req.Messages)
	}
	if !(idxHistoryUser < idxHistoryAssistant && idxHistoryAssistant < idxCurrent) {
		t.Errorf("历史消息应位于当前输入之前且保持时序: user=%d assistant=%d current=%d", idxHistoryUser, idxHistoryAssistant, idxCurrent)
	}
}

// TestRun_SessionHistoryReadback_Empty 验证无历史时行为不变（仅 system + input）。
func TestRun_SessionHistoryReadback_Empty(t *testing.T) {
	mock := llm.NewMockLLM(t)
	mock.WithResponse("hi")

	store := memory.NewInMemoryStore()
	ag, err := NewAgent("hist-empty", "助手", mock, WithSessionID("sess-fresh"))
	if err != nil {
		t.Fatalf("NewAgent 失败: %v", err)
	}
	ag.WithMemory(store)

	if _, err := ag.Run(context.Background(), UserMessage("hello")); err != nil {
		t.Fatalf("Run 失败: %v", err)
	}
	req := mock.LastRequest().(*llm.CompletionRequest)
	// 允许尾部含记忆上下文注入，但不应出现历史 user/assistant 对话轮
	for _, m := range req.Messages {
		if m.Role == "assistant" {
			t.Errorf("空历史时不应注入 assistant 消息: %+v", m)
		}
	}
}

// TestLoadSessionHistory_Ordering 验证回读按时间升序且截断保留最近 N 条。
func TestLoadSessionHistory_Ordering(t *testing.T) {
	mock := llm.NewMockLLM(t)
	ag, err := NewAgent("hist-order", "助手", mock, WithSessionID("s1"))
	if err != nil {
		t.Fatalf("NewAgent 失败: %v", err)
	}
	store := memory.NewInMemoryStore()
	ag.WithMemory(store)

	ctx := context.Background()
	// 乱序写入 25 条（超出回读上限），CreatedAt 决定真实顺序
	const n = 25
	for i := 0; i < n; i++ {
		ep := &memory.Episode{
			ID:        "msg_" + pad(i),
			SessionID: "s1",
			Role:      "user",
			Content:   "turn-" + pad(i),
			CreatedAt: ts(i),
		}
		if err := store.Add(ctx, ep); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	msgs, err := ag.Inner().loadSessionHistory(ctx, "s1")
	if err != nil {
		t.Fatalf("loadSessionHistory 失败: %v", err)
	}
	if len(msgs) == 0 || len(msgs) > maxSessionHistoryMessages {
		t.Fatalf("回读条数 = %d, want (0, %d]", len(msgs), maxSessionHistoryMessages)
	}
	// 时间升序
	for i := 1; i < len(msgs); i++ {
		if msgs[i-1].Content >= msgs[i].Content {
			t.Errorf("历史未按时间升序: [%d]=%s [%d]=%s", i-1, msgs[i-1].Content, i, msgs[i].Content)
		}
	}
	// 截断应保留最近的（序号最大的一条必为 turn-24）
	if got := msgs[len(msgs)-1].Content; got != "turn-24" {
		t.Errorf("最后一条 = %s, want turn-24（应保留最近的消息）", got)
	}
}

func pad(i int) string {
	if i < 10 {
		return "0" + string(rune('0'+i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}

// ts 生成递增且字典序正确的 RFC3339 时间戳（秒级）。
func ts(i int) string {
	// 2026-01-01T00:00:00Z 起，每秒递增（i<60 足够覆盖测试范围）
	return "2026-01-01T00:00:" + pad(i) + "Z"
}
