package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"sync"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/memory"
)

// idCounter 实例级 ID 生成器，消除全局可变状态
type idCounter struct {
	n int64
}

// next 生成唯一的记忆 ID（优化：避免 fmt.Sprintf）
func (c *idCounter) next() string {
	n := c.n
	c.n++
	ts := time.Now().UnixNano()
	return "msg_" + string(rune(ts)) + "_" + string(rune(n))
}

// SessionOption 是 NewSession 的函数式选项。
type SessionOption func(*Session)

// SessWithID 设置自定义会话 ID，不传则自动生成。
func SessWithID(id string) SessionOption {
	return func(s *Session) { s.sessionID = id }
}

// Agent 接口，用于运行消息
type Agent interface {
	Run(ctx context.Context, msg Message) (*Response, error)
}

// Message 消息结构
type Message struct {
	Role     Role
	Content  string
	Metadata Metadata
}

// Role 消息角色
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Metadata 消息元数据
type Metadata struct {
	SessionID string
}

// Response 响应结构
type Response struct {
	Content string
}

// UserMessage 创建用户消息
func UserMessage(content string) Message {
	return Message{
		Role:    RoleUser,
		Content: content,
	}
}

// Session 维护多轮对话上下文，自动追加历史到记忆。
type Session struct {
	mu        sync.RWMutex
	agent     Agent
	mem       memory.Memory
	sessionID string
	idGen     idCounter

	lastResponse *Response
	turnCount    int
	history      []Message
}

// NewSession 创建新会话。
func NewSession(agent Agent, mem memory.Memory, opts ...SessionOption) *Session {
	s := &Session{
		agent:     agent,
		mem:       mem,
		sessionID: generateSessionID(),
	}

	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Ask 发送一轮用户消息，返回 Agent 响应。
func (s *Session) Ask(ctx context.Context, userMessage string) (*Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	msg := UserMessage(userMessage)
	msg.Metadata.SessionID = s.sessionID

	resp, err := s.agent.Run(ctx, msg)
	if err != nil {
		return nil, err
	}

	// 保存交换记录到本地历史
	s.history = append(s.history, msg, Message{
		Role:    RoleAssistant,
		Content: resp.Content,
	})

	// 持久化到记忆存储
	if s.mem != nil {
		now := time.Now().UTC().Format(time.RFC3339)
		if err := s.mem.Add(ctx, &memory.Episode{
			ID:        s.idGen.next(),
			SessionID: s.sessionID,
			Role:      string(RoleUser),
			Content:   userMessage,
			CreatedAt: now,
		}); err != nil {
			slog.Warn("Session: 保存用户消息到记忆失败", "sessionID", s.sessionID, "error", err)
		}
		if err := s.mem.Add(ctx, &memory.Episode{
			ID:        s.idGen.next(),
			SessionID: s.sessionID,
			Role:      string(RoleAssistant),
			Content:   resp.Content,
			CreatedAt: now,
		}); err != nil {
			slog.Warn("Session: 保存助手回复到记忆失败", "sessionID", s.sessionID, "error", err)
		}
	}

	s.lastResponse = resp
	s.turnCount++

	return resp, nil
}

// LastResponse 返回上一轮 Agent 响应，如果没有则为 nil。
func (s *Session) LastResponse() *Response {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastResponse
}

// TurnCount 返回已完成的对话轮次数。
func (s *Session) TurnCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.turnCount
}

// History 返回当前会话的完整消息历史。
func (s *Session) History() []Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Message, len(s.history))
	copy(out, s.history)
	return out
}

// Reset 重置会话状态（不清空底层记忆存储）。
func (s *Session) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = nil
	s.lastResponse = nil
	s.turnCount = 0
}

// SessionID 返回当前会话的唯一标识。
func (s *Session) SessionID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessionID
}

func generateSessionID() string {
	return "sess_" + time.Now().UTC().Format("20060102T150405") + "_" + randomSuffix(8)
}

// randomSuffix 生成 n 字节的随机十六进制后缀。
func randomSuffix(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
