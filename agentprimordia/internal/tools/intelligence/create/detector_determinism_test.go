// detector_determinism_test.go — extractGapKey 确定性回归。
//
// 背景（tool-intelligence 演示缺口段运行间抖动）：patterns 为 map，
// Go map 迭代顺序随机——同一错误消息命中多个 pattern 时（如
// "parse error: invalid CSV format" 同时含 "parse error" 与
// "invalid format"）返回的 gap key 不确定，导致缺口去重计数与键名
// 运行间抖动（3↔4）。修复为有序切片首匹配，本测试固防。
package create

import (
	"testing"
)

// TestExtractGapKey_DeterministicMultiPattern 多模式命中的消息必须
// 稳定返回同一键（有序首匹配：声明序在前的 pattern 优先）。
func TestExtractGapKey_DeterministicMultiPattern(t *testing.T) {
	cases := []struct {
		name string
		err  string
		want string
	}{
		{"parse+invalid", "parse error: invalid CSV format", "missing_parser"},           // "parse error" 声明序先于 "invalid format"
		{"notfound+nosuchfile", "no such file: not found", "missing_resource"},           // "not found" 先于 "no such file"
		{"timeout+refused", "connection refused: timeout", "missing_service"},            // "connection refused" 先于 "timeout"
		{"unsupported+notimpl", "not implemented: unsupported", "missing_capability"}, // "unsupported" 声明序先于 "not implemented"
	}
	for _, tc := range cases {
		// 同一输入重复 50 次必须完全一致（map 迭代下约 50% 概率漂移）。
		for i := 0; i < 50; i++ {
			if got := extractGapKey(tc.err); got != tc.want {
				t.Fatalf("%s: extractGapKey(%q) = %q, want %q（第 %d 次调用，非确定性回归）", tc.name, tc.err, got, tc.want, i)
			}
		}
	}
}

// TestExtractGapKey_SinglePatternUnchanged 单模式命中行为不变。
func TestExtractGapKey_SinglePatternUnchanged(t *testing.T) {
	cases := map[string]string{
		"resource not found":        "missing_resource",
		"no such file or directory": "missing_file",
		"permission denied":         "missing_permission",
		"connection refused":        "missing_service",
		"operation timeout":         "missing_timeout_handler",
		"unsupported operation":     "missing_capability",
		"not implemented yet":       "missing_feature",
		"out of memory":             "missing_resource_limit",
		"":                          "",
		"totally unknown error xyz": "totally unknown erro",
	}
	for in, want := range cases {
		if got := extractGapKey(in); got != want {
			t.Errorf("extractGapKey(%q) = %q, want %q", in, got, want)
		}
	}
}
