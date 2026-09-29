// Package a2a 实现 Agent 间tool租赁协议（Tool Leasing Protocol）。
//
// 背景：
//
//	A2A 基础 RPC（CreateTask / SubscribeTaskEvents / GetAgentCard）已完备。
//	但未提供"Agent A 临时使用 Agent B 的tool"的标准模式。
//	tool租赁协议在现有 RPC 之上构建，无需修改 protobuf。
//
// 模式：
//  1. Agent A 调用 GetAgentCard 发现 Agent B 的 ToolCapabilities
//  2. Agent A 在本地虚拟化一个 "leased_tool"，指向 Agent B 的 A2A 端点
//  3. Agent A 调用tool时，leased_tool 内部通过 CreateTask + SubscribeTaskEvents
//     将调用转发给 Agent B，并将结果流式返回给 Agent A 的 ReAct 循环
//  4. 租期结束后（超时 / 显式释放），leased_tool 从本地注册表移除
//
// 关键类型：
//   - ToolLease：租赁元数据（租期、费用、配额）
//   - LessorHandler：Agent B 端的tool暴露处理器
//   - LesseeClient：Agent A 端的tool租赁客户端
//   - LeasedTool：Agent A 端的虚拟tool实现
//
// 使用方式（Agent B 暴露tool）：
//
//	lessor := a2a.NewLessorHandler(myToolRegistry, a2a.WithLeaseTTL(5*time.Minute))
//	// lessor 自动将 tools.Registry 中的tool暴露为 A2A-task 格式
//
// 使用方式（Agent A 租用tool）：
//
//	_ = a2a.NewLesseeClient(grpcClient, a2a.WithLeaseMaxDuration(time.Hour))
//	ctx := context.Background()
//	tool, err := client.LeaseTool(ctx, "agent-b", "database_query") // 租赁 Agent B 的数据库查询tool
//	defer tool.Release()                                            // 显式释放
//
//	tool.Execute(ctx, args) // 透明调用，内部通过 A2A 转发
package a2a

import (
	"fmt"
	"sync"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/tools"
)

// ===== 租赁元数据 =====

// LeaseStatus 表示tool租赁状态
type LeaseStatus int

const (
	LeaseStatusPending  LeaseStatus = iota // 等待确认
	LeaseStatusActive                      // 租赁中
	LeaseStatusExpired                     // 已过期
	LeaseStatusReleased                    // 已释放
	LeaseStatusDenied                      // 被拒绝
)

// ToolLease 描述一个tool租赁的元数据
type ToolLease struct {
	// LeaseID 租赁唯一 ID
	LeaseID string `json:"lease_id"`
	// LessorAgent 出租方 Agent 名称
	LessorAgent string `json:"lessor_agent"`
	// ToolName 远程tool名称
	ToolName string `json:"tool_name"`
	// LesseeAgent 租用方 Agent 名称（可选，用于审计）
	LesseeAgent string `json:"lessee_agent,omitempty"`
	// Status 当前租赁状态
	Status LeaseStatus `json:"status"`
	// LeasedAt 租赁开始时间
	LeasedAt time.Time `json:"leased_at"`
	// ExpiresAt 过期时间（租期到后自动 Expired）
	ExpiresAt time.Time `json:"expires_at"`
	// RemoteEndpoint 出租方 A2A gRPC 端点
	RemoteEndpoint string `json:"remote_endpoint"`
	// MaxCalls 最大调用次数（0 表示不限）
	MaxCalls int `json:"max_calls"`
	// UsedCalls 已调用次数
	UsedCalls int `json:"used_calls"`
	// MeteringKey 计量/计费标识（可选）
	MeteringKey string `json:"metering_key,omitempty"`
}

// IsActive 返回租赁是否仍在有效期内
func (l *ToolLease) IsActive() bool {
	if l.Status != LeaseStatusActive {
		return false
	}
	return time.Now().Before(l.ExpiresAt)
}

// CanCall 返回租赁是否可以继续调用
func (l *ToolLease) CanCall() bool {
	if !l.IsActive() {
		return false
	}
	if l.MaxCalls > 0 && l.UsedCalls >= l.MaxCalls {
		return false
	}
	return true
}

// Release 标记租赁为已释放
func (l *ToolLease) Release() {
	l.Status = LeaseStatusReleased
}

// ===== 出租方（Lessor） =====

// LessorOption 配置出租方
type LessorOption func(*LessorHandler)

// WithLeaseTTL 设置默认租赁 TTL（默认 5 分钟）
func WithLeaseTTL(ttl time.Duration) LessorOption {
	return func(l *LessorHandler) { l.defaultTTL = ttl }
}

// WithLeaseMaxDuration 设置允许的最大租赁时长（默认 1 小时）
func WithLeaseMaxDuration(d time.Duration) LessorOption {
	return func(l *LessorHandler) { l.maxDuration = d }
}

// LessorHandler 是 Agent B 端的tool暴露处理器
type LessorHandler struct {
	// registry 是被租赁的本地tool注册表
	registry *tools.Registry
	// defaultTTL 默认租期
	defaultTTL time.Duration
	// maxDuration 最大允许租期
	maxDuration time.Duration
	// leases 当前活跃的租赁（leaseID -> *ToolLease）
	mu     sync.RWMutex
	leases map[string]*ToolLease
	// agentName 本 Agent 名称
	agentName string
}

// NewLessorHandler 创建出租方处理器
func NewLessorHandler(registry *tools.Registry, agentName string, opts ...LessorOption) *LessorHandler {
	l := &LessorHandler{
		registry:    registry,
		defaultTTL:  5 * time.Minute,
		maxDuration: time.Hour,
		leases:      make(map[string]*ToolLease),
		agentName:   agentName,
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// GetLeasedTool 根据 leaseID 返回tool租赁信息
func (l *LessorHandler) GetLeasedTool(leaseID string) (*ToolLease, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	lease, ok := l.leases[leaseID]
	if !ok {
		return nil, false
	}
	// 自动过期检查
	if lease.IsActive() && time.Now().After(lease.ExpiresAt) {
		lease.Status = LeaseStatusExpired
	}
	return lease, true
}

// CreateLease 创建新的tool租赁（由 A2A service layer 调用）
func (l *LessorHandler) CreateLease(toolName, lesseeAgent, remoteEndpoint string, requestedTTL time.Duration) (*ToolLease, error) {
	if _, ok := l.registry.Get(toolName); !ok {
		return nil, fmt.Errorf("tool %q not found in registry", toolName)
	}
	if requestedTTL <= 0 {
		requestedTTL = l.defaultTTL
	}
	if requestedTTL > l.maxDuration {
		requestedTTL = l.maxDuration
	}
	lease := &ToolLease{
		LeaseID:        fmt.Sprintf("lease-%d", time.Now().UnixNano()),
		LessorAgent:    l.agentName,
		ToolName:       toolName,
		LesseeAgent:    lesseeAgent,
		Status:         LeaseStatusActive,
		LeasedAt:       time.Now(),
		ExpiresAt:      time.Now().Add(requestedTTL),
		RemoteEndpoint: remoteEndpoint,
	}
	l.mu.Lock()
	l.leases[lease.LeaseID] = lease
	l.mu.Unlock()
	return lease, nil
}

// ReleaseLease 释放指定租赁
func (l *LessorHandler) ReleaseLease(leaseID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if lease, ok := l.leases[leaseID]; ok && lease.IsActive() {
		lease.Status = LeaseStatusReleased
		return true
	}
	return false
}

// RecordCall 记录一次租赁调用
func (l *LessorHandler) RecordCall(leaseID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	lease, ok := l.leases[leaseID]
	if !ok || !lease.CanCall() {
		return false
	}
	lease.UsedCalls++
	return true
}

// GetAvailableTools 返回可供租赁的tool列表
func (l *LessorHandler) GetAvailableTools() []ToolCapability {
	defs := l.registry.Definitions()
	result := make([]ToolCapability, 0, len(defs))
	for _, def := range defs {
		cap := ToolCapability{
			Name:        getString(def, "name"),
			Description: getString(def, "description"),
		}
		// Registry.Definitions() 返回格式：{"type":"function","function":{"name":...,"description":...,"parameters":...}}
		if f, ok := def["function"].(map[string]any); ok {
			if cap.Name == "" {
				cap.Name = getString(f, "name")
			}
			if cap.Description == "" {
				cap.Description = getString(f, "description")
			}
			if params, ok := f["parameters"].(map[string]any); ok {
				cap.InputSchema = params
			}
		} else if params, ok := def["parameters"].(map[string]any); ok {
			cap.InputSchema = params
		}
		result = append(result, cap)
	}
	return result
}

// GetRegistry 返回底层tool注册表（供内部 task executor 使用）
func (l *LessorHandler) GetRegistry() *tools.Registry {
	return l.registry
}

// ===== tool能力描述 =====

// ToolCapability 是 Agent Card 中描述的可租赁tool
type ToolCapability struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
	// LeaseTerms 租赁条款（可选）
	LeaseTerms *LeaseTerms `json:"lease_terms,omitempty"`
}

// LeaseTerms 租赁条款
type LeaseTerms struct {
	// DefaultTTL 默认租期
	DefaultTTL time.Duration `json:"default_ttl"`
	// MaxTTL 最长允许租期
	MaxTTL time.Duration `json:"max_ttl"`
	// MaxCalls 最大调用次数（0=不限）
	MaxCalls int `json:"max_calls"`
	// RatePerMinute 每分钟最大调用次数（0=不限）
	RatePerMinute int `json:"rate_per_minute"`
}

// ===== 辅助函数 =====

func getString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// Ensure the types are used
var _ = (*ToolLease)(nil)
var _ = (*LessorHandler)(nil)
