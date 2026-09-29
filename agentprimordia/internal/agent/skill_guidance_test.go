package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/skills"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
)

func activeSkillStore(t *testing.T) (*skills.Store, *skills.Matcher) {
	t.Helper()
	store := skills.NewStore()
	sk := skills.NewSkill("代码审查", "代码审查 检查 代码 质量", []skills.StepDef{
		{ID: "s1", ToolName: "read_file", Description: "读取目标文件"},
		{ID: "s2", ToolName: "grep", Description: "检索关键实现"},
	})
	sk.ID = "code-review"
	sk.Tags = []string{"代码审查"}
	sk.Activate()
	store.Save(sk)
	return store, skills.NewMatcher(store, skills.MatcherConfig{})
}

// TestSkillGuidanceInjectedOnMatch 回归：配置了技能匹配器且命中时，
// 技能步骤指引必须注入到发给 LLM 的 system 上下文——
// 此前引擎从不调用 GetSkillMatcher，Matcher 完全悬空。
func TestSkillGuidanceInjectedOnMatch(t *testing.T) {
	store, matcher := activeSkillStore(t)
	mock := llm.NewMockLLM(t)
	mock.WithResponse("done")

	a, err := NewAgent("skill-agent", "", mock,
		WithMaxTurns(2),
		WithSkills(SkillsConfig{Store: store, Matcher: matcher}),
	)
	if err != nil {
		t.Fatalf("NewAgent 失败: %v", err)
	}
	if _, err := a.Run(context.Background(), UserMessage("请帮我做代码审查")); err != nil {
		t.Fatalf("Run 失败: %v", err)
	}

	req, ok := mock.LastRequest().(*llm.CompletionRequest)
	if !ok || req == nil {
		t.Fatal("未捕获 LLM 请求")
	}
	for _, m := range req.Messages {
		if strings.Contains(m.Content, "已匹配技能") && strings.Contains(m.Content, "code-review") {
			return
		}
	}
	t.Fatalf("LLM 请求未注入技能指引（messages=%d）", len(req.Messages))
}

// TestSkillGuidanceNotInjectedWithoutMatcher 未配置 Matcher 时默认路径零变更。
func TestSkillGuidanceNotInjectedWithoutMatcher(t *testing.T) {
	mock := llm.NewMockLLM(t)
	mock.WithResponse("done")

	a, err := NewAgent("plain-agent", "", mock, WithMaxTurns(2))
	if err != nil {
		t.Fatalf("NewAgent 失败: %v", err)
	}
	if _, err := a.Run(context.Background(), UserMessage("请帮我做代码审查")); err != nil {
		t.Fatalf("Run 失败: %v", err)
	}

	req, ok := mock.LastRequest().(*llm.CompletionRequest)
	if !ok || req == nil {
		t.Fatal("未捕获 LLM 请求")
	}
	for _, m := range req.Messages {
		if strings.Contains(m.Content, "已匹配技能") {
			t.Fatal("未配置 Matcher 时不应注入技能指引")
		}
	}
}

// TestSkillGuidanceIdempotent 同一历史重复注入应替换而非堆积。
func TestSkillGuidanceIdempotent(t *testing.T) {
	store, matcher := activeSkillStore(t)
	mock := llm.NewMockLLM(t)
	mock.WithResponse("done")

	a, err := NewAgent("skill-agent", "", mock,
		WithMaxTurns(2),
		WithSkills(SkillsConfig{Store: store, Matcher: matcher}),
	)
	if err != nil {
		t.Fatalf("NewAgent 失败: %v", err)
	}
	if _, err := a.Run(context.Background(), UserMessage("请帮我做代码审查")); err != nil {
		t.Fatalf("Run 失败: %v", err)
	}
	req := mock.LastRequest().(*llm.CompletionRequest)
	count := 0
	for _, m := range req.Messages {
		if strings.Contains(m.Content, "已匹配技能") {
			count++
		}
	}
	if count > 1 {
		t.Errorf("技能指引应幂等注入，实际出现 %d 次", count)
	}
}
