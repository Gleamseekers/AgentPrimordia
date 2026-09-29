// all_capabilities_test.go — v5.0-2 全能力整合端到端验收套件
//
// 一条链路验证四跃迁能力（autonomy / skills / realtime / a2a）+
// Studio 真实接线协同工作（全部 mock 驱动，CI 可跑）：
//
//	自治目标完成 → 技能习得入库 → 实时语音会话（流式）→ 视觉帧分析
//	→ A2A 跨协议委托 → Studio 面板读到真实数据
package e2e

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/autonomy"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/realtime"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/skills"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/studio"
	ap "github.com/Gleamseekers/AgentPrimordia/agentprimordia/pkg"
)

// allCapExecutor 全能力链路步骤执行器（确定性）。
type allCapExecutor struct{}

func (e *allCapExecutor) ExecuteStep(_ context.Context, step autonomy.PlanStep) (string, error) {
	return "ok:" + step.ID, nil
}

// allCapDistiller 全能力链路技能提炼器（确定性）。
type allCapDistiller struct{}

func (d *allCapDistiller) Distill(_ context.Context, t skills.Trajectory) (*skills.Skill, error) {
	steps := make([]skills.StepDef, len(t.Records))
	for i, r := range t.Records {
		steps[i] = skills.StepDef{ID: "s" + string(rune('1'+i)), ToolName: r.ToolName, Description: r.ToolName + " 操作"}
	}
	s := skills.NewSkill("链路技能", "全能力链路习得", steps)
	s.Tags = []string{"e2e"}
	return s, nil
}

// TestE2E_AllCapabilities v5.0-2：全能力整合验收。
func TestE2E_AllCapabilities(t *testing.T) {
	ctx := context.Background()

	// 1. autonomy：自治目标完成
	rt := autonomy.NewAutonomyRuntime(autonomy.RuntimeConfig{StepExecutor: &allCapExecutor{}})
	goal := rt.SubmitGoal("监控数据并修复", autonomy.GoalConfig{MaxRetries: 2})
	plan := autonomy.NewGoalPlan(goal.ID, []autonomy.PlanStep{
		{ID: "collect", Description: "采集", Strategy: autonomy.StepStrategySequential},
		{ID: "fix", Description: "修复", DependsOn: []string{"collect"}, Strategy: autonomy.StepStrategySequential},
	})
	if err := rt.SetPlan(goal.ID, plan); err != nil {
		t.Fatalf("autonomy SetPlan: %v", err)
	}
	if err := rt.ExecuteGoal(ctx, goal.ID); err != nil {
		t.Fatalf("autonomy ExecuteGoal: %v", err)
	}
	if err := rt.CompleteGoal(goal.ID); err != nil {
		t.Fatalf("autonomy CompleteGoal: %v", err)
	}
	if g, _ := rt.GetGoal(goal.ID); g.State != autonomy.GoalDone {
		t.Fatalf("autonomy 目标终态 = %s, want done", g.State)
	}

	// 2. skills：技能习得 → 入库 → 匹配
	store := skills.NewStore()
	acq := skills.NewAcquisition(&allCapDistiller{})
	trajectory := skills.Trajectory{
		TaskDescription: "链路任务",
		Success:         true,
		Timestamp:       time.Now(),
		Records: []skills.ToolCallRecord{
			{ToolName: "query", Success: true, Duration: 10 * time.Millisecond},
			{ToolName: "fix", Success: true, Duration: 10 * time.Millisecond},
		},
	}
	acquired, err := acq.Acquire(ctx, trajectory)
	if err != nil {
		t.Fatalf("skills Acquire: %v", err)
	}
	acquired.Activate()
	store.Save(acquired)
	matcher := skills.NewMatcher(store, skills.MatcherConfig{HighThreshold: 0.3})
	if m := matcher.Match("链路技能"); m == nil {
		t.Fatal("skills 匹配失败（习得技能未生效）")
	}

	// 3. realtime：流式语音会话 + 视觉帧分析
	rtrt := realtime.NewRuntime(realtime.RuntimeConfig{
		React:      &streamBridge{chunks: []string{"你好", "，链路"}},
		Multimodal: &visionProvider{desc: "画面中有服务器"},
	})
	rtrt.OpenSession("e2e-voice")
	ch, err := rtrt.ProcessTurnStream(ctx, "e2e-voice", "打招呼")
	if err != nil {
		t.Fatalf("realtime ProcessTurnStream: %v", err)
	}
	var full strings.Builder
	done := false
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("realtime 流错误: %v", chunk.Err)
		}
		if chunk.Done {
			done = true
		}
		full.WriteString(chunk.Text)
	}
	if !done || full.String() != "你好，链路" {
		t.Fatalf("realtime 流式响应 = %q done=%v", full.String(), done)
	}
	if err := rtrt.PushVision(ctx, "e2e-voice", []byte("frame"), 320, 240); err != nil {
		t.Fatalf("realtime PushVision: %v", err)
	}
	desc, err := rtrt.AnalyzeLatestFrame(ctx, "e2e-voice")
	if err != nil || desc != "画面中有服务器" {
		t.Fatalf("realtime 视觉分析 = %q err=%v", desc, err)
	}
	rtrt.CloseSession("e2e-voice")

	// 4. a2a：开放协议委托任务
	card := ap.OpenAgentCard{Name: "e2e-agent", Version: "2.0.0", URL: "http://e2e"}
	srv := ap.NewOpenInteropServer(card, ap.DefaultInteropConfig())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	client := ap.NewOpenInteropClient(ts.URL)
	discovered, err := client.FetchAgentCard(ctx)
	if err != nil || discovered.Name != "e2e-agent" {
		t.Fatalf("a2a 发现失败: %v", err)
	}
	task, err := client.SendTask(ctx, ap.NewTextMessage("user", "处理数据"))
	if err != nil || task.ID == "" {
		t.Fatalf("a2a 委托失败: %v", err)
	}

	// 5. studio：注入真实引擎 → 面板读到真实数据
	h := studio.NewStudioHandler(
		studio.WithAutonomy(studio.NewAutonomyServiceAdapter(rt)),
		studio.WithSkills(studio.NewSkillServiceAdapter(store)),
		studio.WithRealtime(studio.NewRealtimeServiceAdapter(rtrt.Hub, rtrt.Events)),
	)
	sh := httptest.NewServer(h)
	defer sh.Close()

	checkJSON(t, sh.URL+"/api/v1/autonomy/goals", "goal")
	checkJSON(t, sh.URL+"/api/v1/skills", "链路技能")
	checkJSON(t, sh.URL+"/api/v1/realtime/events", "session.closed")

	t.Log("✅ 全能力整合验收通过：autonomy → skills → realtime → a2a → studio 全链路")
}

// checkJSON 请求 Studio 端点并断言响应体含关键字。
func checkJSON(t *testing.T, url, keyword string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("%s: %v", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), keyword) {
		t.Fatalf("%s 响应不含 %q: %s", url, keyword, body)
	}
}

// streamBridge 流式桥（测试替身）。
type streamBridge struct {
	chunks []string
}

func (b *streamBridge) StreamReason(_ context.Context, _ realtime.FusedInput) (<-chan string, error) {
	ch := make(chan string, len(b.chunks))
	for _, c := range b.chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func (b *streamBridge) Reason(_ context.Context, _ realtime.FusedInput) (string, error) {
	return strings.Join(b.chunks, ""), nil
}

// visionProvider 视觉分析（测试替身）。
type visionProvider struct {
	desc string
}

func (p *visionProvider) AnalyzeFrame(_ context.Context, _ realtime.VideoFrame) (string, error) {
	return p.desc, nil
}
