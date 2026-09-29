// feedback.go — v5.4 自进化闭环：结果反馈回路 + 自改进安全边界。
//
// 闭环：任务结果 → SelfModel 画像 / FailureStore 失败模式库 →
// 规则式改进建议（prompt/config/skill 三层）→ 人工确认 → 应用。
//
// 安全边界（V6-ROADMAP §六 任务 4）：改进范围限定 prompt/config/skill 层；
// code 层变更一律拒绝（ErrImprovementScopeViolation）——对抗测试覆盖。
// 技能层建议经 SkillSynthesizer 落地（对接 skills.Acquire 验证+签名链路）。
package learning

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/memory"
	"github.com/Gleamseekers/AgentPrimordia/internal/persist"
)

// Scope 改进作用域（沙箱白名单）
type Scope string

const (
	// ScopePrompt 提示词层
	ScopePrompt Scope = "prompt"
	// ScopeConfig 配置/参数层
	ScopeConfig Scope = "config"
	// ScopeSkill 技能层（经合成器验证签名）
	ScopeSkill Scope = "skill"
	// ScopeCode 代码层——沙箱永久禁止
	ScopeCode Scope = "code"
)

// ErrImprovementScopeViolation 自改进越界（code 层）
var ErrImprovementScopeViolation = errors.New("learning: 自改进越界：code 层变更被沙箱禁止，需人工代码评审")

// ErrNotApproved 建议未经人工批准
var ErrNotApproved = errors.New("learning: 建议未经人工批准")

// Outcome 任务结果
type Outcome struct {
	Domain            string // 能力域
	Success           bool
	Turns             int    // 消耗轮数
	FailureSignature  string // 失败签名（失败时必填）
	ErrorText         string // 原始错误（入失败库）
	Input             string // 用户输入（入失败库）
	TrajectorySummary string // 成功轨迹摘要（高轮数成功触发技能合成建议）
}

// Suggestion 改进建议
type Suggestion struct {
	ID          string            `json:"id"`
	Domain      string            `json:"domain"`
	Scope       Scope             `json:"scope"`
	Description string            `json:"description"`
	Payload     map[string]string `json:"payload,omitempty"` // 应用参数（如 append 提示片段）
	CreatedAt   time.Time         `json:"created_at"`
}

// FailureRecorder 失败库最小接口（persist.FailureStore 子集）
type FailureRecorder interface {
	Record(ctx context.Context, rec *persist.FailureRecord) error
}

// SkillSynthesizer 技能合成器接口（对接 internal/agent/skills）
type SkillSynthesizer interface {
	// Synthesize 由成功轨迹合成技能，返回技能 ID
	Synthesize(ctx context.Context, name, trajectory string) (string, error)
}

// skillSynthAdapter 适配函数为接口
type skillSynthAdapter func(ctx context.Context, name, trajectory string) (string, error)

func (f skillSynthAdapter) Synthesize(ctx context.Context, name, traj string) (string, error) {
	return f(ctx, name, traj)
}

// FeedbackLoop 结果反馈回路
type FeedbackLoop struct {
	mu            sync.Mutex
	model         *memory.SelfModel
	failures      FailureRecorder
	synth         SkillSynthesizer
	pending       map[string]*Suggestion
	approved      map[string]bool
	appliedN      int
	lastTraj      string // 最近一次成功轨迹摘要
	lastTrajTurns int
	applier       func(Suggestion) error
	// 高轮数成功阈值：超过视为可复用轨迹，触发技能合成建议
	SkillTrajectoryTurns int
}

// NewFeedbackLoop 创建反馈回路；synth 可为 nil（跳过技能层）
func NewFeedbackLoop(model *memory.SelfModel, failures FailureRecorder, synth SkillSynthesizer) *FeedbackLoop {
	if synth == nil {
		synth = skillSynthAdapter(func(context.Context, string, string) (string, error) {
			return "", errors.New("learning: 未配置技能合成器")
		})
	}
	return &FeedbackLoop{
		model:                model,
		failures:             failures,
		synth:                synth,
		pending:              make(map[string]*Suggestion),
		approved:             make(map[string]bool),
		SkillTrajectoryTurns: 10,
	}
}

// Model 暴露画像（查询用）
func (f *FeedbackLoop) Model() *memory.SelfModel { return f.model }

// RecordOutcome 记录结果到画像与失败库（失败时），线程安全
func (f *FeedbackLoop) RecordOutcome(ctx context.Context, o Outcome) {
	f.model.RecordOutcome(o.Domain, o.Success, o.Turns, o.FailureSignature)
	f.mu.Lock()
	if o.Success && o.TrajectorySummary != "" && o.Turns >= f.SkillTrajectoryTurns {
		f.lastTraj = o.TrajectorySummary
		f.lastTrajTurns = o.Turns
	}
	f.mu.Unlock()
	if !o.Success && f.failures != nil {
		rec := &persist.FailureRecord{
			ID:        fmt.Sprintf("fb-%d", time.Now().UnixNano()),
			Phase:     "run",
			Error:     o.ErrorText,
			Turn:      o.Turns,
			Input:     o.Input,
			CreatedAt: time.Now(),
		}
		_ = f.failures.Record(ctx, rec)
	}
}

// Suggest 基于画像与已知缓解生成改进建议（规则式；LLM 版可后续插拔）：
//   - 有缓解手段的失败签名 → prompt 层建议（提示词追加规避指引）
//   - 反复失败的弱项域 → config 层建议（提高轮数/修正预算）
//   - 高轮数成功轨迹 → skill 层建议（合成为可复用技能）
func (f *FeedbackLoop) Suggest(domain, signature string) []Suggestion {
	var out []Suggestion
	now := time.Now()
	add := func(s Suggestion) { out = append(out, s) }

	// 1. 缓解手段 → prompt 层
	for _, fp := range f.model.TopFailures(50) {
		if signature != "" && fp.Signature != signature {
			continue
		}
		if fp.Mitigation != "" {
			add(Suggestion{
				ID:     fmt.Sprintf("sg-prompt-%s-%d", fp.Signature, now.UnixNano()),
				Domain: domain, Scope: ScopePrompt,
				Description: fmt.Sprintf("失败模式 %q 已有缓解手段：%s。建议注入系统提示规避。", fp.Signature, fp.Mitigation),
				Payload:     map[string]string{"append": "注意规避：" + fp.Mitigation},
				CreatedAt:   now,
			})
		}
	}

	// 2. 弱项域 → config 层（加深思考预算）
	for _, w := range f.model.WeakDomains(3, 50) {
		if w.Domain == domain {
			add(Suggestion{
				ID:     fmt.Sprintf("sg-config-%s-%d", w.Domain, now.UnixNano()),
				Domain: w.Domain, Scope: ScopeConfig,
				Description: fmt.Sprintf("能力域 %q 成功率 %.0f%%，建议提高 MaxTurns/MaxCorrections 预算。", w.Domain, w.SuccessRate()*100),
				Payload:     map[string]string{"max_turns": "16", "max_corrections": "3"},
				CreatedAt:   now,
			})
		}
	}

	// 3. 高轮数成功轨迹 → skill 层
	if f.lastTraj != "" {
		add(Suggestion{
			ID:          fmt.Sprintf("sg-skill-%s-%d", domain, now.UnixNano()),
			Domain:      domain,
			Scope:       ScopeSkill,
			Description: fmt.Sprintf("成功轨迹（%d 轮）可合成为可复用技能。", f.lastTrajTurns),
			Payload:     map[string]string{"trajectory": f.lastTraj},
			CreatedAt:   now,
		})
	}
	return out
}

// Propose 提交建议进入待审池。安全边界：ScopeCode 一律拒绝（沙箱）。
func (f *FeedbackLoop) Propose(s Suggestion) error {
	if s.Scope == ScopeCode {
		return ErrImprovementScopeViolation
	}
	if s.ID == "" {
		return errors.New("learning: 建议缺少 ID")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	s.CreatedAt = time.Now()
	f.pending[s.ID] = &s
	return nil
}

// Approve 人工批准建议（唯一生效通道）
func (f *FeedbackLoop) Approve(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.pending[id]; ok {
		f.approved[id] = true
	}
}

// appliedCount 已应用计数（测试观测）
func (f *FeedbackLoop) appliedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.appliedN
}

// ApplyApproved 应用全部已批准建议：prompt/config 直接落地回调；
// skill 层调用合成器（对接 skills.Acquire 验证+签名链路）。返回实际应用的建议。
func (f *FeedbackLoop) ApplyApproved(ctx context.Context) ([]Suggestion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var applied []Suggestion
	for id, s := range f.pending {
		if !f.approved[id] {
			continue
		}
		switch s.Scope {
		case ScopeSkill:
			if _, err := f.synth.Synthesize(ctx, s.Domain, s.Payload["trajectory"]); err != nil {
				return applied, fmt.Errorf("learning: 技能合成失败: %w", err)
			}
		case ScopePrompt, ScopeConfig:
			// 应用动作由宿主注入的 Applier 执行；此处仅记账（幂等出队）
			if f.applier != nil {
				if err := f.applier(*s); err != nil {
					return applied, fmt.Errorf("learning: 应用建议 %s 失败: %w", s.ID, err)
				}
			}
		default:
			continue
		}
		delete(f.pending, id)
		delete(f.approved, id)
		f.appliedN++
		applied = append(applied, *s)
	}
	return applied, nil
}

// SetApplier 注入 prompt/config 建议的实际应用回调（宿主接线点）
func (f *FeedbackLoop) SetApplier(fn func(Suggestion) error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applier = fn
}
