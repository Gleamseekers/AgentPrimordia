# ap CLI 开发文档

> 适用对象：使用与维护 `ap` 的所有开发者——先用第 2 节上手，再按需深入。
> 所有行为描述均经实际代码审读与端到端验证（2026-08-27，go1.26.6）。

## 1. 概述

`ap` 是 AgentPrimordia 框架的官方 CLI，覆盖项目脚手架、运行调试、工具管理到集群运维的完整生命周期。实现上遵守仓库『标准库优先』纪律：

- 零第三方 CLI 框架（无 cobra/urfave-cli），命令分发为 `main.go` 内的手写 switch；
- 一级命令约 18 个（init/run/debug/loop/test/config/mcp/plugin/cluster/marketplace/autonomy/skill/a2a/realtime/create-edge-agent/doctor/completion/version）；
- 实现约 6.1k 行 / 测试 2.2k 行，共 120 个测试函数，-short 全绿。

## 2. 快速上手（新手指南）

> 目标：从零到跑起第一个 Agent 约 5 分钟。前置条件：Go ≥ 1.26、本机可访问框架源码目录。

```bash
# ① 安装 ap（在框架源码目录内构建）
cd agentprimordia
go build -o "$(go env GOPATH)/bin/ap" ./cmd/ap

# ② 生成项目（拿不准产物可加 --dry-run 预览）
`ap init myagent --template quickstart`    # 另有 basic/with-tools/multi-agent 等 7 模板 × agent/plugin/provider 三类型
cd myagent

# ③ 配置 LLM：环境变量优先于 .ap.yaml 的 llm 段
export AP_LLM_API_KEY=sk-xxx
export AP_LLM_MODEL=gpt-4o        # OpenAI/Anthropic/Gemini/GLM/Ollama 等 Provider 均内置

# ④ 运行：自动 go build + 注入配置 + 启动；改源码自动重建重启（热重载）
`ap run`
```

生成的工程只有四个文件：`main.go`（ConfigFromEnv → Provider → ReAct 最小闭环）、`.ap.yaml`（项目配置）、`go.mod`、`.gitignore`。先通读 `main.go` 全文——它就是框架公共 API 的最小用法样例，改 prompt 即得到你自己的第一个 Agent。

第一次没跑起来？按顺序排查：

1. `ap doctor` —— 四项体检（Go 版本 / 项目配置 / API Key / 依赖）；
2. 本文第 9 节坑位表 —— 重点两条：在 monorepo 目录树内生成后构建报 *directory prefix does not contain modules listed in go.work* 时，加 GOWORK=off 前缀或执行 go work use ./myagent；tidy 报 malformed module path 时，在 go.mod 补一行 `replace agentprimordia => <框架源码目录>`。

进阶路线：插件开发用 `ap init x --type plugin`，自定义 LLM 用 --type provider，外部工具生态接 `ap mcp`，多实例部署看 `ap cluster`，协议互通自检用 `ap a2a interop-check`。

更系统的入门教程：docs/getting-started/（installation / quickstart / first-agent）、docs/guide/ReAct循环.md；实战配方见 docs/cookbook/。

## 3. 代码结构

位于 `agentprimordia/cmd/ap/`，包名 main，约定一个命令一个文件、入口函数 `runXxx(args []string) error`：

| 文件 | 入口 | 子处理器示例 |
|------|------|--------------|
| main.go | main() + 命令 switch（98 行） | usage 常量文本 |
| init.go | runInit | 模板选择、目录写出 |
| scaffold.go | generate 系列 | buildGoMod / findFrameworkRoot（go.mod 生成核心） |
| run.go | runRun | appendConfigEnv / watchAndRun（热重载） |
| loop.go | runLoop | runLoopTrace / runLoopInspect / runLoopResume |
| cluster.go | runCluster | Init / Join / Status / Leave / Scale |
| marketplace.go | runMarketplace | Search / Install / Publish / Run / List |
| skill.go | runSkill | List / Add / Remove / Verify |
| plugin.go 与 mcp.go | runPlugin / runMCP | 插件与 MCP server 管理 |
| a2a.go | runA2A | runA2AInteropCheck 协议互通自检 |
| autonomy.go | runAutonomy | Run / List / Resume / Status |
| realtime.go | runRealtime | runRealtimeVoice |
| config.go / debug.go / doctor.go | 各自 runXxx | 配置校验 / 调试服务 / 四项体检 |
| completion.go | runCompletion | shell 补全脚本生成 |

新增命令时需同步更新 main.go 顶部的 usage 常量文本块。

## 4. 构建与版本注入

- 本地运行：go build ./cmd/ap；var Version 缺省值 dev。
- 发布版本由根 Makefile 注入：LDFLAGS=-ldflags="-X main.Version=$(VERSION)"，与 scripts/version-check.sh 联动校验。
- 注意：go run 绝对路径/cmd/ap 在模块外会报 go.mod file not found——跨目录使用请先 go build -o /tmp/ap-bin ./cmd/ap 再执行二进制。

## 5. 新增子命令 SOP

1. 新建 cmd/ap/<cmd>.go，实现 func run<Cmd>(args []string) error；
2. 参数解析沿用手写 flag 循环惯例，不引入新第三方依赖；
3. 在 main.go switch 中注册 case；
4. 更新 usage 常量与 ap <cmd> --help 输出；
5. 如涉脚手架产物，补对应 <cmd>_test.go（参考 scaffold_test.go 边界用例风格）；
6. 运行 go test ./cmd/ap/... -short 与 go vet ./...；
7. 必要时同步 completion.go 补全词条。提交粒度遵循仓库规范：feat: ap xxx，单命令单提交。

## 6. 脚手架系统（init/generate）

### 6.1 能力矩阵

ap init <name> 支持 7 个模板 × 3 种类型；模板：quickstart / basic（默认）/ with-tools / multi-agent / agent-with-cache / agent-with-rag / agent-with-metrics；类型：agent（默认）/ plugin / provider；全局旗标：--template --type --dry-run --interactive。

### 6.2 go.mod 生成双分支（scaffold.go buildGoMod）

1. 框架内探测：findFrameworkRoot 从项目父目录向上最多 6 层寻找声明 module github.com/Gleamseekers/AgentPrimordia 的 go.mod；命中则 emit 相对路径 replace，并检测兄弟目录 pgvector/go.mod 连带补 require + replace github.com/Gleamseekers/AgentPrimordia/pgvector（规避 pgvector replace 不具传递性导致的独立子项目断链，commit 891f683d）。
2. standalone：未找到时仅写 require agentprimordia v0.0.0 占位并打印提示——受 Go 语义化导入版本限制，框架必须本地 replace 后才可 tidy（版本规范.md 有模块消费限制说明）。

### 6.3 ap run 行为链

go build -o <binaryName> . → appendConfigEnv 读取 .ap.yaml 的 llm 段注入环境变量（已存在环境变量 > .ap.yaml）→ 启动二进制 → watchAndRun 监听源码变更自动重建重启。

## 7. 测试约定

- 关键断言点：gomod_template_test.go（Standalone / InRepoWithPgvector / GoModInRepo / GoModVersion 四场景）、scaffold_test.go（Basic / ProjectName / UnknownTemplate / EmptyName）、plugin_test.go / interactive_test.go / mcp_test.go 等；
- 测试一律 t.TempDir() 隔离，不写工作区真实路径；
- 运行：go test ./cmd/ap/... -short -count=1。

## 8. 端到端验证 SOP（发布前必跑）

```bash
cd agentprimordia && go build -o /tmp/ap-bin ./cmd/ap || exit 9
TMP=$(mktemp -d); cd "$TMP"
/tmp/ap-bin init demo --template quickstart
cd demo && go mod tidy && GOWORK=off go build ./...   # 树内生成须脱离 workspace 判定，见第 9 节①
cd .. && rm -rf "$TMP" /tmp/ap-bin                    # 清理现场
```

实测基线（2026-08-27）：框架内生成 → 双 replace 正确 → tidy PASS；GOWORK=off 下 build PASS。

## 9. 已知问题与坑位

| # | 现象 | 根因 | 当前绕法 / 修复方向 |
|---|------|------|---------------------|
| ① | monorepo 树内 init 生成的项目 go build 报 directory prefix does not contain modules listed in go.work | 仓库根 go.work 只列 5 个已有模块，新生成模块不在 use 列表 | 绕法：GOWORK=off 或 go work use ./demo。修复方向：init 检测 workspace 上下文后自动追加 use 并提示 |
| ② | 框架目录外生成的项目 go mod tidy 失败：malformed module path | standalone 分支无 replace 可写（语义化导入版本限制，v0.0.0 占位） | 按 init 提示手动添加 replace agentprimordia => <框架源码目录>。修复方向：提示提升到 Next steps 首行 |
| ③ | 开发约束：修改 buildGoMod 模板或 bump 版本会被现有门拦下 | gomod_template_test.go 四场景、TestAPIContractNoDrift、version-check.sh 联动守卫 | 改模板先补四场景断言；改 pkg 导出面需四方 bump（VERSION、pkg/agent.go、TS package.json、Helm values） |

## 10. 相关文档

- docs/版本规范.md —— 模块消费限制与语义化导入版本约束（脚手架 replace 策略依据）
- docs/getting-started/ —— installation / quickstart / first-agent（图文入门三连）
- docs/供应链安全.md —— cosign/SBOM 供应链安全
- scripts/api-diff.sh 与契约漂移门 —— 公共 API 变更检查
- 架构图：docs/ap-architecture.svg