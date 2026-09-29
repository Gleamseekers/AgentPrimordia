# 📖 API 完整参考

本文档提供 AgentPrimordia 所有公共 API 的详细说明。

## 目录

- [Agent 模块](#agent-模块)
  - [ReActLoop](#reactloop)
  - [配置选项](#配置选项)
  - [生命周期管理](#生命周期管理)
- [Pool 多Agent调度](#pool-多agent调度)
  - [创建与配置](#创建与配置)
  - [任务分发](#任务分发)
  - [状态查询](#状态查询)
- [LLM Provider](#llm-provider)
  - [OpenAI Provider](#openai-provider)
  - [Gemini Provider](#gemini-provider)
  - [Qwen Provider](#qwen-provider)
  - [ResilientProvider](#resilientprovider)
- [工具系统](#工具系统)
  - [内置工具](#内置工具)
  - [自定义工具](#自定义工具)
  - [MCP Server](#mcp-server)
  - [MCP Client](#mcp-client)
  - [MCP Registry](#mcp-registry)
- [Memory 系统](#memory-系统)
  - [Memory 接口](#memory-接口)
  - [后端实现](#后端实现)
  - [pgvector Provider](#pgvector-provider)
- [Agent 发现服务](#agent-发现服务)
  - [HTTP API](#发现服务-http-api)
  - [认证](#发现服务认证)
- [跨进程通信](#跨进程通信)
  - [Transport 接口](#transport-接口)
  - [HTTP Transport](#http-transport)
  - [TCP Transport](#tcp-transport)
  - [消息总线](#消息总线)
- [A2A 协议](#a2a-协议)
  - [创建 A2A Server](#创建-a2a-server)
  - [HTTP API](#a2a-http-api)
  - [JSON-RPC 方法](#a2a-json-rpc-方法)
  - [gRPC API](#a2a-grpc-api)
  - [SSE 事件流](#a2a-sse-事件流)
  - [认证](#a2a-认证)
- [Admin 管理面板](#admin-管理面板)
- [Metrics 指标服务](#metrics-指标服务)
- [调试系统](#调试系统)
- [CLI 工具](#cli-工具)
- [K8s Operator](#k8s-operator)
- [Grafana Dashboard](#grafana-dashboard)
- [插件系统](#插件系统)
- [Benchmark 套件](#benchmark-套件)
- [错误处理](#错误处理)
- [并发安全](#并发安全)

---

## Agent 模块

### ReActLoop

ReActLoop 是 Agent 的核心引擎，实现了 **Reason → Act → Observe** 循环。

#### 创建 Agent

```go
import ap "github.com/Gleamseekers/AgentPrimordia/agentprimordia/pkg"

// 推荐入口（v0.7.0 起）
agent, err := ap.NewAgent("MyAgent", "你是一个专业的AI助手", llmProvider,
    ap.WithMaxTurns(50),
)

// 链式 API（CapabilityAgent）
agent, err := ap.NewAgent("MyAgent", "你是一个专业的AI助手", llmProvider,
    ap.WithMaxTurns(50),
)
agent.WithToolkit(tools).WithMemory(mem)
```

#### ReActConfig 配置项

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `Name` | string | ✅ | - | Agent 名称，用于日志和标识 |
| `SystemPrompt` | string | ❌ | "" | 系统提示词，定义 Agent 行为 |
| `Model` | llm.Provider | ✅ | - | LLM Provider 实现 |
| `MaxTurns` | int | ❌ | 50 | 最大推理轮数 |
| `Temperature` | float64 | ❌ | 0 | 生成温度 |
| `SessionID` | string | ❌ | "" | 会话 ID，用于跨轮记忆关联 |
| `PromptTemplate` | *PromptTemplate | ❌ | nil | 提示词模板 |
| `Lifecycle` | *Lifecycle | ❌ | 自动创建 | 生命周期管理器 |
| `Logger` | *slog.Logger | ❌ | slog.Default() | 日志 |

> **注意：** 工具、记忆等能力通过链式 API `WithToolkit()` / `WithMemory()` 注入，不再通过 ReActConfig 字段配置（已废弃）。

#### 运行 Agent

```go
// 同步运行
resp, err := agent.Run(ctx, ap.UserMessage("你好"))

// 流式运行（逐 token 返回）
stream, err := agent.StreamRun(ctx, ap.UserMessage("解释量子计算"))
for event := range stream {
    if event.Type == ap.StreamEventToken {
        fmt.Print(event.Content)
    }
}
```

#### 响应结构

```go
type Response struct {
    RequestID string     // 请求 ID
    Content   string     // 最终回复内容
    ToolCalls []ToolCall // 工具调用记录
    Usage     Usage      // Token 用量
    Metrics   Metrics    // 执行指标
    Error     error      // 错误（如有）
}

type Usage struct {
    PromptTokens     int // 提示 Token 数
    CompletionTokens int // 生成 Token 数
    TotalTokens      int // 总 Token 数
}

type Metrics struct {
    TotalTurns  int           // 总轮数
    Duration    time.Duration // 执行时长
    ToolsCalled map[string]int // 工具调用次数
}
```

### 生命周期管理

```go
// 停止 Agent
agent.Stop()

// 获取状态
stats := agent.Stats()
fmt.Printf("状态: %s, 已完成 %d 轮\n", stats.Status, stats.Turns)

// 暂停 / 恢复
agent.Pause()
agent.Resume()

// 优雅关闭
agent.GracefulShutdown(ctx)

// 从检查点恢复
agent.ResumeFromCheckpoint(ctx)
```

---

## Pool 多Agent调度

Pool 管理多个 Agent 的并发执行，提供任务分发、状态查询和资源限制。

### 创建与配置

```go
import ap "github.com/Gleamseekers/AgentPrimordia/agentprimordia/pkg"

p := ap.NewPool(ap.PoolConfig{
    MaxConcurrency: 10,  // 最大并发 Agent 数
    Timeout:        60 * time.Second,
    DefaultAgent: ap.ReActAgentConfig{
        SystemPrompt: "你是任务处理助手",
        MaxTurns:     10,
    },
})
p.SetModel(llmProvider)  // 设置 LLM Provider
defer p.Close()
```

### 任务分发

```go
tasks := []pool.TaskConfig{
    {ID: "task-1", Title: "翻译文档", Prompt: "将以下内容翻译为英文..."},
    {ID: "task-2", Title: "代码审查", Prompt: "审查以下代码..."},
}

results, err := p.Dispatch(ctx, tasks)
for _, r := range results {
    fmt.Printf("任务 %s: %s\n", r.TaskID, r.Status)
}
```

### 状态查询

```go
// 列出所有 Agent
agents := p.ListAgents()  // map[string]string

// 获取统计信息
stats := p.Stats()
// PoolStats{TotalTasks, CompletedTasks, FailedTasks, RunningTasks, QueuedTasks, MaxConcurrency, ActiveConcurrency}

// 列出所有任务
tasks := p.ListTasks()  // []TaskResult

// 查询单个任务
result, ok := p.GetTask("task-1")
```

---

## LLM Provider

### OpenAI Provider

支持 GPT-4o、GPT-4o-mini 及所有 OpenAI 兼容 API。

```go
import "github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"

// 标准配置
provider, err := llm.NewOpenAIProvider(llm.Config{
    APIKey:      "sk-...",
    Model:       "gpt-4o",
    BaseURL:     "https://api.openai.com/v1",  // 可选，默认官方地址
    Temperature: 0.7,                            // 可选
    MaxTokens:   4096,                           // 可选
})

// 调用 Complete
resp, err := provider.Complete(ctx, &llm.CompletionRequest{
    Messages: []llm.ChatMessage{
        {Role: "user", Content: "你好"},
    },
})

// 调用 CallTools (函数调用)
toolResp, err := provider.CallTools(ctx, &llm.ToolCallRequest{
    Messages: messages,
    Tools:    toolDefinitions,
})
```

**兼容第三方 API**：

```go
// DeepSeek
provider, _ = llm.NewOpenAIProvider(llm.Config{
    APIKey:  "sk-...",
    Model:   "deepseek-chat",
    BaseURL: "https://api.deepseek.com/v1",
})

// 本地 Ollama
provider, _ = llm.NewOpenAIProvider(llm.Config{
    APIKey:  "ollama",  // Ollama 不需要真实 Key
    Model:   "llama3",
    BaseURL: "http://localhost:11434/v1",
})
```

### Gemini Provider

Google Gemini 多模态支持。

```go
provider, _ = llm.NewGeminiMultimodalProvider(llm.Config{
    APIKey: "your-gemini-api-key",
    Model:  "gemini-2.0-flash",
})

// 多模态输入（文本 + 图片）
resp, _ = provider.Complete(ctx, &llm.CompletionRequestExt{
    Messages: []llm.MultimodalMessage{
        {
            Role: "user",
            Parts: []llm.ContentPart{
                {Text: "描述这张图片"},
                {InlineData: imageBytes, MimeType: "image/jpeg"},
            },
        },
    },
})
```

### Qwen Provider

阿里云通义千问 API 支持。

```go
provider, _ = llm.NewQwenProvider(llm.Config{
    APIKey:  "sk-...",
    BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
    Model:   "qwen-plus",
    // 或 qwen-turbo, qwen-max, qwen-long 等
})
```

**支持的模型**：
- `qwen-plus` - 性价比之选
- `qwen-max` - 最强性能
- `qwen-turbo` - 快速响应
- `qwen-long` - 长文本（100K+ tokens）

### ResilientProvider

弹性调用包装器，自动重试、降级和熔断。

```go
primary := llm.NewOpenAIProvider(llm.Config{...})
fallback := llm.NewQwenProvider(llm.Config{...})

resilient := llm.NewResilientProvider(primary, llm.DefaultResilientConfig())
resilient.AddFallback(fallback)  // 主模型失败时自动切换

// 自定义配置
customConfig := &llm.ResilientConfig{
    MaxRetries:          5,           // 最大重试次数
    InitialBackoff:      1 * time.Second,  // 初始退避时间
    MaxBackoff:          30 * time.Second, // 最大退避时间
    CircuitBreakerThreshold: 5,         // 熔断阈值（连续失败次数）
    CircuitBreakerTimeout:   60 * time.Second, // 熔断恢复时间
}
```

---

## 工具系统

### 内置工具

#### 1. FileSystem 文件系统工具

```go
fs := builtin.NewFileSystem("./workspace")

// 能力列表:
// - read: 读取文件内容
// - write: 写入文件
// - edit: 编辑文件（搜索替换）
// - list: 列出目录内容
// - search: 在目录中搜索文件
// - info: 获取文件信息
```

**安全特性**：
- ✅ 路径穿越检测（防止 `../../../etc/passwd`）
- ✅ 敏感文件保护（`.env`, `.ssh/` 等）
- ✅ 文件大小限制（默认 10MB）
- ✅ 可配置的 ScopePolicy 权限控制

#### 2. Shell 命令执行工具

```go
shell := builtin.NewShell()
shell.WithTimeout(30 * time.Second)  // 设置超时
shell.WithWhitelist([]string{"ls", "cat", "grep"})  // 白名单模式

// 安全特性:
// - 命令白名单/黑名单
// - 危险命令检测（rm -rf / 等）
// - 输出大小限制（默认 50KB）
// - 可选的工作目录限制
```

#### 3. Web HTTP 工具

```go
web := builtin.NewWeb()

// 能力:
// - GET/POST 请求
// - 自定义 Headers
// - 超时控制（默认 10s）
// - 响应体大小限制（默认 5MB）
```

#### 4. Calculator 计算器工具

```go
calc := builtin.NewCalculator()

// 支持的操作:
// - add: 加法
// - subtract: 减法
// - multiply: 乘法
// - divide: 除法（含除零保护）

// 示例调用:
result, _ := calc.Execute(ctx, json.RawMessage(`{
    "operation": "add",
    "a": 10,
    "b": 20
}`))
// result.Content: "30.00"
```

#### 5. DateTime 日期时间工具

```go
dt := builtin.NewDateTime()

// 功能:
// - now: 获取当前时间
// - format: 格式化日期

// 支持的格式预设:
// - RFC3339: "2026-05-30T16:39:02+08:00"
// - ISO8601: "2026-05-30T16:39:02Z"
// - simple: "2026-05-30 16:39:02"
// - date: "2026-05-30"
// - time: "16:39:02"
```

### 自定义工具

只需实现 4 个方法即可创建自定义工具：

```go
type MyCustomTool struct{}

func (t *MyCustomTool) Name() string {
    return "my_tool"  // 工具名称（唯一标识）
}

func (t *MyCustomTool) Description() string {
    return "这是我的自定义工具"  // 用于 LLM 理解工具用途
}

func (t *MyCustomTool) Parameters() json.RawMessage {
    return json.RawMessage(`{
        "type": "object",
        "properties": {
            "param1": {"type": "string", "description": "参数1说明"},
            "param2": {"type": "number", "description": "参数2说明"}
        },
        "required": ["param1"]
    }`)
}

func (t *MyCustomTool) Execute(ctx context.Context, args json.RawMessage) (*tools.Result, error) {
    // 解析参数
    var params struct {
        Param1 string  `json:"param1"`
        Param2 float64 `json:"param2"`
    }
    if err := json.Unmarshal(args, &params); err != nil {
        return tools.NewErrorResult("参数解析失败"), nil
    }

    // 执行业务逻辑
    result := doSomething(params.Param1, params.Param2)

    return tools.NewResult(result), nil
}

// 注册到工具集
registry := tools.NewRegistry()
registry.Register(&MyCustomTool{})
```

### MCP Server

MCP (Model Context Protocol) Server 提供标准化的工具、资源和提示词服务。

#### 创建 MCP Server

```go
import "github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools"

server := tools.NewMCPServer(tools.MCPServerConfig{
    Name:    "my-mcp-server",
    Version: "1.0.0",
    APIKey:  "optional-api-key",  // 可选，设置后所有请求需携带 Bearer Token
}, registry)

// 添加资源
server.AddResource(tools.MCPResourceDefinition{
    URI:         "file:///data/config.json",
    Name:        "配置文件",
    Description: "应用配置",
    MimeType:    "application/json",
})

// 添加提示词模板
server.AddPrompt(tools.MCPPromptDefinition{
    Name:        "code-review",
    Description: "代码审查提示词",
    Arguments: []tools.MCPPromptArgument{
        {Name: "language", Description: "编程语言", Required: true},
    },
})

// 设置资源读取处理器
server.SetResourceHandler(func(uri string) (*tools.MCPResourceContent, error) {
    data, err := os.ReadFile(uriToPath(uri))
    if err != nil {
        return nil, err
    }
    return &tools.MCPResourceContent{URI: uri, Text: string(data)}, nil
})

// 设置提示词处理器
server.SetPromptHandler(func(name string, args map[string]string) ([]tools.MCPPromptMessage, error) {
    return []tools.MCPPromptMessage{
        {Role: "user", Content: tools.MCPPromptContent{Type: "text", Text: "审查以下代码..."}},
    }, nil
})

// 挂载到 HTTP 路由
mux := http.NewServeMux()
mux.HandleFunc("/mcp", server.ServeHTTP)
```

#### MCP JSON-RPC 方法

所有请求通过 `POST` 发送，Content-Type 为 `application/json`，遵循 JSON-RPC 2.0 规范。

**请求格式**：

```json
{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "initialize",
    "params": {}
}
```

**方法列表**：

| 方法 | 说明 | 参数 |
|------|------|------|
| `initialize` | 初始化连接，交换能力信息 | `{capabilities, clientInfo}` |
| `notifications/initialized` | 客户端初始化完成通知 | 无（返回 200 空 body） |
| `tools/list` | 列出可用工具 | 无 |
| `tools/call` | 调用指定工具 | `{name, arguments}` |
| `resources/list` | 列出可用资源 | 无 |
| `resources/read` | 读取指定资源 | `{uri}` |
| `prompts/list` | 列出可用提示词模板 | 无 |
| `prompts/get` | 获取指定提示词模板 | `{name, arguments}` |
| `ping` | 心跳检测 | 无 |

**错误码**：

| 错误码 | 含义 |
|--------|------|
| `-32700` | Parse error（JSON 解析失败） |
| `-32600` | Invalid Request（`jsonrpc` 字段必须为 `"2.0"`） |
| `-32601` | Method not found |
| `-32602` | Invalid params |
| `-32603` | Internal error |
| `-32001` | 认证失败（API Key 无效或缺失） |

**请求体限制**：最大 1MB。

### MCP Client

MCP Client 用于连接外部 MCP Server 并调用其工具。

```go
import "github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools"

// 连接已运行的 MCP Server
client := tools.NewMCPClient("http://localhost:3000/mcp")

// 初始化连接
if err := client.Initialize(ctx); err != nil {
    log.Fatal(err)
}

// 列出可用工具
toolDefs := client.Tools()
for _, t := range toolDefs {
    fmt.Printf("工具: %s — %s\n", t.Name, t.Description)
}

// 调用工具
result, err := client.CallTool(ctx, "read_file", map[string]any{
    "path": "/data/config.json",
})

// 将 MCP Server 工具注册到 ToolRegistry
if err := client.RegisterIntoRegistry(registry); err != nil {
    log.Fatal(err)
}

// 关闭连接
client.Close()
```

### MCP Registry

MCP Registry 管理多个 MCP Server 的注册、启动和工具发现，支持从配置文件自动加载。

```go
import "github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools"

registry := tools.NewMCPRegistry()

// 注册 MCP Server（通过 URL 连接）
registry.Register(tools.MCPClientConfig{
    Name:    "filesystem",
    BaseURL: "http://localhost:3001/mcp",
})

// 注册 MCP Server（通过命令启动）
registry.Register(tools.MCPClientConfig{
    Name:      "github",
    Command:   "npx",
    Args:      []string{"@modelcontextprotocol/server-github"},
    Env:       map[string]string{"GITHUB_TOKEN": "ghp_xxx"},
    AutoStart: true,
})

// 从配置文件批量加载
registry.LoadFromConfig(".ap.json")

// 启动所有 AutoStart=true 的 Server
if err := registry.StartAll(ctx); err != nil {
    log.Fatal(err)
}
defer registry.StopAll()

// 启动单个 Server
registry.Start(ctx, "filesystem")

// 将所有运行中 Server 的工具注册到 ToolRegistry
registry.RegisterIntoRegistry(toolRegistry)

// 测试连通性
if err := registry.Test(ctx, "filesystem"); err != nil {
    fmt.Printf("连接失败: %v\n", err)
}

// 列出所有已注册 Server
for _, entry := range registry.List() {
    fmt.Printf("%s: %s\n", entry.Config.Name, entry.Status)
}
```

#### MCPClientConfig 配置项

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `Name` | string | ✅ | Server 名称，用于标识 |
| `Command` | string | ❌ | 启动命令（如 `npx`），与 BaseURL 二选一 |
| `Args` | []string | ❌ | 命令参数 |
| `Env` | map[string]string | ❌ | 环境变量 |
| `AutoStart` | bool | ❌ | Agent 启动时自动拉起（默认 false） |
| `BaseURL` | string | ❌ | 已运行 Server 的 URL，设置后跳过启动 |

---

## Memory 系统

### Memory 接口

```go
type Memory interface {
    Add(ctx, episode) error                    // 添加记忆
    Search(ctx, query, opts) ([]*Episode, error)  // 搜索记忆
    Get(ctx, id) (*Episode, error)             // 获取单条记忆
    Delete(ctx, id) error                      // 删除记忆
    Count(ctx, sessionID) (int64, error)       // 统计数量
    UpdateSummary(ctx, id, summary, topics) error  // 更新摘要
    SetImportance(ctx, id, importance) error   // 设置重要性
    CleanupExpired(ctx, maxAgeDays) (int64, error)  // 清理过期记忆
    Stats(ctx) (*MemoryStats, error)           // 统计信息
    Close() error                              // 关闭连接
}
```

### 后端实现

#### SQLite 后端（推荐生产环境使用）

```go
store, err := memory.NewSQLiteStore("./my_memory.db")
defer store.Close()

// 特性:
// ✅ 持久化存储
// ✅ FTS5 全文搜索
// ✅ 向量检索（RAG）
// ✅ 自动清理过期数据
// ✅ 导入/导出功能（JSON/Markdown）
```

#### InMemory 后端（推荐测试环境使用）

```go
store := memory.NewInMemoryStore()
defer store.Close()

// 特性:
// ⚡ 零延迟
// 🧪 适合单元测试
// 💾 进程退出后数据丢失
```

#### pgvector 后端（推荐已有 PostgreSQL 的场景）

基于 PostgreSQL + pgvector 扩展的向量存储，支持余弦相似度搜索。

**独立模块**：pgvector 依赖 PostgreSQL 驱动（pgx），作为独立 Go Module 提供，不污染主框架零依赖原则。

```go
import pgv "github.com/Gleamseekers/AgentPrimordia/pgvector"

client, err := pgv.NewClient(pgv.Config{
    Host:       "localhost",
    Port:       5432,
    Database:   "mydb",
    User:       "postgres",
    Password:   "password",
    TableName:  "ap_vectors",
    VectorSize: 1536,
    SSLMode:    "disable",
})
if err != nil {
    log.Fatal(err)
}
defer client.Close()

// 插入向量
client.Insert(ctx, "doc-1", []float32{0.1, 0.2, ...}, "文档内容", map[string]any{"source": "wiki"})

// 批量插入
client.BatchInsert(ctx, ids, vectors, texts, metadatas)

// 相似度搜索
results, _ := client.Search(ctx, queryVector, 10, 0.5)
for _, r := range results {
    fmt.Printf("ID=%s Score=%.3f Text=%s\n", r.ID, r.Score, r.Text)
}

// 获取记录
entry, _ := client.Get(ctx, "doc-1")

// 删除记录
client.Delete(ctx, "doc-1", "doc-2")

// 统计
count, _ := client.Count(ctx)

// 健康检查
client.HealthCheck(ctx)
```

#### pgvector.Config 配置项

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `Host` | string | ❌ | localhost | PostgreSQL 主机 |
| `Port` | int | ❌ | 5432 | 端口 |
| `Database` | string | ✅ | - | 数据库名 |
| `User` | string | ✅ | - | 用户名 |
| `Password` | string | ❌ | - | 密码 |
| `TableName` | string | ❌ | ap_vectors | 向量表名 |
| `VectorSize` | int | ❌ | 1536 | 向量维度 |
| `SSLMode` | string | ❌ | disable | SSL 模式 |

#### 使用工厂函数创建

```go
// 方式1: SQLite
mem, _ := memory.NewMemory(memory.Config{
    Type: memory.BackendSQLite,
    Path: "./production.db",
})

// 方式2: 内存
mem, _ := memory.NewMemory(memory.Config{
    Type: memory.BackendMemory,
})
```

### Episode 结构

```go
type Episode struct {
    ID         string            `json:"id"`          // UUID
    SessionID  string            `json:"session_id"`  // 会话ID
    Role       string            `json:"role"`        // user/assistant/system
    Content    string            `json:"content"`     // 内容
    Summary    string            `json:"summary"`     // AI生成的摘要
    Topics     string            `json:"topics"`      // 关键词标签
    Importance float64           `json:"importance"`  // 重要性评分(0-1)
    Metadata   map[string]string `json:"metadata"`    // 元数据
    CreatedAt  string            `json:"created_at"`  // 创建时间
}
```

### 高级功能

#### 语义搜索（RAG）

```go
import "github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/memory"

ragStore := memory.NewRAGStore(store)

// 添加嵌入模型
ragStore.WithEmbedder(myEmbedder)  // 需要实现 Embedder 接口

// 混合搜索（关键词 + 语义向量）
results, _ := ragStore.Query(&memory.RAGQuery{
    Query:          "用户的问题",
    TopK:           5,
    SemanticWeight: 0.3,  // 语义权重（0-1）
    KeywordWeight:  0.7,  // 关键词权重
})
```

#### 时间线视图

```go
timeline, _ := store.GetMemoryTimeline(ctx, 7)  // 最近7天
for _, group := range timeline {
    fmt.Printf("%s: %d 条记忆\n", group.Date, group.Count)
    for _, ep := range group.Episodes {
        fmt.Printf("  - [%s] %s\n", ep.Role, truncate(ep.Summary, 50))
    }
}
```

#### 导入/导出

```go
// 导出为 JSON
data, _ := store.ExportMemories(ctx, "", "json")
err := os.WriteFile("backup.json", data, 0644)

// 从 JSON 导入
data, _ := os.ReadFile("backup.json")
count, _ := store.ImportMemories(ctx, data, "json")
fmt.Printf("导入了 %d 条记忆\n", count)
```

---

## Agent 发现服务

Discovery Server 提供 Agent 注册、发现和心跳管理。

### 创建 Discovery Server

```go
import "github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent"

localDiscovery := agent.NewLocalDiscovery()
server := agent.NewDiscoveryServer(localDiscovery)

// 可选：设置 API Key 保护写操作
server = server.WithAPIKey("your-secret-key")

// 启动 HTTP 服务
go server.Start(":8081")
defer server.Close()
```

### 发现服务 HTTP API

#### 注册 Agent

```
POST /api/discovery/register
Content-Type: application/json
Authorization: Bearer <api-key>  （如果配置了 APIKey）
```

请求体：

```json
{
    "id": "agent-001",
    "name": "翻译Agent",
    "address": "192.168.1.100:9090",
    "capabilities": ["translate", "summarize"],
    "metadata": {"version": "2.0"}
}
```

**验证规则**：
- `id`：必填，1-256 字符
- `name`：必填，1-256 字符
- `address`：可选，最大 1024 字符
- 请求体最大 1MB

**响应**：

| 状态码 | 说明 |
|--------|------|
| 200 | 注册成功 |
| 400 | 参数验证失败（返回 JSON `{"error":"...", "detail":"..."}`） |
| 401 | 未认证或 API Key 无效 |

#### 注销 Agent

```
DELETE /api/discovery/{id}
Authorization: Bearer <api-key>
```

| 状态码 | 说明 |
|--------|------|
| 200 | 注销成功 |
| 400 | 缺少 agent id |
| 401 | 未认证 |
| 500 | 注销失败 |

#### 发现指定 Agent

```
GET /api/discovery/{id}
```

| 状态码 | 说明 |
|--------|------|
| 200 | 返回 AgentInfo JSON |
| 400 | 缺少 agent id |
| 404 | Agent 未找到 |

> 读操作无需认证。

#### 列出所有 Agent

```
GET /api/discovery/agents
```

返回 `AgentInfo` JSON 数组。读操作无需认证。

#### 心跳

```
POST /api/discovery/{id}/heartbeat
Authorization: Bearer <api-key>
```

| 状态码 | 说明 |
|--------|------|
| 200 | 心跳成功 |
| 401 | 未认证 |
| 404 | Agent 未找到 |

### 发现服务认证

当通过 `WithAPIKey(key)` 配置 API Key 后：
- **写操作**（register、unregister、heartbeat）需要 `Authorization: Bearer <key>` 请求头
- **读操作**（list、discover）无需认证
- 未配置 APIKey 时，所有操作均无需认证

也可通过实现 `Authenticator` 接口自定义认证逻辑：

```go
type Authenticator interface {
    Authenticate(r *http.Request) (*Principal, error)
    GenerateToken(principal *Principal) (string, error)
}
```

---

## 跨进程通信

### Transport 接口

```go
type Transport interface {
    Send(ctx context.Context, target string, msg *BusMessage) error
    Receive() <-chan *BusMessage
    Start(addr string) error
    Close() error
}
```

### 消息总线

```go
// BusMessage 是跨进程通信的消息结构
type BusMessage struct {
    ID        string            `json:"id"`        // 消息唯一标识
    From      string            `json:"from"`      // 发送方标识
    To        string            `json:"to"`        // 接收方标识
    Type      string            `json:"type"`      // 消息类型
    Content   string            `json:"content"`   // 消息内容
    Metadata  map[string]string `json:"metadata"`  // 元数据
    Timestamp time.Time         `json:"timestamp"` // 时间戳
}

// 预定义消息类型
BusMsgTaskRequest  = "task_request"
BusMsgTaskResult   = "task_result"
BusMsgHeartbeat    = "heartbeat"
BusMsgRegistration = "registration"
```

### HTTP Transport

基于 HTTP 的跨进程消息传输。

#### 创建与使用

```go
transport := agent.NewHTTPTransport()

// 启动服务
if err := transport.Start(":8090"); err != nil {
    log.Fatal(err)
}
defer transport.Close()

// 发送消息
msg := &agent.BusMessage{
    ID:        "msg-1",
    From:      "agent-A",
    To:        "agent-B",
    Type:      agent.BusMsgTaskRequest,
    Content:   "执行任务",
    Timestamp: time.Now(),
}
err := transport.Send(ctx, "192.168.1.100:8090", msg)

// 接收消息
for msg := range transport.Receive() {
    fmt.Printf("收到消息: %s from %s\n", msg.ID, msg.From)
}
```

#### HTTP Transport API

```
POST /api/message
Content-Type: application/json
```

请求体为 `BusMessage` JSON。

| 状态码 | 说明 |
|--------|------|
| 200 | 消息接收成功 |
| 400 | 无效 JSON 或空消息体 |
| 405 | 非 POST 方法 |
| 503 | 入站通道已满（缓冲区 64 条） |

#### TLS 支持

```go
transport := agent.NewHTTPTransport().WithTLS(&tls.Config{
    Certificates: []tls.Certificate{cert},
})
```

### TCP Transport

基于 TCP 的跨进程消息传输，支持 ACK 确认和连接池。

#### 创建与使用

```go
// 使用默认配置
transport := agent.NewTCPTransport()

// 或自定义配置
transport := agent.NewTCPTransportWithConfig(agent.TCPTransportConfig{
    AckTimeout:    10 * time.Second,   // ACK 超时
    MaxRetries:    3,                  // 最大重试次数
    RetryInterval: 500 * time.Millisecond, // 重试间隔
    PoolSize:      8,                  // 连接池大小
})

if err := transport.Start(":8091"); err != nil {
    log.Fatal(err)
}
defer transport.Close()
```

#### ACK 确认机制

```go
// 发送并等待 ACK 确认
msg := &agent.BusMessage{
    ID:      "msg-ack-1",
    From:    "agent-A",
    To:      "agent-B",
    Type:    agent.BusMsgTaskRequest,
    Content: "重要任务",
}
err := transport.SendWithAck(ctx, "192.168.1.100:8091", msg)
// 如果接收方成功处理消息并回传 ACK，SendWithAck 返回 nil
// 如果超时未收到 ACK，返回错误
```

ACK 工作流程：
1. 发送方通过 TCP 连接发送 JSON 编码的 `BusMessage`
2. 接收方 `handleConn` 解码消息后，在同一连接上回传 ACK 响应
3. 发送方 `readAckResponse` 读取 ACK 并完成确认

#### 连接池

```go
active, idle := transport.PoolStats()
fmt.Printf("活跃连接: %d, 空闲连接: %d\n", active, idle)
```

---

## A2A 协议

A2A (Agent-to-Agent) 协议实现 Google A2A 规范，支持跨 Agent 任务协作。

### 创建 A2A Server

推荐使用公共包 `agentprimordia/pkg/ap`：

```go
import ap "github.com/Gleamseekers/AgentPrimordia/agentprimordia/pkg"

tm := ap.NewA2ATaskManager()
defer tm.Cleanup()

card := ap.NewA2AAgentCard("my-agent", "My Agent")
service := ap.NewA2AService(card, tm)

// 推荐：gRPC Server（v1.x 起为默认传输）
grpcServer := ap.NewA2AGRPCServerWithService(service,
    ap.WithGRPCLogger(slog.Default()),
)
```

### A2A HTTP API

#### 获取 Agent Card

```
GET /
```

返回 `AgentCard` JSON，描述 Agent 的能力、端点和安全方案。

**响应示例**：

```json
{
    "protocol": "a2a/v1",
    "agent_id": "my-agent",
    "name": "My Agent",
    "description": "",
    "capabilities": {"streaming": false, "push_notifications": false},
    "endpoints": {"jsonrpc": "/"},
    "security_schemes": [],
    "skills": [],
    "metadata": {}
}
```

#### JSON-RPC 请求

```
POST /
Content-Type: application/json
```

遵循 JSON-RPC 2.0 规范。`jsonrpc` 字段必须为 `"2.0"`。

**请求格式**：

```json
{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "task/create",
    "params": {
        "message": {
            "role": "user",
            "parts": [{"type": "text", "text": "帮我分析数据"}]
        }
    }
}
```

### A2A JSON-RPC 方法

#### task/create — 创建任务

```json
{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "task/create",
    "params": {
        "message": {
            "role": "user",
            "parts": [{"type": "text", "text": "帮我分析数据"}],
            "message_id": "msg-1"
        },
        "task_id": "optional-custom-id",
        "session_id": "optional-session-id"
    }
}
```

**响应**：返回完整 Task 对象，初始状态为 `submitted`。

#### task/get — 获取任务

```json
{
    "jsonrpc": "2.0",
    "id": 2,
    "method": "task/get",
    "params": {
        "id": "task-xxx"
    }
}
```

**错误码**：
- `-32602`：缺少 id 参数
- `-32002`：任务未找到

#### task/cancel — 取消任务

```json
{
    "jsonrpc": "2.0",
    "id": 3,
    "method": "task/cancel",
    "params": {
        "id": "task-xxx"
    }
}
```

**响应**：返回更新后的完整 `Task` 对象，状态为 `canceled`。

**错误码**：
- `-32602`：缺少 id 参数
- `-32002`：任务未找到
- `-32003`：任务冲突（终态任务无法取消：completed/failed/canceled/rejected）

### A2A gRPC API

A2A 同时提供基于 protobuf/gRPC 的二进制传输，消息定义位于 `internal/agent/a2a/proto/a2a/v1/a2a.proto`。

#### 启动 gRPC Server

```go
import ap "github.com/Gleamseekers/AgentPrimordia/agentprimordia/pkg"

service := ap.NewA2AService(card, tm)
grpcServer := ap.NewA2AGRPCServerWithService(service)

lis, _ := net.Listen("tcp", ":8083")
grpcServer.Serve(lis)
```

#### gRPC 认证拦截器

```go
auth := ap.APIKeyAuthFunc(func(ctx context.Context) (*ap.Principal, error) {
    md, ok := metadata.FromIncomingContext(ctx)
    if !ok {
        return nil, fmt.Errorf("missing metadata")
    }
    key := md.Get("x-api-key")
    if len(key) == 0 || key[0] != "secret" {
        return nil, fmt.Errorf("invalid api key")
    }
    return &ap.Principal{ID: "client-1"}, nil
})

grpcServer := ap.NewA2AGRPCServerWithService(service,
    ap.WithGRPCAuth(auth),
)
```

#### gRPC Client

```go
client, err := ap.NewA2AGRPCClient("localhost:8083")
if err != nil {
    log.Fatal(err)
}

card, err := client.FetchAgentCard(ctx)
task, err := client.CreateTask(ctx, &ap.A2AMessage{
    Role:  "user",
    Parts: []ap.Part{ap.NewTextPart("帮我分析数据")},
}, "")

task, err = client.GetTask(ctx, task.ID)
canceled, err := client.CancelTask(ctx, task.ID)

ch, err := client.StreamEvents(ctx, task.ID)
for ev := range ch {
    fmt.Printf("event: %+v\n", ev)
}
```

### A2A 任务状态机

```
submitted → working → completed
                  ↘ input-required → working → ...
                  ↘ failed
submitted → canceled
submitted → rejected
```

终态（不可变更）：`completed`、`failed`、`canceled`、`rejected`

### A2A SSE 事件流

```
GET /tasks/{id}/events
```

订阅任务状态变更事件，返回 Server-Sent Events 流。

**响应格式**：

```
Content-Type: text/event-stream
Cache-Control: no-cache
Connection: keep-alive

data: {"type":"state_change","task_id":"task-xxx","state":"working","timestamp":"..."}

data: {"type":"state_change","task_id":"task-xxx","state":"completed","timestamp":"..."}
```

- 如果 `taskID` 为空，返回 400
- 如果任务不存在，SSE 连接建立后立即推送 `type=error` 事件并关闭
- 客户端断开连接时自动清理订阅

### A2A 认证

通过 `WithAuth(authenticator)` 配置认证器：

```go
// API Key 认证
auth := a2a.NewAPIKeyAuthenticator(map[string]string{
    "client-a": "key-aaa",
    "client-b": "key-bbb",
}, "X-API-Key")

// Bearer Token 认证
auth := a2a.NewBearerTokenAuthenticator(func(token string) (*a2a.Principal, error) {
    if token == "valid-token" {
        return &a2a.Principal{
            ID:     "user-1",
            Roles:  []string{"admin"},
            Scopes: []string{"*"},
        }, nil
    }
    return nil, fmt.Errorf("invalid token")
})

// 无认证（开发环境）
auth := a2a.NewNoopAuthenticator()
```

**Principal 权限**：

```go
principal.HasScope("task:create")  // 检查权限
principal.HasRole("admin")         // 检查角色
principal.HasScope("*")            // 通配符匹配所有权限
principal.HasRole("*")             // 通配符匹配所有角色
```

**JSON-RPC 错误码**：

| 错误码 | 含义 |
|--------|------|
| `-32700` | Parse error（JSON 解析失败或 jsonrpc 版本非 "2.0"） |
| `-32601` | Method not found |
| `-32602` | Invalid params |
| `-32001` | 认证失败 |
| `-32002` | 任务未找到 |
| `-32003` | 任务冲突（如取消终态任务） |

---

## Admin 管理面板

Admin Handler 提供 Agent Pool 的 HTTP 管理接口。

### 创建

```go
import "github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/admin"

handler := admin.NewAdminHandler(pool)
// handler 实现了 http.Handler 接口
```

### Admin HTTP API

所有端点仅支持 `GET` 方法，返回 `application/json; charset=utf-8`。

#### 列出所有 Agent

```
GET /api/agents
```

返回 `map[string]string`（agent ID → 状态）。

#### 获取指定 Agent

```
GET /api/agents/{id}
```

| 状态码 | 说明 |
|--------|------|
| 200 | 返回 `{id, status}` |
| 404 | Agent 未找到 |

> 仅查询 Agent 列表，不包含 Task 信息。

#### 获取 Pool 统计

```
GET /api/stats
```

返回 `PoolStats`：

```json
{
    "total_tasks": 10,
    "completed_tasks": 8,
    "failed_tasks": 1,
    "running_tasks": 1,
    "queued_tasks": 0,
    "max_concurrency": 5,
    "active_concurrency": 1
}
```

#### 列出所有任务

```
GET /api/tasks
```

返回 `TaskResult` 数组。

#### 健康检查

```
GET /api/health
```

```json
{
    "status": "ok",
    "timestamp": "2026-05-31T10:00:00+08:00",
    "tasks": 10,
    "running": 1
}
```

#### 系统信息

```
GET /api/system
```

```json
{
    "go_version": "go1.22.0",
    "goroutines": 12,
    "cpu_count": 8,
    "memory_alloc_mb": 15.3,
    "memory_sys_mb": 42.1,
    "gc_pause_ms": 0.5
}
```

#### 管理面板首页

```
GET /
```

返回 `text/html; charset=utf-8`，含自动刷新的仪表盘页面。

---

## Metrics 指标服务

Prometheus 格式的指标导出服务。

### 创建

```go
import "github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/metrics"

m := metrics.NewMetrics()
handler := metrics.NewPrometheusHandler(m, ":9090")
go handler.Start()
defer handler.Stop(ctx)
```

### Metrics HTTP API

所有端点仅支持 `GET` 方法。

#### Prometheus 指标

```
GET /metrics
Content-Type: text/plain; version=0.0.4; charset=utf-8
```

返回 Prometheus 文本格式指标，包含：

| 指标名 | 类型 | 说明 |
|--------|------|------|
| `ap_llm_total_calls` | counter | LLM 调用总数 |
| `ap_llm_error_calls` | counter | LLM 错误调用数 |
| `ap_llm_call_duration_seconds` | histogram | LLM 调用耗时 |
| `ap_tool_total_calls` | counter | 工具调用总数 |
| `ap_tool_error_calls` | counter | 工具错误调用数 |
| `ap_tool_call_duration_seconds` | histogram | 工具调用耗时 |
| `ap_agent_total_turns` | counter | Agent 总轮数 |
| `ap_agent_turn_duration_seconds` | histogram | Agent 轮次耗时 |

#### 健康检查

```
GET /health
Content-Type: application/json
```

```json
{"status": "ok"}
```

### 记录指标

```go
m := metrics.NewMetrics()

// 记录 LLM 调用
m.RecordLLMCall(500*time.Millisecond, nil)       // 成功
m.RecordLLMCall(2*time.Second, fmt.Errorf("timeout"))  // 失败

// 记录工具调用
m.RecordToolCall(100*time.Millisecond, nil)

// 记录 Agent 轮次
m.RecordTurn(1*time.Second)
```

### 导出器

```go
// 日志导出器
logExporter := metrics.NewLogExporter()

// JSON 导出器
jsonExporter := metrics.NewJSONExporter(os.Stdout)

// 多导出器组合
multiExporter := metrics.NewMultiExporter(logExporter, jsonExporter)
```

---

## 调试系统

### HTTP 调试服务器

```go
import "github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/debugger"

debugServer := debugger.NewDebugServer(":8080")
go debugServer.Start()
defer debugServer.Close()

// 获取 Handler 用于 httptest
handler := debugServer.Handler()
```

> DebugServer 使用实例级 `http.ServeMux`，支持多实例并行，不污染全局路由。

### 调试 HTTP API

#### 调试面板首页

```
GET /
Content-Type: text/html; charset=utf-8
```

返回包含 "Agent Debugger" 标题的 HTML 仪表盘。

#### 获取调试事件

```
GET /api/events
Content-Type: application/json; charset=utf-8
```

返回 `DebugEvent` JSON 数组，最多保留最近 100 条。

```json
[
    {"type": "info", "message": "Agent 启动成功", "timestamp": "..."},
    {"type": "warning", "message": "LLM 响应超时", "timestamp": "..."}
]
```

#### 获取内存快照

```
GET /api/snapshots
Content-Type: application/json; charset=utf-8
```

返回 `MemorySnapshot` JSON 数组，最多保留最近 10 条。

### 记录调试数据

```go
// 记录不同类型的事件
debugServer.AddEvent("info", "Agent 启动成功")
debugServer.AddEvent("warning", "LLM 响应超时")
debugServer.AddEvent("error", "工具执行失败: permission denied")
debugServer.AddEvent("tool_call", "调用 calculator.add(10, 20)")

// 记录内存快照
debugServer.AddSnapshot(debugger.MemorySnapshot{
    TotalEpisodes: 1500,
    TopSessions: []debugger.SessionInfo{
        {SessionID: "sess-1", Count: 500},
    },
})
```

**容量限制**：
- 事件列表：最多 100 条，超出后淘汰最早的
- 快照列表：最多 10 条，超出后淘汰最早的

### 可视化渲染

```go
visualizer := debugger.NewVisualizer()

// 渲染 Memory 快照
snapshot := &debugger.MemorySnapshot{
    TotalEpisodes: 1500,
    // ...
}
fmt.Println(visualizer.RenderMemorySnapshot(snapshot))

// 渲染 Agent 生命周期
steps := []debugger.LifecycleStep{
    {State: "STARTED", Timestamp: time.Now(), Message: "Agent 开始运行"},
    {State: "THINKING", Timestamp: time.Now().Add(time.Second), Message: "正在思考..."},
    {State: "TOOL_CALL", Timestamp: time.Now().Add(2*time.Second), Message: "调用 shell.ls"},
    {State: "COMPLETED", Timestamp: time.Now().Add(3*time.Second), Message: "任务完成"},
}
fmt.Println(visualizer.RenderAgentLifecycle(steps))
```

---

## CLI 工具

`ap` 命令行工具提供项目初始化、运行、调试和测试功能。

### 安装

```bash
go build -o ap ./cmd/ap/
```

### ap init — 创建项目

```bash
# 基础模板（默认）
ap init my-agent

# 含工具模板
ap init my-agent --template with-tools

# 多 Agent 协作模板
ap init my-agent --template multi-agent
```

生成的项目结构：

```
my-agent/
├── main.go      — Agent 入口代码
├── go.mod       — Go 模块（依赖 agentprimordia）
└── .ap.yaml     — 项目配置
```

`.ap.yaml` 配置项：

```yaml
name: my-agent
template: basic

llm:
  provider: openai
  model: gpt-4o
  # api_key: ${OPENAI_API_KEY}

memory:
  backend: sqlite
  path: ./data/memory.db

agent:
  max_turns: 20
  system_prompt: "你是一个智能助手"
```

### ap run — 运行项目

```bash
# 编译并运行
ap run

# 发送提示
ap run --prompt "分析当前目录的代码"

# 监视模式（文件变更自动重编译）
ap run --watch
```

### ap debug — 调试服务器

```bash
# 启动调试面板（默认端口 6060）
ap debug

# 指定端口
ap debug --port 3000
```

访问 `http://localhost:6060` 查看实时推理链、工具调用、记忆搜索和性能指标。

### ap test — 运行 eval 测试

```bash
# 运行 eval 测试套件
ap test

# 详细输出
ap test --verbose
```

首次运行时自动生成 `eval_agent_test.go` 模板。

### ap mcp — 管理 MCP Server

```bash
# 列出已注册 Server
ap mcp list

# 注册新 Server（URL 模式）
ap mcp add filesystem --url http://localhost:3001/mcp

# 注册新 Server（命令模式）
ap mcp add github --command npx --args "@modelcontextprotocol/server-github" --env "GITHUB_TOKEN=ghp_xxx" --auto-start

# 测试连通性
ap mcp test filesystem

# 列出 Server 工具
ap mcp tools filesystem

# 启动/停止
ap mcp start filesystem
ap mcp stop filesystem

# 移除
ap mcp remove filesystem
```

### ap plugin — 管理插件

```bash
# 从 Go Module 安装
ap plugin install github.com/user/ap-plugin-slack

# 列出已安装插件
ap plugin list

# 创建插件项目骨架
ap plugin create ap-plugin-weather

# 移除插件
ap plugin remove github.com/user/ap-plugin-slack
```

插件必须实现 `ToolPlugin` 接口：

```go
type ToolPlugin interface {
    Name() string
    Version() string
    Tools() []Tool
    Init(config map[string]any) error
    Close() error
}
```

---

## K8s Operator

AgentPrimordia Operator 提供声明式 Agent 部署，通过 `AgentDeployment` CRD 管理。

### 安装

```bash
# 安装 CRD
kubectl apply -f internal/k8s/manifest/crd.yaml

# 部署 Operator
kubectl apply -f internal/k8s/manifest/controller.yaml
```

### AgentDeployment CRD

```yaml
apiVersion: agent.primordia.dev/v1
kind: AgentDeployment
metadata:
  name: code-reviewer
spec:
  replicas: 3
  template:
    provider: openai
    model: gpt-4o
    systemPrompt: "你是一个代码审查助手"
    maxTurns: 25
    apiSecretRef: openai-api-key
    tools:
      - name: filesystem
        config:
          rootDir: "/workspace"
      - name: shell
        config:
          commandWhitelist: "go,git"
    memory:
      backend: sqlite
      sizeLimit: 1Gi
    resources:
      requests: { cpu: 200m, memory: 256Mi }
      limits: { cpu: "2", memory: 1Gi }
  autoscaling:
    minReplicas: 1
    maxReplicas: 10
    targetConcurrentTasks: 5
  healthCheck:
    livenessProbe:
      httpGet: { path: /healthz, port: 8080 }
      initialDelaySeconds: 10
```

### AgentDeploymentSpec 字段

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `replicas` | int32 | ✅ | 副本数量 |
| `template.provider` | string | ✅ | LLM 提供者 (openai/anthropic/gemini/ollama/azure) |
| `template.model` | string | ✅ | 模型名称 |
| `template.systemPrompt` | string | ✅ | 系统提示词 |
| `template.maxTurns` | int32 | ❌ | 最大推理轮次 |
| `template.apiSecretRef` | string | ❌ | API Key 引用的 Secret 名称 |
| `template.tools` | []ToolSpec | ❌ | 工具配置列表 |
| `template.memory` | MemorySpec | ❌ | 记忆存储配置 |
| `template.resources` | ResourceSpec | ❌ | 资源请求和限制 |
| `autoscaling` | AutoscalingSpec | ❌ | 自动扩缩容配置 |
| `healthCheck` | HealthCheckSpec | ❌ | 健康检查配置 |

### 使用示例

```bash
# 部署 Agent
kubectl apply -f internal/k8s/manifest/examples/basic-agent.yaml

# 查看 Agent 状态
kubectl get ad

# 扩容
kubectl scale ad code-reviewer --replicas=5

# 查看详细信息
kubectl describe ad code-reviewer
```

### 构建 Operator

```bash
# 需要先安装 K8s 依赖
go get k8s.io/client-go@latest
go get sigs.k8s.io/controller-runtime@latest

# 编译
go build -o ap-operator ./cmd/operator/
```

---

## Grafana Dashboard

预置的 Grafana Dashboard 模板，导入即用。

### Dashboard 列表

| Dashboard | 文件 | 面板内容 |
|-----------|------|----------|
| Agent Runtime | `deploy/grafana/dashboard-agent.json` | 活跃agent数、轮次延迟P50/P95/P99、工具调用频率、LLM错误率 |
| LLM Operations | `deploy/grafana/dashboard-llm.json` | LLM延迟分布、Token消耗趋势、按Provider调用分布 |
| Cost Tracking | `deploy/grafana/dashboard-cost.json` | 总成本、按Provider/Agent成本分解、成本趋势 |

### 导入方式

**方式一：Grafana UI 导入**

1. 打开 Grafana → Dashboards → Import
2. 上传 JSON 文件
3. 选择 Prometheus 数据源
4. 点击 Import

**方式二：K8s ConfigMap**

```bash
kubectl create configmap ap-dashboard-agent \
  --from-file=deploy/grafana/dashboard-agent.json \
  -n monitoring
```

**方式三：Grafana Provisioning**

```bash
cp deploy/grafana/datasource.yml /etc/grafana/provisioning/datasources/
cp deploy/grafana/dashboard-*.json /etc/grafana/provisioning/dashboards/
```

### Prometheus 抓取配置

```yaml
scrape_configs:
  - job_name: 'agentprimordia'
    scrape_interval: 10s
    static_configs:
      - targets: ['agent:8080']
```

### 模板变量

| 变量 | 说明 |
|------|------|
| `agent_name` | 按 Agent 名称筛选 |
| `provider` | 按 LLM 提供者筛选 |
| `model` | 按模型名称筛选 |

---

## 插件系统

插件通过 Go Module 分发，实现 `ToolPlugin` 接口即可集成。

### 创建插件

```bash
ap plugin create ap-plugin-weather
```

生成插件骨架：

```go
package ap_plugin_weather

import ap "github.com/Gleamseekers/AgentPrimordia/agentprimordia/pkg"

type Plugin struct{}

func NewPlugin() ap.ToolPlugin {
    return &Plugin{}
}

func (p *Plugin) Name() string    { return "ap-plugin-weather" }
func (p *Plugin) Version() string { return "0.1.0" }
func (p *Plugin) Tools() []ap.Tool {
    return []ap.Tool{&WeatherTool{}}
}
func (p *Plugin) Init(config map[string]any) error { return nil }
func (p *Plugin) Close() error                      { return nil }
```

### 安装插件

```bash
ap plugin install github.com/user/ap-plugin-weather
```

等价于 `go get github.com/user/ap-plugin-weather` + 配置写入 `.ap.json`。

### 在代码中使用

```go
import _ "github.com/user/ap-plugin-weather"

// 通过 PluginLoader 加载
loader := tools.NewPluginLoader()
plugin, _ := loader.Load("ap-plugin-weather")
plugin.Init(map[string]any{
    "api_key": os.Getenv("WEATHER_API_KEY"),
})
for _, tool := range plugin.Tools() {
    registry.Register(tool)
}
```

### 插件发现

- GitHub Topic: `agentprimordia-plugin`
- 官方认证：通过 PR 提交到 awesome-agentprimordia 仓库，CI 自动检查后加 `certified` 标签

---

## Benchmark 套件

性能基准测试套件位于 `bench/suite/`。

### 运行基准

```bash
# 全部基准
cd bench && go test -bench=. -benchmem ./suite/

# 单项基准
go test -bench=BenchmarkAgentRun -benchmem ./suite/
go test -bench=BenchmarkConcurrent -benchmem ./suite/
go test -bench=BenchmarkVectorSearch -benchmem ./suite/

# 生成 CPU Profile
go test -bench=BenchmarkAgentRun -cpuprofile=cpu.prof ./suite/
go tool pprof cpu.prof
```

### 基准指标

| 基准 | 指标 | 说明 |
|------|------|------|
| `BenchmarkToolCalling` | ops/sec | 工具调用吞吐量 |
| `BenchmarkAgentRun` | ops/sec | 单 Agent 运行吞吐量 |
| `BenchmarkConcurrent` | ops/sec | 10 并发 Pool 吞吐量 |
| `BenchmarkFirstTokenLatency` | ns/op | 首 Token 延迟 |
| `BenchmarkMemoryStore` | ns/op | 记忆写入和搜索延迟 |
| `BenchmarkVectorSearch` | ns/op | 10K 向量搜索延迟 |
| `BenchmarkMemoryLatency` | ns/op | 1K 记忆搜索延迟 |

### 结果发布

每季度在 `bench/results/` 发布基准结果（JSON 格式），并可在 GitHub Pages 上托管排行榜。

---

## 错误处理

所有错误都使用哨兵错误（Sentinel Errors），便于精确判断：

```go
import (
    "github.com/Gleamseekers/AgentPrimordia/agentprimordia/pkg/errors"
)

_, err := agent.Run(ctx, msg)
if errors.Is(err, errors.ErrMaxTurnsExceeded) {
    fmt.Println("超过最大轮数限制")
} else if errors.Is(err, errors.ErrTimeout) {
    fmt.Println("执行超时")
} else if errors.Is(err, errors.ErrAgentStopped) {
    fmt.Println("Agent 被停止")
}
```

**预定义的错误类型**：
- `ErrAgentStopped` - Agent 已停止
- `ErrMaxTurnsExceeded` - 超过最大轮数
- `ErrInvalidConfig` - 无效配置
- `ErrTimeout` - 超时
- `ErrTaskNotFound` - 任务未找到
- `ErrAgentRunning` - Agent 正在运行中
- `ErrToolNotFound` - 工具未找到
- `ErrToolExecution` - 工具执行失败
- `ErrLLMCallFailed` - LLM 调用失败
- `ErrContextCanceled` - Context 取消
- `ErrPoolFull` - Pool 已满

---

## 并发安全

所有组件都是并发安全的：

```go
var wg sync.WaitGroup
for i := 0; i < 100; i++ {
    wg.Add(1)
    go func(id int) {
        defer wg.Done()
        resp, _ := agent.Run(ctx, agent.UserMessage(fmt.Sprintf("任务%d", id)))
        fmt.Printf("Agent %d 完成\n", id)
    }(i)
}
wg.Wait()
```

**内部同步机制**：
- Agent: `sync.Mutex`
- Memory Store: `sync.RWMutex`
- Tool Registry: `sync.Map`
- Debug Server: `sync.RWMutex`
- Discovery Server: `sync.RWMutex`
- A2A TaskManager: `sync.RWMutex`
- TCP Transport: `sync.Mutex` + 连接池互斥保护

---

## 性能优化建议

1. **连接池**: SQLite 后端默认开启连接池（最大 10 连接）
2. **内存缓存**: InMemoryStore 适合高频访问场景
3. **流式输出**: 使用 `RunStream()` 减少首字延迟
4. **批量操作**: Memory 的 `ImportMemories` 支持批量导入
5. **监控指标**: 使用 Prometheus Exporter 监控性能
6. **TCP 连接池**: TCP Transport 默认连接池大小 8，可通过 `TCPTransportConfig.PoolSize` 调整
7. **消息缓冲**: HTTP Transport 入站缓冲区 64 条，超出后返回 503

---

**📚 更多示例请查看 [examples/](../examples/) 目录**
