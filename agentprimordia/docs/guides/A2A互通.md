# A2A 开放协议互操作指南（v3.5）

本文档说明如何让第三方 Agent 调用 ap Agent，以及 ap Agent 如何调用第三方 Agent。

## 背景

ap 内部 A2A 传输为 **gRPC**（自 v4.0-3 起为 Stable 默认传输；早期 JSON-RPC 传输已在
v4.0-1 移除，见 [`docs/版本规范.md`](../版本规范.md)）。为与外部生态互通，框架另提供一套
**开放协议互操作面**（`OpenInteropServer` / `OpenInteropClient`）：按开放 Agent2Agent 规范
以 JSON-RPC 2.0 over HTTP（端点 `/a2a/v1`）+ SSE 暴露与消费任务，用于跨生态对接；ap 集群
内部仍走 gRPC。二者用途不同，不要混用：

| 面 | 入口 | 用途 |
|----|------|------|
| 开放协议互操作 | `ap.NewOpenInteropServer` / `ap.NewOpenInteropClient` | 跨生态（第三方 Agent 遵循开放规范） |
| ap A2A gRPC | `ap.NewA2AGRPCServer` / `ap.NewA2AGRPCClient` | ap 集群内网高性能传输 |

## 暴露 Agent Card

ap Agent 默认在 `/.well-known/agent.json` 暴露开放规范 Agent Card：

```bash
curl http://localhost:8080/.well-known/agent.json
```

Card 声明 `name` / `url` / `capabilities` / `skills` / `defaultInputModes` / `defaultOutputModes`。

## 第三方调用 ap Agent

开放协议基于 JSON-RPC 2.0，端点 `/a2a/v1`：

```bash
# 发送任务
curl -X POST http://localhost:8080/a2a/v1 \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc":"2.0","method":"tasks/send","id":1,
    "params":{"message":{"role":"user","parts":[{"type":"text","text":"hello"}]}}
  }'

# 查询任务
curl -X POST http://localhost:8080/a2a/v1 \
  -d '{"jsonrpc":"2.0","method":"tasks/get","id":2,"params":{"taskId":"task-xxx"}}'

# 取消任务
curl -X POST http://localhost:8080/a2a/v1 \
  -d '{"jsonrpc":"2.0","method":"tasks/cancel","id":3,"params":{"taskId":"task-xxx"}}'
```

## ap 调用第三方 Agent

使用 `OpenInteropClient`：

```go
client := ap.NewOpenInteropClient("https://third-party.example.com")
ctx := context.Background()

card, err := client.FetchAgentCard(ctx) // 发现
if err != nil {
    log.Fatalf("发现失败: %v", err)
}

task, err := client.SendTask(ctx, ap.NewTextMessage("user", "do X")) // 委托
if err != nil {
    log.Fatalf("委托失败: %v", err)
}

// 轮询：GetTask 失败时返回 (nil, err)，禁止用 _ 丢弃错误后解构 task——
// 那会在对端异常时直接 nil panic。
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

if err := client.CancelTask(ctx, task.ID); err != nil { // 取消
    log.Fatalf("取消失败: %v", err)
}
```

> 纪律：本指南所有片段都不使用 `_` 丢弃错误后解构返回值。`SendTask` / `GetTask` /
> `CancelTask` / `FetchAgentCard` 失败时均返回 `(nil, err)`，必须先判 err。

## 流式事件（SSE）

服务端通过 `OpenSSEWriter` 推送标准事件：

- `message.delta` — 流式 token 增量
- `task.status_update` — 任务状态变更
- `task.artifact_update` — 产出物更新

## 错误码

| 码 | 含义 |
|----|------|
| -32700 | Parse error |
| -32600 | Invalid Request |
| -32601 | Method not found |
| -32602 | Invalid params |
| -32603 | Internal error |
| -32001 | Task not found |
| -32002 | Task already canceled |

## 兼容性检查

```bash
ap a2a interop-check
```

或在代码中：

```go
report := ap.GenerateInteropReport(card, cfg)
fmt.Println(report.Score, report.FailedChecks())
```

## 互操作模式

- `compatible`（默认）：开放协议 + 私有扩展
- `strict`：仅开放协议

## 跨组件集成

开放协议请求可接入现有认证（`auth.go`）、发现（`cluster/`）、追踪（`trace_propagation.go`）、限流（`grpc_circuit_breaker.go`），Agent Card 的 `skills` 字段对接 v3.4 技能库。
