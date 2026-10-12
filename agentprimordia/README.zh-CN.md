<p align="center">
  <img src="https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&style=for-the-badge" alt="Go Version">
  <img src="https://img.shields.io/badge/TypeScript-5.4+-3178C6?logo=typescript&style=for-the-badge" alt="TypeScript">
  <img src="https://img.shields.io/badge/License-Apache--2.0-blue?style=for-the-badge" alt="License">
  <img src="https://img.shields.io/badge/Zero_CGO-✓-brightgreen?style=for-the-badge" alt="Zero CGO">
  <a href="https://github.com/Gleamseekers/AgentPrimordia/releases"><img src="https://img.shields.io/github/v/release/Gleamseekers/AgentPrimordia?style=for-the-badge" alt="最新发布"></a>
  <a href="https://github.com/Gleamseekers/AgentPrimordia/actions/workflows/ci.yml"><img src="https://github.com/Gleamseekers/AgentPrimordia/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
</p>

<h1 align="center">⚡ AgentPrimordia</h1>

<p align="center">
  <strong>万物之源，智能之始</strong><br>
  生产级 AI Agent 开发框架 — Go 为主，TypeScript 双语言对等
</p>

<p align="center">
  <a href="#-快速开始">快速开始</a> · <a href="#-为什么选择-agentprimordia">为什么</a> · <a href="#-核心特性">核心特性</a> · <a href="#-架构设计">架构设计</a> · <a href="#-文档">文档</a> · <a href="./README.md">English</a>
</p>

---

## 🚀 快速开始

### 1. 安装 CLI

```bash
go install github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/cmd/ap@latest

ap version        # 验证安装
```

框架就是普通 Go 模块，经 `GOPROXY` 直接解析，无需克隆仓库。要求 Go 1.26 及以上
（`toolchain` 指令会自动拉取对应工具链）。

### 2. 跑通第一个 Agent — 无需 API key

```bash
ap init my-agent --template quickstart
cd my-agent
go mod tidy
go run .
```

未设置 `AP_LLM_API_KEY` 时，脚手架项目走 **Demo 模式**：由关键词匹配 Provider
（`ap.NewDemoProvider()`）驱动，让你在不花一分钱 token 的情况下跑完整个 ReAct 循环
（轮次、工具分发、统计）。要接真实模型：

```bash
export AP_LLM_API_KEY=sk-...
export AP_LLM_BASE_URL=https://api.openai.com/v1   # 任意 OpenAI 兼容端点
export AP_LLM_MODEL=gpt-4o-mini
go run .                                          # 或 ap run（构建 + 热重启）
```

### 3. 作为库使用

```bash
go get github.com/Gleamseekers/AgentPrimordia/v7@latest
```

```go
package main

import (
    "context"
    "fmt"
    "os"

    ap "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg"
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

> `ap init` 提供 7 套模板：`quickstart`（推荐）· `basic` · `with-tools` · `agent-with-rag` ·
> `agent-with-cache` · `agent-with-metrics` · `multi-agent`。
> 工具/记忆/RAG/多 Agent 进阶示例见 [docs/getting-started/](docs/getting-started/)。

---

## 💡 为什么选择 AgentPrimordia？

构建 AI Agent 应用时，你是否遇到这些痛点？

| 痛点 | AgentPrimordia 的答案 |
|------|----------------------|
| 🧩 LLM Provider 耦合，换个模型就要改代码 | **统一 Provider 接口**，10+ 内置 Provider，一行代码切换 |
| 💥 API 调用不稳定，偶尔超时或限流 | **弹性调用**：指数退避重试 + 降级链 + 熔断器 |
| 🔧 工具系统从零搭建，每个工具都要写胶水代码 | **工具注册表 + 执行器**：Scope 校验 + MCP + 插件 + 20+ 内置工具 |
| 🧠 Agent 没有记忆，每次对话从零开始 | **三层记忆**：SQLite（FTS5）+ 向量库（HNSW）+ RAG 混合检索（RRF 融合） |
| 🔄 多任务编排复杂，手动管理 goroutine | **Pool 调度器**：有界并发 + 会话隔离 + AutoScaler + 事件背压 |
| 🔒 Agent 执行危险操作没有防线 | **ACL + Sandbox**：命令白名单 + 路径穿越/symlink 逃逸防护 + 循环内护栏 |
| 📊 生产环境缺乏可观测性 | **Prometheus 指标 + OpenTelemetry 链路 + 35 个结构化错误码 + 30+ 生命周期 Hook** |

**一句话**：AgentPrimordia 让你专注 Agent 的业务逻辑，基础设施我们包了。

### 🧭 v7.x 实况（真正发布了什么）

版本事实以 [`docs/版本规范.md`](docs/版本规范.md) 与 [`docs/能力清单.md`](../docs/能力清单.md) 为准，下表为摘要。

| 版本 | 交付内容 |
|------|----------|
| **v7.5.1** | 发布链路修正：6 平台二进制 + SHA256 校验和 + cosign 签名 + SBOM、Docker 构建上下文修复，`go install …@latest` 端到端可用 |
| **v7.5** | 安全与质量战役：Shell 工具白名单加固（7 条实证攻击链全阻断）、`jsonutil` 并发覆写修复、Anthropic 结构化输出 panic 修复、167 个被禁用测试恢复；覆盖率 78.8% → 81.1%；模块路径迁至 `github.com/Gleamseekers/AgentPrimordia/v7`，框架自此可经 GOPROXY 分发 |
| **v7.4** | 接线批次：OTel 端到端导出、Studio 接真实引擎、模板注册表持久化 + 签名远程协议、工具学习器自动装配、全链路关联存储、技能库持久化 + 入循环、自治 CLI 真实化 |
| **v7.3** | 零门槛入口（`ap start` / `ap init` 模板）、学习闭环体感、A2A 生态协议卡位 |
| **v7.0–v7.2** | 五构件 GA 线（蒸馏、缺口工具、常驻运行时、联邦社会等）+ 产出下述证据的评测战役 |

### 我们刻意**不**宣传的能力

诚实是 API 表面的一部分，以下三项不上卖点表：

- **世界模型维持 opt-in**：`WithWorldModel()` 默认仍关闭——预注册实验跑完 112 对，
  A 89/112 vs B 95/112（+5.4pp，McNemar p=0.1796），门（+15pp 且 p<0.05）未达，
  翻默认已正式否决（[docs/降级豁免登记-V7弧线.md](../docs/降级豁免登记-V7弧线.md) E-1）。
- **`EnhancedPlanner` 不是卖点**：其递归分解策略在 v7.2 验收实验中短程 ReAct 任务上
  **反向显著 −47.8pp（p=0.001）**（[docs/v7.2-验收实验报告.md](../docs/v7.2-验收实验报告.md)）。
  它以 `Experimental` 身份附带规划预算护栏存在，不作头条能力。
- **agent 自写工具属研究阻塞**：沙箱一半是真的——agent 生成工件只能在经签名验证的
  wazero WASM 沙箱内执行，宿主零编译零加载；但**生成器尚无生产实现**，今天不要期待
  agent 替你写出可用的工具。

可以写的差异化：统一 Provider 接口与弹性调用、带检查点的 ReActLoop、三层记忆、Pool 调度、
Sandbox/ACL、A2A 协议互操作（`ap a2a interop-check`）、可复现评测文化（预注册门、
季度 bootstrap 对比、公开报告）。

---

## ✨ 核心特性

| 能力 | 说明 |
|------|------|
| 🧠 **ReAct Loop 引擎** | Reason→Act→Observe 循环，流式输出，上下文自动裁剪，每轮检查点 + 崩溃恢复 |
| 🛡️ **弹性调用** | 指数退避重试 + 降级链 + 熔断器，同一 Provider 接口 |
| 🧩 **10+ LLM Provider** | OpenAI / Anthropic / Gemini / Ollama（本地）/ Azure / Qwen / GLM / Mistral / Cohere / DeepSeek |
| 🔧 **工具系统** | 20+ 内置工具（FS/Shell/Web/API/DB/Code/Knowledge + 文档加载器）+ MCP + 插件，`Registry.Register` 注册 |
| 🧠 **三层记忆** | SQLite FTS5 + HNSW 向量库 + RAG 混合检索（RRF 融合） |
| 🔄 **多 Agent 调度** | Pool：有界并发 + 会话隔离 + AutoScaler + 事件背压 |
| 🔒 **安全** | ACL + Sandbox + 路径穿越/symlink 逃逸防护 + 护栏（注入检测/PII/主题过滤，已接入循环） |
| 🪝 **30+ 生命周期 Hook** | 审计/告警/成本追踪等关键节点插桩 |
| 📊 **可观测性** | Prometheus 指标 + OpenTelemetry OTLP 导出 + 35 个结构化错误码 |
| 🧭 **多模式编排** | Pipeline / Handoff / Parallel / DAG / GroupChat / Debate / MapReduce |
| 💾 **检查点持久化** | 默认 SQLite；etcd / Redis 后端经 build tag 门控 |
| 🤝 **A2A 互操作** | Agent2Agent 协议（gRPC 传输）+ `ap a2a interop-check` 自检 |
| 🧪 **可复现评测** | 预注册门、季度 bootstrap 对比、公开报告 |

> 每项能力的代码示例与详解见 [docs/index.md](docs/index.md)、[docs/concepts/](docs/concepts/)
> 与 [docs/cookbook/](docs/cookbook/)。

---

## 🏗️ 架构设计

```
┌─────────────────────────────────────────────────┐
│                  pkg/ (公共 API)                 │
├─────────────────────────────────────────────────┤
│  pool/   │  agent/   │  security/  │ concurrency│
│ (调度)   │ (ReAct)   │  (安全)     │  (并发)    │
├──────────┼───────────┼──────────────────────────┤
│  tools/  │  memory/  │  events/   │  metrics/   │
│ (工具)   │ (记忆)    │  (事件)    │  (指标)     │
├──────────┴───────────┴────────────┴─────────────┤
│              llm/ (Provider 抽象层)              │
├─────────────────────────────────────────────────┤
│            persist/ (检查点持久化)               │
└─────────────────────────────────────────────────┘
```

**设计原则**：接口驱动 · 组合优于继承 · 弹性优先 · 零 CGO · 协议式微内核（能力经 `*Capable` 接口自动发现）。

> 分层详解与 ReAct 数据流见 [docs/架构图.md](../docs/架构图.md) 与 [docs/concepts/ReAct循环.md](docs/concepts/ReAct循环.md)。
> 英文完整走查见 [docs/AP-开发手册-EN.md](docs/AP-开发手册-EN.md)。

---

## 🟦 TypeScript SDK

为 Node.js 开发者提供与 Go 功能对等的 TypeScript SDK（`@agentprimordia/sdk`，Node 18+）：

```bash
npm install @agentprimordia/sdk
```

> Go 是唯一权威实现面，TS 侧由跨语言 API 检查保持同步（一份契约、一套基准）。
> 详见 [sdk/typescript/](../sdk/typescript/)。

---

## 📦 项目结构

```
agentprimordia/
├── cmd/ap/              # CLI (init/run/debug/loop/cluster/marketplace/autonomy/skill/a2a/realtime/...)
├── internal/
│   ├── agent/           # ReAct 引擎 + 编排 + 协议式微内核
│   │   ├── a2a/         # Agent2Agent 协议
│   │   ├── planning/ reflection/ tool_learning/ learning/
│   ├── llm/ memory/ tools/ pool/ persist/ orchestration/
│   ├── guardrail/ security/ metrics/ observability/ events/ config/
│   └── ...              # admin / debugger / chaos / health / logger / governance / ...
├── pkg/                 # 公共 API 重导出
├── ecosystem/           # 插件 / 示例 / 模板
├── operator/            # Kubernetes Operator (CRD + HPA)
├── bench/               # 性能基准 / 评测套件 / soak
├── docs/                # 概念 / 指南 / API 参考 / 路线图
└── ../sdk/typescript/   # TypeScript SDK（仓库根，另有 ../sdk/python、../sdk/rust）
```

---

## 🧪 运行示例

```bash
# 源码检出后在 agentprimordia/ 目录下执行
make run-hello          # 最简 Agent
make run-multi          # 多 Agent 并发
make run-production     # RAG + 弹性调用 + 事件系统

go run ./ecosystem/examples/a2a-interop/       # v3.5 跨生态委托
go run ./ecosystem/examples/skill-evolution/   # v3.4 习得→复用
go run ./ecosystem/examples/resilient-provider/# 重试 / 降级 / 熔断
```

> 全部示例见 [ecosystem/examples/](ecosystem/examples/)。

---

## 📚 文档

- **入门（英文）** — [five-minutes.md](docs/getting-started/five-minutes.md) ·
  [installation.md](docs/getting-started/installation.md) ·
  [first-agent.md](docs/getting-started/first-agent.md)
- **开发手册（英文）** — [AP-开发手册-EN.md](docs/AP-开发手册-EN.md)
- **入门（中文）** — [docs/getting-started/](docs/getting-started/) ·
  [docs/AP-开发手册.md](docs/AP-开发手册.md)
- **概念** — [concepts/](docs/concepts/)（react-loop / memory / orchestration / tools / a2a / autonomy / skills / realtime）
- **指南与 cookbook** — [guides/](docs/guides/) · [cookbook/](docs/cookbook/)（RAG / 多 Agent / 代码审查机器人 / K8s 部署 / WASM 沙箱）
- **API 参考** — [docs/api/](docs/api/) · [docs/api-reference/](docs/api-reference/)
- **兼容性与稳定性分级** — [版本规范.md](docs/版本规范.md)
- **路线图与变更** — [V7路线图.md](../docs/V7路线图.md) · [CHANGELOG.md](../docs/CHANGELOG.md) · [CHANGELOG-v7.5.md](docs/CHANGELOG-v7.5.md)
- **性能与部署** — [benchmarks/](docs/benchmarks/) · [部署指南.md](../docs/部署指南.md) · [DEVELOPMENT.md](DEVELOPMENT.md)

---

## ⚖️ 诚实的对比

| | AgentPrimordia | LangChain | AutoGen |
|---|---|---|---|
| **语言** | Go + TypeScript | Python | Python |
| **CGO 依赖** | ❌ 零 CGO | — | — |
| **弹性调用** | ✅ 内建重试+降级+熔断 | 需自行实现 | 需自行实现 |
| **记忆系统** | ✅ SQLite+FTS5+向量+RAG | 需外接 | 基础支持 |
| **安全沙箱** | ✅ ACL+Sandbox+逃逸防护 | ❌ | ❌ |
| **自治 / 技能 CLI** | ✅ 真实接线（v7.4 起） | ❌ | ❌ |
| **Prometheus** | ✅ 内建 | 需自行集成 | 需自行集成 |
| **结构化错误码** | ✅ 35 个错误码 | ❌ | — |
| **TypeScript SDK** | ✅ 官方对等 | 社区 | ❌ |
| **单二进制部署** | ✅ | ❌ | ❌ |

---

<p align="center">
  <strong>万物之源，智能之始</strong><br>
  用 AgentPrimordia 构建你自己的 AI Agent · <a href="./README.md">English</a>
</p>
