package agent

import (
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/collaboration"
	"context"
	"time"
)

// CollaborationPattern 协作模式
type CollaborationPattern = collaboration.CollaborationPattern

// CollaborationConfig 协作配置
type CollaborationConfig = collaboration.CollaborationConfig

// CollaborationResult 协作结果
type CollaborationResult = collaboration.CollaborationResult

// 协作模式常量
const (
	CollabSequential = collaboration.CollabSequential
	CollabParallel   = collaboration.CollabParallel
	CollabDebate     = collaboration.CollabDebate
	CollabReview     = collaboration.CollabReview
)

// 错误变量
var (
	ErrDebateParticipants = collaboration.ErrDebateParticipants
	ErrReviewParticipants = collaboration.ErrReviewParticipants
)

// collabAgentAdapter 将 agent.Agent 适配为 collaboration.Agent
type collabAgentAdapter struct {
	a Agent
}

func (w *collabAgentAdapter) Name() string {
	return w.a.Name()
}

func (w *collabAgentAdapter) Run(ctx context.Context, msg collaboration.Message) (collaboration.Message, error) {
	in := Message{
		Role:     Role(msg.Role),
		Content:  msg.Content,
		Metadata: Metadata{},
	}
	if msg.Metadata != nil {
		if ts, ok := msg.Metadata["timestamp"].(time.Time); ok {
			in.Metadata.Timestamp = ts
		}
		if extra, ok := msg.Metadata["extra"].(map[string]string); ok {
			in.Metadata.Extra = extra
		}
	}
	resp, err := w.a.Run(ctx, in)
	if err != nil {
		return collaboration.Message{}, err
	}
	return collaboration.Message{
		Role:    string(RoleAssistant),
		Content: resp.Content,
		Metadata: map[string]interface{}{
			"timestamp": resp.Metrics.Duration,
		},
	}, nil
}

// Collaborator 协作管理器
type Collaborator struct {
	collab *collaboration.Collaborator
}

// NewCollaborator 创建协作管理器
func NewCollaborator(bus *LocalMessageBus) *Collaborator {
	return &Collaborator{
		collab: collaboration.NewCollaborator(bus),
	}
}

// Run 运行协作
func (c *Collaborator) Run(ctx context.Context, config CollaborationConfig, input string) (*CollaborationResult, error) {
	return c.collab.Run(ctx, config, input)
}

// SpeakerSelector 发言者选择函数类型
type SpeakerSelector = collaboration.SpeakerSelector

// GroupChatConfig GroupChat 配置
type GroupChatConfig struct {
	Agents        []Agent
	MaxRounds     int
	SelectSpeaker SpeakerSelector
	Bus           MessageBus
}

// GroupChat 多 Agent 对话管理器
type GroupChat struct {
	gc *collaboration.GroupChat
}

// GroupChatResult GroupChat 运行结果
type GroupChatResult = collaboration.GroupChatResult

// NewGroupChat 创建 GroupChat 实例
func NewGroupChat(cfg GroupChatConfig) (*GroupChat, error) {
	// 适配 Agent 列表
	agents := make([]collaboration.Agent, len(cfg.Agents))
	for i, a := range cfg.Agents {
		agents[i] = &collabAgentAdapter{a: a}
	}

	// 适配 SpeakerSelector
	var selector collaboration.SpeakerSelector
	if cfg.SelectSpeaker != nil {
		selector = cfg.SelectSpeaker
	}

	gc, err := collaboration.NewGroupChat(collaboration.GroupChatConfig{
		Agents:        agents,
		MaxRounds:     cfg.MaxRounds,
		SelectSpeaker: selector,
		Bus:           cfg.Bus,
	})
	if err != nil {
		return nil, err
	}
	return &GroupChat{gc: gc}, nil
}

// Run 运行多 Agent 对话
func (g *GroupChat) Run(ctx context.Context, initialMessage Message) (*GroupChatResult, error) {
	msg := collaboration.Message{
		Role:    string(initialMessage.Role),
		Content: initialMessage.Content,
	}
	return g.gc.Run(ctx, msg)
}

// RunConsensus 运行共识决策
func (g *GroupChat) RunConsensus(ctx context.Context, question Message) (*ConsensusResult, error) {
	msg := collaboration.Message{
		Role:    string(question.Role),
		Content: question.Content,
	}
	return g.gc.RunConsensus(ctx, msg)
}

// RoundRobinSelector 轮询选择器
func RoundRobinSelector() SpeakerSelector {
	return collaboration.RoundRobinSelector()
}

// RandomSelector 随机选择器
func RandomSelector() SpeakerSelector {
	return collaboration.RandomSelector()
}

// LastSpeakerSelector 选择上一位发言者的回复者
func LastSpeakerSelector() SpeakerSelector {
	return collaboration.LastSpeakerSelector()
}

// AgentRole Agent 在 GroupChat 中的角色定义
type AgentRole = collaboration.AgentRole

// RoleBasedConfig 基于角色的配置
type RoleBasedConfig = collaboration.RoleBasedConfig

// RoleBasedSelector 基于角色/关键词的发言者选择器
func RoleBasedSelector(cfg RoleBasedConfig) SpeakerSelector {
	return collaboration.RoleBasedSelector(cfg)
}

// ConsensusResult 共识结果
type ConsensusResult = collaboration.ConsensusResult
