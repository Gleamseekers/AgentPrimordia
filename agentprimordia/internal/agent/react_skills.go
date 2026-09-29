// react_skills.go — 技能匹配接入 ReAct 循环（v7.4 接线）
//
// 背景：SkillsCapable 接口（capability_skills.go）声明"引擎通过此接口发现
// Agent 是否配置了技能库，从而在运行时自动匹配和调用已习得的技能"，
// 但引擎从未调用 GetSkillMatcher，Matcher 实际悬空。
//
// 本文件把匹配结果接入循环：命中技能时，将该技能的名字/说明/步骤作为一条
// system 上下文注入（带 skill_context 标记，可幂等替换，与 memory_context 同机制）。
// 未配置匹配器时行为完全不变（默认路径零变更）。
package agent

import (
	"fmt"
	"strings"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/skills"
)

// getSkillMatcherOrNil 通过 SkillsCapable 接口发现技能匹配器（nil-safe）。
func (a *ReActAgent) getSkillMatcherOrNil() *skills.Matcher {
	if a.self == nil {
		return nil
	}
	if c, ok := a.self.(SkillsCapable); ok {
		return c.GetSkillMatcher()
	}
	return nil
}

// injectSkillGuidance 在历史中注入命中的技能指引（幂等）。
// 未配置匹配器、无命中、或技能无步骤时原样返回。
func (a *ReActAgent) injectSkillGuidance(history []Message) []Message {
	matcher := a.getSkillMatcherOrNil()
	if matcher == nil {
		return history
	}
	task := lastUserContent(history)
	if task == "" {
		return history
	}
	res := matcher.Match(task)
	if res == nil || res.Skill == nil || len(res.Skill.Steps) == 0 {
		return history
	}
	guidance := buildSkillGuidance(res.Skill, res.Confidence)
	if guidance == "" {
		return history
	}

	msg := SystemMessage(guidance)
	if msg.Metadata.Extra == nil {
		msg.Metadata.Extra = make(map[string]string, 1)
	}
	msg.Metadata.Extra["skill_context"] = "true"

	// 已有技能上下文：替换（保持幂等）
	for i, m := range history {
		if m.Role == RoleSystem && m.Metadata.Extra["skill_context"] == "true" {
			history[i] = msg
			return history
		}
	}

	// 插入到前置 system 段之后（与 memory_context 同位置策略）
	systemEnd := len(history)
	for i, m := range history {
		if m.Role != RoleSystem {
			systemEnd = i
			break
		}
	}
	out := make([]Message, 0, len(history)+1)
	out = append(out, history[:systemEnd]...)
	out = append(out, msg)
	out = append(out, history[systemEnd:]...)
	return out
}

// lastUserContent 取最后一条 user 消息内容。
func lastUserContent(history []Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == RoleUser {
			return history[i].Content
		}
	}
	return ""
}

// buildSkillGuidance 把命中的技能渲染为给 LLM 的步骤指引。
func buildSkillGuidance(skill *skills.Skill, confidence skills.ConfidenceLevel) string {
	if skill == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("【已匹配技能】\n")
	fmt.Fprintf(&b, "技能：%s（id=%s，置信度 %s）\n", skill.Name, skill.ID, confidence)
	if skill.Description != "" {
		fmt.Fprintf(&b, "说明：%s\n", skill.Description)
	}
	b.WriteString("建议步骤：\n")
	for i, step := range skill.Steps {
		fmt.Fprintf(&b, "  %d. %s", i+1, step.ToolName)
		if step.Description != "" {
			fmt.Fprintf(&b, " — %s", step.Description)
		}
		b.WriteString("\n")
	}
	b.WriteString("请在任务合适时按上述步骤执行。\n")
	return b.String()
}
