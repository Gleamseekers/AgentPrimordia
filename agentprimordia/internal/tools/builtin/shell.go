package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/logger"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/tools"
)

// maxTimeoutSec 单条命令超时上限（秒）。用户可控 timeout 参数 clamp 到此，
// 防 int(v) 大值溢出 time.Duration（P2 修复）。
const maxTimeoutSec = 3600

// isWithinAllowedWorkdirs 判断 path 是否落在任一允许的工作目录内
// （Clean + 分隔符边界检查，防 "/home/app-secret" 绕过 "/home/app"）。
func isWithinAllowedWorkdirs(p string, allowed []string) bool {
	abs := filepath.Clean(p)
	for _, dir := range allowed {
		absDir := filepath.Clean(dir)
		if abs == absDir || strings.HasPrefix(abs, absDir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// resolveWithinWorkdir 判定 argv 中的路径 token 是否落在允许的工作目录内。
//
// 相对 token 以生效 workdir 为基准解析后判定——这是修复的关键：此前只校验
// looksLikeAbsolutePath 为真的 token，"../../etc/passwd" 形态上不是绝对路径，
// 被整条跳过；而 cmd.Dir 恰是受禁锢的 workdir，相对路径从它出发即可越界
// （filesystem 的 rootDir jail 可被一句 "cat ../../etc/passwd" 废弃）。
// workdir 缺省时回退到 allowedWorkdirs[0]（调用方已保证非空）。
// 未配置禁锢（allowed 为空）时无从判定，返回 true 保持"无 jail"语义。
func resolveWithinWorkdir(tok, workdir string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	p := tok
	if !filepath.IsAbs(p) {
		base := workdir
		if base == "" {
			base = allowed[0]
		}
		p = filepath.Join(base, p) // Join 内部 Clean，正确折叠 ".."
	}
	return isWithinAllowedWorkdirs(p, allowed)
}

// blockedCommandNames 黑名单模式下的规范化命令名集合（按 token basename 匹配）。
// 相比此前的子串匹配（可被 "rm -fr" flags 重排、/bin/rm 路径前缀、大小写变形
// 轻易绕过），此处对 tokenizeCommand 的结果逐 token 取 basename 后比对，
// 并经 isBlockedCommandName 归一化（小写 + 首个点分分量），覆盖
// "mkfs.ext4" 等命令变体。
var blockedCommandNames = map[string]bool{
	"rm": true, "mkfs": true, "dd": true,
	"shutdown": true, "reboot": true, "halt": true, "poweroff": true,
	"fdisk": true,
}

// isBlockedCommandName 判断 token basename 是否为黑名单命令。
// 归一化规则：转小写（覆盖大小写变形）；额外取首个点分分量
// （"mkfs.ext4" → "mkfs"，覆盖命令变体——此前变体后缀名不在集合中被绕过）。
// 路径前缀（/bin/rm）由调用方 path.Base 先行剥离。
func isBlockedCommandName(base string) bool {
	lower := strings.ToLower(base)
	if blockedCommandNames[lower] {
		return true
	}
	if i := strings.Index(lower, "."); i > 0 {
		return blockedCommandNames[lower[:i]]
	}
	return false
}

// blockedCommandPatterns 黑名单模式下仍需保留的整串危险模式（shell 函数定义等）。
var blockedCommandPatterns = []string{
	":(){ :|:& };:",
	"> /dev/sda",
}

// defaultWhitelist 默认允许的安全命令列表（2026-09-28 P0 安全修复后收敛）。
//
// 收录原则：**只收不可派生进程、不可解释代码的单用途命令**。以下类别
// 曾被收录但已移除——它们使"白名单"形同虚设（被 prompt injection 控制
// 的 agent 借此获得宿主任意代码执行，7 条攻击链实证见 shell_security_test.go）：
//   - 进程派生器：env / printenv（env 可执行任意程序）、find（-exec）、ln（符号链接逃逸）；
//   - 代码解释器/编译器：python / python3 / node / go / make / cargo / rustc；
//   - 通用 VCS：git（-c/alias/ext 可执行任意命令）。
// 确有需要的用户应经 WithWhitelist 显式追加，并自行承担相应风险
// （显式 opt-in 是安全默认值与灵活性之间的边界）。
var defaultWhitelist = []string{
	"ls", "cat", "head", "tail", "wc", "echo", "pwd", "whoami",
	"grep", "sort", "uniq", "diff",
	"date", "uname", "which",
	"mkdir", "cp", "mv", "touch",
}

// containsShellMetacharacters 检查命令是否包含危险的 shell 元字符。
// 复用 tools 包的统一规则，确保 Shell tool与 Sandbox 校验一致。
func containsShellMetacharacters(cmd string) (bool, string) {
	return tools.ContainsShellMetacharacter(cmd)
}

type Shell struct {
	defaultTimeout  time.Duration
	whitelistMode   bool
	whitelist       []string
	allowedWorkdirs []string
	scopePolicy     tools.ScopePolicy
	scopeAgent      string
	maxOutputSize   int
	sandbox         SandboxChecker // 可选：统一安全检查入口
	sandboxAgentID  string
}

// SandboxChecker 沙箱安全检查接口（与 security.Sandbox 兼容）
type SandboxChecker interface {
	CanExecute(agentID, cmd string) error
	ValidatePath(agentID, path string, level int) error
}

const defaultMaxOutputSize = 50000

func NewShell() *Shell {
	return &Shell{
		defaultTimeout: 30 * time.Second,
		whitelistMode:  true,
		whitelist:      defaultWhitelist,
		maxOutputSize:  defaultMaxOutputSize,
	}
}

// WithTimeout 设置execution timeout
func (s *Shell) WithTimeout(d time.Duration) *Shell {
	s.defaultTimeout = d
	return s
}

// WithWhitelist 启用白名单模式，只允许执行指定的命令
// 命令名应为命令行第一个词（如 "ls", "cat", "git" 等）
func (s *Shell) WithWhitelist(commands []string) *Shell {
	s.whitelistMode = true
	s.whitelist = commands
	return s
}

// warnBlacklistRisk 黑名单模式启用时的风险告警。
// 包级变量便于测试替换捕获；默认走 logger.Warn（stderr，Info 级别可见）。
var warnBlacklistRisk = func() {
	logger.Warn(
		"SECURITY: shell blacklist mode enabled — blacklist is fundamentally weaker than whitelist and can be bypassed by command mutation; use WithWhitelist instead. WithBlacklist is deprecated and will be removed in v8.0.0",
	)
}

// WithBlacklist 启用黑名单模式（不推荐，安全性较低）。
//
// Deprecated: 黑名单模式根本弱于白名单——已知可被命令变形绕过（token 化
// 匹配仅覆盖已枚举的变形，无法穷尽）。请改用 WithWhitelist 显式声明允许的
// 命令集。启用时会打 Warning 日志提示风险。
// Removed in v8.0.0.
func (s *Shell) WithBlacklist() *Shell {
	s.whitelistMode = false
	s.whitelist = nil
	warnBlacklistRisk()
	return s
}

// WithAllowedWorkdirs 限制命令执行的工作目录范围。
//
// 目录在此归一为绝对路径：调用方常传入相对 RootDir（如 DefaultToolkit 的 "."），
// 而禁锢判定是前缀比对——若允许集里混着相对路径，"sub/x" 解析出的相对 token
// 将无法通过 "./" 前缀比对，jail 内的合法相对路径会被误拒。统一以绝对路径为
// 基准后，相对 token 与绝对 token 走同一套判定。
func (s *Shell) WithAllowedWorkdirs(dirs []string) *Shell {
	abs := make([]string, 0, len(dirs))
	for _, d := range dirs {
		if a, err := filepath.Abs(d); err == nil {
			abs = append(abs, filepath.Clean(a))
		} else {
			abs = append(abs, filepath.Clean(d))
		}
	}
	s.allowedWorkdirs = abs
	return s
}

// WithScopePolicy 注入权限策略，以 workdir 为资源路径进行权限检查
func (s *Shell) WithScopePolicy(policy tools.ScopePolicy, agentID string) *Shell {
	s.scopePolicy = policy
	s.scopeAgent = agentID
	return s
}

// WithSandbox 注入沙箱安全检查，命令执行前必须通过沙箱验证
func (s *Shell) WithSandbox(sandbox SandboxChecker, agentID string) *Shell {
	s.sandbox = sandbox
	s.sandboxAgentID = agentID
	return s
}

func (s *Shell) Name() string { return "shell" }

func (s *Shell) Description() string {
	return "Command execution tool. Executes a single command with arguments directly (no shell interpreter). Supports quoted arguments and backslash escaping. Returns stdout, stderr, and exit code."
}

func (s *Shell) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "action": {"type": "string", "enum": ["execute"], "description": "The operation to perform"},
    "command": {"type": "string", "description": "Command to execute, including arguments (e.g. \"echo hello\"). No shell interpretation; pipes, redirections, and command substitution are not supported."},
    "args": {"type": "array", "items": {"type": "string"}, "description": "Optional explicit argument list. If provided, command is treated as executable name only."},
    "timeout": {"type": "number", "description": "Timeout in seconds (default: 30)"},
    "workdir": {"type": "string", "description": "Working directory for command execution"}
  },
  "required": ["action", "command"]
}`)
}

func (s *Shell) Execute(ctx context.Context, args json.RawMessage) (*tools.Result, error) {
	var params map[string]json.RawMessage
	if err := json.Unmarshal(args, &params); err != nil {
		return tools.NewErrorResult(fmt.Sprintf("invalid arguments: %v", err)), nil
	}

	action := ""
	if err := unmarshalRaw(params["action"], &action); err != nil {
		return tools.NewErrorResult(fmt.Sprintf("invalid parameter 'action': %v", err)), nil
	}

	if action != "execute" {
		return tools.NewErrorResult(fmt.Sprintf("unknown action: %s", action)), nil
	}

	command := ""
	if raw, ok := params["command"]; ok && len(raw) > 0 {
		if err := unmarshalRaw(raw, &command); err != nil {
			return tools.NewErrorResult(fmt.Sprintf("invalid parameter 'command': %v", err)), nil
		}
	}
	if strings.TrimSpace(command) == "" {
		return tools.NewErrorResult("command is required"), nil
	}

	// ===== 参数解析与安全校验（2026-09-28 P0 安全修复后重整）=====
	//
	// 校验顺序（每一层都不可被 args 路径绕过）：
	//   1. timeout 解析并 clamp 到 [1, maxTimeoutSec]（防 int 溢出 Duration）；
	//   2. workdir 解析与 ScopePolicy / allowedWorkdirs / Sandbox 校验；
	//   3. argv 构建（args 路径与 command 字符串路径归一）；
	//   4. 元字符检查：command 字符串 + argv 每一个元素（修复：args 曾完全绕过）；
	//   5. 白名单/黑名单判定（基于 argv[0] basename）；
	//   6. 路径禁锢：argv 中的**每一个** token（相对路径按生效 workdir 解析后）
	//      都过 allowedWorkdirs / Sandbox（修复：shell 曾可 cat rootDir 之外任意
	//      文件；再修复：曾只覆盖绝对路径 token，相对穿越 ../ 被整条跳过）；
	//   7. Sandbox.CanExecute 统一门。

	// 1. timeout（clamp 防溢出）
	timeoutSec := int(s.defaultTimeout.Seconds())
	if raw, ok := params["timeout"]; ok && len(raw) > 0 {
		var v float64
		if err := unmarshalRaw(raw, &v); err != nil {
			return tools.NewErrorResult(fmt.Sprintf("invalid parameter 'timeout': %v", err)), nil
		}
		if v > 0 {
			timeoutSec = int(v)
		}
	}
	if timeoutSec < 1 {
		timeoutSec = 1
	}
	if timeoutSec > maxTimeoutSec {
		timeoutSec = maxTimeoutSec
	}

	// 2. workdir
	workdir := ""
	if raw, ok := params["workdir"]; ok && len(raw) > 0 {
		if err := unmarshalRaw(raw, &workdir); err != nil {
			return tools.NewErrorResult(fmt.Sprintf("invalid parameter 'workdir': %v", err)), nil
		}
	}

	// ScopePolicy 权限检查：以 workdir 为资源路径
	if s.scopePolicy != nil && workdir != "" {
		if !s.scopePolicy.Allow(s.scopeAgent, workdir) {
			deniedErr := tools.NewScopeDeniedError(s.scopeAgent, workdir)
			return tools.NewErrorResult(deniedErr.Error()), deniedErr
		}
	}
	// 生效 workdir：显式参数优先；缺省但配置了禁锢目录时回退到 allowedWorkdirs[0]。
	// 此前此处被 `workdir != ""` 门控——省略 workdir 时整条禁锢校验不执行，
	// 且 cmd.Dir 不设置，命令相对宿主进程 cwd 解析，等于没有 jail（攻击链 5c）。
	effectiveWorkdir := workdir
	if effectiveWorkdir == "" && len(s.allowedWorkdirs) > 0 {
		effectiveWorkdir = s.allowedWorkdirs[0]
	}
	if effectiveWorkdir != "" && len(s.allowedWorkdirs) > 0 {
		// 使用 filepath.Clean + 分隔符边界检查，与 scope.go 的 Allow 实现一致。
		// 防止 "/home/app" 被路径 "/home/app-secret" 绕过。
		// 相对 workdir 先以宿主进程 cwd 解析为绝对路径——否则 "./sub" 这类
		// 合法相对 workdir 无法通过绝对基准的前缀比对，会被误拒。
		checkDir := effectiveWorkdir
		if !filepath.IsAbs(checkDir) {
			if a, aerr := filepath.Abs(checkDir); aerr == nil {
				checkDir = a
			}
		}
		if !isWithinAllowedWorkdirs(checkDir, s.allowedWorkdirs) {
			return tools.NewErrorResult(fmt.Sprintf("workdir '%s' is not in the allowed directories", checkDir)), nil
		}
		// 后续相对 token 的解析与 cmd.Dir 都基于已归一的有效 workdir。
		effectiveWorkdir = checkDir
	}

	// Sandbox 路径验证（workdir）
	if s.sandbox != nil && workdir != "" {
		if err := s.sandbox.ValidatePath(s.sandboxAgentID, workdir, 1); err != nil { // 1 = ReadWrite
			return tools.NewErrorResult(fmt.Sprintf("sandbox denied workdir access: %v", err)), nil
		}
	}

	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	// 3. argv 构建（两条路径归一为 name + cmdArgs）
	var name string
	var cmdArgs []string

	if raw, ok := params["args"]; ok && len(raw) > 0 {
		if err := unmarshalRaw(raw, &cmdArgs); err != nil {
			return tools.NewErrorResult(fmt.Sprintf("invalid parameter 'args': %v", err)), nil
		}
		name = strings.TrimSpace(command)
		cmdArgs = append([]string(nil), cmdArgs...)
	} else {
		tokens, err := tokenizeCommand(command)
		if err != nil {
			return tools.NewErrorResult(fmt.Sprintf("failed to parse command: %v", err)), nil
		}
		name = tokens[0]
		cmdArgs = tokens[1:]
	}

	// 4. 元字符检查：command 字符串 + argv 每一个元素。
	// 修复前只检查 command 字符串，显式 args 完全绕过——注入方可用
	// args 携带 "sh -c '...'" 等载荷（当 argv[0] 可派生 shell 时触发）。
	if hasMeta, meta := containsShellMetacharacters(command); hasMeta {
		return tools.NewErrorResult(fmt.Sprintf("command rejected: shell metacharacter '%s' is not allowed", meta)), nil
	}
	for i, arg := range cmdArgs {
		if hasMeta, meta := containsShellMetacharacters(arg); hasMeta {
			return tools.NewErrorResult(fmt.Sprintf("args[%d] rejected: shell metacharacter '%s' is not allowed", i, meta)), nil
		}
	}

	// 5. 白名单/黑名单判定（基于 argv[0] basename，两条路径同一标准）
	baseName := path.Base(name)
	lowerBase := strings.ToLower(baseName)
	if s.whitelistMode {
		allowed := false
		for _, w := range s.whitelist {
			if lowerBase == strings.ToLower(path.Base(w)) {
				allowed = true
				break
			}
		}
		if !allowed {
			return tools.NewErrorResult(fmt.Sprintf("command '%s' is not in the allowed list: %v", baseName, s.whitelist)), nil
		}
	} else {
		// 黑名单模式：token basename 规范化匹配（修复：子串匹配可被
		// "rm -fr" flags 重排、/bin/rm 路径前缀、大小写变形绕过）。
		// isBlockedCommandName 额外覆盖 "mkfs.ext4" 等命令变体。
		if isBlockedCommandName(baseName) {
			return tools.NewErrorResult(fmt.Sprintf("command blocked for safety reasons: '%s' is not allowed", baseName)), nil
		}
		for _, arg := range cmdArgs {
			if isBlockedCommandName(path.Base(arg)) {
				return tools.NewErrorResult(fmt.Sprintf("command blocked for safety reasons: argument '%s' is not allowed", arg)), nil
			}
		}
		lowerWhole := strings.ToLower(command)
		for _, pattern := range blockedCommandPatterns {
			if strings.Contains(lowerWhole, pattern) {
				return tools.NewErrorResult(fmt.Sprintf("command blocked for safety reasons: matches pattern '%s'", pattern)), nil
			}
		}
	}

	// 6. 路径禁锢：argv 中的**每一个** token 都必须在 allowedWorkdirs 内，
	// 且通过 Sandbox.ValidatePath（若注入）。修复前 shell 对命令参数中的
	// 路径零校验，filesystem 工具的 rootDir jail 可被一句 cat 废弃；
	// 2026-09-28 补齐时又只覆盖 looksLikeAbsolutePath 为真的 token，
	// 相对穿越路径（../../etc/passwd）被整条跳过（攻击链 5b）。
	for _, tok := range cmdArgs {
		if len(s.allowedWorkdirs) > 0 && !resolveWithinWorkdir(tok, effectiveWorkdir, s.allowedWorkdirs) {
			return tools.NewErrorResult(fmt.Sprintf("path '%s' is outside the allowed directories", tok)), nil
		}
		if s.sandbox != nil {
			if err := s.sandbox.ValidatePath(s.sandboxAgentID, tok, 1); err != nil {
				return tools.NewErrorResult(fmt.Sprintf("sandbox denied path access: %v", err)), nil
			}
		}
	}

	// 7. Sandbox 统一执行门
	if s.sandbox != nil {
		if err := s.sandbox.CanExecute(s.sandboxAgentID, command); err != nil {
			return tools.NewErrorResult(fmt.Sprintf("sandbox denied execution: %v", err)), nil
		}
	}

	cmd := exec.CommandContext(execCtx, name, cmdArgs...)

	// 环境变量隔离：仅传递必要的安全环境变量。
	// 除 PATH/HOME/TEMP/TMP 外，补充用户级缓存/系统目录变量——
	// 编译器（go/node 等）定位构建缓存时依赖它们（如 Windows 的
	// LOCALAPPDATA 决定 GOCACHE），缺失会导致「实施」环节运行代码失败。
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"TEMP=" + os.Getenv("TEMP"),
		"TMP=" + os.Getenv("TMP"),
	}
	for _, key := range []string{"LOCALAPPDATA", "APPDATA", "USERPROFILE", "SystemRoot", "GOCACHE", "XDG_CACHE_HOME"} {
		if v := os.Getenv(key); v != "" {
			cmd.Env = append(cmd.Env, key+"="+v)
		}
	}

	if effectiveWorkdir != "" {
		cmd.Dir = effectiveWorkdir
	}

	output, err := cmd.CombinedOutput()
	exitCode := 0

	if err != nil {
		exitCode = 1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		if execCtx.Err() == context.DeadlineExceeded || strings.Contains(err.Error(), "signal: killed") {
			outputStr := string(output)
			if outputStr == "" {
				outputStr = "(no output before timeout)"
			}
			return tools.NewErrorResult(fmt.Sprintf("command timed out after %d seconds\n%s", timeoutSec, outputStr)), nil
		}
	}

	stdout := string(output)
	stderr := ""

	stdout, stderr = splitOutput(stdout)

	if s.maxOutputSize > 0 && len(stdout) > s.maxOutputSize {
		stdout = stdout[:s.maxOutputSize] + "\n... [输出已截断，总长度超过限制]"
	}

	result := map[string]any{
		"stdout":    stdout,
		"stderr":    stderr,
		"exit_code": exitCode,
	}

	resultJSON, _ := json.MarshalIndent(result, "", "  ")

	if exitCode != 0 {
		return tools.NewErrorResult(string(resultJSON)), fmt.Errorf("exit code %d", exitCode)
	}
	return tools.NewResult(string(resultJSON)), nil
}

// tokenizeCommand 将命令字符串拆分为 [name, args...]
// 支持单引号、双引号和反斜杠转义，避免使用 shell 解释器
func tokenizeCommand(cmd string) ([]string, error) {
	var tokens []string
	var current strings.Builder
	var inSingleQuote, inDoubleQuote bool

	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}

	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		switch {
		case c == '\\' && !inSingleQuote:
			if i+1 < len(cmd) {
				current.WriteByte(cmd[i+1])
				i++
			} else {
				return nil, fmt.Errorf("trailing backslash")
			}
		case c == '\'' && !inDoubleQuote:
			inSingleQuote = !inSingleQuote
		case c == '"' && !inSingleQuote:
			inDoubleQuote = !inDoubleQuote
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			if inSingleQuote || inDoubleQuote {
				current.WriteByte(c)
			} else {
				flush()
			}
		default:
			current.WriteByte(c)
		}
	}

	if inSingleQuote || inDoubleQuote {
		return nil, fmt.Errorf("unclosed quote")
	}
	flush()

	if len(tokens) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	return tokens, nil
}

func splitOutput(combined string) (stdout, stderr string) {
	// Using CombinedOutput, so we can't separate stdout from stderr.
	// Return combined output as stdout with empty stderr.
	return combined, ""
}
