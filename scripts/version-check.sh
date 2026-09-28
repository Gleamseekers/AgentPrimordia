#!/usr/bin/env bash
# version-check.sh — 跨语言版本一致性检查
#
# 检查以下三处版本定义是否一致：
#   1. agentprimordia/pkg/agent.go    — const Version = "x.y.z"
#   2. sdk/typescript/package.json    — "version": "x.y.z"
#   3. agentprimordia/docs/版本规范.md — 版本表中 Go SDK 行
#
# 规则：
#   - Go 版本 与 Docs 版本必须完全一致
#   - TS 版本允许 -beta / -rc 等预发布后缀，基础版本号应与 Go 一致
#   - 至少 major.minor 必须匹配
#
# 用法：bash scripts/version-check.sh

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

GO_FILE="${REPO_ROOT}/agentprimordia/pkg/agent.go"
TS_FILE="${REPO_ROOT}/sdk/typescript/package.json"
DOC_FILE="${REPO_ROOT}/agentprimordia/docs/版本规范.md"

ERRORS=0

# ===== 提取版本号 =====

# Go: const Version = "x.y.z"
# 注意：用可移植的 sed -E（BSD/GNU 通用），不要用 grep -oP——macOS 自带 grep 无 PCRE，
# 否则本地无法复现 CI 门（历史踩坑）。
GO_VERSION=$(sed -E -n 's/.*const[[:space:]]+Version[[:space:]]*=[[:space:]]*"([^"]*)".*/\1/p' "$GO_FILE" 2>/dev/null | head -1 || true)
if [ -z "$GO_VERSION" ]; then
  echo "::error::无法从 ${GO_FILE} 提取 Go 版本号"
  ERRORS=$((ERRORS + 1))
fi

# TS: "version": "x.y.z"
TS_VERSION=$(sed -E -n 's/.*"version"[[:space:]]*:[[:space:]]*"([^"]*)".*/\1/p' "$TS_FILE" 2>/dev/null | head -1 || true)
if [ -z "$TS_VERSION" ]; then
  echo "::error::无法从 ${TS_FILE} 提取 TS 版本号"
  ERRORS=$((ERRORS + 1))
fi

# Docs: 版本表中 Go SDK 行的版本号
DOC_GO_VERSION=$(grep -E '^[[:space:]]*\|[[:space:]]*Go SDK' "$DOC_FILE" | grep -oE 'v?[0-9]+\.[0-9]+\.[0-9]+' | head -1 | sed -E 's/^v//' || true)
if [ -z "$DOC_GO_VERSION" ]; then
  echo "::error::无法从 ${DOC_FILE} 版本表提取 Go SDK 版本号"
  ERRORS=$((ERRORS + 1))
fi

# Docs: 版本表中 TS SDK 行的版本号
DOC_TS_VERSION=$(grep -E '^[[:space:]]*\|[[:space:]]*TypeScript SDK' "$DOC_FILE" | grep -oE 'v?[0-9]+\.[0-9]+\.[0-9]+' | head -1 | sed -E 's/^v//' || true)

if [ "$ERRORS" -gt 0 ]; then
  echo ""
  echo "::error::版本号提取失败，请检查文件格式"
  exit 1
fi

# ===== 输出版本信息 =====

echo "========================================"
echo "  跨语言版本一致性检查"
echo "========================================"
echo ""
echo "  Go SDK (pkg/agent.go):       ${GO_VERSION}"
echo "  TS SDK (package.json):       ${TS_VERSION}"
echo "  Docs Go SDK (版本规范.md): ${DOC_GO_VERSION}"
if [ -n "$DOC_TS_VERSION" ]; then
  echo "  Docs TS SDK (版本规范.md): ${DOC_TS_VERSION}"
fi
echo ""

# ===== 比较函数 =====

# 去除预发布后缀，返回 x.y.z
strip_prerelease() {
  echo "$1" | grep -oE '^[0-9]+\.[0-9]+\.[0-9]+'
}

# 提取 major 版本号
major_version() {
  echo "$1" | cut -d. -f1
}

# 提取 major.minor
major_minor() {
  echo "$1" | cut -d. -f1-2
}

FAIL=0

# 检查 1: Go 版本 vs Docs Go 版本
if [ "$GO_VERSION" != "$DOC_GO_VERSION" ]; then
  echo "::error::Go 版本 (${GO_VERSION}) 与 版本规范.md 中 Go SDK 版本 (${DOC_GO_VERSION}) 不一致"
  echo "  请更新 agentprimordia/docs/版本规范.md 版本表中的 Go SDK 行"
  FAIL=1
else
  echo "OK: Go 版本与 版本规范.md 一致 (${GO_VERSION})"
fi

# 检查 2: TS 基础版本 vs Go 版本
TS_BASE=$(strip_prerelease "$TS_VERSION")
TS_SUFFIX="${TS_VERSION#${TS_BASE}}"

if [ -n "$TS_SUFFIX" ]; then
  echo "INFO: TS SDK 包含预发布后缀: ${TS_SUFFIX}"
fi

if [ "$TS_BASE" != "$GO_VERSION" ]; then
  # 至少 major.minor 必须匹配
  TS_MM=$(major_minor "$TS_BASE")
  GO_MM=$(major_minor "$GO_VERSION")
  if [ "$TS_MM" != "$GO_MM" ]; then
    echo "::error::TS SDK 版本 (${TS_VERSION}, 基础: ${TS_BASE}) 与 Go 版本 (${GO_VERSION}) 的 major.minor 不匹配"
    echo "  TS major.minor: ${TS_MM}, Go major.minor: ${GO_MM}"
    echo "  请更新 sdk/typescript/package.json 的版本号"
    FAIL=1
  else
    echo "WARN: TS SDK patch 版本不一致 (TS: ${TS_BASE}, Go: ${GO_VERSION})，major.minor 匹配"
  fi
else
  echo "OK: TS SDK 基础版本与 Go 版本一致 (${GO_VERSION})"
fi

# 检查 3: Docs TS 版本 vs package.json TS 版本（如果文档中有记录）
if [ -n "$DOC_TS_VERSION" ]; then
  if [ "$TS_BASE" != "$DOC_TS_VERSION" ]; then
    TS_DOC_MM=$(major_minor "$DOC_TS_VERSION")
    if [ "$TS_MM" != "$TS_DOC_MM" ]; then
      echo "::error::package.json TS 版本 (${TS_BASE}) 与 版本规范.md 中 TS SDK 版本 (${DOC_TS_VERSION}) 的 major.minor 不匹配"
      FAIL=1
    else
      echo "WARN: Docs 中 TS 版本 (${DOC_TS_VERSION}) 与 package.json (${TS_BASE}) patch 不一致"
    fi
  else
    echo "OK: Docs TS 版本与 package.json 一致 (${DOC_TS_VERSION})"
  fi
fi

echo ""

if [ "$FAIL" -eq 1 ]; then
  echo "::error::版本一致性检查失败！请确保以下三处版本保持同步："
  echo "  1. agentprimordia/pkg/agent.go          — const Version"
  echo "  2. sdk/typescript/package.json           — \"version\""
  echo "  3. agentprimordia/docs/版本规范.md     — 版本表"
  exit 1
fi

echo "========================================"
echo "  版本一致性检查通过"
echo "========================================"
