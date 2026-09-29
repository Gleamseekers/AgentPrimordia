// studio_bench_test.go — Studio 后端压力测试（v5.0 全能力整合后的负载验证）
//
// 场景：StudioHandler 注入真实引擎（AutonomyRuntime 100 目标 + SkillStore +
// RealtimeHub/EventBus），并发客户端混合请求全部 9 个 /api/v1/* 端点
// （含 v3.3-v3.6 五端点），验证：
//  1. 并发下 0 错误（无数据竞争、无 panic）
//  2. 端点延迟分布 P50/P95/P99 量化
//  3. 模拟前端轮询节奏（2s/5s/10s 混合）下的稳定性
package suite

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/autonomy"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/realtime"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/skills"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/studio"
)

// studioLoadFixture 注入真实引擎的 StudioHandler 测试夹具。
func studioLoadFixture(tb testing.TB, goals int) *httptest.Server {
	tb.Helper()

	// 1. AutonomyRuntime：注入 N 个真实目标（含进度与告警）
	rt := autonomy.NewAutonomyRuntime(autonomy.RuntimeConfig{StepExecutor: &studioStepExecutor{}})
	for i := range goals {
		g := rt.SubmitGoal(fmt.Sprintf("压测目标 %d：监控数据并修复", i), autonomy.GoalConfig{})
		plan := autonomy.NewGoalPlan(g.ID, []autonomy.PlanStep{
			{ID: "collect", Description: "采集", Strategy: autonomy.StepStrategySequential},
			{ID: "fix", Description: "修复", DependsOn: []string{"collect"}, Strategy: autonomy.StepStrategySequential},
		})
		_ = rt.SetPlan(g.ID, plan)
		rt.GetMonitor().ReportHeartbeat(g.ID, 0.5)
		if i%10 == 0 {
			rt.GetMonitor().ReportAnomaly(g.ID, autonomy.AlertWarn, "压测告警")
		}
	}

	// 2. SkillStore：注入技能
	store := skills.NewStore()
	for i := range 20 {
		s := skills.NewSkill(fmt.Sprintf("技能-%d", i), "压测技能描述", []skills.StepDef{{ID: "s1", ToolName: "query"}})
		s.Activate()
		store.Save(s)
	}

	// 3. Realtime：会话 + 事件历史
	hub := realtime.NewRealtimeHub(realtime.HubConfig{})
	bus := realtime.NewEventBus()
	for i := range 30 {
		sess := hub.CreateSession(fmt.Sprintf("session-%d", i))
		_ = sess.TransitionTo(realtime.SessionListening, "load")
		bus.Publish(realtime.RealtimeEvent{Type: realtime.EventSessionCreated, SessionID: sess.ID})
	}

	h := studio.NewStudioHandler(
		studio.WithAutonomy(studio.NewAutonomyServiceAdapter(rt)),
		studio.WithSkills(studio.NewSkillServiceAdapter(store)),
		studio.WithRealtime(studio.NewRealtimeServiceAdapter(hub, bus)),
	)
	return httptest.NewServer(h)
}

// studioStepExecutor 压测步骤执行器（确定性）。
type studioStepExecutor struct{}

func (e *studioStepExecutor) ExecuteStep(_ context.Context, step autonomy.PlanStep) (string, error) {
	return "ok:" + step.ID, nil
}

// studioEndpoints 压测覆盖的全部端点（GET）。
var studioEndpoints = []string{
	"/api/v1/chaos/experiments",
	"/api/v1/cluster/status",
	"/api/v1/learning/stats",
	"/api/v1/marketplace/templates",
	"/api/v1/autonomy/goals",
	"/api/v1/autonomy/alerts",
	"/api/v1/skills",
	"/api/v1/realtime/sessions",
	"/api/v1/realtime/events",
}

// TestStudio_ConcurrentLoad 并发混合负载：100 并发 × 9 端点 × 200 轮 → 0 错误。
func TestStudio_ConcurrentLoad(t *testing.T) {
	srv := studioLoadFixture(t, 100)
	defer srv.Close()

	const workers = 100
	const rounds = 200
	var errCount atomic.Int64
	var okCount atomic.Int64
	var wg sync.WaitGroup

	// 连接池必须与并发度匹配：默认 Transport 的 MaxIdleConnsPerHost=2
	// 会导致 100 并发下每请求重建连接，重复/并发运行时耗尽临时端口，
	// 产生大量"0 错误要求"下的伪失败（2026-09-27 定位并修复）。
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        workers,
			MaxIdleConnsPerHost: workers,
			IdleConnTimeout:     30 * time.Second,
		},
	}
	defer client.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 预热连接池（≥ 并发度），消除并发起跑的拨号风暴
	warmUpConnections(ctx, client, srv.URL+"/api/v1/cluster/status", workers)

	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := range rounds {
				ep := studioEndpoints[(w+r)%len(studioEndpoints)]
				req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+ep, nil)
				resp, err := client.Do(req)
				if err != nil {
					errCount.Add(1)
					continue
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					errCount.Add(1)
					continue
				}
				okCount.Add(1)
			}
		}()
	}
	wg.Wait()

	if errCount.Load() > 0 {
		t.Fatalf("并发负载下错误请求 %d 个（0 错误要求）", errCount.Load())
	}
	if okCount.Load() != workers*rounds {
		t.Fatalf("成功请求 %d, want %d", okCount.Load(), workers*rounds)
	}
	t.Logf("✅ 并发负载通过：%d 请求 / %d 并发 / 9 端点，0 错误", workers*rounds, workers)
}

// TestStudio_PollingMix 前端轮询节奏模拟：2s/5s/10s 混合轮询 × 8 客户端，30s 稳定。
func TestStudio_PollingMix(t *testing.T) {
	srv := studioLoadFixture(t, 100)
	defer srv.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	var errCount atomic.Int64
	var reqCount atomic.Int64
	var wg sync.WaitGroup

	// 轮询节奏：realtime 2s、autonomy 5s、skills 10s（与前端一致）
	pollers := []struct {
		interval time.Duration
		ep       string
	}{
		{2 * time.Second, "/api/v1/realtime/sessions"},
		{2 * time.Second, "/api/v1/realtime/events"},
		{5 * time.Second, "/api/v1/autonomy/goals"},
		{5 * time.Second, "/api/v1/autonomy/alerts"},
		{10 * time.Second, "/api/v1/skills"},
	}
	stop := time.Now().Add(30 * time.Second)
	for _, p := range pollers {
		for range 2 { // 每节奏 2 个客户端
			wg.Add(1)
			go func(ep string, interval time.Duration) {
				defer wg.Done()
				for time.Now().Before(stop) {
					resp, err := client.Get(srv.URL + ep)
					if err != nil {
						errCount.Add(1)
					} else {
						_, _ = io.Copy(io.Discard, resp.Body)
						resp.Body.Close()
						if resp.StatusCode != http.StatusOK {
							errCount.Add(1)
						}
					}
					reqCount.Add(1)
					time.Sleep(interval)
				}
			}(p.ep, p.interval)
		}
	}
	wg.Wait()

	if errCount.Load() > 0 {
		t.Fatalf("轮询模拟下错误请求 %d 个", errCount.Load())
	}
	t.Logf("✅ 轮询模拟通过：30s 内 %d 次轮询请求（2s/5s/10s 混合 × 10 客户端），0 错误", reqCount.Load())
}

// BenchmarkStudioEndpoints 各端点延迟分布（P50/P95/P99）。
func BenchmarkStudioEndpoints(b *testing.B) {
	srv := studioLoadFixture(b, 100)
	defer srv.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	ctx := context.Background()

	for _, ep := range studioEndpoints {
		b.Run(ep, func(b *testing.B) {
			var c p95Collector
			b.ResetTimer()
			for i := 0; i < b.N; i += p95BatchSize {
				n := p95BatchSize
				if i+n > b.N {
					n = b.N - i
				}
				var total int64
				for range n {
					s := time.Now().UnixNano()
					req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+ep, nil)
					resp, err := client.Do(req)
					if err == nil {
						_, _ = io.Copy(io.Discard, resp.Body)
						resp.Body.Close()
					}
					e := time.Now().UnixNano()
					total += e - s
				}
				c.add(time.Duration(total), n)
			}
			c.report(b)
		})
	}
}

// ensure 排序引用（延迟分位工具依赖）
var _ = sort.Float64s

// TestStudio_LargeFixture 极限场景：1000 目标下 goals 端点仍稳定且数据完整。
func TestStudio_LargeFixture(t *testing.T) {
	srv := studioLoadFixture(t, 1000)
	defer srv.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 连续 50 轮读取 goals 端点，验证 0 错误 + 数据完整（返回全部 1000 目标）
	var maxLatency time.Duration
	var errCount int
	for range 50 {
		s := time.Now()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/v1/autonomy/goals", nil)
		resp, err := client.Do(req)
		lat := time.Since(s)
		if err != nil {
			errCount++
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			errCount++
			continue
		}
		if lat > maxLatency {
			maxLatency = lat
		}
		// 断言返回 1000 个目标（"description":"压测目标" 出现 1000 次）
		if n := countSubstring(string(body), "压测目标"); n != 1000 {
			t.Fatalf("goals 响应目标数 = %d, want 1000（数据完整性受损）", n)
		}
	}
	if errCount > 0 {
		t.Fatalf("1000 目标场景错误 %d 次", errCount)
	}
	t.Logf("✅ 1000 目标极限场景通过：50 轮全量读取 0 错误，最大延迟 %v", maxLatency)
}

// countSubstring 统计子串出现次数。
func countSubstring(s, sub string) int {
	n := 0
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			n++
		}
	}
	return n
}

// warmUpConnections 串行预热连接池，避免并发起跑瞬间的拨号风暴。
//
// 背景（2026-09-27）：并发起跑时数百个 goroutine 同时拨号，在全量并行测试
// （多测试二进制争抢资源）下会触发客户端侧 ECONNRESET/ECONNREFUSED/EADDRNOTAVAIL。
// 压测目标是"服务端在并发下的行为"，客户端拨号噪声必须先排除；预热即提前建立
// 与并发度相当的空闲连接，使并发阶段复用连接而非重新拨号。
//
// 注意：**不使用重试**处理写请求——POST 非幂等，重试会造成重复写入，
// 破坏"写后读一致"计数（曾实测 812≠800）。
func warmUpConnections(ctx context.Context, client *http.Client, url string, n int) {
	for i := 0; i < n; i++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return
		}
		resp, err := client.Do(req)
		if err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

// TestStudio_WritePathConcurrentLoad 写路径压测：并发 POST 混沌实验 + 市场部署
// （demo 服务锁竞争点），验证写后读一致性（0 错误 + 数量核对）。
func TestStudio_WritePathConcurrentLoad(t *testing.T) {
	srv := studioLoadFixture(t, 50)
	defer srv.Close()

	// 高并发连接池（默认 2 条/主机在 100 并发下会连接风暴）
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        256,
			MaxIdleConnsPerHost: 256,
			MaxConnsPerHost:     256,
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	defer client.CloseIdleConnections()

	// 注意：demo 存储有界保留上限为 maxDemoRetained=1000（v5.0 压测修复引入）。
	// 写入总量必须 ≤ 保留上限，否则"写后读一致"断言会因旧记录被淘汰而失败。
	//
	// 并发度与轮次（2026-09-27 调整）：总写入量保持不变（各 800 次），
	// 但把"同时拨号数"从 200 降到 50——同时拨号过多会在全量并行测试时
	// 触发客户端侧 ECONNRESET/EADDRNOTAVAIL 噪声，掩盖服务端行为。
	const (
		chaosWorkers  = 25
		chaosRounds   = 32 // 800 次 POST /chaos/experiments（≤ 保留上限 1000）
		deployWorkers = 25
		deployRounds  = 32 // 800 次 POST /marketplace/deploy（≤ 保留上限 1000）
	)

	// 预热连接池（≥ 并发度），消除并发起跑的拨号风暴
	warmUpConnections(ctx, client, srv.URL+"/api/v1/cluster/status", chaosWorkers+deployWorkers)

	var (
		chaosOK    atomic.Int64
		deployOK   atomic.Int64
		badStatus  atomic.Int64
		wg         sync.WaitGroup
		statusMu   sync.Mutex
		statusHist = map[int]int{}
	)
	recordBad := func(code int) {
		badStatus.Add(1)
		statusMu.Lock()
		statusHist[code]++
		statusMu.Unlock()
	}

	// 写路径 1：创建混沌实验（demo 服务锁保护）
	for w := range chaosWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := range chaosRounds {
				body := fmt.Sprintf(`{"name":"load-%d-%d","hypothesis":"并发写路径","faultType":"latency"}`, w, r)
				req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/api/v1/chaos/experiments", strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				resp, err := client.Do(req)
				if err != nil {
					recordBad(-1)
					continue
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode == http.StatusCreated {
					chaosOK.Add(1)
				} else {
					recordBad(resp.StatusCode)
				}
			}
		}()
	}

	// 写路径 2：市场部署（demo 服务锁保护）
	for range deployWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range deployRounds {
				req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/api/v1/marketplace/deploy",
					strings.NewReader(`{"template_id":"code-reviewer"}`))
				req.Header.Set("Content-Type", "application/json")
				resp, err := client.Do(req)
				if err != nil {
					recordBad(-1)
					continue
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					deployOK.Add(1)
				} else {
					recordBad(resp.StatusCode)
				}
			}
		}()
	}
	wg.Wait()

	if badStatus.Load() > 0 {
		statusMu.Lock()
		hist := fmt.Sprintf("%v", statusHist)
		statusMu.Unlock()
		t.Fatalf("写路径非预期状态/错误 %d 次，分布: %s", badStatus.Load(), hist)
	}
	if chaosOK.Load() != chaosWorkers*chaosRounds || deployOK.Load() != deployWorkers*deployRounds {
		t.Fatalf("chaosOK=%d deployOK=%d, want %d/%d", chaosOK.Load(), deployOK.Load(),
			chaosWorkers*chaosRounds, deployWorkers*deployRounds)
	}

	// 写后读一致性：实验与部署数量与成功写入数一致
	checkJSONCount(t, client, ctx, srv.URL+"/api/v1/chaos/experiments", "load-", int(chaosOK.Load()))
	checkJSONCount(t, client, ctx, srv.URL+"/api/v1/marketplace/deployments", "code-reviewer", int(deployOK.Load()))

	t.Logf("✅ 写路径压测通过：chaos %d 次创建 + deploy %d 次部署，0 错误，写后读数量一致",
		chaosOK.Load(), deployOK.Load())
}

// checkJSONCount 断言响应体中关键字出现次数（写后读一致性核对）。
func checkJSONCount(t *testing.T, client *http.Client, ctx context.Context, url, keyword string, want int) {
	t.Helper()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s: %v", url, err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if n := countSubstring(string(body), keyword); n != want {
		t.Fatalf("%s 响应含 %q %d 次, want %d（写后读不一致）", url, keyword, n, want)
	}
}
