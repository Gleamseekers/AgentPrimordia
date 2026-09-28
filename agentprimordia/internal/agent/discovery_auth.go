// discovery_auth.go — Token 认证与鉴权发现服务
package agent

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// AgentIdentity Agent 身份信息
type AgentIdentity struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Roles    []string          `json:"roles"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// TokenAuthenticator Token 认证器（HMAC-SHA256 签名）
type TokenAuthenticator struct {
	secret []byte
}

// NewTokenAuthenticator 创建 Token 认证器
func NewTokenAuthenticator(secret string) *TokenAuthenticator {
	return &TokenAuthenticator{
		secret: []byte(secret),
	}
}

// GenerateToken 为 Agent 身份生成签名 token
func (a *TokenAuthenticator) GenerateToken(identity *AgentIdentity) (string, error) {
	if identity == nil {
		return "", errors.New("identity must not be empty")
	}

	payload, err := json.Marshal(identity)
	if err != nil {
		return "", fmt.Errorf("failed to serialize identity: %w", err)
	}

	mac := hmac.New(sha256.New, a.secret)
	mac.Write(payload)
	signature := mac.Sum(nil)

	// token 格式: base64(payload).base64(signature)
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	encodedSig := base64.RawURLEncoding.EncodeToString(signature)
	return encodedPayload + "." + encodedSig, nil
}

// Authenticate 验证 token 并返回 Agent 身份
func (a *TokenAuthenticator) Authenticate(token string) (*AgentIdentity, error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return nil, errors.New("invalid token format")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("failed to decode payload: %w", err)
	}

	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode signature: %w", err)
	}

	// 验证签名
	mac := hmac.New(sha256.New, a.secret)
	mac.Write(payload)
	if !hmac.Equal(mac.Sum(nil), signature) {
		return nil, errors.New("invalid token signature")
	}

	var identity AgentIdentity
	if err := json.Unmarshal(payload, &identity); err != nil {
		return nil, fmt.Errorf("failed to deserialize identity: %w", err)
	}

	return &identity, nil
}

// AuthenticatedDiscovery 带认证的服务发现
type AuthenticatedDiscovery struct {
	inner Discovery
	auth  *TokenAuthenticator
	mu    sync.RWMutex
	// 存储 agentID -> token 映射，用于验证操作权限
	agentTokens map[string]string
}

// NewAuthenticatedDiscovery 创建带认证的发现服务
func NewAuthenticatedDiscovery(inner Discovery, auth *TokenAuthenticator) *AuthenticatedDiscovery {
	return &AuthenticatedDiscovery{
		inner:       inner,
		auth:        auth,
		agentTokens: make(map[string]string),
	}
}

// Register 注册 Agent（需要有效 token）
func (d *AuthenticatedDiscovery) Register(ctx context.Context, info *AgentInfo, token string) error {
	identity, err := d.auth.Authenticate(token)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	if identity.ID != info.ID {
		return errors.New("token identity does not match registration")
	}

	// 将 identity 的角色合并进 info.Capabilities，确保 ListAgentsByRole 可按角色过滤。
	// 2026-09-28 修复（分布式集成测试回归暴露）：此前仅在 Capabilities 为空时
	// 同步——同时声明 capabilities 与 roles 的 agent（最常见形态）角色被丢弃，
	// ListAgentsByRole 静默返回空。现改为去重合并，两者共存。
	if len(identity.Roles) > 0 {
		seen := make(map[string]bool, len(info.Capabilities))
		for _, c := range info.Capabilities {
			seen[c] = true
		}
		for _, r := range identity.Roles {
			if !seen[r] {
				info.Capabilities = append(info.Capabilities, r)
				seen[r] = true
			}
		}
	}

	if err := d.inner.Register(ctx, info); err != nil {
		return err
	}

	d.mu.Lock()
	d.agentTokens[info.ID] = token
	d.mu.Unlock()

	return nil
}

// Discover 发现 Agent（无需认证）
func (d *AuthenticatedDiscovery) Discover(ctx context.Context, id string) (*AgentInfo, error) {
	return d.inner.Discover(ctx, id)
}

// Unregister 注销 Agent（需要有效 token）
func (d *AuthenticatedDiscovery) Unregister(ctx context.Context, id string, token string) error {
	_, err := d.auth.Authenticate(token)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	d.mu.Lock()
	delete(d.agentTokens, id)
	d.mu.Unlock()

	return d.inner.Unregister(ctx, id)
}

// Heartbeat 发送心跳
func (d *AuthenticatedDiscovery) Heartbeat(ctx context.Context, id string) error {
	return d.inner.Heartbeat(ctx, id)
}

// ListAgents 列出所有 Agent（无需认证）
func (d *AuthenticatedDiscovery) ListAgents(ctx context.Context) ([]*AgentInfo, error) {
	return d.inner.ListAgents(ctx)
}

// ListAgentsByRole 按角色列出 Agent
func (d *AuthenticatedDiscovery) ListAgentsByRole(ctx context.Context, role string) ([]*AgentInfo, error) {
	agents, err := d.inner.ListAgents(ctx)
	if err != nil {
		return nil, err
	}

	var filtered []*AgentInfo
	for _, a := range agents {
		if hasAgentRole(a, role) {
			filtered = append(filtered, a)
		}
	}
	return filtered, nil
}

// hasAgentRole 检查 AgentInfo 是否有指定角色
func hasAgentRole(a *AgentInfo, role string) bool {
	for _, r := range a.Capabilities {
		if r == role {
			return true
		}
	}
	return false
}
