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
var moduleRoots = []struct{ dir, module string }{
	{"agentprimordia", "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia"}, // dir = 仓库内目录名（迁移不变），module = 模块路径（已迁移）
	{"wasm", "github.com/Gleamseekers/AgentPrimordia/wasm"}, // 根 wasm 模块（目录名 wasm 不变）
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

// TestA1_WazeroImportBoundary 全量枚举三个模块的包导入图：
// import wazero 的包集合必须 ⊆ 白名单前缀集合（白名单外出现即失败）。
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
				if imp == "github.com/tetratelabs/wazero" {
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

// TestA3_ConfigSurfaceCannotEnable 开关不可被 agent 可写配置面触达：
// 枚举主模块生产代码的全部 os.Setenv 写入点——不得有任何路径设置
// AP_ALLOW_CODE_EXECUTION（agent 写配置→重启执行的对抗形态被静态排除）。
func TestA3_ConfigSurfaceCannotEnable(t *testing.T) {
	root := findRepoRoot(t)
	cmd := exec.Command("grep", "-rn", "Setenv", "--include=*.go",
		filepath.Join(root, "agentprimordia"))
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return // 无任何 Setenv 写入点 = 配置面天然封闭
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "AP_ALLOW_CODE_EXECUTION") && !strings.Contains(line, "_test.go") {
			t.Errorf("A3 违例：生产代码存在 AP_ALLOW_CODE_EXECUTION 写入点: %s", line)
		}
	}
}
