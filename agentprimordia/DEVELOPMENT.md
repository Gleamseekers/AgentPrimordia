# AgentPrimordia 开发文档

本文档面向希望深入理解、扩展和贡献 AgentPrimordia 框架的开发者。

---

## 目录

- [架构概览](#架构概览)
- [模块依赖关系](#模块依赖关系)
- [核心模块详解](#核心模块详解)
  - [Agent（ReAct Loop 引擎）](#agentreact-loop-引擎)
  - [LLM（大语言模型抽象层）](#llm大语言模型抽象层)
  - [Memory（记忆系统）](#memory记忆系统)
  - [Tools（工具系统）](#tools工具系统)
  - [Pool（多 Agent 并发调度）](#pool多-agent-并发调度)
  - [Security（安全沙箱）](#security安全沙箱)
  - [Events（事件总线）](#events事件总线)
  - [Metrics（指标采集）](#metrics指标采集)
  - [Persist（检查点持久化）](#persist检查点持久化)
  - [Concurrency（并发控制）](#concurrency并发控制)
  - [K8s Operator（Kubernetes 运维）](#k8s-operatorkubernetes-运维)
- [公共 API 层（pkg）](#公共-api-层pkg)
- [自定义扩展指南](#自定义扩展指南)
  - [实现自定义 LLM Provider](#实现自定义-llm-provider)
  - [实现自定义 Tool](#实现自定义-tool)
  - [实现自定义 Memory 后端](#实现自定义-memory-后端)
  - [实现自定义 CheckpointStore](#实现自定义-checkpointstore)
  - [实现自定义 ContextWindowStrategy](#实现自定义-contextwindowstrategy)
- [Hook 系统](#hook-系统)
- [RAG 知识库集成](#rag-知识库集成)
- [流式输出](#流式输出)
- [Agent 生命周期管理](#agent-生命周期管理)
- [错误处理与错误码](#错误处理与错误码)
- [并发安全设计](#并发安全设计)
- [测试指南](#测试指南)
- [构建与部署](#构建与部署)

---

## 架构概览

AgentPrimordia 采用分层架构，核心设计原则：

1. **接口驱动** — 所有子系统通过 Go interface 解耦，可独立替换
2. **组合优于继承** — Agent 能力通过配置组合（Memory、Hooks、RAG 等），而非继承
3. **弹性优先** — ResilientProvider 内建重试、降级、熔断，生产级可靠性
4. **核心路径零第三方依赖** — 核心路径仅依赖纯 Go SQLite 驱动（modernc.org/sqlite）；受限第三方依赖（gRPC/etcd/redis/wazero 等，均有 build tag 或模块边界）见 [AGENTS.md](../AGENTS.md) §2.1

### 数据流

```
用户输入 (Message)
    │
    ▼
┌─────────────────────────────────────────┐
│          ReAct Loop (Run/StreamRun)     │
│                                         │
│  ┌─────┐   ┌──────┐   ┌──────────────┐ │
│  │ RAG │──▶│  LLM │──▶│ Tool Execute │ │
│  └─────┘   └──────┘   └──────────────┘ │
│      ▲         │              │         │
│      │         ▼              ▼         │
│      │    ┌──────────┐  ┌──────────┐   │
│      │    │ Thought  │  │  Result  │   │
│      │    └──────────┘  └──────────┘   │
│      │         │              │         │
│      └─────────┴──────────────┘         │
│              (历史消息循环)               │
└─────────────────────────────────────────┘
    │
    ▼
最终响应 (Response)
```

---

## 模块依赖关系

```
pkg (公共 API 重导出)
 │
 ├── pool (多 Agent 调度)
 │     ├── agent (ReAct Loop 引擎)
 │     │     ├── llm (Provider 接口 + 实现)
 │     │     ├── tools (工具注册 + 执行)
 │     │     │     └── builtin (FileSystem/Shell/Web)
 │     │     ├── memory (记忆接口 + SQLite)
 │     │     ├── persist (检查点接口)
 │     │     └── events (事件总线)
 │     │
 │     └── tools
 │
 ├── operator (K8s Operator: CRD + Controller)
 │     ├── agentdeployment (CRD 定义)
 │     └── controller (调谐循环)
 │
 ├── security (ACL + Sandbox)
 └── concurrency (FileLock)
```

**依赖规则**：上层可依赖下层，下层不可反向依赖上层。`internal/` 下的包不对外暴露，所有公开类型通过 `pkg/` 重导出。

---

## 核心模块详解

### Agent（ReAct Loop 引擎）

**包路径**：`internal/agent`

ReAct Loop 是框架的核心，实现 **Reason → Act → Observe** 循环。

#### 关键类型

```go
// Agent 配置
type ReActConfig struct {
    Name            string                  // Agent 名称
    SystemPrompt    string                  // 系统提示词
    Model           llm.Provider            // LLM Provider（必需）
    Toolkit         *tools.Registry         // 工具注册表
    Memory          MemoryStore             // 记忆存储
    EventPublisher  EventPublisher          // 事件发布器
    Metrics         MetricsRecorder         // 指标记录器
    ContextWindow   ContextWindowStrategy   // 上下文裁剪策略
    CheckpointStore persist.CheckpointStore // 状态持久化
    SessionID       string                  // 会话标识
    RAG             *RAGConfig              // RAG 知识库配置
    MaxTurns        int                     // 最大推理轮数（默认 50）
    Temperature     float64                // LLM 温度
    Hooks           Hooks                   // 生命周期钩子
    Lifecycle       *Lifecycle              // 生命周期管理器
    Logger          *slog.Logger            // 结构化日志
}

// Agent 实例
type ReActAgent struct { ... }
```

#### 核心方法

| 方法 | 签名 | 说明 |
|------|------|------|
| `NewAgent` | `(name, system, model, opts ...) (*CapabilityAgent, error)` | 创建 Agent（推荐） |
| `Run` | `(ctx, msg) (*Response, error)` | 同步运行，阻塞直到完成 |
| `StreamRun` | `(ctx, msg) (<-chan StreamEvent, error)` | 流式运行，通过 channel 逐步返回事件 |
| `ResumeFromCheckpoint` | `(ctx) (*Response, error)` | 从检查点恢复执行 |
| `Stats` | `() AgentStats` | 获取运行时统计 |
| `Stop` | `()` | 优雅停止 |
| `Pause` | `()` | 暂停 |
| `Resume` | `()` | 恢复暂停 |

#### ReAct Loop 执行流程

```
1. 初始化历史消息 [system_prompt, user_message]
2. 上下文裁剪 (trimContext)
3. RAG 上下文注入 (如果配置了 RAG)
4. 调用 LLM (Complete/CallTools)
5. 解析 Thought (Content + ToolCalls)
6. 如果有 ToolCalls → 执行工具 → 结果加入历史 → 回到步骤 2
7. 如果无 ToolCalls → 返回最终 Response
8. 循环直到 MaxTurns 或无工具调用
```

#### Agent 状态

```go
type AgentStatus string

const (
    StatusIdle      AgentStatus = "idle"       // 空闲
    StatusRunning   AgentStatus = "running"    // 运行中
    StatusPaused    AgentStatus = "paused"     // 已暂停
    StatusCompleted AgentStatus = "completed"  // 已完成
    StatusFailed    AgentStatus = "failed"     // 失败
    StatusCancelled AgentStatus = "cancelled"  // 已取消
)
```

---

### LLM（大语言模型抽象层）

**包路径**：`internal/llm`

#### Provider 接口

```go
type Provider interface {
    Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)
    Stream(ctx context.Context, req *CompletionRequest) (<-chan Chunk, error)
    CallTools(ctx context.Context, req *ToolCallRequest) (*ToolCallResponse, error)
    Embeddings(ctx context.Context, texts []string) ([][]float32, error)
    Info() ModelInfo
}
```

#### 内置 Provider

| Provider | 文件 | 说明 |
|----------|------|------|
| `OpenAIProvider` | `openai_provider.go` | 兼容 OpenAI API 格式，支持 DeepSeek 等兼容 API |
| `AnthropicProvider` | `anthropic_provider.go` | Anthropic Claude API |
| `GeminiProvider` | `gemini_provider.go` | Google Gemini API |
| `OllamaProvider` | `ollama_provider.go` | Ollama 本地模型 |
| `AzureProvider` | `azure_provider.go` | Azure OpenAI |
| `QwenProvider` | `qwen_provider.go` | 通义千问 (OpenAI 兼容) |
| `GLMProvider` | `glm_provider.go` | 智谱 GLM (OpenAI 兼容) |
| `MistralProvider` | `mistral_provider.go` | Mistral AI (OpenAI 兼容) |
| `CohereProvider` | `cohere_provider.go` | Cohere v2 API |
| `DeepSeekProvider` | `deepseek_provider.go` | DeepSeek (OpenAI 兼容) |
| `ResilientProvider` | `resilient.go` | 弹性包装器：重试 + 降级 + 熔断 |
| `MockLLM` | `mock_llm.go` | 测试用模拟 Provider |

#### ResilientProvider 详解

```go
type ResilientConfig struct {
    MaxRetries          int           // 最大重试次数（默认 3）
    RetryBackoff        time.Duration // 重试退避初始值（默认 500ms）
    MaxBackoff          time.Duration // 最大退避时间（默认 10s）
    CircuitThreshold    int           // 熔断阈值（默认 5 次失败）
    CircuitRecoverAfter time.Duration // 熔断恢复时间（默认 30s）
}
```

**熔断状态机**：
- `Closed` → 正常请求，失败计数累加
- `Open` → 直接拒绝请求（返回 `ErrCircuitOpen`），等待 `CircuitRecoverAfter` 后进入半开
- `HalfOpen` → 允许一次请求，成功则回到 Closed，失败则回到 Open

**降级链**：主 Provider 重试耗尽后，依次尝试 `AddFallback` 注册的降级 Provider。

#### 配置加载

```go
// 从环境变量加载，前缀默认 AP_LLM
cfg := llm.ConfigFromEnv("")  
// 读取: AP_LLM_API_KEY, AP_LLM_BASE_URL, AP_LLM_MODEL, AP_LLM_TEMPERATURE, AP_LLM_MAX_TOKENS
// 额外: AP_LLM_EXTRA_* → cfg.Extra[key]

// 验证
if err := cfg.Validate(); err != nil { ... }
```

---

### Memory（记忆系统）

**包路径**：`internal/memory`

#### Memory 接口

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
    Close() error
}
```

#### Episode（记忆片段）

```go
type Episode struct {
    ID         string            `json:"id"`
    SessionID  string            `json:"session_id"`
    Role       string            `json:"role"`
    Content    string            `json:"content"`
    Summary    string            `json:"summary,omitempty"`
    Topics     string            `json:"topics,omitempty"`
    Importance float64           `json:"importance,omitempty"` // 0.0 ~ 1.0
    Metadata   map[string]string `json:"metadata,omitempty"`
    CreatedAt  string            `json:"created_at"`
}
```

#### SQLite 实现

基于 FTS5 全文搜索，支持：
- 全文搜索（`Search`）：使用 SQLite FTS5 MATCH 查询
- 标签搜索（`SearchByTag`）：LIKE 匹配 Topics 字段，已防 SQL 注入
- 重要性过滤（`GetImportant`）：Importance >= threshold
- 时间线视图（`GetTimeline`）：按天分组
- 自动清理（`CleanupExpired`）：删除超龄记录

```go
// 持久化存储
store, err := memory.NewSQLiteStore("./data/memory.db")

// 内存存储（测试用）
store, err := memory.WithInMemory()
```

#### 向量存储

```go
type VectorStore struct { ... }

vs := memory.NewVectorStore(1536)  // 嵌入维度
vs.Add(ctx, "doc-1", embedding, metadata)
results, _ := vs.Search(ctx, queryEmbedding, 5)  // 余弦相似度搜索
```

#### RAG（检索增强生成）

```go
type RAGStore struct {
    memory    Memory
    vectors   *VectorStore
    embedder  EmbeddingProvider
}

// 创建
rag := memory.NewRAGStore(memStore, embedder)

// 查询
results, _ := rag.Query(ctx, "用户问题", 5)

// 混合检索：FTS + 向量相似度，加权融合
results, _ := rag.HybridSearch(ctx, "用户问题", 5)
```

**混合检索融合策略**：
- FTS 结果权重 0.4，向量结果权重 0.6
- 同时被 FTS 和向量命中的结果加权融合：`score = fts*0.4 + vec*0.6`
- 按最终分数降序排列

---

### Tools（工具系统）

**包路径**：`internal/tools`

#### Tool 接口

```go
type Tool interface {
    Name() string
    Description() string
    Parameters() json.RawMessage
    Execute(ctx context.Context, args json.RawMessage) (*Result, error)
}
```

#### Registry（工具注册表）

```go
reg := tools.NewRegistry()
reg.Register(myTool)          // 注册工具（幂等，同名覆盖）
reg.Get("tool-name")          // 获取工具
reg.List()                    // 列出所有工具名
reg.Count()                   // 已注册工具数
reg.RegisterMultiple(t1, t2)  // 批量注册
```

#### Executor（工具执行器）

```go
exec := tools.NewToolExecutor(registry, scopePolicy)
result, err := exec.Execute(ctx, "tool-name", argsJSON, agentID)
```

#### 内置工具

| 工具 | 文件 | 功能 |
|------|------|------|
| `FileSystem` | `builtin/filesystem.go` | 文件/目录读写、搜索（默认字符串匹配，可选 regex 模式） |
| `Shell` | `builtin/shell.go` | Shell 命令执行 |
| `Web` | `builtin/web.go` | HTTP 请求（GET/POST） |
| `API` | `builtin/api.go` | API 调用工具（`NewAPI()`） |
| `Database` | `builtin/database.go` | 数据库查询工具（`NewDatabase(dbPath, opts...)`） |
| `CodeExecution` | `builtin/code_execution.go` | 代码执行工具（`NewCodeExecution()`） |
| `Knowledge` | `builtin/knowledge.go` | RAG on_demand 模式的知识检索工具 |

#### 权限与确认

```go
type Permission struct {
    AllowedRoles        []string
    BlockedPaths        []string
    RequireConfirmation bool
    ConfirmFunc         ConfirmationFunc  // func(toolName string, args json.RawMessage) bool
}
```

#### FileScopePolicy（文件作用域策略）

```go
policy := tools.NewFileScopePolicy()
policy.Allow("/workspace/data")
policy.Deny("/workspace/.env")
policy.IsAllowed("/workspace/data/file.txt")  // true
policy.IsAllowed("/workspace/.env")           // false
```

---

### Pool（多 Agent 并发调度）

**包路径**：`internal/pool`

```go
type PoolConfig struct {
    MaxConcurrency int                    // 最大并发度（默认 10）
    Timeout        time.Duration          // 任务超时
    DefaultAgent   ReActAgentConfig       // 默认 Agent 配置
}

type TaskConfig struct {
    ID      string
    Title   string
    Prompt  string
    Agent   *ReActAgentConfig  // 可选：覆盖默认 Agent 配置
}

type TaskResult struct {
    TaskID   string
    Task     TaskConfig
    Response *agent.Response
    Error    error
    Duration time.Duration
    Status   PoolTaskStatus
}
```

#### 使用方式

```go
pool := pool.NewPool(pool.PoolConfig{
    MaxConcurrency: 5,
    Timeout:        30 * time.Second,
})
defer pool.Close()

pool.SetModel(provider)
pool.SetToolkit(registry)

results, err := pool.Dispatch(ctx, []pool.TaskConfig{...})
stats := pool.Stats()
```

#### 事件通道

```go
eventCh := pool.EventChannel()
for event := range eventCh {
    // 处理 PoolEvent
}
```

---

### Security（安全沙箱）

**包路径**：`internal/security`

#### ACL（访问控制列表）

```go
acl := security.NewACL()
acl.Allow("agent-1", "/workspace/data", security.AccessAll)
acl.Deny("agent-1", "/workspace/.env")
acl.Check("agent-1", "/workspace/data", security.AccessRead)  // true
```

支持通配符：`acl.Allow("*", "/public", security.AccessRead)`

#### Sandbox（沙箱）

```go
sandbox := security.NewSandbox(acl)
sandbox.AllowCommand("ls")
sandbox.BlockCommand("rm")

sandbox.CanExecute("agent-1", "ls")   // nil (允许)
sandbox.CanExecute("agent-1", "rm")   // ErrCommandBlocked
sandbox.ValidatePath("agent-1", "/etc/../../../passwd", security.AccessRead)  // ErrPathTraversal
```

#### 访问级别

```go
const (
    AccessNone    AccessLevel = 0
    AccessRead    AccessLevel = 1
    AccessWrite   AccessLevel = 2
    AccessExecute AccessLevel = 4
    AccessAll     AccessLevel = 7  // Read | Write | Execute
)
```

---

### Events（事件总线）

**包路径**：`internal/events`

```go
bus := events.NewBus(100)  // 缓冲区大小

// 订阅特定事件类型
ch, subID := bus.Subscribe(events.EventToolCall)

// 订阅所有事件
ch, subID := bus.SubscribeAll()

// 发布事件
bus.Publish(events.Event{
    Type:    events.EventToolCall,
    Source:  "agent-1",
    Payload: map[string]interface{}{"tool": "filesystem"},
})

// 取消订阅
bus.Unsubscribe(subID)

// 关闭
bus.Close()
```

#### 事件类型

| 常量 | 值 | 说明 |
|------|-----|------|
| `EventAgentStart` | `"agent.start"` | Agent 启动 |
| `EventAgentStop` | `"agent.stop"` | Agent 停止 |
| `EventAgentPanic` | `"agent.panic"` | Agent panic 恢复 |
| `EventAgentError` | `"agent.error"` | Agent 错误 |
| `EventAgentResume` | `"agent.resume"` | Agent 从检查点恢复 |
| `EventTurnStart` | `"turn.start"` | 轮次开始 |
| `EventTurnEnd` | `"turn.end"` | 轮次结束 |
| `EventToolCall` | `"tool.call"` | 工具调用 |
| `EventToolResult` | `"tool.result"` | 工具结果 |
| `EventLLMCall` | `"llm.call"` | LLM 调用 |
| `EventLLMResponse` | `"llm.response"` | LLM 响应 |
| `EventPoolDispatch` | `"pool.dispatch"` | Pool 调度 |
| `EventPoolComplete` | `"pool.complete"` | Pool 完成 |

---

### Metrics（指标采集）

**包路径**：`internal/metrics`

Prometheus 格式的指标采集器，支持直方图和计数器。

```go
m := metrics.NewMetrics()

// 记录指标
m.RecordLLMCall(duration, err)
m.RecordToolCall(duration, err)
m.RecordTurn(duration)
m.IncActiveAgents()
m.DecActiveAgents()

// 获取快照
snapshot := m.Snapshot()

// Prometheus 格式输出
fmt.Println(m.String())
```

#### 指标列表

| 指标名 | 类型 | 说明 |
|--------|------|------|
| `ap_llm_total_calls` | Counter | LLM 调用总数 |
| `ap_llm_total_errors` | Counter | LLM 错误总数 |
| `ap_tool_total_calls` | Counter | 工具调用总数 |
| `ap_tool_total_errors` | Counter | 工具错误总数 |
| `ap_total_turns` | Counter | 推理轮次总数 |
| `ap_active_agents` | Gauge | 活跃 Agent 数 |
| `ap_pool_queue_length` | Gauge | Pool 队列长度 |
| `ap_memory_size_bytes` | Gauge | Memory 存储大小 |
| `ap_llm_latency_ms` | Histogram | LLM 延迟分布 |
| `ap_tool_latency_ms` | Histogram | 工具延迟分布 |
| `ap_turn_duration_ms` | Histogram | 轮次耗时分布 |

---

### Persist（检查点持久化）

**包路径**：`internal/persist`

```go
type CheckpointStore interface {
    Save(ctx context.Context, state *AgentState) error
    Load(ctx context.Context, agentID string) (*AgentState, error)
    List(ctx context.Context, sessionID string) ([]*AgentState, error)
    Delete(ctx context.Context, agentID string) error
}
```

内置实现：
- `SQLiteCheckpointStore` — SQLite 持久化
- `InMemoryCheckpointStore` — 内存存储（测试用）

---

### Concurrency（并发控制）

**包路径**：`internal/concurrency`

FileLock 管理器，带作用域重叠验证。

```go
flm := concurrency.NewFileLockManager()

// 获取读锁
flm.RLock(ctx, "scope-path")

// 获取写锁
flm.Lock(ctx, "scope-path")

// 释放
flm.RUnlock("scope-path")
flm.Unlock("scope-path")

// 验证作用域不重叠
concurrency.ValidateScopes([]string{"/path1", "/path2"})
```

---

### K8s Operator（Kubernetes 运维）

**包路径**：`operator/`（独立 Go module `agentprimordia/operator`，go.work 挂载；2026-08-09 修正自过时的 `internal/operator`）

K8s Operator 提供 Agent 的 Kubernetes 原生部署与管理能力，通过自定义资源（CRD）声明式定义 Agent 部署。

#### AgentDeployment CRD

`AgentDeployment` 是核心 CRD，定义了一个 Agent 的完整部署规格：

```yaml
apiVersion: agent.agentprimordia.io/v1alpha1
kind: AgentDeployment
metadata:
  name: my-agent
spec:
  replicas: 1
  template:
    spec:
      containers:
        - name: agent
          image: agentprimordia/agent:latest
          env:
            - name: AP_LLM_API_KEY
              valueFrom:
                secretKeyRef:
                  name: llm-secret
                  key: api-key
  metrics:
    port: 9090
    path: /metrics
  autoscaling:
    minReplicas: 1
    maxReplicas: 10
    targetConcurrentTasks: 5
```

#### Controller 调谐流程

Controller 遵循 Kubernetes 声明式调谐模式，处理 `AgentDeployment` 资源变更：

```
AgentDeployment 变更
    │
    ▼
┌──────────────────────────────────────────┐
│          Controller Reconcile            │
│                                          │
│  1. ConfigMap — 渲染 Agent 配置模板      │
│  2. Deployment — 管理 Agent Pod 生命周期  │
│  3. Service — 暴露 Metrics 端口          │
│  4. HPA — 基于并发任务数自动扩缩容        │
│  5. Status — 聚合真实 Pod 指标更新状态    │
└──────────────────────────────────────────┘
```

#### Metrics Service

Controller 自动创建 `{name}-metrics` Service，暴露端口 9090 供 Prometheus 抓取：

```yaml
apiVersion: v1
kind: Service
metadata:
  name: my-agent-metrics
spec:
  ports:
    - port: 9090
      targetPort: 9090
      name: metrics
```

#### HPA 自动扩缩容

基于 `concurrent_tasks_per_pod` Pods 指标实现自动扩缩容：

```go
type AutoscalingSpec struct {
    MinReplicas           *int32 `json:"minReplicas,omitempty"`
    MaxReplicas           int32  `json:"maxReplicas"`
    TargetConcurrentTasks int32  `json:"targetConcurrentTasks"` // 每个 Pod 目标并发任务数
}
```

HPA 通过自定义 Pods 指标 `concurrent_tasks_per_pod` 监控每个 Pod 的实时并发任务数，当超过 `TargetConcurrentTasks` 时自动扩容，低于阈值时缩容。

#### Status 状态聚合

Controller 实时聚合 Pod 指标到 `AgentDeployment` 的 Status 字段：

```go
type AgentDeploymentStatus struct {
    Replicas           int32   `json:"replicas"`
    ReadyReplicas      int32   `json:"readyReplicas"`
    ActiveTasks        int32   `json:"activeTasks"`
    AvgConcurrentTasks float64 `json:"avgConcurrentTasks"`
    Conditions         []Condition `json:"conditions,omitempty"`
}
```

状态数据来源于 Pod `/metrics` 端点的真实运行时指标，确保 Status 反映实际负载情况。

---

## 公共 API 层（pkg）

**包路径**：`pkg/`（导入名 `ap`）

所有公开类型通过 `pkg/agent.go` 重导出，用户只需导入一个包：

```go
import ap "github.com/Gleamseekers/AgentPrimordia/pkg"
```

导出映射：

| 公开名称 | 内部来源 |
|----------|----------|
| `ap.ReActAgent` | `agent.ReActAgent` |
| `ap.ReActConfig` | `agent.ReActConfig` |
| `ap.Message` | `agent.Message` |
| `ap.Response` | `agent.Response` |
| `ap.StreamEvent` | `agent.StreamEvent` |
| `ap.RAGDocument` | `agent.RAGDocument` |
| `ap.RAGProvider` | `agent.RAGProvider` |
| `ap.RAGConfig` | `agent.RAGConfig` |
| `ap.UserMessage()` | `agent.UserMessage()` |
| `ap.NewAgent()` | `agent.NewAgent()` |
| ... | ... |

---

## 自定义扩展指南

### 实现自定义 LLM Provider

```go
package myprovider

import (
    "context"
    "github.com/Gleamseekers/AgentPrimordia/internal/llm"
)

type MyProvider struct {
    apiKey string
    baseURL string
}

func NewMyProvider(apiKey, baseURL string) *MyProvider {
    return &MyProvider{apiKey: apiKey, baseURL: baseURL}
}

func (p *MyProvider) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
    // 实现同步补全逻辑
    // 将 req.Messages 转换为你 API 需要的格式
    // 调用 API，解析响应
    return &llm.CompletionResponse{
        Content: "response text",
        Usage:   llm.Usage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30},
    }, nil
}

func (p *MyProvider) Stream(ctx context.Context, req *llm.CompletionRequest) (<-chan llm.Chunk, error) {
    ch := make(chan llm.Chunk, 100)
    go func() {
        defer close(ch)
        // 实现流式输出逻辑
        ch <- llm.Chunk{Content: "partial ", Done: false}
        ch <- llm.Chunk{Content: "response", Done: true}
    }()
    return ch, nil
}

func (p *MyProvider) CallTools(ctx context.Context, req *llm.ToolCallRequest) (*llm.ToolCallResponse, error) {
    // 实现工具调用逻辑
    return &llm.ToolCallResponse{
        Content:   "tool result",
        ToolCalls: []llm.FunctionCall{...},
    }, nil
}

func (p *MyProvider) Embeddings(ctx context.Context, texts []string) ([][]float32, error) {
    // 实现文本嵌入逻辑
    return [][]float32{{0.1, 0.2, ...}}, nil
}

func (p *MyProvider) Info() llm.ModelInfo {
    return llm.ModelInfo{
        Name:              "my-model",
        Provider:          "my-provider",
        MaxContext:        8192,
        SupportsTools:     true,
        SupportsStreaming: true,
    }
}
```

然后用 ResilientProvider 包装以提高可靠性：

```go
provider := NewMyProvider("key", "https://api.example.com")
resilient := llm.NewResilientProvider(provider, llm.DefaultResilientConfig())
```

### 实现自定义 Tool

```go
package mytools

import (
    "context"
    "encoding/json"
    "github.com/Gleamseekers/AgentPrimordia/internal/tools"
)

type WeatherTool struct{}

func (t *WeatherTool) Name() string                 { return "weather" }
func (t *WeatherTool) Description() string           { return "查询指定城市的天气" }
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
    var params struct {
        City string `json:"city"`
    }
    if err := json.Unmarshal(args, &params); err != nil {
        return tools.NewErrorResult("invalid arguments"), nil
    }

    // 调用天气 API...
    weather := fmt.Sprintf("%s: 晴，25°C", params.City)
    return tools.NewResult(weather), nil
}
```

注册到 Agent：

```go
registry := tools.NewRegistry()
registry.Register(&WeatherTool{})
```

### 实现自定义 Memory 后端

```go
type MyMemoryStore struct{}

func (m *MyMemoryStore) Add(ctx context.Context, episode *memory.Episode) error {
    // 存储到你的后端
    return nil
}

func (m *MyMemoryStore) Search(ctx context.Context, query string, opts *memory.SearchOptions) ([]*memory.Episode, error) {
    // 实现搜索
    return []*memory.Episode{}, nil
}

// ... 实现其余 Memory 接口方法
```

集成到 Agent：

```go
agent, _ := agent.NewAgent("my-agent", "...", provider,
    agent.WithMemory(&MyMemoryStore{}),
)
```

### 实现自定义 CheckpointStore

```go
type RedisCheckpointStore struct{}

func (s *RedisCheckpointStore) Save(ctx context.Context, state *persist.AgentState) error {
    data, _ := state.Marshal()
    // 存储到 Redis
    return nil
}

func (s *RedisCheckpointStore) Load(ctx context.Context, agentID string) (*persist.AgentState, error) {
    // 从 Redis 加载
    return &persist.AgentState{}, nil
}

// ... 实现其余 CheckpointStore 接口方法
```

### 实现自定义 ContextWindowStrategy

```go
type SlidingWindowStrategy struct {
    WindowSize int
}

func (s *SlidingWindowStrategy) Trim(messages []agent.Message, maxMessages int) []agent.Message {
    effectiveMax := maxMessages
    if effectiveMax <= 0 {
        effectiveMax = s.WindowSize
    }
    if len(messages) <= effectiveMax {
        return messages
    }
    // 保留第一条（system prompt）和最后 N 条
    result := []agent.Message{messages[0]}
    result = append(result, messages[len(messages)-effectiveMax+1:]...)
    return result
}
```

---

## Hook 系统

Hook 系统允许在 Agent 生命周期的关键节点注入自定义逻辑。

### 12 个 Hook 点

| HookPoint | 常量 | 触发时机 |
|-----------|------|----------|
| `HookBeforeRun` | `"before_run"` | Agent 运行开始前 |
| `HookAfterRun` | `"after_run"` | Agent 运行结束后 |
| `HookBeforeTurn` | `"before_turn"` | 每轮推理开始前 |
| `HookAfterTurn` | `"after_turn"` | 每轮推理结束后 |
| `HookBeforeLLM` | `"before_llm"` | LLM 调用前 |
| `HookAfterLLM` | `"after_llm"` | LLM 响应后 |
| `HookBeforeTool` | `"before_tool"` | 工具执行前 |
| `HookAfterTool` | `"after_tool"` | 工具执行后 |
| `HookOnError` | `"on_error"` | 错误发生时 |
| `HookOnComplete` | `"on_complete"` | Agent 成功完成时 |
| `HookBeforeRAG` | `"before_rag"` | RAG 检索前 |
| `HookAfterRAG` | `"after_rag"` | RAG 检索后 |

### 使用示例

```go
hooks := agent.NewHookManager()

// 日志 Hook
hooks.Register(agent.HookBeforeTool, func(ctx context.Context, hctx *agent.HookContext) error {
    slog.Info("工具调用", "tool", hctx.ToolCall.Name, "args", hctx.ToolCall.Args)
    return nil
})

// 审计 Hook
hooks.Register(agent.HookAfterTool, func(ctx context.Context, hctx *agent.HookContext) error {
    auditLog.Record(hctx.ToolCall.Name, hctx.ToolResult)
    return nil
})

// 错误监控 Hook
hooks.Register(agent.HookOnError, func(ctx context.Context, hctx *agent.HookContext) error {
    sentry.CaptureException(hctx.Error)
    return nil  // 返回 nil 继续执行，返回 error 中断循环
})

// 带优先级的 Hook（数字越小越先执行）
hooks.RegisterWithPriority(agent.HookBeforeTool, rateLimitHook, -10)  // 最先执行
hooks.RegisterWithPriority(agent.HookBeforeTool, auditHook, 0)        // 正常优先级
```

**HookContext 字段**：

```go
type HookContext struct {
    AgentID    string
    SessionID  string
    Point      HookPoint
    Turn       int
    Message    *Message
    Response   *Response
    ToolCall   *ToolCall
    ToolResult *ToolResult
    Error      error
    Metadata   map[string]interface{}
}
```

---

## RAG 知识库集成

### 三种 RAG 模式

| 模式 | 常量 | 行为 |
|------|------|------|
| Auto | `RAGModeAuto` | 每轮推理前自动检索（默认） |
| First | `RAGModeFirst` | 仅在第一轮检索 |
| OnDemand | `RAGModeOnDemand` | 仅当 Agent 调用 `knowledge_search` 工具时检索 |

### 配置

```go
ragStore := memory.NewRAGStore(memStore, myEmbedder)

agent, _ := agent.NewAgent("my-agent", "...", provider,
    agent.WithRAG(agent.RAGConfig{
        Provider:         ragAdapter,  // 实现 RAGProvider 接口
        Mode:             agent.RAGModeAuto,
        TopK:             5,           // 返回最多 5 条
        MinScore:         0.3,         // 最低相关度阈值
        // ContextTemplate: "...",     // TODO: 自定义模板
    }),
)
```

### RAG 上下文注入

RAG 上下文作为 system 消息注入，通过 `Metadata.Extra["rag_context"]` 标记。如果已存在 RAG 上下文消息，后续轮次会替换而非追加，避免历史膨胀。

---

## 流式输出

```go
ch, err := agent.StreamRun(ctx, userMsg)
if err != nil {
    log.Fatal(err)
}

for event := range ch {
    switch event.Type {
    case agent.StreamEventToken:
        fmt.Print(event.Content)  // 逐 token 输出
    case agent.StreamEventThought:
        fmt.Printf("\n[思考] %s\n", event.Content)
    case agent.StreamEventToolCall:
        fmt.Printf("\n[调用工具] %s\n", event.Content)
    case agent.StreamEventToolResult:
        fmt.Printf("\n[工具结果] %s\n", event.Content)
    case agent.StreamEventComplete:
        fmt.Println("\n[完成]")
    case agent.StreamEventError:
        fmt.Printf("\n[错误] %s\n", event.Content)
    }
}
```

---

## Agent 生命周期管理

```go
// 运行中暂停
go func() {
    time.Sleep(5 * time.Second)
    agent.Pause()
}()

// 恢复执行
agent.Resume()

// 优雅停止
agent.Stop()

// 检查状态
stats := agent.Stats()
fmt.Println(stats.Status)  // idle / running / paused / completed / failed / cancelled
```

**注意**：`Run` 和 `StreamRun` 通过 `runMu` 互斥锁串行化，同一 Agent 不会并发执行。

---

## 错误处理与错误码

所有错误携带结构化错误码，便于程序化处理：

```go
resp, err := agent.Run(ctx, msg)
if err != nil {
    code := llm.GetCodeFromError(err)
    switch code {
    case llm.ErrCodeMaxTurnsExceeded:  // "AGENT_003"
        // 增加最大轮数重试
    case llm.ErrCodeCircuitOpen:       // "LLM_003"
        // Provider 不健康，切换降级
    case llm.ErrCodePathTraversal:     // "SEC_004"
        // 路径穿越攻击，记录安全事件
    }
}
```

完整错误码参见 `internal/llm/config.go` 中的 `ErrorCode` 常量定义。

---

## 并发安全设计

| 组件 | 锁类型 | 保护对象 |
|------|--------|----------|
| `ReActAgent` | `sync.Mutex (runMu)` | 串行化 Run/StreamRun |
| `ReActAgent` | `sync.RWMutex (statsMu)` | 保护 stats 字段 |
| `Registry` | `sync.RWMutex` | 保护工具注册表 |
| `ResilientProvider` | `sync.RWMutex` | 保护熔断状态和降级链 |
| `Lifecycle` | `sync.RWMutex` | 保护状态转换 |
| `HookManager` | `sync.RWMutex` | 保护 Hook 注册表 |
| `Bus` | `sync.RWMutex` | 保护订阅者列表 |
| `ACL` | `sync.RWMutex` | 保护规则列表 |
| `Sandbox` | `sync.RWMutex` | 保护命令白/黑名单 |
| `Pool` | `sync.RWMutex` | 保护任务映射 |

---

## 测试指南

### 运行测试

```bash
# 全量测试（含竞态检测）
make test

# 单独包测试
go test -v ./internal/agent/
go test -v ./internal/llm/
go test -v ./internal/memory/
go test -v ./internal/tools/

# 集成测试（需要 API Key）
make test-integration

# 手动运行集成测试（需要设置 OPENAI_API_KEY）
OPENAI_API_KEY=sk-xxx go test -tags=integration ./internal/llm/
```

> **注意**：集成测试需要真实的 API Key 才能运行。`make test-integration` 会自动检测 `OPENAI_API_KEY` 环境变量，未设置时跳过集成测试并输出警告。

### Agent 集成测试示例

```go
//go:build integration

func TestAgentIntegration(t *testing.T) {
    provider, err := llm.NewOpenAIProvider(llm.Config{
        APIKey: os.Getenv("OPENAI_API_KEY"),
        Model:  "gpt-4o-mini",
    })
    if err != nil {
        t.Skip("OPENAI_API_KEY not set, skipping integration test")
    }

    registry := tools.NewRegistry()
    registry.Register(&WeatherTool{})

    agent, _ := agent.NewAgent("integration-test-agent", "", provider,
        agent.WithMaxTurns(3),
        agent.WithToolkit(registry),
    )

    resp, err := agent.Run(context.Background(), agent.UserMessage("What's the weather?"))
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if resp.Content == "" {
        t.Error("expected non-empty response")
    }
}
```

### 使用 MockLLM

```go
// 固定回复
mock := llm.NewMockLLM("Hello, I'm a mock!")

// 队列式回复（每次调用返回下一个）
mock := llm.NewMockLLM("first response", "second response", "third")

// 工具调用模拟
mock.SetToolResponses([]llm.ToolCallResponse{
    {Content: "weather: sunny", ToolCalls: []llm.FunctionCall{}},
})

// 错误模拟
mock.SetError(errors.New("API rate limit"))

// 查询调用记录
count := mock.CallCount()
lastReq := mock.LastRequest()
```

### 测试 Agent

```go
func TestMyAgent(t *testing.T) {
    mock := llm.NewMockLLM("I'll check the weather for you.")
    
    registry := tools.NewRegistry()
    registry.Register(&WeatherTool{})
    
    agent, _ := agent.NewAgent("test-agent", "", provider,
        agent.WithMaxTurns(3),
        agent.WithToolkit(registry),
    )
    
    resp, err := agent.Run(context.Background(), agent.UserMessage("What's the weather?"))
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if resp.Content == "" {
        t.Error("expected non-empty response")
    }
}
```

---

## 构建与部署

### 本地构建

```bash
make build          # 编译所有包
make test           # 测试 + 覆盖率报告
make lint           # 静态分析
make clean          # 清理构建产物
```

### Docker 部署

```bash
make docker-build   # 构建镜像
make docker-run     # 启动服务
make docker-clean   # 清理容器和镜像
```

Dockerfile 位于项目根目录，支持多阶段构建，最终镜像基于 scratch。

### K8s 部署

通过 Kubernetes Operator 部署 Agent，使用 `kubectl apply` 声明式管理：

```bash
# 安装 CRD 和 Operator
kubectl apply -f operator/manifest/crd.yaml
kubectl apply -f operator/manifest/controller.yaml

# 部署 Agent
kubectl apply -f examples/agent-deployment.yaml

# 查看部署状态
kubectl get agentdeployment
kubectl describe agentdeployment my-agent

# 查看自动扩缩容状态
kubectl get hpa
```

Operator 会自动创建 ConfigMap、Deployment、Service 和 HPA 资源，详见 [K8s Operator](#k8s-operatorkubernetes-运维) 章节。

### 环境变量配置

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `AP_LLM_API_KEY` | LLM API 密钥 | — |
| `AP_LLM_BASE_URL` | LLM API 地址 | OpenAI 默认 |
| `AP_LLM_MODEL` | 模型名称 | — |
| `AP_LLM_TEMPERATURE` | 温度 | 0 |
| `AP_LLM_MAX_TOKENS` | 最大 token 数 | 0 (不限) |
| `AP_LLM_EXTRA_*` | 额外配置 | — |

---

## v0.7 → v0.8 迁移指南

### 新特性（向后兼容）

1. **简化 Agent 入口** — 推荐使用 `ap.NewAgent()`，返回 `(*CapabilityAgent, error)`
   ```go
   // v0.8.0 推荐写法
   agent, err := ap.NewAgent("hello", "你是一个助手", provider, ap.WithMaxTurns(10))
   ```

2. **协议式微内核** — 通过 17 个 Capable 接口实现能力自动发现与组合

3. **7 个内置工具** — FileSystem、Shell、Web、API、Database、CodeExecution、Knowledge

4. **20+ 生命周期 Hook** — 覆盖 Agent 全生命周期的审计、告警、成本追踪

5. **A2A 协议** — Agent 间协作，支持自动 Agent Card 发现和异步任务执行

6. **自反思能力** — Agent 可评估自身输出质量并自动改进

7. **工具学习** — Agent 可自动发现和学习新工具

8. **配置热加载** — 运行时动态更新配置，无需重启

### 破坏性变更

无。v0.8.0 完全向后兼容 v0.7.x。

### 依赖变更

- Go 版本要求：1.22 → **1.26+**
- 外部依赖：核心路径零第三方依赖（纯 Go SQLite 驱动 modernc.org/sqlite）；受限第三方依赖（gRPC/etcd/redis/wazero 等，均有 build tag 或模块边界）见 [AGENTS.md](../AGENTS.md) §2.1
