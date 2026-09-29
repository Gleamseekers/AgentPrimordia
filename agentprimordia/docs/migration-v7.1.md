# AgentPrimordia v7.0 -> v7.1 迁移指南

> **版本**: v7.1.0-rc1
> **日期**: 2026-09
> **兼容性**: 所有变更均为 opt-in，不影响现有 v7.0 用户

---

## 1. 版本概述

v7.1.0-rc1 围绕五个命题展开能力扩展：

| 命题 | 核心能力 | 新增包 |
|------|----------|--------|
| P1 多模态 | 图文混合输入、ContentPart 抽象 | `internal/agent/multimodal/` |
| P2 规划增强 | 任务分解、重规划、审批门控 | `internal/agent/planning/` |
| P3 可观测性升级 | 告警引擎、Dashboard、otel 迁移 | `internal/observability/` |
| P4 工具智能统一 | 缺口检测、自动创建、性能调优 | `internal/tools/intelligence/` 及子包 |
| P5 V7 验证收尾 | 世界模型一致性、验收实验 | 无新增包 |

**rc1 状态说明**：当前为接口骨架 + 基础算法阶段，生产级实现将在后续 rc 迭代中完善。

---

## 2. 新增包

### 2.1 internal/agent/multimodal/

多模态消息抽象，支持图文混合输入。

```go
import "github.com/Gleamseekers/AgentPrimordia/internal/agent/multimodal"

msg := multimodal.MultimodalMessage{
    Role: "user",
    Parts: []multimodal.ContentPart{
        {Type: multimodal.PartText, Text: "描述这张图片"},
        {Type: multimodal.PartImage, URL: "https://example.com/img.png"},
    },
}

// 获取多模态内置工具集
tools := multimodal.BuiltinTools()
```

### 2.2 internal/agent/planning/

增强型规划器，支持任务分解、动态重规划与人工审批。

```go
import "github.com/Gleamseekers/AgentPrimordia/internal/agent/planning"

planner := planning.NewEnhancedPlanner(llm)

// 任务分解
plan, err := planner.Decompose(ctx, "构建一个 REST API 服务")

// 执行中重规划
if needsReplan {
    plan, err = planner.Replan(ctx, plan, recoveryStrategy)
}

// 审批门控（opt-in）
gate := planning.NewApprovalGate(planner)
managedPlan := planning.NewManagedPlan(plan, gate)
```

### 2.3 internal/observability/

统一可观测性层，包含告警引擎与 Dashboard。

```go
import "github.com/Gleamseekers/AgentPrimordia/internal/observability"

// 告警规则
rule := observability.AlertRule{
    Name:      "high_latency",
    Condition: observability.LatencyAbove(500 * time.Millisecond),
    Severity:  observability.SeverityWarning,
}

engine := observability.NewAlertEngine([]observability.AlertRule{rule})

// Dashboard HTTP 处理器
handler := observability.NewDashboardHandler(engine)
mux.Handle("/dashboard", handler)
```

### 2.4 internal/tools/intelligence/ 及子包

工具智能统一层，覆盖检测 -> 创建 -> 优化 -> 复用全生命周期。

```go
import (
    "github.com/Gleamseekers/AgentPrimordia/internal/tools/intelligence"
    "github.com/Gleamseekers/AgentPrimordia/internal/tools/intelligence/create"
    "github.com/Gleamseekers/AgentPrimordia/internal/tools/intelligence/optimize"
    "github.com/Gleamseekers/AgentPrimordia/internal/tools/intelligence/reuse"
)

// 顶层类型
var _ intelligence.ToolIntelligence   // 智能元数据
var _ intelligence.GapDetector        // 缺口检测器
var _ intelligence.ToolCreator        // 工具创建器
var _ intelligence.ToolProfiler       // 性能分析器
var _ intelligence.ToolTuner          // 调优器
var _ intelligence.ToolSelector       // 工具选择器

// 子包实现
// create/  — TraceGapDetector, LifecycleCreator
// optimize/ — InMemoryProfiler, DataDrivenTuner
// reuse/   — ToolCatalog, TaskMatcher
```

---

## 3. API 变更

### 3.1 internal/otel/ 包迁移

`internal/otel/` 的实现代码迁移至 `internal/observability/export/otel/`。

**向后兼容**：`internal/otel/alias.go` 保留类型别名，现有 import 无需修改：

```go
// 旧代码无需变更，以下 import 继续有效
import "github.com/Gleamseekers/AgentPrimordia/internal/otel"
```

新代码建议直接使用新路径：

```go
import "github.com/Gleamseekers/AgentPrimordia/internal/observability/export/otel"
```

### 3.2 新增类型汇总

| 包 | 新增类型 |
|----|----------|
| `internal/observability/` | `AlertRule`, `AlertEngine`, `DashboardHandler` |
| `internal/observability/export/otel/` | 原 `internal/otel/` 全部导出（通过别名兼容） |

---

## 4. 升级步骤

1. **更新依赖**：拉取 v7.1.0-rc1 标签
2. **编译验证**：`go build ./...` -- 无需修改即可编译通过
3. **可选启用**：按需 import 新包，逐步接入能力
4. **otel 迁移**（可选）：新代码使用 `internal/observability/export/otel/` 路径

无需修改现有代码即可从 v7.0 升级到 v7.1。

---

## 5. 向后兼容承诺

- 所有 v7.1 新增包均为 opt-in，不改变 v7.0 已有 API 的行为
- `internal/otel/` 通过 `alias.go` 类型别名保持完全兼容
- 不引入新的第三方依赖（白名单不变，见 AGENTS.md S2.1）
- 规划器、多模态、工具智能等能力需显式构造实例后才会参与 Agent 循环

---

## 6. 已知限制（rc1）

- `IntelligenceHook` 创建的工具需手动回注注册表，后续 rc 将自动化
- 规划增强在当前简单任务上增益不明显，适合复杂多步任务场景
- 多模态能力依赖上游 LLM Provider 的原生支持

详细验收数据与根因分析见 `docs/v7.1-rc1-验收实验报告.md`。
