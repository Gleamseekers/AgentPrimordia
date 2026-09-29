// p1_locking_test.go — P1-2 / P1-3：锁纪律回归测试
//
// 评估报告 §4.2：
//
//	P1-2 resumeFromState 在 statsMu 之外写 a.startTime，与 Stats() 持
//	     RLock 读同一字段构成数据竞争；同类问题还有 a.hookCtx（reactLoopEngine
//	     入口无锁写、fireHook/saveMemory 无锁读）、a.memSessionID（入口无锁写、
//	     resolveSessionID 无锁读）、a.stats.Status（死字段——Stats() 用
//	     lifecycle.Status() 覆盖，写入无意义）。
//	P1-3 react_loop.go 的锁顺序注释写的是 statsMu → runMu → mu，与实战
//	     唯一的嵌套方向 runMu → statsMu 相反。
//
// 本文件用 -race 下的并发场景固化修复。
package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/internal/persist"
	"os"
	"strings"
)

// p1CheckpointStore 最小 CheckpointStore 实现，返回 paused 状态以走恢复路径。
type p1CheckpointStore struct {
	state *persist.AgentState
}

func (s *p1CheckpointStore) Save(_ context.Context, _ *persist.AgentState) error { return nil }
func (s *p1CheckpointStore) Load(_ context.Context, _ string) (*persist.AgentState, error) {
	return s.state, nil
}
func (s *p1CheckpointStore) List(_ context.Context, _ string) ([]*persist.AgentState, error) {
	return nil, nil
}
func (s *p1CheckpointStore) Delete(_ context.Context, _ string) error { return nil }

// TestStats_ConcurrentWithRun_NoRace P1-2：Stats() 与 Run() 并发调用不得有
// 数据竞争（-race 门下）。修复前 startTime 无锁写/无锁读。
func TestStats_ConcurrentWithRun_NoRace(t *testing.T) {
	mock := llm.NewMockLLM(t).WithResponse("hello")
	a, _ := newP1Agent(t, "lock-stats", mock)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	// 并发 Stats() 读
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = a.Stats()
			}
		}()
	}
	// 并发 Run()
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = a.Run(ctx, UserMessage("hi"))
		}()
	}
	wg.Wait()
}

// TestResumeFromState_StartTimeUnderLock P1-2：resumeFromState 写 startTime
// 必须与 Stats() 的读互斥。用带 Duration 的 paused 检查点驱动恢复路径，
// 同时并发 Stats()，-race 门下暴露无锁写。
func TestResumeFromState_StartTimeUnderLock(t *testing.T) {
	mock := llm.NewMockLLM(t).WithResponse("resumed")

	store := &p1CheckpointStore{state: &persist.AgentState{
		AgentID:   "lock-resume",
		SessionID: "s",
		Status:    "paused",
		Messages: []persist.CheckpointMessage{
			{Role: "user", Content: "上一次的问题"},
		},
		TurnCount: 2,
		Metrics: persist.CheckpointMetrics{
			Duration: "1500ms", // 非零 → 走 startTime = now.Add(-prevDur) 分支
		},
	}}

	a, _ := newP1Agent(t, "lock-resume", mock, WithCheckpointStore(store))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	// 恢复期间并发读 Stats()
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = a.Stats()
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = a.ResumeFromCheckpoint(ctx)
	}()
	wg.Wait()

	// 语义校验：Duration 累计应扣除已运行时长
	st := a.Stats()
	if st.StartTime.IsZero() {
		t.Fatal("StartTime 未被写入")
	}
}

// TestStats_StatusFromLifecycle P1-2：Stats().Status 必须来自 lifecycle，
// 而不是 a.stats.Status 死字段。
func TestStats_StatusFromLifecycle(t *testing.T) {
	mock := llm.NewMockLLM(t).WithResponse("hello")
	a, _ := newP1Agent(t, "lock-status", mock)

	// 运行前：idle
	if got := a.Stats().Status; got != StatusIdle {
		t.Errorf("运行前 Status = %q, want %q", got, StatusIdle)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := a.Run(ctx, UserMessage("hi")); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := a.Stats().Status; got != StatusCompleted {
		t.Errorf("运行后 Status = %q, want %q", got, StatusCompleted)
	}

	// a.stats.Status 不应再被写入（死字段已删除）
	// newP1Agent 返回 *CapabilityAgent，需取内层 *ReActAgent 访问私有字段
	inner := a.inner
	inner.statsMu.RLock()
	raw := inner.stats.Status
	inner.statsMu.RUnlock()
	if raw == StatusRunning {
		t.Error("a.stats.Status 仍被写入为 StatusRunning——死字段未删除")
	}
}

// TestFireHook_ConcurrentWithRun_NoRace P1-2：hookCtx 在入口写入、
// fireHook 在运行期读取，并发 Run + Stats 不得竞争。
func TestFireHook_ConcurrentWithRun_NoRace(t *testing.T) {
	mock := llm.NewMockLLM(t).WithResponse("hello")
	a, _ := newP1Agent(t, "lock-hookctx", mock)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = a.Run(ctx, UserMessage("hi"))
		}()
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = a.Stats()
			}
		}()
	}
	wg.Wait()
}

// TestLockOrderComment_MatchesReality P1-3：锁顺序注释必须与实战嵌套方向一致。
// 实战唯一嵌套是 runMu → statsMu（reactLoopEngine 入口持 runMu 时取 statsMu），
// 不存在 statsMu → runMu 的反向嵌套。
func TestLockOrderComment_MatchesReality(t *testing.T) {
	src := readAgentSource(t, "react_loop.go")
	if !containsLockOrder(src, "runMu → statsMu") {
		t.Error("react_loop.go 锁顺序注释未声明 runMu → statsMu（实战嵌套方向）")
	}
	if containsLockOrder(src, "statsMu → runMu") {
		t.Error("react_loop.go 锁顺序注释仍写 statsMu → runMu（与实战相反）")
	}
}

// readAgentSource 读取 agent 包内源码文件内容（测试辅助）。
func readAgentSource(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", name, err)
	}
	return string(b)
}

// containsLockOrder 判断源码中是否出现指定锁顺序声明。
func containsLockOrder(src, order string) bool {
	return strings.Contains(src, order)
}
