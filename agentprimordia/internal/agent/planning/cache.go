// Package planning 提供任务分解和计划生成能力
package planning

import (
	"strings"
	"sync"
	"unicode"
)

// cacheEntry 缓存条目，存储任务描述与对应的计划
type cacheEntry struct {
	task string
	plan *Plan
}

// PlanCache 基于任务描述相似度的计划缓存
// 当新任务与已缓存任务足够相似时，可直接复用已有 plan 骨架，避免重复 LLM 调用
type PlanCache struct {
	mu        sync.RWMutex
	entries   []cacheEntry
	threshold float64 // 相似度阈值，低于此值视为不匹配
}

// NewPlanCache 创建计划缓存实例
// threshold 为 Jaccard 相似度阈值（0.0~1.0），仅当相似度 >= threshold 时命中缓存
func NewPlanCache(threshold float64) *PlanCache {
	return &PlanCache{
		entries:   make([]cacheEntry, 0),
		threshold: threshold,
	}
}

// Put 将计划存入缓存，以任务描述为键
func (c *PlanCache) Put(task string, plan *Plan) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 检查是否已存在完全相同的任务描述，若存在则更新
	for i, e := range c.entries {
		if e.task == task {
			c.entries[i].plan = plan
			return
		}
	}

	c.entries = append(c.entries, cacheEntry{task: task, plan: plan})
}

// Get 根据任务描述查找最相似的缓存计划
// 返回相似度最高且 >= threshold 的计划；若无匹配则返回 nil
func (c *PlanCache) Get(task string) *Plan {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var bestPlan *Plan
	var bestScore float64

	for _, e := range c.entries {
		score := tokenOverlap(task, e.task)
		if score >= c.threshold && score > bestScore {
			bestScore = score
			bestPlan = e.plan
		}
	}

	return bestPlan
}

// tokenOverlap 计算两个字符串的 Jaccard 系数（基于 token 集合）
// Jaccard(A, B) = |A ∩ B| / |A ∪ B|
func tokenOverlap(a, b string) float64 {
	tokensA := tokenize(a)
	tokensB := tokenize(b)

	if len(tokensA) == 0 && len(tokensB) == 0 {
		return 1.0 // 两个空字符串视为完全相似
	}
	if len(tokensA) == 0 || len(tokensB) == 0 {
		return 0.0
	}

	// 构建集合
	setA := make(map[string]struct{}, len(tokensA))
	for _, t := range tokensA {
		setA[t] = struct{}{}
	}

	setB := make(map[string]struct{}, len(tokensB))
	for _, t := range tokensB {
		setB[t] = struct{}{}
	}

	// 计算交集大小
	intersection := 0
	for t := range setA {
		if _, ok := setB[t]; ok {
			intersection++
		}
	}

	// 计算并集大小 = |A| + |B| - |A ∩ B|
	union := len(setA) + len(setB) - intersection

	if union == 0 {
		return 0.0
	}

	return float64(intersection) / float64(union)
}

// tokenize 简单分词器：按空格、标点、中文标点切分，转小写
func tokenize(s string) []string {
	// 将中文标点替换为空格，便于按空格统一分割
	replacer := strings.NewReplacer(
		"\u2014", " ", // —
		"\u2026", " ", // …
		"\u201c", " ", // "
		"\u201d", " ", // "
		"\u2018", " ", // '
		"\u2019", " ", // '
		"\uff08", " ", // （
		"\uff09", " ", // ）
		"\u300a", " ", // 《
		"\u300b", " ", // 》
	)
	normalized := replacer.Replace(s)

	// 按 Unicode 字符逐字符判断：字母/数字保留，其余视为分隔符
	var tokens []string
	var current strings.Builder

	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, strings.ToLower(current.String()))
			current.Reset()
		}
	}

	for _, r := range normalized {
		if isCJK(r) {
			// CJK 字符逐个拆分为独立 token
			flush()
			tokens = append(tokens, strings.ToLower(string(r)))
		} else if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()

	return tokens
}

// isCJK 判断字符是否为 CJK 表意文字（中日韩统一汉字）
// CJK 字符在 Unicode 中被归类为 Letter，但需要逐字拆分为独立 token
func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || // CJK 统一汉字
		(r >= 0x3400 && r <= 0x4DBF) || // CJK 扩展 A
		(r >= 0xF900 && r <= 0xFAFF) || // CJK 兼容汉字
		(r >= 0x20000 && r <= 0x2A6DF) // CJK 扩展 B
}
