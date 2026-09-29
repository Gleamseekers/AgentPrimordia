# AgentPrimordia 工作区规则

> 此规则优先级最高，不可跳过，不可以"任务简单"为由省略

## 1. 项目定位

AgentPrimordia 是从 CodeCast 生产验证的 Agent 架构中提炼出的 **通用 Go Agent 开发框架**。
- 不是 CodeCast 应用本身
- 核心价值：ReActLoop、AgentPool、Tool System、Memory Store、LLM Abstraction
- 目标用户：任何想用 Go 构建 AI Agent 应用的开发者

## 2. 技术栈约束

- **语言**: Go 1.26+（`go.mod` 已声明 `go 1.26.6`）
- **默认原则：所有新模块仅使用 Go 标准库**（net/http、database/sql、os/exec 等）
- **不引入**：任何 Web 框架、ORM、配置解析库、CLI 框架等第三方包

### 2.1 已批准的第三方依赖（白名单）

下列依赖因历史原因存在于 `go.mod`，新增代码**不得引入**白名单之外的第三方包：

- `modernc.org/sqlite` — 纯 Go SQLite 驱动（无 CGO），用于需要持久化/嵌入式 DB 的场景：`internal/memory`（SQLiteStore）、`internal/llm`（cache_sqlite）、`internal/persist`（sqlite_checkpoint）、`internal/tools`（builtin database 工具与 data_tools）、`ecosystem/plugins/kv`。
- `gopkg.in/yaml.v3` — 仅用于配置/模板/策略文件的 YAML 解析：`cmd/ap` 脚手架子命令、`internal/config`（配置热加载）、`internal/governance`（策略文件解析）。不作为通用配置库。
- `google.golang.org/grpc` + `google.golang.org/protobuf` + 间接依赖 `google.golang.org/genproto/googleapis/rpc` — **仅限 `internal/agent/a2a/` 及其子包，以及 `internal/agent/cluster/`（`grpc_bus.go`，跨节点消息复用 A2A gRPC 基础设施，见 V3.1 计划 3.2）与 `internal/agent/transport/`（`grpc.go`）** 使用；另允许 `pkg/a2a.go` 作为公共 API re-export 层引用 grpc 选项类型（§4.2 规定的 pkg 导出职责，v4.0-3 评审已确认）。用于实现 Agent2Agent 协议与跨节点传输（gRPC + protobuf 是该协议的事实标准）。
- `go.etcd.io/etcd/client/v3` — etcd 客户端（G2-3 分布式检查点后端）。**仅限 `internal/persist/` 与 `internal/agent/cluster/` 下带 `etcd` build tag 的文件** 使用（persist 为检查点后端，cluster 为分布式 KV/服务发现）。etcd 是分布式强一致协调的行业标准协议，其客户端无 Go 标准库等价实现，符合 §2.2 硬性需求豁免。
- `github.com/redis/go-redis/v9` — Redis 客户端（G2-3 分布式检查点后端）。**仅限 `internal/persist/` 下带 `redis` build tag 的文件** 使用。Redis 线协议客户端属行业标准实现，无法用标准库合理复现，符合 §2.2 硬性需求豁免。
- `github.com/tetratelabs/wazero` — 纯 Go（CGO-free）WebAssembly 运行时（G3-3 WASM 执行）。**仅限工作区根 `wasm/` 模块与主模块内 `wasm/` 包**（`github.com/Gleamseekers/AgentPrimordia/agentprimordia/wasm/`）使用。WASM 运行时无标准库等价实现，wazero 为 CGO-free 纯 Go 实现，符合 §2.2 硬性需求豁免。该依赖同时是 code 层「沙箱受控释放」（§2.3）的隔离底座：除上述两处外任何包不得 import wazero（CI 断言 A1 强制）。
- `github.com/jackc/pgx/v5` + `pgvector/` 模块（`github.com/Gleamseekers/AgentPrimordia/pgvector`，go.mod `replace => ../pgvector`）— PostgreSQL/pgvector 向量存储（`internal/memory/pgvector_store.go`）。pgx 为 PostgreSQL 事实标准驱动，无标准库等价实现，符合 §2.2 硬性需求豁免。**边界：pgx 仅由 pgvector 模块直接 require，内部代码不得直接 import pgx**。

### 2.2 依赖扩展的审批流程

如需新增白名单外的第三方包，必须满足以下条件之一：

- 存在无法用 Go 标准库复现的硬性需求（例如某个行业协议的标准实现）
- 新增功能位于 `internal/agent/a2a/` 范围内（沿用 A2A 协议栈）
- 任何其他场景需先在 PR 中说明理由并征得维护者同意

依赖的真实使用边界以 `go mod why -m <package>` 输出为准；如发现某依赖被白名单外的包引用，应立即调整或回滚。

> **审批记录（2026-07-09）**：经维护者确认，新增 `go.etcd.io/etcd/client/v3`、`github.com/redis/go-redis/v9`、`github.com/tetratelabs/wazero` 三项白名单外依赖（分别对应 G2-3 分布式检查点、G3-3 WASM 执行所需）。三项均属「行业标准协议/运行时、无法用 Go 标准库复现」的硬性需求豁免，使用边界见上 §2.1。

### 2.3 code 层安全边界（宿主永久拒绝 + 沙箱受控释放）

- INV-0：宿主进程运行期零写入、零编译、零加载任何 agent 生成的代码；
  agent 生成代码唯一合法执行位置是 wazero WASM 沙箱（github.com/Gleamseekers/AgentPrimordia/agentprimordia/wasm/、
  工作区根 wasm/ 模块），且必须经签名验证与对抗测试后方可注册为工具。
- 宿主边界由确定性断言 A1–A8 保证（定义见 docs/提案-code层沙箱受控释放.md §二），
  断言测试全部进 CI、失败即红；本边界属于确定性安全不变式，允许 100%/0 容忍
  （V7 路线图 §一 R3）。
- learning 反馈通道的 code 层拒绝（internal/agent/learning/feedback.go ScopeCode）
  保留不变：feedback 通道 code 层仍走人工代码评审，沙箱释放不改变该通道语义。
- 本条的任何修订属安全承诺变更，须维护者书面批准（流程见
  docs/提案-code层沙箱受控释放.md §七）。对应的 `etcd_checkpoint.go` / `redis_checkpoint.go` 经 build tag 门控，`wazero` 仅限 `wasm/` 模块，不污染默认构建。

## 3. 代码规范

- **TDD 强制**: 所有功能必须先写测试（Red → Green → Refactor）
- **接口优先**: LLM、Tools、Memory、Pool 全部通过接口解耦
- **并发安全**: 共享状态必须用 sync.RWMutex / sync.Mutex / channel 保护
- **错误处理**: 使用 `pkg/errors.go` 中定义的错误变量
- **中文注释**: 代码注释使用中文
- **代码风格**: 与现有代码保持一致（参考 internal/agent/、internal/pool/）
- **确定性安全不变式**: 安全边界断言（宿主零写入零加载、签名前置、导入段白名单等，见 §2.3）必须以确定性测试固化并全量进 CI；此类断言允许 100%/0 容忍（V7 路线图 R3），禁止以抽样统计口径改写

## 4. 模块边界

> **2026-06 更新**: 随着 Phase 6 及后续阶段演进，项目模块已从最初的 6 个核心包扩展为更大的 monorepo 结构。
> 当前 `agent/` 仍是 ReAct 循环与协议式微内核的顶层入口；完整模块清单见 [`internal/AGENTS.md`](agentprimordia/internal/AGENTS.md)。
> 历史 Phase 实施记录见 [`docs/CHANGELOG.md`](docs/CHANGELOG.md)。

### 4.1 当前模块结构

```
internal/
├── admin/          — Admin HTTP API（调试/管理接口）
├── agent/          — ReActLoop 引擎 + 协议式微内核（顶层，下含 a2a/planning/reflection/tool_learning/cluster/transport 等 32 个子包，完整清单见 internal/AGENTS.md）
│   ├── a2a/        — Agent2Agent 协议实现（JSON-RPC / SSE / 任务管理）
│   ├── planning/   — 任务规划器
│   ├── reflection/ — Agent 自反思能力
│   ├── worldmodel/ — 世界模型（实体/关系/因果算子状态图；自 v6.1 起经 WithWorldModel() opt-in，**默认仍关闭**——"v7.0 翻默认"未执行，见 docs/实验性能力清单.md）
│   └── tool_learning/ — 工具学习/自动发现
├── concurrency/    — 文件锁等并发原语
├── config/         — 配置热加载
├── debugger/       — 调试器 / Inspector / 可视化编辑器
├── events/         — 内部事件总线
├── guardrail/      — 输入/输出护栏（注入检测、PII、主题过滤等）
├── llm/            — LLM 抽象层与多家 Provider 实现
├── memory/         — 记忆存储（SQLite / InMemory / RAG / Vector）
├── metrics/        — Prometheus 指标收集
├── orchestration/  — 编排模式（Pipeline / Handoff / DAG / GroupChat / Debate）
├── persist/        — 状态持久化与 Checkpoint
├── pool/           — 多 Agent 调度与会话管理
├── security/       — ACL / Sandbox / 路径校验
└── tools/          — 工具系统（注册表、执行器、MCP、内置工具）
    └── builtin/    — filesystem / shell / web / api / database / code_execution

pkg/                — 公共 API 导出（类型别名 + 少量辅助构造器）
ecosystem/          — 示例、插件、模板、生态文档
operator/           — Kubernetes Operator（AgentDeployment CRD）
pgvector/           — pgvector 向量存储扩展
```

> 上图仅列核心分层包。其余横向支撑包（audit/chaos/eval/governance/health/jsonutil/logger/marketplace/multi_agent/observability/resilience/self_bootstrap/studio）见 [`internal/AGENTS.md`](agentprimordia/internal/AGENTS.md) 的完整 29 包表格。

### 4.2 依赖方向规则

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

        orchestration/、debugger/、metrics/、observability/、guardrail/、
        security/、events/、config/、agent/prompt/ 等模块为横向支撑层，
        可消费 agent/llm/memory/tools/pool 等下层能力。
```

- **`agent/` 处于依赖顶层**：可引用 `llm/memory/persist/tools/pool/orchestration/security/metrics/observability/events/config/agent/prompt/concurrency` 等下层/横向模块。
- **下层模块禁止反向引用上层**：`llm/memory/persist/tools` 不得 import `agent/`、`pool/`、`orchestration/`。
- **编排/调试/可观测等横向模块**：可引用 `agent/` 及以下模块，但不得被 `llm/memory/persist/tools` 反向引用。
- **`pkg/` 以类型导出和 re-export 为主**：允许少量不可或缺的公共错误/辅助构造器（如 `pkg/errors.go` 中的 `CodeError`），新增业务逻辑应优先放在 `internal/`。
- **`ecosystem/` 与 `internal/` 解耦**：`ecosystem/plugins/*`、`ecosystem/examples/*`、`ecosystem/templates/*` 应仅通过 `pkg/` 公共 API 与核心交互，不直接 import `internal/*`。
  - **当前状态**：部分示例与插件仍直接依赖 `internal/*`，属于已知的技术债务，需逐步迁移至公共 API。
- **`operator/`、`pgvector/`**：独立模块，与 `internal/` 通过 `pkg/` 或 CRD 解耦。
- **code 层生成工具通道（v6.3 起）**：agent 生成工具的生成/彩排/注册链路位于
  internal/tools/lifecycle/（生命周期框架）与 github.com/Gleamseekers/AgentPrimordia/agentprimordia/wasm/（沙箱执行），
  依赖方向同上层规则；生成工件（WASM 字节码+清单）是数据不是代码——宿主编译
  与加载边界由 §2.3 INV-0 与断言 A1–A8 强制。TS 侧仅协议对等（签名/工具包格式/
  注册客户端），沙箱执行 Go-only（docs/双线豁免矩阵.md B4/#3 豁免）。

## 5. 测试要求

- 每个新功能必须有对应测试
- 使用 `t.TempDir()` 创建临时文件，不污染项目
- Shell/Web 工具测试用 `httptest.Server` 或模拟，不需要真实网络
- Memory 测试用 `WithInMemory()` 创建内存数据库
- MockLLM 用于 agent/pool 层测试；DemoLLM 用于示例应用
- 世界模型（internal/agent/worldmodel/）状态更新必须带断言一致性测试：同一任务轨迹上，状态图断言与消息序列回放结果逐条对账，不一致即测试失败（V7 路线图 §三随行项；回放断言准确率验收口径见 V7 §三命题 2；门落点 internal/agent/worldmodel_consistency_test.go）

## 6. 提交粒度

- 每个 Task 完成后应可独立编译和通过测试
- 提交信息格式: `feat: xxx` / `fix: xxx` / `refactor: xxx`
- 不要在一个提交中混合多个 Task 的改动

### 6.1 多阶段计划执行约定

当用户引用实施计划文档（如 `docs/*-IMPLEMENTATION-PLAN.md`）并要求批量执行时，遵循以下流程：

1. **解析计划**：读取计划文档，提取编号任务列表；识别每项的目标文件、依赖关系和验证方式
2. **逐项执行**：按编号顺序逐一实施；每项完成后立即运行验证门（`make test-short` 或变更包对应的 `go test ./internal/<pkg>/...`）
3. **状态报告**：每完成一项或一批后，向用户报告当前进度（已完成/总数、失败项）
4. **提交**：全部通过验证后，按 §6 提交粒度规范 commit（每个 Task 独立提交）

**停止规则**：
- 任何一项的验证门失败 → 立即停止，报告失败项、错误输出和已完成的进度
- 用户指示暂停 → 报告当前进度，标记下一项编号
- 全部完成 → 报告最终状态并等待用户确认提交

## 7. 文档同步

- 用户文档: `agentprimordia/docs/` 下各模块文档
- 代码变更后如影响设计文档，需同步更新

## 8. CodeCast 参考代码

AgentPrimordia 最初从 CodeCast 生产架构中提炼核心模式，相关参考实现已融入当前代码库：

- **Pool + FileLock + Scope 校验** → `internal/pool/`、`internal/concurrency/`、`internal/tools/scope.go`
- **ReAct Loop + OpenAI HTTP 调用** → `internal/agent/react_loop.go`、`internal/llm/openai_provider.go`
- **工具分发 + FilesScope 权限** → `internal/tools/executor.go`、`internal/tools/builtin/filesystem.go`
- **增强 Memory (topics/importance/cleanup)** → `internal/memory/`、特别是 `sqlite.go` / `summarizer.go`

> 注：原 `CodeCast-desktop/` 参考目录已不再随仓库分发，上述对应关系用于追溯设计来源。

**提取模式而非复制代码**：理解 CodeCast 设计意图后，在 AP 中用更通用、接口化的方式重新实现。
