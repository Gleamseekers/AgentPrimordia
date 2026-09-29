package session

import (
	"context"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/memory"
)

// mockAgent 用于测试的模拟 Agent
type mockAgent struct {
	response *Response
	err      error
	calls    int
	checkCtx bool // 是否检查上下文取消
}

func (m *mockAgent) Run(ctx context.Context, msg Message) (*Response, error) {
	m.calls++
	if m.checkCtx {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

// TestUserMessage 测试创建用户消息
func TestUserMessage(t *testing.T) {
	msg := UserMessage("hello")
	if msg.Role != RoleUser {
		t.Fatalf("角色应该为 %q，实际为 %q", RoleUser, msg.Role)
	}
	if msg.Content != "hello" {
		t.Fatalf("内容应该为 %q，实际为 %q", "hello", msg.Content)
	}
}

// TestRoleConstants 测试角色常量
func TestRoleConstants(t *testing.T) {
	if RoleUser != "user" {
		t.Fatalf("RoleUser 应该为 %q，实际为 %q", "user", RoleUser)
	}
	if RoleAssistant != "assistant" {
		t.Fatalf("RoleAssistant 应该为 %q，实际为 %q", "assistant", RoleAssistant)
	}
}

// TestNewSession 测试创建会话
func TestNewSession(t *testing.T) {
	agent := &mockAgent{response: &Response{Content: "hi"}}
	mem := memory.NewInMemoryStore()

	sess := NewSession(agent, mem)
	if sess == nil {
		t.Fatal("NewSession 返回 nil")
	}
	if sess.SessionID() == "" {
		t.Fatal("会话 ID 不应该为空")
	}
	if sess.TurnCount() != 0 {
		t.Fatal("新会话轮次应该为 0")
	}
}

// TestNewSessionWithCustomID 测试自定义会话 ID
func TestNewSessionWithCustomID(t *testing.T) {
	agent := &mockAgent{response: &Response{Content: "hi"}}
	mem := memory.NewInMemoryStore()

	sess := NewSession(agent, mem, SessWithID("custom-id-123"))
	if sess.SessionID() != "custom-id-123" {
		t.Fatalf("会话 ID 应该为 %q，实际为 %q", "custom-id-123", sess.SessionID())
	}
}

// TestAsk 测试单轮对话
func TestAsk(t *testing.T) {
	agent := &mockAgent{response: &Response{Content: "world"}}
	mem := memory.NewInMemoryStore()
	sess := NewSession(agent, mem)

	resp, err := sess.Ask(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Ask 返回错误: %v", err)
	}
	if resp.Content != "world" {
		t.Fatalf("响应内容应该为 %q，实际为 %q", "world", resp.Content)
	}
	if agent.calls != 1 {
		t.Fatalf("Agent.Run 应该被调用 1 次，实际被调用 %d 次", agent.calls)
	}
}

// TestAskTurnCount 测试对话轮次计数
func TestAskTurnCount(t *testing.T) {
	agent := &mockAgent{response: &Response{Content: "ok"}}
	mem := memory.NewInMemoryStore()
	sess := NewSession(agent, mem)

	_, _ = sess.Ask(context.Background(), "msg1")
	if sess.TurnCount() != 1 {
		t.Fatalf("1 轮对话后计数应该为 1，实际为 %d", sess.TurnCount())
	}

	_, _ = sess.Ask(context.Background(), "msg2")
	if sess.TurnCount() != 2 {
		t.Fatalf("2 轮对话后计数应该为 2，实际为 %d", sess.TurnCount())
	}
}

// TestAskLastResponse 测试获取最后响应
func TestAskLastResponse(t *testing.T) {
	agent := &mockAgent{response: &Response{Content: "ok"}}
	mem := memory.NewInMemoryStore()
	sess := NewSession(agent, mem)

	if sess.LastResponse() != nil {
		t.Fatal("未对话时 LastResponse 应该为 nil")
	}

	_, _ = sess.Ask(context.Background(), "hello")
	last := sess.LastResponse()
	if last == nil {
		t.Fatal("对话后 LastResponse 不应该为 nil")
	}
	if last.Content != "ok" {
		t.Fatalf("LastResponse 内容应该为 %q，实际为 %q", "ok", last.Content)
	}
}

// TestHistory 测试消息历史
func TestHistory(t *testing.T) {
	agent := &mockAgent{response: &Response{Content: "ok"}}
	mem := memory.NewInMemoryStore()
	sess := NewSession(agent, mem)

	_, _ = sess.Ask(context.Background(), "msg1")
	_, _ = sess.Ask(context.Background(), "msg2")

	history := sess.History()
	// 每轮对话产生 2 条消息（用户 + 助手）
	if len(history) != 4 {
		t.Fatalf("2 轮对话后历史应该有 4 条消息，实际有 %d 条", len(history))
	}

	// 检查消息角色交替
	if history[0].Role != RoleUser {
		t.Fatal("第 1 条消息角色应该为 user")
	}
	if history[1].Role != RoleAssistant {
		t.Fatal("第 2 条消息角色应该为 assistant")
	}
}

// TestHistoryIsCopy 测试历史是副本
func TestHistoryIsCopy(t *testing.T) {
	agent := &mockAgent{response: &Response{Content: "ok"}}
	mem := memory.NewInMemoryStore()
	sess := NewSession(agent, mem)

	_, _ = sess.Ask(context.Background(), "hello")

	h1 := sess.History()
	h2 := sess.History()

	// 修改返回的切片不应该影响内部状态
	h1[0].Content = "modified"
	if h2[0].Content == "modified" {
		t.Fatal("修改返回的历史不应该影响内部状态")
	}
}

// TestReset 测试重置会话
func TestReset(t *testing.T) {
	agent := &mockAgent{response: &Response{Content: "ok"}}
	mem := memory.NewInMemoryStore()
	sess := NewSession(agent, mem)

	_, _ = sess.Ask(context.Background(), "hello")
	sess.Reset()

	if sess.TurnCount() != 0 {
		t.Fatal("重置后轮次应该为 0")
	}
	if sess.LastResponse() != nil {
		t.Fatal("重置后 LastResponse 应该为 nil")
	}
	if len(sess.History()) != 0 {
		t.Fatal("重置后历史应该为空")
	}
}

// TestAskError 测试 Agent 返回错误
func TestAskError(t *testing.T) {
	agent := &mockAgent{err: context.DeadlineExceeded}
	mem := memory.NewInMemoryStore()
	sess := NewSession(agent, mem)

	_, err := sess.Ask(context.Background(), "hello")
	if err == nil {
		t.Fatal("Agent 返回错误时 Ask 应该返回错误")
	}
}

// TestAskWithNilMemory 测试无记忆存储时的对话
func TestAskWithNilMemory(t *testing.T) {
	agent := &mockAgent{response: &Response{Content: "ok"}}
	sess := NewSession(agent, nil)

	resp, err := sess.Ask(context.Background(), "hello")
	if err != nil {
		t.Fatalf("无记忆存储时 Ask 不应该返回错误: %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("响应内容应该为 %q，实际为 %q", "ok", resp.Content)
	}
}

// TestNewSessionNilMemory 测试 nil Memory
func TestNewSessionNilMemory(t *testing.T) {
	agent := &mockAgent{response: &Response{Content: "hi"}}
	sess := NewSession(agent, nil)
	if sess == nil {
		t.Fatal("NewSession 传入 nil Memory 应该返回非 nil")
	}
}

// TestMessageMetadata 测试消息元数据
func TestMessageMetadata(t *testing.T) {
	msg := UserMessage("hello")
	// UserMessage 创建时 Metadata.SessionID 应该为空
	if msg.Metadata.SessionID != "" {
		t.Fatal("UserMessage 的 SessionID 应该为空")
	}
}

// TestResponse 测试响应结构
func TestResponse(t *testing.T) {
	resp := &Response{Content: "test"}
	if resp.Content != "test" {
		t.Fatalf("Content 应该为 %q，实际为 %q", "test", resp.Content)
	}
}

// TestSessionIDFormat 测试会话 ID 格式
func TestSessionIDFormat(t *testing.T) {
	agent := &mockAgent{response: &Response{Content: "ok"}}
	mem := memory.NewInMemoryStore()
	sess := NewSession(agent, mem)

	id := sess.SessionID()
	if len(id) == 0 {
		t.Fatal("会话 ID 不应该为空")
	}
	// 自动生成的 ID 应该以 "sess_" 开头
	if len(id) < 5 || id[:5] != "sess_" {
		t.Fatalf("自动生成的会话 ID 应该以 'sess_' 开头，实际为 %q", id)
	}
}

// TestMultipleAsksHistory 测试多轮对话历史完整性
func TestMultipleAsksHistory(t *testing.T) {
	agent := &mockAgent{response: &Response{Content: "ok"}}
	mem := memory.NewInMemoryStore()
	sess := NewSession(agent, mem)

	_, _ = sess.Ask(context.Background(), "first")
	_, _ = sess.Ask(context.Background(), "second")
	_, _ = sess.Ask(context.Background(), "third")

	history := sess.History()
	// 3 轮对话产生 6 条消息
	if len(history) != 6 {
		t.Fatalf("3 轮对话后历史应该有 6 条消息，实际有 %d 条", len(history))
	}

	// 验证用户消息内容
	userMsgs := []string{"first", "second", "third"}
	for i, expected := range userMsgs {
		idx := i * 2
		if history[idx].Role != RoleUser {
			t.Fatalf("第 %d 条消息角色应该为 user", idx)
		}
		if history[idx].Content != expected {
			t.Fatalf("第 %d 条消息内容应该为 %q，实际为 %q", idx, expected, history[idx].Content)
		}
	}
}

// TestContextCancellation 测试上下文取消
func TestContextCancellation(t *testing.T) {
	agent := &mockAgent{response: &Response{Content: "ok"}, checkCtx: true}
	mem := memory.NewInMemoryStore()
	sess := NewSession(agent, mem)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	_, err := sess.Ask(ctx, "hello")
	if err == nil {
		t.Fatal("上下文取消后 Ask 应该返回错误")
	}
}
