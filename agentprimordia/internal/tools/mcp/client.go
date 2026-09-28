package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// mcpProtocolVersion 支持的 MCP 协议版本
	mcpProtocolVersion = "2024-11-05"

	// defaultTimeout 默认request timeout时间
	defaultTimeout = 30 * time.Second

	// maxToolResultLen 单个 MCP tool结果文本最大长度
	maxToolResultLen = 100 * 1024

	// maxStdioLineBytes stdio 单行最大字节数（16MB）。
	// 旧实现 c.stdout.ReadString('\n') 无上限：异常/恶意的超长行会持续
	// 膨胀读取缓冲直至 OOM。现限制单行上限，超限行跳过（保持行边界同步）
	// 而非退出 readLoop，避免后续请求全部挂死。
	maxStdioLineBytes = 16 << 20

	// stdioWriteTimeout stdio 写入超时：子进程不读 stdin 时，避免持锁写入
	// 永久阻塞（exec 管道为 *os.File，支持 SetWriteDeadline）。
	stdioWriteTimeout = 30 * time.Second
)

// errStdioLineTooLong 单行超过 maxStdioLineBytes 上限
var errStdioLineTooLong = errors.New("stdio line exceeds max length")

// stdioDeadlineWriter 支持写超时的写入器（exec 管道 *os.File 实现该方法）
type stdioDeadlineWriter interface {
	SetWriteDeadline(t time.Time) error
}

// withStdioWriteDeadline 在写入期间为 w 设置写超时，返回恢复函数。
// 超时值取 stdioWriteTimeout 与 ctx deadline 中较短的——请求方取消/超时时
// 写入同步失败，不会继续阻塞。w 不支持 deadline 时退化为直写。
func withStdioWriteDeadline(w io.Writer, ctx context.Context) func() {
	dw, ok := w.(stdioDeadlineWriter)
	if !ok {
		return func() {}
	}
	timeout := stdioWriteTimeout
	if deadline, ok := ctx.Deadline(); ok {
		if d := time.Until(deadline); d < timeout {
			timeout = d
		}
	}
	_ = dw.SetWriteDeadline(time.Now().Add(timeout))
	return func() { _ = dw.SetWriteDeadline(time.Time{}) }
}

// readStdioLineLimited 从 r 读取一行（含结尾 '\n'）。
// 单行超过 limit 字节时，丢弃该行剩余内容（直到换行符）并返回 errStdioLineTooLong，
// 使调用方可以跳过该行继续读取；其他读取错误（含 io.EOF）原样返回。
func readStdioLineLimited(r *bufio.Reader, limit int) ([]byte, error) {
	var buf []byte
	for {
		slice, err := r.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			buf = append(buf, slice...)
			if len(buf) > limit {
				// 丢弃该行剩余部分直到换行符，保持后续行边界同步
				for {
					_, err2 := r.ReadSlice('\n')
					if err2 == nil {
						break
					}
					if !errors.Is(err2, bufio.ErrBufferFull) {
						// EOF 等错误：已到流末尾，无需继续丢弃
						return nil, errStdioLineTooLong
					}
				}
				return nil, errStdioLineTooLong
			}
			continue
		}
		if err != nil {
			// io.EOF 或其他错误：连同已读数据返回，由调用方决定如何处理
			buf = append(buf, slice...)
			return buf, err
		}
		buf = append(buf, slice...)
		if len(buf) > limit {
			return nil, errStdioLineTooLong
		}
		return buf, nil
	}
}

// childEnvWhitelist MCP 子进程环境变量白名单。
// 旧实现 cmd.Env 为 nil（或直接 os.Environ()）时子进程继承完整宿主环境，
// 含 API Key 等敏感变量，存在泄露风险。现显式构造最小环境，
// 仅传递必要项 + 用户显式配置（Config.Env）。
var childEnvWhitelist = map[string]bool{
	"PATH": true, "HOME": true, "TMPDIR": true,
	"LANG": true, "LC_ALL": true, "USER": true, "LOGNAME": true,
	// Go 工具链相关（MCP server 常以 go run 方式启动，非敏感信息）
	"GOPATH": true, "GOROOT": true, "GOCACHE": true,
	"GOMODCACHE": true, "GOPROXY": true, "GOFLAGS": true, "GOTOOLCHAIN": true,
}

// buildChildEnv 构建 MCP 子进程环境变量：白名单内的宿主变量 + 用户显式配置
// （后者覆盖前者，可引入白名单外自定义项）。其余宿主变量一律不传递。
func buildChildEnv(extra map[string]string) []string {
	allowed := make(map[string]bool, len(childEnvWhitelist)+8)
	for k := range childEnvWhitelist {
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

// Client MCP 客户端，通过 stdio JSON-RPC 与 MCP 服务器通信。
// 客户端负责管理子进程的完整生命周期：启动、心跳检测、优雅关闭。
type Client struct {
	cmd       *exec.Cmd                       // MCP 服务器子进程
	stdin     io.WriteCloser                  // 子进程标准输入
	stdout    *bufio.Reader                   // 子进程标准输出
	mu        sync.Mutex                      // 保护 stdin 写入和 pending 操作
	requestID atomic.Int64                    // 自增请求 ID
	pending   map[int64]chan *jsonRPCResponse // 等待响应的回调通道
	pendingMu sync.Mutex                      // 保护 pending map

	tools      []ToolDefinition   // 已发现的tool列表
	resources  []Resource         // 已发现的资源列表
	prompts    []PromptDefinition // 已发现的提示词列表
	serverInfo ServerInfo         // 服务器信息
	dataMu     sync.RWMutex       // 保护 tools/resources/prompts/serverInfo

	logger   *slog.Logger  // 日志记录器
	timeout  time.Duration // request timeout时间
	done     chan struct{} // 关闭信号
	reapDone chan struct{} // 子进程收割完成信号（reapProcess 退出时关闭）
	closed   bool          // 是否已关闭
}

// Config MCP 客户端配置
type Config struct {
	Command string            // 要启动的 MCP 服务器命令
	Args    []string          // 命令参数
	Env     map[string]string // 环境变量
	Timeout time.Duration     // request timeout，0 使用默认值
}

// NewClient 创建并启动 MCP 服务器子进程，返回客户端实例。
// 调用者应在使用完毕后调用 Close() 关闭连接。
func NewClient(cfg Config) (*Client, error) {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}

	if strings.TrimSpace(cfg.Command) == "" {
		return nil, fmt.Errorf("MCP client config error: Command must not be empty")
	}

	cmd := exec.Command(cfg.Command, cfg.Args...)

	// 设置环境变量：最小白名单 + 用户显式配置。
	// 旧实现仅在用户配置了 Env 时才基于 os.Environ() 追加、否则继承完整宿主
	// 环境（含 API Key 等敏感变量）；现一律显式构造最小环境，避免密钥泄露。
	cmd.Env = buildChildEnv(cfg.Env)

	// 创建 stdin/stdout 管道
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	// stderr 丢弃，避免泄露敏感信息
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return nil, fmt.Errorf("failed to start MCP server: %w", err)
	}

	c := &Client{
		cmd:      cmd,
		stdin:    stdin,
		stdout:   bufio.NewReader(stdout),
		pending:  make(map[int64]chan *jsonRPCResponse),
		logger:   slog.Default(),
		timeout:  timeout,
		done:     make(chan struct{}),
		reapDone: make(chan struct{}),
	}

	// 启动后台 goroutine 读取响应
	go c.readLoop()

	// 启动后台 goroutine 收割子进程
	go c.reapProcess()

	return c, nil
}

// Initialize 发送 initialize 请求，完成 MCP 握手。
// 必须在任何其他操作之前调用。
func (c *Client) Initialize(ctx context.Context) error {
	// 1. 发送 initialize 请求
	params := initializeParams{
		ProtocolVersion: mcpProtocolVersion,
		Capabilities:    map[string]any{},
		ClientInfo: struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		}{
			Name:    "AgentPrimordia",
			Version: "0.1.0",
		},
	}

	result, err := c.sendRequest(ctx, "initialize", params)
	if err != nil {
		return fmt.Errorf("MCP initialize failed: %w", err)
	}

	// 解析 initialize 结果
	var initResult initializeResult
	if err := json.Unmarshal(result, &initResult); err != nil {
		return fmt.Errorf("failed to parse initialize response: %w", err)
	}

	c.dataMu.Lock()
	c.serverInfo = initResult.ServerInfo
	c.dataMu.Unlock()

	// 2. 发送 initialized 通知（无需响应）
	if err := c.sendNotification(ctx, "notifications/initialized", nil); err != nil {
		c.logger.Warn("发送 initialized 通知失败", "error", err)
	}

	// 3. 获取tool列表
	if err := c.fetchTools(ctx); err != nil {
		c.logger.Warn("获取tool列表失败", "error", err)
	}

	// 4. 获取资源列表（忽略错误，服务器可能不支持）
	_ = c.fetchResources(ctx)

	// 5. 获取提示词列表（忽略错误，服务器可能不支持）
	_ = c.fetchPrompts(ctx)

	c.logger.Info("MCP 客户端初始化完成",
		"server", initResult.ServerInfo.Name,
		"version", initResult.ServerInfo.Version,
		"tools", len(c.Tools()),
	)

	return nil
}

// ListTools 列出 MCP 服务器提供的tool
func (c *Client) ListTools(ctx context.Context) ([]ToolDefinition, error) {
	result, err := c.sendRequest(ctx, "tools/list", nil)
	if err != nil {
		return nil, fmt.Errorf("tools/list request failed: %w", err)
	}

	var listResult listToolsResult
	if err := json.Unmarshal(result, &listResult); err != nil {
		return nil, fmt.Errorf("failed to parse tools/list response: %w", err)
	}

	c.dataMu.Lock()
	c.tools = listResult.Tools
	c.dataMu.Unlock()

	return listResult.Tools, nil
}

// CallTool 调用 MCP 服务器上的tool
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error) {
	params := callToolParams{
		Name:      name,
		Arguments: args,
	}

	result, err := c.sendRequest(ctx, "tools/call", params)
	if err != nil {
		return nil, fmt.Errorf("tools/call request failed: %w", err)
	}

	var callResult ToolCallResult
	if err := json.Unmarshal(result, &callResult); err != nil {
		return nil, fmt.Errorf("failed to parse tools/call response: %w", err)
	}

	return &callResult, nil
}

// ListResources 列出 MCP 服务器提供的资源
func (c *Client) ListResources(ctx context.Context) ([]Resource, error) {
	result, err := c.sendRequest(ctx, "resources/list", nil)
	if err != nil {
		return nil, fmt.Errorf("resources/list request failed: %w", err)
	}

	var listResult listResourcesResult
	if err := json.Unmarshal(result, &listResult); err != nil {
		return nil, fmt.Errorf("failed to parse resources/list response: %w", err)
	}

	return listResult.Resources, nil
}

// ReadResource 读取 MCP 服务器上的资源
func (c *Client) ReadResource(ctx context.Context, uri string) (string, error) {
	params := readResourceParams{URI: uri}

	result, err := c.sendRequest(ctx, "resources/read", params)
	if err != nil {
		return "", fmt.Errorf("resources/read request failed: %w", err)
	}

	var readResult readResourceResult
	if err := json.Unmarshal(result, &readResult); err != nil {
		return "", fmt.Errorf("failed to parse resources/read response: %w", err)
	}

	// 合并所有文本内容
	var texts []string
	for _, content := range readResult.Contents {
		if content.Text != "" {
			texts = append(texts, content.Text)
		}
	}

	return strings.Join(texts, "\n"), nil
}

// ListPrompts 列出 MCP 服务器提供的提示词模板
func (c *Client) ListPrompts(ctx context.Context) ([]PromptDefinition, error) {
	result, err := c.sendRequest(ctx, "prompts/list", nil)
	if err != nil {
		return nil, fmt.Errorf("prompts/list request failed: %w", err)
	}

	var listResult listPromptsResult
	if err := json.Unmarshal(result, &listResult); err != nil {
		return nil, fmt.Errorf("failed to parse prompts/list response: %w", err)
	}

	return listResult.Prompts, nil
}

// GetPrompt 获取 MCP 服务器上的提示词
func (c *Client) GetPrompt(ctx context.Context, name string, args map[string]any) (string, error) {
	params := getPromptParams{
		Name:      name,
		Arguments: args,
	}

	result, err := c.sendRequest(ctx, "prompts/get", params)
	if err != nil {
		return "", fmt.Errorf("prompts/get request failed: %w", err)
	}

	var getResult getPromptResult
	if err := json.Unmarshal(result, &getResult); err != nil {
		return "", fmt.Errorf("failed to parse prompts/get response: %w", err)
	}

	// 合并所有消息文本
	var texts []string
	for _, msg := range getResult.Messages {
		if msg.Content.Text != "" {
			texts = append(texts, msg.Content.Text)
		}
	}

	return strings.Join(texts, "\n"), nil
}

// Tools 返回已发现的tool列表
func (c *Client) Tools() []ToolDefinition {
	c.dataMu.RLock()
	defer c.dataMu.RUnlock()
	// 返回副本，避免外部修改
	result := make([]ToolDefinition, len(c.tools))
	copy(result, c.tools)
	return result
}

// Resources 返回已发现的资源列表
func (c *Client) Resources() []Resource {
	c.dataMu.RLock()
	defer c.dataMu.RUnlock()
	result := make([]Resource, len(c.resources))
	copy(result, c.resources)
	return result
}

// Prompts 返回已发现的提示词列表
func (c *Client) Prompts() []PromptDefinition {
	c.dataMu.RLock()
	defer c.dataMu.RUnlock()
	result := make([]PromptDefinition, len(c.prompts))
	copy(result, c.prompts)
	return result
}

// ServerInfo 返回 MCP 服务器信息
func (c *Client) ServerInfo() ServerInfo {
	c.dataMu.RLock()
	defer c.dataMu.RUnlock()
	return c.serverInfo
}

// Ping 发送心跳检测请求
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.sendRequest(ctx, "ping", nil)
	return err
}

// Close 关闭 MCP 客户端，优雅关闭服务器进程。
// 先发送 shutdown 通知，再关闭管道，最后终止进程。
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()

	// 关闭 done 通道，通知 readLoop 退出
	close(c.done)

	// 尝试发送关闭通知（忽略错误，进程可能已退出）
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = c.sendNotification(shutdownCtx, "shutdown", nil)

	// 关闭 stdin 管道
	if c.stdin != nil {
		_ = c.stdin.Close()
	}

	// 终止子进程：Wait 统一由 reapProcess 执行，这里只发信号并等待收割完成
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Signal(os.Interrupt)
		select {
		case <-c.reapDone:
			// 进程正常退出（reapProcess 已完成 Wait）
		case <-time.After(3 * time.Second):
			// 超时，强制终止；Kill 后 reapProcess 的 Wait 会返回
			_ = c.cmd.Process.Kill()
			select {
			case <-c.reapDone:
			case <-time.After(3 * time.Second):
				c.logger.Warn("MCP 子进程强制终止后仍未退出")
			}
		}
	}

	return nil
}

// ===== 内部方法 =====

// readLoop 后台持续读取子进程 stdout 的 JSON-RPC 响应。
// 单行上限 maxStdioLineBytes：超限行跳过而非退出循环，避免 readLoop
// 静默退出后所有后续请求挂死；读取错误（EOF 等）才退出。
func (c *Client) readLoop() {
	for {
		select {
		case <-c.done:
			return
		default:
		}

		line, err := readStdioLineLimited(c.stdout, maxStdioLineBytes)
		if err != nil {
			if errors.Is(err, errStdioLineTooLong) {
				// 超长行：记录并跳过，readLoop 继续存活
				c.logger.Warn("MCP 响应行超过单行上限，已跳过", "limit", maxStdioLineBytes)
				continue
			}
			// 管道关闭或读取错误，退出循环
			return
		}

		lineStr := strings.TrimSpace(string(line))
		if lineStr == "" {
			continue
		}

		var resp jsonRPCResponse
		if err := json.Unmarshal([]byte(lineStr), &resp); err != nil {
			c.logger.Warn("解析 JSON-RPC 响应失败", "line", lineStr, "error", err)
			continue
		}

		// 分发响应到等待的请求
		c.pendingMu.Lock()
		if ch, ok := c.pending[resp.ID]; ok {
			delete(c.pending, resp.ID)
			ch <- &resp
		}
		c.pendingMu.Unlock()
	}
}

// reapProcess 后台等待子进程退出，更新状态
// 修复（-race 实测发现）：旧实现与 Close 各自调用 c.cmd.Wait()，
// exec.Cmd.Wait 非并发安全。现统一由本 goroutine 执行 Wait，
// 退出时关闭 reapDone，Close 等待该信号而非自行 Wait。
func (c *Client) reapProcess() {
	defer close(c.reapDone)
	if c.cmd != nil {
		_ = c.cmd.Wait()
	}
}

// sendRequest 发送 JSON-RPC 请求并等待响应
func (c *Client) sendRequest(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.requestID.Add(1)

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize request: %w", err)
	}

	// 注册等待通道
	respCh := make(chan *jsonRPCResponse, 1)
	c.pendingMu.Lock()
	c.pending[id] = respCh
	c.pendingMu.Unlock()

	// 写入 stdin。持锁写入并设置写超时：子进程不读 stdin 时写入在 deadline
	// 后失败，不会永久占用写锁导致后续请求全部挂死。
	c.mu.Lock()
	restore := withStdioWriteDeadline(c.stdin, ctx)
	_, err = fmt.Fprintf(c.stdin, "%s\n", string(reqBody))
	restore()
	c.mu.Unlock()

	if err != nil {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, fmt.Errorf("write request failed: %w", err)
	}

	// 等待响应或超时
	timeout := c.timeout
	if deadline, ok := ctx.Deadline(); ok {
		if d := time.Until(deadline); d < timeout {
			timeout = d
		}
	}

	select {
	case resp := <-respCh:
		if resp.Error != nil {
			return nil, fmt.Errorf("MCP error [%d]: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	case <-time.After(timeout):
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, fmt.Errorf("request %q timed out (%v)", method, timeout)
	case <-ctx.Done():
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, ctx.Err()
	}
}

// sendNotification 发送 JSON-RPC 通知（无需响应）
func (c *Client) sendNotification(ctx context.Context, method string, params any) error {
	notif := jsonRPCNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}

	body, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("failed to serialize notification: %w", err)
	}

	// 持锁写入并设置写超时，避免子进程不读 stdin 时永久阻塞
	c.mu.Lock()
	defer c.mu.Unlock()

	restore := withStdioWriteDeadline(c.stdin, ctx)
	_, err = fmt.Fprintf(c.stdin, "%s\n", string(body))
	restore()
	return err
}

// fetchTools 获取并缓存tool列表
func (c *Client) fetchTools(ctx context.Context) error {
	tools, err := c.ListTools(ctx)
	if err != nil {
		return err
	}
	c.dataMu.Lock()
	c.tools = tools
	c.dataMu.Unlock()
	return nil
}

// fetchResources 获取并缓存资源列表
func (c *Client) fetchResources(ctx context.Context) error {
	resources, err := c.ListResources(ctx)
	if err != nil {
		return err
	}
	c.dataMu.Lock()
	c.resources = resources
	c.dataMu.Unlock()
	return nil
}

// fetchPrompts 获取并缓存提示词列表
func (c *Client) fetchPrompts(ctx context.Context) error {
	prompts, err := c.ListPrompts(ctx)
	if err != nil {
		return err
	}
	c.dataMu.Lock()
	c.prompts = prompts
	c.dataMu.Unlock()
	return nil
}
