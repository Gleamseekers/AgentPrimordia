# 提案：模块路径迁移以支持 `go install` / GOPROXY 分发

> **状态**：✅ **已执行**（2026-09-28，维护者批准后当日完成；本文档保留为决策记录与迁移方法论文档）
> **提出日期**：2026-09-28（项目深度评估收尾）
> **关联**：评估报告 §4.3 P2 项"模块路径不可 GOPROXY 解析"

## 一、问题

主模块路径为 `agentprimordia`（`agentprimordia/go.mod`），首段不含点号：

1. `go install agentprimordia/cmd/ap@latest` **不可用**——GOPROXY 无法解析无点号模块路径；
2. `v2+` 标签受语义化导入版本（SIV）规则限制——模块路径不含 major 版本后缀时无法发布 v2；
3. 用户只能 `git clone` 源码构建（README 已诚实说明，但对采用率是真实摩擦）；
4. `ap init` 脚手架生成的项目需 emit `replace` 指向本地框架源码（gomod_template 的复杂装配即为此存在）。

## 二、方案（已采纳 A 并执行）

### 方案 A：迁移到 GitHub 路径（已执行）

```
agentprimordia → github.com/Gleamseekers/AgentPrimordia
```

- **收益**：`go install github.com/<org>/agentprimordia/cmd/ap@latest` 直接可用；GOPROXY 生态完整支持；脚手架无需 replace；与 K8s Operator / SDK 的发布链路一致。
- **成本**：全仓 Go import 路径批量替换（主模块 ~1300 文件 + pgvector/operator/gateway/wasm 四模块 + 全部示例/插件/模板/测试）；go.work use 路径不变（目录不变）；下游用户 import 路径变更（破坏性，需 major 版本窗口 + 迁移指南）。
- **风险控制**：`gofmt -r` 或 `golang.org/x/tools/cmd/goimports -w` 批量替换 + `go build ./...` + 全测试 + CI 全绿后合并；保留一版 `docs/migration/v8-import-path.md`。

### 方案 B：保留现路径，仅补文档与脚手架体验

- `ap start`/`ap init` 已自动生成自洽 replace（现状可用）；
- 成本近零，但 `go install` 永不可用，生态采用持续摩擦。

### 方案 C：Go workspaces 官方推荐姿势文档化

- 引导用户以 workspace 模式消费（`go work init` + replace），不变模块路径；
- 适合内部/ vendored 消费，不适合公开分发。

## 三、执行记录（方案 A）

维护者于评估当日批准执行。实际动作与预演的差异：

1. GitHub org/仓库名：Gleamseekers/AgentPrimordia（git remote 确认）；
2. 批量替换：1390 个 Go 文件（import 路径）+ 5 个 go.mod（module 行与跨模块
   require/replace）+ 49 个文档的 import 代码块；CI paths-filter 用目录路径
   不受影响；脚手架模板（cmd/ap/scaffold.go）的模块探测与发射逻辑同步更新；
3. 全量验证：5 模块 build + go vet + go test ./... 全绿 + go mod tidy 零漂移；
4. README 安装说明更新为 go install 优先；版本规范.md 模块消费章节重写；
5. GOPROXY 实解析待下一个 tag 发布后验证（`GOPROXY=https://proxy.golang.org
   go install github.com/Gleamseekers/AgentPrimordia/agentprimordia/cmd/ap@latest`）。

**若短期不做公开分发 → 方案 B/C**，本提案关闭。

## 四、影响面清单（方案 A 预演）

| 面 | 位置 | 动作 |
|----|------|------|
| 模块声明 | 5 个 go.mod 的 `module` 行 + go.work | 替换 |
| import 路径 | 全仓 `agentprimordia/...` import | 批量替换 |
| 文档 | README/API 参考/各模块文档的 import 示例 | 批量替换 |
| CI | 缓存 key（hashFiles('**/go.sum') 不受影响）、supply-chain 镜像名 | 核对 |
| 脚手架 | cmd/ap/scaffold 的 go.mod 模板（replace 逻辑可简化） | 简化 |
| 下游 | 生态插件/示例若被外部引用 | major 版本 + 迁移指南 |
