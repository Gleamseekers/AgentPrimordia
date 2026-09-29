package builtin

// shell_blacklist_test.go — 黑名单模式（WithBlacklist）Deprecated 标注 /
// 启用告警 / 匹配加固（P2 安全修复，TDD：先红后绿）。
//
// 背景：黑名单模式根本弱于白名单（已知可被命令变形绕过），但为避免破坏
// pkg 稳定性承诺暂保留 API。修复：按仓库 deprecation 规范标注
// "Deprecated: ... Removed in v8.0.0"，启用时打 Warning 日志说明风险，
// 并将 token basename 匹配加固到覆盖大小写 / 路径前缀 / 命令变体
// （如 mkfs.ext4 此前后缀名不在集合中而被绕过）。

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/internal/tools"
)

// execShellResult 执行 shell 工具并返回结果（辅助，与现有测试风格一致）
func execShellResult(t *testing.T, sh *Shell, params map[string]any) *tools.Result {
	t.Helper()
	args, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal args error: %v", err)
	}
	result, err := sh.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	return result
}

// TestShell_WithBlacklist_DeprecatedAnnotation 验证 WithBlacklist 带有
// 符合仓库规范的 Deprecated 标注（// Deprecated: + // Removed in vX.Y）。
func TestShell_WithBlacklist_DeprecatedAnnotation(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "shell.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse shell.go error: %v", err)
	}

	var doc *ast.CommentGroup
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if ok && fn.Name.Name == "WithBlacklist" {
			doc = fn.Doc
		}
		return true
	})

	if doc == nil {
		t.Fatal("WithBlacklist 缺少文档注释（应有 Deprecated 标注）")
	}
	text := doc.Text()
	if !strings.Contains(text, "Deprecated:") {
		t.Errorf("WithBlacklist 注释缺少 'Deprecated:' 标注，当前注释:\n%s", text)
	}
	if !strings.Contains(text, "Removed in v") {
		t.Errorf("WithBlacklist 注释缺少 'Removed in vX.Y' 时点标注（仓库 deprecation 规范），当前注释:\n%s", text)
	}
}

// TestShell_WithBlacklist_EmitsWarning 验证启用黑名单模式时发出风险告警。
func TestShell_WithBlacklist_EmitsWarning(t *testing.T) {
	orig := warnBlacklistRisk
	defer func() { warnBlacklistRisk = orig }()

	warned := 0
	warnBlacklistRisk = func() { warned++ }

	NewShell().WithBlacklist()
	if warned != 1 {
		t.Errorf("expected exactly 1 warning on WithBlacklist, got %d", warned)
	}

	// 白名单模式不应触发黑名单告警
	warned = 0
	NewShell().WithWhitelist([]string{"ls"})
	if warned != 0 {
		t.Errorf("WithWhitelist must not emit blacklist warning, got %d", warned)
	}
}

// TestShell_Blacklist_BlocksDangerousCommands 验证黑名单模式仍阻止
// rm / mkfs / dd 及常见变形（大小写、路径前缀、命令变体）。
func TestShell_Blacklist_BlocksDangerousCommands(t *testing.T) {
	cases := []struct {
		name    string
		command string
	}{
		{"rm", "rm -rf /tmp/victim"},
		{"rm flags reorder", "rm -fr /tmp/victim"},
		{"rm path prefix", "/bin/rm -rf /tmp/victim"},
		{"rm case", "RM -RF /tmp/victim"},
		{"mkfs", "mkfs /dev/sda"},
		{"mkfs variant", "mkfs.ext4 /dev/sda"},
		{"dd", "dd if=/dev/zero of=/dev/sda"},
		{"dd path prefix", "/usr/bin/dd if=/dev/zero of=./x"},
		{"shutdown", "shutdown -h now"},
	}

	sh := NewShell().WithBlacklist()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := execShellResult(t, sh, map[string]any{
				"action":  "execute",
				"command": tc.command,
			})
			if !result.IsError {
				t.Errorf("command %q should be blocked by blacklist, got: %s", tc.command, result.Content)
			}
			if !strings.Contains(strings.ToLower(result.Content), "blocked") {
				t.Errorf("expected 'blocked' in error for %q, got: %s", tc.command, result.Content)
			}
		})
	}
}

// TestShell_Blacklist_AllowsSafeCommand 验证黑名单模式下安全命令仍可执行
// （告警不应误伤正常路径）。
func TestShell_Blacklist_AllowsSafeCommand(t *testing.T) {
	// 抑制告警噪音（不影响被测行为）
	orig := warnBlacklistRisk
	defer func() { warnBlacklistRisk = orig }()
	warnBlacklistRisk = func() {}

	sh := NewShell().WithBlacklist()
	result := execShellResult(t, sh, map[string]any{
		"action":  "execute",
		"command": "echo hello",
	})
	if result.IsError {
		t.Errorf("safe command should pass in blacklist mode, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "hello") {
		t.Errorf("expected 'hello' in output, got: %s", result.Content)
	}
}
