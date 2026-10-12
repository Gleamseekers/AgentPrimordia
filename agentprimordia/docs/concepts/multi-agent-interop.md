# Multi-Agent Interop (A2A)

AgentPrimordia speaks **two Agent2Agent (A2A) protocol faces**. Pick the face by
*who* you are talking to, never mix their clients:

| Face | Entry points | Transport | Stability | Use when |
|------|--------------|-----------|-----------|----------|
| ap-native A2A | `ap.NewA2AGRPCServer` / `ap.NewA2AGRPCClient` | gRPC | **Stable** | Both ends run AgentPrimordia (in-cluster / in-network, performance-first) |
| Open interop | `ap.NewOpenInteropServer` / `ap.NewOpenInteropClient` | JSON-RPC 2.0 over HTTP + SSE | **Experimental** | The peer is a third-party agent that follows the open A2A spec (cross-ecosystem) |

Both faces are exported through the `ap` package (`pkg/a2a.go` and
`pkg/a2a_interop.go`); `internal/agent/a2a` is the implementation and carries no
compatibility promise. Signatures below were checked line by line against
`internal/agent/a2a/grpc_client.go`, `interop_client.go`, `grpc_server.go` and
`interop_server.go` — the full reference lives in
[A2A API](../api/A2A-API.md).

## 60-second demo: two processes, zero credentials

The cross-process handoff demo starts a server process and a client process,
then runs discover → delegate → poll → collect → cancel over the open protocol:

```bash
./agentprimordia/ecosystem/examples/a2a-handoff/run.sh
```

The server runs in **Demo mode**: when `AP_LLM_API_KEY` is unset it uses the
keyword-matching provider (the same path as `ap init --template quickstart`),
so the demo needs no API key and no network. Set `AP_LLM_API_KEY` and the same
demo switches to a real LLM. Manual two-terminal walkthrough, environment
variables and the code tour are in the demo's
[README](../../ecosystem/examples/a2a-handoff/README.md).

## The two client faces — do not mix

| | `OpenInteropClient` (open protocol) | `A2AGRPCClient` (ap-native) |
|---|---|---|
| Constructor | `ap.NewOpenInteropClient(baseURL string)` | `ap.NewA2AGRPCClient(target string, opts...)` |
| Discover | `FetchAgentCard(ctx) (*OpenAgentCard, error)` | `FetchAgentCard(ctx) (*A2AAgentCard, error)` |
| Delegate | **`SendTask(ctx, OpenMessage)`** | **`CreateTask(ctx, *A2AMessage, taskID)`** |
| Query | `GetTask(ctx, taskID) (*OpenTask, error)` | `GetTask(ctx, taskID) (*A2ATask, error)` |
| Cancel | `CancelTask(ctx, taskID) error` | `CancelTask(ctx, taskID) (*A2ATask, error)` |
| Stream | *no client method* — poll `GetTask` | `StreamEvents(ctx, taskID)` |
| Close | — | `Close() error` |

Two traps that bite in review:

- **`A2AGRPCClient` has no `SendTask`** — that method belongs to
  `OpenInteropClient`. Its delegate call is `CreateTask`, and the message
  parameter is `*A2AMessage` (role + parts), not a bare string.
- **`OpenInteropClient` has no streaming subscribe** — the open-protocol
  server exposes an SSE endpoint (`/a2a/v1/tasks/{id}/events`), but the
  exported client surface is poll-based. Poll until the state is terminal.

Client methods return `(nil, err)` on failure. Never discard the error and
dereference the task anyway (`got, _ := client.GetTask(...)` then `got.ID`) —
that nil-panics when the peer misbehaves. Check the error first, as below.

## Open interop face (cross-ecosystem)

Server — expose an ap agent (or any `TaskExecutor`) over the open protocol:

```go
card := ap.OpenAgentCard{
    Name:        "research-agent",
    Description: "Research assistant exposed over the open A2A protocol",
    URL:         "http://localhost:8080",
    Version:     "1.0.0",
    Capabilities: ap.OpenCapabilities{
        Streaming: true,
    },
    Skills: []ap.OpenSkillDecl{
        {ID: "search", Name: "Search", Description: "Search and summarize"},
    },
    DefaultInputModes:  []string{"text"},
    DefaultOutputModes: []string{"text"},
}

// EchoTaskExecutor is a built-in test executor; production code implements
// ap.TaskExecutor (Execute + Cancel) and bridges to the real agent.
server := ap.NewOpenInteropServer(card, ap.DefaultInteropConfig()).
    WithExecutor(ap.NewEchoTaskExecutor())

if err := http.ListenAndServe(":8080", server.Handler()); err != nil {
    log.Fatalf("listen: %v", err)
}
```

Client — discover, delegate, poll to a terminal state, collect the result:

```go
client := ap.NewOpenInteropClient("http://localhost:8080")
ctx := context.Background()

card, err := client.FetchAgentCard(ctx) // discovery via /.well-known/agent.json
if err != nil {
    log.Fatalf("fetch agent card: %v", err)
}
log.Printf("discovered %s with %d skills", card.Name, len(card.Skills))

task, err := client.SendTask(ctx, ap.NewTextMessage("user", "帮我分析数据"))
if err != nil {
    log.Fatalf("send task: %v", err)
}

var final *ap.OpenTask
for {
    got, err := client.GetTask(ctx, task.ID)
    if err != nil {
        log.Fatalf("get task: %v", err)
    }
    if got.Status.State.IsTerminal() { // completed / failed / canceled
        final = got
        break
    }
    time.Sleep(200 * time.Millisecond)
}

for _, artifact := range final.Artifacts {
    for _, part := range artifact.Parts {
        if part.Type == "text" {
            log.Printf("result: %s", part.Text)
        }
    }
}

if err := client.CancelTask(ctx, task.ID); err != nil {
    log.Fatalf("cancel task: %v", err)
}
```

Task states on the open face (`OpenTaskState`): `submitted`, `working`,
`input-required`, `completed`, `failed`, `canceled`; the terminal ones are
`completed` / `failed` / `canceled`.

## ap-native face (gRPC, both ends run AgentPrimordia)

Same choreography, different client — note `CreateTask(ctx, *A2AMessage, taskID)`
and the streaming option instead of polling:

```go
client, err := ap.NewA2AGRPCClient("localhost:50051",
    ap.WithA2AGRPCClientBearerToken("my-token"),
)
if err != nil {
    log.Fatalf("dial: %v", err)
}
defer client.Close()

card, err := client.FetchAgentCard(ctx)
if err != nil {
    log.Fatalf("fetch agent card: %v", err)
}
log.Printf("discovered %s", card.Name)

task, err := client.CreateTask(ctx, &ap.A2AMessage{
    Role:  "user",
    Parts: []ap.A2APart{ap.NewA2ATextPart("帮我分析数据")},
}, "task-001")
if err != nil {
    log.Fatalf("create task: %v", err)
}

// Option A: stream events (no polling)
events, err := client.StreamEvents(ctx, task.ID)
if err != nil {
    log.Fatalf("subscribe: %v", err)
}
for ev := range events {
    log.Printf("event: task=%s", ev.TaskID)
}

// Option B: poll
got, err := client.GetTask(ctx, task.ID)
if err != nil {
    log.Fatalf("get task: %v", err)
}
log.Printf("state: %s", got.State)
```

## Conformance self-check

```bash
ap a2a interop-check
```

The command runs `a2a.GenerateInteropReport(card, cfg)` — a static, offline
analysis of an Agent Card plus interop config across 8 checks
(`agent_card.name` / `url` / `version`, `capabilities.streaming`,
`default_input_modes` / `default_output_modes`, `agent_card.exposed`,
`io_modes.text`). No running endpoint is required. The same report is available
programmatically:

```go
card := ap.OpenAgentCard{Name: "agent-1", URL: "http://localhost:8080"}
report := ap.GenerateInteropReport(card, ap.DefaultInteropConfig())
log.Printf("score %.0f%%, failed: %v", report.Score*100, report.FailedChecks())
```

Versioned evidence for the demo server's card is published under
[bench/results/a2a-interop](../../bench/results/a2a-interop/) (regenerated per
release; generation method documented there).

## When to use which face

| Situation | Face |
|-----------|------|
| Peer agent is a third-party system following the open A2A spec | Open interop (`OpenInteropClient` / `OpenInteropServer`) |
| Both ends are AgentPrimordia services you control | ap-native gRPC (`A2AGRPCClient` / `A2AGRPCServer`) |
| You need streaming task events on the client side | ap-native gRPC (`StreamEvents`) |
| You need in-process composition, not network boundaries | Orchestration (Pipeline / Handoff / DAG / GroupChat / Debate) — see [多Agent编排](多Agent编排.md) |
| You need cross-node messaging between ap services | Cluster (gRPC bus reuses the A2A infrastructure), not the open face |

## Known limitations (verified 2026-10-12, framework 7.5.1)

- **No task-level checkpoint/resume on either face.** The open-interop server
  keeps tasks in an in-process memory map (`internal/agent/a2a/interop_server.go`),
  and the gRPC-side `TaskManagerImpl` is likewise in-memory. Killing and
  restarting the server loses all tasks — after restart `tasks/get` returns
  `Task not found` (error `-32001`), reproduced empirically. The framework's
  checkpoint capability (`internal/persist/`) covers the ReAct loop and plan
  execution, **not** A2A tasks. Do not design flows that assume a delegated task
  survives a server restart.
- The open interop face is **Experimental**; the gRPC face is **Stable**.
- The open-interop client has no streaming subscribe method — polling is the
  intended pattern.
- The conformance report is a schema/config self-check, not a wire-protocol
  certification against third-party implementations.

## See also

- [A2A API reference](../api/A2A-API.md) — full method tables for both faces
- [A2A通信](A2A通信.md) — Chinese concept page (gRPC-first)
- [AP Development Handbook (EN)](../AP-开发手册-EN.md) — terminology baseline
- Cross-process demo: [ecosystem/examples/a2a-handoff](../../ecosystem/examples/a2a-handoff/)
- Single-process interop demo: [ecosystem/examples/a2a-interop](../../ecosystem/examples/a2a-interop/main.go)
