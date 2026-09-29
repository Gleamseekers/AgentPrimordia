// detector_test.go — 轨迹缺口检测器测试
package create

import (
	"context"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/tools/intelligence"
)

// TestTraceGapDetector_Detect 测试检测失败模式
func TestTraceGapDetector_Detect(t *testing.T) {
	ctx := context.Background()
	detector := NewTraceGapDetector()

	now := time.Now()
	trace := []intelligence.ToolCallRecord{
		{ToolName: "shell", Error: "permission denied: /etc/secret", Success: false, Timestamp: now},
		{ToolName: "file", Error: "no such file: /tmp/missing.txt", Success: false, Timestamp: now.Add(time.Second)},
		{ToolName: "shell", Error: "permission denied: /root/key", Success: false, Timestamp: now.Add(2 * time.Second)},
		{ToolName: "web", Success: true, Timestamp: now.Add(3 * time.Second)}, // 成功，不应计入
		{ToolName: "shell", Error: "permission denied: /var/log", Success: false, Timestamp: now.Add(4 * time.Second)},
	}

	gaps, err := detector.Detect(ctx, trace)
	if err != nil {
		t.Fatalf("检测失败: %v", err)
	}

	// 应检测到 2 种缺口
	if len(gaps) != 2 {
		t.Errorf("期望 2 个缺口，实际=%d", len(gaps))
	}

	// 查找 permission 缺口
	var permGap *intelligence.GapCandidate
	for i := range gaps {
		if gaps[i].Key == "missing_permission" {
			permGap = &gaps[i]
			break
		}
	}

	if permGap == nil {
		t.Fatal("期望找到 missing_permission 缺口")
	}
	if permGap.Count != 3 {
		t.Errorf("期望 missing_permission 出现 3 次，实际=%d", permGap.Count)
	}

	// 查找 file 缺口
	var fileGap *intelligence.GapCandidate
	for i := range gaps {
		if gaps[i].Key == "missing_file" {
			fileGap = &gaps[i]
			break
		}
	}

	if fileGap == nil {
		t.Fatal("期望找到 missing_file 缺口")
	}
	if fileGap.Count != 1 {
		t.Errorf("期望 missing_file 出现 1 次，实际=%d", fileGap.Count)
	}
}

// TestTraceGapDetector_EmptyTrace 测试空轨迹
func TestTraceGapDetector_EmptyTrace(t *testing.T) {
	ctx := context.Background()
	detector := NewTraceGapDetector()

	gaps, err := detector.Detect(ctx, []intelligence.ToolCallRecord{})
	if err != nil {
		t.Fatalf("检测失败: %v", err)
	}

	if len(gaps) != 0 {
		t.Errorf("期望 0 个缺口，实际=%d", len(gaps))
	}
}

// TestTraceGapDetector_AllSuccess 测试全部成功
func TestTraceGapDetector_AllSuccess(t *testing.T) {
	ctx := context.Background()
	detector := NewTraceGapDetector()

	trace := []intelligence.ToolCallRecord{
		{ToolName: "shell", Success: true},
		{ToolName: "file", Success: true},
	}

	gaps, err := detector.Detect(ctx, trace)
	if err != nil {
		t.Fatalf("检测失败: %v", err)
	}

	if len(gaps) != 0 {
		t.Errorf("期望 0 个缺口，实际=%d", len(gaps))
	}
}

// TestExtractGapKey 测试缺口键提取
func TestExtractGapKey(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"permission denied: /etc/secret", "missing_permission"},
		{"no such file: /tmp/missing.txt", "missing_file"},
		{"connection refused", "missing_service"},
		{"timeout exceeded", "missing_timeout_handler"},
		{"unsupported operation", "missing_capability"},
		{"not implemented yet", "missing_feature"},
		{"unknown error", "unknown error"}, // 无匹配，取前 20 字符
	}

	for _, tt := range tests {
		key := extractGapKey(tt.input)
		if key != tt.expected {
			t.Errorf("输入=%q，期望=%s，实际=%s", tt.input, tt.expected, key)
		}
	}
}
