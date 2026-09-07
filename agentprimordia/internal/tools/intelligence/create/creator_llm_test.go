// creator_llm_test.go — LLMCreator 单元测试
package create

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"agentprimordia/internal/tools/intelligence"
)

// mockLLM 模拟 LLM 补全器
type mockLLM struct {
	result string
	err    error
}

func (m *mockLLM) Complete(_ context.Context, _ string) (string, error) {
	return m.result, m.err
}

func TestLLMCreator_Success(t *testing.T) {
	script := "#!/bin/sh\necho 'hello world'"
	llm := &mockLLM{result: script}
	creator := NewLLMCreator(llm)

	gap := intelligence.GapCandidate{
		Key:         "test_tool",
		Kind:        "missing",
		Count:       5,
		SampleError: "command not found",
		FirstSeen:   time.Now().Add(-time.Hour),
		LastSeen:    time.Now(),
	}

	artifact, err := creator.Create(context.Background(), gap)
	if err != nil {
		t.Fatalf("期望无错误，得到: %v", err)
	}
	if artifact == nil {
		t.Fatal("期望返回产物，得到 nil")
	}
	if artifact.ID != "auto-test_tool" {
		t.Errorf("期望 ID=auto-test_tool，得到: %s", artifact.ID)
	}
	if artifact.Name != "test_tool" {
		t.Errorf("期望 Name=test_tool，得到: %s", artifact.Name)
	}
	if artifact.ArtifactSHA == "" {
		t.Error("期望 ArtifactSHA 非空")
	}
	if !strings.Contains(string(artifact.Artifact), "echo 'hello world'") {
		t.Errorf("期望产物包含脚本内容，得到: %s", string(artifact.Artifact))
	}
}

func TestLLMCreator_FallbackOnError(t *testing.T) {
	llm := &mockLLM{err: fmt.Errorf("LLM 服务不可用")}
	creator := NewLLMCreator(llm)

	// 使用已知硬编码键触发 fallback
	gap := intelligence.GapCandidate{
		Key:         "csv_stats",
		Kind:        "missing",
		Count:       3,
		SampleError: "no such tool",
	}

	artifact, err := creator.Create(context.Background(), gap)
	if err != nil {
		t.Fatalf("期望 fallback 成功，得到错误: %v", err)
	}
	if artifact == nil {
		t.Fatal("期望 fallback 返回产物，得到 nil")
	}
	// 验证使用了硬编码模板
	if !strings.Contains(string(artifact.Artifact), "awk") {
		t.Errorf("期望 fallback 使用 csv_stats 模板，得到: %s", string(artifact.Artifact))
	}
}

func TestLLMCreator_FallbackOnEmptyResult(t *testing.T) {
	llm := &mockLLM{result: "   "} // 空白输出
	creator := NewLLMCreator(llm)

	gap := intelligence.GapCandidate{
		Key:  "log_parser",
		Kind: "missing",
	}

	artifact, err := creator.Create(context.Background(), gap)
	if err != nil {
		t.Fatalf("期望 fallback 成功，得到错误: %v", err)
	}
	if !strings.Contains(string(artifact.Artifact), "grep") {
		t.Errorf("期望 fallback 使用 log_parser 模板，得到: %s", string(artifact.Artifact))
	}
}

func TestLLMCreator_EmptyKey(t *testing.T) {
	llm := &mockLLM{result: "#!/bin/sh\necho ok"}
	creator := NewLLMCreator(llm)

	gap := intelligence.GapCandidate{Key: ""}

	_, err := creator.Create(context.Background(), gap)
	if err == nil {
		t.Fatal("期望空键返回错误")
	}
	if !strings.Contains(err.Error(), "空") {
		t.Errorf("期望错误信息包含'空'，得到: %v", err)
	}
}

func TestLLMCreator_ArtifactFields(t *testing.T) {
	script := "#!/bin/sh\nwc -l \"$1\" 2>/dev/null || echo 0"
	llm := &mockLLM{result: script}
	creator := NewLLMCreator(llm)

	gap := intelligence.GapCandidate{
		Key:         "line_counter",
		Kind:        "utility",
		Count:       10,
		SampleError: "no line count tool",
	}

	artifact, err := creator.Create(context.Background(), gap)
	if err != nil {
		t.Fatalf("期望无错误，得到: %v", err)
	}

	// 验证 ID 格式
	if artifact.ID != "auto-line_counter" {
		t.Errorf("ID 不匹配: %s", artifact.ID)
	}
	// 验证 Name
	if artifact.Name != "line_counter" {
		t.Errorf("Name 不匹配: %s", artifact.Name)
	}
	// 验证 Description 包含 LLM 标记
	if !strings.Contains(artifact.Description, "LLM") {
		t.Errorf("Description 应包含 LLM 标记: %s", artifact.Description)
	}
	// 验证 SHA 非空且长度正确（SHA-256 hex = 64 字符）
	if len(artifact.ArtifactSHA) != 64 {
		t.Errorf("ArtifactSHA 长度应为 64，得到: %d", len(artifact.ArtifactSHA))
	}
}

func TestLLMCreator_MarkdownStrip(t *testing.T) {
	// LLM 返回 markdown 包裹的脚本
	script := "```sh\n#!/bin/sh\necho hello\n```"
	llm := &mockLLM{result: script}
	creator := NewLLMCreator(llm)

	gap := intelligence.GapCandidate{Key: "md_test", Kind: "test"}

	artifact, err := creator.Create(context.Background(), gap)
	if err != nil {
		t.Fatalf("期望无错误，得到: %v", err)
	}
	content := string(artifact.Artifact)
	if strings.Contains(content, "```") {
		t.Errorf("期望去除 markdown 包裹，得到: %s", content)
	}
	if !strings.Contains(content, "#!/bin/sh") {
		t.Errorf("期望保留脚本内容，得到: %s", content)
	}
}
