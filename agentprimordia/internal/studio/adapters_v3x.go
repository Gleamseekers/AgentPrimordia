// adapters_v3x.go — v3.3-v3.6 真实引擎 → Studio 服务接口适配器
//
// 将 internal/agent/{autonomy,skills,realtime} 的真实运行时适配为
// AutonomyService / SkillService / RealtimeService，经 WithAutonomy /
// WithSkills / WithRealtime 注入 StudioHandler。
package studio

import (
	"context"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent/autonomy"
	"github.com/Gleamseekers/AgentPrimordia/internal/agent/realtime"
	"github.com/Gleamseekers/AgentPrimordia/internal/agent/skills"
)

// ===== AutonomyService 适配器 =====

// autonomyServiceAdapter 将 AutonomyRuntime 适配为 AutonomyService。
type autonomyServiceAdapter struct {
	runtime *autonomy.AutonomyRuntime
}

// NewAutonomyServiceAdapter 创建自治运行时适配器。
func NewAutonomyServiceAdapter(rt *autonomy.AutonomyRuntime) AutonomyService {
	return &autonomyServiceAdapter{runtime: rt}
}

func (a *autonomyServiceAdapter) Goals(ctx context.Context) ([]AutonomyGoal, error) {
	goals := a.runtime.ListGoals()
	monitor := a.runtime.GetMonitor()
	out := make([]AutonomyGoal, 0, len(goals))
	for _, g := range goals {
		view := g.Snapshot()
		progress := 0.0
		if monitor != nil {
			progress = monitor.GetStatus(view.ID).Progress
		}
		out = append(out, AutonomyGoal{
			ID:          view.ID,
			Description: view.Description,
			State:       view.State.String(),
			Priority:    int(view.Priority),
			Progress:    progress,
			RetryCount:  view.RetryCount,
			CreatedAt:   view.CreatedAt.Format(time.RFC3339),
		})
	}
	return out, nil
}

func (a *autonomyServiceAdapter) Alerts(ctx context.Context) ([]AutonomyAlert, error) {
	monitor := a.runtime.GetMonitor()
	if monitor == nil {
		return []AutonomyAlert{}, nil
	}
	alerts := monitor.RecentAlerts()
	out := make([]AutonomyAlert, 0, len(alerts))
	for _, al := range alerts {
		out = append(out, AutonomyAlert{
			GoalID:    al.GoalID,
			Level:     string(al.Level),
			Message:   al.Message,
			Timestamp: al.Timestamp.Format(time.RFC3339),
		})
	}
	return out, nil
}

// ===== SkillService 适配器 =====

// skillServiceAdapter 将技能库 Store 适配为 SkillService。
type skillServiceAdapter struct {
	store *skills.Store
}

// NewSkillServiceAdapter 创建技能库适配器。
func NewSkillServiceAdapter(store *skills.Store) SkillService {
	return &skillServiceAdapter{store: store}
}

func (a *skillServiceAdapter) List(ctx context.Context) ([]SkillEntry, error) {
	items := a.store.List()
	out := make([]SkillEntry, 0, len(items))
	for _, s := range items {
		out = append(out, SkillEntry{
			ID:          s.ID,
			Name:        s.Name,
			Description: s.Description,
			Version:     s.Version.String(),
			Status:      string(s.Status),
			UsageCount:  s.UsageCount,
			SuccessRate: s.SuccessRate,
			Tags:        s.Tags,
		})
	}
	return out, nil
}

// ===== RealtimeService 适配器 =====

// realtimeServiceAdapter 将 RealtimeHub + EventBus 适配为 RealtimeService。
type realtimeServiceAdapter struct {
	hub *realtime.RealtimeHub
	bus *realtime.EventBus
}

// NewRealtimeServiceAdapter 创建实时运行时适配器（hub 提供会话，bus 提供事件历史）。
func NewRealtimeServiceAdapter(hub *realtime.RealtimeHub, bus *realtime.EventBus) RealtimeService {
	return &realtimeServiceAdapter{hub: hub, bus: bus}
}

func (a *realtimeServiceAdapter) Sessions(ctx context.Context) ([]RealtimeSessionInfo, error) {
	sessions := a.hub.ListSessions()
	out := make([]RealtimeSessionInfo, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, RealtimeSessionInfo{
			ID:        s.ID,
			State:     s.State.String(),
			CreatedAt: s.CreatedAt.Format(time.RFC3339),
		})
	}
	return out, nil
}

func (a *realtimeServiceAdapter) Events(ctx context.Context) ([]RealtimeEventInfo, error) {
	events := a.bus.RecentEvents()
	out := make([]RealtimeEventInfo, 0, len(events))
	for _, e := range events {
		out = append(out, RealtimeEventInfo{
			Type:      string(e.Type),
			SessionID: e.SessionID,
			Timestamp: e.Timestamp.Format(time.RFC3339),
		})
	}
	return out, nil
}
