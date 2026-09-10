
# AP（AgentPrimordia）完整开发手册

> 主文档：面向三类读者——**只用框架的开发者**读第一部分即可动手；**为框架贡献代码的工程师**加读第二、三部分；遇到问题直接查第四部分 FAQ。
>
> 手册内所有命令与行为均经实际验证（2026-08-27，go1.26.6）。CLI 内部实现细节深潜见 `AP-CLI开发手册.md`；图文版入门三连见 `docs/getting-started/`。
>
> English edition: `AP-开发手册-EN.md`（逐节对应 / section-by-section mirror）.

# 第一部分 · 新手教程


## 1. 环境准备

| 要求 | 说明 |
|---|---|
| Go ≥ 1.26 | 工具链在 go.mod 锁定 1.26.6，toolchain 指令会自动拉取 |
| git | monorepo 以 go.work 组织：根目录 5 个模块（agentprimordia 主模块、pgvector、operator、gateway、wasm），克隆即用无需额外 setup |
| LLM API Key | 任选：OpenAI / Anthropic / Gemini / Ollama(本地免Key) / Azure / DeepSeek / GLM / Cohere / Mistral / Qwen |

规模预期：主模块 134 包、实现约 13 万行——首次构建下载依赖需要几分钟，之后走本地缓存。

## 2. 构建 ap 与测试环境

```
make build            # 或: cd agentprimordia && go build -o "$(go env GOPATH)/bin/ap" ./cmd/ap
make test-short       # 快速全量回归；完整门禁还有 test-race / test-integration
```

常用 Makefile 目标：build · test · test-race · test-short · test-integration · test-distributed-backends · cover · cover-html · cover-check · api-diff · api-extract · cover-trend · deprecation-check · benchmark。

> ⚠ 跨目录调用：不要用 go run 绝对路径/cmd/ap——模块外执行报 go.mod file not found。先 build 出二进制再运行。

## 3. 五分钟跑通第一个 Agent

```
ap init myagent --template quickstart   # 可加 --dry-run 预览；共 7 模板 × agent/plugin/provider 三类型
cd myagent
export AP_LLM_API_KEY=sk-xxx             # 或写进 .ap.yaml（环境变量优先）
export AP_LLM_MODEL=gpt-4o               # 不设则用 yaml 默认值
ap run                                   # 自动 build → 注入配置 → 启动；改源码自动重建重启（热重载）
```

**生成的工程只有 4 个文件**，逐个读一遍是理解框架的最快路径：

| 文件 | 内容 |
|------|------|
| main.go | 公共 API 最小样例：ConfigFromEnv 读配置 → 缺 Key 即 Fatal 提示 → NewOpenAIProvider(cfg) 建 Provider → 启动 ReAct 循环。改 system prompt 就是你的第一个定制 Agent |
| .ap.yaml | 项目配置：llm(provider/model/api_key)、memory(backend, path)、agent(max_turns, system_prompt)，全量字段见下 |
| go.mod / .gitignore | 依赖声明与忽略规则 |
```
# .ap.yaml 全量字段（生成器默认值）
template: quickstart
llm:
  provider: openai       # openai | anthropic | gemini | ollama | azure | deepseek | qwen
  model: gpt-4o
  # api_key: "sk-xxx"    # 建议用环境变量 AP_LLM_API_KEY，不落盘
memory:
  backend: sqlite        # sqlite | memory
  path: ./data/memory.db
agent:
  max_turns: 20
  system_prompt: "you are a helpful assistant"
```

## 4. 日常开发循环

- 改完代码保存即可：ap run 内置 watchAndRun 监听源码变更，自动重建重启；
- 调试循环：ap loop trace / inspect / resume 观测与恢复 ReAct 执行轨迹；
- 环境体检：ap doctor 四项检查（Go 版本 / 项目配置 / API Key / 依赖）；
- 补全脚本：ap completion 生成 shell 补全。

## 5. 核心概念速览（每个 1 分钟）

| 概念 | 一句话 |
|------|--------|
| ReAct Loop | Agent 引擎：LLM 思考→调工具→观察→循环到产出答案；每轮受 max_turns 约束 |
| Tool | Agent 能力单元：20+ 内置工具，带作用域权限（FilesScope/ShellScope），执行前统一校验 |
| Memory | 对话记忆与知识检索：SQLite(FTS5+向量) / 内存 / RAG(Hybrid Fusion) / 向量库(HNSW) |
| Pool | 多 Agent 并发调度、会话管理、重试 |
| Orchestration | Pipeline / Handoff / DAG / GroupChat / Debate / MapReduce 编排模式 |
| Guardrail | 输入输出护栏**默认接入 ReAct 主循环**：注入检测/PII/主题过滤/敏感工具拦截，命中即 block+审计 |
| Checkpoint | 断点续跑：SQLite 默认；etcd/Redis 后端由 build tag 门控编译 |
| Stability 分级 | 公共 API 四级承诺 Stable/Experimental/Deprecated/Internal，决定升级成本 |

# 第二部分 · 架构地图


## 6. Monorepo 布局与分层纪律

```
/
├── agentprimordia/        # 主模块（134 包）+ operator K8s 子模块
│   ├── cmd/ap/            # CLI 本体
│   ├── pkg/               # 公共 API 唯一对外面（类型别名 re-export + 少量构造器）
│   ├── internal/          # 30 个子包（见 §7 速查卡）
│   ├── ecosystem/         # examples / plugins / templates（只准经 pkg 访问核心）
│   └── bench/             # eval-ci / llm-bench / soak / suite / self-bootstrap / results
├── pgvector/              # 独立向量扩展模块（internal 不得直接 import pgx）
├── gateway/ wasm/         # 独立部署单元 / WASM 执行（wazero，CGO-free）
└── sdk/                   # python / rust / typescript 多语言 SDK（非 Go 模块）
```

**分层规则（实测 134 包零违规，grep 可复核）**：

| 层 | 包 | 禁止反向引用 |
|----|-----|--------------|
| 顶层 | agent/（含 a2a/planning/reflection/skills/autonomy 等 12 子包） | — |
| 核心 | llm/ memory/ persist/ tools/ | 不得 import agent/pool/orchestration/debugger/admin |
| 编排 | pool/ | 处于 tools 下层，可引用 tools/agent 及横向层 |
| 横向 | orchestration debugger metrics otel guardrail security audit eval governance 等 | 可消费上层能力，不得被核心层引用 |

## 7. 子系统速查卡

| 子系统 | 要点 | 最常用入口 |
|--------|------|------------|
| llm | 多家 Provider 同一接口；缓存三级（内存/SQLite/增强）；批处理 | NewOpenAIProvider(cfg) |
| memory | SQLite 默认（FTS5 全文 + 向量列）；RAG 支持 Hybrid Fusion 重排 | WithInMemory() / NewSQLiteStore(path) / NewRAGStore(mem, embedder) |
| tools | Registry 三入口 Register / RegisterMultiple / RegisterPlugin；executor 统一分发；Scope 权限前置校验 | Registry.Register(tool) |
| guardrail | 规则可插拔：injection / pii(Trie 加速) / topic / output / sensitive_tool；命中写审计事件 | hook 挂 Agent 构造项 |
| security | ACL、沙箱、密钥管理（env/memory/vault 三后端 + AES-GCM 加密抽象） | secrets 抽象 |
| marketplace | 插件远程协议：Manifest + cosign(ECDSA P-256) 验签不过拒装 | ap marketplace install |
| persist | Checkpoint 多后端：SQLite 默认；etcd/redis 带 build tag | NewSQLiteCheckpointStore(dsn)；Save / Load(agentID) / List(sessionID) |
| eval | 基准集单一权威 JSON；季度自举 RunQuarterly vs base 对照 + CompareQuarters 回归门 | ap test |
| observability | trace→指标→审计全链路关联；otel 导出桥 | — |

## 8. 多语言 SDK 与跨语言一致性

AP 的对外能力不止 Go 一条路：`sdk/` 下提供 typescript / python / rust 三套 SDK，**Go 是唯一权威实现面**，其余语言经同一份契约与基准对齐。

### 8.1 TypeScript SDK（sdk/typescript，@agentprimordia/sdk v7.0.0）

- 构建 `npm run build`（tsup，ESM）；类型检查 `npm run typecheck`；API 面检查 `npm run api-check`（api-extractor）；
- 测试 `npm run test`（vitest，111 个测试文件；另有 coverage / affected / bench 变体）；文档站 vitepress；
- `src/` 域与 Go internal 同构映射：agent / a2a / cluster / eval / governance / marketplace / …（二十余域，一域一目录）；
- 版本联动：Go 四方 bump 中的一项就是 TS package.json + lockfile——Go 侧升版时 TS 必须同步。

### 8.2 跨语言一致性双线机制

`scripts/cross-language-api-check.mjs` 是守门人，流程：

1. 运行 Go 侧 api-extract 生成最新 api-contract.json；
2. 读取 cross-language-spec.json 中声明的 Go↔TS 类型/函数等价关系；
3. 校验每个 Go 符号存在于契约、每个 TS 对应实现在源码中存在；发现漂移 **CI 直接失败**。

基准集同理单一权威：评测用例的权威 JSON 在 Go 侧维护，TS 侧**再生成**而非手改——历史上 v5.1 扩容 60→160 条时 TS 未同步导致双线测试失败，此后该教训固化为流程：扩基准必须同步再生成 TS 集。

> ⚠ 给贡献者：改任何 pkg 导出面或基准集结构时，把「跑一次 cross-language-api-check + 再生成 TS 基准」加进你的 checklist，否则 CI 的最后一环会替你补课。

## 9. 依赖白名单（硬约束）

新增第三方依赖需满足『行业标准协议且无法用标准库复现』的豁免条件（见根 AGENTS.md §2.2）。当前白名单及边界：

| 依赖 | 允许范围 |
|------|----------|
| modernc.org/sqlite | memory / llm(cache_sqlite) / persist(sqlite_checkpoint) / tools(builtin database) |
| gopkg.in/yaml.v3 | config / governance / cmd/ap 脚手架 |
| grpc + protobuf + genproto/rpc | 仅 agent/a2a、agent/cluster(grpc_bus)、agent/transport(grpc) |
| etcd client v3 | 仅 persist/ 与 agent/cluster/（etcd build tag） |
| go-redis v9 | 仅 persist/（redis build tag） |
| wazero | 仅 wasm/ 模块 |
| pgx v5 | 仅 pgvector/ 模块间接 require，internal 直接 import 即违规 |

CLI 层连 cobra 都不用——手写 switch 是纪律的一部分，贡献时请保持。

# 第三部分 · 开发攻略


## 10. Recipe A：开发你自己的 Agent 应用

```
ap init my-bot --template with-tools      # 需要文件/shell/web 选 with-tools
#   multi-agent → 多智能体协作     agent-with-rag → 知识库问答
#   agent-with-cache → 结果缓存省钱 agent-with-metrics → Prometheus 指标
#   basic → 最小骨架               quickstart → 新手推荐
```

然后只改两处：main.go 的 system_prompt 与注册的工具集；.ap.yaml 的模型与记忆后端。运行调试走第 4 节循环。

## 11. Recipe B：实现一个自定义内置工具

三步（docs/cookbook/自定义工具.md 有完整图文版）：

1. 在 internal/tools/builtin/ 新建 <name>.go，实现 internal/tools.Tool 接口（名称/描述/schema/Execute）；参照 builtin/text_splitter.go 的最简形态——结构体字段即参数、JSON tag 即入参契约；
2. 若带文件/命令副作用，必须接 Scope 校验（参照 filesystem_safety_test.go、shell_scope_test.go 既有模式）；
3. TDD：先写 _test.go（外部依赖一律 httptest / t.TempDir() / MockLLM），过 go test ./internal/tools/...。

注册走 Registry.Register（批量 RegisterMultiple；动态插件走 RegisterPlugin → marketplace 验签链路）。

## 12. Recipe C：接入一家新的 LLM Provider

框架把流程做成模板复制——internal/llm/provider_template.go（build tag ignore_template 保证永不参与编译）：
```
cp provider_template.go yourname_provider.go
# 全局替换 Template → YourName，依次实现四个方法：Complete() → Stream() → CallTools() → Info()
# 删除警告注释块；模板的 NewTemplateProvider 刻意返回错误防误用，你的才是真逻辑
go test -run TestTemplate ./internal/llm/
```

完成后：在 .ap.yaml provider 枚举处登记、补对照测试；若公开到 pkg 层会触发契约漂移门——按 §16 四方 bump。

## 13. Recipe D：切换/组合记忆后端

```
store, _ := memory.WithInMemory()                     // 测试专用内存 SQLite
store, _ := memory.NewSQLiteStore("./data/mem.db")    // 生产默认（FTS5+向量）
rag := memory.NewRAGStore(store, embedder)            // 叠加 RAG（Hybrid Fusion）
vec := memory.NewVectorStoreWithHNSW(dim, cfg)        // 大规模向量检索
```

.ap.yaml 侧对应 memory.backend= sqlite | memory；更多后端在代码层组合。

## 14. Recipe E：多 Agent 与跨节点协作

- 单机编排：ExecutionEngine.Run(mode OrchestratorMode, steps, edges []DAGEdge)——DAG 用 edges 表达；pipeline / debate / mapreduce 有独立实现文件，handoff / groupchat 属 collaboration 协作模式族（collaboration*.go）；pkg/debate.go、pkg/adapters.go 已导出常用形态；
- 协议互联：A2A（JSON-RPC/SSE/gRPC 三传输），自带互操作自检 ap a2a interop-check；
- 集群：ap cluster init/join/status/leave/scale（gRPC 总线复用 A2A 设施）；
- 长任务自治：ap autonomy run/resume/status（带 HITL 断点）。

## 15. 测试纪律（强制 TDD）

| 约定 | 做法 |
|------|------|
| 红-绿-重构 | 先 _test.go 再实现；提交粒度=单 Task 独立可编译过测 |
| 隔离手段 | MockLLM（agent/pool 层）、DemoLLM（示例）、httptest.Server（网络）、t.TempDir()（磁盘）、WithInMemory()（存储） |
| 并发正确性 | 共享状态必上锁；核心包 -race 必须绿（CI 矩阵含 race） |
| 中文注释 | 注释语种随仓库惯例；错误处理统一走 pkg/errors.go 错误变量 |
| 分布式后端 | etcd/redis 测试单独 make test-distributed-backends，不入默认套件 |

## 16. 质量门与提交规范（CI 会拦什么）

| 门 | 行为 |
|----|------|
| 测试矩阵（ci.yml） | OS×Go 版本矩阵跑 build+race；Windows 无 race 回退；MockLLM 集成测试 |
| 覆盖率分层门 | Phase 7.2 分层门 + Tier3 软门（放行但留痕） |
| 弃用检查 | deprecation-check.sh + ci.yml：Deprecated 注解必须有去向计划 |
| 变更日志 | [Unreleased] 段缺失即拦 |
| 安全扫描 | govulncheck；supply-chain.yml 出 Syft SBOM + cosign 镜像签名 |
| 契约漂移 | api-diff.sh + TestAPIContractNoDrift：pkg 导出面变更未重锁基线即拦 |
| 版本一致性 | version-check.sh：源码 Version 与发布流水不符即拦 |

提交信息：feat:/fix:/refactor: 单 Task 单提交；改 pkg 导出面或 buildGoMod 模板需四方 bump（VERSION、pkg/agent.go、TS package.json、Helm values）并刷新契约基线。

## 17. 发布流程速览

```
git tag vX.Y.0 && push          # tag-release.yml / release.yml 接管
#   version 四方同步 → Makefile ldflags 注入 main.Version → cosign 签名 → SBOM 归档
#   季度: make benchmark 跑 RunQuarterly 自举对比，结果落盘 bench/results/
```

# 第四部分 · 常见问题（FAQ）


## 18. 环境与脚手架

`Q1` go run 绝对路径/cmd/ap 报 go.mod file not found？
go run 只在模块上下文有效。先构建二进制（§2），之后任意目录可用。

`Q2` init 生成的项目构建报 directory prefix does not contain modules listed in go.work？
你在 monorepo 目录树内生成了项目，仓库根 go.work 未包含新模块。绕法：GOWORK=off go build ./... 或 go work use ./myagent。（修复方向已列入 CLI 文档坑位表：init 应自动检测 workspace。）

`Q3` 框架目录外生成的项目 tidy 报 malformed module path: missing dot in first path element？
standalone 场景默认无 replace（语义化导入版本限制 + v0.0.0 占位）。按 init 尾部提示补 replace agentprimordia => <框架源码绝对路径>；用到向量存储还需 replace agentprimordia/pgvector => <源码>/pgvector。详见 版本规范.md。

`Q4` 为什么不能像普通开源库一样 go get？
框架尚未启用主版本后缀发布路线（SIV 限制），决策记录在 版本规范.md。想走 GOPROXY 直装需先按该文档规划模块路径演进。

## 19. 运行与行为

`Q5` 启动立即退出提示 set AP_LLM_API_KEY？
生成的 main.go 显式检查 cfg.APIKey == "" 即 Fatal——刻意设计。设置环境变量优先于 .ap.yaml 落盘。

`Q6` 记忆存在哪？重启会丢吗？
默认 ./data/memory.db（SQLite 含 FTS5 全文与向量列，持久）；backend: memory 则进程内易失。RAG/向量检索叠加其上。

`Q7` Agent 死循环 / token 消耗大？
调低 .ap.yaml 的 agent.max_turns（默认 20）；为 Provider 开缓存（agent-with-cache 模板 / llm cache_enhanced 支持语义命中）；ap loop trace 定位循环点。

`Q8` Shell/Web 工具有危险吗？
shell 默认白名单模式（黑名单仅兜底且注释自标『不推荐』）；http/web/api 三客户端在 Transport.DialContext 建立 TCP 连接时实时校验目标 IP，Loopback / Private / LinkLocal(169.254 段) 单播与组播地址一律拒连并返回 internal/private address 错误；文件操作经 FilesScope 路径越界校验；护栏命中自动 block + 审计留痕。

`Q9` -short 里 TestSoak_Studio 失败了？
已知误报模式：退化门按前后半段吞吐对比判定，本机并发负载抖动即可触发（实测 584 请求 0 错误仍 FAIL）。规避 SOAK_CI_MODE=1 或跳过该包；彻底修复方向见 CLI 文档坑位表②。

## 20. 贡献与发布

`Q10` PR 被 CI 拦：CHANGELOG / Deprecation / coverage？
三条独立门：改动必须落入 CHANGELOG [Unreleased]；Deprecated API 必须给出替代与时间表；覆盖率跌破所在层阈值（Tier3 软门放行但留痕）。

`Q11` 依赖可以随便加吗？
不可以。白名单见 §9；新增需满足行业标准协议豁免并在 PR 说明理由。生态代码只能经 pkg/ 访问核心（实测 ecosystem→internal 零真实 import）。

`Q12` 想跑分布式测试为什么总是空跑？
etcd/redis 后端测试由 build tag 门控且不在常规套件：make test-distributed-backends，需本机起对应服务。

## 附录 A · ap 命令速查

| 命令 | 用途 |
|------|------|
| init / run / debug | 脚手架 / 构建运行热重载 / 调试服务 |
| loop trace·inspect·resume | ReAct 工程化观测与断点续跑 |
| test | 评估套件入口 |
| config validate | .ap.yaml 校验 |
| mcp / plugin | MCP server 与插件管理 |
| cluster init·join·status·leave·scale | 多节点集群生命周期 |
| marketplace search·install·publish·run·list | 模板市场（cosign 验签安装） |
| autonomy run·list·resume·status | 长周期自治目标 |
| skill list·add·remove·verify | 演化技能管理 |
| a2a interop-check | 协议互通自检 |
| realtime（voice） | 实时多模态会话 |
| create-edge-agent | 边缘 Agent 工程 |
| doctor / completion / version | 四项体检 / 补全脚本 / 版本号 |


## 附录 B · 关键路径索引

```
docs/AP-CLI开发手册.md      # CLI 内部实现深潜（坑位表更细）
docs/getting-started/*.md       # installation / quickstart / first-agent 图文教程
docs/cookbook/自定义工具.md custom-provider.md customer-support.md …   # 实战配方
docs/guide/ReAct循环.md orchestration.md security.md deployment.md     # 专题指南
docs/版本规范.md              # API 兼容承诺与 replace 策略依据
docs/供应链安全.md   # SBOM / cosign 供应链安全说明
agentprimordia/internal/AGENTS.md  # 30 个子包职责总表
bench/results/                  # 季度基准与回归报告存证
```
---
*维护者注：本文档基于 2026-08-27 代码实测编写；所有结论可用 grep/编译器复验。发现漂移请在 PR 同步更新对应章节。*