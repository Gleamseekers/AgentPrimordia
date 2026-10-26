# AgentPrimordia v7.5 发布说明（当前补丁 v7.5.2）

> **v7.5.0 作废**：模块路径发布链路修复（go.mod 移至仓库根、/v7 后缀、移除 replace）在 v7.5.0 tag 首次推送后才完成，代理已缓存损坏版本。v7.5.1 为正式发布版，安装命令见文末。

> 发布日期：2026-09-29（v7.5 线）／2026-10-17（补丁 v7.5.2）  
> 版本：v7.5.2（v7.5.0 已作废，见文首说明）  
> 代号：评估修复战役（Assessment Remediation）

---

## 补丁 v7.5.2（2026-10-26）

**门禁可信度修复 + shell jail 逃逸修复 + A1 断言面加固 + 文档与实测对账**；Go 生产代码变更 2 处（`internal/tools/builtin/shell.go`、`pkg/llm.go` 补导出 `NewDeepSeekProvider`），另有 `pkg/agent.go` 的 `const Version` 推进；无 API 破坏，但含一处**安全行为收紧**（shell 相对路径 `../../` 逃逸自此被拒）。
wasm 沙箱默认 fail-closed 翻转与 GLM/Qwen 多模态重构**不在本版**——按 `版本规范.md` §实验性 API（wasm 的不兼容变更应发生在次版本号）推迟至 v7.6.0。

- 修死门：`make cover-check` 覆盖率阈值判断因 Makefile 跨行变量丢失而恒真（此前不达标也打印 OK），并补齐退出码检查与空覆盖率 FAIL。
- 接孤儿门：S0-3 双线召回对账门（`scripts/dual-line-recall-check.mjs`）此前全仓零消费者，现接入 CI job `dual-line-recall` 与 `make dual-line-recall`，且以"重跑两侧真臂 → 对账 → 夹具零 diff"三步执行。
- 补断言：A4（wasm 零值配置回退）此前无 CI 覆盖，新增 job `workspace-modules`（wasm/pgvector/gateway 三模块编译 + 测试）。
- 堵禁锢洞：`shell` 工具的 workdir jail 原只校验绝对路径形态，`cat ../../etc/passwd` 从受禁锢的 `cmd.Dir` 出发即可越界；现按生效 workdir 解析（`filepath.Join` 折叠 `..`）后再判定，允许集统一归一为绝对路径，并新增 `shell_security_test.go` 固化。
- 加固 A1：`host_boundary_test.go` 的 `moduleRoots` 由 2 个扩为 `go.work` 全部模块，wazero 匹配由精确等值改为覆盖全部子包（原实现换一条 `wazero/api` 导入即可绕过），并新增 `TestA1_WazeroImportMatcher` 与 grep fail-closed。
- 文档对齐实测：internal 包口径统一为「顶层 28 包 / agent 子包 34 个 / agent 子树 Go 包 37 个」，并入 `doc-version-consistency.mjs` 锚点。
- 完整明细见 [`docs/发布说明-v7.5.2.md`](../../docs/发布说明-v7.5.2.md)。

---

## 核心亮点

**v7.5 是一次安全与质量战役的交付，并让框架首次具备 GOPROXY 分发能力。**

对全仓 302K 行 Go 的深度评估实证了 4 个 P0（默认 Shell 工具 7 条攻击链 RCE、jsonutil 并发请求体覆写、Anthropic 公开 API panic、167 个测试被禁用）与 12 项 P1——全部以 TDD 修复并以回归测试固化。同时五项工作区模块迁移至 `github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia` 命名空间，`go install` 随 tag 发布即可用。

- 测试文件 622 → **659**，总覆盖率 78.8% → **81.1%**
- 评估报告与全程实证记录：`docs/项目深度评估报告-2026-09-28.md`

---

## 安全修复（P0）

### Shell 工具白名单加固（7 条攻击链实证→全阻断）
- 默认白名单收敛：移除 env/printenv/python*/node/go/find/git 等可派生进程/解释代码条目（显式 opt-in 保留灵活性）
- 显式 args 参数与 command 字符串走同一套元字符校验（此前 args 完全绕过唯一注入过滤）
- 绝对路径 token 过 allowedWorkdirs/Sandbox；`DefaultToolkit` 的 shell 默认禁锢 RootDir
- 黑名单匹配 token 化（修复 `rm -fr` flags 重排、`/bin/rm` 前缀、大小写变形绕过）
- `extractPathFromArgs` 完整 JSON 递归 + 扩充 key 集合 + 解析失败 fail-closed
- 敏感文件模式补 authorized_keys/shadow/kdbx 且大小写不敏感（`.ENV` 曾绕过 `*.env`）

### jsonutil.Marshal 并发请求体覆写
- pooled buffer 归还后返回其底层切片 → 并发下被其他请求整体覆写（实测 2000 次并发 99.5% 损坏，兼跨会话数据泄漏）
- 修复：归还前拷贝；`TestMarshalConcurrentNoAliasing` 固防

### Anthropic 结构化输出 nil panic
- `ResponseFormat{Type: json_object}`（无 schema）触发公开 API panic → 通用兜底 tool；json_schema 缺 schema 返回明确 error

### 恢复 167 个被禁用的测试
- 12 个 `//go:build ignore` 测试文件去标签并修复腐烂（符号冲突/类型漂移/API 迁移）
- 顺带修复被测出的两个真实缺陷：`DiscoveryServer.Addr()` 返回真实绑定地址、`AuthenticatedDiscovery.Register` 角色合并进 Capabilities

---

## 质量修复（P1/P2 摘要）

| 域 | 项 |
|----|----|
| llm | Stats 原子读（-race 实证）；ctx 取消不再误触发熔断；transport 包级单例；流式长流不再被 Client.Timeout 掐断；缓存键切完整请求指纹（消除假命中）；TTL goroutine 可关闭；LSH 索引清理 |
| tools | RegisteringCreator INV-0 门控（宿主零执行 agent 生成代码）；插件安装器穿越净化+先验后写+256MB 上限；MCP 常量时间认证+WithAPIKey；stdio 64KB 挂起修复；子进程 env 白名单（宿主密钥不泄露）；SSRF 残留段补全（CGNAT/6to4/NAT64 等）；web/http_client 连接池共享+10MB 请求体上限；data_tools rootDir jail+SQLite 行数上限；WithBlacklist 废弃标注 |
| agent | trace span 全部闭合（8 路径回归）；startTime 锁纪律；锁顺序注释修正；HITL 并行不再挂死；摘要 goroutine 有界池；runLoop 134 行/cc23→71 行/cc11；流式 tool_calls 单请求化（费用/延迟减半）+Usage 三档填充 |
| 测试 | 覆盖率补课：federation 33%→98%、config 64%→98%、a2a 66%→86%；补课实测修复 5 个生产缺陷（a2a interop_server 数据竞争、federation 重放刷声誉、SimulatePartitionRecovery 口径、MinReputation 负门槛、FileRegistry Endpoints） |
| 遗留 | FalsePositives 误拦口径接线；config FlagSet 注入；intelligence pkg 导出（ecosystem internal 依赖清零）；extractGapKey 等三处 map 迭代非确定性修复 |

## v7.4 接线批次（随本版交付）

OTel 端到端接线、Studio 接真实引擎、模板注册表持久化+远程协议、工具学习器自动装配、全链路关联存储、技能库持久化+入循环、自治 CLI 真实化（七项，明细见 `docs/实验性能力清单.md`）。

## 模块路径迁移（Changed）

| 模块 | 新路径 |
|------|--------|
| 主模块（仓库根 go.mod） | `github.com/Gleamseekers/AgentPrimordia/v7`（包路径如 `.../agentprimordia/cmd/ap`） |
| pgvector | `github.com/Gleamseekers/AgentPrimordia/pgvector` |
| operator | `github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/operator` |
| gateway | `github.com/Gleamseekers/AgentPrimordia/gateway` |
| wasm 沙箱 | `github.com/Gleamseekers/AgentPrimordia/wasm` |

旧路径首段无点号导致模块从未可经 GOPROXY 解析（无下游消费者，无兼容影响）。源码用户 import 路径需更新；本地 replace 消费方式见 `agentprimordia/docs/版本规范.md`。

## 安装

```bash
go install github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/cmd/ap@latest
```

## 验证门（发布前全绿）

5 模块 build / go vet / 102 包 test exit 0 / -race 零竞争 / TS SDK 2766 测试+tsc+tsup / stability 双门 / deprecation 残留门 / 跨语言 API 检查 / 文档死链 303 条 / chaos e2e / security 覆盖率 90.6%。

## 升级

- 新用户：`go install github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/cmd/ap@latest`
- 源码 clone 用户：import 路径 `agentprimordia/...` → `github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/...`
- 本地 replace 用户：go.mod require/replace 更新为新路径（`ap init` 已内置）
