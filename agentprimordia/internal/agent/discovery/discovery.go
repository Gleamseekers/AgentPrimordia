// Package discovery 提供 Agent 服务发现功能
package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// AgentInfo 表示 Agent 的注册信息
type AgentInfo struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Address      string            `json:"address"`
	Capabilities []string          `json:"capabilities,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	LastSeen     time.Time         `json:"last_seen"`
}

// Discovery 服务发现接口
type Discovery interface {
	// Register 注册 Agent
	Register(ctx context.Context, info *AgentInfo) error
	// Unregister 注销 Agent
	Unregister(ctx context.Context, agentID string) error
	// Discover 发现指定 Agent
	Discover(ctx context.Context, agentID string) (*AgentInfo, error)
	// ListAgents 列出所有 Agent
	ListAgents(ctx context.Context) ([]*AgentInfo, error)
	// Heartbeat 发送心跳
	Heartbeat(ctx context.Context, agentID string) error
	// Close 关闭服务
	Close() error
}

// LocalDiscovery 本地内存实现
type LocalDiscovery struct {
	mu     sync.RWMutex
	agents map[string]*AgentInfo
	logger *slog.Logger
}

// NewLocalDiscovery 创建本地发现服务
func NewLocalDiscovery() *LocalDiscovery {
	return &LocalDiscovery{
		agents: make(map[string]*AgentInfo),
		logger: slog.Default(),
	}
}

// Register 注册 Agent
func (d *LocalDiscovery) Register(ctx context.Context, info *AgentInfo) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	info.LastSeen = time.Now()
	d.agents[info.ID] = info
	d.logger.Info("Agent registered", "id", info.ID, "name", info.Name)
	return nil
}

// Unregister 注销 Agent
func (d *LocalDiscovery) Unregister(ctx context.Context, agentID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	delete(d.agents, agentID)
	d.logger.Info("Agent unregistered", "id", agentID)
	return nil
}

// Discover 发现指定 Agent
func (d *LocalDiscovery) Discover(ctx context.Context, agentID string) (*AgentInfo, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	info, ok := d.agents[agentID]
	if !ok {
		return nil, fmt.Errorf("agent not found: %s", agentID)
	}
	// 返回副本，避免外部修改影响内部存储
	cp := *info
	return &cp, nil
}

// ListAgents 列出所有 Agent
func (d *LocalDiscovery) ListAgents(ctx context.Context) ([]*AgentInfo, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	result := make([]*AgentInfo, 0, len(d.agents))
	for _, info := range d.agents {
		result = append(result, info)
	}
	return result, nil
}

// Heartbeat 发送心跳
func (d *LocalDiscovery) Heartbeat(ctx context.Context, agentID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	info, ok := d.agents[agentID]
	if !ok {
		return fmt.Errorf("agent not found: %s", agentID)
	}
	info.LastSeen = time.Now()
	return nil
}

// Close 关闭服务
func (d *LocalDiscovery) Close() error {
	return nil
}

// HTTPDiscoveryClient HTTP 客户端实现
type HTTPDiscoveryClient struct {
	baseURL string
	client  *http.Client
}

// NewHTTPDiscoveryClient 创建 HTTP 发现客户端
func NewHTTPDiscoveryClient(baseURL string) *HTTPDiscoveryClient {
	return &HTTPDiscoveryClient{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

// Register 注册 Agent
func (c *HTTPDiscoveryClient) Register(ctx context.Context, info *AgentInfo) error {
	body, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("failed to serialize AgentInfo: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/discovery/register", strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("registration request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("registration failed, status code: %d", resp.StatusCode)
	}
	return nil
}

// Unregister 注销 Agent
func (c *HTTPDiscoveryClient) Unregister(ctx context.Context, agentID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/api/discovery/"+agentID, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("deregistration request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("deregistration failed, status code: %d", resp.StatusCode)
	}
	return nil
}

// Discover 发现指定 Agent
func (c *HTTPDiscoveryClient) Discover(ctx context.Context, agentID string) (*AgentInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/discovery/"+agentID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("discovery request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("agent not found: %s", agentID)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discovery failed, status code: %d", resp.StatusCode)
	}

	var info AgentInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return &info, nil
}

// ListAgents 列出所有 Agent
func (c *HTTPDiscoveryClient) ListAgents(ctx context.Context) ([]*AgentInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/discovery/agents", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list request failed, status code: %d", resp.StatusCode)
	}

	var agents []*AgentInfo
	if err := json.NewDecoder(resp.Body).Decode(&agents); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return agents, nil
}

// Heartbeat 发送心跳
func (c *HTTPDiscoveryClient) Heartbeat(ctx context.Context, agentID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/discovery/"+agentID+"/heartbeat", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("heartbeat request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("heartbeat failed, status code: %d", resp.StatusCode)
	}
	return nil
}

// Close 关闭服务
func (c *HTTPDiscoveryClient) Close() error {
	return nil
}

// DiscoveryServer 发现服务 HTTP 服务器
type DiscoveryServer struct {
	discovery Discovery
	server    *http.Server
	logger    *slog.Logger
	apiKey    string

	// mu 保护 realAddr（Start 的 goroutine 写，Addr 的调用方读）
	mu       sync.RWMutex
	realAddr string // Start 后记录的真实绑定地址（"host:0" 场景）
}

// NewDiscoveryServer 创建发现服务服务器
func NewDiscoveryServer(discovery Discovery, addr string) *DiscoveryServer {
	s := &DiscoveryServer{
		discovery: discovery,
		logger:    slog.Default(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/discovery/register", s.handleRegister)
	mux.HandleFunc("/api/discovery/agents", s.handleList)
	mux.HandleFunc("/api/discovery/", s.handleAgentByID)

	s.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	return s
}

// WithAPIKey 设置 API Key 认证（写操作需要 Bearer token）
func (s *DiscoveryServer) WithAPIKey(key string) *DiscoveryServer {
	s.apiKey = key
	return s
}

// Handler 返回 HTTP handler（用于测试）
func (s *DiscoveryServer) Handler() http.Handler {
	return s.server.Handler
}

// requireAuth 检查 Bearer token 认证，返回 true 表示未通过（已写响应）
func (s *DiscoveryServer) requireAuth(w http.ResponseWriter, r *http.Request) bool {
	if s.apiKey == "" {
		return false
	}
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") || auth[7:] != s.apiKey {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return true
	}
	return false
}

// Start 启动服务器。
// 使用显式 listener 而非 ListenAndServe：addr 为 "host:0" 时 Addr() 可
// 返回真实绑定端口（ListenAndServe 不暴露 listener，Addr() 只能返回配置值
// "127.0.0.1:0"，调用方无法发现服务）。
func (s *DiscoveryServer) Start() error {
	ln, err := net.Listen("tcp", s.server.Addr)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.realAddr = ln.Addr().String()
	s.mu.Unlock()
	s.logger.Info("Discovery server starting", "addr", s.realAddr)
	return s.server.Serve(ln)
}

// Addr 返回服务器实际监听地址。
// Start 之前返回配置的地址；Start 之后（含 "host:0" 场景）返回真实绑定地址。
func (s *DiscoveryServer) Addr() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.realAddr != "" {
		return s.realAddr
	}
	return s.server.Addr
}

// Stop 停止服务器
func (s *DiscoveryServer) Stop(ctx context.Context) error {
	s.logger.Info("Discovery server stopping")
	return s.server.Shutdown(ctx)
}

// Close 优雅关闭服务器（便捷方法，使用默认超时）
func (s *DiscoveryServer) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.Stop(ctx)
}

func (s *DiscoveryServer) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 写操作需要认证
	if s.requireAuth(w, r) {
		return
	}

	// 限制请求体大小（1MB）
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var info AgentInfo
	if err := json.NewDecoder(r.Body).Decode(&info); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// 验证必填字段
	if len(info.ID) < 1 || len(info.ID) > 256 {
		http.Error(w, "agent id must be between 1 and 256 characters", http.StatusBadRequest)
		return
	}
	if len(info.Name) < 1 || len(info.Name) > 256 {
		http.Error(w, "agent name must be between 1 and 256 characters", http.StatusBadRequest)
		return
	}
	if len(info.Address) > 1024 {
		http.Error(w, "agent address must not exceed 1024 characters", http.StatusBadRequest)
		return
	}

	if err := s.discovery.Register(r.Context(), &info); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// handleAgentByID 路由到具体的 Agent 操作（discover/unregister/heartbeat）
func (s *DiscoveryServer) handleAgentByID(w http.ResponseWriter, r *http.Request) {
	// 解析路径：/api/discovery/{id}[/action]
	path := strings.TrimPrefix(r.URL.Path, "/api/discovery/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "missing agent id", http.StatusBadRequest)
		return
	}

	agentID := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch {
	case action == "" && r.Method == http.MethodGet:
		// GET /api/discovery/{id} - discover
		s.handleDiscoverByID(w, r, agentID)
	case action == "" && r.Method == http.MethodDelete:
		// DELETE /api/discovery/{id} - unregister
		s.handleUnregisterByID(w, r, agentID)
	case action == "heartbeat" && r.Method == http.MethodPost:
		// POST /api/discovery/{id}/heartbeat
		s.handleHeartbeatByID(w, r, agentID)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func (s *DiscoveryServer) handleDiscoverByID(w http.ResponseWriter, r *http.Request, agentID string) {
	info, err := s.discovery.Discover(r.Context(), agentID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(info)
}

func (s *DiscoveryServer) handleUnregisterByID(w http.ResponseWriter, r *http.Request, agentID string) {
	// 写操作需要认证
	if s.requireAuth(w, r) {
		return
	}

	if err := s.discovery.Unregister(r.Context(), agentID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *DiscoveryServer) handleHeartbeatByID(w http.ResponseWriter, r *http.Request, agentID string) {
	// 写操作需要认证
	if s.requireAuth(w, r) {
		return
	}

	if err := s.discovery.Heartbeat(r.Context(), agentID); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *DiscoveryServer) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	agents, err := s.discovery.ListAgents(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(agents)
}
