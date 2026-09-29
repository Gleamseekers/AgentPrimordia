package orchestration

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent"
)

const (
	defaultCollabMaxRounds    = 3
	defaultVotingThreshold    = 0.6
	defaultCollabTimeout      = 5 * time.Minute
	collabEventBufferSize     = 100
	defaultCollaboratorWeight = 1.0

	reviewAgreementLevel          = 0.7
	brainstormAgreementLevel      = 0.3
	agreementBaseScore            = 0.2
	agreementParticipationWeight  = 0.8
	defaultVoteConfidence         = 0.8
	unanimityScoreThreshold       = 99.0
	suggestionSimilarityThreshold = 0.7
)

// CollaborationMode 协作模式
type CollaborationMode string

const (
	// DebateMode 辩论模式：多Agent发表不同观点并进行辩论
	DebateMode CollaborationMode = "debate"
	// ReviewMode 评审模式：Agent互相审查和改进输出
	ReviewMode CollaborationMode = "review"
	// ConsensusMode 共识模式：多Agent讨论并达成一致
	ConsensusMode CollaborationMode = "consensus"
	// BrainstormMode 头脑风暴模式：自由发散思维收集想法
	BrainstormMode CollaborationMode = "brainstorm"
)

// CollaborationConfig 协作配置
type CollaborationConfig struct {
	Mode             CollaborationMode `json:"mode"`
	Name             string            `json:"name"`
	Description      string            `json:"description,omitempty"`
	MaxRounds        int               `json:"max_rounds"`        // 最大轮次
	Timeout          time.Duration     `json:"timeout"`           // 总超时
	VotingThreshold  float64           `json:"voting_threshold"`  // 投票阈值(0-1)
	RequireUnanimity bool              `json:"require_unanimity"` // 是否要求一致同意
	EnableCritique   bool              `json:"enable_critique"`   // 是否允许批评
	SaveHistory      bool              `json:"save_history"`      // 保存完整历史
}

// Collaborator 参与者定义
type Collaborator struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Role        string      `json:"role"` // debater, reviewer, moderator, synthesizer
	Agent       agent.Agent `json:"-"`
	Perspective string      `json:"perspective,omitempty"` // 视角/立场
	Weight      float64     `json:"weight"`                // 权重(0-1)
}

// DebateRound 辩论轮次
type DebateRound struct {
	RoundNumber int                       `json:"round_number"`
	Statements  []*CollaborationStatement `json:"statements"`
	Duration    time.Duration             `json:"duration"`
}

// CollaborationStatement 声明/发言
type CollaborationStatement struct {
	ID             string         `json:"id"`
	CollaboratorID string         `json:"collaborator_id"`
	RoundNumber    int            `json:"round_number"`
	Content        string         `json:"content"`
	Type           StatementType  `json:"type"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	Timestamp      time.Time      `json:"timestamp"`
	References     []string       `json:"references,omitempty"` // 引用之前的声明
}

// StatementType 声明类型
type StatementType string

const (
	// StatementArgument 论点/主张
	StatementArgument StatementType = "argument"
	// StatementCounterargument 反驳
	StatementCounterargument StatementType = "counterargument"
	// StatementQuestion 质疑
	StatementQuestion StatementType = "question"
	// StatementSynthesis 综合/总结
	StatementSynthesis StatementType = "synthesis"
	// StatementAgreement 同意
	StatementAgreement StatementType = "agreement"
	// StatementCritique 批评/评价
	StatementCritique StatementType = "critique"
)

// Vote 投票
type Vote struct {
	CollaboratorID string  `json:"collaborator_id"`
	OptionID       string  `json:"option_id"`
	Confidence     float64 `json:"confidence"` // 置信度(0-1)
	Reasoning      string  `json:"reasoning,omitempty"`
}

// ConsensusOption 共识选项
type ConsensusOption struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Supporters  []string `json:"supporters,omitempty"`
	Score       float64  `json:"score"` // 加权得分
}

// CollaborationResult 协作结果
type CollaborationResult struct {
	SessionID    string                    `json:"session_id"`
	Mode         CollaborationMode         `json:"mode"`
	Status       CollaborationStatus       `json:"status"`
	Rounds       []*DebateRound            `json:"rounds,omitempty"`
	FinalOutcome *FinalOutcome             `json:"final_outcome,omitempty"`
	Metrics      *CollaborationMetrics     `json:"metrics"`
	History      []*CollaborationStatement `json:"history,omitempty"`
	StartTime    time.Time                 `json:"start_time"`
	EndTime      time.Time                 `json:"end_time"`
	Duration     time.Duration             `json:"duration"`
}

// FinalOutcome 最终结果
type FinalOutcome struct {
	Type           string             `json:"type"` // consensus, majority, best_effort
	Content        string             `json:"content"`
	Options        []*ConsensusOption `json:"options,omitempty"`
	Winner         *ConsensusOption   `json:"winner,omitempty"`
	AgreementLevel float64            `json:"agreement_level"` // 一致程度(0-1)
	Reasoning      string             `json:"reasoning,omitempty"`
}

// CollaborationStatus 状态
type CollaborationStatus string

const (
	CollabStatusPending    CollaborationStatus = "pending"
	CollabStatusInProgress CollaborationStatus = "in_progress"
	CollabStatusCompleted  CollaborationStatus = "completed"
	CollabStatusFailed     CollaborationStatus = "failed"
	CollabStatusCancelled  CollaborationStatus = "cancelled"
)

// CollaborationMetrics 指标
type CollaborationMetrics struct {
	TotalRounds           int           `json:"total_rounds"`
	TotalStatements       int           `json:"total_statements"`
	UniquePerspectives    int           `json:"unique_perspectives"`
	AvgStatementsPerRound float64       `json:"avg_statements_per_round"`
	ConvergenceRate       float64       `json:"convergence_rate"` // 收敛率(0-1)
	TimeToConsensus       time.Duration `json:"time_to_consensus"`
	ParticipationRate     float64       `json:"participation_rate"` // 参与率(0-1)
}

// CollaborationSession 协作会话
type CollaborationSession struct {
	mu            sync.RWMutex
	config        CollaborationConfig
	collaborators map[string]*Collaborator
	result        *CollaborationResult
	eventCh       chan *CollaborationEvent
	// currentRound/statementID 被并发 debate goroutine 读写（-race 实测发现），
	// 使用原子类型避免数据竞争
	currentRound atomic.Int64
	statementID  atomic.Int64
}

// CollaborationEvent 事件
type CollaborationEvent struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Data      any       `json:"data,omitempty"`
}

// Suggestion 建议类型
type Suggestion struct {
	Content            string
	CollaboratorID     string
	CollaboratorWeight float64
}

// NewCollaborationSession 创建协作会话
func NewCollaborationSession(config CollaborationConfig) *CollaborationSession {
	if config.MaxRounds <= 0 {
		config.MaxRounds = defaultCollabMaxRounds
	}
	if config.VotingThreshold <= 0 {
		config.VotingThreshold = defaultVotingThreshold
	}
	if config.Timeout <= 0 {
		config.Timeout = defaultCollabTimeout
	}

	return &CollaborationSession{
		config:        config,
		collaborators: make(map[string]*Collaborator),
		result: &CollaborationResult{
			SessionID: generateSessionID(),
			Mode:      config.Mode,
			Status:    CollabStatusPending,
			StartTime: time.Now(),
			Metrics:   &CollaborationMetrics{},
			History:   make([]*CollaborationStatement, 0),
		},
		eventCh: make(chan *CollaborationEvent, collabEventBufferSize),
	}
}

// AddCollaborator 添加参与者
func (s *CollaborationSession) AddCollaborator(collab *Collaborator) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if collab.ID == "" || collab.Agent == nil {
		return fmt.Errorf("invalid collaborator")
	}
	if collab.Weight <= 0 {
		collab.Weight = defaultCollaboratorWeight
	}

	s.collaborators[collab.ID] = collab
	return nil
}

// Execute 执行协作流程
func (s *CollaborationSession) Execute(ctx context.Context, topic string) (*CollaborationResult, error) {
	s.mu.Lock()
	s.result.Status = CollabStatusInProgress
	s.mu.Unlock()

	s.emitEvent("session_started", map[string]any{
		"topic": topic,
		"mode":  s.config.Mode,
	})

	var err error
	switch s.config.Mode {
	case DebateMode:
		err = s.executeDebate(ctx, topic)
	case ReviewMode:
		err = s.executeReview(ctx, topic)
	case ConsensusMode:
		err = s.executeConsensus(ctx, topic)
	case BrainstormMode:
		err = s.executeBrainstorm(ctx, topic)
	default:
		err = fmt.Errorf("unsupported collaboration mode: %s", s.config.Mode)
	}

	now := time.Now()
	s.mu.Lock()
	if err != nil {
		s.result.Status = CollabStatusFailed
	} else {
		s.result.Status = CollabStatusCompleted
	}
	s.result.EndTime = now
	s.result.Duration = now.Sub(s.result.StartTime)
	s.mu.Unlock()

	s.emitEvent("session_completed", map[string]any{
		"status":   s.result.Status,
		"duration": s.result.Duration,
	})

	return s.result, err
}

// executeDebate 执行辩论
