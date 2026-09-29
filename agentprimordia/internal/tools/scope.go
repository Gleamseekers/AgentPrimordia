package tools

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/concurrency"
)

type ScopePolicy interface {
	Allow(agentID, resource string) bool
	Validate(agentScopes map[string][]string) error
}

type FileScopePolicy struct {
	mu          sync.RWMutex
	agentScopes map[string][]string
}

func NewFileScopePolicy() *FileScopePolicy {
	return &FileScopePolicy{
		agentScopes: make(map[string][]string),
	}
}

func (p *FileScopePolicy) SetScope(agentID string, paths []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.agentScopes[agentID] = paths
}

func (p *FileScopePolicy) GetScope(agentID string) []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	paths := p.agentScopes[agentID]
	if paths == nil {
		return nil
	}
	cp := make([]string, len(paths))
	copy(cp, paths)
	return cp
}

func (p *FileScopePolicy) RemoveScope(agentID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.agentScopes, agentID)
}

func (p *FileScopePolicy) Allow(agentID, resource string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	scope, exists := p.agentScopes[agentID]
	if !exists {
		// 未注册的 Agent 默认access denied
		return false
	}

	if len(scope) == 0 {
		// 已注册但未设置路径限制的 Agent 默认access denied
		// 如需全局权限，请显式设置 scope 为 ["/"] 或具体路径
		return false
	}

	absPath := filepath.Clean(resource)
	for _, s := range scope {
		scopeAbs := filepath.Clean(s)
		// 根路径 "/" 或 "\" 匹配所有路径（Windows 兼容）
		if scopeAbs == "/" || scopeAbs == `\` {
			return true
		}
		if absPath == scopeAbs || strings.HasPrefix(absPath, scopeAbs+string(filepath.Separator)) {
			return true
		}
	}

	return false
}

func (p *FileScopePolicy) Validate(agentScopes map[string][]string) error {
	scopes := make([][]string, 0, len(agentScopes))
	for _, scope := range agentScopes {
		scopes = append(scopes, scope)
	}
	return concurrency.ValidateScopes(scopes)
}

func (p *FileScopePolicy) ValidateCurrent() error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	scopes := make([][]string, 0, len(p.agentScopes))
	for _, scope := range p.agentScopes {
		scopes = append(scopes, scope)
	}
	return concurrency.ValidateScopes(scopes)
}

func NewScopeDeniedError(agentID, resource string) error {
	return fmt.Errorf("agent %s denied access to resource %s", agentID, resource)
}
