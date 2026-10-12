#!/usr/bin/env bash
# a2a-handoff demo 一键编排：server 与 client 两个独立进程，跑通
# 「发现 Agent Card → 委托 → 轮询至终态 → 取回结果 → 取消」全链路。
#
# 用法（任意工作目录）：
#   ./run.sh
#
# 口径（对齐 .github/workflows/a2a-smoke.yml clean-room 门）：
#   - 本地构建 demo 二进制（不依赖 go install 代理）；
#   - 二进制与日志放在仓库外临时目录，证明 demo 不依赖仓库内相对状态；
#   - GOWORK=off：显式关闭 workspace 模式，走模块依赖图；
#   - server 绑定随机端口（:0），实际地址经 stdout 协商标识行传递给 client；
#   - 退出前对 client 输出断言关键串，失败即非零退出。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# 仓库根：a2a-handoff → examples → ecosystem → agentprimordia → 仓库根（go.mod 所在）
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../../.." && pwd)"

WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/a2a-handoff.XXXXXX")"
BIN="${WORK_DIR}/a2a-handoff"
SERVER_LOG="${WORK_DIR}/server.log"
CLIENT_LOG="${WORK_DIR}/client.log"

cleanup() {
  if [ -n "${SERVER_PID:-}" ]; then
    kill "${SERVER_PID}" 2>/dev/null || true
    wait "${SERVER_PID}" 2>/dev/null || true
  fi
  rm -rf "${WORK_DIR}"
}
trap cleanup EXIT

echo "=== [1/4] 构建 demo 二进制（本地构建，GOWORK=off）==="
( cd "${REPO_ROOT}" && GOWORK=off go build -o "${BIN}" ./agentprimordia/ecosystem/examples/a2a-handoff )
test -s "${BIN}"
echo "OK: ${BIN}"

echo "=== [2/4] 启动 server 进程（随机端口，仓库外临时目录运行）==="
AP_A2A_HANDOFF_ADDR="127.0.0.1:0" "${BIN}" server >"${SERVER_LOG}" 2>&1 &
SERVER_PID=$!

# 等待 server 就绪：解析其 stdout 的协商标识行
SERVER_ADDR=""
for _ in $(seq 1 100); do
  if ! kill -0 "${SERVER_PID}" 2>/dev/null; then
    echo "::error::server 进程提前退出" >&2
    cat "${SERVER_LOG}" >&2
    exit 1
  fi
  SERVER_ADDR="$(sed -n 's/^A2A_HANDOFF_SERVER_ADDR=//p' "${SERVER_LOG}" | head -n 1)"
  [ -n "${SERVER_ADDR}" ] && break
  sleep 0.1
done
if [ -z "${SERVER_ADDR}" ]; then
  echo "::error::server 未在 10s 内就绪" >&2
  cat "${SERVER_LOG}" >&2
  exit 1
fi
echo "OK: server 就绪于 ${SERVER_ADDR}（pid=${SERVER_PID}）"

echo "=== [3/4] 运行 client 进程（独立进程，经开放协议调用 server）==="
set +e
AP_A2A_HANDOFF_ADDR="${SERVER_ADDR}" "${BIN}" client 2>&1 | tee "${CLIENT_LOG}"
CLIENT_RC="${PIPESTATUS[0]}"
set -e
if [ "${CLIENT_RC}" -ne 0 ]; then
  echo "::error::client 进程退出码 ${CLIENT_RC}" >&2
  cat "${SERVER_LOG}" >&2
  exit "${CLIENT_RC}"
fi

echo "=== [4/4] 关闭 server 并断言关键输出 ==="
kill "${SERVER_PID}" 2>/dev/null || true
wait "${SERVER_PID}" 2>/dev/null || true
SERVER_PID=""
echo "--- server 日志 ---"
cat "${SERVER_LOG}"

# 断言关键字与 client.go 的真实输出逐条对应（grep -F 固定串）
assert_contains() {
  if ! grep -qF "$1" "${CLIENT_LOG}"; then
    echo "::error::client 输出缺少关键串: $1" >&2
    exit 1
  fi
}
assert_contains "=== A2A 跨进程委托 Demo（client 进程）==="
assert_contains "[1/5] 发现 Agent Card: name=ap-handoff-agent"
assert_contains "[2/5] 委托任务:"
assert_contains "[3/5] 轮询至终态: state=completed"
assert_contains "[4/5] 取回结果:"
assert_contains "[5/5] 取消演示:"
assert_contains "协议符合性: 100%（8/8 项检查通过）"
assert_contains "=== A2A 跨进程委托 Demo 完成 ==="

echo ""
echo "OK: a2a-handoff demo 全链路通过（发现→委托→轮询→取回→取消，符合性 8/8）"
