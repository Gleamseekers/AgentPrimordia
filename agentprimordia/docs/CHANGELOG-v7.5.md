# AgentPrimordia v7.5 发布说明

> 发布日期：2026-09-28  
> 版本：v7.5.0  
> 代号：评估修复战役（Assessment Remediation）

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
