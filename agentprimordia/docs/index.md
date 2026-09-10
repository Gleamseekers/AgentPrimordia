# AgentPrimordia

> 通用 AI Agent 开发框架 — 轻量、并发原生、生产验证
> **Go + TypeScript 双语言 SDK，100% 功能对等**

[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](https://github.com/AgentPrimordia/agentprimordia)
[![Go Version](https://img.shields.io/badge/go-1.26+-00ADD8E.svg)](https://golang.org)
[![TypeScript](https://img.shields.io/badge/TypeScript-100%25%20Go%20Parity-3178C6.svg)](../../sdk/typescript/)

---

## 框架概览

AgentPrimordia（AP）是一个用 Go 语言编写、同时提供 TypeScript SDK 的生产级 AI Agent 开发框架。它提供完整的工具来构建、部署和管理智能 Agent 系统，核心能力覆盖从单 Agent ReAct 循环到多 Agent DAG 编排的全链路场景。

### 核心特性

- **ReAct Loop 引擎** — Reasoning + Acting 循环，20+ 生命周期钩子，支持检查点恢复
- **多模式编排** — Pipeline / Handoff / Parallel / DAG / GroupChat / A2A
- **A2A 协议** — gRPC（唯一传输，v4.0.0 移除 JSON-RPC 兼容层）+ 开放互操作（`pkg/a2a_interop.go`）
- **工具系统** — FileSystem / Shell / Web / Knowledge / Database / CodeExecution / API 内置工具，MCP 协议集成，插件扩展（含 `ap plugin search/update`）
- **三层记忆** — SQLite FTS5 + Vector Store + RAG Pipeline 混合检索（支持 RRF 融合）
- **10+ 内置 LLM Provider** — OpenAI / Anthropic / Gemini / Ollama / Azure / Qwen / GLM / Mistral / Cohere / DeepSeek / 多模态 / Resilient 弹性包装器
- **并发调度** — Pool 信号量调度，会话隔离，自动扩缩容
- **安全防护** — ACL / Sandbox / Guardrails / PII 检测 / 路径遍历防护 + symlink 逃逸防护
- **可观测性** — Prometheus Metrics / OpenTelemetry / Grafana Dashboard / pprof 性能分析 + 结构化日志（统一 `logger.Field*` 字段 + trace-id 注入）
- **K8s Operator** — AgentDeployment CRD 声明式部署 + HPA（Behavior 精细化扩缩容）+ PDB（PodDisruptionBudget）+ 滚动升级（preStop hook）+ 自定义 Metrics Adapter
- **TypeScript SDK** — 100% Go 功能对等，24+ 模块全覆盖
- **长期自治（v3.3）** — 目标驱动自主规划/执行/校验/重规划，崩溃恢复 + 幂等保护（`ap autonomy`）
- **技能进化（v3.4）** — 运行中习得/验证/沉淀可复用技能，语义匹配自动调用（`ap skill`）
- **协议互操作（v3.5）** — 对齐开放 Agent2Agent 协议，跨生态任务委托 + 符合性报告（`ap a2a interop-check`）
- **多模态实时（v3.6）** — 语音/视觉实时双向流 + 打断，ASR/TTS 可插拔（`ap realtime`）
- **CLI 工具** — `ap init / run / debug / loop / test / mcp / plugin / autonomy / skill / a2a / realtime / doctor / config / completion`

### v7.3.0 亮点

- **开发者体验重构** — `ap.NewAgent()` 简化入口，3 行创建带记忆 / RAG / Hook 的 Agent
- **`WithRAGMemory()` 一步 RAG** — 自动完成 EmbeddingAdapter + RAGStore + RAGProvider 组装
- **`ap loop` 工程化子命令** — `trace`（执行追踪）/ `inspect`（状态检查）/ `resume`（检查点恢复）
- **RAG RRF 融合** — Reciprocal Rank Fusion 混合检索算法，支持运行时切换 Linear / RRF 模式
- **性能优化** — BufferPool（bytes.Buffer 复用）、TokenCache、JSON Pool、pprof 端点
- **供应链安全** — govulncheck + npm audit + Trivy + cosign 签名 + SBOM 生成
- **PGO 性能调优** — Profile-Guided Optimization 指南
- **Fuzz 测试** — Sandbox / RAG / 工具执行器安全模糊测试

## 快速开始

### 安装

```bash
# 安装 CLI 工具
go install github.com/AgentPrimordia/agentprimordia/cmd/ap@latest

# 创建新项目
ap init my-agent --template quickstart
cd my-agent

# 运行
ap run
```

### 5 行代码创建 Agent

=== "Go"

    ```go
    package main

    import (
        "context"
        "fmt"
        "os"
        ap "agentprimordia/pkg"
    )

    func main() {
        agent := ap.NewAgent("HelloAgent", "你是一个智能助手",
            ap.NewOpenAIProvider(ap.Config{
                APIKey: os.Getenv("OPENAI_API_KEY"),
                Model:  "gpt-4o",
            }),
            ap.WithMaxTurns(10),
        )

        resp, _ := agent.Run(context.Background(), ap.UserMessage("你好"))
        fmt.Println(resp.Content)
    }
    ```

=== "TypeScript"

    ```bash
    npm install @agentprimordia/sdk
    ```

    ```typescript
    import { ReActAgent, OpenAIProvider, ToolRegistry } from '@agentprimordia/sdk';

    const agent = new ReActAgent({
      name: 'HelloAgent',
      model: new OpenAIProvider({ apiKey: process.env.OPENAI_API_KEY!, model: 'gpt-4o' }),
      toolkit: new ToolRegistry(),
      maxTurns: 10,
      systemPrompt: '你是一个智能助手',
    });

    const resp = await agent.run('你好');
    console.log(resp.content);
    ```

### 链式 API — 工具 + 记忆 + RAG 一步到位

=== "Go"

    ```go
    agent := ap.NewAgent("assistant", "你是助手", provider, ap.WithMaxTurns(10)).
        WithToolkit(toolkit).
        WithMemory(mem).
        WithRAG(ragProvider)
    ```

=== "TypeScript"

    ```typescript
    const agent = new ReActAgent({
      name: 'assistant',
      model: provider,
      toolkit: registry,
      maxTurns: 10,
      systemPrompt: '你是助手',
      memory,
    });
    ```

## 性能对比

| 指标 | AgentPrimordia (Go) | LangGraph (Python) | 提升 |
|------|---------------------|-------------------|------|
| 启动时间 | 0.12s | 2.8s | **23x** |
| 内存占用 | 45MB | 320MB | **7x** |
| 并发吞吐 | 852 req/s | 98 req/s | **8.7x** |
| 部署体积 | 15MB | 280MB | **18x** |

详细性能报告：[Go vs TypeScript 基准对比](benchmarks/Go与TypeScript对比.md)

## 架构

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

## CLI 命令

```bash
ap init my-agent              # 创建项目 (--template basic|with-tools|multi-agent|agent-with-cache|agent-with-rag|agent-with-metrics|quickstart)
ap run                        # 编译运行 (--watch 监视模式)
ap debug                      # 调试服务器 (http://localhost:6060)
ap loop trace                 # 查看 Agent 执行追踪
ap loop inspect               # 查看 Agent 当前状态
ap loop resume                # 从检查点恢复运行
ap test                       # 运行 eval 测试套件
ap mcp list                   # 列出 MCP Server
ap mcp add fs --command npx --args "@mcp/server-filesystem,/tmp"
ap plugin install github.com/user/ap-plugin-xxx  # 安装插件（go get + 写 .ap.yaml）
ap plugin list                                  # 列出已安装插件
ap plugin search database                       # 在本地 registry.json 中按关键词搜索
ap plugin update                                # 更新全部已安装插件（go get -u + mod tidy）
ap doctor                                       # 健康检查
ap completion bash                              # 生成 Shell 补全脚本
```

## A2A 协议（默认 gRPC）

自 v1.x 起 **gRPC 是 A2A 的默认且唯一传输**（序列化延迟更低、消息体积更小、内建拦截器链）。
JSON-RPC over HTTP 兼容层已于 **v4.0.0 移除**（见 版本规范.md「已移除记录」）；
开放 Agent2Agent 协议互操作（JSON-RPC over HTTP/SSE 承载开放协议）见 `pkg/a2a_interop.go`。

=== "Go（推荐 gRPC）"

    ```go
    service := ap.NewA2AService(card, taskManager)
    srv  := ap.NewA2AGRPCServer(service,
        ap.WithGRPCLogger(slog.Default()),
        ap.ChainUnaryInterceptors(
            ap.RecoveryInterceptor(),
            ap.LoggingInterceptor(ap.A2AInterceptorConfig{...}),
            ap.MetricsInterceptor(metrics),
        ),
    )
    cli, _ := ap.NewA2AGRPCClient("localhost:50051")
    resp, _ := cli.GetAgentCard(ctx, &ap.A2AGetAgentCardRequest{})
    ```

=== "Go（gRPC Server，推荐）"

    ```go
    // 推荐：gRPC 传输（v1.x 起为默认）
    srv := ap.NewA2AGRPCServer(service)
    ```

## 文档导览

| 类别 | 内容 |
|------|------|
| **快速开始** | [安装](getting-started/安装指南.md) · [入门](getting-started/五分钟入门.md) · [第一个Agent](getting-started/第一个Agent.md) |
| **核心概念** | [Agent架构](concepts/Agent架构.md) · [ReAct循环](concepts/ReAct循环.md) · [工具系统](concepts/工具系统.md) · [记忆系统](concepts/记忆系统.md) · [RAG](concepts/RAG检索增强.md) · [编排](concepts/多Agent编排.md) · [A2A](concepts/A2A通信.md) · [护栏](concepts/安全护栏.md) |
| **使用指南** | [创建Agent](guides/创建Agent.md) · [添加工具](guides/添加工具.md) · [并发调度](guides/并发调度.md) · [流式输出](guides/流式输出.md) · [多模态](guides/多模态.md) · [部署](guides/部署到生产.md) |
| **API参考** | [Agent](api/Agent-API.md) · [LLM](api/LLM-API.md) · [Tools](api/Tools-API.md) · [Memory](api/Memory-API.md) · [Pool](api/Pool-API.md) · [A2A](api/A2A-API.md) |
| **进阶主题** | [性能优化](advanced/性能优化.md) · [PGO调优](advanced/PGO调优.md) · [安全](advanced/安全最佳实践.md) · [供应链安全](advanced/供应链安全.md) · [Debugger](advanced/调试器.md) · [Metrics](advanced/指标.md) · [OTel](advanced/OpenTelemetry.md) |
| **基准测试** | [Go vs TypeScript](benchmarks/Go与TypeScript对比.md) · [性能对比](benchmarks/性能对比-2026.md) |
| **实施进度** | [STATUS 总览](项目状态.md) · [VERSIONING](版本规范.md) · [路线图](../../docs/路线图.md) |

## 社区

- [GitHub Discussions](https://github.com/AgentPrimordia/agentprimordia/discussions)
- [贡献指南](https://github.com/AgentPrimordia/agentprimordia/blob/main/CONTRIBUTING.md)
- [TypeScript SDK 文档](../../sdk/typescript/README.md)

## License

Apache-2.0 © AgentPrimordia Contributors
