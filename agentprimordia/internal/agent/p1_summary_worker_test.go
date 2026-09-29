// p1_summary_worker_test.go — P1-5：异步摘要 goroutine 无界回归测试
//
// 评估报告 §4.2 P1-5：react_persist.go 的 saveMemory 为每条消息派生一个
// 摘要提取 goroutine，无并发上限、无 WaitGroup。高吞吐场景下消息数直接
// 决定 goroutine 数量，且运行结束时无法等待在途提取完成（runLoopEngine
// 的 defer 只 flush 了 memWriter，摘要 goroutine 可能被遗弃）。
//
// 修复：以 memoryWriter 为范本引入 summaryWriter——有界 worker 池 +
// WaitGroup，flush 可等待全部在途任务完成。
package agent

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"log/slog"

	"github.com/Gleamseekers/AgentPrimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/internal/memory"
	"github.com/Gleamseekers/AgentPrimordia/internal/persist"
)

// blockingSummarizer 可控制并发度的摘要提取器（测试专用）。
type blockingSummarizer struct {
	mu       sync.Mutex
	inFlight int
	maxSeen  int
	calls    atomic.Int64
	release  chan struct{} // 非 nil 时每个调用阻塞到关闭
	delay    time.Duration
}

func (s *blockingSummarizer) ExtractSummary(_ context.Context, _ string) (*memory.SummaryResult, error) {
	s.calls.Add(1)
	s.mu.Lock()
	s.inFlight++
	if s.inFlight > s.maxSeen {
		s.maxSeen = s.inFlight
	}
	release := s.release
	delay := s.delay
	s.mu.Unlock()

	if release != nil {
		<-release
	}
	if delay > 0 {
		time.Sleep(delay)
	}

	s.mu.Lock()
	s.inFlight--
	s.mu.Unlock()
	return &memory.SummaryResult{Summary: "摘要", Topics: "topic"}, nil
}

func (s *blockingSummarizer) stats() (calls, maxSeen int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls.Load(), int64(s.maxSeen)
}

// p1SummaryStore 记录 UpdateSummary 调用次数。
type p1SummaryStore struct {
	mu        sync.Mutex
	updates   int
	summaries map[string]string
}

func newP1SummaryStore() *p1SummaryStore {
	return &p1SummaryStore{summaries: make(map[string]string)}
}

func (s *p1SummaryStore) Add(_ context.Context, _ *memory.Episode) error { return nil }

func (s *p1SummaryStore) UpdateSummary(_ context.Context, id, summary, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updates++
	s.summaries[id] = summary
	return nil
}

// TestSummaryWriter_BoundedConcurrency P1-5 核心回归：
// 提交远超 worker 数的摘要任务时，同时在飞的 ExtractSummary 不得超过上限。
func TestSummaryWriter_BoundedConcurrency(t *testing.T) {
	sum := &blockingSummarizer{release: make(chan struct{})}
	store := newP1SummaryStore()

	const (
		workers = 4
		tasks   = 200
	)
	w := newSummaryWriter(sum, store, workers, slog.Default())
	w.refresh(sum, store) // 惰性启动 worker 池

	for i := 0; i < tasks; i++ {
		w.submit(summaryTask{epID: fmt.Sprintf("ep-%d", i), content: "内容"})
	}
	// 放行所有在飞任务
	close(sum.release)

	// flush 必须等待全部任务完成
	done := make(chan struct{})
	go func() { w.flush(); close(done) }()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("summaryWriter.flush 挂起")
	}

	calls, maxSeen := sum.stats()
	if calls != tasks {
		t.Errorf("ExtractSummary 调用 %d 次, want %d", calls, tasks)
	}
	if maxSeen > workers {
		t.Errorf("同时在飞摘要任务峰值 %d, 超过 worker 上限 %d（goroutine 无界）", maxSeen, workers)
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if store.updates != tasks {
		t.Errorf("UpdateSummary 调用 %d 次, want %d", store.updates, tasks)
	}
}

// TestSummaryWriter_FlushWaitsForCompletion P1-5：
// flush 返回时所有在途摘要必须已完成（含 UpdateSummary 落库）。
func TestSummaryWriter_FlushWaitsForCompletion(t *testing.T) {
	sum := &blockingSummarizer{delay: 20 * time.Millisecond}
	store := newP1SummaryStore()
	w := newSummaryWriter(sum, store, 8, slog.Default())
	w.refresh(sum, store)

	const tasks = 50
	for i := 0; i < tasks; i++ {
		w.submit(summaryTask{epID: fmt.Sprintf("ep-%d", i), content: "内容"})
	}
	w.flush()

	store.mu.Lock()
	defer store.mu.Unlock()
	if store.updates != tasks {
		t.Errorf("flush 后 UpdateSummary = %d, want %d（在途任务未等待完成）", store.updates, tasks)
	}
	if calls, _ := sum.stats(); calls != tasks {
		t.Errorf("flush 后 ExtractSummary = %d, want %d", calls, tasks)
	}
}

// TestSummaryWriter_NoGoroutineLeak P1-5：
// flush 后 worker goroutine 必须全部退出（不得常驻）。
func TestSummaryWriter_NoGoroutineLeak(t *testing.T) {
	sum := &blockingSummarizer{}
	store := newP1SummaryStore()

	base := runtime.NumGoroutine()
	for round := 0; round < 5; round++ {
		w := newSummaryWriter(sum, store, 4, slog.Default())
		w.refresh(sum, store)
		for i := 0; i < 20; i++ {
			w.submit(summaryTask{epID: fmt.Sprintf("r%d-%d", round, i), content: "内容"})
		}
		w.flush()
	}
	// 等待退出收敛
	deadline := time.Now().Add(3 * time.Second)
	for runtime.NumGoroutine() > base+2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := runtime.NumGoroutine(); got > base+2 {
		t.Errorf("flush 后 goroutine 数 = %d，基线 %d（worker 未退出，goroutine 泄漏）", got, base)
	}
}

// TestSummaryWriter_DropWhenQueueFull P1-5：
// 队列满时非阻塞丢弃并告警，不阻塞主循环（与 memoryWriter 语义一致）。
func TestSummaryWriter_DropWhenQueueFull(t *testing.T) {
	release := make(chan struct{})
	sum := &blockingSummarizer{release: release}
	store := newP1SummaryStore()
	// worker 少 + 队列小 → 必然触发丢弃。
	// 先设 queueCap 再 refresh：worker 池惰性启动，此时才按 queueCap 建队列。
	w := newSummaryWriter(sum, store, 1, slog.Default())
	w.queueCap = 2
	w.refresh(sum, store)

	// 提交 10 个：第一个被唯一 worker 取走并阻塞在 release 上，
	// 队列容量 2 只能再收 2 个，其余 7 个被非阻塞丢弃。
	const submitN = 10
	for i := 0; i < submitN; i++ {
		w.submit(summaryTask{epID: fmt.Sprintf("ep-%d", i), content: "内容"})
	}
	close(release)
	w.flush()

	calls, _ := sum.stats()
	if calls > 3 {
		t.Errorf("ExtractSummary 调用 %d 次, want <= 3（队列容量 2 + 1 个在飞）", calls)
	}
	if calls == 0 {
		t.Error("ExtractSummary 未被调用——队列未正常工作")
	}
	t.Logf("提交 %d 个，实际处理 %d 个（丢弃 %d 个）", submitN, calls, submitN-int(calls))
}

// TestSaveMemory_SummaryBoundedDuringRun P1-5 集成回归：
// Agent 运行期大量 saveMemory 时，摘要提取并发度必须有界。
func TestSaveMemory_SummaryBoundedDuringRun(t *testing.T) {
	// 用短延迟而非 release 闸门：Run 结束时会 flush 摘要 worker 池并等待
	// 在途任务完成，若此处用闸门阻塞会导致 Run 永不返回（死锁）。
	sum := &blockingSummarizer{delay: 2 * time.Millisecond}
	store := newP1SummaryStore()

	mock := llm.NewMockLLM(t).WithResponse("hello")
	a, _ := newP1Agent(t, "summary-bounded", mock,
		WithMemory(store), WithSummarizer(sum))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := a.Run(ctx, UserMessage("hi")); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// 摘要 worker 必须已挂到 agent 上（newP1Agent 返回 *CapabilityAgent，
	// 需取内层 *ReActAgent 访问私有字段）
	inner := a.inner
	inner.summaryMu.Lock()
	sw := inner.summaryWriter
	inner.summaryMu.Unlock()
	if sw == nil {
		t.Fatal("agent 未装配 summaryWriter（P1-5 未接线）")
	}

	// flush 必须可重入且不挂起
	done := make(chan struct{})
	go func() { inner.flushSummaryWriter(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("flushSummaryWriter 挂起")
	}

	// Run 结束时的 flush 已等待全部在途任务完成，摘要必须已落库
	store.mu.Lock()
	updates := store.updates
	store.mu.Unlock()
	if updates == 0 {
		t.Error("Run 结束后没有摘要落库——flush 未等待在途任务")
	}

	calls, maxSeen := sum.stats()
	if calls != int64(updates) {
		t.Errorf("ExtractSummary %d 次 vs UpdateSummary %d 次，数量不一致", calls, updates)
	}
	if maxSeen > summaryWorkerCount {
		t.Errorf("摘要提取并发峰值 %d, 超过 worker 上限 %d", maxSeen, summaryWorkerCount)
	}
}

// TestResumeFromCheckpoint_FlushesSummaryWriter P1-5：
// 恢复路径绕过 reactLoopEngine，必须自行 flush 摘要 worker 池，
// 否则每次 ResumeFromCheckpoint / ReplayFailure 都会泄漏 worker goroutine。
func TestResumeFromCheckpoint_FlushesSummaryWriter(t *testing.T) {
	sum := &blockingSummarizer{delay: time.Millisecond}
	store := newP1SummaryStore()

	mock := llm.NewMockLLM(t).WithResponse("resumed")
	cs := &p1CheckpointStore{state: &persist.AgentState{
		AgentID:   "summary-resume",
		SessionID: "s",
		Status:    "paused",
		Messages: []persist.CheckpointMessage{
			{Role: "user", Content: "上一次的问题"},
		},
		TurnCount: 1,
		Metrics:   persist.CheckpointMetrics{Duration: "10ms"},
	}}

	a, _ := newP1Agent(t, "summary-resume", mock,
		WithCheckpointStore(cs), WithMemory(store), WithSummarizer(sum))

	base := runtime.NumGoroutine()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// 连续恢复多轮：若未 flush，goroutine 数会线性增长
	for i := 0; i < 5; i++ {
		if _, err := a.ResumeFromCheckpoint(ctx); err != nil {
			t.Fatalf("第 %d 次 ResumeFromCheckpoint: %v", i, err)
		}
	}

	deadline := time.Now().Add(3 * time.Second)
	for runtime.NumGoroutine() > base+2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := runtime.NumGoroutine(); got > base+2 {
		t.Errorf("5 次恢复后 goroutine 数 = %d，基线 %d（恢复路径未 flush 摘要 worker 池）", got, base)
	}

	// 摘要应已落库（flush 等待了在途任务）
	store.mu.Lock()
	updates := store.updates
	store.mu.Unlock()
	if updates == 0 {
		t.Error("恢复路径没有摘要落库——flush 未生效")
	}
}
