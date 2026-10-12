// api_surface_test.go — v7.6 API 冻结机制（黄金快照）的机械化执行。
//
// 【纪律（与 pkg/api_surface.golden 头部一致）】
//  1. 本测试用 go/parser + go/ast 解析 pkg/*.go（不调用 go doc / 不 exec 外部命令），
//     收集包 ap 的全部导出符号（导出的 func/type/var/const，含签名摘要），
//     与 pkg/api_surface.golden 逐行比对；
//  2. 任何导出符号的新增 / 删除 / 签名变更都会使本测试变红（CI 语义：新增导出即红）；
//  3. 更新黄金文件 = 一次维护者批准的 API 契约变更，必须在提交信息中显式说明
//     （改了什么、为什么、影响面），或在 APPROVALS 记录中登记批准人；
//  4. 重新生成方式（两种等价）：
//     AP_API_SURFACE_UPDATE=1 go test ./pkg/ -run TestAPISurfaceGoldenMatches
//     go test ./pkg/ -run TestAPISurfaceGoldenMatches -api-surface-update
//  5. 禁止手改黄金文件后直接提交——必须走重新生成，保证黄金文件与收集器口径一致。
package ap

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// apiSurfaceGoldenPath 黄金快照文件路径（相对测试工作目录 = pkg/）。
const apiSurfaceGoldenPath = "api_surface.golden"

// apiSurfaceUpdateEnv 重新生成黄金文件的环境变量（置 "1" 生效）。
const apiSurfaceUpdateEnv = "AP_API_SURFACE_UPDATE"

// ===== 测试 =====

// TestAPISurfaceGoldenMatches 主测试：导出表面必须与黄金文件逐行一致。
func TestAPISurfaceGoldenMatches(t *testing.T) {
	entries, err := collectAPISurfaceFrom(".")
	if err != nil {
		t.Fatalf("收集导出表面失败: %v", err)
	}
	got := make([]string, 0, len(entries))
	for _, e := range entries {
		got = append(got, e.line)
	}

	if apiSurfaceUpdateRequested() {
		if err := writeAPISurfaceGolden(apiSurfaceGoldenPath, got); err != nil {
			t.Fatalf("重新生成黄金文件失败: %v", err)
		}
		t.Logf("已按更新开关重新生成 %s（%d 个导出符号）", apiSurfaceGoldenPath, len(got))
		return
	}

	raw, err := os.ReadFile(apiSurfaceGoldenPath)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("读取黄金文件 %s 失败: %v", apiSurfaceGoldenPath, err)
		}
		// 首次运行：生成黄金文件并失败，要求人工审阅后提交。
		if err := writeAPISurfaceGolden(apiSurfaceGoldenPath, got); err != nil {
			t.Fatalf("生成黄金文件失败: %v", err)
		}
		t.Fatalf("黄金文件 %s 不存在：已按当前导出表面生成 %d 个符号。"+
			"请人工审阅后提交；此后任何导出表面变更都会使本测试变红。"+
			"（维护者批准的契约变更可用 %s=1 重新生成）",
			apiSurfaceGoldenPath, len(got), apiSurfaceUpdateEnv)
	}

	want := parseAPISurfaceGolden(string(raw))
	diff := diffAPISurface(want, entries)
	if diff.empty() {
		t.Logf("公共 API 表面与黄金文件一致（%d 个导出符号）", len(got))
		return
	}
	t.Errorf("公共 API 表面与 %s 不一致（CI 语义：新增/删除/变更导出即红）\n\n%s\n%s",
		apiSurfaceGoldenPath, diff.report(), apiSurfaceRemediationHint())
}

// TestCollectAPISurfaceBasics 收集器单测（TDD 先红）：
// 用合成源码验证导出符号的收集、渲染与排序口径，以及未导出符号被排除。
func TestCollectAPISurfaceBasics(t *testing.T) {
	src := `package sample

import (
	"errors"
	"time"
)

// Exported 导出结构体（含一个带 tag 的字段）。
type Exported struct {
	A int
	B string ` + "`json:\"b\"`" + `
}

// Duration 导出别名。
type Duration = time.Duration

// List 导出泛型类型。
type List[T any] struct {
	Items []T
}

// hidden 未导出类型，不得出现在表面中。
type hidden struct{ x int }

// Reader 导出接口（含方法签名）。
type Reader interface {
	Read(p []byte) (int, error)
	Close() error
}

// Do 导出函数。
func Do(x int) error { return nil }

// Map 导出泛型函数。
func Map[T any](in []T) []T { return nil }

// Multi 参数跨多行书写的导出函数（渲染必须归一为单行）。
func Multi(
	a int,
	b string,
) (bool, error) {
	return true, nil
}

// helper 未导出函数，不得出现在表面中。
func helper() {}

// Method 导出方法（指针接收者）。
func (e *Exported) Method() {}

// ErrBoom 导出变量。
var ErrBoom = errors.New("boom")

// K 导出常量。
const K = 1

// unexportedVar 未导出变量，不得出现在表面中。
var unexportedVar = 2
`
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.go")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatalf("写合成源码失败: %v", err)
	}

	entries, err := collectAPISurfaceFrom(dir)
	if err != nil {
		t.Fatalf("收集失败: %v", err)
	}

	var lines []string
	for _, e := range entries {
		lines = append(lines, e.line)
	}
	joined := strings.Join(lines, "\n")

	want := strings.Join([]string{
		`const K = 1`,
		`func (e *Exported) Method()`,
		`func Do(x int) error`,
		`func Map[T any](in []T) []T`,
		`func Multi(a int, b string) (bool, error)`,
		`type Duration = time.Duration`,
		`type Exported struct { A int; B string ` + "`json:\"b\"`" + ` }`,
		`type List[T any] struct { Items []T }`,
		`type Reader interface { Read(p []byte) (int, error); Close() error }`,
		`var ErrBoom = errors.New("boom")`,
	}, "\n")

	if joined != want {
		t.Errorf("收集结果与预期不一致\n--- got ---\n%s\n--- want ---\n%s", joined, want)
	}
	for _, forbidden := range []string{"hidden", "helper", "unexportedVar"} {
		if strings.Contains(joined, forbidden) {
			t.Errorf("未导出符号 %q 不应出现在 API 表面中", forbidden)
		}
	}
}

// TestAPISurfaceDiffClassification 差异归类单测：
// 新增 / 删除 / 变更 必须被明确列出，失败信息才具备可读性。
func TestAPISurfaceDiffClassification(t *testing.T) {
	want := []apiSurfaceEntry{
		{key: "func Old", line: "func Old()"},
		{key: "func Same", line: "func Same(int) string"},
		{key: "type T", line: "type T struct { A int }"},
	}
	got := []apiSurfaceEntry{
		{key: "func Same", line: "func Same(int, bool) string"},
		{key: "func New", line: "func New() error"},
		{key: "type T", line: "type T struct { A int }"},
	}

	diff := diffAPISurface(want, got)
	if diff.empty() {
		t.Fatal("存在差异时 diff.empty() 不应为 true")
	}
	if len(diff.added) != 1 || diff.added[0] != "func New() error" {
		t.Errorf("新增归类错误: %v", diff.added)
	}
	if len(diff.removed) != 1 || diff.removed[0] != "func Old()" {
		t.Errorf("删除归类错误: %v", diff.removed)
	}
	if len(diff.changed) != 1 {
		t.Fatalf("变更归类数量错误: %v", diff.changed)
	}
	c := diff.changed[0]
	if c.before != "func Same(int) string" || c.after != "func Same(int, bool) string" {
		t.Errorf("变更内容错误: before=%q after=%q", c.before, c.after)
	}

	report := diff.report()
	for _, want := range []string{"【新增", "func New() error", "【删除", "func Old()", "【变更", "func Same(int) string"} {
		if !strings.Contains(report, want) {
			t.Errorf("差异报告缺少 %q，实际报告:\n%s", want, report)
		}
	}
	if !strings.Contains(apiSurfaceRemediationHint(), apiSurfaceUpdateEnv) {
		t.Errorf("处置提示应说明 %s 更新方式", apiSurfaceUpdateEnv)
	}
}

// TestAPISurfaceGoldenFileDiscipline 黄金文件头部必须写清纪律
// （更新黄金文件 = 维护者批准的 API 契约变更），且不得包含符号行以外的杂项。
func TestAPISurfaceGoldenFileDiscipline(t *testing.T) {
	raw, err := os.ReadFile(apiSurfaceGoldenPath)
	if err != nil {
		t.Skipf("黄金文件尚未生成，跳过纪律检查: %v", err)
	}
	content := string(raw)
	for _, keyword := range []string{"API", "契约", "维护者", apiSurfaceUpdateEnv} {
		if !strings.Contains(content, keyword) {
			t.Errorf("黄金文件头部缺少纪律关键字 %q", keyword)
		}
	}
	// 黄金文件的符号行必须与当前收集结果一致（防止只改纪律不改清单的漂移）。
	entries, err := collectAPISurfaceFrom(".")
	if err != nil {
		t.Fatalf("收集失败: %v", err)
	}
	if diff := diffAPISurface(parseAPISurfaceGolden(content), entries); !diff.empty() {
		t.Errorf("黄金文件符号清单与当前表面不一致:\n%s", diff.report())
	}
}

// ===== 实现：导出表面收集器（go/parser + go/ast，不调用外部命令）=====

// apiSurfaceEntry 一个导出符号：黄金行 + 归类键。
type apiSurfaceEntry struct {
	key  string // 归类键：kind + 接收者类型 + 名称（用于新增/删除/变更归类）
	line string // 黄金行：单行签名摘要
}

// apiSurfaceUpdateFlag 等价于 AP_API_SURFACE_UPDATE=1 的命令行开关。
var apiSurfaceUpdateFlag = flag.Bool("api-surface-update", false,
	"重新生成 pkg/api_surface.golden 黄金快照（仅限维护者批准的 API 契约变更）")

// apiSurfaceUpdateRequested 是否请求重新生成黄金文件。
func apiSurfaceUpdateRequested() bool {
	if os.Getenv(apiSurfaceUpdateEnv) == "1" {
		return true
	}
	return apiSurfaceUpdateFlag != nil && *apiSurfaceUpdateFlag
}

// apiSurfaceGoldenHeader 黄金文件头部：纪律 + 格式说明（重新生成时整体重写）。
var apiSurfaceGoldenHeader = []string{
	"# AgentPrimordia 公共 API 表面黄金快照（pkg/api_surface.golden）",
	"#",
	"# 本文件由 agentprimordia/pkg/api_surface_test.go 机械化生成与校验：",
	"# 用 go/parser + go/ast 解析 pkg/*.go，逐行记录包 ap 的全部导出符号。",
	"#",
	"# 【纪律——本文件是公共 API 契约的一部分】",
	"#   1. 任何导出符号的新增 / 删除 / 签名变更都会使 TestAPISurfaceGoldenMatches 变红",
	"#      （CI 语义：新增导出即红，不设豁免抽样）；",
	"#   2. 更新本文件 = 一次维护者批准的 API 契约变更：必须在提交信息中显式说明",
	"#      （改了什么、为什么、影响面），或在 APPROVALS 记录中登记批准人；",
	"#   3. 重新生成命令（两种等价，禁止手改本文件后直接提交）：",
	"#        AP_API_SURFACE_UPDATE=1 go test ./pkg/ -run TestAPISurfaceGoldenMatches",
	"#        go test ./pkg/ -run TestAPISurfaceGoldenMatches -api-surface-update",
	"#   4. 「先更新黄金文件再合入」不是合法绕过路径：评审必须看到契约变更说明。",
	"#",
	"# 【行格式】每行一个符号，按字典序全量排序；# 开头为注释行，比对时忽略。",
	"#   func <Name>(<params>) <results>      导出函数 / 方法（方法含接收者）",
	"#   type <Name> ...                     导出类型（含导出字段 / 方法集 / 底层类型 / 别名）",
	"#   var  <Name> = <value>               导出变量",
	"#   const <Name> = <value>              导出常量",
}

// collectAPISurfaceFrom 解析 dir 目录下的包级 Go 源码（经 go/build 尊重构建标签，
// 只收录当前平台参与编译的非测试文件），返回全部导出符号（按黄金行字典序排序）。
func collectAPISurfaceFrom(dir string) ([]apiSurfaceEntry, error) {
	bp, err := build.ImportDir(dir, 0)
	if err != nil {
		return nil, fmt.Errorf("读取 Go 包目录 %s 失败: %w", dir, err)
	}
	files := make([]string, 0, len(bp.GoFiles)+len(bp.CgoFiles))
	files = append(files, bp.GoFiles...)
	files = append(files, bp.CgoFiles...)
	sort.Strings(files)

	fset := token.NewFileSet()
	var entries []apiSurfaceEntry
	for _, name := range files {
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			return nil, fmt.Errorf("解析 %s 失败: %w", name, err)
		}
		entries = append(entries, collectExportedFromFile(f)...)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].line < entries[j].line })

	// 防御：键唯一性校验——重复键意味着收集器口径缺陷，必须显式失败而不是静默合并。
	seen := make(map[string]string, len(entries))
	for _, e := range entries {
		if prev, ok := seen[e.key]; ok {
			return nil, fmt.Errorf("收集器缺陷：符号键 %q 重复（%q 与 %q）", e.key, prev, e.line)
		}
		seen[e.key] = e.line
	}
	return entries, nil
}

// collectExportedFromFile 收集单个文件的导出符号。
func collectExportedFromFile(f *ast.File) []apiSurfaceEntry {
	var out []apiSurfaceEntry
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Name == nil || !d.Name.IsExported() {
				continue
			}
			out = append(out, newAPISurfaceEntry(renderFuncDecl(d)))
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if !s.Name.IsExported() {
						continue
					}
					out = append(out, newAPISurfaceEntry("type "+renderTypeSpec(s)))
				case *ast.ValueSpec:
					kind := d.Tok.String() // "var" / "const"
					for i, name := range s.Names {
						if !name.IsExported() {
							continue
						}
						var value ast.Expr
						if i < len(s.Values) {
							value = s.Values[i]
						}
						out = append(out, newAPISurfaceEntry(kind+" "+renderValueSpec(name, s.Type, value)))
					}
				}
			}
		}
	}
	return out
}

// newAPISurfaceEntry 由黄金行构造条目（键从行内解析，保证与黄金文件口径一致）。
func newAPISurfaceEntry(line string) apiSurfaceEntry {
	line = collapseSpaces(line)
	return apiSurfaceEntry{key: apiSurfaceKeyOf(line), line: line}
}

// ===== 渲染器：AST → 单行签名摘要（口径完全自控，保证确定性与可读性）=====

// renderFuncDecl 渲染函数/方法签名（不含函数体）：func [recv ]Name[typeparams](params) results
func renderFuncDecl(d *ast.FuncDecl) string {
	var b strings.Builder
	b.WriteString("func ")
	if d.Recv != nil && len(d.Recv.List) > 0 {
		b.WriteString("(")
		b.WriteString(renderField(d.Recv.List[0]))
		b.WriteString(") ")
	}
	b.WriteString(d.Name.Name)
	if d.Type == nil {
		return b.String()
	}
	if tp := d.Type.TypeParams; tp != nil && len(tp.List) > 0 {
		b.WriteString("[")
		b.WriteString(renderFieldList(tp))
		b.WriteString("]")
	}
	b.WriteString(renderFuncSignature(d.Type))
	return b.String()
}

// renderFuncSignature 渲染 "(params) results"；单一匿名返回值省略括号（Go 书写惯例）。
func renderFuncSignature(ft *ast.FuncType) string {
	var b strings.Builder
	b.WriteString("(")
	if ft.Params != nil {
		b.WriteString(renderFieldList(ft.Params))
	}
	b.WriteString(")")
	if ft.Results == nil || len(ft.Results.List) == 0 {
		return b.String()
	}
	if len(ft.Results.List) == 1 && ft.Results.List[0].Names == nil {
		if _, isFunc := ft.Results.List[0].Type.(*ast.FuncType); !isFunc {
			b.WriteString(" ")
			b.WriteString(renderExpr(ft.Results.List[0].Type))
			return b.String()
		}
	}
	b.WriteString(" (")
	b.WriteString(renderFieldList(ft.Results))
	b.WriteString(")")
	return b.String()
}

// renderFieldList 渲染字段列表（逗号分隔；字段带名称时输出 "name type"）。
func renderFieldList(fl *ast.FieldList) string {
	if fl == nil || len(fl.List) == 0 {
		return ""
	}
	parts := make([]string, 0, len(fl.List))
	for _, f := range fl.List {
		parts = append(parts, renderField(f))
	}
	return strings.Join(parts, ", ")
}

// renderField 渲染单个字段："a int" / "*Foo"（嵌入或匿名）/ "T any"（泛型形参）。
func renderField(f *ast.Field) string {
	t := renderExpr(f.Type)
	if len(f.Names) == 0 {
		return t
	}
	names := make([]string, 0, len(f.Names))
	for _, n := range f.Names {
		names = append(names, n.Name)
	}
	return strings.Join(names, ", ") + " " + t
}

// renderTypeSpec 渲染类型声明（不含 "type " 前缀）：
// Name[typeparams] = <别名> 或 Name[typeparams] <定义>。
func renderTypeSpec(s *ast.TypeSpec) string {
	var b strings.Builder
	b.WriteString(s.Name.Name)
	if s.TypeParams != nil && len(s.TypeParams.List) > 0 {
		b.WriteString("[")
		b.WriteString(renderFieldList(s.TypeParams))
		b.WriteString("]")
	}
	if s.Assign.IsValid() {
		b.WriteString(" = ")
		b.WriteString(renderExpr(s.Type))
		return b.String()
	}
	b.WriteString(" ")
	b.WriteString(renderExpr(s.Type))
	return b.String()
}

// renderValueSpec 渲染 var/const 单项：Name[ Type][ = Value]。
func renderValueSpec(name *ast.Ident, typ, value ast.Expr) string {
	var b strings.Builder
	b.WriteString(name.Name)
	if typ != nil {
		b.WriteString(" ")
		b.WriteString(renderExpr(typ))
	}
	if value != nil {
		b.WriteString(" = ")
		b.WriteString(renderExpr(value))
	}
	return b.String()
}

// renderExpr 渲染类型/值表达式为单行文本；未覆盖的节点回退到 go/printer + 空白折叠。
func renderExpr(e ast.Expr) string {
	switch x := e.(type) {
	case nil:
		return ""
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return renderExpr(x.X) + "." + x.Sel.Name
	case *ast.StarExpr:
		return "*" + renderExpr(x.X)
	case *ast.ParenExpr:
		return "(" + renderExpr(x.X) + ")"
	case *ast.ArrayType:
		if x.Len == nil {
			return "[]" + renderExpr(x.Elt)
		}
		return "[" + renderExpr(x.Len) + "]" + renderExpr(x.Elt)
	case *ast.MapType:
		return "map[" + renderExpr(x.Key) + "]" + renderExpr(x.Value)
	case *ast.ChanType:
		switch x.Dir {
		case ast.SEND:
			return "chan<- " + renderExpr(x.Value)
		case ast.RECV:
			return "<-chan " + renderExpr(x.Value)
		default:
			return "chan " + renderExpr(x.Value)
		}
	case *ast.Ellipsis:
		return "..." + renderExpr(x.Elt)
	case *ast.IndexExpr:
		return renderExpr(x.X) + "[" + renderExpr(x.Index) + "]"
	case *ast.IndexListExpr:
		idx := make([]string, 0, len(x.Indices))
		for _, i := range x.Indices {
			idx = append(idx, renderExpr(i))
		}
		return renderExpr(x.X) + "[" + strings.Join(idx, ", ") + "]"
	case *ast.FuncType:
		return "func" + renderFuncSignature(x)
	case *ast.StructType:
		return renderStructType(x)
	case *ast.InterfaceType:
		return renderInterfaceType(x)
	case *ast.BasicLit:
		return normalizeLit(x.Value)
	case *ast.CompositeLit:
		elts := make([]string, 0, len(x.Elts))
		for _, el := range x.Elts {
			elts = append(elts, renderExpr(el))
		}
		return renderExpr(x.Type) + "{" + strings.Join(elts, ", ") + "}"
	case *ast.KeyValueExpr:
		return renderExpr(x.Key) + ": " + renderExpr(x.Value)
	case *ast.CallExpr:
		args := make([]string, 0, len(x.Args))
		for _, a := range x.Args {
			args = append(args, renderExpr(a))
		}
		if x.Ellipsis.IsValid() && len(args) > 0 {
			args[len(args)-1] += "..."
		}
		return renderExpr(x.Fun) + "(" + strings.Join(args, ", ") + ")"
	case *ast.UnaryExpr:
		return x.Op.String() + renderExpr(x.X)
	case *ast.BinaryExpr:
		return renderExpr(x.X) + " " + x.Op.String() + " " + renderExpr(x.Y)
	case *ast.TypeAssertExpr:
		return renderExpr(x.X) + ".(" + renderExpr(x.Type) + ")"
	case *ast.SliceExpr:
		s := renderExpr(x.X) + "["
		if x.Low != nil {
			s += renderExpr(x.Low)
		}
		s += ":"
		if x.High != nil {
			s += renderExpr(x.High)
		}
		if x.Max != nil {
			s += ":" + renderExpr(x.Max)
		}
		return s + "]"
	default:
		return printerFallback(e)
	}
}

// renderStructType 渲染结构体类型，仅收录导出字段（含 tag）与可导出的嵌入字段。
func renderStructType(x *ast.StructType) string {
	if x.Fields == nil || len(x.Fields.List) == 0 {
		return "struct {}"
	}
	parts := make([]string, 0, len(x.Fields.List))
	for _, f := range x.Fields.List {
		if len(f.Names) == 0 {
			// 嵌入字段：嵌入类型不可导出即不可从包外引用，不计入 API 表面
			if !embeddedExported(f.Type) {
				continue
			}
			parts = append(parts, renderExpr(f.Type))
			continue
		}
		var names []string
		for _, n := range f.Names {
			if n.IsExported() {
				names = append(names, n.Name)
			}
		}
		if len(names) == 0 {
			continue
		}
		field := strings.Join(names, ", ") + " " + renderExpr(f.Type)
		if f.Tag != nil {
			field += " " + normalizeLit(f.Tag.Value)
		}
		parts = append(parts, field)
	}
	if len(parts) == 0 {
		return "struct {}"
	}
	return "struct { " + strings.Join(parts, "; ") + " }"
}

// renderInterfaceType 渲染接口类型，仅收录导出方法（含签名）与可导出的嵌入接口。
func renderInterfaceType(x *ast.InterfaceType) string {
	if x.Methods == nil || len(x.Methods.List) == 0 {
		return "interface {}"
	}
	var parts []string
	for _, m := range x.Methods.List {
		if len(m.Names) == 0 {
			if !embeddedExported(m.Type) {
				continue
			}
			parts = append(parts, renderExpr(m.Type))
			continue
		}
		ft, ok := m.Type.(*ast.FuncType)
		for _, n := range m.Names {
			if !n.IsExported() {
				continue
			}
			if !ok {
				parts = append(parts, n.Name+" "+renderExpr(m.Type))
				continue
			}
			parts = append(parts, n.Name+renderFuncSignature(ft))
		}
	}
	if len(parts) == 0 {
		return "interface {}"
	}
	return "interface { " + strings.Join(parts, "; ") + " }"
}

// embeddedExported 判断嵌入字段/接口的类型是否可导出（可从包外引用）。
func embeddedExported(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Ident:
		return x.IsExported()
	case *ast.SelectorExpr:
		return x.Sel.IsExported()
	case *ast.StarExpr:
		return embeddedExported(x.X)
	case *ast.IndexExpr:
		return embeddedExported(x.X)
	case *ast.IndexListExpr:
		return embeddedExported(x.X)
	default:
		return true // 未识别形态保守计入
	}
}

// normalizeLit 规范化字面量文本：跨行 raw string 的换行转义为 \n，保证黄金行单行稳定。
func normalizeLit(v string) string {
	if !strings.ContainsAny(v, "\n\r") {
		return v
	}
	v = strings.ReplaceAll(v, "\r\n", `\n`)
	v = strings.ReplaceAll(v, "\n", `\n`)
	v = strings.ReplaceAll(v, "\r", `\n`)
	return v
}

// printerFallback 渲染器未覆盖的 AST 形态：用 go/printer + 空白折叠兜底，
// 避免收集器因陌生语法整体失败（fresh fset 使 printer 输出单行）。
func printerFallback(e ast.Expr) string {
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, token.NewFileSet(), e); err != nil {
		return fmt.Sprintf("<渲染失败: %v>", err)
	}
	return collapseSpaces(buf.String())
}

// collapseSpaces 把渲染文本折叠成单行：连续空白压成一个空格，
// 字符串字面量（含结构体 tag）内部的空白原样保留，保证黄金行稳定且不损坏 tag。
func collapseSpaces(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	pendingSpace := false
	inString := byte(0) // 0 = 不在字符串中；'`' = raw string；'"' = interpreted string
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString != 0 {
			b.WriteByte(c)
			switch {
			case c == '\\' && inString == '"':
				if i+1 < len(s) {
					i++
					b.WriteByte(s[i])
				}
			case c == inString:
				inString = 0
			}
			continue
		}
		switch c {
		case ' ', '\t', '\n', '\r':
			pendingSpace = true
			continue
		case '`', '"':
			inString = c
		}
		if pendingSpace {
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			pendingSpace = false
		}
		b.WriteByte(c)
	}
	return b.String()
}

// apiSurfaceKeyOf 从黄金行解析归类键，用于把差异归类为 新增 / 删除 / 变更。
// 行格式（由收集器生成，格式受控）：
//
//	func  [<recv>] <Name> ...      type <Name> ...      var|const <Name> ...
func apiSurfaceKeyOf(line string) string {
	rest := strings.TrimSpace(line)
	kind, rest := cutField(rest)
	if kind == "func" && strings.HasPrefix(rest, "(") {
		if end := strings.IndexByte(rest, ')'); end > 0 {
			if recv := receiverTypeName(rest[:end+1]); recv != "" {
				return "func (" + recv + ") " + symbolName(rest[end+1:])
			}
		}
	}
	return kind + " " + symbolName(rest)
}

// cutField 切出第一个空白分隔的字段，返回 (首字段, 其余部分)。
func cutField(s string) (first, rest string) {
	s = strings.TrimLeft(s, " \t")
	i := strings.IndexAny(s, " \t")
	if i < 0 {
		return s, ""
	}
	return s[:i], strings.TrimLeft(s[i:], " \t")
}

// symbolName 取行内符号名（读到 '(' / '[' / 空白 / '=' 或行尾为止）。
func symbolName(s string) string {
	s = strings.TrimLeft(s, " \t")
	i := 0
	for i < len(s) {
		c := s[i]
		if c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			i++
			continue
		}
		break
	}
	return s[:i]
}

// receiverTypeName 从 "(f *Foo)" / "(*Foo)" 形式的接收者文本中取出类型 "*Foo"。
func receiverTypeName(recv string) string {
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(recv, "("), ")"))
	fields := strings.Fields(inner)
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1] // 接收者形参名可选，末段即类型
}

// parseAPISurfaceGolden 解析黄金文件：忽略注释行与空行，其余每行一个符号。
func parseAPISurfaceGolden(content string) []apiSurfaceEntry {
	var entries []apiSurfaceEntry
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		entries = append(entries, apiSurfaceEntry{key: apiSurfaceKeyOf(trimmed), line: trimmed})
	}
	return entries
}

// writeAPISurfaceGolden 写黄金文件（头部纪律 + 符号清单）。
func writeAPISurfaceGolden(path string, lines []string) error {
	var buf bytes.Buffer
	for _, h := range apiSurfaceGoldenHeader {
		buf.WriteString(h)
		buf.WriteByte('\n')
	}
	buf.WriteByte('\n')
	for _, l := range lines {
		buf.WriteString(l)
		buf.WriteByte('\n')
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// apiSurfaceRemediationHint 测试失败时输出的处置指引。
func apiSurfaceRemediationHint() string {
	return "处置：更新黄金文件 = 维护者批准的 API 契约变更。确认后执行\n" +
		"  " + apiSurfaceUpdateEnv + "=1 go test ./pkg/ -run TestAPISurfaceGoldenMatches\n" +
		"重新生成 pkg/api_surface.golden，并在提交信息中说明变更理由（改了什么 / 为什么 / 影响面）。"
}

// ===== 差异归类（失败信息必须可读：明确列出 新增 / 删除 / 变更）=====

// apiSurfaceChange 一个签名发生变更的符号。
type apiSurfaceChange struct {
	key    string
	before string
	after  string
}

// apiSurfaceDiff 导出表面差异。
type apiSurfaceDiff struct {
	added   []string
	removed []string
	changed []apiSurfaceChange
}

func (d apiSurfaceDiff) empty() bool {
	return len(d.added)+len(d.removed)+len(d.changed) == 0
}

// diffAPISurface 按符号键比对黄金行：只在一边出现 = 新增/删除，两边都有但行不同 = 变更。
func diffAPISurface(want, got []apiSurfaceEntry) apiSurfaceDiff {
	wantByKey := indexAPISurface(want)
	gotByKey := indexAPISurface(got)

	var d apiSurfaceDiff
	for key, line := range gotByKey {
		if _, ok := wantByKey[key]; !ok {
			d.added = append(d.added, line)
		}
	}
	for key, line := range wantByKey {
		if _, ok := gotByKey[key]; !ok {
			d.removed = append(d.removed, line)
		}
	}
	for key, after := range gotByKey {
		if before, ok := wantByKey[key]; ok && before != after {
			d.changed = append(d.changed, apiSurfaceChange{key: key, before: before, after: after})
		}
	}
	sort.Strings(d.added)
	sort.Strings(d.removed)
	sort.Slice(d.changed, func(i, j int) bool { return d.changed[i].key < d.changed[j].key })
	return d
}

func indexAPISurface(entries []apiSurfaceEntry) map[string]string {
	m := make(map[string]string, len(entries))
	for _, e := range entries {
		m[e.key] = e.line
	}
	return m
}

// report 渲染可读差异报告。
func (d apiSurfaceDiff) report() string {
	var b strings.Builder
	if len(d.added) > 0 {
		fmt.Fprintf(&b, "【新增 %d 个导出符号】\n", len(d.added))
		for _, l := range d.added {
			fmt.Fprintf(&b, "  + %s\n", l)
		}
	}
	if len(d.removed) > 0 {
		fmt.Fprintf(&b, "【删除 %d 个导出符号】\n", len(d.removed))
		for _, l := range d.removed {
			fmt.Fprintf(&b, "  - %s\n", l)
		}
	}
	if len(d.changed) > 0 {
		fmt.Fprintf(&b, "【变更 %d 个导出符号】\n", len(d.changed))
		for _, c := range d.changed {
			fmt.Fprintf(&b, "  ~ %s\n      黄金文件: %s\n      当前代码: %s\n", c.key, c.before, c.after)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
