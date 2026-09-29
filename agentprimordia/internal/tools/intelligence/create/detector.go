// detector.go — 轨迹缺口检测器（从失败轨迹识别缺口）
package create

import (
	"sort"
	"context"
	"strings"
	"sync"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools/intelligence"
)

// TraceGapDetector 轨迹缺口检测器（并发安全）
// 分析 ToolCallRecord 中的失败模式，从错误消息中提取缺口键
type TraceGapDetector struct {
	mu      sync.Mutex
	records []intelligence.ToolCallRecord
}

// NewTraceGapDetector 创建检测器
func NewTraceGapDetector() *TraceGapDetector {
	return &TraceGapDetector{}
}

// AddRecords 添加调用记录
func (d *TraceGapDetector) AddRecords(records []intelligence.ToolCallRecord) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.records = append(d.records, records...)
}

// Detect 分析轨迹，返回缺口候选列表
// 按错误消息中的关键词聚类失败模式
func (d *TraceGapDetector) Detect(_ context.Context, trace []intelligence.ToolCallRecord) ([]intelligence.GapCandidate, error) {
	d.mu.Lock()
	all := append(d.records, trace...)
	d.mu.Unlock()

	// 按错误关键词聚类
	type gapKey struct {
		key   string
		first time.Time
		last  time.Time
	}
	clusters := make(map[string]*gapKey)
	errors := make(map[string]string) // key -> 样本错误

	for _, rec := range all {
		if rec.Success || rec.Error == "" {
			continue
		}

		key := extractGapKey(rec.Error)
		if key == "" {
			continue
		}

		cluster, ok := clusters[key]
		if !ok {
			cluster = &gapKey{
				key:   key,
				first: rec.Timestamp,
				last:  rec.Timestamp,
			}
			clusters[key] = cluster
			errors[key] = rec.Error
		} else {
			if rec.Timestamp.Before(cluster.first) {
				cluster.first = rec.Timestamp
			}
			if rec.Timestamp.After(cluster.last) {
				cluster.last = rec.Timestamp
			}
		}
	}

	// 转换为 GapCandidate 列表（**按键排序保证确定性**）。
	// 修复（2026-09-28）：clusters 为 map，直接 range 迭代使缺口列表
	// 顺序运行间抖动（演示输出 3↔4 个缺口时看似计数变化，实为顺序+
	// 展示问题）；按键升序排序后输出稳定。
	keys := make([]string, 0, len(clusters))
	for key := range clusters {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	result := make([]intelligence.GapCandidate, 0, len(clusters))
	for _, key := range keys {
		cluster := clusters[key]
		// 计算出现次数
		count := 0
		for _, rec := range all {
			if !rec.Success && extractGapKey(rec.Error) == key {
				count++
			}
		}

		result = append(result, intelligence.GapCandidate{
			Kind:        "missing_tool",
			Key:         key,
			Count:       count,
			SampleError: errors[key],
			FirstSeen:   cluster.first,
			LastSeen:    cluster.last,
		})
	}

	return result, nil
}

// extractGapKey 从错误消息中提取缺口键
// 策略：提取第一个有意义的错误片段作为缺口标识
func extractGapKey(errMsg string) string {
	if errMsg == "" {
		return ""
	}

	// 常见错误模式（**有序切片，首匹配优先**）。
	// 修复（2026-09-28）：原为 map，Go map 迭代顺序随机——同一消息
	// 命中多个 pattern 时（如 "parse error: invalid CSV format"）返回
	// 的 gap key 不确定，导致缺口去重计数与键名运行间抖动。改为有序
	// 切片后行为确定；声明序即优先级（更具体的模式靠前）。
	patterns := []struct {
		pattern string
		key     string
	}{
		{"not found", "missing_resource"},
		{"no such file", "missing_file"},
		{"permission denied", "missing_permission"},
		{"connection refused", "missing_service"},
		{"timeout", "missing_timeout_handler"},
		{"unsupported", "missing_capability"},
		{"not implemented", "missing_feature"},
		{"parse error", "missing_parser"},
		{"invalid format", "missing_formatter"},
		{"out of memory", "missing_resource_limit"},
	}

	lower := strings.ToLower(errMsg)
	for _, p := range patterns {
		if strings.Contains(lower, p.pattern) {
			return p.key
		}
	}

	// 无匹配模式时，取错误消息前 20 个字符作为键
	if len(errMsg) > 20 {
		return errMsg[:20]
	}
	return errMsg
}
