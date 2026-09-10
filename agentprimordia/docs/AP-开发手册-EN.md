
# AP (AgentPrimordia) Complete Development Handbook

> Primary document for three audiences — **framework users** only need Part I to get productive; **contributors** should also read Parts II–III; when something breaks, go straight to the FAQ (Part IV).
>
> Every command and behavior below was verified against the actual codebase (2026-08-27, go1.26.6). For a deep dive into CLI internals see `AP-CLI开发手册.md`; illustrated beginner tutorials live under `docs/getting-started/`. Chinese edition: `AP-开发手册.md`.

# Part I · Beginner Tutorial


## 1. Prerequisites

| Requirement | Notes |
|---|---|
| Go ≥ 1.26 | Toolchain pinned to 1.26.6 in go.mod; the `toolchain` directive downloads it automatically |
| git | The monorepo is organized via go.work: 5 modules at the root (`agentprimordia` main module, pgvector, operator, gateway, wasm) — clone and go |
| LLM API Key | Pick any: OpenAI / Anthropic / Gemini / Ollama (local, no key) / Azure / DeepSeek / GLM / Cohere / Mistral / Qwen |

Scale expectation: main module has 134 packages, ~130k lines of implementation — the first build spends a few minutes downloading dependencies, then hits the local cache.

## 2. Build ap and the test environment

```
make build            # or: cd agentprimordia && go build -o "$(go env GOPATH)/bin/ap" ./cmd/ap
make test-short       # fast full regression; full gates also include test-race / test-integration
```

Common Makefile targets: build · test · test-race · test-short · test-integration · test-distributed-backends · cover · cover-html · cover-check · api-diff · api-extract · cover-trend · deprecation-check · benchmark.

> ⚠ Cross-directory invocation: never run `go run /abs/path/cmd/ap` from outside a module — you will get *go.mod file not found*. Build a binary first, then run it.

## 3. First agent in five minutes

```
ap init myagent --template quickstart   # add --dry-run to preview output; 7 templates × agent/plugin/provider types
cd myagent
export AP_LLM_API_KEY=sk-xxx             # or put it into .ap.yaml (env var wins)
export AP_LLM_MODEL=gpt-4o               # falls back to the yaml default
ap run                                   # auto build → inject config → start; edits rebuild & restart automatically (hot reload)
```

**The generated project is just 4 files** — reading them all is the fastest way to understand the framework:

| File | Contents |
|------|----------|
| main.go | Minimal public-API sample: ConfigFromEnv reads config → Fatal hint if key missing → NewOpenAIProvider(cfg) → start the ReAct loop. Changing the system prompt gives you your first custom agent |
| .ap.yaml | Project config: llm(provider/model/api_key), memory(backend, path), agent(max_turns, system_prompt); full fields below |
| go.mod / .gitignore | Dependency declaration and ignore rules |
```
# Full .ap.yaml fields (generator defaults)
template: quickstart
llm:
  provider: openai       # openai | anthropic | gemini | ollama | azure | deepseek | qwen
  model: gpt-4o
  # api_key: "sk-xxx"    # prefer env var AP_LLM_API_KEY; keep secrets off disk
memory:
  backend: sqlite        # sqlite | memory
  path: ./data/memory.db
agent:
  max_turns: 20
  system_prompt: "you are a helpful assistant"
```

## 4. Daily development loop

- Save your code and let go: `ap run` ships watchAndRun which watches sources and rebuild/restarts automatically;
- Loop debugging: `ap loop trace / inspect / resume` observe and resume ReAct execution traces;
- Environment checkup: `ap doctor` runs four checks (Go version / project config / API key / dependencies);
- Shell completion: `ap completion` emits completion scripts.

## 5. Core concepts in one minute each

| Concept | One-liner |
|---------|-----------|
| ReAct Loop | Agent engine: LLM thinks → calls tools → observes → loops until final answer; bounded by max_turns |
| Tool | Capability unit: 20+ built-ins carrying scoped permissions (FilesScope/ShellScope), validated before every execution |
| Memory | Conversation memory & knowledge retrieval: SQLite(FTS5+vector) / in-memory / RAG(Hybrid Fusion) / vector store(HNSW) |
| Pool | Concurrent multi-agent scheduling, session management, retries |
| Orchestration | Pipeline / Handoff / DAG / GroupChat / Debate / MapReduce composition patterns |
| Guardrail | Input/output guardrails **wired into the ReAct loop by default**: injection detection / PII / topic filters / sensitive-tool interception; hits are blocked+audited |
| Checkpoint | Resume-from-crash: SQLite by default; etcd/Redis backends compile behind build tags |
| Stability tiers | Four public-API promises Stable/Experimental/Deprecated/Internal that define upgrade cost |

# Part II · Architecture Map


## 6. Monorepo layout and layering discipline

```
/
├── agentprimordia/        # main module (134 packages) + operator K8s submodule
│   ├── cmd/ap/            # the CLI itself
│   ├── pkg/               # the single public API surface (type aliases re-exported + few constructors)
│   ├── internal/          # 30 sub-packages (see §7 cheat sheet)
│   ├── ecosystem/         # examples / plugins / templates (must reach core only through pkg)
│   └── bench/             # eval-ci / llm-bench / soak / suite / self-bootstrap / results
├── pgvector/              # standalone vector extension module (internal must not import pgx directly)
├── gateway/ wasm/         # standalone deployables / WASM execution (wazero, CGO-free)
└── sdk/                   # python / rust / typescript SDKs (not Go modules)
```

**Layering rules (verified zero violations across 134 packages; re-runnable via grep)**:

| Layer | Packages | Forbidden reverse imports |
|-------|----------|---------------------------|
| Top | agent/ (12 capability subpackages incl. a2a/planning/reflection/skills/autonomy) | — |
| Core | llm/ memory/ persist/ tools/ | must not import agent/pool/orchestration/debugger/admin |
| Orchestration | pool/ | sits below tools/; may reference tools/agent and horizontal layers |
| Horizontal | orchestration debugger metrics otel guardrail security audit eval governance etc. | may consume upper layers; must not be referenced by core |

## 7. Subsystem cheat sheet

| Subsystem | Highlights | Most common entry points |
|-----------|------------|--------------------------|
| llm | Many providers behind one interface; three cache tiers (memory/SQLite/enhanced); batching | NewOpenAIProvider(cfg) |
| memory | SQLite default (FTS5 full-text + vector columns); RAG supports Hybrid Fusion reranking | WithInMemory() / NewSQLiteStore(path) / NewRAGStore(mem, embedder) |
| tools | Registry triple entry Register / RegisterMultiple / RegisterPlugin; unified executor dispatch; Scope permission checks up front | Registry.Register(tool) |
| guardrail | Pluggable rules: injection / pii(Trie-accelerated) / topic / output / sensitive_tool; hits are written to audit events | hook attached at Agent construction |
| security | ACL, sandbox, secret management (env/memory/vault backends + AES-GCM encryption abstraction) | secrets abstraction |
| marketplace | Remote plugin protocol: Manifest + cosign(ECDSA P-256) signature verification; reject on failure | ap marketplace install |
| persist | Multi-backend checkpointing: SQLite default; etcd/redis behind build tags | NewSQLiteCheckpointStore(dsn); Save / Load(agentID) / List(sessionID) |
| eval | Single authoritative JSON benchmark set; quarterly bootstrap RunQuarterly vs base + CompareQuarters regression gate | ap test |
| observability | trace→metrics→audit full-chain correlation; otel export bridge | — |

## 8. Multi-language SDKs and cross-language consistency

AP speaks more than Go: `sdk/` hosts typescript / python / rust SDKs, with **Go as the single authoritative implementation surface** — other languages align via one contract and one benchmark set.

### 8.1 TypeScript SDK (sdk/typescript, @agentprimordia/sdk v7.0.0)

- Build `npm run build` (tsup, ESM); typecheck `npm run typecheck`; API-surface check `npm run api-check` (api-extractor);
- Tests `npm run test` (vitest, 111 test files; plus coverage / affected / bench variants); docs site on vitepress;
- `src/` domains mirror Go internals: agent / a2a / cluster / eval / governance / marketplace / … (20+ domains, one directory each);
- Version coupling: one of the four-way bumps is TS package.json + lockfile — bumping Go means bumping TS in the same change.

### 8.2 Dual-line consistency mechanism

`scripts/cross-language-api-check.mjs` is the gatekeeper:

1. Run Go-side api-extract to produce fresh api-contract.json;
2. Load Go↔TS type/function equivalence declarations from cross-language-spec.json;
3. Verify every declared Go symbol exists in the contract and every TS counterpart exists in source; any drift **fails CI**.

Benchmarks follow the same single-authority rule: the authoritative JSON lives on the Go side and the TS side is **regenerated**, never hand-edited — history proved it when v5.1 expanded 60→160 cases and an out-of-sync TS side broke dual-line tests; the lesson is now codified: expanding benchmarks requires regenerating the TS set.

> ⚠ Contributors: whenever you touch pkg exports or benchmark-set structure, add 'run cross-language-api-check + regenerate TS benchmarks' to your checklist, otherwise CI will teach you the lesson for free.

## 9. Dependency whitelist (hard constraint)

New third-party deps require the exemption of 'industry-standard protocol impossible to reproduce with stdlib' (root AGENTS.md §2.2). Current whitelist and boundaries:

| Dependency | Allowed scope |
|------------|---------------|
| modernc.org/sqlite | memory / llm(cache_sqlite) / persist(sqlite_checkpoint) / tools(builtin database) |
| gopkg.in/yaml.v3 | config / governance / cmd/ap scaffolding |
| grpc + protobuf + genproto/rpc | agent/a2a only, agent/cluster(grpc_bus), agent/transport(grpc) |
| etcd client v3 | persist/ and agent/cluster/ only (etcd build tag) |
| go-redis v9 | persist/ only (redis build tag) |
| wazero | wasm/ module only |
| pgx v5 | required indirectly by pgvector/ module only; direct import inside internal is a violation |

Even the CLI skips cobra — the hand-written switch is part of the discipline; keep it that way.

# Part III · Development Recipes


## 10. Recipe A: Build your own agent application

```
ap init my-bot --template with-tools      # pick with-tools when you need files/shell/web
#   multi-agent → multi-agent collab      agent-with-rag → knowledge-base QA
#   agent-with-cache → cached savings     agent-with-metrics → Prometheus metrics
#   basic → minimal skeleton              quickstart → recommended for beginners
```

Then change exactly two things: system_prompt & registered toolset in main.go; model & memory backend in .ap.yaml. Iterate using the §4 loop.

## 11. Recipe B: Implement a custom built-in tool

Three steps (full walkthrough in docs/cookbook/自定义工具.md):

1. Create internal/tools/builtin/<name>.go implementing the internal/tools.Tool interface (name/description/schema/Execute); model it after builtin/text_splitter.go — struct fields are parameters, JSON tags are the input contract;
2. Any file/command side effects MUST go through Scope validation (see existing patterns in filesystem_safety_test.go, shell_scope_test.go);
3. TDD: write _test.go first (external deps always httptest / t.TempDir() / MockLLM), pass go test ./internal/tools/....

Register via Registry.Register (batch with RegisterMultiple; dynamic plugins use RegisterPlugin → marketplace verification chain).

## 12. Recipe C: Add a new LLM provider

The framework ships the flow as copy-paste template — internal/llm/provider_template.go (build tag ignore_template guarantees it never compiles):
```
cp provider_template.go yourname_provider.go
# Replace Template → YourName everywhere, implement four methods in order: Complete() → Stream() → CallTools() → Info()
# Remove the warning comment block; NewTemplateProvider deliberately returns an error against misuse — yours carries real logic
go test -run TestTemplate ./internal/llm/
```

Then: register in the .ap.yaml provider enum, add matching tests; exposing it at pkg level trips the contract-drift gate — do the four-way bump per §16.

## 13. Recipe D: Switch/combine memory backends

```
store, _ := memory.WithInMemory()                     // in-memory SQLite for tests
store, _ := memory.NewSQLiteStore("./data/mem.db")    // production default (FTS5+vector)
rag := memory.NewRAGStore(store, embedder)            // layer RAG on top (Hybrid Fusion)
vec := memory.NewVectorStoreWithHNSW(dim, cfg)        // large-scale vector search
```

.ap.yaml equivalent: memory.backend= sqlite | memory; further backends compose at code level.

## 14. Recipe E: Multi-agent and cross-node collaboration

- Single-host orchestration: ExecutionEngine.Run(mode OrchestratorMode, steps, edges []DAGEdge) — DAGs are expressed via edges; pipeline / debate / mapreduce have dedicated implementation files while handoff / groupchat belong to the collaboration mode family (collaboration*.go); common shapes already exported via pkg/debate.go and pkg/adapters.go;
- Protocol interop: A2A (JSON-RPC/SSE/gRPC transports) with self-check `ap a2a interop-check`;
- Cluster: `ap cluster init/join/status/leave/scale` (gRPC bus reuses A2A infrastructure);
- Long-horizon autonomy: `ap autonomy run/resume/status` (HITL breakpoints included).

## 15. Testing discipline (TDD mandatory)

| Rule | Practice |
|------|----------|
| Red-Green-Refactor | _test.go first, then implementation; one Task per commit, independently compilable & passing |
| Isolation toolkit | MockLLM (agent/pool layers), DemoLLM (examples), httptest.Server (network), t.TempDir() (disk), WithInMemory() (storage) |
| Concurrency correctness | Shared state always locked; -race must be green on core packages (CI matrix includes race) |
| Comments | Follow repo convention; errors unified via pkg/errors.go sentinel variables |
| Distributed backends | etcd/redis tests run separately via make test-distributed-backends, excluded from default suites |

## 16. Quality gates and commit standards (what CI blocks)

| Gate | Behavior |
|------|----------|
| Test matrix (ci.yml) | OS×Go-version matrix running build+race; Windows no-race fallback; MockLLM integration tests |
| Tiered coverage gate | Phase 7.2 tiered gates + Tier3 soft gate (allowed but recorded) |
| Deprecation check | deprecation-check.sh + ci.yml: Deprecated annotations need a removal plan |
| Changelog | Blocks if [Unreleased] section missing |
| Security scan | govulncheck; supply-chain.yml produces Syft SBOM + cosign image signing |
| Contract drift | api-diff.sh + TestAPIContractNoDrift: pkg export changes without baseline re-lock are blocked |
| Version consistency | version-check.sh: source Version must match release pipeline |

Commit messages: feat:/fix:/refactor:, one Task per commit; changing pkg exports or the buildGoMod template requires the four-way bump (VERSION, pkg/agent.go, TS package.json, Helm values) plus refreshed contract baselines.

## 17. Release flow at a glance

```
git tag vX.Y.0 && push          # tag-release.yml / release.yml take over
#   four-way version sync → Makefile ldflags injects main.Version → cosign signing → SBOM archived
#   quarterly: make benchmark runs RunQuarterly bootstrap comparison, recorded under bench/results/
```

# Part IV · FAQ


## 18. Environment & scaffolding

`Q1` `go run /abs/path/cmd/ap` fails with *go.mod file not found*?
`go run` only works within module context. Build a binary first (§2); then it works from anywhere.

`Q2` Scaffolded project build fails with *directory prefix does not contain modules listed in go.work*?
You generated the project inside the monorepo tree but the root go.work doesn't list the new module. Workarounds: `GOWORK=off go build ./...` or `go work use ./myagent`. (Fix direction tracked in the CLI doc pitfalls table: init should auto-detect workspace context.)

`Q3` Project generated outside the framework tree fails tidy with *malformed module path: missing dot in first path element*?
Standalone scaffolds carry no replace by default (semantic import versioning limit + v0.0.0 placeholder). Following init's trailing hint, add `replace agentprimordia => <absolute framework source path>`; for vector storage also `replace agentprimordia/pgvector => <source>/pgvector`. See 版本规范.md.

`Q4` Why can't I just `go get` it like a normal library?
The framework hasn't adopted the major-version-suffix publishing route yet (SIV limitation) — decision recorded in 版本规范.md. GOPROXY-based installs require that module-path evolution first.

## 19. Runtime behavior

`Q5` Process exits immediately asking to *set AP_LLM_API_KEY*?
The generated main.go explicitly Fatals when cfg.APIKey == "" — by design. Prefer env vars over committing keys into .ap.yaml.

`Q6` Where is memory stored? Does it survive restarts?
Default ./data/memory.db (SQLite with FTS5 full-text and vector columns — persistent); backend: memory is in-process and volatile. RAG/vector retrieval layer on top.

`Q7` Agent loops forever / token bill too high?
Lower agent.max_turns in .ap.yaml (default 20); enable provider caching (agent-with-cache template / llm cache_enhanced supports semantic hits); locate loops with `ap loop trace`.

`Q8` Are shell/web tools dangerous?
shell runs in allowlist mode by default (the blacklist exists only as fallback and its own comments label it 'not recommended'); http/web/api clients validate the target IP in real time at Transport.DialContext TCP setup — Loopback / Private / LinkLocal(169.254-range) unicast and multicast addresses are refused outright with an *internal/private address* error; file operations pass FilesScope path-escape validation; guardrail hits block + audit-log automatically.

`Q9` TestSoak_Studio fails under -short?
Known false-positive pattern: the degradation gate compares first-half vs second-half throughput, so concurrent machine load alone can trip it (observed 584 requests / 0 errors still FAIL). Mitigate with SOAK_CI_MODE=1 or skip that package; the durable fix direction is item ② of the CLI doc pitfalls table.

## 20. Contributing & releases

`Q10` PR blocked by CI over CHANGELOG / Deprecation / coverage?
Three independent gates: changes must land in CHANGELOG [Unreleased]; Deprecated APIs need a replacement and timeline; coverage below the layer threshold (Tier3 soft-gates with a paper trail).

`Q11` Can I add dependencies freely?
No. Whitelist in §9; additions require the industry-standard-protocol exemption justified in the PR. Ecosystem code may reach the core only via pkg/ (measured: ecosystem→internal has zero real imports).

`Q12` Why do distributed tests do nothing?
etcd/redis backend tests are gated behind build tags and excluded from default suites: run make test-distributed-backends with those services up locally.

## Appendix A · ap command quick reference

| Command | Purpose |
|---------|---------|
| init / run / debug | Scaffolding / build-run hot-reload / debug server |
| loop trace·inspect·resume | ReAct observability and resumability |
| test | Evaluation suite entry |
| config validate | .ap.yaml validation |
| mcp / plugin | MCP servers and plugin management |
| cluster init·join·status·leave·scale | Multi-node cluster lifecycle |
| marketplace search·install·publish·run·list | Template marketplace (cosign-verified install) |
| autonomy run·list·resume·status | Long-horizon autonomous goals |
| skill list·add·remove·verify | Evolved skill management |
| a2a interop-check | Protocol interop self-check |
| realtime (voice) | Realtime multimodal sessions |
| create-edge-agent | Edge agent project |
| doctor / completion / version | Four-point checkup / completion scripts / version |


## Appendix B · Key paths index

```
docs/AP-CLI开发手册.md      # deep dive into CLI internals (finer-grained pitfalls)
docs/getting-started/*.md       # installation / quickstart / first-agent illustrated tutorials
docs/cookbook/自定义工具.md custom-provider.md customer-support.md …   # recipes
docs/guide/ReAct循环.md orchestration.md security.md deployment.md     # topic guides
docs/版本规范.md              # API compatibility promises & replace-strategy rationale
docs/供应链安全.md   # SBOM / cosign supply-chain security notes
agentprimordia/internal/AGENTS.md  # responsibility table of all 30 sub-packages
bench/results/                  # quarterly benchmarks & regression reports archive
```
---
*Maintainer note: authored 2026-08-27 from hands-on code verification; every claim here is re-verifiable with grep/the compiler. Update the corresponding sections in the same PR whenever reality drifts.*