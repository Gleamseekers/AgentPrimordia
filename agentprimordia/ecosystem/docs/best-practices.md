# 最佳实践指南

本文档总结了在生产环境中使用 AgentPrimordia 的最佳实践和经验教训。

## 目录

- [架构设计](#架构设计)
- [性能优化](#性能优化)
- [安全最佳实践](#安全最佳实践)
- [错误处理与容错](#错误处理与容错)
- [测试策略](#测试策略)
- [部署建议](#部署建议)
- [常见反模式](#常见反模式)

---

## 架构设计

### 1. 单一职责原则

每个 Agent 应该专注于一个明确的任务：

```go
import ap "github.com/Gleamseekers/AgentPrimordia/agentprimordia/pkg"

// 好的：职责清晰
codeAgent := ap.NewAgent("CodeAssistant", "你是一个代码助手，只负责编写和调试代码",
    provider,
    ap.WithMaxTurns(20),
).WithToolkit(codeTools)  // 只注册代码相关工具

reviewAgent := ap.NewAgent("CodeReviewer", "你是一个代码审查专家，负责审查代码质量",
    provider,
    ap.WithMaxTurns(10),
)

// 差的：职责混乱
superAgent := ap.NewAgent("SuperBot", "你能做所有事情：写代码、审代码、写文档、发邮件...",
    provider,
    ap.WithMaxTurns(50),
).WithToolkit(allTools)  // 注册了所有工具
```

### 2. 合理使用 Memory

根据场景选择合适的 Memory 后端：

```go
// 生产环境：SQLite + 持久化
memoryStore, _ := ap.NewSQLiteStore("/data/production_memory.db")
defer memoryStore.Close()

// 测试/演示环境：InMemory（快速启动）
memoryStore, _ := ap.WithInMemory()
defer memoryStore.Close()
```

**Memory 使用建议**：
- 定期清理过期记忆（`CleanupExpired`）
- 设置合理的重要性阈值
- 使用摘要功能减少存储空间
- 考虑使用 RAG 增强检索能力

### 3. LLM Provider 选择策略

```go
import ap "github.com/Gleamseekers/AgentPrimordia/agentprimordia/pkg"

// 方案1: 主备切换（推荐生产环境）
primary := ap.NewOpenAIProvider(ap.Config{Model: "gpt-4o"})
fallback := ap.NewQwenProvider(ap.Config{Model: "qwen-plus"})

resilient := ap.NewResilientProvider(primary, ap.DefaultResilientConfig())
resilient.AddFallback(fallback)

// 方案2: 成本优化（简单任务用便宜模型）
simpleAgent := ap.NewAgent("SimpleBot", "简单任务助手",
    ap.NewQwenProvider(ap.Config{Model: "qwen-turbo"}),  // 便宜快速
    ap.WithMaxTurns(5),
)

complexAgent := ap.NewAgent("ComplexBot", "复杂任务助手",
    ap.NewOpenAIProvider(ap.Config{Model: "gpt-4o"}),  // 强但贵
    ap.WithMaxTurns(30),
)

// 方案3: 多模态任务
visionAgent := ap.NewAgent("VisionBot", "视觉分析助手",
    ap.NewMultimodalProvider(ap.NewGeminiProvider(ap.Config{Model: "gemini-2.0-flash"})),
    ap.WithMaxTurns(10),
)
```

### 4. 工具权限控制

**永远不要在生产环境中禁用安全检查！**

```go
// 好：严格的权限控制
policy := ap.NewFileScopePolicy()
policy.SetScope("agent-1", []string{"/workspace/src/", "/workspace/docs/"})

registry, _ := ap.DefaultToolkit(ap.ToolkitConfig{
    RootDir:     "/workspace",
    EnableFS:    true,
    EnableShell: true,
    EnableWeb:   false,
    ScopePolicy: policy,
    ScopeAgent:  "agent-1",
})

// 差：完全开放（危险！）
registry, _ := ap.DefaultToolkit(ap.ToolkitConfig{
    RootDir:     "/",          // 根目录！
    EnableFS:    true,
    EnableShell: true,
    EnableWeb:   true,
})
```

---

## 性能优化

### 1. 流式输出

对于需要实时反馈的场景，使用流式输出：

```go
// 同步方式：用户等待时间长
resp, _ := agent.Run(ctx, ap.UserMessage("你好"))
fmt.Println(resp.Content)  // 一次性输出所有内容

// 流式方式：用户体验更好
ch, _ := agent.StreamRun(ctx, ap.UserMessage("你好"))
for event := range ch {
    switch event.Type {
    case ap.StreamEventToken:
        fmt.Print(event.Content)
    case ap.StreamEventComplete:
        fmt.Println("\n完成")
    }
}
```

### 2. Context 超时设置

始终设置合理的超时：

```go
// 好：带超时控制
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

resp, err := agent.Run(ctx, ap.UserMessage("你好"))

// 差：无超时，可能无限等待
resp, err := agent.Run(context.Background(), ap.UserMessage("你好"))
```

### 3. 缓存策略

对于重复查询，使用内置缓存减少 LLM 调用：

```go
// 使用 LLM 缓存（Experimental）
cache := ap.NewFingerprintCache(10000, time.Hour) // 指纹精确匹配缓存（容量, TTL）
cachedProvider, err := ap.NewCachedProvider(provider, cache, 0.85) // 三参：provider, cache, minScore
if err != nil { log.Fatal(err) }

agent, err := ap.NewAgent("CachedBot", "你是一个助手",
    cachedProvider,
    ap.WithMaxTurns(10),
)
if err != nil { log.Fatal(err) }
```

### 4. 批量操作

```go
// 使用 ImportMemories 批量导入记忆
data, _ := json.Marshal(episodes)
count, _ := memoryStore.ImportMemories(ctx, data, "json")
```

---

## 安全最佳实践

### 1. API Key 管理

```go
// 好：从环境变量读取
apiKey := os.Getenv("OPENAI_API_KEY")
if apiKey == "" {
    log.Fatal("OPENAI_API_KEY environment variable is required")
}

provider := ap.NewOpenAIProvider(ap.Config{
    APIKey: apiKey,
})

// 或使用 ConfigFromEnv 自动读取
provider := ap.NewOpenAIProvider(ap.ConfigFromEnv())

// 差：硬编码在代码中
provider := ap.NewOpenAIProvider(ap.Config{
    APIKey: "sk-proj-abc123...",  // 绝对不要这样做！
})
```

### 2. 输入验证

```go
func safeHandler(w http.ResponseWriter, r *http.Request) {
    var req struct {
        Message string `json:"message"`
    }

    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid request body", http.StatusBadRequest)
        return
    }

    // 验证消息长度
    if len(req.Message) > 10000 {  // 限制 10KB
        http.Error(w, "Message too long", http.StatusBadRequest)
        return
    }

    resp, err := myAgent.Run(r.Context(), ap.UserMessage(req.Message))
    // ...
}
```

### 3. 资源限制

```go
// 限制并发 Agent 数量
pool := ap.NewPool(ap.PoolConfig{
    MaxConcurrency: 10,
    Timeout:        60 * time.Second,
})

// 限制工具执行资源（通过 ToolkitConfig）
registry, _ := ap.DefaultToolkit(ap.ToolkitConfig{
    RootDir:     "/workspace",
    EnableFS:    true,
    EnableShell: true,
    EnableWeb:   true,
})
```

---

## 错误处理与容错

### 1. 分层错误处理

```go
import ap "github.com/Gleamseekers/AgentPrimordia/agentprimordia/pkg"

resp, err := agent.Run(ctx, ap.UserMessage("你好"))

switch {
case ap.IsErrorCode(err, ap.ErrMaxTurnsExceeded):
    // 可恢复错误：提示用户简化问题
    respondWithError(w, "问题太复杂，请简化后重试", http.StatusBadRequest)

case ap.IsErrorCode(err, ap.ErrTimeout):
    // 可恢复错误：建议稍后重试
    respondWithError(w, "处理超时，请稍后重试", http.StatusRequestTimeout)

case ap.IsErrorCode(err, ap.ErrLLMCallFailed):
    // 外部服务故障：返回友好提示
    log.Printf("LLM 服务异常: %v", err)
    respondWithError(w, "AI服务暂时不可用", http.StatusServiceUnavailable)

case err != nil:
    // 未预期错误
    log.Printf("未预期错误: %v", err)
    respondWithError(w, "内部服务器错误", http.StatusInternalServerError)

default:
    respondWithSuccess(w, resp.Content)
}
```

### 2. 重试策略

```go
// 使用 ResilientProvider 自动重试
resilient, _ := ap.NewResilientProvider(primary, ap.ResilientConfig{
    MaxRetries:       3,
    RetryBackoff:     1 * time.Second,
    MaxBackoff:       10 * time.Second,
})

// 添加降级 Provider
resilient.AddFallback(backupProvider)
```

### 3. 熔断器模式

ResilientProvider 内置熔断器：

```go
config := ap.ResilientConfig{
    MaxRetries:       3,
    RetryBackoff:     500 * time.Millisecond,
    MaxBackoff:       10 * time.Second,
}
resilient, _ := ap.NewResilientProvider(primary, config)
resilient.AddFallback(fallback)
```

---

## 测试策略

### 1. 单元测试

使用 MockLLM 进行单元测试：

```go
import (
    "testing"
    ap "github.com/Gleamseekers/AgentPrimordia/agentprimordia/pkg"
    "github.com/Gleamseekers/AgentPrimordia/agentprimordia/testutil"
)

func TestAgentBasicFlow(t *testing.T) {
    mockLLM := testutil.NewMockProvider("你好！有什么我可以帮助你的？")

    agent := ap.NewAgent("TestAgent", "你是一个测试助手",
        mockLLM,
        ap.WithMaxTurns(5),
    )

    resp, err := agent.Run(context.Background(), ap.UserMessage("你好"))
    if err != nil {
        t.Fatalf("Run() error = %v", err)
    }

    if resp.Content == "" {
        t.Error("expected non-empty response")
    }
}
```

### 2. Memory 测试

使用 WithInMemory 进行快速测试：

```go
func TestMemoryOperations(t *testing.T) {
    store, err := ap.WithInMemory()
    if err != nil {
        t.Fatal(err)
    }
    defer store.Close()

    episode := &ap.Episode{
        ID:        "test-1",
        SessionID: "session-1",
        Role:      "user",
        Content:   "测试内容",
    }

    err = store.Add(context.Background(), episode)
    if err != nil {
        t.Fatalf("Add() error = %v", err)
    }

    retrieved, err := store.Get(context.Background(), "test-1")
    if err != nil {
        t.Fatalf("Get() error = %v", err)
    }

    if retrieved.Content != "测试内容" {
        t.Errorf("Content mismatch: got %s, want %s", retrieved.Content, "测试内容")
    }
}
```

### 3. 工具测试

使用 httptest.Server 测试 Web 工具：

```go
func TestWebTool_MockServer(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
        w.Write([]byte(`{"status": "ok"}`))
    }))
    defer server.Close()

    webTool := ap.NewWeb()

    args, _ := json.Marshal(map[string]string{
        "action": "fetch",
        "url":    server.URL + "/api/test",
    })

    result, err := webTool.Execute(context.Background(), args)
    if err != nil {
        t.Fatalf("Execute() error = %v", err)
    }

    if !strings.Contains(result.Content, `"status": "ok"`) {
        t.Errorf("unexpected response: %s", result.Content)
    }
}
```

---

## 部署建议

### 1. Docker 化部署

```dockerfile
# Dockerfile
FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o agentprimordia ./cmd/ap

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/agentprimordia .
EXPOSE 8080
CMD ["./agentprimordia"]
```

```yaml
# docker-compose.yml
version: '3.8'
services:
  app:
    build: .
    ports:
      - "8080:8080"
    environment:
      - OPENAI_API_KEY=${OPENAI_API_KEY}
      - GEMINI_API_KEY=${GEMINI_API_KEY}
    volumes:
      - ./data:/data  # 持久化 Memory 存储
    restart: unless-stopped
```

### 2. K8s 部署

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
    tools:
      - name: filesystem
      - name: shell
    memory:
      backend: sqlite
  autoscaling:
    minReplicas: 1
    maxReplicas: 10
    targetConcurrentTasks: 5
```

### 3. 监控与告警

```go
// 启动 Prometheus Exporter
handler := ap.NewPrometheusHandler()
// 挂载到 HTTP 路由

// 使用 Metrics 适配器
m := ap.NewMetrics()
agent := ap.NewAgent("MonitoredBot", "你是一个助手",
    provider,
    ap.WithMaxTurns(10),
).WithMetrics(m)
```

### 4. 日志规范

```go
import "log/slog"

// 结构化日志
logger := slog.Default()

logger.Info("Agent started",
    "name", "CodeAssistant",
    "model", "gpt-4o",
    "session_id", "sess-123",
)

// 生产环境建议使用 JSON 格式
handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelInfo,
})
logger = slog.New(handler)
```

---

## 常见反模式

### 反模式 1: 无限循环

```go
// 危险：没有最大轮数限制
badAgent := ap.NewAgent("LoopBot", "你是一个助手",
    provider,
    // 没有设置 WithMaxTurns！默认 50 但最好显式指定
)

// 正确做法
goodAgent := ap.NewAgent("SafeBot", "你是一个助手",
    provider,
    ap.WithMaxTurns(10),  // 根据任务复杂度设置合理值
)
```

### 反模式 2: 忽略错误

```go
// 危险：忽略所有错误
_, _ = agent.Run(ctx, msg)  // 不要这样做！

// 正确做法
resp, err := agent.Run(ctx, ap.UserMessage(msg))
if err != nil {
    if ap.IsErrorCode(err, ap.ErrMaxTurnsExceeded) {
        // 提示用户简化问题
    } else {
        log.Printf("Agent failed: %v", err)
    }
    return
}
```

### 反模式 3: 共享可变状态

```go
// 危险：多个 goroutine 共享同一个 Agent 实例
var sharedAgent ap.Agent

func handler(w http.ResponseWriter, r *http.Request) {
    go func() {
        sharedAgent.Run(r.Context(), msg)  // 数据竞争！
    }()
}

// 正确做法：
// 方案1: 使用 Pool 调度器
pool := ap.NewPool(ap.PoolConfig{MaxConcurrency: 10})
pool.Dispatch(ctx, tasks)

// 方案2: 每个请求创建新 Agent（无状态）
func handler(w http.ResponseWriter, r *http.Request) {
    localAgent := ap.NewAgent("handler", "助手", provider, ap.WithMaxTurns(10))
    resp, _ := localAgent.Run(r.Context(), ap.UserMessage(msg))
}
```

### 反模式 4: 超大 Prompt

```go
// 危险：发送整个文件库作为 context
files := readAllFiles("/project")  // 可能几十 MB
prompt := fmt.Sprintf("这些是我的代码文件:\n%s\n请审查...", files)
agent.Run(ctx, ap.UserMessage(prompt))  // Token 爆炸！

// 正确做法：
// 1. 只发送相关文件
// 2. 使用 RAG/向量检索
agent := ap.NewAgent("Reviewer", "你是代码审查助手",
    provider,
    ap.WithMaxTurns(10),
).WithRAG(ap.RAGConfig{
    Provider: ragProvider,
    Mode:     ap.RAGModeAuto,
    TopK:     5,
})
```

### 反模式 5: 同步阻塞 HTTP 处理

```go
// 危险：阻塞 HTTP handler
func handler(w http.ResponseWriter, r *http.Request) {
    resp, err := agent.Run(r.Context(), ap.UserMessage(msg))  // 可能阻塞 30+ 秒
    json.NewEncoder(w).Encode(resp)
}

// 正确做法：
// 方案1: 设置超时
ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
defer cancel()
resp, err := agent.Run(ctx, ap.UserMessage(msg))

// 方案2: 异步处理 + 轮询
results, _ := pool.Dispatch(ctx, tasks)
```

---

## 总结

遵循这些最佳实践，你可以构建出：
- **高性能**: 低延迟、高吞吐、资源高效利用
- **高可靠**: 自动容错、优雅降级、快速恢复
- **高安全**: 权限控制、输入验证、数据保护
- **易维护**: 清晰架构、完善测试、良好文档
- **易扩展**: 模块化设计、接口抽象、插件机制

核心原则：
1. **简单优于复杂** - 从最小可行方案开始迭代
2. **安全第一** - 永远不要关闭安全检查
3. **可观测性** - 记录一切，监控关键指标
4. **渐进增强** - 先保证基本功能，再优化性能
