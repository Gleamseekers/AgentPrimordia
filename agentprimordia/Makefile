.PHONY: build test test-race test-short test-integration test-distributed-backends lint lint-all lint-go lint-ts cover cover-html cover-check \
        dual-line-recall \
        deprecation-check benchmark api-diff api-extract cover-trend clean run-hello run-multi run-production \
        docker-build docker-run docker-clean \
        pgo-profile pgo-build pgo-clean test-ts test-affected

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS = -ldflags="-X main.Version=$(VERSION)"

# 项目强制无 CGO（modernc.org/sqlite 为纯 Go 驱动），所有目标默认继承此设置。
export CGO_ENABLED=0

# PGO（Profile-Guided Optimization）配置
# Go 1.21+ 支持 PGO，可通过代表性工作负载 profile 指导编译器优化（通常 2-7% 提速）
# 详见 docs/advanced/pgo.md
PGO_PROFILE ?= default.pgo
PGO_DIR ?= .pgo

build:
	go build $(LDFLAGS) -o ap ./cmd/ap/

test:
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# race 检测需要 CGO，仅在已安装 C 编译器的环境显式调用：
#   make test-race CGO_ENABLED=1
# Windows 默认无 gcc，请勿在此平台启用。
test-race:
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# 跨平台友好的全量测试（语义 = 无 -race，Windows 无 gcc 也能跑）
# 注意：名字里的 "short" 是历史遗留，本目标**不传** -short。全仓仅 6 个测试文件使用
# testing.Short()，一旦传 -short 会静默少跑用例，故此处保持全量。需要真正的短测请显式
# go test -short ./...（2026-10-17 澄清，避免"门禁其实只跑了一部分"的误判）。
test-short:
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# 集成测试（需要 OPENAI_API_KEY 等环境变量）
test-integration:
	go test -tags=integration -timeout 5m -v ./...

# 分布式后端集成测试（需要真实 etcd + Redis）
# 先启动依赖：docker compose -f deploy/compose/distributed-test.yaml up -d
# 无服务时测试会优雅跳过（skip），有服务时跑真实 CRUD/租约/发现链路
test-distributed-backends:
	go test -tags=etcd,redis -count=1 -timeout 10m -v ./internal/persist/...
	go test -tags=etcd -count=1 -timeout 10m -v ./internal/agent/cluster/...

# 跑覆盖率检查并打印报告
cover:
	go test -coverprofile=coverage.out ./internal/... ./pkg/... 2>&1 | tee coverage.txt
	@echo ""
	@echo "=== 覆盖率汇总 ==="
	@grep "coverage:" coverage.txt | sort

# 生成 HTML 报告（需先跑 cover）
cover-html:
	go tool cover -html=coverage.out -o coverage.html
	@echo "已生成 coverage.html，浏览器打开查看"

# 覆盖率门槛检查（Phase 7.2 引入；默认镜像 CI 分层门 ci.yml: "internal/agent:80" "internal/llm:65" "pkg:65"）
# 实现约束：整段必须落在**同一个 shell** 内。Makefile 每条 recipe 行各起一个 shell，
# 旧实现把 fail=0 / for / if [ $fail -eq 1 ] 拆成三行，变量不跨 shell 共享，失败分支
# 永远不触发（实测报 `[: -eq: unary operator expected` 后仍打印 OK）——2026-10-17 修复。
# 另外三点（同日复核补）：
#   ① 必须检查 `go test` 退出码——测试失败时 go test 仍会打印覆盖率，只比对数字会假绿；
#   ② 解析不出覆盖率时按 FAIL 处理（旧实现把"没有测试文件"和"编译失败"一起放进 SKIP 假绿）；
#   ③ 本目标只做「覆盖率抽检」，不是全量测试门（全量测试见 make test-short / CI test job），
#      且 `./internal/agent` 只统计该单包（不含子包），与 CI 同口径。
AGENT_COVER_THRESH ?= 80
COVER_THRESH ?= 65

cover-check:
	@echo "=== 覆盖率门槛检查（agent ≥ $(AGENT_COVER_THRESH)% / llm、pkg ≥ $(COVER_THRESH)%）==="
	@fail=0; \
	for tier in internal/agent:$(AGENT_COVER_THRESH) internal/llm:$(COVER_THRESH) pkg:$(COVER_THRESH); do \
		pkg=$${tier%%:*}; thresh=$${tier##*:}; \
		out=$$(go test -cover ./$$pkg 2>&1); rc=$$?; \
		if [ $$rc -ne 0 ]; then \
			echo "  FAIL: $$pkg go test 退出码 $${rc}（测试或编译失败）"; \
			fail=1; continue; \
		fi; \
		cov=$$(printf '%s\n' "$$out" | grep -oE '[0-9.]+%' | head -1 | tr -d '%'); \
		if [ -z "$$cov" ]; then \
			echo "  FAIL: $$pkg 无法解析覆盖率输出（是否根本没有测试文件？）"; \
			fail=1; continue; \
		fi; \
		echo "  $$pkg: $$cov% (门槛 $$thresh%)"; \
		if awk -v c=$$cov -v t=$$thresh 'BEGIN { exit !(c < t) }'; then \
			echo "  FAIL: $$pkg 覆盖率 $$cov% < $$thresh%"; \
			fail=1; \
		fi; \
	done; \
	if [ $$fail -eq 1 ]; then \
		echo ""; \
		echo "FAIL: 核心包覆盖率未达门槛（agent ≥ $(AGENT_COVER_THRESH)% / llm、pkg ≥ $(COVER_THRESH)%）"; \
		exit 1; \
	fi; \
	echo ""; \
	echo "OK: 核心包覆盖率门槛通过"

# Phase 12.1: API 变化对比
api-diff:
	bash ../scripts/api-diff.sh $(REF)

# API 契约提取：从 pkg/ 提取公共 API 签名，输出 JSON 供跨语言漂移检测
api-extract:
	go run ./scripts/api-extract/ -output ../sdk/typescript/api-contract.json -no-timestamp

# Phase 12.2: 覆盖率趋势跟踪
cover-trend:
	bash ../scripts/cover-trend.sh

# Phase 12.3: Deprecated 标注完整性检查
deprecation-check:
	bash ../scripts/deprecation-check.sh

# S0-3 双线召回对账门（Go 臂 × TS 臂）。CI 对应 job：ci.yml `dual-line-recall`（CI 直接调用本目标，避免两处步骤漂移）。
# 2026-10-17 接线：此前 scripts/dual-line-recall-check.mjs 全仓零消费者（门存在但从不运行）。
# 步骤：① 先删两份夹具，再真跑两侧 lexical 底档臂（AP_WRITE_S03_RESULTS=1，离线无需端点）——
#          "先删后跑"是必要防线：若环境变量丢失或 -run 过滤不中，go test 仍会 exit 0 且 [no tests to run]，
#          夹具写不出来 → ②对账脚本报"结果文件缺失"即红（否则会出现"四步全绿零验证"的假绿）。
#       ② 对账双线差 ≤0.02 / 降级位臂 / 缓存基线；③ 断言重生成结果与已提交夹具逐字节一致，
#          且两路径不得处于 untracked/删除态（git diff --exit-code 对 untracked 无感）。
# 前置：sdk/typescript 已 npm ci（本地跑 TS 臂需要 vitest）。失败中断后请 `git -C .. checkout -- <两夹具>` 恢复。
dual-line-recall:
	@echo "① Go 臂重生成（lexical 底档，先删夹具）"
	@rm -f bench/results/s0-3-recall-go.json ../sdk/typescript/bench/results/s0-3-recall-ts.json
	CGO_ENABLED=0 AP_WRITE_S03_RESULTS=1 go test ./internal/memory/ -run 'TestEmbeddingCorpusRecall' -count=1
	@echo "① TS 臂重生成"
	cd ../sdk/typescript && AP_WRITE_S03_RESULTS=1 npx --no-install vitest run src/llm/__tests__/embedding-recall.test.ts
	@echo "② 双线对账"
	node ../scripts/dual-line-recall-check.mjs
	@echo "③ 夹具确定性（重生成须零 diff，且不得留下 untracked/删除态）"
	git -C .. diff --exit-code -- agentprimordia/bench/results/s0-3-recall-go.json sdk/typescript/bench/results/s0-3-recall-ts.json
	@test -z "$$(git -C .. status --porcelain -- agentprimordia/bench/results/s0-3-recall-go.json sdk/typescript/bench/results/s0-3-recall-ts.json)" \
		|| { echo "FAIL: 夹具处于未跟踪/删除/脏状态（git status --porcelain 非空）"; exit 1; }
	@echo "OK: S0-3 双线召回对账通过（重生成夹具零 diff 且均为已跟踪干净态）"

# Phase 12.4: 性能基准
benchmark:
	go test -bench=. -benchmem -benchtime=5x -count=3 ./bench/suite/... > bench-output.txt
	@echo "Benchmark results: bench-output.txt"

# ===== PGO（Profile-Guided Optimization）=====
# Go 1.21+ 的 PGO 流程：
# 1. go test -cpuprofile 生成 CPU profile（代表性工作负载）
# 2. 将 profile 重命名为 default.pgo 放入主包目录
# 3. go build -pgo=auto 自动使用该 profile 指导编译
# 预期收益：2-7% 性能提升（取决于工作负载特征）
# 详见 https://go.dev/doc/pgo

# pgo-profile: 运行代表性 benchmark 生成 CPU profile
pgo-profile:
	@echo "=== 生成 PGO Profile ==="
	@mkdir -p $(PGO_DIR)
	go test -run=^$$ -bench=. -benchmem -benchtime=10x -cpuprofile=$(PGO_DIR)/cpu.prof ./bench/suite/...
	@echo "Profile 已生成: $(PGO_DIR)/cpu.prof"
	@echo "运行 'make pgo-build' 使用此 profile 构建优化二进制"

# pgo-build: 使用 PGO profile 构建优化的二进制文件
pgo-build:
	@echo "=== PGO 构建 ==="
	@if [ ! -f "$(PGO_DIR)/cpu.prof" ]; then \
		echo "错误: 未找到 PGO profile ($(PGO_DIR)/cpu.prof)"; \
		echo "请先运行 'make pgo-profile'"; \
		exit 1; \
	fi
	cp $(PGO_DIR)/cpu.prof $(PGO_PROFILE)
	go build $(LDFLAGS) -pgo=$(PGO_PROFILE) -o ap ./cmd/ap/
	@echo "PGO 优化二进制已构建: ap"
	@echo "清理: make pgo-clean"

# pgo-clean: 清理 PGO 临时文件
pgo-clean:
	rm -f $(PGO_PROFILE)
	rm -rf $(PGO_DIR)
	@echo "PGO 临时文件已清理"

# Phase 12.5: 统一 lint 入口
lint-all: lint-go lint-ts
	@echo "All lint checks passed"

lint-go:
	golangci-lint run ./...

lint-ts:
	cd ../sdk/typescript && npm run lint
	@echo "=== TS typecheck (tsc --noEmit) ==="
	cd ../sdk/typescript && npx tsc --noEmit

# 统一 TS SDK 测试入口
# sdk/typescript 使用 vitest（直接测试 TS 源码）
test-ts:
	@echo "=== TS SDK 测试 ==="
	cd ../sdk/typescript && npx vitest run --coverage

lint:
	golangci-lint run ./...

clean:
	rm -rf coverage.out coverage.html coverage.txt dist/ bench-output.txt

run-hello:
	go run ./ecosystem/examples/chain-api/

run-multi:
	go run ./ecosystem/examples/multi-agent/

run-production:
	go run ./ecosystem/examples/chain-production/

docker-build:
	docker compose build

docker-run:
	docker compose up

docker-clean:
	docker compose down --rmi local --volumes --remove-orphans

# 按变更范围运行测试：仅对 git diff 涉及的 Go 包执行 go test
# 用法: make test-affected [BASE=origin/main]
BASE ?= origin/main
test-affected:
	@echo "=== 按变更范围运行 Go 测试 (base: $(BASE)) ==="
	@pkgs=$$(git diff --name-only $(BASE) -- '*.go' | grep -v '_test.go' | xargs -r -n1 dirname | sort -u | sed 's|^agentprimordia/||' | sed 's|^|./|'); \
	if [ -z "$$pkgs" ]; then \
		echo "无 Go 源码变更，跳过测试"; \
	else \
		echo "受影响包:"; \
		echo "$$pkgs" | tr ' ' '\n'; \
		echo ""; \
		go test -v -count=1 $$pkgs; \
	fi

# Protocol Buffers generation
.PHONY: proto
proto:
	@mkdir -p internal/agent/a2a/proto/a2a/v1
	protoc \
		--go_out=. \
		--go_opt=module=agentprimordia \
		--go-grpc_out=. \
		--go-grpc_opt=module=agentprimordia \
		internal/agent/a2a/proto/a2a/v1/a2a.proto
