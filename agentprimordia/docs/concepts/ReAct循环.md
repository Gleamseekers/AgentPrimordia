# ReAct 循环

ReAct（Reasoning + Acting）是 AgentPrimordia 的核心执行引擎，实现了推理与行动的交替循环。

## 工作原理

ReAct 循环由三个阶段组成：

```
┌─────────────────────────────────────────┐
│  1. Think（推理）                        │
│     - LLM 分析当前状态                  │
│     - 决定下一步行动                    │
│     - 输出思考过程或动作指令            │
└─────────────┬───────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────┐
│  2. Act（行动）                          │
│     - 执行工具调用                      │
│     - 获取执行结果                      │
│     - 更新上下文                        │
└─────────────┬───────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────┐
│  3. Observe（观察）                      │
│     - 分析行动结果                      │
│     - 判断是否达成目标                  │
│     - 决定继续循环或返回结果            │
└─────────────────────────────────────────┘
```

## 使用方式

ReAct 循环由 `Agent.Run()` 方法内部驱动，用户无需手动管理循环：

=== "Go"

    ```go
    package main

    import (
        "context"
        "fmt"
        ap "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg"
    )

    func main() {
        agent, err := ap.NewAgent("assistant", "你是一个专业的助手",
            provider,
            ap.WithMaxTurns(10), // 控制最大迭代次数
        )
        if err != nil {
            panic(err)
        }

        // Run 内部自动执行 Think → Act → Observe 循环
        response, err := agent.Run(ctx, ap.UserMessage("帮我分析这段代码"))
        if err != nil {
            panic(err)
        }
        fmt.Println(response.Content)
    }
    ```

=== "TypeScript"

    ```typescript
    import { ReActAgent, OpenAIProvider, ToolRegistry } from '@agentprimordia/sdk';

    const agent = new ReActAgent({
      name: 'assistant',
      model: new OpenAIProvider({ apiKey: process.env.OPENAI_API_KEY!, model: 'gpt-4o' }),
      toolkit: new ToolRegistry(),
      maxTurns: 10,
      systemPrompt: '你是一个专业的助手',
    });

    // run() 内部自动执行 Think → Act → Observe 循环
    const response = await agent.run('帮我分析这段代码');
    console.log(response.content);
    ```

### 流式运行

使用 `StreamRun()` / `stream()` 可逐 token 获取推理过程：

=== "Go"

    ```go
    // StreamRun 返回事件通道，需双值接收并处理错误
    events, err := agent.StreamRun(ctx, ap.UserMessage("分析这段代码"))
    if err != nil {
        panic(err)
    }
    for event := range events {
        switch event.Type {
        case ap.StreamEventThought:
            fmt.Printf("[思考] %s\n", event.Content)
        case ap.StreamEventToolCall:
            fmt.Printf("[工具调用] %s\n", event.Content)
        case ap.StreamEventToolResult:
            fmt.Printf("[工具结果] %s\n", event.Content)
        case ap.StreamEventComplete:
            fmt.Printf("[完成] %s\n", event.Content)
        }
    }
    ```

=== "TypeScript"

    ```typescript
    for await (const chunk of agent.stream('分析这段代码')) {
      // chunk 是逐 token 输出的文本片段
      process.stdout.write(chunk);
    }
    ```

## 生命周期钩子

ReAct 循环的每个阶段都可以注入自定义逻辑：

=== "Go"

    通过 `NewHookManager()` 创建钩子管理器，在 HookPoint 上注册处理函数，
    再经 `WithHooks()` Option 注入：

    ```go
    hooks := ap.NewHookManager()

    // Register(point, fn)：fn 签名为 func(ctx context.Context, hctx *ap.HookContext) error
    hooks.Register(ap.HookBeforeLLM, func(ctx context.Context, hctx *ap.HookContext) error {
        log.Printf("Turn %d: 即将调用 LLM", hctx.Turn)
        return nil
    })
    hooks.Register(ap.HookBeforeTool, func(ctx context.Context, hctx *ap.HookContext) error {
        if !isAllowed(hctx.ToolCall.Name) {
            return fmt.Errorf("工具 %s 不允许执行", hctx.ToolCall.Name)
        }
        return nil
    })
    hooks.Register(ap.HookAfterTool, func(ctx context.Context, hctx *ap.HookContext) error {
        log.Printf("工具 %s 执行完成", hctx.ToolCall.Name)
        return nil
    })

    agent, err := ap.NewAgent("assistant", "你是助手", provider,
        ap.WithMaxTurns(10),
        ap.WithHooks(hooks),
    )
    ```

=== "TypeScript"

    通过 `HookManager` 注入：

    ```typescript
    import { ReActAgent, HookManager, OpenAIProvider, ToolRegistry } from '@agentprimordia/sdk';

    const hooks = new HookManager();
    hooks.register('before_llm', (ctx) => {
      console.log(`Turn ${ctx.turn}: 即将调用 LLM`);
    });
    hooks.register('after_tool', (ctx) => {
      console.log(`工具 ${ctx.toolCall?.name} 执行完成`);
    });

    const agent = new ReActAgent({
      name: 'assistant',
      model: new OpenAIProvider({ apiKey: process.env.OPENAI_API_KEY!, model: 'gpt-4o' }),
      toolkit: new ToolRegistry(),
      maxTurns: 10,
      hooks,
    });
    ```

支持的钩子点（HookPoint，共 20+，此处为常用部分）：

| 钩子点 | 触发时机 | 典型用途 |
|------|----------|----------|
| `HookBeforeLLM` | LLM 调用前 | 日志、输入预处理 |
| `HookAfterLLM` | LLM 响应后 | 结果记录 |
| `HookBeforeTool` | 工具执行前 | 权限检查、参数验证 |
| `HookAfterTool` | 工具执行后 | 结果记录、指标收集 |
| `HookOnComplete` | 循环结束 | 资源清理 |
| `HookOnError` | 发生错误 | 错误上报 |

此外可通过 `RegisterWithPriority(point, fn, priority)` 控制执行优先级，
通过 `Use(middleware)` 注入钩子中间件。

## 最大迭代控制

通过 `WithMaxTurns()` / `maxTurns` 防止无限循环（默认 50）：

=== "Go"

    ```go
    agent, err := ap.NewAgent("assistant", "你是助手", provider,
        ap.WithMaxTurns(20), // 最大 20 轮
    )
    ```

=== "TypeScript"

    ```typescript
    const agent = new ReActAgent({
      name: 'assistant',
      model: provider,
      toolkit: new ToolRegistry(),
      maxTurns: 20, // 最大 20 轮
    });
    ```

## 错误处理

ReAct 循环中的错误处理策略：

1. **Think 错误**（LLM 调用失败）：立即返回，停止循环
2. **Act 错误**（工具执行失败）：错误信息注入上下文，继续下一轮推理
3. **上下文取消**：通过 `ctx.Done()` 检查，100ms 内响应取消

对于 LLM 调用失败，建议使用 `ResilientProvider` 包装：

=== "Go"

    ```go
    resilient, err := ap.NewResilientProvider(primary, ap.DefaultResilientConfig())
    if err != nil {
        panic(err)
    }
    resilient.AddFallback(fallbackProvider)

    agent, err := ap.NewAgent("assistant", "你是助手", resilient,
        ap.WithMaxTurns(10),
    )
    ```

=== "TypeScript"

    ```typescript
    import { ResilientProvider } from '@agentprimordia/sdk';

    const resilient = new ResilientProvider(primaryProvider);
    resilient.addFallback(fallbackProvider);

    const agent = new ReActAgent({
      name: 'assistant',
      model: resilient,
      toolkit: new ToolRegistry(),
      maxTurns: 10,
    });
    ```

## 上下文传递

ReAct 循环通过 `context.Context` 传递请求 ID、会话 ID 等信息：

```go
// 注入请求 ID（用于链路追踪）
ctx := ap.WithRequestID(context.Background(), ap.NewRequestID())

// 设置会话 ID（关联记忆）
agent, err := ap.NewAgent("assistant", "你是助手", provider,
    ap.WithSessionID("session-123"),
)

result, err := agent.Run(ctx, ap.UserMessage("任务"))
```

## 性能优化

### 缓存 LLM 响应

使用 `CachedProvider` 包装 LLM Provider，对相同输入缓存响应：

```go
// 指纹精确匹配缓存（无需 Embedding）
cache := ap.NewFingerprintCache(10000, time.Hour)

// NewCachedProvider(inner, cache, minScore)：minScore 为语义相似度阈值
cached, err := ap.NewCachedProvider(provider, cache, 0.8)
if err != nil {
    panic(err)
}
agent, err := ap.NewAgent("assistant", "你是助手", cached,
    ap.WithMaxTurns(10),
)
```

### 并行工具调用

当 LLM 在一轮推理中输出多个工具调用时，框架自动并行执行：

```go
// LLM 返回多个 tool_calls 时自动并行执行
// 无需额外配置
```

### BufferPool 优化

框架内部使用 `sync.Pool` 复用 `bytes.Buffer`，减少 LLM 请求体构造和
SSE chunk 解析热路径上的内存分配。此优化对用户透明，无需配置。

## 调试技巧

### 使用 `ap debug` 命令

启动调试服务器，查看实时追踪：

```bash
ap debug  # 启动调试服务器 http://localhost:6060
```

### 使用 `ap loop` 命令

查看 Agent 执行追踪和状态，从检查点恢复：

```bash
ap loop trace     # 查看 Agent 执行追踪
ap loop inspect   # 查看 Agent 当前状态
ap loop resume    # 从检查点恢复运行
```

### 使用 pprof 性能分析

```go
mux := http.NewServeMux()
ap.RegisterPProfSecure(mux)
go http.ListenAndServe("127.0.0.1:6060", mux)
```

```bash
# CPU 分析
go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30

# 内存分析
go tool pprof http://localhost:6060/debug/pprof/heap
```

## 最佳实践

1. **设置合理的 `MaxTurns`**：防止无限循环消耗资源（默认 50）
2. **使用 `WithHooks` 记录日志和监控**：便于调试和审计
3. **实现幂等的工具**：支持重试和恢复
4. **及时调用 `Close()`**：释放 Agent 持有的资源
5. **使用 `context.WithTimeout`**：防止长时间阻塞
6. **用 `ResilientProvider` 包装 LLM**：自动重试和故障转移

## 下一步

- 学习 [工具系统](工具系统.md) 如何与 ReAct 循环集成
- 查看 [多 Agent 编排](多Agent编排.md) 如何组合多个 ReAct 循环
- 阅读 [API 参考](../api/Agent-API.md) 了解详细接口定义
