# AgentPrimordia Ecosystem

> 框架核心之外的扩展层 — 插件、模板、贡献指南、示例应用。

## 这是什么?

`ecosystem/` 是 AgentPrimordia 框架的**生态聚合目录**,
与 `cmd/` / `internal/` / `pkg/` 等核心包物理分离。
其存在意义是:

1. **核心稳定** — 改 `internal/` / `pkg/` 是 breaking change,需要走完整 SemVer
2. **生态自由** — 改 `ecosystem/` 不影响 v1 兼容承诺,可自由扩展
3. **物理隔离** — 内部边界规则通过目录结构强制,比文档约定更可靠

## 目录结构

```
ecosystem/
├── docs/          # 文档(API 参考、最佳实践、Cookbook)
├── examples/      # 示例应用(20 个 chain-* 例子供学习)
├── plugins/       # 开箱即用工具插件(email/git/http/json/kv/sql)
├── templates/     # `ap init` 脚手架模板(basic/with-tools/multi-agent)
└── contributing/  # 贡献者指南(PLUGIN.md / PROVIDER.md)
```

## Go import 约定

### 用户代码 (in your own project)

```go
// ✅ 推荐：导入核心包
import ap "github.com/Gleamseekers/AgentPrimordia/pkg"

// ❌ 禁止：从核心包导入生态包
import "github.com/Gleamseekers/AgentPrimordia/ecosystem/plugins/email"
```

理由:生态包的 API **不在 v1 兼容承诺范围** — 任何 minor 版本都可能
调整、废弃或重命名。直接 import 等于"自己承担维护负担"。

### 核心代码 (in internal/ or pkg/)

```go
// ❌ 绝对禁止：核心代码依赖生态
// internal/agent/xxx.go 不允许出现:
import "github.com/Gleamseekers/AgentPrimordia/ecosystem/..."
```

理由:这会破坏模块边界(AGENTS.md §模块边界),并让核心包
的依赖图出现"循环引用"风险。

生态代码如需在核心中注册工具,应通过 `tools.Plugin` 协议:

```go
// ✅ 推荐：核心提供 interface，生态实现
//   internal/tools/plugin.go 定义 ToolPlugin interface
//   ecosystem/plugins/* 实现该 interface
//   用户在外部通过 PluginLoader 注入

registry.Register(plugins.EmailPlugin)
```

### 生态代码 (in ecosystem/)

```go
// ✅ 推荐：导入核心包 + 自身依赖
import (
    "github.com/Gleamseekers/AgentPrimordia/internal/tools"  // 实现 tools.Tool 接口
    "github.com/Gleamseekers/AgentPrimordia/ecosystem/plugins/kv"  // 复用其他生态包
)

// ⚠️ 谨慎：跨生态包依赖应通过文档明示
```

生态包之间可以相互引用(如 email 依赖 json),但应在
README.md 中显式声明,避免传递依赖不可控。

## 兼容性承诺矩阵

| 路径 | SemVer 承诺 | 改动影响 |
|------|:-----------:|----------|
| `pkg/` (核心 API) | ✅ Stable | 走 SemVer,大版本破坏 |
| `internal/agent/` 等 | ✅ Stable | 内部包,改动需大版本 |
| `ecosystem/plugins/*` | ❌ 无 | 任何 minor 可能调整 |
| `ecosystem/templates/*` | ❌ 无 | 自由扩展 |
| `ecosystem/examples/*` | ❌ 无 | 自由扩展 |
| `ecosystem/docs/*` | ❌ 无 | 自由扩展 |
| `ecosystem/contributing/*` | ❌ 无 | 自由扩展 |

## 贡献

参见 [ecosystem/contributing/PLUGIN.md](contributing/PLUGIN.md)
和 [ecosystem/contributing/PROVIDER.md](contributing/PROVIDER.md)。

## 文档导航

完整文档见 [ecosystem/docs/](docs/)。仓库导航见
[CODE_WIKI.md](../../CODE_WIKI.md)（位于仓库根，2026-08-09 修正链接）。
