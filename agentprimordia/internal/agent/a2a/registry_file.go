package a2a

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// FileRegistry 基于文件的 Discovery 实现。
// 适用于 standalone 模式，无需 etcd 等外部依赖。
// 使用 JSON 文件存储注册信息，文件锁保证并发安全。
type FileRegistry struct {
	path     string
	agents   map[string]*AgentRegistry
	watchers []chan DiscoveryEvent
	mu       sync.RWMutex
}

// fileRegistryData 文件存储结构
type fileRegistryData struct {
	Agents map[string]*fileRegistryEntry `json:"agents"`
}

type fileRegistryEntry struct {
	Card      *AgentCard       `json:"card"`
	Endpoints AgentEndpoints   `json:"endpoints"`
	SeenAt    time.Time        `json:"seen_at"`
}

// NewFileRegistry 创建文件注册表。
// path 为注册表文件路径（如 ~/.ap/registry.json）。
func NewFileRegistry(path string) (*FileRegistry, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建注册表目录失败: %w", err)
	}

	r := &FileRegistry{
		path:   path,
		agents: make(map[string]*AgentRegistry),
	}

	// 加载已有数据
	if err := r.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("加载注册表失败: %w", err)
	}

	return r, nil
}

// Register 注册 agent
func (r *FileRegistry) Register(card *AgentCard) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.agents[card.AgentID] = &AgentRegistry{
		Card:   card,
		SeenAt: time.Now(),
	}

	return r.save()
}

// Deregister 注销 agent
func (r *FileRegistry) Deregister(agentID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.agents, agentID)
	r.notify(DiscoveryEvent{Type: EventAgentDeregistered, AgentID: agentID})
	return r.save()
}

// Resolve 查找 agent
func (r *FileRegistry) Resolve(agentID string) (*AgentRegistry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, ok := r.agents[agentID]
	if !ok {
		return nil, fmt.Errorf("agent %q not found", agentID)
	}
	return entry, nil
}

// List 列出所有已注册 agent
func (r *FileRegistry) List() []*AgentRegistry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*AgentRegistry, 0, len(r.agents))
	for _, entry := range r.agents {
		result = append(result, entry)
	}
	return result
}

// Watch 监听注册变更事件
func (r *FileRegistry) Watch() <-chan DiscoveryEvent {
	r.mu.Lock()
	defer r.mu.Unlock()

	ch := make(chan DiscoveryEvent, 32)
	r.watchers = append(r.watchers, ch)
	return ch
}

// load 从文件加载注册表
func (r *FileRegistry) load() error {
	data, err := os.ReadFile(r.path)
	if err != nil {
		return err
	}

	var fileData fileRegistryData
	if err := json.Unmarshal(data, &fileData); err != nil {
		return err
	}

	for id, entry := range fileData.Agents {
		r.agents[id] = &AgentRegistry{
			Card:      entry.Card,
			Endpoints: entry.Endpoints,
			SeenAt:    entry.SeenAt,
		}
	}
	return nil
}

// save 持久化注册表到文件
func (r *FileRegistry) save() error {
	fileData := fileRegistryData{
		Agents: make(map[string]*fileRegistryEntry),
	}
	for id, entry := range r.agents {
		fileData.Agents[id] = &fileRegistryEntry{
			Card:      entry.Card,
			Endpoints: entry.Endpoints,
			SeenAt:    entry.SeenAt,
		}
	}

	data, err := json.MarshalIndent(fileData, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(r.path, data, 0o644)
}

// notify 通知所有监听者
func (r *FileRegistry) notify(event DiscoveryEvent) {
	for _, ch := range r.watchers {
		select {
		case ch <- event:
		default:
		}
	}
}

// Close 关闭注册表
func (r *FileRegistry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, ch := range r.watchers {
		close(ch)
	}
	r.watchers = nil
	return nil
}
