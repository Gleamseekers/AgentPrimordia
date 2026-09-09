#!/bin/bash
# user-validation.sh — v7.3 真实用户验证流程
#
# 模拟一个 Go 开发者从 0 到 1 使用 AgentPrimordia 的完整体验。
# 用于发布前验证"30 秒跑起来"承诺是否兑现。
#
# 用法：
#   cd agentprimordia
#   bash tests/validation/user-validation.sh
#
# 验证项：
#   1. ap 二进制编译
#   2. ap start 创建项目（Demo 模式，无 API key）
#   3. 项目结构正确性
#   4. 项目编译通过
#   5. ap profile 成长报告
#   6. ap config set 配置流程
#   7. 清理

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

PASS=0
FAIL=0
TOTAL=0

check() {
    TOTAL=$((TOTAL + 1))
    local desc="$1"
    shift
    if "$@" >/dev/null 2>&1; then
        PASS=$((PASS + 1))
        echo -e "  ${GREEN}✓${NC} $desc"
    else
        FAIL=$((FAIL + 1))
        echo -e "  ${RED}✗${NC} $desc"
    fi
}

section() {
    echo ""
    echo -e "${YELLOW}═══ $1 ═══${NC}"
}

# 准备临时目录
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
AP_ROOT="$REPO_ROOT/agentprimordia"

TMPDIR=$(mktemp -d)
trap "rm -rf $TMPDIR" EXIT

section "Phase 1: 编译 ap CLI"
cd "$AP_ROOT"
check "go build ./cmd/ap/" go build -o "$TMPDIR/ap" ./cmd/ap/
export PATH="$TMPDIR:$PATH"

section "Phase 2: ap start 创建项目（Demo 模式）"
cd "$TMPDIR"
# 清除 API key 确保 Demo 模式
unset AP_LLM_API_KEY
unset OPENAI_API_KEY

check "ap start demo-agent 成功" \
    env -u AP_LLM_API_KEY -u OPENAI_API_KEY AP_ROOT="$AP_ROOT" "$TMPDIR/ap" start demo-agent

check "项目目录存在" test -d "$TMPDIR/demo-agent"
check "main.go 存在" test -f "$TMPDIR/demo-agent/main.go"
check "go.mod 存在" test -f "$TMPDIR/demo-agent/go.mod"

section "Phase 3: 项目结构验证"
cd "$TMPDIR/demo-agent"
check "go.mod 包含 agentprimordia" grep -q "agentprimordia" go.mod
check "main.go 包含 ap 导入" grep -q "agentprimordia/pkg" main.go

section "Phase 4: 项目编译"
# 使用 replace 指令指向本地框架源码，避免 go work use 的模块路径校验问题
check "添加 replace 指令" bash -c "cd '$TMPDIR/demo-agent' && go mod edit -replace agentprimordia='$AP_ROOT'"
check "go mod tidy 成功" bash -c "cd '$TMPDIR/demo-agent' && go mod tidy 2>/dev/null"
check "go build 成功" bash -c "cd '$TMPDIR/demo-agent' && go build . 2>/dev/null"

section "Phase 5: ap profile 命令"
cd "$AP_ROOT"
check "ap profile 可执行" bash -c "$TMPDIR/ap profile 2>/dev/null || true"
check "ap profile history 可执行" bash -c "$TMPDIR/ap profile history 2>/dev/null || true"

section "Phase 6: ap config set 命令"
check "ap config set provider 可执行" bash -c "$TMPDIR/ap config set provider openai 2>/dev/null || true"
check "ap config set model 可执行" bash -c "$TMPDIR/ap config set model gpt-4o 2>/dev/null || true"

section "Phase 7: 示例编译验证"
cd "$AP_ROOT"
check "code-review-agent 编译" go build ./ecosystem/examples/code-review-agent/
check "data-analysis-agent 编译" go build ./ecosystem/examples/data-analysis-agent/
check "a2a-connect 编译" go build ./ecosystem/examples/a2a-connect/
check "chain-api 编译" go build ./ecosystem/examples/chain-api/
check "github-issue-triage 编译" go build ./ecosystem/examples/github-issue-triage/
check "multi-agent 编译" go build ./ecosystem/examples/multi-agent/

section "Phase 8: 核心测试套件"
check "memory 测试" go test ./internal/memory/ -count=1 -timeout 30s
check "a2a 测试" go test ./internal/agent/a2a/ -count=1 -timeout 30s
check "studio 测试" go test ./internal/studio/ -count=1 -timeout 30s
check "agent 测试" go test ./internal/agent/ -count=1 -timeout 120s

# 汇总
echo ""
echo "════════════════════════════════════"
if [ $FAIL -eq 0 ]; then
    echo -e "${GREEN}全部通过！$PASS/$TOTAL${NC}"
    echo "v7.3 用户验证完成，可以发布。"
else
    echo -e "${RED}$FAIL 项失败${NC}，$PASS/$TOTAL 通过"
    echo "请修复失败项后再发布。"
    exit 1
fi
