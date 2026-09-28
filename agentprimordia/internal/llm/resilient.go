package llm

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrCircuitOpen      = errors.New("circuit breaker is open")
	ErrRetriesExhausted = errors.New("all retries exhausted")
	ErrFallbackFailed   = errors.New("all fallback providers failed")
)

type circuitState int

const (
	circuitClosed circuitState = iota
	circuitOpen
	circuitHalfOpen
)

type ResilientConfig struct {
	MaxRetries          int
	RetryBackoff        time.Duration
	MaxBackoff          time.Duration
	CircuitThreshold    int
	CircuitRecoverAfter time.Duration
}

func DefaultResilientConfig() ResilientConfig {
	return ResilientConfig{
		MaxRetries:          3,
		RetryBackoff:        500 * time.Millisecond,
		MaxBackoff:          10 * time.Second,
		CircuitThreshold:    5,
		CircuitRecoverAfter: 30 * time.Second,
	}
}

// ResilientProvider 弹性 Provider（带重试 + Fallback + 熔断）
// perf-v4 Task 10：state/failures/lastFail/halfOpenProbe 改为原子操作，
// checkCircuit() 快速路径（closed 状态）零锁获取
type ResilientProvider struct {
	primary       Provider
	fallbacks     []Provider
	config        ResilientConfig
	state         atomic.Int32 // circuitState
	failures      atomic.Int64
	mu            sync.RWMutex // 仅保护 fallbacks slice
	lastFail      atomic.Int64 // UnixNano 时间戳
	halfOpenProbe atomic.Bool
}

func NewResilientProvider(primary Provider, cfg ResilientConfig) (*ResilientProvider, error) {
	if primary == nil {
		return nil, fmt.Errorf("primary provider must not be nil")
	}
	r := &ResilientProvider{
		primary: primary,
		config:  cfg,
	}
	r.state.Store(int32(circuitClosed))
	return r, nil
}

func (r *ResilientProvider) AddFallback(provider Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fallbacks = append(r.fallbacks, provider)
}

// getFallbacks returns a snapshot of current fallback providers under read lock.
func (r *ResilientProvider) getFallbacks() []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	snapshot := make([]Provider, len(r.fallbacks))
	copy(snapshot, r.fallbacks)
	return snapshot
}

func (r *ResilientProvider) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	if err := r.checkCircuit(); err != nil {
		return nil, err
	}

	resp, err := executeWithRetry(ctx, r, func(p Provider) (*CompletionResponse, error) {
		return p.Complete(ctx, req)
	})
	if err != nil {
		r.recordFailure(err)
		return nil, err
	}

	r.recordSuccess()
	return resp, nil
}

func (r *ResilientProvider) Stream(ctx context.Context, req *CompletionRequest) (<-chan Chunk, error) {
	if err := r.checkCircuit(); err != nil {
		return nil, err
	}

	// 尝试主 Provider（带一次重试）
	var lastErr error
	for attempt := 0; attempt <= 1; attempt++ {
		if attempt > 0 {
			backoff := r.computeRetryBackoff(attempt, lastErr)
			timer := time.NewTimer(backoff)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			}
			timer.Stop()
		}
		ch, err := r.primary.Stream(ctx, req)
		if err == nil {
			r.recordSuccess()
			return ch, nil
		}
		lastErr = err

		// perf-v6 round 8 Task 3：FatalError 不重试
		if re := AsRetryableError(err); re != nil && !re.IsRetryable() {
			return nil, err
		}
	}

	// 主 Provider 失败，尝试 Fallback
	fallbacks := r.getFallbacks()
	for _, fb := range fallbacks {
		fbCh, fbErr := fb.Stream(ctx, req)
		if fbErr == nil {
			r.recordSuccess()
			return fbCh, nil
		}
		lastErr = fbErr
	}

	r.recordFailure(lastErr)
	return nil, fmt.Errorf("%w: stream failed on all providers: %v", ErrFallbackFailed, lastErr)
}

func (r *ResilientProvider) CallTools(ctx context.Context, req *ToolCallRequest) (*ToolCallResponse, error) {
	if err := r.checkCircuit(); err != nil {
		return nil, err
	}

	resp, err := executeWithRetry(ctx, r, func(p Provider) (*ToolCallResponse, error) {
		return p.CallTools(ctx, req)
	})
	if err != nil {
		r.recordFailure(err)
		return nil, err
	}

	r.recordSuccess()
	return resp, nil
}

func (r *ResilientProvider) Embeddings(ctx context.Context, texts []string) ([][]float32, error) {
	if embedder, ok := r.primary.(Embedder); ok {
		resp, err := embedder.Embeddings(ctx, texts)
		if err != nil {
			r.recordFailure(err)
			return nil, err
		}
		r.recordSuccess()
		return resp, nil
	}
	return nil, ErrNotSupported
}

func (r *ResilientProvider) Info() ModelInfo {
	return r.primary.Info()
}

// checkCircuit 检查熔断器状态（perf-v4 Task 10：快速路径零锁；perf-v5 Task 3：修复 CAS 失败方 race）
func (r *ResilientProvider) checkCircuit() error {
	state := circuitState(r.state.Load())
	switch state {
	case circuitClosed:
		// 快速路径：closed 状态零锁直接返回（perf-v4 Task 10）
		return nil
	case circuitOpen:
		// open 状态：检查是否已过恢复时间（无锁读取 lastFail）
		lastFailNanos := r.lastFail.Load()
		if lastFailNanos == 0 {
			return ErrCircuitOpen
		}
		if time.Since(time.Unix(0, lastFailNanos)) > r.config.CircuitRecoverAfter {
			// 升级到 half-open：使用 CAS 避免并发升级
			if r.state.CompareAndSwap(int32(circuitOpen), int32(circuitHalfOpen)) {
				r.halfOpenProbe.Store(false)
				return nil
			}
			// perf-v5 Task 3：CAS 失败方应返回 ErrCircuitOpen，避免错误放行
			return ErrCircuitOpen
		}
		return ErrCircuitOpen
	case circuitHalfOpen:
		// 半开状态只允许一个试探请求
		if r.halfOpenProbe.Load() {
			return ErrCircuitOpen
		}
		r.halfOpenProbe.Store(true)
		return nil
	default:
		return nil
	}
}

func (r *ResilientProvider) recordSuccess() {
	// 成功直接重置为 closed（perf-v4 Task 10：无锁）
	r.failures.Store(0)
	r.state.Store(int32(circuitClosed))
	r.halfOpenProbe.Store(false)
}

func (r *ResilientProvider) recordFailure(err error) {
	if err == nil {
		return
	}
	// v6.x 评估 §4.2 P1-2：调用方主动取消（context.Canceled）或 ctx 超时
	// （DeadlineExceeded）不属于 provider 故障——不计入熔断失败计数。
	// 否则调用方连续取消 CircuitThreshold 次即误打开熔断
	// CircuitRecoverAfter（默认 30s），后续正常请求被误拒。
	// errors.Is 穿透 executeWithRetry 的 %w 包装。
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}

	// perf-v6 round 8 Task 3：客户端错误（4xx 除 429）和authentication error不计入熔断失败计数
	// 因为这些错误不会因 provider 不健康而恢复，触发熔断无意义
	if re := AsRetryableError(err); re != nil && !re.CountsAsFailure() {
		return
	}

	// 失败累加计数并记录时间戳（perf-v4 Task 10：无锁）
	r.failures.Add(1)
	r.lastFail.Store(time.Now().UnixNano())
	r.halfOpenProbe.Store(false)

	if r.failures.Load() >= int64(r.config.CircuitThreshold) {
		r.state.Store(int32(circuitOpen))
	}
}

// executeWithRetry 泛型重试方法，统一 Complete 和 CallTools 的重试逻辑
// perf-v6 round 8 Task 3：识别 RetryableError 并尊重 Retry-After；
// FatalError（不可重试）立即停止；429/5xx 优先使用 Retry-After 而非指数退避
func executeWithRetry[T any](ctx context.Context, r *ResilientProvider, fn func(Provider) (T, error)) (T, error) {
	var lastErr error
	var zero T

	for attempt := 0; attempt <= r.config.MaxRetries; attempt++ {
		if attempt > 0 {
			backoff := r.computeRetryBackoff(attempt, lastErr)
			timer := time.NewTimer(backoff)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return zero, ctx.Err()
			}
			timer.Stop()
		}

		resp, err := fn(r.primary)
		if err == nil {
			return resp, nil
		}
		lastErr = err

		// perf-v6 round 8 Task 3：FatalError 不重试
		if re := AsRetryableError(err); re != nil && !re.IsRetryable() {
			return zero, err
		}
	}

	fallbacks := r.getFallbacks()
	if len(fallbacks) == 0 {
		return zero, fmt.Errorf("%w: %w", ErrRetriesExhausted, lastErr)
	}

	for _, fallback := range fallbacks {
		// Fallback 也使用重试，默认重试 1 次
		for attempt := 0; attempt <= 1; attempt++ {
			if attempt > 0 {
				backoff := r.computeRetryBackoff(attempt, lastErr)
				timer := time.NewTimer(backoff)
				select {
				case <-timer.C:
				case <-ctx.Done():
					timer.Stop()
					return zero, ctx.Err()
				}
				timer.Stop()
			}
			resp, err := fn(fallback)
			if err == nil {
				return resp, nil
			}
			lastErr = err

			// perf-v6 round 8 Task 3：Fallback 路径同样尊重错误分类
			if re := AsRetryableError(err); re != nil && !re.IsRetryable() {
				return zero, err
			}
		}
	}

	return zero, fmt.Errorf("%w: %w", ErrFallbackFailed, lastErr)
}

// computeRetryBackoff 计算重试退避时间（perf-v6 round 8 Task 3）
// 优先使用 Retry-After（如果合理且不超过 MaxBackoff），否则使用指数退避 + jitter
func (r *ResilientProvider) computeRetryBackoff(attempt int, lastErr error) time.Duration {
	if re := AsRetryableError(lastErr); re != nil && re.RetryAfter > 0 {
		// 限流场景：尊重服务器 Retry-After
		// 但要 cap 在 MaxBackoff 之内，避免服务器给个 1 小时导致永远卡住
		if re.RetryAfter > r.config.MaxBackoff {
			return r.config.MaxBackoff
		}
		return re.RetryAfter
	}
	return r.calculateBackoff(attempt)
}

func (r *ResilientProvider) calculateBackoff(attempt int) time.Duration {
	backoff := float64(r.config.RetryBackoff) * math.Pow(2, float64(attempt-1))
	if backoff > float64(r.config.MaxBackoff) {
		backoff = float64(r.config.MaxBackoff)
	}
	// 添加随机抖动 (jitter) 防止惊群效应，但不超过 MaxBackoff
	jitter := rand.Float64() * float64(r.config.RetryBackoff) * 0.5
	result := time.Duration(backoff + jitter)
	if result > r.config.MaxBackoff {
		result = r.config.MaxBackoff
	}
	return result
}
