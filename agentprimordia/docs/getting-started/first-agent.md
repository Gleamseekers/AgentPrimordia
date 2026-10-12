# Your First Agent

Build a working agent application from an empty directory: two custom tools, a memory store,
and an interactive loop. Everything compiles against the published module — no clone, no
`replace` directive.

> 中文版：[第一个Agent.md](第一个Agent.md)

## Prerequisites

- Go 1.26+ installed (`go version`)
- Finished the [five-minute quickstart](five-minutes.md), or at least installed the CLI
- An API key for any OpenAI-compatible endpoint (optional — see [Demo mode](#run-it-in-demo-mode))

## Project layout

```
my-agent/
├── main.go          # entry point: provider → registry → memory → agent → REPL
├── tools.go         # two custom tools
├── go.mod           # module declaration
└── data/            # created at runtime for the SQLite memory store
```

## Step 1 — Create the module

```bash
mkdir my-agent && cd my-agent
go mod init my-agent

go get github.com/Gleamseekers/AgentPrimordia/v7@latest
```

`go get` resolves the framework (and its dependencies) from the module proxy and records the
current released version in `go.mod`. The import path you will use is
`github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg` — the `/agentprimordia`
segment is part of it because the code lives in the `agentprimordia/` subdirectory.

## Step 2 — Define tools

Create `tools.go`. A tool is any type implementing four methods — `Name`, `Description`,
`Parameters` (a JSON schema) and `Execute`:

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"

	ap "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg"
)

// WeatherTool answers weather questions for a city.
type WeatherTool struct{}

func (t *WeatherTool) Name() string        { return "get_weather" }
func (t *WeatherTool) Description() string { return "Get the current weather for a city" }
func (t *WeatherTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"city": {"type": "string", "description": "City name, e.g. Beijing"}
		},
		"required": ["city"]
	}`)
}

func (t *WeatherTool) Execute(ctx context.Context, args json.RawMessage) (*ap.ToolResult, error) {
	var params struct {
		City string `json:"city"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("parse arguments: %w", err)
	}
	// A real project calls a weather API here.
	return &ap.ToolResult{Content: fmt.Sprintf("%s: sunny, 25°C, 60%% humidity", params.City)}, nil
}

// CalculatorTool evaluates a simple arithmetic expression.
type CalculatorTool struct{}

func (t *CalculatorTool) Name() string        { return "calculator" }
func (t *CalculatorTool) Description() string { return "Evaluate a basic arithmetic expression" }
func (t *CalculatorTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"expression": {"type": "string", "description": "e.g. 2 + 3 * 4"}
		},
		"required": ["expression"]
	}`)
}

func (t *CalculatorTool) Execute(ctx context.Context, args json.RawMessage) (*ap.ToolResult, error) {
	var params struct {
		Expression string `json:"expression"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("parse arguments: %w", err)
	}
	// A real project uses a safe expression parser here.
	return &ap.ToolResult{Content: fmt.Sprintf("%s = 14", params.Expression)}, nil
}
```

`ToolResult` carries `Content`, `IsError` and `Metadata`. Tools that touch the filesystem or
run commands must go through the framework's scope validation (FilesScope / ShellScope) —
see the [tool system concepts](../concepts/工具系统.md).

## Step 3 — Create the agent

Create `main.go`. `ap.NewAgent` is the recommended entry point:

```go
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	ap "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg"
)

func main() {
	ctx := context.Background()

	// 1. LLM provider — read the configuration from the environment.
	provider, err := ap.NewOpenAIProvider(ap.Config{
		APIKey: os.Getenv("AP_LLM_API_KEY"),
		Model:  "gpt-4o-mini",
	})
	if err != nil {
		log.Fatalf("create provider: %v", err)
	}

	// 2. Tool registry.
	registry := ap.NewToolRegistry()
	if err := registry.Register(&WeatherTool{}); err != nil {
		log.Fatalf("register WeatherTool: %v", err)
	}
	if err := registry.Register(&CalculatorTool{}); err != nil {
		log.Fatalf("register CalculatorTool: %v", err)
	}

	// 3. Memory store — SQLite in a local file.
	memory, err := ap.NewSQLiteStore("./data/memory.db")
	if err != nil {
		log.Fatalf("create memory: %v", err)
	}
	defer memory.Close()

	// 4. The agent itself.
	agent, err := ap.NewAgent("my-agent",
		"You are a helpful assistant that can check the weather and do arithmetic.",
		provider,
		ap.WithMaxTurns(10),
		ap.WithToolkit(registry),
		ap.WithMemory(memory),
	)
	if err != nil {
		log.Fatalf("create agent: %v", err)
	}

	fmt.Println("🤖 My first agent is running!")
	fmt.Println("Type your question, 'quit' to exit")
	fmt.Println(strings.Repeat("-", 50))

	// 5. Interactive loop.
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("\n> ")
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if input == "quit" || input == "exit" {
			fmt.Println("Bye!")
			break
		}

		resp, err := agent.Run(ctx, ap.UserMessage(input))
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			continue
		}
		fmt.Printf("\n%s\n", resp.Content)
	}
}
```

`ap.NewAgent(name, systemPrompt, provider, options...)` — the system prompt is where the
agent's persona lives, and the options are ordinary functions (`WithMaxTurns`,
`WithToolkit`, `WithMemory`, `WithHooks`, `WithRAG`, …).

## Step 4 — Run

```bash
mkdir -p data                      # the SQLite store needs a directory to live in
export AP_LLM_API_KEY="your-api-key"

go run .
```

You should see:

```
🤖 My first agent is running!
Type your question, 'quit' to exit
--------------------------------------------------

> What's the weather in Beijing?

Beijing: sunny, 25°C, 60% humidity

> Calculate 2 + 3 * 4

2 + 3 * 4 = 14

> quit
Bye!
```

### Run it in Demo mode

No key yet? Swap the provider and the same program runs end to end against the
keyword-matching demo provider:

```go
provider := ap.NewDemoProvider()
```

The ReAct loop, tool dispatch and turn accounting are all real — only the model is simulated.
See [five-minutes.md](five-minutes.md) for the same path through the CLI.

## Going further

### Multi-agent orchestration

```go
analyzer, _ := ap.NewAgent("analyzer", "You are a code analysis expert.", provider, ap.WithMaxTurns(5))
executor, _ := ap.NewAgent("executor", "You are a senior engineer.", provider, ap.WithMaxTurns(5))
reviewer, _ := ap.NewAgent("reviewer", "You are a code reviewer.", provider, ap.WithMaxTurns(5))

pipeline := ap.NewPipeline(
	ap.PipelineStep{Name: "Analyze", Agent: analyzer},
	ap.PipelineStep{Name: "Implement", Agent: executor},
	ap.PipelineStep{Name: "Review", Agent: reviewer},
)

result, err := pipeline.Run(ctx, "Add a health endpoint")
fmt.Println(result.Final)
```

Handoff, DAG, GroupChat, Debate and MapReduce shapes live in the same package
([orchestration concepts](../concepts/多Agent编排.md)).

### RAG over your own documents

```go
store, _ := ap.NewSQLiteStore("./data/memory.db")
defer store.Close()

embedder := ap.NewEmbeddingAdapter(provider, 1536)   // adapt the LLM provider for embeddings
ragStore := ap.NewRAGStore(store, embedder)          // FTS5 + vector hybrid retrieval

agent, _ := ap.NewAgent("rag-agent", "You answer questions from the knowledge base.", provider,
	ap.WithMaxTurns(15),
	ap.WithMemory(store),
	ap.WithRAG(ap.RAGConfig{
		Provider: ap.NewRAGProviderAdapter(ragStore),
		Mode:     ap.RAGModeAuto,   // retrieve and inject before every turn
		TopK:     5,
		MinScore: 0.3,
	}),
)
```

### Lifecycle hooks

```go
hooks := ap.NewHookManager()
hooks.Register(ap.HookBeforeTool, func(ctx context.Context, hctx *ap.HookContext) error {
	log.Printf("about to run tool: %s", hctx.ToolCall.Name)
	return nil
})
hooks.Register(ap.HookAfterTool, func(ctx context.Context, hctx *ap.HookContext) error {
	log.Printf("tool finished: %s", hctx.ToolCall.Name)
	return nil
})

agent, _ := ap.NewAgent("hooked-agent", "You are an assistant.", provider,
	ap.WithMaxTurns(10),
	ap.WithHooks(hooks),
)
```

There are 30+ hook points (before/after LLM, tool, turn, RAG, memory read/write, pipeline
step, handoff, stream, shutdown, …) — enough to wire audit trails, cost tracking and
alerting without touching the loop.

### Inspect and resume

```bash
ap debug            # debug server on http://localhost:6060
ap loop trace       # replay a ReAct execution trace
ap loop resume      # resume from the last checkpoint
```

### Honest boundaries

Two capabilities are intentionally **not** part of this path:

- The **world model** (`ap.WithWorldModel()`) is opt-in and off by default: the
  pre-registered experiment did not clear its gate (+5.4pp, p=0.1796), so the default was
  not flipped.
- `ap.NewEnhancedPlanner()` ships as `Experimental` with planning-budget guardrails because
  its recursive decomposition measured **−47.8pp (p=0.001)** on short-horizon ReAct tasks.
  Do not reach for it to make a simple agent "smarter".

Agent-authored tools are likewise research-blocked: the WASM sandbox and signature
verification exist, but there is no production tool *generator* yet.

## Next steps

- 🧠 [Memory system](../concepts/记忆系统.md) · 🛠️ [Tool system](../concepts/工具系统.md)
- 👥 [Multi-agent orchestration](../concepts/多Agent编排.md) · 🔒 [Security](../concepts/安全护栏.md)
- 📚 [Complete handbook (EN)](../AP-开发手册-EN.md) · [Cookbook](../cookbook/index.md)
