// migrate_test.go — ap migrate 工具单测（TDD：先红后绿）。
//
// 覆盖任务验收要求的六种情形 + 边界：
//  1. 基本重写：import 行中的模块路径前缀 /v7 → /v8；
//  2. --check 只报告不改（供 CI），且有待迁移项时返回 ErrMigrationPending；
//  3. 幂等性：第二次运行 0 变更；
//  4. --from/--to 自定义前缀；
//  5. 非 .go 文件不动、import 之外的文本（字符串/注释）不动；
//  6. go.mod 的 module / require 行同步改写；
//     外加：分组 import 与单行 import、无关模块 import 不受影响、.git/vendor 跳过、
//     空树不报错、--help 不落盘。
package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 测试用模块路径前缀（与生产默认值同构，但故意用不同的 org 名以证明工具不硬编码）。
const (
	testFrom = "example.com/org/v7"
	testTo   = "example.com/org/v8"
)

// writeTree 在 dir 下按 rel→content 写文件（自动建目录）。
func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
}

// readTree 读回 rel 对应文件内容。
func readTree(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

// goFileWithImports 生成一个含分组 import 的 .go 文件（含无关模块 import 与
// 一段含模块路径文本的注释/字符串，用于验证"只改 import 行"）。
func goFileWithImports() string {
	return `package sample

import (
	"fmt"

	"example.com/org/v7/pkg/agent"
	sub "example.com/org/v7/pkg/pool"
	"github.com/other/dep/v2"
)

// 注释里提到 example.com/org/v7/pkg/agent 不应被改写。
const docNote = "example.com/org/v7/pkg/agent 只应出现在 import 行"

func main() {
	fmt.Println(agent.X, sub.Y, docNote)
}
`
}

// TestMigrateRewritesGoImports 情形 1：基本重写（分组 import）。
func TestMigrateRewritesGoImports(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"main.go": goFileWithImports()})

	res, err := migrateTree(migrateOptions{dir: dir, from: testFrom, to: testTo})
	if err != nil {
		t.Fatalf("migrateTree 失败: %v", err)
	}
	if res.changedFiles() != 1 {
		t.Errorf("期望改写 1 个文件，实际 %d（%v）", res.changedFiles(), res.Files)
	}
	if res.totalChanges() != 2 {
		t.Errorf("期望 2 处 import 改写，实际 %d", res.totalChanges())
	}

	got := readTree(t, dir, "main.go")
	for _, want := range []string{
		`"example.com/org/v8/pkg/agent"`,
		`sub "example.com/org/v8/pkg/pool"`,
		`"github.com/other/dep/v2"`, // 无关模块 import 不动
		`const docNote = "example.com/org/v7/pkg/agent 只应出现在 import 行"`,
		`// 注释里提到 example.com/org/v7/pkg/agent 不应被改写。`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("改写后缺少 %q，实际内容:\n%s", want, got)
		}
	}
	if strings.Contains(got, "example.com/org/v7/pkg/agent\"") && !strings.Contains(got, "docNote") {
		t.Error("import 行的 /v7 路径未被改写")
	}
}

// TestMigrateSingleLineImport 单行 import 形态也要改写。
func TestMigrateSingleLineImport(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a.go": `package a

import "example.com/org/v7/pkg/tools"

var _ = tools.T
`})

	res, err := migrateTree(migrateOptions{dir: dir, from: testFrom, to: testTo})
	if err != nil {
		t.Fatalf("migrateTree 失败: %v", err)
	}
	if res.totalChanges() != 1 {
		t.Fatalf("期望 1 处改写，实际 %d", res.totalChanges())
	}
	got := readTree(t, dir, "a.go")
	if !strings.Contains(got, `import "example.com/org/v8/pkg/tools"`) {
		t.Errorf("单行 import 未改写:\n%s", got)
	}
}

// TestMigrateCheckDoesNotWrite 情形 2：--check 只报告不改，且返回 ErrMigrationPending。
func TestMigrateCheckDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	original := goFileWithImports()
	writeTree(t, dir, map[string]string{"main.go": original})

	res, err := migrateTree(migrateOptions{dir: dir, from: testFrom, to: testTo, check: true})
	if err != nil {
		t.Errorf("--check 模式不应返回错误（统计口径），实际 %v", err)
	}
	if res.totalChanges() != 2 {
		t.Errorf("--check 仍应报告待迁移数量，实际 %d", res.totalChanges())
	}
	if got := readTree(t, dir, "main.go"); got != original {
		t.Errorf("--check 模式不得落盘，但文件被改写了:\n%s", got)
	}

	// CI 语义：--check 且有待迁移项时 runMigrate 必须返回 ErrMigrationPending（非零退出）。
	if err := runMigrate([]string{dir, "--from", testFrom, "--to", testTo, "--check"}); !errors.Is(err, ErrMigrationPending) {
		t.Errorf("--check 模式有待迁移项时应返回 ErrMigrationPending，实际 %v", err)
	}
	if got := readTree(t, dir, "main.go"); got != original {
		t.Errorf("--check 模式不得落盘，但文件被改写了:\n%s", got)
	}
}

// TestMigrateCheckCleanTreeNoError --check 在无需迁移时不应报错（CI 绿）。
func TestMigrateCheckCleanTreeNoError(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"main.go": `package main

import "example.com/org/v8/pkg/agent"

func main() { _ = agent.X }
`,
		"go.mod": "module example.com/app\n\ngo 1.26\n\nrequire example.com/org/v8 v8.0.0\n",
	})

	res, err := migrateTree(migrateOptions{dir: dir, from: testFrom, to: testTo, check: true})
	if err != nil {
		t.Errorf("无需迁移时 --check 不应报错: %v", err)
	}
	if res.totalChanges() != 0 || res.changedFiles() != 0 {
		t.Errorf("无需迁移时应 0 变更，实际 %d 处 / %d 文件", res.totalChanges(), res.changedFiles())
	}
}

// TestMigrateIdempotent 情形 3：幂等性——第二次运行 0 变更。
func TestMigrateIdempotent(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"main.go":  goFileWithImports(),
		"go.mod":   "module example.com/app\n\ngo 1.26\n\nrequire (\n\texample.com/org/v7 v7.1.0\n\tgithub.com/other/dep/v2 v2.0.0 // indirect\n)\n",
		"docs.md":  "import example.com/org/v7/pkg/agent\n",
		"data.txt": "example.com/org/v7\n",
	})

	first, err := migrateTree(migrateOptions{dir: dir, from: testFrom, to: testTo})
	if err != nil {
		t.Fatalf("第一次迁移失败: %v", err)
	}
	if first.totalChanges() == 0 {
		t.Fatal("第一次迁移应有变更")
	}
	snapshot := map[string]string{}
	for _, rel := range []string{"main.go", "go.mod", "docs.md", "data.txt"} {
		snapshot[rel] = readTree(t, dir, rel)
	}

	second, err := migrateTree(migrateOptions{dir: dir, from: testFrom, to: testTo})
	if err != nil {
		t.Fatalf("第二次迁移失败: %v", err)
	}
	if second.totalChanges() != 0 || second.changedFiles() != 0 {
		t.Errorf("第二次运行应 0 变更（幂等），实际 %d 处 / %d 文件", second.totalChanges(), second.changedFiles())
	}
	for rel, want := range snapshot {
		if got := readTree(t, dir, rel); got != want {
			t.Errorf("第二次运行改动了 %s（幂等性破坏）", rel)
		}
	}
}

// TestMigrateCustomFromTo 情形 4：--from/--to 自定义前缀。
func TestMigrateCustomFromTo(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"main.go": `package main

import (
	"acme.com/lib/v1/a"
	"acme.com/lib/v1/b"
	"example.com/org/v7/pkg/agent"
)

var _, _ = a.A, b.B
`})

	res, err := migrateTree(migrateOptions{dir: dir, from: "acme.com/lib/v1", to: "acme.com/lib/v2"})
	if err != nil {
		t.Fatalf("migrateTree 失败: %v", err)
	}
	if res.totalChanges() != 2 {
		t.Fatalf("期望 2 处改写，实际 %d", res.totalChanges())
	}
	got := readTree(t, dir, "main.go")
	if !strings.Contains(got, `"acme.com/lib/v2/a"`) || !strings.Contains(got, `"acme.com/lib/v2/b"`) {
		t.Errorf("自定义前缀未生效:\n%s", got)
	}
	// 不在自定义前缀内的 import 必须保持原样。
	if !strings.Contains(got, `"example.com/org/v7/pkg/agent"`) {
		t.Errorf("自定义前缀外的 import 被误改:\n%s", got)
	}
}

// TestMigrateIgnoresNonGoFiles 情形 5：非 .go 文件不动（即使内容含模块路径）。
func TestMigrateIgnoresNonGoFiles(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"main.go":       `package main` + "\n\nimport \"example.com/org/v7/pkg/agent\"\n\nvar _ = agent.X\n",
		"README.md":     "# 文档\n\nimport \"example.com/org/v7/pkg/agent\"\n",
		"notes.txt":     "example.com/org/v7/pkg/agent\n",
		"config.json":   "{\"path\": \"example.com/org/v7/pkg/agent\"}\n",
		"script.sh":     "go get example.com/org/v7/pkg/agent\n",
		"gen.go.bak":    "package x\nimport \"example.com/org/v7/pkg/agent\"\n",
		"sub/data.yaml": "path: example.com/org/v7\n",
	}
	writeTree(t, dir, files)
	before := map[string]string{}
	for rel := range files {
		if rel == "main.go" {
			continue
		}
		before[rel] = readTree(t, dir, rel)
	}

	if _, err := migrateTree(migrateOptions{dir: dir, from: testFrom, to: testTo}); err != nil {
		t.Fatalf("migrateTree 失败: %v", err)
	}
	for rel, want := range before {
		if got := readTree(t, dir, rel); got != want {
			t.Errorf("非 .go 文件 %s 被改写了:\nwant: %q\ngot:  %q", rel, want, got)
		}
	}
	if got := readTree(t, dir, "main.go"); !strings.Contains(got, "example.com/org/v8/pkg/agent") {
		t.Errorf(".go 文件应被改写:\n%s", got)
	}
}

// TestMigrateDoesNotRewriteNonImportText import 之外的 /v7 文本（字符串、注释）不动。
func TestMigrateDoesNotRewriteNonImportText(t *testing.T) {
	dir := t.TempDir()
	src := `package main

import "example.com/org/v7/pkg/agent"

// 注释：example.com/org/v7 是旧路径
const note = "example.com/org/v7/pkg/agent"

func main() {
	println(agent.X, note)
}
`
	writeTree(t, dir, map[string]string{"main.go": src})
	if _, err := migrateTree(migrateOptions{dir: dir, from: testFrom, to: testTo}); err != nil {
		t.Fatalf("migrateTree 失败: %v", err)
	}
	got := readTree(t, dir, "main.go")
	if !strings.Contains(got, `// 注释：example.com/org/v7 是旧路径`) {
		t.Errorf("注释被误改:\n%s", got)
	}
	if !strings.Contains(got, `const note = "example.com/org/v7/pkg/agent"`) {
		t.Errorf("字符串字面量被误改:\n%s", got)
	}
	if !strings.Contains(got, `import "example.com/org/v8/pkg/agent"`) {
		t.Errorf("import 行未改写:\n%s", got)
	}
}

// TestMigrateGoModModuleAndRequire 情形 6：go.mod 的 module / require 行同步改写。
func TestMigrateGoModModuleAndRequire(t *testing.T) {
	dir := t.TempDir()
	mod := `module example.com/app

go 1.26.9

require (
	example.com/org/v7 v7.1.0
	github.com/other/dep/v2 v2.0.0 // indirect
)

require example.com/org/v7/extra v7.0.0

replace example.com/org/v7 => ../v7

retract v7.0.0
`
	writeTree(t, dir, map[string]string{"go.mod": mod})

	res, err := migrateTree(migrateOptions{dir: dir, from: testFrom, to: testTo})
	if err != nil {
		t.Fatalf("migrateTree 失败: %v", err)
	}
	if res.totalChanges() != 3 {
		t.Errorf("期望 3 处改写（require 块 + 单行 require + replace），实际 %d", res.totalChanges())
	}
	got := readTree(t, dir, "go.mod")
	for _, want := range []string{
		"module example.com/app\n",                   // 自身模块名不含该前缀，不动
		"example.com/org/v8 v7.1.0",                  // require 块内（只改路径，版本号不动）
		"github.com/other/dep/v2 v2.0.0 // indirect", // 无关 require 不动（含注释）
		"require example.com/org/v8/extra v7.0.0",    // 单行 require
		"replace example.com/org/v8 => ../v7",        // replace 左侧改写、右侧本地路径不动
		"retract v7.0.0",                             // retract 版本号不动
	} {
		if !strings.Contains(got, want) {
			t.Errorf("go.mod 改写后缺少 %q，实际:\n%s", want, got)
		}
	}
	if strings.Contains(got, "example.com/org/v7 ") {
		t.Errorf("go.mod 仍残留旧前缀:\n%s", got)
	}
}

// TestMigrateGoModModuleLineOwnModule go.mod 的 module 行本身命中前缀时也要改写。
func TestMigrateGoModModuleLineOwnModule(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"go.mod": "module example.com/org/v7\n\ngo 1.26\n",
	})
	res, err := migrateTree(migrateOptions{dir: dir, from: testFrom, to: testTo})
	if err != nil {
		t.Fatalf("migrateTree 失败: %v", err)
	}
	if res.totalChanges() != 1 {
		t.Fatalf("期望 1 处改写，实际 %d", res.totalChanges())
	}
	if got := readTree(t, dir, "go.mod"); !strings.Contains(got, "module example.com/org/v8") {
		t.Errorf("module 行未改写:\n%s", got)
	}
}

// TestMigrateSkipsVendorAndGit .git / vendor / node_modules 目录不扫。
func TestMigrateSkipsVendorAndGit(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"main.go":                        "package main\n",
		".git/hooks/pre-commit.go":       "package x\n\nimport \"example.com/org/v7/pkg/a\"\n",
		"vendor/example.com/org/v7/x.go": "package x\n\nimport \"example.com/org/v7/pkg/b\"\n",
		"node_modules/dep/index.go":      "package x\n\nimport \"example.com/org/v7/pkg/c\"\n",
	})
	res, err := migrateTree(migrateOptions{dir: dir, from: testFrom, to: testTo})
	if err != nil {
		t.Fatalf("migrateTree 失败: %v", err)
	}
	if res.totalChanges() != 0 {
		t.Errorf("跳过目录内的文件不应被改写，实际 %d 处", res.totalChanges())
	}
	for _, rel := range []string{".git/hooks/pre-commit.go", "vendor/example.com/org/v7/x.go", "node_modules/dep/index.go"} {
		if got := readTree(t, dir, rel); !strings.Contains(got, "example.com/org/v7/pkg/") {
			t.Errorf("%s 不应被改写", rel)
		}
	}
}

// TestMigrateEmptyTree 空树不报错、0 变更。
func TestMigrateEmptyTree(t *testing.T) {
	dir := t.TempDir()
	res, err := migrateTree(migrateOptions{dir: dir, from: testFrom, to: testTo})
	if err != nil {
		t.Fatalf("空树不应报错: %v", err)
	}
	if res.totalChanges() != 0 || res.changedFiles() != 0 {
		t.Errorf("空树应 0 变更，实际 %d 处 / %d 文件", res.totalChanges(), res.changedFiles())
	}
}

// TestMigrateUnparseableGoFileReported 无法解析的 .go 文件要显式登记（不静默跳过）。
func TestMigrateUnparseableGoFileReported(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"broken.go": "package broken\n\nthis is not go\n",
	})
	res, err := migrateTree(migrateOptions{dir: dir, from: testFrom, to: testTo})
	if err != nil {
		t.Fatalf("migrateTree 失败: %v", err)
	}
	if len(res.Skipped) != 1 {
		t.Errorf("期望登记 1 个跳过的文件，实际 %v", res.Skipped)
	}
}

// TestMigrateRejectsBadPrefix from/to 为空或相等必须显式报错（防止误把整树文本改坏）。
func TestMigrateRejectsBadPrefix(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name string
		opts migrateOptions
	}{
		{"空 from", migrateOptions{dir: dir, from: "", to: testTo}},
		{"空 to", migrateOptions{dir: dir, from: testFrom, to: ""}},
		{"from==to", migrateOptions{dir: dir, from: testFrom, to: testFrom}},
		{"目录不存在", migrateOptions{dir: filepath.Join(dir, "nope"), from: testFrom, to: testTo}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := migrateTree(tc.opts); err == nil {
				t.Errorf("%s：期望返回错误", tc.name)
			}
		})
	}
}

// TestRunMigrateParsesFlags 参数解析：位置参数目录 + --from/--to/--check。
func TestRunMigrateParsesFlags(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"main.go": "package main\n\nimport \"example.com/org/v7/pkg/agent\"\n\nvar _ = agent.X\n",
	})

	// --check + 自定义前缀：不应落盘，且返回 ErrMigrationPending。
	err := runMigrate([]string{dir, "--from", "example.com/org/v7", "--to", "example.com/org/v8", "--check"})
	if !errors.Is(err, ErrMigrationPending) {
		t.Errorf("期望 ErrMigrationPending，实际 %v", err)
	}
	if got := readTree(t, dir, "main.go"); !strings.Contains(got, "example.com/org/v7/pkg/agent") {
		t.Errorf("--check 不得落盘:\n%s", got)
	}

	// 去掉 --check：落盘。
	if err := runMigrate([]string{dir, "--from", "example.com/org/v7", "--to", "example.com/org/v8"}); err != nil {
		t.Fatalf("runMigrate 失败: %v", err)
	}
	if got := readTree(t, dir, "main.go"); !strings.Contains(got, "example.com/org/v8/pkg/agent") {
		t.Errorf("未落盘:\n%s", got)
	}

	// --help 不报错、不落盘。
	if err := runMigrate([]string{"--help"}); err != nil {
		t.Errorf("--help 不应报错: %v", err)
	}
}

// TestRunMigrateMissingFlagValue flag 缺值要报错（而不是把下一个参数当值）。
func TestRunMigrateMissingFlagValue(t *testing.T) {
	if err := runMigrate([]string{".", "--from"}); err == nil {
		t.Error("--from 缺值应报错")
	}
	if err := runMigrate([]string{".", "--unknown-flag"}); err == nil {
		t.Error("未知 flag 应报错")
	}
}

// TestDefaultMigratePrefixes 默认前缀必须是本仓库的 /v7 → /v8 模块路径。
func TestDefaultMigratePrefixes(t *testing.T) {
	if defaultMigrateFrom != "github.com/Gleamseekers/AgentPrimordia/v7" {
		t.Errorf("默认 from 应为 v7 模块路径，实际 %q", defaultMigrateFrom)
	}
	if defaultMigrateTo != "github.com/Gleamseekers/AgentPrimordia/v8" {
		t.Errorf("默认 to 应为 v8 模块路径，实际 %q", defaultMigrateTo)
	}
	if !strings.HasSuffix(defaultMigrateFrom, "/v7") || !strings.HasSuffix(defaultMigrateTo, "/v8") {
		t.Error("默认前缀应体现 /v7 → /v8 的主版本迁移")
	}
}
