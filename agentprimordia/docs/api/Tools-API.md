# Tools API

工具系统 API 参考文档。

## Tool 接口

```go
// Tool 是所有工具必须实现的接口
type Tool interface {
    Name() string
    Description() string
    Parameters() json.RawMessage           // JSON Schema 参数定义
    Execute(ctx context.Context, args json.RawMessage) (*Result, error)
}
```

## Result

```go
type Result struct {
    Content  string         `json:"content"`
    IsError  bool           `json:"is_error"`
    Metadata map[string]any `json:"metadata,omitempty"`
}

// 辅助构造函数
func NewResult(content string) *Result       // 成功结果
func NewErrorResult(content string) *Result  // 错误结果
```

## Permission

```go
type Permission struct {
    AllowedRoles        []string         `json:"allowed_roles,omitempty"`
    BlockedPaths        []string         `json:"blocked_paths,omitempty"`
    RequireConfirmation bool             `json:"require_confirmation,omitempty"`
    ConfirmFunc         ConfirmationFunc `json:"-"`
}

type ConfirmationFunc func(toolName string, args json.RawMessage) bool
```

## Registry

工具注册中心，管理工具注册、查找和权限：

```go
func NewRegistry() *Registry
```

### 主要方法

| 方法 | 说明 |
|------|------|
| `Register(tool Tool) error` | 注册工具（同名幂等覆盖） |
| `Get(name string) (Tool, error)` | 按名称获取工具 |
| `List() []Tool` | 列出所有工具 |
| `Count() int64` | 工具数量 |
| `Definitions() []map[string]any` | 获取所有工具定义（用于 LLM） |
| `SetPermission(name string, perm *Permission)` | 设置工具权限 |
| `GetPermission(name string) *Permission` | 获取工具权限 |

**示例：**

```go
registry := ap.NewToolRegistry()
registry.Register(myTool)
registry.Register(shellTool)

tool, err := registry.Get("shell")
```

## Executor

工具执行器，负责超时控制、权限检查和文件锁：

```go
func NewExecutor(registry *Registry) *Executor

func (e *Executor) Execute(ctx context.Context, toolName string, args json.RawMessage) (*Result, error)
```

**执行流程：**

```
ToolCall → Executor.Execute()
    ↓
1. Registry.Get(name) 查找工具
2. ScopePolicy.Allow(agent, path) 权限检查
3. Permission.RequireConfirmation 确认检查
4. context.WithTimeout 设置超时
5. tool.Execute(ctx, args) 执行
6. 记录指标、返回 Result
```

## ScopePolicy

作用域权限策略接口：

```go
type ScopePolicy interface {
    Allow(agentName string, path string) error
}

// 基于文件路径的权限策略
type FileScopePolicy struct {
    AllowedPaths []string
    BlockedPaths []string
    ReadOnly     bool
}
```

## 内置工具

### DefaultToolkit

一键创建内置工具包：

```go
func DefaultToolkit(config ToolkitConfig) (*Registry, error)

type ToolkitConfig struct {
    RootDir     string
    EnableFS    bool
    EnableShell bool
    EnableWeb   bool
}
```

**示例：**

```go
registry, _ := ap.DefaultToolkit(ap.ToolkitConfig{
    RootDir:     ".",
    EnableFS:    true,
    EnableShell: true,
    EnableWeb:   true,
})
```

### FileSystem

文件系统操作工具（读写、搜索、编辑）：

```go
// 自动注册到 DefaultToolkit(EnableFS=true)
// 工具名: "filesystem"
// 支持: read_file, write_file, edit_file, search_directory
```

### Shell

Shell 命令执行工具（白名单、超时）：

```go
// 自动注册到 DefaultToolkit(EnableShell=true)
// 工具名: "shell"
// 支持: 命令白名单、超时控制、工作目录限制
```

### Web

HTTP 请求工具：

```go
// 自动注册到 DefaultToolkit(EnableWeb=true)
// 工具名: "web"
// 支持: GET / POST / PUT / DELETE
```

### KnowledgeSearch

知识库搜索工具：

```go
// 工具名: "knowledge_search"
// 通过 RAG Provider 检索知识库
```

### 其他内置工具

| 工具 | 说明 |
|------|------|
| `API` | REST API 调用（白名单、超时） |
| `Database` | SQL 数据库查询 |
| `CodeExecution` | 代码执行（沙箱） |

## MCP 协议集成

### MCPClient

连接外部 MCP Server：

```go
func NewMCPClient(url string) *MCPClient

func (c *MCPClient) Initialize(ctx context.Context) error
func (c *MCPClient) RegisterIntoRegistry(registry *Registry) error
```

**示例：**

```go
client := ap.NewMCPClient("http://localhost:3001/mcp")
client.Initialize(ctx)
client.RegisterIntoRegistry(toolRegistry)
```

### MCPRegistry

管理多个 MCP Server：

```go
func NewMCPRegistry() *MCPRegistry

func (r *MCPRegistry) Register(config MCPClientConfig)
func (r *MCPRegistry) StartAll(ctx context.Context) error
func (r *MCPRegistry) RegisterIntoRegistry(registry *Registry) error
```

**示例：**

```go
mcpReg := ap.NewMCPRegistry()
mcpReg.Register(ap.MCPClientConfig{
    Name:      "filesystem",
    Command:   "npx",
    Args:      []string{"@modelcontextprotocol/server-filesystem", "/tmp"},
    AutoStart: true,
})
mcpReg.StartAll(ctx)
mcpReg.RegisterIntoRegistry(toolRegistry)
```

## 插件系统

### ToolPlugin 接口

```go
type ToolPlugin interface {
    Name() string
    Version() string
    Tools() []Tool
    Init(config map[string]any) error
    Close() error
}
```

### PluginLoader

```go
func NewPluginLoader(registry *Registry) *PluginLoader

func (l *PluginLoader) Load(plugin ToolPlugin) error
func (l *PluginLoader) LoadWithConfig(plugin ToolPlugin, config map[string]any) error
```

**示例：**

```go
loader := ap.NewPluginLoader(registry)
loader.Load(jsonplugin.New())
loader.LoadWithConfig(kvplugin.New(), map[string]any{"db_path": "test.db"})
```

## 自定义工具

```go
type WeatherTool struct{}

func (t *WeatherTool) Name() string { return "get_weather" }
func (t *WeatherTool) Description() string { return "获取天气信息。参数：city (string)" }
func (t *WeatherTool) Parameters() json.RawMessage {
    return json.RawMessage(`{
        "type": "object",
        "properties": {
            "city": {"type": "string", "description": "城市名称"}
        },
        "required": ["city"]
    }`)
}
func (t *WeatherTool) Execute(ctx context.Context, args json.RawMessage) (*tools.Result, error) {
    var params struct{ City string `json:"city"` }
    if err := json.Unmarshal(args, &params); err != nil {
        return nil, err
    }
    return tools.NewResult(fmt.Sprintf("%s: 晴, 25°C", params.City)), nil
}
```

## 官方插件

| 插件 | 分类 | 工具 | 说明 |
|------|------|------|------|
| http | network | `http_client` | HTTP 客户端封装 |
| sql | database | `sqlite_processor` | SQLite 数据处理 |
| git | vcs | `git_tool` | Git 版本控制 |
| json | data | `json_processor` + `csv_processor` | JSON/CSV 处理 |
| email | communication | `email_sender` | 邮件发送 |
| kv | database | `kv_store` | 键值存储 |

## 错误定义

```go
var (
    ErrInvalidConfig = errors.New("invalid configuration")
    ErrToolNotFound  = errors.New("tool not found")
    ErrToolExecution = errors.New("tool execution failed")
    ErrConfirmDenied = errors.New("tool confirmation denied")
)
```

## 完整示例

=== "Go"

    ```go
    package main

    import (
        "context"
        "fmt"
        "log"
        "os"

        ap "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg"
    )

    func main() {
        // 创建工具包
        registry, _ := ap.DefaultToolkit(ap.ToolkitConfig{
            RootDir:     ".",
            EnableFS:    true,
            EnableShell: true,
        })

        // 注册自定义工具
        registry.Register(&WeatherTool{})

        // 创建 Agent
        provider := ap.NewOpenAIProvider(ap.Config{
            APIKey: os.Getenv("OPENAI_API_KEY"),
            Model:  "gpt-4o",
        })
        agent, _ := ap.NewAgent("tool-agent", "你是助手", provider,
            ap.WithMaxTurns(10),
            ap.WithToolkit(registry),
        )

        resp, err := agent.Run(context.Background(), ap.UserMessage("北京天气怎么样？"))
        if err != nil {
            log.Fatal(err)
        }
        fmt.Println(resp.Content)
    }
    ```

=== "TypeScript"

    ```typescript
    import { ReActAgent, OpenAIProvider, ToolRegistry, FileSystemTool, ShellTool } from '@agentprimordia/sdk';

    const registry = new ToolRegistry();
    registry.register(new FileSystemTool({ rootDir: '.' }));
    registry.register(new ShellTool({ allowedCommands: ['ls', 'cat'] }));

    const agent = new ReActAgent({
      name: 'tool-agent',
      systemPrompt: '你是助手',
      model: new OpenAIProvider({ apiKey: process.env.OPENAI_API_KEY!, model: 'gpt-4o' }),
      maxTurns: 10,
      toolkit: registry,
    });

    const resp = await agent.run('当前目录有哪些文件？');
    console.log(resp.content);
    ```
