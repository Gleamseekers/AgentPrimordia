// migrate.go — ap migrate：模块路径机械重写工具（v7.8「8.0 彩排」交付物）。
//
// 【定位】为 v8.0 的 /v7 → /v8 模块路径迁移预置。工具只做文本重写，
// 不依赖 /v8 是否存在——迁移窗口打开前它就能在 --check 模式下当 CI 门用。
//
// 【改什么】
//   - .go 文件：import 声明里的模块路径前缀（用 go/parser 定位，绝不碰
//     字符串字面量与注释里同形文本，避免误改）；
//   - go.mod：module 行、require（块与单行）行、replace 左侧的模块路径
//     （右侧本地路径不动，retract/toolchain 等版本号不动）。
//
// 【明确不改】非 .go 文件（.md/.txt/.json/.yaml/.sh/…）、.git / vendor /
// node_modules 目录、以及 go.work（use 的是目录不是模块路径）。
//
// 【用法】
//
//	ap migrate [DIR] [--from PREFIX] [--to PREFIX] [--check]
//
// 默认把 github.com/Gleamseekers/AgentPrimordia/v7 前缀改写为
// github.com/Gleamseekers/AgentPrimordia/v8（即 /v7 → /v8 主版本迁移）。
// --check 只报告不落盘，并在存在待迁移项时以非零退出（供 CI）。
package main

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// 默认迁移前缀：本仓库 v7 → v8 模块路径（/v7 → /v8 的主版本迁移）。
const (
	defaultMigrateFrom = "github.com/Gleamseekers/AgentPrimordia/v7"
	defaultMigrateTo   = "github.com/Gleamseekers/AgentPrimordia/v8"
)

// ErrMigrationPending --check 模式下发现待迁移项时的哨兵错误。
// CI 语义：非零退出码即"迁移未完成"，与 gofmt -l 同类。
var ErrMigrationPending = errors.New("存在待迁移的模块路径引用（ap migrate --check）")

// migrateSkipDirs 不扫描的目录名（版本库元数据 / 依赖副本 / 前端依赖）。
var migrateSkipDirs = map[string]bool{
	".git":         true,
	"vendor":       true,
	"node_modules": true,
	"testdata":     false, // 占位：testdata 仍要扫（示例模块树常在 testdata 下）
}

// migrateOptions 一次迁移的输入。
type migrateOptions struct {
	dir   string // 待扫描目录树（空 = 当前目录）
	from  string // 模块路径前缀（原）
	to    string // 模块路径前缀（目标）
	check bool   // 只报告不落盘
}

// migrateFileChange 单个文件的改写统计。
type migrateFileChange struct {
	Path    string // 相对扫描根目录的路径（报告用）
	Changes int    // 该文件的改写处数
}

// migrateResult 一次迁移的结果。
type migrateResult struct {
	Files      []migrateFileChange // 发生改写的文件（按路径排序）
	GoFiles    int                 // 扫描到的 .go 文件数
	GoModFiles int                 // 扫描到的 go.mod 文件数
	Skipped    []string            // 无法解析而跳过的 .go 文件（显式登记，不静默）
}

// changedFiles 发生改写的文件数。
func (r migrateResult) changedFiles() int { return len(r.Files) }

// totalChanges 改写处数合计。
func (r migrateResult) totalChanges() int {
	n := 0
	for _, f := range r.Files {
		n += f.Changes
	}
	return n
}

// ===== 入口 =====

// runMigrate 解析命令行参数并执行迁移（CLI 分发入口）。
func runMigrate(args []string) error {
	opts := migrateOptions{from: defaultMigrateFrom, to: defaultMigrateTo}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--help", "-h", "help":
			printMigrateHelp()
			return nil
		case "--check":
			opts.check = true
		case "--from":
			i++
			if i >= len(args) {
				return fmt.Errorf("--from 需要一个路径前缀参数")
			}
			opts.from = args[i]
		case "--to":
			i++
			if i >= len(args) {
				return fmt.Errorf("--to 需要一个路径前缀参数")
			}
			opts.to = args[i]
		case "-":
			return fmt.Errorf("无效参数 %q", args[i])
		default:
			if strings.HasPrefix(args[i], "-") {
				return fmt.Errorf("未知参数 %q，运行 %s 查看用法", args[i], bold("ap migrate --help"))
			}
			if opts.dir != "" {
				return fmt.Errorf("只能指定一个目录参数（已指定 %q，又出现 %q）", opts.dir, args[i])
			}
			opts.dir = args[i]
		}
	}

	res, err := migrateTree(opts)
	printMigrateReport(opts, res)
	if err != nil {
		return err
	}
	if opts.check && res.totalChanges() > 0 {
		return ErrMigrationPending
	}
	return nil
}

// printMigrateHelp 打印用法（与 cmd/ap 其他子命令的 help 口径一致）。
func printMigrateHelp() {
	fmt.Print(`ap migrate — rewrite module import paths for the next major version

Usage:
  ap migrate [DIR] [--from PREFIX] [--to PREFIX] [--check]

Arguments:
  DIR                 directory tree to rewrite (default: current directory)

Options:
  --from PREFIX       source import path prefix (default: ` + defaultMigrateFrom + `)
  --to PREFIX         target import path prefix (default: ` + defaultMigrateTo + `)
  --check             report only, do not write; exit non-zero if anything is pending (for CI)
  --help, -h          show this help

What gets rewritten:
  - .go files: the module path prefix inside import declarations
    (located via go/parser, so strings and comments are never touched)
  - go.mod: the module line, require entries (block and single-line),
    and the left side of replace directives

What is never touched:
  - non-.go files (.md / .txt / .json / .yaml / .sh / ...)
  - .git / vendor / node_modules directories
  - go.work (its "use" lines name directories, not module paths)

The rewrite is idempotent: a second run makes no changes.

Examples:
  ap migrate --check                 # CI gate: report only, non-zero exit if pending
  ap migrate                         # rewrite ./ from /v7 to /v8
  ap migrate ../my-app               # rewrite a downstream project tree
  ap migrate --from acme.com/lib/v1 --to acme.com/lib/v2
`)
}

// ===== 核心：目录树迁移 =====

// migrateTree 扫描 dir 目录树并执行（或仅报告）迁移。
func migrateTree(opts migrateOptions) (migrateResult, error) {
	if opts.from == "" || opts.to == "" {
		return migrateResult{}, fmt.Errorf("--from/--to 不能为空")
	}
	if opts.from == opts.to {
		return migrateResult{}, fmt.Errorf("--from 与 --to 相同（%q），无可迁移", opts.from)
	}
	if opts.dir == "" {
		opts.dir = "."
	}
	info, err := os.Stat(opts.dir)
	if err != nil {
		return migrateResult{}, fmt.Errorf("读取目录 %s 失败: %w", opts.dir, err)
	}
	if !info.IsDir() {
		return migrateResult{}, fmt.Errorf("%s 不是目录", opts.dir)
	}

	var res migrateResult
	walkErr := filepath.WalkDir(opts.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != opts.dir && migrateSkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		switch {
		case strings.HasSuffix(d.Name(), ".go"):
			res.GoFiles++
			n, skip, err := migrateGoFile(path, opts.from, opts.to, opts.check)
			if err != nil {
				return fmt.Errorf("改写 %s 失败: %w", path, err)
			}
			if skip {
				res.Skipped = append(res.Skipped, path)
				return nil
			}
			if n > 0 {
				res.Files = append(res.Files, migrateFileChange{Path: path, Changes: n})
			}
		case d.Name() == "go.mod":
			res.GoModFiles++
			n, err := migrateGoModFile(path, opts.from, opts.to, opts.check)
			if err != nil {
				return fmt.Errorf("改写 %s 失败: %w", path, err)
			}
			if n > 0 {
				res.Files = append(res.Files, migrateFileChange{Path: path, Changes: n})
			}
		}
		return nil
	})
	if walkErr != nil {
		return migrateResult{}, walkErr
	}
	sort.Slice(res.Files, func(i, j int) bool { return res.Files[i].Path < res.Files[j].Path })
	return res, nil
}

// migrateGoFile 改写单个 .go 文件 import 声明中的模块路径前缀。
// 返回 (改写处数, 是否因无法解析而跳过, error)。
func migrateGoFile(path, from, to string, check bool) (int, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, false, err
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, raw, parser.ParseComments)
	if err != nil {
		// 无法解析：显式登记而不是静默跳过（CI 需要看见）。
		return 0, true, nil
	}

	var repls []migrateReplacement
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		for _, spec := range gd.Specs {
			is, ok := spec.(*ast.ImportSpec)
			if !ok || is.Path == nil || is.Path.Kind != token.STRING {
				continue
			}
			imp, err := strconv.Unquote(is.Path.Value)
			if err != nil {
				continue
			}
			next, ok := rewritePrefix(imp, from, to)
			if !ok {
				continue
			}
			repls = append(repls, migrateReplacement{
				start: fset.Position(is.Path.Pos()).Offset,
				end:   fset.Position(is.Path.End()).Offset,
				text:  strconv.Quote(next),
			})
		}
	}
	if len(repls) == 0 {
		return 0, false, nil
	}
	if check {
		return len(repls), false, nil
	}
	if err := os.WriteFile(path, applyReplacements(raw, repls), 0o644); err != nil {
		return 0, false, err
	}
	return len(repls), false, nil
}

// migrateGoModFile 改写 go.mod 的 module / require / replace 行。
func migrateGoModFile(path, from, to string, check bool) (int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	lines := strings.Split(string(raw), "\n")
	changes := 0
	inRequireBlock := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "require (" || trimmed == "require(" {
			inRequireBlock = true
			continue
		}
		if inRequireBlock && trimmed == ")" {
			inRequireBlock = false
			continue
		}
		newLine, n := rewriteGoModLine(line, from, to, inRequireBlock)
		if n == 0 {
			continue
		}
		changes += n
		if !check {
			lines[i] = newLine
		}
	}
	if changes == 0 || check {
		return changes, nil
	}
	return changes, os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
}

// rewriteGoModLine 改写 go.mod 单行中的模块路径字段（保留缩进、注释与其余内容）。
func rewriteGoModLine(line, from, to string, inRequireBlock bool) (string, int) {
	code, _ := splitGoModComment(line)
	fields := strings.Fields(code)
	if len(fields) == 0 {
		return line, 0
	}

	// 目标字段下标：module/require/replace 取第 2 段；require 块内取第 1 段。
	idx := -1
	switch {
	case fields[0] == "module" && len(fields) >= 2:
		idx = 1
	case fields[0] == "require" && len(fields) >= 2:
		idx = 1
	case fields[0] == "replace" && len(fields) >= 2:
		idx = 1
	case inRequireBlock:
		idx = 0
	}
	if idx < 0 || idx >= len(fields) {
		return line, 0
	}
	next, ok := rewritePrefix(fields[idx], from, to)
	if !ok {
		return line, 0
	}
	// 就地替换该字段文本（字段本身足够独特，首现即目标），保留原排版。
	return strings.Replace(line, fields[idx], next, 1), 1
}

// splitGoModComment 把 go.mod 行拆成代码与注释两部分。
func splitGoModComment(line string) (code, comment string) {
	if i := strings.Index(line, "//"); i >= 0 {
		return line[:i], line[i:]
	}
	return line, ""
}

// rewritePrefix 把 import 路径前缀 from 换成 to；不命中返回 ok=false。
// 只在段边界上匹配：from="x/v7" 命中 "x/v7" 与 "x/v7/pkg"，不命中 "x/v70"。
func rewritePrefix(path, from, to string) (string, bool) {
	if path == from {
		return to, true
	}
	if !strings.HasPrefix(path, from) {
		return "", false
	}
	rest := path[len(from):]
	if !strings.HasPrefix(rest, "/") {
		return "", false // 段边界保护：x/v7 不应命中 x/v70
	}
	return to + rest, true
}

// migrateReplacement 一处字节区间替换（.go 文件 import 路径）。
type migrateReplacement struct {
	start int
	end   int
	text  string
}

// applyReplacements 按偏移倒序应用文本替换（保证前面的偏移不失效）。
func applyReplacements(raw []byte, repls []migrateReplacement) []byte {
	sort.Slice(repls, func(i, j int) bool { return repls[i].start > repls[j].start })
	out := raw
	for _, r := range repls {
		if r.start < 0 || r.end > len(out) || r.start > r.end {
			continue
		}
		out = append(out[:r.start], append([]byte(r.text), out[r.end:]...)...)
	}
	return out
}

// ===== 报告 =====

// printMigrateReport 输出变更文件清单与统计（与 cmd/ap 其他命令的输出口径一致）。
func printMigrateReport(opts migrateOptions, res migrateResult) {
	dir := opts.dir
	if dir == "" {
		dir = "."
	}
	fmt.Printf("%s\n\n", bold("AgentPrimordia 模块路径迁移"))
	infof("前缀: %s → %s", opts.from, opts.to)
	infof("目录: %s（%d 个 .go / %d 个 go.mod）", dir, res.GoFiles, res.GoModFiles)
	if len(res.Skipped) > 0 {
		warnf("%d 个 .go 文件无法解析、未处理：%s", len(res.Skipped), strings.Join(res.Skipped, ", "))
	}
	fmt.Println()

	if res.totalChanges() == 0 {
		successf("无需迁移：未发现 %s 前缀引用", opts.from)
		return
	}
	for _, f := range res.Files {
		infof("%s  %d 处", f.Path, f.Changes)
	}
	fmt.Println()
	if opts.check {
		warnf("共 %d 个文件 / %d 处待迁移（--check 模式：未落盘）", res.changedFiles(), res.totalChanges())
		return
	}
	successf("已改写 %d 个文件 / %d 处；请运行 go mod tidy 与 go build ./... 验证",
		res.changedFiles(), res.totalChanges())
}
