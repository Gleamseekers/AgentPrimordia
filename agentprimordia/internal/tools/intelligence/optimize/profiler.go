// profiler.go — 工具性能画像器（per-tool 成功率/延迟/token 统计 + P95）
package optimize

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/tools/intelligence"
)

// usageRecord 内部使用记录
type usageRecord struct {
	Success  bool
	Duration time.Duration
	Tokens   int
	At       time.Time
}

// InMemoryProfiler 内存版工具性能画像器（并发安全）
type InMemoryProfiler struct {
	mu      sync.RWMutex
	records map[string][]usageRecord // toolName -> 使用记录列表
}

// NewInMemoryProfiler 创建画像器
func NewInMemoryProfiler() *InMemoryProfiler {
	return &InMemoryProfiler{records: make(map[string][]usageRecord)}
}

// Record 记录一次工具使用
func (p *InMemoryProfiler) Record(_ context.Context, rec intelligence.ToolUsageRecord) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.records[rec.ToolName] = append(p.records[rec.ToolName], usageRecord{
		Success:  rec.Success,
		Duration: rec.Duration,
		Tokens:   rec.Tokens,
		At:       time.Now(),
	})
	return nil
}

// Profile 获取单个工具的性能画像
func (p *InMemoryProfiler) Profile(_ context.Context, toolName string) (*intelligence.ToolProfile, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	recs := p.records[toolName]
	if len(recs) == 0 {
		return &intelligence.ToolProfile{ToolName: toolName}, nil
	}
	return computeProfile(toolName, recs), nil
}

// AllProfiles 获取所有工具的性能画像
func (p *InMemoryProfiler) AllProfiles(_ context.Context) (map[string]*intelligence.ToolProfile, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make(map[string]*intelligence.ToolProfile, len(p.records))
	for name, recs := range p.records {
		result[name] = computeProfile(name, recs)
	}
	return result, nil
}

// computeProfile 从记录列表计算画像（无锁，调用方须持锁）
func computeProfile(toolName string, recs []usageRecord) *intelligence.ToolProfile {
	total := len(recs)
	var successCount int
	var totalDuration time.Duration
	var totalTokens int
	var lastUsed time.Time

	durations := make([]time.Duration, total)
	for i, r := range recs {
		if r.Success {
			successCount++
		}
		totalDuration += r.Duration
		totalTokens += r.Tokens
		durations[i] = r.Duration
		if r.At.After(lastUsed) {
			lastUsed = r.At
		}
	}

	// 计算 P95
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p95Idx := int(float64(total) * 0.95)
	if p95Idx >= total {
		p95Idx = total - 1
	}

	return &intelligence.ToolProfile{
		ToolName:    toolName,
		TotalCalls:  total,
		SuccessRate: float64(successCount) / float64(total),
		AvgDuration: totalDuration / time.Duration(total),
		P95Duration: durations[p95Idx],
		AvgTokens:   totalTokens / total,
		LastUsed:    lastUsed,
	}
}
