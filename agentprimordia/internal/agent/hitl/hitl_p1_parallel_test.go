// hitl_p1_parallel_test.go — P1-4：并行 tool × HITL 挂死回归测试
//
// 评估报告 §4.2 P1-4：HITLManager 只有单个 pending 槽与共享 responseCh。
// 并行 tool 执行下两个 goroutine 同时请求确认时，后到者覆盖 pending，
// Resume() 投递的单个响应被其中一个消费，另一个挂起到 ctx 超时。
//
// 本文件固化修复后的行为：并发 RequestInterrupt 都能收到各自正确的响应，
// 不会互相覆盖 pending，也不会挂起。
package hitl

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// newParallelHITL 构造所有工具都需确认的 HITLManager。
func newParallelHITL(t *testing.T, buf int) (*HITLManager, chan *HumanResponse) {
	t.Helper()
	humanChan := make(chan *HumanResponse, buf)
	mgr := NewHITLManager(HITLConfig{
		InterruptPoints: []InterruptPoint{{Type: InterruptToolConfirm, ToolName: ""}},
		HumanInputChan:  humanChan,
	})
	return mgr, humanChan
}

// newParallelHITLWithOnInterrupt 构造带 OnInterrupt 回调的 HITLManager。
func newParallelHITLWithOnInterrupt(t *testing.T, buf int, cb func(*InterruptRequest)) (*HITLManager, chan *HumanResponse) {
	t.Helper()
	humanChan := make(chan *HumanResponse, buf)
	mgr := NewHITLManager(HITLConfig{
		InterruptPoints: []InterruptPoint{{Type: InterruptToolConfirm, ToolName: ""}},
		HumanInputChan:  humanChan,
		OnInterrupt:     cb,
	})
	return mgr, humanChan
}

// TestRequestInterrupt_Concurrent_NoDeadlock P1-4 核心回归：
// 两个 goroutine 同时请求确认，各自都应收到属于自己的响应，
// 且在合理时间内返回（不得挂起到 ctx 超时）。
func TestRequestInterrupt_Concurrent_NoDeadlock(t *testing.T) {
	mgr, humanChan := newParallelHITL(t, 8)

	const n = 4
	var wg sync.WaitGroup
	got := make([]*HumanResponse, n)
	errs := make([]error, n)

	// 经 HumanInputChan 预填 n 条人类响应：串行化后各请求按排队顺序逐个消费。
	// （不能用 Resume() 预填——修复后无在飞请求时 Resume() 直接丢弃，
	//  这正是"过期响应不污染后续请求"的保证。）
	for i := 0; i < n; i++ {
		humanChan <- &HumanResponse{Approved: true, Input: "resp-" + string(rune('A'+i))}
	}

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			got[idx], errs[idx] = mgr.RequestInterrupt(ctx, &InterruptRequest{
				Reason:  InterruptToolConfirm,
				Message: "tool confirm",
				Data:    map[string]any{"idx": idx},
				Turn:    idx,
			})
		}(i)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("并发 RequestInterrupt 挂死（P1-4 未修复）")
	}

	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Errorf("请求 %d 失败: %v", i, errs[i])
			continue
		}
		if got[i] == nil || !got[i].Approved {
			t.Errorf("请求 %d 未收到正确响应: %+v", i, got[i])
		}
	}
}

// TestRequestInterrupt_Concurrent_OnInterruptPerRequest P1-4：
// 串行化确认下每个请求都应触发一次 OnInterrupt（不被覆盖吞掉）。
func TestRequestInterrupt_Concurrent_OnInterruptPerRequest(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	mgr, humanChan := newParallelHITLWithOnInterrupt(t, 8, func(req *InterruptRequest) {
		mu.Lock()
		seen = append(seen, req.Data["tool"].(string))
		mu.Unlock()
	})

	const n = 3
	for i := 0; i < n; i++ {
		humanChan <- &HumanResponse{Approved: true}
	}

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = mgr.RequestInterrupt(ctx, &InterruptRequest{
				Reason: InterruptToolConfirm,
				Data:   map[string]any{"tool": "tool-" + string(rune('A'+idx))},
			})
		}(i)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("并发 RequestInterrupt 挂死（P1-4 未修复）")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != n {
		t.Errorf("OnInterrupt 触发 %d 次, want %d（pending 被覆盖导致请求被吞）", len(seen), n)
	}
}

// TestRequestInterrupt_Concurrent_PendingNotOverwritten P1-4：
// 等待期间 Pending() 应始终反映当前唯一在飞的请求，
// 而不是被后到者覆盖成最后一个。
func TestRequestInterrupt_Concurrent_PendingNotOverwritten(t *testing.T) {
	mgr, humanChan := newParallelHITL(t, 8)

	// 第一个请求先占用临界区（不预填响应，让它挂起）
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _ = mgr.RequestInterrupt(ctx, &InterruptRequest{
			Reason: InterruptToolConfirm,
			Data:   map[string]any{"tool": "first"},
		})
	}()

	// 等第一个成为 pending
	deadline := time.Now().Add(2 * time.Second)
	for mgr.Pending() == nil {
		if time.Now().After(deadline) {
			t.Fatal("第一个请求未成为 pending")
		}
		time.Sleep(2 * time.Millisecond)
	}
	firstPending := mgr.Pending()
	if firstPending.Data["tool"] != "first" {
		t.Fatalf("Pending = %v, want first", firstPending.Data["tool"])
	}

	// 第二个请求排队（此时不应覆盖 pending）
	secondDone := make(chan struct{})
	go func() {
		defer close(secondDone)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _ = mgr.RequestInterrupt(ctx, &InterruptRequest{
			Reason: InterruptToolConfirm,
			Data:   map[string]any{"tool": "second"},
		})
	}()

	time.Sleep(100 * time.Millisecond)
	if p := mgr.Pending(); p == nil || p.Data["tool"] != "first" {
		t.Errorf("排队期间 Pending 被覆盖为 %v, want first（P1-4 未修复）", p)
	}

	// 放行第一个 → 第二个应随后被受理
	humanChan <- &HumanResponse{Approved: true}
	<-firstDone

	deadline = time.Now().Add(2 * time.Second)
	for mgr.Pending() == nil {
		if time.Now().After(deadline) {
			t.Fatal("第一个完成后第二个请求未被受理")
		}
		time.Sleep(2 * time.Millisecond)
	}
	humanChan <- &HumanResponse{Approved: true}

	select {
	case <-secondDone:
	case <-time.After(5 * time.Second):
		t.Fatal("第二个请求挂死（P1-4 未修复）")
	}
}

// TestRequestInterrupt_StaleResponseNotLeaked P1-4：
// 请求因 ctx 超时失败后，迟到的 Resume() 响应不得被下一个无关请求误消费。
//
// 修复前 Resume() 投向共享 responseCh（容量 8），超时失败的请求不会消费它，
// 响应滞留缓冲区并被后续无关请求取走；修复后改为每请求专属通道，
// 无在飞请求时 Resume() 直接丢弃。
func TestRequestInterrupt_StaleResponseNotLeaked(t *testing.T) {
	mgr, _ := newParallelHITL(t, 8)

	// 第一个请求超时失败
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := mgr.RequestInterrupt(ctx, &InterruptRequest{
		Reason: InterruptToolConfirm,
		Data:   map[string]any{"tool": "stale"},
	}); err == nil {
		t.Fatal("期望第一个请求超时失败")
	}

	// 迟到响应：模拟 Resume 在第一个请求超时失败之后才被调用
	mgr.Resume(&HumanResponse{Approved: true, Input: "stale"})

	// 第二个请求：人类通道未预填任何响应，只可能因超时而失败
	ctx2, cancel2 := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel2()
	_, err := mgr.RequestInterrupt(ctx2, &InterruptRequest{
		Reason: InterruptToolConfirm,
		Data:   map[string]any{"tool": "fresh"},
	})
	if err == nil {
		t.Fatal("第二个请求消费到了过期响应（stale response 泄漏）")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Logf("第二个请求错误（可接受）: %v", err)
	}
}

// TestResume_NonBlockingWhenNoWaiter P1-4：
// 无在飞请求时 Resume 不得阻塞调用方。
func TestResume_NonBlockingWhenNoWaiter(t *testing.T) {
	mgr, _ := newParallelHITL(t, 1)
	// 缓冲区仅 1，快速连续投递多于容量时应立即返回而非死锁
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 20; i++ {
			mgr.Resume(&HumanResponse{Approved: true})
		}
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Resume 在无在飞请求时阻塞（P1-4 未修复）")
	}
}
