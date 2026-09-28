package llm

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestResilient_ContextCancelNotCountedAsFailure 复现 v6.x 评估 §4.2 P1-2：
// recordFailure 不豁免 context.Canceled → 调用方每次取消都累加熔断失败计数，
// 连续取消 CircuitThreshold 次即打开熔断 CircuitRecoverAfter（默认 30s），
// 后续正常请求被误拒。
func TestResilient_ContextCancelNotCountedAsFailure(t *testing.T) {
	mock := NewMockLLM(t).WithError(context.Canceled)

	config := DefaultResilientConfig()
	config.MaxRetries = 0
	config.CircuitThreshold = 3

	provider, err := NewResilientProvider(mock, config)
	if err != nil {
		t.Fatalf("NewResilientProvider: %v", err)
	}

	// 模拟调用方连续取消 3 倍阈值次数（均已取消的 ctx）
	for i := 0; i < config.CircuitThreshold*3; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, _ = provider.Complete(ctx, &CompletionRequest{})
	}

	if got := provider.failures.Load(); got != 0 {
		t.Errorf("failures = %d, want 0（ctx 取消不应计入熔断失败）", got)
	}
	if got := circuitState(provider.state.Load()); got != circuitClosed {
		t.Errorf("circuit state = %v, want closed（ctx 取消不应打开熔断）", got)
	}

	// 恢复后的正常请求必须被放行（不被误开的熔断拦截）
	mock.mu.Lock()
	mock.err = nil
	mock.mu.Unlock()
	resp, err := provider.Complete(context.Background(), &CompletionRequest{})
	if err != nil {
		t.Fatalf("正常请求被误拒: %v", err)
	}
	if resp == nil {
		t.Fatal("正常请求返回 nil response")
	}
}

// TestResilient_DeadlineExceededNotCountedAsFailure 复现同一缺陷的
// DeadlineExceeded 变体：ctx 超时同样不属于 provider 故障。
func TestResilient_DeadlineExceededNotCountedAsFailure(t *testing.T) {
	mock := NewMockLLM(t).WithError(context.DeadlineExceeded)

	config := DefaultResilientConfig()
	config.MaxRetries = 0
	config.CircuitThreshold = 2

	provider, err := NewResilientProvider(mock, config)
	if err != nil {
		t.Fatalf("NewResilientProvider: %v", err)
	}

	for i := 0; i < config.CircuitThreshold*3; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
		_, _ = provider.Complete(ctx, &CompletionRequest{})
		cancel()
	}

	if got := provider.failures.Load(); got != 0 {
		t.Errorf("failures = %d, want 0（ctx 超时不应计入熔断失败）", got)
	}
	if got := circuitState(provider.state.Load()); got != circuitClosed {
		t.Errorf("circuit state = %v, want closed", got)
	}
}

// TestResilient_WrappedContextCancelNotCounted 验证被 %w 包装的
// context.Canceled（executeWithRetry 的 ErrRetriesExhausted 包装路径）
// 同样被豁免。
func TestResilient_WrappedContextCancelNotCounted(t *testing.T) {
	mock := NewMockLLM(t).WithError(context.Canceled)

	config := DefaultResilientConfig()
	config.MaxRetries = 1
	config.RetryBackoff = time.Millisecond
	config.CircuitThreshold = 2

	provider, err := NewResilientProvider(mock, config)
	if err != nil {
		t.Fatalf("NewResilientProvider: %v", err)
	}

	for i := 0; i < config.CircuitThreshold*3; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := provider.Complete(ctx, &CompletionRequest{})
		if err == nil {
			t.Fatal("expected error from canceled ctx")
		}
		if !errors.Is(err, context.Canceled) {
			t.Logf("iteration %d error (record only): %v", i, err)
		}
		cancel()
	}

	if got := provider.failures.Load(); got != 0 {
		t.Errorf("failures = %d, want 0（包装后的 ctx 取消也不应计入熔断失败）", got)
	}
}

// TestResilient_ProviderErrorStillCountsAsFailure 反向验证：
// 真实 provider 故障仍然计入熔断（豁免不能误伤正常路径）。
func TestResilient_ProviderErrorStillCountsAsFailure(t *testing.T) {
	mock := NewMockLLM(t).WithError(errors.New("provider boom"))

	config := DefaultResilientConfig()
	config.MaxRetries = 0
	config.CircuitThreshold = 2

	provider, err := NewResilientProvider(mock, config)
	if err != nil {
		t.Fatalf("NewResilientProvider: %v", err)
	}

	for i := 0; i < config.CircuitThreshold; i++ {
		_, _ = provider.Complete(context.Background(), &CompletionRequest{})
	}

	if got := provider.failures.Load(); got < int64(config.CircuitThreshold) {
		t.Errorf("failures = %d, want >= %d（真实故障必须计入熔断）", got, config.CircuitThreshold)
	}
	if got := circuitState(provider.state.Load()); got != circuitOpen {
		t.Errorf("circuit state = %v, want open", got)
	}
}
