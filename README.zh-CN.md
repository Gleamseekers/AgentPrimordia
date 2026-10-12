# AgentPrimordia

**用 Go 构建 AI Agent — 编译型、并发原生、单二进制、零 CGO**

[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26+-00ADD8.svg)](https://golang.org)
<a href="https://github.com/Gleamseekers/AgentPrimordia/releases"><img src="https://img.shields.io/github/v/release/Gleamseekers/AgentPrimordia?style=flat-square" alt="最新发布"></a>
<a href="https://github.com/Gleamseekers/AgentPrimordia/actions/workflows/ci.yml"><img src="https://github.com/Gleamseekers/AgentPrimordia/actions/workflows/ci.yml/badge.svg" alt="CI"></a>

<p align="center">
  <img src="agentprimordia/docs/ap-architecture.svg" alt="AgentPrimordia Architecture" width="90%">
  <br>
  <sub>交互式架构图（缩放 / 主题切换 / 引导视图）：
  <a href="agentprimordia/docs/ap-architecture-v7.3.html">ap-architecture-v7.3.html</a></sub>
</p>

```bash
# 从模块代理直接安装 CLI — 无需克隆、无需 replace
go install github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/cmd/ap@latest

# 脚手架创建并运行第一个 Agent — 无需 API key
ap init my-agent --template quickstart
cd my-agent
go mod tidy
go run .
```

**约一分钟。零配置。无需 API Key。** Agent 已经在跑了：未设置 `AP_LLM_API_KEY` 时脚手架项目
走 Demo 模式——ReAct 循环全是真的，只有模型是模拟的。

English: [README.md](README.md)。

---

## 为什么选 AgentPrimordia

| | AgentPrimordia | 其他框架 |
|---|---|---|
| **上手** | `go install` + `ap init`，然后 `go run .` — 首跑不需要 key | 装依赖、配 key、写 boilerplate |
| **语言** | Go — 编译型、并发原生、单二进制 | Python — 运行时依赖负担 |
| **部署** | 单二进制 + SQLite，零基础设施 | Docker + Redis + Postgres + … |
| **互联** | A2A 协议原生，并带自检（`ap a2a interop-check`） | 各自为战 |
| **证据** | 能力声明可实测、可查证；门为预注册 | 营销表格 |

## 核心能力

- **ReAct 引擎** — 推理 + 行动循环，每轮检查点、崩溃恢复、流式输出、上下文裁剪、30+ 生命周期钩子
- **可审视的学习闭环** — 工具成功率与任务结果被记录并呈现（`ap profile`、`ap profile history`）；习得的技能持久化后重新注入循环。能力**增益**逐项实测，从不整体打包承诺
- **多模式编排** — Pipeline / Handoff / DAG / GroupChat / Debate / MapReduce，外加有界并发、会话隔离与自动扩缩的 Pool 调度器
- **工具系统** — filesystem / shell / web / api / database / code-execution 内置工具（带 Scope 校验）、MCP 集成、签名验证的插件市场
- **三层记忆** — SQLite（FTS5）+ 向量库（HNSW）+ RAG 混合检索（RRF 融合）
- **10+ LLM Provider** — OpenAI / Anthropic / Gemini / Ollama（本地）/ Azure / Qwen / GLM / DeepSeek / Mistral / Cohere，统一接口之上带重试、降级链与熔断器
- **A2A 协议** — Agent 发现、任务委托、结果回传的标准化协议（`ap a2a interop-check`）
- **安全防护** — ACL / Sandbox / Guardrails（注入、PII、主题）/ 路径穿越与 symlink 逃逸防护；agent 生成代码只能在经签名验证的 WASM 沙箱内执行
- **可观测性** — Prometheus 指标、OpenTelemetry OTLP 导出、35 个结构化错误码、Grafana 仪表盘
- **K8s Operator** — `AgentDeployment` CRD 声明式部署 + HPA 自动扩缩容

## 快速开始

### 安装

```bash
# 从模块代理安装（推荐 — 框架就是普通 Go 模块）
go install github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/cmd/ap@latest

# 或从源码检出构建（在仓库根目录）
go build -o ap ./agentprimordia/cmd/ap/
```

### 约一分钟首跑（无需 API Key）

```bash
ap init my-agent --template quickstart
cd my-agent
go mod tidy
go run .          # 或 ap run（构建 + 热重载）
```

就这样。`ap init` 写入 `main.go`、`.ap.yaml`、`go.mod` 与 `.gitignore`；`go run .` 直接编译运行。
未设置 `AP_LLM_API_KEY` 时走 Demo 模式——关键词匹配 Provider，让你在花掉第一个 token 前
把真实循环跑通。

`ap start my-agent` 是连依赖解析一起做的一条命令版本，但它目前需要能探测到框架源码检出
（仓库内，或经 `AP_ROOT`）；干净机器上请用上面的 `ap init` 路径。

### 配置真实 LLM

```bash
ap config set api-key sk-xxx
ap config set provider openai
ap config set model gpt-4o
go run .           # 这次用真实模型
```

环境变量优先于 `.ap.yaml`：`AP_LLM_API_KEY`、`AP_LLM_BASE_URL`、`AP_LLM_MODEL`。
任意 OpenAI 兼容端点均可。

### 查看 Agent 成长报告

```bash
ap profile                 # 能力画像：成功率、强项、弱项
ap profile history         # 成长事件历史
```

```
Agent Growth Profile
====================

Overall: 78% success (14/18 tasks), avg 4.2 turns

Top Capabilities:
  1. code-review     92% (23/25)  improving ↑
  2. data-analysis   85% (17/20)  stable →

Weak Areas:
  1. sql-migration   45% (5/11)   declining ↓
```

*（示例输出，数字为演示用）*

### 打开学习仪表盘

Studio 是框架模块内的独立二进制，默认监听 `:8090`，学习仪表盘位于 `/dashboard/learning`：

```bash
# 在仓库根目录执行
go run ./agentprimordia/cmd/studio          # 默认接真实引擎
go run ./agentprimordia/cmd/studio -demo    # 内置 demo 数据
# 打开 http://localhost:8090/dashboard/learning
```

能力雷达图、成长曲线、领域详情 — Agent 记录了什么，看得见。

## 代码示例

### 最简 Agent

```go
provider, err := ap.NewOpenAIProvider(ap.Config{APIKey: os.Getenv("AP_LLM_API_KEY"), Model: "gpt-4o-mini"})
if err != nil {
    log.Fatal(err)
}

agent, err := ap.NewAgent("HelloAgent", "你是一个智能助手", provider, ap.WithMaxTurns(10))
if err != nil {
    log.Fatal(err)
}

resp, err := agent.Run(ctx, ap.UserMessage("你好"))
if err != nil {
    log.Fatal(err)
}
fmt.Println(resp.Content)
```

### 带工具 + 记忆的 Agent

```go
registry, err := ap.DefaultToolkit(ap.ToolkitConfig{RootDir: ".", EnableFS: true, EnableShell: true})
if err != nil {
    log.Fatal(err)
}
memory, err := ap.WithInMemory()
if err != nil {
    log.Fatal(err)
}
defer memory.Close()

agent, err := ap.NewAgent("CodingAssistant", "", provider,
    ap.WithMaxTurns(20),
    ap.WithToolkit(registry),
    ap.WithMemory(memory),
)
if err != nil {
    log.Fatal(err)
}

resp, err := agent.Run(ctx, ap.UserMessage("审查 main.go 的缺陷"))
if err != nil {
    log.Fatal(err)
}
fmt.Println(resp.Content)
```

### 多 Agent 并发调度

```go
pool := ap.NewPool(ap.PoolConfig{MaxConcurrency: 5})
defer pool.Close()
pool.SetModel(provider)

results, err := pool.Dispatch(ctx, []ap.TaskConfig{
    {ID: "task-1", Title: "代码分析", Prompt: "分析 main.go"},
    {ID: "task-2", Title: "运行测试", Prompt: "执行 go test"},
})
if err != nil {
    log.Fatal(err)
}
for _, r := range results {
    fmt.Println(r.TaskID, r.Status, r.Duration)
}
```

### DAG 编排

```go
dag, err := ap.NewDAGBuilder("pipeline").
    Node("collect", func(ctx context.Context, in string) (string, error) { return "raw: " + in, nil }).
    Node("analyze", func(ctx context.Context, in string) (string, error) { return "analyzed: " + in, nil }).
    Node("report", func(ctx context.Context, in string) (string, error) { return "report: " + in, nil }).
    Edge("collect", "analyze").
    Edge("analyze", "report").
    Build()
if err != nil {
    log.Fatal(err)
}

result, err := dag.Run(ctx, "分析销售数据")
if err != nil {
    log.Fatal(err)
}
fmt.Println(result.Succeeded, "/", result.TotalNodes, "nodes")
```

### A2A 互联

```go
// 通过 A2A 协议把任务委托给对端 Agent
client := ap.NewOpenInteropClient("https://peer.example.com")

task, err := client.SendTask(ctx, ap.OpenMessage{
    Role:  "user",
    Parts: []ap.OpenPart{{Type: "text", Text: "总结这个仓库"}},
})
if err != nil {
    log.Fatal(err)
}
fmt.Println(task.ID)
```

若要**对外提供**任务受托能力，用实现了 `ap.TaskExecutor`（`Execute` / `Cancel`）的类型注册：
`ap.NewOpenInteropServer(card, cfg).WithExecutor(myExecutor)`。

以上片段均已针对已发布模块（v7.5.1）实际编译并运行验证（未设 API key 时以 Demo 模式运行）。

## 架构

```
┌─────────────────────────────────────────────────────┐
│                  Your Application                    │
├─────────────────────────────────────────────────────┤
│              AgentPrimordia Framework                │
│                                                      │
│  ┌──────────┐ ┌──────────┐ ┌────────┐ ┌─────────┐  │
│  │ ReActLoop│ │   Pool   │ │  DAG   │ │Pipeline │  │
│  └────┬─────┘ └────┬─────┘ └───┬────┘ └────┬────┘  │
│       └────────────┼───────────┼───────────┘        │
│              ┌─────┴─────┐                           │
│              │Tool System│                           │
│              │Built-in + MCP + Plugin                │
│              └───────────┘                           │
│                                                      │
│  ┌──────────┐ ┌──────────┐ ┌────────┐ ┌──────────┐  │
│  │ Memory   │ │ Learning │ │ Metrics│ │Guardrails│  │
│  │SQLite+FTS│ │SelfModel │ │OTel/   │ │ACL/Sandbox│ │
│  │+Vector   │ │+GrowthLog│ │Prom    │ │+PII      │  │
│  │+RAG      │ │+Studio   │ │        │ │          │  │
│  └──────────┘ └──────────┘ └────────┘ └──────────┘  │
└─────────────────────────────────────────────────────┘
```

## 项目结构

```
AgentPrimordia/
├── agentprimordia/          # 核心框架主模块
│   ├── cmd/ap/              # CLI
│   ├── internal/            # 30+ 内部包
│   │   ├── agent/           # ReAct 引擎 + 微内核（30+ 个子包）
│   │   ├── pool/            # 多 Agent 并发调度
│   │   ├── tools/           # 工具系统
│   │   ├── memory/          # 记忆存储 + SelfModel + GrowthLog
│   │   ├── llm/             # 10+ LLM Provider
│   │   ├── studio/          # Studio 引擎 + 学习面板
│   │   └── ...              # 完整清单见 internal/AGENTS.md
│   ├── pkg/                 # 公共 API
│   ├── operator/            # K8s Operator（独立 Go 模块）
│   ├── bench/               # 性能基准与评测套件
│   └── ecosystem/           # 示例 + 插件 + 模板
├── pgvector/                # pgvector 向量存储扩展
├── gateway/                 # 网关
├── wasm/                    # WASM 沙箱（wazero）
└── sdk/typescript/          # TypeScript SDK（Go 功能对等）
```

## CLI 命令

```bash
ap start my-agent              # 一键创建 + 依赖 + 运行（需框架源码检出）
ap init my-agent               # 创建项目（手动模式）
ap run                         # 编译运行当前项目（--watch 热重载）
ap profile                     # 查看 Agent 成长报告
ap profile history             # 成长事件历史
ap config set api-key sk-xxx   # 配置 API Key
ap debug                       # 调试服务器
ap loop trace                  # 执行追踪
ap test                        # Eval 测试套件
ap mcp list / add / test       # MCP Server 管理
ap plugin install <url>        # 安装插件（签名验证）
ap marketplace install <url>   # 安装 Agent 模板（签名验证）
ap cluster init / join / status / leave / scale
ap autonomy run / list / resume / status
ap skill list / add / remove / verify
ap a2a interop-check           # A2A 协议自检
ap doctor                      # 健康检查
ap completion                  # shell 补全脚本
```

## 示例

| 示例 | 说明 | 运行 |
|------|------|------|
| code-review-agent | 代码审查 Agent（学习闭环） | `go run ./agentprimordia/ecosystem/examples/code-review-agent/` |
| data-analysis-agent | 数据分析 Agent | `go run ./agentprimordia/ecosystem/examples/data-analysis-agent/` |
| a2a-connect | A2A 协议互联 | `go run ./agentprimordia/ecosystem/examples/a2a-connect/` |
| github-issue-triage | GitHub Issue 自动分类 | `go run ./agentprimordia/ecosystem/examples/github-issue-triage/` |
| multi-agent | Pool 并发调度 | `go run ./agentprimordia/ecosystem/examples/multi-agent/` |
| chain-api | 链式 API 最简示例 | `go run ./agentprimordia/ecosystem/examples/chain-api/` |

示例需在仓库根的源码检出中运行。

## 设计哲学

1. **来自生产，服务生产** — 核心模式从生产环境 Agent 架构提炼，不是玩具框架
2. **接口优先** — LLM / Tools / Memory 全部接口解耦，自由替换
3. **零配置起步** — `ap init` + `go run .` 不需要任何配置就能跑起来
4. **实测而非承诺** — 能力声明带证据锚点；未过门的能力以失败记录在案
5. **最小依赖** — 核心零 CGO，仅 SQLite + YAML；gRPC / Redis / etcd / wazero 按需引入
6. **TDD 强制** — Red → Green → Refactor，覆盖率分级门 + 废弃残留门 + API 契约漂移门

## 技术栈

- **Go 1.26+** — 编译型、并发原生、单二进制部署
- **SQLite** — 嵌入式记忆存储（modernc.org/sqlite，纯 Go，零 CGO）
- **gRPC** — A2A 协议传输层
- **wazero** — WASM 沙箱执行（纯 Go，零 CGO）
- **OpenTelemetry** — 可观测性标准

## 我们刻意不宣传的能力

诚实是 API 表面的一部分，以下三项不上头条：

- **世界模型维持 opt-in**：`ap.WithWorldModel()` 默认仍关闭——预注册实验跑完 112 对，
  A 89/112 vs B 95/112（+5.4pp，McNemar p=0.1796），门（+15pp 且 p<0.05）未达，
  翻默认已正式否决。登记见 [docs/降级豁免登记-V7弧线.md](docs/降级豁免登记-V7弧线.md)（E-1）。
- **`EnhancedPlanner` 不是卖点**：其递归分解策略在短程 ReAct 任务上实测
  **反向显著 −47.8pp（p=0.001）**（[docs/v7.2-验收实验报告.md](docs/v7.2-验收实验报告.md)）；
  以 `Experimental` 身份附带规划预算护栏存在。
- **agent 自写工具属研究阻塞**：沙箱一半是真的——agent 生成工件只能在经签名验证的
  WASM 沙箱内执行，宿主零编译零加载；但生成器尚无生产实现。

可以写的差异化：统一 Provider 接口与弹性调用、带检查点的 ReActLoop、三层记忆、Pool 调度、
Sandbox/ACL、A2A 协议互操作、可复现评测文化（预注册门、季度 bootstrap 对比、公开报告）。

## 文档

- **[文档权威索引](docs/文档权威索引.md)** — 260+ 篇文档"该信哪一篇"的唯一入口（版本真值 / 当前事实 / 规划 / 历史）
- **[实验性能力清单](docs/实验性能力清单.md)** — 哪些能力真接线、哪些仅 demo/空壳
- **[英文前门](agentprimordia/README.md)** · [英文入门](agentprimordia/docs/getting-started/five-minutes.md) · [英文开发手册](agentprimordia/docs/AP-开发手册-EN.md)
- [v7.5 发布说明](agentprimordia/docs/CHANGELOG-v7.5.md) · [三战线用户指南](agentprimordia/docs/三战线用户指南.md) · [Demo 视频脚本](agentprimordia/docs/demo-video-script.md)
- [架构设计](docs/架构图.md) · [API 参考](docs/API参考.md) · [版本规范](agentprimordia/docs/版本规范.md)
- [TypeScript SDK](sdk/typescript/README.md) · [入门指南](agentprimordia/ecosystem/docs/getting-started.md) · [CLI 手册](agentprimordia/ecosystem/docs/ap-guide.md) · [最佳实践](agentprimordia/ecosystem/docs/best-practices.md) · [Cookbook](agentprimordia/ecosystem/docs/cookbook/)
- [内部模块清单](agentprimordia/internal/AGENTS.md) · [完整变更记录](docs/CHANGELOG.md)

## 版本历史

| 版本 | 主题 | 亮点 |
|------|------|------|
| **v7.5.1** | 发布链路修正 | 6 平台二进制 + 校验和 + cosign 签名 + SBOM；`go install …@latest` 端到端可用 |
| v7.5 | 安全与质量战役 | Shell 白名单加固（7 条实证攻击链全阻断）、`jsonutil` 并发覆写修复、167 个被禁用测试恢复；模块路径迁至 `/v7`，框架可经 GOPROXY 分发 |
| v7.4 | 接线批次 | OTel 端到端导出、Studio 接真实引擎、签名模板注册表、工具学习器自动装配、技能库入循环、自治 CLI 真实化 |
| v7.3 | 三战线 | 零门槛入口 + 学习闭环体感 + A2A 生态协议卡位 |
| v7.2 | A/B 验收 | 全量验收实验（P2 EnhancedPlanner 反向显著——见上） |
| v7.0-7.1 | 五构件 | 蒸馏、缺口工具、常驻运行时、联邦社会；世界模型 opt-in 发布 |
| v6.0 | 大成 | 39 模块冻结，认知引擎 + 自进化闭环 |
| v5.0 | 均衡 | 真实接线 + 多模态 + 分布式 + 平台化 |
| v4.0 | 稳定 | 契约锁定 + 性能大版 + 兼容性收紧 |
| v1-3 | 孵化 | 核心引擎 + 微内核 + 双语言 + 生态 |

完整历史见 [CHANGELOG](docs/CHANGELOG.md)。

## License

Apache-2.0 © AgentPrimordia Contributors
