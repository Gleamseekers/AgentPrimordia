// registering_test.go — RegisteringCreator 单元测试
package intelligence

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"agentprimordia/internal/tools"
)

// === 桩实现 ===

// mockCreator 模拟 ToolCreator，返回预设的 ToolArtifact
type mockCreator struct {
	artifact *ToolArtifact
	err      error
	called   int
}

func (m *mockCreator) Create(_ context.Context, _ GapCandidate) (*ToolArtifact, error) {
	m.called++
	return m.artifact, m.err
}

// === 测试 ===

// TestRegisteringCreator_RegistersTool 验证 Create 后工具被注册到 Registry
func TestRegisteringCreator_RegistersTool(t *testing.T) {
	dir := t.TempDir()
	reg := tools.NewRegistry()

	art := &ToolArtifact{
		ID:          "tool-1",
		Name:        "count_lines",
		Description: "统计文件行数",
		ArtifactSHA: "sha-abc",
		Artifact:    []byte("#!/bin/sh\nwc -l \"$1\""),
	}
	base := &mockCreator{artifact: art}
	rc := NewRegisteringCreator(base, reg, dir)

	gap := GapCandidate{Kind: "tool_create", Key: "count_lines", Count: 3}
	result, err := rc.Create(context.Background(), gap)
	if err != nil {
		t.Fatalf("Create 不应返回错误: %v", err)
	}
	if result == nil {
		t.Fatal("Create 不应返回 nil artifact")
	}
	if result.Name != "count_lines" {
		t.Errorf("artifact 名称 = %q，期望 %q", result.Name, "count_lines")
	}

	// 验证注册到 Registry
	tool, ok := reg.Get("count_lines")
	if !ok {
		t.Fatal("工具应已注册到 Registry")
	}
	if tool.Name() != "count_lines" {
		t.Errorf("Registry 中工具名 = %q，期望 %q", tool.Name(), "count_lines")
	}
	if tool.Description() != "统计文件行数" {
		t.Errorf("工具描述 = %q，期望 %q", tool.Description(), "统计文件行数")
	}
}

// TestRegisteringCreator_ScriptFileWritten 验证脚本文件写入磁盘且权限正确
func TestRegisteringCreator_ScriptFileWritten(t *testing.T) {
	dir := t.TempDir()
	reg := tools.NewRegistry()

	scriptContent := []byte("#!/bin/sh\necho hello")
	art := &ToolArtifact{
		Name:        "hello_tool",
		Description: "输出 hello",
		Artifact:    scriptContent,
	}
	base := &mockCreator{artifact: art}
	rc := NewRegisteringCreator(base, reg, dir)

	_, err := rc.Create(context.Background(), GapCandidate{Kind: "tool_create", Key: "hello"})
	if err != nil {
		t.Fatalf("Create 不应返回错误: %v", err)
	}

	// 验证文件存在
	scriptPath := filepath.Join(dir, ".intel-tools", "hello_tool")
	info, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("脚本文件应存在: %v", err)
	}

	// 验证文件权限为 0755（可执行）
	if info.Mode().Perm() != os.FileMode(0755) {
		t.Errorf("脚本权限 = %o，期望 0755", info.Mode().Perm())
	}

	// 验证文件内容与 artifact 一致
	data, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("读取脚本文件失败: %v", err)
	}
	if string(data) != string(scriptContent) {
		t.Errorf("脚本内容 = %q，期望 %q", string(data), string(scriptContent))
	}
}

// TestRegisteringCreator_ToolRetrievableByName 验证注册后可通过名称检索
func TestRegisteringCreator_ToolRetrievableByName(t *testing.T) {
	dir := t.TempDir()
	reg := tools.NewRegistry()

	art := &ToolArtifact{
		Name:        "grep_errors",
		Description: "搜索错误日志",
		Artifact:    []byte("#!/bin/sh\ngrep error \"$1\""),
	}
	base := &mockCreator{artifact: art}
	rc := NewRegisteringCreator(base, reg, dir)

	_, err := rc.Create(context.Background(), GapCandidate{Kind: "tool_create", Key: "grep"})
	if err != nil {
		t.Fatalf("Create 不应返回错误: %v", err)
	}

	// 通过 Registry.Get 检索
	tool, ok := reg.Get("grep_errors")
	if !ok {
		t.Fatal("应能通过名称检索到工具")
	}

	// 验证 Parameters 返回正确的 JSON Schema
	params := tool.Parameters()
	var schema map[string]any
	if err := json.Unmarshal(params, &schema); err != nil {
		t.Fatalf("Parameters() 应返回有效 JSON: %v", err)
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("Parameters schema 应包含 properties")
	}
	if _, ok := props["args"]; !ok {
		t.Fatal("Parameters schema 应包含 args 字段")
	}
}

// TestRegisteringCreator_ExecuteRunsScript 验证 Execute 能执行脚本并返回输出
func TestRegisteringCreator_ExecuteRunsScript(t *testing.T) {
	dir := t.TempDir()
	reg := tools.NewRegistry()

	// 写一个简单的 sh 脚本：输出参数
	art := &ToolArtifact{
		Name:        "echo_tool",
		Description: "回显参数",
		Artifact:    []byte("#!/bin/sh\necho \"$@\""),
	}
	base := &mockCreator{artifact: art}
	rc := NewRegisteringCreator(base, reg, dir)

	_, err := rc.Create(context.Background(), GapCandidate{Kind: "tool_create", Key: "echo"})
	if err != nil {
		t.Fatalf("Create 不应返回错误: %v", err)
	}

	// 获取注册的工具并执行
	tool, ok := reg.Get("echo_tool")
	if !ok {
		t.Fatal("工具应已注册")
	}

	args := json.RawMessage(`{"args":"hello world"}`)
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute 不应返回错误: %v", err)
	}
	if result.IsError {
		t.Fatalf("Execute 不应标记为错误: %s", result.Content)
	}
	if result.Content != "hello world" {
		t.Errorf("Execute 输出 = %q，期望 %q", result.Content, "hello world")
	}
}

// TestRegisteringCreator_BaseReturnsNil 验证基础生成器返回 nil 时正确处理
func TestRegisteringCreator_BaseReturnsNil(t *testing.T) {
	dir := t.TempDir()
	reg := tools.NewRegistry()

	base := &mockCreator{artifact: nil, err: nil}
	rc := NewRegisteringCreator(base, reg, dir)

	result, err := rc.Create(context.Background(), GapCandidate{Kind: "tool_create", Key: "noop"})
	if err != nil {
		t.Fatalf("Create 不应返回错误: %v", err)
	}
	if result != nil {
		t.Error("Create 应返回 nil artifact")
	}
	// 不应有任何工具被注册
	if reg.Count() != 0 {
		t.Errorf("Registry 应有 0 个工具，实际 %d", reg.Count())
	}
}

// TestRegisteringCreator_BaseReturnsError 验证基础生成器返回错误时正确处理
func TestRegisteringCreator_BaseReturnsError(t *testing.T) {
	dir := t.TempDir()
	reg := tools.NewRegistry()

	base := &mockCreator{artifact: nil, err: os.ErrNotExist}
	rc := NewRegisteringCreator(base, reg, dir)

	_, err := rc.Create(context.Background(), GapCandidate{Kind: "tool_create", Key: "fail"})
	if err == nil {
		t.Fatal("Create 应返回错误")
	}
	// 不应有任何工具被注册
	if reg.Count() != 0 {
		t.Errorf("Registry 应有 0 个工具，实际 %d", reg.Count())
	}
}
