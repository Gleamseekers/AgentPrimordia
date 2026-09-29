# AgentPrimordia API 参考

> 包路径：`agentprimordia/pkg` → 导入别名 `ap`
>
> 版本：`ap.Version` = `"7.5.0"` (Go) / `@agentprimordia/sdk` `7.5.0` (TypeScript)
>
> **TypeScript SDK 与 Go 框架 100% 功能对等**，下方 API 参考 covers Go 公共 API。
> TypeScript SDK 完整 API 参考见 [sdk/typescript/docs/api/index.md](../sdk/typescript/docs/api/index.md)。

## 核心类型

### Agent

`Agent` 是所有 Agent 实现的核心接口，编排模式（Pipeline / Handoff / Parallel）和 Pool 均面向此接口编程。

```go
type Agent interface {
    Run(ctx context.Context, input Message) (*Response, error)
    StreamRun(ctx context.Context, input Message) (<-chan StreamEvent, error)
    Stop()
    Stats() AgentStats
    Name() string
}
```

### ReActAgent

`ReActAgent` 是基于 ReAct（推理+行动）循环的 Agent 实现，是框架的核心引擎。

```go
// 推荐入口：ap.NewAgent（v0.7.0 起，不暴露废弃字段）
agent, err := ap.NewAgent("my-agent", "你是一个助手", provider,
    ap.WithMaxTurns(50),
    ap.WithToolkit(registry),
)
if err != nil {
    log.Fatal(err)
}

// ReActAgent 类型仍可用，但请通过 NewAgent 创建实例

resp, err := agent.Run(ctx, ap.UserMessage("你好"))
```

### ReActConfig

`ReActConfig` 是 ReActAgent 的核心配置结构（仅标量字段）。工具、记忆、RAG 等能力通过链式 API `WithToolkit()` / `WithMemory()` / `WithRAG()` 注入，不再通过 Config 字段配置。

| 字段 | 类型 | 说明 |
|------|------|------|
| `Name` | `string` | Agent 名称 |
| `SystemPrompt` | `string` | 系统提示词 |
| `PromptTemplate` | `*PromptTemplate` | 提示词模板（优先于 SystemPrompt） |
| `Model` | `llm.Provider` | LLM 提供者 |
| `MaxTurns` | `int` | 最大推理轮次（默认 50） |
| `Temperature` | `float64` | LLM 温度参数 |
| `SessionID` | `string` | 会话标识 |
| `Lifecycle` | `*Lifecycle` | 生命周期管理器（默认自动创建） |
| `Logger` | `*slog.Logger` | 结构化日志（默认 `slog.Default()`） |

> **注意：** v0.7.0 前 ReActConfig 包含 Toolkit / Memory / Hooks / RAG 等 14 个字段，已废弃。使用 `NewAgent()` + Functional Options 或链式 API 替代。

### Message / Role / ToolCall / Thought / Response

| 类型 | 说明 |
|------|------|
| `Message` | 对话消息，包含角色、内容和可选的工具调用 |
| `Role` | 消息角色：`RoleSystem` / `RoleUser` / `RoleAssistant` / `RoleTool` |
| `ToolCall` | LLM 发起的工具调用请求（ID + 名称 + JSON 参数） |
| `Thought` | LLM 的推理输出（文本 + 工具调用列表） |
| `Response` | Agent 最终响应（内容 + 工具调用 + 用量 + 指标） |

快捷函数：

```go
msg := ap.UserMessage("用户输入")
msg := ap.SystemMessage("系统指令")
```

### AgentStatus / AgentStats

| 状态 | 说明 |
|------|------|
| `StatusIdle` | 空闲 |
| `StatusRunning` | 运行中 |
| `StatusPaused` | 已暂停 |
| `StatusCompleted` | 已完成 |
| `StatusFailed` | 执行失败 |
| `StatusCancelled` | 已取消 |

`AgentStats` 包含状态、当前轮次、总消息数、工具调用计数和启动时间。

### StreamEvent / StreamEventType

流式输出事件类型：

| 事件类型 | 说明 |
|----------|------|
| `StreamEventToken` | 逐 token 输出 |
| `StreamEventThought` | 推理/思考 |
| `StreamEventToolCall` | 工具调用开始 |
| `StreamEventToolResult` | 工具执行结果 |
| `StreamEventComplete` | 运行完成 |
| `StreamEventError` | 错误 |

```go
ch, err := agent.StreamRun(ctx, ap.UserMessage("你好"))
for event := range ch {
    switch event.Type {
    case ap.StreamEventToken:
        fmt.Print(event.Content)
    case ap.StreamEventComplete:
        fmt.Println("\n完成")
    }
}
```

### PromptTemplate

支持 `{{.Variable}}` 格式的系统提示词模板，可自动注入 Agent 名称、权限规则等变量。

```go
tmpl := ap.NewPromptTemplate("你是 {{.AgentName}}，一个专业的助手")
tmpl = tmpl.WithVar("AgentName", "CodeBot")
rendered, _ := tmpl.Render()
```

`DefaultSystemPrompt()` 返回包含 Agent 名称和权限规则占位符的默认模板。

---

## 消息总线

### MessageBus

`MessageBus` 是统一消息总线接口，支持 Agent 间的点对点消息发送和广播。

```go
type MessageBus interface {
    Send(ctx context.Context, msg *BusMessage) (*BusMessage, error)
    Broadcast(ctx context.Context, msg *BusMessage) map[string]*BusMessage
    Register(agentID string, handler BusMessageHandler)
    Unregister(agentID string)
    ListAgents() []string
    Subscribe(agentID string) <-chan *BusMessage
}
```

### LocalMessageBus

`LocalMessageBus` 是进程内消息总线实现，支持注册、发送、广播和订阅。

```go
bus := ap.NewLocalMessageBus()
bus.Register("agent-1", handler)
resp, err := bus.Send(ctx, &ap.BusMessage{
    From:    "coordinator",
    To:      "agent-1",
    Type:    ap.BusMsgTaskRequest,
    Content: "请处理这个任务",
})
```

### BusMessage / BusMessageType

消息类型常量：

| 常量 | 说明 |
|------|------|
| `BusMsgTaskRequest` | 任务请求 |
| `BusMsgTaskResult` | 任务结果 |
| `BusMsgQuery` | 查询 |
| `BusMsgResponse` | 查询响应 |
| `BusMsgHandoff` | Agent 交接 |
| `BusMsgBroadcast` | 广播 |
| `BusMsgStatusUpdate` | 状态更新 |
| `BusMsgNotify` | 通知 |

### Transport / HTTPTransport

`Transport` 是跨进程 Agent 通信传输层接口，`HTTPTransport` 是基于 HTTP 的实现。

```go
transport := ap.NewHTTPTransport()
transport.Start(":8080")
```

---

## 编排

### Pipeline

`Pipeline` 是顺序编排器，前一个 Agent 的输出作为后一个的输入。支持条件跳过步骤。

```go
pipeline := ap.NewPipeline(
    ap.PipelineStep{Name: "分析", Agent: analyzer},
    ap.PipelineStep{Name: "生成", Agent: generator},
    ap.PipelineStep{
        Name:      "审核",
        Agent:     reviewer,
        Condition: func(ctx context.Context, prev *ap.StepResult) bool {
            return prev != nil && !prev.Skipped
        },
    },
)
result, err := pipeline.Run(ctx, "初始输入")
fmt.Println(result.Final)
```

### Handoff

`Handoff` 是动态交接编排器，支持 Agent 间根据路由函数自动交接。

```go
handoff := ap.NewHandoff(ap.HandoffConfig{
    Agents: []ap.Agent{coder, reviewer, tester},
    Router: func(ctx context.Context, input string) int {
        if strings.Contains(input, "代码") { return 0 }
        if strings.Contains(input, "审核") { return 1 }
        if strings.Contains(input, "测试") { return 2 }
        return -1
    },
    MaxHandoffs: 10,
})
result, err := handoff.Run(ctx, "请写代码实现排序")
```

### ParallelRun

`ParallelRun` 并行执行多个 Agent 并汇总结果。

```go
result, err := ap.ParallelRun(ctx, []ap.Agent{agent1, agent2, agent3}, "输入")
for _, r := range result.Results {
    fmt.Printf("Agent %s: %s\n", r.AgentName, r.Output)
}
```

### DAG / DAGBuilder / DAGWorkflow

有向无环图工作流引擎，支持条件边、并行节点、重试和错误处理。

```go
dag, err := ap.NewDAGBuilder("data-analysis").
    Node("collect", collectAgent).
    Node("analyze", analyzeAgent).
    Node("report", reportAgent).
    Edge("collect", "analyze").
    Edge("analyze", "report").
    Build()

result, err := dag.Run(ctx, "分析销售数据")
```

| 方法 | 说明 |
|------|------|
| `Node(id, agent)` | 添加节点 |
| `Edge(from, to)` | 添加边 |
| `EdgeIf(from, to, condition)` | 条件边 |
| `Build()` | 构建工作流 |
| `MustBuild()` | 构建工作流（panic on error） |

### GroupChat / Debate / Supervisor

多 Agent 协作模式：

| 模式 | 说明 |
|------|------|
| `GroupChat` | 多 Agent 轮流发言，支持多种发言选择器（轮询 / 投票 / LLM 路由） |
| `Debate` | 对抗式辩论，多 Agent 从不同角度辩论后由裁判总结 |
| `Supervisor` | 主管模式，一个 Supervisor Agent 负责分配任务给 Worker Agent |

```go
chat := ap.NewGroupChat(ap.GroupChatConfig{
    Agents:    []ap.Agent{agent1, agent2, agent3},
    MaxRounds: 10,
    // SelectSpeaker 可选，默认为轮询（RoundRobin）
})
result, err := chat.Run(ctx, "讨论 Go 1.26 的新特性")
```

---

## RAG 知识库

### RAGConfig / RAGMode / RAGProvider

`RAGConfig` 配置 RAG 注入行为，`RAGMode` 控制注入方式：

| 模式 | 说明 |
|------|------|
| `RAGModeAuto` | 每轮推理前自动查询知识库 |
| `RAGModeFirst` | 仅第一轮推理前查询 |
| `RAGModeOnDemand` | 仅当 Agent 主动调用 knowledge_search 工具时查询 |

```go
ragStore := ap.NewRAGStore(memStore, embedder)
ragProvider := ap.NewRAGProviderAdapter(ragStore)

agent, err := ap.NewAgent("rag-agent", "你是一个 RAG 助手", provider,
    ap.WithRAG(ap.RAGConfig{
        Provider: ragProvider,
        Mode:     ap.RAGModeAuto,
        TopK:     5,
        MinScore: 0.3,
    }),
)
```

### RAGDocument

RAG 检索返回的文档片段，包含 ID、内容、相关度分数和来源。

---

## 工具系统

### Tool / ToolRegistry / ToolExecutor

```go
registry := ap.NewToolRegistry()
registry.Register(myTool)

executor := ap.NewToolExecutor(registry)
result, err := executor.Execute(ctx, &ap.ToolFunctionCall{
    ID:   "call_1",
    Name: "my_tool",
    Args: `{"key": "value"}`,
})
```

### ToolPermission / ScopePolicy / FileScopePolicy

```go
policy := ap.NewFileScopePolicy()
policy.SetScope("agent-1", []string{"/src/", "/docs/"})

executor := ap.NewToolExecutor(registry).WithScopePolicy(policy, "agent-1")
```

### 内置工具

| 工具 | 创建函数 | 说明 |
|------|----------|------|
| `FileSystem` | `NewFileSystem(rootDir)` | 文件读写、搜索目录 |
| `Shell` | `NewShell()` | 命令行执行（超时、白名单） |
| `Web` | `NewWeb()` | HTTP 请求 |
| `KnowledgeSearch` | `NewKnowledgeSearch(searcher)` | 知识库搜索 |

### ToolkitConfig / DefaultToolkit / MinimalToolkit

```go
registry, err := ap.DefaultToolkit(ap.ToolkitConfig{
    RootDir:      "/workspace",
    EnableFS:     true,
    EnableShell:  true,
    EnableWeb:    true,
    EnableSearch: true,
    ScopePolicy:  policy,
    ScopeAgent:   "agent-1",
    FileLock:     fileLockMgr,
})

registry, err := ap.MinimalToolkit("/workspace")
```

---

## 记忆存储

### Memory

`Memory` 是记忆存储的核心接口，提供增删查改、搜索、统计、导入导出等完整能力。

```go
type Memory interface {
    Add(ctx context.Context, episode *Episode) error
    Search(ctx context.Context, query string, opts *SearchOptions) ([]*Episode, error)
    Get(ctx context.Context, id string) (*Episode, error)
    Delete(ctx context.Context, id string) error
    Count(ctx context.Context, sessionID string) (int64, error)
    List(ctx context.Context, opts *ListOptions) ([]*Episode, error)
    UpdateSummary(ctx context.Context, id string, summary, topics string) error
    SetImportance(ctx context.Context, id string, importance float64) error
    SearchByTag(ctx context.Context, tag string, opts *SearchOptions) ([]*Episode, error)
    GetImportant(ctx context.Context, threshold float64, limit int) ([]*Episode, error)
    GetTimeline(ctx context.Context, days int) (map[string][]*Episode, error)
    CleanupExpired(ctx context.Context, maxAgeDays int) (int64, error)
    Stats(ctx context.Context) (*MemoryStats, error)
    RecordToolUse(ctx context.Context, sessionID, agentName, toolName, args, result string) error
    ClearAll(ctx context.Context, sessionID string) error
    ExportMemories(ctx context.Context, sessionID, format string) ([]byte, error)
    ImportMemories(ctx context.Context, data []byte, format string) (int, error)
    Close() error
}
```

### SQLiteStore

基于 SQLite + FTS5 的记忆存储实现，支持全文搜索和自动清理。

```go
store, err := ap.NewSQLiteStore("./data/memory.db")
store, err := ap.WithInMemory()  // 测试用内存模式
```

### VectorStore / EmbeddingProvider

```go
vecStore := ap.NewVectorStore(1536)
embedder := ap.NewEmbeddingAdapter(llmProvider, 1536)
```

### RAGStore

封装 Memory + VectorStore + EmbeddingProvider，提供混合 RAG 检索能力（FTS5 + 向量），支持 RRF 融合。

```go
ragStore := ap.NewRAGStore(store, embedder)
results, err := ragStore.HybridSearch(ctx, "查询内容", 5)

// RRF 融合模式（运行时切换）
ragStore = ap.NewRAGStoreWithFusionConfig(store, embedder, ap.RAGFusionConfig{
    FusionMode:    ap.FusionRRF,  // Reciprocal Rank Fusion
    RRFK:          60,
    OverFetchSize: 5,
})

// 运行时切换回 Linear 模式
ragStore.SetFusionConfig(ap.RAGFusionConfig{
    FusionMode:   ap.FusionLinear,
    FTSWeight:    0.4,
    VectorWeight: 0.6,
})
```

融合模式：
| 模式 | 说明 |
|------|------|
| `FusionLinear` | 基于原始分数加权融合（默认） |
| `FusionRRF` | 基于排名融合，对量纲差异鲁棒 |

检索流程：`查询 → Embedding → Vector Search → FTS Search → RRF 融合 → Rerank → TopK → 上下文注入`

### Episode / SearchOptions / ListOptions / MemoryStats / CleanupConfig

| 类型 | 说明 |
|------|------|
| `Episode` | 一条记忆片段（会话 ID、角色、内容、摘要、主题、重要性） |
| `SearchOptions` | 搜索选项（会话过滤、分页、角色过滤） |
| `ListOptions` | 列表选项（会话过滤、分页、排序方向） |
| `MemoryStats` | 统计信息（总数、会话数、时间范围、存储大小） |
| `CleanupConfig` | 自动清理配置（过期天数、间隔、保留角色） |

### 文档处理

| 类型 | 说明 |
|------|------|
| `Document` | 加载后的文档（ID、内容、元数据、来源） |
| `DocumentLoader` | 文档加载接口 |
| `TextFileLoader` | 文本文件加载器 |
| `ReaderLoader` | io.Reader 加载器 |
| `TextSplitter` | 文本切分接口 |
| `CharacterSplitter` | 按字符数切分 |
| `RecursiveSplitter` | 递归切分（多分隔符） |
| `LineSplitter` | 按行数切分 |
| `DocChunk` | 切分后的文本块 |
| `DocumentPipeline` | 文档处理管道（加载 → 切分） |

```go
pipeline := ap.NewDocumentPipeline(
    ap.NewTextFileLoader(),
    ap.NewRecursiveSplitter(1000, 200),
)
chunks, err := pipeline.Process(ctx, "/path/to/docs")
```

---

## LLM 抽象

### Provider

`Provider` 是 LLM 提供者的核心接口。

```go
type Provider interface {
    Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)
    Stream(ctx context.Context, req *CompletionRequest) (<-chan Chunk, error)
    CallTools(ctx context.Context, req *ToolCallRequest) (*ToolCallResponse, error)
    Info() ModelInfo
}

// Embedder 嵌入接口（可选实现，调用方通过类型断言检查）
type Embedder interface {
    Embeddings(ctx context.Context, texts []string) ([][]float32, error)
}
```

### 提供者实现

| 提供者 | 创建函数 | 说明 |
|--------|----------|------|
| `OpenAIProvider` | `NewOpenAIProvider(cfg)` | OpenAI GPT 系列 |
| `AnthropicProvider` | `NewAnthropicProvider(cfg)` | Claude 系列 |
| `GeminiProvider` | `NewGeminiProvider(cfg)` | Google Gemini 系列 |
| `OllamaProvider` | `NewOllamaProvider(cfg)` | 本地 Ollama |
| `AzureOpenAIProvider` | `NewAzureOpenAIProvider(cfg)` | Azure OpenAI |
| `QwenProvider` | `NewQwenProvider(cfg)` | 通义千问（DashScope 兼容） |
| `GLMProvider` | `NewGLMProvider(cfg)` | 智谱 GLM |
| `CohereProvider` | `NewCohereProvider(cfg)` | Cohere v2 API |
| `MistralProvider` | `NewMistralProvider(cfg)` | Mistral AI |
| `ResilientProvider` | `NewResilientProvider(primary, cfg)` | 弹性提供者（重试+熔断+降级） |

> **DeepSeek** 使用 `NewOpenAIProvider` 并设置 `BaseURL` 即可（OpenAI 兼容模式）。

### Config

```go
cfg := ap.Config{
    APIKey:      "sk-xxx",
    Model:       "gpt-4o",
    Temperature: 0.7,
    MaxTokens:   4096,
}
```

### AzureConfig

```go
azureCfg := ap.AzureConfig{
    ResourceName:   "my-resource",
    DeploymentName: "gpt-4o-deployment",
    APIVersion:     "2024-02-15-preview",
    APIKey:         "xxx",
}
```

### ResilientProvider

弹性提供者支持重试、熔断和降级。

```go
resilient := ap.NewResilientProvider(primary, ap.DefaultResilientConfig())
resilient.AddFallback(fallback1)
resilient.AddFallback(fallback2)
```

`DefaultResilientConfig()` 返回：3 次重试、500ms 退避、10s 最大退避、5 次熔断阈值、30s 恢复时间。

### 请求/响应类型

| 类型 | 说明 |
|------|------|
| `CompletionRequest` | 补全请求（消息列表 + 模型 + 温度 + 最大 Token） |
| `CompletionResponse` | 补全响应（ID + 内容 + 角色 + 用量） |
| `ChatMessage` | 对话消息（角色 + 内容 + 工具调用） |
| `ToolCallRequest` | 工具调用请求（消息 + 工具定义） |
| `ToolCallResponse` | 工具调用响应（内容 + 工具调用 + 用量） |
| `Chunk` | 流式片段（内容 + 完成标记） |
| `ModelInfo` | 模型信息（名称 + 提供者 + 上下文窗口 + 能力标记） |
| `Usage` | Token 用量（输入 + 输出 + 总计） |
| `FunctionCall` | 函数调用（ID + 名称 + JSON 参数） |
| `FunctionDefinition` | 函数定义（名称 + 描述 + 参数 Schema） |
| `ToolDefinition` | 工具定义（类型 + 函数定义） |
| `APIError` | API 错误（错误码 + 消息 + 类型） |

---

## Pool 调度

### Pool

`Pool` 是多 Agent 并发调度器，支持任务分发、重试、取消和会话管理。

```go
pool := ap.NewPool(ap.PoolConfig{
    MaxConcurrency: 10,
    Timeout:        5 * time.Minute,
    RetryPolicy: ap.RetryPolicy{
        MaxRetries:      3,
        Backoff:         time.Second,
        RetryableErrors: []string{"rate_limit"},
    },
})
pool.SetModel(provider)
pool.SetToolkit(registry)
defer pool.Close()

results, err := pool.Dispatch(ctx, []ap.TaskConfig{
    {ID: "t1", Title: "任务1", Prompt: "执行任务1"},
    {ID: "t2", Title: "任务2", Prompt: "执行任务2"},
})
```

### AgentFactory

自定义 Agent 创建逻辑：

```go
pool.SetAgentFactory(func(cfg ap.AgentFactoryConfig) ap.Agent {
    agent, _ := ap.NewAgent(cfg.Name, cfg.SystemPrompt, provider,
        ap.WithMaxTurns(cfg.MaxTurns),
        ap.WithMemory(memAdapter),
    )
    return agent
})
```

### PoolConfig / TaskConfig / TaskResult / PoolStats

| 类型 | 说明 |
|------|------|
| `PoolConfig` | Pool 配置（最大并发 + 超时 + 重试策略 + 默认 Agent 配置） |
| `TaskConfig` | 任务配置（ID + 标题 + 提示词 + 会话 + 作用域） |
| `TaskResult` | 任务结果（ID + 响应 + 错误 + 耗时 + 状态） |
| `PoolStats` | 运行统计（总任务 + 完成 + 失败 + 运行 + 排队） |
| `PoolEvent` | Pool 事件（类型 + 任务 ID + 时间戳） |
| `RetryPolicy` | 重试策略（最大重试 + 退避 + 可重试错误模式） |
| `AgentFactoryConfig` | 工厂配置（名称 + 提示词 + 作用域 + 会话 ID） |

任务状态：`PoolTaskQueued` / `PoolTaskRunning` / `PoolTaskCompleted` / `PoolTaskFailed` / `PoolTaskCancelled`

---

## 钩子系统

### HookManager / HookPoint

钩子挂载点对应 Agent 生命周期的各个阶段：

| 挂载点 | 说明 |
|--------|------|
| `HookBeforeRun` | Agent 运行前 |
| `HookAfterRun` | Agent 运行后 |
| `HookBeforeTurn` | 每轮推理前 |
| `HookAfterTurn` | 每轮推理后 |
| `HookBeforeLLM` | 调用 LLM 前 |
| `HookAfterLLM` | LLM 响应后 |
| `HookBeforeTool` | 工具执行前 |
| `HookAfterTool` | 工具执行后 |
| `HookOnError` | 发生错误时 |
| `HookOnComplete` | Agent 完成时 |
| `HookBeforeRAG` | RAG 检索前 |
| `HookAfterRAG` | RAG 检索后 |
| `HookBeforePipelineStep` | Pipeline 步骤前 |
| `HookAfterPipelineStep` | Pipeline 步骤后 |
| `HookBeforeHandoff` | Agent 交接前 |
| `HookAfterHandoff` | Agent 交接后 |
| `HookBeforeParallelAgent` | 并行 Agent 前 |
| `HookAfterParallelAgent` | 并行 Agent 后 |

```go
hooks := ap.NewHookManager()
hooks.Register(ap.HookBeforeTool, func(ctx context.Context, hctx *ap.HookContext) error {
    log.Printf("即将执行工具: %s", hctx.ToolCall.Name)
    return nil
})
hooks.RegisterWithPriority(ap.HookOnError, myErrorHandler, 10)
```

---

## 适配器

适配器将不同模块的接口桥接到一起，是组装完整 Agent 的关键。

| 适配器 | 函数 | 说明 |
|--------|------|------|
| Memory → Agent | `SQLiteStore` / `InMemoryStore` 直接实现 `MemoryStore` 接口，无需适配器 |
| EventBus → Agent | `NewEventBusAdapter(bus)` | 将 `Bus` 适配为 `EventPublisher` |
| Metrics → Agent | `AgentMetrics` 直接实现 `MetricsRecorder` 接口，无需适配器 |
| LLM → Memory | `NewEmbeddingAdapter(provider, dim)` | 将 `Provider` 适配为 `EmbeddingProvider` |
| RAGStore → Agent | `NewRAGProviderAdapter(store)` | 将 `RAGStore` 适配为 `RAGProvider` |
| RAGStore → Tool | `NewKnowledgeSearcherAdapter(store)` | 将 `RAGStore` 适配为 `KnowledgeSearcher` |

完整组装示例：

```go
store, _ := ap.NewSQLiteStore("./data/memory.db")
embedder := ap.NewEmbeddingAdapter(llmProvider, 1536)
ragStore := ap.NewRAGStore(store, embedder)

agent, err := ap.NewAgent("full-agent", "你是一个助手", llmProvider,
    ap.WithMaxTurns(50),
    ap.WithToolkit(registry),
    ap.WithMemory(store),
    ap.WithRAG(ap.RAGConfig{
        Provider: ap.NewRAGProviderAdapter(ragStore),
        Mode:     ap.RAGModeAuto,
        TopK:     5,
    }),
    ap.WithEvents(ap.NewEventBusAdapter(ap.NewBus())),
    ap.WithMetrics(ap.NewMetrics()),
)
```

---

## 事件系统

### Bus / Event / EventType

```go
bus := ap.NewBus()
bus.Subscribe(ap.EventAgentStart, func(evt ap.Event) {
    log.Printf("Agent 启动: %s", evt.Source)
})
bus.PublishAsync(ap.Event{Type: ap.EventAgentStart, Source: "my-agent"})
```

事件类型：`EventAgentStart` / `EventAgentStop` / `EventAgentPanic` / `EventAgentError` / `EventAgentResume` / `EventTurnStart` / `EventTurnEnd` / `EventToolCall` / `EventToolResult` / `EventLLMCall` / `EventLLMResponse` / `EventPoolDispatch` / `EventPoolComplete`

---

## 安全

### ACL / ACLRule / AccessLevel

```go
acl := ap.NewACL()
acl.AddRule(ap.ACLRule{Agent: "agent-1", Resource: "/src/", Level: ap.AccessRead | ap.AccessWrite})
acl.AddRule(ap.ACLRule{Agent: "agent-2", Resource: "/src/", Level: ap.AccessRead})
```

访问级别：`AccessNone` / `AccessRead` / `AccessWrite` / `AccessExecute` / `AccessAll`

### Sandbox

```go
sandbox := ap.NewSandbox()
sandbox.AllowCommand("ls")
sandbox.AllowCommand("cat")
sandbox.AllowPath("/workspace")
```

---

## 并发控制

### FileLockManager

```go
fl := ap.NewFileLockManager()
lock, _ := fl.Acquire(ctx, "/workspace/file.go")
defer fl.Release("/workspace/file.go")
```

### ValidateScopes

验证 Agent 的文件作用域是否存在重叠冲突。

```go
scopes := map[string][]string{
    "agent-1": {"/src/"},
    "agent-2": {"/src/"},
}
err := ap.ValidateScopes(scopes) // 返回 ErrScopeOverlap
```

---

## 指标

### AgentMetricsCollector / Histogram

```go
m := ap.NewMetrics()
m.RecordLLMCall(duration, nil)
m.RecordToolCall(duration, nil)
m.RecordTurn(duration)

hist := ap.NewHistogram([]float64{0.1, 0.5, 1.0, 5.0})
hist.Observe(0.3)
snapshot := hist.Snapshot()
```

### 导出器

| 导出器 | 创建函数 | 说明 |
|--------|----------|------|
| `PrometheusHandler` | `NewPrometheusHandler()` | Prometheus 格式 HTTP 处理器 |
| `LogExporter` | `NewLogExporter()` | 日志格式导出 |
| `JSONExporter` | `NewJSONExporter()` | JSON 格式导出 |
| `MultiExporter` | `NewMultiExporter()` | 多目标导出 |
| `MetricsExporter` | `NewMetricsExporter()` | 通用导出器 |

---

## 状态持久化

### CheckpointStore / SQLiteCheckpointStore

```go
cpStore, _ := ap.NewSQLiteCheckpointStore("./data/checkpoints.db")
// 或内存模式
cpStore := ap.InMemoryCheckpointStore()
```

### AgentState

Agent 的持久化状态，包含消息历史、轮次和状态信息。

---

## 函数选项

| 选项 | 说明 |
|------|------|
| `WithTimeout(d)` | 设置执行超时 |
| `WithMaxTurns(n)` | 设置最大推理轮次 |
| `WithTemperature(t)` | 设置 LLM 温度 |
| `WithCheckpoint(dir)` | 启用状态检查点 |
| `WithStreaming(fn)` | 启用流式输出 |
| `WithMetadata(m)` | 添加自定义元数据 |

---

## 错误码

`GetErrorCode(err)` 从错误中提取结构化错误码：

| 错误码 | 错误 | 说明 |
|--------|------|------|
| `AGENT_001` | `ErrAgentStopped` | Agent 已停止 |
| `AGENT_002` | `ErrAgentRunning` | Agent 已在运行 |
| `AGENT_003` | `ErrMaxTurnsExceeded` | 超出最大轮次 |
| `AGENT_004` | `ErrNoToolkit` | 未配置工具包 |
| `TOOL_001` | `ErrToolNotFound` | 工具未注册 |
| `TOOL_002` | `ErrToolExecution` | 工具执行失败 |
| `TOOL_003` | `ErrInvalidConfig` | 配置无效 |
| `TOOL_004` | `ErrConfirmDenied` | 确认被拒绝 |
| `LLM_001` | `ErrLLMCallFailed` | LLM 调用失败 |
| `LLM_002` | `ErrNotSupported` | 操作不支持 |
| `LLM_003` | `ErrCircuitOpen` | 熔断器打开 |
| `LLM_004` | `ErrAPIKeyRequired` | API Key 缺失 |
| `LLM_005` | `ErrEmptyResponse` | 空响应 |
| `LLM_006` | `ErrResponseParseFailed` | 响应解析失败 |
| `LLM_007` | `ErrRetriesExhausted` | 重试耗尽 |
| `LLM_008` | `ErrFallbackFailed` | 降级失败 |
| `POOL_001` | `ErrPoolFull` | Pool 已满 |
| `POOL_002` | `ErrTaskNotFound` | 任务未找到 |
| `POOL_003` | `ErrTimeout` | 操作超时 |
| `CTX_001` | `ErrContextCanceled` | 上下文取消 |
| `MEM_001` | `ErrEpisodeNotFound` | 记忆未找到 |
| `MEM_002` | `ErrInvalidImportance` | 重要性无效 |
| `MEM_003` | `ErrEmptyEpisodeID` | Episode ID 为空 |
| `MEM_004` | `ErrEmptySessionID` | Session ID 为空 |
| `MEM_005` | `ErrEmptyRole` | 角色为空 |
| `MEM_006` | `ErrEmptyContent` | 内容为空 |
| `MEM_007` | `ErrDimensionMismatch` | 向量维度不匹配 |
| `MEM_008` | `ErrVectorNotFound` | 向量未找到 |
| `SEC_001` | `ErrCommandBlocked` | 命令被阻止 |
| `SEC_002` | `ErrCommandNotAllowed` | 命令不允许 |
| `SEC_003` | `ErrAccessDenied` | 访问拒绝 |
| `SEC_004` | `ErrPathTraversal` | 路径遍历 |
| `EVT_001` | `ErrBusClosed` | 总线关闭 |
| `PST_001` | `ErrCheckpointNotFound` | 检查点未找到 |
| `CON_001` | `ErrGlobalWriteConflict` | 全局写冲突 |
| `CON_002` | `ErrScopeOverlap` | 作用域重叠 |

---

## TypeScript SDK API

> **100% Go 功能对等** — 完整 API 参考见 [sdk/typescript/docs/api/index.md](../sdk/typescript/docs/api/index.md)

### 安装

```bash
npm install @agentprimordia/sdk
npm install better-sqlite3  # 可选：SQLite 持久化
```

### 核心 API 对照表

| Go (`ap.`) | TypeScript (`@agentprimordia/sdk`) | 说明 |
|---|---|---|
| `NewAgent()` | `new ReActAgent()` / `newAgent()` | 创建 Agent |
| `NewOpenAIProvider()` | `new OpenAIProvider()` | OpenAI |
| `NewAnthropicProvider()` | `new AnthropicProvider()` | Anthropic Claude |
| `NewGeminiProvider()` | `new GeminiProvider()` | Google Gemini |
| `NewOllamaProvider()` | `new OllamaProvider()` | Ollama 本地 |
| `NewResilientProvider()` | `new ResilientProvider()` | 弹性 Provider |
| `NewPool()` | `new AgentPool()` | Agent 并发池 |
| `NewDAGBuilder()` | `new DAGBuilder()` | DAG 工作流 |
| `NewMCPClient()` | `new MCPClient()` | MCP 客户端 |
| `NewMCPRegistry()` | `new MCPRegistry()` | MCP 注册中心 |
| `DefaultToolkit()` | `defaultToolkit` | 默认工具集 |
| `NewAuditLogger()` | `new AuditLogger()` | 审计日志 |
| `NewAdminHandler()` | `new AdminHandler()` | 管理 HTTP API |
| `NewHealthServer()` | `new HealthServer()` | 健康检查端点 |

### TypeScript 基础设施 API（Phase 24）

```typescript
import {
  AuditLogger, InMemoryAuditOutput,     // 审计日志
  AdminHandler,                          // 管理 HTTP API + Web UI
  Inspector, InspectorServer, DebugServer, // 调试器 HTTP 服务
  SQLiteCheckpointStore,                 // SQLite 检查点持久化
  HealthServer,                          // /healthz /readyz /livez
} from '@agentprimordia/sdk';
```

详见 [TypeScript SDK API 参考](../sdk/typescript/docs/api/index.md)。

## v5.0 新增 API 索引（v4.1 – v5.0 弧线）

| API | 版本 | 说明 | 文档 |
|-----|------|------|------|
| `ap.ProviderFromEnv()` | v4.1 | env 驱动真实 LLM（AP_LLM_*） | guides/第三方集成.md |
| `ap.NewOpenAIASR/NewOpenAITTS` | v4.1 | 真实语音适配器（本地免 key） | guides/实时通信.md |
| `realtime.Runtime.ProcessTurnStream` | v4.3 | 流式语音链路 | guides/实时通信.md |
| `skills.SignSkillManifest/InstallSkillFromManifest` | v4.4 | 技能市场发布/订阅（验签） | guides/发布指南.md |
| `memory.NewPhysicalTenantStore` | v4.6 | 物理分库强隔离 | 安全态势.md |
| `autonomy.GoalConfig.BudgetUSD` | v4.9 | 目标级成本预算护栏 | V5路线图.md §十 |
| `marketplace.Installer.EnableDownloadStats` | v4.8 | 市场下载统计 | publishing.md |

完整契约见 `sdk/typescript/api-contract.json`（39 模块，version-sync + api-extract 门维护）。v5.1–v6.0 新增转正 API（strategy / consolidation / organization / learning feedback 等）见 [`V6路线图.md`](V6路线图.md) 与 api-contract.json。
