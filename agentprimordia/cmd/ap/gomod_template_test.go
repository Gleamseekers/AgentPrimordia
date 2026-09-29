// gomod_template_test.go — ap init/plugin 脚手架 go.mod 生成策略测试。
//
// v6.0 复测发现的问题（本组测试锁定修复行为）：
//  1. 模板硬编码 go 1.23 / 框架 v1.0.0，落后框架实际要求（go 1.26）与版本（v6.0.0）
//  2. 根模块的 replace github.com/Gleamseekers/AgentPrimordia/pgvector => ../pgvector 不具传递性——
//     生成的独立子项目 import pkg → internal/memory → pgvector 链路无法解析，
//     go mod tidy 直接失败（workspace 模式掩盖了该问题，独立构建必现）
//
// 生成策略：
//   - 从项目目录向上探测框架模块（go.mod 声明 module github.com/Gleamseekers/AgentPrimordia）：
//     找到 → emit 相对路径 replace，并连带 pgvector 的 require+replace
//   - 未找到（standalone）→ 不 emit replace，调用方提示依赖 GOPROXY 发布版
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeFakeFramework 构造最小假框架布局：<dir>/frame/go.mod(module github.com/Gleamseekers/AgentPrimordia) + <dir>/pgvector/go.mod
func makeFakeFramework(t *testing.T, dir string) string {
	t.Helper()
	frame := filepath.Join(dir, "frame")
	pgv := filepath.Join(dir, "pgvector")
	for _, d := range []string{frame, pgv} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	write := func(p, content string) {
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	write(filepath.Join(frame, "go.mod"), "module github.com/Gleamseekers/AgentPrimordia\n\ngo 1.26\n")
	write(filepath.Join(pgv, "go.mod"), "module github.com/Gleamseekers/AgentPrimordia/pgvector\n\ngo 1.26\n")
	return frame
}

// TestBuildGoMod_Standalone 独立目录：无 replace、版本对齐 v6.0.0、go 1.26。
func TestBuildGoMod_Standalone(t *testing.T) {
	tmpDir := t.TempDir()
	content, standalone := buildGoMod("my-agent", filepath.Join(tmpDir, "my-agent"))
	if !standalone {
		t.Error("空目录应判定为 standalone")
	}
	if !strings.Contains(content, "go 1.26") {
		t.Errorf("go.mod 应声明 go 1.26:\n%s", content)
	}
	if !strings.Contains(content, "github.com/Gleamseekers/AgentPrimordia/agentprimordia v0.0.0") {
		t.Errorf("go.mod 应包含 require github.com/Gleamseekers/AgentPrimordia/agentprimordia（SIV 合法占位版本）:\n%s", content)
	}
	if strings.Contains(content, "replace") {
		t.Errorf("standalone 场景不应包含 replace:\n%s", content)
	}
}

// TestBuildGoMod_InRepoWithPgvector 框架内：emit 双 replace（含 pgvector 依赖链闭合）。
func TestBuildGoMod_InRepoWithPgvector(t *testing.T) {
	tmpDir := t.TempDir()
	frame := makeFakeFramework(t, tmpDir)

	content, standalone := buildGoMod("demo", filepath.Join(frame, "demo"))
	if standalone {
		t.Error("框架内应判定为非 standalone")
	}
	for _, want := range []string{
		"go 1.26",
		"github.com/Gleamseekers/AgentPrimordia/agentprimordia v0.0.0",
		"github.com/Gleamseekers/AgentPrimordia/pgvector v0.0.0",
		"replace github.com/Gleamseekers/AgentPrimordia/agentprimordia => ..",
		"replace github.com/Gleamseekers/AgentPrimordia/pgvector => ../../pgvector",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("go.mod 缺少 %q:\n%s", want, content)
		}
	}
}

// TestRunInit_GoModInRepo 端到端：在假框架内 init，生成的 go.mod 可闭合 pgvector 链路。
func TestRunInit_GoModInRepo(t *testing.T) {
	tmpDir := t.TempDir()
	frame := makeFakeFramework(t, tmpDir)
	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()
	if err := os.Chdir(frame); err != nil {
		t.Fatalf("chdir 失败: %v", err)
	}

	if err := runInit([]string{"demo"}); err != nil {
		t.Fatalf("runInit 失败: %v", err)
	}

	mod, err := os.ReadFile(filepath.Join(frame, "demo", "go.mod"))
	if err != nil {
		t.Fatalf("读取 go.mod 失败: %v", err)
	}
	s := string(mod)
	if !strings.Contains(s, "replace github.com/Gleamseekers/AgentPrimordia/pgvector") {
		t.Errorf("框架内 init 的 go.mod 必须包含 pgvector replace（否则 tidy 断链）:\n%s", s)
	}
	if !strings.Contains(s, "go 1.26") {
		t.Errorf("go.mod 应声明 go 1.26:\n%s", s)
	}
}

// TestGenerate_GoModVersion Generate() 与 init 同一策略：go 1.26 + SIV 合法占位版本。
func TestGenerate_GoModVersion(t *testing.T) {
	files, err := Generate(GenerateOptions{Name: "demo", Template: "basic"})
	if err != nil {
		t.Fatalf("Generate 失败: %v", err)
	}
	mod := string(files["go.mod"])
	if !strings.Contains(mod, "go 1.26") || !strings.Contains(mod, "github.com/Gleamseekers/AgentPrimordia/agentprimordia v0.0.0") {
		t.Errorf("go.mod 版本未对齐（期望 go 1.26 + github.com/Gleamseekers/AgentPrimordia/agentprimordia v0.0.0 占位）:\n%s", mod)
	}
}
