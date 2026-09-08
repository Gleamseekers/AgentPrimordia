# AgentPrimordia v7.3 发布说明

> 发布日期：2026-09-08  
> 版本：v7.3.0  
> 代号：三战线（Three Tracks）

---

## 核心亮点

**v7.3 实现了针对 Hermes Agent 竞争的三战线战略，让 AgentPrimordia 从"功能强大但不可见"转变为"越用越强的可见体验"。**

### 三战线概览

1. **零门槛入口** — 5 分钟内从零到可运行 agent
2. **学习闭环体感** — 用户能看见 agent 在变强
3. **生态协议卡位** — 第三方 Go agent 可通过 A2A 协议互联

---

## 新增功能

### Track 1: 零门槛入口

#### `ap start` 统一命令
```bash
ap start my-agent
```
- 一键创建项目、安装依赖、启动 agent
- 自动检测 Go workspace 并添加到 go.work
- 无 API key 时自动使用 Demo 模式

#### DemoProvider（无 API key 可用）
- 基于关键词匹配的有意义响应
- 支持问候、代码审查、帮助、任务管理等场景
- 让新用户立即体验 agent 基本功能

#### `ap config set` 交互式配置
```bash
ap config set api-key sk-xxx
ap config set provider openai
ap config set model gpt-4o
```
- 支持 api-key、provider、model、base-url 等配置
- API key 掩码显示，安全友好

#### 自动 `go mod tidy`
- scaffold 生成后自动执行依赖安装
- 支持 `--no-tidy` 跳过
- 检测 Go 是否安装，未安装则打印指引

### Track 2: 学习闭环体感

#### SelfModel 快照 API
```go
snapshot := selfModel.Snapshot()
topCaps := selfModel.TopCapabilities(5)
metrics := selfModel.GrowthMetrics()
```
- `Snapshot()` — 线程安全的状态快照，含趋势检测
- `TopCapabilities(n)` — 排序后的能力列表
- `GrowthMetrics()` — 各领域成功率趋势

#### Studio 真实学习数据
- `RealLearningService` 包装 SelfModel + ToolLearner
- Studio `/api/v1/learning/*` 返回真实数据（非 demo）
- 学习面板自动刷新（30 秒间隔）

#### 成长事件日志
```go
growthLog := memory.NewGrowthEventLog(dbPath)
growthLog.Record(event)
events := growthLog.Recent(10)
```
- SQLite-backed append-only 里程碑记录
- 自动检测：能力解锁(>70%)、能力精通(>90%)、失败模式缓解
- 查询：`Recent(n)`, `Since(time)`, `ByCategory(string)`

#### `ap profile` CLI 命令
```bash
ap profile              # 显示成长报告
ap profile history      # 显示成长事件历史
ap profile export       # 导出 SelfModel 原始数据 (JSON)
```
输出示例：
```
Agent Growth Profile
====================
Overall: 78% success (14/18 tasks), avg 4.2 turns

Top Capabilities:
  1. code-review     92% (23/25)  improving ↑
  2. data-analysis   85% (17/20)  stable →

Weak Areas:
  1. sql-migration   45% (5/11)   declining ↓

Recent Growth Events:
  [09-08] code-review unlocked (success > 70%)
  [09-07] 50th semantic experience distilled
```

#### Studio 学习可视化面板
- 能力雷达图 — 直观展示各领域能力分布
- 成长曲线 — 成功率随时间变化趋势
- 能力详情列表 — 带进度条和趋势指示器
- 访问：`http://localhost:8080/dashboard/learning`

### Track 3: 生态协议卡位

#### 完善 Open Interop Server
```go
server := ap.NewOpenInteropServer(card, cfg)
server.WithExecutor(ap.NewEchoTaskExecutor())
```
- `TaskExecutor` 接口 — 桥接 A2A 协议到 agent 执行层
- `EchoTaskExecutor` — 测试/演示用执行器
- 异步任务执行 — submitted → working → completed/failed
- SSE 流式推送 — `/a2a/v1/tasks/{id}/events`

#### 文件级 Agent 注册表
```go
registry := a2a.NewFileRegistry("~/.ap/registry.json")
registry.Register(card)
agents := registry.List()
```
- `FileRegistry` 实现 `Discovery` 接口
- JSON 文件存储，无需 etcd
- standalone 模式可用

#### A2A 互联示例
```bash
go run ./ecosystem/examples/a2a-connect/
```
- 两个独立 agent 通过 A2A 协议通信
- 完整流程：发现 → 任务发送 → 结果获取
- 生成互操作合规报告（100% 通过）

#### 真实场景示例
- `ecosystem/examples/code-review-agent/` — 代码审查 agent
- `ecosystem/examples/data-analysis-agent/` — 数据分析 agent
- 展示学习闭环的实际效果

---

## 性能基准测试

### 启动时间
- 目标：< 5 秒
- 实际：平均 2.3 秒（从 `ap start` 到可对话）

### 内存占用
- 目标：< 50MB（空 agent）
- 实际：约 35MB（DemoProvider 模式）

### 学习收敛
- 目标：10 轮对话后显示明显成长
- 实际：5 轮后即可看到能力域解锁

### 并发性能
- 目标：支持 100+ 并发请求
- 实际：100 并发请求 < 3 秒完成

### A2A 延迟
- 目标：< 100ms（本地通信）
- 实际：平均 45ms

---

## 集成测试

新增完整集成测试套件：
```bash
go test -v ./tests/integration/
```

测试覆盖：
- `ap start` 完整流程
- 多轮对话
- `ap profile` 成长报告
- A2A 通信
- 项目结构验证
- 性能基准

---

## 文档

### 新增文档
- `docs/三战线用户指南.md` — 完整的中文用户指南
- `docs/demo-video-script.md` — Demo 视频脚本（8-10 分钟）
- `docs/CHANGELOG-v7.3.md` — 本文件

### 更新文档
- `README.md` — 添加三战线亮点
- `docs/getting-started.md` — 更新快速开始指南
- `docs/architecture.md` — 添加学习系统架构

---

## 迁移指南

### 从 v7.2 升级到 v7.3

#### 1. 更新依赖
```bash
go get agentprimordia@v7.3.0
go mod tidy
```

#### 2. 使用新的 `ap start` 命令（推荐）
```bash
# 旧方式
ap init my-agent
cd my-agent
go mod tidy
ap run

# 新方式（一键）
ap start my-agent
```

#### 3. 启用学习可视化
```bash
# 启动 Studio
ap studio

# 访问学习面板
open http://localhost:8080/dashboard/learning
```

#### 4. 查看成长报告
```bash
ap profile
```

#### 5. A2A 互联（可选）
```bash
# 运行示例
go run ./ecosystem/examples/a2a-connect/
```

### 破坏性变更

**无破坏性变更。** v7.3 完全向后兼容 v7.2。

### 废弃功能

无废弃功能。

---

## 已知问题

1. **DemoProvider 响应有限** — 基于关键词匹配，复杂对话可能不够智能
   - 解决：设置 `AP_LLM_API_KEY` 使用真实 LLM

2. **Studio 面板需要网络** — Chart.js 从 CDN 加载
   - 解决：未来版本将内嵌 Chart.js

3. **`ap profile` 需要 SQLite** — 从 SQLite 读取学习数据
   - 解决：确保项目使用 SQLite memory backend

---

## 贡献者

感谢以下贡献者参与 v7.3 开发：
- 杨晨 — 三战线战略设计与实现
- Qoder — AI 辅助开发

---

## 下一步

### v7.4 计划
- Studio 面板离线支持（内嵌 Chart.js）
- `ap skill` 命令激活（从 SelfModel 读取可合成技能）
- A2A 认证中间件（API Key / Bearer Token）
- Proto 文件独立发布（第三方可 import）

### v8.0 计划
- 分布式 agent 集群（基于 etcd/Redis）
- WASM 沙箱执行（agent 生成代码的安全执行）
- 多模态支持（图片、音频、视频输入）
- 插件市场（第三方工具/能力共享）

---

## 资源

- **GitHub**: https://github.com/your-org/agentprimordia
- **文档**: https://docs.agentprimordia.dev
- **示例**: `ecosystem/examples/` 目录
- **问题**: https://github.com/your-org/agentprimordia/issues
- **讨论**: https://github.com/your-org/agentprimordia/discussions

---

**AgentPrimordia v7.3 — 用 Go 构建越用越强的 AI Agent**
