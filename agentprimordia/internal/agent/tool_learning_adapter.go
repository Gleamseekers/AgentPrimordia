// tool_learning_adapter.go — 工具学习器与 internal/memory 的适配层（v7.4 接线）
//
// 背景：ToolLearning 的 ReAct 回注钩子（react_loop_tools.go）读的是
// tool_learning.ToolLearner；但 tool_learning.MemoryStore 要求的
// Add(*tool_learning.Episode) + Query(sessionID, metadata) 与 agent 层
// MemoryStore（Add(*memory.Episode) + UpdateSummary）以及 internal/memory
// 的存储签名都不兼容，因此历史上没有任何生产构造点，回注恒不生效。
//
// 本文件提供适配器，使具备"会话列举"能力的记忆存储可直接充当
// tool_learning.MemoryStore。适配选择 List(SessionID) 而非 Search：
// 后者在 SQLite 后端走 FTS5 MATCH，空 query 不成立，无法可靠列举。
package agent

import (
	"context"

	"agentprimordia/internal/agent/tool_learning"
	"agentprimordia/internal/memory"
)

// memoryEpisodeStore 是适配所需的记忆存储最小能力集。
// internal/memory 的 SQLiteStore 与 InMemoryStore 均满足。
type memoryEpisodeStore interface {
	Add(ctx context.Context, episode *memory.Episode) error
	List(ctx context.Context, opts *memory.ListOptions) ([]*memory.Episode, error)
}

// toolLearningQueryLimit 工具学习聚合需要历史全量样本；
// memory 默认列举上限为 10，会截断统计，故显式放大。
const toolLearningQueryLimit = 1000

// toolLearningMemoryAdapter 把 internal/memory 的 Episode 存储适配为 tool_learning.MemoryStore。
type toolLearningMemoryAdapter struct {
	store memoryEpisodeStore
}

// newToolLearningMemoryAdapter 创建适配器（store 为 nil 时安全降级）。
func newToolLearningMemoryAdapter(store memoryEpisodeStore) *toolLearningMemoryAdapter {
	return &toolLearningMemoryAdapter{store: store}
}

// Add 把工具学习 Episode 转换为 memory.Episode 落库。
func (a *toolLearningMemoryAdapter) Add(ctx context.Context, ep *tool_learning.Episode) error {
	if a == nil || a.store == nil || ep == nil {
		return nil
	}
	return a.store.Add(ctx, &memory.Episode{
		ID:        ep.ID,
		SessionID: ep.SessionID,
		Role:      ep.Role,
		Content:   ep.Content,
		Metadata:  ep.Metadata,
		CreatedAt: ep.CreatedAt,
	})
}

// Query 按 sessionID + metadata 过滤列举 episodes。
// metadata 为 nil/空时只按 session 过滤（与 tool_learning.MemoryStore 契约一致）。
func (a *toolLearningMemoryAdapter) Query(ctx context.Context, sessionID string, metadata map[string]string) ([]*tool_learning.Episode, error) {
	if a == nil || a.store == nil {
		return []*tool_learning.Episode{}, nil
	}
	eps, err := a.store.List(ctx, &memory.ListOptions{
		SessionID: sessionID,
		Limit:     toolLearningQueryLimit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]*tool_learning.Episode, 0, len(eps))
	for _, ep := range eps {
		if ep == nil || !metadataMatches(ep.Metadata, metadata) {
			continue
		}
		out = append(out, &tool_learning.Episode{
			ID:        ep.ID,
			SessionID: ep.SessionID,
			Role:      ep.Role,
			Content:   ep.Content,
			Metadata:  ep.Metadata,
			CreatedAt: ep.CreatedAt,
		})
	}
	return out, nil
}

// metadataMatches 判断 filter 中每个键值对是否都存在于 actual（filter 为空视为全匹配）。
func metadataMatches(actual, filter map[string]string) bool {
	for k, v := range filter {
		if actual == nil || actual[k] != v {
			return false
		}
	}
	return true
}
