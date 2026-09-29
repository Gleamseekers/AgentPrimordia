package a2a

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/tools"
)

// LesseeClient 是 Agent A 端的租赁客户端
type LesseeClient struct {
	registry     *tools.Registry
	leaseMaxTTL  time.Duration
	activeLeases map[string]*ToolLease // leaseID -> lease
	mu           sync.RWMutex
	nextLeaseNum int64
}

// NewLesseeClient 创建租赁客户端
func NewLesseeClient(registry *tools.Registry, opts ...LesseeOption) *LesseeClient {
	c := &LesseeClient{
		registry:     registry,
		leaseMaxTTL:  time.Hour,
		activeLeases: make(map[string]*ToolLease),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// LesseeOption 配置租赁客户端
type LesseeOption func(*LesseeClient)

// WithLeaseClientMaxTTL 设置客户端允许的最大租期
func WithLeaseClientMaxTTL(ttl time.Duration) LesseeOption {
	return func(c *LesseeClient) { c.leaseMaxTTL = ttl }
}

// LeaseTool 模拟租赁tool（创建本地租约记录）
// 实际场景中通过 gRPC 远程调用 LessorHandler.CreateLease
func (c *LesseeClient) LeaseTool(ctx context.Context, lessorName, toolName, remoteEndpoint string) (*ToolLease, error) {
	c.mu.Lock()
	id := fmt.Sprintf("lease-%d-%d", time.Now().UnixNano(), c.nextLeaseNum)
	c.nextLeaseNum++
	c.mu.Unlock()

	lease := &ToolLease{
		LeaseID:        id,
		LessorAgent:    lessorName,
		ToolName:       toolName,
		Status:         LeaseStatusActive,
		LeasedAt:       time.Now(),
		ExpiresAt:      time.Now().Add(c.leaseMaxTTL),
		RemoteEndpoint: remoteEndpoint,
		MaxCalls:       100, // 默认 100 次
	}
	c.mu.Lock()
	c.activeLeases[lease.LeaseID] = lease
	c.mu.Unlock()
	return lease, nil
}

// GetLease 返回指定租赁
func (c *LesseeClient) GetLease(leaseID string) (*ToolLease, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	lease, ok := c.activeLeases[leaseID]
	if !ok {
		return nil, false
	}
	// 自动过期
	if lease.IsActive() && time.Now().After(lease.ExpiresAt) {
		lease.Status = LeaseStatusExpired
	}
	return lease, true
}

// ReleaseLease 释放租赁
func (c *LesseeClient) ReleaseLease(leaseID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	lease, ok := c.activeLeases[leaseID]
	if !ok {
		return false
	}
	lease.Status = LeaseStatusReleased
	delete(c.activeLeases, leaseID)
	return true
}

// ActiveLeaseCount 返回活跃租赁数量
func (c *LesseeClient) ActiveLeaseCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	count := 0
	for _, lease := range c.activeLeases {
		if lease.IsActive() {
			count++
		}
	}
	return count
}
