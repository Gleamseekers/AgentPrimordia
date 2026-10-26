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

// ===== 攻击链 5b：相对路径穿越（../）绕过路径禁锢 =====

// TestAttack_ShellRelativePathTraversal 固化攻击链 5 的遗漏形态：攻击链 5 只用
// 绝对路径取证，而 shell.go 的禁锢循环以 looksLikeAbsolutePath 为前置门控——
// "../../etc/passwd" 形态上不是绝对路径，被 `continue` 整条跳过。cmd.Dir 恰是
// 受禁锢的 workdir 本身，相对路径从它出发即可越界。原注释断言"相对路径由 cmd.Dir
// 决定，不构成逃逸向量"，该论证错误：逃逸取决于解析基准，而基准正是被禁锢的目录。
func TestAttack_ShellRelativePathTraversal(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("TOPSECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	// 构造从 root 出发的相对穿越路径（如 ../002/secret.txt）
	rel, err := filepath.Rel(root, secret)
	if err != nil {
		t.Fatal(err)
	}
	sh := NewShell().WithAllowedWorkdirs([]string{root})
	out := mustExecShell(t, sh, map[string]any{
		"action":  "execute",
		"command": "cat " + rel,
		"workdir": root,
	})
	if strings.Contains(out, "TOPSECRET") {
		t.Fatalf("攻击链 5b 复现：相对路径 %q 越狱读取了 allowedWorkdirs 之外的文件 -> %s", rel, out)
	}
}

// ===== 攻击链 5c：省略 workdir 使禁锢检查整体不执行 =====

// TestAttack_ShellOmittedWorkdirFallsBackToJail workdir 参数缺省时的执行基准。
// 原实现的禁锢校验被 `workdir != ""` 门控：省略 workdir 时整条检查不执行，
// 且 cmd.Dir 不设置，命令相对宿主进程 cwd 解析——等于没有 jail。
// 修复后必须回退到 allowedWorkdirs[0] 执行。以 jail 内 marker 可读为判据
// （若仍继承宿主 cwd，则读不到该文件）——该判据与进程 cwd 无关，确定可复现。
func TestAttack_ShellOmittedWorkdirFallsBackToJail(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "marker.txt"), []byte("INJAIL"), 0o600); err != nil {
		t.Fatal(err)
	}
	sh := NewShell().WithAllowedWorkdirs([]string{root})
	out := mustExecShell(t, sh, map[string]any{
		"action":  "execute",
		"command": "cat marker.txt",
	})
	if !strings.Contains(out, "INJAIL") {
		t.Fatalf("省略 workdir 时未回退到禁锢目录执行（应能读到 jail 内 marker.txt）: %s", out)
	}
}

// TestAttack_ShellOmittedWorkdirStillJailed 省略 workdir 时禁锢必须**显式拒绝**
// 越界路径，而不只是"碰巧没读到"（后者可能因进程 cwd 不同而文件不存在，
// 是假阴性）。拒绝文案是禁锢生效的直接证据。
func TestAttack_ShellOmittedWorkdirStillJailed(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("TOPSECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(root, secret)
	if err != nil {
		t.Fatal(err)
	}
	sh := NewShell().WithAllowedWorkdirs([]string{root})
	out := mustExecShell(t, sh, map[string]any{
		"action":  "execute",
		"command": "cat " + rel,
	})
	if strings.Contains(out, "TOPSECRET") {
		t.Fatalf("攻击链 5c 复现：省略 workdir 后仍可越狱读取 -> %s", out)
	}
	if !strings.Contains(out, "outside the allowed directories") {
		t.Fatalf("省略 workdir 时禁锢检查未生效（应显式拒绝越界路径）: %s", out)
	}
}

// TestShell_RelativePathInsideJailAllowed 正向对照：jail 内的合法相对路径必须放行。
// 固化"只堵穿越、不堵合法相对路径"——防止修复退化成把一切相对路径拒尽，
// 那会让 shell 工具在默认 toolkit 下基本不可用。
func TestShell_RelativePathInsideJailAllowed(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "ok.txt"), []byte("INSIDE"), 0o600); err != nil {
		t.Fatal(err)
	}
	sh := NewShell().WithAllowedWorkdirs([]string{root})
	for _, cmd := range []string{
		"cat sub/ok.txt",
		"cat ./sub/ok.txt",
		"cat sub/../sub/ok.txt", // 含 .. 但回落 jail 内
	} {
		out := mustExecShell(t, sh, map[string]any{
			"action":  "execute",
			"command": cmd,
			"workdir": root,
		})
		if !strings.Contains(out, "INSIDE") {
			t.Errorf("jail 内合法相对路径被误拒: %q -> %s", cmd, out)
		}
	}
}

// TestResolveWithinWorkdir 禁锢判定的纯函数表驱动测试（跨平台：路径用
// filepath.Join 构造，不硬编码 "/"）。
func TestResolveWithinWorkdir(t *testing.T) {
	sep := string(filepath.Separator)
	root := sep + filepath.Join("srv", "app")
	allowed := []string{root}
	cases := []struct {
		name    string
		tok     string
		workdir string
		want    bool
	}{
		{"jail 内相对路径", filepath.Join("sub", "ok.txt"), root, true},
		{"jail 内点前缀", "." + sep + filepath.Join("sub", "ok.txt"), root, true},
		{"jail 内含 .. 但回落", filepath.Join("sub", "..", "sub", "ok.txt"), root, true},
		{"相对穿越逃逸", filepath.Join("..", "..", "etc", "passwd"), root, false},
		{"单层相对穿越", filepath.Join("..", "secret"), root, false},
		{"绝对路径在 jail 内", filepath.Join(root, "sub", "ok.txt"), root, true},
		{"绝对路径逃逸", sep + filepath.Join("etc", "passwd"), root, false},
		{"前缀边界不越界", root + "-secret" + sep + "x", root, false},
		{"workdir 缺省回退 jail 内", filepath.Join("sub", "ok.txt"), "", true},
		{"workdir 缺省仍堵穿越", filepath.Join("..", "..", "etc", "passwd"), "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := resolveWithinWorkdir(c.tok, c.workdir, allowed); got != c.want {
				t.Errorf("resolveWithinWorkdir(%q, workdir=%q) = %v, want %v", c.tok, c.workdir, got, c.want)
			}
		})
	}
	// 未配置禁锢时无从判定，一律放行（保持既有"无 jail"语义）。
	if !resolveWithinWorkdir(sep+filepath.Join("etc", "passwd"), "", nil) {
		t.Error("未配置 allowedWorkdirs 时不应拒绝（无 jail 语义）")
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
