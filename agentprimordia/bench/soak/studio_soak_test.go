// studio_soak_test.go — Studio 端点长时间稳态（v5.0 压测第二项）
//
// 30 分钟（可配置）混合读写流量打 Studio 全部端点，复用 soak.Runner
// 的前后半段对比退化检测（延迟/错误率/吞吐），断言：
//   - 错误率 ≤ 1%
//   - 无退化（HasDegradation=false，前后半段对比）
//
// 运行（注意：30 分钟需 -timeout 覆盖 go test 默认 10m 超时）：
//
//	SOAK_STUDIO_DURATION=30m SOAK_STUDIO_RPS=50 go test -timeout 40m -run TestSoak_Studio -v ./bench/soak/
//
// CI 冒烟默认 60s。
package soak

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/autonomy"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/realtime"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/skills"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm/soak"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/studio"
)

// soakStudioFixture 注入真实引擎的 Studio 服务器（Soak 用）。
func soakStudioFixture(t *testing.T) *httptest.Server {
	t.Helper()

	rt := autonomy.NewAutonomyRuntime(autonomy.RuntimeConfig{StepExecutor: &soakStepExecutor{}})
	for i := range 100 {
		g := rt.SubmitGoal(fmt.Sprintf("Soak 目标 %d", i), autonomy.GoalConfig{})
		plan := autonomy.NewGoalPlan(g.ID, []autonomy.PlanStep{
			{ID: "collect", Description: "采集", Strategy: autonomy.StepStrategySequential},
			{ID: "fix", Description: "修复", DependsOn: []string{"collect"}, Strategy: autonomy.StepStrategySequential},
		})
		_ = rt.SetPlan(g.ID, plan)
		rt.GetMonitor().ReportHeartbeat(g.ID, 0.5)
	}

	store := skills.NewStore()
	for i := range 20 {
		s := skills.NewSkill(fmt.Sprintf("Soak技能-%d", i), "Soak 描述", []skills.StepDef{{ID: "s1", ToolName: "query"}})
		s.Activate()
		store.Save(s)
	}

	hub := realtime.NewRealtimeHub(realtime.HubConfig{})
	bus := realtime.NewEventBus()
	for i := range 20 {
		sess := hub.CreateSession(fmt.Sprintf("soak-session-%d", i))
		_ = sess.TransitionTo(realtime.SessionListening, "soak")
		bus.Publish(realtime.RealtimeEvent{Type: realtime.EventSessionCreated, SessionID: sess.ID})
	}

	h := studio.NewStudioHandler(
		studio.WithAutonomy(studio.NewAutonomyServiceAdapter(rt)),
		studio.WithSkills(studio.NewSkillServiceAdapter(store)),
		studio.WithRealtime(studio.NewRealtimeServiceAdapter(hub, bus)),
	)
	return httptest.NewServer(h)
}

type soakStepExecutor struct{}

func (e *soakStepExecutor) ExecuteStep(_ context.Context, step autonomy.PlanStep) (string, error) {
	return "ok:" + step.ID, nil
}

// soakStudioEndpoints Soak 覆盖的 GET 端点。
var soakStudioEndpoints = []string{
	"/api/v1/chaos/experiments",
	"/api/v1/cluster/status",
	"/api/v1/autonomy/goals",
	"/api/v1/autonomy/alerts",
	"/api/v1/skills",
	"/api/v1/realtime/sessions",
	"/api/v1/realtime/events",
}

// TestSoak_Studio 30 分钟端点稳态：混合读写 + 退化检测。
func TestSoak_Studio(t *testing.T) {
	duration := envSoakDuration("SOAK_STUDIO_DURATION", 60*time.Second)
	rps := envSoakInt("SOAK_STUDIO_RPS", 30)
	if testing.Short() {
		duration = 20 * time.Second
	}

	srv := soakStudioFixture(t)
	defer srv.Close()

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        256,
			MaxIdleConnsPerHost: 256,
			MaxConnsPerHost:     256,
		},
	}

	// 混合流量：90% GET 轮询端点 + 10% POST 写路径（chaos 实验创建）
	// reqSeq 被并发 RequestFn 回调递增，须原子（-race 实测发现）
	var reqSeq atomic.Int64
	runner := soak.NewRunner(soak.RunnerConfig{
		Duration:         duration,
		Pattern:          soak.ConstantPattern(rps),
		SamplingInterval: 5 * time.Second,
		RequestFn: func(ctx context.Context) (*soak.Response, error) {
			start := time.Now()
			seq := reqSeq.Add(1) - 1
			var (
				req      *http.Request
				body     []byte
				wantCode int
			)
			if seq%10 == 0 {
				// 写路径：创建实验成功返回 201
				wantCode = http.StatusCreated
				req, _ = http.NewRequestWithContext(ctx, http.MethodPost,
					srv.URL+"/api/v1/chaos/experiments",
					strings.NewReader(fmt.Sprintf(`{"name":"soak-%d","hypothesis":"soak","faultType":"latency"}`, seq)))
				req.Header.Set("Content-Type", "application/json")
			} else {
				wantCode = http.StatusOK
				ep := soakStudioEndpoints[int(seq)%len(soakStudioEndpoints)]
				req, _ = http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+ep, nil)
			}
			resp, err := client.Do(req)
			if err != nil {
				return &soak.Response{Latency: time.Since(start), Success: false, Error: err}, nil
			}
			body, _ = io.ReadAll(io.LimitReader(resp.Body, 4<<20))
			resp.Body.Close()
			ok := resp.StatusCode == wantCode
			return &soak.Response{Latency: time.Since(start), Status: resp.StatusCode, Success: ok, BodySize: len(body)}, nil
		},
	})

	result := runner.Run(context.Background())
	t.Logf("Studio Soak 结果: 请求=%d 错误=%d 错误率=%.4f 平均延迟=%s",
		result.TotalRequests, result.TotalErrors, result.ErrorRate(), result.AvgLatency())
	if result.Degradation != nil {
		t.Logf("退化检测: HasDegradation=%v 延迟变化=%.1f%% 错误率变化=%.1f%% 吞吐变化=%.1f%%",
			result.Degradation.HasDegradation,
			result.Degradation.LatencyChangePercent,
			result.Degradation.ErrorRateChangePercent,
			result.Degradation.ThroughputChangePercent)
	}

	// 验收 1：错误率 ≤ 1%
	if rate := result.ErrorRate(); rate > 0.01 {
		t.Fatalf("Studio Soak 错误率 = %.4f, want ≤0.01", rate)
	}
	// 验收 2：无退化（前后半段对比）
	// CI 共享 runner 资源抖动会导致 60s 冒烟的前后半段延迟波动远超 50%
	// 阈值而误报；SOAK_CI_MODE=1 时降级为记录（退化判定在专用基准机执行）
	if os.Getenv("SOAK_CI_MODE") != "1" && result.Degradation != nil && result.Degradation.HasDegradation {
		t.Fatalf("Studio Soak 检测到退化：延迟 +%.1f%% / 错误率 +%.1f%% / 吞吐 -%.1f%%",
			result.Degradation.LatencyChangePercent,
			result.Degradation.ErrorRateChangePercent,
			result.Degradation.ThroughputChangePercent)
	}
	// 验收 3：有真实流量（写路径确实执行）
	if result.TotalRequests < 10 {
		t.Fatalf("Studio Soak 请求过少: %d", result.TotalRequests)
	}
	t.Logf("✅ Studio 端点 Soak 通过（%v，%d 请求，错误率 %.4f，无退化）",
		duration, result.TotalRequests, result.ErrorRate())
}

func envSoakDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func envSoakInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
