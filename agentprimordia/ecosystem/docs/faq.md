# AgentPrimordia 常见问题（FAQ）

---

## 入门相关

### Q: AgentPrimordia 和 LangChain 有什么区别？

AgentPrimordia 是 **Go 原生** 的 Agent 开发框架，核心差异：

| 特性 | AgentPrimordia | LangChain |
|------|---------------|-----------|
| 语言 | Go 1.26+ | Python / JS |
| 外部依赖 | 零（仅 `modernc.org/sqlite`） | 大量第三方包 |
| 并发模型 | goroutine + channel 原生支持 | GIL 限制 / 异步回调 |
| 架构风格 | 接口优先、协议式微内核 | 继承链 + 装饰器 |
| 部署产物 | 单二进制 | 解释型 / Node 运行时 |

简而言之：如果你用 Go 构建生产级 Agent，AgentPrimordia 是零依赖、高并发、单二进制部署的选择。

---

### Q: 需要付费的 API Key 吗？

**不需要。** 框架本身不绑定任何付费服务：

- 测试开发：使用 `testutil.NewMockProvider`，完全离线
- 本地推理：对接 [Ollama](https://ollama.ai) 等本地模型，零成本
- 生产环境：按需接入 OpenAI / Anthropic / Azure 等付费 Provider

---

### Q: 支持 Windows 吗？

**完全支持。** AgentPrimordia 采用 **Zero CGO** 设计——唯一的外部依赖 `modernc.org/sqlite` 是纯 Go 实现的 SQLite 驱动，无需 CGO 编译。在 Windows 上直接 `go build` 即可，无需 MinGW 或其他 C 工具链。

---

### Q: 最低 Go 版本要求？

**Go 1.26+**。框架使用了 1.22 引入的范围循环变量语义（`for i := range n`）等特性，更低版本无法编译。

---

### Q: 如何安装？

```bash
# 方式一：安装 CLI（推荐，经 GOPROXY，无需克隆仓库）
go install github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/cmd/ap@latest

# 方式二：作为依赖引入
go get github.com/Gleamseekers/AgentPrimordia/v7@latest
```

---

## API 使用

### Q: NewAgent 怎么用？

`ap.NewAgent` 是创建 Agent 的推荐入口，使用 Functional Options 注入能力：

```go
// 推荐写法
agent, err := ap.NewAgent(
    "my-agent",
    "你是一个助手",
    provider,
    ap.WithMaxTurns(10),
)
```

---

### Q: 如何切换不同的 LLM？

所有 Provider 实现相同的 `llm.Provider` 接口，只需替换 `Provider` 参数即可：

```go
// OpenAI
agent, _ := ap.NewAgent(ap.WithProvider(openaiProvider))

// 切换到 Ollama 本地模型
agent, _ := ap.NewAgent(ap.WithProvider(ollamaProvider))

// 切换到 Mock（测试）
agent, _ := ap.NewAgent(ap.WithProvider(testutil.NewMockProvider()))
```

框架内部通过接口解耦，Agent 不关心底层是哪个 LLM。

---

### Q: WithMaxTurns 和 MaxTurns 有什么区别？

- **`WithMaxTurns(n)`**：`NewAgent` 的链式 `AgentOption`，推荐用法
- **`MaxTurns`**：`ReActConfig` 结构体的字段，旧入口用法（Deprecated）

两者功能相同，只是配置入口不同。新代码统一使用 `WithMaxTurns`。

---

### Q: 如何使用流式输出？

调用 `agent.StreamRun()` 返回 `<-chan StreamEvent` 通道：

```go
stream := agent.StreamRun(ctx, "分析这段代码")
for event := range stream {
    switch event.Type {
    case ap.StreamThinking:
        fmt.Printf("[思考] %s\n", event.Content)
    case ap.StreamToolCall:
        fmt.Printf("[工具] %s(%v)\n", event.ToolName, event.ToolArgs)
    case ap.StreamText:
        fmt.Printf("[输出] %s\n", event.Content)
    case ap.StreamDone:
        fmt.Println("[完成]")
    }
}
```

---

### Q: 如何实现多轮对话？

使用 `ap.NewSession` 自动管理对话历史：

```go
session := ap.NewSession(agent)

// 第一轮
session.Run(ctx, "你好，我叫小明")

// 第二轮——Agent 记得你的名字
session.Run(ctx, "我叫什么名字？")  // → "你叫小明"
```

Session 内部维护消息列表，每次 `Run` 自动携带完整上下文。

---

## 工具系统

### Q: 如何限制 Agent 的文件访问范围？

使用 `FileScopePolicy` + `ToolkitConfig.ScopePolicy`：

```go
policy := ap.FileScopePolicy{
    AllowDirs: []string{"/home/user/project"},
    DenyDirs:  []string{"/home/user/project/secrets"},
    ReadOnly:  []string{"/home/user/project/docs"},
}

toolkit := ap.NewToolkit(ap.ToolkitConfig{
    ScopePolicy: policy,
})
```

Agent 在执行文件操作时，会自动校验路径是否在允许范围内，越界操作返回权限错误。

---

### Q: 如何添加自定义工具？

实现 `ap.Tool` 接口，注册到 `ToolRegistry`：

```go
// 1. 实现 Tool 接口
type WeatherTool struct{}

func (t *WeatherTool) Name() string        { return "weather" }
func (t *WeatherTool) Description() string  { return "查询城市天气" }
func (t *WeatherTool) Parameters() ap.ParamSchema { /* ... */ }
func (t *WeatherTool) Run(ctx context.Context, args map[string]any) (string, error) {
    city := args["city"].(string)
    return fmt.Sprintf("%s: 晴，25°C", city), nil
}

// 2. 注册
registry := ap.NewToolRegistry()
registry.Register(&WeatherTool{})

// 3. 绑定到 Agent
agent, _ := ap.NewAgent(ap.WithTools(registry))
```

---

### Q: MCP Server 是什么？

**MCP（Model Context Protocol）** 是一种标准化的工具集成协议，允许 Agent 通过统一接口调用外部工具服务。AgentPrimordia 内置 MCP 客户端，可以：

- 连接任意 MCP 兼容的 Server
- 自动发现 Server 暴露的工具
- 将远程工具注册为本地 `ap.Tool`

```go
mcpClient := ap.NewMCPClient("http://localhost:8080/mcp")
tools, _ := mcpClient.DiscoverTools(ctx)
registry.RegisterMany(tools)
```

---

### Q: 如何开发插件？

实现 `ap.ToolPlugin` 接口，用 CLI 创建骨架：

```bash
# 创建插件骨架
ap plugin create my-plugin
```

生成的骨架包含 `ToolPlugin` 接口的完整实现模板。核心接口：

```go
type ToolPlugin interface {
    Name() string
    Version() string
    Tools() []ap.Tool
    Init(config map[string]any) error
    Close() error
}
```

插件通过 `tools.Plugin` 协议与核心解耦，`ecosystem/` 下的插件不依赖 `internal/` 任何模块。

---

## 记忆与 RAG

### Q: SQLite 和 InMemory 存储有什么区别？

| | SQLite | InMemory |
|---|--------|----------|
| 持久化 | ✅ 数据写入磁盘文件 | ❌ 进程退出即丢失 |
| 性能 | 略低（磁盘 I/O） | 更快（纯内存） |
| 适用场景 | 生产环境 | 单元测试、快速原型 |
| 创建方式 | `ap.WithSQLite("data.db")` | `ap.WithInMemory()` |

---

### Q: 如何实现 RAG？

`RAGStore` = `Memory` + `VectorStore` + `EmbeddingProvider`，三步搭建：

```go
// 1. 创建向量存储
vecStore := ap.NewInMemoryVectorStore()

// 2. 创建嵌入 Provider
embedder := ap.NewOllamaEmbedder("nomic-embed-text")

// 3. 组装 RAG Store
ragStore := ap.NewRAGStore(memory, vecStore, embedder)

// 4. 索引文档
ragStore.Index(ctx, "doc-id", "AgentPrimordia 是 Go 原生 Agent 框架...")

// 5. 检索相关上下文
results, _ := ragStore.Retrieve(ctx, "什么是 AgentPrimordia？", 5)
```

---

### Q: 向量存储选型？

| 数据规模 | 推荐方案 | 理由 |
|---------|---------|------|
| < 100K 向量 | `InMemoryVectorStore` | 零依赖，启动即用 |
| 100K – 1M | Qdrant | 高性能 ANN，Go SDK 成熟 |
| > 1M | Milvus | 分布式架构，水平扩展 |

小规模场景优先用内置 InMemory 方案，避免引入外部依赖。

---

### Q: 如何清理过期记忆？

调用 `store.CleanupExpired` 按时间清理：

```go
// 清理 30 天前的记忆
removed, err := store.CleanupExpired(ctx, 30)
fmt.Printf("清理了 %d 条过期记忆\n", removed)
```

建议在后台 goroutine 中定期执行：

```go
go func() {
    ticker := time.NewTicker(24 * time.Hour)
    for range ticker.C {
        store.CleanupExpired(ctx, 30)
    }
}()
```

---

## 编排与调度

### Q: Pool 和 DAG 有什么区别？

| | Pool | DAG |
|---|------|-----|
| 调度模型 | 并发任务调度（Worker Pool） | 有向无环图工作流 |
| 任务关系 | 任务之间无依赖，并发执行 | 任务之间有先后依赖，拓扑排序执行 |
| 适用场景 | 批量独立任务（如并行处理文件） | 流水线 / 多阶段工作流 |
| 失败策略 | 单任务失败不影响其他 | 下游任务自动跳过 |

---

### Q: Pipeline 和 Handoff 有什么区别？

| | Pipeline | Handoff |
|---|----------|---------|
| 执行方式 | 顺序执行，A → B → C | 动态交接，A 决定交给 B 或 C |
| Agent 角色 | 固定顺序，各管一段 | 运行时决定下一个 Agent |
| 适用场景 | 固定流水线（如：提取 → 清洗 → 存储） | 路由式场景（如：客服转专家、代码审查转安全审查） |

```go
// Pipeline：固定顺序
pipeline := ap.NewPipeline(
    ap.PipelineStep{Name: "step1", Agent: agent1},
    ap.PipelineStep{Name: "step2", Agent: agent2},
    ap.PipelineStep{Name: "step3", Agent: agent3},
)
pipeline.Run(ctx, input)

// Handoff：动态交接（Router 返回目标 Agent 在 Agents 中的下标）
handoff := ap.NewHandoff(ap.HandoffConfig{
    Agents: []ap.Agent{codeAgent, secAgent},
    Router: func(ctx context.Context, input string) int {
        if strings.Contains(input, "安全") {
            return 1
        }
        return 0
    },
})
handoff.Run(ctx, input)
```

---

### Q: 如何实现 Agent 间通信？

使用 `MessageBus`（发布-订阅模式）：

```go
bus := ap.NewLocalMessageBus()

// Agent A 注册消息处理器（也可 Subscribe 订阅通道）
bus.Register("agent-a", func(ctx context.Context, msg *ap.BusMessage) (*ap.BusMessage, error) {
    // 处理收到的消息
    return msg, nil
})

// Agent B 发送点对点消息
reply, err := bus.Send(ctx, &ap.BusMessage{
    From:    "agent-b",
    To:      "agent-a",
    Type:    ap.BusMsgTaskRequest,
    Content: "task-done",
})

// 广播给所有已注册 Agent（排除发送方）
bus.Broadcast(ctx, &ap.BusMessage{From: "agent-b", Content: "hello"})
```

支持三种模式：
- **Send**：点对点发送
- **Broadcast**：广播给所有已注册 Agent（排除发送方）
- **Subscribe**：订阅指定 Agent 的消息通道

---

## 生产部署

### Q: 如何实现高可用？

三层保障：

1. **ResilientProvider**：自动重试 + 故障转移 + 熔断
   ```go
   provider, err := ap.NewResilientProvider(openaiProvider, ap.DefaultResilientConfig())
   if err != nil { log.Fatal(err) }
   provider.AddFallback(azureProvider) // 主 Provider 不可用时切换到备用
   ```
   重试/退避参数可在 `ap.ResilientConfig{MaxRetries, RetryBackoff, ...}` 中自定义（默认 3 次重试、500ms 退避）。

2. **降级策略**：主 Provider 不可用时自动切换到备用 Provider

3. **K8s Operator + HPA**：基于 Agent 池利用率自动扩缩容

---

### Q: 如何监控 Agent 运行？

三件套：

1. **Prometheus Metrics**：内置指标（请求延迟、Token 消耗、工具调用次数）
2. **OpenTelemetry**：分布式追踪，跨 Agent 调用链可视化
3. **Grafana Dashboard**：预置仪表盘模板，开箱即用

```go
agent, _ := ap.NewAgent(
    ap.WithMetrics(prometheusExporter),
    ap.WithTracing(otelExporter),
)
```

---

### Q: 如何控制成本？

三层成本控制：

1. **CostTracker**：实时追踪 Token 消耗和费用
   ```go
   tracker := ap.NewCostTracker(ap.BudgetConfig{
       MaxDailySpend:  10.0,  // 日预算 $10
       MaxRequestCost: 0.5,   // 单次请求上限 $0.5
   })
   ```

2. **BudgetConfig**：超预算自动熔断，拒绝新请求

3. **缓存策略**：相同 Prompt 缓存响应，减少重复调用

---

### Q: 如何做安全防护？

四层防护体系：

1. **ACL（访问控制列表）**：限制 Agent 可调用的工具和资源
2. **Sandbox（沙箱）**：隔离执行环境，限制文件/网络访问
3. **Guardrails（护栏）**：输入/输出内容过滤，防止注入和泄露
4. **PII 检测**：自动识别和脱敏个人身份信息

```go
// 工具白名单：只注册允许的内置工具，未注册的不可被调用
registry := ap.NewToolRegistry()
registry.RegisterMultiple(ap.NewWeb(), ap.NewCalculator())

// 文件访问范围限制
fsTool, err := ap.NewFileSystem("/data/workspace") // 越界访问被 scope 拒绝
if err != nil { log.Fatal(err) }
registry.Register(fsTool)

// 护栏 + PII 检测：经 Guardrail 引擎注入输入端护栏
engine := ap.NewGuardrailEngine()
engine.AddRule(ap.NewPromptInjectionRule(ap.PromptInjectionConfig{})) // 注入检测
engine.AddRule(ap.NewPIIRule(ap.PIIRuleConfig{}))                     // PII 脱敏

agent, err := ap.NewAgent("secure-bot", "你是一个助手", provider,
    ap.WithToolkit(registry),
    ap.WithInputGuard(func(content string) (string, bool, error) {
        report, err := engine.Check(content, ap.CheckInput)
        if err != nil {
            return content, false, err
        }
        if !report.Passed {
            return content, true, nil // blocked=true 拒绝该输入
        }
        return content, false, nil
    }))
if err != nil { log.Fatal(err) }
```

---

## 迁移与兼容

### Q: 从 v0.x 升级到 v0.7 需要注意什么？

**核心变更**：`ReActConfig` 的核心字段已迁移到函数式 Option（`ap.NewAgent(name, prompt, model, opts...)`），推荐直接使用新入口。

迁移对照表：

| 旧写法（Deprecated） | 新写法 |
|---------------------|--------|
| `ReActConfig.Name` | `NewAgent(name, systemPrompt, model, ...)` 第 1 参 |
| `ReActConfig.SystemPrompt` | `NewAgent(name, systemPrompt, model, ...)` 第 2 参 |
| `ReActConfig.Model` | `NewAgent(name, systemPrompt, model, ...)` 第 3 参（llm.Provider） |
| `ReActConfig.MaxTurns` | `WithMaxTurns(n)` |
| `ReActConfig.Temperature` | `WithTemperature(f)` |
| `ReActConfig.SessionID` | `WithSessionID(id)` |
| `ReActConfig.Memory`（原 Tools 等分组配置） | `WithMemory(m)` / `WithToolkit(r)` |

重试不作为 Agent 配置项：LLM 调用的重试/故障转移/熔断统一在 Provider 层处理——`ap.NewResilientProvider(primary, ap.DefaultResilientConfig())`。

详细迁移指南参见 [v0-deprecations.md](migration/v0-deprecations.md)。

---

### Q: API 稳定性策略是什么？

四级分类：

| 级别 | 含义 | 兼容承诺 |
|------|------|---------|
| **Stable** | 稳定 API | 主版本内不破坏兼容 |
| **Experimental** | 实验性 API | 可能变更，需显式 opt-in |
| **Deprecated** | 已废弃 API | 保留至少 2 个次版本后移除 |
| **Internal** | 内部 API | 不对外暴露，随时可变 |

判断方式：
- Stable：无任何标注
- Experimental：注释标记 `// Experimental: ...`
- Deprecated：注释标记 `// Deprecated: ...`
- Internal：位于 `internal/` 目录下

---

> 还有其他问题？欢迎在 [GitHub Issues](https://github.com/your-org/AgentPrimordia/issues) 提交。
