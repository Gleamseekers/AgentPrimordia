// memory_readback_test.go — v3.4-3 长期记忆回读注入测试
package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/memory"
)

// mockMemoryQuerier 同时实现 agent.MemoryStore 与 MemoryQuerier（回读）
type mockMemoryQuerier struct {
	episodes []*memory.Episode
}

func (m *mockMemoryQuerier) Add(_ context.Context, _ *memory.Episode) error { return nil }
func (m *mockMemoryQuerier) UpdateSummary(_ context.Context, _, _, _ string) error {
	return nil
}
func (m *mockMemoryQuerier) Search(_ context.Context, _ string, _ *memory.SearchOptions) ([]*memory.Episode, error) {
	return m.episodes, nil
}

// TestInjectMemoryContext_AddsSystemMessage 验证记忆上下文作为 system 消息注入
func TestInjectMemoryContext_AddsSystemMessage(t *testing.T) {
	history := []Message{
		{Role: RoleSystem, Content: "你是助手"},
		{Role: RoleUser, Content: "你好"},
	}
	got := injectMemoryContext(history, "[长期记忆]\n- [user] 用户偏好 Go 语言")
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[1].Role != RoleSystem || !strings.Contains(got[1].Content, "长期记忆") {
		t.Errorf("应插入记忆 system 消息, got: %+v", got[1])
	}
	if got[1].Metadata.Extra["memory_context"] != "true" {
		t.Errorf("应标记 memory_context, got: %+v", got[1].Metadata.Extra)
	}
}

// TestInjectMemoryContext_ReplacesExisting 验证已存在记忆消息时替换而非重复插入
func TestInjectMemoryContext_ReplacesExisting(t *testing.T) {
	existing := SystemMessage("旧记忆")
	existing.Metadata.Extra = map[string]string{"memory_context": "true"}
	history := []Message{
		{Role: RoleSystem, Content: "你是助手"},
		existing,
		{Role: RoleUser, Content: "你好"},
	}
	got := injectMemoryContext(history, "新记忆")
	count := 0
	for _, m := range got {
		if m.Metadata.Extra["memory_context"] == "true" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("记忆消息应唯一, count = %d", count)
	}
}

// TestSearchMemoryContext_FormatsEpisodes 验证检索结果格式化为记忆上下文
func TestSearchMemoryContext_FormatsEpisodes(t *testing.T) {
	store := &mockMemoryQuerier{
		episodes: []*memory.Episode{
			{Role: "user", Content: "之前讨论过用 SQLite 存储"},
			{Role: "assistant", Content: "建议使用 WAL 模式"},
		},
	}
	// 编译期验证 mockMemoryQuerier 实现 MemoryQuerier
	var _ MemoryQuerier = store
	eps, err := store.Search(context.Background(), "存储", nil)
	if err != nil || len(eps) != 2 {
		t.Fatalf("Search = %d, err = %v", len(eps), err)
	}
	got := formatMemoryContext(eps)
	if !strings.Contains(got, "SQLite") || !strings.Contains(got, "WAL") || !strings.Contains(got, "长期记忆") {
		t.Errorf("格式化结果缺失内容: %q", got)
	}
}

// TestRun_InjectMemoryReadback 集成验证：agent 配置可回读的 memory 后，
// Run 时目标作为查询召回长期记忆并注入 LLM 请求。
func TestRun_InjectMemoryReadback(t *testing.T) {
	mock := llm.NewMockLLM(t)
	mock.WithResponse("好的，我记得之前的讨论")

	store := &mockMemoryQuerier{
		episodes: []*memory.Episode{{Role: "user", Content: "之前约定用 PostgreSQL 存储"}},
	}
	ag, err := NewAgent("mem-read", "助手", mock)
	if err != nil {
		t.Fatalf("NewAgent 失败: %v", err)
	}
	ag.WithMemory(store)

	resp, err := ag.Run(context.Background(), Message{Role: RoleUser, Content: "继续上次关于存储的讨论"})
	if err != nil {
		t.Fatalf("Run 失败: %v", err)
	}
	if resp.Content == "" {
		t.Fatal("响应为空")
	}

	// 断言 LLM 收到的请求含长期记忆
	last := mock.LastRequest()
	req, ok := last.(*llm.CompletionRequest)
	if !ok {
		t.Fatalf("LastRequest 类型 = %T, want *llm.CompletionRequest", last)
	}
	all := ""
	for _, m := range req.Messages {
		all += m.Content + "\n"
	}
	if !strings.Contains(all, "长期记忆") || !strings.Contains(all, "PostgreSQL") {
		t.Errorf("LLM 请求应包含长期记忆回读, got: %q", all)
	}
}
