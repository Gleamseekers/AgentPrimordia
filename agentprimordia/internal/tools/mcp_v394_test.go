package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ===== v3.9-4 MCP 深度集成 =====
// 目标：主流 MCP server 开箱即用（多 server 同名工具不冲突、npx 启动兼容）

// mcpHandlerV394 构造一个最小 MCP JSON-RPC 处理器
func mcpHandlerV394(responses map[string]any) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req MCPRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		w.Header().Set("Content-Type", "application/json")
		if resp, ok := responses[req.Method]; ok {
			_ = json.NewEncoder(w).Encode(MCPResponse{JSONRPC: "2.0", ID: req.ID, Result: resp})
			return
		}
		_ = json.NewEncoder(w).Encode(MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &MCPError{Code: -32601, Message: "method not found: " + req.Method},
		})
	})
}

// 测试1：mcpToolAdapter 支持命名空间前缀（名称冲突隔离）
func TestMCPToolAdapter_WithPrefix(t *testing.T) {
	responses := map[string]any{
		"tools/call": map[string]any{
			"content": []any{map[string]any{"type": "text", "text": "pong"}},
		},
	}
	ts := httptest.NewServer(mcpHandlerV394(responses))
	defer ts.Close()

	client := NewMCPClient(ts.URL)
	adapter := &mcpToolAdapter{
		client: client,
		def: MCPToolDefinition{
			Name:        "ping",
			Description: "ping tool",
		},
		prefix: "github",
	}

	// 对外名称应带命名空间前缀
	if got := adapter.Name(); got != "github_ping" {
		t.Errorf("带前缀 Name 应为 github_ping，实际 %q", got)
	}

	// 执行仍调用原始工具名（MCP server 只认识 ping）
	result, err := adapter.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	if result.Content != "pong" {
		t.Errorf("期望 pong，实际 %q", result.Content)
	}
}

// 测试2：无前缀时行为保持不变（向后兼容）
func TestMCPToolAdapter_NoPrefix(t *testing.T) {
	adapter := &mcpToolAdapter{
		client: nil,
		def:    MCPToolDefinition{Name: "ping"},
	}
	if got := adapter.Name(); got != "ping" {
		t.Errorf("无前缀 Name 应为 ping，实际 %q", got)
	}
}

// 测试3：MCPClient.SetToolPrefix + RegisterIntoRegistry 注册带前缀工具
func TestMCPClient_RegisterIntoRegistry_WithPrefix(t *testing.T) {
	responses := map[string]any{
		"initialize": MCPInitializeResponse{
			ProtocolVersion: mcpProtocolVersion,
			ServerInfo:      MCPServerInfo{Name: "github-server", Version: "1.0"},
		},
		"tools/list": MCPListToolsResponse{
			Tools: []MCPToolDefinition{
				{Name: "read_file", Description: "read a file"},
				{Name: "list_repos", Description: "list repos"},
			},
		},
	}
	ts := httptest.NewServer(mcpHandlerV394(responses))
	defer ts.Close()

	client := NewMCPClient(ts.URL)
	if err := client.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize 失败: %v", err)
	}
	client.SetToolPrefix("github")

	reg := NewRegistry()
	if err := client.RegisterIntoRegistry(reg); err != nil {
		t.Fatalf("register 失败: %v", err)
	}

	if _, ok := reg.Get("github_read_file"); !ok {
		t.Error("应能通过 github_read_file 找到工具")
	}
	if _, ok := reg.Get("read_file"); ok {
		t.Error("原始名 read_file 不应暴露（避免与其它 server 冲突）")
	}
	if _, ok := reg.Get("github_list_repos"); !ok {
		t.Error("应能通过 github_list_repos 找到工具")
	}
}

// 测试4：两个 MCP server 提供同名工具，带前缀注册后互不覆盖
func TestMCPRegistry_RegisterIntoRegistry_NoCollision(t *testing.T) {
	base := map[string]any{
		"initialize": MCPInitializeResponse{
			ProtocolVersion: mcpProtocolVersion,
			ServerInfo:      MCPServerInfo{Name: "s", Version: "1.0"},
		},
		"tools/list": MCPListToolsResponse{
			Tools: []MCPToolDefinition{
				{Name: "get_weather", Description: "weather"},
			},
		},
	}
	tsA := httptest.NewServer(mcpHandlerV394(base))
	defer tsA.Close()
	tsB := httptest.NewServer(mcpHandlerV394(base))
	defer tsB.Close()

	reg := NewMCPRegistry()
	reg.Register(MCPClientConfig{Name: "server-a", BaseURL: tsA.URL, ToolPrefix: "a"})
	reg.Register(MCPClientConfig{Name: "server-b", BaseURL: tsB.URL, ToolPrefix: "b"})

	// 直接通过 MCPRegistry 的注册逻辑测试：手动构造运行中状态
	// （HTTP 模式通过 connectExisting 启动，这里用最小注入验证前缀隔离）
	if err := reg.Start(context.Background(), "server-a"); err != nil {
		t.Fatalf("启动 server-a 失败: %v", err)
	}
	if err := reg.Start(context.Background(), "server-b"); err != nil {
		t.Fatalf("启动 server-b 失败: %v", err)
	}

	toolReg := NewRegistry()
	if err := reg.RegisterIntoRegistry(toolReg); err != nil {
		t.Fatalf("RegisterIntoRegistry 失败: %v", err)
	}

	if _, ok := toolReg.Get("a_get_weather"); !ok {
		t.Error("应能找到 a_get_weather")
	}
	if _, ok := toolReg.Get("b_get_weather"); !ok {
		t.Error("应能找到 b_get_weather")
	}
}

// 测试5：resolveMCPCommand 对 npx 命令的解析（Windows .cmd 兼容）
func TestResolveMCPCommand(t *testing.T) {
	// 普通命令保持不变
	if got := resolveMCPCommand("my-server"); got != "my-server" {
		t.Errorf("普通命令应不变，实际 %q", got)
	}
	if got := resolveMCPCommand("/usr/bin/mcp-server"); got != "/usr/bin/mcp-server" {
		t.Errorf("绝对路径应不变，实际 %q", got)
	}

	// npx 解析：在 PATH 中能找到 npx.cmd（Windows）时返回 .cmd 后缀。
	// 为避免依赖宿主是否安装 npx（CI / 容器常无 Node），构造隔离的临时 PATH 垫片，
	// 使该断言在任意环境确定性通过（AGENTS §5：外部依赖需隔离）。
	shimDir := t.TempDir()
	shimName := "npx"
	if runtime.GOOS == "windows" {
		shimName += ".cmd"
	}
	if err := os.WriteFile(filepath.Join(shimDir, shimName), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("写入 npx 垫片失败: %v", err)
	}
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	got := resolveMCPCommand("npx")
	if !strings.HasPrefix(got, "npx") {
		t.Errorf("npx 解析结果应以 npx 开头，实际 %q", got)
	}

	// 无论平台，若显式带扩展名则保持不变
	if got := resolveMCPCommand("npx.cmd"); got != "npx.cmd" {
		t.Errorf("显式扩展名应不变，实际 %q", got)
	}

	// 平台一致性检查：lookup 逻辑在两个平台都应指向实际可用的可执行文件
	if _, err := exec.LookPath(got); err != nil {
		t.Errorf("LookPath(%q) 失败: %v", got, err)
	}
}
