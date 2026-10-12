#!/usr/bin/env bash
# wasm-tool-supply-chain demo 一键运行：签名 → 钉扎 → 注册 → 沙箱执行。
#
# 用法（任意工作目录）：
#   ./run.sh
#
# 口径（对齐 .github/workflows/wasm-smoke.yml clean-room 门）：
#   - 本地构建 demo 二进制（不依赖 go install 代理）；
#   - 二进制放在仓库外临时目录，证明 demo 不依赖仓库内相对状态；
#   - GOWORK=off：显式关闭 workspace 模式，走模块依赖图；
#   - 零凭据：不设 AP_LLM_API_KEY，脚本化 provider 不发任何网络请求；
#   - 退出前断言关键输出串，失败即非零退出。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# 仓库根：wasm-tool-supply-chain → examples → ecosystem → agentprimordia → 仓库根（go.mod 所在）
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../../.." && pwd)"

WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/wasm-sc.XXXXXX")"
BIN="${WORK_DIR}/wasm-tool-supply-chain"

cleanup() {
  rm -rf "${WORK_DIR}"
}
trap cleanup EXIT

echo "=== [1/3] 构建 demo 二进制（本地构建，GOWORK=off）==="
( cd "${REPO_ROOT}" && GOWORK=off go build -o "${BIN}" ./agentprimordia/ecosystem/examples/wasm-tool-supply-chain )
test -s "${BIN}"
echo "OK: ${BIN}"

echo ""
echo "=== [2/3] 运行 demo（仓库外临时目录，零凭据）==="
# 框架的 slog 日志走 stderr，与 stdout 的结构化输出分离：stderr 单独落盘后
# 原样展示（作为框架侧证据），断言只针对 stdout 的固定串——避免日志时间戳
# 污染可断言输出。
STDERR_LOG="${WORK_DIR}/demo.stderr"
set +e
OUTPUT="$("${BIN}" 2>"${STDERR_LOG}")"
RC=$?
set -e
echo "${OUTPUT}"
echo "--- 框架日志（stderr，含时间戳，仅作证据展示）---"
cat "${STDERR_LOG}" 2>/dev/null || true
if [ "${RC}" -ne 0 ]; then
  echo "::error::demo 退出码 ${RC}" >&2
  exit "${RC}"
fi

echo ""
echo "=== [3/3] 断言关键输出 ==="
assert_contains() {
  if ! printf '%s\n' "${OUTPUT}" | grep -qF "$1"; then
    echo "::error::demo 输出缺少关键串: $1" >&2
    exit 1
  fi
}
# 四个分支 + 成功路径各至少一条断言（串与 main.go 的真实输出逐条对应）
assert_contains "=== WASM 工具安全供应链 Demo（v7.7 副线）==="
assert_contains "诚实边界：工具生成器无生产实现"
assert_contains "[1/7] 手工组装最小有效 WASM 工具模块（字节级，无外部工具链）"
assert_contains "范式出处: agentprimordia/wasm/runtime_e2e_test.go validTestWASM()"
assert_contains "[2/7] 开发者签名（ed25519 over SHA-256(工件)）"
assert_contains "验签 · 原始工件: 通过"
assert_contains "验签 · 篡改 1 字节"
assert_contains "[3/7] 公钥钉扎（fail-closed：空钉扎集合 = 拒绝一切）"
assert_contains "空钉扎集合: 0 个受信公钥 → 钉扎集合为空：无任何受信公钥，拒绝一切（fail-closed）"
assert_contains "[4/7] 拒绝分支 A · 未签名工件（A6 签名前置）"
assert_contains "✗ 拒绝: wasm: tool \"unsigned-tool\" 注册被拒绝：缺少签名或公钥（A6 签名前置）"
assert_contains "[5/7] 拒绝分支 B · 签名公钥未钉扎"
assert_contains "✗ 拒绝: 签名公钥未在钉扎集合中（指纹"
assert_contains "[6/7] 拒绝分支 C · 非 WASM 工件（shell 脚本，已签名且公钥已钉扎）"
assert_contains "✗ 拒绝: wasm: load module for tool \"shell-tool\""
assert_contains "[7/7] 篡改工件被拒 + 验签门放行 → 注册 → agent 在沙箱中真实调用"
assert_contains "✗ 篡改工件被验签门拒绝"
assert_contains "篡改工件在沙箱中仍可编译（仍是合法 WASM）"
assert_contains "✓ 验签门放行: 签名有效 + 公钥已钉扎"
assert_contains "✓ 注册成功: name=wasm-checksum-tool"
assert_contains "✓ 注册进 tools.Registry（AsTool 桥，ReActAgent toolkit 调用路径）"
assert_contains "✓ 导入段审计(A7): tool_wasm-checksum-tool 导入 0 项宿主函数"
assert_contains "✓ agent 运行（零凭据脚本化 provider，无网络调用）: tool_calls=1"
assert_contains "✓ 沙箱执行结果: wasm-tool-exec ok; input-checksum="
assert_contains "✓ 独立复算对账: 沙箱内计算与宿主复算一致"
assert_contains "=== 全链路通过：签名 → 钉扎 → 注册 → 沙箱执行 ==="

echo ""
echo "OK: wasm-tool-supply-chain demo 全链路通过（签名→钉扎→注册→沙箱执行，四类拒绝均有实证）"
