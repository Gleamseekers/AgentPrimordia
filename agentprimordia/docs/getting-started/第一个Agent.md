# 第一个 Agent

本教程将带你从零创建一个完整的 Agent 应用。

## 前置条件

- Go 1.26+ 已安装
- 已阅读 [安装指南](安装指南.md) 和 [5分钟入门](五分钟入门.md)

## 项目结构

```
my-agent/
├── main.go          # 入口文件
├── tools.go         # 工具定义
├── go.mod           # Go 模块文件
└── data/            # 数据目录
    └── memory.db    # SQLite 记忆数据库
```

## Step 1: 初始化项目

```bash
mkdir my-agent && cd my-agent
go mod init my-agent
```

AgentPrimordia 的模块名为 `agentprimordia`，没有发布到远程模块仓库，
需在 `go.mod` 中通过 `replace` 指向本地框架源码目录消费：

```bash
# 将 /path/to/agentprimordia 替换为本地框架仓库的检出路径
go mod edit -require=agentprimordia@v0.0.0 -replace=agentprimordia=/path/to/agentprimordia
```

## Step 2: 定义工具

创建 `tools.go`。自定义工具只需实现 `Tool` 接口的 4 个方法：

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"

	ap "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg"
)

// WeatherTool 天气查询工具
type WeatherTool struct{}

func (t *WeatherTool) Name() string        { return "get_weather" }
func (t *WeatherTool) Description() string { return "获取指定城市的天气信息" }
func (t *WeatherTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"city": {
				"type": "string",
				"description": "城市名称，如：北京、上海"
			}
		},
		"required": ["city"]
	}`)
}

func (t *WeatherTool) Execute(ctx context.Context, args json.RawMessage) (*ap.ToolResult, error) {
	var params struct {
		City string `json:"city"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("参数解析失败: %w", err)
	}

	// 模拟天气查询（实际项目中调用真实 API）
	return &ap.ToolResult{
		Content: fmt.Sprintf("%s: 晴, 25°C, 湿度 60%%", params.City),
	}, nil
}

// CalculatorTool 计算器工具
type CalculatorTool struct{}

func (t *CalculatorTool) Name() string        { return "calculator" }
func (t *CalculatorTool) Description() string { return "执行数学计算" }
func (t *CalculatorTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"expression": {
				"type": "string",
				"description": "数学表达式，如：2 + 3 * 4"
			}
		},
		"required": ["expression"]
	}`)
}

func (t *CalculatorTool) Execute(ctx context.Context, args json.RawMessage) (*ap.ToolResult, error) {
	var params struct {
		Expression string `json:"expression"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("参数解析失败: %w", err)
	}

	// 简化实现，实际项目中使用安全的表达式解析器
	return &ap.ToolResult{
		Content: fmt.Sprintf("计算结果: %s = 14", params.Expression),
	}, nil
}
```

## Step 3: 创建 Agent

创建 `main.go`，使用 `ap.NewAgent()` 推荐入口：

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

	// 1. 创建 LLM Provider
	provider, err := ap.NewOpenAIProvider(ap.Config{
		APIKey: os.Getenv("OPENAI_API_KEY"),
		Model:  "gpt-4o-mini",
	})
	if err != nil {
		log.Fatalf("创建 Provider 失败: %v", err)
	}

	// 2. 创建工具注册表
	registry := ap.NewToolRegistry()
	if err := registry.Register(&WeatherTool{}); err != nil {
		log.Fatalf("注册 WeatherTool 失败: %v", err)
	}
	if err := registry.Register(&CalculatorTool{}); err != nil {
		log.Fatalf("注册 CalculatorTool 失败: %v", err)
	}

	// 3. 创建记忆存储
	memory, err := ap.WithInMemory()
	if err != nil {
		log.Fatalf("创建 Memory 失败: %v", err)
	}
	defer memory.Close()

	// 4. 创建 Agent（推荐入口：ap.NewAgent）
	agent, err := ap.NewAgent("my-agent", "你是一个智能助手，可以查询天气和做计算。",
		provider,
		ap.WithMaxTurns(10),
		ap.WithToolkit(registry),
		ap.WithMemory(memory),
	)
	if err != nil {
		log.Fatalf("创建 Agent 失败: %v", err)
	}

	fmt.Println("🤖 我的第一个 Agent 已启动！")
	fmt.Println("输入你的问题，输入 'quit' 退出")
	fmt.Println(strings.Repeat("-", 50))

	// 5. 交互式循环
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
			fmt.Println("再见！")
			break
		}

		// 运行 Agent
		resp, err := agent.Run(ctx, ap.UserMessage(input))
		if err != nil {
			fmt.Fprintf(os.Stderr, "错误: %v\n", err)
			continue
		}

		fmt.Printf("\n%s\n", resp.Content)
	}
}
```

## Step 4: 运行

```bash
# 创建数据目录（使用 SQLite 持久化时需要）
mkdir -p data

# 设置 API Key（以 OpenAI 为例）
export OPENAI_API_KEY="your-api-key"

# 运行
go run .
```

你应该看到：

```
🤖 我的第一个 Agent 已启动！
输入你的问题，输入 'quit' 退出
--------------------------------------------------

> 北京今天天气怎么样？

北京: 晴, 25°C, 湿度 60%

> 帮我计算 2 + 3 * 4

计算结果: 2 + 3 * 4 = 14

> quit
再见！
```

## 进阶功能

### 添加多 Agent 编排

```go
analyzer, _ := ap.NewAgent("analyzer", "你是代码分析专家", provider, ap.WithMaxTurns(5))
executor, _ := ap.NewAgent("executor", "你是开发工程师", provider, ap.WithMaxTurns(5))
reviewer, _ := ap.NewAgent("reviewer", "你是代码审查员", provider, ap.WithMaxTurns(5))

// 顺序编排
pipeline := ap.NewPipeline(
	ap.PipelineStep{Name: "分析", Agent: analyzer},
	ap.PipelineStep{Name: "实现", Agent: executor},
	ap.PipelineStep{Name: "审查", Agent: reviewer},
)

result, err := pipeline.Run(ctx, "开发一个新功能")
fmt.Println(result.Final)
```

### 添加 RAG 能力

```go
// 创建记忆存储
store, _ := ap.NewSQLiteStore("./data/memory.db")
defer store.Close()

// 创建 Embedding 适配器（将 LLM Provider 适配为 EmbeddingProvider）
embedder := ap.NewEmbeddingAdapter(provider, 1536)

// 创建 RAG Store（FTS5 + 向量混合检索）
ragStore := ap.NewRAGStore(store, embedder)

// 通过 WithRAG 选项注入 RAG 能力（Provider 由 NewRAGProviderAdapter 适配）
agent, _ := ap.NewAgent("rag-agent", "你是知识库问答助手", provider,
	ap.WithMaxTurns(15),
	ap.WithMemory(store),
	ap.WithRAG(ap.RAGConfig{
		Provider: ap.NewRAGProviderAdapter(ragStore),
		Mode:     ap.RAGModeAuto, // 每轮推理前自动检索注入
		TopK:     5,
		MinScore: 0.3,
	}),
)
```

### 添加 Inspector 监控

```bash
# 启动调试服务器
ap debug
```

在浏览器中打开 `http://localhost:6060` 查看实时追踪。

### 添加生命周期钩子

```go
hooks := ap.NewHookManager()
hooks.Register(ap.HookBeforeTool, func(ctx context.Context, hctx *ap.HookContext) error {
	log.Printf("即将执行工具: %s", hctx.ToolCall.Name)
	return nil
})
hooks.Register(ap.HookAfterTool, func(ctx context.Context, hctx *ap.HookContext) error {
	log.Printf("工具 %s 执行完成", hctx.ToolCall.Name)
	return nil
})

agent, _ := ap.NewAgent("hooked-agent", "你是助手", provider,
	ap.WithMaxTurns(10),
	ap.WithHooks(hooks),
)
```

## 下一步

- 学习 [核心概念](../concepts/Agent架构.md) 深入理解架构
- 查看 [使用指南](../guides/创建Agent.md) 了解更多用法
- 阅读 [工具系统](../concepts/工具系统.md) 学习更多工具开发
- 尝试 [多 Agent 编排](../guides/多Agent协作.md) 构建协作系统
