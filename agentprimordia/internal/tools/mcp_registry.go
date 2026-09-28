package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// MCPClientConfig 描述一个外部 MCP Server 的连接配置（从 AP 客户端视角）
type MCPClientConfig struct {
	Name       string            `json:"name"`                 // 服务器名称
	Command    string            `json:"command"`              // 启动命令 (如 "npx")
	Args       []string          `json:"args"`                 // 命令参数
	Env        map[string]string `json:"env,omitempty"`        // 环境变量
	AutoStart  bool              `json:"autoStart,omitempty"`  // Agent 启动时自动拉起
	BaseURL    string            `json:"baseUrl,omitempty"`    // 已运行 Server 的 URL（跳过启动）
	ToolPrefix string            `json:"toolPrefix,omitempty"` // v3.9-4：工具名命名空间前缀，隔离多 server 同名工具
}

// MCPClientStatus 描述 MCP Server 的运行状态
type MCPClientStatus string

const (
	MCPClientStopped  MCPClientStatus = "stopped"
	MCPClientStarting MCPClientStatus = "starting"
	MCPClientRunning  MCPClientStatus = "running"
	MCPClientFailed   MCPClientStatus = "failed"
)

// MCPClientEntry 注册的 MCP Server 条目
type MCPClientEntry struct {
	Config MCPClientConfig
	Status MCPClientStatus
	Client *MCPClient
	Cmd    *exec.Cmd
	Tools  []MCPToolDefinition
	Stdin  io.WriteCloser // stdio 模式下的 stdin 管道
	Stdout io.ReadCloser  // stdio 模式下的 stdout 管道
}

// MCPRegistry 管理多个 MCP Server 的注册、启动和tool发现
type MCPRegistry struct {
	mu      sync.RWMutex
	servers map[string]*MCPClientEntry
}

// mcpEnvWhitelist MCP 子进程环境变量白名单。
// 外部 MCP Server 是独立进程，默认（cmd.Env 为 nil）会继承完整宿主环境，
// 将宿主 API Key 等敏感变量泄露给第三方 server。这里仅传递必要项，
// 其余宿主变量一律不传递；需要更多变量时由使用方在配置中显式指定。
var mcpEnvWhitelist = map[string]bool{
	"PATH": true, "HOME": true, "TMPDIR": true,
	"LANG": true, "LC_ALL": true, "USER": true, "LOGNAME": true,
	// Go 工具链相关（MCP server 常以 go run 方式启动，非敏感信息）
	"GOPATH": true, "GOROOT": true, "GOCACHE": true,
	"GOMODCACHE": true, "GOPROXY": true, "GOFLAGS": true, "GOTOOLCHAIN": true,
}

// buildMCPSubprocessEnv 构建 MCP 子进程环境变量：白名单内的宿主变量 + 用户
// 显式配置（后者覆盖前者，可引入白名单外自定义项，如 NODE_ENV）。
// 返回去重后的 KEY=VALUE 列表。
func buildMCPSubprocessEnv(extra map[string]string) []string {
	allowed := make(map[string]bool, len(mcpEnvWhitelist)+8)
	for k := range mcpEnvWhitelist {
		allowed[k] = true
	}
	if runtime.GOOS == "windows" {
		// Windows 下 npx.cmd 等批处理启动器依赖的基础变量
		for _, k := range []string{"SYSTEMROOT", "WINDIR", "COMSPEC", "PATHEXT", "TEMP", "TMP", "USERPROFILE"} {
			allowed[k] = true
		}
	}

	env := make(map[string]string, len(allowed))
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok && allowed[k] {
			env[k] = v
		}
	}
	for k, v := range extra {
		env[k] = v // 用户显式配置优先（可覆盖白名单项，也可引入自定义项）
	}

	result := make([]string, 0, len(env))
	for k, v := range env {
		result = append(result, k+"="+v)
	}
	return result
}

// NewMCPRegistry 创建 MCP Server 注册中心
func NewMCPRegistry() *MCPRegistry {
	return &MCPRegistry{
		servers: make(map[string]*MCPClientEntry),
	}
}

// Register 注册一个 MCP Server 配置
func (r *MCPRegistry) Register(config MCPClientConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.servers[config.Name] = &MCPClientEntry{
		Config: config,
		Status: MCPClientStopped,
	}
}

// Unregister 移除并停止一个 MCP Server
func (r *MCPRegistry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, ok := r.servers[name]
	if !ok {
		return fmt.Errorf("MCP server %q not registered", name)
	}

	if entry.Cmd != nil && entry.Cmd.Process != nil {
		_ = entry.Cmd.Process.Signal(os.Interrupt)
	}

	delete(r.servers, name)
	return nil
}

// Start 启动指定 MCP Server 并初始化连接
func (r *MCPRegistry) Start(ctx context.Context, name string) error {
	r.mu.Lock()
	entry, ok := r.servers[name]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("MCP server %q not registered", name)
	}

	if entry.Status == MCPClientRunning {
		r.mu.Unlock()
		return nil
	}

	entry.Status = MCPClientStarting
	// v3.9-4：解析启动命令（Windows npx.cmd 兼容）。
	// 必须持锁修改 entry.Config.Command——List()/Get() 会读取并拷贝该字段，
	// 无锁写入构成数据竞争（-race 可检出）。
	entry.Config.Command = resolveMCPCommand(entry.Config.Command)
	// 锁内拷贝配置快照：startProcess 在锁外读取 Command/Args/Env，
	// 直接读 entry.Config 会与并发 Start 的持锁写构成数据竞争。
	cfg := entry.Config
	r.mu.Unlock()

	if cfg.BaseURL != "" {
		return r.connectExisting(ctx, name, cfg.BaseURL)
	}

	return r.startProcess(ctx, name, entry, cfg)
}

// connectExisting 连接已运行的 MCP Server
func (r *MCPRegistry) connectExisting(ctx context.Context, name string, baseURL string) error {
	client := NewMCPClient(baseURL)
	if err := client.Initialize(ctx); err != nil {
		r.mu.Lock()
		if entry, ok := r.servers[name]; ok {
			entry.Status = MCPClientFailed
		}
		r.mu.Unlock()
		return fmt.Errorf("MCP server %q initialization failed: %w", name, err)
	}

	r.mu.Lock()
	if entry, ok := r.servers[name]; ok {
		entry.Client = client
		entry.Tools = client.Tools()
		entry.Status = MCPClientRunning
	}
	r.mu.Unlock()

	return nil
}

// startProcess 启动 MCP Server 子进程并连接。
// cfg 为 Start 持锁拷贝的配置快照，避免锁外读取 entry.Config 与并发写竞争。
func (r *MCPRegistry) startProcess(ctx context.Context, name string, entry *MCPClientEntry, cfg MCPClientConfig) error {
	if strings.TrimSpace(cfg.Command) == "" {
		r.mu.Lock()
		entry.Status = MCPClientFailed
		r.mu.Unlock()
		return fmt.Errorf("MCP Server %q command cannot be empty", name)
	}

	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)

	// 环境变量：最小白名单 + 用户显式配置。
	// 旧实现仅在用户配置了 Env 时才基于 os.Environ() 追加、否则（cmd.Env 为 nil）
	// 子进程继承完整宿主环境（含 API Key 等敏感变量），存在密钥泄露风险；
	// 现一律显式构造最小环境（见 buildMCPSubprocessEnv）。
	cmd.Env = buildMCPSubprocessEnv(cfg.Env)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("MCP server %q: failed to create stdin pipe: %w", name, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("MCP server %q: failed to create stdout pipe: %w", name, err)
	}
	// stderr 不再直接透传到进程 stderr，避免泄露敏感信息；改为丢弃。
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		r.mu.Lock()
		entry.Status = MCPClientFailed
		r.mu.Unlock()
		return fmt.Errorf("MCP server %q: failed to start: %w", name, err)
	}

	// 启动后台 goroutine 收割子进程，避免僵尸进程并更新状态。
	go func() {
		_ = cmd.Wait()
		r.mu.Lock()
		if entry.Cmd == cmd {
			entry.Status = MCPClientStopped
		}
		r.mu.Unlock()
	}()

	// 等待服务就绪
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		return ctx.Err()
	}

	r.mu.Lock()
	entry.Cmd = cmd
	entry.Stdin = stdin
	entry.Stdout = stdout

	// 创建 stdio 模式 MCP 客户端并通过 JSON-RPC 初始化连接
	client := NewMCPClientStdio(stdin, stdout)
	entry.Client = client
	r.mu.Unlock()

	// 执行 MCP 握手（initialize → tools/list），最多重试 3 次应对慢启动
	var initErr error
	for i := 0; i < 3; i++ {
		initCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		initErr = client.Initialize(initCtx)
		cancel()
		if initErr == nil {
			break
		}
		select {
		case <-time.After(time.Duration(i+1) * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if initErr != nil {
		r.mu.Lock()
		entry.Status = MCPClientFailed
		entry.Client = nil
		r.mu.Unlock()
		_ = cmd.Process.Signal(os.Interrupt)
		return fmt.Errorf("MCP server %q: stdio initialization failed: %w", name, initErr)
	}

	r.mu.Lock()
	entry.Tools = client.Tools()
	entry.Status = MCPClientRunning
	r.mu.Unlock()

	return nil
}

// StartAll 启动所有 AutoStart=true 的 MCP Server
func (r *MCPRegistry) StartAll(ctx context.Context) error {
	r.mu.RLock()
	var names []string
	for name, entry := range r.servers {
		if entry.Config.AutoStart {
			names = append(names, name)
		}
	}
	r.mu.RUnlock()

	var errs []error
	for _, name := range names {
		if err := r.Start(ctx, name); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("some MCP servers failed to start: %v", errs)
	}
	return nil
}

// Stop 停止指定 MCP Server
func (r *MCPRegistry) Stop(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, ok := r.servers[name]
	if !ok {
		return fmt.Errorf("MCP server %q not registered", name)
	}

	if entry.Client != nil {
		_ = entry.Client.Close()
		entry.Client = nil
	}

	// 关闭 stdio 管道
	if entry.Stdin != nil {
		_ = entry.Stdin.Close()
		entry.Stdin = nil
	}
	if entry.Stdout != nil {
		_ = entry.Stdout.Close()
		entry.Stdout = nil
	}

	if entry.Cmd != nil && entry.Cmd.Process != nil {
		_ = entry.Cmd.Process.Signal(os.Interrupt)
		entry.Cmd = nil
	}

	entry.Status = MCPClientStopped
	return nil
}

// StopAll 停止所有 MCP Server
func (r *MCPRegistry) StopAll() {
	r.mu.RLock()
	names := make([]string, 0, len(r.servers))
	for name := range r.servers {
		names = append(names, name)
	}
	r.mu.RUnlock()

	for _, name := range names {
		_ = r.Stop(name)
	}
}

// RegisterIntoRegistry 将所有运行中的 MCP Server tool注册到 ToolRegistry
func (r *MCPRegistry) RegisterIntoRegistry(registry *Registry) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for name, entry := range r.servers {
		if entry.Status != MCPClientRunning || entry.Client == nil {
			continue
		}

		// v3.9-4：应用命名空间前缀，隔离多 server 同名工具
		if entry.Config.ToolPrefix != "" {
			entry.Client.SetToolPrefix(entry.Config.ToolPrefix)
		} else if entry.Config.Name != "" {
			// 未显式指定前缀时，用服务器名作为默认前缀，避免同名工具互相覆盖
			entry.Client.SetToolPrefix(entry.Config.Name)
		}

		if err := entry.Client.RegisterIntoRegistry(registry); err != nil {
			return fmt.Errorf("MCP server %q: tool registration failed: %w", name, err)
		}
	}

	return nil
}

// List 列出所有已注册的 MCP Server
func (r *MCPRegistry) List() []MCPClientEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]MCPClientEntry, 0, len(r.servers))
	for _, entry := range r.servers {
		result = append(result, *entry)
	}
	return result
}

// Get 获取指定 MCP Server 的信息
func (r *MCPRegistry) Get(name string) (*MCPClientEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, ok := r.servers[name]
	if !ok {
		return nil, false
	}
	return entry, true
}

// Test 测试 MCP Server 连通性
func (r *MCPRegistry) Test(ctx context.Context, name string) error {
	r.mu.RLock()
	entry, ok := r.servers[name]
	r.mu.RUnlock()

	if !ok {
		return fmt.Errorf("MCP server %q not registered", name)
	}

	if entry.Client == nil {
		return fmt.Errorf("MCP server %q not started", name)
	}

	return entry.Client.Initialize(ctx)
}

// LoadFromConfig 从配置文件加载 MCP Server 配置。
//
// 配置文件完整性警示（P2）：
//   - 配置文件通常包含各 server 的启动命令、参数与环境变量（可能含令牌），
//     应限制文件权限（如 0o600）并纳入密钥管理，不要提交到版本库；
//   - 每个 server 配置必须至少提供 command（子进程启动）或 baseUrl
//     （连接已运行实例）之一，否则 Start 时才会失败；
//   - 环境变量经 buildMCPSubprocessEnv 过滤：仅白名单宿主变量 + 本配置
//     显式声明的 Env 会传递给子进程，宿主其余变量不会泄露。
func (r *MCPRegistry) LoadFromConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg struct {
		MCP struct {
			Servers map[string]MCPClientConfig `json:"servers"`
		} `json:"mcp"`
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	for name, serverCfg := range cfg.MCP.Servers {
		serverCfg.Name = name
		r.Register(serverCfg)
	}

	return nil
}
