// interop_router.go — v4.5-4 跨节点 A2A 任务路由（选路 + 熔断 + 故障切换）
//
// 多个 Agent 节点组成候选池：按端点顺序选路，每端点独立熔断
// （resilience.CircuitBreaker）；节点故障（网络/5xx/熔断打开）
// 自动切换到下一可用端点，第三方任务委托不因单点故障中断。
package a2a

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/resilience"
)

// InteropRouterConfig 跨节点路由配置。
type InteropRouterConfig struct {
	// Endpoints 候选节点端点（按优先级排序）
	Endpoints []string
	// FailureThreshold 单节点熔断阈值（默认 3 次连续失败）
	FailureThreshold int
	// CircuitTimeout 熔断恢复时间（默认 10s）
	CircuitTimeout time.Duration
}

// OpenInteropRouter 跨节点 A2A 任务路由。
type OpenInteropRouter struct {
	cfg      InteropRouterConfig
	clients  []*OpenInteropClient
	breakers []*resilience.CircuitBreaker
	mu       sync.Mutex
	cursor   int
}

// NewOpenInteropRouter 创建跨节点路由。
func NewOpenInteropRouter(cfg InteropRouterConfig) *OpenInteropRouter {
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = 3
	}
	if cfg.CircuitTimeout <= 0 {
		cfg.CircuitTimeout = 10 * time.Second
	}
	r := &OpenInteropRouter{cfg: cfg}
	for _, ep := range cfg.Endpoints {
		r.clients = append(r.clients, NewOpenInteropClient(ep))
		r.breakers = append(r.breakers, resilience.NewCircuitBreaker(resilience.Config{
			FailureThreshold: cfg.FailureThreshold,
			Timeout:          cfg.CircuitTimeout,
		}))
	}
	return r
}

// Endpoints 返回候选端点列表。
func (r *OpenInteropRouter) Endpoints() []string {
	return r.cfg.Endpoints
}

// pick 选路：从游标起轮询跳过熔断打开的端点，返回端点序号。
func (r *OpenInteropRouter) pick() (int, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := 0; i < len(r.breakers); i++ {
		idx := (r.cursor + i) % len(r.breakers)
		if r.breakers[idx].State() != resilience.StateOpen {
			return idx, true
		}
	}
	return 0, false
}

// stick 记录最近成功端点，后续请求优先（粘性选路）。
func (r *OpenInteropRouter) stick(idx int) {
	r.mu.Lock()
	r.cursor = idx
	r.mu.Unlock()
}

// route 经路由执行一次 RPC：跳过熔断端点，失败切换下一端点。
func (r *OpenInteropRouter) route(ctx context.Context, fn func(c *OpenInteropClient, ctx context.Context) error) error {
	start := r.cursor
	attempts := 0
	for attempts < len(r.clients) {
		idx, ok := r.pick()
		if !ok {
			return errors.New("a2a interop: 所有端点熔断打开")
		}
		attempts++
		client := r.clients[idx]
		breaker := r.breakers[idx]

		err := breaker.Execute(ctx, func(ctx context.Context) error {
			return fn(client, ctx)
		})
		if err == nil {
			r.stick(idx)
			return nil
		}
		if errors.Is(err, resilience.ErrCircuitOpen) {
			// 该端点熔断，尝试下一个
			r.mu.Lock()
			r.cursor = (idx + 1) % len(r.clients)
			r.mu.Unlock()
			continue
		}
		// 请求失败：记录并切换
		r.mu.Lock()
		r.cursor = (idx + 1) % len(r.clients)
		r.mu.Unlock()
		if attempts == len(r.clients) {
			return fmt.Errorf("a2a interop: 全部端点失败: %w", err)
		}
	}
	return fmt.Errorf("a2a interop: 无可用端点（起点 %d）", start)
}

// SendTask 发送任务到可用节点。
func (r *OpenInteropRouter) SendTask(ctx context.Context, message OpenMessage) (*OpenTask, error) {
	var task *OpenTask
	err := r.route(ctx, func(c *OpenInteropClient, ctx context.Context) error {
		t, e := c.SendTask(ctx, message)
		if e != nil {
			return e
		}
		task = t
		return nil
	})
	if err != nil {
		return nil, err
	}
	return task, nil
}

// FetchAgentCard 获取可用节点的 Agent Card。
func (r *OpenInteropRouter) FetchAgentCard(ctx context.Context) (*OpenAgentCard, error) {
	var card *OpenAgentCard
	err := r.route(ctx, func(c *OpenInteropClient, ctx context.Context) error {
		cc, e := c.FetchAgentCard(ctx)
		if e != nil {
			return e
		}
		card = cc
		return nil
	})
	if err != nil {
		return nil, err
	}
	return card, nil
}

// GetTask 查询任务状态（从候选节点轮询）。
func (r *OpenInteropRouter) GetTask(ctx context.Context, taskID string) (*OpenTask, error) {
	var task *OpenTask
	err := r.route(ctx, func(c *OpenInteropClient, ctx context.Context) error {
		t, e := c.GetTask(ctx, taskID)
		if e != nil {
			return e
		}
		task = t
		return nil
	})
	if err != nil {
		return nil, err
	}
	return task, nil
}

// CancelTask 取消任务。
func (r *OpenInteropRouter) CancelTask(ctx context.Context, taskID string) error {
	return r.route(ctx, func(c *OpenInteropClient, ctx context.Context) error {
		return c.CancelTask(ctx, taskID)
	})
}
