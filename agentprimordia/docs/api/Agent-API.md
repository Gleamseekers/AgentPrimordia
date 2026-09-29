# Agent API

Agent 是框架的核心抽象，所有编排模式和 Pool 均面向此接口编程。

## 核心接口

```go
type Agent interface {
    Run(ctx context.Context, input Message) (*Response, error)
    StreamRun(ctx context.Context, input Message) (<-chan StreamEvent, error)
    Name() string
    Status() AgentStatus
    Stats() AgentStats
    Stop() error
}
```

## 构造 Agent

```go
import ap "github.com/Gleamseekers/AgentPrimordia/pkg"

agent, err := ap.NewAgent("my-agent", "You are a helpful assistant.", provider,
    ap.WithMaxTurns(20),
    ap.WithTemperature(0.7),
)
```

## 链式能力注入

`NewAgent` 返回的 `*CapabilityAgent` 支持链式注入能力：

```go
agent, err := ap.NewAgent("my-agent", "You are a helpful assistant.", provider).
    WithToolkit(toolkit).
    WithMemory(memStore).
    WithHooks(hooks).
    WithHITL(hitlCfg).
    WithPlanner(planner).
    WithReflector(reflector).
    WithToolLearner(learner).
    WithTracer(tracer).
    WithMetrics(metricsRecorder)
```

## 运行

```go
resp, err := agent.Run(ctx, ap.Message{
    Role:    ap.RoleUser,
    Content: "帮我分析这段代码",
})
fmt.Println(resp.Content)
```

## 流式运行

```go
ch, err := agent.StreamRun(ctx, input)
for event := range ch {
    switch event.Type {
    case ap.StreamEventThought:
        fmt.Print(event.Content)
    case ap.StreamEventToolCall:
        fmt.Printf("[调用工具: %s]\n", event.ToolName)
    case ap.StreamEventDone:
        fmt.Println("\n[完成]")
    }
}
```

## 相关类型

- `Message`：对话消息（Role + Content + ToolCalls）
- `Response`：最终响应（Content + Usage + Metrics）
- `AgentStatus`：Idle / Running / Stopped / Error
- `AgentStats`：运行统计（轮次、工具调用次数等）
