package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent/discovery"
)

// ===== KVStore 接口 =====

// KVStore 分布式 KV 存储接口
//
// 用于支撑分布式服务发现。
// 具体实现可以是内存版（测试用）、etcd、Consul 或 ZooKeeper。
type KVStore interface {
	// Put 写入键值（带 TTL，0 表示永不过期）
	Put(ctx context.Context, key, value string, ttl time.Duration) error
	// Get 读取键值
	Get(ctx context.Context, key string) (string, error)
	// Delete 删除键
	Delete(ctx context.Context, key string) error
	// ListByPrefix 列出指定前缀的所有键值
	ListByPrefix(ctx context.Context, prefix string) (map[string]string, error)
	// Watch 监听前缀下键值变化（返回事件通道）
	Watch(ctx context.Context, prefix string) <-chan KVEvent
	// Close 关闭存储
	Close() error
}

// KVEvent KV 变化事件
type KVEvent struct {
	Type  EventType
	Key   string
	Value string
}

// CASStore 支持原子"比较并设置"的 KV 存储。
//
// v6.x（评估报告 Issue #3）：选举不能仅靠"最小 ID 节点"规则，否则
// 网络分区恢复时多个节点同时自认为 Leader（split-brain）。通过
// CompareAndSwap 以原子方式抢占/续约 _leader_lease，保证任意时刻
// 只有一个节点持有租约——这是 fencing 的基础。
//
// MemKVStore / EtcdKVStore 实现该接口；调用方用类型断言探测。
type CASStore interface {
	// CompareAndSwap 仅当 key 的当前值等于 oldValue（或 oldValue 为空表示
	// "键不存在或已过期"）时，将 key 更新为 newValue 并设置 TTL。
	// 返回 true 表示 CAS 成功；false 表示被其他值占据。
	CompareAndSwap(ctx context.Context, key, oldValue, newValue string, ttl time.Duration) (bool, error)
}

// EventType 事件类型
type EventType int

const (
	EventPut    EventType = iota // 键被创建或更新
	EventDelete                  // 键被删除
)

// ===== MemKVStore 内存 KV 实现（用于测试和单节点模式） =====

// MemKVStore 基于内存的 KV 存储，支持 TTL 和 Watch
type MemKVStore struct {
	mu       sync.RWMutex
	data     map[string]*memEntry
	watchers map[string][]*watcherEntry
	logger   *slog.Logger
}

type watcherEntry struct {
	ch     chan KVEvent
	closed bool
}

type memEntry struct {
	value     string
	expiresAt time.Time
}

// NewMemKVStore 创建内存 KV 存储
func NewMemKVStore() *MemKVStore {
	return &MemKVStore{
		data:     make(map[string]*memEntry),
		watchers: make(map[string][]*watcherEntry),
		logger:   slog.Default(),
	}
}

// Put 写入键值
func (s *MemKVStore) Put(ctx context.Context, key, value string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	expiresAt := time.Time{}
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl)
	}

	s.data[key] = &memEntry{value: value, expiresAt: expiresAt}

	// 通知 watchers
	s.notifyWatchers(key, value, EventPut)
	return nil
}

// Get 读取键值
func (s *MemKVStore) Get(ctx context.Context, key string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.data[key]
	if !ok {
		return "", fmt.Errorf("key not found: %s", key)
	}
	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		return "", fmt.Errorf("key expired: %s", key)
	}
	return entry.value, nil
}

// Delete 删除键
func (s *MemKVStore) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.data, key)
	s.notifyWatchers(key, "", EventDelete)
	return nil
}

// CompareAndSwap 原子 CAS（内存实现）。
//
// oldValue 为空表示"期望键不存在或已过期"；否则必须精确匹配当前值。
func (s *MemKVStore) CompareAndSwap(ctx context.Context, key, oldValue, newValue string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.data[key]
	if ok {
		// 已过期视为不存在
		if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
			ok = false
		}
	}
	if ok {
		if oldValue != "" && entry.value != oldValue {
			return false, nil
		}
		// oldValue 为空但键存在 → CAS 失败（被他人占据）
		if oldValue == "" {
			return false, nil
		}
	} else if oldValue != "" {
		// 键不存在但期望旧值非空 → CAS 失败
		return false, nil
	}

	expiresAt := time.Time{}
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl)
	}
	s.data[key] = &memEntry{value: newValue, expiresAt: expiresAt}
	s.notifyWatchers(key, newValue, EventPut)
	return true, nil
}

var _ CASStore = (*MemKVStore)(nil)

// ListByPrefix 列出指定前缀的所有键值
func (s *MemKVStore) ListByPrefix(ctx context.Context, prefix string) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]string)
	now := time.Now()
	for key, entry := range s.data {
		if strings.HasPrefix(key, prefix) {
			if entry.expiresAt.IsZero() || now.Before(entry.expiresAt) {
				result[key] = entry.value
			}
		}
	}
	return result, nil
}

// Watch 监听前缀下键值变化
func (s *MemKVStore) Watch(ctx context.Context, prefix string) <-chan KVEvent {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry := &watcherEntry{ch: make(chan KVEvent, 64)}
	s.watchers[prefix] = append(s.watchers[prefix], entry)

	// ctx 取消时关闭通道
	go func() {
		<-ctx.Done()
		s.mu.Lock()
		defer s.mu.Unlock()
		if !entry.closed {
			entry.closed = true
			close(entry.ch)
		}
		// 从 watchers 列表中移除
		ws := s.watchers[prefix]
		for i, w := range ws {
			if w == entry {
				s.watchers[prefix] = append(ws[:i], ws[i+1:]...)
				break
			}
		}
	}()

	return entry.ch
}

// Close 关闭存储
func (s *MemKVStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, ws := range s.watchers {
		for _, entry := range ws {
			if !entry.closed {
				entry.closed = true
				close(entry.ch)
			}
		}
	}
	s.watchers = make(map[string][]*watcherEntry)
	s.data = make(map[string]*memEntry)
	return nil
}

// notifyWatchers 通知所有匹配的 watchers（调用方需持有写锁）
func (s *MemKVStore) notifyWatchers(key, value string, et EventType) {
	for prefix, ws := range s.watchers {
		if strings.HasPrefix(key, prefix) {
			for _, entry := range ws {
				if entry.closed {
					continue
				}
				select {
				case entry.ch <- KVEvent{Type: et, Key: key, Value: value}:
				default:
					s.logger.Warn("watcher 通道已满，跳过事件", "key", key)
				}
			}
		}
	}
}

// Cleanup 清理过期键
func (s *MemKVStore) Cleanup() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	count := 0
	for key, entry := range s.data {
		if !entry.expiresAt.IsZero() && now.After(entry.expiresAt) {
			delete(s.data, key)
			s.notifyWatchers(key, "", EventDelete)
			count++
		}
	}
	return count
}

// ===== DistributedDiscovery 分布式服务发现 =====

// discoveryKeyPrefix etcd/KV 中 Agent 信息的键前缀
const discoveryKeyPrefix = "github.com/Gleamseekers/AgentPrimordia/discovery/"

// DistributedDiscovery 基于 KV 存储的分布式服务发现
//
// 实现 discovery.Discovery 接口，使用 KVStore 作为后端。
// 支持跨节点服务发现，可通过 etcd/Consul/ZooKeeper 等 KV 存储实现真正的分布式发现。
type DistributedDiscovery struct {
	kv        KVStore
	localID   string
	logger    *slog.Logger
	heartbeat time.Duration
	mu        sync.RWMutex
	// 本地缓存的 Agent 信息（从 KV 存储同步）
	cache map[string]*discovery.AgentInfo
	// 心跳停止
	stopCh  chan struct{}
	running bool
}

// DistributedDiscoveryConfig 分布式发现配置
type DistributedDiscoveryConfig struct {
	// NodeID 本地节点 ID
	NodeID string
	// KVStore KV 存储后端
	KVStore KVStore
	// HeartbeatInterval 心跳间隔（默认 10s）
	HeartbeatInterval time.Duration
	// SyncInterval 从 KV 存储同步缓存的间隔（默认 15s）
	SyncInterval time.Duration
}

// NewDistributedDiscovery 创建分布式服务发现
func NewDistributedDiscovery(cfg DistributedDiscoveryConfig) *DistributedDiscovery {
	if cfg.HeartbeatInterval == 0 {
		cfg.HeartbeatInterval = 10 * time.Second
	}
	if cfg.SyncInterval == 0 {
		cfg.SyncInterval = 15 * time.Second
	}

	return &DistributedDiscovery{
		kv:        cfg.KVStore,
		localID:   cfg.NodeID,
		logger:    slog.Default(),
		heartbeat: cfg.HeartbeatInterval,
		cache:     make(map[string]*discovery.AgentInfo),
		stopCh:    make(chan struct{}),
	}
}

// WithLogger 设置日志器
func (d *DistributedDiscovery) WithLogger(logger *slog.Logger) *DistributedDiscovery {
	d.logger = logger
	return d
}

// Start 启动分布式发现（开始心跳和缓存同步）
func (d *DistributedDiscovery) Start(ctx context.Context) error {
	d.mu.Lock()
	if d.running {
		d.mu.Unlock()
		return fmt.Errorf("distributed discovery already running")
	}
	d.running = true
	d.mu.Unlock()

	// 初始全量同步
	if err := d.syncFromKV(ctx); err != nil {
		d.logger.Warn("初始全量同步失败", "error", err)
	}

	// 启动心跳 goroutine
	go d.heartbeatLoop(ctx)

	// 启动缓存同步 goroutine
	go d.syncLoop(ctx)

	// 启动 KV Watch 监听
	go d.watchLoop(ctx)

	d.logger.Info("分布式发现启动", "node_id", d.localID)
	return nil
}

// Register 注册 Agent
func (d *DistributedDiscovery) Register(ctx context.Context, info *discovery.AgentInfo) error {
	info.LastSeen = time.Now()

	key := discoveryKeyPrefix + info.ID
	value, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("distributed discovery: marshal agent info: %w", err)
	}

	// 写入 KV 存储，TTL 为心跳间隔的 3 倍
	ttl := d.heartbeat * 3
	if err := d.kv.Put(ctx, key, string(value), ttl); err != nil {
		return fmt.Errorf("distributed discovery: put to kv: %w", err)
	}

	// 更新本地缓存
	d.mu.Lock()
	d.cache[info.ID] = info
	d.mu.Unlock()

	d.logger.Info("Agent 注册到分布式发现", "id", info.ID, "name", info.Name)
	return nil
}

// Unregister 注销 Agent
func (d *DistributedDiscovery) Unregister(ctx context.Context, agentID string) error {
	key := discoveryKeyPrefix + agentID
	if err := d.kv.Delete(ctx, key); err != nil {
		return fmt.Errorf("distributed discovery: delete from kv: %w", err)
	}

	d.mu.Lock()
	delete(d.cache, agentID)
	d.mu.Unlock()

	d.logger.Info("Agent 从分布式发现注销", "id", agentID)
	return nil
}

// Discover 发现指定 Agent
func (d *DistributedDiscovery) Discover(ctx context.Context, agentID string) (*discovery.AgentInfo, error) {
	// 先查本地缓存
	d.mu.RLock()
	info, ok := d.cache[agentID]
	d.mu.RUnlock()

	if ok {
		cp := *info
		return &cp, nil
	}

	// 缓存未命中，从 KV 存储读取
	key := discoveryKeyPrefix + agentID
	value, err := d.kv.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("distributed discovery: agent %q not found: %w", agentID, err)
	}

	var agentInfo discovery.AgentInfo
	if err := json.Unmarshal([]byte(value), &agentInfo); err != nil {
		return nil, fmt.Errorf("distributed discovery: unmarshal agent info: %w", err)
	}

	// 更新缓存
	d.mu.Lock()
	d.cache[agentID] = &agentInfo
	d.mu.Unlock()

	cp := agentInfo
	return &cp, nil
}

// ListAgents 列出所有 Agent
func (d *DistributedDiscovery) ListAgents(ctx context.Context) ([]*discovery.AgentInfo, error) {
	// 先从 KV 存储获取最新列表
	kvs, err := d.kv.ListByPrefix(ctx, discoveryKeyPrefix)
	if err != nil {
		// KV 存储不可用，回退到缓存
		d.mu.RLock()
		result := make([]*discovery.AgentInfo, 0, len(d.cache))
		for _, info := range d.cache {
			cp := *info
			result = append(result, &cp)
		}
		d.mu.RUnlock()
		return result, nil
	}

	// 解析并更新缓存
	d.mu.Lock()
	newCache := make(map[string]*discovery.AgentInfo)
	result := make([]*discovery.AgentInfo, 0, len(kvs))

	for _, value := range kvs {
		var info discovery.AgentInfo
		if err := json.Unmarshal([]byte(value), &info); err != nil {
			continue
		}
		newCache[info.ID] = &info
		cp := info
		result = append(result, &cp)
	}
	d.cache = newCache
	d.mu.Unlock()

	return result, nil
}

// Heartbeat 发送心跳（刷新 KV 中的 TTL）
func (d *DistributedDiscovery) Heartbeat(ctx context.Context, agentID string) error {
	key := discoveryKeyPrefix + agentID

	// 读取当前值
	value, err := d.kv.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("distributed discovery: heartbeat get: %w", err)
	}

	var info discovery.AgentInfo
	if err := json.Unmarshal([]byte(value), &info); err != nil {
		return fmt.Errorf("distributed discovery: heartbeat unmarshal: %w", err)
	}

	info.LastSeen = time.Now()
	newValue, err := json.Marshal(&info)
	if err != nil {
		return fmt.Errorf("distributed discovery: heartbeat marshal: %w", err)
	}

	// 刷新 TTL
	ttl := d.heartbeat * 3
	if err := d.kv.Put(ctx, key, string(newValue), ttl); err != nil {
		return fmt.Errorf("distributed discovery: heartbeat put: %w", err)
	}

	// 更新本地缓存
	d.mu.Lock()
	d.cache[agentID] = &info
	d.mu.Unlock()

	return nil
}

// Close 关闭服务
func (d *DistributedDiscovery) Close() error {
	d.mu.Lock()
	if d.running {
		d.running = false
		close(d.stopCh)
	}
	d.mu.Unlock()

	return d.kv.Close()
}

// ===== 内部 goroutine =====

// heartbeatLoop 心跳循环
func (d *DistributedDiscovery) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(d.heartbeat)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-d.stopCh:
			return
		case <-ticker.C:
			if err := d.Heartbeat(ctx, d.localID); err != nil {
				d.logger.Warn("心跳失败", "node_id", d.localID, "error", err)
			}
		}
	}
}

// syncLoop 定期从 KV 存储全量同步缓存
func (d *DistributedDiscovery) syncLoop(ctx context.Context) {
	ticker := time.NewTicker(d.heartbeat * 2)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-d.stopCh:
			return
		case <-ticker.C:
			if err := d.syncFromKV(ctx); err != nil {
				d.logger.Warn("缓存同步失败", "error", err)
			}
		}
	}
}

// watchLoop 监听 KV 变化，实时更新缓存
func (d *DistributedDiscovery) watchLoop(ctx context.Context) {
	ch := d.kv.Watch(ctx, discoveryKeyPrefix)

	for {
		select {
		case <-ctx.Done():
			return
		case <-d.stopCh:
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			d.handleKVEvent(event)
		}
	}
}

// handleKVEvent 处理 KV 变化事件
func (d *DistributedDiscovery) handleKVEvent(event KVEvent) {
	// 从 key 中提取 agentID
	agentID := strings.TrimPrefix(event.Key, discoveryKeyPrefix)

	d.mu.Lock()
	defer d.mu.Unlock()

	switch event.Type {
	case EventPut:
		var info discovery.AgentInfo
		if err := json.Unmarshal([]byte(event.Value), &info); err == nil {
			d.cache[agentID] = &info
			d.logger.Info("发现新 Agent", "id", agentID)
		}
	case EventDelete:
		delete(d.cache, agentID)
		d.logger.Info("Agent 已移除", "id", agentID)
	}
}

// syncFromKV 从 KV 存储全量同步缓存
func (d *DistributedDiscovery) syncFromKV(ctx context.Context) error {
	kvs, err := d.kv.ListByPrefix(ctx, discoveryKeyPrefix)
	if err != nil {
		return err
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	newCache := make(map[string]*discovery.AgentInfo)
	for _, value := range kvs {
		var info discovery.AgentInfo
		if err := json.Unmarshal([]byte(value), &info); err != nil {
			continue
		}
		newCache[info.ID] = &info
	}
	d.cache = newCache

	return nil
}
