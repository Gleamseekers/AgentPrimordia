package a2a

import (
	"context"
	"fmt"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent/discovery"
)

// DiscoveryAdapter 将 discovery.Discovery（内部服务发现）适配为 a2a.Discovery 接口。
// 这使得内部注册的 Agent 可以通过 A2A 协议被外部发现和调用。
//
// 注意：a2a.Discovery 的 Watch() 方法在当前适配中返回一个仅推送注册/注销事件的通道；
// 如需实时心跳事件推送，应直接使用 discovery.Discovery 的 Heartbeat 机制配合外部事件总线。
type DiscoveryAdapter struct {
	inner discovery.Discovery
	watch chan DiscoveryEvent
}

// 编译期接口满足检查
var _ Discovery = (*DiscoveryAdapter)(nil)

// NewDiscoveryAdapter 创建适配器，将内部 discovery.Discovery 包装为 a2a.Discovery。
func NewDiscoveryAdapter(inner discovery.Discovery) *DiscoveryAdapter {
	return &DiscoveryAdapter{
		inner: inner,
		watch: make(chan DiscoveryEvent, 64),
	}
}

// Register 将 AgentCard 转换为 AgentInfo 并注册到内部发现服务。
func (a *DiscoveryAdapter) Register(card *AgentCard) error {
	info := agentCardToInfo(card)
	if err := a.inner.Register(context.Background(), info); err != nil {
		return fmt.Errorf("a2a discovery adapter: register %q: %w", card.AgentID, err)
	}
	a.emitEvent(DiscoveryEvent{
		Type:    EventAgentRegistered,
		AgentID: card.AgentID,
		Card:    card,
	})
	return nil
}

// Deregister 从内部发现服务注销 Agent。
func (a *DiscoveryAdapter) Deregister(agentID string) error {
	if err := a.inner.Unregister(context.Background(), agentID); err != nil {
		return fmt.Errorf("a2a discovery adapter: deregister %q: %w", agentID, err)
	}
	a.emitEvent(DiscoveryEvent{
		Type:    EventAgentDeregistered,
		AgentID: agentID,
	})
	return nil
}

// Resolve 从内部发现服务解析 Agent 并转换为 AgentRegistry。
func (a *DiscoveryAdapter) Resolve(agentID string) (*AgentRegistry, error) {
	info, err := a.inner.Discover(context.Background(), agentID)
	if err != nil {
		return nil, fmt.Errorf("a2a discovery adapter: resolve %q: %w", agentID, err)
	}
	return agentInfoToRegistry(info), nil
}

// List 列出所有已注册的 Agent。
func (a *DiscoveryAdapter) List() []*AgentRegistry {
	infos, err := a.inner.ListAgents(context.Background())
	if err != nil {
		return nil
	}
	out := make([]*AgentRegistry, 0, len(infos))
	for _, info := range infos {
		out = append(out, agentInfoToRegistry(info))
	}
	return out
}

// Watch 返回发现事件通道。
func (a *DiscoveryAdapter) Watch() <-chan DiscoveryEvent {
	return a.watch
}

// emitEvent 非阻塞推送事件到 watch 通道。
func (a *DiscoveryAdapter) emitEvent(ev DiscoveryEvent) {
	select {
	case a.watch <- ev:
	default:
		// 通道满时丢弃事件，避免阻塞注册/注销操作
	}
}

// agentCardToInfo 将 A2A AgentCard 转换为内部 AgentInfo。
func agentCardToInfo(card *AgentCard) *discovery.AgentInfo {
	info := &discovery.AgentInfo{
		ID:       card.AgentID,
		Name:     card.Name,
		Metadata: card.Metadata,
	}
	if card.Endpoints.BaseURL != "" {
		info.Address = card.Endpoints.BaseURL
	}
	for _, skill := range card.Skills {
		info.Capabilities = append(info.Capabilities, skill.Name)
	}
	return info
}

// agentInfoToRegistry 将内部 AgentInfo 转换为 A2A AgentRegistry。
func agentInfoToRegistry(info *discovery.AgentInfo) *AgentRegistry {
	card := &AgentCard{
		Protocol: "a2a",
		AgentID:  info.ID,
		Name:     info.Name,
		Metadata: info.Metadata,
	}
	card.Endpoints.BaseURL = info.Address
	for _, cap := range info.Capabilities {
		card.Skills = append(card.Skills, AgentSkill{Name: cap})
	}
	return &AgentRegistry{
		Card:   card,
		SeenAt: info.LastSeen,
	}
}
