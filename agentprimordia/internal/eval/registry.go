// registry.go — V7 弧线 S0-2：题面注册表装载与冻结校验（新规 R4 的执行侧）
//
// docs/evals/ 是本框架全部验收题面的唯一来源（长程集/缺口集/对抗集/泛化集/judge 标定集）。
// 本文件提供：
//   - 读取 manifest.json 冻结台账（逐文件 sha256 + 留出/可见 id 清单 + 冻结 commit）；
//   - VerifyFrozen：CI 门——题面文件一经冻结若被改动即报告违规；
//   - LoadSet：按集合名装载题面，并支持「仅留出」装载（验收口径），
//     使「开发不可见留出集」成为可由代码强制的装载模式而非口头纪律。
package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// MinHoldoutRate 是 R4 规定的留出比例下限。
const MinHoldoutRate = 0.30

// RegistryPolicy 台账中的冻结策略。
type RegistryPolicy struct {
	HoldoutMinRate float64 `json:"holdout_min_rate"`
	Rule           string  `json:"rule"`
	Governance     string  `json:"governance"`
}

// RegistryFile 台账中一条注册题面文件。
type RegistryFile struct {
	File         string   `json:"file"`
	Purpose      string   `json:"purpose"`
	Consumers    string   `json:"consumers"`
	Oracle       string   `json:"oracle"`
	Count        int      `json:"count"`
	SHA256       string   `json:"sha256"`
	HoldoutCount int      `json:"holdout_count"`
	HoldoutRate  float64  `json:"holdout_rate"`
	HoldoutIDs   []string `json:"holdout_ids"`
	VisibleIDs   []string `json:"visible_ids"`
}

// EvalRegistry docs/evals/manifest.json 的内存表示。
type EvalRegistry struct {
	Schema       string         `json:"schema"`
	GeneratedBy  string         `json:"generated_by"`
	Seed         int            `json:"seed"`
	Policy       RegistryPolicy `json:"policy"`
	FreezeCommit string         `json:"freeze_commit"`
	Files        []RegistryFile `json:"files"`
}

// RepoRoot 返回仓库根目录（.../AgentPrimordia）。
// 由本文件位置反推：internal/eval/registry.go -> internal/ -> github.com/Gleamseekers/AgentPrimordia/ -> 仓库根。
func RepoRoot() string {
	_, filename, _, _ := runtime.Caller(0)
	moduleDir := filepath.Dir(filepath.Dir(filepath.Dir(filename))) // .../agentprimordia
	return filepath.Dir(moduleDir)
}

// DefaultEvalsDir 返回仓库内题面目录 docs/evals。
func DefaultEvalsDir() string {
	return filepath.Join(RepoRoot(), "docs", "evals")
}

// LoadRegistry 从 dir 读取 manifest.json。
func LoadRegistry(dir string) (*EvalRegistry, error) {
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("eval: 读取题面台账失败: %w", err)
	}
	var reg EvalRegistry
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("eval: 解析题面台账失败: %w", err)
	}
	if reg.Schema != "ap-eval-registry/v1" {
		return nil, fmt.Errorf("eval: 不支持的题面台账 schema %q", reg.Schema)
	}
	return &reg, nil
}

// FileSHA256 计算 dir 下相对路径文件的 sha256（十六进制）。
func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("eval: 打开文件失败: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("eval: 计算哈希失败: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// VerifyFrozen 台账与磁盘对账：逐文件 sha256 必须一致，留出比例不得低于 R4 下限。
// 返回违规描述列表（空 = 冻结完好），供 CI 门与验收前置检查使用。
func (r *EvalRegistry) VerifyFrozen(dir string) []string {
	var violations []string
	// R4 防回退：冻结台账必须登记冻结 commit——PENDING 占位 = 登记未完成，
	// 计划书的题面引用 @commit 将无从对账（e7995ba6 重算台账时曾把 d0491cbe 的登记冲回占位符）。
	if r.FreezeCommit == "" || r.FreezeCommit == "PENDING-FREEZE-COMMIT" {
		violations = append(violations,
			"台账 freeze_commit 未登记（空或 PENDING 占位）——R4 要求题面以独立 commit 冻结并在台账登记哈希")
	}
	root := filepath.Clean(dir)
	for _, f := range r.Files {
		abs, ok := resolveRegistered(root, f.File)
		if !ok {
			violations = append(violations, fmt.Sprintf("注册文件缺失: %s", f.File))
			continue
		}
		got, err := FileSHA256(abs)
		if err != nil {
			violations = append(violations, fmt.Sprintf("%s: %v", f.File, err))
			continue
		}
		if got != f.SHA256 {
			violations = append(violations, fmt.Sprintf("题面漂移 %s: 台账 %s 实际 %s（一经冻结不得修改，扩充走 *-v2.json）",
				f.File, f.SHA256[:12], got[:12]))
		}
		items, err := readRawSet(abs)
		if err != nil {
			violations = append(violations, fmt.Sprintf("%s: %v", f.File, err))
			continue
		}
		if len(items) != f.Count {
			violations = append(violations, fmt.Sprintf("%s: 条数 %d 与台账 %d 不符", f.File, len(items), f.Count))
			continue
		}
		hold := 0
		ids := map[string]bool{}
		for _, it := range items {
			if it["holdout"] == true {
				hold++
			}
			if id, ok := it["id"].(string); ok {
				if ids[id] {
					violations = append(violations, fmt.Sprintf("%s: id 重复 %s", f.File, id))
				}
				ids[id] = true
			}
		}
		rate := float64(hold) / float64(len(items))
		if rate < MinHoldoutRate {
			violations = append(violations, fmt.Sprintf("R4 违规 %s: 留出比例 %.2f%% < %.0f%%",
				f.File, rate*100, MinHoldoutRate*100))
		}
		if hold != f.HoldoutCount {
			violations = append(violations, fmt.Sprintf("%s: 留出条数 %d 与台账 %d 不符", f.File, hold, f.HoldoutCount))
		}
	}
	return violations
}

// resolveRegistered 把台账里的路径解析为磁盘绝对路径。
// 台账以仓库根为基准书写（docs/evals/xxx.json），而调用方传入的 dir 通常已是 docs/evals，
// 因此依次尝试：dir+全路径 / dir+basename / dir 的父目录+basename，取第一个存在者。
func resolveRegistered(dir, registered string) (string, bool) {
	rel := filepath.FromSlash(registered)
	candidates := []string{
		filepath.Join(dir, rel),
		filepath.Join(dir, filepath.Base(rel)),
		filepath.Join(filepath.Dir(dir), filepath.Base(rel)),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return filepath.Clean(c), true
		}
	}
	return "", false
}

// readRawSet 读取题面文件为原始对象切片（用于 holdout 字段与 id 校验）。
func readRawSet(path string) ([]map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("eval: 读取题面失败: %w", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("eval: 解析题面失败: %w", err)
	}
	return items, nil
}

// Fixture 任务的初始环境文件（inline 内容或仅登记路径由沙箱自备）。
type Fixture struct {
	Path   string `json:"path"`
	Inline string `json:"inline,omitempty"`
	Raw    string `json:"raw,omitempty"`
	Note   string `json:"note,omitempty"`
}

// Milestone 长程任务的阶段里程碑（含确定性断言组）。
type Milestone struct {
	ID     string      `json:"id"`
	Assert []Assertion `json:"assert"`
}

// Interruption 跨会话中断指令（harness 在指定里程碑后重启会话或换题面）。
type Interruption struct {
	AfterMilestone string `json:"after_milestone"`
	Action         string `json:"action"`
	Swap           string `json:"swap,omitempty"`
}

// Grading 判据：success 全过=任务成功（二值），partial 计过程分。
type Grading struct {
	Success []Assertion `json:"success"`
	Partial []Assertion `json:"partial"`
}

// Budget 执行预算上限（轮数/工具调用数）。
type Budget struct {
	MaxTurns     int `json:"max_turns"`
	MaxToolCalls int `json:"max_tool_calls"`
}

// EvalSetItem 一条注册题面（长程/缺口/泛化/judge 共用宽松字段集）。
type EvalSetItem struct {
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	Category         string         `json:"category"`
	Difficulty       string         `json:"difficulty"`
	Lang             string         `json:"lang"`
	Goal             string         `json:"goal"`
	Prompt           string         `json:"prompt,omitempty"`
	AbsentCapability string         `json:"absent_capability,omitempty"`
	Fixtures         []Fixture      `json:"fixtures,omitempty"`
	Toolset          []string       `json:"toolset,omitempty"`
	Milestones       []Milestone    `json:"milestones,omitempty"`
	Interruptions    []Interruption `json:"interruptions,omitempty"`
	Grading          Grading        `json:"grading"`
	Budget           Budget         `json:"budget"`
	Holdout          bool           `json:"holdout"`
	Kind             string         `json:"kind,omitempty"`
	AnswerCheck      map[string]any `json:"answer_check,omitempty"`
	Response         string         `json:"response,omitempty"`
	Label            string         `json:"label,omitempty"`
	Source           string         `json:"source,omitempty"`
}

// LoadSet 装载题面集合。onlyHoldout=true 时仅返回留出样本（验收口径）。
// name 为文件名（可带或不带 docs/evals/ 前缀）。
func LoadSet(dir, name string, onlyHoldout bool) ([]EvalSetItem, error) {
	base := filepath.Base(name)
	path := filepath.Join(dir, base)
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("eval: 题面集合 %s 不存在: %w", base, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("eval: 读取题面集合失败: %w", err)
	}
	var items []EvalSetItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("eval: 解析题面集合失败: %w", err)
	}
	if !onlyHoldout {
		return items, nil
	}
	var out []EvalSetItem
	for _, it := range items {
		if it.Holdout {
			out = append(out, it)
		}
	}
	return out, nil
}
