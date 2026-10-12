// v8_cut_scope_test.go — v7.8「8.0 彩排」deprecation 范围的双边包含断言（TDD）。
//
// 【机制】
//  1. 事实来源：pkg/v8_cut_scope.txt（每行一个符号名，增删需维护者批准）；
//  2. 代码侧事实：pkg/*.go 中每个导出符号的文档注释是否含稳定 token [v8.0-cut]；
//  3. 双边包含断言：
//     a. 决定稿 ⊆ 已标记——清单里每个符号都必须存在且带 [v8.0-cut]（防"登记了没标"）；
//     b. 已标记 ⊆ 决定稿——任何带 [v8.0-cut] 的符号都必须在清单里（防漏登记）；
//     c. worldmodel 导出既不在清单里、也不带标记（版本路线 §四：D1 已决，不动）。
//
// 【失败输出】列出双边差异（缺标记的 / 未登记的），可直接指到具体文件与符号。
//
// 【为什么不用 go doc】与 pkg/api_surface_test.go 同口径：go/parser + go/ast
// 纯标准库解析，不 exec 外部命令，保证 CI 确定性与跨平台可跑。
package ap

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// v8CutScopePath 范围数据文件（相对测试工作目录 = pkg/）。
const v8CutScopePath = "v8_cut_scope.txt"

// v8CutToken 稳定标记 token：每个计划 v8.0 移除的导出符号的 Deprecated 注释必须含它。
// 用稳定 token 而不是自由文本匹配，是因为注释措辞会随评审演化，token 不会。
const v8CutToken = "[v8.0-cut]"

// v8CutScopeFileHeaderMustContain 范围文件头部必须写清的纪律关键字
// （与 api_surface.golden 的纪律检查同思路：数据文件也要能自证纪律）。
var v8CutScopeFileHeaderMustContain = []string{"事实来源", "维护者", v8CutToken, "worldmodel"}

// v8WorldModelExports worldmodel 相关导出（pkg/agent.go）。
// 版本路线 §四：worldmodel 维持 opt-in、E-1 阴性后翻默认否决，但明确"不动"——
// 因此这些符号既不得进移除清单，也不得带 [v8.0-cut] 标记。
var v8WorldModelExports = []string{"WithWorldModel", "WorldModelTracker"}

// ===== 测试 =====

// TestV8CutScopeFullyMarked 断言 a：清单（决定稿）⊆ 已标记。
// 清单里每个符号都必须存在于 pkg 导出面，且其文档注释含 [v8.0-cut]。
func TestV8CutScopeFullyMarked(t *testing.T) {
	scope := loadV8CutScope(t)
	exports := collectPkgExportedSymbols(t)

	var missing, unmarked []string
	for _, name := range scope {
		sym, ok := exports[name]
		if !ok {
			missing = append(missing, name)
			continue
		}
		if !sym.marked {
			unmarked = append(unmarked, fmt.Sprintf("%s（%s）", name, sym.file))
		}
	}
	if len(missing) > 0 {
		t.Errorf("清单登记了 %d 个但 pkg 中不存在对应导出符号（决定稿 §登记了没标）:\n  %s",
			len(missing), strings.Join(sortedCopy(missing), "\n  "))
	}
	if len(unmarked) > 0 {
		t.Errorf("清单中 %d 个符号缺少 %s 标记（决定稿 ⊆ 已标记 不成立）:\n  %s",
			len(unmarked), v8CutToken, strings.Join(sortedCopy(unmarked), "\n  "))
	}
	if len(missing) == 0 && len(unmarked) == 0 {
		t.Logf("决定稿 ⊆ 已标记：清单 %d 个符号全部存在且带 %s 标记", len(scope), v8CutToken)
	}
}

// TestNoUnregisteredV8CutMarkers 断言 b：已标记 ⊆ 决定稿（防漏登记）。
// 任何带 [v8.0-cut] 的导出符号都必须登记在 v8_cut_scope.txt 中。
func TestNoUnregisteredV8CutMarkers(t *testing.T) {
	scope := setOf(loadV8CutScope(t))
	exports := collectPkgExportedSymbols(t)

	var extra []string
	for name, sym := range exports {
		if sym.marked && !scope[name] {
			extra = append(extra, fmt.Sprintf("%s（%s）", name, sym.file))
		}
	}
	if len(extra) > 0 {
		t.Errorf("%d 个符号带 %s 标记但未登记在 %s（已标记 ⊆ 决定稿 不成立）:\n  %s\n"+
			"处置：补登记该符号，或撤掉其 Deprecated 注释。",
			len(extra), v8CutToken, v8CutScopePath, strings.Join(sortedCopy(extra), "\n  "))
		return
	}
	marked := 0
	for _, sym := range exports {
		if sym.marked {
			marked++
		}
	}
	t.Logf("已标记 ⊆ 决定稿：%d 个带标记符号全部在案", marked)
}

// TestWorldModelNotInV8CutScope 断言 c：worldmodel 导出不在移除清单、也不带标记。
func TestWorldModelNotInV8CutScope(t *testing.T) {
	scope := setOf(loadV8CutScope(t))
	exports := collectPkgExportedSymbols(t)

	for _, name := range v8WorldModelExports {
		if scope[name] {
			t.Errorf("worldmodel 导出 %s 不得进入 8.0 移除清单（版本路线 §四：D1 已决，不动）", name)
		}
		if sym, ok := exports[name]; ok && sym.marked {
			t.Errorf("worldmodel 导出 %s 不得带 %s 标记（%s）", name, v8CutToken, sym.file)
		}
	}
	// 兜底：任何声明里引用 worldmodel 包的导出符号都不得进清单/带标记。
	for name, sym := range exports {
		if !strings.Contains(sym.declText, "worldmodel") && !strings.HasPrefix(name, "WorldModel") {
			continue
		}
		if scope[name] || sym.marked {
			t.Errorf("引用 worldmodel 的导出 %s（%s）不得进入移除清单或带 %s 标记",
				name, sym.file, v8CutToken)
		}
	}
}

// TestV8CutScopeFileDiscipline 范围数据文件头部必须写清纪律，
// 且不得出现重复行（重复行会让双边断言失去"清单"语义）。
func TestV8CutScopeFileDiscipline(t *testing.T) {
	raw, err := os.ReadFile(v8CutScopePath)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", v8CutScopePath, err)
	}
	content := string(raw)
	for _, keyword := range v8CutScopeFileHeaderMustContain {
		if !strings.Contains(content, keyword) {
			t.Errorf("%s 头部缺少纪律关键字 %q", v8CutScopePath, keyword)
		}
	}
	seen := map[string]bool{}
	var dups []string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if seen[trimmed] {
			dups = append(dups, trimmed)
		}
		seen[trimmed] = true
	}
	if len(dups) > 0 {
		t.Errorf("%s 存在重复符号行: %s", v8CutScopePath, strings.Join(dups, ", "))
	}
}

// TestPkgExportedSymbolNamesUnique 同名导出符号会让"每行一个符号名"的清单产生歧义，
// 必须在测试里显式失败（而不是静默取第一个）。
func TestPkgExportedSymbolNamesUnique(t *testing.T) {
	exports := collectPkgExportedSymbols(t)
	if len(exports) == 0 {
		t.Fatal("未收集到任何导出符号（收集器缺陷）")
	}
	// collectPkgExportedSymbols 以名称为键，重复键会被显式记录。
	for _, name := range sortedKeys(exports) {
		if len(exports[name].dupOf) > 0 {
			t.Errorf("导出符号名 %s 在 pkg/ 中重复出现（%s 与 %s）：清单口径要求名称唯一",
				name, exports[name].file, strings.Join(exports[name].dupOf, ", "))
		}
	}
}

// ===== 实现 =====

// v8CutSymbol 一个 pkg 导出符号的 deprecation 状态。
type v8CutSymbol struct {
	file     string   // 声明所在文件（失败信息要能指到文件）
	kind     string   // func / type / var / const
	declText string   // 声明源码片段（用于 worldmodel 兜底判定）
	marked   bool     // 文档注释是否含 [v8.0-cut]
	dupOf    []string // 同名符号的其他声明位置（非空即口径缺陷）
}

// loadV8CutScope 读取范围数据文件：忽略空行与 # 注释行，返回符号名清单。
func loadV8CutScope(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(v8CutScopePath)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", v8CutScopePath, err)
	}
	var out []string
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		t.Fatalf("%s 为空：8.0 移除范围不能是空清单", v8CutScopePath)
	}
	return out
}

// collectPkgExportedSymbols 解析 pkg/*.go（非测试文件，尊重构建标签），
// 以符号名为键收集全部导出符号及其 deprecation 状态。
//
// 与 api-extract（scripts/api-extract/main.go）同口径：符号自身的文档注释优先，
// 单 spec 的 GenDecl 回退到 GenDecl 的文档注释——这样 `type X = internal.X`
// 这类写在 GenDecl 文档上的标记也能被识别。
func collectPkgExportedSymbols(t *testing.T) map[string]*v8CutSymbol {
	t.Helper()
	dir := "."
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		name := fi.Name()
		return strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
	}, parser.ParseComments)
	if err != nil {
		t.Fatalf("解析 pkg/ 失败: %v", err)
	}

	out := map[string]*v8CutSymbol{}
	record := func(file, kind, name, declText string, doc *ast.CommentGroup) {
		prev, ok := out[name]
		if ok {
			prev.dupOf = append(prev.dupOf, file)
			return
		}
		marked := doc != nil && strings.Contains(doc.Text(), v8CutToken)
		out[name] = &v8CutSymbol{
			file:     file,
			kind:     kind,
			declText: declText,
			marked:   marked,
		}
	}

	for _, pkg := range pkgs {
		for filename, f := range pkg.Files {
			base := filepath.Base(filename)
			for _, decl := range f.Decls {
				switch d := decl.(type) {
				case *ast.FuncDecl:
					if d.Name == nil || !d.Name.IsExported() {
						continue
					}
					if d.Recv != nil {
						continue // pkg 纯 re-export 层无导出方法；出现即口径变化，交由黄金快照管
					}
					record(base, "func", d.Name.Name, declSnippet(fset, d), d.Doc)
				case *ast.GenDecl:
					for _, spec := range d.Specs {
						switch s := spec.(type) {
						case *ast.TypeSpec:
							if !s.Name.IsExported() {
								continue
							}
							doc := s.Doc
							if doc == nil && len(d.Specs) == 1 {
								doc = d.Doc
							}
							record(base, "type", s.Name.Name, declSnippet(fset, d), doc)
						case *ast.ValueSpec:
							for _, n := range s.Names {
								if !n.IsExported() {
									continue
								}
								doc := s.Doc
								if doc == nil && len(d.Specs) == 1 {
									doc = d.Doc
								}
								record(base, d.Tok.String(), n.Name, declSnippet(fset, d), doc)
							}
						}
					}
				}
			}
		}
	}
	return out
}

// declSnippet 取声明的源码片段（单行折叠），仅用于失败信息与 worldmodel 兜底判定。
func declSnippet(fset *token.FileSet, decl ast.Decl) string {
	start := fset.Position(decl.Pos()).Offset
	end := fset.Position(decl.End()).Offset
	// 片段上限 400 字符：只可能是 `type X = worldmodel.X` 这类短声明。
	if end-start > 400 {
		end = start + 400
	}
	return collapseSpaces(declSnippetRaw(fset, decl, start, end))
}

// declSnippetRaw 从磁盘读回声明片段（parse 时未保留 src，按偏移取）。
func declSnippetRaw(fset *token.FileSet, decl ast.Decl, start, end int) string {
	pos := fset.Position(decl.Pos())
	raw, err := os.ReadFile(pos.Filename)
	if err != nil {
		return ""
	}
	if start < 0 || end > len(raw) || start > end {
		return ""
	}
	return string(raw[start:end])
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func sortedKeys(m map[string]*v8CutSymbol) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func setOf(in []string) map[string]bool {
	out := make(map[string]bool, len(in))
	for _, s := range in {
		out[s] = true
	}
	return out
}
