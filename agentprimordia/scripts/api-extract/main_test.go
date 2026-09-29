package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// ===== v4.0-2 契约基线锁定：本地漂移门 =====
//
// 验收标准：漂移即失败。
// 该测试重新提取 pkg/ 公共 API，与提交的 sdk/typescript/api-contract.json 对比，
// 不一致即失败（提示运行 make api-extract 或 go run ./scripts/api-extract/）。
//
// CI 侧已有 bash 版漂移门（ci.yml contract-baseline job），此测试提供
// Windows 可运行的本地等价门，开发者提交前即可捕获契约漂移。

// TestAPIContractNoDrift 校验提交的契约基线与实际公共 API 一致。
func TestAPIContractNoDrift(t *testing.T) {
	// 从包目录出发定位仓库根
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd 失败: %v", err)
	}
	// cwd = <repo>/agentprimordia/scripts/api-extract
	repoRoot := filepath.Join(cwd, "..", "..", "..")
	apDir := filepath.Join(repoRoot, "agentprimordia")
	baselineFile := filepath.Join(repoRoot, "sdk", "typescript", "api-contract.json")

	// 切到 agentprimordia 目录，使提取出的 File 路径与基线一致（相对 pkg/...）
	if err := os.Chdir(apDir); err != nil {
		t.Fatalf("Chdir 失败: %v", err)
	}

	contract, err := extractAPI("pkg")
	if err != nil {
		t.Fatalf("提取 API 失败: %v", err)
	}
	// 与 CLI 一致：填充版本号（从 pkg/agent.go const Version 提取）
	version, err := loadVersion("pkg")
	if err != nil {
		t.Fatalf("读取版本失败: %v", err)
	}
	contract.Version = version
	contract.GeneratedAt = ""

	data, err := json.MarshalIndent(contract, "", "  ")
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	actual := append(data, '\n')

	baseline, err := os.ReadFile(baselineFile)
	if err != nil {
		t.Fatalf("读取契约基线失败（是否已运行 make api-extract?）: %v", err)
	}

	if string(actual) != string(baseline) {
		t.Errorf("API 契约漂移！\n"+
			"基线文件: %s\n"+
			"修复: 在 github.com/Gleamseekers/AgentPrimordia/ 下运行 `make api-extract` 并提交更新。", baselineFile)
	}
}
