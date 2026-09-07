#!/bin/bash
# v7.2 统一 Bench 运行器
# 用法: ./run.sh [额外参数]
set -euo pipefail
cd "$(dirname "$0")/../.."
go run ./bench/v72 \
  --prop "${V72_PROP:-p1}" \
  --model "${V72_MODEL:-sensenova-6.8-flash-lite}" \
  --base-url "${V72_BASE_URL:-https://token.sensenova.cn/v1}" \
  --api-key "${V72_API_KEY:-sk-IE84nyrP9MdJXleKAfAKGNiawk81sHZW}" \
  --out bench/results/v72 \
  "$@"
