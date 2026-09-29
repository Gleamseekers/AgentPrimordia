// Package collaboration 提供多 Agent 协作功能
package collaboration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/bus"
)

// perf-v6 round 4 Task 2：协作模式静态错误
var (
	ErrDebateParticipants = errors.New("debate requires at least 2 participants")
	ErrReviewParticipants = errors.New("review requires at least 2 participants: author + reviewer")
)

// CollaborationPattern 协作模式
type CollaborationPattern string

const (
	CollabSequential CollaborationPattern = "sequential" // 顺序
	CollabParallel   CollaborationPattern = "parallel"   // 并行
	CollabDebate     CollaborationPattern = "debate"     // 辩论
	CollabReview     CollaborationPattern = "review"     // 评审
)

// CollaborationConfig 协作配置
type CollaborationConfig struct {
	Pattern      CollaborationPattern
	Participants []string // Agent ID 列表
	MaxRounds    int      // 最大轮次（用于辩论/评审）
	Timeout      time.Duration
}

// CollaborationResult 协作结果
type CollaborationResult struct {
	Pattern  CollaborationPattern `json:"pattern"`
	Rounds   int                  `json:"rounds"`
	Outputs  map[string]string    `json:"outputs"`          // agentID -> output
	Winner   string               `json:"winner,omitempty"` // 辩论胜者
	Duration time.Duration        `json:"duration"`
}

// Collaborator 协作管理器（使用 bus.MessageBus）
type Collaborator struct {
	bus    bus.MessageBus
	logger *slog.Logger
}

// NewCollaborator 创建协作管理器
func NewCollaborator(bus bus.MessageBus) *Collaborator {
	return &Collaborator{
		bus:    bus,
		logger: slog.Default(),
	}
}

// Run 运行协作
func (c *Collaborator) Run(ctx context.Context, config CollaborationConfig, input string) (*CollaborationResult, error) {
	start := time.Now()

	switch config.Pattern {
	case CollabSequential:
		return c.runSequential(ctx, config, input, start)
	case CollabParallel:
		return c.runParallel(ctx, config, input, start)
	case CollabDebate:
		return c.runDebate(ctx, config, input, start)
	case CollabReview:
		return c.runReview(ctx, config, input, start)
	default:
		return nil, fmt.Errorf("unknown collaboration pattern: %s", config.Pattern)
	}
}

func (c *Collaborator) runSequential(ctx context.Context, config CollaborationConfig, input string, start time.Time) (*CollaborationResult, error) {
	result := &CollaborationResult{
		Pattern: CollabSequential,
		Outputs: make(map[string]string),
	}

	currentInput := input
	for i, agentID := range config.Participants {
		msg := &bus.BusMessage{
			ID:      fmt.Sprintf("collab-seq-%d", i),
			From:    "collaborator",
			To:      agentID,
			Type:    bus.BusMsgTaskRequest,
			Content: currentInput,
		}
		resp, err := c.bus.Send(ctx, msg)
		if err != nil {
			return nil, fmt.Errorf("sequential step %d failed: %w", i, err)
		}
		result.Outputs[agentID] = resp.Content
		currentInput = resp.Content
		result.Rounds = i + 1
	}

	result.Duration = time.Since(start)
	return result, nil
}

func (c *Collaborator) runParallel(ctx context.Context, config CollaborationConfig, input string, start time.Time) (*CollaborationResult, error) {
	result := &CollaborationResult{
		Pattern: CollabParallel,
		Outputs: make(map[string]string),
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	var firstErr error

	for _, agentID := range config.Participants {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			msg := &bus.BusMessage{
				ID:      fmt.Sprintf("collab-par-%s", id),
				From:    "collaborator",
				To:      id,
				Type:    bus.BusMsgTaskRequest,
				Content: input,
			}
			resp, err := c.bus.Send(ctx, msg)
			mu.Lock()
			defer mu.Unlock()
			if err != nil && firstErr == nil {
				firstErr = err
			} else if err == nil {
				result.Outputs[id] = resp.Content
			}
		}(agentID)
	}

	wg.Wait()
	result.Duration = time.Since(start)
	result.Rounds = 1
	if firstErr != nil {
		return result, firstErr
	}
	return result, nil
}

func (c *Collaborator) runDebate(ctx context.Context, config CollaborationConfig, input string, start time.Time) (*CollaborationResult, error) {
	if len(config.Participants) < 2 {
		return nil, ErrDebateParticipants // perf-v6 round 4 Task 2
	}
	if config.MaxRounds <= 0 {
		config.MaxRounds = 3
	}

	result := &CollaborationResult{
		Pattern: CollabDebate,
		Outputs: make(map[string]string),
	}

	currentInput := input
	for round := 0; round < config.MaxRounds; round++ {
		for _, agentID := range config.Participants {
			msg := &bus.BusMessage{
				ID:      fmt.Sprintf("collab-debate-%d-%s", round, agentID),
				From:    "collaborator",
				To:      agentID,
				Type:    bus.BusMsgTaskRequest,
				Content: currentInput,
			}
			resp, err := c.bus.Send(ctx, msg)
			if err != nil {
				result.Duration = time.Since(start)
				return result, fmt.Errorf("debate round %d failed: %w", round, err)
			}
			result.Outputs[agentID] = resp.Content
			currentInput = resp.Content
		}
		result.Rounds = round + 1
	}

	result.Winner = config.Participants[len(config.Participants)-1]
	result.Duration = time.Since(start)
	return result, nil
}

func (c *Collaborator) runReview(ctx context.Context, config CollaborationConfig, input string, start time.Time) (*CollaborationResult, error) {
	if len(config.Participants) < 2 {
		return nil, ErrReviewParticipants // perf-v6 round 4 Task 2
	}

	result := &CollaborationResult{
		Pattern: CollabReview,
		Outputs: make(map[string]string),
	}

	authorID := config.Participants[0]
	msg := &bus.BusMessage{
		ID:      "collab-review-author",
		From:    "collaborator",
		To:      authorID,
		Type:    bus.BusMsgTaskRequest,
		Content: input,
	}
	resp, err := c.bus.Send(ctx, msg)
	if err != nil {
		return result, err
	}
	result.Outputs[authorID] = resp.Content
	currentInput := resp.Content

	for i := 1; i < len(config.Participants); i++ {
		reviewerID := config.Participants[i]
		reviewMsg := &bus.BusMessage{
			ID:      fmt.Sprintf("collab-review-%d", i),
			From:    "collaborator",
			To:      reviewerID,
			Type:    bus.BusMsgTaskRequest,
			Content: fmt.Sprintf("Review the following:\n\n%s", currentInput),
		}
		reviewResp, err := c.bus.Send(ctx, reviewMsg)
		if err != nil {
			c.logger.Warn("评审失败", "reviewer", reviewerID, "error", err)
			continue
		}
		result.Outputs[reviewerID] = reviewResp.Content
		currentInput = reviewResp.Content
		result.Rounds = i
	}

	result.Duration = time.Since(start)
	return result, nil
}

// Message 消息结构
type Message struct {
	Role     string
	Content  string
	Metadata map[string]interface{}
}

// Agent 接口
type Agent interface {
	Name() string
	Run(ctx context.Context, msg Message) (Message, error)
}

// SpeakerSelector 发言者选择函数类型
type SpeakerSelector func(ctx context.Context, messages []Message, agents []Agent) (Agent, error)

// GroupChatConfig GroupChat 配置
type GroupChatConfig struct {
	Agents        []Agent
	MaxRounds     int
	SelectSpeaker SpeakerSelector
	Bus           bus.MessageBus
}

// GroupChat 多 Agent 对话管理器
type GroupChat struct {
	agents        []Agent
	maxRounds     int
	selectSpeaker SpeakerSelector
	bus           bus.MessageBus
	mu            sync.RWMutex
	logger        *slog.Logger
}

// GroupChatResult GroupChat 运行结果
type GroupChatResult struct {
	Messages   []Message
	AgentOrder []string
	Rounds     int
	Terminated bool
}

// NewGroupChat 创建 GroupChat 实例
func NewGroupChat(cfg GroupChatConfig) (*GroupChat, error) {
	if len(cfg.Agents) < 2 {
		return nil, errors.New("group chat requires at least 2 agents")
	}
	if cfg.MaxRounds <= 0 {
		return nil, errors.New("max rounds must be greater than 0")
	}
	if cfg.SelectSpeaker == nil {
		cfg.SelectSpeaker = RoundRobinSelector()
	}

	return &GroupChat{
		agents:        cfg.Agents,
		maxRounds:     cfg.MaxRounds,
		selectSpeaker: cfg.SelectSpeaker,
		bus:           cfg.Bus,
		logger:        slog.Default(),
	}, nil
}

// Run 运行多 Agent 对话
func (g *GroupChat) Run(ctx context.Context, initialMessage Message) (*GroupChatResult, error) {
	result := &GroupChatResult{
		Messages:   []Message{initialMessage},
		AgentOrder: make([]string, 0, g.maxRounds),
	}

	for round := 0; round < g.maxRounds; round++ {
		// 每轮检查 context 是否取消
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		speaker, err := g.selectSpeaker(ctx, result.Messages, g.agents)
		if err != nil {
			return result, fmt.Errorf("select speaker failed at round %d: %w", round, err)
		}

		lastMsg := result.Messages[len(result.Messages)-1]
		resp, err := speaker.Run(ctx, lastMsg)
		if err != nil {
			return result, fmt.Errorf("agent %s run failed at round %d: %w", speaker.Name(), round, err)
		}

		msg := Message{
			Role:    "assistant",
			Content: resp.Content,
			Metadata: map[string]interface{}{
				"timestamp": time.Now(),
				"agent":     speaker.Name(),
			},
		}
		result.Messages = append(result.Messages, msg)
		result.AgentOrder = append(result.AgentOrder, speaker.Name())
		result.Rounds = round + 1

		if g.bus != nil {
			busMsg := &bus.BusMessage{
				ID:        "groupchat-" + strconv.Itoa(round) + "-" + speaker.Name(),
				From:      speaker.Name(),
				Type:      bus.BusMsgBroadcast,
				Content:   resp.Content,
				Timestamp: time.Now(),
			}
			g.bus.Broadcast(ctx, busMsg)
		}

		if strings.Contains(resp.Content, "TERMINATE") {
			result.Terminated = true
			break
		}
	}

	return result, nil
}

// RoundRobinSelector 轮询选择器
func RoundRobinSelector() SpeakerSelector {
	var index atomic.Uint64

	return func(_ context.Context, _ []Message, agents []Agent) (Agent, error) {
		if len(agents) == 0 {
			return nil, errors.New("no agents available")
		}

		idx := index.Add(1) - 1
		return agents[idx%uint64(len(agents))], nil
	}
}

// RandomSelector 随机选择器
func RandomSelector() SpeakerSelector {
	return func(_ context.Context, _ []Message, agents []Agent) (Agent, error) {
		if len(agents) == 0 {
			return nil, errors.New("no agents available")
		}

		return agents[rand.Intn(len(agents))], nil
	}
}

// LastSpeakerSelector 选择上一位发言者的回复者
func LastSpeakerSelector() SpeakerSelector {
	var lastIndex atomic.Int64

	return func(_ context.Context, messages []Message, agents []Agent) (Agent, error) {
		if len(agents) == 0 {
			return nil, errors.New("no agents available")
		}

		if len(messages) >= 2 {
			lastMsg := messages[len(messages)-1]
			if lastMsg.Metadata != nil {
				if agentName, ok := lastMsg.Metadata["agent"].(string); ok {
					for i, a := range agents {
						if a.Name() == agentName {
							li := int64((i + 1) % len(agents))
							lastIndex.Store(li)
							return agents[li], nil
						}
					}
				}
			}
		}

		li := lastIndex.Load()
		agent := agents[li%int64(len(agents))]
		lastIndex.Store((li + 1) % int64(len(agents)))
		return agent, nil
	}
}

// AgentRole Agent 在 GroupChat 中的角色定义
type AgentRole struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Keywords    []string `json:"keywords,omitempty"`
	Priority    int      `json:"priority"` // 越小优先级越高
}

// RoleBasedConfig 基于角色的配置
type RoleBasedConfig struct {
	Roles        map[string]AgentRole
	DefaultRole  string
	FallbackMode string // "round_robin" | "random"
}

// loweredRole 预计算 lowered 后的角色
type loweredRole struct {
	Role            AgentRole
	LoweredKeywords []string
}

// roleCache 简单的角色 lowered 缓存
var roleCache sync.Map

// getLoweredRole 获取预 lowered 后的角色
func getLoweredRole(agentName string, cfg RoleBasedConfig) (*loweredRole, bool) {
	role, exists := cfg.Roles[agentName]
	if !exists {
		return nil, false
	}
	cacheKey := agentName
	if v, ok := roleCache.Load(cacheKey); ok {
		lr := v.(*loweredRole)
		if lr.Role.Priority == role.Priority && len(lr.Role.Keywords) == len(role.Keywords) {
			return lr, true
		}
	}
	lr := &loweredRole{Role: role}
	lr.LoweredKeywords = make([]string, len(role.Keywords))
	for i, kw := range role.Keywords {
		lr.LoweredKeywords[i] = strings.ToLower(kw)
	}
	roleCache.Store(cacheKey, lr)
	return lr, true
}

// RoleBasedSelector 基于角色/关键词的发言者选择器
func RoleBasedSelector(cfg RoleBasedConfig) SpeakerSelector {
	return func(ctx context.Context, messages []Message, agents []Agent) (Agent, error) {
		if len(agents) == 0 {
			return nil, errors.New("no agents available")
		}

		if len(messages) == 0 {
			return agents[0], nil
		}

		lastMsg := messages[len(messages)-1]
		content := strings.ToLower(lastMsg.Content)

		bestMatch := -1
		bestScore := 0

		for i, a := range agents {
			roleLower, exists := getLoweredRole(a.Name(), cfg)
			if !exists {
				continue
			}
			score := 0
			for _, kw := range roleLower.LoweredKeywords {
				if strings.Contains(content, kw) {
					score++
				}
			}
			if score > bestScore {
				bestScore = score
				bestMatch = i
			} else if score == bestScore && score > 0 && bestMatch >= 0 {
				curRole, _ := getLoweredRole(agents[bestMatch].Name(), cfg)
				if curRole != nil && roleLower.Role.Priority < curRole.Role.Priority {
					bestMatch = i
				}
			}
		}

		if bestMatch >= 0 && bestScore > 0 {
			return agents[bestMatch], nil
		}

		switch cfg.FallbackMode {
		case "random":
			return agents[rand.Intn(len(agents))], nil
		default:
			return agents[len(messages)%len(agents)], nil
		}
	}
}

// ConsensusResult 共识结果
type ConsensusResult struct {
	Decision  string            `json:"decision"`
	Votes     map[string]string `json:"votes"`
	Winner    string            `json:"winner"`
	Unanimous bool              `json:"unanimous"`
}

// RunConsensus 运行共识决策
func (g *GroupChat) RunConsensus(ctx context.Context, question Message) (*ConsensusResult, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	votes := make(map[string]string)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, agent := range g.agents {
		wg.Add(1)
		go func(a Agent) {
			defer wg.Done()
			resp, err := a.Run(ctx, question)
			if err != nil {
				g.logger.Warn("consensus agent failed", "agent", a.Name(), "error", err)
				return
			}
			mu.Lock()
			votes[a.Name()] = resp.Content
			mu.Unlock()
		}(agent)
	}

	wg.Wait()

	if len(votes) == 0 {
		return nil, errors.New("no votes received")
	}

	// 统计投票
	voteCount := make(map[string]int)
	for _, vote := range votes {
		voteCount[vote]++
	}

	var winner string
	maxVotes := 0
	for decision, count := range voteCount {
		if count > maxVotes {
			maxVotes = count
			winner = decision
		}
	}

	unanimous := maxVotes == len(g.agents)

	return &ConsensusResult{
		Decision:  winner,
		Votes:     votes,
		Winner:    winner,
		Unanimous: unanimous,
	}, nil
}
