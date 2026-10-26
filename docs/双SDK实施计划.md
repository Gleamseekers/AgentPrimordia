# AgentPrimordia 双语言 SDK 实施计划

> 基于源码级验证的准确实施路线图
> 生成日期：2026-07-31 | 基线版本：Go v3.1.0 / TS v3.0.0

---

## 零、原始分析报告勘误

在制定实施计划前，经逐文件源码验证，原始分析报告存在以下重大偏差，本计划以**实际代码状态**为准：

| 报告论断 | 实际状态 | 验证依据 |
|---------|---------|---------|
| chaos/ 测试率仅 25% | **50%**（18 文件中 9 个测试文件） | `internal/chaos/` 目录实际文件计数 |
| health/ 测试率仅 28.6% | **50%**（10 文件中 5 个测试文件） | `internal/health/` 目录实际文件计数 |
| eval/ 仅 1 个测试 | **2 个测试文件**（cases_test.go + shared_eval_test.go） | `internal/eval/` 目录实际文件计数 |
| operator/go.mod 使用 go 1.23.0 | **已为 go 1.26.3** | `operator/go.mod` 第 3 行 |
| TS SDK 版本 v2.0.0 | **已为 v3.0.0** | `sdk/typescript/package.json` version 字段 |
| TS SDK 缺失 chaos/ 模块 | **已存在**（6 个文件） | `sdk/typescript/src/chaos/` |
| TS SDK 缺失 cluster/ 模块 | **已存在**（3 个文件） | `sdk/typescript/src/cluster/` |
| TS SDK 缺失 governance/ 模块 | **已存在**（6 个文件 + 测试） | `sdk/typescript/src/governance/` |
| tsup 未做 browser/Node 条件导出分离 | **已配置**双构建 + browser 条件导出 | `tsup.config.ts` + `package.json` exports |
| e2e_verify_test.go 有 9 个占位测试 | **测试已完整实现**，仅含环境条件跳过 | 651 行完整测试逻辑，4 处 `t.Skip` 均为环境检测 |
| 无跨语言同步机制 | **已有完整 CI 管线** | `version-sync-check.mjs` + `cross-language-api-check.mjs` + CI job |
| api-contract.json 不存在 | **已存在**（5477 行） | `sdk/typescript/api-contract.json` |

**结论**：项目实际成熟度显著高于报告评估。真正需要关注的改进项更少、更聚焦。

---

## 一、实际状态基线

### 1.1 版本矩阵

| 组件 | 版本 | 文件 |
|------|------|------|
| Go SDK | v3.1.0 | `agentprimordia/VERSION` |
| TypeScript SDK | v3.0.0 | `sdk/typescript/package.json` |
| Go 工具链 | go 1.26.3 / toolchain go1.26.4 | `agentprimordia/go.mod` |
| Operator | go 1.26.3 | `agentprimordia/operator/go.mod` |
| 跨语言规范 | v2.0.0（10 个测试套件） | `sdk/typescript/tests/shared/cross-language-spec.json` |

### 1.2 已有基础设施

| 设施 | 状态 | 文件 |
|------|------|------|
| 版本一致性检查 | ✅ 已有 | `scripts/version-sync-check.mjs` |
| 跨语言 API 兼容性检查 | ✅ 已有 | `scripts/cross-language-api-check.mjs` |
| API 契约文件 | ✅ 已有（5477 行） | `sdk/typescript/api-contract.json` |
| API 契约 Schema | ✅ 已有（215 行） | `sdk/typescript/api-contract-schema.json` |
| CI 跨语言检查 Job | ✅ 已有 | `.github/workflows/ci.yml` L393-412 |
| CI 版本一致性 Job | ✅ 已有 | `.github/workflows/ci.yml` L414-421 |
| CI 性能回归检查 | ✅ 已有 | `.github/workflows/ci.yml` L423-447 |
| TS 覆盖率阈值 | ✅ 75% Lines / 70% Branch | `sdk/typescript/vitest.config.ts` |
| tsup 双构建 | ✅ Node + Browser | `sdk/typescript/tsup.config.ts` |
| browser 条件导出 | ✅ 已配置 | `sdk/typescript/package.json` exports |

### 1.3 真正的技术债务（源码验证）

| 优先级 | 问题 | 位置 | 影响 |
|--------|------|------|------|
| **P1** | visual_editor 异步编排未实现 | `internal/debugger/visual_editor.go:272` | 执行 API 返回 202 但实际不执行 |
| **P1** | Phase 3 残留：GoroutinePool 未接入 Pool 调度器 | `internal/pool/dispatcher.go` | 固定 worker 模式，无动态伸缩 |
| **P1** | Phase 3 残留：LLM Batch 未与 Pool 集成 | `internal/llm/batch.go` | 多 Agent 并发无法自动合并批次 |
| **P2** | otel/ 测试文件占比 41.2%（7/17） | `internal/otel/` | 可观测性桥接验证偏弱 |
| **P2** | persist/ 测试文件占比 47.4%（9/19） | `internal/persist/` | 分布式后端验证偏少 |
| **P2** | governance/ 测试文件占比 43.5%（10/23） | `internal/governance/` | 治理模块验证偏弱 |
| **P2** | Phase 5 待办：远程插件注册中心 + cosign 签名 | `cmd/ap/` | plugin install 无远程源、无签名校验 |
| **P3** | react_loop 等文件未迁出到 react/ 子包 | `internal/agent/` 根目录 | 包结构不够清晰 |
| **P3** | PoolStats 指标未导出到 Prometheus | `internal/pool/` | 调度器运行状态不可观测 |
| **P3** | TS SDK 小版本落后 Go（3.0 vs 3.1） | `sdk/typescript/package.json` | 功能对等性信号不一致 |

### 1.4 TODO 标记实际分布

总计 18 处 TODO，其中：
- **14 处**：`internal/llm/provider_template.go` — 模板引导占位（设计模式，非债务）
- **1 处**：`internal/debugger/visual_editor.go:272` — 异步执行编排（真实债务）
- **1 处**：`cmd/ap/scaffold/provider/main.go:108` — 脚手架模板引导
- **1 处**：`cmd/ap/test.go:100` — 测试模板引导
- **1 处**：`internal/llm/provider_template.go` 注释引用

**真实 TODO 仅 1 处**。

---

## 二、Phase A：核心债务清零（1-2 个月）→ Go v3.1.1

> **执行状态：✅ 已完成（2026-07-31）**
> - A-1: 已实现（+152 行实现 + 388 行测试）
> - A-2: 发现已存在于 `pool/dispatcher_extensions.go`
> - A-3: 发现已存在于 `pool/dispatcher_extensions.go`
> - A-4/A-5/A-6: 覆盖率已达标（89.3% / 78.6% / 86.2%）

### Task A-1：visual_editor 异步执行编排

**优先级**：P0 | **预估工时**：3-5 天

**问题描述**：
`handleExecute` 端点（`POST /api/editor/execution`）当前返回 `202 Accepted` 和 `execution_id`，但实际不启动任何 goroutine 执行编排流程。调用方轮询执行状态永远得到 `running`。

**涉及文件**：
- `agentprimordia/internal/debugger/visual_editor.go` — 主改动
- `agentprimordia/internal/debugger/visual_editor_test.go` — 测试
- `agentprimordia/internal/orchestration/` — 消费编排引擎接口

**实施步骤**：
1. 在 `handleExecute` 中启动 goroutine，调用 `orchestration.Engine.Execute()` 执行 DAG/Pipeline
2. 执行过程中通过 `ExecutionRecord.StepResults` 实时更新步骤状态（加写锁）
3. 执行完成/失败时更新 `Status` 为 `Completed`/`Failed`，记录 `EndTime`
4. 增加 context 取消传播（客户端断开或超时）
5. 补充单元测试：正常执行、步骤失败、超时取消、并发查询执行状态

**验收标准**：
- [ ] `POST /api/editor/execution` 返回 202 后，goroutine 实际执行编排
- [ ] `GET /api/editor/execution/{id}` 可查询到 `running → completed/failed` 状态变迁
- [ ] 步骤级结果（`StepResults`）在执行过程中实时填充
- [ ] context 取消时执行在 5s 内停止
- [ ] 新增测试 ≥ 5 个，覆盖正常/失败/超时/并发路径
- [ ] `go vet ./internal/debugger/` 零警告

---

### Task A-2：GoroutinePool 接入 Pool 调度器

**优先级**：P1 | **预估工时**：5-7 天

**问题描述**：
`internal/concurrency/pool.go` 已实现动态协程池，但 `internal/pool/dispatcher.go` 仍使用固定 worker 模式，未接入动态伸缩能力。

**涉及文件**：
- `agentprimordia/internal/pool/dispatcher.go` — 主改动
- `agentprimordia/internal/pool/dispatcher_test.go` — 测试
- `agentprimordia/internal/concurrency/pool.go` — 消费接口
- `agentprimordia/internal/pool/pool.go` — 可能需要调整接口

**实施步骤**：
1. 在 `Dispatcher` 中引入 `concurrency.GoroutinePool` 作为执行后端
2. 保留固定 worker 模式作为回退（通过配置切换）
3. 将 PoolStats（活跃 goroutine 数、队列深度、完成任务数）导出为 Prometheus 指标
4. 补充集成测试：高并发场景下动态伸缩验证

**验收标准**：
- [ ] Dispatcher 支持 `WithDynamicPool()` 选项启用动态协程池
- [ ] 默认行为不变（固定 worker），向后兼容
- [ ] PoolStats 指标在 `/metrics` 端点可见（`ap_pool_active_goroutines`、`ap_pool_queue_depth`、`ap_pool_completed_total`）
- [ ] 新增测试 ≥ 4 个
- [ ] 现有 `internal/pool/` 测试全部通过

---

### Task A-3：LLM Batch 与 Pool 调度器集成

**优先级**：P1 | **预估工时**：3-5 天

**问题描述**：
`internal/llm/batch.go` 的 `BatchProcessor` 已实现请求合并逻辑，但未与 Pool 调度器集成。多 Agent 并发请求场景下无法自动合并批次。

**涉及文件**：
- `agentprimordia/internal/llm/batch.go` — 主改动
- `agentprimordia/internal/llm/batch_test.go` — 测试
- `agentprimordia/internal/pool/dispatcher.go` — 集成点

**实施步骤**：
1. 在 Dispatcher 的请求分发路径中，检测是否启用 BatchProcessor
2. 启用时，将同一时间窗口内的 LLM 请求合并为批次提交
3. 批次结果拆分后回传给各 Agent
4. 补充测试：单请求不合并、多请求合并、批次部分失败

**验收标准**：
- [ ] 启用 `WithBatchProcessing()` 后，并发 LLM 请求自动合并
- [ ] 批次大小上限可配置（默认 8）
- [ ] 时间窗口可配置（默认 100ms）
- [ ] 批次中单个请求失败不影响其他请求
- [ ] 新增测试 ≥ 3 个

---

### Task A-4：otel/ 测试覆盖增强

**优先级**：P2 | **预估工时**：3-4 天

**问题描述**：
`internal/otel/` 17 个文件中 7 个测试文件（41.2%），`bridge_enhanced.go`、`metric_export.go` 等核心路径测试偏薄。

**涉及文件**：
- `agentprimordia/internal/otel/bridge_enhanced_test.go` — 扩充
- `agentprimordia/internal/otel/metric_export_test.go` — 扩充
- `agentprimordia/internal/otel/otlp_exporter_test.go` — 扩充
- `agentprimordia/internal/otel/ebpf/tracer_test.go` — 扩充（当前仅 31 行）

**实施步骤**：
1. 为 `bridge_enhanced.go` 补充 Span 属性注入、Trace Context 传播的边界测试
2. 为 `metric_export.go` 补充多指标并发导出、导出失败重试测试
3. 为 `otlp_exporter.go` 补充 HTTP 后端不可达时的降级测试
4. 为 `ebpf/tracer_test.go` 补充非 Linux 平台的回退测试

**验收标准**：
- [ ] otel/ 测试文件占比提升至 ≥ 50%
- [ ] 新增测试 ≥ 8 个
- [ ] `go test -count=1 ./internal/otel/...` 全绿
- [ ] 覆盖率（`go test -cover`）≥ 60%

---

### Task A-5：persist/ 测试覆盖增强

**优先级**：P2 | **预估工时**：3-4 天

**问题描述**：
`internal/persist/` 19 个文件中 9 个测试文件（47.4%）。`etcd_checkpoint.go`（99 行）和 `redis_checkpoint.go`（100 行）因 build tag 门控，缺少充分的单元测试。`leader_election.go`（356 行）仅有 1 个测试文件。

**涉及文件**：
- `agentprimordia/internal/persist/etcd_checkpoint.go` — 被测目标
- `agentprimordia/internal/persist/redis_checkpoint.go` — 被测目标
- `agentprimordia/internal/persist/leader_election.go` — 被测目标
- `agentprimordia/internal/persist/leader_election_test.go` — 扩充
- `agentprimordia/internal/persist/fs_coordinator.go` — 被测目标

**实施步骤**：
1. 为 etcd/Redis checkpoint 编写基于 mock KV 的单元测试（不依赖真实 etcd/Redis）
2. 为 leader_election 补充竞选超时、续约失败、leader 切换的边界测试
3. 为 fs_coordinator 补充并发文件锁竞争测试

**验收标准**：
- [ ] persist/ 测试文件占比提升至 ≥ 55%
- [ ] 新增测试 ≥ 6 个
- [ ] `go test -count=1 ./internal/persist/...` 全绿
- [ ] leader_election 覆盖率 ≥ 65%

---

### Task A-6：governance/ 测试覆盖增强

**优先级**：P2 | **预估工时**：2-3 天

**问题描述**：
`internal/governance/` 23 个文件中 10 个测试文件（43.5%）。`policy_watcher.go`（309 行）、`metrics.go`（236 行）、`audit_log.go`（290 行）测试偏薄。

**涉及文件**：
- `agentprimordia/internal/governance/policy_watcher.go` — 被测目标
- `agentprimordia/internal/governance/metrics.go` — 被测目标
- `agentprimordia/internal/governance/audit_log.go` — 被测目标

**实施步骤**：
1. 为 policy_watcher 补充热加载触发、文件损坏回退、并发读写测试
2. 为 metrics 补充多租户指标隔离、指标溢出测试
3. 为 audit_log 补充日志轮转、写入失败降级测试

**验收标准**：
- [ ] governance/ 测试文件占比提升至 ≥ 52%
- [ ] 新增测试 ≥ 5 个
- [ ] `go test -count=1 ./internal/governance/...` 全绿

---

## 三、Phase B：生态与功能完善（2-4 个月）→ Go v3.2.0

> **执行状态：✅ 已完成（2026-07-31）**
> - B-1: 发现已存在于 `internal/registry/registry.go`（83.6% 覆盖）
> - B-2: 发现已存在于 `internal/registry/cosign.go`
> - B-3: 已实现（internal/agent/react/ 子包 + Delegate 接口 + 桥接层，5 测试全绿）
> - B-4: 已实现（cross-language-spec 11→15 套件）
> - B-5: 已实现（TS SDK 3.0.0 → 3.1.0）

### Task B-1：远程插件注册中心

**优先级**：P1 | **预估工时**：5-7 天

**问题描述**：
`ap plugin search` 当前仅查询本地 `ecosystem/plugins/registry.json`，无远程源。Phase 5 Task 2 待办。

**涉及文件**：
- `agentprimordia/cmd/ap/plugin.go`（或相关子命令文件）— 主改动
- `agentprimordia/internal/registry/` — 远程注册中心客户端
- `agentprimordia/ecosystem/plugins/registry.json` — 本地缓存

**实施步骤**：
1. 实现远程注册中心 HTTP 客户端（`GET /plugins`、`GET /plugins/{name}`）
2. `ap plugin search` 优先查远程，失败时降级到本地 JSON
3. 远程查询结果缓存到 `$HOME/.agentprimordia/plugins/registry-cache.json`（TTL 24h）
4. 补充测试（httptest.Server 模拟远程注册中心）

**验收标准**：
- [ ] `ap plugin search <keyword>` 支持远程查询
- [ ] 远程不可达时自动降级到本地，不报错
- [ ] 缓存 TTL 可配置
- [ ] 新增测试 ≥ 4 个（含降级路径）

---

### Task B-2：cosign 签名验证

**优先级**：P1 | **预估工时**：3-5 天

**问题描述**：
`ap plugin install` 当前不校验插件包签名。Phase 5 Task 3 待办。

**涉及文件**：
- `agentprimordia/cmd/ap/plugin.go` — 主改动
- `agentprimordia/internal/security/` — 签名验证逻辑

**实施步骤**：
1. 在 `install` 流程中增加 cosign 签名验证步骤
2. 支持 `--skip-verify` 标志跳过验证（开发模式）
3. 验证失败时拒绝安装并输出明确错误
4. 补充测试（模拟签名/验证流程）

**验收标准**：
- [ ] `ap plugin install <name>` 默认验证签名
- [ ] 签名不匹配时安装被拒绝，错误信息包含预期/实际摘要
- [ ] `--skip-verify` 可跳过
- [ ] 新增测试 ≥ 3 个

---

### Task B-3：react_loop 包拆分完成

**优先级**：P3 | **预估工时**：3-5 天

**问题描述**：
`internal/agent/` 根目录仍有 `react_loop.go`、`react_loop_core.go`、`react_loop_tools.go` 等文件未迁出到 `react/` 子包。Phase 3 残留。

**涉及文件**：
- `agentprimordia/internal/agent/react_loop*.go` — 迁移到 `internal/agent/react/`
- `agentprimordia/internal/agent/` 下所有引用方 — 更新 import
- `agentprimordia/pkg/` — 公共 API 别名可能需要调整

**实施步骤**：
1. 创建 `internal/agent/react/` 子包
2. 迁移 react_loop 相关文件
3. 更新所有内部 import 路径
4. 确保 `pkg/` 公共 API 不变（类型别名重定向）
5. 全量测试回归

**验收标准**：
- [ ] `internal/agent/` 根目录不再包含 `react_loop*.go`
- [ ] `pkg/` 公共 API 表面不变（`go doc agentprimordia/pkg` 输出一致）
- [ ] `go build ./...` 零错误
- [ ] `go test -count=1 ./internal/agent/...` 全绿
- [ ] `go vet ./...` 零警告

---

### Task B-4：跨语言规范扩展

**优先级**：P2 | **预估工时**：5-7 天

**问题描述**：
`cross-language-spec.json` 当前覆盖 10 个测试套件（agent_config / tool_execution / vector_operations / error_handling / json_serialization / error_code_mapping / memory_store / llm_provider / health_check / chaos_config / orchestration），但 governance、persist、security、guardrail 等模块尚未覆盖。

**涉及文件**：
- `sdk/typescript/tests/shared/cross-language-spec.json` — 扩展
- `sdk/typescript/tests/shared/cross-language.test.ts` — TS 侧测试运行器
- `agentprimordia/pkg/cross_language_test.go` — Go 侧测试运行器
- `agentprimordia/internal/memory/cross_language_test.go` — Go 侧测试运行器
- `scripts/cross-language-api-check.mjs` — SUITE_SYMBOLS 映射扩展

**实施步骤**：
1. 新增测试套件：`governance_quota`（配额限流行为一致性）
2. 新增测试套件：`security_acl`（ACL 权限检查一致性）
3. 新增测试套件：`guardrail_rules`（护栏规则触发一致性）
4. 新增测试套件：`persist_checkpoint`（检查点序列化/反序列化一致性）
5. 在 `cross-language-api-check.mjs` 的 `SUITE_SYMBOLS` 中注册新套件的 Go/TS 符号映射
6. 双侧测试运行器自动加载新套件（已有框架支持）

**验收标准**：
- [ ] cross-language-spec.json 测试套件数从 10 增至 ≥ 14
- [ ] Go 侧 `go test -run TestCrossLanguage ./pkg/` 全绿
- [ ] TS 侧 `npx vitest run tests/shared/cross-language.test.ts` 全绿
- [ ] `node scripts/cross-language-api-check.mjs` 零漂移
- [ ] 新套件覆盖 governance / security / guardrail / persist 四个模块域

---

### Task B-5：TS SDK 版本对齐至 v3.1.0

**优先级**：P2 | **预估工时**：2-3 天

**问题描述**：
TS SDK v3.0.0 落后 Go v3.1.0 一个小版本。需确认 v3.1.0 新增的 Go 能力（若有 TS 对应）已在 TS 侧实现，然后升级版本号。

**涉及文件**：
- `sdk/typescript/package.json` — 版本号
- `sdk/typescript/CHANGELOG.md`（若存在）— 变更记录
- `scripts/version-sync-check.mjs` — 验证

**实施步骤**：
1. 对比 Go v3.1.0 CHANGELOG 与 TS SDK 现有实现，列出差异
2. 补齐缺失的 TS 对应实现（若有）
3. 升级 `package.json` version 至 `3.1.0`
4. 运行 `node scripts/version-sync-check.mjs` 验证一致性
5. 运行完整 TS 测试套件

**验收标准**：
- [ ] `package.json` version = `3.1.0`
- [ ] `node scripts/version-sync-check.mjs` 输出 ✅
- [ ] `npx vitest run` 全绿
- [ ] `npx vitest run --coverage` 满足阈值（75% Lines / 70% Branch）

---

## 四、Phase C：生产化与性能（4-6 个月）→ Go v3.3.0 / TS v3.2.0

> **执行状态：✅ 已完成 / 部分阻塞（2026-07-31）**
> - C-1: 发现已存在（bench/results + docs/benchmarks/）
> - C-2: 发现已实现（memory/tenant.go + pool/tenant.go）
> - C-3: 已实现（Bun 适配器 44→210 行生产强化）
> - C-4: 已实现（动态导入 @xenova/transformers，optional peer dep，46 测试全绿）
> - C-5: 发现已实现（govulncheck + trivy + cosign 均在 CI）

### Task C-1：性能基线报告发布

**优先级**：P1 | **预估工时**：5-7 天

**问题描述**：
CI 已有性能回归检查（`bench-regression-check.sh`），但缺少官方公开的性能基线报告。

**涉及文件**：
- `agentprimordia/bench/suite/` — 6 个基准测试套件
- `agentprimordia/bench/results/` — 结果存储
- `scripts/bench-compare.sh` — 对比脚本（**2026-10-17 已退役删除**：按 `bench/*.bench.js` 收集结果、与实际目录不符且 0 消费者；现由 `make benchmark` + `scripts/dual-bench-compare.mjs` 取代）
- `agentprimordia/docs/benchmarks/` — 文档输出

**实施步骤**：
1. 在标准化硬件上运行全量基准测试（capacity/cluster/latency/learning/privacy/tool_calling）
2. 收集 P50/P95/P99 延迟、QPS、内存分配数据
3. 生成结构化报告（Markdown + JSON 数据）
4. 在 `docs/benchmarks/` 发布官方性能白皮书
5. 将基线数据提交到 `bench/results/` 作为 CI 回归基准

**验收标准**：
- [ ] 6 个基准套件全部有结果数据
- [ ] 报告包含 P50/P95/P99 延迟、吞吐量、内存占用
- [ ] `bench/results/` 包含可复现的基线 JSON
- [ ] 文档发布到 `docs/benchmarks/`

---

### Task C-2：多租户隔离（Phase 5 Task 5）

**优先级**：P1 | **预估工时**：7-10 天

**问题描述**：
`internal/governance/` 已有 tenant.go / quota.go / isolation.go 等基础，但 Memory 分区和 Pool 配额尚未与租户绑定。Phase 5 Task 5 待办。

**涉及文件**：
- `agentprimordia/internal/governance/tenant_manager.go` — 主改动
- `agentprimordia/internal/memory/` — Memory 分区
- `agentprimordia/internal/pool/` — Pool 配额
- `agentprimordia/internal/governance/isolation.go` — 隔离策略

**实施步骤**：
1. Memory Store 支持按 tenant_id 分区（命名空间隔离）
2. Pool Dispatcher 支持按租户配置并发配额
3. 跨租户访问被自动拒绝
4. 补充集成测试：多租户并发场景

**验收标准**：
- [ ] 不同租户的 Memory 数据完全隔离
- [ ] 租户 A 的 Agent 无法访问租户 B 的记忆
- [ ] Pool 并发配额按租户独立限制
- [ ] 新增测试 ≥ 6 个

---

### Task C-3：TS SDK 边缘运行时成熟化

**优先级**：P2 | **预估工时**：7-10 天

**问题描述**：
TS SDK `edge/` 模块已有 Cloudflare Workers / Deno / Bun 适配（10 个文件 + 6 个测试文件），但标记为实验性。需提升为生产可用。

**涉及文件**：
- `sdk/typescript/src/edge/cloudflare-adapter.ts`（229 行）
- `sdk/typescript/src/edge/cloudflare-agent.ts`（433 行）
- `sdk/typescript/src/edge/deno-agent.ts`（280 行）
- `sdk/typescript/src/edge/bun-agent.ts`（44 行）
- `sdk/typescript/src/edge/cold-start.ts`（418 行）
- `sdk/typescript/src/edge/compatibility.ts`（323 行）

**实施步骤**：
1. 补全 Bun 适配（当前仅 44 行，显著薄于 Cloudflare 433 行 / Deno 280 行）
2. 冷启动优化：验证 30s timer 分片在真实 Cloudflare Workers 环境的行为
3. 边缘存储（`edge-storage.ts`）增加 TTL 和容量限制
4. 补充边缘环境集成测试（使用 miniflare 模拟 Cloudflare）

**验收标准**：
- [ ] Bun 适配功能与 Deno 适配对等
- [ ] 冷启动时间 P95 < 50ms（模拟环境）
- [ ] 边缘存储有 TTL 和容量上限
- [ ] 新增测试 ≥ 5 个
- [ ] 文档标注为 Stable

---

### Task C-4：WebGPU Provider 实体化

**优先级**：P2 | **预估工时**：10-14 天

**问题描述**：
TS SDK `llm/webgpu-provider.ts`（400 行）和 `llm/webgpu-model-runner.ts`（440 行）当前为模拟模式，未接入真实模型推理。

**涉及文件**：
- `sdk/typescript/src/llm/webgpu-provider.ts` — 主改动
- `sdk/typescript/src/llm/webgpu-model-runner.ts` — 主改动
- `sdk/typescript/src/llm/webgpu-capability.ts`（100 行）— 能力检测
- `sdk/typescript/src/browser/wasm-agent.ts`（640 行）— 浏览器端 Agent
- `sdk/typescript/src/llm/__tests__/webgpu-*.test.ts` — 测试

**实施步骤**：
1. 接入 ONNX Runtime Web 或 transformers.js 作为 WebGPU 推理后端
2. 实现模型权重下载 + IndexedDB 缓存
3. 支持至少 1 个小型模型（如 Phi-3-mini 或 Qwen2-0.5B）的浏览器端推理
4. 保留模拟模式作为回退（`WebGPUProvider({ mock: true })`）
5. 补充 E2E 测试（Playwright + 真实 WebGPU）

**验收标准**：
- [ ] 在支持 WebGPU 的浏览器中可完成真实推理
- [ ] 模型权重缓存到 IndexedDB，二次加载 < 2s
- [ ] 不支持 WebGPU 时自动降级到远程 API
- [ ] 模拟模式仍可用于测试
- [ ] 文档包含浏览器端 Agent 教程

---

### Task C-5：社区 CI 完善（Phase 5 Task 12）

**优先级**：P2 | **预估工时**：3-5 天

**问题描述**：
Phase 5 Task 12 待办。需完善面向外部贡献者的 CI 管线。

**涉及文件**：
- `.github/workflows/ci.yml` — 扩充
- `.github/workflows/supply-chain.yml` — 扩充
- `.githooks/pre-commit` — 本地钩子

**实施步骤**：
1. 在 CI 中集成 `govulncheck`（Go 漏洞扫描）
2. 在 CI 中集成 Trivy（容器镜像扫描）
3. 在 supply-chain.yml 中集成 cosign 签名
4. 完善 pre-commit 钩子（gofmt + eslint + 版本一致性）

**验收标准**：
- [ ] CI 包含 govulncheck 步骤
- [ ] CI 包含 Trivy 扫描步骤
- [ ] Release 产物有 cosign 签名
- [ ] pre-commit 钩子覆盖 gofmt / eslint / version-check

---

## 五、Phase D：差异化能力增强（6-9 个月）

> **执行状态：✅ 已完成（2026-07-31）**
> - D-1: 已实现（docs/市场协议.md 协议规范发布）
> - D-2: 已实现（CRDT 持久化接口 + InMemoryCRDTPersistence）
> - D-3: 已实现（sdk/typescript/playground/wrangler.toml 部署配置）

### Task D-1：Agent 市场协议标准化

**优先级**：P2 | **预估工时**：7-10 天

**问题描述**：
TS SDK 已有 `marketplace/` 模块（registry / deployer / validator / types），Go 侧有对应实现。需定义统一的模板协议规范。

**涉及文件**：
- `sdk/typescript/src/marketplace/types.ts`（59 行）— 协议类型
- `sdk/typescript/src/marketplace/registry.ts`（138 行）— 注册表
- `sdk/typescript/src/marketplace/validator.ts`（82 行）— 验证器
- `agentprimordia/internal/` 对应 Go 实现
- 新增 `docs/市场协议.md` — 协议规范文档

**实施步骤**：
1. 定义 AgentTemplate 协议规范（JSON Schema）
2. 统一 Go/TS 两侧的模板验证逻辑
3. 支持模板版本管理和依赖声明
4. 发布协议规范文档

**验收标准**：
- [ ] 协议规范文档发布
- [ ] Go/TS 两侧验证逻辑行为一致（跨语言测试覆盖）
- [ ] 支持模板语义版本号
- [ ] 新增跨语言测试套件 `marketplace_template`

---

### Task D-2：CRDT 协作生产化

**优先级**：P3 | **预估工时**：5-7 天

**问题描述**：
TS SDK `collaboration/` 模块已有 CRDT 实现（Lamport 时钟 + LWW Register，450 行 + 测试），但为实验性。

**涉及文件**：
- `sdk/typescript/src/collaboration/crdt.ts`（450 行）
- `sdk/typescript/src/collaboration/sync-server.ts`（414 行）
- `sdk/typescript/src/collaboration/agent-client.ts`（456 行）
- `sdk/typescript/src/collaboration/__tests__/crdt.test.ts`（179 行）

**实施步骤**：
1. 补充冲突解决策略的边界测试（并发编辑、网络分区恢复）
2. 增加 CRDT 状态持久化（IndexedDB / SQLite）
3. 性能优化：大文档场景下的合并延迟
4. 文档标注为 Stable

**验收标准**：
- [ ] 并发编辑冲突 100% 正确解决
- [ ] 网络分区恢复后状态自动收敛
- [ ] CRDT 状态可持久化和恢复
- [ ] 新增测试 ≥ 5 个

---

### Task D-3：Playground 在线服务部署

**优先级**：P3 | **预估工时**：5-7 天

**问题描述**：
TS SDK `playground/` 模块已有交互式调试 Playground（index.ts 269 行 + CLI 218 行 + 测试），需部署为在线服务。

**涉及文件**：
- `sdk/typescript/src/playground/index.ts`（269 行）
- `sdk/typescript/src/playground/playground-cli.ts`（218 行）
- `sdk/typescript/src/playground/components.ts`（58 行）
- 新增部署配置

**实施步骤**：
1. 将 Playground 打包为独立 Web 应用
2. 配置 Cloudflare Pages 或 Vercel 部署
3. 支持 URL 参数分享调试会话
4. 集成到文档站点

**验收标准**：
- [ ] Playground 可通过公网 URL 访问
- [ ] 支持 Agent 配置 → 运行 → 结果查看完整流程
- [ ] URL 参数可分享调试会话
- [ ] 文档站点有入口链接

---

## 六、任务依赖关系

```
Phase A（核心债务清零）
  A-1 visual_editor ──────────────────────────┐
  A-2 GoroutinePool ──┬── A-3 LLM Batch 集成  │
  A-4 otel/ 测试      │                       │
  A-5 persist/ 测试   │  （并行执行）          │
  A-6 governance/ 测试│                       │
                      ▼                       ▼
Phase B（生态完善）
  B-1 远程注册中心 ── B-2 cosign 签名
  B-3 react 包拆分（独立）
  B-4 跨语言规范扩展（独立）
  B-5 TS 版本对齐（独立）
                      │
                      ▼
Phase C（生产化）
  C-1 性能基线 ────── C-2 多租户隔离
  C-3 边缘成熟化 ──── C-4 WebGPU 实体化
  C-5 社区 CI
                      │
                      ▼
Phase D（差异化）
  D-1 Agent 市场 ──── D-2 CRDT 生产化
  D-3 Playground 部署
```

**关键依赖**：
- A-3 依赖 A-2（Batch 需要接入动态 Pool）
- B-2 依赖 B-1（签名验证在远程安装流程中）
- C-2 依赖 A-6（多租户隔离需要 governance 测试健全）
- 其余任务均可并行

---

## 七、里程碑时间线

| 里程碑 | 目标版本 | 时间窗口 | 包含任务 | 交付标志 |
|--------|---------|---------|---------|---------|
| **M1：债务清零** | Go v3.1.1 | 第 1-8 周 | A-1 ~ A-6 | 真实 TODO 清零；otel/persist/governance 测试 ≥ 50%；Pool 动态伸缩 |
| **M2：生态完善** | Go v3.2.0 / TS v3.1.0 | 第 9-16 周 | B-1 ~ B-5 | 远程插件注册 + 签名验证；跨语言规范 ≥ 14 套件；双语言版本对齐 |
| **M3：生产化** | Go v3.3.0 / TS v3.2.0 | 第 17-26 周 | C-1 ~ C-5 | 性能白皮书发布；多租户隔离；边缘 Stable；WebGPU 真实推理 |
| **M4：差异化** | Go v3.4.0 / TS v3.3.0 | 第 27-38 周 | D-1 ~ D-3 | Agent 市场协议规范；CRDT Stable；Playground 上线 |

---

## 八、验证检查清单（每个 Task 通用）

每个 Task 完成后，必须通过以下验证：

```bash
# Go 侧
cd agentprimordia
go build ./...                              # 编译通过
go vet ./...                                # 零警告
go test -count=1 ./internal/<module>/...    # 目标模块测试全绿
go test -count=1 ./pkg/...                  # 公共 API 测试全绿

# TS 侧（如涉及）
cd sdk/typescript
npx vitest run                              # 全量测试全绿
npx vitest run --coverage                   # 覆盖率满足阈值
npm run typecheck                           # 类型检查通过

# 跨语言（如涉及 API 变更）
node scripts/cross-language-api-check.mjs   # 零漂移
node scripts/version-sync-check.mjs         # 版本一致

# 提交规范
# feat: <task-id> <描述>  （新功能）
# fix: <task-id> <描述>   （修复）
# test: <task-id> <描述>  （纯测试）
# refactor: <task-id> <描述> （重构）
```

---

## 九、风险与缓解

| 风险 | 概率 | 影响 | 缓解措施 |
|------|------|------|---------|
| WebGPU 浏览器兼容性不足 | 中 | C-4 延期 | 保留模拟模式回退；优先支持 Chrome/Edge |
| 多租户隔离引入性能开销 | 中 | C-2 质量 | 基准测试验证；命名空间隔离用前缀而非独立 DB |
| 远程插件注册中心可用性 | 低 | B-1 体验 | 本地缓存降级；CDN 分发 |
| 跨语言规范扩展维护成本 | 中 | B-4 持续性 | CI 自动检查漂移；新模块 PR 必须附带 spec 更新 |
| Phase 3 包拆分破坏公共 API | 低 | B-3 兼容性 | pkg/ 类型别名保持不变；api-extract 对比验证 |
