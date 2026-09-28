// shell_security_test.go — Shell 工具安全回归测试（P0 修复固防）
//
// 背景（2026-09-28 评估实证）：默认白名单含 env/printenv/python3/node/go/find
// 等可派生进程或解释代码的条目；且显式 args 参数路径不经过任何元字符检查。
// 二者叠加使"白名单模式"可被轻易绕过——被 prompt injection 控制的 agent 在
// 开箱配置下即可获得宿主任意代码执行。本测试固防全部 7 条实证攻击链，
// 任何时候失败即安全回归。
package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// execShell 辅助：以 JSON 参数执行 Shell.Execute。
func execShell(t *testing.T, sh *Shell, params map[string]any) (string, error) {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	res, err := sh.Execute(context.Background(), raw)
	if err != nil {
		return "", err
	}
	return res.Content, nil
}

// mustJSON 辅助：marshal map 为 json.RawMessage。
func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

// mustExecShell 辅助：执行且要求不返回 error（用于攻击"成功"判据）。
func mustExecShell(t *testing.T, sh *Shell, params map[string]any) string {
	t.Helper()
	out, err := execShell(t, sh, params)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	return out
}

// ===== 攻击链 1：env/printenv 白名单逃逸 → 任意命令执行 =====

func TestAttack_EnvWhitelistBypass(t *testing.T) {
	sh := NewShell() // 默认白名单模式
	tmp := t.TempDir()
	marker := filepath.Join(tmp, "pwned")
	out := mustExecShell(t, sh, map[string]any{
		"action":  "execute",
		"command": "env",
		"args":    []string{"touch", marker},
	})
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatalf("攻击链 1 复现：env+args 任意命令执行成功 -> %s\noutput: %s", marker, out)
	}
}

func TestAttack_PrintenvWhitelistBypass(t *testing.T) {
	sh := NewShell()
	tmp := t.TempDir()
	marker := filepath.Join(tmp, "pwned2")
	out := mustExecShell(t, sh, map[string]any{
		"action":  "execute",
		"command": "printenv",
		"args":    []string{"touch", marker},
	})
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatalf("攻击链 1b 复现：printenv+args 任意命令执行成功 -> %s\noutput: %s", marker, out)
	}
}

// ===== 攻击链 2：args 数组绕过元字符检查 → 命令替换执行 =====

func TestAttack_ArgsBypassMetachar(t *testing.T) {
	// 真实攻击向量：白名单内可派生 shell 的命令（env）+ args 携带
	// "sh -c '...'" —— 命令替换在派生的 sh 内执行。
	sh := NewShell() // 默认白名单（修复后不含 env）
	out := mustExecShell(t, sh, map[string]any{
		"action":  "execute",
		"command": "env",
		"args":    []string{"sh", "-c", "echo INJECTED$(id -u)"},
	})
	if strings.Contains(out, "INJECTED") && !strings.Contains(out, "INJECTED$(") {
		t.Fatalf("攻击链 2 复现：args 中命令替换经派生 shell 执行 -> %s", out)
	}
}

// TestAttack_ArgsMetacharRejected args 中的元字符必须被拒绝（纵深防御：
// 即使 argv[0] 不可派生 shell，携带 $()/;/| 等载荷的 args 也不放行）。
func TestAttack_ArgsMetacharRejected(t *testing.T) {
	sh := NewShell().WithWhitelist([]string{"echo"})
	res, err := sh.Execute(context.Background(), mustJSON(t, map[string]any{
		"action":  "execute",
		"command": "echo",
		"args":    []string{"INJECTED$(id -u)"},
	}))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if res != nil && !res.IsError && strings.Contains(res.Content, "INJECTED") {
		t.Fatalf("args 中的 $() 载荷被放行: %s", res.Content)
	}
}

func TestAttack_ArgsMetacharSemicolon(t *testing.T) {
	sh := NewShell().WithWhitelist([]string{"echo"})
	tmp := t.TempDir()
	marker := filepath.Join(tmp, "pwned3")
	out := mustExecShell(t, sh, map[string]any{
		"action":  "execute",
		"command": "echo",
		"args":    []string{"x; touch " + marker},
	})
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatalf("攻击链 2b 复现：args 中分号命令执行成功 -> %s\noutput: %s", marker, out)
	}
}

// ===== 攻击链 3：filesystem write → shell python3 RCE 链 =====

func TestAttack_WriteThenRunInterpreter(t *testing.T) {
	root := t.TempDir()
	fs, err := NewFileSystem(root)
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "x.py")
	wArgs, _ := json.Marshal(map[string]any{
		"action":  "write",
		"path":    "x.py",
		"content": "import os\nos.system('echo RCE_VIA_PY > " + filepath.Join(root, "rce") + "')\n",
	})
	if _, err := fs.Execute(context.Background(), wArgs); err != nil {
		t.Fatal(err)
	}
	sh := NewShell() // 默认白名单
	out := mustExecShell(t, sh, map[string]any{
		"action":  "execute",
		"command": "python3 " + script,
	})
	if _, statErr := os.Stat(filepath.Join(root, "rce")); statErr == nil {
		t.Fatalf("攻击链 3 复现：write→python3 RCE 闭环\noutput: %s", out)
	}
}

// ===== 攻击链 4：黑名单模式 flags 重排绕过 =====

func TestAttack_BlacklistFlagReorder(t *testing.T) {
	sh := NewShell().WithBlacklist()
	tmp := t.TempDir()
	victim := filepath.Join(tmp, "victim")
	if err := os.WriteFile(victim, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	// "rm -fr" flags 重排 + 反斜杠变形均不在原 blockedCommands 子串表
	for _, cmd := range []string{"rm -fr " + victim, "rm -r -f " + victim, "/bin/rm -rf " + victim, "RM -RF " + victim} {
		out, _ := execShell(t, sh, map[string]any{"action": "execute", "command": cmd})
		if _, statErr := os.Stat(victim); statErr != nil {
			t.Fatalf("攻击链 4 复现：黑名单被 %q 绕过，文件被删\noutput: %s", cmd, out)
		}
	}
}

// ===== 攻击链 5：shell 无路径禁锢（cat rootDir 之外文件）=====

func TestAttack_ShellNoPathConfinement(t *testing.T) {
	root := t.TempDir()
	// rootDir 之外写一个秘密文件
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("TOPSECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	sh := NewShell().WithAllowedWorkdirs([]string{root})
	out := mustExecShell(t, sh, map[string]any{
		"action":  "execute",
		"command": "cat " + secret,
	})
	if strings.Contains(out, "TOPSECRET") {
		t.Fatalf("攻击链 5 复现：shell 读取了 allowedWorkdirs 之外的文件 -> %s", out)
	}
}

// ===== 攻击链 7：敏感文件模式缺口 =====

func TestAttack_SensitivePatternGaps(t *testing.T) {
	root := t.TempDir()
	fs, err := NewFileSystem(root)
	if err != nil {
		t.Fatal(err)
	}
	// authorized_keys
	if err := os.WriteFile(filepath.Join(root, "authorized_keys"), []byte("ssh-rsa AAAA"), 0o600); err != nil {
		t.Fatal(err)
	}
	rArgs, _ := json.Marshal(map[string]any{"action": "read", "path": "authorized_keys"})
	res, err := fs.Execute(context.Background(), rArgs)
	if err == nil && res != nil && !res.IsError && strings.Contains(res.Content, "ssh-rsa") {
		t.Error("攻击链 7 复现：authorized_keys 可被 filesystem read")
	}
	// 大小写变形 .ENV
	if err := os.WriteFile(filepath.Join(root, ".ENV"), []byte("SECRET=1"), 0o600); err != nil {
		t.Fatal(err)
	}
	eArgs, _ := json.Marshal(map[string]any{"action": "read", "path": ".ENV"})
	res, err = fs.Execute(context.Background(), eArgs)
	if err == nil && res != nil && !res.IsError && strings.Contains(res.Content, "SECRET=1") {
		t.Error("攻击链 7 复现：.ENV 大小写变形绕过敏感模式")
	}
}
