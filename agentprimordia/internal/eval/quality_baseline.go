// quality_baseline.go — v5.1 质量度量体系：核心四件套质量基线与回归门
//
// V6-ROADMAP §三 任务 1：召回率 / 任务成功率 / P95 延迟 / 成本四件套指标
// 全部进回归门——每项有数字、有阈值、有数据来源；不达标即门禁失败。
//
// 数据文件：bench/results/2026-Q3-v5.1-quality-baseline.json（质量看板数据源）。
//
// 无 key 降级：requires_key=true 的门（nightly 真实 LLM 跑分项）在无 secrets
// 环境自动跳过，不阻塞其余门禁；配合 internal/llm 的 RecordedProvider 回放路径。
package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// QualityComparison 比较方向
type QualityComparison string

const (
	// AtLeast 实际值 >= 阈值（如召回率、成功率）
	AtLeast QualityComparison = ">="
	// AtMost 实际值 <= 阈值（如延迟、成本）
	AtMost QualityComparison = "<="
)

// QualityGate 单条质量门
type QualityGate struct {
	// Name 门名称（如 "recall_ts_hnsw_n3000"）
	Name string `json:"name"`
	// Metric 指标名（recall_at_10 / success_rate / p95_ns / cost_usd_per_task）
	Metric string `json:"metric"`
	// Value 当前实测值
	Value float64 `json:"value"`
	// Threshold 阈值
	Threshold float64 `json:"threshold"`
	// Comparison 比较方向
	Comparison QualityComparison `json:"comparison"`
	// Source 数据来源（基准文件/命令）
	Source string `json:"source"`
	// RequiresKey 是否依赖真实 LLM API Key（无 key CI 自动跳过）
	RequiresKey bool `json:"requires_key,omitempty"`
}

// QualityBaseline 质量基线（四件套聚合）
type QualityBaseline struct {
	Version string        `json:"version"`
	Updated string        `json:"updated"`
	Gates   []QualityGate `json:"gates"`
}

// LoadQualityBaseline 从仓库 bench/results 目录加载质量基线
func LoadQualityBaseline(path string) (*QualityBaseline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("eval: 读取质量基线失败: %w", err)
	}
	var qb QualityBaseline
	if err := json.Unmarshal(data, &qb); err != nil {
		return nil, fmt.Errorf("eval: 解析质量基线失败: %w", err)
	}
	return &qb, nil
}

// DefaultQualityBaselinePath 返回仓库内默认质量基线文件路径
func DefaultQualityBaselinePath() string {
	_, filename, _, _ := runtime.Caller(0)
	// internal/eval/ -> internal/ -> github.com/Gleamseekers/AgentPrimordia/
	root := filepath.Dir(filepath.Dir(filepath.Dir(filename)))
	return filepath.Join(root, "bench", "results", "2026-Q3-v5.1-quality-baseline.json")
}

// Check 校验全部质量门，返回违规描述列表（空切片 = 全部通过）。
// requiresKey 为 true 时跳过 requires_key 门（无 secrets 环境）。
func (q *QualityBaseline) Check(requiresKey bool) []string {
	var violations []string
	for _, g := range q.Gates {
		if g.RequiresKey && !requiresKey {
			continue
		}
		switch g.Comparison {
		case AtLeast:
			if g.Value < g.Threshold {
				violations = append(violations, fmt.Sprintf(
					"质量门 %s(%s): %.4f < 阈值 %.4f（来源 %s）", g.Name, g.Metric, g.Value, g.Threshold, g.Source))
			}
		case AtMost:
			if g.Value > g.Threshold {
				violations = append(violations, fmt.Sprintf(
					"质量门 %s(%s): %.4f > 阈值 %.4f（来源 %s）", g.Name, g.Metric, g.Value, g.Threshold, g.Source))
			}
		default:
			violations = append(violations, fmt.Sprintf("质量门 %s: 未知比较方向 %q", g.Name, g.Comparison))
		}
	}
	return violations
}
