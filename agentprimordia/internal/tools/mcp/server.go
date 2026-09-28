package mcp

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ToolHandler exposes an Agent function as an MCP tool.
type ToolHandler func(ctx context.Context, args map[string]any) (content string, isErr bool, err error)

// mcpServerProtocolVersion is the MCP protocol version this server advertises.
const mcpServerProtocolVersion = "2024-11-05"

// serverToolEntry is an internal exposed-tool record.
type serverToolEntry struct {
	name        string
	description string
	schema      map[string]any
	handler     ToolHandler
}

// AgentMCPServer exposes Agent capabilities (tools, resources, prompts)
// via the Model Context Protocol over HTTP JSON-RPC.
type AgentMCPServer interface {
	RegisterTool(name, description string, handler ToolHandler)
	RegisterResource(uri, name string, mimeType string, handler ResourceHandler)
	RegisterPrompt(name string, handler PromptHandler)
	Start(ctx context.Context, addr string) error
	Stop() error
	NotifyToolListChanged() error
}

// MCPServer is the concrete default implementation of AgentMCPServer.
type MCPServer struct {
	tools            *serverToolRegistry
	resources        *resourceRegistry
	prompts          *promptRegistry
	notifier         *toolListChangedNotifier
	server           *http.Server
	mu               sync.Mutex
	logger           *slog.Logger
	builtinResources *BuiltinResources
	apiKey           string // P1 安全修复：Bearer 认证密钥，空 = 不认证（兼容既有部署）
}

// MCPServerOption configures an MCPServer.
type MCPServerOption func(*MCPServer)

// WithLogger sets the logger for the server.
func WithLogger(l *slog.Logger) MCPServerOption {
	return func(s *MCPServer) { s.logger = l }
}

// WithAPIKey 启用 Bearer 认证（P1 安全修复）。
// 设置后所有 /mcp 请求必须携带 `Authorization: Bearer <key>`，
// 常量时间比较（crypto/subtle）防 timing attack。
// 不设置时保持默认行为（无认证）——兼容既有内网/本地部署。
func WithAPIKey(key string) MCPServerOption {
	return func(s *MCPServer) { s.apiKey = key }
}

// WithBuiltinResources installs default agent-memory / session / status resources.
func WithBuiltinResources(br *BuiltinResources) MCPServerOption {
	return func(s *MCPServer) { s.builtinResources = br }
}

// WithBuiltinPrompts installs the three built-in prompts (summarize, analyze, plan).
func WithBuiltinPrompts() MCPServerOption {
	return func(s *MCPServer) { NewBuiltinPrompts().RegisterTo(s.prompts) }
}

// NewMCPServer creates an empty MCPServer.
func NewMCPServer(opts ...MCPServerOption) *MCPServer {
	s := &MCPServer{
		tools:     newServerToolRegistry(),
		resources: newResourceRegistry(),
		prompts:   newPromptRegistry(),
		notifier:  newToolListChangedNotifier(),
		logger:    slog.Default(),
	}
	for _, opt := range opts {
		opt(s)
	}
	// Install built-in resources and patterns after options applied.
	if s.builtinResources != nil {
		s.installBuiltinResources()
	}
	return s
}

// serverToolRegistry exposes Agent tools as MCP tools.
type serverToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]*serverToolEntry
	order []string
}

func newServerToolRegistry() *serverToolRegistry {
	return &serverToolRegistry{tools: make(map[string]*serverToolEntry)}
}

func (r *serverToolRegistry) Register(name, description string, schema map[string]any, handler ToolHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[name]; !exists {
		r.order = append(r.order, name)
	}
	r.tools[name] = &serverToolEntry{name: name, description: description, schema: schema, handler: handler}
}

func (r *serverToolRegistry) Unregister(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tools[name]; !ok {
		return false
	}
	delete(r.tools, name)
	for i, n := range r.order {
		if n == name {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
	return true
}

func (r *serverToolRegistry) List() []ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]ToolDefinition, 0, len(r.order))
	for _, name := range r.order {
		entry := r.tools[name]
		result = append(result, ToolDefinition{
			Name:        entry.name,
			Description: entry.description,
			InputSchema: entry.schema,
		})
	}
	return result
}

func (r *serverToolRegistry) Get(name string) (*serverToolEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.tools[name]
	return entry, ok
}

func (r *serverToolRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}

// RegisterTool implements AgentMCPServer.
func (s *MCPServer) RegisterTool(name, description string, handler ToolHandler) {
	schema := map[string]any{"type": "object", "properties": map[string]any{}}
	s.tools.Register(name, description, schema, handler)
	s.notifier.Notify()
}

// RegisterResource implements AgentMCPServer.
func (s *MCPServer) RegisterResource(uri, name string, mimeType string, handler ResourceHandler) {
	s.resources.Register(uri, name, mimeType, handler)
}

// RegisterPrompt implements AgentMCPServer.
func (s *MCPServer) RegisterPrompt(name string, handler PromptHandler) {
	s.prompts.Register(name, handler)
}

// NotifyToolListChanged sends a tools/list_changed notification.
func (s *MCPServer) NotifyToolListChanged() error {
	s.notifier.Notify()
	return nil
}

// Start launches the HTTP server on the given address.
func (s *MCPServer) Start(ctx context.Context, addr string) error {
	s.mu.Lock()
	if s.server != nil {
		s.mu.Unlock()
		return fmt.Errorf("MCP server already started")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", s.handleMCP)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	})
	s.server = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	s.mu.Unlock()

	errCh := make(chan error, 1)
	go func() { errCh <- s.server.ListenAndServe() }()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Stop gracefully shuts down the HTTP server.
func (s *MCPServer) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server == nil {
		return nil
	}
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := s.server.Shutdown(shutCtx)
	s.server = nil
	return err
}

// Handler returns the HTTP handler for embedding into an existing mux.
func (s *MCPServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", s.handleMCP)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// bearerAuthorized 常量时间校验 Bearer 令牌（P1 安全修复）。
// 双重加固：
//  1. 强制 Authorization 采用 "Bearer " 方案前缀——裸 key 直接当头部值
//     不得放行（旧式 strings.TrimPrefix 不校验前缀会放行裸 key）；
//  2. crypto/subtle.ConstantTimeCompare 防 timing attack。
func bearerAuthorized(authHeader, apiKey string) bool {
	if apiKey == "" { // fail-closed：空 key 一律不认证通过
		return false
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(authHeader, prefix) {
		return false
	}
	token := authHeader[len(prefix):]
	return subtle.ConstantTimeCompare([]byte(token), []byte(apiKey)) == 1
}

// handleMCP dispatches a single JSON-RPC request and writes the response.
func (s *MCPServer) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed, use POST"})
		return
	}
	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]any{"error": "unsupported content type"})
		return
	}
	// P1 安全修复：Bearer 认证（设置 WithAPIKey 后强制；常量时间比较）
	if s.apiKey != "" && !bearerAuthorized(r.Header.Get("Authorization"), s.apiKey) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized: missing or invalid Bearer token"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "read body failed: " + err.Error()})
		return
	}
	var req jsonRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, jsonRPCError{Code: -32700, Message: "invalid JSON-RPC request"})
		return
	}
	resp := s.dispatch(r.Context(), &req)
	if resp != nil {
		writeJSON(w, http.StatusOK, resp)
	}
}

// dispatch routes the JSON-RPC method to the appropriate handler.
func (s *MCPServer) dispatch(ctx context.Context, req *jsonRPCRequest) *jsonRPCResponse {
	if req.ID == 0 {
		_ = s.handleNotification(ctx, req)
		return nil
	}
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "initialized":
		return &jsonRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: json.RawMessage("{}")}
	case "ping":
		return &jsonRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: json.RawMessage("{}")}
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolCall(ctx, req)
	case "resources/list":
		return s.handleResourcesList(req)
	case "resources/read":
		return s.handleResourceRead(ctx, req)
	case "prompts/list":
		return s.handlePromptsList(req)
	case "prompts/get":
		return s.handlePromptGet(ctx, req)
	default:
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &jsonRPCError{Code: -32601, Message: fmt.Sprintf("method not found: %s", req.Method)},
		}
	}
}

func (s *MCPServer) handleNotification(ctx context.Context, req *jsonRPCRequest) error {
	switch req.Method {
	case "notifications/initialized":
		s.logger.Debug("MCP client initialized")
	case "notifications/cancelled":
		s.logger.Debug("MCP client cancelled")
	default:
		return fmt.Errorf("unknown notification: %s", req.Method)
	}
	return nil
}

func (s *MCPServer) handleInitialize(req *jsonRPCRequest) *jsonRPCResponse {
	result := initializeResult{
		ProtocolVersion: mcpServerProtocolVersion,
		Capabilities: map[string]any{
			"tools":     map[string]any{"listChanged": true},
			"resources": map[string]any{"subscribe": false, "listChanged": false},
			"prompts":   map[string]any{"listChanged": false},
		},
		ServerInfo: ServerInfo{Name: "AgentPrimordia-MCP", Version: "0.1.0"},
	}
	raw, _ := json.Marshal(result)
	return &jsonRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: raw}
}

func (s *MCPServer) handleToolsList(req *jsonRPCRequest) *jsonRPCResponse {
	tools := s.tools.List()
	raw, _ := json.Marshal(map[string]any{"tools": tools})
	return &jsonRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: raw}
}

func (s *MCPServer) handleToolCall(ctx context.Context, req *jsonRPCRequest) *jsonRPCResponse {
	params := callToolParams{}
	paramsRaw, _ := json.Marshal(req.Params)
	_ = json.Unmarshal(paramsRaw, &params)

	entry, ok := s.tools.Get(params.Name)
	if !ok {
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &jsonRPCError{Code: -32602, Message: fmt.Sprintf("tool not found: %s", params.Name)},
		}
	}
	content, isErr, err := entry.handler(ctx, params.Arguments)
	if err != nil {
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  mustMarshalContent(err.Error(), true),
		}
	}
	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  mustMarshalContent(content, isErr),
	}
}

func mustMarshalContent(text string, isErr bool) json.RawMessage {
	b, _ := json.Marshal(map[string]any{
		"content": []ContentBlock{{Type: "text", Text: text}},
		"isError": isErr,
	})
	return b
}

func (s *MCPServer) handleResourcesList(req *jsonRPCRequest) *jsonRPCResponse {
	resources := s.resources.List()
	raw, _ := json.Marshal(map[string]any{"resources": resources})
	return &jsonRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: raw}
}

func (s *MCPServer) handleResourceRead(ctx context.Context, req *jsonRPCRequest) *jsonRPCResponse {
	params := readResourceParams{}
	paramsRaw, _ := json.Marshal(req.Params)
	_ = json.Unmarshal(paramsRaw, &params)

	// Try direct lookup first.
	rc, err := s.resources.Read(ctx, params.URI)
	if err != nil {
		// Fall back to session pattern match.
		if strings.HasPrefix(params.URI, "agent://session/") {
			handler := sessionResourceHandler(s.builtinResources.getSession)
			data, err := handler(ctx, params.URI)
			if err != nil {
				return &jsonRPCResponse{
					JSONRPC: "2.0",
					ID:      req.ID,
					Error:   &jsonRPCError{Code: -32000, Message: err.Error()},
				}
			}
			raw, _ := json.Marshal(map[string]any{
				"contents": []ResourceContent{{URI: params.URI, MimeType: "application/json", Text: string(data)}},
			})
			return &jsonRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: raw}
		}
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &jsonRPCError{Code: -32000, Message: err.Error()},
		}
	}
	raw, _ := json.Marshal(map[string]any{"contents": []ResourceContent{*rc}})
	return &jsonRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: raw}
}

func (s *MCPServer) handlePromptsList(req *jsonRPCRequest) *jsonRPCResponse {
	prompts := s.prompts.List()
	raw, _ := json.Marshal(map[string]any{"prompts": prompts})
	return &jsonRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: raw}
}

func (s *MCPServer) handlePromptGet(ctx context.Context, req *jsonRPCRequest) *jsonRPCResponse {
	params := getPromptParams{}
	paramsRaw, _ := json.Marshal(req.Params)
	_ = json.Unmarshal(paramsRaw, &params)

	text, err := s.prompts.Get(ctx, params.Name, toStringAnyMap(params.Arguments))
	if err != nil {
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &jsonRPCError{Code: -32000, Message: err.Error()},
		}
	}
	raw, _ := json.Marshal(map[string]any{
		"messages": []PromptMessage{{Role: "user", Content: ContentBlock{Type: "text", Text: text}}},
	})
	return &jsonRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: raw}
}

// toStringAnyMap coerces map[string]any to the string-keyed map
// expected by PromptHandler.
func toStringAnyMap(in map[string]any) map[string]string {
	if in == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		if s, ok := v.(string); ok {
			out[k] = s
		} else {
			out[k] = fmt.Sprintf("%v", v)
		}
	}
	return out
}

// Helper to ensure unused imports are actually referenced.
var _ = strings.HasPrefix

// installBuiltinResources registers built-in session / status / memory resources.
func (s *MCPServer) installBuiltinResources() {
	s.resources.Register("agent://memory", "Agent Memory Snapshot", "application/json",
		func(ctx context.Context, uri string) ([]byte, error) {
			return s.builtinResources.getMemorySnapshot(ctx)
		})
	s.resources.Register("agent://status", "Agent Status", "application/json",
		func(ctx context.Context, uri string) ([]byte, error) { return s.builtinResources.getStatus(ctx) })
	s.resources.Register("agent://session/", "Agent Session State", "application/json",
		sessionResourceHandler(s.builtinResources.getSession))
}
