# 跨语言开发指南

## Go ↔ TypeScript 行为一致性

AgentPrimordia 的 Go SDK 是核心引擎（800+ 导出），TypeScript SDK 是轻量客户端（200+ 导出）。
两端通过以下机制保持行为一致性：

### 1. 跨语言测试规范

`sdk/typescript/tests/shared/cross-language-spec.json` 定义了 11 个测试套件、36 个用例，
覆盖 Agent 配置、工具执行、向量操作、错误处理、错误码映射、Memory CRUD、LLM Provider、
健康检查、混沌工程、编排模式等领域。

Go 端测试：`go test -run TestCrossLanguage ./pkg/`
TS 端测试：`npx vitest run tests/cross-language.test.ts`

### 2. 错误码对齐

两端共享 36 个结构化错误码（`pkg/errors.go` ↔ `src/errors.ts`）：

| 模块 | 前缀 | 数量 |
|------|------|------|
| Agent | AGENT_ | 4 |
| Tool | TOOL_ | 4 |
| LLM | LLM_ | 8 |
| Pool | POOL_ | 3 |
| Memory | MEM_ | 8 |
| Security | SEC_ | 4 |
| Infra | EVT_/PST_/CON_/CTX_ | 5 |

### 3. API 契约

运行 `make api-extract` 从 Go `pkg/` 提取公共 API 签名到 `sdk/typescript/api-contract.json`。
CI 中的 `cross-language-check` job 会检测 API 漂移。

### 4. 模块对应关系

| Go 包 | TS 模块 | 状态 |
|--------|---------|------|
| `internal/chaos/` | `src/chaos/` | ✅ Experimental |
| `internal/agent/cluster/` | `src/cluster/` | ✅ Experimental |
| `pkg/errors.go` | `src/errors.ts` | ✅ Stable |
| `internal/memory/` | `src/memory/` | ✅ Stable |
| `internal/llm/` | `src/llm/` | ✅ Stable |
| `internal/agent/` | `src/agent/` | ✅ Stable |
| `internal/governance/` | — | 🔲 未移植 |
| `internal/registry/` | — | 🔲 未移植 |

## 版本策略

- Go SDK: v7.5.1（`pkg/agent.go` Version 常量）
- TypeScript SDK: v7.5.1（`package.json` version）
- 版本联动规则：TS 主版本号跟随 Go 主版本号，允许 `-beta` 后缀
