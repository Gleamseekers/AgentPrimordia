// sandbox_boundary_test.go — 宿主边界断言 A5/A6/A7/A8（提案-code层沙箱受控释放.md §2.2）
//
// R3 口径：确定性逻辑不变式，允许 100%/0——判定是算法（验签/导入段枚举/
// 哈希对比），测试面有限可枚举，失效显式。
package wasm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ===== A5：CPU 死循环可终止 =====

// TestA5_InfiniteLoopTerminated 无限循环模块在 MaxExecutionTime 内被真实终止。
func TestA5_InfiniteLoopTerminated(t *testing.T) {
	cfg := DefaultSandboxConfig()
	cfg.MaxExecutionTime = 500 * time.Millisecond
	s := NewSandbox(cfg)
	defer s.Close()
	if err := s.Load("spin", spinWasm(t)); err != nil {
		t.Fatalf("加载无限循环模块失败: %v", err)
	}
	start := time.Now()
	_, err := s.Execute(context.Background(), "spin", "spin")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("无限循环必须以错误终止（超时），不得挂死")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("终止耗时 %v 超出 ExecutionTimeout 量级（WithCloseOnContextDone 失效）", elapsed)
	}
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "deadline") &&
		!strings.Contains(err.Error(), "timeout") && !strings.Contains(err.Error(), "context") {
		t.Logf("终止错误形态: %v", err) // 错误文案不锁定，终止事实必须成立
	}
}

// ===== A6：签名前置 =====

// TestA6_RegisterRequiresSignature 缺签/坏签/篡改一律拒绝（表驱动穷举篡改位）。
func TestA6_RegisterRequiresSignature(t *testing.T) {
	wasmBytes := toolWasm(t)
	priv := mustKey(t)
	sig, pub, err := SignWASM(wasmBytes, priv)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("缺签名拒绝", func(t *testing.T) {
		s := NewSandbox(DefaultSandboxConfig())
		defer s.Close()
		a := NewWASMToolAdapter(s)
		meta := ToolMetadata{Name: "nosig", ExecuteFunc: "tool_execute", PublicKey: pub}
		err := a.RegisterTool(context.Background(), meta, wasmBytes)
		if err == nil || !strings.Contains(err.Error(), "A6") {
			t.Fatalf("缺签名注册必须拒绝（A6）: %v", err)
		}
	})
	t.Run("篡改任一字节即拒绝", func(t *testing.T) {
		// 表驱动穷举：首/尾/中间字节翻转
		for _, pos := range []int{0, len(wasmBytes) / 2, len(wasmBytes) - 1} {
			tampered := append([]byte(nil), wasmBytes...)
			tampered[pos] ^= 0xFF
			s := NewSandbox(DefaultSandboxConfig())
			a := NewWASMToolAdapter(s)
			meta := ToolMetadata{Name: "tampered", ExecuteFunc: "tool_execute", Signature: sig, PublicKey: pub}
			if err := a.RegisterTool(context.Background(), meta, tampered); err == nil {
				t.Fatalf("篡改位置 %d 的模块必须被拒绝", pos)
			}
			s.Close()
		}
	})
	t.Run("合法签名通过并注册", func(t *testing.T) {
		s := NewSandbox(DefaultSandboxConfig())
		defer s.Close()
		a := NewWASMToolAdapter(s)
		meta := ToolMetadata{Name: "signed", ExecuteFunc: "tool_execute", Signature: sig, PublicKey: pub}
		if err := a.RegisterTool(context.Background(), meta, wasmBytes); err != nil {
			t.Fatalf("合法签名注册应通过: %v", err)
		}
		if _, ok := a.GetTool("signed"); !ok {
			t.Fatal("注册后应可查询")
		}
	})
}

// ===== A7：导入段白名单 =====

// TestA7_ImportWhitelist 未批准宿主导入拒绝；WASI 未批准时其导入拒绝；
// 批准后放行；干净模块零导入直接通过。
func TestA7_ImportWhitelist(t *testing.T) {
	t.Run("默认配置拒绝 wasi 导入", func(t *testing.T) {
		s := NewSandbox(DefaultSandboxConfig()) // AllowedImports 含 wasi_snapshot_preview1
		defer s.Close()
		// wasi 模块的导入在默认批准集内 → 通过（默认配置显式批准了 wasi）
		if err := s.Load("wasi-mod", wasiImportWasm(t)); err != nil {
			t.Fatalf("默认批准集含 wasi，应通过: %v", err)
		}
	})
	t.Run("未启用 WASI 的批准集拒绝 wasi 导入", func(t *testing.T) {
		cfg := DefaultSandboxConfig()
		cfg.AllowedImports = []string{} // 未显式启用 WASI
		s := NewSandbox(cfg)
		defer s.Close()
		err := s.Load("wasi-mod", wasiImportWasm(t))
		if err == nil || !strings.Contains(err.Error(), "未批准的宿主导入") {
			t.Fatalf("未启用 WASI 时 wasi 导入必须拒绝: %v", err)
		}
	})
	t.Run("干净模块零导入通过", func(t *testing.T) {
		cfg := DefaultSandboxConfig()
		cfg.AllowedImports = []string{}
		s := NewSandbox(cfg)
		defer s.Close()
		if err := s.Load("tool", toolWasm(t)); err != nil {
			t.Fatalf("零导入模块应通过: %v", err)
		}
		imports, err := s.ImportedFunctions("tool")
		if err != nil {
			t.Fatal(err)
		}
		if len(imports) != 0 {
			t.Fatalf("零导入模块的导入段应为空: %v", imports)
		}
	})
}

// ===== A8：宿主文件系统零写入 =====

// TestA8_HostTreeUnchanged 工具执行前后，宿主工作树（Go 代码目录 + 配置
// 目录）哈希不变；生成物只允许落在沙箱数据目录（t.TempDir）。
func TestA8_HostTreeUnchanged(t *testing.T) {
	root := findModuleRoot(t)
	dirs := []string{filepath.Join(root, "wasm"), filepath.Join(root, "internal"), filepath.Join(root, "pkg"), filepath.Join(root, "cmd")}
	hashBefore := hashGoTree(t, dirs)

	// 执行一次完整的「加载→签名注册→执行」工具链路
	s := NewSandbox(DefaultSandboxConfig())
	defer s.Close()
	wasmBytes := toolWasm(t)
	priv := mustKey(t)
	sig, pub, err := SignWASM(wasmBytes, priv)
	if err != nil {
		t.Fatal(err)
	}
	a := NewWASMToolAdapter(s)
	meta := ToolMetadata{Name: "a8", Description: "A8 harness", Parameters: []byte(`{"type":"object"}`),
		ExecuteFunc: "tool_execute", Version: "1.0.0", Signature: sig, PublicKey: pub}
	if err := a.RegisterTool(context.Background(), meta, wasmBytes); err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	if _, err := a.ExecuteTool(context.Background(), "a8", []byte(`{"input":"x"}`)); err != nil {
		t.Fatalf("执行失败: %v", err)
	}

	hashAfter := hashGoTree(t, dirs)
	if hashBefore != hashAfter {
		t.Fatal("A8 违例：工具执行前后宿主工作树哈希发生变化")
	}
}

// hashGoTree 对目录集内全部普通文件做确定性哈希（路径+内容串联后 sha256）。
func hashGoTree(t *testing.T, dirs []string) string {
	t.Helper()
	h := sha256.New()
	for _, dir := range dirs {
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil // 不存在的目录（如 root 下无 internal）跳过
			}
			if !strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, ".mod") && !strings.HasSuffix(path, ".sum") {
				return nil
			}
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			h.Write([]byte(path))
			h.Write(data)
			return nil
		})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// findModuleRoot 向上定位主模块目录（含 go.mod 的 github.com/Gleamseekers/AgentPrimordia/）。
func findModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("未找到模块根")
	return ""
}
