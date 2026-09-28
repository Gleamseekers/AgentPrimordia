// scope_extraction_security_test.go — ScopePolicy 路径提取回归（P0 修复固防）
//
// 背景（2026-09-28 评估实证）：extractPathFromArgs 只识别 6 个固定 key、
// 只看顶层、只取第一个匹配；对 filePath/file/嵌套/MCP 风格参数返回空，
// executor 层 ScopePolicy 检查被静默跳过（fail-open）。修复为完整 JSON
// 递归遍历 + 全 key 集合，本测试固防。
package tools

import (
	"encoding/json"
	"testing"
)

// TestScopeExtraction_KeyCoverage 常见路径参数名（含 camelCase 与简写）都必须被识别。
func TestScopeExtraction_KeyCoverage(t *testing.T) {
	for _, key := range []string{
		"path", "file_path", "filePath", "file", "target_dir", "workdir",
		"directory", "output_path", "src", "dst", "filename", "dir",
	} {
		args, err := json.Marshal(map[string]any{key: "/etc/passwd"})
		if err != nil {
			t.Fatal(err)
		}
		if got := extractPathFromArgs(string(args)); got == "" {
			t.Errorf("key %q 未被 extractPathFromArgs 识别（ScopePolicy 将被静默跳过）", key)
		}
	}
}

// TestScopeExtraction_Nested 嵌套对象中的路径字段必须被识别。
func TestScopeExtraction_Nested(t *testing.T) {
	cases := []string{
		`{"request":{"path":"/etc/passwd"}}`,
		`{"input":{"filePath":"/etc/passwd"}}`,
		`{"args":{"file":"/etc/passwd"}}`,
		`{"a":{"b":{"path":"/etc/passwd"}}}`,
	}
	for _, c := range cases {
		if got := extractPathFromArgs(c); got == "" {
			t.Errorf("嵌套路径未被识别: %s", c)
		}
	}
}

// TestScopeExtraction_ArrayElement 数组元素中的路径字段必须被识别。
func TestScopeExtraction_ArrayElement(t *testing.T) {
	c := `{"files":[{"path":"/etc/passwd"}]}`
	if got := extractPathFromArgs(c); got == "" {
		t.Errorf("数组元素中的路径未被识别: %s", c)
	}
}

// TestScopeExtraction_NoPathReturnsEmpty 无路径字段时返回空（由调用方决定 fail 策略）。
func TestScopeExtraction_NoPathReturnsEmpty(t *testing.T) {
	if got := extractPathFromArgs(`{"command":"ls","args":["-la"]}`); got != "" {
		t.Errorf("无路径字段应返回空, got %q", got)
	}
}
