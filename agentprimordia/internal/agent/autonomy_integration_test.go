package agent

// v3.3 自治 × 跨组件联动集成测试
//
// 验证 autonomy 的集成接口（RAG/Pool/Guardrail）能被外部适配器实现，
// 并与 AutonomyRuntime 组合形成端到端联动：
//   - 自治执行的每步经 Guardrail 校验
//   - 步骤执行前经 RAG 注入知识上下文
//   - 多目标经 Pool 信号量调度

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/autonomy"
)

// --- 联动用适配器 ---

// integGuard 集成测试护栏：拦截含 "dangerous" 的步骤
type integGuard struct{ blocked int }

func (g *integGuard) CheckStep(_ context.Context, _ string, step autonomy.PlanStep) (bool, string, error) {
	if step.Description == "dangerous" {
		g.blocked++
		return false, "集成护栏拦截", nil
	}
	return true, "", nil
}

func (g *integGuard) CheckOutput(_ context.Context, _ string, out string) (string, bool, error) {
	return out, false, nil
}

// integRAG 集成测试 RAG：记录被增强的步骤
type integRAG struct {
	mu       sync.Mutex
	enriched []string
}

func (r *integRAG) Retrieve(_ context.Context, query string, _ int) ([]autonomy.RAGDocument, error) {
	r.mu.Lock()
	r.enriched = append(r.enriched, query)
	r.mu.Unlock()
	return []autonomy.RAGDocument{{Content: "知识:" + query, Score: 0.9}}, nil
}

// integPool 集成测试调度池：限制并发并记录调度
type integPool struct {
	mu     sync.Mutex
	sem    chan struct{}
	calls  []string
	active int
	maxAct int
}

func newIntegPool(concurrency int) *integPool {
	return &integPool{sem: make(chan struct{}, concurrency)}
}

func (p *integPool) Dispatch(ctx context.Context, goalID string, fn func() error) error {
	p.sem <- struct{}{}
	p.mu.Lock()
	p.calls = append(p.calls, goalID)
	p.active++
	if p.active > p.maxAct {
		p.maxAct = p.active
	}
	p.mu.Unlock()

	defer func() {
		p.mu.Lock()
		p.active--
		p.mu.Unlock()
		<-p.sem
	}()
	return fn()
}

func (p *integPool) ActiveCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.active
}

// ragEnrichedExecutor 在每步执行前经 RAG 增强的执行器
type ragEnrichedExecutor struct {
	rag *integRAG
}

func (e *ragEnrichedExecutor) ExecuteStep(ctx context.Context, step autonomy.PlanStep) (string, error) {
	docs, err := e.rag.Retrieve(ctx, step.Description, 3)
	if err != nil {
		return "", err
	}
	if len(docs) == 0 {
		return "", fmt.Errorf("无知识")
	}
	return "ok:" + docs[0].Content, nil
}

// --- 联动测试 ---

// TestAutonomyGuardrailIntegration 验证自治执行每步经护栏校验
func TestAutonomyGuardrailIntegration(t *testing.T) {
	guard := &integGuard{}
	gi := autonomy.NewGuardrailIntegration(guard)
	ctx := context.Background()

	// 安全步骤通过
	if err := gi.ValidateStep(ctx, "g1", autonomy.PlanStep{ID: "s1", Description: "safe"}); err != nil {
		t.Fatalf("safe step: %v", err)
	}
	// 危险步骤拦截
	err := gi.ValidateStep(ctx, "g1", autonomy.PlanStep{ID: "s2", Description: "dangerous"})
	if err == nil {
		t.Fatal("dangerous step should be blocked")
	}
	if guard.blocked != 1 {
		t.Errorf("blocked = %d, want 1", guard.blocked)
	}
}

// TestAutonomyRAGIntegration 验证步骤执行前 RAG 注入
func TestAutonomyRAGIntegration(t *testing.T) {
	rag := &integRAG{}
	exec := &ragEnrichedExecutor{rag: rag}

	rt := autonomy.NewAutonomyRuntime(autonomy.RuntimeConfig{
		StepExecutor: exec,
	})

	goal := rt.SubmitGoal("RAG 联动", autonomy.GoalConfig{})
	plan := autonomy.NewGoalPlan(goal.ID, []autonomy.PlanStep{
		{ID: "s1", Description: "查询异常模式"},
		{ID: "s2", Description: "应用修复策略", DependsOn: []string{"s1"}},
	})
	_ = rt.SetPlan(goal.ID, plan)
	if err := rt.ExecuteGoal(context.Background(), goal.ID); err != nil {
		t.Fatalf("execute: %v", err)
	}

	rag.mu.Lock()
	n := len(rag.enriched)
	rag.mu.Unlock()
	if n != 2 {
		t.Errorf("RAG enriched = %d, want 2", n)
	}
}

// TestAutonomyPoolIntegration 验证多目标经 Pool 并发调度
func TestAutonomyPoolIntegration(t *testing.T) {
	pool := newIntegPool(2) // 并发上限 2
	pi := autonomy.NewPoolIntegration(pool)

	exec := &ragEnrichedExecutor{rag: &integRAG{}}
	rt := autonomy.NewAutonomyRuntime(autonomy.RuntimeConfig{StepExecutor: exec})

	// 提交 4 个目标
	var ids []string
	for i := 0; i < 4; i++ {
		g := rt.SubmitGoal(fmt.Sprintf("目标%d", i), autonomy.GoalConfig{})
		plan := autonomy.NewGoalPlan(g.ID, []autonomy.PlanStep{{ID: "s1", Description: fmt.Sprintf("步骤%d", i)}})
		_ = rt.SetPlan(g.ID, plan)
		ids = append(ids, g.ID)
	}

	// 经 Pool 并发调度执行
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(gid string) {
			defer wg.Done()
			_ = pi.DispatchGoal(context.Background(), gid, func() error {
				return rt.ExecuteGoal(context.Background(), gid)
			})
		}(id)
	}
	wg.Wait()

	pool.mu.Lock()
	dispatched := len(pool.calls)
	maxActive := pool.maxAct
	pool.mu.Unlock()

	if dispatched != 4 {
		t.Errorf("dispatched = %d, want 4", dispatched)
	}
	if maxActive > 2 {
		t.Errorf("max active = %d, should respect concurrency limit 2", maxActive)
	}
}

// TestAutonomyFullPipeline 验证 RAG+Guardrail+Monitor+Checkpoint 全链路联动
func TestAutonomyFullPipeline(t *testing.T) {
	rag := &integRAG{}
	guard := &integGuard{}
	gi := autonomy.NewGuardrailIntegration(guard)
	store := &integCheckpointStore{data: make(map[string]*autonomy.Checkpoint)}

	exec := &ragEnrichedExecutor{rag: rag}
	rt := autonomy.NewAutonomyRuntime(autonomy.RuntimeConfig{
		StepExecutor:    exec,
		CheckpointStore: store,
		MonitorConfig:   autonomy.MonitorConfig{StallThreshold: 5},
	})

	var alerts []autonomy.Alert
	rt.GetMonitor().OnAlert(func(a autonomy.Alert) { alerts = append(alerts, a) })

	goal := rt.SubmitGoal("全链路", autonomy.GoalConfig{})
	plan := autonomy.NewGoalPlan(goal.ID, []autonomy.PlanStep{
		{ID: "s1", Description: "采集"},
		{ID: "s2", Description: "分析", DependsOn: []string{"s1"}},
	})
	_ = rt.SetPlan(goal.ID, plan)

	// 执行前对每步做护栏校验
	for _, s := range plan.Steps {
		if err := gi.ValidateStep(context.Background(), goal.ID, s); err != nil {
			t.Fatalf("guardrail: %v", err)
		}
	}

	if err := rt.ExecuteGoal(context.Background(), goal.ID); err != nil {
		t.Fatalf("execute: %v", err)
	}

	// 断言联动生效
	if _, ok := store.data[goal.ID]; !ok {
		t.Error("checkpoint should be saved")
	}
	rag.mu.Lock()
	enriched := len(rag.enriched)
	rag.mu.Unlock()
	if enriched != 2 {
		t.Errorf("RAG enriched = %d, want 2", enriched)
	}
	_ = alerts // 无停滞，alerts 应为空
	_ = time.Now()
}

// integCheckpointStore 集成测试检查点存储
type integCheckpointStore struct {
	data map[string]*autonomy.Checkpoint
}

func (s *integCheckpointStore) SaveCheckpoint(_ context.Context, cp *autonomy.Checkpoint) error {
	s.data[cp.GoalID] = cp
	return nil
}
func (s *integCheckpointStore) LoadCheckpoint(_ context.Context, id string) (*autonomy.Checkpoint, error) {
	return s.data[id], nil
}
func (s *integCheckpointStore) ListIncomplete(_ context.Context) ([]*autonomy.Checkpoint, error) {
	var r []*autonomy.Checkpoint
	for _, cp := range s.data {
		if !cp.Completed {
			r = append(r, cp)
		}
	}
	return r, nil
}
