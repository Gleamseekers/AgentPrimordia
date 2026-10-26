// host_boundary_test.go — 宿主边界断言 A1/A2/A3（提案-code层沙箱受控释放.md §2.2）
//
// INV-0：宿主进程零写入、零编译、零加载 agent 生成代码；agent 生成代码的
// 唯一合法执行位置是 wazero WASM 沙箱。
// R3 口径：确定性逻辑不变式——判定是算法（导入图比对/源码枚举/符号扫描），
// 集合有限可穷举，0 违例是结构性质而非统计宣称。
package security

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/tools/builtin"
)

// wasmWhitelistPrefixes 允许 import wazero 的包路径前缀（AGENTS.md §2.1 白名单边界）：
// 根 wasm 模块 github.com/Gleamseekers/AgentPrimordia/wasm（仓库 wasm/ 目录）与主模块内 wasm 包 github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/wasm。
var wasmWhitelistPrefixes = []string{
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/wasm", // 主模块内 wasm 包
	"github.com/Gleamseekers/AgentPrimordia/wasm",               // 根 wasm 模块
}

// moduleRoots 参与扫描的 Go 模块目录（相对仓库根）。
//
// 必须覆盖 go.work 的全部 use 模块，依次为：主模块（源码在 agentprimordia/ 子目录，
// 仓库根的 bench/、extensions/ 及根目录自身均无 .go 文件，故扫该目录即覆盖主模块全量）、
// Operator 独立模块、Gateway 独立模块、pgvector 独立模块、根 wasm 模块。
//
// 历史缺陷：此处只列主模块与根 wasm 模块，gateway / pgvector / operator 三个模块
// 完全在扫描面之外——白名单外的包 import wazero 无从发现，A1 的"全量枚举"名不副实。
var moduleRoots = []struct{ dir, module string }{
	{"agentprimordia", "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia"},
	{"agentprimordia/operator", "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/operator"},
	{"gateway", "github.com/Gleamseekers/AgentPrimordia/gateway"},
	{"pgvector", "github.com/Gleamseekers/AgentPrimordia/pgvector"},
	{"wasm", "github.com/Gleamseekers/AgentPrimordia/wasm"},
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("未找到仓库根")
	return ""
}

// ===== A1：wazero 依赖边界封闭 =====

// wazeroModuleRoot wazero 模块的导入路径根。
const wazeroModuleRoot = "github.com/tetratelabs/wazero"

// importsWazero 判断单个 import 路径是否引入 wazero 本体或其子包。
//
// 历史缺陷：原实现用 `imp == "github.com/tetratelabs/wazero"` 精确匹配，只覆盖
// 根包。白名单外的包改用 "github.com/tetratelabs/wazero/api"（或 /sys、/internal/...）
// 即可完全绕过 A1——而这并非假设：主模块 wasm 包（agentprimordia/wasm/sandbox.go）
// 实际 import 的正是 wazero 与 wazero/api 两个路径。凡 wazero 子包都必须纳入判定。
func importsWazero(imp string) bool {
	return imp == wazeroModuleRoot || strings.HasPrefix(imp, wazeroModuleRoot+"/")
}

// TestA1_WazeroImportMatcher A1 的匹配器必须能识别 wazero 子包，且不得误判
// 同名前缀的非 wazero 依赖——否则整条边界断言可被换一个导入路径绕过。
func TestA1_WazeroImportMatcher(t *testing.T) {
	for _, imp := range []string{
		"github.com/tetratelabs/wazero",
		"github.com/tetratelabs/wazero/api",
		"github.com/tetratelabs/wazero/sys",
		"github.com/tetratelabs/wazero/internal/wasm",
		"github.com/tetratelabs/wazero/wasi_snapshot_preview1",
	} {
		if !importsWazero(imp) {
			t.Errorf("importsWazero(%q) = false，wazero 子包必须被识别（否则 A1 可被绕过）", imp)
		}
	}
	for _, imp := range []string{
		"github.com/tetratelabs/wazerox",
		"github.com/tetratelabs/wazero-fork",
		"github.com/other/wazero",
		"github.com/tetratelabs/wazer",
		"wazero",
	} {
		if importsWazero(imp) {
			t.Errorf("importsWazero(%q) = true，非 wazero 依赖被误判", imp)
		}
	}
	// 白名单前缀判定同向：白名单包自身与其子包都必须命中。
	for _, p := range []string{
		"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/wasm",
		"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/wasm/sub",
		"github.com/Gleamseekers/AgentPrimordia/wasm",
	} {
		if !hasWhitelistPrefix(p) {
			t.Errorf("白名单前缀漏判: %s", p)
		}
	}
	for _, p := range []string{
		"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/wasmx",
		"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/tools",
	} {
		if hasWhitelistPrefix(p) {
			t.Errorf("白名单前缀误判: %s", p)
		}
	}
}

// TestA1_WazeroImportBoundary 全量枚举各模块的包导入图：
// import wazero（含子包）的包集合必须 ⊆ 白名单前缀集合（白名单外出现即失败）。
func TestA1_WazeroImportBoundary(t *testing.T) {
	root := findRepoRoot(t)
	for _, m := range moduleRoots {
		cmd := exec.Command("go", "list", "-json", "./...")
		cmd.Dir = filepath.Join(root, m.dir)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("go list（%s）失败: %v: %s", m.module, err, stderr.String())
		}
		// 流式解码 go list -json 的对象序列
		dec := json.NewDecoder(bytes.NewReader(out))
		for {
			var pkg struct {
				ImportPath string   `json:"ImportPath"`
				Imports    []string `json:"Imports"`
			}
			if derr := dec.Decode(&pkg); derr != nil {
				break
			}
			if pkg.ImportPath == "" {
				continue
			}
			hasWazero := false
			for _, imp := range pkg.Imports {
				if importsWazero(imp) {
					hasWazero = true
					break
				}
			}
			if !hasWazero {
				continue
			}
			if !hasWhitelistPrefix(pkg.ImportPath) {
				t.Errorf("A1 违例：白名单外包 %s import 了 wazero", pkg.ImportPath)
			}
		}
	}
}

func hasWhitelistPrefix(importPath string) bool {
	for _, p := range wasmWhitelistPrefixes {
		if importPath == p || strings.HasPrefix(importPath, p+"/") {
			return true
		}
	}
	return false
}

// ===== A2：零动态加载 =====

// TestA2_NoDynamicPluginLoading 全仓源码不出现 plugin.Open；
// 主模块二进制符号表不含 plugin.Open（无 cgo 激活路径的证据链）。
func TestA2_NoDynamicPluginLoading(t *testing.T) {
	root := findRepoRoot(t)

	// ① 源码枚举（全部 .go，排除 vendor/ 检索噪声——本仓无 vendor）
	cmd := exec.Command("grep", "-rl", "plugin.Open", "--include=*.go", root)
	out, err := cmd.Output()
	if err == nil && len(bytes.TrimSpace(out)) > 0 {
		for _, f := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if strings.Contains(f, "_test.go") && strings.Contains(f, "host_boundary_test") {
				continue // 本测试文件自身的字符串字面量
			}
			t.Errorf("A2 违例：源码出现 plugin.Open: %s", f)
		}
	}

	// ② 构建产物符号扫描：CGO 关闭构建主 CLI，符号表不得含 plugin.Open
	bin := filepath.Join(t.TempDir(), "ap")
	build := exec.Command("go", "build", "-o", bin, "./cmd/ap")
	build.Dir = filepath.Join(root, "agentprimordia")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if berr := build.Run(); berr != nil {
		t.Fatalf("构建主 CLI 失败: %v", berr)
	}
	nm := exec.Command("go", "tool", "nm", bin)
	nmOut, err := nm.Output()
	if err != nil {
		t.Fatalf("符号表扫描失败: %v", err)
	}
	if bytes.Contains(nmOut, []byte("plugin.Open")) {
		t.Error("A2 违例：构建产物符号表含 plugin.Open")
	}
}

// ===== A3：零宿主派生执行（code_execution 默认禁用 + 开关不可被 agent 写入）=====

// TestA3_CodeExecutionDefaultOff code_execution 工具默认禁用：
// 未设 AP_ALLOW_CODE_EXECUTION 时执行必须被拒绝且错误信息给出开关名。
func TestA3_CodeExecutionDefaultOff(t *testing.T) {
	t.Setenv("AP_ALLOW_CODE_EXECUTION", "")
	ex := builtin.NewCodeExecution()
	res, err := ex.Execute(context.Background(), []byte(`{"language":"python","code":"print(1)"}`))
	if err == nil && res != nil && !res.IsError {
		t.Fatal("A3 违例：默认禁用态下 code_execution 不应执行")
	}
	joined := ""
	if err != nil {
		joined += err.Error()
	}
	if res != nil {
		joined += res.Content
	}
	if !strings.Contains(joined, "AP_ALLOW_CODE_EXECUTION") {
		t.Fatalf("拒绝信息应指向开关（可审计性）: %q", joined)
	}
}

// grepScanRoots 静态扫描的目录根（相对仓库根）。agentprimordia/operator 嵌套在
// agentprimordia/ 下，已被前者覆盖，不重复列入以免同一文件被重复报告。
var grepScanRoots = []string{"agentprimordia", "gateway", "pgvector", "wasm"}

// grepRepo 在仓库内执行 grep，返回匹配行。
//
// fail-closed：退出码 1（无匹配）返回空切片；其余失败（grep 缺失、路径不可读、
// 退出码 ≥2）一律 t.Fatalf。静态断言绝不允许把"扫描失败"静默当作"无违例"——
// 那是断言失效最常见的形态：看起来一直是绿的。
func grepRepo(t *testing.T, root, pattern string, targets ...string) []string {
	t.Helper()
	args := append([]string{"-rn", pattern, "--include=*.go"}, targets...)
	cmd := exec.Command("grep", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 1 {
			return nil // 无匹配
		}
		t.Fatalf("静态扫描失败（fail-closed，不得当作无违例）: grep %q: %v", pattern, err)
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

// TestA3_ConfigSurfaceCannotEnable 开关不可被 agent 可写配置面触达：
// 枚举各模块生产代码的全部 os.Setenv 写入点——不得有任何路径设置
// AP_ALLOW_CODE_EXECUTION（agent 写配置→重启执行的对抗形态被静态排除）。
//
// 历史缺陷：原实现只扫 agentprimordia 一个目录，且 `if err != nil { return }`
// 把 grep 的任何失败（含 grep 不存在、目录不可读）都当成"无写入点"——
// 扫描面残缺 + 失败即放行，两者叠加使这条断言可以在完全失效的情况下保持绿色。
func TestA3_ConfigSurfaceCannotEnable(t *testing.T) {
	root := findRepoRoot(t)
	targets := make([]string, 0, len(grepScanRoots))
	for _, d := range grepScanRoots {
		targets = append(targets, filepath.Join(root, d))
	}
	for _, line := range grepRepo(t, root, "Setenv", targets...) {
		if strings.Contains(line, "_test.go") {
			continue
		}
		if strings.Contains(line, "AP_ALLOW_CODE_EXECUTION") {
			t.Errorf("A3 违例：生产代码存在 AP_ALLOW_CODE_EXECUTION 写入点: %s", line)
		}
	}
}

// TestA3_GenericEnvSetterCannotReachGate A3 的第二形态：攻击面不止"字面写入
// 开关名"，还包括**通用 env 写入器**——EnvBackend.SetSecret 执行
// os.Setenv(prefix + strings.ToUpper(key))，key 由调用方给定。若前缀可被设为
// "AP_"，则 SetSecret("allow_code_execution","true") 即拼出受控开关名。
// 字符串 grep 对这一形态完全失明，故用行为断言钉住默认前缀下的不可能性。
func TestA3_GenericEnvSetterCannotReachGate(t *testing.T) {
	t.Setenv("AP_ALLOW_CODE_EXECUTION", "")
	be := NewEnvBackend() // 默认前缀 AP_SECRET_
	keys := []string{"allow_code_execution", "ALLOW_CODE_EXECUTION", "allow-code-execution"}
	t.Cleanup(func() {
		for _, k := range keys {
			os.Unsetenv(EnvPrefix + strings.ToUpper(k))
		}
	})
	for _, key := range keys {
		if err := be.SetSecret(context.Background(), key, "true"); err != nil {
			t.Fatalf("SetSecret(%q) 失败: %v", key, err)
		}
		if got := os.Getenv("AP_ALLOW_CODE_EXECUTION"); got == "true" {
			t.Fatalf("A3 违例：EnvBackend.SetSecret(%q) 可写入受控开关 AP_ALLOW_CODE_EXECUTION", key)
		}
	}

	// 自定义前缀会破坏上述性质，故生产代码不得使用自定义前缀构造 EnvBackend。
	// 模式带左括号：只匹配调用点与函数声明，避免命中文档注释里的名字
	// （注释行无 "("，否则会把 "// NewEnvBackendWithPrefix 创建..." 误判为违例）。
	root := findRepoRoot(t)
	for _, line := range grepRepo(t, root, "NewEnvBackendWithPrefix(", filepath.Join(root, "agentprimordia")) {
		if strings.Contains(line, "_test.go") || strings.Contains(line, "func NewEnvBackendWithPrefix") {
			continue
		}
		t.Errorf("A3 违例：生产代码使用自定义前缀构造 EnvBackend（可借此拼出受控开关名）: %s", line)
	}
}
