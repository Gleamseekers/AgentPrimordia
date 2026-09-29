// memory_readback.go — 长期记忆回读注入（v3.4-3）
//
// 此前 MemoryStore 只写不读（loop 仅 Add/UpdateSummary）。本文件定义可选
// 接口 MemoryQuerier 与注入逻辑：agent 的 memory store 若实现回读能力，
// 循环首轮将用户目标作为查询召回相关记忆，作为 system 消息注入 LLM。
package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/memory"
)

// MemoryQuerier 可选接口：实现者支持长期记忆回读。
// loop 首轮用目标检索相关记忆并注入，实现「跨 session 记忆召回」。
type MemoryQuerier interface {
	Search(ctx context.Context, query string, opts *memory.SearchOptions) ([]*memory.Episode, error)
}

// searchMemoryContext 以历史中的用户目标为查询，检索长期记忆并格式化为注入上下文。
// store 未实现 MemoryQuerier 或检索无结果时返回空串。
func (a *ReActAgent) searchMemoryContext(ctx context.Context, history []Message) string {
	store := a.getMemoryStore()
	q, ok := store.(MemoryQuerier)
	if !ok {
		return ""
	}
	query := extractUserInput(history)
	if query == "" {
		return ""
	}
	episodes, err := q.Search(ctx, query, nil)
	if err != nil || len(episodes) == 0 {
		return ""
	}
	return formatMemoryContext(episodes)
}

// tryMemorySolution 尝试命中"已解任务"记忆（v3.6-3 跨任务记忆 fast-path）。
//
// 当历史中存在高度相关且标记 solved=true 的记忆片段时，直接复用其答案，
// 跳过整个 ReAct 循环（0 次 LLM 调用），使相似任务第二次显著更快。
// 未命中或不确定时返回 ("", false)，走正常推理。
func (a *ReActAgent) tryMemorySolution(ctx context.Context, history []Message) (string, bool) {
	store := a.getMemoryStore()
	q, ok := store.(MemoryQuerier)
	if !ok {
		return "", false
	}
	query := extractUserInput(history)
	if query == "" {
		return "", false
	}
	episodes, err := q.Search(ctx, query, nil)
	if err != nil || len(episodes) == 0 {
		return "", false
	}
	// 扫描检索结果，取第一个标记为已解决且内容非空的片段（避免被普通消息片段抢占首位）
	for _, top := range episodes {
		if top == nil || top.Metadata == nil || top.Metadata["solved"] != "true" {
			continue
		}
		answer := top.Content
		if top.Summary != "" {
			answer = top.Summary
		}
		if answer == "" {
			continue
		}
		return answer, true
	}
	return "", false
}

// saveSolutionMemory 在 Agent 成功完成一次任务后，把任务+答案存为
// "已解决"记忆（v3.6-3 跨任务记忆真正注入）。
// 后续相似任务可通过 tryMemorySolution 命中该记忆，直接复用答案（0 轮推理）。
//
// v6.x 修复（评估报告 Issue #10）：原同步实现会在 Agent 完成最终轮时阻塞
// 响应；改成 fire-and-forget 后立即返回，让 store.Add 在背景 ctx 中跑。
func (a *ReActAgent) saveSolutionMemory(ctx context.Context, history []Message, answer string) {
	store := a.getMemoryStore()
	if store == nil || answer == "" {
		return
	}
	goal := extractUserInput(history)
	content := answer
	if goal != "" {
		content = "任务：" + goal + "\n解法：" + answer
	}
	sessionID := a.config.SessionID
	if sessionID == "" {
		sessionID = "task-solutions" // 跨任务共享的解决方案记忆桶
	}
	ep := &memory.Episode{
		ID:         "solution_" + fmt.Sprintf("%d", time.Now().UnixNano()),
		SessionID:  sessionID,
		Role:       "solution",
		Content:    content,
		Summary:    answer,
		Importance: 1.0,
		Metadata:   map[string]string{"solved": "true"},
		CreatedAt:  time.Now().Format(time.RFC3339),
	}
	// v6.x：fire-and-forget 异步写入，避免阻塞 Agent 响应。
	logger := a.logger
	bgCtx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	go func() {
		defer cancel()
		if err := store.Add(bgCtx, ep); err != nil {
			logger.Warn("保存已解记忆失败", "error", err, "session_id", sessionID)
			return
		}
		logger.Info("已解记忆已保存", "session_id", sessionID, "id", ep.ID)
	}()
	_ = ctx // 保留 ctx 参数以维持 API 向后兼容
}

// formatMemoryContext 将检索到的记忆片段格式化为注入文本。
func formatMemoryContext(episodes []*memory.Episode) string {
	if len(episodes) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("[长期记忆]\n")
	for _, ep := range episodes {
		content := ep.Content
		if len(content) > 300 {
			content = content[:300] + "..."
		}
		role := ep.Role
		if role == "" {
			role = "episode"
		}
		sb.WriteString(fmt.Sprintf("- [%s] %s\n", role, content))
	}
	return sb.String()
}

// injectMemoryContext 将记忆上下文作为 system 消息注入历史。
// 已存在 memory_context 消息时替换；否则在系统消息之后插入。
func injectMemoryContext(history []Message, memoryContext string) []Message {
	if memoryContext == "" {
		return history
	}

	memMsg := SystemMessage(memoryContext)
	if memMsg.Metadata.Extra == nil {
		memMsg.Metadata.Extra = make(map[string]string)
	}
	memMsg.Metadata.Extra["memory_context"] = "true"

	// 查找已有的记忆上下文消息并替换
	for i, m := range history {
		if m.Role == RoleSystem && m.Metadata.Extra["memory_context"] == "true" {
			history[i] = memMsg
			return history
		}
	}

	// 没有找到已有的记忆消息，在 system 消息之后插入
	systemEnd := 0
	for i, m := range history {
		if m.Role != RoleSystem {
			systemEnd = i
			break
		}
		if i == len(history)-1 {
			systemEnd = len(history)
		}
	}

	newHistory := make([]Message, 0, len(history)+1)
	newHistory = append(newHistory, history[:systemEnd]...)
	newHistory = append(newHistory, memMsg)
	newHistory = append(newHistory, history[systemEnd:]...)
	return newHistory
}
