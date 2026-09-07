// registry_v72.go — V7.2 命题级任务注册表
//
// 为 v7.2 弧线提供按命题（proposition）组织的硬任务装载能力。
// 任务文件以 JSON 数组形式存放于 docs/evals/v72/ 下，清单 manifest.json
// 记录版本、冻结时间与留出比例，并在文件加入后登记逐文件 sha256。
//
// 依赖方向：本文件位于 internal/eval/（横向支撑层），可消费本包已有工具函数。
package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// V72Manifest v7.2 任务集清单（docs/evals/v72/manifest.json 的内存表示）。
type V72Manifest struct {
	Version      string   `json:"version"`
	SHA256       string   `json:"sha256"`
	Files        []string `json:"files"`
	FrozenAt     string   `json:"frozen_at"`
	HoldoutRatio float64  `json:"holdout_ratio"`
}

// V72Task v7.2 单条硬任务。
type V72Task struct {
	ID       string       `json:"id"`       // 全局唯一标识
	Prop     string       `json:"prop"`     // 所属命题（p1/p2/p4/p5 等）
	Task     string       `json:"task"`     // 任务描述（自然语言）
	Holdout  bool         `json:"holdout"`  // 是否为留出样本
	Fixtures []V72Fixture `json:"fixtures"` // 初始环境文件
	Asserts  []V72Assert  `json:"asserts"`  // 验收断言
}

// V72Fixture 任务初始环境文件（路径 + 内联内容）。
// 注意：与 registry.go 中的 Fixture 结构不同，v7.2 使用简化版。
type V72Fixture struct {
	Path    string `json:"path"`    // 沙箱内相对路径
	Content string `json:"content"` // 文件内容（内联）
}

// V72Assert 任务验收断言。
type V72Assert struct {
	Type   string `json:"type"`   // 断言类型（file_exists / file_eq / json_path_eq 等）
	Target string `json:"target"` // 断言目标（文件路径或 JSON 路径表达式）
	Expect string `json:"expect"` // 期望值（部分断言类型可为空）
}

// v72ModuleRoot 返回 agentprimordia 模块根目录。
// 由本文件位置反推：internal/eval/registry_v72.go → internal/eval → internal → agentprimordia。
func v72ModuleRoot() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Dir(filepath.Dir(filepath.Dir(filename)))
}

// v72EvalsDir 返回 v7.2 题面目录。
func v72EvalsDir() string {
	return filepath.Join(v72ModuleRoot(), "docs", "evals", "v72")
}

// LoadV72Manifest 加载 v7.2 清单文件。
func LoadV72Manifest() (*V72Manifest, error) {
	path := filepath.Join(v72EvalsDir(), "manifest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("eval: 读取 v7.2 清单失败: %w", err)
	}
	var m V72Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("eval: 解析 v7.2 清单失败: %w", err)
	}
	return &m, nil
}

// LoadV72Tasks 加载指定命题的任务集（从 docs/evals/v72/<prop>.json）。
func LoadV72Tasks(prop string) ([]V72Task, error) {
	path := filepath.Join(v72EvalsDir(), prop+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("eval: 读取 v7.2 任务文件 %s 失败: %w", prop, err)
	}
	var tasks []V72Task
	if err := json.Unmarshal(data, &tasks); err != nil {
		return nil, fmt.Errorf("eval: 解析 v7.2 任务文件 %s 失败: %w", prop, err)
	}
	return tasks, nil
}

// V72TaskCount 统计指定命题的任务总数与留出数。
func V72TaskCount(prop string) (total, holdout int, err error) {
	tasks, err := LoadV72Tasks(prop)
	if err != nil {
		return 0, 0, err
	}
	for _, t := range tasks {
		total++
		if t.Holdout {
			holdout++
		}
	}
	return total, holdout, nil
}
