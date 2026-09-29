// registering_test.go — RegisteringCreator 单元测试（P1 INV-0 安全加固）
//
// 背景（修复前）：RegisteringCreator 将 LLM/自动生成的 artifact 以 0755
// 写入磁盘并注册为经 `sh <path>` 执行的工具——正是 AGENTS.md §2.3 INV-0
// 禁止的"宿主进程运行期写入并加载 agent 生成代码"（唯一合法执行位置是
// wazero WASM 沙箱）。
//
// 修复后语义：
//   - 未配置验签器 → 一律拒绝注册（fail-closed）；
//   - 工件哈希未锚定/被篡改 → 拒绝；
//   - 验签未通过 → 拒绝；
//   - 验签通过 → 以 0644（数据文件，无可执行位）落盘并注册；
//   - 注册工具的 Execute 绝不派生宿主进程，仅可经注入的
//     ArtifactExecutor（唯一合法实现：wasm 沙箱）执行。
package intelligence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools"
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

// passVerifier 总是通过的验签桩（模拟组装根绑定的 TrustChain 验签）。
type passVerifier struct{}

func (passVerifier) VerifyArtifact(_ *ToolArtifact) error { return nil }

// failVerifier 总是失败的验签桩。
type failVerifier struct{}

func (failVerifier) VerifyArtifact(_ *ToolArtifact) error {
	return errors.New("验签失败：签名者不在钉扎集合内")
}

// echoExecutor 回显 executor 桩（模拟 wasm 沙箱执行通道）。
func echoExecutor(_ context.Context, art *ToolArtifact, args json.RawMessage) (*tools.Result, error) {
	return tools.NewResult("executed:" + art.Name + ":" + string(args)), nil
}

// sha256Hex 计算工件内容的 sha256（十六进制）。
func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// === 测试 ===

// TestRegisteringCreator_UnsignedArtifactNotRegistered 固化 INV-0：
// 未配置验签器时，未签名工件不得注册、不得落盘。
func TestRegisteringCreator_UnsignedArtifactNotRegistered(t *testing.T) {
	dir := t.TempDir()
	reg := tools.NewRegistry()

	art := &ToolArtifact{
		ID:          "tool-1",
		Name:        "count_lines",
		Description: "统计文件行数",
		ArtifactSHA: sha256Hex([]byte("#!/bin/sh\nwc -l \"$1\"")),
		Artifact:    []byte("#!/bin/sh\nwc -l \"$1\""),
	}
	base := &mockCreator{artifact: art}
	rc := NewRegisteringCreator(base, reg, dir) // 未配置验签器

	result, err := rc.Create(context.Background(), GapCandidate{Kind: "tool_create", Key: "count_lines"})
	if err == nil {
		t.Fatal("未配置验签器时应拒绝注册（INV-0 fail-closed）")
	}
	if result != nil {
		t.Error("拒绝注册时不应返回 artifact")
	}
	if reg.Count() != 0 {
		t.Errorf("Registry 应有 0 个工具，实际 %d", reg.Count())
	}
	// 不得有任何文件落盘
	if _, statErr := os.Stat(filepath.Join(dir, ".intel-tools")); !os.IsNotExist(statErr) {
		t.Error("未验签通过时不得创建 .intel-tools 目录/写入工件")
	}
}

// TestRegisteringCreator_VerificationFailureNotRegistered 验签失败 → 拒绝
func TestRegisteringCreator_VerificationFailureNotRegistered(t *testing.T) {
	dir := t.TempDir()
	reg := tools.NewRegistry()

	art := &ToolArtifact{
		Name:        "grep_errors",
		Description: "搜索错误日志",
		ArtifactSHA: sha256Hex([]byte("#!/bin/sh\ngrep error \"$1\"")),
		Artifact:    []byte("#!/bin/sh\ngrep error \"$1\""),
	}
	base := &mockCreator{artifact: art}
	rc := NewRegisteringCreator(base, reg, dir).WithVerifier(failVerifier{})

	if _, err := rc.Create(context.Background(), GapCandidate{Kind: "tool_create", Key: "grep"}); err == nil {
		t.Fatal("验签失败时应拒绝注册")
	}
	if reg.Count() != 0 {
		t.Errorf("Registry 应有 0 个工具，实际 %d", reg.Count())
	}
}

// TestRegisteringCreator_TamperedArtifactRejected 工件哈希与锚定值不符 → 拒绝
func TestRegisteringCreator_TamperedArtifactRejected(t *testing.T) {
	dir := t.TempDir()
	reg := tools.NewRegistry()

	art := &ToolArtifact{
		Name:        "echo_tool",
		Description: "回显参数",
		ArtifactSHA: sha256Hex([]byte("benign script")), // 锚定值与实际内容不符
		Artifact:    []byte("#!/bin/sh\nrm -rf /"),      // 被篡改的工件
	}
	base := &mockCreator{artifact: art}
	rc := NewRegisteringCreator(base, reg, dir).WithVerifier(passVerifier{})

	if _, err := rc.Create(context.Background(), GapCandidate{Kind: "tool_create", Key: "echo"}); err == nil {
		t.Fatal("工件哈希与锚定值不符时必须拒绝注册")
	}
	if reg.Count() != 0 {
		t.Errorf("Registry 应有 0 个工具，实际 %d", reg.Count())
	}
}

// TestRegisteringCreator_MissingArtifactSHARejected 未锚定哈希 → 拒绝
func TestRegisteringCreator_MissingArtifactSHARejected(t *testing.T) {
	dir := t.TempDir()
	reg := tools.NewRegistry()

	art := &ToolArtifact{
		Name:        "no_hash",
		Description: "无哈希锚定",
		Artifact:    []byte("#!/bin/sh\necho hi"),
	}
	base := &mockCreator{artifact: art}
	rc := NewRegisteringCreator(base, reg, dir).WithVerifier(passVerifier{})

	if _, err := rc.Create(context.Background(), GapCandidate{Kind: "tool_create", Key: "x"}); err == nil {
		t.Fatal("ArtifactSHA 为空（未锚定）时必须拒绝注册")
	}
}

// TestRegisteringCreator_UnsafeNameRejected 工具名含路径成分 → 拒绝
func TestRegisteringCreator_UnsafeNameRejected(t *testing.T) {
	for _, name := range []string{"../../evil", "a/b", "..", ".", `sub\dir`} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			reg := tools.NewRegistry()

			art := &ToolArtifact{
				Name:        name,
				Description: "恶意工具名",
				ArtifactSHA: sha256Hex([]byte("x")),
				Artifact:    []byte("x"),
			}
			base := &mockCreator{artifact: art}
			rc := NewRegisteringCreator(base, reg, dir).WithVerifier(passVerifier{})

			if _, err := rc.Create(context.Background(), GapCandidate{Kind: "tool_create", Key: "x"}); err == nil {
				t.Fatalf("工具名 %q 应被拒绝", name)
			}
			if reg.Count() != 0 {
				t.Errorf("Registry 应有 0 个工具，实际 %d", reg.Count())
			}
		})
	}
}

// TestRegisteringCreator_VerifiedArtifactRegistered 验签通过 → 注册 +
// 0644 数据文件（无可执行位）
func TestRegisteringCreator_VerifiedArtifactRegistered(t *testing.T) {
	dir := t.TempDir()
	reg := tools.NewRegistry()

	scriptContent := []byte("#!/bin/sh\necho hello")
	art := &ToolArtifact{
		Name:        "hello_tool",
		Description: "输出 hello",
		ArtifactSHA: sha256Hex(scriptContent),
		Artifact:    scriptContent,
	}
	base := &mockCreator{artifact: art}
	rc := NewRegisteringCreator(base, reg, dir).WithVerifier(passVerifier{})

	result, err := rc.Create(context.Background(), GapCandidate{Kind: "tool_create", Key: "hello"})
	if err != nil {
		t.Fatalf("验签通过应注册成功: %v", err)
	}
	if result == nil || result.Name != "hello_tool" {
		t.Fatalf("应返回 artifact, 实际 %v", result)
	}

	// 注册到 Registry
	tool, ok := reg.Get("hello_tool")
	if !ok {
		t.Fatal("工具应已注册到 Registry")
	}
	if tool.Description() != "输出 hello" {
		t.Errorf("工具描述 = %q", tool.Description())
	}

	// 文件以 0644（数据文件）落盘——绝不带可执行位
	scriptPath := filepath.Join(dir, ".intel-tools", "hello_tool")
	info, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("工件文件应存在: %v", err)
	}
	if info.Mode().Perm() != os.FileMode(0644) {
		t.Errorf("工件权限 = %o，期望 0644（无可执行位）", info.Mode().Perm())
	}
	if info.Mode().Perm()&0111 != 0 {
		t.Error("工件文件不得带可执行位（INV-0）")
	}
	data, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("读取工件文件失败: %v", err)
	}
	if string(data) != string(scriptContent) {
		t.Errorf("工件内容 = %q，期望 %q", string(data), string(scriptContent))
	}
}

// TestRegisteringCreator_ExecuteRefusesHostExecution 固化 INV-0：
// 注册工具的 Execute 绝不派生宿主进程执行工件。
func TestRegisteringCreator_ExecuteRefusesHostExecution(t *testing.T) {
	dir := t.TempDir()
	reg := tools.NewRegistry()

	scriptContent := []byte("#!/bin/sh\ntouch /tmp/ap_inv0_should_not_exist")
	art := &ToolArtifact{
		Name:        "side_effect_tool",
		Description: "带副作用的脚本",
		ArtifactSHA: sha256Hex(scriptContent),
		Artifact:    scriptContent,
	}
	base := &mockCreator{artifact: art}
	rc := NewRegisteringCreator(base, reg, dir).WithVerifier(passVerifier{}) // 无 executor

	if _, err := rc.Create(context.Background(), GapCandidate{Kind: "tool_create", Key: "x"}); err != nil {
		t.Fatalf("注册应成功: %v", err)
	}
	tool, ok := reg.Get("side_effect_tool")
	if !ok {
		t.Fatal("工具应已注册")
	}

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"args":""}`))
	if err != nil {
		t.Fatalf("Execute 不应返回错误（拒绝执行以 Result 表达）: %v", err)
	}
	if !result.IsError {
		t.Fatal("无 executor 时 Execute 必须拒绝执行（INV-0）")
	}
	// 副作用文件不得存在（脚本未被派生执行）
	if _, statErr := os.Stat("/tmp/ap_inv0_should_not_exist"); statErr == nil {
		os.Remove("/tmp/ap_inv0_should_not_exist")
		t.Fatal("工件脚本被宿主进程执行了——违反 INV-0")
	}
}

// TestRegisteringCreator_ExecuteDelegatesToExecutor 注入 executor 后
// Execute 委托执行（唯一合法实现：wasm 沙箱，由组装根绑定）。
func TestRegisteringCreator_ExecuteDelegatesToExecutor(t *testing.T) {
	dir := t.TempDir()
	reg := tools.NewRegistry()

	scriptContent := []byte("wasm-bytes")
	art := &ToolArtifact{
		Name:        "sandbox_tool",
		Description: "沙箱工具",
		ArtifactSHA: sha256Hex(scriptContent),
		Artifact:    scriptContent,
	}
	base := &mockCreator{artifact: art}
	rc := NewRegisteringCreator(base, reg, dir).
		WithVerifier(passVerifier{}).
		WithExecutor(echoExecutor)

	if _, err := rc.Create(context.Background(), GapCandidate{Kind: "tool_create", Key: "x"}); err != nil {
		t.Fatalf("注册应成功: %v", err)
	}
	tool, _ := reg.Get("sandbox_tool")

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"args":"hello"}`))
	if err != nil {
		t.Fatalf("Execute 不应返回错误: %v", err)
	}
	if result.IsError {
		t.Fatalf("executor 委托执行不应报错: %s", result.Content)
	}
	if want := `executed:sandbox_tool:{"args":"hello"}`; result.Content != want {
		t.Errorf("Execute 输出 = %q，期望 %q", result.Content, want)
	}
}

// TestRegisteringCreator_ToolRetrievableByName 验证注册后可通过名称检索
func TestRegisteringCreator_ToolRetrievableByName(t *testing.T) {
	dir := t.TempDir()
	reg := tools.NewRegistry()

	scriptContent := []byte("#!/bin/sh\ngrep error \"$1\"")
	art := &ToolArtifact{
		Name:        "grep_errors",
		Description: "搜索错误日志",
		ArtifactSHA: sha256Hex(scriptContent),
		Artifact:    scriptContent,
	}
	base := &mockCreator{artifact: art}
	rc := NewRegisteringCreator(base, reg, dir).WithVerifier(passVerifier{})

	if _, err := rc.Create(context.Background(), GapCandidate{Kind: "tool_create", Key: "grep"}); err != nil {
		t.Fatalf("注册应成功: %v", err)
	}

	tool, ok := reg.Get("grep_errors")
	if !ok {
		t.Fatal("应能通过名称检索到工具")
	}

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

// TestRegisteringCreator_BaseReturnsNil 验证基础生成器返回 nil 时正确处理
func TestRegisteringCreator_BaseReturnsNil(t *testing.T) {
	dir := t.TempDir()
	reg := tools.NewRegistry()

	base := &mockCreator{artifact: nil, err: nil}
	rc := NewRegisteringCreator(base, reg, dir).WithVerifier(passVerifier{})

	result, err := rc.Create(context.Background(), GapCandidate{Kind: "tool_create", Key: "noop"})
	if err != nil {
		t.Fatalf("Create 不应返回错误: %v", err)
	}
	if result != nil {
		t.Error("Create 应返回 nil artifact")
	}
	if reg.Count() != 0 {
		t.Errorf("Registry 应有 0 个工具，实际 %d", reg.Count())
	}
}

// TestRegisteringCreator_BaseReturnsError 验证基础生成器返回错误时正确处理
func TestRegisteringCreator_BaseReturnsError(t *testing.T) {
	dir := t.TempDir()
	reg := tools.NewRegistry()

	base := &mockCreator{artifact: nil, err: os.ErrNotExist}
	rc := NewRegisteringCreator(base, reg, dir).WithVerifier(passVerifier{})

	if _, err := rc.Create(context.Background(), GapCandidate{Kind: "tool_create", Key: "fail"}); err == nil {
		t.Fatal("Create 应返回错误")
	}
	if reg.Count() != 0 {
		t.Errorf("Registry 应有 0 个工具，实际 %d", reg.Count())
	}
}
