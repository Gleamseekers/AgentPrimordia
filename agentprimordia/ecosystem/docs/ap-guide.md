# AP 开发者指南

> AgentPrimordia CLI — 从创建到部署的完整开发手册

`ap` 是 AgentPrimordia 框架的命令行工具，提供项目初始化、编译运行、热重载、调试、测试、MCP Server 管理和插件管理等全生命周期能力。

---

## 目录

- [1. 概览与安装](#1-概览与安装)
- [2. 项目初始化 (ap init)](#2-项目初始化-ap-init)
- [3. 运行与开发 (ap run)](#3-运行与开发-ap-run)
- [4. 配置文件](#4-配置文件)
- [5. 调试 (ap debug)](#5-调试-ap-debug)
- [6. 测试 (ap test)](#6-测试-ap-test)
- [7. MCP 管理 (ap mcp)](#7-mcp-管理-ap-mcp)
- [8. 插件管理 (ap plugin)](#8-插件管理-ap-plugin)
- [9. 公共 API 速查](#9-公共-api-速查)
- [附录 A: 命令速查表](#附录-a-命令速查表)
- [附录 B: 错误信息速查](#附录-b-错误信息速查)

---

## 1. 概览与安装

### 1.1 ap 是什么

`ap` 是 AgentPrimordia 框架的命令行开发工具，提供以下核心能力：

| 命令 | 功能 |
|------|------|
| `ap init` | 从模板创建 Agent 项目 |
| `ap run` | 编译并运行项目（支持热重载） |
| `ap debug` | 启动 HTTP 调试服务器 |
| `ap test` | 运行 eval 评估测试套件 |
| `ap mcp` | 管理 MCP Server |
| `ap plugin` | 管理插件 |
| `ap version` | 显示版本号 |

### 1.2 安装

```bash
go install agentprimordia/cmd/ap@latest
```

安装完成后，`ap` 命令即可在终端中使用。

### 1.3 查看版本

```bash
ap version
# 或
ap -v
ap --version
```

输出：`AgentPrimordia CLI v7.5.0`

### 1.4 查看帮助

不带参数运行 `ap` 将显示用法概览：

```bash
ap
```

输出：

```
AgentPrimordia (ap) — Go Agent 开发框架命令行工具

用法:
  ap <command> [arguments]

命令:
  init     创建新的 Agent 项目
  run      编译并运行当前项目
  debug    启动调试服务器
  test     运行 eval 测试套件
  mcp      管理 MCP Server
  plugin   管理插件
  version  显示版本号

使用 "ap <command> --help" 查看子命令详情。
```

每个子命令也支持 `--help`：

```bash
ap init --help
ap run --help
ap debug --help
ap test --help
ap mcp --help
ap plugin --help
```

---

## 2. 项目初始化 (ap init)

### 2.1 命令语法

```bash
ap init <项目名> [--template basic|with-tools|multi-agent]
```

**选项：**

| 选项 | 短选项 | 说明 | 默认值 |
|------|--------|------|--------|
| `--template` | `-t` | 项目模板 | `basic` |
| `--help` | `-h` | 显示帮助 | — |

**参数：**

| 参数 | 说明 |
|------|------|
| `<项目名>` | 必填，项目目录名称，同时用作 Go Module 名称 |

### 2.2 三种模板

#### basic — 最小 Agent

最简单的 Agent 模板，仅包含 LLM 对话，不加载任何工具。适合学习框架基础和快速原型。

```bash
ap init my-agent
ap init my-agent --template basic
ap init my-agent -t basic
```

生成的 `main.go`：

```go
package main

import (
	"context"
	"fmt"
	"log"

	ap "github.com/Gleamseekers/AgentPrimordia/pkg"
)

func main() {
	agent := ap.NewAgent("my-agent", "你是一个智能助手，用中文回答问题。",
		nil, // 替换为你的 LLM Provider: ap.NewOpenAIProvider(ap.Config{APIKey: os.Getenv("OPENAI_API_KEY"), Model: "gpt-4o"}),
		ap.WithMaxTurns(10),
	)

	prompt := "你好！"
	resp, err := agent.Run(context.Background(), ap.UserMessage(prompt))
	if err != nil {
		log.Fatalf("Agent 运行失败: %v", err)
	}

	fmt.Printf("回复: %s\n", resp.Content)
	fmt.Printf("轮数: %d\n", resp.Metrics.TotalTurns)
}
```

**要点：**
- 使用 `ap.NewAgent` 创建 Agent
- `ReActConfig` 配置名称、系统提示词和最大轮次
- `Model` 字段需要替换为实际的 LLM Provider
- `ap.UserMessage()` 创建用户消息
- `agent.Run()` 返回 `Response`，包含回复内容、指标等

#### with-tools — 含工具 Agent

内置文件系统、Shell 命令和 Web 访问三大工具，同时配置了内存记忆存储。适合需要与文件系统和网络交互的场景。

```bash
ap init my-agent --template with-tools
```

生成的 `main.go`：

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	ap "github.com/Gleamseekers/AgentPrimordia/pkg"
)

func main() {
	// 配置工具集
	registry, err := ap.DefaultToolkit(ap.ToolkitConfig{
		RootDir:     ".",
		EnableFS:    true,
		EnableShell: true,
		EnableWeb:   true,
	})
	if err != nil {
		log.Fatalf("创建工具集失败: %v", err)
	}

	// 配置记忆存储
	memory, err := ap.WithInMemory()
	if err != nil {
		log.Fatalf("创建记忆存储失败: %v", err)
	}
	defer memory.Close()

agent, err := ap.NewAgent("my-agent", "你是一个可以读写文件、执行命令和访问网页的助手。", provider,
    ap.WithMaxTurns(20),
    ap.WithToolkit(registry),
    ap.WithMemory(memory),
)

	prompt := "列出当前目录的文件"
	if envPrompt := os.Getenv("AP_PROMPT"); envPrompt != "" {
		prompt = envPrompt
	}

	resp, err := agent.Run(context.Background(), ap.UserMessage(prompt))
	if err != nil {
		log.Fatalf("Agent 运行失败: %v", err)
	}

	fmt.Printf("回复: %s\n", resp.Content)
	fmt.Printf("工具调用: %d 次\n", resp.Metrics.TotalTools)
}
```

**要点：**
- `ap.DefaultToolkit()` 创建默认工具集，通过 `ToolkitConfig` 控制启用哪些工具
- `ap.WithInMemory()` 创建内存模式记忆存储
- `Toolkit` 字段注入工具注册表，Agent 可在 ReAct 循环中调用工具
- `Memory` 字段注入记忆适配器，Agent 可存取对话历史
- 支持 `AP_PROMPT` 环境变量动态传入提示词

**ToolkitConfig 字段：**

| 字段 | 类型 | 说明 | 默认值 |
|------|------|------|--------|
| `RootDir` | string | 工作根目录 | `"."` |
| `EnableFS` | bool | 启用文件系统工具 | `true` |
| `EnableShell` | bool | 启用 Shell 命令工具 | `true` |
| `EnableWeb` | bool | 启用 Web 请求工具 | `true` |

#### multi-agent — 多 Agent 协作

使用 Agent Pool 进行并发任务调度，多个 Agent 实例同时处理不同任务。适合需要并行处理多个独立任务的场景。

```bash
ap init my-agent --template multi-agent
```

生成的 `main.go`：

```go
package main

import (
	"context"
	"fmt"
	"log"

	ap "github.com/Gleamseekers/AgentPrimordia/pkg"
	"github.com/Gleamseekers/AgentPrimordia/testutil"
)

func main() {
	fmt.Println("=== my-agent: 多 Agent 协作 ===")

	// 使用 testutil.NewMockProvider 提供预设响应（无需手写 mock）
	mockLLM := testutil.NewMockProvider(
		"任务处理完成",
		"分析结果已生成",
		"报告已生成",
	)

	// 使用 Pool 进行多 Agent 调度
	pool := ap.NewPool(ap.PoolConfig{
		MaxConcurrency: 3,
		DefaultAgent: ap.ReActAgentConfig{
			SystemPrompt: "你是一个任务处理助手",
			MaxTurns:     5,
		},
	})
	defer pool.Close()

	// 替换为你的 LLM Provider:
	// pool.SetModel(ap.NewOpenAIProvider(ap.Config{APIKey: os.Getenv("OPENAI_API_KEY"), Model: "gpt-4o"}))
	pool.SetModel(mockLLM)

	tasks := []ap.TaskConfig{
		{ID: "task-1", Title: "数据收集", Prompt: "收集相关数据", SessionID: "session-001"},
		{ID: "task-2", Title: "分析处理", Prompt: "分析收集的数据", SessionID: "session-001"},
		{ID: "task-3", Title: "报告生成", Prompt: "生成分析报告", SessionID: "session-001"},
	}

	results, err := pool.Dispatch(context.Background(), tasks)
	if err != nil {
		log.Fatalf("Pool 调度失败: %v", err)
	}

	for _, r := range results {
		status := "成功"
		if r.Error != nil {
			status = r.Error.Error()
		}
		fmt.Printf("任务 [%s] %s: %s (耗时 %v)\n", r.TaskID, r.Task.Title, status, r.Duration)
	}

	stats := pool.Stats()
	fmt.Printf("\n统计: 完成=%d, 失败=%d\n", stats.CompletedTasks, stats.FailedTasks)
}
```

**要点：**
- `ap.NewPool()` 创建 Agent 池，`MaxConcurrency` 控制最大并发数
- `pool.SetModel()` 设置池中所有 Agent 使用的 LLM Provider
- `ap.TaskConfig` 定义任务，包含 ID、标题、提示词和会话 ID
- `pool.Dispatch()` 并发分发任务，返回每个任务的结果
- `pool.Stats()` 获取运行统计
- `pool.Close()` 释放资源

> **注意：** 模板中使用 `mockLLM` 作为占位，开发时需要替换为真实的 LLM Provider。

### 2.3 生成的项目结构

`ap init` 执行后，会在当前目录下创建以下结构：

```
<项目名>/
├── main.go          # 入口文件（从模板生成）
└── .ap.yaml         # 项目配置文件
```

### 2.4 初始化后的下一步

```bash
cd my-agent
# 设置 API Key（以 OpenAI 为例）
export OPENAI_API_KEY=sk-xxx    # Linux/macOS
set OPENAI_API_KEY=sk-xxx       # Windows CMD
$env:OPENAI_API_KEY="sk-xxx"   # Windows PowerShell

# 编辑 main.go，取消 Model 字段的注释并配置 Provider
# 然后运行
ap run
```

---

## 3. 运行与开发 (ap run)

### 3.1 命令语法

```bash
ap run [--watch] [--prompt "消息"]
```

**选项：**

| 选项 | 短选项 | 说明 | 默认值 |
|------|--------|------|--------|
| `--watch` | `-w` | 文件变更时自动重编译 | `false` |
| `--prompt` | `-p` | 向 Agent 发送消息（通过 `AP_PROMPT` 环境变量） | — |
| `--help` | `-h` | 显示帮助 | — |

### 3.2 基本运行流程

```bash
ap run
```

执行流程：

1. **查找项目目录** — 从当前目录向上搜索，找到包含 `.ap.yaml` 或 `go.mod` 的目录
2. **编译** — 执行 `go build -o <目录名>-agent .`
3. **运行** — 执行编译后的二进制文件
4. **清理** — 运行结束后删除二进制文件

### 3.3 传入提示词

使用 `--prompt` 向 Agent 发送消息：

```bash
ap run --prompt "分析当前目录的代码结构"
ap run -p "列出所有 TODO 注释"
```

`--prompt` 的值会通过 `AP_PROMPT` 环境变量传递给 Agent 进程。在代码中读取：

```go
prompt := "默认提示词"
if envPrompt := os.Getenv("AP_PROMPT"); envPrompt != "" {
    prompt = envPrompt
}
```

### 3.4 热重载开发模式

```bash
ap run --watch
# 或
ap run -w
```

监视模式的工作方式：

1. 持续监控项目中所有 `.go` 文件的修改时间
2. 跳过隐藏目录（以 `.` 开头）和 `vendor/` 目录
3. 检测到文件变更时自动重新编译并运行
4. 编译失败不会退出，显示错误后继续监视

输出示例：

```
监视模式: 文件变更自动重编译 (Ctrl+C 退出)

编译 my-agent-agent ...
运行 my-agent-agent ...

回复: ...
工具调用: 3 次

--- 文件变更，重新编译 ---

编译 my-agent-agent ...
运行 my-agent-agent ...
```

### 3.5 项目目录发现

`ap run` 通过 `findProjectDir()` 查找项目根目录：

1. 从当前工作目录开始
2. 检查是否存在 `.ap.yaml` 或 `go.mod`
3. 如果没有，向上进入父目录继续搜索
4. 直到找到匹配文件或到达文件系统根目录

如果未找到项目目录，输出错误：`未找到项目目录（缺少 .ap.yaml 或 go.mod）`

### 3.6 组合使用

```bash
# 监视模式 + 提示词
ap run --watch --prompt "检查代码质量"

# 简写
ap run -w -p "检查代码质量"
```

---

## 4. 配置文件

### 4.1 概述

`ap` 使用两种配置文件格式：

| 文件 | 格式 | 用途 |
|------|------|------|
| `.ap.yaml` | YAML | 项目初始化时生成，人类可读的项目配置 |
| `.ap.json` | JSON | 运行时由 `ap mcp add`、`ap plugin install` 等命令自动生成/更新 |

### 4.2 配置加载优先级

`loadAPConfig()` 的加载顺序：

1. 优先读取 `.ap.json`（JSON 格式，可完整解析）
2. 如不存在，尝试读取 `.ap.yaml`（当前版本仅做基础解析）
3. 都不存在则返回空配置

### 4.3 .ap.yaml 完整字段

`ap init` 生成的 `.ap.yaml` 文件：

```yaml
# AgentPrimordia 项目配置
name: my-agent
template: basic

llm:
  provider: openai       # openai | anthropic | gemini | ollama | azure
  model: gpt-4o
  # api_key: ${OPENAI_API_KEY}  # 建议用环境变量

memory:
  backend: sqlite        # sqlite | memory
  path: ./data/memory.db

agent:
  max_turns: 20
  system_prompt: "你是一个智能助手"
```

**字段说明：**

#### 顶层字段

| 字段 | 类型 | 说明 |
|------|------|------|
| `name` | string | 项目名称 |
| `template` | string | 使用的模板（`basic` / `with-tools` / `multi-agent`） |

#### llm 配置

| 字段 | 类型 | 说明 |
|------|------|------|
| `llm.provider` | string | LLM 提供者：`openai` / `anthropic` / `gemini` / `ollama` / `azure` |
| `llm.model` | string | 模型标识，如 `gpt-4o`、`claude-3-5-sonnet-20241022` |
| `llm.api_key` | string | API Key（建议使用环境变量而非明文写入配置） |

#### memory 配置

| 字段 | 类型 | 说明 |
|------|------|------|
| `memory.backend` | string | 存储后端：`sqlite`（持久化） / `memory`（内存模式） |
| `memory.path` | string | SQLite 数据库文件路径（仅 `sqlite` 后端需要） |

#### agent 配置

| 字段 | 类型 | 说明 |
|------|------|------|
| `agent.max_turns` | int | Agent 最大推理轮次 |
| `agent.system_prompt` | string | Agent 系统提示词 |

#### mcp 配置（手动添加或通过 `ap mcp add` 生成）

| 字段 | 类型 | 说明 |
|------|------|------|
| `mcp.servers` | map | MCP Server 配置映射，键为 Server 名称 |

每个 MCP Server 配置：

| 字段 | 类型 | 说明 |
|------|------|------|
| `command` | string | 启动命令（如 `npx`） |
| `args` | []string | 命令参数列表 |
| `baseUrl` | string | 远程 Server 的 HTTP URL |
| `autoStart` | bool | 是否随 Agent 自动启动 |
| `env` | map[string]string | 环境变量 |

#### plugins 配置（通过 `ap plugin install` 生成）

| 字段 | 类型 | 说明 |
|------|------|------|
| `plugins` | []string | 已安装插件的 Go Module 路径列表 |

### 4.4 .ap.json 示例

由 `ap mcp add` 或 `ap plugin install` 命令自动生成：

```json
{
  "name": "my-agent",
  "template": "with-tools",
  "llm": {
    "provider": "openai",
    "model": "gpt-4o"
  },
  "memory": {
    "backend": "sqlite",
    "path": "./data/memory.db"
  },
  "agent": {
    "maxTurns": 20,
    "systemPrompt": "你是一个智能助手"
  },
  "mcp": {
    "servers": {
      "filesystem": {
        "command": "npx",
        "args": ["@modelcontextprotocol/server-filesystem", "/tmp"],
        "baseUrl": "",
        "autoStart": true,
        "env": {}
      }
    }
  },
  "plugins": [
    "github.com/user/ap-plugin-slack"
  ]
}
```

### 4.5 配置保存机制

`saveAPConfig()` 将配置保存到项目根目录的 `.ap.json`，使用 `json.MarshalIndent` 格式化输出，便于人工阅读和版本控制。

---

## 5. 调试 (ap debug)

### 5.1 命令语法

```bash
ap debug [--port <端口号>]
```

**选项：**

| 选项 | 短选项 | 说明 | 默认值 |
|------|--------|------|--------|
| `--port` | `-p` | 调试服务器端口 | `6060` |
| `--help` | `-h` | 显示帮助 | — |

### 5.2 启动调试服务器

```bash
ap debug
# 输出：调试服务器启动: http://localhost:6060

ap debug --port 3000
# 输出：调试服务器启动: http://localhost:3000

ap debug -p 8080
```

按 `Ctrl+C` 停止服务器。

### 5.3 调试面板

在浏览器中访问 `http://localhost:<端口>` 打开调试面板，包含以下四个区域：

| 面板 | 功能 |
|------|------|
| **Agent 推理链** | 实时显示 Agent 的推理过程，按轮次展示思考链 |
| **工具调用** | 显示工具调用的历史记录，包含工具名和参数 |
| **记忆搜索** | 搜索 Agent 记忆数据，支持关键词输入 |
| **性能指标** | 展示 Agent 运行时的性能数据 |

面板通过 EventSource（SSE）连接 `/api/events` 端点，每 3 秒自动刷新数据。

### 5.4 API 端点

| 端点 | 方法 | 说明 | 响应格式 |
|------|------|------|----------|
| `/` | GET | 调试面板 HTML 页面 | `text/html` |
| `/api/project` | GET | 项目信息（名称、目录） | `application/json` |
| `/api/env` | GET | Go 环境信息（版本号） | `application/json` |
| `/api/files` | GET | 文件列表 | `application/json` |
| `/api/events` | GET | SSE 实时事件流 | `text/event-stream` |

**响应示例：**

`GET /api/project`:
```json
{"name": "my-agent", "dir": "/path/to/my-agent"}
```

`GET /api/env`:
```json
{"go_version": "go version go1.22.0 linux/amd64"}
```

---

## 6. 测试 (ap test)

### 6.1 命令语法

```bash
ap test [--verbose]
```

**选项：**

| 选项 | 短选项 | 说明 | 默认值 |
|------|--------|------|--------|
| `--verbose` | `-v` | 显示详细输出 | `false` |
| `--help` | `-h` | 显示帮助 | — |

### 6.2 测试发现机制

`ap test` 在项目目录中搜索符合 `eval_*_test.go` 命名模式的文件：

- 前缀：`eval_`
- 后缀：`_test.go`
- 示例：`eval_agent_test.go`、`eval_tools_test.go`

如果未找到任何 eval 测试文件，会自动生成模板文件 `eval_agent_test.go`。

### 6.3 运行测试

```bash
# 基本运行
ap test

# 详细输出
ap test --verbose
ap test -v
```

实际执行的是 Go 测试命令：

```bash
# ap test
go test -run Eval ./...

# ap test --verbose
go test -v -run Eval ./...
```

### 6.4 自动生成的测试模板

首次运行 `ap test` 时（无 eval 测试文件），自动生成 `eval_agent_test.go`：

```go
package main

import (
	"context"
	"testing"

	ap "github.com/Gleamseekers/AgentPrimordia/pkg"
)

// EvalTestSuite 定义 Agent 评估测试套件
// 修改以下测试用例以匹配你的 Agent 行为
func EvalTestSuite(t *testing.T) {
	// TODO: 替换为你的实际 LLM Provider
	mockLLM := &testMockLLM{}

agent, err := ap.NewAgent("TestAgent", "你是一个测试助手", mockLLM,
    ap.WithMaxTurns(5),
)

	t.Run("基础回复", func(t *testing.T) {
		resp, err := agent.Run(context.Background(), ap.UserMessage("你好"))
		if err != nil {
			t.Fatalf("运行失败: %v", err)
		}
		if resp.Content == "" {
			t.Error("回复内容为空")
		}
	})
}

// testMockLLM 是测试用的 Mock LLM
type testMockLLM struct{}

func (m *testMockLLM) Complete(ctx context.Context, req *ap.CompletionRequest) (*ap.CompletionResponse, error) {
	return &ap.CompletionResponse{
		ID:      "eval-mock-1",
		Content: "这是测试回复",
		Role:    "assistant",
		Usage:   ap.Usage{PromptTokens: 5, CompletionTokens: 10},
	}, nil
}

func (m *testMockLLM) Stream(ctx context.Context, req *ap.CompletionRequest) (<-chan ap.Chunk, error) {
	ch := make(chan ap.Chunk, 1)
	go func() {
		defer close(ch)
		ch <- ap.Chunk{Content: "这是测试回复", Done: true}
	}()
	return ch, nil
}

func (m *testMockLLM) CallTools(ctx context.Context, req *ap.ToolCallRequest) (*ap.ToolCallResponse, error) {
	return &ap.ToolCallResponse{Usage: ap.Usage{}}, nil
}

func (m *testMockLLM) Embeddings(ctx context.Context, texts []string) ([][]float32, error) {
	return make([][]float32, len(texts)), nil
}

func (m *testMockLLM) Info() ap.ModelInfo {
	return ap.ModelInfo{Name: "eval-mock", Provider: "mock", MaxContext: 4096, SupportsTools: true}
}
```

### 6.5 评估器类型

框架内置多种评估器（定义在 `internal/agent/eval.go`），可在测试中使用：

| 评估器 | 说明 | 适用场景 |
|--------|------|----------|
| `ExactMatchEvaluator` | 精确字符串匹配（支持空白/大小写归一化） | 输出固定格式的场景 |
| `ContainsEvaluator` | 检查输出是否包含指定关键词 | 验证关键信息是否出现 |
| `ToolUsageEvaluator` | 验证 Agent 是否调用了指定工具 | 验证工具调用行为 |
| `CompositeEvaluator` | 组合多个评估器（All / Any / Weighted 模式） | 复合评估场景 |
| `LLMEvaluator` | 使用 LLM 作为评审进行语义评估 | 开放式回答质量评估 |

### 6.6 评估测试工作流

```
1. 编辑 eval_agent_test.go，编写测试用例
2. 运行 ap test
3. 查看通过/失败结果
4. 调整 Agent 或测试用例
5. 重复
```

---

## 7. MCP 管理 (ap mcp)

### 7.1 概述

MCP（Model Context Protocol）是一种标准化的工具集成协议，允许 Agent 通过统一接口调用外部工具服务。`ap mcp` 提供 MCP Server 的完整生命周期管理。

### 7.2 子命令一览

| 子命令 | 语法 | 说明 |
|--------|------|------|
| `list` | `ap mcp list` | 列出所有已注册的 MCP Server |
| `add` | `ap mcp add <name> [选项]` | 注册新的 MCP Server |
| `remove` | `ap mcp remove <name>` | 移除 MCP Server |
| `start` | `ap mcp start <name>` | 启动 MCP Server（10 秒超时） |
| `stop` | `ap mcp stop <name>` | 停止 MCP Server |
| `test` | `ap mcp test <name>` | 测试连通性（5 秒超时） |
| `tools` | `ap mcp tools <name>` | 列出 MCP Server 提供的工具 |

### 7.3 ap mcp list

列出所有已注册的 MCP Server：

```bash
ap mcp list
```

输出示例：

```
名称                 命令                            自动启动   URL
---------------------------------------------------------------------------
filesystem           npx @modelcontextprotocol...   是
github               http://localhost:3000          否         http://localhost:3000
```

如果没有注册任何 Server，输出提示信息和添加命令。

### 7.4 ap mcp add

注册新的 MCP Server：

```bash
ap mcp add <name> --command <cmd> [--args <参数>] [--url <URL>] [--auto-start] [--env <环境变量>]
```

**选项：**

| 选项 | 短选项 | 说明 | 必填 |
|------|--------|------|------|
| `--command` | `-c` | 启动命令 | 与 `--url` 二选一 |
| `--args` | `-a` | 命令参数（逗号分隔） | 否 |
| `--url` | `-u` | 远程 Server URL | 与 `--command` 二选一 |
| `--auto-start` | — | 启用自动启动 | 否 |
| `--env` | `-e` | 环境变量（逗号分隔的 `KEY=VALUE`） | 否 |

**示例：**

```bash
# 注册本地命令式 MCP Server
ap mcp add filesystem \
  --command "npx" \
  --args "@modelcontextprotocol/server-filesystem,/tmp" \
  --auto-start

# 注册远程 HTTP MCP Server
ap mcp add github --url "http://localhost:3000"

# 带环境变量
ap mcp add github \
  --url "http://localhost:3000" \
  --env "GITHUB_TOKEN=ghp_xxx,LOG_LEVEL=debug"
```

注册后，配置会保存到 `.ap.json` 的 `mcp.servers` 中。

### 7.5 ap mcp remove

移除已注册的 MCP Server：

```bash
ap mcp remove <name>
```

示例：

```bash
ap mcp remove filesystem
# 输出：✓ MCP Server "filesystem" 已移除
```

如果 Server 不存在，输出错误信息。

### 7.6 ap mcp start

启动 MCP Server（10 秒超时）：

```bash
ap mcp start <name>
```

示例：

```bash
ap mcp start filesystem
# 输出：启动 MCP Server "filesystem" ...
#       ✓ MCP Server "filesystem" 已启动
```

启动过程使用 `ap.NewMCPRegistry()` 注册并启动 Server。

### 7.7 ap mcp stop

停止运行中的 MCP Server：

```bash
ap mcp stop <name>
```

示例：

```bash
ap mcp stop filesystem
# 输出：MCP Server "filesystem" 已停止
```

### 7.8 ap mcp test

测试 MCP Server 的连通性（5 秒超时）：

```bash
ap mcp test <name>
```

前提条件：Server 必须配置了 `baseUrl`（即 `--url` 参数）。

测试流程：
1. 创建 MCP 客户端连接到 Server 的 BaseURL
2. 调用 `Initialize()` 建立连接
3. 列出 Server 提供的工具

示例：

```bash
ap mcp test filesystem
# 输出：测试 MCP Server "filesystem" 连通性...
#       ✓ 连接成功，发现 5 个工具:
#         - read_file: 读取文件内容
#         - write_file: 写入文件
#         - list_directory: 列出目录内容
#         - search_files: 搜索文件
#         - get_file_info: 获取文件信息
```

如果 Server 未配置 URL，输出：`MCP Server "xxx" 未配置 URL，无法测试`

### 7.9 ap mcp tools

列出 MCP Server 提供的所有工具：

```bash
ap mcp tools <name>
```

前提条件：Server 必须配置了 `baseUrl` 且已启动。

输出包含工具名称、描述和输入参数 Schema：

```
名称: read_file
描述: 读取指定路径的文件内容
参数: {
    "type": "object",
    "properties": {
      "path": {"type": "string", "description": "文件路径"}
    },
    "required": ["path"]
  }

名称: write_file
描述: 向指定路径写入内容
参数: {
    "type": "object",
    "properties": {
      "path": {"type": "string", "description": "文件路径"},
      "content": {"type": "string", "description": "写入内容"}
    },
    "required": ["path", "content"]
  }
```

### 7.10 MCP 协议交互流程

```
ap mcp add       →  配置写入 .ap.json
       ↓
ap mcp start     →  MCPRegistry.Register() + MCPRegistry.Start()
       ↓
ap mcp test      →  MCPClient.Initialize() + 列出工具
       ↓
ap mcp tools     →  MCPClient.Initialize() + Tools() 详情
       ↓
ap mcp stop      →  停止 Server 进程
       ↓
ap mcp remove    →  从 .ap.json 删除配置
```

### 7.11 MCP Server 配置格式

`.ap.json` 中的 MCP Server 配置：

```json
{
  "mcp": {
    "servers": {
      "<server-name>": {
        "command": "npx",
        "args": ["@modelcontextprotocol/server-filesystem", "/tmp"],
        "baseUrl": "",
        "autoStart": true,
        "env": {
          "CUSTOM_VAR": "value"
        }
      }
    }
  }
}
```

**字段说明：**

| 字段 | 类型 | 说明 |
|------|------|------|
| `command` | string | 启动命令（如 `npx`、`python`、`node`） |
| `args` | []string | 命令参数列表 |
| `baseUrl` | string | 远程 Server 的 HTTP 地址（命令式和远程式二选一） |
| `autoStart` | bool | 是否随 Agent 自动启动 |
| `env` | map[string]string | 传递给 Server 进程的环境变量 |

---

## 8. 插件管理 (ap plugin)

### 8.1 概述

插件是扩展 Agent 工具能力的主要方式。每个插件实现 `ap.ToolPlugin` 接口，提供一组工具注册到 Agent 的工具注册表。

### 8.2 子命令一览

| 子命令 | 语法 | 说明 |
|--------|------|------|
| `install` | `ap plugin install <module>` | 从 Go Module 安装插件 |
| `list` | `ap plugin list` | 列出已安装插件 |
| `create` | `ap plugin create <name>` | 创建插件项目骨架 |
| `remove` | `ap plugin remove <module>` | 移除插件 |

### 8.3 ap plugin install

从 Go Module 安装插件：

```bash
ap plugin install <module-path>
```

**执行流程：**

1. 运行 `go get <module>` 安装依赖
2. 将模块路径添加到 `.ap.json` 的 `plugins` 列表
3. 输出使用指引

示例：

```bash
ap plugin install github.com/user/ap-plugin-slack
# 输出：
# 安装插件: github.com/user/ap-plugin-slack
# (go get 输出)
# ✓ 插件 github.com/user/ap-plugin-slack 已安装
#
# 在代码中引入插件:
#   import _ "github.com/user/ap-plugin-slack"
#   // 然后在 init() 中: pluginLoader.Load("github.com/user/ap-plugin-slack".NewPlugin())
#
# 运行 ap run 使插件生效
```

### 8.4 ap plugin list

列出所有已安装的插件：

```bash
ap plugin list
```

输出示例：

```
模块路径                                     瑙态
------------------------------------------------------------
github.com/user/ap-plugin-slack              已安装
github.com/user/ap-plugin-weather            已安装
```

如果没有安装任何插件，输出提示信息和安装命令。

### 8.5 ap plugin create

创建插件项目骨架：

```bash
ap plugin create <插件名>
```

示例：

```bash
ap plugin create ap-plugin-weather
# 输出：
# ✓ 插件项目 "ap-plugin-weather" 已创建
#
# 目录结构:
#   ap-plugin-weather/
#   ├── plugin.go    — 插件入口（实现 ToolPlugin 接口）
#   ├── go.mod
#   └── README.md
#
# 下一步:
#   cd ap-plugin-weather
#   # 编辑 plugin.go 添加你的工具
#   go mod tidy
```

**生成的项目结构：**

```
ap-plugin-weather/
├── plugin.go       # 插件入口（实现 ToolPlugin 接口）
├── go.mod          # Go 模块定义
├── tools/          # 工具实现目录
└── README.md       # 插件文档
```

**生成的 plugin.go：**

```go
package ap_plugin_weather

import (
	ap "github.com/Gleamseekers/AgentPrimordia/pkg"
)

// Plugin 实现 ap.ToolPlugin 接口
type Plugin struct{}

// NewPlugin 创建插件实例（入口点）
func NewPlugin() ap.ToolPlugin {
	return &Plugin{}
}

// Name 返回插件名称
func (p *Plugin) Name() string {
	return "ap-plugin-weather"
}

// Version 返回插件版本
func (p *Plugin) Version() string {
	return "0.1.0"
}

// Tools 返回插件提供的工具列表
func (p *Plugin) Tools() []ap.Tool {
	return []ap.Tool{
		// 在此注册你的工具
	}
}

// Init 初始化插件
func (p *Plugin) Init(config map[string]any) error {
	return nil
}

// Close 清理资源
func (p *Plugin) Close() error {
	return nil
}
```

### 8.6 ap plugin remove

移除已安装的插件：

```bash
ap plugin remove <module-path>
```

示例：

```bash
ap plugin remove github.com/user/ap-plugin-slack
# 输出：
# ✓ 插件 "github.com/user/ap-plugin-slack" 已从配置中移除
# 提示: 运行 go mod tidy 清理依赖
```

> **注意：** `ap plugin remove` 仅从 `.ap.json` 配置中移除，不会自动执行 `go mod tidy`。需要手动运行以清理 Go 依赖。

### 8.7 ToolPlugin 接口

所有插件必须实现 `ap.ToolPlugin` 接口：

```go
type ToolPlugin interface {
    Name() string                    // 插件名称
    Version() string                 // 插件版本
    Tools() []Tool                   // 提供的工具列表
    Init(config map[string]any) error // 初始化（接收配置）
    Close() error                    // 清理资源
}
```

**生命周期：**

```
NewPlugin() → Init(config) → Tools() 注册到 Registry → Agent 使用 → Close()
```

### 8.8 插件开发工作流

```
1. ap plugin create ap-plugin-xxx     # 创建骨架
2. 编辑 plugin.go，在 Tools() 中注册工具
3. go mod tidy                        # 整理依赖
4. go test ./...                      # 测试插件
5. 推送到 Git 仓库
6. ap plugin install github.com/user/ap-plugin-xxx  # 在项目中安装
7. 在 main.go 中 import 并加载插件
8. ap run                             # 运行
```

---

## 9. 公共 API 速查

### 9.1 导入方式

```go
import ap "github.com/Gleamseekers/AgentPrimordia/pkg"
```

### 9.2 Agent 核心

| 类型 | 说明 |
|------|------|
| `ap.Agent` | Agent 核心接口 |
| `ap.ReActAgent` | ReAct 循环 Agent 实现 |
| `ap.ReActConfig` | Agent 配置（名称、提示词、模型、工具、记忆等） |
| `ap.Response` | Agent 运行响应（内容、工具调用、用量、指标） |
| `ap.Message` | 对话消息 |
| `ap.AgentStatus` | Agent 状态（Idle / Running / Paused / Completed / Failed / Cancelled） |
| `ap.AgentStats` | 运行统计信息 |

**常用函数：**

```go
ap.NewAgent(name, system, model, opts...)  // 创建 Agent（推荐）
ap.UserMessage("内容")             // 创建用户消息
ap.SystemMessage("内容")           // 创建系统消息
ap.NewPromptTemplate("模板")       // 创建提示词模板
ap.DefaultSystemPrompt()          // 默认系统提示词
```

### 9.3 LLM Provider

| 类型 | 说明 |
|------|------|
| `ap.Provider` | LLM 提供者核心接口 |
| `ap.Config` | 通用配置（APIKey、BaseURL、Model、Temperature、MaxTokens） |
| `ap.CompletionRequest` | 补全请求 |
| `ap.CompletionResponse` | 补全响应 |
| `ap.Usage` | Token 用量统计 |
| `ap.ModelInfo` | 模型信息 |

**Provider 实现：**

```go
ap.NewOpenAIProvider(config)       // OpenAI (GPT-4o 等)
ap.NewAnthropicProvider(config)    // Anthropic (Claude)
ap.NewGeminiProvider(config)       // Google Gemini
ap.NewOllamaProvider(config)       // Ollama (本地模型)
ap.NewAzureOpenAIProvider(config)  // Azure OpenAI
ap.NewCohereProvider(config)       // Cohere
ap.NewMistralProvider(config)      // Mistral AI
ap.NewResilientProvider(primary, fallback, config)  // 弹性 Provider（重试+熔断+降级）
```

### 9.4 工具系统

| 类型 | 说明 |
|------|------|
| `ap.Tool` | 工具接口 |
| `ap.ToolResult` | 工具执行结果 |
| `ap.ToolRegistry` | 工具注册中心 |
| `ap.ToolExecutor` | 工具执行器 |
| `ap.ToolPermission` | 工具权限控制 |
| `ap.ScopePolicy` | 作用域权限策略 |
| `ap.FileSystem` | 文件系统工具 |
| `ap.Shell` | Shell 命令工具 |
| `ap.Web` | Web 请求工具 |
| `ap.KnowledgeSearch` | 知识库搜索工具 |

**常用函数：**

```go
ap.NewToolRegistry()               // 创建工具注册中心
ap.DefaultToolkit(config)          // 创建默认工具集（FS + Shell + Web）
ap.MinimalToolkit(config)          // 创建最小工具集（FS + Shell）
ap.NewFileSystem(scopePolicy)      // 创建文件系统工具
ap.NewShell(scopePolicy)           // 创建 Shell 工具
ap.NewWeb()                        // 创建 Web 工具
```

### 9.5 记忆系统

| 类型 | 说明 |
|------|------|
| `ap.Memory` | 记忆存储核心接口 |
| `ap.Episode` | 记忆片段 |
| `ap.SearchOptions` | 搜索选项 |
| `ap.MemoryStats` | 存储统计 |
| `ap.SQLiteStore` | SQLite + FTS5 存储 |
| `ap.VectorStore` | 向量存储 |
| `ap.RAGStore` | RAG 混合检索 |
| `ap.RAGResult` | RAG 查询结果 |

**常用函数：**

```go
ap.NewSQLiteStore(path)            // 创建 SQLite 记忆存储
ap.WithInMemory()                  // 创建内存模式存储
ap.NewVectorStore(dims)            // 创建向量存储
ap.FormatRAGContext(results)       // 格式化 RAG 结果为上下文
```

### 9.6 多 Agent 调度

| 类型 | 说明 |
|------|------|
| `ap.Pool` | 多 Agent 并发调度器 |
| `ap.PoolConfig` | 池配置（并发数、超时、重试、默认 Agent） |
| `ap.TaskConfig` | 任务配置（ID、标题、提示词、会话） |
| `ap.TaskResult` | 任务执行结果 |
| `ap.PoolStats` | 池运行统计 |
| `ap.RetryPolicy` | 重试策略 |

**常用函数：**

```go
ap.NewPool(config)                 // 创建 Agent 池
```

### 9.7 DAG 工作流

| 类型 | 说明 |
|------|------|
| `ap.DAGWorkflow` | DAG 工作流引擎 |
| `ap.DAGBuilder` | 声明式 DAG 构建器（链式 API） |
| `ap.DAGNode` | DAG 节点 |
| `ap.DAGEdge` | DAG 边（含条件谓词） |
| `ap.DAGResult` | DAG 执行结果 |
| `ap.AgentDelegateNode` | Agent 包装为 DAG 节点 |
| `ap.SubWorkflowNode` | 子工作流包装为 DAG 节点 |

**常用函数：**

```go
ap.NewDAGWorkflow()                // 创建 DAG 工作流
ap.NewDAGBuilder()                 // 创建 DAG 构建器
ap.MakeNode(id, handler)           // 创建节点
ap.NewAgentDelegateNode(agent)     // 包装 Agent 为节点
ap.NewSubWorkflowNode(workflow)    // 包装子工作流为节点
```

### 9.8 MCP 协议

| 类型 | 说明 |
|------|------|
| `ap.MCPClient` | MCP 客户端 |
| `ap.MCPRegistry` | MCP Server 注册中心 |
| `ap.MCPClientConfig` | MCP Server 连接配置 |
| `ap.MCPToolDefinition` | MCP 工具定义 |

**常用函数：**

```go
ap.NewMCPClient(baseURL)          // 创建 MCP 客户端
ap.NewMCPRegistry()               // 创建 MCP Server 注册中心
```

### 9.9 消息总线

| 类型 | 说明 |
|------|------|
| `ap.MessageBus` | 消息总线接口 |
| `ap.LocalMessageBus` | 进程内消息总线 |
| `ap.BusMessage` | 消息结构 |
| `ap.BusMessageType` | 消息类型 |
| `ap.HTTPTransport` | HTTP 传输层 |

**常用函数：**

```go
ap.NewLocalMessageBus()            // 创建本地消息总线
ap.NewHTTPTransport(addr)          // 创建 HTTP 传输层
```

### 9.10 缓存

| 类型 | 说明 |
|------|------|
| `ap.InMemoryCache` | 内存向量相似度缓存 |
| `ap.FingerprintCache` | Prompt 指纹精确匹配缓存 |
| `ap.HybridCache` | 混合缓存（精确+语义） |
| `ap.CachedProvider` | 带缓存的 Provider 装饰器 |
| `ap.CacheManager` | 缓存管理器 |

**常用函数：**

```go
cache, _ := ap.NewCachedProvider(provider,
    ap.NewFingerprintCache(10000, time.Hour), // 创建指纹缓存（容量, TTL）
    0.85)                                     // minScore：语义相似度阈值
```

### 9.11 结构化输出

| 类型 | 说明 |
|------|------|
| `ap.StructuredExtractor` | 结构化数据提取器 |
| `ap.SchemaDef` | JSON Schema 定义 |
| `ap.ResponseFormat` | 响应格式控制 |
| `ap.ValidationError` | Schema 验证错误 |

**常用函数：**

```go
ap.NewStructuredExtractor(provider, model)  // 创建提取器
ap.SchemaFromStruct(obj, options...)         // 从 Go struct 生成 Schema
ap.ValidateAgainstSchema(data, schema)       // 验证 JSON 数据
```

### 9.12 多模态

| 类型 | 说明 |
|------|------|
| `ap.MultimodalProvider` | 多模态 LLM 接口 |
| `ap.MultimodalAdapter` | 多模态适配器 |
| `ap.MultimodalContent` | 多模态内容块（Text/Image/Audio/Video） |

**常用函数：**

```go
ap.NewMultimodalProvider(provider)  // 创建多模态 Provider
ap.NewTextContent(text)             // 文本内容块
ap.NewImageURLContent(url)          // 图片 URL 内容块
ap.NewImageB64Content(data, mime)   // 图片 Base64 内容块
ap.NewAudioContent(data, mime)      // 音频内容块
ap.NewVideoContent(data, mime)      // 视频内容块
```

---

## 附录 A: 命令速查表

| 命令 | 说明 |
|------|------|
| `ap init <name>` | 创建项目（basic 模板） |
| `ap init <name> -t with-tools` | 创建含工具项目 |
| `ap init <name> -t multi-agent` | 创建多 Agent 项目 |
| `ap run` | 编译并运行 |
| `ap run -w` | 监视模式运行 |
| `ap run -p "消息"` | 带提示词运行 |
| `ap run -w -p "消息"` | 监视模式 + 提示词 |
| `ap debug` | 启动调试服务器（端口 6060） |
| `ap debug -p 3000` | 指定端口调试 |
| `ap test` | 运行 eval 测试 |
| `ap test -v` | 详细模式运行测试 |
| `ap mcp list` | 列出 MCP Server |
| `ap mcp add <name> -c <cmd> -a <args>` | 注册命令式 MCP Server |
| `ap mcp add <name> -u <url>` | 注册远程 MCP Server |
| `ap mcp remove <name>` | 移除 MCP Server |
| `ap mcp start <name>` | 启动 MCP Server |
| `ap mcp stop <name>` | 停止 MCP Server |
| `ap mcp test <name>` | 测试连通性 |
| `ap mcp tools <name>` | 列出工具 |
| `ap plugin install <module>` | 安装插件 |
| `ap plugin list` | 列出插件 |
| `ap plugin create <name>` | 创建插件 |
| `ap plugin remove <module>` | 移除插件 |
| `ap version` | 显示版本 |

---

## 附录 B: 错误信息速查

| 错误信息 | 原因 | 解决方式 |
|----------|------|----------|
| `错误: 请指定项目名称` | `ap init` 未提供项目名 | 添加项目名参数 |
| `错误: 未知模板 "xxx"，可选: basic, with-tools, multi-agent` | 指定了无效模板 | 使用 `basic`、`with-tools` 或 `multi-agent` |
| `错误: 目录 "xxx" 已存在` | 目标目录已存在 | 使用不同的项目名或删除现有目录 |
| `错误: --template 需要指定模板名称` | `--template` 后缺少值 | 添加模板名称 |
| `未找到项目目录（缺少 .ap.yaml 或 go.mod）` | 当前目录及上级目录中没有配置文件 | 切换到项目目录或创建 `.ap.yaml` |
| `错误: MCP Server "xxx" 不存在` | 操作了未注册的 Server | 先使用 `ap mcp add` 注册 |
| `MCP Server "xxx" 未配置 URL，无法测试` | 尝试测试命令式 Server | 使用 `ap mcp start` 启动后测试 |
| `错误: 插件 "xxx" 未安装` | 尝试移除未安装的插件 | 检查插件列表 |
| `错误: --command 需要指定命令` | `ap mcp add` 的 `--command` 缺少值 | 添加命令值 |
| `错误: 必须指定 --command 或 --url` | `ap mcp add` 未提供连接方式 | 添加 `--command` 或 `--url` |
