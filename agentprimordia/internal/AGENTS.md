# internal/ 模块上下文路由

> 本文件为嵌套上下文路由，适用于 `agentprimordia/internal/` 下所有子模块。
> 根规则见仓库根目录 [`AGENTS.md`](../../AGENTS.md)，本文件仅补充 internal 特有的模块边界与依赖方向约束。

## 模块总览

本目录包含顶层 29 个子包；其中 `agent/` 下另有 32 个子包，本表仅列常被跨包引用的 a2a/planning/reflection/tool_learning 等（完整清单见 `internal/agent/` 目录）。

| 子包 | 职责 |
|------|------|
| `admin/` | Admin HTTP API（调试/管理接口） |
| `agent/` | ReActLoop 引擎 + 协议式微内核（顶层入口） |
| `agent/a2a/` | Agent2Agent 协议实现（JSON-RPC / SSE / gRPC） |
| `agent/planning/` | 任务规划器 |
| `agent/reflection/` | Agent 自反思能力 |
| `agent/tool_learning/` | 工具学习/自动发现 |
| `agent/worldmodel/` | 世界模型（状态图/预演/回溯/state-checkpoint；自 v6.1 起 opt-in，**默认仍关闭**："v7.0 翻默认"未执行） |
| `audit/` | 审计日志（事件模型唯一真相源，agent/observability 复用其 Event） |
| `chaos/` | 混沌工程故障注入 |
| `concurrency/` | 文件锁等并发原语 |
| `config/` | 配置热加载 |
| `debugger/` | 调试器 / Inspector / 可视化编辑器 |
| `eval/` | 评估框架 |
| `events/` | 内部事件总线 |
| `governance/` | 治理策略（配额/租户/策略引擎） |
| `guardrail/` | 输入/输出护栏（注入检测、PII、主题过滤） |
| `health/` | 健康检查 |
| `jsonutil/` | JSON 工具（对象池等） |
| `llm/` | LLM 抽象层与多家 Provider 实现 |
| `logger/` | 日志抽象 |
| `marketplace/` | 插件远程协议 + cosign 验签 |
| `memory/` | 记忆存储（SQLite / InMemory / RAG / Vector） |
| `metrics/` | Prometheus 指标收集 |
| `multi_agent/` | 多 Agent Swarm 编排（经 pkg Experimental 导出） |
| `observability/` | trace → 指标 → 审计 全链路关联；OTel 桥接与 OTLP 导出在 `observability/export/otel` |
| `orchestration/` | 编排模式（Pipeline / Handoff / DAG / GroupChat / Debate） |
| `persist/` | 状态持久化与 Checkpoint |
| `pool/` | 多 Agent 调度与会话管理 |
| `resilience/` | 弹性（熔断/重试） |
| `security/` | ACL / Sandbox / 路径校验 |
| `self_bootstrap/` | AP 用 AP 自举 |
| `studio/` | Studio 面板后端 |
| `tools/` | 工具系统（注册表、执行器、MCP、内置工具） |
| `tools/builtin/` | filesystem / shell / web / api / database / code_execution |

## 依赖方向约束（与根 AGENTS.md §4.2 一致）

```
        ┌────────────────────────────────────────┐
        │           agent/  (顶层)               │
        │   引用 llm, memory, persist, tools    │
        └────┬───────┬───────┬───────────┬──────┘
             │       │       │           │
        ┌────▼─┐ ┌───▼──┐ ┌──▼───┐ ┌────▼────┐
        │ llm  │ │memory│ │persist│ │  tools  │
        └──────┘ └──────┘ └───────┘ └────┬────┘
                                          │
                                     ┌────▼────┐
                                     │  pool   │
                                     └─────────┘
```

### 分层规则

1. **顶层 — `agent/`**：可引用 `llm/memory/persist/tools/pool/orchestration/security/metrics/observability/events/config/prompt/concurrency/guardrail/health/logger/jsonutil/resilience` 等下层/横向模块。
2. **核心下层 — `llm/`、`memory/`、`persist/`、`tools/`**：
   - 禁止 import `agent/`、`pool/`、`orchestration/`、`debugger/`、`admin/`。
   - 可引用同层或更基础的横向模块（如 `logger/`、`jsonutil/`、`events/`）。
3. **横向支撑层 — `orchestration/`、`debugger/`、`metrics/`、`guardrail/`、`security/`、`events/`、`config/`、`admin/`、`chaos/`、`eval/`、`governance/`、`health/`、`audit/`、`resilience/`、`concurrency/`、`logger/`、`jsonutil/`、`observability/`、`studio/`、`self_bootstrap/`、`marketplace/`、`multi_agent/`**：
   - 可消费 `agent/` 及以下模块能力。
   - 不得被 `llm/memory/persist/tools` 反向引用。
4. **`pool/`**：处于 `tools/` 下层，可引用 `tools/`、`agent/` 及横向模块。

### 禁止的 import 方向

| 禁止方 | 不得 import |
|--------|-------------|
| `llm/` | `agent/`、`pool/`、`orchestration/`、`debugger/`、`admin/` |
| `memory/` | `agent/`、`pool/`、`orchestration/`、`debugger/`、`admin/` |
| `persist/` | `agent/`、`pool/`、`orchestration/`、`debugger/`、`admin/` |
| `tools/` | `agent/`、`pool/`、`orchestration/`、`debugger/`、`admin/` |

## 第三方依赖边界

在 internal/ 内使用白名单外依赖的限制（详见根 AGENTS.md §2.1）：

- `modernc.org/sqlite` → `memory/`、`llm/`（cache_sqlite）、`persist/`（sqlite_checkpoint）、`tools/`（builtin database / data_tools）
- `google.golang.org/grpc` + `protobuf` → 仅 `agent/a2a/` 及其子包、`agent/cluster/`（grpc_bus）、`agent/transport/`（grpc）
- `go.etcd.io/etcd/client/v3` → 仅 `persist/` 与 `agent/cluster/`（均带 `etcd` build tag）
- `github.com/redis/go-redis/v9` → 仅 `persist/`（`redis` build tag）
- `github.com/jackc/pgx/v5` → **禁止在 internal/ 直接 import**；仅由 `pgvector/` 独立模块 require，internal 通过 `github.com/Gleamseekers/AgentPrimordia/pgvector` 间接使用
- `gopkg.in/yaml.v3` → 仅 `config/`、`governance/`（策略/配置 YAML 解析）

## 编码约定

- 中文注释，与根 AGENTS.md §3 一致
- TDD 强制：先写 `_test.go`，再写实现
- 接口优先：跨模块交互通过接口解耦
- 并发安全：共享状态必须加锁
- 错误处理：使用 `pkg/errors.go` 中的错误变量
- 测试中使用 `WithInMemory()` / `MockLLM` / `httptest.Server` 隔离外部依赖
