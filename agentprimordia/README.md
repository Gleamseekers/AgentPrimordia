<p align="center">
  <img src="https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&style=for-the-badge" alt="Go Version">
  <img src="https://img.shields.io/badge/TypeScript-5.0+-3178C6?logo=typescript&style=for-the-badge" alt="TypeScript">
  <img src="https://img.shields.io/badge/License-Apache--2.0-blue?style=for-the-badge" alt="License">
  <img src="https://img.shields.io/badge/Zero_CGO-✓-brightgreen?style=for-the-badge" alt="Zero CGO">
  <img src="https://img.shields.io/badge/version-v7.3.0-blue?style=for-the-badge" alt="Version">
</p>

<h1 align="center">⚡ AgentPrimordia</h1>

<p align="center">
  <strong>万物之源，智能之始</strong><br>
  生产级 AI Agent 开发框架 — Go + TypeScript 双语言支持
</p>

<p align="center">
  <a href="#-quick-start">快速开始</a> · <a href="#-核心特性">核心特性</a> · <a href="#-架构设计">架构设计</a> · <a href="./DEVELOPMENT.md">开发文档</a> · <a href="#-typescript-sdk">TypeScript SDK</a>
</p>

---

## 💡 为什么选择 AgentPrimordia？

构建 AI Agent 应用时，你是否遇到这些痛点？

| 痛点 | AgentPrimordia 的答案 |
|------|----------------------|
| 🧩 LLM Provider 耦合，换个模型就要改代码 | **统一 Provider 接口**，10+ 内置 Provider，一行代码切换 |
| 💥 API 调用不稳定，偶尔超时或限流 | **ResilientProvider**：自动重试 + 降级链 + 熔断器 |
| 🔧 工具系统从零搭建，每个工具都要写胶水代码 | **Plugin Tool 接口**，7 个内置工具 + 权限确认机制 |
| 🧠 Agent 没有记忆，每次对话从零开始 | **Episodic Memory**：SQLite + FTS5 + 向量检索 + RAG |
| 🔄 多任务编排复杂，手动管理 goroutine | **Pool 调度器**：并发控制 + 超时 + 重试 + 事件通知 |
| 🔒 Agent 执行危险操作没有防线 | **Sandbox + ACL**：命令白名单 + 路径穿越检测 + 访问控制 |
| 📊 生产环境缺乏可观测性 | **Prometheus 指标 + Hook 系统 + Event Bus** |

**一句话**：AgentPrimordia 让你专注 Agent 的业务逻辑，基础设施我们包了。

### 🚀 v3.3–v3.6 能力跃迁（已实现）

| 版本 | 能力 | 一句话 | CLI |
|------|------|--------|-----|
| v3.3 | 长期自治 | 给定目标自主规划/执行/校验/重规划，崩溃恢复 + 幂等 | `ap autonomy` |
| v3.4 | 技能进化 | 运行中习得/验证/沉淀可复用技能，语义匹配自动调用 | `ap skill` |
| v3.5 | 协议互操作 | 对齐开放 Agent2Agent 协议，跨生态任务委托 + 符合性报告 | `ap a2a interop-check` |
| v3.6 | 多模态实时 | 语音/视觉实时双向流 + 打断，ASR/TTS 可插拔 | `ap realtime` |

> 详见 [`docs/V4路线图.md`](../docs/V4路线图.md)，模块概念 `docs/concepts/{autonomy,skills,realtime}.md`，使用指南 `docs/guides/{skill-format,a2a-interop,realtime}.md`。

---

## ✨ 核心特性

| 能力 | 说明 |
|------|------|
| 🧠 **ReAct Loop 引擎** | Reason→Act→Observe 循环，流式输出，上下文自动裁剪，检查点恢复 |
| 🛡️ **弹性调用** | ResilientProvider：指数退避重试 + 降级链 + 熔断器 |
| 🧩 **10+ LLM Provider** | OpenAI / Anthropic / Gemini / Ollama / Azure / Qwen / GLM / Mistral / Cohere / DeepSeek |
| 🔧 **工具系统** | 7 内置工具（FS/Shell/Web/API/DB/Code/Knowledge）+ MCP + 插件，4 方法自定义 |
| 🧠 **三层记忆** | SQLite FTS5 + Vector Store + RAG 混合检索（RRF 融合） |
| 🔄 **多 Agent 调度** | Pool 信号量并发 + 会话隔离 + AutoScaler + 事件通知 |
| 🔒 **安全沙箱** | ACL + Sandbox + 路径穿越/symlink 逃逸防护 + Guardrails/PII |
| 🪝 **20+ 生命周期 Hook** | 审计/告警/成本追踪等关键节点插桩 |
| 📊 **可观测性** | Prometheus 指标 + OpenTelemetry + 35 个结构化错误码 |
| 🧭 **多模式编排** | Pipeline / Handoff / Parallel / DAG / GroupChat / Debate / Workflow |

> 每项能力的代码示例与详解见 [docs/index.md](docs/index.md) 与 [docs/concepts/](docs/concepts/)。V4 新增能力（自治/技能/互操作/实时）见上方速览表。

---

## 🚀 Quick Start

```bash
go get agentprimordia
```

```go
package main

import (
    "context"
    "fmt"
    "os"

    ap "agentprimordia/pkg"
)

func main() {
    provider, _ := ap.NewOpenAIProvider(ap.Config{
        APIKey: os.Getenv("OPENAI_API_KEY"), Model: "gpt-4o-mini",
    })
    agent, _ := ap.NewAgent("my-agent", "You are a helpful assistant.",
        provider, ap.WithMaxTurns(10), ap.WithToolkit(ap.NewToolRegistry()))
    resp, _ := agent.Run(context.Background(), ap.UserMessage("Hello!"))
    fmt.Println(resp.Content)
}
```

> 带工具/记忆/RAG/多 Agent 的进阶示例见 [docs/getting-started/](docs/getting-started/) 与 [docs/guides/](docs/guides/)。

### 🎯 5 分钟体验

```bash
# 克隆并进入项目
git clone https://github.com/Gleamseekers/AgentPrimordia.git && cd AgentPrimordia/agentprimordia

# 方式1: CLI 一键启动（无需 API key 也能跑 Demo 模式）
./ap init my-agent --template quickstart && cd my-agent && ../ap run

# 方式2: 多 Agent 辩论展示（需设置 API key）
export AP_LLM_API_KEY=sk-xxx
export AP_LLM_BASE_URL=https://token.sensenova.cn/v1  # 或其他 OpenAI 兼容端点
export AP_LLM_MODEL=sensenova-6.8-flash-lite
go run showcase-debate.go
```

**辩论展示输出示例**：三个 Agent（架构师/安全专家/产品经理）围绕「微服务 vs 单体」进行多轮辩论，各自从专业角度阐述立场、回应对方、收敛共识——这是 AgentPrimordia 独有的多 Agent 编排能力。

---

## 🏗️ 架构设计

```
┌─────────────────────────────────────────────────┐
│                  pkg/ (公共 API)                 │
├─────────────────────────────────────────────────┤
│  pool/   │  agent/   │  security/  │ concurrency│
│ (调度)   │ (ReAct)   │  (安全)     │  (并发)    │
├──────────┼───────────┼────────────────────────┤
│  tools/  │  memory/  │  events/   │  metrics/  │
│ (工具)   │  (记忆)    │  (事件)    │  (指标)    │
├──────────┴───────────┴────────────┴────────────┤
│              llm/ (Provider 抽象层)              │
├─────────────────────────────────────────────────┤
│            persist/ (检查点持久化)               │
└─────────────────────────────────────────────────┘
```

**设计原则**：接口驱动 · 组合优于继承 · 弹性优先 · 零 CGO · 协议式微内核（能力经 `*Capable` 接口自动发现）。

> 分层详解与 ReAct 数据流见 [docs/架构图.md](../docs/架构图.md) 与 [docs/concepts/react-loop.md](docs/concepts/ReAct循环.md)。

---

## 🟦 TypeScript SDK

为 Node.js 开发者提供与 Go 100% 功能对等的 TypeScript SDK：

```bash
npm install @agentprimordia/sdk
```

> 完整 API、Provider/工具/记忆/编排示例见 [docs/index.md](docs/index.md)（TypeScript 标签页）与 [sdk/typescript/](../sdk/typescript/)。

---

## 📦 项目结构

```
agentprimordia/
├── cmd/ap/              # CLI (init/run/debug/loop/autonomy/skill/a2a/realtime/...)
├── internal/
│   ├── agent/           # ReAct 引擎 + 编排 + 协议式微内核
│   │   ├── autonomy/    # 长期自治 (v3.3)
│   │   ├── skills/      # 技能进化 (v3.4)
│   │   ├── realtime/    # 多模态实时 (v3.6)
│   │   ├── a2a/         # Agent2Agent + 开放协议互操作 (v3.5)
│   │   ├── planning/ reflection/ tool_learning/ learning/
│   ├── llm/ memory/ tools/ pool/ persist/ orchestration/
│   ├── guardrail/ security/ metrics/ otel/ events/ config/
│   └── ...              # admin / debugger / chaos / health / logger / ...
├── pkg/                 # 公共 API 重导出
├── ecosystem/           # 插件 / 示例 / 模板
├── operator/            # Kubernetes Operator (CRD + HPA)
├── bench/               # 性能基准测试套件
├── docs/                # 概念 / 指南 / API 参考 / 路线图
└── ../sdk/typescript/   # TypeScript SDK（仓库根，另有 ../sdk/python、../sdk/rust）
```

---

## 🧪 运行示例

```bash
# 既有示例（monorepo 根 agentprimordia/ 下）
make run-hello          # 最简 Agent
make run-multi          # 多 Agent 并发
make run-production     # RAG + 弹性调用 + 事件系统

# V4 验收 demo
go run ./ecosystem/examples/autonomous-task/   # v3.3 长期自治 + 崩溃恢复
go run ./ecosystem/examples/skill-evolution/   # v3.4 习得→复用
go run ./ecosystem/examples/a2a-interop/       # v3.5 跨生态委托
go run ./ecosystem/examples/realtime-voice/    # v3.6 语音多轮 + 打断
```

> 全部示例见 [ecosystem/examples/](ecosystem/examples/)。

---

## 📚 文档

- **入门** — [getting-started/](docs/getting-started/) · [guides/create-agent.md](docs/guides/创建Agent.md)
- **概念** — [concepts/](docs/concepts/)（react-loop / memory / orchestration / tools / [autonomy](docs/concepts/自治.md) / [skills](docs/concepts/技能系统.md) / [realtime](docs/concepts/实时通信.md) / [a2a](docs/concepts/A2A通信.md)）
- **指南** — [guides/](docs/guides/)（[skill-format](docs/guides/技能格式.md) · [a2a-interop](docs/guides/A2A互通.md) · [realtime](docs/guides/实时通信.md) · 部署 · 安全 · 性能）
- **API 参考** — [docs/api/](docs/api/) · [docs/api-reference/](docs/api-reference/)
- ** cookbook** — [docs/cookbook/](docs/cookbook/)（RAG / 多 Agent / 代码审查机器人 / K8s 部署 / WASM 沙箱 …）
- **路线图与变更** — [V4路线图.md](../docs/V4路线图.md) · [ROADMAP.md](../docs/路线图.md) · [CHANGELOG.md](../docs/CHANGELOG.md)
- **性能与部署** — [benchmarks/](docs/benchmarks/) · [DEPLOYMENT.md](../docs/部署指南.md) · [DEVELOPMENT.md](./DEVELOPMENT.md)

---

## 🌟 核心优势总结

| | AgentPrimordia | LangChain | AutoGen |
|---|---|---|---|
| **语言** | Go + TypeScript | Python | Python |
| **CGO 依赖** | ❌ 零 CGO | — | — |
| **弹性调用** | ✅ 内建重试+降级+熔断 | 需自行实现 | 需自行实现 |
| **记忆系统** | ✅ SQLite+FTS+Vector+RAG | 需外接 | 基础支持 |
| **安全沙箱** | ✅ ACL+Sandbox+路径穿越检测 | ❌ | ❌ |
| **长期自治 / 技能进化** | ✅ v3.3 / v3.4 内建 | ❌ | ❌ |
| **Prometheus** | ✅ 内建 | 需自行集成 | 需自行集成 |
| **结构化错误码** | ✅ 35 个错误码 | ❌ |  |
| **TypeScript SDK** | ✅ 官方对等 | 社区 | ❌ |
| **单二进制部署** | ✅ | ❌ | ❌ |

---

<p align="center">
  <strong>万物之源，智能之始</strong><br>
  用 AgentPrimordia 构建你自己的 AI Agent
</p>
