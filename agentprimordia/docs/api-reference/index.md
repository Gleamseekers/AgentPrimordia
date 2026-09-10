# API 参考

> AgentPrimordia v7.3.0 完整 API 索引。

## 核心模块

公共 API 统一经 `agentprimordia/pkg`（导入别名 `ap`）以类型别名 re-export 暴露，实现位于 `internal/`（用户不应直接导入 internal 包）：

| 模块 | 包 | 说明 |
|------|-----|------|
| Agent | `ap` | Agent 类型与运行循环 |
| Tools | `ap` | 工具系统（注册表、执行器、内置工具） |
| Memory | `ap` | 记忆存储接口与实现（NewInMemoryStore / NewSQLiteStore / NewVectorStore / NewRAGStore 等） |
| LLM | `ap` | LLM 抽象层与 Provider（NewOpenAIProvider 等） |
| Orchestration | `ap` | 多 Agent 编排模式（NewPipeline / NewHandoff / NewDAGBuilder / GroupChat / Debate） |
| Pool | `ap` | 多 Agent 调度池（NewPool） |
| Guardrail | `ap` | 输入/输出护栏（NewEngine + 规则构造器） |
| Security | `ap` | ACL / Sandbox / 密钥管理 |

## 详细参考

- [Agent API](Agent-API.md)
- [Tools API](Tools-API.md)
- [Memory API](Memory-API.md)
- [LLM API](LLM-API.md)
- [Orchestration API](Orchestration-API.md)
- [Pool API](Pool-API.md)
- [Guardrail API](Guardrail-API.md)
- [Security API](Security-API.md)

## 类型关系图

```
Agent ──→ LLM (Provider)
  │
  ├──→ Tools (Registry → Executor)
  │         │
  │         └── InjectTool / FileSystemTool / ShellTool / WebTool / ...
  │
  ├──→ Memory (InMemory / SQLite / Vector / PgVector)
  │
  ├──→ Guardrail (InjectionDetector / PIIFilter / TopicBoundary)
  │
  └──→ Orchestrator (Pipeline / Handoff / DAG / GroupChat / Debate)

Pool ──→ Agent (多实例并发调度)
```
