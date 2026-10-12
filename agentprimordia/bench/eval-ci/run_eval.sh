#!/usr/bin/env bash
# run_eval.sh — AgentPrimordia Eval CI runner
# 运行 eval 用例并按 threshold 判定通过/失败
#
# 用法：
#   ./bench/eval-ci/run_eval.sh [--threshold 0.8] [--json]
#
# 退出码：
#   0 — 通过（pass_rate >= threshold）
#   1 — 失败（pass_rate < threshold）
#   2 — 配置错误
#
# 前置：
#   - go 1.26+
#   - agentprimordia/internal/agent/eval 包已编译通过
#
# 输出格式：
#   - 默认：人类可读摘要
#   - --json：JSON 格式（便于 CI 解析）

set -euo pipefail

THRESHOLD=0.8
JSON_OUTPUT=false
GO_TEST_FILTER="TestExactMatch|TestEvalSuite|TestEvaluator"
EVAL_PKG="agentprimordia/internal/agent/eval/..."

for arg in "$@"; do
  case $arg in
    --threshold=*)
      THRESHOLD="${arg#*=}"
      ;;
    --threshold)
      shift
      THRESHOLD="${1:-0.8}"
      ;;
    --json)
      JSON_OUTPUT=true
      ;;
    --help|-h)
      cat <<EOF
Usage: $0 [--threshold <float>] [--json]

Options:
  --threshold <float>   最小通过率（默认 0.8）
  --json                输出 JSON 格式结果
  --help, -h            显示本帮助
EOF
      exit 0
      ;;
  esac
done

# 验证 threshold 是合法数字
if ! [[ "$THRESHOLD" =~ ^[0-9]*\.?[0-9]+$ ]] || (( $(echo "$THRESHOLD > 1.0" | bc -l 2>/dev/null || echo 0) )); then
  echo "ERROR: --threshold must be a float in [0, 1], got '$THRESHOLD'" >&2
  exit 2
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AGENTPRIMORDIA_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

cd "$AGENTPRIMORDIA_ROOT"

echo "==> AgentPrimordia Eval Runner"
echo "    Threshold: $THRESHOLD"
echo "    Filter:   $GO_TEST_FILTER"
echo "    Package:  $EVAL_PKG"
echo ""

# 运行 go test 并解析输出
TEST_OUTPUT="$(mktemp)"
trap "rm -f $TEST_OUTPUT" EXIT

set +e
# 工具链下限：go1.26.9 起修复了 govulncheck 报出的可达标准库漏洞
# （原默认值为 go1.26.4，与仓库其余部分的 1.26.9 基线不一致）
GOTOOLCHAIN="${GOTOOLCHAIN:-go1.26.9}" go test -count=1 -v -run "$GO_TEST_FILTER" "$EVAL_PKG" 2>&1 | tee "$TEST_OUTPUT"
TEST_EXIT=$?
set -e

# 统计通过/失败
PASSED=$(grep -c "^--- PASS:" "$TEST_OUTPUT" || true)
FAILED=$(grep -c "^--- FAIL:" "$TEST_OUTPUT" || true)
SKIPPED=$(grep -c "^--- SKIP:" "$TEST_OUTPUT" || true)
TOTAL=$((PASSED + FAILED))

if [ "$TOTAL" -eq 0 ]; then
  echo "ERROR: no tests matched filter '$GO_TEST_FILTER'" >&2
  exit 2
fi

# 计算通过率（用 awk 避免 bc 依赖）
PASS_RATE=$(awk "BEGIN { printf \"%.4f\", $PASSED / $TOTAL }")

# 判定
PASSED_THRESHOLD=$(awk "BEGIN { print ($PASS_RATE >= $THRESHOLD) ? 1 : 0 }")

if [ "$JSON_OUTPUT" = true ]; then
  cat <<EOF
{
  "total": $TOTAL,
  "passed": $PASSED,
  "failed": $FAILED,
  "skipped": $SKIPPED,
  "pass_rate": $PASS_RATE,
  "threshold": $THRESHOLD,
  "meets_threshold": $PASSED_THRESHOLD,
  "go_test_exit": $TEST_EXIT
}
EOF
else
  echo "==> Results"
  echo "    Total:   $TOTAL"
  echo "    Passed:  $PASSED"
  echo "    Failed:  $FAILED"
  if [ "$SKIPPED" -gt 0 ]; then
    echo "    Skipped: $SKIPPED"
  fi
  echo "    Rate:    $PASS_RATE (threshold: $THRESHOLD)"
  echo ""

  if [ "$FAILED" -gt 0 ]; then
    echo "==> Failed tests:"
    grep "^--- FAIL:" "$TEST_OUTPUT" || true
    echo ""
  fi

  if [ "$PASSED_THRESHOLD" = "1" ]; then
    echo "✅ Eval PASSED (rate $PASS_RATE >= threshold $THRESHOLD)"
    exit 0
  else
    echo "❌ Eval FAILED (rate $PASS_RATE < threshold $THRESHOLD)"
    exit 1
  fi
fi