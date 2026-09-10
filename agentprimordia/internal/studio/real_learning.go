package studio

import (
	"context"
	"time"

	"agentprimordia/internal/memory"
)

// RealLearningService 基于真实 SelfModel 数据的 LearningService 实现。
// 替代 demo 实现，将 agent 的实际学习状态暴露给 Studio 面板。
type RealLearningService struct {
	selfModel *memory.SelfModel
}

// NewRealLearningService 创建真实学习数据服务。
func NewRealLearningService(sm *memory.SelfModel) *RealLearningService {
	return &RealLearningService{selfModel: sm}
}

// Stats 返回知识蒸馏统计（基于 SelfModel 的任务统计）。
func (s *RealLearningService) Stats(ctx context.Context) (*LearningStats, error) {
	snap := s.selfModel.Snapshot()
	return &LearningStats{
		TotalInteractions:   snap.TotalTasks,
		TotalDistilled:      len(snap.Capabilities),
		TotalKnowledgeItems: snap.TotalTasks,
	}, nil
}

// Capabilities 返回能力列表（从 SelfModel 快照转换）。
func (s *RealLearningService) Capabilities(ctx context.Context) ([]Capability, error) {
	snap := s.selfModel.Snapshot()
	caps := make([]Capability, 0, len(snap.Capabilities))

	for _, cs := range snap.Capabilities {
		caps = append(caps, Capability{
			Name:        cs.Domain,
			Description: capabilityDescription(cs.Domain, cs.Trend),
			Score:       cs.SuccessRate,
			TimesTested: cs.Successes + cs.Failures,
			TimesPassed: cs.Successes,
		})
	}
	return caps, nil
}

// PipelineStats 返回蒸馏管道统计。
func (s *RealLearningService) PipelineStats(ctx context.Context) (*PipelineStats, error) {
	snap := s.selfModel.Snapshot()
	return &PipelineStats{
		TotalProcessed:  snap.TotalTasks,
		TotalFactsWritten: len(snap.Capabilities),
		TotalPatternsWritten: len(snap.TopFailures),
		LastProcessTime: snap.UpdatedAt.Format(time.RFC3339),
	}, nil
}

// CapabilityHistory 返回能力历史（当前返回单点快照，未来可扩展为时序数据）。
func (s *RealLearningService) CapabilityHistory(ctx context.Context) ([]CapabilityHistory, error) {
	snap := s.selfModel.Snapshot()
	history := make([]CapabilityHistory, 0, len(snap.Capabilities))

	for _, cs := range snap.Capabilities {
		history = append(history, CapabilityHistory{
			Name: cs.Domain,
			History: []CapabilityHistoryPoint{
				{
					Score:      cs.SuccessRate,
					RecordedAt: snap.UpdatedAt.Format(time.RFC3339),
				},
			},
		})
	}
	return history, nil
}

// SelfModelSnapshot 返回完整的 SelfModel 快照（供 ap profile 等消费）。
func (s *RealLearningService) SelfModelSnapshot() *memory.SelfModelSnapshot {
	return s.selfModel.Snapshot()
}

// capabilityDescription 根据域名和趋势生成描述。
func capabilityDescription(domain, trend string) string {
	trendText := "稳定"
	switch trend {
	case "improving":
		trendText = "提升中"
	case "declining":
		trendText = "需关注"
	}
	return domain + " (" + trendText + ")"
}
