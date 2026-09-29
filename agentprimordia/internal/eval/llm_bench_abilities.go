// llm_bench_abilities.go — v4.1 真实 LLM 能力跑分：自治目标执行成功率 + 技能习得成功率
//
// 与 RunLLMBench 同构：产出 LLMBenchResult 报告（成本/延迟/通过率），
// 失败用例记 0 分、不设门禁（分数仅供基线记录，随版本写入 ROADMAP 状态行）。
package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm"
)

// AutonomyGoalCase 自治目标跑分用例：目标描述 + 计划必须覆盖的必达阶段关键词。
type AutonomyGoalCase struct {
	ID       string   `json:"id"`
	Goal     string   `json:"goal"`
	Required []string `json:"required"` // 计划描述必须覆盖的关键词（如 "采集"/"修复"）
}

// RunAutonomyGoalBench 真实 LLM 自治目标执行成功率跑分：
// 模型为目标产出分步执行计划（JSON），校验计划覆盖必达阶段的比例。
// 返回与 RunLLMBench 同构的 LLMBenchResult（通过率即目标执行成功率）。
func RunAutonomyGoalBench(ctx context.Context, cfg LLMBenchConfig, agent *LLMBenchAgent, cases []AutonomyGoalCase) (*LLMBenchResult, error) {
	res := &LLMBenchResult{
		Version:   cfg.Version,
		Model:     cfg.Model,
		Provider:  cfg.ProviderName,
		Threshold: cfg.Threshold,
		Baseline:  cfg.Baseline,
		Cases:     make([]LLMBenchCaseResult, 0, len(cases)),
	}
	res.Total = len(cases)

	startAll := time.Now()
	for _, c := range cases {
		caseStart := time.Now()
		cr := LLMBenchCaseResult{CaseID: c.ID, Phase: "autonomy"}

		prompt := fmt.Sprintf("你是任务规划器。目标：%s\n请输出分步执行计划 JSON 数组（不要任何其他文本）：[{\"id\":\"1\",\"description\":\"步骤描述\",\"depends_on\":[]}]", c.Goal)
		output, err := agent.Run(ctx, prompt)
		if err != nil {
			cr.Error = err.Error()
		} else {
			cr.Score, cr.Passed = evalGoalPlan(output, c.Required)
		}

		cr.Duration = time.Since(caseStart).Milliseconds()
		if cr.Passed {
			res.Passed++
		} else {
			res.Failed++
		}
		res.Cases = append(res.Cases, cr)
	}

	res.LatencyMs = time.Since(startAll).Milliseconds()
	if res.Total > 0 {
		res.PassRate = float64(res.Passed) / float64(res.Total)
		res.AvgLatencyMs = res.LatencyMs / int64(res.Total)
	}
	usage := agent.Usage()
	res.PromptTokens = usage.PromptTokens
	res.CompletionTokens = usage.CompletionTokens
	res.TotalTokens = usage.TotalTokens
	res.CostUSD = llm.EstimateCost(cfg.Model, usage, llm.DefaultPricingTable())
	res.Generated = time.Now().UTC().Format(time.RFC3339)
	return res, nil
}

// evalGoalPlan 解析计划 JSON，按必达阶段关键词覆盖比例给分；
// 计划解析失败或空计划 → 0 分失败。
func evalGoalPlan(output string, required []string) (float64, bool) {
	var plan []struct {
		Description string `json:"description"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &plan); err != nil {
		return 0, false
	}
	if len(plan) == 0 {
		return 0, false
	}
	covered := 0
	for _, req := range required {
		for _, step := range plan {
			if strings.Contains(step.Description, req) {
				covered++
				break
			}
		}
	}
	if len(required) == 0 {
		return 1, true
	}
	return float64(covered) / float64(len(required)), covered == len(required)
}

// SkillAcquisitionCase 技能习得跑分用例：任务描述 + 成功工具调用轨迹。
type SkillAcquisitionCase struct {
	ID        string   `json:"id"`
	Task      string   `json:"task"`
	ToolCalls []string `json:"tool_calls"` // 工具名序列
	MinSteps  int      `json:"min_steps"`  // 提炼技能最少步骤数
}

// RunSkillAcquisitionBench 真实 LLM 技能习得成功率跑分：
// 模型把轨迹提炼为可复用技能 JSON，校验（解析成功 + 名称非空 + 步骤达标）。
func RunSkillAcquisitionBench(ctx context.Context, cfg LLMBenchConfig, agent *LLMBenchAgent, cases []SkillAcquisitionCase) (*LLMBenchResult, error) {
	res := &LLMBenchResult{
		Version:   cfg.Version,
		Model:     cfg.Model,
		Provider:  cfg.ProviderName,
		Threshold: cfg.Threshold,
		Baseline:  cfg.Baseline,
		Cases:     make([]LLMBenchCaseResult, 0, len(cases)),
	}
	res.Total = len(cases)

	startAll := time.Now()
	for _, c := range cases {
		caseStart := time.Now()
		cr := LLMBenchCaseResult{CaseID: c.ID, Phase: "skills"}

		prompt := fmt.Sprintf("任务：%s\n成功工具调用轨迹：%s\n请提炼为可复用技能，输出 JSON（不要任何其他文本）：{\"name\":\"技能名\",\"description\":\"描述\",\"steps\":[{\"id\":\"s1\",\"tool_name\":\"工具\",\"description\":\"步骤\",\"depends_on\":[]}],\"tags\":[\"标签\"]}",
			c.Task, strings.Join(c.ToolCalls, " → "))
		output, err := agent.Run(ctx, prompt)
		if err != nil {
			cr.Error = err.Error()
		} else {
			cr.Score, cr.Passed = evalAcquiredSkill(output, c.MinSteps)
		}

		cr.Duration = time.Since(caseStart).Milliseconds()
		if cr.Passed {
			res.Passed++
		} else {
			res.Failed++
		}
		res.Cases = append(res.Cases, cr)
	}

	res.LatencyMs = time.Since(startAll).Milliseconds()
	if res.Total > 0 {
		res.PassRate = float64(res.Passed) / float64(res.Total)
		res.AvgLatencyMs = res.LatencyMs / int64(res.Total)
	}
	usage := agent.Usage()
	res.PromptTokens = usage.PromptTokens
	res.CompletionTokens = usage.CompletionTokens
	res.TotalTokens = usage.TotalTokens
	res.CostUSD = llm.EstimateCost(cfg.Model, usage, llm.DefaultPricingTable())
	res.Generated = time.Now().UTC().Format(time.RFC3339)
	return res, nil
}

// evalAcquiredSkill 校验提炼出的技能：JSON 解析 + 名称非空 + 步骤数达标；
// 任一项不满足 → 0 分失败。
func evalAcquiredSkill(output string, minSteps int) (float64, bool) {
	var skill struct {
		Name  string `json:"name"`
		Steps []struct {
			ID       string `json:"id"`
			ToolName string `json:"tool_name"`
		} `json:"steps"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &skill); err != nil {
		return 0, false
	}
	if skill.Name == "" {
		return 0, false
	}
	if minSteps <= 0 {
		minSteps = 1
	}
	if len(skill.Steps) < minSteps {
		return 0, false
	}
	for _, s := range skill.Steps {
		if s.ID == "" || s.ToolName == "" {
			return 0, false
		}
	}
	return 1, true
}
