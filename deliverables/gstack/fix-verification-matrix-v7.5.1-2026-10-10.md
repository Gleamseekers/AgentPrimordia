# AgentPrimordia v7.5.1 — 问题复验与修复验证矩阵

**日期**：2026-10-10
**范围**：对《上线前检查报告》中 17 条问题逐项复验（含证伪）+ 实施修复 + 回归验证
**前置报告**：`deliverables/gstack/pre-launch-check-v7.5.1-2026-10-10.md`
**改动规模**：24 个文件，+507 / −261 行

---

## 📌 TL;DR

- **复验结论**：原始清单 **17 条**问题中，**16 条确认为真**，**1 条（O2）被证伪**（glob 语义实际正确，原担心不成立）
- **修复状态**（按原始 17 条计）：**14 条完全修复**、**2 条部分修复**（M6 镜像 digest 缺 registry 访问、O3 未合并 Dockerfile）、**1 条证伪无需修复**（O2）
- **派生项 B3**（由 O5 与 §4 异常场景表派生，非原始编号）：✅ 已完全修复
- **复验新发现 5 条**：M11（全局 `GOSUMDB=off`）✅、M12（`eval.yml` 未定义输入）✅、M13（`Dockerfile.prod` 全文件 CRLF）✅、**M14**（两处硬编码的过期工具链 `go1.26.4`）✅、**M15**（3 份历史文档含旧工具链描述）⚠️ 部分修复
- **⚠️ 因此仅剩 2 项未完全闭环**：**M6**（镜像 digest，环境受限）、**O3**（Dockerfile 未合并，有意不改）
- **漏洞闭环**：`govulncheck` 由修复前的 **11 条可达漏洞** → 修复后 **`No vulnerabilities found.`**
- **工作流静态检查**：`actionlint` 由 1 条告警 → **零输出、退出码 0**
- **回归验证**：`go build` / `gofmt` / `go mod verify` / 核心包测试 / 沙箱安全断言 / 版本一致性门 / YAML 语法 ×7 / **流水线逻辑复刻（1 组正向 + 3 组负向）全部通过**

---

## 1. 复验方法

| 手段 | 适用问题 | 说明 |
|------|---------|------|
| **本地逻辑复刻** | B1、B3、O5 | 用真实 shell 复刻新旧两阶段产物处理逻辑，含正向与负向用例 |
| **命令实测** | B3、M2、M8、M9、M11、M13 | `find`/`xargs` 退出码、`git describe` 回退、`govulncheck`、`go env`、行尾统计 |
| **静态读取 + 交叉比对** | B2、M1、M4、M5、M6、M7、M10、M12、O1、O3、O4 | 读源码/配置，并与 `v7.5.0` 版本比对区分存量与新增 |
| **第三方工具** | M5、M7、M12 | `actionlint`（工作流静态分析）、`glob`（通配语义）、`yaml`（语法） |
| **依赖审计** | M8、M9 | `govulncheck`（Go 官方漏洞扫描） |
| **影响面核验** | M3、M13 | 枚举规则命中文件，确认无 `go:embed` 目标被误伤 |

---

## 2. 逐项复验与修复矩阵

### 🔴 原判阻断项

| # | 原判 | 复验结论 | 验证依据（可复跑） | 修复状态 | 修复内容 | 残留风险 |
|---|------|---------|------------------|---------|---------|---------|
| **B1** | 🔴 | ✅ **确认存在** | 复刻旧逻辑：`find . -name 'ap-*' \| xargs sha256sum` 产出 `a23de36... ./ap-linux-amd64/ap-linux-amd64`；release 阶段扁平化为 `ap-linux-amd64`；`sha256sum -c` → `No such file or directory` | ✅ **已修复** | `sign-binaries` 改为**先扁平化到 `flat/` → 再 `sha256sum ap-*`**，清单条目为裸文件名；并加 `sha256sum -c` 自校验回归门。复刻正向测试：6/6 `OK` | 无（逻辑层已闭环；仍需真实 tag 触发一次 CI 确认） |
| **B2** | 🔴 | ✅ **确认存在**（条件触发） | `bench/llm-bench/main.go:166-167`：`pass_rate < gate` → `os.Exit(1)`；`release.yml` 原 `needs: [sign-binaries, llm-benchmark]`；`docker` 仅 `needs: [build-binaries]` → 镜像先推、Release 被跳过 | ✅ **已修复** | ① `llm-benchmark` 加 job 级 `continue-on-error: true`；② 从 `release.needs` 移除；③ 门禁结果写入 `$GITHUB_OUTPUT` 与 job summary；④ `docker.needs` 改为 `[build-binaries, sign-binaries]` | ⚠️ 门禁语义由**阻断**降为 **advisory**。若需硬门禁，应在 `ci.yml`（打 tag 前）实现 —— 已在报告中建议，未实施 |
| **B3**<br>（**派生项**，见下方说明） | 🟠 | ✅ **确认存在** | 实测：`find <空目录> -type f -exec cp …` **退出码 0**（不报错）；`printf "" \| xargs sha256sum` **退出码 0 且产出 0 字节文件**（该空清单会被 cosign 正常签名并发布） | ✅ **已修复** | release 作业新增 4 重断言：同名覆盖检测（`SRC != DST`）、6 个平台二进制计数、`checksums-sha256.txt`/`.sig`/`.pem` 非空（`test -s`）、SBOM 存在；并执行用户侧同款 `sha256sum -c`。负向测试 3/3 全部阻断 | 无 |

> **关于 B3 的编号说明（更正）**：前置报告的原始清单是 **17 条**（🔴 B1/B2 + 🟠 M1–M10 + 🟡 O1–O5），**其中并无 "B3" 编号**。B3 是本次复验时从以下两处**派生**出的独立项，为便于追踪单独编号：
> - 原 **O5**（`xargs` 缺 `-r` + 空校验和文件静默通过）
> - 原报告 **§4 异常场景表**第 168–170 行（`find` 无匹配 / `xargs` 空输入 / 同名产物冲突 → 均标注"缺断言"）
>
> 因此"原判 17 条"与"本次逐条核验 18 项"的差额即为 B3。B3 的修复内容与原 O5 的修复同属一次改动。

### 🟠 原判建议修复项

| # | 原判 | 复验结论 | 验证依据 | 修复状态 | 修复内容 | 残留风险 |
|---|------|---------|---------|---------|---------|---------|
| **M1** | 🟠 | ✅ **确认存在** | 原代码 `if … elif … else echo "::warning::"` —— 签名失败不阻断 | ✅ **已修复** | 改为**硬失败**：`digest` 为空即 `exit 1`；`cosign sign` 失败即 job 失败。同时移除 `COSIGN_EXPERIMENTAL` | ⚠️ cosign 瞬时故障将导致发布失败（属供应链门的预期行为，可一行回退） |
| **M2** | 🟠 | ✅ **确认存在** | 实测无 `.git` 目录下 `git describe --tags --always --dirty \|\| echo dev` → 输出 `dev`；`.dockerignore:5` 排除 `.git` | ✅ **已修复** | `Dockerfile.prod` 新增 `ARG VERSION=dev`，ldflags 改 `${VERSION}`；`release.yml` docker 作业加 `build-args: VERSION=${{ github.ref_name }}` | ⚠️ 需 CI 实测镜像内 `ap-admin` 版本号 |
| **M3** | 🟠 | ✅ **确认存在**（范围比原判更大） | 逐模式核验：`.env` / `.env.local` / `*.pem` / `*.key` / `id_rsa` / `*.p12` **均未排除**。复验中另发现**第二处** `agentprimordia/.env.local`，同样含真实 `AP_LLM_API_KEY`/`OPENAI_API_KEY` | ✅ **已修复** | 两个 `.dockerignore`（仓库根 + `agentprimordia/`）均新增 13 类敏感模式。影响面核验：仅命中 2 个 `.env.local`，`go:embed` 目标（`scaffold/`、`benchmark_cases.json`、`static/editor.html`）**无误伤** | 无 |
| **M4** | 🟠 | ✅ **确认存在** | `release`、`build-binaries`、`generate-sbom`、`llm-benchmark`、`npm-publish` 均未声明 `permissions`，继承工作流级 `contents:write + packages:write` | ✅ **已修复** | 删除工作流级 `permissions`，改为 7 个 job 各自精确声明（构建类 `contents: read`，`release` 为 `contents: write`，`docker` 为 `contents: read + packages: write + id-token: write`） | 无 |
| **M5** | 🟠 | ✅ **确认存在** | `grep` 列出全部未 pin 引用；`aquasecurity/trivy-action@master` 直接跟踪浮动分支 | ✅ **已修复** | **109 处 action 引用全部 pin 到 commit SHA**（附 `# vX` 可读注释）；`trivy-action` 由 `@master` 改 pin 正式版 **v0.36.0**；`dtolnay/rust-toolchain` 因 pin 后 `github.action_ref` 失效，**补显式 `toolchain: stable`** | ⚠️ pin 需定期更新，建议接入 Dependabot/Renovate |
| **M6** | 🟠 | ✅ **确认存在** | 4 处 `FROM` 均未 pin digest | ⚠️ **部分修复** | `golang:1.26` → **`golang:1.26.9-alpine`**（精确补丁版，同时落实 M9）；两个 Dockerfile 内已留 TODO 注释 | ⚠️ **digest pin 未完成**：本机无 `docker`/`crane`/`skopeo`，且 `registry-1.docker.io`、`hub.docker.com`、USTC/DockerProxy 镜像源**实测均不可达（HTTP 000）**，无法取得可信 digest。需在有 registry 访问的环境执行 `docker buildx imagetools inspect <image>` 补齐 |
| **M7** | 🟠 | ✅ **确认存在**（含额外缺陷） | Node 步骤只有 `npm ci`，**无任何 syft 调用**，注释与实现不符；另发现 Go 扫描用 `image: .`，而 `image` 输入期望容器镜像引用，传 `.` 无法正确扫描目录 | ✅ **已修复** | ① Go 扫描改 `path: .`；② 新增**真正的** Node 扫描（`path: sdk/typescript` → `sbom-node.spdx.json`）；③ SBOM 上传加 `if-no-files-found: error` | ⚠️ 需 CI 实测 syft 产出 |
| **M8** | 🟠 | ✅ **确认存在** | `govulncheck` 报 `golang.org/x/net@v0.58.0` 含 5 条公告（GO-2026-6617/6612/6611/6610/6603），修复版本 `v0.60.0` | ✅ **已修复** | 升至 **`golang.org/x/net v0.61.0`**（连带 `x/sync v0.24.0`、`x/sys v0.49.0`、`x/text v0.43.0`）；`go.sum` 补 11 条哈希 | 无（`govulncheck` 复跑为 0） |
| **M9** | 🟠 | ✅ **确认存在** | `govulncheck` 报 **11 条可达标准库漏洞**（`net/http` Range 头 DoS、`html/template` ×2、`net/textproto`、`crypto/tls`、`os` 等），全部修复于 `go1.26.9` | ✅ **已修复** | ① `go.mod` / `go.work` + 4 个子模块（pgvector/operator/gateway/wasm）go 指令 → **1.26.9**；② 全部 workflow `go-version` → **1.26.9**（含 `eval.yml` 原为 `1.26.4` 的遗漏点）；③ 两个 Dockerfile → `golang:1.26.9-alpine`；④ `build-binaries` 新增**工具链下限断言**（低于 1.26.9 即拒绝构建） | 无（`govulncheck` 复跑为 0） |
| **M10** | 🟠 | ✅ **确认存在** | `docs/发布说明-v7.5.1.md` 标题仍为 "v7.5.0"、版本表写 7.5.0、`api-contract.json 7.5.0 再生成`；git 显示为 `R099` 纯改名（仅改日期），且通篇未提发布链路修复 | ✅ **已修复** | 重写为 v7.5.1 内容：新增「发布链路修复明细」7 项根因表、「校验发布产物」章节、正确的版本统一表与升级说明；v7.5.0 功能内容压缩为附录 | 无 |

### 🟡 原判可选优化项

| # | 原判 | 复验结论 | 验证依据 | 修复状态 | 修复内容 | 残留风险 |
|---|------|---------|---------|---------|---------|---------|
| **O1** | 🟡 | ✅ **确认存在** | cosign v2+ 中 keyless 为默认行为，`COSIGN_EXPERIMENTAL` 已废弃；`cosign-installer@v3` 安装 v2.x | ✅ **已修复** | 删除该环境变量 | 无 |
| **O2** | 🟡 | ❌ **证伪（原判断有误）** | 用 **glob v13.0.6** 实测：`glob.sync('release-files/**/*')` → `["release-files/sub","release-files/b.bin","release-files/a.txt","release-files/sub/c.txt"]` —— **顶层文件确实被匹配**，原"部分实现下不匹配"的担心不成立 | ✅ **无需修复** | 无（属非缺陷）。重写时顺手改为更显式的 `release-files/*`，仅为可读性，非缺陷修复 | 无 |
| **O3** | 🟡 | ✅ **部分确认** | 两个 Dockerfile 版本注入方式确实不一致（`.prod` 用 `git describe` → 实际得 `dev`；`.sbomlabel` 用 `ARG VERSION`） | ⚠️ **部分修复** | M2 修复后两者**均为 build-arg 注入，行为已统一**；**未合并为单一 Dockerfile**（属结构性重构，风险高于收益，不建议在发布修正版中夹带） | 🟡 仍存在两个 Dockerfile（可接受，已消除行为漂移） |
| **O4** | 🟡 | ✅ **确认存在** | `docs/项目深度评估报告-2026-09-28.md` 含 `7.5.0` 字样（历史记录） | ✅ **已修复** | 文首新增「⚠️ 历史快照声明」，明确文中版本号/命令不随发布更新，当前版本以 `VERSION` 与发布说明为准 | 无 |
| **O5** | 🟡 | ✅ **确认存在** | 实测 `printf "" \| xargs sha256sum` → 退出码 0、产出 **0 字节**文件；原步骤无 `set -euo pipefail` | ✅ **已修复** | 弃用 `xargs`（改用 `sha256sum ap-*`）+ `set -euo pipefail` + `test -s` 非空断言，与 B3 一并闭环 | 无 |

---

## 3. 复验中新发现的问题

| # | 严重度 | 问题 | 验证依据 | 修复状态 | 说明 / 残留风险 |
|---|--------|------|---------|---------|----------------|
| **M11** | 🟠 | **全局 `GOSUMDB=off` 禁用了 Go 模块校验和验证** | `go env GOSUMDB` → `off`；配置位于 `/Users/new/Library/Application Support/go/env`（非 shell 环境变量）。该设置会让所有模块下载**跳过校验和验证**，属供应链风险；同时它阻断了 `go.mod` 工具链下限升级（`verifying module: checksum database disabled by GOSUMDB=off`） | ✅ **已修复** | 经你确认后执行 `go env -w GOSUMDB=sum.golang.org`。`sum.golang.org` 经 `goproxy.cn` 可达（实测 HTTP 200）。**回退命令**：`go env -u GOSUMDB` |
| **M12** | 🟡 | `eval.yml:17` 引用未定义的 `inputs.threshold` | `actionlint` 报 `property "threshold" is not defined in object type {}`；经 `git show v7.5.0:.github/workflows/eval.yml` 比对，**确认是存量问题，非本次引入** | ✅ **已修复** | ① 新增 `workflow_dispatch.inputs.threshold`（`type: string`、`default: '0.8'`，仅手动触发，**不影响既有 push/PR 行为**）；② 阈值经 **env 变量**传入脚本（避免把触发输入插值进 shell 命令行），并在步骤内加数值预检；③ 实测预检逻辑：`0.8`/`0.95` 通过，`abc`/`0.8;rm -rf /`/空值 拒绝。`actionlint` 现**零输出、退出码 0** | 无 |
| **M14**<br>（新发现） | 🟠 | **两处硬编码的过期工具链默认值**（正是 M9 所修漏洞版本） | 全仓检索 `GOTOOLCHAIN` / `go1.26.[0-8]` 发现：`bench/eval-ci/run_eval.sh:76` 为 `GOTOOLCHAIN="${GOTOOLCHAIN:-go1.26.4}"`；`bench/eval-ci/main.go:39` 为 `flag.String("toolchain", "go1.26.4", …)`。即 eval 链路会强制以 **go1.26.4**（含 M9 所列可达标准库漏洞）运行测试，与仓库其余部分的 1.26.9 基线矛盾 | ✅ **已修复** | 两处默认值均改为 **`go1.26.9`**（保留可被 `GOTOOLCHAIN` 环境变量覆盖的行为）；`gofmt` 后 `go build` / `go vet` 均通过 | 无 |
| **M15**<br>（新发现） | 🟡 | 3 份**历史评估文档**含旧工具链描述（`docs/技术全量评估.md:34`、`docs/项目评估与路线图.md:28`、`docs/双SDK实施计划.md:39`） | 三份文档分别为 2026-07-18 / 07-26 / 07-31 的时点快照（描述 v2.0.0 / v3.1.0 状态），其中 `项目评估与路线图.md` 自称"当前的全景状态报告" | ⚠️ **部分修复** | 仅对**自称"当前"**的 `docs/项目评估与路线图.md` 加「⚠️ 历史快照声明」，明确工具链下限以 `go.mod` 为准（现为 1.26.9）。**另两份未改动** —— 它们已在文首标注评估日期与基线版本，修改内容会篡改历史记录 | 🟡 另两份文档的旧版本描述保留（属历史记录，非缺陷） |
| **M13** | 🟠 | `agentprimordia/Dockerfile.prod` 与 `agentprimordia/.dockerignore` **全文件为 CRLF 行尾** | 统计：`Dockerfile.prod` **56/56 行**、`.dockerignore` **28/28 行**为 CRLF；同目录 `Dockerfile.sbomlabel` 为 0 行 CRLF。CRLF 在 Dockerfile 中可能导致行续接 `\` 前的 `\r` 破坏指令解析 | ✅ **已修复** | 两个文件规范化为 LF，现为 0 行 CRLF，与同目录同类文件一致 |

---

## 4. 修复后整体回归验证

| # | 验证项 | 命令 / 手段 | 结果 |
|---|--------|------------|------|
| 1 | 全工作区构建 | `go build ./...`（root + pgvector + gateway + wasm） | ✅ 全部 exit 0 |
| 2 | 代码格式 | `gofmt -l agentprimordia/pkg/agent.go` | ✅ 无输出（干净） |
| 3 | 依赖完整性 | `go mod verify` | ✅ `all modules verified` |
| 4 | 公共 API 测试 | `go test ./agentprimordia/pkg/...` | ✅ `ok`（0.551s / 0.380s） |
| 5 | **§2.3 沙箱安全断言** | `go test ./agentprimordia/wasm/... ./agentprimordia/internal/security/...` | ✅ `ok`（0.932s / 20.291s） |
| 6 | 版本一致性门 | `node scripts/version-sync-check.mjs` | ✅ exit 0（Go/TS/Helm/api-contract 四方 = 7.5.1） |
| 7 | **依赖漏洞扫描** | `govulncheck ./...` | ✅ **`No vulnerabilities found.`**（修复前 11 条可达） |
| 8 | 工作流静态分析 | `actionlint -no-color` | ✅ **零输出、退出码 0**（修复前有 1 条告警） |
| 9 | 工作流 YAML 语法 | `yaml.parse` × 7 个文件 | ✅ 全部 OK |
| 10 | eval 阈值接口 | `bash run_eval.sh --threshold {abc,0.8}` / `--help` | ✅ 非法阈值退出码 2、`--help` 退出码 0、合法阈值正常启动 |
| 11 | 阈值预检与注入防护 | 复刻 workflow 内 `case` 守卫 | ✅ `0.8`/`0.95` 通过；`abc`/`0.8;rm -rf /`/空值 拒绝 |
| 12 | **流水线逻辑复刻（正向）** | 复刻新版两阶段处理，模拟 12 个产物文件 | ✅ 6/6 二进制 `sha256sum -c` → `OK`；release 阶段断言全通过 |
| 13 | **流水线逻辑复刻（负向 1）** | artifacts 为空 | ✅ 已按预期阻断（旧实现会静默产出空 Release） |
| 14 | **流水线逻辑复刻（负向 2）** | 仅 4 个平台二进制 | ✅ 已按预期阻断 |
| 15 | **流水线逻辑复刻（负向 3）** | 校验和清单为空 | ✅ 已按预期阻断（空清单不再进入 cosign 签名） |
| 16 | action pin 完整性 | `grep -rn "uses:" \| grep -v "@[0-9a-f]\{40\}"` | ✅ 无输出（109 处全部已 pin） |
| 17 | 工具链版本统一 | `grep -rn "1\.26\.[0-8]"`（含脚本/Go 源码） | ✅ 无残留（run_eval.sh / main.go 默认值已改 1.26.9） |
| 18 | `.dockerignore` 影响面 | 枚举命中文件 | ✅ 仅命中 2 个 `.env.local`；`go:embed` 目标无误伤 |
| 19 | 行尾规范 | CRLF 行数统计 | ✅ 全部为 0 |

---

## 5. 残留风险汇总

| # | 残留项 | 严重度 | 原因 | 建议动作 |
|---|--------|--------|------|---------|
| R1 | **镜像基础镜像未 pin digest**（M6） | 🟠 | 本机无 Docker 工具且所有 registry 端点不可达（实测 HTTP 000） | 在可访问 registry 的环境执行：`docker buildx imagetools inspect golang:1.26.9-alpine` 与 `alpine:3.20`，将 digest 写入两个 Dockerfile（已留 TODO 注释） |
| R2 | **所有 workflow 改动未经真实 tag 触发验证** | 🟠 | 本地无法运行 GitHub Actions | 推送 `v7.5.2` tag 后，按前置报告 §6.4 冒烟清单逐项验证（尤其第 1/2/4/5 项） |
| R3 | M7 / M2 的 CI 行为未实测 | 🟡 | 同上 | 同上；重点确认 syft 产出 `sbom-node.spdx.json`、镜像内 `ap-admin --version` 显示版本号而非 `dev` |
| R4 | B2 门禁降为 advisory，硬门禁未落 `ci.yml` | 🟡 | 本次仅修复"阻断发布"这一缺陷，未新增 CI 门 | 若需要质量硬门禁，在 `ci.yml` 增加跑分 job 并作为 tag 前置条件 |
| R5 | M15：另 2 份历史文档（`docs/技术全量评估.md`、`docs/双SDK实施计划.md`）仍含旧工具链描述 | 🟡 | 属历史评估快照，改动会篡改记录 | 无需动作；如需，可加与 `项目评估与路线图.md` 同款的历史快照声明 |
| R6 | **全局 Go 配置已变更**（M11） | 🟢 | 经你确认后修改 | 无需动作；如需回退：`go env -u GOSUMDB` |
| R7 | M5 的 SHA pin 会随时间过期 | 🟢 | pin 是静态快照 | 接入 Dependabot / Renovate 自动更新 |
| R8 | eval 链路的 `GOTOOLCHAIN` 仍可被环境变量覆盖 | 🟢 | 保留 `"${GOTOOLCHAIN:-go1.26.9}"` 的可覆盖行为属有意设计 | 无需动作 |

---

## 6. 变更文件清单（24 个，+507 / −261）

**发布链路（7 个）**
- `.github/workflows/release.yml` — 重写（B1/B2/B3/M1/M2/M4/M7/O1）
- `.github/workflows/eval.yml` — 重写（M12 补齐 `workflow_dispatch.inputs.threshold` 并接线、缓存键 1.26.9、阈值经 env 传入 + 预检）
- `.github/workflows/ci.yml`、`nightly.yml`、`supply-chain.yml`、`tag-release.yml`、`vscode-release.yml` — action pin + `go-version: 1.26.9`（M5/M9）

**Eval 链路（2 个）**
- `agentprimordia/bench/eval-ci/run_eval.sh` — `GOTOOLCHAIN` 默认值 `go1.26.4` → `go1.26.9`（M14）
- `agentprimordia/bench/eval-ci/main.go` — `--toolchain` 默认值 `go1.26.4` → `go1.26.9`（M14）

**容器（4 个）**
- `agentprimordia/Dockerfile.prod` — `ARG VERSION` 注入 + 精确补丁版基础镜像 + LF 规范化（M2/M6/M9/M13）
- `agentprimordia/Dockerfile.sbomlabel` — 精确补丁版基础镜像（M6/M9）
- `.dockerignore`、`agentprimordia/.dockerignore` — 敏感文件排除（M3/M13）

**依赖（6 个）**
- `go.mod`、`go.sum`、`go.work`、`go.work.sum` — 工具链 1.26.9 + x/net v0.61.0（M8/M9）
- `pgvector/go.mod`、`agentprimordia/operator/go.mod`、`gateway/go.mod`、`wasm/go.mod` — 工具链 1.26.9（M9）

**文档（3 个）**
- `docs/发布说明-v7.5.1.md` — 重写（M10）
- `docs/项目深度评估报告-2026-09-28.md` — 历史快照声明（O4）
- `docs/项目评估与路线图.md` — 历史快照声明（M15）

---

## 7. 无法验证 / 未执行项（诚实披露）

| 项目 | 原因 | 影响 |
|------|------|------|
| GitHub Actions 端到端执行 | 本地无 runner，无法触发 workflow | 所有 workflow 改动均为**静态验证**（YAML + actionlint + 逻辑复刻），需真实 tag 触发确认 |
| Docker 镜像构建 | 本机无 `docker` CLI，registry 不可达 | M2/M6/M7 的容器侧改动未实测 |
| 镜像 digest pin | 所有 registry 端点不可达（实测 HTTP 000） | M6 仅部分修复（见 R1） |
| TS SDK 测试 | 未执行（本次未改动 SDK 源码，仅版本号已在上一版变更） | 低风险 |
| 全仓 `go test ./...` | 规模大，未全量执行 | 已跑与改动强相关的包；建议以 CI 全绿为准 |

---

> 本报告由软件工坊 AI 协作生成。所有"已修复"结论均附可复跑命令或复刻脚本（`/tmp/pipe_sim.sh`），关键决策请由工程负责人复核。
