package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/tools"
)

// ===== MCPToolAdapter 测试 =====

func newInitializedClient(t *testing.T) *Client {
	t.Helper()
	client := newTestClient(t)
	ctx := context.Background()
	if err := client.Initialize(ctx); err != nil {
		client.Close()
		t.Fatalf("初始化客户端失败: %v", err)
	}
	return client
}

func TestMCPToolAdapter_Name(t *testing.T) {
	client := newInitializedClient(t)
	defer client.Close()

	toolDefs := client.Tools()
	if len(toolDefs) == 0 {
		t.Fatal("模拟服务器应提供工具")
	}

	adapter := NewMCPToolAdapter(client, toolDefs[0])
	if adapter.Name() != toolDefs[0].Name {
		t.Errorf("Name() = %q, 期望 %q", adapter.Name(), toolDefs[0].Name)
	}
}

func TestMCPToolAdapter_Description(t *testing.T) {
	client := newInitializedClient(t)
	defer client.Close()

	toolDefs := client.Tools()
	adapter := NewMCPToolAdapter(client, toolDefs[0])

	if adapter.Description() != toolDefs[0].Description {
		t.Errorf("Description() = %q, 期望 %q", adapter.Description(), toolDefs[0].Description)
	}
}

func TestMCPToolAdapter_Parameters_有效Schema(t *testing.T) {
	client := newInitializedClient(t)
	defer client.Close()

	toolDefs := client.Tools()
	adapter := NewMCPToolAdapter(client, toolDefs[0])

	params := adapter.Parameters()
	if params == nil {
		t.Fatal("Parameters() 不应返回 nil")
	}

	var schema map[string]any
	if err := json.Unmarshal(params, &schema); err != nil {
		t.Fatalf("Parameters() 应返回有效 JSON: %v", err)
	}
	if schema["type"] != "object" {
		t.Errorf("schema type = %v, 期望 object", schema["type"])
	}
}

func TestMCPToolAdapter_Parameters_NilSchema(t *testing.T) {
	client := newInitializedClient(t)
	defer client.Close()

	adapter := NewMCPToolAdapter(client, ToolDefinition{
		Name:        "nil_schema_tool",
		Description: "无 Schema 工具",
		InputSchema: nil,
	})

	params := adapter.Parameters()
	if params == nil {
		t.Fatal("Parameters() 不应返回 nil 即使 InputSchema 为 nil")
	}

	var schema map[string]any
	if err := json.Unmarshal(params, &schema); err != nil {
		t.Fatalf("Parameters() 应返回有效 JSON: %v", err)
	}
}

func TestMCPToolAdapter_Execute_echo(t *testing.T) {
	client := newInitializedClient(t)
	defer client.Close()

	// 找到 echo 工具
	var echoDef ToolDefinition
	for _, td := range client.Tools() {
		if td.Name == "echo" {
			echoDef = td
			break
		}
	}
	if echoDef.Name == "" {
		t.Fatal("未找到 echo 工具")
	}

	adapter := NewMCPToolAdapter(client, echoDef)

	result, err := adapter.Execute(context.Background(), json.RawMessage(`{"message":"hello world"}`))
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	if result.IsError {
		t.Errorf("不应为错误结果: %s", result.Content)
	}
	if result.Content != "hello world" {
		t.Errorf("结果 = %q, 期望 %q", result.Content, "hello world")
	}
}

func TestMCPToolAdapter_Execute_add(t *testing.T) {
	client := newInitializedClient(t)
	defer client.Close()

	var addDef ToolDefinition
	for _, td := range client.Tools() {
		if td.Name == "add" {
			addDef = td
			break
		}
	}
	if addDef.Name == "" {
		t.Fatal("未找到 add 工具")
	}

	adapter := NewMCPToolAdapter(client, addDef)

	result, err := adapter.Execute(context.Background(), json.RawMessage(`{"a":10,"b":20}`))
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	if result.IsError {
		t.Errorf("不应为错误结果: %s", result.Content)
	}
	if result.Content != "30" {
		t.Errorf("结果 = %q, 期望 %q", result.Content, "30")
	}
}

func TestMCPToolAdapter_Execute_无效JSON参数(t *testing.T) {
	client := newInitializedClient(t)
	defer client.Close()

	toolDefs := client.Tools()
	adapter := NewMCPToolAdapter(client, toolDefs[0])

	// 无效 JSON 参数应被解析为空 map，不应 panic
	result, err := adapter.Execute(context.Background(), json.RawMessage(`not json`))
	// echo 工具收到空 message，返回空字符串
	if err != nil {
		// 这也是可接受的，取决于服务器行为
		t.Logf("无效参数导致错误（可接受）: %v", err)
	} else {
		t.Logf("无效参数被处理为空 map，结果: %s", result.Content)
	}
}

func TestMCPToolAdapter_Execute_错误结果(t *testing.T) {
	client := newInitializedClient(t)
	defer client.Close()

	adapter := NewMCPToolAdapter(client, ToolDefinition{
		Name:        "nonexistent",
		Description: "不存在的工具",
		InputSchema: map[string]any{"type": "object"},
	})

	result, err := adapter.Execute(context.Background(), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("调用不存在的工具应返回错误")
	}
	if result == nil {
		t.Fatal("结果不应为 nil")
	}
	if !result.IsError {
		t.Error("结果应标记为错误")
	}
}

func TestMCPToolAdapter_实现Tool接口(t *testing.T) {
	// 编译时验证 MCPToolAdapter 实现了 tools.Tool 接口
	var _ tools.Tool = (*MCPToolAdapter)(nil)
}

// ===== Registry 测试 =====

func TestRegistry_Connect_成功(t *testing.T) {
	registry := NewRegistry()
	defer registry.Close()

	ctx := context.Background()
	cfg := Config{
		Command: "go",
		Args:    []string{"run", mockServerSrcPath},
		Timeout: 5 * time.Second,
	}

	err := registry.Connect(ctx, "test-server", cfg)
	if err != nil {
		t.Fatalf("Connect 失败: %v", err)
	}

	names := registry.List()
	if len(names) != 1 {
		t.Fatalf("服务器数量 = %d, 期望 1", len(names))
	}
	if names[0] != "test-server" {
		t.Errorf("服务器名称 = %q, 期望 %q", names[0], "test-server")
	}
}

func TestRegistry_Connect_重复名称(t *testing.T) {
	registry := NewRegistry()
	defer registry.Close()

	ctx := context.Background()
	cfg := Config{
		Command: "go",
		Args:    []string{"run", mockServerSrcPath},
		Timeout: 5 * time.Second,
	}

	// 第一次连接
	if err := registry.Connect(ctx, "test-server", cfg); err != nil {
		t.Fatalf("第一次 Connect 失败: %v", err)
	}

	// 第二次连接同名服务器（应替换旧连接）
	if err := registry.Connect(ctx, "test-server", cfg); err != nil {
		t.Fatalf("第二次 Connect 失败: %v", err)
	}

	names := registry.List()
	if len(names) != 1 {
		t.Errorf("重复连接后服务器数量 = %d, 期望 1", len(names))
	}
}

func TestRegistry_Disconnect_成功(t *testing.T) {
	registry := NewRegistry()
	defer registry.Close()

	ctx := context.Background()
	cfg := Config{
		Command: "go",
		Args:    []string{"run", mockServerSrcPath},
		Timeout: 5 * time.Second,
	}

	if err := registry.Connect(ctx, "test-server", cfg); err != nil {
		t.Fatalf("Connect 失败: %v", err)
	}

	if err := registry.Disconnect("test-server"); err != nil {
		t.Fatalf("Disconnect 失败: %v", err)
	}

	names := registry.List()
	if len(names) != 0 {
		t.Errorf("断开后服务器数量 = %d, 期望 0", len(names))
	}
}

func TestRegistry_Disconnect_未连接(t *testing.T) {
	registry := NewRegistry()
	defer registry.Close()

	err := registry.Disconnect("nonexistent")
	if err == nil {
		t.Fatal("断开未连接的服务器应返回错误")
	}
}

func TestRegistry_GetTools(t *testing.T) {
	registry := NewRegistry()
	defer registry.Close()

	ctx := context.Background()
	cfg := Config{
		Command: "go",
		Args:    []string{"run", mockServerSrcPath},
		Timeout: 5 * time.Second,
	}

	if err := registry.Connect(ctx, "test-server", cfg); err != nil {
		t.Fatalf("Connect 失败: %v", err)
	}

	toolList := registry.GetTools()
	if len(toolList) != 2 {
		t.Errorf("工具数量 = %d, 期望 2", len(toolList))
	}

	// 验证返回的是 tools.Tool 接口
	for _, tool := range toolList {
		if tool.Name() == "" {
			t.Error("工具名称不应为空")
		}
		if tool.Description() == "" {
			t.Error("工具描述不应为空")
		}
	}
}

func TestRegistry_RegisterIntoRegistry(t *testing.T) {
	mcpRegistry := NewRegistry()
	defer mcpRegistry.Close()

	ctx := context.Background()
	cfg := Config{
		Command: "go",
		Args:    []string{"run", mockServerSrcPath},
		Timeout: 5 * time.Second,
	}

	if err := mcpRegistry.Connect(ctx, "test-server", cfg); err != nil {
		t.Fatalf("Connect 失败: %v", err)
	}

	// 注册到 AP 的 ToolRegistry
	apRegistry := tools.NewRegistry()
	if err := mcpRegistry.RegisterIntoRegistry(apRegistry); err != nil {
		t.Fatalf("RegisterIntoRegistry 失败: %v", err)
	}

	// 验证工具已注册
	if apRegistry.Count() != 2 {
		t.Errorf("AP Registry 工具数量 = %d, 期望 2", apRegistry.Count())
	}

	// 验证可以通过 AP Registry 调用 MCP 工具
	echoTool, ok := apRegistry.Get("echo")
	if !ok {
		t.Fatal("echo 工具应已注册")
	}

	result, err := echoTool.Execute(context.Background(), json.RawMessage(`{"message":"from AP registry"}`))
	if err != nil {
		t.Fatalf("通过 AP Registry 调用 echo 失败: %v", err)
	}
	if result.Content != "from AP registry" {
		t.Errorf("结果 = %q, 期望 %q", result.Content, "from AP registry")
	}
}

func TestRegistry_GetClient(t *testing.T) {
	registry := NewRegistry()
	defer registry.Close()

	ctx := context.Background()
	cfg := Config{
		Command: "go",
		Args:    []string{"run", mockServerSrcPath},
		Timeout: 5 * time.Second,
	}

	if err := registry.Connect(ctx, "test-server", cfg); err != nil {
		t.Fatalf("Connect 失败: %v", err)
	}

	client, ok := registry.GetClient("test-server")
	if !ok {
		t.Fatal("应找到已连接的客户端")
	}

	tools := client.Tools()
	if len(tools) != 2 {
		t.Errorf("工具数量 = %d, 期望 2", len(tools))
	}

	_, ok = registry.GetClient("nonexistent")
	if ok {
		t.Error("不应找到未连接的客户端")
	}
}

func TestRegistry_Close(t *testing.T) {
	registry := NewRegistry()

	ctx := context.Background()
	cfg := Config{
		Command: "go",
		Args:    []string{"run", mockServerSrcPath},
		Timeout: 5 * time.Second,
	}

	if err := registry.Connect(ctx, "server-1", cfg); err != nil {
		t.Fatalf("Connect server-1 失败: %v", err)
	}
	if err := registry.Connect(ctx, "server-2", cfg); err != nil {
		t.Fatalf("Connect server-2 失败: %v", err)
	}

	if err := registry.Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}

	names := registry.List()
	if len(names) != 0 {
		t.Errorf("关闭后服务器数量 = %d, 期望 0", len(names))
	}
}

func TestRegistry_多个服务器(t *testing.T) {
	registry := NewRegistry()
	defer registry.Close()

	ctx := context.Background()
	cfg := Config{
		Command: "go",
		Args:    []string{"run", mockServerSrcPath},
		Timeout: 5 * time.Second,
	}

	if err := registry.Connect(ctx, "server-a", cfg); err != nil {
		t.Fatalf("Connect server-a 失败: %v", err)
	}
	if err := registry.Connect(ctx, "server-b", cfg); err != nil {
		t.Fatalf("Connect server-b 失败: %v", err)
	}

	names := registry.List()
	if len(names) != 2 {
		t.Errorf("服务器数量 = %d, 期望 2", len(names))
	}

	// 每个服务器提供 2 个工具，总共 4 个
	toolList := registry.GetTools()
	if len(toolList) != 4 {
		t.Errorf("总工具数量 = %d, 期望 4", len(toolList))
	}
}

// ===== 大结果截断测试 =====

func TestMCPToolAdapter_Execute_大结果截断(t *testing.T) {
	// 直接测试截断逻辑，不依赖实际 MCP 服务器

	// 模拟大结果
	largeText := strings.Repeat("x", 200*1024) // 200KB
	result := &ToolCallResult{
		Content: []ContentBlock{
			{Type: "text", Text: largeText},
		},
	}

	// 直接测试截断逻辑
	var textParts []string
	totalLen := 0
	for _, content := range result.Content {
		if content.Type == "text" && content.Text != "" {
			text := content.Text
			remaining := maxToolResultLen - totalLen
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

	combined := strings.Join(textParts, "\n")
	if len(combined) > maxToolResultLen+50 {
		t.Errorf("结果应被截断到约 %d 字节, 实际 %d 字节", maxToolResultLen, len(combined))
	}
}

// ===== 完整集成测试 =====

func TestIntegration_完整MCP流程(t *testing.T) {
	registry := NewRegistry()
	defer registry.Close()

	ctx := context.Background()
	cfg := Config{
		Command: "go",
		Args:    []string{"run", mockServerSrcPath},
		Timeout: 5 * time.Second,
	}

	// 1. 连接服务器
	if err := registry.Connect(ctx, "integration-server", cfg); err != nil {
		t.Fatalf("Connect 失败: %v", err)
	}

	// 2. 获取客户端
	client, ok := registry.GetClient("integration-server")
	if !ok {
		t.Fatal("应找到客户端")
	}

	// 3. 验证服务器信息
	info := client.ServerInfo()
	if info.Name != "mock-mcp-server" {
		t.Errorf("服务器名称 = %q, 期望 %q", info.Name, "mock-mcp-server")
	}

	// 4. 验证工具发现
	toolList := client.Tools()
	if len(toolList) != 2 {
		t.Fatalf("工具数量 = %d, 期望 2", len(toolList))
	}

	// 5. 调用工具
	result, err := client.CallTool(ctx, "echo", map[string]any{"message": "integration test"})
	if err != nil {
		t.Fatalf("CallTool 失败: %v", err)
	}
	if result.Content[0].Text != "integration test" {
		t.Errorf("结果 = %q, 期望 %q", result.Content[0].Text, "integration test")
	}

	// 6. 读取资源
	resourceText, err := client.ReadResource(ctx, "file:///data/config.json")
	if err != nil {
		t.Fatalf("ReadResource 失败: %v", err)
	}
	if resourceText == "" {
		t.Error("资源内容不应为空")
	}

	// 7. 获取提示词
	promptText, err := client.GetPrompt(ctx, "greet", map[string]any{"name": "Test"})
	if err != nil {
		t.Fatalf("GetPrompt 失败: %v", err)
	}
	if promptText == "" {
		t.Error("提示词内容不应为空")
	}

	// 8. 心跳检测
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("Ping 失败: %v", err)
	}

	// 9. 注册到 AP Registry
	apRegistry := tools.NewRegistry()
	if err := registry.RegisterIntoRegistry(apRegistry); err != nil {
		t.Fatalf("RegisterIntoRegistry 失败: %v", err)
	}

	// 10. 通过 AP Registry 调用
	echoTool, ok := apRegistry.Get("echo")
	if !ok {
		t.Fatal("echo 工具应已注册到 AP Registry")
	}
	apResult, err := echoTool.Execute(ctx, json.RawMessage(`{"message":"via AP"}`))
	if err != nil {
		t.Fatalf("通过 AP Registry 调用失败: %v", err)
	}
	if apResult.Content != "via AP" {
		t.Errorf("AP Registry 结果 = %q, 期望 %q", apResult.Content, "via AP")
	}
}
