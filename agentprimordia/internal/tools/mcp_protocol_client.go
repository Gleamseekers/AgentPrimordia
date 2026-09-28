package tools

// 本文件从 mcp.go 拆分而来，包含 MCPClient 的实现和工具适配器。

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

type MCPClient struct {
	transport   mcpTransport
	serverInfo  *MCPServerInfo
	tools       []MCPToolDefinition
	mu          sync.RWMutex
	logger      *slog.Logger
	initialized bool
	toolPrefix  string // v3.9-4：工具名命名空间前缀，隔离多 server 同名工具
}

// resolveMCPCommand 解析 MCP 服务器启动命令。
// 主流 MCP server 多通过 `npx -y @modelcontextprotocol/server-xxx` 启动，
// 在 Windows 上 npx/npm 实为 .cmd 批处理，需显式补充扩展名才能被 exec 找到。
func resolveMCPCommand(command string) string {
	if command == "" {
		return command
	}
	// 已有扩展名或非裸命令名（含路径分隔符），保持不变
	lower := strings.ToLower(command)
	if strings.ContainsAny(command, `/\`) || strings.HasSuffix(lower, ".cmd") ||
		strings.HasSuffix(lower, ".exe") || strings.HasSuffix(lower, ".bat") {
		return command
	}
	// Windows 下为常见包管理器命令补充 .cmd 扩展名
	if runtime.GOOS == "windows" {
		switch lower {
		case "npx", "npm", "pnpm", "yarn", "bunx":
			if _, err := exec.LookPath(command + ".cmd"); err == nil {
				return command + ".cmd"
			}
			if _, err := exec.LookPath(command + ".exe"); err == nil {
				return command + ".exe"
			}
		}
	}
	return command
}

// SetToolPrefix 设置工具名命名空间前缀（v3.9-4）。
// 多个 MCP server 暴露同名工具（如 read_file / get_weather）时，
// 使用前缀 `<serverName>_<toolName>` 隔离，避免注册时互相覆盖。
func (c *MCPClient) SetToolPrefix(prefix string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.toolPrefix = strings.Trim(prefix, "_")
}

// NewMCPClient 创建 HTTP 模式 MCP 客户端
func NewMCPClient(baseURL string) *MCPClient {
	return &MCPClient{
		transport: newHTTPTransport(baseURL),
		logger:    slog.Default(),
	}
}

// NewMCPClientStdio 创建 stdio 模式 MCP 客户端（JSON-RPC over stdin/stdout）
func NewMCPClientStdio(stdin io.WriteCloser, stdout io.ReadCloser) *MCPClient {
	return &MCPClient{
		transport: newStdioTransport(stdin, stdout),
		logger:    slog.Default(),
	}
}

// Initialize 初始化 MCP 连接，获取服务端信息和tool列表
func (c *MCPClient) Initialize(ctx context.Context) error {
	// 1. 发送 initialize 请求
	initResp, err := c.sendRequest(ctx, "initialize", MCPInitializeRequest{
		ProtocolVersion: mcpProtocolVersion,
		Capabilities:    map[string]any{},
		ClientInfo:      MCPClientInfo{Name: "AgentPrimordia", Version: "0.1.0"},
	})
	if err != nil {
		return fmt.Errorf("MCP initialize failed: %w", err)
	}

	// 解析服务端信息
	initData, ok := initResp.Result.(map[string]any)
	if ok {
		c.mu.Lock()
		c.serverInfo = &MCPServerInfo{}
		if si, ok := initData["serverInfo"].(map[string]any); ok {
			if n, ok := si["name"].(string); ok {
				c.serverInfo.Name = n
			}
			if v, ok := si["version"].(string); ok {
				c.serverInfo.Version = v
			}
		}
		c.mu.Unlock()
	}

	// 2. 发送 initialized 通知（无需响应）
	_ = c.sendNotification(ctx, "notifications/initialized", nil)

	// 3. 获取tool列表
	toolsResp, err := c.sendRequest(ctx, "tools/list", nil)
	if err != nil {
		return fmt.Errorf("MCP tools/list failed: %w", err)
	}

	toolsData, ok := toolsResp.Result.(map[string]any)
	if ok {
		if toolsRaw, ok := toolsData["tools"].([]any); ok {
			var tools []MCPToolDefinition
			for _, t := range toolsRaw {
				if tMap, ok := t.(map[string]any); ok {
					tool := MCPToolDefinition{}
					if n, ok := tMap["name"].(string); ok {
						tool.Name = n
					}
					if d, ok := tMap["description"].(string); ok {
						tool.Description = d
					}
					if s, ok := tMap["inputSchema"].(map[string]any); ok {
						tool.InputSchema = s
					}
					tools = append(tools, tool)
				}
			}
			c.mu.Lock()
			c.tools = tools
			c.initialized = true
			c.mu.Unlock()
		}
	}

	c.logger.Info("MCP 客户端初始化完成",
		"server", c.serverInfo.Name,
		"tools", len(c.tools),
	)
	return nil
}

// Tools 返回 MCP 服务器提供的tool列表
func (c *MCPClient) Tools() []MCPToolDefinition {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.tools
}

// CallTool 调用 MCP 服务器上的tool
func (c *MCPClient) CallTool(ctx context.Context, name string, args map[string]any) (*MCPToolCallResult, error) {
	resp, err := c.sendRequest(ctx, "tools/call", MCPToolCallRequest{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		return nil, err
	}

	result := &MCPToolCallResult{}
	if resp.Error != nil {
		result.IsError = true
		result.Content = []MCPContent{{Type: "text", Text: resp.Error.Message}}
		return result, fmt.Errorf("MCP tool error: %s", resp.Error.Message)
	}

	// 解析结果
	if resultMap, ok := resp.Result.(map[string]any); ok {
		if contentRaw, ok := resultMap["content"].([]any); ok {
			for _, c := range contentRaw {
				if cMap, ok := c.(map[string]any); ok {
					content := MCPContent{}
					if t, ok := cMap["type"].(string); ok {
						content.Type = t
					}
					if t, ok := cMap["text"].(string); ok {
						content.Text = t
					}
					result.Content = append(result.Content, content)
				}
			}
		}
		if isError, ok := resultMap["isError"].(bool); ok {
			result.IsError = isError
		}
	}

	return result, nil
}

// RegisterIntoRegistry 将 MCP tool注册到 Tool Registry
func (c *MCPClient) RegisterIntoRegistry(registry *Registry) error {
	c.mu.RLock()
	tools := c.tools
	prefix := c.toolPrefix
	c.mu.RUnlock()

	for _, mcpTool := range tools {
		tool := &mcpToolAdapter{
			client: c,
			def:    mcpTool,
			prefix: prefix,
		}
		if err := registry.Register(tool); err != nil {
			return fmt.Errorf("register MCP tool %q: %w", mcpTool.Name, err)
		}
	}
	c.logger.Info("MCP tool已注册到 Registry", "count", len(tools))
	return nil
}

// Close 关闭 MCP 客户端
func (c *MCPClient) Close() error {
	return c.transport.Close()
}

// ===== 内部方法 =====

func (c *MCPClient) sendRequest(ctx context.Context, method string, params any) (*MCPResponse, error) {
	req := MCPRequest{
		JSONRPC: "2.0",
		ID:      nextMCPID(),
		Method:  method,
		Params:  params,
	}

	resp, err := c.transport.SendRequest(ctx, &req)
	if err != nil {
		return nil, fmt.Errorf("MCP request %q failed: %w", method, err)
	}
	return resp, nil
}

func (c *MCPClient) sendNotification(ctx context.Context, method string, params any) error {
	req := MCPRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}

	return c.transport.SendNotification(ctx, &req)
}

// ===== MCP Tool Adapter =====

// mcpToolAdapter 将 MCP tool适配为 Tool 接口
type mcpToolAdapter struct {
	client *MCPClient
	def    MCPToolDefinition
	prefix string // v3.9-4：命名空间前缀，为空时保持向后兼容
}

func (t *mcpToolAdapter) Name() string {
	if t.prefix == "" {
		return t.def.Name
	}
	return t.prefix + "_" + t.def.Name
}

func (t *mcpToolAdapter) Description() string {
	return t.def.Description
}

func (t *mcpToolAdapter) Parameters() json.RawMessage {
	if t.def.InputSchema != nil {
		raw, _ := json.Marshal(t.def.InputSchema)
		return raw
	}
	return json.RawMessage(`{"type":"object","properties":{}}`)
}

func (t *mcpToolAdapter) Execute(ctx context.Context, args json.RawMessage) (*Result, error) {
	var argsMap map[string]any
	if err := json.Unmarshal(args, &argsMap); err != nil {
		argsMap = make(map[string]any)
	}

	mcpResult, err := t.client.CallTool(ctx, t.def.Name, argsMap)
	if err != nil {
		return NewErrorResult(err.Error()), err
	}

	// 合并所有文本内容，并限制总大小
	var textParts []string
	totalLen := 0
	for _, content := range mcpResult.Content {
		if content.Type == "text" && content.Text != "" {
			text := content.Text
			remaining := mcpMaxToolResultLen - totalLen
			if remaining <= 0 {
				break
			}
			if len(text) > remaining {
				text = text[:remaining] + "\n... [MCP 结果已截断]"
			}
			textParts = append(textParts, text)
			totalLen += len(text)
		}
	}

	resultText := strings.Join(textParts, "\n")
	if mcpResult.IsError {
		return NewErrorResult(resultText), fmt.Errorf("MCP tool %q returned error", t.def.Name)
	}
	return NewResult(resultText), nil
}

// ===== MCP Server =====

// MCPResourceDefinition 描述 MCP 服务端提供的资源
type MCPResourceDefinition struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// MCPResourceContent MCP 资源内容
type MCPResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     []byte `json:"blob,omitempty"`
}

// MCPPromptDefinition 描述 MCP 服务端提供的提示词模板
type MCPPromptDefinition struct {
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	Arguments   []MCPPromptArgument `json:"arguments,omitempty"`
}

// MCPPromptArgument 提示词模板参数
type MCPPromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// MCPPromptMessage 提示词消息
type MCPPromptMessage struct {
	Role    string `json:"role"`
	Content struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// MCPServerConfig MCP 服务端配置
type MCPServerConfig struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	APIKey  string `json:"api_key,omitempty"`
}

// MCPServer 让 AP 自身作为 MCP 服务端，暴露tool、资源和提示词
type MCPServer struct {
	config    MCPServerConfig
	registry  *Registry
	executor  *Executor
	resources []MCPResourceDefinition
	prompts   []MCPPromptDefinition

	resourceHandler func(ctx context.Context, uri string) (*MCPResourceContent, error)
	promptHandler   func(ctx context.Context, name string, args map[string]string) ([]MCPPromptMessage, error)

	mu     sync.RWMutex
	logger *slog.Logger
}

// NewMCPServer 创建 MCP 服务端。
//
// 安全警示（P1）：默认执行器（NewExecutor）**不含 ScopePolicy**——
// tools/call 可调用 Registry 中任意已注册工具（含 shell 等危险工具），
// 且默认无认证（config.APIKey 为空时）。暴露到网络前必须：
//  1. 设置 config.APIKey 启用 Bearer 认证；
//  2. 经 SetScopePolicy 或 SetExecutor 注入权限策略/沙箱校验，
//     收敛 tools/call 可触及的工具与资源面。
func NewMCPServer(config MCPServerConfig, registry *Registry) *MCPServer {
	return &MCPServer{
		config:   config,
		registry: registry,
		executor: NewExecutor(registry),
		logger:   slog.Default(),
	}
}

// SetExecutor 设置自定义 Executor，用于覆盖默认的执行器
// 通过自定义 Executor 可以注入 ScopePolicy、Permission 等安全机制。
// 安全警示（P1）：tools/call 经执行器可调任意已注册工具（含 shell）；
// 面向不可信客户端暴露时，必须经此方法（或 SetScopePolicy）注入权限策略。
func (s *MCPServer) SetExecutor(executor *Executor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.executor = executor
}

// SetScopePolicy 为当前 Executor 注入 ScopePolicy（P1 安全加固入口）。
// tools/call 的默认执行器无任何权限策略——可调 Registry 中任意工具；
// 注入策略后，执行前按参数中的路径字段做 agentID 维度的 Allow 校验，
// 拒绝即返回 isError 结果。agentID 标识当前 Agent（策略主体）。
func (s *MCPServer) SetScopePolicy(policy ScopePolicy, agentID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.executor == nil {
		return
	}
	s.executor.WithScopePolicy(policy, agentID)
}

// AddResource 添加 MCP 资源定义
func (s *MCPServer) AddResource(res MCPResourceDefinition) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resources = append(s.resources, res)
}

// AddPrompt 添加 MCP 提示词模板
func (s *MCPServer) AddPrompt(prompt MCPPromptDefinition) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prompts = append(s.prompts, prompt)
}

// SetResourceHandler 设置资源读取处理器
func (s *MCPServer) SetResourceHandler(handler func(ctx context.Context, uri string) (*MCPResourceContent, error)) {
	s.resourceHandler = handler
}

// SetPromptHandler 设置提示词获取处理器
func (s *MCPServer) SetPromptHandler(handler func(ctx context.Context, name string, args map[string]string) ([]MCPPromptMessage, error)) {
	s.promptHandler = handler
}

// ServeHTTP 实现 http.Handler，处理 MCP JSON-RPC 请求
func (s *MCPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// API Key 认证检查（P1 修复：crypto/subtle 常量时间比较防 timing
	// attack；强制 "Bearer " 方案前缀——裸 key 直接作为 Authorization
	// 值不得放行）
	if s.config.APIKey != "" {
		if !bearerAuthorized(r.Header.Get("Authorization"), s.config.APIKey) {
			writeMCPError(w, 0, -32001, "unauthorized: missing or invalid Bearer token")
			return
		}
	}

	// 限制请求体大小为 1MB
	r.Body = http.MaxBytesReader(w, r.Body, mcpMaxRequestBody)

	var req MCPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeMCPError(w, 0, -32700, "parse error")
		return
	}

	if req.JSONRPC != "2.0" {
		writeMCPError(w, req.ID, -32600, "invalid request: jsonrpc must be \"2.0\"")
		return
	}

	var resp *MCPResponse
	switch req.Method {
	case "initialize":
		resp = s.handleInitialize(req)
	case "notifications/initialized":
		w.WriteHeader(http.StatusOK)
		return
	case "tools/list":
		resp = s.handleToolsList(req)
	case "tools/call":
		resp = s.handleToolsCall(r.Context(), req)
	case "resources/list":
		resp = s.handleResourcesList(req)
	case "resources/read":
		resp = s.handleResourcesRead(r.Context(), req)
	case "prompts/list":
		resp = s.handlePromptsList(req)
	case "prompts/get":
		resp = s.handlePromptsGet(r.Context(), req)
	case "ping":
		resp = &MCPResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}
	default:
		resp = &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &MCPError{Code: -32601, Message: "method not found"},
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *MCPServer) handleInitialize(req MCPRequest) *MCPResponse {
	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: MCPInitializeResponse{
			ProtocolVersion: mcpProtocolVersion,
			Capabilities: map[string]any{
				"tools":     map[string]any{"listChanged": true},
				"resources": map[string]any{"subscribe": true, "listChanged": true},
				"prompts":   map[string]any{"listChanged": true},
			},
			ServerInfo: MCPServerInfo{
				Name:    s.config.Name,
				Version: s.config.Version,
			},
		},
	}
}

func (s *MCPServer) handleToolsList(req MCPRequest) *MCPResponse {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var tools []MCPToolDefinition
	if s.registry != nil {
		for _, name := range s.registry.List() {
			if tool, ok := s.registry.Get(name); ok {
				tools = append(tools, MCPToolDefinition{
					Name:        tool.Name(),
					Description: tool.Description(),
					InputSchema: schemaFromRaw(tool.Parameters()),
				})
			}
		}
	}

	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  MCPListToolsResponse{Tools: tools},
	}
}

func (s *MCPServer) handleToolsCall(ctx context.Context, req MCPRequest) *MCPResponse {
	// 安全警示（P1）：本方法经（默认无 ScopePolicy 的）执行器调用
	// Registry 中任意工具——含 shell 等宿主任意代码执行工具。
	// 面向不可信客户端暴露 MCPServer 时，必须经 SetScopePolicy /
	// SetExecutor 注入权限策略收敛暴露面（见 NewMCPServer 文档）。
	params, ok := req.Params.(map[string]any)
	if !ok {
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &MCPError{Code: -32602, Message: "invalid params"},
		}
	}

	toolName, _ := params["name"].(string)
	argsRaw, _ := params["arguments"].(map[string]any)
	argsJSON, _ := json.Marshal(argsRaw)

	if s.registry == nil {
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &MCPError{Code: -32602, Message: "no tool registry"},
		}
	}

	_, ok = s.registry.Get(toolName)
	if !ok {
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &MCPError{Code: -32602, Message: fmt.Sprintf("tool %q not found", toolName)},
		}
	}

	s.mu.RLock()
	executor := s.executor
	s.mu.RUnlock()

	fc := &FunctionCall{
		Name: toolName,
		Args: string(argsJSON),
	}
	result, err := executor.Execute(ctx, fc)
	isError := err != nil
	text := ""
	if result != nil {
		text = result.Content
	}

	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: MCPToolCallResult{
			Content: []MCPContent{{Type: "text", Text: text}},
			IsError: isError,
		},
	}
}

func (s *MCPServer) handleResourcesList(req MCPRequest) *MCPResponse {
	s.mu.RLock()
	resources := make([]MCPResourceDefinition, len(s.resources))
	copy(resources, s.resources)
	s.mu.RUnlock()

	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  map[string]any{"resources": resources},
	}
}

func (s *MCPServer) handleResourcesRead(ctx context.Context, req MCPRequest) *MCPResponse {
	if s.resourceHandler == nil {
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &MCPError{Code: -32601, Message: "resources not supported"},
		}
	}

	params, ok := req.Params.(map[string]any)
	if !ok {
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &MCPError{Code: -32602, Message: "invalid params"},
		}
	}

	uri, _ := params["uri"].(string)
	content, err := s.resourceHandler(ctx, uri)
	if err != nil {
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &MCPError{Code: -32603, Message: err.Error()},
		}
	}

	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  map[string]any{"contents": []MCPResourceContent{*content}},
	}
}

func (s *MCPServer) handlePromptsList(req MCPRequest) *MCPResponse {
	s.mu.RLock()
	prompts := make([]MCPPromptDefinition, len(s.prompts))
	copy(prompts, s.prompts)
	s.mu.RUnlock()

	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  map[string]any{"prompts": prompts},
	}
}

func (s *MCPServer) handlePromptsGet(ctx context.Context, req MCPRequest) *MCPResponse {
	if s.promptHandler == nil {
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &MCPError{Code: -32601, Message: "prompts not supported"},
		}
	}

	params, ok := req.Params.(map[string]any)
	if !ok {
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &MCPError{Code: -32602, Message: "invalid params"},
		}
	}

	name, _ := params["name"].(string)
	argsRaw, _ := params["arguments"].(map[string]any)
	args := make(map[string]string, len(argsRaw))
	for k, v := range argsRaw {
		if s, ok := v.(string); ok {
			args[k] = s
		}
	}

	messages, err := s.promptHandler(ctx, name, args)
	if err != nil {
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &MCPError{Code: -32603, Message: err.Error()},
		}
	}

	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  map[string]any{"messages": messages},
	}
}

func schemaFromRaw(raw json.RawMessage) map[string]any {
	if raw == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return schema
}

func writeMCPError(w http.ResponseWriter, id int, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(MCPResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &MCPError{Code: code, Message: message},
	})
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
