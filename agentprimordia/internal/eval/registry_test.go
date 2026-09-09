// registry_test.go / asserts_test.go — S0-1/S0-2 题面装载与确定性判定测试
package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEvalRegistryFrozen 题面冻结门（R4）：docs/evals 台账与磁盘必须逐字节一致。
// 这是验收前置门——题面被偷偷改动时，本测试即红。
func TestEvalRegistryFrozen(t *testing.T) {
	dir := DefaultEvalsDir()
	if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err != nil {
		t.Skipf("题面目录不可见（非仓库内运行？）: %v", err)
	}
	reg, err := LoadRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	if v := reg.VerifyFrozen(dir); len(v) > 0 {
		for _, x := range v {
			t.Errorf("冻结门失败: %s", x)
		}
	}
	if len(reg.Files) < 6 {
		t.Errorf("注册题面文件数应 ≥6，实际 %d", len(reg.Files))
	}
	var total, hold int
	for _, f := range reg.Files {
		total += f.Count
		hold += f.HoldoutCount
		if f.Count <= 0 {
			t.Errorf("%s 条数非法", f.File)
		}
		// 留出 id 清单与可见 id 清单必须互斥且并起来等于全集规模
		set := map[string]bool{}
		for _, id := range f.HoldoutIDs {
			if set[id] {
				t.Errorf("%s 留出 id 重复 %s", f.File, id)
			}
			set[id] = true
		}
		for _, id := range f.VisibleIDs {
			if set[id] {
				t.Errorf("%s 留出/可见 id 重叠 %s", f.File, id)
			}
		}
		if len(f.HoldoutIDs) != f.HoldoutCount {
			t.Errorf("%s 留出 id 数 %d 与 holdout_count %d 不符", f.File, len(f.HoldoutIDs), f.HoldoutCount)
		}
		if len(f.HoldoutIDs)+len(f.VisibleIDs) != f.Count {
			t.Errorf("%s 留出+可见 %d != 条数 %d", f.File, len(f.HoldoutIDs)+len(f.VisibleIDs), f.Count)
		}
		if f.HoldoutRate < MinHoldoutRate {
			t.Errorf("%s 留出比例 %.3f < %.2f", f.File, f.HoldoutRate, MinHoldoutRate)
		}
	}
	if float64(hold)/float64(total) < MinHoldoutRate {
		t.Errorf("总体留出比例 %.3f < %.2f", float64(hold)/float64(total), MinHoldoutRate)
	}
	t.Logf("题面冻结完好：%d 个注册文件 / %d 样本 / 留出 %d (%.0f%%)，冻结 commit %s",
		len(reg.Files), total, hold, 100*float64(hold)/float64(total), reg.FreezeCommit[:12])
}

// TestRegistry规模达路线图下限 S0-2 交付要求：长程 ≥20 / 缺口 ≥50 / 对抗 ≥500 / 外部 ≥100 / judge ≥200。
func TestRegistryScaleMeetsRoadmap(t *testing.T) {
	reg, err := LoadRegistry(DefaultEvalsDir())
	if err != nil {
		t.Skipf("题面目录不可见: %v", err)
	}
	want := map[string]int{
		"long-horizon-v1.json": 20, "gap-tools-v1.json": 50, "adversarial-holdout-v1.json": 500,
		"external-general-v1.json": 100, "judge-calibration-v1.json": 200,
	}
	for _, f := range reg.Files {
		base := filepath.Base(f.File)
		if minCount, ok := want[base]; ok {
			if f.Count < minCount {
				t.Errorf("%s 条数 %d < 路线图下限 %d", base, f.Count, minCount)
			}
			delete(want, base)
		}
	}
	for base := range want {
		t.Errorf("缺少注册题面 %s", base)
	}
}

// TestVerifyFrozenDetectsDrift 篡改拷贝目录里的题面必须被检出。
func TestVerifyFrozenDetectsDrift(t *testing.T) {
	src := DefaultEvalsDir()
	reg, err := LoadRegistry(src)
	if err != nil {
		t.Skipf("题面目录不可见: %v", err)
	}
	dst := t.TempDir()
	for _, f := range reg.Files {
		data, err := os.ReadFile(filepath.Join(src, filepath.Base(f.File)))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, filepath.Base(f.File)), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	manifest, err := os.ReadFile(filepath.Join(src, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "manifest.json"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	if v := reg.VerifyFrozen(dst); len(v) > 0 {
		t.Fatalf("干净副本不应报违规: %v", v)
	}
	// 改一个字节
	p := filepath.Join(dst, "long-horizon-v1.json")
	data, _ := os.ReadFile(p)
	mutated := strings.Replace(string(data), "clean_rows", "clean_rowsX", 1)
	if err := os.WriteFile(p, []byte(mutated), 0o644); err != nil {
		t.Fatal(err)
	}
	v := reg.VerifyFrozen(dst)
	if len(v) == 0 {
		t.Fatal("题面被改动却未检出——冻结门失效")
	}
	if !strings.Contains(v[0], "题面漂移") {
		t.Errorf("违规描述应指出漂移，得 %v", v)
	}
	t.Logf("检出漂移: %s", v[0])
}

// TestVerifyFrozenRequiresFreezeCommit R4 防回退回归门：freeze_commit 未登记（空或 PENDING 占位）
// 必须报违规——e7995ba6 重算台账曾把 d0491cbe 登记的冻结 commit 冲回占位符而无人拦截。
func TestVerifyFrozenRequiresFreezeCommit(t *testing.T) {
	reg, err := LoadRegistry(DefaultEvalsDir())
	if err != nil {
		t.Skipf("题面目录不可见: %v", err)
	}
	for _, bad := range []string{"", "PENDING-FREEZE-COMMIT"} {
		r2 := *reg
		r2.FreezeCommit = bad
		v := r2.VerifyFrozen(DefaultEvalsDir())
		found := false
		for _, x := range v {
			if strings.Contains(x, "freeze_commit") {
				found = true
			}
		}
		if !found {
			t.Errorf("freeze_commit=%q 应报违规，得 %v", bad, v)
		}
	}
	if reg.FreezeCommit == "" || reg.FreezeCommit == "PENDING-FREEZE-COMMIT" {
		t.Errorf("工作区台账 freeze_commit 处于未登记状态 %q——登记已回退", reg.FreezeCommit)
	}
}

func TestLoadSetHoldoutPartition(t *testing.T) {
	reg, err := LoadRegistry(DefaultEvalsDir())
	if err != nil {
		t.Skipf("题面目录不可见: %v", err)
	}
	all, err := LoadSet(DefaultEvalsDir(), "long-horizon-v1.json", false)
	if err != nil {
		t.Fatal(err)
	}
	// 留出装载结果必须与台账登记条数一致（台账与装载两条路径互验）
	var lhReg RegistryFile
	for _, f := range reg.Files {
		if strings.HasSuffix(f.File, "long-horizon-v1.json") {
			lhReg = f
		}
	}
	if lhReg.Count != len(all) {
		t.Errorf("全量装载 %d != 台账 %d", len(all), lhReg.Count)
	}
	hold, err := LoadSet(DefaultEvalsDir(), "long-horizon-v1.json", true)
	if err != nil {
		t.Fatal(err)
	}
	nHold := 0
	for _, it := range all {
		if it.Holdout {
			nHold++
		}
	}
	if len(hold) != nHold {
		t.Errorf("留出装载 %d != 全量中 holdout %d", len(hold), nHold)
	}
	for _, it := range hold {
		if !it.Holdout {
			t.Errorf("留出集里混入非留出样本 %s", it.ID)
		}
	}
	// 长程题面结构完整性：≥3 里程碑、≥1 次中断、success 断言非空、预算合理
	for _, it := range all {
		if len(it.Milestones) < 3 {
			t.Errorf("%s 里程碑数 %d < 3", it.ID, len(it.Milestones))
		}
		if len(it.Interruptions) < 1 {
			t.Errorf("%s 缺跨会话中断", it.ID)
		}
		if len(it.Grading.Success) == 0 {
			t.Errorf("%s 缺终局判据", it.ID)
		}
		if it.Budget.MaxTurns < 30 || it.Budget.MaxToolCalls < 60 {
			t.Errorf("%s 预算 %v/%v 未达长程下限（30 轮/60 调用）", it.ID, it.Budget.MaxTurns, it.Budget.MaxToolCalls)
		}
		if len(it.Fixtures) == 0 {
			t.Errorf("%s 缺初始环境", it.ID)
		}
	}
}

// TestLoadGapSetShape 缺口题面必须声明缺失能力与机检判据。
func TestLoadGapSetShape(t *testing.T) {
	items, err := LoadSet(DefaultEvalsDir(), "gap-tools-v1.json", false)
	if err != nil {
		t.Skipf("题面目录不可见: %v", err)
	}
	for _, it := range items {
		if it.AbsentCapability == "" {
			t.Errorf("%s 缺 absent_capability", it.ID)
		}
		if len(it.Grading.Success) != 1 {
			t.Errorf("%s 终局判据应为 1 条，实际 %d", it.ID, len(it.Grading.Success))
		}
	}
}

func TestAssertionUnmarshalAndKinds(t *testing.T) {
	var a Assertion
	if err := json.Unmarshal([]byte(`{"file_exists":"report.json"}`), &a); err != nil {
		t.Fatal(err)
	}
	if a.Kind != AssertFileExists {
		t.Errorf("Kind = %q", a.Kind)
	}
	if got := a.String(); !strings.Contains(got, "file_exists") {
		t.Errorf("String() = %q", got)
	}
	back, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	if string(back) != `{"file_exists":"report.json"}` {
		t.Errorf("往返不一致: %s", back)
	}
	var bad Assertion
	if err := json.Unmarshal([]byte(`{"a":1,"b":2}`), &bad); err == nil {
		t.Error("多键对象应报错")
	}
	if err := json.Unmarshal([]byte(`["file_exists"]`), &bad); err == nil {
		t.Error("非对象应报错")
	}
}

func TestEvaluateAssertions(t *testing.T) {
	root := t.TempDir()
	mk := func(rel, content string) {
		t.Helper()
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("report.json", `{"totals":{"A":30,"B":10},"clean_rows":2,"snap2":{"changed":["b.txt","x"]}}`)
	mk("report.txt", "2\n")
	mk("notes/a.md", "hello world\nsecond\n")
	mk("ledger.csv", "row,type,amount\n1,debit,50\n2,credit,100\n")
	mk("bad.json", "not json at all")

	cases := []struct {
		name string
		json string
		want bool
	}{
		{"存在", `{"file_exists":"report.txt"}`, true},
		{"不存在", `{"file_exists":"missing.txt"}`, false},
		{"嵌套不存在", `{"file_exists":"a/b/c.txt"}`, false},
		{"含子串", `{"file_contains":["notes/a.md","hello"]}`, true},
		{"不含子串", `{"file_contains":["notes/a.md","nope"]}`, false},
		{"全文相等去尾换行", `{"file_eq":["report.txt","2"]}`, true},
		{"json 点路径数字", `{"json_path_eq":["report.json","totals.A",30]}`, true},
		{"json 整数与浮点等值", `{"json_path_eq":["report.json","clean_rows",2.0]}`, true},
		{"json 数组下标", `{"json_path_eq":["report.json","snap2.changed[0]","b.txt"]}`, true},
		{"json 值不符", `{"json_path_eq":["report.json","totals.A",31]}`, false},
		{"json 路径缺失", `{"json_path_eq":["report.json","totals.Z",1]}`, false},
		{"json 非 JSON 文件", `{"json_path_eq":["bad.json","x",1]}`, false},
		{"行数匹配", `{"lines_match_count":["ledger.csv","^[0-9]",2]}`, true},
		{"行数匹配零", `{"lines_match_count":["notes/a.md","zzz",0]}`, true},
		{"行数不符", `{"lines_match_count":["ledger.csv","^",5]}`, false},
		{"正则非法", `{"lines_match_count":["ledger.csv","[",1]}`, false},
	}
	for _, c := range cases {
		var a Assertion
		if err := json.Unmarshal([]byte(c.json), &a); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		switch a.Kind {
		case AssertJSONPathEq:
			if c.name == "json 非 JSON 文件" {
				if _, err := EvaluateAssertion(root, a, nil); err == nil {
					t.Errorf("%s: 非 JSON 应返回错误", c.name)
				}
				continue
			}
			// json 路径缺失：路径缺失但文件存在，应判 false 而非 error；落入下方公共断言逻辑
		case AssertLinesMatchCount:
			if c.name == "正则非法" {
				if _, err := EvaluateAssertion(root, a, nil); err == nil {
					t.Errorf("%s: 非法正则应报错", c.name)
				}
				continue
			}
		}
		if a.Kind == AssertJSONPathEq && c.name == "json 值不符" {
			_, _ = fmt.Fprintf(os.Stderr, "")
		}
		got, err := EvaluateAssertion(root, a, nil)
		if err != nil {
			if c.want {
				t.Errorf("%s: 意外错误 %v", c.name, err)
			}
			continue
		}
		if got != c.want {
			t.Errorf("%s: EvaluateAssertion = %v, 期望 %v", c.name, got, c.want)
		}
	}
}

// TestEvaluateAssertionMissingFileIsFalse 终态里没有该文件应判 false（不是错误），
// 否则「没干活」会被算成判定异常。
func TestEvaluateAssertionMissingFileIsFalse(t *testing.T) {
	root := t.TempDir()
	for _, raw := range []string{
		`{"file_contains":["nope.txt","x"]}`,
		`{"file_eq":["nope.txt","x"]}`,
		`{"json_path_eq":["nope.json","a",1]}`,
		`{"lines_match_count":["nope.log","a",0]}`,
	} {
		var a Assertion
		if err := json.Unmarshal([]byte(raw), &a); err != nil {
			t.Fatal(err)
		}
		got, err := EvaluateAssertion(root, a, nil)
		if err != nil {
			t.Errorf("%s 应判 false 而非错误: %v", raw, err)
		}
		if got {
			t.Errorf("%s 应判 false", raw)
		}
	}
}

func TestEvaluateAssertionPathEscape(t *testing.T) {
	root := t.TempDir()
	var a Assertion
	if err := json.Unmarshal([]byte(`{"file_exists":"../../etc/passwd"}`), &a); err != nil {
		t.Fatal(err)
	}
	if _, err := EvaluateAssertion(root, a, nil); err == nil {
		t.Error("越出沙箱根的路径必须被拒绝")
	} else if !strings.Contains(err.Error(), "越") {
		t.Errorf("错误信息应说明越界: %v", err)
	}
	var abs Assertion
	if err := json.Unmarshal([]byte(`{"file_exists":"/etc/passwd"}`), &abs); err != nil {
		t.Fatal(err)
	}
	if _, err := EvaluateAssertion(root, abs, nil); err == nil {
		t.Error("绝对路径必须被拒绝")
	}
}

// mkAssert 以程序化方式构造断言（避免手写转义把非法 JSON 当成合法用例）。
func mkAssert(t *testing.T, kind string, arg any) Assertion {
	t.Helper()
	raw, err := json.Marshal(map[string]any{kind: arg})
	if err != nil {
		t.Fatal(err)
	}
	var a Assertion
	if err := json.Unmarshal(raw, &a); err != nil {
		t.Fatalf("构造断言失败 %s: %v", raw, err)
	}
	if a.Kind != kind {
		t.Fatalf("断言 kind 丢失: %+v", a)
	}
	return a
}

func TestEvaluateToolAssertionsWithRunnerFacts(t *testing.T) {
	facts := &RunnerFacts{
		RegisteredTools: map[string]bool{"base32-encode": true},
		ToolName:        "base32-encode",
		ToolOutput:      "{\"encoded\":\"NBSWY3DP\"}",
	}
	if ok, err := EvaluateAssertion(t.TempDir(), mkAssert(t, AssertToolRegistered, "base32-encode"), facts); err != nil || !ok {
		t.Errorf("tool_registered 应为 true: %v %v", ok, err)
	}
	if ok, _ := EvaluateAssertion(t.TempDir(), mkAssert(t, AssertToolRegistered, "no-such-tool"), facts); ok {
		t.Error("未注册工具应判 false")
	}
	if _, err := EvaluateAssertion(t.TempDir(), mkAssert(t, AssertToolRegistered, "x"), nil); err != ErrAssertionNeedsRunner {
		t.Errorf("facts=nil 应返回 ErrAssertionNeedsRunner，得 %v", err)
	}
	// 期望值以「JSON 装成字符串」承载（生成器对端点的写法）
	if ok, err := EvaluateAssertion(t.TempDir(),
		mkAssert(t, AssertToolOutputJSONEq, "{\"encoded\":\"NBSWY3DP\"}"), facts); err != nil || !ok {
		t.Errorf("tool_output_json_eq 应判 true: %v %v", ok, err)
	}
	if ok, err := EvaluateAssertion(t.TempDir(),
		mkAssert(t, AssertToolOutputJSONEq, "{\"encoded\":\"WRONG\"}"), facts); err != nil || ok {
		t.Errorf("值不符应判 false: %v %v", ok, err)
	}
	// 原生 JSON 对象形式的期望值同样支持（子集等值：期望键须相等）
	if ok, err := EvaluateAssertion(t.TempDir(),
		mkAssert(t, AssertToolOutputJSONEq, map[string]any{"encoded": "NBSWY3DP"}), facts); err != nil || !ok {
		t.Errorf("原生对象期望应判 true: %v %v", ok, err)
	}
	// 工具没产出：判 false 而非 error
	if ok, err := EvaluateAssertion(t.TempDir(),
		mkAssert(t, AssertToolOutputJSONEq, "{\"a\":1}"), &RunnerFacts{ToolName: "x"}); err != nil || ok {
		t.Errorf("空输出应判 false 无错误: %v %v", ok, err)
	}
	// 输出不是 JSON：判 error（判定器无法背书），不静默算失败
	if _, err := EvaluateAssertion(t.TempDir(),
		mkAssert(t, AssertToolOutputJSONEq, "{\"a\":1}"),
		&RunnerFacts{ToolName: "x", ToolOutput: "not json at all"}); err == nil {
		t.Error("非 JSON 输出应返回错误")
	}
}

func TestEvaluateAllReportsFirstFailure(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := `[{"file_exists":"a.txt"},{"file_contains":["a.txt","nope"]},{"file_exists":"b.txt"}]`
	var asserts []Assertion
	if err := json.Unmarshal([]byte(src), &asserts); err != nil {
		t.Fatal(err)
	}
	ok, fail, err := EvaluateAll(root, asserts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ok || fail == "" {
		t.Errorf("应报首个失败断言，得 ok=%v fail=%q", ok, fail)
	}
	if !strings.Contains(fail, "file_contains") {
		t.Errorf("首个失败应是 file_contains，得 %q", fail)
	}
	all := `[{"file_exists":"a.txt"}]`
	asserts = nil
	_ = json.Unmarshal([]byte(all), &asserts)
	if ok, fail, err := EvaluateAll(root, asserts, nil); !ok || fail != "" || err != nil {
		t.Errorf("全对应 ok=true: %v %q %v", ok, fail, err)
	}
}

func TestLookupJSONPath(t *testing.T) {
	doc := map[string]any{}
	if err := json.Unmarshal([]byte(`{"a":{"b":[1,2,{"c":"x"}]},"n":3,"arr":[{"k":1}]}`), &doc); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		path string
		want any
		ok   bool
	}{
		{"a.b[2].c", "x", true},
		{"n", float64(3), true},
		{"arr[0].k", float64(1), true},
		{"", doc, true},
		{"a.b", []any{float64(1), float64(2), map[string]any{"c": "x"}}, true},
		{"a.missing", nil, false},
		{"a.b[9]", nil, false},
		{"n.x", nil, false},
		{"a.b[", nil, false},
	}
	for _, c := range cases {
		got, ok := LookupJSONPath(doc, c.path)
		if ok != c.ok {
			t.Errorf("LookupJSONPath(%q) ok = %v, 期望 %v", c.path, ok, c.ok)
			continue
		}
		if ok && !jsonValuesEqual(got, c.want) {
			t.Errorf("LookupJSONPath(%q) = %v, 期望 %v", c.path, got, c.want)
		}
	}
}

func TestSafeJoin(t *testing.T) {
	root, err := filepath.Abs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := safeJoin(root, "/abs/path"); err == nil {
		t.Error("绝对路径应拒绝")
	}
	if _, err := safeJoin(root, "ok/file.txt"); err != nil {
		t.Errorf("正常相对路径应通过: %v", err)
	}
	if _, err := safeJoin(root, "../escape"); err == nil {
		t.Error(".. 逃逸应拒绝")
	}
	if p, err := safeJoin(root, "./x"); err != nil || !strings.HasPrefix(p, root) {
		t.Errorf("./x 处理异常: %v %v", p, err)
	}
}
