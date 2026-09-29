# 快速入门指南

欢迎来到 AgentPrimordia！本指南将在 5 分钟内带你从零到运行第一个 AI Agent。

## 前置要求

- **Go 1.26+**
- **Git**（用于克隆项目）
- 可选：**API Key**（OpenAI / Gemini / 通义千问）

## 安装步骤

### 方式一：直接下载（推荐）

```bash
# 克隆仓库
git clone https://github.com/your-org/agentprimordia.git
cd agentprimordia

# 编译
go build ./...
```

### 方式二：Go module 引用（本地 replace 消费）

框架目前以本地模块方式消费（模块名 `agentprimordia`，无远程发布路径），在你的 `go.mod` 中通过 `replace` 指向框架源码目录：

```bash
go mod edit -require=agentprimordia@v0.0.0 -replace=agentprimordia=/path/to/agentprimordia
go mod tidy
```

## 第一个 Agent（无需 API Key）

让我们创建一个最简单的 Agent：

```go
package main

import (
    "context"
    "fmt"
    "log"

    ap "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg"
    "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/testutil"
)

func main() {
    // 使用 testutil.NewMockProvider 提供预设响应（无需 API Key）
    mockLLM := testutil.NewMockProvider(
        "你好!我是AI助手,很高兴为你服务!",
    )

    // 创建 ReAct Agent（推荐入口：ap.NewAgent）
    simpleAgent, err := ap.NewAgent("SimpleBot", "你是一个友好的AI助手",
        mockLLM,
        ap.WithMaxTurns(3),
    )
    if err != nil {
        log.Fatalf("创建 Agent 失败: %v", err)
    }

    // 运行 Agent
    resp, err := simpleAgent.Run(context.Background(), ap.UserMessage("你好"))
    if err != nil {
        log.Fatalf("运行失败: %v", err)
    }

    fmt.Println("Agent 回复:", resp.Content)
}
```

运行：

```bash
go run ./ecosystem/examples/basic/
```

## 连接真实 LLM Provider

所有 Provider 通过 `ap.NewXxxProvider(ap.Config{...})` 创建，统一使用 `ap.Config` 配置。

### OpenAI (GPT-4o)

```go
import ap "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg"

provider := ap.NewOpenAIProvider(ap.Config{
    APIKey: os.Getenv("OPENAI_API_KEY"),
    Model:  "gpt-4o",
})

agent := ap.NewAgent("GPTBot", "你是一个专业的AI助手",
    provider,
    ap.WithMaxTurns(10),
)
```

### Google Gemini

```go
provider := ap.NewGeminiProvider(ap.Config{
    APIKey: os.Getenv("GEMINI_API_KEY"),
    Model:  "gemini-2.0-flash",
})
```

### 通义千问 (Qwen)

```go
provider := ap.NewQwenProvider(ap.Config{
    APIKey:  os.Getenv("QWEN_API_KEY"),
    BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
    Model:   "qwen-plus",
})
```

### Ollama（本地模型，无需 API Key）

```go
provider := ap.NewOllamaProvider(ap.Config{
    BaseURL: "http://localhost:11434",
    Model:   "llama3",
})
```

### Resilient Provider（重试 + 降级 + 熔断）

```go
primary := ap.NewOpenAIProvider(ap.Config{APIKey: key, Model: "gpt-4o"})
fallback := ap.NewGeminiProvider(ap.Config{APIKey: geminiKey, Model: "gemini-2.0-flash"})
local := ap.NewOllamaProvider(ap.Config{BaseURL: "http://localhost:11434", Model: "llama3"})

resilient := ap.NewResilientProvider(primary, ap.DefaultResilientConfig())
resilient.AddFallback(fallback)
resilient.AddFallback(local)
```

## 添加工具能力

让 Agent 能够执行文件操作、Shell 命令等：

```go
// 创建工具集
registry, err := ap.DefaultToolkit(ap.ToolkitConfig{
    RootDir:     "./workspace",
    EnableFS:    true,
    EnableShell: true,
    EnableWeb:   true,
})
if err != nil {
    log.Fatal(err)
}

// 创建带工具的 Agent（链式 API）
agent := ap.NewAgent("ToolBot", "你可以使用工具来帮助用户完成任务",
    provider,
    ap.WithMaxTurns(20),
).WithToolkit(registry)
```

## 使用 Memory 系统

让 Agent 拥有记忆能力：

```go
// 方式1: SQLite 存储（持久化，推荐生产环境）
memoryStore, err := ap.NewSQLiteStore("./my_agent_memory.db")
if err != nil {
    log.Fatal(err)
}
defer memoryStore.Close()

// 方式2: 内存存储（测试用，无需文件）
memoryStore, err := ap.WithInMemory()
if err != nil {
    log.Fatal(err)
}
defer memoryStore.Close()

// 创建带记忆的 Agent（链式 API）
agent := ap.NewAgent("MemoryBot", "你拥有长期记忆，可以记住对话内容",
    provider,
    ap.WithMaxTurns(20),
).WithMemory(memoryStore)
```

## 多 Agent 调度

```go
pool := ap.NewPool(ap.PoolConfig{
    MaxConcurrency: 5,
    DefaultAgent: ap.ReActAgentConfig{
        SystemPrompt: "你是任务处理助手",
        MaxTurns:     10,
    },
})
defer pool.Close()

pool.SetModel(provider)

results, err := pool.Dispatch(ctx, []ap.TaskConfig{
    {ID: "task-1", Title: "代码分析", Prompt: "分析 main.go"},
    {ID: "task-2", Title: "运行测试", Prompt: "执行 go test"},
})
```

## 使用 CLI 工具

```bash
# 安装 CLI
go build -o ap ./cmd/ap/

# 3 步创建 Agent
ap init my-agent
cd my-agent
ap run

# 带工具模板
ap init my-agent --template with-tools

# 多 Agent 模板
ap init my-agent --template multi-agent

# 热重载开发
ap run --watch

# 调试服务器
ap debug
```

## 下一步

- [API 完整参考](./api-reference.md) - 所有接口详细说明
- [最佳实践指南](./best-practices.md) - 生产环境建议
- [CLI 开发手册](./ap-guide.md) - ap 命令行工具完整文档
- [示例代码库](../examples/) - 18+ 示例
- [Cookbook: 客服机器人](./cookbook/customer-support-bot.md)
- [Cookbook: 代码审查 Agent](./cookbook/code-review-agent.md)

## 常见问题

### Q: 需要付费的 API Key 吗？
不需要！可以使用 `testutil.NewMockProvider` 免费体验所有功能，或使用 `ap.NewOllamaProvider` 连接本地模型。

### Q: 支持 Windows 吗？
完全支持！Zero CGO 设计确保跨平台兼容性。

### Q: 如何切换不同的 LLM？
只需替换 Provider 参数即可，所有 Provider 实现相同的 `ap.Provider` 接口：
```go
// 从 OpenAI 切换到 Gemini，只需改一行
provider := ap.NewGeminiProvider(ap.Config{...})  // 替换 ap.NewOpenAIProvider(...)
```

### Q: 性能如何？
单 Agent 响应 <100ms（不含 LLM 调用时间），支持并发 1000+ Agents。

### Q: NewAgent 怎么用？
`ap.NewAgent` 是推荐入口，使用 Functional Options 注入能力。返回 `(*CapabilityAgent, error)`，可通过链式 API 进一步注入能力。
