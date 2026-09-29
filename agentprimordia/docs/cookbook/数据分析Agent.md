# 数据分析 Agent

> 从 CSV 到洞察的端到端流程：加载数据、清洗、统计、可视化。

## 代码

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    ap "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg"
)

func main() {
    provider, err := ap.NewOpenAIProvider(ap.Config{
        APIKey: os.Getenv("OPENAI_API_KEY"),
        Model:  "gpt-4o",
    })
    if err != nil { log.Fatal(err) }

    registry := ap.NewToolRegistry()
    fsTool, err := ap.NewFileSystem("./data") // 只读能力经文件系统权限/路径范围控制
    if err != nil { log.Fatal(err) }
    dbTool, err := ap.NewDatabase("./data.db")
    if err != nil { log.Fatal(err) }
    registry.RegisterMultiple(
        fsTool,
        dbTool,
        ap.NewShell(),
        ap.NewCodeExecution(),
    )

    agent, err := ap.NewAgent("data-analyst", `你是数据分析师。
1. 用 filesystem 工具读取 CSV
2. 用 database 工具执行 SQL 统计
3. 用 code_execution / shell 工具生成图表（gnuplot / matplotlib）`,
        provider, ap.WithToolkit(registry))
    if err != nil { log.Fatal(err) }

    resp, err := agent.Run(context.Background(),
        ap.UserMessage("分析 sales.csv：按月统计销售额并生成折线图"))
    if err != nil { log.Fatal(err) }
    fmt.Println(resp.Content)
}
```

## 扩展

- **流式处理**：大文件分块读取，避免 OOM
- **缓存**：相同查询命中 LLM 缓存
- **自动报告**：输出 Markdown + 图表，直接发布到 Wiki
