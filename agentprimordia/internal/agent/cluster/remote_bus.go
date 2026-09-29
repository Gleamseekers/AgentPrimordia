package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/bus"
)

// ===== 远程节点管理 =====

// RemoteNode 远程节点连接
type RemoteNode struct {
	ID      string
	Address string // 例如 "http://10.0.0.2:8080"
	client  *http.Client
}

// NewRemoteNode 创建远程节点
func NewRemoteNode(id, address string) *RemoteNode {
	return &RemoteNode{
		ID:      id,
		Address: address,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SendMessage 向远程节点发送消息
func (n *RemoteNode) SendMessage(ctx context.Context, msg *bus.BusMessage) (*bus.BusMessage, error) {
	body, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("remote bus: marshal message: %w", err)
	}

	url := n.Address + "/cluster/message"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("remote bus: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("remote bus: send to %s: %w", n.ID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("remote bus: node %s returned %d: %s", n.ID, resp.StatusCode, string(body))
	}

	var reply bus.BusMessage
	if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil {
		return nil, fmt.Errorf("remote bus: decode reply from %s: %w", n.ID, err)
	}

	return &reply, nil
}

// BroadcastMessage 向远程节点广播消息
func (n *RemoteNode) BroadcastMessage(ctx context.Context, msg *bus.BusMessage) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("remote bus: marshal broadcast: %w", err)
	}

	url := n.Address + "/cluster/broadcast"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("remote bus: create broadcast request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("remote bus: broadcast to %s: %w", n.ID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("remote bus: node %s broadcast returned %d", n.ID, resp.StatusCode)
	}

	return nil
}

// Ping 检查远程节点是否可达
func (n *RemoteNode) Ping(ctx context.Context) error {
	url := n.Address + "/cluster/ping"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("remote bus: create ping request: %w", err)
	}

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("remote bus: ping %s: %w", n.ID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("remote bus: node %s ping returned %d", n.ID, resp.StatusCode)
	}

	return nil
}

// ===== RemoteMessageBus 跨节点消息总线 =====

// RemoteMessageBus 跨节点消息总线
//
// 在 LocalMessageBus 的基础上，增加跨节点消息转发能力：
//   - Send: 先尝试本地投递，如果目标 Agent 不在本地，转发到远程节点
//   - Broadcast: 本地广播 + 远程广播
//   - 通过 HTTP 在节点间通信，可扩展为 gRPC
type RemoteMessageBus struct {
	local  bus.MessageBus
	mu     sync.RWMutex
	nodes  map[string]*RemoteNode // 远程节点列表（nodeID -> RemoteNode）
	logger *slog.Logger
	stats  RemoteBusStats
	state  *DistributedState // 可选：关联分布式状态用于同步
}

// RemoteBusStats 远程消息总线统计（内部使用，含 atomic 字段，禁止值拷贝）
type RemoteBusStats struct {
	LocalSends       atomic.Int64
	RemoteForwards   atomic.Int64
	RemoteFailures   atomic.Int64
	Broadcasts       atomic.Int64
	RemoteBroadcasts atomic.Int64
}

// RemoteBusStatsSnapshot 远程消息总线统计快照（值安全，可自由拷贝）
type RemoteBusStatsSnapshot struct {
	LocalSends       int64 `json:"local_sends"`
	RemoteForwards   int64 `json:"remote_forwards"`
	RemoteFailures   int64 `json:"remote_failures"`
	Broadcasts       int64 `json:"broadcasts"`
	RemoteBroadcasts int64 `json:"remote_broadcasts"`
}

// RemoteBusConfig 远程消息总线配置
type RemoteBusConfig struct {
	// Local 本地消息总线
	Local bus.MessageBus
	// State 分布式状态（可选，用于状态同步）
	State *DistributedState
}

// NewRemoteMessageBus 创建远程消息总线
func NewRemoteMessageBus(cfg RemoteBusConfig) *RemoteMessageBus {
	return &RemoteMessageBus{
		local:  cfg.Local,
		nodes:  make(map[string]*RemoteNode),
		logger: slog.Default(),
		state:  cfg.State,
	}
}

// WithLogger 设置日志器
func (b *RemoteMessageBus) WithLogger(logger *slog.Logger) *RemoteMessageBus {
	b.logger = logger
	return b
}

// AddNode 添加远程节点
func (b *RemoteMessageBus) AddNode(node *RemoteNode) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nodes[node.ID] = node
	b.logger.Info("添加远程节点", "node_id", node.ID, "address", node.Address)
}

// RemoveNode 移除远程节点
func (b *RemoteMessageBus) RemoveNode(nodeID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.nodes, nodeID)
	b.logger.Info("移除远程节点", "node_id", nodeID)
}

// GetNodes 获取所有远程节点
func (b *RemoteMessageBus) GetNodes() []*RemoteNode {
	b.mu.RLock()
	defer b.mu.RUnlock()
	result := make([]*RemoteNode, 0, len(b.nodes))
	for _, node := range b.nodes {
		result = append(result, node)
	}
	return result
}

// GetStats 获取统计信息快照（返回值安全，无 atomic 拷贝）
func (b *RemoteMessageBus) GetStats() RemoteBusStatsSnapshot {
	return RemoteBusStatsSnapshot{
		LocalSends:       b.stats.LocalSends.Load(),
		RemoteForwards:   b.stats.RemoteForwards.Load(),
		RemoteFailures:   b.stats.RemoteFailures.Load(),
		Broadcasts:       b.stats.Broadcasts.Load(),
		RemoteBroadcasts: b.stats.RemoteBroadcasts.Load(),
	}
}

// Send 发送消息到指定 Agent
//
// 策略：
// 1. 先尝试本地投递
// 2. 如果本地没有该 Agent，转发到远程节点（逐个尝试直到成功）
func (b *RemoteMessageBus) Send(ctx context.Context, msg *bus.BusMessage) (*bus.BusMessage, error) {
	// 尝试本地投递
	localAgents := b.local.ListAgents()
	for _, agentID := range localAgents {
		if agentID == msg.To {
			b.stats.LocalSends.Add(1)
			return b.local.Send(ctx, msg)
		}
	}

	// 本地没有目标 Agent，转发到远程节点
	b.mu.RLock()
	nodes := make([]*RemoteNode, 0, len(b.nodes))
	for _, node := range b.nodes {
		nodes = append(nodes, node)
	}
	b.mu.RUnlock()

	if len(nodes) == 0 {
		return nil, fmt.Errorf("remote bus: agent %q not found locally and no remote nodes available", msg.To)
	}

	// 并行尝试所有远程节点
	type result struct {
		node  *RemoteNode
		reply *bus.BusMessage
		err   error
	}
	resultCh := make(chan result, len(nodes))

	for _, node := range nodes {
		go func(n *RemoteNode) {
			reply, err := n.SendMessage(ctx, msg)
			resultCh <- result{node: n, reply: reply, err: err}
		}(node)
	}

	// 等待第一个成功的结果
	var lastErr error
	for i := 0; i < len(nodes); i++ {
		r := <-resultCh
		if r.err == nil && r.reply != nil {
			b.stats.RemoteForwards.Add(1)
			b.logger.Info("消息已转发到远程节点", "to", msg.To, "via", r.node.ID)
			return r.reply, nil
		}
		if r.err != nil {
			lastErr = r.err
		}
	}

	b.stats.RemoteFailures.Add(1)
	if lastErr != nil {
		return nil, fmt.Errorf("remote bus: failed to send to %q via remote nodes: %w", msg.To, lastErr)
	}
	return nil, fmt.Errorf("remote bus: agent %q not found on any node", msg.To)
}

// Broadcast 广播消息到所有 Agent（本地 + 远程）
func (b *RemoteMessageBus) Broadcast(ctx context.Context, msg *bus.BusMessage) map[string]*bus.BusMessage {
	b.stats.Broadcasts.Add(1)

	// 本地广播
	results := b.local.Broadcast(ctx, msg)

	// 远程广播（异步，不阻塞）
	b.mu.RLock()
	nodes := make([]*RemoteNode, 0, len(b.nodes))
	for _, node := range b.nodes {
		nodes = append(nodes, node)
	}
	b.mu.RUnlock()

	var wg sync.WaitGroup

	for _, node := range nodes {
		wg.Add(1)
		go func(n *RemoteNode) {
			defer wg.Done()
			if err := n.BroadcastMessage(ctx, msg); err != nil {
				b.logger.Warn("远程广播失败", "node", n.ID, "error", err)
				b.stats.RemoteFailures.Add(1)
				return
			}
			b.stats.RemoteBroadcasts.Add(1)
		}(node)
	}
	wg.Wait()

	return results
}

// Register 注册本地 Agent 消息处理器
func (b *RemoteMessageBus) Register(agentID string, handler bus.BusMessageHandler) {
	b.local.Register(agentID, handler)
}

// Unregister 注销本地 Agent
func (b *RemoteMessageBus) Unregister(agentID string) {
	b.local.Unregister(agentID)
}

// ListAgents 列出已注册的本地 Agent
func (b *RemoteMessageBus) ListAgents() []string {
	return b.local.ListAgents()
}

// Subscribe 订阅本地 Agent 的消息通道
func (b *RemoteMessageBus) Subscribe(agentID string) <-chan *bus.BusMessage {
	return b.local.Subscribe(agentID)
}

// SyncState 同步分布式状态到远程节点
func (b *RemoteMessageBus) SyncState(ctx context.Context) error {
	if b.state == nil {
		return nil
	}

	snapshot := b.state.ExportForSync()
	if len(snapshot) == 0 {
		return nil
	}

	body, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("remote bus: marshal state: %w", err)
	}

	b.mu.RLock()
	nodes := make([]*RemoteNode, 0, len(b.nodes))
	for _, node := range b.nodes {
		nodes = append(nodes, node)
	}
	b.mu.RUnlock()

	var lastErr error
	for _, node := range nodes {
		url := node.Address + "/cluster/state/sync"
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := node.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("node %s returned %d", node.ID, resp.StatusCode)
			continue
		}
	}

	return lastErr
}

// ReceiveStateSync 接收远程状态同步请求
//
// 作为 HTTP handler 使用，处理来自其他节点的状态同步
func (b *RemoteMessageBus) ReceiveStateSync(snapshot map[string]RemoteEntry) int {
	if b.state == nil {
		return 0
	}
	return b.state.Merge(snapshot)
}

// ===== HTTP Handler（供远程节点调用） =====

// MessageHandler 返回处理远程消息的 HTTP handler
//
// 部署方式：
//
//	mux.HandleFunc("/cluster/message", bus.MessageHandler())
//	mux.HandleFunc("/cluster/broadcast", bus.BroadcastHandler())
//	mux.HandleFunc("/cluster/ping", bus.PingHandler())
//	mux.HandleFunc("/cluster/state/sync", bus.StateSyncHandler())
func (b *RemoteMessageBus) MessageHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var msg bus.BusMessage
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		reply, err := b.local.Send(r.Context(), &msg)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(reply)
	}
}

// BroadcastHandler 返回处理远程广播的 HTTP handler
func (b *RemoteMessageBus) BroadcastHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var msg bus.BusMessage
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		// 本地广播（不等待结果）
		go b.local.Broadcast(r.Context(), &msg)

		w.WriteHeader(http.StatusOK)
	}
}

// PingHandler 返回健康检查 HTTP handler
func (b *RemoteMessageBus) PingHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		b.mu.RLock()
		nodeCount := len(b.nodes)
		b.mu.RUnlock()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":       "ok",
			"node_count":   nodeCount,
			"local_agents": len(b.local.ListAgents()),
		})
	}
}

// StateSyncHandler 返回处理状态同步的 HTTP handler
func (b *RemoteMessageBus) StateSyncHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if b.state == nil {
			http.Error(w, "state not configured", http.StatusServiceUnavailable)
			return
		}

		var snapshot map[string]RemoteEntry
		if err := json.NewDecoder(r.Body).Decode(&snapshot); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		merged := b.ReceiveStateSync(snapshot)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"merged": merged,
		})
	}
}
