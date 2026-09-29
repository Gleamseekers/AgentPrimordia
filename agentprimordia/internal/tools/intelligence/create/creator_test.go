// creator_test.go — 工具生成器测试
package create

import (
	"context"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/tools/intelligence"
)

// TestLifecycleCreator_Create 测试工具生成
func TestLifecycleCreator_Create(t *testing.T) {
	ctx := context.Background()
	creator := NewLifecycleCreator()

	gap := intelligence.GapCandidate{
		Kind:        "missing_tool",
		Key:         "csv_parser",
		Count:       5,
		SampleError: "unsupported format: csv",
	}

	artifact, err := creator.Create(ctx, gap)
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}

	if artifact == nil {
		t.Fatal("期望生成工具产物，实际为 nil")
	}

	// 验证 ID
	if artifact.ID != "auto-csv_parser" {
		t.Errorf("期望 ID=auto-csv_parser，实际=%s", artifact.ID)
	}

	// 验证名称
	if artifact.Name != "csv_parser" {
		t.Errorf("期望 Name=csv_parser，实际=%s", artifact.Name)
	}

	// 验证 SHA 非空
	if artifact.ArtifactSHA == "" {
		t.Error("期望 ArtifactSHA 非空")
	}

	// 验证工件内容非空
	if len(artifact.Artifact) == 0 {
		t.Error("期望工件内容非空")
	}
}

// TestLifecycleCreator_EmptyKey 测试空缺口键报错
func TestLifecycleCreator_EmptyKey(t *testing.T) {
	ctx := context.Background()
	creator := NewLifecycleCreator()

	gap := intelligence.GapCandidate{Key: ""}

	_, err := creator.Create(ctx, gap)
	if err == nil {
		t.Error("期望空缺口键返回错误，实际为 nil")
	}
}
