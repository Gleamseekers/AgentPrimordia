# A2A API

Agent-to-Agent 通信协议 API 参考文档。

> **两套客户端，不要混用**（v4.0-1 起旧版 HTTP JSON-RPC 传输已移除，见
> [`docs/版本规范.md`](../版本规范.md)「已移除记录」）：
>
> | 面 | 入口 | 传输 | 用途 |
> |----|------|------|------|
> | ap A2A（Stable） | `ap.NewA2AGRPCServer` / `ap.NewA2AGRPCClient` | gRPC | ap 集群内网高性能传输 |
> | 开放协议互操作 | `ap.NewOpenInteropServer` / `ap.NewOpenInteropClient` | JSON-RPC 2.0 over HTTP + SSE | 跨生态（第三方 Agent 遵循开放 Agent2Agent 规范） |
>
> 公共 API 经 `pkg/a2a.go`（Stable）与 `pkg/a2a_interop.go` 导出；`internal/agent/a2a/`
> 为实现，不提供兼容承诺。以下签名逐行核对自 `internal/agent/a2a/grpc_client.go`、
> `interop_client.go`、`grpc_server.go`、`interop_server.go`。

## 一、ap A2A（gRPC 传输，Stable）

### 服务端

```go
func NewA2AAgentCard(agentID, name string) *A2AAgentCard
func NewA2ATaskManager() *A2ATaskManagerImpl
func NewA2AService(card *A2AAgentCard, tm A2ATaskManager, opts ...A2AServiceOption) *A2AService
func NewA2AGRPCServer(service *A2AService, opts ...A2AGRPCServerOption) *A2AGRPCServer
func NewA2AGRPCServerWithService(service *A2AService, opts ...A2AGRPCServerOption) *grpc.Server
```

`A2AGRPCServer` 方法：

| 方法 | 签名 | 说明 |
|------|------|------|
| `Register` | `Register(server *grpc.Server)` | 把 A2A 服务注册到既有 `*grpc.Server` |
| `Metrics` | `Metrics() A2AMetricsSnapshot` | 拦截器指标快照 |

> `NewA2AGRPCServerWithService` 内部完成 `grpc.NewServer` + `Register`，返回可直接
> `Serve(lis)` 的 `*grpc.Server`——推荐用法，用户无需自己 import grpc。
> 没有 `Start(addr)` 方法，也不再有 HTTP `Handler()`（那属于下面的开放协议面）。

**服务端选项**

| 选项 | 签名 |
|------|------|
| `WithGRPCAuth` | `WithGRPCAuth(auth A2AGRPCAuthFunc) A2AGRPCServerOption` |
| `WithGRPCLogger` | `WithGRPCLogger(logger *slog.Logger) A2AGRPCServerOption` |
| `WithGRPCMetrics` | `WithGRPCMetrics(m *A2AInterceptorMetrics) A2AGRPCServerOption` |
| `WithGRPCSlowRequestThreshold` | `WithGRPCSlowRequestThreshold(d time.Duration) A2AGRPCServerOption` |

**Service 选项**

| 选项 | 签名 |
|------|------|
| `WithA2AServiceLogger` | `WithA2AServiceLogger(logger *slog.Logger) A2AServiceOption` |
| `WithA2AServiceTaskHandler` | `WithA2AServiceTaskHandler(handler A2ATaskHandler) A2AServiceOption` |

**示例**

```go
card := ap.NewA2AAgentCard("agent-1", "ResearchAgent")
card.Description = "研究助手"
card.Skills = []ap.A2AAgentSkill{
    {ID: "search", Name: "搜索", Description: "搜索并总结信息"},
}

svc := ap.NewA2AService(card, ap.NewA2ATaskManager())
grpcServer := ap.NewA2AGRPCServerWithService(svc) // 已 Register，可直接 Serve

lis, err := net.Listen("tcp", ":50051")
if err != nil {
    log.Fatalf("监听失败: %v", err)
}
if err := grpcServer.Serve(lis); err != nil {
    log.Fatalf("服务失败: %v", err)
}
```

### 客户端

```go
func NewA2AGRPCClient(target string, opts ...A2AGRPCClientOption) (*A2AGRPCClient, error)
func NewA2AGRPCClientWithConn(conn *grpc.ClientConn, opts ...A2AGRPCClientOption) *A2AGRPCClient
```

`A2AGRPCClient` 方法集（`internal/agent/a2a/grpc_client.go`）：

| 方法 | 签名 | 说明 |
|------|------|------|
| `FetchAgentCard` | `FetchAgentCard(ctx context.Context) (*A2AAgentCard, error)` | 获取远端 Agent Card |
| `CreateTask` | `CreateTask(ctx context.Context, message *A2AMessage, taskID string) (*A2ATask, error)` | 创建任务 |
| `GetTask` | `GetTask(ctx context.Context, taskID string) (*A2ATask, error)` | 获取任务 |
| `CancelTask` | `CancelTask(ctx context.Context, taskID string) (*A2ATask, error)` | 取消任务 |
| `StreamEvents` | `StreamEvents(ctx context.Context, taskID string) (<-chan *TaskEvent, error)` | 流式订阅任务事件 |
| `Close` | `Close() error` | 关闭连接 |
| `WithTraceContext` / `StartTrace` / `ContinueTrace` | 见 `pkg/a2a.go` | 追踪上下文传播 |

> **没有 `SendTask` 方法**（那是开放协议客户端的）；`CreateTask` 的 message 参数是
> `*A2AMessage`，不是裸字符串。

**客户端选项**

| 选项 | 签名 |
|------|------|
| `WithA2AGRPCClientTLS` | `WithA2AGRPCClientTLS(config A2ATLSConfig) A2AGRPCClientOption` |
| `WithA2AGRPCClientCredentials` | `WithA2AGRPCClientCredentials(creds credentials.TransportCredentials) A2AGRPCClientOption` |
| `WithA2AGRPCClientAPIKey` | `WithA2AGRPCClientAPIKey(key string) A2AGRPCClientOption` |
| `WithA2AGRPCClientBearerToken` | `WithA2AGRPCClientBearerToken(token string) A2AGRPCClientOption` |
| `WithA2AGRPCClientLogger` | `WithA2AGRPCClientLogger(logger *slog.Logger) A2AGRPCClientOption` |

**示例**

```go
client, err := ap.NewA2AGRPCClient("localhost:50051",
    ap.WithA2AGRPCClientBearerToken("my-token"),
)
if err != nil {
    log.Fatalf("连接失败: %v", err)
}
defer client.Close()

card, err := client.FetchAgentCard(ctx) // 发现
if err != nil {
    log.Fatalf("发现失败: %v", err)
}

task, err := client.CreateTask(ctx, &ap.A2AMessage{
    Role:  "user",
    Parts: []ap.A2APart{ap.NewA2ATextPart("帮我分析数据")},
}, "")
if err != nil {
    log.Fatalf("创建任务失败: %v", err)
}

// 流式订阅（无需轮询）
ch, err := client.StreamEvents(ctx, task.ID)
if err != nil {
    log.Fatalf("订阅失败: %v", err)
}
for ev := range ch {
    fmt.Printf("event: type=%s task=%s\n", ev.Type, ev.TaskID)
}

// 或轮询：GetTask 失败返回 (nil, err)，禁止 _ 丢弃后解构
got, err := client.GetTask(ctx, task.ID)
if err != nil {
    log.Fatalf("查询失败: %v", err)
}
fmt.Println(got.State)
```

## 二、开放协议互操作（JSON-RPC over HTTP + SSE）

### 服务端

```go
func NewOpenInteropServer(card OpenAgentCard, cfg InteropConfig) *OpenInteropServer
func DefaultInteropConfig() InteropConfig
func GenerateInteropReport(card OpenAgentCard, cfg InteropConfig) InteropReport
```

| 方法 | 签名 | 说明 |
|------|------|------|
| `Handler` | `Handler() http.Handler` | HTTP 处理入口（含 `/.well-known/agent.json` 与 `/a2a/v1`） |
| `WithExecutor` | `WithExecutor(executor TaskExecutor) *OpenInteropServer` | 设置任务执行器 |

内置执行器：`ap.NewEchoTaskExecutor()` / `ap.NewSimpleTaskExecutor()`。

### 客户端

```go
func NewOpenInteropClient(baseURL string) *OpenInteropClient
func NewTextMessage(role, text string) OpenMessage
```

| 方法 | 签名 | 说明 |
|------|------|------|
| `FetchAgentCard` | `FetchAgentCard(ctx context.Context) (*OpenAgentCard, error)` | 获取远端 Agent Card |
| `SendTask` | `SendTask(ctx context.Context, message OpenMessage) (*OpenTask, error)` | 发送任务 |
| `GetTask` | `GetTask(ctx context.Context, taskID string) (*OpenTask, error)` | 获取任务 |
| `CancelTask` | `CancelTask(ctx context.Context, taskID string) error` | 取消任务 |

> `SendTask` 失败返回 `(nil, err)`，同样禁止 `_` 丢弃后解构 `task.ID`。

**示例**

```go
// 服务方：暴露开放协议互操作面（HTTP + SSE）
card := ap.OpenAgentCard{Name: "agent-1", Description: "研究助手"}
server := ap.NewOpenInteropServer(card, ap.DefaultInteropConfig()).
    WithExecutor(ap.NewEchoTaskExecutor()) // 回声执行器；生产用自定义 ap.TaskExecutor
if err := http.ListenAndServe(":8080", server.Handler()); err != nil {
    log.Fatalf("监听失败: %v", err)
}

// 调用方：发现 → 委托 → 轮询 → 取消
client := ap.NewOpenInteropClient("http://localhost:8080")

_, err := client.FetchAgentCard(ctx)
if err != nil {
    log.Fatalf("发现失败: %v", err)
}

task, err := client.SendTask(ctx, ap.NewTextMessage("user", "帮我分析数据"))
if err != nil {
    log.Fatalf("委托失败: %v", err)
}

for {
    got, err := client.GetTask(ctx, task.ID)
    if err != nil {
        log.Fatalf("查询失败: %v", err)
    }
    if got.Status.State == ap.OpenTaskCompleted || got.Status.State == ap.OpenTaskFailed {
        break
    }
    time.Sleep(200 * time.Millisecond)
}

if err := client.CancelTask(ctx, task.ID); err != nil {
    log.Fatalf("取消失败: %v", err)
}
```

**协议符合性自检**

```bash
ap a2a interop-check
```

```go
report := ap.GenerateInteropReport(ap.OpenAgentCard{Name: "agent-1"}, ap.DefaultInteropConfig())
fmt.Println(report.Score, report.FailedChecks())
```

## Task 状态机

```
submitted → working → completed
     ↓          ↓
  rejected   input-required
                ↓
            canceled / failed
```

终态（不可变更）：`completed`、`failed`、`canceled`、`rejected`

开放协议侧状态为 `OpenTaskState`：`submitted` / `working` / `input-required` /
`completed` / `failed` / `canceled`（导出常量见 `pkg/a2a_interop.go` 的 `OpenTask*`）。

## 核心类型

| 类型 | 说明 |
|------|------|
| `A2AAgentCard` | Agent 能力卡片（ID、名称、技能、端点、安全方案） |
| `A2AAgentSkill` | Agent 技能描述 |
| `A2ATask` | 任务对象（ID、状态、消息、产物） |
| `A2ATaskState` | 任务状态（submitted / working / completed / failed / canceled / rejected / input-required） |
| `A2AMessage` | 消息（角色 + Parts 多模态内容） |
| `A2APart` / `A2ATextPart` / `A2AFilePart` / `A2ADataPart` | 消息内容片段（文本 / 文件 / 数据） |
| `A2ATaskHandler` | 任务处理器接口 |
| `A2APrincipal` | 认证主体 |
| `A2AAuthenticator` | 认证器接口 |
| `OpenAgentCard` / `OpenMessage` / `OpenPart` / `OpenTask` | 开放协议侧对等类型 |
| `TaskExecutor` | 开放协议任务执行器接口 |
| `TaskEvent` | 流式事件（Type / TaskID / State / Message / Artifact / Error） |

构造器：`ap.NewA2ATextPart(text)`、`ap.NewA2AFilePartFromURI(uri, mime)`、
`ap.NewA2ADataPart(data)`、`ap.NewA2AMessageBridge()`。

## 认证

```go
// 方式一：API Key 认证（keys: clientID → key；headerName: 承载头）
authAPIKey := ap.NewA2AAPIKeyAuthenticator(map[string]string{
    "client-a": "key-aaa",
}, "X-API-Key")

// 方式二：Bearer Token 认证（validate 为 A2ABearerTokenValidator 函数）
authBearer := ap.NewA2ABearerTokenAuthenticator(func(token string) (*ap.A2APrincipal, error) {
    if token == "valid-token" {
        return &ap.A2APrincipal{ID: "user-1"}, nil
    }
    return nil, fmt.Errorf("invalid token")
})

// 方式三：无认证（开发环境）
authNoop := ap.NewA2ANoopAuthenticator()

_, _, _ = authAPIKey, authBearer, authNoop
```

gRPC 服务端/客户端侧另有 `WithGRPCAuth(auth A2AGRPCAuthFunc)` 与
`WithA2AGRPCClientBearerToken(token)` 等选项。

## 与编排的区别

| 维度 | A2A | Orchestration |
|------|-----|---------------|
| 边界 | 跨进程 / 跨服务 | 进程内 |
| 发现 | Agent Card | 代码显式组合 |
| 通信 | gRPC（ap 内）/ HTTP+SSE（跨生态） | 函数调用 |
| 适用 | 异构 Agent 系统 | 同构模块协作 |
