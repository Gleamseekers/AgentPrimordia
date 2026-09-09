# AgentPrimordia

**用 Go 构建越用越强的 AI Agent**

[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26+-00ADD8.svg)](https://golang.org)
[![Version](https://img.shields.io/badge/version-7.3.0-2ea44f.svg)](agentprimordia/docs/CHANGELOG-v7.3.md)
[![Tests](https://img.shields.io/badge/tests-538%20files-green.svg)](agentprimordia/internal/)

```
go install agentprimordia/cmd/ap@latest
ap start my-agent
```

**30 秒。零配置。无需 API Key。** Agent 已经在跑了。

---

## 为什么选 AgentPrimordia

| | AgentPrimordia | 其他框架 |
|---|---|---|
| **上手** | `ap start` 一条命令 | 装依赖、配 key、写 boilerplate |
| **学习** | 越用越强，能力可视化 | 每次对话从零开始 |
| **语言** | Go — 编译型、并发原生、单二进制 | Python — 运行时依赖地狱 |
| **部署** | 单二进制 + SQLite，零基础设施 | Docker + Redis + Postgres + ... |
| **互联** | A2A 协议原生支持 | 各自为战 |

## 核心能力

- **ReAct 引擎** — Reasoning + Acting 循环，20+ 生命周期钩子
- **学习闭环** — SelfModel 能力画像 + 成长事件日志 + Studio 可视化面板
- **多模式编排** — Pipeline / Handoff / DAG / GroupChat / MapReduce
- **工具系统** — FileSystem / Shell / Web / Database 内置，MCP 协议集成，插件市场扩展
- **三层记忆** — SQLite FTS5 + Vector Store + RAG Pipeline 混合检索
- **10+ LLM Provider** — OpenAI / Anthropic / Gemini / Ollama / Azure / Qwen / GLM / DeepSeek 等
- **A2A 协议** — Agent 间发现、任务委托、结果回传的标准化协议
- **安全防护** — ACL / Sandbox / Guardrails / PII 检测 / 路径遍历防护
- **可观测性** — Prometheus Metrics / OpenTelemetry / Grafana Dashboard
- **K8s Operator** — AgentDeployment CRD 声明式部署 + HPA 自动扩缩容

## 快速开始

### 安装

```bash
# 从源码编译
cd agentprimordia && go build -o ap ./cmd/ap/

# 或直接用 go install
go install agentprimordia/cmd/ap@latest
```

### 30 秒体验（无需 API Key）

```bash
ap start my-agent
```

就这样。`ap start` 会自动创建项目、安装依赖、启动 Agent。没有 API Key？自动进入 Demo 模式，用关键词匹配模拟 LLM 响应，让你先体验完整流程。

### 配置真实 LLM

```bash
ap config set api-key sk-xxx
ap config set provider openai
ap config set model gpt-4o
ap start my-agent    # 这次用真实 LLM
```

### 查看 Agent 成长报告

```bash
ap profile
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

### 打开学习仪表盘

```bash
ap studio
# 浏览器打开 http://localhost:8080/dashboard/learning
```

能力雷达图、成长曲线、领域详情 — 你的 Agent 在变强，而且你能看见。

## 代码示例

### 最简 Agent（3 行）

```go
agent, _ := ap.NewAgent("HelloAgent", "你是一个智能助手", provider, ap.WithMaxTurns(10))
resp, _ := agent.Run(ctx, ap.UserMessage("你好"))
fmt.Println(resp.Content)
```

### 带工具 + 记忆的 Agent

```go
registry, _ := ap.DefaultToolkit(ap.ToolkitConfig{EnableFS: true, EnableShell: true})
memory, _ := ap.WithInMemory()
defer memory.Close()

agent, _ := ap.NewAgent("CodingAssistant", "", provider,
    ap.WithMaxTurns(20),
    ap.WithToolkit(registry),
    ap.WithMemory(memory),
)
```

### 多 Agent 并发调度

```go
pool := ap.NewPool(ap.PoolConfig{MaxConcurrency: 5})
defer pool.Close()
pool.SetModel(provider)

results, _ := pool.Dispatch(ctx, []ap.TaskConfig{
    {ID: "task-1", Title: "代码分析", Prompt: "分析 main.go"},
    {ID: "task-2", Title: "运行测试", Prompt: "执行 go test"},
})
```

### DAG 编排

```go
dag, _ := ap.NewDAGBuilder("pipeline").
    Node("collect", collectFn).
    Node("analyze", analyzeFn).
    Node("report", reportFn).
    Edge("collect", "analyze").
    Edge("analyze", "report").
    Build()

result, _ := dag.Run(ctx, "分析销售数据")
```

### A2A 互联

```go
// Agent 间通过 A2A 协议通信
server, _ := ap.NewOpenInteropServer(card, cfg)
server.WithExecutor(myTaskExecutor)

client, _ := ap.NewOpenInteropClient(targetURL)
task, _ := client.SendTask(ctx, &ap.TaskSendRequest{...})
```

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
│              │Tool System│                            │
│              │Built-in + MCP + Plugin                 │
│              └───────────┘                            │
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
├── agentprimordia/          # 核心框架（73 个包，1079 个 Go 文件）
│   ├── cmd/ap/              # CLI（20 个子命令）
│   ├── internal/            # 29 个内部包
│   │   ├── agent/           # ReAct 引擎 + 微内核（32 个子包）
│   │   ├── pool/            # 多 Agent 并发调度
│   │   ├── tools/           # 工具系统
│   │   ├── memory/          # 记忆存储 + SelfModel + GrowthLog
│   │   ├── llm/             # 10+ LLM Provider
│   │   ├── studio/          # Studio 引擎 + 学习面板
│   │   └── ...              # 完整清单见 internal/AGENTS.md
│   ├── pkg/                 # 公共 API
│   ├── operator/            # K8s Operator
│   ├── bench/               # 性能基准测试
│   └── ecosystem/           # 示例 + 插件 + 模板
├── pgvector/                # pgvector 向量存储扩展
├── gateway/                 # 网关
├── wasm/                    # WASM 沙箱（wazero）
└── sdk/typescript/          # TypeScript SDK（Go 功能对等）
```

## CLI 命令

```bash
ap start my-agent              # 一键创建 + 依赖 + 运行
ap run                         # 编译运行（--watch 热重载）
ap profile                     # 查看 Agent 成长报告
ap profile history             # 成长事件历史
ap config set api-key sk-xxx   # 配置 API Key
ap init my-agent               # 创建项目（手动模式）
ap debug                       # 调试服务器
ap loop trace                  # 执行追踪
ap test                        # Eval 测试套件
ap mcp list / add / test       # MCP Server 管理
ap plugin install <url>        # 安装插件
ap doctor                      # 健康检查
ap studio                      # 启动 Studio 面板
```

## 示例

| 示例 | 说明 | 运行 |
|------|------|------|
| code-review-agent | 代码审查 Agent（学习闭环） | `go run ./ecosystem/examples/code-review-agent/` |
| data-analysis-agent | 数据分析 Agent | `go run ./ecosystem/examples/data-analysis-agent/` |
| a2a-connect | A2A 协议互联 | `go run ./ecosystem/examples/a2a-connect/` |
| github-issue-triage | GitHub Issue 自动分类 | `go run ./ecosystem/examples/github-issue-triage/` |
| multi-agent | Pool 并发调度 | `go run ./ecosystem/examples/multi-agent/` |
| chain-api | 链式 API 最简示例 | `go run ./ecosystem/examples/chain-api/` |

## 设计哲学

1. **来自生产，服务生产** — 核心模式从 CodeCast 生产环境提炼，不是玩具框架
2. **接口优先** — LLM / Tools / Memory 全部接口解耦，自由替换
3. **零配置起步** — `ap start` 不需要任何配置就能跑起来
4. **越用越强** — 学习闭环让 Agent 从每次交互中积累经验
5. **最小依赖** — 核心零 CGO，仅 SQLite + YAML；可选 gRPC/Redis/etcd/wazero 按需引入
6. **TDD 强制** — Red → Green → Refactor，538 个测试文件

## 技术栈

- **Go 1.26+** — 编译型、并发原生、单二进制部署
- **SQLite** — 嵌入式记忆存储（modernc.org/sqlite，纯 Go，零 CGO）
- **gRPC** — A2A 协议传输层
- **wazero** — WASM 沙箱执行（纯 Go，零 CGO）
- **OpenTelemetry** — 可观测性标准

## 文档

- [v7.3 发布说明](agentprimordia/docs/CHANGELOG-v7.3.md)
- [三战线用户指南](agentprimordia/docs/三战线用户指南.md)
- [Demo 视频脚本](agentprimordia/docs/demo-video-script.md)
- [架构设计](docs/架构图.md)
- [API 参考](docs/API参考.md)
- [版本规范](agentprimordia/docs/版本规范.md)
- [TypeScript SDK](sdk/typescript/README.md)
- [入门指南](agentprimordia/ecosystem/docs/getting-started.md)
- [CLI 手册](agentprimordia/ecosystem/docs/ap-guide.md)
- [最佳实践](agentprimordia/ecosystem/docs/best-practices.md)
- [Cookbook](agentprimordia/ecosystem/docs/cookbook/) — 客服机器人 / 代码审查 / 数据分析 / RAG
- [内部模块清单](agentprimordia/internal/AGENTS.md)

## 版本历史

| 版本 | 主题 | 亮点 |
|------|------|------|
| **v7.3** | 三战线 | 零门槛入口 + 学习闭环体感 + 生态协议卡位 |
| v7.2 | A/B 验收 | 全量验收实验，P4/P5 正向趋势 |
| v7.0-7.1 | 世界模型 | 实体/关系/因果算子状态图 |
| v6.0 | 大成 | 39 模块冻结，认知引擎 + 自进化闭环 |
| v5.0 | 均衡 | 真实接线 + 多模态 + 分布式 + 平台化 |
| v4.0 | 稳定 | 契约锁定 + 性能大版 + 兼容性收紧 |
| v1-3 | 孵化 | 核心引擎 + 微内核 + 双语言 + 生态 |

完整历史见 [CHANGELOG](docs/CHANGELOG.md)。

## License

Apache-2.0 © AgentPrimordia Contributors
