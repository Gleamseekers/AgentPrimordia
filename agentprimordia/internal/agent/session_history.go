// session_history.go — 会话历史回读注入（v6.0.1 修复多轮记忆失效）
//
// 真实 LLM 复测发现：reactLoopEngine 构建 LLM 请求时只放 [system, input]，
// 从不回读本 SessionID 下已持久化的历史消息，导致多轮对话中 Agent
// 无法记住上一轮说过的内容（记忆只写不读）。
// 本文件通过可选接口 MemoryLister 按会话回读最近历史，注入到当前输入之前。
package agent

import (
	"context"
	"sort"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/memory"
)

// maxSessionHistoryMessages 会话历史回读上限（条）。
// 与 defaultMaxHistoryMessages（滑动窗口）保持同一量级，避免长会话 token 爆炸。
const maxSessionHistoryMessages = 20

// MemoryLister 可选接口：实现者支持按会话列出记忆条目。
// 回读会话历史依赖此能力；store 未实现时静默跳过（向后兼容）。
type MemoryLister interface {
	List(ctx context.Context, opts *memory.ListOptions) ([]*memory.Episode, error)
}

// resolveSessionID 计算记忆读写使用的会话 ID，回退链：
// 消息显式携带 > 本次运行解析值（memSessionID）> Agent 配置 > Agent 名称。
// Session.Ask 路径下历史由 Session 以消息级 SessionID 写入，故需优先取 input。
func (a *ReActAgent) resolveSessionID(input Message) string {
	if input.Metadata.SessionID != "" {
		return input.Metadata.SessionID
	}
	// P1-2：经访问器在 mu 保护下读取，避免与并发 Run 的入口写入竞争
	if id := a.getMemSessionID(); id != "" {
		return id
	}
	if a.config.SessionID != "" {
		return a.config.SessionID
	}
	return a.config.Name
}

// loadSessionHistory 回读指定会话最近 maxSessionHistoryMessages 条
// user/assistant 历史消息，按时间升序返回。
// 无记忆 store、store 不支持 List 或查询失败时返回空。
func (a *ReActAgent) loadSessionHistory(ctx context.Context, sessionID string) ([]Message, error) {
	store := a.getMemoryStore()
	lister, ok := store.(MemoryLister)
	if !ok {
		return nil, nil
	}
	eps, err := lister.List(ctx, &memory.ListOptions{
		SessionID: sessionID,
		Limit:     maxSessionHistoryMessages,
		Ascending: false, // 取最近 N 条（倒序），下面再翻正
	})
	if err != nil {
		return nil, err
	}
	// 按 CreatedAt 升序（store 实现可能无序，统一在此排序保证确定性）
	sort.SliceStable(eps, func(i, j int) bool { return eps[i].CreatedAt < eps[j].CreatedAt })

	out := make([]Message, 0, len(eps))
	for _, ep := range eps {
		switch Role(ep.Role) {
		case RoleUser:
			out = append(out, UserMessage(ep.Content))
		case RoleAssistant:
			out = append(out, Message{Role: RoleAssistant, Content: ep.Content})
		}
	}
	return out, nil
}
