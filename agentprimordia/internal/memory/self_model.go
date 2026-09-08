// self_model.go — v5.3 自我模型记忆：Agent 能力画像与失败画像结构化沉淀。
//
// 供 v5.4 学习回路消费：画像条目可查询、可注入系统提示。
package memory

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// CapabilityProfile 单项能力画像
type CapabilityProfile struct {
	Domain      string    `json:"domain"` // 能力域（如 "go_testing"、"sql_migration"）
	Successes   int       `json:"successes"`
	Failures    int       `json:"failures"`
	AvgTurns    float64   `json:"avg_turns"` // 平均完成轮数（效率信号）
	LastUpdated time.Time `json:"last_updated"`
}

// SuccessRate 成功率
func (p CapabilityProfile) SuccessRate() float64 {
	if p.Successes+p.Failures == 0 {
		return 0
	}
	return float64(p.Successes) / float64(p.Successes+p.Failures)
}

// FailurePattern 失败模式画像
type FailurePattern struct {
	Signature  string    `json:"signature"` // 失败签名（归一化错误类别）
	Count      int       `json:"count"`
	LastSeen   time.Time `json:"last_seen"`
	Mitigation string    `json:"mitigation,omitempty"` // 已知缓解手段
}

// SelfModel Agent 自我模型：能力画像 + 失败画像
type SelfModel struct {
	mu          sync.RWMutex
	caps        map[string]*CapabilityProfile
	failures    map[string]*FailurePattern
	maxFailures int // 失败画像容量上限（防膨胀）
}

// NewSelfModel 创建自我模型
func NewSelfModel() *SelfModel {
	return &SelfModel{
		caps:        make(map[string]*CapabilityProfile),
		failures:    make(map[string]*FailurePattern),
		maxFailures: 100,
	}
}

// RecordOutcome 记录一次任务结果：domain 为能力域，success 为成败，
// turns 为消耗轮数；失败时 signature 为归一化失败签名。
func (m *SelfModel) RecordOutcome(domain string, success bool, turns int, signature string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()

	cp := m.caps[domain]
	if cp == nil {
		cp = &CapabilityProfile{Domain: domain}
		m.caps[domain] = cp
	}
	if success {
		cp.Successes++
	} else {
		cp.Failures++
		if signature != "" {
			fp := m.failures[signature]
			if fp == nil {
				fp = &FailurePattern{Signature: signature}
				m.failures[signature] = fp
			}
			fp.Count++
			fp.LastSeen = now
		}
	}
	if turns > 0 {
		total := cp.Successes + cp.Failures
		cp.AvgTurns = (cp.AvgTurns*float64(total-1) + float64(turns)) / float64(total)
	}
	cp.LastUpdated = now
}

// SetMitigation 登记某失败签名的缓解手段（学习回路写入）。
// upsert 语义：签名尚无失败记录时先建条目——缓解知识可先于失败到达。
func (m *SelfModel) SetMitigation(signature, mitigation string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fp := m.failures[signature]
	if fp == nil {
		fp = &FailurePattern{Signature: signature}
		m.failures[signature] = fp
	}
	fp.Mitigation = mitigation
}

// WeakDomains 返回弱项能力域（样本 ≥ minSamples 且成功率 < threshold），按成功率升序
func (m *SelfModel) WeakDomains(minSamples, threshold int) []CapabilityProfile {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var weak []CapabilityProfile
	for _, cp := range m.caps {
		if cp.Successes+cp.Failures >= minSamples && cp.SuccessRate() < float64(threshold)/100 {
			weak = append(weak, *cp)
		}
	}
	sort.Slice(weak, func(i, j int) bool { return weak[i].SuccessRate() < weak[j].SuccessRate() })
	return weak
}

// TopFailures 返回最高频的前 n 个失败模式
func (m *SelfModel) TopFailures(n int) []FailurePattern {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]FailurePattern, 0, len(m.failures))
	for _, fp := range m.failures {
		out = append(out, *fp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// InjectIntoSystemPrompt 将画像摘要注入系统提示（供策略层消费）：
// 包含强项 top3 与弱项 top3 及其缓解手段。
func (m *SelfModel) InjectIntoSystemPrompt(base string) string {
	m.mu.RLock()
	caps := make([]CapabilityProfile, 0, len(m.caps))
	for _, cp := range m.caps {
		if cp.Successes+cp.Failures > 0 {
			caps = append(caps, *cp)
		}
	}
	failures := m.TopFailures(3)
	m.mu.RUnlock()

	if len(caps) == 0 {
		return base
	}
	sort.Slice(caps, func(i, j int) bool { return caps[i].SuccessRate() > caps[j].SuccessRate() })

	var sb strings.Builder
	sb.WriteString(base)
	sb.WriteString("\n\n## 自我能力画像（历史统计，供决策参考）\n")
	sb.WriteString("### 强项\n")
	for i, cp := range caps {
		if i >= 3 {
			break
		}
		fmt.Fprintf(&sb, "- %s：成功率 %.0f%%（%d 次）\n", cp.Domain, cp.SuccessRate()*100, cp.Successes+cp.Failures)
	}
	sb.WriteString("### 弱项（需谨慎，考虑更多验证步骤）\n")
	n := 0
	for i := len(caps) - 1; i >= 0 && n < 3; i-- {
		cp := caps[i]
		if cp.SuccessRate() >= 0.6 {
			continue
		}
		fmt.Fprintf(&sb, "- %s：成功率 %.0f%%（%d 次）\n", cp.Domain, cp.SuccessRate()*100, cp.Successes+cp.Failures)
		n++
	}
	if len(failures) > 0 {
		sb.WriteString("### 高频失败模式及缓解\n")
		for _, fp := range failures {
			if fp.Mitigation != "" {
				fmt.Fprintf(&sb, "- %s（%d 次）：%s\n", fp.Signature, fp.Count, fp.Mitigation)
			}
		}
	}
	return sb.String()
}

// Marshal 导出（持久化/跨节点同步）
func (m *SelfModel) Marshal() ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return json.Marshal(struct {
		Caps     map[string]*CapabilityProfile `json:"caps"`
		Failures map[string]*FailurePattern    `json:"failures"`
	}{m.caps, m.failures})
}

// SelfModelSnapshot SelfModel 的线程安全快照，供 Studio/API 消费。
type SelfModelSnapshot struct {
	Capabilities []CapabilitySnapshot `json:"capabilities"`
	TopFailures  []FailurePattern     `json:"top_failures"`
	TotalTasks   int                  `json:"total_tasks"`
	SuccessRate  float64              `json:"success_rate"`
	AvgTurns     float64              `json:"avg_turns"`
	UpdatedAt    time.Time            `json:"updated_at"`
}

// CapabilitySnapshot 单项能力的快照视图
type CapabilitySnapshot struct {
	Domain      string  `json:"domain"`
	Successes   int     `json:"successes"`
	Failures    int     `json:"failures"`
	SuccessRate float64 `json:"success_rate"`
	AvgTurns    float64 `json:"avg_turns"`
	Trend       string  `json:"trend"` // "improving" | "stable" | "declining"
}

// Snapshot 生成线程安全的 SelfModel 快照。
func (m *SelfModel) Snapshot() *SelfModelSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	snap := &SelfModelSnapshot{
		Capabilities: make([]CapabilitySnapshot, 0, len(m.caps)),
		TopFailures:  make([]FailurePattern, 0),
		UpdatedAt:    time.Time{},
	}

	var totalSuccess, totalFail int
	var totalTurns float64

	for _, cp := range m.caps {
		total := cp.Successes + cp.Failures
		if total == 0 {
			continue
		}
		totalSuccess += cp.Successes
		totalFail += cp.Failures
		totalTurns += cp.AvgTurns * float64(total)

		cs := CapabilitySnapshot{
			Domain:      cp.Domain,
			Successes:   cp.Successes,
			Failures:    cp.Failures,
			SuccessRate: cp.SuccessRate(),
			AvgTurns:    cp.AvgTurns,
			Trend:       "stable",
		}
		// 简单趋势判断：成功率 > 70% 且样本 >= 3 视为 improving
		if cp.SuccessRate() > 0.7 && total >= 3 {
			cs.Trend = "improving"
		} else if cp.SuccessRate() < 0.4 && total >= 3 {
			cs.Trend = "declining"
		}
		snap.Capabilities = append(snap.Capabilities, cs)

		if cp.LastUpdated.After(snap.UpdatedAt) {
			snap.UpdatedAt = cp.LastUpdated
		}
	}

	totalTasks := totalSuccess + totalFail
	snap.TotalTasks = totalTasks
	if totalTasks > 0 {
		snap.SuccessRate = float64(totalSuccess) / float64(totalTasks)
		snap.AvgTurns = totalTurns / float64(totalTasks)
	}

	// 排序能力：按成功率降序
	sort.Slice(snap.Capabilities, func(i, j int) bool {
		return snap.Capabilities[i].SuccessRate > snap.Capabilities[j].SuccessRate
	})

	// 复制 top failures
	for _, fp := range m.failures {
		snap.TopFailures = append(snap.TopFailures, *fp)
	}
	sort.Slice(snap.TopFailures, func(i, j int) bool {
		return snap.TopFailures[i].Count > snap.TopFailures[j].Count
	})
	if len(snap.TopFailures) > 5 {
		snap.TopFailures = snap.TopFailures[:5]
	}

	return snap
}

// TopCapabilities 返回成功率最高的前 n 个能力域。
func (m *SelfModel) TopCapabilities(n int) []CapabilityProfile {
	m.mu.RLock()
	defer m.mu.RUnlock()

	caps := make([]CapabilityProfile, 0, len(m.caps))
	for _, cp := range m.caps {
		if cp.Successes+cp.Failures > 0 {
			caps = append(caps, *cp)
		}
	}
	sort.Slice(caps, func(i, j int) bool {
		if caps[i].SuccessRate() != caps[j].SuccessRate() {
			return caps[i].SuccessRate() > caps[j].SuccessRate()
		}
		// 成功率相同时，样本数多的排前面（更可靠）
		totalI := caps[i].Successes + caps[i].Failures
		totalJ := caps[j].Successes + caps[j].Failures
		return totalI > totalJ
	})
	if len(caps) > n {
		caps = caps[:n]
	}
	return caps
}

// GrowthMetrics 返回各领域成功率，供趋势分析消费。
func (m *SelfModel) GrowthMetrics() map[string]float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	metrics := make(map[string]float64, len(m.caps))
	for domain, cp := range m.caps {
		metrics[domain] = cp.SuccessRate()
	}
	return metrics
}
