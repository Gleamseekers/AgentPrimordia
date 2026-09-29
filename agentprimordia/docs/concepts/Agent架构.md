# Agent 架构

AgentPrimordia 的核心是 **协议式微内核架构**，通过 14 个 Capable 接口实现能力发现与组合。

## 核心设计原则

### 1. 协议优于配置

Agent 通过类型断言发现能力，而非配置文件：

=== "Go"

    ```go
    import ap "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg"

    // 创建 Agent 时通过 Functional Options 注入能力
    agent, err := ap.NewAgent("my-agent", "你是助手", provider,
        ap.WithMaxTurns(10),
    )

    // 构造后还可通过链式 API 按需注入工具、记忆、RAG 等能力
    agent = agent.WithToolkit(toolkit).
        WithMemory(memory).
        WithRAG(ragConfig)
    ```

=== "TypeScript"

    ```typescript
    import { ReActAgent, OpenAIProvider, ToolRegistry } from '@agentprimordia/sdk';

    // 创建 Agent 时通过配置注入能力
    const agent = new ReActAgent({
      name: 'my-agent',
      model: new OpenAIProvider({ apiKey: process.env.OPENAI_API_KEY!, model: 'gpt-4o' }),
      toolkit: new ToolRegistry(),
      maxTurns: 10,
      systemPrompt: '你是助手',
    });
    ```

### 2. 接口组合

Agent 通过实现不同 `*Capable` 接口组合获得不同能力：

=== "Go"

    所有能力通过 `WithXxx()` Option 注入：

    ```go
    agent, err := ap.NewAgent("assistant", "你是助手", provider,
        ap.WithMaxTurns(10),
        ap.WithMemory(memory),       // MemoryCapable
        ap.WithToolkit(toolkit),     // ToolCapable
        ap.WithRAG(ragConfig),       // RAGCapable
        ap.WithHooks(hooks),         // HookCapable
        ap.WithMetrics(recorder),    // MetricsCapable
        ap.WithTracer(tracer),       // TraceCapable
        ap.WithCostTracker(tracker), // CostCapable
    )
    ```

=== "TypeScript"

    所有能力通过构造配置注入：

    ```typescript
    const agent = new ReActAgent({
      name: 'assistant',
      model: provider,
      toolkit: registry,       // ToolCapable
      maxTurns: 10,
      systemPrompt: '你是助手',
      hooks: hookManager,      // LifecycleCapable
      // memory、RAG 等通过链式 API 或配置注入
    });
    ```

### 3. 生命周期钩子

ReAct 循环的每个阶段都可以注入自定义逻辑：

```go
hooks := ap.NewHookManager()

// Register(point, fn)：fn 签名为 func(ctx context.Context, hctx *ap.HookContext) error
hooks.Register(ap.HookBeforeLLM, func(ctx context.Context, hctx *ap.HookContext) error {
    fmt.Println("即将调用 LLM...")
    return nil
})
hooks.Register(ap.HookAfterTool, func(ctx context.Context, hctx *ap.HookContext) error {
    fmt.Printf("工具 %s 执行完成\n", hctx.ToolCall.Name)
    return nil
})

agent, err := ap.NewAgent("assistant", "你是助手", provider,
    ap.WithMaxTurns(10),
    ap.WithHooks(hooks),
)
```

## Agent 创建方式

=== "Go"

    **推荐方式：`ap.NewAgent()` （v0.7.0+）**

    ```go
    agent, err := ap.NewAgent("assistant", "你是助手", provider,
        ap.WithMaxTurns(10),
        ap.WithToolkit(toolkit),
        ap.WithMemory(memory),
    )

    resp, err := agent.Run(ctx, ap.UserMessage("你好"))
    ```

    **链式 API：**

    链式方法（`WithToolkit` / `WithMemory` / `WithRAG` 等）定义在
    `*CapabilityAgent` 上，可在构造后继续注入能力：

    ```go
    agent, err := ap.NewAgent("assistant", "你是助手", provider,
        ap.WithMaxTurns(10),
    )
    agent = agent.WithToolkit(toolkit).
        WithMemory(memory).
        WithRAG(ragConfig)
    ```

    > 注：不存在 `ap.NewAgent(ap.ReActConfig{...})` 形式的 struct 构造入口；
    > `ReActConfig` 由框架内部使用，公共 API 统一为 `NewAgent` + Option。

    **传统方式：`ap.NewAgent()` （向后兼容）**

    ```go
    agent, err := ap.NewAgent("assistant", "你是助手", provider,
        ap.WithMaxTurns(10),
        ap.WithToolkit(toolkit),
        ap.WithMemory(memory),
        ap.WithTemperature(0.7),
    )
    ```

=== "TypeScript"

    **`ReActAgent` 构造器：**

    ```typescript
    import { ReActAgent, OpenAIProvider, ToolRegistry } from '@agentprimordia/sdk';

    const agent = new ReActAgent({
      name: 'assistant',
      model: new OpenAIProvider({ apiKey: process.env.OPENAI_API_KEY!, model: 'gpt-4o' }),
      toolkit: new ToolRegistry(),
      maxTurns: 10,
      systemPrompt: '你是助手',
    });

    const resp = await agent.run('你好');
    console.log(resp.content);
    ```

    **使用 MockProvider（无需 API Key）：**

    ```typescript
    import { ReActAgent, MockProvider, ToolRegistry } from '@agentprimordia/sdk';

    const agent = new ReActAgent({
      name: 'test-agent',
      model: new MockProvider({ response: 'Hello!' }),
      toolkit: new ToolRegistry(),
      maxTurns: 5,
    });
    ```

### 编排 Agent

支持多 Agent 协作的高级编排模式：

=== "Go"

    ```go
    // DAG 编排（NodeHandler 签名为 func(ctx, input string) (string, error)）
    dag, _ := ap.NewDAGBuilder("data-analysis").
        Node("analyze", func(ctx context.Context, input string) (string, error) {
            resp, err := analyzeAgent.Run(ctx, ap.UserMessage(input))
            if err != nil {
                return "", err
            }
            return resp.Content, nil
        }).
        Node("report", func(ctx context.Context, input string) (string, error) {
            resp, err := reportAgent.Run(ctx, ap.UserMessage(input))
            if err != nil {
                return "", err
            }
            return resp.Content, nil
        }).
        Edge("analyze", "report").
        Build()

    result, _ := dag.Run(ctx, "分析销售数据")
    ```

=== "TypeScript"

    ```typescript
    import { DAGBuilder } from '@agentprimordia/sdk';

    // DAG 编排
    const dag = new DAGBuilder('data-analysis')
      .node('analyze', async (input: string) => {
        const resp = await analyzeAgent.run(input);
        return resp.content;
      })
      .node('report', async (input: string) => {
        const resp = await reportAgent.run(input);
        return resp.content;
      })
      .edge('analyze', 'report')
      .build();

    const result = await dag.run('分析销售数据');
    ```

## 14 个 Capable 接口

| 接口 | 能力 | 典型用途 |
|------|------|----------|
| `MemoryCapable` | 记忆存储与检索 | 长期记忆、上下文保持 |
| `RAGCapable` | 检索增强生成 | 知识库问答、文档检索 |
| `ToolCapable` | 工具调用 | 执行外部操作 |
| `ContextCapable` | 上下文管理 | 会话管理、历史追踪 |
| `OrchestrationCapable` | 多 Agent 编排 | 任务分解、协作 |
| `LifecycleCapable` | 生命周期钩子 | 监控、日志、拦截 |
| `RetryCapable` | 重试策略 | 容错、稳定性 |
| `TimeoutCapable` | 超时控制 | 防止阻塞 |
| `CircuitBreakerCapable` | 熔断器 | 服务降级 |
| `MetricsCapable` | 指标收集 | 性能监控 |
| `TraceCapable` | 链路追踪 | 调试、分析 |
| `ConfigCapable` | 动态配置 | 运行时调整 |
| `HealthCheckCapable` | 健康检查 | 运维监控 |
| `InspectorCapable` | 检查器集成 | 可视化调试 |

## 架构优势

1. **零配置能力发现**：无需配置文件，通过类型断言即可发现 Agent 能力
2. **高度可扩展**：新增能力只需定义新接口，不影响现有代码
3. **组合优于继承**：通过接口组合而非继承获得能力，避免类爆炸
4. **生产级可靠性**：内置重试、熔断、超时等生产必需特性
5. **可观测性**：原生支持指标、追踪、日志，便于调试和监控

## 下一步

- 深入了解 [ReAct 循环](ReAct循环.md) 的工作原理
- 学习如何 [创建 Agent](../guides/创建Agent.md)
- 查看 [API 参考](../api/Agent-API.md) 了解详细接口定义
