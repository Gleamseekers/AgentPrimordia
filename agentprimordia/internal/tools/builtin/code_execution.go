package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/tools"
)

const (
	codeExecDefaultTimeout = 10 * time.Second
	codeExecMaxOutputSize  = 10 * 1024 // 10KB
)

// CodeExecution 代码执行tool，支持在安全沙箱中执行 Python、JavaScript、Go 代码
type CodeExecution struct {
	defaultTimeout time.Duration
	maxOutputSize  int
}

// NewCodeExecution 创建代码执行tool实例
func NewCodeExecution() *CodeExecution {
	return &CodeExecution{
		defaultTimeout: codeExecDefaultTimeout,
		maxOutputSize:  codeExecMaxOutputSize,
	}
}

// WithTimeout 设置默认execution timeout
func (c *CodeExecution) WithTimeout(d time.Duration) *CodeExecution {
	c.defaultTimeout = d
	return c
}

// WithMaxOutputSize 设置最大输出大小（字节）
func (c *CodeExecution) WithMaxOutputSize(size int) *CodeExecution {
	c.maxOutputSize = size
	return c
}

func (c *CodeExecution) Name() string { return "code_execution" }

func (c *CodeExecution) Description() string {
	return "Execute code with timeout and output limits. Supports Python, JavaScript, and Go. " +
		"WARNING: This is NOT a security sandbox. Code runs directly on the host with the privileges " +
		"of the AgentPrimordia process and can access the filesystem, network, and environment. " +
		"Enable only in trusted environments by setting AP_ALLOW_CODE_EXECUTION=true."
}

func (c *CodeExecution) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "language": {"type": "string", "enum": ["python", "javascript", "go"], "description": "Programming language: python, javascript, or go"},
    "code": {"type": "string", "description": "Source code to execute. WARNING: arbitrary code execution can compromise this system."},
    "timeout": {"type": "number", "description": "Execution timeout in seconds (default: 10)"}
  },
  "required": ["language", "code"]
}`)
}

func (c *CodeExecution) Execute(ctx context.Context, args json.RawMessage) (*tools.Result, error) {
	if os.Getenv("AP_ALLOW_CODE_EXECUTION") != "true" {
		return tools.NewErrorResult(
			"code_execution is disabled by default for security reasons. " +
				"It is NOT a sandbox and runs arbitrary code on the host. " +
				"Set AP_ALLOW_CODE_EXECUTION=true to enable it only in trusted environments.",
		), nil
	}

	var params map[string]json.RawMessage
	if err := json.Unmarshal(args, &params); err != nil {
		return tools.NewErrorResult(fmt.Sprintf("invalid arguments: %v", err)), nil
	}

	// 解析 language
	var language string
	if err := unmarshalRaw(params["language"], &language); err != nil {
		return tools.NewErrorResult("parameter 'language' is required"), nil
	}
	language = strings.ToLower(strings.TrimSpace(language))

	// 解析 code
	var code string
	if err := unmarshalRaw(params["code"], &code); err != nil {
		return tools.NewErrorResult("parameter 'code' is required"), nil
	}
	if strings.TrimSpace(code) == "" {
		return tools.NewErrorResult("parameter 'code' cannot be empty"), nil
	}

	// 解析 timeout（默认 10 秒）
	timeoutSec := int(c.defaultTimeout.Seconds())
	if raw, ok := params["timeout"]; ok && len(raw) > 0 {
		var v float64
		if err := unmarshalRaw(raw, &v); err != nil {
			return tools.NewErrorResult(fmt.Sprintf("invalid parameter 'timeout': %v", err)), nil
		}
		if v > 0 {
			// P1 修复：clamp 到 [1, 3600] 秒，防 int(v) 大值溢出 time.Duration
			timeoutSec = clampTimeoutSec(v)
		}
	}

	// 确定运行时命令和临时文件扩展名
	cmdName, ext := runtimeCommand(language)
	if cmdName == "" {
		return tools.NewErrorResult(fmt.Sprintf("unsupported language: %s (supported: python, javascript, go)", language)), nil
	}

	// 检查运行时是否可用
	if _, err := exec.LookPath(cmdName); err != nil {
		return tools.NewErrorResult(fmt.Sprintf(
			"runtime '%s' not found. Please install %s and ensure it is in your PATH.",
			cmdName, languageDisplayName(language),
		)), nil
	}

	// 创建临时文件（执行后自动删除）
	tmpFile, err := os.CreateTemp("", "code_exec_*"+ext)
	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("failed to create temp file: %v", err)), nil
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	// 写入代码到临时文件
	if _, err := tmpFile.WriteString(code); err != nil {
		tmpFile.Close()
		return tools.NewErrorResult(fmt.Sprintf("failed to write code to temp file: %v", err)), nil
	}
	tmpFile.Close()

	// 构建执行命令参数
	var cmdArgs []string
	if language == "go" {
		cmdArgs = []string{"run", tmpPath}
	} else {
		cmdArgs = []string{tmpPath}
	}

	// 设置超时 context
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(execCtx, cmdName, cmdArgs...)

	// 环境变量隔离：仅传递必要的安全环境变量
	cmd.Env = buildCodeExecEnv(language)

	// 工作目录设为临时文件所在目录，避免 Go 模块等冲突
	cmd.Dir = os.TempDir()

	output, err := cmd.CombinedOutput()
	exitCode := 0

	if err != nil {
		exitCode = 1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		// 超时检测
		if execCtx.Err() == context.DeadlineExceeded || strings.Contains(err.Error(), "signal: killed") {
			outputStr := string(output)
			if outputStr == "" {
				outputStr = "(no output before timeout)"
			}
			return tools.NewErrorResult(fmt.Sprintf("execution timed out after %d seconds\n%s", timeoutSec, outputStr)), nil
		}
	}

	// 输出截断（最大 10KB）
	outputStr := string(output)
	truncated := false
	if c.maxOutputSize > 0 && len(outputStr) > c.maxOutputSize {
		outputStr = outputStr[:c.maxOutputSize] + "\n... [output truncated, exceeded 10KB limit]"
		truncated = true
	}

	result := map[string]any{
		"language":  language,
		"exit_code": exitCode,
		"output":    outputStr,
		"truncated": truncated,
	}

	resultJSON, _ := json.MarshalIndent(result, "", "  ")

	if exitCode != 0 {
		return tools.NewErrorResult(string(resultJSON)), nil
	}
	return tools.NewResult(string(resultJSON)), nil
}

// buildCodeExecEnv 构建代码执行的环境变量（隔离）
func buildCodeExecEnv(language string) []string {
	env := []string{"PATH=" + os.Getenv("PATH")}
	for _, name := range []string{"HOME", "TEMP", "TMP", "USERPROFILE"} {
		if v := os.Getenv(name); v != "" {
			env = append(env, name+"="+v)
		}
	}
	// Go 运行时额外需要 GOPATH/GOROOT/GOCACHE 及 Windows 相关环境变量
	if language == "go" {
		for _, name := range []string{"GOPATH", "GOROOT", "GOCACHE", "GOMODCACHE", "LOCALAPPDATA", "APPDATA", "SYSTEMROOT", "USERPROFILE"} {
			if v := os.Getenv(name); v != "" {
				env = append(env, name+"="+v)
			}
		}
	}
	return env
}

// runtimeCommand 根据语言返回运行时命令和临时文件扩展名
func runtimeCommand(language string) (cmdName, ext string) {
	switch language {
	case "python":
		if runtime.GOOS == "windows" {
			return "python", ".py"
		}
		return "python3", ".py"
	case "javascript":
		return "node", ".js"
	case "go":
		return "go", ".go"
	default:
		return "", ""
	}
}

// languageDisplayName 返回语言的显示名称（用于错误提示）
func languageDisplayName(lang string) string {
	switch lang {
	case "python":
		return "Python"
	case "javascript":
		return "Node.js"
	case "go":
		return "Go"
	default:
		return lang
	}
}
