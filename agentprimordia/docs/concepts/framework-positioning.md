# Framework Positioning

This page answers three questions as plainly as we can: **what AgentPrimordia is,
what it is not, and when you should not use it.** Every number below points at its
evidence; where our evidence is missing, we say so instead of filling the gap with
marketing.

## What AgentPrimordia is

- **A pure-Go agent framework.** One binary, CGO-free builds as a tested property
  (assertion A2 builds the CLI with `CGO_ENABLED=0` and scans the symbol table),
  and a short dependency whitelist enforced in CI — the default rule is standard
  library only.
- **A ReActLoop engine with a real tool system.** The loop thinks → calls tools →
  observes, bounded by a turn budget, with input/output guardrails wired in by
  default (injection detection, PII, topic filters, sensitive-tool interception).
- **Three-layer memory.** SQLite FTS5 full-text store → vector store → RAG
  pipeline, behind one `Memory` interface with in-memory and SQLite backends.
- **Pool scheduling.** Concurrent multi-agent execution with session management
  and retries.
- **Two A2A protocol faces.** An ap-native gRPC face (Stable) for
  AgentPrimordia-to-AgentPrimordia traffic, and an open interop face
  (JSON-RPC 2.0 over HTTP + SSE, Experimental) for third-party agents — see
  [Multi-Agent Interop](multi-agent-interop.md) for the client tables and the
  verified limitations.
- **A reproducible-evaluation culture.** Experiment plans are pre-registered
  before data is collected, question sets are frozen with sha256 manifests,
  paired A/B runs are sized by power analysis and judged with McNemar, holdout
  discipline applies — and **negative results are published and archived instead
  of quietly dropped.** The [E-1 world-model final report](../../../docs/E-1-世界模型最终报告.md)
  is the reference instance: 112 pre-registered pairs, +5.4pp, McNemar p=0.1796,
  gate missed — so the "flip the default" proposal was formally rejected (D1
  closed) and the full report, including its disclosure that the official
  summary's counting rule could not be reproduced, stays in the repository.

## What AgentPrimordia is not

- **Not a cognitive-gain guarantee.** Across the v7.1–v7.2 rounds, ten paired
  A/B experiments were registered and executed (six in v7.1 — one aborted
  mid-run for cost — and four in v7.2). **None met its pre-registered gate, and
  the only statistically significant outcome went backwards**: EnhancedPlanner at
  −47.8pp, p=0.001 (v7.2 P2). The world model
  stays opt-in after E-1's negative result (D1 closed, 2026-10-12); the gap-tool
  and active-arm propositions came in at −11.9pp (p=0.11) and −1.7pp (p=0.83)
  respectively. Treat every capability claim in this project as
  "evidence pending" unless a linked report says otherwise — that is the culture,
  not false modesty.
- **Not a full-stack suite.** The public surface is deliberately being narrowed:
  the 8.0 scope decision removes or moves a marked set of `pkg/` exports (each
  carries a stable `[v8.0-cut]` token), deletes the EnhancedPlanner family
  (falsified by our own experiment), and moves the module path `/v7` → `/v8`.
  The item-by-item decision draft is being prepared at
  `deliverables/gstack/8.0-范围决定稿-2026-10-12.md`; the candidate list and
  rationale live in the [version roadmap §4](../../../deliverables/gstack/版本路线-7.5.2到8.0-2026-10-10.md).
  `Experimental` labels mean what they say — check the
  [experimental capabilities registry](../../../docs/实验性能力清单.md) before
  building on anything marked Experimental.
- **Not a Python ecosystem port.** There is no LangChain compatibility layer and
  no notebook-first workflow. The APIs, idioms, and module path are Go-first. If
  your system is mostly Python glue, this framework will fight you — see below.

## Where it sits against alternatives

Honest preamble: **we have not run head-to-head benchmarks** against the projects
below. The differences listed are architectural facts verified in their public
documentation as of 2026-10, not measured comparisons.

| | eino (ByteDance / CloudWeGo) | AgentPrimordia |
|---|---|---|
| Middleware ecosystem | Broad ADK middleware ecosystem (Skill / ToolSearch / PatchToolCalls / AgentsMD / PlanTask) | Deliberately narrow — peripheral exports are being cut for 8.0 because parity does not attract users |
| Code execution | Cloud sandbox backend (Ark Agentkit, Volcengine remote sandbox) | Local, zero-dependency wazero sandbox with an ed25519 signature gate and deterministic CI assertions |
| API stability | Still breaking per minor version at v0.9, with dedicated migration guides | Short API freeze since 7.6; breaking changes batched once at 8.0 |
| Evaluation | — | Pre-registered plans, frozen question sets, published negative results |
| Interop | A2A within the eino ecosystem | Pure-Go A2A dual faces plus a conformance self-check report |

On sandboxes we claim only the narrow, verified difference: eino offers a
**cloud** sandbox bound to a vendor; AgentPrimordia offers a **local,
dependency-free** one with signature pinning and machine-checked boundary
assertions (see [Security Model](security-model.md)). Neither is "the sandbox the
other one lacks".

**langchain-go** is an ecosystem port of the Python LangChain surface to Go. It
does not ship deterministic security-execution invariants comparable to A1–A8 in
its published contract — to be precise about the limit of our claim: we have not
audited its security, and the difference we do claim is what each project's own
published contract guarantees.

## When you should not use AgentPrimordia

- **You want the Python ecosystem as glue** — LangChain/LlamaIndex plugins,
  Jupyter-centric workflows. Use the Python tools directly; a Go port of that
  experience is explicitly not what this is.
- **You want a managed or cloud sandbox out of the box.** eino's Ark Agentkit
  path is the honest choice; our sandbox is local and operator-run.
- **You want a guarantee of cognitive gains.** We publish negative A/B results;
  no capability here comes with a measured-gain promise.
- **You want a batteries-included platform.** This is a framework, and 8.0
  narrows its surface further, not wider.
- **You need agents that write and run their own tools in production.** The
  generator is research-blocked; the supported pattern is developers bringing
  signed WASM artifacts into the sandbox supply chain.
- **You need A2A task-level checkpoint/resume.** It is not in the current
  protocol face — see the known limitations in
  [Multi-Agent Interop](multi-agent-interop.md).
- **You need long-horizon state tracking with measured gains.** The world model
  is opt-in and its paired evidence is negative so far (E-1: +5.4pp, p=0.1796,
  gate +15pp/p<0.05).

## See also

- [Security Model](security-model.md) — INV-0, assertions A1–A8, the signature gate, and the honest boundaries
- [Multi-Agent Interop (A2A)](multi-agent-interop.md) — the two protocol faces
- [AP Development Handbook (EN)](../AP-开发手册-EN.md) — terminology baseline
- [Version specification](../版本规范.md) — stability labels and the version truth chain
- [Capability inventory](../../../docs/能力清单.md) / [experimental capabilities](../../../docs/实验性能力清单.md) — what is real, wired, partial, or reserved
- [Version roadmap 7.5.2 → 8.0](../../../deliverables/gstack/版本路线-7.5.2到8.0-2026-10-10.md) — the 8.0 scope candidates and the evidence behind them
- [E-1 world-model final report](../../../docs/E-1-世界模型最终报告.md) — the negative result we publish instead of hiding
