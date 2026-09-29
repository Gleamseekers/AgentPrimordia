package orchestration

// 本文件从 collaboration.go 拆分而来，包含各协作模式（Debate/Review/Consensus/Brainstorm）
// 的执行逻辑和结果综合方法。

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent"
)

func (s *CollaborationSession) executeDebate(ctx context.Context, topic string) error {
	for round := 1; round <= s.config.MaxRounds; round++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		s.currentRound.Store(int64(round))
		debateRound := &DebateRound{
			RoundNumber: round,
			Statements:  make([]*CollaborationStatement, 0),
		}

		roundStart := time.Now()

		// 每个参与者发言
		var wg sync.WaitGroup
		stmtCh := make(chan *CollaborationStatement, len(s.collaborators))

		for _, collab := range s.collaborators {
			wg.Add(1)
			go func(c *Collaborator) {
				defer wg.Done()

				prompt := buildDebatePrompt(topic, c.Perspective, round, s.getPreviousStatements())
				resp, agentErr := c.Agent.Run(ctx, agent.UserMessage(prompt))
				if agentErr != nil {
					return
				}

				stmt := &CollaborationStatement{
					ID:             s.generateStatementID(),
					CollaboratorID: c.ID,
					RoundNumber:    round,
					Content:        resp.Content,
					Type:           StatementArgument,
					Timestamp:      time.Now(),
				}

				if round > 1 && s.config.EnableCritique {
					stmt.Type = StatementCounterargument
				}

				stmtCh <- stmt
			}(collab)
		}

		wg.Wait()
		close(stmtCh)

		for stmt := range stmtCh {
			debateRound.Statements = append(debateRound.Statements, stmt)
			s.addToHistory(stmt)
		}

		debateRound.Duration = time.Since(roundStart)
		s.mu.Lock()
		s.result.Rounds = append(s.result.Rounds, debateRound)
		s.result.Metrics.TotalRounds++
		s.mu.Unlock()

		s.emitEvent("round_completed", map[string]any{
			"round":      round,
			"statements": len(debateRound.Statements),
		})
	}

	// 综合辩论结果
	s.synthesizeDebateResult()

	return nil
}

// executeReview 执行评审
// 优化（perf-v2）：使用 channel 替代 mutex-protected append，减少锁竞争
func (s *CollaborationSession) executeReview(ctx context.Context, content string) error {
	// 检查上下文是否已取消
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	reviewers := s.getCollaboratorsByRole("reviewer")

	if len(reviewers) == 0 {
		reviewers = s.getAllCollaborators()
	}

	// 优化（perf-v2）：使用 channel 替代 mutex-protected append
	reviewCh := make(chan *CollaborationStatement, len(reviewers))
	var wg sync.WaitGroup

	for _, reviewer := range reviewers {
		wg.Add(1)
		go func(r *Collaborator) {
			defer wg.Done()

			prompt := buildReviewPrompt(content, r.Perspective)
			resp, err := r.Agent.Run(ctx, agent.UserMessage(prompt))
			if err != nil {
				return
			}

			review := &CollaborationStatement{
				ID:             s.generateStatementID(),
				CollaboratorID: r.ID,
				RoundNumber:    1,
				Content:        resp.Content,
				Type:           StatementCritique,
				Timestamp:      time.Now(),
			}

			reviewCh <- review
			s.addToHistory(review)
		}(reviewer)
	}

	wg.Wait()
	close(reviewCh)

	reviews := make([]*CollaborationStatement, 0, len(reviewers))
	for review := range reviewCh {
		reviews = append(reviews, review)
	}

	round := &DebateRound{
		RoundNumber: 1,
		Statements:  reviews,
	}

	s.mu.Lock()
	s.result.Rounds = []*DebateRound{round}
	s.result.Metrics.TotalRounds = 1
	s.mu.Unlock()

	// 综合评审结果
	s.synthesizeReviewResult(reviews, content)

	return nil
}

// executeConsensus 执行共识达成
func (s *CollaborationSession) executeConsensus(ctx context.Context, topic string) error {
	options := make([]*ConsensusOption, 0)

	for round := 1; round <= s.config.MaxRounds; round++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// 收集建议
		suggestions := s.collectSuggestions(ctx, topic, round)

		// 创建或更新选项
		if round == 1 && len(options) == 0 {
			for _, sug := range suggestions {
				option := &ConsensusOption{
					ID:          "option-" + strconv.Itoa(len(options)+1),
					Description: sug.Content,
					Supporters:  []string{sug.CollaboratorID},
					Score:       sug.CollaboratorWeight,
				}
				options = append(options, option)
			}
		} else if round > 1 {
			s.mergeSuggestionsIntoOptions(suggestions, options)
		}

		// 投票
		votes := s.conductVoting(ctx, options, round)
		s.updateOptionScores(options, votes)

		// 检查是否达成共识
		if s.checkConsensusReached(options) {
			break
		}

		// 如果不是最后一轮，进行讨论
		if round < s.config.MaxRounds {
			s.facilitateDiscussion(ctx, options, votes, round)
		}
	}

	// 确定最终结果
	s.determineConsensusWinner(options)

	return nil
}

// executeBrainstorm 执行头脑风暴
// 优化（perf-v2）：使用 channel 替代 mutex-protected append
func (s *CollaborationSession) executeBrainstorm(ctx context.Context, topic string) error {
	// 检查上下文是否已取消
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// 优化（perf-v2）：使用 channel 替代 mutex-protected append
	ideaCh := make(chan *CollaborationStatement, len(s.collaborators))
	var wg sync.WaitGroup

	for _, collab := range s.collaborators {
		wg.Add(1)
		go func(c *Collaborator) {
			defer wg.Done()

			prompt := buildBrainstormPrompt(topic, c.Perspective)
			resp, err := c.Agent.Run(ctx, agent.UserMessage(prompt))
			if err != nil {
				return
			}

			idea := &CollaborationStatement{
				ID:             s.generateStatementID(),
				CollaboratorID: c.ID,
				RoundNumber:    1,
				Content:        resp.Content,
				Type:           StatementArgument,
				Timestamp:      time.Now(),
			}

			ideaCh <- idea
			s.addToHistory(idea)
		}(collab)
	}

	wg.Wait()
	close(ideaCh)

	ideas := make([]*CollaborationStatement, 0, len(s.collaborators))
	for idea := range ideaCh {
		ideas = append(ideas, idea)
	}

	round := &DebateRound{
		RoundNumber: 1,
		Statements:  ideas,
	}

	s.mu.Lock()
	s.result.Rounds = []*DebateRound{round}
	s.result.Metrics.TotalRounds = 1
	s.mu.Unlock()

	// 综合头脑风暴结果
	s.synthesizeBrainstormResult(ideas)

	return nil
}

// ===== 辅助方法 =====

// getPreviousStatements 获取之前的声明历史。
// 优化（perf-v2）：返回只读引用而非深拷贝，因为历史仅追加不修改。
// 调用者不得修改返回的 slice。
func (s *CollaborationSession) getPreviousStatements() []*CollaborationStatement {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// 返回底层 slice 的引用，因为 history 只追加不修改，并发读安全
	return s.result.History
}

func (s *CollaborationSession) addToHistory(stmt *CollaborationStatement) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.result.History = append(s.result.History, stmt)
	s.result.Metrics.TotalStatements++
}

func (s *CollaborationSession) generateStatementID() string {
	id := s.statementID.Add(1)
	return "stmt-" + strconv.Itoa(int(s.currentRound.Load())) + "-" + strconv.Itoa(int(id))
}

func (s *CollaborationSession) getAllCollaborators() []*Collaborator {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]*Collaborator, 0, len(s.collaborators))
	for _, c := range s.collaborators {
		list = append(list, c)
	}
	return list
}

func (s *CollaborationSession) getCollaboratorsByRole(role string) []*Collaborator {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]*Collaborator, 0)
	for _, c := range s.collaborators {
		if c.Role == role {
			list = append(list, c)
		}
	}
	return list
}

func (s *CollaborationSession) emitEvent(eventType string, data any) {
	select {
	case s.eventCh <- &CollaborationEvent{
		Type:      eventType,
		Timestamp: time.Now(),
		Data:      data,
	}:
	default:
	}
}

// Events 返回事件通道
func (s *CollaborationSession) Events() <-chan *CollaborationEvent {
	return s.eventCh
}

// GetResult 获取结果
func (s *CollaborationSession) GetResult() *CollaborationResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.result
}

// Export 导出为JSON（perf-v5 Task 5：锁内只快照，锁外 marshal）
func (s *CollaborationSession) Export() ([]byte, error) {
	s.mu.RLock()
	data := map[string]any{
		"config":        s.config,
		"result":        s.result,
		"collaborators": s.collaborators,
	}
	s.mu.RUnlock()
	return json.MarshalIndent(data, "", "  ")
}

// synthesizeDebateResult 综合辩论结果
func (s *CollaborationSession) synthesizeDebateResult() {
	s.mu.Lock()
	defer s.mu.Unlock()

	allStatements := make([]string, 0)
	perspectives := make(map[string]bool)

	for _, round := range s.result.Rounds {
		for _, stmt := range round.Statements {
			allStatements = append(allStatements, stmt.Content)
			if collab, exists := s.collaborators[stmt.CollaboratorID]; exists {
				perspectives[collab.Perspective] = true
			}
		}
	}

	s.result.FinalOutcome = &FinalOutcome{
		Type:           "debate_summary",
		Content:        strings.Join(allStatements, "\n\n"),
		AgreementLevel: s.calculateAgreementLevel(),
		Reasoning:      "经过 " + strconv.Itoa(s.result.Metrics.TotalRounds) + " 轮辩论，收集了 " + strconv.Itoa(len(perspectives)) + " 个观点",
	}

	s.result.Metrics.UniquePerspectives = len(perspectives)
	if s.result.Metrics.TotalRounds > 0 {
		s.result.Metrics.AvgStatementsPerRound = float64(s.result.Metrics.TotalStatements) / float64(s.result.Metrics.TotalRounds)
	}
}

// synthesizeReviewResult 综合评审结果
func (s *CollaborationSession) synthesizeReviewResult(reviews []*CollaborationStatement, originalContent string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	feedback := make([]string, 0)
	for _, review := range reviews {
		feedback = append(feedback, review.Content)
	}

	s.result.FinalOutcome = &FinalOutcome{
		Type:           "review_summary",
		Content:        strings.Join(feedback, "\n\n---\n\n"),
		Reasoning:      "基于 " + strconv.Itoa(len(reviews)) + " 位评审者的反馈",
		AgreementLevel: reviewAgreementLevel,
	}
}

// determineConsensusWinner 确定共识获胜者
func (s *CollaborationSession) determineConsensusWinner(options []*ConsensusOption) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(options) == 0 {
		s.result.FinalOutcome = &FinalOutcome{
			Type:    "no_consensus",
			Content: "未能达成共识",
		}
		return
	}

	sort.Slice(options, func(i, j int) bool {
		return options[i].Score > options[j].Score
	})

	winner := options[0]

	consensusType := "majority"
	if winner.Score >= s.config.VotingThreshold*100 {
		consensusType = "consensus"
	}

	s.result.FinalOutcome = &FinalOutcome{
		Type:           consensusType,
		Content:        winner.Description,
		Options:        options,
		Winner:         winner,
		AgreementLevel: winner.Score / 100.0,
		Reasoning:      "选项 '" + winner.Description + "' 以 " + strconv.FormatFloat(winner.Score, 'f', 1, 64) + "% 的支持率胜出",
	}
}

// synthesizeBrainstormResult 综合头脑风暴结果
func (s *CollaborationSession) synthesizeBrainstormResult(ideas []*CollaborationStatement) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ideaList := make([]string, 0)
	for _, idea := range ideas {
		ideaList = append(ideaList, idea.Content)
	}

	s.result.FinalOutcome = &FinalOutcome{
		Type:           "brainstorm_collection",
		Content:        strings.Join(ideaList, "\n\n💡 "),
		Reasoning:      "收集了 " + strconv.Itoa(len(ideas)) + " 个创意想法",
		AgreementLevel: brainstormAgreementLevel,
	}
}

// calculateAgreementLevel 计算一致性水平
func (s *CollaborationSession) calculateAgreementLevel() float64 {
	if len(s.result.History) == 0 {
		return 0.0
	}

	lastRoundStmts := 0
	for _, stmt := range s.result.History {
		if stmt.RoundNumber == int(s.currentRound.Load()) {
			lastRoundStmts++
		}
	}

	if lastRoundStmts == 0 {
		return 0.5
	}

	// 简化计算：基于参与率和声明类型分布
	participationRate := float64(lastRoundStmts) / float64(len(s.collaborators))
	if participationRate > 1.0 {
		participationRate = 1.0
	}

	return participationRate*agreementParticipationWeight + agreementBaseScore
}

// collectSuggestions 收集建议
// 优化（perf-v2）：使用 channel 替代 mutex-protected append
func (s *CollaborationSession) collectSuggestions(ctx context.Context, topic string, round int) []Suggestion {
	// 优化（perf-v2）：使用带缓冲的 channel 避免 mutex 竞争
	sugCh := make(chan Suggestion, len(s.collaborators))
	var wg sync.WaitGroup

	for _, collab := range s.collaborators {
		wg.Add(1)
		go func(c *Collaborator) {
			defer wg.Done()

			prompt := buildConsensusPrompt(topic, round, s.getPreviousStatements())
			resp, err := c.Agent.Run(ctx, agent.UserMessage(prompt))
			if err != nil {
				return
			}

			sugCh <- Suggestion{
				Content:            resp.Content,
				CollaboratorID:     c.ID,
				CollaboratorWeight: c.Weight,
			}
		}(collab)
	}

	wg.Wait()
	close(sugCh)

	suggestions := make([]Suggestion, 0, len(s.collaborators))
	for sug := range sugCh {
		suggestions = append(suggestions, sug)
	}
	return suggestions
}

// conductVoting 进行投票
// 优化（perf-v2）：使用 channel 替代 mutex-protected append
func (s *CollaborationSession) conductVoting(ctx context.Context, options []*ConsensusOption, round int) []*Vote {
	// 优化（perf-v2）：使用带缓冲的 channel 避免 mutex 竞争
	voteCh := make(chan *Vote, len(s.collaborators))
	var wg sync.WaitGroup

	for _, collab := range s.collaborators {
		wg.Add(1)
		go func(c *Collaborator) {
			defer wg.Done()

			prompt := buildVotingPrompt(options, round)
			resp, err := c.Agent.Run(ctx, agent.UserMessage(prompt))
			if err != nil {
				return
			}

			selectedOption := parseVoteSelection(resp.Content, options)
			voteCh <- &Vote{
				CollaboratorID: c.ID,
				OptionID:       selectedOption.ID,
				Confidence:     defaultVoteConfidence,
				Reasoning:      resp.Content,
			}
		}(collab)
	}

	wg.Wait()
	close(voteCh)

	votes := make([]*Vote, 0, len(s.collaborators))
	for vote := range voteCh {
		votes = append(votes, vote)
	}
	return votes
}

// updateOptionScores 更新选项得分
func (s *CollaborationSession) updateOptionScores(options []*ConsensusOption, votes []*Vote) {
	for _, opt := range options {
		opt.Score = 0
		opt.Supporters = make([]string, 0)
	}

	totalWeight := 0.0
	for _, vote := range votes {
		for _, opt := range options {
			if opt.ID == vote.OptionID {
				collabWeight := 1.0
				if c, ok := s.collaborators[vote.CollaboratorID]; ok {
					collabWeight = c.Weight
				}
				opt.Score += vote.Confidence * collabWeight * 100
				opt.Supporters = append(opt.Supporters, vote.CollaboratorID)
				totalWeight += collabWeight
			}
		}
	}

	// 归一化得分
	if totalWeight > 0 {
		for _, opt := range options {
			opt.Score = (opt.Score / totalWeight) * 100
		}
	}
}

// checkConsensusReached 检查是否达成共识
func (s *CollaborationSession) checkConsensusReached(options []*ConsensusOption) bool {
	if len(options) == 0 {
		return false
	}

	sort.Slice(options, func(i, j int) bool {
		return options[i].Score > options[j].Score
	})

	topOption := options[0]

	if s.config.RequireUnanimity {
		return topOption.Score >= unanimityScoreThreshold
	}

	return topOption.Score >= s.config.VotingThreshold*100
}

// facilitateDiscussion 促进讨论
func (s *CollaborationSession) facilitateDiscussion(ctx context.Context, options []*ConsensusOption, votes []*Vote, round int) {
	discussionPrompt := buildDiscussionPrompt(options, votes, round)

	var wg sync.WaitGroup
	for _, collab := range s.collaborators {
		wg.Add(1)
		go func(c *Collaborator) {
			defer wg.Done()

			resp, err := c.Agent.Run(ctx, agent.UserMessage(discussionPrompt))
			if err != nil {
				return
			}

			stmt := &CollaborationStatement{
				ID:             s.generateStatementID(),
				CollaboratorID: c.ID,
				RoundNumber:    round,
				Content:        resp.Content,
				Type:           StatementSynthesis,
				Timestamp:      time.Now(),
			}
			s.addToHistory(stmt)
		}(collab)
	}
	wg.Wait()
}

// mergeSuggestionsIntoOptions 合并建议到选项
// perf-v4 Task 5：预计算 option 的词频表，避免每次相似度比较时重复 make(map[string]int)
func (s *CollaborationSession) mergeSuggestionsIntoOptions(suggestions []Suggestion, options []*ConsensusOption) {
	// 预计算所有 option 的词频表（option 数量较少，map 可重复利用）
	optionTokenMaps := make([]map[string]int, len(options))
	optionWordCounts := make([]int, len(options))
	for i, opt := range options {
		words := strings.Fields(opt.Description)
		optionWordCounts[i] = len(words)
		optionTokenMaps[i] = wordFrequency(opt.Description)
	}

	for _, sug := range suggestions {
		sugWords := strings.Fields(sug.Content)
		sugTokens := wordFrequency(sug.Content)
		sugLen := len(sugWords)
		matched := false
		for i, opt := range options {
			if similarityScorePrecomputed(optionTokenMaps[i], optionWordCounts[i], sugTokens, sugLen) > suggestionSimilarityThreshold {
				opt.Supporters = append(opt.Supporters, sug.CollaboratorID)
				matched = true
				break
			}
		}
		if !matched {
			newOpt := &ConsensusOption{
				ID:          "option-" + strconv.Itoa(len(options)+1),
				Description: sug.Content,
				Supporters:  []string{sug.CollaboratorID},
			}
			options = append(options, newOpt)
			// 为新创建的 option 补齐预计算结果，保持后续循环可继续利用
			optionTokenMaps = append(optionTokenMaps, sugTokens)
			optionWordCounts = append(optionWordCounts, sugLen)
		}
	}
}

// similarityScore 计算相似度（简化版）
