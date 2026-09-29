# AgentPrimordia Code Wiki

> 万物之源,智能之始 — 生产级 AI Agent 开发框架 (Go + TypeScript 双语言 SDK)

**版本**: v6.0.0（截至 v6.0.0 全弧线已实现） | **语言**: Go 1.26+ / TypeScript 5.4+ | **许可**: Apache-2.0 | **CGO**: 核心零 CGO（SQLite + YAML）；可选 gRPC/Redis/etcd/wazero 按需引入

> 注：本 Wiki 主体成文于 v3.6 时期。v5.1–v6.0 新增能力（策略自适应、记忆固化、组织智能、自进化反馈、自举季度曲线）未在此展开，详见 [docs/V6路线图.md](docs/V6路线图.md)。

---

## 目录

- [1. 项目概述](#1-项目概述)
- [2. 架构总览](#2-架构总览)
- [3. 项目结构](#3-项目结构)
- [4. 核心模块详解](#4-核心模块详解)
  - [4.1 agent — ReAct 引擎与编排](#41-agent--react-引擎与编排)
  - [4.2 llm — LLM 抽象层](#42-llm--llm-抽象层)
  - [4.3 tools — 工具系统](#43-tools--工具系统)
  - [4.4 memory — 记忆存储](#44-memory--记忆存储)
  - [4.5 pool — 多 Agent 调度](#45-pool--多-agent-调度)
  - [4.6 persist — 状态持久化](#46-persist--状态持久化)
  - [4.7 guardrail — 安全防护](#47-guardrail--安全防护)
  - [4.8 metrics — 可观测性](#48-metrics--可观测性)
  - [4.9 events — 事件总线](#49-events--事件总线)
  - [4.10 security — ACL 与沙箱](#410-security--acl-与沙箱)
  - [4.11 otel — OpenTelemetry 桥接](#411-otel--opentelemetry-桥接)
  - [4.12 orchestration — 编排引擎](#412-orchestration--编排引擎)
  - [4.13 concurrency — 并发原语](#413-concurrency--并发原语)
  - [4.14 config — 配置管理](#414-config--配置管理)
  - [4.15 prompt — Prompt 工程](#415-prompt--prompt-工程)
  - [4.16 debugger — 调试器](#416-debugger--调试器)
  - [4.17 admin — 管理 API](#417-admin--管理-api)
  - [4.18 governance — 多租户与治理](#418-governance--多租户与治理)
  - [4.19 security — 密钥管理与加密](#419-security--密钥管理与加密)
  - [4.20 health — SLO/SLI 与增强诊断](#420-health--slosli-与增强诊断)
  - [4.21 logger — 结构化日志与 Shipper](#421-logger--结构化日志与-shipper)
  - [4.22 eval — 评估框架](#422-eval--评估框架)
  - [4.23 orchestration — MapReduce](#423-orchestration--mapreduce)
  - [4.24 transport — gRPC 与连接池](#424-transport--grpc-与连接池)
  - [4.25 tokencache — 语义缓存](#425-tokencache--语义缓存)
  - [4.26 memory — 分层记忆与生命周期](#426-memory--分层记忆与生命周期)
  - [4.27 tools — 动态注册与插件市场](#427-tools--动态注册与插件市场)
  - [4.28 debugger — 断点与时间旅行](#428-debugger--断点与时间旅行)
  - [4.29 audit — 合规报告](#429-audit--合规报告)
  - [4.30 wasm — 增强沙箱](#430-wasm--增强沙箱)
  - [4.31 chaos — 混沌工程](#431-chaos--混沌工程)
  - [4.32 autonomy — 长期自治](#432-autonomy--长期自治)
  - [4.33 skills — 技能进化](#433-skills--技能进化)
  - [4.34 realtime — 多模态实时](#434-realtime--多模态实时)
- [5. 协议式微内核架构](#5-协议式微内核架构)
- [6. 插件生态](#6-插件生态)
- [7. 公共 API 层 (pkg/)](#7-公共-api-层-pkg)
- [8. 依赖关系图](#8-依赖关系图)
- [9. 关键接口总览](#9-关键接口总览)
- [10. 错误码体系详解](#10-错误码体系详解)
- [11. 快速上手](#11-快速上手)
- [12. 构建与运行](#12-构建与运行)
- [13. Docker 部署](#13-docker-部署)
- [14. 测试体系](#14-测试体系)
- [15. 性能基准测试](#15-性能基准测试)
- [16. 示例应用](#16-示例应用)
- [17. Provider 贡献指南](#17-provider-贡献指南)
- [18. TypeScript SDK](#18-typescript-sdk)
- [19. 版本迁移指南](#19-版本迁移指南)
  - [19.1 v0.7 → v0.8 迁移](#191-v07--v08-迁移)
  - [19.2 v0.6 → v0.7 迁移](#192-v06--v07-迁移)
  - [19.3 v1.0 → v2.0 迁移](#193-v10--v20-迁移)
  - [19.4 v2.0 → v3.0 迁移](#194-v20--v30-迁移)
- [20. pgvector 适配器](#20-pgvector-适配器)
- [21. 相关文档](#21-相关文档)
- [22. 设计哲学](#22-设计哲学)

---

## 1. 项目概述

**AgentPrimordia** 是一个用 Go 构建的通用 AI Agent 开发框架，从 CodeCast 生产环境验证的 Agent 架构中提炼而来。

### 核心特性

| 能力 | 说明 |
|------|------|
| **ReAct Loop 引擎** | Reasoning + Acting 循环，20+ 生命周期钩子 |
| **协议式微内核** | 20 个 Capable 接口 + 链式 API，能力按需组合、自动发现（v3.3–v3.6 新增 AutonomyCapable / SkillsCapable / RealtimeCapable） |
| **多 Agent 编排** | Pipeline / Handoff / Parallel / DAG / GroupChat / Collaboration / Workflow |
| **工具系统** | FileSystem / Shell / Web / API / Database / CodeExecution / Knowledge 内置，MCP 协议集成，插件扩展 |
| **三层记忆** | SQLite FTS5 + Vector Store + RAG Pipeline 混合检索 |
| **LLM 抽象** | 10+ 家 Provider（OpenAI / Anthropic / Gemini / Ollama / Azure / Qwen / GLM / Mistral / Cohere / DeepSeek / 多模态 / 弹性包装器） |
| **Resilient Provider** | 自动重试 / 降级 / 熔断 |
| **Pool 调度** | 信号量并发控制，会话隔离，重试策略 |
| **安全防护** | ACL / Sandbox / Guardrails / PII 检测 / 路径遍历防护 + symlink 逃逸防护 |
| **可观测性** | Prometheus Metrics / OpenTelemetry / Grafana Dashboard |
| **K8s Operator** | AgentDeployment CRD 声明式部署 |
| **长期自治 (v3.3)** | 目标驱动自主规划/执行/校验/重规划，崩溃恢复 + 幂等保护（`internal/agent/autonomy/`） |
| **技能进化 (v3.4)** | 运行中习得/验证/沉淀可复用技能，语义匹配自动调用（`internal/agent/skills/`） |
| **协议互操作 (v3.5)** | 对齐开放 Agent2Agent 协议，跨生态委托 + 符合性报告（`internal/agent/a2a/interop_*`） |
| **多模态实时 (v3.6)** | 语音/视觉实时双向流 + 打断，ASR/TTS 可插拔（`internal/agent/realtime/`） |
| **CLI 工具** | `ap init / run / debug / loop / test / mcp / plugin / autonomy / skill / a2a / realtime / doctor / config / completion` |
| **TypeScript SDK** | Go 功能对等，模块全覆盖（含 v3.3 自治 / v3.4 技能 / v3.5 互操作 / v3.6 实时 对等实现） |

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

### 设计原则

1. **接口驱动** — 所有子系统通过 Go interface 解耦，可独立替换
2. **组合优于继承** — Agent 能力通过配置组合（Memory、Hooks、RAG 等），而非继承
3. **弹性优先** — ResilientProvider 内建重试、降级、熔断，生产级可靠性
4. **最小外部依赖** — 核心零 CGO，仅依赖纯 Go SQLite + YAML；可选 gRPC/Redis/etcd/wazero 按需引入
5. **协议式微内核** — 能力通过接口发现，而非配置字段
6. **链式 API** — `NewAgent(...).WithMemory(mem).WithRAG(cfg)` 风格
7. **TDD 强制** — 每个功能先写测试，Red → Green → Refactor

---

## 2. 架构总览

```
┌──────────────────────────────────────────────────────────┐
│                    Your Application                       │
├──────────────────────────────────────────────────────────┤
│                   AgentPrimordia Framework                 │
│                                                            │
│  ┌──────────┐ ┌──────────┐ ┌────────┐ ┌──────────────┐  │
│  │ ReActLoop │ │   Pool   │ │  DAG   │ │  Pipeline    │  │
│  │ (Engine) │ │(Dispatch)│ │(Graph) │ │(Sequential)  │  │
│  └────┬─────┘ └────┬─────┘ └───┬────┘ └──────┬───────┘  │
│       └────────────┼───────────┼──────────────┘          │
│              ┌──────┴──────┐                               │
│              │ Tool System │                               │
│              │ ┌─────────┐ │                               │
│              │ │Built-in │ │  MCP  │  Plugin │             │
│              │ │FS/Shell/│ │  Client│  System │             │
│              │ │Web/Know │ │  Reg.  │         │             │
│              │ └─────────┘ │        │         │             │
│              └─────────────┘        │         │             │
│                                    │         │             │
│  ┌─────────────────────────────────────────────────────┐  │
│  │                  LLM Layer                          │  │
│  │  OpenAI │ Anthropic │ Gemini │ Ollama │ Azure │ ...│  │
│  │              ResilientProvider                       │  │
│  └─────────────────────────────────────────────────────┘  │
│                                                            │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌────────────┐  │
│  │ Memory   │ │ EventBus │ │ Metrics  │ │ Guardrails │  │
│  │SQLite+FTS│ │(Pub/Sub) │ │OTel/Prom │ │ACL/Sandbox │  │
│  │ +Vector  │ │          │ │ +pprof   │ │  PII检测   │  │
│  │ +RAG(RRF)│ │          │ │          │ │            │  │
│  └──────────┘ └──────────┘ └──────────┘ └────────────┘  │
└──────────────────────────────────────────────────────────┘
```

---

## 3. 项目结构

```
agentprimordia/
├── cmd/
│   ├── ap/                       # CLI 工具 (ap init/run/debug/test/mcp/plugin)
│   │   ├── scaffold/             # 项目模板 (basic/with-tools/multi-agent 等)
│   │   ├── main.go               # CLI 入口
│   │   ├── run.go                # ap run 命令
│   │   ├── init.go               # ap init 命令
│   │   ├── debug.go              # ap debug 命令
│   │   ├── test.go               # ap test 命令
│   │   ├── mcp.go                # ap mcp 命令
│   │   └── plugin.go             # ap plugin 命令
│   ├── admin/                    # 管理后台
│   └── example/                  # 示例应用
│       ├── hello-agent/          # 最简 Agent
│       ├── multi-agent/          # 多 Agent 调度
│       ├── production/           # 生产级示例
│       └── demo/                 # Demo LLM
├── internal/
│   ├── agent/                    # ReActLoop 引擎 + 编排
│   │   ├── a2a/                  # Agent-to-Agent 协议 (JSON-RPC/SSE)
│   │   ├── react_loop.go         # ReAct 循环核心
│   │   ├── types.go              # Agent/Message/Response 等核心类型
│   │   ├── new_agent.go          # NewAgent 简化入口
│   │   ├── capability_agent.go   # CapabilityAgent 链式 API
│   │   ├── capabilities.go       # Capable 接口协议 (微内核)
│   │   ├── orchestration.go      # Pipeline / Handoff / Parallel
│   │   ├── dag.go                # DAG 工作流引擎
│   │   ├── dag_builder.go        # 声明式 DAG 构建器
│   │   ├── dag_delegate.go       # Agent 委派节点
│   │   ├── group_chat.go         # 多 Agent 群聊
│   │   ├── workflow.go           # 工作流执行引擎 (5 种模式)
│   │   ├── hooks.go              # Hook 系统 (20+ 钩子点)
│   │   ├── lifecycle.go          # 生命周期状态机
│   │   ├── session.go            # 会话管理
│   │   ├── context_window.go     # 上下文窗口裁剪
│   │   ├── prompt.go             # Prompt 模板
│   │   ├── transport.go          # 传输层接口
│   │   ├── http_transport.go     # HTTP 传输
│   │   ├── tcp_transport.go      # TCP 传输
│   │   ├── bus.go                # 消息总线
│   │   ├── tracer.go             # 分布式追踪
│   │   ├── cost_tracker.go       # 成本追踪
│   │   ├── hitl.go               # 人机协作 (Human-in-the-Loop)
│   │   ├── multimodal.go         # 多模态支持
│   │   ├── eval.go               # 评估框架
│   │   └── discovery.go          # Agent 发现
│   ├── pool/                     # 多 Agent 并发调度
│   │   ├── dispatcher.go         # Pool 调度器
│   │   ├── types.go              # PoolConfig/TaskConfig/TaskResult
│   │   └── events.go             # Pool 事件
│   ├── tools/                    # 工具系统
│   │   ├── types.go              # Tool 接口
│   │   ├── registry.go           # 工具注册中心
│   │   ├── executor.go           # 工具执行器
│   │   ├── scope.go              # 作用域权限策略
│   │   ├── plugin.go             # 插件系统
│   │   ├── mcp.go                # MCP 客户端
│   │   ├── mcp_registry.go       # MCP 注册中心
│   │   ├── mcp_transport.go      # MCP 传输层
│   │   └── builtin/              # 内置工具
│   │       ├── filesystem.go     # 文件系统操作
│   │       ├── shell.go          # Shell 命令执行
│   │       ├── web.go            # HTTP 请求
│   │       ├── knowledge.go      # 知识库搜索
│   │       └── toolkit.go        # 工具包快捷创建
│   ├── memory/                   # 记忆存储
│   │   ├── types.go              # Memory 接口 (组合接口)
│   │   ├── memory.go             # InMemoryStore 实现
│   │   ├── sqlite.go             # SQLite + FTS5 实现
│   │   ├── vector.go             # 向量存储 (余弦相似度)
│   │   ├── rag.go                # RAG 检索
│   │   ├── rag_pipeline.go       # RAG Pipeline
│   │   ├── rag_rerank.go         # RAG 重排序
│   │   ├── rag_generator.go      # RAG 端到端生成
│   │   ├── summarizer.go         # 摘要提取
│   │   ├── conversational_memory.go  # 对话记忆
│   │   ├── qdrant_provider.go    # Qdrant 向量库
│   │   ├── milvus_provider.go    # Milvus 向量库
│   │   └── episode.go            # 记忆片段
│   ├── llm/                      # LLM 抽象层
│   │   ├── types.go              # Provider 接口
│   │   ├── openai_provider.go    # OpenAI
│   │   ├── anthropic_provider.go # Anthropic (Claude)
│   │   ├── gemini_provider.go    # Google Gemini
│   │   ├── ollama_provider.go    # Ollama (本地)
│   │   ├── azure_provider.go     # Azure OpenAI
│   │   ├── qwen_provider.go      # 通义千问
│   │   ├── glm_provider.go       # 智谱 GLM
│   │   ├── mistral_provider.go   # Mistral AI
│   │   ├── cohere_provider.go    # Cohere
│   │   ├── resilient.go          # 弹性 Provider (重试/熔断/降级)
│   │   ├── cache.go              # LLM 缓存
│   │   ├── cache_enhanced.go     # 增强缓存 (语义匹配)
│   │   ├── cache_sqlite.go       # SQLite 持久化缓存
│   │   ├── structured.go         # 结构化输出
│   │   ├── multimodal_provider.go # 多模态 Provider
│   │   ├── pricing.go            # 定价表
│   │   ├── mock_llm.go           # MockLLM (测试用)
│   │   └── provider_template.go  # Provider 模板
│   ├── guardrail/                # 安全防护
│   │   ├── engine.go             # 守卫引擎
│   │   ├── injection_rule.go     # 注入防护
│   │   ├── output_rule.go        # 输出过滤
│   │   ├── pii_rule.go           # PII 检测
│   │   ├── topic_rule.go         # 主题限制
│   │   ├── trie_rule.go          # Trie 树规则
│   │   ├── sanitizer.go          # 内容清洗
│   │   └── hook.go               # Guardrail Hook 集成
│   ├── persist/                  # 状态持久化
│   │   ├── checkpoint.go         # CheckpointStore 接口
│   │   └── sqlite_checkpoint.go  # SQLite 实现
│   ├── metrics/                  # Prometheus 指标
│   │   ├── metrics.go            # 指标收集器
│   │   └── exporter.go           # 指标导出
│   ├── events/                   # 事件总线
│   │   └── bus.go                # 发布/订阅
│   ├── security/                 # ACL + Sandbox
│   │   └── sandbox.go            # 沙箱执行
│   ├── otel/                     # OpenTelemetry
│   │   ├── bridge.go             # OTel 桥接
│   │   ├── provider.go           # OTel Provider
│   │   └── otlp_exporter.go      # OTLP 导出
│   ├── concurrency/              # 并发工具
│   │   └── filelock.go           # 文件锁
│   ├── config/                   # 配置
│   │   └── hot_reload.go         # 热重载
│   ├── prompt/                   # Prompt 工程
│   │   ├── template.go           # 模板引擎
│   │   ├── few_shot.go           # Few-shot 示例
│   │   └── parser.go             # Prompt 解析
│   ├── debugger/                 # 调试器
│   │   ├── http.go               # HTTP 调试服务
│   │   └── visualizer.go         # 可视化
│   ├── orchestration/            # 编排 (独立包)
│   │   ├── orchestrator.go       # 编排器
│   │   ├── collaboration.go      # 协作模式
│   │   └── handoff.go            # 交接
│   ├── admin/                    # 管理 API
│   │   └── handler.go            # HTTP Handler
│   ├── governance/                # 多租户与治理 (v2.0)
│   │   ├── tenant.go              # 租户模型
│   │   ├── tenant_manager.go      # 租户管理器
│   │   ├── quota.go              # 配额管理 (令牌桶)
│   │   ├── isolation.go          # 数据隔离 (context 注入)
│   │   ├── resource_mgr.go       # 资源管理器
│   │   ├── policy.go             # 策略类型
│   │   ├── policy_enforcer.go    # 策略执行器
│   │   ├── policy_loader.go      # YAML 策略加载
│   │   └── policy_watcher.go     # 策略热监听
│   ├── audit/                    # 合规审计 (v2.0)
│   │   └── compliance.go         # 合规报告
│   ├── resilience/               # 熔断器 + 重试 + 降级 (v2.0)
│   ├── health/                   # /healthz /readyz /livez + pprof (v2.0)
│   ├── logger/                   # 结构化日志 + Shipper (v2.0)
│   ├── eval/                     # Agent 评估框架 (v2.0)
│   ├── chaos/                  # 混沌工程 (v3.0)
│   ├── edgeruntime/              # WASM Edge 运行时 (v2.0)
│   ├── mcp/                      # MCP Server 端实现 (v2.0)
│   ├── protocol/                 # 通用协议定义
│   ├── registry/                 # 服务注册中心
│   └── jsonutil/                 # JSON 序列化 buffer 池
├── operator/                     # K8s Operator (独立 go.mod)
│   ├── api/v1/                   # AgentDeployment CRD
│   ├── controller/               # Reconciler
│   ├── cmd/                      # Operator 入口
│   └── manifest/                 # CRD + 部署清单
├── pgvector/                     # pgvector 适配器
├── pkg/                          # 公共 API (类型别名 + re-export)
│   ├── agent.go                  # Agent 相关类型
│   ├── llm.go                    # LLM 相关类型
│   ├── tools.go                  # 工具相关类型
│   ├── memory.go                 # 记忆相关类型
│   ├── pool.go                   # Pool 相关类型
│   ├── errors.go                 # 错误定义
│   ├── options.go                # 选项模式
│   ├── pipeline.go               # Pipeline 类型
│   ├── adapters.go               # 适配器
│   ├── hooks.go                  # Hook 类型
│   ├── guardrail.go              # Guardrail 类型
│   ├── security.go               # 安全类型
│   ├── metrics.go                # 指标类型
│   ├── otel.go                   # OTel 类型
│   ├── persist.go                # 持久化类型
│   ├── lifecycle.go              # 生命周期类型
│   ├── events.go                 # 事件类型
│   ├── concurrency.go            # 并发类型
│   └── version.go                # 版本信息
├── sdk/typescript/               # TypeScript SDK
├── bench/                        # 性能基准测试
├── deploy/grafana/               # Grafana Dashboard 模板
├── ecosystem/                    # 生态系统
│   ├── examples/                 # 20+ 示例应用
│   ├── plugins/                  # 官方插件 (email/git/http/json/kv/sql)
│   ├── templates/                # 项目模板
│   ├── docs/                     # 文档 + Cookbook
│   └── contributing/             # 贡献指南
├── testutil/                     # 测试工具
├── Makefile                      # 构建脚本
├── Dockerfile                    # 容器化
└── docker-compose.yml            # 编排
```

---

## 4. 核心模块详解

### 4.1 agent — ReAct 引擎与编排

**位置：** `internal/agent/`

**核心职责：** Agent 推理引擎、多 Agent 编排、生命周期管理

#### 核心接口

```go
// Agent 是所有 Agent 实现的核心接口
type Agent interface {
    Run(ctx context.Context, input Message) (*Response, error)
    StreamRun(ctx context.Context, input Message) (<-chan StreamEvent, error)
    Stop()
    Stats() AgentStats
    Name() string
}
```

#### 核心类型

| 类型 | 文件 | 说明 |
|------|------|------|
| `ReActAgent` | `react_loop.go` | ReAct 循环引擎实现 |
| `ReActConfig` | `react_loop.go` | Agent 配置（模型、工具、记忆、钩子等） |
| `CapabilityAgent` | `capability_agent.go` | 链式 API 包装器，实现协议式微内核 |
| `Message` | `types.go` | 对话消息（角色、内容、工具调用） |
| `Response` | `types.go` | Agent 最终响应（内容、用量、指标） |
| `Thought` | `types.go` | LLM 推理输出（文本 + 工具调用） |
| `ToolCall` | `types.go` | 工具调用请求 |
| `StreamEvent` | `types.go` | 流式输出事件 |
| `AgentStatus` | `types.go` | Agent 状态枚举 |
| `AgentStats` | `types.go` | 运行时统计 |

#### 协议式微内核 (Capabilities)

```go
// 能力接口 — 引擎通过类型断言自动发现
type MemoryCapable interface { GetMemoryStore() MemoryStore }
type RAGCapable interface { GetRAGConfig() *RAGConfig }
type HITLCapable interface { GetHITLConfig() *HITLConfig }
type HookCapable interface { GetHooks() Hooks }
type TraceCapable interface { GetTracer() Tracer }
type CostCapable interface { GetCostTracker() *CostTracker }
type ContextWindowCapable interface { GetContextWindowStrategy() ContextWindowStrategy }
type EventCapable interface { GetEventPublisher() EventPublisher }
type MetricsCapable interface { GetMetricsRecorder() MetricsRecorder }
type CheckpointCapable interface { GetCheckpointStore() persist.CheckpointStore }
type SummarizerCapable interface { GetSummarizer() memory.SummaryExtractor }
type FileScopeCapable interface { GetFileScope() []string }
type CacheCapable interface { GetCache() llm.LLMCache }
```

#### ReAct 循环流程

```
用户输入 → 构建 SystemPrompt → 初始化 History
    ↓
┌─── runLoop ──────────────────────────────────────────┐
│  for turn := 0; turn < MaxTurns; turn++ {            │
│    1. 检查 Lifecycle / Context 取消                    │
│    2. 触发 HookBeforeTurn                              │
│    3. 检查预算 (CostTracker)                           │
│    4. RAG 检索 & 注入上下文 (如启用)                    │
│    5. 裁剪上下文窗口                                   │
│    6. 调用 LLM (Complete / CallTools)                  │
│    7. 保存 Assistant 消息到 Memory                     │
│    8. 若无 ToolCalls → 返回 Response (完成)            │
│    9. 遍历 ToolCalls:                                  │
│       a. HITL 检查 (如启用)                            │
│       b. 执行工具 (Executor)                           │
│       c. 保存 Tool 结果到 Memory                       │
│   10. 保存 Checkpoint                                  │
│  }                                                     │
└────────────────────────────────────────────────────────┘
```

#### 编排模式

| 模式 | 类型 | 说明 |
|------|------|------|
| **Pipeline** | `Pipeline` | 顺序执行，前一个输出作为后一个输入 |
| **Handoff** | `Handoff` | Agent 间动态交接，Router 函数路由 |
| **Parallel** | `ParallelRun()` | 并行执行，同一输入发给多个 Agent |
| **DAG** | `DAGWorkflow` | 有向无环图，支持条件边、重试、并行 |
| **GroupChat** | `GroupChat` | 多 Agent 对话，支持多种发言选择器 |
| **Workflow** | `WorkflowExecution` | 5 种工作流类型（线性/条件/循环/并行/状态机） |

#### 生命周期状态机

```
Idle → Running → [Paused | WaitingForInput | Completed | Failed | Cancelled]
Paused → Running | Cancelled
WaitingForInput → Running | Cancelled | Failed
Completed/Failed/Cancelled → Idle (Reset)
```

#### Hook 系统

20+ 钩子点，按阶段执行（Validation → PreProcessing → Execution → PostProcessing）：

| 类别 | 钩子点 |
|------|--------|
| 生命周期 | `before_run`, `after_run`, `before_shutdown`, `after_shutdown` |
| 执行 | `before_turn`, `after_turn`, `on_complete`, `on_error`, `on_state_change` |
| LLM | `before_llm`, `after_llm` |
| 工具 | `before_tool`, `after_tool`, `before_tool_parse`, `after_tool_parse` |
| 记忆 | `before_rag`, `after_rag`, `before_memory_read/write`, `after_memory_read/write` |
| 流式 | `on_stream`, `on_stream_start`, `on_stream_end` |
| 编排 | `before_pipeline_step`, `before_handoff`, `before_dag_node` 等 |

---

### 4.2 llm — LLM 抽象层

**位置：** `internal/llm/`

**核心职责：** 统一 LLM 调用接口，支持 12 家 Provider（DeepSeek 已合并到 OpenAI 兼容模式）

#### 核心接口

```go
type Provider interface {
    Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)
    Stream(ctx context.Context, req *CompletionRequest) (<-chan Chunk, error)
    CallTools(ctx context.Context, req *ToolCallRequest) (*ToolCallResponse, error)
    Info() ModelInfo
}

type Embedder interface {
    Embeddings(ctx context.Context, texts []string) ([][]float32, error)
}
```

#### Provider 实现

| Provider | 文件 | 说明 |
|----------|------|------|
| `OpenAIProvider` | `openai_provider.go` | GPT-4o 等 |
| `AnthropicProvider` | `anthropic_provider.go` | Claude 系列 |
| `GeminiProvider` | `gemini_provider.go` | Google Gemini |
| `OllamaProvider` | `ollama_provider.go` | 本地模型 |
| `AzureOpenAIProvider` | `azure_provider.go` | Azure OpenAI |
| `QwenProvider` | `qwen_provider.go` | 通义千问 (DashScope) |
| `GLMProvider` | `glm_provider.go` | 智谱 GLM |
| `MistralProvider` | `mistral_provider.go` | Mistral AI |
| `CohereProvider` | `cohere_provider.go` | Cohere v2 |
| `ResilientProvider` | `resilient.go` | 弹性包装器 |
| `CachedProvider` | `cache.go` | 缓存装饰器 |
| `MultimodalProvider` | `multimodal_provider.go` | 多模态 |

#### ResilientProvider

```go
type ResilientProvider struct {
    primary   Provider
    fallbacks []Provider
    config    ResilientConfig
    state     circuitState  // closed / open / halfOpen
    failures  int
}
```

功能：
- **重试** — 指数退避 + 随机抖动
- **熔断** — 失败次数超阈值后熔断，定时恢复
- **降级** — 主 Provider 失败后自动切换 Fallback

#### 结构化输出

```go
type StructuredExtractor interface {
    Extract(ctx context.Context, text string) (any, error)
}
```

支持从 Go struct 生成 JSON Schema，预定义模板：情感分析、NER、分类、摘要。

---

### 4.3 tools — 工具系统

**位置：** `internal/tools/`

**核心职责：** 工具注册、执行、权限控制、MCP 集成、插件扩展

#### 核心接口

```go
type Tool interface {
    Name() string
    Description() string
    Parameters() json.RawMessage
    Execute(ctx context.Context, args json.RawMessage) (*Result, error)
}
```

#### 组件

| 组件 | 文件 | 说明 |
|------|------|------|
| `Registry` | `registry.go` | 工具注册中心，管理注册/查找/权限 |
| `Executor` | `executor.go` | 工具执行器，超时/权限/文件锁 |
| `ScopePolicy` | `scope.go` | 作用域权限策略接口 |
| `FileScopePolicy` | `scope.go` | 基于文件路径的权限策略 |
| `MCPClient` | `mcp.go` | MCP 协议客户端 |
| `MCPRegistry` | `mcp_registry.go` | MCP Server 注册中心 |
| `ToolPlugin` | `plugin.go` | 插件接口 |
| `PluginLoader` | `plugin.go` | 插件加载器 |

#### 内置工具 (`builtin/`)

| 工具 | 文件 | 说明 |
|------|------|------|
| `FileSystem` | `filesystem.go` | 文件读写、搜索、编辑 |
| `Shell` | `shell.go` | 命令执行（白名单、超时） |
| `Web` | `web.go` | HTTP GET/POST |
| `API` | `api.go` | REST API 调用（白名单、超时） |
| `Database` | `database.go` | SQL 数据库查询 |
| `CodeExecution` | `code_execution.go` | 代码执行（沙箱） |
| `KnowledgeSearch` | `knowledge.go` | 知识库搜索 |
| `Toolkit` | `toolkit.go` | 工具包快捷创建 |

#### 工具执行流程

```
ToolCall → Executor.Execute()
    ↓
1. Registry.Get(name) 查找工具
2. ScopePolicy.Allow(agent, path) 权限检查
3. Permission.RequireConfirmation 确认检查
4. context.WithTimeout 设置超时
5. tool.Execute(ctx, args) 执行
6. 记录指标、返回 Result
```

---

### 4.4 memory — 记忆存储

**位置：** `internal/memory/`

**核心职责：** 对话记忆存储、全文搜索、向量检索、RAG Pipeline

#### 核心接口

```go
// Memory 组合接口
type Memory interface {
    MemoryReader    // Get/Search/List/Count/Stats
    MemoryWriter    // Add/Delete/UpdateSummary/SetImportance
    MemorySearcher  // SearchAdvanced/SearchByTag/GetImportant/GetTimeline
    MemoryLifecycle // Close/CleanupExpired/ClearAll
    MemoryExporter  // ExportMemories/ImportMemories
    MemoryQuery     // GetMemoriesByTag/Session/Important/Timeline
    MemoryToolUse   // RecordToolUse
}
```

#### 实现

| 实现 | 文件 | 说明 |
|------|------|------|
| `InMemoryStore` | `memory.go` | 内存存储（测试用） |
| `SQLiteStore` | `sqlite.go` | SQLite + FTS5 全文搜索 |
| `VectorStore` | `vector.go` | 内存向量存储（余弦相似度） |
| `RAGStore` | `rag.go` | 混合 RAG（FTS + Vector） |
| `QdrantProvider` | `qdrant_provider.go` | Qdrant 向量库 |
| `MilvusProvider` | `milvus_provider.go` | Milvus 向量库 |

#### RAG Pipeline

```
查询 → Embedding → Vector Search → FTS Search → RRF 融合 → Rerank → TopK → 上下文注入
```

#### RRF 融合模式

Reciprocal Rank Fusion（RRF）混合检索算法，支持运行时切换：

```go
// 默认 Linear 模式 — 基于原始分数加权融合
store := memory.NewRAGStore(mem, emb)

// 创建时指定 RRF 模式 — 基于排名融合，对量纲差异鲁棒
store = memory.NewRAGStoreWithFusionConfig(mem, emb, memory.RAGFusionConfig{
    FusionMode:    memory.FusionRRF,
    RRFK:          60,  // RRF 平滑常数，默认 60
    OverFetchSize: 5,   // 预取数量，增加融合召回率
})

// 运行时切换融合模式
store.SetFusionConfig(memory.RAGFusionConfig{
    FusionMode:    memory.FusionLinear,
    FTSWeight:     0.4,
    VectorWeight:  0.6,
})
```

| 组件 | 文件 | 说明 |
|------|------|------|
| `RAGStore` | `rag.go` | 混合检索 |
| `RAGPipeline` | `rag_pipeline.go` | 完整 Pipeline |
| `Reranker` | `rag_rerank.go` | 重排序 |
| `RetrievalAugmentedGenerator` | `rag_generator.go` | 端到端生成 |

#### Episode 结构

```go
type Episode struct {
    ID         string            // 唯一 ID
    SessionID  string            // 会话 ID
    Role       string            // user/assistant/system/tool
    Content    string            // 内容
    Summary    string            // 摘要
    Topics     string            // 主题标签
    Importance float64           // 重要性
    Metadata   map[string]string // 元数据
    CreatedAt  string            // 创建时间
}
```

---

### 4.5 pool — 多 Agent 调度

**位置：** `internal/pool/`

**核心职责：** 并发任务分发、信号量控制、会话隔离、重试策略

#### 核心类型

```go
type Pool struct {
    config       PoolConfig
    semaphore    chan struct{}      // 并发控制
    tasks        map[string]*poolTask
    agents       map[string]agent.Agent
    agentFactory AgentFactory
    // ...
}

type PoolConfig struct {
    MaxConcurrency   int
    Timeout          time.Duration
    RetryPolicy      RetryPolicy
    MaxRetainedTasks int
    DefaultAgent     ReActAgentConfig
}

type TaskConfig struct {
    ID         string
    Title      string
    Prompt     string
    SessionID  string
    FilesScope []string
    MaxTurns   int
    Metadata   map[string]string
}
```

#### 调度流程

```
Dispatch(tasks) → 为每个 task 启动 goroutine
    ↓
executeTask():
    1. semaphore <- struct{}{} (获取信号量)
    2. createAgentForTask() (工厂或默认)
    3. agent.Run(ctx, prompt)
    4. 失败时检查 RetryPolicy
    5. 释放信号量
    6. 更新 Stats
```

---

### 4.6 persist — 状态持久化

**位置：** `internal/persist/`

**核心职责：** Agent 状态检查点，支持断点恢复

```go
type CheckpointStore interface {
    Save(ctx context.Context, state *AgentState) error
    Load(ctx context.Context, agentID string) (*AgentState, error)
    List(ctx context.Context, sessionID string) ([]*AgentState, error)
    Delete(ctx context.Context, agentID string) error
}

type AgentState struct {
    AgentID   string
    SessionID string
    Status    string
    Messages  []CheckpointMessage
    TurnCount int
    Metrics   CheckpointMetrics
    SavedAt   time.Time
}
```

---

### 4.7 guardrail — 安全防护

**位置：** `internal/guardrail/`

**核心职责：** 输入/输出内容审核，安全防护规则引擎

```go
type Engine struct {
    rules []Rule
}

type Rule interface {
    Name() string
    Check(input string, point CheckPoint) (*Result, error)
}

// CheckPoint: input / output
// Action: pass / reject / sanitize / flag
// Severity: low / medium / high / critical
```

#### 内置规则

| 规则 | 文件 | 说明 |
|------|------|------|
| `InjectionRule` | `injection_rule.go` | Prompt 注入防护 |
| `OutputRule` | `output_rule.go` | 输出过滤 |
| `PIIRule` | `pii_rule.go` | PII 检测 |
| `TopicRule` | `topic_rule.go` | 主题限制 |
| `TrieRule` | `trie_rule.go` | Trie 树关键词 |
| `Sanitizer` | `sanitizer.go` | 内容清洗 |

---

### 4.8 metrics — 可观测性

**位置：** `internal/metrics/`

**核心职责：** Prometheus 指标收集与导出

```go
type MetricsRecorder interface {
    RecordLLMCall(duration time.Duration, err error)
    RecordToolCall(duration time.Duration, err error)
    RecordTurn(duration time.Duration)
    RecordTokenUsage(model string, promptTokens, completionTokens int)
    IncActiveAgents()
    DecActiveAgents()
}
```

---

### 4.9 events — 事件总线

**位置：** `internal/events/`

**核心职责：** 发布/订阅事件系统

```go
type EventPublisher interface {
    PublishAsync(eventType string, source string, payload any) error
}
```

---

### 4.10 security — ACL 与沙箱

**位置：** `internal/security/`

**核心职责：** 命令沙箱、ACL 访问控制

---

### 4.11 otel — OpenTelemetry 桥接

**位置：** `internal/otel/`

**核心职责：** 分布式追踪，OTLP 导出

```go
type Tracer interface {
    Start(name string, kind SpanKind, opts ...SpanOption) Span
}

type Span interface {
    SpanContext() SpanContext
    SetAttribute(key string, value any)
    SetStatus(code SpanStatus, msg string)
    End()
}
```

---

### 4.12 orchestration — 编排引擎

**位置：** `internal/orchestration/`

独立于 `agent/orchestration.go` 的编排引擎，提供更高级的编排能力：

```go
type OrchestratorMode string

const (
    SequentialMode OrchestratorMode = "sequential"  // 顺序执行
    ParallelMode   OrchestratorMode = "parallel"    // 并行执行
    DAGMode        OrchestratorMode = "dag"         // DAG 工作流
)

type OrchestratorConfig struct {
    Name        string
    Mode        OrchestratorMode
    MaxRetries  int           // 默认 3
    Timeout     time.Duration // 默认 5 分钟
}

type AgentStep struct {
    ID          string
    Name        string
    Agent       agent.Agent
    Prompt      string
    InputFrom   []string      // 输入来源（其他步骤 ID）
    OutputKey   string
    Condition   StepCondition
    RetryPolicy *RetryPolicy
    Timeout     time.Duration
    Priority    int
}
```

#### 组件

| 组件 | 文件 | 说明 |
|------|------|------|
| `Orchestrator` | `orchestrator.go` | 编排器主逻辑 |
| `Workflow` | `workflow.go` | 工作流定义 |
| `Handoff` | `handoff.go` | Agent 交接 |
| `Collaboration` | `collaboration.go` | 协作模式 |
| `Visualizer` | `visualizer.go` | 编排可视化 |

---

### 4.13 concurrency — 并发原语

**位置：** `internal/concurrency/`

#### FileLockManager

基于路径的文件锁，支持引用计数：

```go
flm := concurrency.NewFileLockManager()

// 获取锁
flm.Acquire("/path/to/file")

// 尝试获取（非阻塞）
if flm.TryAcquire("/path/to/file") {
    defer flm.Release("/path/to/file")
    // 操作文件
}

// 释放
flm.Release("/path/to/file")
```

#### Scope 验证

```go
// 验证作用域不重叠
err := concurrency.ValidateScopes([]string{"/path1", "/path2"})

// 错误类型
ErrGlobalWriteConflict  // 全局写入冲突
ErrScopeOverlap         // 作用域重叠
```

---

### 4.14 config — 配置管理

**位置：** `internal/config/`

#### 热重载

```go
// 监听配置文件变更
watcher := config.NewWatcher("config.yaml")
watcher.OnChange(func(cfg *config.Config) {
    // 配置变更回调
    slog.Info("配置已更新", "version", cfg.Version)
})

// 启动监听
go watcher.Watch()
defer watcher.Close()
```

---

### 4.15 prompt — Prompt 工程

**位置：** `internal/agent/prompt/`

#### 模板引擎

```go
tmpl := prompt.NewTemplate("你是一个{{.Role}}，请{{.Task}}")
result := tmpl.Execute(map[string]any{
    "Role": "编程助手",
    "Task": "解释这段代码",
})
```

#### Few-shot 示例

```go
fewshot := prompt.NewFewShotBuilder().
    AddExample("输入: 你好\n输出: 你好！有什么可以帮助的吗？").
    AddExample("输入: 再见\n输出: 再见！祝你有美好的一天！").
    Build()

prompt := fewshot.BuildPrompt("输入: 谢谢")
```

---

### 4.16 debugger — 调试器

**位置：** `internal/debugger/`

#### HTTP 调试服务

```bash
# 启动调试服务器
ap debug

# ReAct Loop 工程化工具
ap loop trace                 # 查看 Agent 执行追踪
ap loop inspect               # 查看 Agent 当前状态
ap loop resume                # 从检查点恢复运行

# 访问端点
http://localhost:6060/debug/pprof/     # pprof 性能分析
http://localhost:6060/debug/agent/     # Agent 状态
http://localhost:6060/debug/tools/     # 工具注册表
http://localhost:6060/debug/memory/    # 记忆存储
```

#### 可视化

```go
viz := debugger.NewVisualizer(agent)
viz.RenderHTML()  // 生成 HTML 可视化报告
```

---

### 4.17 admin — 管理 API

**位置：** `internal/admin/`

提供 HTTP API 用于 Agent 管理：

```go
admin := admin.NewServer(admin.Config{
    Port: 8080,
})

// 启动
go admin.Start()
```

**API 端点：**

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/agents` | GET | 列出所有 Agent |
| `/api/agents/:id` | GET | 获取 Agent 详情 |
| `/api/agents/:id/stop` | POST | 停止 Agent |
| `/api/metrics` | GET | 获取指标 |
| `/api/health` | GET | 健康检查 |

---

### 4.18 governance — 多租户与治理

**位置：** `internal/governance/`

**核心职责：** 多租户管理、配额限流、数据隔离、策略执行

#### 租户模型

```go
type Tenant struct {
    ID        string
    Name      string
    Plan      TenantPlan      // free / pro / enterprise
    Quotas    TenantQuota
    CreatedAt time.Time
    Status    TenantStatus    // active / disabled / archived
    Metadata  map[string]string
}

type TenantQuota struct {
    MaxAgents       int
    MaxSessions     int
    MaxTokensPerDay int64
    MaxStorageGB    int64
    MaxQPS          int
}
```

#### 核心组件

| 组件 | 文件 | 说明 |
|------|------|------|
| `TenantManager` | `tenant_manager.go` | 租户 CRUD、API Key 绑定与验证 |
| `QuotaManager` | `quota.go` | 令牌桶限流、Token 用量追踪 |
| `ResourceManager` | `resource_mgr.go` | 多租户资源配额统一管理 |
| `PolicyEnforcer` | `policy_enforcer.go` | YAML 策略加载与运行时执行 |
| `PolicyWatcher` | `policy_watcher.go` | 策略文件热监听 |
| `TenantContext` | `isolation.go` | context 注入租户 ID，实现数据隔离 |

#### 数据隔离

```go
// 在请求入口注入租户
ctx := governance.WithTenant(context.Background(), "t_abc123")

// 后续读取
tenantID, err := governance.RequireTenant(ctx)

// 带租户作用域的查询
query := governance.NewScopedQuery("t_abc123")
query.Set("limit", 10)
```

#### 配额检查

```go
mgr := governance.NewTenantManager()
tenant, _, _ := mgr.CreateTenant(ctx, "MyCorp", governance.PlanPro, governance.TenantQuota{}, true)

rm := governance.NewResourceManager()
rm.Register(tenant.ID, tenant.Quotas)

// QPS 检查
err := rm.CheckRequest(ctx, tenant.ID, 100) // 100 tokens
```

---

### 4.19 security — 密钥管理与加密

**位置：** `internal/security/`

**核心职责：** 密钥管理、AES-GCM 加密、多后端密钥存储

#### SecretsManager 接口

```go
type SecretsManager interface {
    GetSecret(ctx context.Context, key string) (string, error)
    SetSecret(ctx context.Context, key, value string) error
    RotateSecret(ctx context.Context, key string) error
    ListSecrets(ctx context.Context) ([]string, error)
    DeleteSecret(ctx context.Context, key string) error
}
```

#### 后端实现

| 后端 | 文件 | 说明 |
|------|------|------|
| `MemoryBackend` | `memory_backend.go` | 内存存储（测试用），含审计日志 |
| `EnvBackend` | `env_backend.go` | 环境变量后端（兼容现有用法） |
| `VaultBackend` | `vault_backend.go` | HashiCorp Vault 后端（预留） |
| `CachedSecretsManager` | `secrets.go` | 带缓存的装饰器（TTL 过期） |
| `AuditLog` | `secrets.go` | 密钥操作审计记录 |

#### AES-GCM 加密

```go
// AES-GCM 对称加密
encryptor, _ := security.NewAESGCMEncryption(key32Bytes)
ciphertext, _ := encryptor.Encrypt(plaintext)
decrypted, _ := encryptor.Decrypt(ciphertext)
```

---

### 4.20 health — SLO/SLI 与增强诊断

**位置：** `internal/health/`

**核心职责：** SLO/SLI 指标、增强 pprof、性能分析配置

#### SLO/SLI

```go
sli := health.NewSLIRegistry()
sli.Record("llm_latency", 150*time.Millisecond)

slo := health.NewSLOConfig()
slo.SetTarget("llm_latency_p99", 500*time.Millisecond)
report := slo.Check(sli)
```

#### 增强 pprof

| 组件 | 文件 | 说明 |
|------|------|------|
| `pprof_enhanced.go` | 增强 pprof 端点 | heap/goroutine/cpu/block/mutex 全 profile |
| `profiling_config.go` | 性能分析配置 | 持续采样、自动 dump |

---

### 4.21 logger — 结构化日志与 Shipper

**位置：** `internal/logger/`

**核心职责：** 结构化日志、日志聚合与远程传输

| 组件 | 文件 | 说明 |
|------|------|------|
| `StandardLogger` | `standard.go` | 基于 `log/slog` 的结构化日志器 |
| `LogShipper` | `shipper.go` | 日志远程传输（HTTP/gRPC batch） |

---

### 4.22 eval — 评估框架

**位置：** `internal/eval/`

**核心职责：** Agent 质量评估、自动化测试套件

评估维度包括：准确性、工具调用正确性、响应延迟、Token 效率。

---

### 4.23 orchestration — MapReduce

**位置：** `internal/orchestration/mapreduce.go`

**核心职责：** 大规模任务的 MapReduce 编排模式

```go
mr := orchestration.NewMapReduce(mapper, reducer)
result, err := mr.Run(ctx, inputs)
```

---

### 4.24 transport — gRPC 与连接池

**位置：** `internal/agent/transport/`

| 组件 | 文件 | 说明 |
|------|------|------|
| `gRPCTransport` | `grpc.go` | gRPC 传输层（Agent-to-Agent） |
| `ConnPool` | `conn_pool.go` | 连接池管理（复用 gRPC/HTTP 连接） |

---

### 4.25 tokencache — 语义缓存

**位置：** `internal/agent/tokencache/`

| 组件 | 文件 | 说明 |
|------|------|------|
| `SemanticCache` | `semantic_cache.go` | 基于语义相似度的 LLM 响应缓存 |
| `MultiLevelCache` | `multilevel.go` | L1 内存 + L2 持久化多级缓存 |

---

### 4.26 memory — 分层记忆与生命周期

**位置：** `internal/memory/`

v2.0 新增组件：

| 组件 | 文件 | 说明 |
|------|------|------|
| `ImportanceScorer` | `importance.go` | 记忆重要性评分（衰减、访问频率） |
| `MemoryLifecycle` | `lifecycle.go` | 记忆自动归档/删除/压缩 |
| `Clusterer` | `clusterer.go` | 记忆聚类（相似记忆合并） |
| `SimpleVectorStore` | `vector_store.go` | HNSW 向量存储（替代旧 VectorStore） |

---

### 4.27 tools — 动态注册与插件市场

**位置：** `internal/tools/`

v2.0 新增组件：

| 组件 | 文件 | 说明 |
|------|------|------|
| `DynamicRegistry` | `dynamic_registry.go` | 运行时动态工具注册/注销 |
| `PluginMarket` | `plugin_market.go` | 插件市场元数据管理 |
| `PluginVersion` | `plugin_version.go` | 插件版本管理（SemVer） |
| `PluginInstaller` | `plugin_installer.go` | 插件安装/卸载 |
| `ResourceLimiter` | `resource_limiter.go` | 工具资源限制（内存/CPU/FD） |
| `ToolTrace` | `trace.go` | 工具调用追踪与审计 |
| `MCPServer` | `mcp/server.go` | MCP Server 端实现 |

---

### 4.28 debugger — 断点与时间旅行

**位置：** `internal/debugger/`

v2.0 新增组件：

| 组件 | 文件 | 说明 |
|------|------|------|
| `Breakpoint` | `breakpoint.go` | 条件断点（工具名/轮次/状态） |
| `TimeTravel` | `time_travel.go` | 状态回放（从检查点恢复历史状态） |
| `Watch` | `watch.go` | 变量监视（内存/上下文/工具结果） |

---

### 4.29 audit — 合规报告

**位置：** `internal/audit/`

**核心职责：** 合规审计报告生成

```go
report := audit.NewComplianceReport()
report.AddFinding("PII", "检测到邮箱地址未脱敏", audit.SeverityMedium)
report.Generate() // 输出 JSON/CSV 合规报告
```

---

### 4.30 wasm — 增强沙箱

**位置：** `wasm/sandbox_enhanced.go`

**核心职责：** WASM 模块安全执行（资源限制 + 文件系统隔离）

```go
sandbox, _ := wasm.NewEnhancedSandbox(wasm.Config{
    MaxMemory:   64 * 1024 * 1024, // 64MB
    MaxCPU:      5 * time.Second,
    FSPolicy:    wasm.FSWhitelist{"/tmp/wasm"},
    NetPolicy:   wasm.NetDenyAll,
})
result, err := sandbox.Execute(ctx, wasmModule, "func_name", args)
```

---

### 4.31 chaos — 混沌工程

**位置：** `internal/chaos/`

**核心职责：** 混沌实验编排、故障注入、稳态验证、报告生成

#### 核心组件

| 组件 | 文件 | 说明 |
|------|------|------|
| `ChaosEngine` | `engine.go` | 实验编排器（定义→执行→验证→报告） |
| `SteadyStateVerifier` | `verifier.go` | 稳态假设验证（SLO 指标比对） |
| `ReportGenerator` | `report.go` | Markdown 实验报告生成 |
| `FaultAgent` | `fault_agent.go` | LLM 驱动的智能故障代理 |
| `RealInjector` | `real_injector_linux.go` | iptables/tc 真实网络故障注入（Linux） |

#### 实验流程

```
定义实验 (Hypothesis + Fault + SteadyState)
    ↓
ChaosEngine.Run()
    ↓
1. 记录基线指标
2. 注入故障 (网络延迟/丢包/分区 / Pod Kill / 资源耗尽)
3. 观察系统行为
4. 稳态验证 (SLO 是否违反)
5. 生成 Markdown 报告
```

---

### 4.32 autonomy — 长期自治

**位置：** `internal/agent/autonomy/`

**核心职责：** 目标驱动的自主规划/执行/校验/重规划循环，支持长时间运行与崩溃恢复（V4 v3.3）。

#### 核心组件

| 组件 | 文件 | 说明 |
|------|------|------|
| `AgentGoal` | `goal.go` | 持久化目标：验收标准 + 优先级 + 状态机 + 重试 |
| `GoalPlan` / `PlanStep` | `plan.go` | 依赖 DAG + 顺序/并行/条件策略 + 循环检测 |
| `GoalExecutor` | `executor.go` | 逐步执行 + 重试 + 重规划 + 并行步骤 |
| `Replanner` / `Validator` | `replan.go` | 校验不达标自动重规划 + 结果校验 |
| `Scheduler` | `scheduler.go` | cron 定时唤醒 + 事件驱动调度 |
| `Monitor` | `monitor.go` | 停滞检测 + 进度追踪 + 异常分级上报 |
| `GoalMemory` | `memory.go` | 跨会话记忆 + 失败教训反馈 |
| `ResumeManager` | `resume.go` | 检查点写入 + 崩溃恢复 + 一致性校验 |
| `IdempotencyGuard` | `idempotency.go` | 幂等键保护（goalID→keys 精确重置） |
| `AutonomyRuntime` | `runtime.go` | 端到端装配 |

**目标状态机：** `created → planned → executing → validated → done`；非终态可转 `failed`；`validated` 可回 `executing` 重规划；`failed` 可回 `planned` 重试。能力注入 `WithAutonomy(...)`，引擎经 `AutonomyCapable` 发现。详见 [concepts/autonomy](agentprimordia/docs/concepts/自治.md)。

---

### 4.33 skills — 技能进化

**位置：** `internal/agent/skills/`

**核心职责：** 让 Agent 运行中习得/验证/沉淀/复用可执行技能，从"静态工具"跃迁为"越用越强"（V4 v3.4）。

#### 核心组件

| 组件 | 文件 | 说明 |
|------|------|------|
| `Skill` / `StepDef` | `skill.go` | 多步骤能力单元 + SemVer + 状态 + 使用统计 |
| `Validator` | `validator.go` | schema 校验 + 循环依赖检测 + 安全扫描 |
| `Codec` | `codec.go` | JSON 编解码（可发布/可存储） |
| `Composition` | `composition.go` | 多技能组合为工作流 |
| `Acquisition` | `acquisition.go` | 轨迹采集 + LLM 提炼流水线 |
| `Verification` | `verification.go` | 验证门：测试用例通过才可启用 |
| `Deduplicator` | `dedup.go` | 相似技能去重/合并 |
| `Trigger` | `trigger.go` | 习得触发策略（重复模式/低成功率/显式） |
| `Store` / `Matcher` | `store.go` / `matcher.go` | 技能库存取 + 语义匹配（置信度三档） |
| `UsageTracker` | `usage.go` | 命中率/成功率/成本统计 |

**跨组件集成：** 技能 × 工具/学习/市场/自治/RAG（`integration.go`）。能力注入 `WithSkills(...)`，引擎经 `SkillsCapable` 发现。详见 [concepts/skills](agentprimordia/docs/concepts/技能系统.md)。

---

### 4.34 realtime — 多模态实时

**位置：** `internal/agent/realtime/`

**核心职责：** 语音/视觉/图像实时双向流交互，从"文本流式对话"跃迁为"多感官实时"（V4 v3.6）。

#### 核心组件

| 组件 | 文件 | 说明 |
|------|------|------|
| `RealtimeSession` | `session.go` | 会话状态机 idle→listening→thinking→speaking |
| `RealtimeHub` | `hub.go` | 双向流编排 + ASR/TTS 可插拔 |
| `ASRAdapter` / `TTSAdapter` | `hub.go` | 语音识别/合成接口（含 Mock 实现） |
| `AudioStream` | `audio_stream.go` | chunk 缓冲 + 静音检测 + 格式协商 |
| `VisionStream` | `vision_stream.go` | 连续视频帧输入 |
| `Fusion` | `fusion.go` | 文本+视觉+音频多路感知融合 |
| `BargeInHandler` | `barge_in.go` | 打断支持（仅 speaking 可打断） |
| `EventBus` | `events.go` | 会话事件发布 |
| `CleanupManager` | `cleanup.go` | 空闲超时关闭 + 资源回收 |
| `Runtime` | `runtime.go` | Hub+感知流+事件+清理+ReAct 桥接装配 |

**跨组件集成：** 实时 × 多模态/边缘/自治/守卫/A2A（`integration.go`）。能力注入 `WithRealtime(...)`，引擎经 `RealtimeCapable` 发现。详见 [concepts/realtime](agentprimordia/docs/concepts/实时通信.md)。

---

## 5. 协议式微内核架构

**位置：** `internal/agent/capabilities.go`

AgentPrimordia 采用协议式微内核设计，通过 20 个 Capable 接口实现能力的自动发现与组合（v3.3–v3.6 新增 `AutonomyCapable` / `SkillsCapable` / `RealtimeCapable`）：

```go
// 能力接口 — 引擎通过类型断言自动发现
type MemoryCapable interface { GetMemoryStore() MemoryStore }
type RAGCapable interface { GetRAGConfig() *RAGConfig }
type HITLCapable interface { GetHITLConfig() *HITLConfig }
type HookCapable interface { GetHooks() Hooks }
type TraceCapable interface { GetTracer() Tracer }
type CostCapable interface { GetCostTracker() *CostTracker }
type ContextWindowCapable interface { GetContextWindowStrategy() ContextWindowStrategy }
type EventCapable interface { GetEventPublisher() EventPublisher }
type MetricsCapable interface { GetMetricsRecorder() MetricsRecorder }
type CheckpointCapable interface { GetCheckpointStore() persist.CheckpointStore }
type SummarizerCapable interface { GetSummarizer() memory.SummaryExtractor }
type FileScopeCapable interface { GetFileScope() []string }
type CacheCapable interface { GetCache() llm.LLMCache }
type ToolkitCapable interface { GetToolkit() *tools.Registry }
type PlanningCapable interface { GetPlanner() planning.Planner }
type ReflectionCapable interface { GetReflectionConfig() *reflection.Config }
type ToolLearningCapable interface { GetToolLearningConfig() *toollearning.Config }
```

### CapabilityAgent 包装器

`CapabilityAgent` 实现所有 Capable 接口，提供链式 API：

```go
agent, _ := ap.NewAgent("my-agent", "你是一个助手", provider,
    ap.WithMaxTurns(10),
)
agent.
    WithMemory(mem).
    WithRAG(ragCfg).
    WithHooks(hooks).
    WithToolkit(registry)
```

### 自引用模式

```go
// ReActAgent.self 默认指向自身
a.initSelf()  // a.self = a

// WithXxx 时更新为 CapabilityAgent
a.self = capAgent  // 引擎通过 a.self.(XxxCapable) 发现能力
```

---

## 6. 插件生态

**位置：** `ecosystem/plugins/`

### 官方插件

| 插件 | 版本 | 分类 | 工具 | 说明 |
|------|------|------|------|------|
| http | 0.1.0 | network | `http_client` | HTTP 客户端封装 |
| sql | 0.1.0 | database | `sqlite_processor` | SQLite 数据处理 |
| git | 0.1.0 | vcs | `git_tool` | Git 版本控制 |
| json | 0.1.0 | data | `json_processor` + `csv_processor` | JSON/CSV 处理 |
| email | 0.1.0 | communication | `email_sender` | 邮件发送（net/smtp） |
| kv | 0.1.0 | database | `kv_store` | 键值存储（SQLite 后端） |

### 插件使用

```go
registry := ap.NewToolRegistry()
loader := ap.NewPluginLoader(registry)

// 无配置插件
loader.Load(jsonplugin.New())

// 带配置插件
loader.LoadWithConfig(kvplugin.New(), map[string]any{"db_path": "test.db"})
loader.LoadWithConfig(emailplugin.New(), map[string]any{
    "smtp_host": "smtp.example.com",
    "smtp_port": "587",
    "smtp_username": "user@example.com",
    "smtp_password": "pass",
})

// 一键加载全部官方插件
plugins.LoadAll(registry, configs)
```

---

## 7. 公共 API 层 (pkg/)

**位置：** `pkg/`

**核心职责：** 通过类型别名和 re-export 暴露稳定的公共 API

```go
// pkg/agent.go
type Agent = agent.Agent
type ReActAgent = agent.ReActAgent
type CapabilityAgent = agent.CapabilityAgent
var NewAgent = agent.NewAgent
// pkg/llm.go
type Provider = llm.Provider
type OpenAIProvider = llm.OpenAIProvider
var NewOpenAIProvider = llm.NewOpenAIProvider

// pkg/tools.go
type Tool = tools.Tool
type ToolRegistry = tools.Registry
var DefaultToolkit = builtin.DefaultToolkit

// pkg/memory.go
type Memory = memory.Memory
var WithInMemory = memory.WithInMemory

// pkg/pool.go
type Pool = pool.Pool
var NewPool = pool.NewPool

// pkg/autonomy.go (v3.3 长期自治)
type AutonomyRuntime = autonomy.AutonomyRuntime
type AgentGoal = autonomy.AgentGoal
var NewAutonomyRuntime = autonomy.NewAutonomyRuntime

// pkg/skills.go (v3.4 技能进化)
type Skill = skills.Skill
type SkillStore = skills.Store
type SkillMatcher = skills.Matcher

// pkg/realtime.go (v3.6 多模态实时)
type RealtimeHub = realtime.RealtimeHub
type RealtimeSession = realtime.Session

// pkg/a2a_interop.go (v3.5 协议互操作)
type OpenAgentCard = a2a.OpenAgentCard
type OpenInteropServer = a2a.OpenInteropServer
type OpenInteropClient = a2a.OpenInteropClient
```

**API 稳定性等级：**

| 等级 | 说明 |
|------|------|
| Stable | 向后兼容，破坏性变更需大版本 |
| Experimental | 签名可能在 minor 版本调整 |
| Deprecated | 已废弃，v2.0 移除 |
| Internal | 仅供内部使用 |

---

## 8. 依赖关系图

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

        横切关注点:
        ├── guardrail/  (被 agent/hooks 集成)
        ├── metrics/    (被 agent 记录)
        ├── events/     (被 agent 发布)
        ├── otel/       (被 agent/tracer 使用)
        ├── security/   (被 tools/executor 使用)
        └── concurrency/ (被 tools/executor 使用)
```

**规则：**
- `agent/` 处于顶层，可引用 llm/memory/persist/tools
- 下层模块（llm/memory/persist/tools）不能反向引用 `agent/`
- `pool/` 依赖 `agent/` 和 `tools/`
- `pkg/` 只做类型导出，不含业务逻辑

---

## 9. 关键接口总览

### 核心接口

| 接口 | 模块 | 方法 | 说明 |
|------|------|------|------|
| `Agent` | agent | Run/StreamRun/Stop/Stats/Name | Agent 核心接口 |
| `llm.Provider` | llm | Complete/Stream/CallTools/Info | LLM 提供者 |
| `llm.Embedder` | llm | Embeddings | 嵌入接口 |
| `Tool` | tools | Name/Description/Parameters/Execute | 工具接口 |
| `ToolPlugin` | tools | Name/Version/Tools/Init/Close | 插件接口 |
| `Memory` | memory | 7 个子接口组合 | 记忆组合接口 |
| `MemoryStore` | agent | Add | Agent 所需记忆接口 |
| `RAGProvider` | agent | Search | RAG 检索接口 |
| `CheckpointStore` | persist | Save/Load/List/Delete | 检查点存储 |

### 横切关注点接口

| 接口 | 模块 | 方法 | 说明 |
|------|------|------|------|
| `EventPublisher` | agent | PublishAsync | 事件发布 |
| `MetricsRecorder` | agent | RecordLLMCall/RecordToolCall/... | 指标记录 |
| `MessageBus` | agent | Send/Broadcast/Subscribe | 消息总线 |
| `Transport` | agent | Send/Receive/Start/Stop | 传输层 |
| `ContextWindowStrategy` | agent | Trim | 上下文窗口裁剪 |
| `llm.LLMCache` | llm | Get/Set/Stats/Clear | LLM 缓存 |
| `Hooks` | agent | Register/Fire | Hook 管理器 |
| `Lifecycle` | agent | SetStatus/IsGracefulShutdown | 生命周期 |
| `Tracer` | agent | Start/Span | 追踪器 |

### 20 个 Capable 接口（协议式微内核）

详见 [第 5 章 协议式微内核架构](#5-协议式微内核架构)

---

## 10. 错误码体系详解

**位置：** `pkg/errors.go`

### 错误码分类

| 模块 | 错误码前缀 | 数量 | 说明 |
|------|-----------|------|------|
| Agent | `AGENT_` | 4 | Agent 生命周期错误 |
| Tool | `TOOL_` | 4 | 工具相关错误 |
| LLM | `LLM_` | 8 | LLM 调用错误 |
| Pool | `POOL_` | 3 | Pool 调度错误 |
| Context | `CTX_` | 1 | 上下文错误 |
| Memory | `MEM_` | 8 | 记忆相关错误 |
| Security | `SEC_` | 4 | 安全相关错误 |
| Event | `EVT_` | 1 | 事件总线错误 |
| Persist | `PST_` | 1 | 持久化错误 |
| Concurrency | `CON_` | 2 | 并发冲突错误 |

**总计：36 个结构化错误码**

### 使用示例

```go
resp, err := agent.Run(ctx, msg)
if err != nil {
    code := ap.GetErrorCode(err)
    switch {
    case strings.HasPrefix(code, "LLM_"):
        // LLM 调用失败，可重试
    case strings.HasPrefix(code, "TOOL_"):
        // 工具执行失败，检查参数
    case code == "AGENT_001":
        // Agent 已停止
    }
}
```

---

## 11. 快速上手

### 5 分钟创建第一个 Agent（v0.8.0 简化入口）

```go
package main

import (
    "context"
    "log"
    "os"
    
    ap "github.com/Gleamseekers/AgentPrimordia/pkg"
)

func main() {
    // 1. 创建 LLM Provider
    provider := ap.NewOpenAIProvider(ap.Config{
        APIKey: os.Getenv("OPENAI_API_KEY"),
        Model:  "gpt-4o-mini",
    })
    
    // 2. 创建 Agent（3 行即可，链式添加能力）
    agent := ap.NewAgent("my-first-agent", "你是一个友好的助手", provider,
        ap.WithMaxTurns(10),
    )
    
    // 3. 运行
    resp, err := agent.Run(context.Background(), ap.UserMessage("你好！"))
    if err != nil {
        log.Fatal(err)
    }
    
    log.Printf("Agent 回复: %s", resp.Content)
}
```

### 链式 API（CapabilityAgent）

```go
agent, _ := ap.NewAgent("my-first-agent", "你是一个友好的助手", provider,
    ap.WithMaxTurns(10),
)
```

### 渐进式添加能力

```go
agent, _ := ap.NewAgent("capable-agent", "你是一个助手", provider,
    ap.WithMaxTurns(10),
)
agent.
    WithMemory(mem).              // 添加记忆
    WithRAG(ragCfg).              // 添加 RAG
    WithToolkit(registry).        // 添加工具
    WithHooks(hooks).             // 添加钩子
    WithEvents(eventBus).         // 添加事件
    WithMetrics(metricsRecorder)  // 添加指标
```

---

## 12. 构建与运行

### 安装 CLI

```bash
cd agentprimordia
go build -o ap ./cmd/ap/
```

### 创建项目

```bash
ap init my-agent              # 创建项目
ap init my-agent --template with-tools  # 带工具模板
ap init my-agent --template multi-agent # 多 Agent 模板
```

### 运行

```bash
ap run                        # 编译运行
ap run --watch                # 监视模式
ap debug                      # 调试服务器 (http://localhost:6060)
ap loop trace                 # 查看 Agent 执行追踪
ap loop inspect               # 查看 Agent 当前状态
ap loop resume                # 从检查点恢复运行
ap doctor                     # 健康检查
ap completion bash            # 生成 Shell 补全脚本 (bash/zsh/fish/powershell)
```

### MCP 管理

```bash
ap mcp list
ap mcp add fs --command npx --args "@mcp/server-filesystem,/tmp"
ap mcp test fs
```

### 插件管理

```bash
ap plugin install github.com/user/ap-plugin-xxx
ap plugin create ap-plugin-weather
```

### K8s 部署

```bash
kubectl apply -f operator/manifest/crd.yaml
kubectl apply -f operator/manifest/examples/basic-agent.yaml
kubectl get ad        # 查看 AgentDeployment
kubectl get hpa       # 查看 HPA 状态
kubectl get svc       # 查看 Service
```

---

## 13. Docker 部署

### 构建镜像

```bash
# 使用 Makefile
make docker-build

# 或手动构建
docker build -t agentprimordia/agent:latest .
```

### Docker Compose

```yaml
version: '3.8'

services:
  agent:
    image: agentprimordia/agent:latest
    environment:
      - AP_LLM_API_KEY=${OPENAI_API_KEY}
      - AP_LLM_MODEL=gpt-4o-mini
    ports:
      - "8080:8080"   # Admin API
      - "9090:9090"   # Metrics
    volumes:
      - ./data:/app/data
    restart: unless-stopped
```

### 启动

```bash
# 使用 Docker Compose
docker-compose up -d

# 查看日志
docker-compose logs -f agent

# 停止
docker-compose down
```

---

## 14. 测试体系

### 运行测试

```bash
# 核心测试
go test ./internal/... ./pkg/... -race

# CLI 测试
go test ./cmd/ap/

# 集成测试（需要 API Key）
make test-integration

# 基准测试
go test -bench=. -benchmem ./bench/suite/

# Lint
golangci-lint run
```

### 测试约定

| 约定 | 说明 |
|------|------|
| `t.TempDir()` | 临时文件，不污染项目 |
| `httptest.Server` | Web/Shell 工具测试用模拟 |
| `WithInMemory()` | Memory 测试用内存数据库 |
| `MockLLM` | Agent/Pool 层测试用 |
| `DemoLLM` | 示例应用用 |

---

## 15. 性能基准测试

**位置：** `bench/suite/`

### 运行基准测试

```bash
# 运行所有基准测试
go test -bench=. -benchmem ./bench/suite/

# 仅运行延迟测试
go test -bench=BenchmarkLatency -benchmem ./bench/suite/

# 仅运行并发测试
go test -bench=BenchmarkConcurrent -benchmem ./bench/suite/

# 生成 CPU 性能分析
go test -bench=. -cpuprofile=cpu.prof ./bench/suite/
go tool pprof cpu.prof
```

### 基准测试套件

| 测试 | 说明 |
|------|------|
| `BenchmarkLatency` | 单 Agent 延迟测试 |
| `BenchmarkConcurrent` | 并发 Agent 吞吐量测试 |
| `BenchmarkToolCall` | 工具调用性能 |
| `BenchmarkMemorySearch` | 记忆搜索性能 |

### 测试结果示例

```
BenchmarkLatency-8           100    12.5 ms/op    1024 B/op    15 allocs/op
BenchmarkConcurrent-8         50    25.3 ms/op    2048 B/op    30 allocs/op
BenchmarkToolCall-8          200     6.2 ms/op     512 B/op     8 allocs/op
```

### 性能优化（v0.8.0 perf-v11）

| 优化项 | 位置 | 说明 |
|--------|------|------|
| **BufferPool** | `internal/agent/bufferpool.go` | `sync.Pool` 复用 `bytes.Buffer`，减少 LLM 请求体构造和 SSE chunk 解析热路径上的内存分配 |
| **TokenCache** | `internal/agent/tokencache.go` | FNV-1a hash + `sync.Map` 的 token 估算缓存，面向长文档 chunk 和重复消息场景 |
| **JSON Buffer Pool** | `internal/jsonutil/pool.go` | JSON 序列化/反序列化的 buffer 复用池 |
| **pprof 端点** | `internal/health/pprof.go` | `ap.RegisterPProf(mux)` 和 `ap.PProfHandler()` 导出至 `pkg/`，支持所有标准 profile 类型 |
| **PGO** | `docs/advanced/PGO调优.md` | Profile-Guided Optimization 使用指南 |
| **Fuzz 测试** | `internal/security/`, `internal/memory/`, `internal/tools/` | Sandbox 路径遍历、RAG 检索、工具执行器安全模糊测试 |
| **供应链安全** | `docs/advanced/供应链安全.md` | govulncheck + npm audit + Trivy + cosign 签名 + SBOM 生成 |

---

## 16. 示例应用

**位置：** `ecosystem/examples/`

### 链式 API 示例（推荐）

| 示例 | 说明 |
|------|------|
| `chain-api/` | 最简 Agent — 4 个必填字段 |
| `chain-capable/` | 渐进式添加能力 — 工具 + 记忆 + Hook |
| `chain-rag/` | RAG + 事件 + 指标 |
| `chain-plugins/` | 插件生态 — JSON + KV + Email |
| `chain-production/` | 生产级 Agent — 全部能力 |

### 传统示例

| 示例 | 说明 |
|------|------|
| `basic/` | 基础用法 |
| `simple/` | 简单示例 |
| `with-tools/` | 带工具的 Agent |
| `builtin-tools/` | 内置工具演示 |
| `multi-agent/` | 多 Agent 并发 |
| `multi-agent-collab/` | 多 Agent 协作 |
| `memory-backends/` | 记忆后端对比 |
| `multimodal-vision/` | 视觉多模态 |
| `multimodal-advanced/` | 高级多模态 |
| `gemini-provider/` | Gemini Provider |
| `qwen-provider/` | 千问 Provider |
| `resilient-provider/` | 弹性 Provider |
| `debug-tools/` | 调试工具 |
| `github-issue-triage/` | **GitHub Issue Triage Bot**（生产级，无 API Key 可跑） |

### 运行示例

```bash
# 链式 API 示例
go run ./ecosystem/examples/chain-api/

# 多 Agent 示例
go run ./ecosystem/examples/multi-agent/

# 生产级示例
go run ./ecosystem/examples/chain-production/

# 使用 Makefile
make run-hello        # chain-api
make run-multi        # multi-agent
make run-production   # chain-production
```

---

## 17. Provider 贡献指南

**位置：** `ecosystem/contributing/PROVIDER.md`

### 9 章节指南

1. **概述** — 为什么要贡献 Provider
2. **快速开始** — 5 步创建新 Provider
3. **Provider 接口规范** — Complete/Stream/CallTools/Info 详解
4. **可选接口** — Embedder、MultimodalProvider
5. **配置模式** — 使用通用 Config 结构体
6. **测试要求** — 必须通过的测试
7. **命名规范** — 文件命名、结构体命名
8. **提交流程** — PR 检查清单
9. **常见问题** — 8 个 FAQ

### Provider 模板

```go
// internal/llm/provider_template.go
package llm

import "context"

type MyProvider struct {
    apiKey  string
    baseURL string
}

func NewMyProvider(apiKey, baseURL string) *MyProvider {
    return &MyProvider{apiKey: apiKey, baseURL: baseURL}
}

func (p *MyProvider) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
    // TODO: 实现同步补全逻辑
    return &CompletionResponse{
        Content: "response",
        Usage:   Usage{PromptTokens: 10, CompletionTokens: 20},
    }, nil
}

func (p *MyProvider) Stream(ctx context.Context, req *CompletionRequest) (<-chan Chunk, error) {
    // TODO: 实现流式输出
    ch := make(chan Chunk, 10)
    go func() {
        defer close(ch)
        ch <- Chunk{Content: "response", Done: true}
    }()
    return ch, nil
}

func (p *MyProvider) CallTools(ctx context.Context, req *ToolCallRequest) (*ToolCallResponse, error) {
    // TODO: 实现工具调用
    return &ToolCallResponse{}, nil
}

func (p *MyProvider) Info() ModelInfo {
    return ModelInfo{
        Name:              "my-model",
        Provider:          "my-provider",
        MaxContext:        8192,
        SupportsTools:     true,
        SupportsStreaming: true,
    }
}
```

---

## 18. TypeScript SDK

**位置：** `sdk/typescript/`

**Go 功能对等** — 34 个模块目录覆盖 Go `internal/` 全部能力：

```
sdk/typescript/src/
├── agent/                    # ReAct 循环 + 微内核 + 反思 + 规划 + 评估 + 可视化
│   ├── react-loop.ts         # ReAct 循环（对应 Go react_loop.go）
│   ├── capability-agent.ts   # 协议式微内核（对应 Go capability_agent.go）
│   ├── request-id.ts         # 请求ID + 上下文窗口 + 检查点 + HITL + CostTracker
│   ├── session.ts            # 会话管理
│   ├── prompt-template.ts    # 提示词模板
│   ├── reflection.ts         # 自反思
│   ├── planning.ts           # 任务规划
│   ├── tool-learning.ts      # 工具学习
│   ├── eval.ts               # Agent 评估
│   ├── visualize.ts          # 工作流可视化
│   ├── stream-extended.ts    # SSE 流式 + 中间件
│   └── lifecycle-extended.ts # 生命周期状态机
├── llm/                      # LLM 抽象层（12+ Provider）
│   ├── openai.ts             # OpenAI
│   ├── anthropic.ts          # Anthropic Claude
│   ├── gemini.ts             # Google Gemini
│   ├── ollama.ts             # Ollama 本地
│   ├── providers.ts          # DeepSeek / Qwen / GLM / Mistral / Cohere / Azure
│   ├── resilient.ts          # 弹性 Provider（重试 + 熔断 + 降级）
│   ├── multimodal.ts         # 多模态适配器
│   └── cache-structured.ts   # LLM 缓存 + 结构化提取
├── tools/                    # 工具系统
│   ├── registry.ts           # 工具注册中心
│   ├── scope.ts              # 文件作用域
│   ├── scope-extended.ts     # 权限控制
│   ├── document-loaders.ts   # PDF/DOCX/JSON/CSV/HTML/MD 加载器
│   └── builtin/              # 7 个内置工具 + 插件加载器
├── memory/                   # 记忆存储
│   ├── store.ts              # 内存存储
│   ├── sqlite-store.ts       # SQLite FTS5 持久化
│   ├── vector.ts             # 向量存储
│   ├── vector-extended.ts    # HNSW / Milvus / Qdrant / 共享存储
│   └── rag.ts                # RAG 管道
├── orchestration/            # 编排引擎
│   ├── pipeline.ts           # Pipeline / ParallelRun / Handoff
│   ├── advanced.ts           # DAG / GroupChat / Debate / Supervisor
│   └── extended.ts           # 动态编排 / 调度器 / WorkerPool
├── pool/                     # 并发调度
│   ├── agent-pool.ts         # Agent Pool
│   └── dispatcher-autoscaler.ts  # AutoScaler / Dispatcher / ConcurrencyPool / FileLock
├── a2a/                      # A2A 通信
│   ├── bus.ts                # Agent-to-Agent 消息总线
│   └── transport.ts          # HTTP/TCP 传输 + 发现协议 + 认证
├── mcp/                      # MCP 协议
│   ├── types.ts              # MCP 客户端
│   └── registry-adapter.ts   # MCP 注册 + JSON-RPC + A2A 桥接
├── security/                 # 安全防护
│   ├── sandbox.ts            # ACL + Sandbox
│   ├── guardrails.ts         # PII / 注入 / 话题过滤
│   └── extended.ts           # 输入净化 + 命令防护
├── metrics/                  # 可观测性
│   ├── collector.ts          # 指标收集
│   ├── otel-prometheus.ts    # OTel + Prometheus + Debugger
│   └── otel-extended.ts      # Baggage + OTLP 导出
├── resilience/               # 弹性容错
│   └── circuit-retry.ts      # 熔断器 + 重试 + 包装器
├── prompt/                   # 提示词引擎
│   └── engine.ts             # PromptEngine / FewShot / Parser / Registry
├── operator/                 # K8s Operator CRD
│   └── crd.ts                # AgentDeployment YAML 生成
├── audit/                    # 审计日志
│   └── logger.ts             # AuditLogger + 合规报告
├── admin/                    # 管理 HTTP API
│   └── handler.ts            # Bearer Token 认证 + Web UI Dashboard
├── debugger/                 # 调试器
│   └── server.ts             # Inspector + DebugServer HTTP 服务
├── persist/                  # 状态持久化
│   └── sqlite-checkpoint.ts  # SQLite 检查点存储
├── health/                   # 健康检查
│   └── http.ts               # /healthz /readyz /livez 端点
├── events/                   # 事件总线
│   └── bus.ts                # Pub/sub 事件总线
├── browser/                  # Browser / IndexedDB / WASM Agent
├── edge/                     # Edge Runtime (Cloudflare/Deno/Bun)
├── collaboration/            # CRDT 协作
├── codegen/                  # 代码生成器
├── schema/                   # JSON Schema 校验
├── i18n/                     # 国际化
├── playground/               # Playground 组件 + CLI
├── react/                    # React 集成（协作 / Hooks / Server Components）
├── visual/                   # 可视化编辑器（节点 / 边 / 面板）
├── vscode/                   # VSCode 插件 API
├── protocol/                 # 通用协议
├── eval/                     # 评估共享用例
├── jsonutil/                 # JSON buffer 池
└── utils/                    # 高级工具
    ├── advanced.ts           # ConfigWatcher / StructuredLogger / EventBus
    └── zerocopy-pricing.ts   # ZeroCopyPool / StringBuilder / PricingCalculator
```

### Go 对等表

| Go (`internal/`) | TS (`src/`) | 对等状态 |
|-------------------|-------------|----------|
| `agent/` | `agent/` | ✅ 完整 |
| `llm/` | `llm/` | ✅ 完整 (12+ Provider) |
| `tools/` | `tools/` | ✅ 完整 (7 内置 + 6 加载器 + 插件) |
| `memory/` | `memory/` | ✅ 完整 (SQLite/HNSW/Milvus/Qdrant/RAG) |
| `orchestration/` | `orchestration/` | ✅ 完整 (DAG/GroupChat/Debate/Supervisor) |
| `pool/` | `pool/` | ✅ 完整 |
| `agent/a2a/` | `a2a/` | ✅ 完整 (HTTP/TCP/Discovery) |
| `tools/` (MCP) | `mcp/` | ✅ 完整 (Registry/Adapter/Bridge) |
| `security/` `guardrail/` | `security/` | ✅ 完整 |
| `metrics/` `otel/` | `metrics/` | ✅ 完整 |
| `resilience/` | `resilience/` | ✅ 完整 |
| `prompt/` | `prompt/` | ✅ 完整 |
| `operator/` | `operator/` | ✅ 完整 (CRD YAML) |
| `audit/` | `audit/` | ✅ 完整 |
| `admin/` | `admin/` | ✅ 完整 (Bearer Token + Web UI) |
| `debugger/` | `debugger/` | ✅ 完整 (Inspector + DebugServer) |
| `persist/` | `persist/` | ✅ 完整 (SQLite Checkpoint) |
| `health/` | `health/` | ✅ 完整 (/healthz /readyz /livez) |
| `concurrency/` | `pool/` | ✅ 完整 (FileLock + ConcurrencyPool) |
| `config/` | `utils/` | ✅ 完整 (ConfigWatcher) |
| `logger/` | `utils/` | ✅ 完整 (StructuredLogger) |
| `jsonutil/` | `utils/` | ✅ 完整 |
| `events/` | `events/` | ✅ 完整 |
| `edgeruntime/` `wasm/` | `edge/` `browser/` | ✅ 完整 (Edge/WASM) |
| `studio/web/` | `react/` `visual/` | ✅ 完整 (可视化编辑) |
| `agent/autonomy/` | `autonomy/` | ✅ 完整 (v3.3 长期自治) |
| `agent/skills/` | `skills/` | ✅ 完整 (v3.4 技能进化) |
| `agent/realtime/` | `realtime/` | ✅ 完整 (v3.6 多模态实时) |
| `agent/a2a/interop_*` | `a2a/interop.ts` | ✅ 完整 (v3.5 开放协议互操作) |
| `extensions/vscode/` | `vscode/` | ✅ 完整 (VSCode 插件) |
| — | `codegen/` `schema/` `i18n/` `playground/` | ✅ 完整 (TS 生态) |

### 使用示例

```typescript
import { ReActAgent, OpenAIProvider, ToolRegistry, AuditLogger, HealthServer } from '@agentprimordia/sdk';

const provider = new OpenAIProvider({
  apiKey: process.env.OPENAI_API_KEY!,
  model: 'gpt-4o',
});

const agent = new ReActAgent({
  name: 'my-agent',
  model: provider,
  toolkit: new ToolRegistry(),
  maxTurns: 10,
});

const response = await agent.run('你好！');
console.log(response.content);

// 基础设施端点（与 Go 框架对等）
const health = new HealthServer();
health.setReady(true);
// GET /healthz → 200, /readyz → 200, /livez → 200

const audit = new AuditLogger({ output: new InMemoryAuditOutput() });
await audit.log({ actor: 'user-1', action: 'agent.run', resource: 'my-agent' });
```

---

## 19. 版本迁移指南

**位置：** `ecosystem/docs/migration/v0-deprecations.md`

### 19.1 v0.7 → v0.8 迁移

**新特性（向后兼容）：**

1. **简化 Agent 入口** — 推荐使用 `ap.NewAgent()`，返回 `(*CapabilityAgent, error)`
   ```go
   // v0.8.0 推荐写法
   agent, err := ap.NewAgent("hello", "你是一个助手", provider, ap.WithMaxTurns(10))
   ```

2. **`WithRAGMemory()` 一步 RAG**
   ```go
   // v0.7（链式 API）
   agent, _ := ap.NewAgent(name, system, provider).
       WithRAG(ragCfg).
       WithMemory(store)
   
   // v0.8.0
   agent, err := ap.NewAgent(name, system, provider,
       ap.WithRAGMemory(ragMemoryConfig),
   )
   ```

3. **`testutil` 测试包**
   ```go
   import "github.com/Gleamseekers/AgentPrimordia/testutil"
   
   provider := testutil.NewMockProvider()
   agent := testutil.NewTestAgent(provider)
   ```

### 19.2 v0.6 → v0.7 迁移

**破坏性变更：**

1. **ReActConfig 字段调整**
   ```go
   // v0.6
   config := ReActConfig{
       Toolkit: registry,  // 已废弃
   }
   
   // v0.7
   agent, _ := ap.NewAgent(name, system, provider, ap.WithToolkit(registry))
   ```

2. **Memory 接口组合**
   ```go
   // v0.6
   type Memory interface {
       Add(ctx, episode) error
       Search(ctx, query, opts) ([]*Episode, error)
       // ... 20+ 方法
   }
   
   // v0.7
   type Memory interface {
       MemoryReader
       MemoryWriter
       MemorySearcher
       MemoryLifecycle
       MemoryExporter
       MemoryQuery
       MemoryToolUse
   }
   ```

3. **Provider 数量**
   - v0.6: 13 家 Provider
   - v0.7: 12 家 Provider（DeepSeek 合并到 OpenAI 兼容模式）

### 19.3 v1.0 → v2.0 迁移

**完全向后兼容**，v1.x 代码无需修改。v2.0.0 新增功能为可选增强：

1. **多租户治理**（可选）— 启用 `TenantManager` + `QuotaManager`，context 级数据隔离
2. **密钥管理**（可选）— 用 `SecretsManager`（AES-GCM）替代明文环境变量
3. **gRPC 传输**（可选）— A2A gRPC + 连接池复用
4. **语义缓存**（可选）— L1/L2 多级语义缓存，降低 LLM 成本
5. **MapReduce 编排**（可选）— 大规模任务分片并行

```bash
# 升级依赖
go get agentprimordia@v2.0.0
npm install @agentprimordia/sdk@2.0.0
ap version   # 验证: v2.0.0
```

### 19.4 v2.0 → v3.0 迁移

**完全向后兼容**，v2.x 代码无需修改。v3.0/v3.1 新增功能均为可选增强：

1. **混沌工程**（可选）— `internal/chaos/` 实验编排 + 故障注入
2. **分布式集群**（可选）— `internal/agent/cluster/` KVStore + 跨节点消息总线
3. **Agent 市场**（可选）— `internal/agent/marketplace/` 模板注册 + 一键部署
4. **WASM 自定义工具**（可选）— `wasm/` WASM→Tool 适配器 + Ed25519 签名
5. **自适应学习**（可选）— `internal/agent/learning/` 知识蒸馏 + 能力进化
6. **隐私混合推理**（可选）— PrivacyRouter PII 检测 + 本地/云端路由

```bash
# 升级依赖
go get agentprimordia@v3.1.0
ap version   # 验证: v3.1.0
```

### 19.5 v3.2 → v3.3+ 迁移（V4 能力跃迁）

**完全向后兼容**，v3.2 代码无需修改。v3.3–v3.6 新增能力均为可选注入：

1. **长期自治**（可选，v3.3）— `internal/agent/autonomy/` + `WithAutonomy(...)` + `ap autonomy`
2. **技能进化**（可选，v3.4）— `internal/agent/skills/` + `WithSkills(...)` + `ap skill`
3. **协议互操作**（可选，v3.5）— `internal/agent/a2a/interop_*` 对齐开放 A2A 协议 + `ap a2a interop-check`；JSON-RPC 重新定位为开放协议标准传输（不再标记移除）
4. **多模态实时**（可选，v3.6）— `internal/agent/realtime/` + `WithRealtime(...)` + `ap realtime`

```bash
# 升级依赖
go get agentprimordia@latest
ap version
```

### 迁移检查清单

- [ ] 更新 `ReActConfig` 使用链式 API
- [ ] 检查 Memory 接口实现
- [ ] 更新 Provider 导入路径
- [ ] 运行测试验证兼容性

---

## 20. pgvector 适配器

**位置：** `pgvector/`

PostgreSQL pgvector 扩展适配器，用于生产级向量存储：

```go
import "github.com/Gleamseekers/AgentPrimordia/pgvector"

// 创建 pgvector 存储
store, err := pgvector.New(pgvector.Config{
    Host:     "localhost",
    Port:     5432,
    User:     "postgres",
    Password: "password",
    Database: "agentprimordia",
    Table:    "vectors",
})
if err != nil {
    log.Fatal(err)
}
defer store.Close()

// 使用与 VectorStore 相同的接口
agent, _ := ap.NewAgent("my-agent", "你是一个助手", provider).
    WithMemory(store)
```

### 特性

- 支持 HNSW 索引
- 余弦相似度 / L2 距离 / 内积
- 批量插入优化
- 连接池管理

---

## 21. 相关文档

### 项目文档

| 文档 | 位置 | 说明 |
|------|------|------|
| Code Wiki | `CODE_WIKI.md` | 完整代码文档（22 章节，4.1–4.30 模块详解） |
| CHANGELOG | `docs/CHANGELOG.md` | 变更日志 |
| v2.0.0 发布说明 | `docs/发布说明-v2.0.0.md` | 生产就绪里程碑 |
| v1.0.0 发布说明 | `docs/发布说明-v1.0.0.md` | API 稳定性锁定 |
| 开发文档 | `DEVELOPMENT.md` | 开发者指南 |
| 快速入门 | `ecosystem/docs/getting-started.md` | 5 分钟上手 |
| API 参考 | `ecosystem/docs/api-reference.md` | 完整 API 文档 |
| 最佳实践 | `ecosystem/docs/best-practices.md` | 生产环境建议 |
| CLI 指南 | `ecosystem/docs/ap-guide.md` | ap 命令详解 |
| Go 生态 | `ecosystem/docs/go-ecosystem.md` | Go 生态集成 |
| 向量库指南 | `ecosystem/docs/vector-db-guide.md` | 向量数据库集成 |
| Cookbook | `ecosystem/docs/cookbook/` | 实战菜谱 |
| 供应链安全 | `docs/advanced/供应链安全.md` | govulncheck + Trivy + cosign 签名 + SBOM |
| PGO 性能调优 | `docs/advanced/PGO调优.md` | Profile-Guided Optimization 指南 |
| Go vs TypeScript 基准 | `docs/benchmarks/Go与TypeScript对比.md` | 双 SDK 性能对比报告 |

### 贡献指南

| 文档 | 位置 | 说明 |
|------|------|------|
| Provider 贡献 | `ecosystem/contributing/PROVIDER.md` | 贡献新 Provider |
| 插件贡献 | `ecosystem/contributing/PLUGIN.md` | 贡献新插件 |

### 迁移文档

| 文档 | 位置 | 说明 |
|------|------|------|
| v0 废弃说明 | `ecosystem/docs/migration/v0-deprecations.md` | 版本迁移指南 |

---

## 22. 设计哲学

1. **来自生产，服务生产** — 核心模式从 CodeCast 生产环境提炼
2. **接口优先** — LLM / Tools / Memory 全部接口解耦，自由替换
3. **并发原生** — Goroutine + Channel 是一等公民
4. **最小外部依赖** — 核心零 CGO，仅依赖纯 Go SQLite + YAML；可选 gRPC/Redis/etcd/wazero 按需引入
5. **TDD 强制** — 每个功能先写测试，Red → Green → Refactor
6. **协议式微内核** — 能力通过接口发现，而非配置字段
7. **链式 API** — `NewAgent(...).WithMemory(mem).WithRAG(cfg)` 风格

---

*文档更新时间：2026-08-28*
*版本：v6.0.0 (Go SDK) / v6.0.0 (TypeScript SDK)*
