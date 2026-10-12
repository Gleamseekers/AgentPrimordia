<p align="center">
  <img src="https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&style=for-the-badge" alt="Go Version">
  <img src="https://img.shields.io/badge/TypeScript-5.4+-3178C6?logo=typescript&style=for-the-badge" alt="TypeScript">
  <img src="https://img.shields.io/badge/License-Apache--2.0-blue?style=for-the-badge" alt="License">
  <img src="https://img.shields.io/badge/Zero_CGO-✓-brightgreen?style=for-the-badge" alt="Zero CGO">
  <a href="https://github.com/Gleamseekers/AgentPrimordia/releases"><img src="https://img.shields.io/github/v/release/Gleamseekers/AgentPrimordia?style=for-the-badge" alt="Latest release"></a>
  <a href="https://github.com/Gleamseekers/AgentPrimordia/actions/workflows/ci.yml"><img src="https://github.com/Gleamseekers/AgentPrimordia/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
</p>

<h1 align="center">⚡ AgentPrimordia</h1>

<p align="center">
  <strong>Origin of all things, beginning of intelligence</strong><br>
  A production-grade AI agent framework — Go first, with a feature-parity TypeScript SDK
</p>

<p align="center">
  <a href="#-quick-start">Quick Start</a> ·
  <a href="#-why-agentprimordia">Why</a> ·
  <a href="#-core-features">Features</a> ·
  <a href="#-architecture">Architecture</a> ·
  <a href="#-documentation">Docs</a> ·
  <a href="./README.zh-CN.md">中文</a>
</p>

---

## 🚀 Quick Start

### 1. Install the CLI

```bash
go install github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/cmd/ap@latest

ap version        # verify
```

The framework is a normal Go module — it resolves through `GOPROXY`, no clone required.
Requires Go 1.26 or newer (the `toolchain` directive pulls it automatically).

### 2. Run your first agent — no API key needed

```bash
ap init my-agent --template quickstart
cd my-agent
go mod tidy
go run .
```

With `AP_LLM_API_KEY` unset the scaffolded project runs in **Demo mode**: a keyword-matching
provider (`ap.NewDemoProvider()`) so you can exercise the full ReAct loop — turns, tool
dispatch, stats — before you own a single token. To switch to a real model:

```bash
export AP_LLM_API_KEY=sk-...
export AP_LLM_BASE_URL=https://api.openai.com/v1   # any OpenAI-compatible endpoint
export AP_LLM_MODEL=gpt-4o-mini
go run .                                          # or: ap run  (build + hot reload)
```

### 3. Or use it as a library

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

> `ap init` ships 7 templates: `quickstart` (recommended) · `basic` · `with-tools` ·
> `agent-with-rag` · `agent-with-cache` · `agent-with-metrics` · `multi-agent`.
> Tool/memory/RAG/multi-agent walkthroughs: [docs/getting-started/](docs/getting-started/).

---

## 💡 Why AgentPrimordia?

| Pain | Answer |
|------|--------|
| 🧩 LLM vendor lock-in — switching models means rewriting code | **One `Provider` interface**, 10+ built-in providers, one-line swap |
| 💥 Flaky APIs — timeouts, rate limits, partial outages | **Resilient provider**: exponential-backoff retry + fallback chain + circuit breaker |
| 🔧 Tool plumbing rebuilt from scratch for every project | **Tool registry + executor** with scope checks, MCP, plugins, 20+ built-ins |
| 🧠 Agents that forget everything between runs | **Three-tier memory**: SQLite (FTS5) + vector store (HNSW) + RAG hybrid fusion |
| 🔄 Hand-rolled goroutines for concurrency | **Pool scheduler**: bounded concurrency + session isolation + autoscaling + events |
| 🔒 No guardrail in front of dangerous actions | **ACL + sandbox + path-traversal/symlink escape protection + in-loop guardrails** |
| 📊 Production blind spots | **Prometheus metrics + OpenTelemetry traces + 35 structured error codes + 30+ hooks** |

**In one line**: you write the agent's business logic; the infrastructure is already there.

---

## 🧭 v7.x status (what actually shipped)

Version reality is maintained in [`docs/版本规范.md`](docs/版本规范.md) and
[`docs/能力清单.md`](../docs/能力清单.md); the table below is the short version.

| Release | What shipped |
|---------|--------------|
| **v7.5.1** | Release pipeline fixed: 6-platform binaries + SHA256 checksums + cosign signatures + SBOM, Docker build context, `go install …@latest` works end to end |
| **v7.5** | Security & quality campaign: shell-tool allowlist hardened against 7 verified attack chains, `jsonutil` concurrent-marshal corruption fixed, Anthropic structured-output panic fixed, 167 disabled tests restored; coverage 78.8% → 81.1%; module path moved to `github.com/Gleamseekers/AgentPrimordia/v7` so the framework is distributable via `GOPROXY` |
| **v7.4** | Wiring batch: OTel end-to-end export, Studio bound to real engines, template registry persistence + signed remote protocol, tool-learner auto-assembly, correlation store, skill-library persistence + in-loop injection, autonomy CLI real execution |
| **v7.3** | Zero-barrier entry (`ap start` / `ap init` templates), visible learning loop, A2A ecosystem interop |
| **v7.0–v7.2** | Five-component GA line (distillation, gap tools, resident runtime, federation, …) plus the evaluation campaign that produced the evidence below |

### What we deliberately do **not** claim

Honesty is part of the API surface. Three capabilities are intentionally kept off the
marketing table:

- **World model is opt-in.** `WithWorldModel()` stays off by default: the pre-registered
  experiment ran 112 pairs, A 89/112 vs B 95/112 (+5.4pp, McNemar p=0.1796), gate
  (+15pp and p<0.05) not met — flipping the default was formally rejected
  ([docs/降级豁免登记-V7弧线.md](../docs/降级豁免登记-V7弧线.md), E-1).
- **`EnhancedPlanner` is not a selling point.** Its recursive decomposition measured
  **−47.8pp (p=0.001)** on short-horizon ReAct tasks in the v7.2 acceptance campaign
  ([docs/v7.2-验收实验报告.md](../docs/v7.2-验收实验报告.md)). It ships as `Experimental`
  behind planning-budget guardrails, not as a headline feature.
- **Agent-authored tools are research-blocked.** The sandbox half is real — agent-generated
  artifacts execute only inside the signed wazero WASM sandbox, and the host never compiles
  or loads agent code — but the *generator* has no production implementation. Do not expect
  an agent to write you a working tool today.

What we do claim: a single provider interface with resilient calling, a ReAct loop with
per-turn checkpoints and resume, three-tier memory, pool scheduling, sandbox/ACL,
A2A protocol interop (`ap a2a interop-check`), and a reproducible evaluation culture
(pre-registered gates, quarterly bootstrap comparisons, published reports).

---

## ✨ Core features

| Capability | Notes |
|------------|-------|
| 🧠 **ReAct loop engine** | Reason→Act→Observe, streaming output, context trimming, per-turn checkpoint + crash resume |
| 🛡️ **Resilient calling** | Exponential-backoff retry + fallback chain + circuit breaker on one `Provider` |
| 🧩 **10+ LLM providers** | OpenAI / Anthropic / Gemini / Ollama (local) / Azure / Qwen / GLM / Mistral / Cohere / DeepSeek |
| 🔧 **Tool system** | 20+ built-ins (filesystem / shell / web / api / database / code execution / knowledge + document loaders) + MCP + plugins, `Registry.Register` |
| 🧠 **Three-tier memory** | SQLite FTS5 + HNSW vector store + RAG hybrid retrieval (RRF fusion) |
| 🔄 **Multi-agent scheduling** | Pool: bounded concurrency + session isolation + AutoScaler + event backpressure |
| 🔒 **Security** | ACL + sandbox + path-traversal/symlink escape protection + guardrails (injection / PII / topic) wired into the loop |
| 🪝 **30+ lifecycle hooks** | Audit, alerting, cost tracking at every stage of a run |
| 📊 **Observability** | Prometheus metrics + OpenTelemetry OTLP export + 35 structured error codes |
| 🧭 **Orchestration** | Pipeline / Handoff / Parallel / DAG / GroupChat / Debate / MapReduce |
| 💾 **Checkpoints** | SQLite by default; etcd / Redis backends behind build tags |
| 🤝 **A2A interop** | Agent2Agent protocol (gRPC transport) + `ap a2a interop-check` self-test |
| 🧪 **Reproducible evals** | Pre-registered gates, quarterly bootstrap comparison, published reports |

> Per-capability examples: [docs/index.md](docs/index.md), [docs/concepts/](docs/concepts/),
> [docs/cookbook/](docs/cookbook/).

---

## 🏗️ Architecture

```
┌─────────────────────────────────────────────────┐
│                  pkg/ (public API)               │
├─────────────────────────────────────────────────┤
│  pool/   │  agent/   │  security/  │ concurrency│
│ (调度)   │ (ReAct)   │  (安全)     │  (并发)    │
├──────────┼───────────┼──────────────────────────┤
│  tools/  │  memory/  │  events/   │  metrics/   │
│ (工具)   │ (记忆)    │  (事件)    │  (指标)     │
├──────────┴───────────┴────────────┴─────────────┤
│              llm/ (provider abstraction)         │
├─────────────────────────────────────────────────┤
│            persist/ (checkpoint store)           │
└─────────────────────────────────────────────────┘
```

**Design principles**: interface-driven · composition over inheritance · resilience first ·
zero CGO · protocol-style microkernel (capabilities are discovered via `*Capable` interfaces).

> Layering detail and the ReAct data flow: [docs/架构图.md](../docs/架构图.md) and
> [docs/concepts/ReAct循环.md](docs/concepts/ReAct循环.md).
> Complete English walkthrough: [docs/AP-开发手册-EN.md](docs/AP-开发手册-EN.md).

---

## 🟦 TypeScript SDK

A feature-parity TypeScript SDK for Node.js developers (`@agentprimordia/sdk`, Node 18+):

```bash
npm install @agentprimordia/sdk
```

> Go is the single authoritative implementation surface; the TS side is kept in lock-step by
> the cross-language API check (one contract, one benchmark set). See
> [sdk/typescript/](../sdk/typescript/).

---

## 📦 Project structure

```
agentprimordia/
├── cmd/ap/              # CLI (init/run/debug/loop/cluster/marketplace/autonomy/skill/a2a/realtime/…)
├── internal/
│   ├── agent/           # ReAct engine + orchestration + protocol microkernel
│   │   ├── a2a/         # Agent2Agent protocol
│   │   ├── planning/ reflection/ tool_learning/ learning/
│   ├── llm/ memory/ tools/ pool/ persist/ orchestration/
│   ├── guardrail/ security/ metrics/ observability/ events/ config/
│   └── ...              # admin / debugger / chaos / health / logger / governance / …
├── pkg/                 # the single public API surface
├── ecosystem/           # plugins / examples / templates
├── operator/            # Kubernetes Operator (CRD + HPA)
├── bench/               # benchmarks, eval suites, soak tests
├── docs/                # concepts / guides / API reference / roadmaps
└── ../sdk/typescript/   # TypeScript SDK (repo root; python/rust clients alongside)
```

---

## 🧪 Run examples

```bash
# from the agentprimordia/ directory of a source checkout
make run-hello          # minimal agent
make run-multi          # concurrent agents
make run-production     # RAG + resilient calling + events

go run ./ecosystem/examples/a2a-interop/       # cross-ecosystem delegation over A2A
./ecosystem/examples/a2a-handoff/run.sh        # cross-process handoff: server + client, zero credentials
go run ./ecosystem/examples/skill-evolution/   # skills learned → reused
go run ./ecosystem/examples/resilient-provider/# retry / fallback / circuit breaker
```

> All examples: [ecosystem/examples/](ecosystem/examples/).

---

## 📚 Documentation

- **Getting started (English)** — [five-minutes.md](docs/getting-started/five-minutes.md) ·
  [installation.md](docs/getting-started/installation.md) ·
  [first-agent.md](docs/getting-started/first-agent.md)
- **Complete handbook (English)** — [AP-开发手册-EN.md](docs/AP-开发手册-EN.md)
- **入门（中文）** — [docs/getting-started/](docs/getting-started/) ·
  [docs/AP-开发手册.md](docs/AP-开发手册.md)
- **Concepts** — [concepts/](docs/concepts/) (ReAct loop / memory / orchestration / tools / A2A / autonomy / skills / realtime)
- **Guides & cookbook** — [guides/](docs/guides/) · [cookbook/](docs/cookbook/) (RAG / multi-agent / code-review bot / K8s / WASM sandbox)
- **API reference** — [api/](docs/api/) · [api-reference/](docs/api-reference/)
- **Compatibility & stability tiers** — [版本规范.md](docs/版本规范.md)
- **Roadmaps & changelogs** — [V7路线图.md](../docs/V7路线图.md) · [CHANGELOG.md](../docs/CHANGELOG.md) · [CHANGELOG-v7.5.md](docs/CHANGELOG-v7.5.md)
- **Performance & deployment** — [benchmarks/](docs/benchmarks/) · [部署指南.md](../docs/部署指南.md) · [DEVELOPMENT.md](DEVELOPMENT.md)

---

## ⚖️ Honest comparison

| | AgentPrimordia | LangChain | AutoGen |
|---|---|---|---|
| **Language** | Go + TypeScript | Python | Python |
| **CGO** | ❌ none | — | — |
| **Resilient calling** | ✅ built in (retry + fallback + breaker) | DIY | DIY |
| **Memory** | ✅ SQLite + FTS5 + vector + RAG | external | basic |
| **Security** | ✅ ACL + sandbox + escape protection | ❌ | ❌ |
| **Autonomy / skills CLI** | ✅ real, wired since v7.4 | ❌ | ❌ |
| **Prometheus** | ✅ built in | integrate yourself | integrate yourself |
| **Structured error codes** | ✅ 35 codes | ❌ | — |
| **TypeScript SDK** | ✅ official parity | community | ❌ |
| **Single-binary deploy** | ✅ | ❌ | ❌ |

---

<p align="center">
  <strong>Origin of all things, beginning of intelligence</strong><br>
  Build your own AI agent with AgentPrimordia · <a href="./README.zh-CN.md">中文说明</a>
</p>
