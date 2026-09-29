package studio

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent/autonomy"
	"github.com/Gleamseekers/AgentPrimordia/internal/agent/realtime"
	"github.com/Gleamseekers/AgentPrimordia/internal/agent/skills"
)

// v3.3-v3.6 Studio 面板后端端点测试：路由注册 + demo 数据源标注 + 真实引擎注入。

// TestStudioV3X_AutonomyEndpoints 默认 demo：200 + X-Data-Source: demo + 空数组。
func TestStudioV3X_AutonomyEndpoints(t *testing.T) {
	h := NewStudioHandler()
	for _, p := range []string{"/api/v1/autonomy/goals", "/api/v1/autonomy/alerts"} {
		rec := doJSON(t, h, "GET", p, "")
		if rec.Code != http.StatusOK {
			t.Errorf("%s status=%d, want 200", p, rec.Code)
		}
		if rec.Header().Get("X-Data-Source") != "demo" {
			t.Errorf("%s 未注入时应标 X-Data-Source: demo，got %q", p, rec.Header().Get("X-Data-Source"))
		}
	}
	if rec := doJSON(t, h, "POST", "/api/v1/autonomy/goals/g1/resume", ""); rec.Code != http.StatusOK {
		t.Errorf("resume status=%d, want 200", rec.Code)
	}
}

// TestStudioV3X_AutonomyInjected 注入真实 AutonomyRuntime：目标与告警来自真实数据，无 demo 头。
func TestStudioV3X_AutonomyInjected(t *testing.T) {
	rt := autonomy.NewAutonomyRuntime(autonomy.RuntimeConfig{
		StepExecutor:  &stubStepExecutor{},
		MonitorConfig: autonomy.MonitorConfig{},
	})
	rt.SubmitGoal("监控数据异常并修复", autonomy.GoalConfig{Priority: autonomy.PriorityHigh})
	rt.GetMonitor().ReportHeartbeat("g", 0.5)
	rt.GetMonitor().ReportAnomaly("g", autonomy.AlertError, "LLM 调用超时")

	h := NewStudioHandler(WithAutonomy(NewAutonomyServiceAdapter(rt)))

	rec := doJSON(t, h, "GET", "/api/v1/autonomy/goals", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("goals status=%d", rec.Code)
	}
	if rec.Header().Get("X-Data-Source") != "" {
		t.Errorf("注入后不应标 demo 头，got %q", rec.Header().Get("X-Data-Source"))
	}
	goals := decode[[]AutonomyGoal](t, rec)
	if len(goals) != 1 {
		t.Fatalf("goals = %d, want 1", len(goals))
	}
	g := goals[0]
	if g.Description != "监控数据异常并修复" || g.State != "created" || g.Priority != int(autonomy.PriorityHigh) {
		t.Errorf("goal = %+v, want description/state/priority 真实数据", g)
	}
	if g.CreatedAt == "" {
		t.Error("createdAt 为空")
	}

	rec = doJSON(t, h, "GET", "/api/v1/autonomy/alerts", "")
	alerts := decode[[]AutonomyAlert](t, rec)
	if len(alerts) != 1 || alerts[0].Level != "error" || alerts[0].Message != "LLM 调用超时" {
		t.Errorf("alerts = %+v, want 1 条真实告警", alerts)
	}
}

// TestStudioV3X_SkillsEndpoints 默认 demo：200 + demo 头。
func TestStudioV3X_SkillsEndpoints(t *testing.T) {
	h := NewStudioHandler()
	rec := doJSON(t, h, "GET", "/api/v1/skills", "")
	if rec.Code != http.StatusOK {
		t.Errorf("skills list status=%d", rec.Code)
	}
	if rec.Header().Get("X-Data-Source") != "demo" {
		t.Errorf("未注入时应标 demo 头，got %q", rec.Header().Get("X-Data-Source"))
	}
	if rec := doJSON(t, h, "POST", "/api/v1/skills/s1/verify", ""); rec.Code != http.StatusOK {
		t.Errorf("verify status=%d", rec.Code)
	}
	if rec := doJSON(t, h, "POST", "/api/v1/skills/s1/deprecate", ""); rec.Code != http.StatusOK {
		t.Errorf("deprecate status=%d", rec.Code)
	}
}

// TestStudioV3X_SkillsInjected 注入真实技能库：技能条目来自真实数据，无 demo 头。
func TestStudioV3X_SkillsInjected(t *testing.T) {
	store := skills.NewStore()
	s := skills.NewSkill("数据修复", "自动修复异常数据", []skills.StepDef{{ID: "s1", ToolName: "fix"}})
	s.Activate()
	s.RecordUsage(true)
	s.Tags = []string{"数据"}
	store.Save(s)

	h := NewStudioHandler(WithSkills(NewSkillServiceAdapter(store)))
	rec := doJSON(t, h, "GET", "/api/v1/skills", "")
	if rec.Header().Get("X-Data-Source") != "" {
		t.Errorf("注入后不应标 demo 头，got %q", rec.Header().Get("X-Data-Source"))
	}
	items := decode[[]SkillEntry](t, rec)
	if len(items) != 1 {
		t.Fatalf("skills = %d, want 1", len(items))
	}
	entry := items[0]
	if entry.Name != "数据修复" || entry.Status != "active" || entry.Version == "" {
		t.Errorf("entry = %+v, want 真实技能数据", entry)
	}
	if len(entry.Tags) != 1 || entry.Tags[0] != "数据" {
		t.Errorf("tags = %v, want [数据]", entry.Tags)
	}
}

// TestStudioV3X_A2AInteropStatus 静态互操作状态端点。
func TestStudioV3X_A2AInteropStatus(t *testing.T) {
	h := NewStudioHandler()
	rec := doJSON(t, h, "GET", "/api/v1/a2a/interop/status", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	status := decode[map[string]any](t, rec)
	if status["mode"] != "compatible" {
		t.Errorf("mode=%v, want compatible", status["mode"])
	}
	if status["agentCardExposed"] != true {
		t.Errorf("agentCardExposed=%v", status["agentCardExposed"])
	}
}

// TestStudioV3X_RealtimeEndpoints 默认 demo：200 + demo 头。
func TestStudioV3X_RealtimeEndpoints(t *testing.T) {
	h := NewStudioHandler()
	for _, p := range []string{"/api/v1/realtime/sessions", "/api/v1/realtime/events"} {
		rec := doJSON(t, h, "GET", p, "")
		if rec.Code != http.StatusOK {
			t.Errorf("%s status=%d, want 200", p, rec.Code)
		}
		if rec.Header().Get("X-Data-Source") != "demo" {
			t.Errorf("%s 未注入时应标 demo 头，got %q", p, rec.Header().Get("X-Data-Source"))
		}
	}
	if rec := doJSON(t, h, "POST", "/api/v1/realtime/sessions/s1/barge-in", ""); rec.Code != http.StatusOK {
		t.Errorf("barge-in status=%d", rec.Code)
	}
}

// TestStudioV3X_RealtimeInjected 注入真实 RealtimeHub + EventBus：会话与事件来自真实数据。
func TestStudioV3X_RealtimeInjected(t *testing.T) {
	hub := realtime.NewRealtimeHub(realtime.HubConfig{})
	bus := realtime.NewEventBus()
	s := hub.CreateSession("voice-1")
	_ = s.TransitionTo(realtime.SessionListening, "start")
	bus.Publish(realtime.RealtimeEvent{Type: realtime.EventSessionCreated, SessionID: "voice-1"})

	h := NewStudioHandler(WithRealtime(NewRealtimeServiceAdapter(hub, bus)))

	rec := doJSON(t, h, "GET", "/api/v1/realtime/sessions", "")
	if rec.Header().Get("X-Data-Source") != "" {
		t.Errorf("注入后不应标 demo 头，got %q", rec.Header().Get("X-Data-Source"))
	}
	sessions := decode[[]RealtimeSessionInfo](t, rec)
	if len(sessions) != 1 || sessions[0].ID != "voice-1" || sessions[0].State != "listening" {
		t.Errorf("sessions = %+v, want voice-1 listening", sessions)
	}

	rec = doJSON(t, h, "GET", "/api/v1/realtime/events", "")
	events := decode[[]RealtimeEventInfo](t, rec)
	if len(events) != 1 || events[0].Type != "session.created" || events[0].SessionID != "voice-1" {
		t.Errorf("events = %+v, want session.created", events)
	}
	if events[0].Timestamp == "" {
		t.Error("event timestamp 为空")
	}
}

// stubStepExecutor 注入测试用的步骤执行器。
type stubStepExecutor struct{}

func (s *stubStepExecutor) ExecuteStep(_ context.Context, _ autonomy.PlanStep) (string, error) {
	return "ok", nil
}

// 确保 time 被引用（CreatedAt 断言辅助）
var _ = time.RFC3339
