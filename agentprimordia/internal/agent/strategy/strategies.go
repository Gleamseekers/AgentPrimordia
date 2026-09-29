// strategies.go — v5.2 三种内置推理策略：ReAct / Plan-Execute-Reflect / 验证循环。
//
// 统一策略协议：模型在内容中输出 JSON 指令——
//
//	{"tool": "工具名", "args": {...}}   调用工具（结果回注为观察）
//	{"final": "答案"}                    结束并给出最终输出
//
// 该协议使三策略共享同一引擎原语（Engine），可运行时热切换与 A/B 对照。
package strategy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Gleamseekers/AgentPrimordia/internal/llm"
)

// 策略名常量（注册键）
const (
	NameReAct        = "react"
	NamePlanReflect  = "plan_execute_reflect"
	NameVerification = "verification_loop"
)

// strategyAction 模型输出的结构化指令
type strategyAction struct {
	Tool  string         `json:"tool,omitempty"`
	Args  map[string]any `json:"args,omitempty"`
	Final string         `json:"final,omitempty"`
}

func defaultTurns(max int) int {
	if max <= 0 {
		return 12
	}
	return max
}

// parseAction 从模型输出提取动作；非法 JSON 视为最终答案（容错降级）
func parseAction(content string) strategyAction {
	var a strategyAction
	raw := extractJSON(content)
	if err := json.Unmarshal([]byte(raw), &a); err != nil || (a.Tool == "" && a.Final == "") {
		return strategyAction{Final: strings.TrimSpace(content)}
	}
	return a
}

// chatReq 构造单轮补全请求并累计用量
func chatReq(ctx context.Context, eng Engine, system, user string) (*llm.CompletionResponse, error) {
	return eng.Complete(ctx, &llm.CompletionRequest{
		Messages: []llm.ChatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	})
}

// ReActStrategy 经典 ReAct：思考→行动→观察 循环。
type ReActStrategy struct{}

// Name 实现 Strategy
func (s *ReActStrategy) Name() string { return NameReAct }

// Run 实现 Strategy
func (s *ReActStrategy) Run(ctx context.Context, eng Engine, task Task) (*Result, error) {
	system := task.SystemPrompt + "\n" + reactProtocolPrompt
	history := append([]llm.ChatMessage(nil), task.History...)
	res := &Result{}
	maxTurns := defaultTurns(task.MaxTurns)

	for turn := 1; turn <= maxTurns; turn++ {
		res.Turns = turn
		resp, err := chatReq(ctx, eng, system, renderTask(task, history))
		if err != nil {
			return nil, fmt.Errorf("strategy: react 第 %d 轮失败: %w", turn, err)
		}
		res.Usage.PromptTokens += resp.Usage.PromptTokens
		res.Usage.CompletionTokens += resp.Usage.CompletionTokens
		res.Usage.TotalTokens += resp.Usage.TotalTokens

		act := parseAction(resp.Content)
		if act.Final != "" {
			res.Output = act.Final
			return res, nil
		}
		obs, err := eng.ExecuteTool(ctx, act.Tool, marshalArgs(act.Args))
		if err != nil {
			obs = fmt.Sprintf("工具执行失败: %v", err)
		}
		history = append(history,
			llm.ChatMessage{Role: "assistant", Content: resp.Content},
			llm.ChatMessage{Role: "user", Content: "观察结果：" + obs},
		)
	}
	return nil, fmt.Errorf("strategy: react 达到最大轮数 %d 未得出结论", maxTurns)
}

// reactProtocolPrompt ReAct 协议说明（追加到系统提示词）
const reactProtocolPrompt = `
你是 ReAct 智能体。每轮只输出一个 JSON 行动（不要输出其他内容）：
- 需要工具时：{"tool": "工具名", "args": {...}}
- 任务完成时：{"final": "最终答案"}`

// renderTask 渲染任务输入（目标 + 历史）
func renderTask(task Task, history []llm.ChatMessage) string {
	var sb strings.Builder
	sb.WriteString("任务目标：" + task.Goal + "\n")
	sb.WriteString("请以 JSON 行动：{\"tool\": \"...\", \"args\": {...}} 或 {\"final\": \"...\"}")
	for _, m := range history {
		sb.WriteString("\n" + m.Role + ": " + m.Content)
	}
	return sb.String()
}

// marshalArgs 序列化工具参数
func marshalArgs(args map[string]any) string {
	if len(args) == 0 {
		return "{}"
	}
	b, err := json.Marshal(args)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// PlanExecuteReflectStrategy 计划-执行-反思策略：
// 先生成子任务计划，逐步执行，最后反思评估；反思不通过则重规划一次。
type PlanExecuteReflectStrategy struct {
	// MaxReplans 反思不通过时的最大重规划次数（默认 1）
	MaxReplans int
}

// Name 实现 Strategy
func (s *PlanExecuteReflectStrategy) Name() string { return NamePlanReflect }

// planStep 计划步骤
type planStep struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

// Run 实现 Strategy
func (s *PlanExecuteReflectStrategy) Run(ctx context.Context, eng Engine, task Task) (*Result, error) {
	res := &Result{}
	maxReplans := s.MaxReplans
	if maxReplans <= 0 {
		maxReplans = 1
	}
	goal := task.Goal

	for attempt := 0; attempt <= maxReplans; attempt++ {
		// 1. 计划
		planResp, err := chatReq(ctx, eng, task.SystemPrompt,
			"任务目标："+goal+"\n请把目标分解为不超过 5 个子任务，只返回 JSON 数组：[{\"id\":\"1\",\"description\":\"...\"}...]")
		if err != nil {
			return nil, fmt.Errorf("strategy: plan 失败: %w", err)
		}
		res.Turns++
		res.Usage.TotalTokens += planResp.Usage.TotalTokens
		steps, perr := parsePlanSteps(planResp.Content)
		if perr != nil || len(steps) == 0 {
			steps = []planStep{{ID: "1", Description: goal}} // 容错：单步直做
		}

		// 2. 逐步执行
		var outputs []string
		failed := false
		for _, st := range steps {
			stepResp, err := chatReq(ctx, eng, task.SystemPrompt,
				"任务目标："+goal+"\n当前子任务："+st.Description+"\n已完成步骤输出：\n"+strings.Join(outputs, "\n")+
					"\n直接给出该子任务的完整结果。")
			if err != nil {
				return nil, fmt.Errorf("strategy: 执行子任务 %s 失败: %w", st.ID, err)
			}
			res.Turns++
			res.Usage.TotalTokens += stepResp.Usage.TotalTokens
			outputs = append(outputs, stepResp.Content)
			if strings.Contains(stepResp.Content, "无法完成") || strings.Contains(stepResp.Content, "失败") {
				failed = true
			}
		}
		draft := strings.Join(outputs, "\n\n")

		// 3. 反思
		reflResp, err := chatReq(ctx, eng, task.SystemPrompt,
			"任务目标："+goal+"\n候选结果：\n"+draft+"\n请评估是否达成目标。只返回 JSON：{\"passed\": bool, \"reasons\": [...]}")
		if err != nil {
			return nil, fmt.Errorf("strategy: reflect 失败: %w", err)
		}
		res.Turns++
		res.Usage.TotalTokens += reflResp.Usage.TotalTokens

		var verdict struct {
			Passed bool `json:"passed"`
		}
		_ = json.Unmarshal([]byte(extractJSON(reflResp.Content)), &verdict)
		if verdict.Passed && !failed {
			res.Output = draft
			return res, nil
		}
		// 反思不通过 → 收集反馈进入重规划
		goal = task.Goal + "\n\n上一轮尝试未通过评审，反馈：" + extractJSON(reflResp.Content)
	}
	return nil, fmt.Errorf("strategy: plan_execute_reflect 在 %d 次重规划后仍未通过反思", maxReplans)
}

// parsePlanSteps 解析计划 JSON 数组
func parsePlanSteps(content string) ([]planStep, error) {
	s := extractJSON(content)
	// extractJSON 提取 {...}；数组场景改为定位 [...]
	start := strings.Index(content, "[")
	end := strings.LastIndex(content, "]")
	if start >= 0 && end > start {
		s = content[start : end+1]
	}
	var steps []planStep
	if err := json.Unmarshal([]byte(s), &steps); err != nil {
		return nil, err
	}
	return steps, nil
}

// VerificationLoopStrategy 验证循环策略：生成→校验→修正，verifier 一等公民。
type VerificationLoopStrategy struct {
	// Verifier 结果校验器（必填，nil 则 Run 报错）
	Verifier Verifier
	// MaxCorrections 校验失败后的最大修正轮数（默认 2）
	MaxCorrections int
}

// Name 实现 Strategy
func (s *VerificationLoopStrategy) Name() string { return NameVerification }

// Run 实现 Strategy
func (s *VerificationLoopStrategy) Run(ctx context.Context, eng Engine, task Task) (*Result, error) {
	if s.Verifier == nil {
		return nil, fmt.Errorf("strategy: verification_loop 缺少 verifier")
	}
	maxCorr := s.MaxCorrections
	if maxCorr <= 0 {
		maxCorr = 2
	}

	res := &Result{}
	userMsg := "任务目标：" + task.Goal + "\n请直接给出完整结果。"
	var feedback string

	for round := 0; ; round++ {
		res.Turns++
		prompt := userMsg
		if feedback != "" {
			prompt += "\n\n上一版未通过校验，原因：\n" + feedback + "\n请修正后重新给出完整结果。"
		}
		resp, err := chatReq(ctx, eng, task.SystemPrompt, prompt)
		if err != nil {
			return nil, fmt.Errorf("strategy: verification 第 %d 轮失败: %w", round+1, err)
		}
		res.Usage.TotalTokens += resp.Usage.TotalTokens

		rep, err := s.Verifier.Verify(ctx, eng, task, resp.Content)
		if err != nil {
			return nil, fmt.Errorf("strategy: 校验器执行失败: %w", err)
		}
		res.Verification = rep
		if rep.Passed {
			res.Output = resp.Content
			return res, nil
		}
		if round >= maxCorr {
			// 修正预算耗尽：返回最后一版但标记未通过（调用方可决策）
			res.Output = resp.Content
			return res, fmt.Errorf("strategy: 校验未通过（%d 轮修正后）：%s", maxCorr, strings.Join(rep.Reasons, "; "))
		}
		feedback = strings.Join(rep.Reasons, "; ")
	}
}

// ===== 构造函数 =====

// NewReAct 创建 ReAct 策略
func NewReAct() *ReActStrategy { return &ReActStrategy{} }

// NewPlanExecuteReflect 创建计划-执行-反思策略；maxReplans <= 0 用默认 1
func NewPlanExecuteReflect(maxReplans int) *PlanExecuteReflectStrategy {
	return &PlanExecuteReflectStrategy{MaxReplans: maxReplans}
}

// NewVerificationLoop 创建验证循环策略；verifier 必填，maxCorrections <= 0 用默认 2
func NewVerificationLoop(v Verifier, maxCorrections int) *VerificationLoopStrategy {
	return &VerificationLoopStrategy{Verifier: v, MaxCorrections: maxCorrections}
}

// NewKeywordVerifier 创建关键词校验器
func NewKeywordVerifier(requires []string) *KeywordVerifier {
	return &KeywordVerifier{Requires: requires}
}

// NewSelfCheckVerifier 创建自校验器
func NewSelfCheckVerifier() *SelfCheckVerifier { return &SelfCheckVerifier{} }
