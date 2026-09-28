// Package hitl 提供人机协作（Human-in-the-Loop）管理
package hitl

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// ErrHumanChannelClosed human input channel closed错误
var ErrHumanChannelClosed = errors.New("human input channel closed")

// InterruptReason 中断原因
type InterruptReason string

const (
	InterruptToolConfirm   InterruptReason = "tool_confirm"
	InterruptDecisionPoint InterruptReason = "decision_point"
	InterruptBudgetExceed  InterruptReason = "budget_exceed"
	InterruptCustom        InterruptReason = "custom"
)

// InterruptPoint 中断点配置
type InterruptPoint struct {
	Type     InterruptReason
	ToolName string
	Message  string
}

// InterruptRequest 中断请求（Agent 发出）
type InterruptRequest struct {
	Reason  InterruptReason `json:"reason"`
	Message string          `json:"message"`
	Data    map[string]any  `json:"data,omitempty"`
	Turn    int             `json:"turn"`
}

// HumanResponse 人类响应
type HumanResponse struct {
	Approved bool           `json:"approved"`
	Input    string         `json:"input,omitempty"`
	Modified map[string]any `json:"modified,omitempty"`
}

// HITLConfig 人机协作配置
type HITLConfig struct {
	InterruptPoints  []InterruptPoint
	HumanInputChan   <-chan *HumanResponse
	OnInterrupt      func(req *InterruptRequest)
	AutoApproveTools []string
}

// HITLManager 人机协作管理器
//
// P1-4（评估报告 §4.2）：HITLManager 只有单个 pending 槽和一个共享
// responseCh。并行 tool 执行下多个 goroutine 并发调用 RequestInterrupt 时：
//   - 后到者覆盖 pending，Pending() 只反映最后一个请求；
//   - Resume() 只投递一个响应，被其中一个 goroutine 消费，另一个挂起到
//     ctx 超时（表现为"并行 tool × HITL 挂死"）。
//
// 修复：用 confirmMu 把「登记 pending → 等待响应」整段临界区串行化，
// 保证同一时刻只有一个中断在飞；后续请求排队等待前一个完成（响应或超时）
// 后再成为 pending。串行路径（默认）无锁竞争，行为零变化。
type HITLManager struct {
	config  HITLConfig
	pending *InterruptRequest
	mu      sync.RWMutex
	// confirmMu 串行化确认临界区（P1-4）。与 mu 分工：
	// mu 只保护 pending/activeCh 字段的短临界区；
	// confirmMu 保护跨阻塞等待的长临界区。
	confirmMu sync.Mutex
	// activeCh 当前唯一在飞请求的专属响应通道（P1-4）。
	// 确认流程已串行化，因此同时只有一个在飞请求；Resume() 只投递给该
	// 通道，请求结束（响应或超时）后即废弃。迟到的 Resume 响应因无
	// activeCh 可投而被丢弃，不会滞留缓冲区污染后续无关请求。
	activeCh chan *HumanResponse
}

// NewHITLManager 创建人机协作管理器
func NewHITLManager(config HITLConfig) *HITLManager {
	return &HITLManager{config: config}
}

// ShouldInterrupt 判断当前操作是否需要中断
func (m *HITLManager) ShouldInterrupt(toolName string, reason InterruptReason) bool {
	if m.isAutoApproved(toolName) {
		return false
	}

	for _, ip := range m.config.InterruptPoints {
		if ip.Type != reason {
			continue
		}
		if ip.ToolName == "" || ip.ToolName == toolName {
			return true
		}
	}
	return false
}

// isAutoApproved 检查tool是否在自动批准列表中
func (m *HITLManager) isAutoApproved(toolName string) bool {
	for _, name := range m.config.AutoApproveTools {
		if name == toolName {
			return true
		}
	}
	return false
}

// RequestInterrupt 发起中断请求，阻塞等待人类响应。
//
// P1-4：确认流程串行化。整个「登记 pending → 触发 OnInterrupt → 等待响应」
// 临界区在 confirmMu 保护下执行，同一时刻只允许一个中断在飞，从而：
//   - pending 不会被并发请求互相覆盖；
//   - Resume() 投递的响应只会送达当前唯一在飞的请求；
//   - 排队的后续请求在前一个完成（响应或 ctx 超时）后才被受理，
//     不会再出现"第二个 goroutine 永久挂起"。
func (m *HITLManager) RequestInterrupt(ctx context.Context, req *InterruptRequest) (*HumanResponse, error) {
	// 排队前先做一次 ctx 检查，避免已取消的请求仍去抢锁
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("timed out waiting for human response: %w", err)
	}

	m.confirmMu.Lock()
	defer m.confirmMu.Unlock()

	// P1-4：为本次请求分配专属响应通道。请求结束（响应或 ctx 超时）后
	// 通道即废弃，迟到的 Resume 响应无处可投而被丢弃，从根本上消除
	// "过期响应被后续无关请求误消费"的问题。
	active := make(chan *HumanResponse, 1)
	m.mu.Lock()
	m.pending = req
	m.activeCh = active
	m.mu.Unlock()
	defer m.finishRequest(active)

	if m.config.OnInterrupt != nil {
		m.config.OnInterrupt(req)
	}

	var resp *HumanResponse

	select {
	case r, ok := <-m.config.HumanInputChan:
		if !ok {
			return nil, ErrHumanChannelClosed
		}
		resp = r
	case r, ok := <-active:
		if !ok {
			return nil, fmt.Errorf("response channel closed")
		}
		resp = r
	case <-ctx.Done():
		return nil, fmt.Errorf("timed out waiting for human response: %w", ctx.Err())
	}

	return resp, nil
}

// finishRequest 结束当前在飞请求：清理 pending 与 activeCh（mu 短临界区）。
// 仅当 activeCh 仍指向本次请求的通道时才清理，避免误清排队请求的状态。
func (m *HITLManager) finishRequest(active chan *HumanResponse) {
	m.mu.Lock()
	if m.activeCh == active {
		m.activeCh = nil
	}
	m.pending = nil
	m.mu.Unlock()
}

// Resume 恢复 Agent 执行（外部调用）。
//
// P1-4：只投递给当前唯一在飞的请求（确认流程已串行化，同时只有一个）。
// 无在飞请求时直接丢弃——响应不会滞留缓冲区，因此不会被后续无关请求
// 误消费；非阻塞发送保证外部调用方永远不会被挂起。
func (m *HITLManager) Resume(response *HumanResponse) {
	m.mu.RLock()
	ch := m.activeCh
	m.mu.RUnlock()
	if ch == nil {
		return
	}
	select {
	case ch <- response:
	default:
	}
}

// Pending 返回当前挂起的中断请求
func (m *HITLManager) Pending() *InterruptRequest {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.pending
}
