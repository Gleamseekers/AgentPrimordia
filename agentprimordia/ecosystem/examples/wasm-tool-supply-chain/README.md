# wasm-tool-supply-chain — WASM 工具安全供应链 Demo

> ## ⚠️ 诚实边界（先读这段，再读任何输出）
>
> **工具生成器没有生产实现。** `internal/tools/lifecycle` 的全部 `Generator`
> 实现均为测试替身，`cmd/ap/tool_forge.go` 的注释亦自认「生成质量（WASM 化）
> 属研究面」。因此：
>
> - 本 demo 的 WASM 工件由**开发者**（即本 demo 的代码）**手工字节组装**提供；
> - 本 demo 演示的是**「开发者自带 WASM 工件的安全供应链」**——
>   **不是**「agent 自己写工具」的演示；
> - 任何把本 demo 读成「agent 可自主生成并执行代码」的理解都是错的。
>
> 另两条边界：① agent「决定调用哪个工具」由零凭据**脚本化 provider** 给出
> （不发网络请求、不需要 API key），工具执行走框架生产路径；② ed25519 密钥对
> 每次运行随机生成，故公钥指纹/签名十六进制每次不同，其余输出字节级确定。

## 这是什么

一条可运行的证据链：**开发者手工组装的 WASM 字节 → ed25519 签名 → 公钥钉扎 →
验签门注册 → agent 在 wazero 沙箱中真实调用**。四类拒绝（未签名 / 公钥未钉扎 /
非 WASM / 工件被篡改）各有一次真实实证，成功分支跑到底并展示沙箱执行结果。

```
开发者（本 demo 代码）                框架生产路径
──────────────────────              ─────────────────────────────────────────
buildWASMToolModule()  ──274 字节──▶  ap.WASMSignWASM / ap.WASMVerifySignature
ap.WASMGenerateKeyPair()             （ed25519 over SHA-256(工件)）
                                    ▶ 钉扎验签门（空集合 = 拒绝一切）
                                    ▶ WASMToolAdapter.RegisterTool（A6 签名前置）
                                    ▶ AsTool 桥 → tools.Registry（ReActAgent toolkit）
                                    ▶ Sandbox.ExecuteWithMemory（wazero 沙箱执行）
```

## 运行

一条命令（任意工作目录，约 1 秒）：

```bash
./run.sh
```

手动（在 monorepo 根目录）：

```bash
go run ./agentprimordia/ecosystem/examples/wasm-tool-supply-chain
```

单测（12 个用例，约 0.5 秒，零外部依赖）：

```bash
go test ./agentprimordia/ecosystem/examples/wasm-tool-supply-chain/
```

**零凭据**：不需要 `AP_LLM_API_KEY`，不发起任何网络请求。`run.sh` 本地构建
二进制、在仓库外临时目录运行（`GOWORK=off`），退出前断言关键输出串。

## 每一步对应哪条安全不变式

不变式定义见 [AGENTS.md §2.3](../../../../AGENTS.md) 与
[提案-code层沙箱受控释放.md §2.2](../../../../docs/提案-code层沙箱受控释放.md)；
英文口径见概念页 [security-model.md](../../../docs/concepts/security-model.md)。

| demo 步骤 | 不变式 | 实证内容 |
|---|---|---|
| `[1/7]` 手工组装 WASM 模块 | INV-0 | 工件是**数据**不是代码；宿主编译/加载边界之外不存在「先落盘再跑」的通道 |
| `[2/7]` ed25519 签名 | INV-0 / A6 | 签名覆盖 `SHA-256(工件)`；篡改 1 字节即验签失败（负对照） |
| `[3/7]` 公钥钉扎 | INV-0 | 信任锚是运维侧公钥；**空钉扎集合 = 拒绝一切**（fail-closed） |
| `[4/7]` 未签名工件拒绝 | **A6** 签名前置 | 在编译/加载之前被拒（`RegisterTool` 的签名门） |
| `[5/7]` 公钥未钉扎拒绝 | INV-0 信任链 | 签名有效但签名方不受信——agent 不得自铸密钥自签入库 |
| `[6/7]` 非 WASM 工件拒绝 | INV-0 / A4 / A7 | shell 脚本过了签名门，仍被沙箱编译拦下（宿主零执行） |
| `[7/7]` 篡改工件拒绝 | **A6** | 篡改点落在数据段，工件仍是合法 WASM（可编译），但验签失败 |
| `[7/7]` 注册 + 沙箱执行 | **A4** / **A7** / INV-0 | 资源上限 256 页(16MB)/30s；导入段 0 项宿主导入；唯一合法执行位置是 wazero |
| 全程 | A8 | 本 demo 不写任何文件（`run.sh` 的临时目录在仓库外，退出即删） |

**不在本 demo 范围内**（诚实登记，各自的断言在别处）：

- **A1** wazero 依赖边界、**A2** 零动态加载、**A3** 零宿主派生执行 →
  `internal/security/host_boundary_test.go`；
- **A5** CPU 死循环可终止 → `wasm/sandbox_boundary_test.go`（需无限循环模块，
  本 demo 未构造）；
- **A8** 宿主工作树哈希对比 → `wasm/sandbox_boundary_test.go`。

## 代码结构

| 文件 | 职责 |
|------|------|
| `main.go` | 编排与结构化输出：7 步全链路 + 四类拒绝 + 成功分支 |
| `artifact.go` | **手工字节组装**最小有效 WASM 工具模块（magic/段/opcode 全字面量 + LEB128） |
| `gate.go` | 公钥钉扎 + 验签门（复刻 `cmd/ap/tool_forge.go` 的 `pinnedArtifactVerifier` 语义） |
| `reject.go` | 四类拒绝分支的实证 + 篡改工件「仍可编译」佐证 |
| `agent.go` | 零凭据脚本化 provider + ReActAgent 运行 |
| `main_test.go` | 12 个确定性单测（字节稳定性 / 执行对账 / 各拒绝分支 / agent 调用） |
| `run.sh` | 一键运行 + 关键输出断言（CI 门同款口径） |

## 两个实现细节（为什么值得读）

**① WASM 模块是手工组装的，且做了可对账的计算。**
范式照抄 [`wasm/runtime_e2e_test.go`](../../../wasm/runtime_e2e_test.go) 的
`validTestWASM()`（memory/alloc/tool_execute/free 四导出 + 对应段），差异只有
一处：本模块多一个 data 段，`tool_execute` 会把宿主写入线性内存的参数逐字节
求和（mod 256）后以两位小写十六进制写回载荷。宿主读回后，demo 用 Go **独立
复算同一值并逐位比对**——这证明参数真的经过了 WASM 线性内存往返，而不是常量
返回。`alloc` 是常量桩（每次执行只分配一次），已在代码注释中如实标注。

**② 签名门与沙箱门是两道分工不同的门。**
签名门回答「谁造的」（A6），沙箱门回答「这是什么」（编译 + 导入段白名单）。
拒绝分支 C 特意构造了一个**已签名且公钥已钉扎**的 shell 脚本：它过得去签名门，
过不去沙箱门——这正是「宿主零执行」的落点。

## 已知缺口 / 版本状态

- **pkg 的 WASM 面已标记 v8.0 移除**（`pkg/wasm.go` 全部导出带 `[v8.0-cut]`，
  理由：仓内 0 外部采用证据）。本 demo 仍可用它跑通；若 v8.0 移除 pkg 面，
  本 demo 需改走 CLI 或新公共面。安全不变式本身不随 API 形状变动。
- **生成器无生产实现**（见页首）——沙箱管线的正确定位就是「自带工件的安全
  供应链」，不是 agent 自主写代码。

## 延伸阅读

- 英文概念页（安全模型）：[security-model.md](../../../docs/concepts/security-model.md)
- 英文概念页（框架定位）：[framework-positioning.md](../../../docs/concepts/framework-positioning.md)
- 安全提案原文（INV-0 与 A1–A8 定义）：[提案-code层沙箱受控释放.md](../../../../docs/提案-code层沙箱受控释放.md)
- 签名实现：[wasm/signing.go](../../../wasm/signing.go)（ed25519 + SHA-256 + 指纹）
- 沙箱与 ABI：[wasm/sandbox.go](../../../wasm/sandbox.go)（`ExecuteWithMemory` 调用协议）
- 生产装配范例（钉扎语义出处）：[cmd/ap/tool_forge.go](../../../cmd/ap/tool_forge.go)
- 运行时端到端参照测试（本 demo 的范式出处）：[wasm/runtime_e2e_test.go](../../../wasm/runtime_e2e_test.go)
- 同族示例（工具智能 + 验签门）：[tool-intelligence](../tool-intelligence/README.md)
- CI 门：[.github/workflows/wasm-smoke.yml](../../../../.github/workflows/wasm-smoke.yml)
