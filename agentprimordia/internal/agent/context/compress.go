// compress.go — 智能上下文压缩策略
package context

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/core"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
)

// CompressConfig 压缩配置
type CompressConfig struct {
	// MaxTokens 压缩后最大 Token 数（估算）
	MaxTokens int
	// SummaryModel 用于摘要的 LLM Provider
	SummaryModel llm.Provider
	// KeepSystemMessages 是否保留所有系统消息
	KeepSystemMessages bool
	// KeepRecentN 保留最近 N 条消息不压缩
	KeepRecentN int
	// CompressRatio 压缩比例（0.3 = 保留 30% 的 Token）
	CompressRatio float64
}

// CompressStrategy 智能压缩策略
type CompressStrategy struct {
	config CompressConfig
	logger *slog.Logger
}

// NewCompressStrategy 创建压缩策略
func NewCompressStrategy(config CompressConfig) *CompressStrategy {
	if config.KeepRecentN <= 0 {
		config.KeepRecentN = 2
	}
	if config.CompressRatio <= 0 {
		config.CompressRatio = 0.3
	}
	return &CompressStrategy{
		config: config,
		logger: slog.Default(),
	}
}

// Trim 实现 Strategy 接口
func (s *CompressStrategy) Trim(messages []core.Message, maxMessages int) []core.Message {
	if len(messages) == 0 {
		return messages
	}

	effectiveMax := maxMessages
	if effectiveMax <= 0 {
		effectiveMax = 20
	}

	if len(messages) <= effectiveMax {
		return messages
	}

	var systemMsgs []core.Message
	var recentMsgs []core.Message
	var oldMsgs []core.Message

	for _, m := range messages {
		if m.Role == core.RoleSystem {
			if s.config.KeepSystemMessages {
				systemMsgs = append(systemMsgs, m)
			}
		}
	}

	nonSystem := make([]core.Message, 0)
	for _, m := range messages {
		if m.Role != core.RoleSystem {
			nonSystem = append(nonSystem, m)
		}
	}

	keepN := s.config.KeepRecentN
	if keepN > len(nonSystem) {
		keepN = len(nonSystem)
	}

	recentMsgs = nonSystem[len(nonSystem)-keepN:]
	oldMsgs = nonSystem[:len(nonSystem)-keepN]

	if len(oldMsgs) == 0 {
		result := make([]core.Message, 0, len(systemMsgs)+len(recentMsgs))
		result = append(result, systemMsgs...)
		result = append(result, recentMsgs...)
		return result
	}

	var summary string
	if s.config.SummaryModel != nil {
		var err error
		// 使用带超时的 context 防止 LLM 调用无限阻塞
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		summary, err = s.compressOldMessages(ctx, oldMsgs)
		if err != nil {
			s.logger.Warn("摘要压缩失败，降级为简单截断", "error", err)
			summary = s.fallbackSummary(oldMsgs)
		}
	} else {
		summary = s.fallbackSummary(oldMsgs)
	}

	result := make([]core.Message, 0, len(systemMsgs)+1+len(recentMsgs))
	result = append(result, systemMsgs...)
	result = append(result, core.SystemMessage("[对话摘要]\n"+summary))
	result = append(result, recentMsgs...)

	return result
}

// compressOldMessages 压缩旧消息为摘要
func (s *CompressStrategy) compressOldMessages(ctx context.Context, old []core.Message) (string, error) {
	var sb strings.Builder
	for _, m := range old {
		role := string(m.Role)
		content := m.TextContent()
		if content == "" {
			continue
		}
		sb.WriteString(fmt.Sprintf("%s: %s\n", role, content))
	}

	prompt := "请将以下对话历史压缩为简洁摘要，保留关键信息、决策和结论：\n\n" + sb.String()

	resp, err := s.config.SummaryModel.Complete(ctx, &llm.CompletionRequest{
		Messages: []llm.ChatMessage{
			{Role: "system", Content: "你是一个对话摘要助手，擅长提取关键信息。"},
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return "", err
	}

	return resp.Content, nil
}

// fallbackSummary 无 LLM 时的降级摘要（取首尾各一条）
func (s *CompressStrategy) fallbackSummary(old []core.Message) string {
	if len(old) == 0 {
		return ""
	}
	if len(old) == 1 {
		return fmt.Sprintf("%s: %s", old[0].Role, old[0].TextContent())
	}

	first := old[0]
	last := old[len(old)-1]
	return fmt.Sprintf("%s: %s\n...\n%s: %s",
		first.Role, first.TextContent(),
		last.Role, last.TextContent())
}

// EstimateTokens 估算消息的 Token 数（简单启发式：1 Token ≈ 4 字符）
func EstimateTokens(messages []core.Message) int {
	total := 0
	for _, m := range messages {
		total += len(m.TextContent())
	}
	return total / 4
}
