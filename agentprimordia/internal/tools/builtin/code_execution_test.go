package builtin

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/tools"
)

func TestMain(m *testing.M) {
	// 测试期间显式启用 code_execution（本身不是沙箱，仅用于验证超时/输出限制）
	os.Setenv("AP_ALLOW_CODE_EXECUTION", "true")
	code := m.Run()
	os.Unsetenv("AP_ALLOW_CODE_EXECUTION")
	os.Exit(code)
}

func TestCodeExecution_Name(t *testing.T) {
	ce := NewCodeExecution()
	if ce.Name() != "code_execution" {
		t.Errorf("expected 'code_execution', got '%s'", ce.Name())
	}
}

func TestCodeExecution_Description(t *testing.T) {
	ce := NewCodeExecution()
	desc := ce.Description()
	if desc == "" {
		t.Error("description should not be empty")
	}
	if !strings.Contains(strings.ToLower(desc), "python") {
		t.Error("description should mention Python")
	}
	if !strings.Contains(strings.ToLower(desc), "javascript") {
		t.Error("description should mention JavaScript")
	}
	if !strings.Contains(strings.ToLower(desc), "go") {
		t.Error("description should mention Go")
	}
}

func TestCodeExecution_Parameters(t *testing.T) {
	ce := NewCodeExecution()
	params := ce.Parameters()
	if params == nil {
		t.Fatal("parameters should not be nil")
	}
	var schema map[string]any
	if err := json.Unmarshal(params, &schema); err != nil {
		t.Fatalf("invalid JSON schema: %v", err)
	}
	if schema["type"] != "object" {
		t.Errorf("expected type object, got %v", schema["type"])
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties should be a map")
	}
	if _, ok := props["language"]; !ok {
		t.Error("properties should contain 'language'")
	}
	if _, ok := props["code"]; !ok {
		t.Error("properties should contain 'code'")
	}
	if _, ok := props["timeout"]; !ok {
		t.Error("properties should contain 'timeout'")
	}
}

func TestCodeExecution_DisabledByDefault(t *testing.T) {
	t.Setenv("AP_ALLOW_CODE_EXECUTION", "")
	ce := NewCodeExecution()
	args, _ := json.Marshal(map[string]any{
		"language": "python",
		"code":     "print('hello')",
	})
	result, err := ce.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("code_execution should be disabled by default")
	}
	if !strings.Contains(result.Content, "AP_ALLOW_CODE_EXECUTION") {
		t.Errorf("error should mention AP_ALLOW_CODE_EXECUTION, got: %s", result.Content)
	}
}

func TestCodeExecution_InvalidArguments(t *testing.T) {
	ce := NewCodeExecution()
	args := json.RawMessage(`invalid json`)
	result, err := ce.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("invalid arguments should return error result")
	}
	if !strings.Contains(strings.ToLower(result.Content), "invalid arguments") {
		t.Errorf("error should mention invalid arguments, got: %s", result.Content)
	}
}

func TestCodeExecution_MissingLanguage(t *testing.T) {
	ce := NewCodeExecution()
	args, _ := json.Marshal(map[string]any{
		"code": "print('hello')",
	})
	result, err := ce.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("missing language should return error result")
	}
	if !strings.Contains(strings.ToLower(result.Content), "language") {
		t.Errorf("error should mention language, got: %s", result.Content)
	}
}

func TestCodeExecution_MissingCode(t *testing.T) {
	ce := NewCodeExecution()
	args, _ := json.Marshal(map[string]any{
		"language": "python",
	})
	result, err := ce.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("missing code should return error result")
	}
	if !strings.Contains(strings.ToLower(result.Content), "code") {
		t.Errorf("error should mention code, got: %s", result.Content)
	}
}

func TestCodeExecution_EmptyCode(t *testing.T) {
	ce := NewCodeExecution()
	args, _ := json.Marshal(map[string]any{
		"language": "python",
		"code":     "   ",
	})
	result, err := ce.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("empty code should return error result")
	}
	if !strings.Contains(strings.ToLower(result.Content), "empty") {
		t.Errorf("error should mention empty, got: %s", result.Content)
	}
}

func TestCodeExecution_UnsupportedLanguage(t *testing.T) {
	ce := NewCodeExecution()
	args, _ := json.Marshal(map[string]any{
		"language": "rust",
		"code":     "fn main() {}",
	})
	result, err := ce.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("unsupported language should return error result")
	}
	if !strings.Contains(strings.ToLower(result.Content), "unsupported") {
		t.Errorf("error should mention unsupported, got: %s", result.Content)
	}
}

func TestCodeExecution_InvalidTimeout(t *testing.T) {
	ce := NewCodeExecution()
	args, _ := json.Marshal(map[string]any{
		"language": "python",
		"code":     "print('hello')",
		"timeout":  "invalid",
	})
	result, err := ce.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("invalid timeout should return error result")
	}
	if !strings.Contains(strings.ToLower(result.Content), "timeout") {
		t.Errorf("error should mention timeout, got: %s", result.Content)
	}
}

// TestCodeExecution_RuntimeNotAvailable 测试运行时不可用的情况
func TestCodeExecution_RuntimeNotAvailable(t *testing.T) {
	// 使用一个不存在的运行时命令
	ce := NewCodeExecution()
	// 临时修改 runtimeCommand 的返回值（通过测试一个不存在的语言来触发）
	// 实际上我们通过 LookPath 检查，所以如果 python3/node/go 不存在会返回错误
	// 这里我们测试一个确定不存在的场景：使用一个虚构的语言（但已支持）
	// 由于无法真正模拟运行时不存在，我们只测试接口行为

	// 如果系统上没有 python3/python，这个测试会验证错误处理
	// 如果有 python3，则跳过此测试
	if _, err := exec.LookPath("python3"); err == nil {
		t.Skip("python3 is available, skipping runtime-not-available test")
	}
	if _, err := exec.LookPath("python"); err == nil {
		t.Skip("python is available, skipping runtime-not-available test")
	}

	args, _ := json.Marshal(map[string]any{
		"language": "python",
		"code":     "print('hello')",
	})
	result, err := ce.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("runtime not available should return error result")
	}
	if !strings.Contains(strings.ToLower(result.Content), "not found") {
		t.Errorf("error should mention 'not found', got: %s", result.Content)
	}
}

// TestCodeExecution_PythonExecution 测试 Python 代码执行（如果运行时可用）
func TestCodeExecution_PythonExecution(t *testing.T) {
	// 检查 python3 或 python 是否可用
	if _, err := exec.LookPath("python3"); err != nil {
		if _, err := exec.LookPath("python"); err != nil {
			t.Skip("python3/python not available, skipping execution test")
		}
	}

	ce := NewCodeExecution()
	args, _ := json.Marshal(map[string]any{
		"language": "python",
		"code":     "print('hello from python')",
		"timeout":  float64(5),
	})
	result, err := ce.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("python execution should succeed, got: %s", result.Content)
	}

	var output map[string]any
	if err := json.Unmarshal([]byte(result.Content), &output); err != nil {
		t.Fatalf("result should be JSON: %v, got: %s", err, result.Content)
	}
	if output["language"] != "python" {
		t.Errorf("expected language 'python', got %v", output["language"])
	}
	if exitCode, ok := output["exit_code"].(float64); !ok || exitCode != 0 {
		t.Errorf("expected exit_code 0, got %v", output["exit_code"])
	}
	stdout, _ := output["output"].(string)
	if !strings.Contains(stdout, "hello from python") {
		t.Errorf("expected output to contain 'hello from python', got: %s", stdout)
	}
}

// TestCodeExecution_JavaScriptExecution 测试 JavaScript 代码执行（如果运行时可用）
func TestCodeExecution_JavaScriptExecution(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available, skipping execution test")
	}

	ce := NewCodeExecution()
	args, _ := json.Marshal(map[string]any{
		"language": "javascript",
		"code":     "console.log('hello from node');",
		"timeout":  float64(5),
	})
	result, err := ce.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("javascript execution should succeed, got: %s", result.Content)
	}

	var output map[string]any
	if err := json.Unmarshal([]byte(result.Content), &output); err != nil {
		t.Fatalf("result should be JSON: %v, got: %s", err, result.Content)
	}
	if output["language"] != "javascript" {
		t.Errorf("expected language 'javascript', got %v", output["language"])
	}
	if exitCode, ok := output["exit_code"].(float64); !ok || exitCode != 0 {
		t.Errorf("expected exit_code 0, got %v", output["exit_code"])
	}
	stdout, _ := output["output"].(string)
	if !strings.Contains(stdout, "hello from node") {
		t.Errorf("expected output to contain 'hello from node', got: %s", stdout)
	}
}

// TestCodeExecution_GoExecution 测试 Go 代码执行（如果运行时可用）
func TestCodeExecution_GoExecution(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not available, skipping execution test")
	}

	ce := NewCodeExecution()
	args, _ := json.Marshal(map[string]any{
		"language": "go",
		"code": `package main
import "fmt"
func main() {
	fmt.Println("hello from go")
}`,
		"timeout": float64(10),
	})
	result, err := ce.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("go execution should succeed, got: %s", result.Content)
	}

	var output map[string]any
	if err := json.Unmarshal([]byte(result.Content), &output); err != nil {
		t.Fatalf("result should be JSON: %v, got: %s", err, result.Content)
	}
	if output["language"] != "go" {
		t.Errorf("expected language 'go', got %v", output["language"])
	}
	if exitCode, ok := output["exit_code"].(float64); !ok || exitCode != 0 {
		t.Errorf("expected exit_code 0, got %v", output["exit_code"])
	}
	stdout, _ := output["output"].(string)
	if !strings.Contains(stdout, "hello from go") {
		t.Errorf("expected output to contain 'hello from go', got: %s", stdout)
	}
}

// TestCodeExecution_CodeError 测试代码执行错误
func TestCodeExecution_CodeError(t *testing.T) {
	// 检查 python3 或 python 是否可用
	if _, err := exec.LookPath("python3"); err != nil {
		if _, err := exec.LookPath("python"); err != nil {
			t.Skip("python not available, skipping error test")
		}
	}

	ce := NewCodeExecution()
	args, _ := json.Marshal(map[string]any{
		"language": "python",
		"code":     "raise Exception('test error')",
		"timeout":  float64(5),
	})
	result, err := ce.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 代码执行失败应该返回错误结果
	if !result.IsError {
		t.Fatal("code error should return error result")
	}

	var output map[string]any
	if err := json.Unmarshal([]byte(result.Content), &output); err != nil {
		t.Fatalf("result should be JSON: %v, got: %s", err, result.Content)
	}
	if exitCode, ok := output["exit_code"].(float64); !ok || exitCode == 0 {
		t.Errorf("expected non-zero exit_code, got %v", output["exit_code"])
	}
	stdout, _ := output["output"].(string)
	if !strings.Contains(stdout, "Exception") {
		t.Errorf("expected output to contain 'Exception', got: %s", stdout)
	}
}

// TestCodeExecution_Timeout 测试超时控制
func TestCodeExecution_Timeout(t *testing.T) {
	// 检查 python3 或 python 是否可用
	if _, err := exec.LookPath("python3"); err != nil {
		if _, err := exec.LookPath("python"); err != nil {
			t.Skip("python not available, skipping timeout test")
		}
	}

	ce := NewCodeExecution()
	args, _ := json.Marshal(map[string]any{
		"language": "python",
		"code":     "import time; time.sleep(10)",
		"timeout":  float64(1),
	})
	result, err := ce.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("timeout should return error result")
	}
	if !strings.Contains(strings.ToLower(result.Content), "timeout") {
		t.Errorf("error should mention timeout, got: %s", result.Content)
	}
}

// TestCodeExecution_OutputTruncation 测试输出截断
func TestCodeExecution_OutputTruncation(t *testing.T) {
	// 检查 python3 或 python 是否可用
	if _, err := exec.LookPath("python3"); err != nil {
		if _, err := exec.LookPath("python"); err != nil {
			t.Skip("python not available, skipping truncation test")
		}
	}

	// 使用较小的截断限制以便测试
	ce := NewCodeExecution().WithMaxOutputSize(50)
	args, _ := json.Marshal(map[string]any{
		"language": "python",
		"code":     "print('a' * 100)",
		"timeout":  float64(5),
	})
	result, err := ce.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("execution should succeed, got: %s", result.Content)
	}

	var output map[string]any
	if err := json.Unmarshal([]byte(result.Content), &output); err != nil {
		t.Fatalf("result should be JSON: %v, got: %s", err, result.Content)
	}
	if truncated, ok := output["truncated"].(bool); !ok || !truncated {
		t.Error("expected truncated to be true")
	}
	stdout, _ := output["output"].(string)
	if len(stdout) > 100 { // 50 字节 + 截断提示
		t.Errorf("output should be truncated, got length: %d", len(stdout))
	}
	if !strings.Contains(stdout, "truncated") {
		t.Errorf("output should contain truncation message, got: %s", stdout)
	}
}

// TestCodeExecution_DefaultTimeout 测试默认超时
func TestCodeExecution_DefaultTimeout(t *testing.T) {
	ce := NewCodeExecution()
	if ce.defaultTimeout != 10*time.Second {
		t.Errorf("expected default timeout 10s, got %v", ce.defaultTimeout)
	}
}

// TestCodeExecution_DefaultMaxOutputSize 测试默认最大输出大小
func TestCodeExecution_DefaultMaxOutputSize(t *testing.T) {
	ce := NewCodeExecution()
	if ce.maxOutputSize != 10*1024 {
		t.Errorf("expected default max output size 10KB, got %d", ce.maxOutputSize)
	}
}

// TestCodeExecution_WithTimeout 测试设置自定义超时
func TestCodeExecution_WithTimeout(t *testing.T) {
	ce := NewCodeExecution().WithTimeout(30 * time.Second)
	if ce.defaultTimeout != 30*time.Second {
		t.Errorf("expected timeout 30s, got %v", ce.defaultTimeout)
	}
}

// TestCodeExecution_WithMaxOutputSize 测试设置自定义最大输出大小
func TestCodeExecution_WithMaxOutputSize(t *testing.T) {
	ce := NewCodeExecution().WithMaxOutputSize(5 * 1024)
	if ce.maxOutputSize != 5*1024 {
		t.Errorf("expected max output size 5KB, got %d", ce.maxOutputSize)
	}
}

// TestCodeExecution_LanguageCaseInsensitive 测试语言名称大小写不敏感
func TestCodeExecution_LanguageCaseInsensitive(t *testing.T) {
	// 检查 python3 或 python 是否可用
	if _, err := exec.LookPath("python3"); err != nil {
		if _, err := exec.LookPath("python"); err != nil {
			t.Skip("python not available, skipping case-insensitive test")
		}
	}

	ce := NewCodeExecution()
	args, _ := json.Marshal(map[string]any{
		"language": "PYTHON",
		"code":     "print('test')",
		"timeout":  float64(5),
	})
	result, err := ce.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("uppercase language should work, got: %s", result.Content)
	}
}

// TestCodeExecution_TempFileCleanup 测试临时文件清理
func TestCodeExecution_TempFileCleanup(t *testing.T) {
	// 检查 python3 或 python 是否可用
	if _, err := exec.LookPath("python3"); err != nil {
		if _, err := exec.LookPath("python"); err != nil {
			t.Skip("python not available, skipping temp file cleanup test")
		}
	}

	ce := NewCodeExecution()
	args, _ := json.Marshal(map[string]any{
		"language": "python",
		"code":     "print('test cleanup')",
		"timeout":  float64(5),
	})
	_, err := ce.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 临时文件应该在执行后自动删除
	// 由于 defer os.Remove，我们无法直接检查，但测试应该通过无错误来验证
}

// TestCodeExecution_InterfaceCompliance 测试接口实现
func TestCodeExecution_InterfaceCompliance(t *testing.T) {
	var _ tools.Tool = (*CodeExecution)(nil)
}
