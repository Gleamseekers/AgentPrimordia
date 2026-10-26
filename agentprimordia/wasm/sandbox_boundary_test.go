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
	"fmt"
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

// TestA7_ImportWhitelist 未批准宿主导入拒绝；默认配置（空批准集）拒绝 wasi；
// 显式批准后放行；干净模块零导入直接通过。
func TestA7_ImportWhitelist(t *testing.T) {
	t.Run("默认配置拒绝 wasi 导入", func(t *testing.T) {
		// 默认配置 AllowedImports 为空集——与 A4「WASI 默认关闭」同向。
		// 历史缺陷：默认曾放行 wasi_snapshot_preview1，本子测试**名为"拒绝"
		// 而断言体为"应通过"**，名与体相互矛盾，且与 A4 的确定性断言冲突。
		s := NewSandbox(DefaultSandboxConfig())
		defer s.Close()
		err := s.Load("wasi-mod", wasiImportWasm(t))
		if err == nil {
			t.Fatal("A4/A7 违例：默认配置不得放行 wasi 导入（WASI 默认关闭）")
		}
		if !strings.Contains(err.Error(), "未批准的宿主导入") {
			t.Fatalf("拒绝原因应指向导入段白名单: %v", err)
		}
	})
	t.Run("显式批准 wasi 后放行", func(t *testing.T) {
		cfg := DefaultSandboxConfig()
		cfg.AllowedImports = []string{"wasi_snapshot_preview1"} // 显式 opt-in
		s := NewSandbox(cfg)
		defer s.Close()
		if err := s.Load("wasi-mod", wasiImportWasm(t)); err != nil {
			t.Fatalf("显式批准 wasi 后应放行: %v", err)
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

// TestA8_HostTreeUnchanged 工具执行前后，宿主工作树（Go 代码目录）哈希不变；
// 生成物只允许落在沙箱数据目录（t.TempDir）。
//
// 历史缺陷（v7.5.2 整改）：原实现用 findModuleRoot 向上找 go.mod——本仓主模块
// go.mod 在**仓库根**（源码在 agentprimordia/ 子目录），于是拼出
// root/{wasm,internal,pkg,cmd}；其中 internal/pkg/cmd 在仓库根并不存在，被
// hashGoTree 的 `if err != nil { return nil }` 静默跳过，A8 实际只哈希了
// root/wasm——即沙箱模块自身（信任方，非被保护对象）。守卫集与意图脱靶，
// 断言退化为同义反复：执行路径本就不碰文件系统，哈希必然相等。
func TestA8_HostTreeUnchanged(t *testing.T) {
	root := findRepoRoot(t)
	dirs := hostTreeDirs(root)
	hashBefore, err := hashGoTree(dirs)
	if err != nil {
		t.Fatalf("A8 前置哈希失败（守卫目标必须存在且可读）: %v", err)
	}

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

	hashAfter, err := hashGoTree(dirs)
	if err != nil {
		t.Fatalf("A8 后置哈希失败（守卫目标必须存在且可读）: %v", err)
	}
	if hashBefore != hashAfter {
		t.Fatal("A8 违例：工具执行前后宿主工作树哈希发生变化")
	}
}

// TestA8_DetectorCanFail 证明 A8 的检测器本身能失败——否则"哈希不变"是无意义的永真断言。
// 三条负样本：内容变化、新增文件、守卫目标缺失（后者必须报错而非静默通过）。
func TestA8_DetectorCanFail(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "internal")
	if err := os.MkdirAll(filepath.Join(target, "tools"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(target, "tools", "a.go")
	if err := os.WriteFile(file, []byte("package tools\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	h1, err := hashGoTree([]string{target})
	if err != nil {
		t.Fatalf("首次哈希应成功: %v", err)
	}

	// ① 内容变化必须改变哈希
	if err := os.WriteFile(file, []byte("package tools\n// mutated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h2, err := hashGoTree([]string{target})
	if err != nil {
		t.Fatalf("二次哈希应成功: %v", err)
	}
	if h1 == h2 {
		t.Error("A8 检测器失效：文件内容变化后哈希未变（断言永真）")
	}

	// ② 新增文件必须改变哈希
	if err := os.WriteFile(filepath.Join(target, "tools", "b.go"), []byte("package tools\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h3, err := hashGoTree([]string{target})
	if err != nil {
		t.Fatalf("三次哈希应成功: %v", err)
	}
	if h3 == h2 {
		t.Error("A8 检测器失效：新增文件后哈希未变（断言永真）")
	}

	// ③ 守卫目标缺失必须报错（fail-closed）——原实现此处静默跳过，
	//    使得"目录不存在"与"无违例"不可区分，A8 因而可被无意间架空。
	if _, err := hashGoTree([]string{filepath.Join(base, "nonexistent")}); err == nil {
		t.Error("A8 检测器失效：守卫目标缺失时未报错（fail-open）")
	}
}

// hostTreeDirs A8 守卫的宿主工作树目录（相对仓库根）。这些目录承载 agent
// 框架源码，是 INV-0「宿主零写入」真正要保护的对象。
func hostTreeDirs(root string) []string {
	return []string{
		filepath.Join(root, "agentprimordia", "internal"),
		filepath.Join(root, "agentprimordia", "pkg"),
		filepath.Join(root, "agentprimordia", "cmd"),
	}
}

// hashGoTree 对目录集内全部 .go/.mod/.sum 文件做确定性哈希（路径+内容串联后 sha256）。
//
// fail-closed：守卫目标缺失、不是目录、或遍历失败一律返回 error——
// 调用方必须显式失败，不允许把"扫不到"当成"没违规"。
func hashGoTree(dirs []string) (string, error) {
	h := sha256.New()
	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil {
			return "", fmt.Errorf("A8 守卫目标不可访问: %s: %w", dir, err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("A8 守卫目标不是目录: %s", dir)
		}
		werr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
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
		if werr != nil {
			return "", fmt.Errorf("A8 遍历守卫目标失败: %s: %w", dir, werr)
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// findRepoRoot 向上定位仓库根（含 .git 的目录）。不用 go.mod 判定——本仓主模块
// go.mod 就在仓库根，而 A8 的守卫目标位于仓库根的 agentprimordia/ 子目录下，
// 两者不是同一层，用 go.mod 定位曾导致守卫集拼错一层。
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
	t.Fatal("未找到仓库根（.git）")
	return ""
}
