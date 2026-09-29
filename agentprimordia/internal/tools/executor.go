package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/concurrency"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/logger"
)

const (
	defaultToolTimeout      = 30 * time.Second
	defaultBatchConcurrency = 10
)

// ExecutorConfig 执行器配置
type ExecutorConfig struct {
	DefaultTimeout time.Duration            // 默认tool超时
	PerToolTimeout map[string]time.Duration // 按tool名设置超时（覆盖默认值）
	CacheConfig    *CacheConfig             // 缓存配置，nil 表示不启用缓存
}

// FunctionCall 表示执行tool函数的请求
type FunctionCall struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Args string `json:"args"`
}

// Executor 处理tool执行，包含日志、计时和错误处理
// slog logger 使用 internal/logger.FieldTool/FieldDuration 等统一字段常量。
type Executor struct {
	registry       *Registry
	slogger        *slog.Logger // 结构化日志（slog），Phase 4 Task 10 起强制唯一日志通道
	timeout        time.Duration
	perToolTimeout map[string]time.Duration // 按tool名设置超时（覆盖默认值）
	scopePolicy    ScopePolicy
	scopeAgent     string
	fileLock       *concurrency.FileLockManager
	cache          *Cache // tool结果缓存，nil 表示不启用
}

// NewExecutor 创建新的tool执行器
func NewExecutor(registry *Registry) *Executor {
	return &Executor{
		registry: registry,
		slogger:  slog.Default(),
		timeout:  defaultToolTimeout,
	}
}

// NewExecutorWithConfig 使用配置创建tool执行器
func NewExecutorWithConfig(registry *Registry, cfg ExecutorConfig) *Executor {
	timeout := defaultToolTimeout
	if cfg.DefaultTimeout > 0 {
		timeout = cfg.DefaultTimeout
	}
	e := &Executor{
		registry:       registry,
		slogger:        slog.Default(),
		timeout:        timeout,
		perToolTimeout: cfg.PerToolTimeout,
	}
	// 启用缓存（如果配置了）
	if cfg.CacheConfig != nil {
		e.cache = NewCache(*cfg.CacheConfig)
	}
	return e
}

// WithSlogLogger 注入自定义 *slog.Logger（perf-v5 Task 20）
func (e *Executor) WithSlogLogger(l *slog.Logger) *Executor {
	e.slogger = l
	return e
}

// WithTimeout 设置所有tool的execution timeout
func (e *Executor) WithTimeout(d time.Duration) *Executor {
	e.timeout = d
	return e
}

// WithScopePolicy 注入权限策略，agentID 标识当前 Agent
// 执行tool前会检查 Agent 是否有权限操作指定资源
func (e *Executor) WithScopePolicy(policy ScopePolicy, agentID string) *Executor {
	e.scopePolicy = policy
	e.scopeAgent = agentID
	return e
}

// WithFileLock 注入文件锁管理器
// 文件写入/编辑操作会自动获取和释放文件锁
func (e *Executor) WithFileLock(fl *concurrency.FileLockManager) *Executor {
	e.fileLock = fl
	return e
}

// Execute 按名称执行tool调用
func (e *Executor) Execute(ctx context.Context, tc *FunctionCall) (*Result, error) {
	start := time.Now()

	// Phase 4 Task 10：从 ctx 取出 trace-id 注入 slog，使所有日志自动可关联
	l := e.slogger
	if l == nil {
		l = slog.Default()
	}
	l = logger.FromContext(ctx, l)
	l.Debug("tool executing",
		logger.FieldTool, tc.Name,
		logger.FieldArgsLen, len(tc.Args),
		"args_preview", redactSensitiveArgs(tc.Args),
	)

	tool, exists := e.registry.Get(tc.Name)
	if !exists {
		return NewErrorResult(fmt.Sprintf("tool not found: %s", tc.Name)), ErrToolNotFound
	}

	// ScopePolicy 权限检查：从参数中提取全部路径字段逐一校验
	// （2026-09-28 P0 修复：提取改为完整递归遍历 + fail-closed——
	//  此前只认 6 个顶层 key，嵌套/MCP 风格参数会静默跳过检查）。
	if e.scopePolicy != nil {
		first, second := extractPathsFromArgs(tc.Args)
		for _, resource := range []string{first, second} {
			if resource == "" {
				continue
			}
			if !e.scopePolicy.Allow(e.scopeAgent, resource) {
				deniedErr := NewScopeDeniedError(e.scopeAgent, resource)
				return NewErrorResult(deniedErr.Error()), deniedErr
			}
		}
		// fail-closed：策略已启用但参数无法解析为 JSON 时拒绝执行，
		// 杜绝"解析失败即放行"的绕过面。
		if !json.Valid([]byte(tc.Args)) {
			return NewErrorResult("tool arguments are not valid JSON; execution denied by scope policy"),
				NewScopeDeniedError(e.scopeAgent, "(unparseable args)")
		}
	}

	var args json.RawMessage = json.RawMessage(tc.Args)

	// 权限检查：需要确认的tool必须通过确认回调
	// 优化：合并两次 GetPermission 调用为一次，避免冗余的 sync.Map.Load
	if perm, ok := e.registry.GetPermission(tc.Name); ok {
		if perm.RequireConfirmation {
			l.Debug("tool requires confirmation", logger.FieldTool, tc.Name)
			if perm.ConfirmFunc != nil {
				if !perm.ConfirmFunc(tc.Name, args) {
					return NewErrorResult(fmt.Sprintf("tool %s requires confirmation and was denied", tc.Name)), ErrConfirmDenied
				}
			} else {
				// 没有确认回调时默认拒绝
				return NewErrorResult(fmt.Sprintf("tool %s requires confirmation but no confirmation handler is registered", tc.Name)), ErrConfirmDenied
			}
		}
	}

	// 检查缓存（如果启用）
	cacheKey := ""
	if e.cache != nil {
		cacheKey = e.buildCacheKey(tc.Name, tc.Args)
		if cached, ok := e.cache.Get(cacheKey); ok {
			l.Debug("tool cache hit", logger.FieldTool, tc.Name, "cache_key", cacheKey)
			// 复制缓存结果，避免修改原始数据
			result := &Result{
				Content:  cached.Content,
				Metadata: make(map[string]any),
			}
			for k, v := range cached.Metadata {
				result.Metadata[k] = v
			}
			result.Metadata["cached"] = true
			return result, nil
		}
	}

	// 按tool名查找专属超时，未配置则使用默认超时
	timeout := e.timeout
	if perTool, ok := e.perToolTimeout[tc.Name]; ok {
		timeout = perTool
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// perf-v5 Task 1：tool panic recover，避免任意tool panic 杀死整个 agent 进程
	result, err := e.safeExecute(execCtx, tool, args)

	duration := time.Since(start)

	if err != nil {
		l.Error("tool execution failed",
			logger.FieldTool, tc.Name,
			logger.FieldDuration, duration.Milliseconds(),
			logger.FieldError, err.Error(),
		)
		if result == nil {
			result = NewErrorResult(err.Error())
		}
		return result, err
	}

	l.Info("tool completed",
		logger.FieldTool, tc.Name,
		logger.FieldDuration, duration.Milliseconds(),
	)

	if result.Metadata == nil {
		result.Metadata = make(map[string]any)
	}
	result.Metadata["duration_ms"] = duration.Milliseconds()
	result.Metadata["tool_name"] = tc.Name

	// 写入缓存（如果启用且执行成功）
	if e.cache != nil && cacheKey != "" {
		e.cache.Set(cacheKey, result)
	}

	return result, nil
}

// buildCacheKey 构建缓存键：Agent 命名空间 + 工具名 + 参数哈希。
// 修复（评估发现）：旧实现键含原始参数全文——参数可能携带密钥等敏感信息，
// 全文入键会长期滞留内存；且无 Agent 命名空间，共享 Cache 时跨 Agent 数据串扰。
// 新实现以 SHA-256 截断哈希入键（原文不落键），并按 scopeAgent 隔离命名空间。
func (e *Executor) buildCacheKey(toolName, args string) string {
	sum := sha256.Sum256([]byte(args))
	hash := hex.EncodeToString(sum[:8]) // 64bit 截断足以区分调用
	ns := e.scopeAgent
	if ns == "" {
		ns = "global"
	}
	return ns + ":" + toolName + ":" + hash
}

// safeExecute 包装tool调用并捕获 panic（perf-v5 Task 1）
// 任意tool panic 转为 error 返回，避免杀死 agent 进程
func (e *Executor) safeExecute(ctx context.Context, tool Tool, args json.RawMessage) (result *Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			l := e.slogger
			if l == nil {
				l = slog.Default()
			}
			logger.FromContext(ctx, l).Error("tool panic recovered",
				logger.FieldTool, tool.Name(),
				"panic", r,
			)
			result = NewErrorResult(fmt.Sprintf("tool %s panic: %v", tool.Name(), r))
			err = fmt.Errorf("tool %s panic: %v", tool.Name(), r)
		}
	}()
	return tool.Execute(ctx, args)
}

// ExecuteBatch 并发执行多个tool调用
func (e *Executor) ExecuteBatch(ctx context.Context, calls []*FunctionCall) ([]*Result, error) {
	if len(calls) == 0 {
		return nil, nil
	}

	results := make([]*Result, len(calls))
	errs := make([]error, len(calls))

	// 限制并发数，避免资源耗尽
	sem := make(chan struct{}, defaultBatchConcurrency)

	var wg sync.WaitGroup
	for i, tc := range calls {
		if tc == nil {
			errs[i] = fmt.Errorf("tool call at index %d is nil", i)
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, call *FunctionCall) {
			defer wg.Done()
			defer func() { <-sem }()
			// perf-v5 Task 1：goroutine 顶层 panic recover
			defer func() {
				if r := recover(); r != nil {
					l := e.slogger
					if l == nil {
						l = slog.Default()
					}
					logger.FromContext(ctx, l).Error("tool batch panic",
						logger.FieldTool, call.Name,
						"panic", r,
					)
					results[idx] = NewErrorResult(fmt.Sprintf("tool panic: %v", r))
					errs[idx] = fmt.Errorf("tool panic: %v", r)
				}
			}()
			result, err := e.Execute(ctx, call)
			results[idx] = result
			errs[idx] = err
		}(i, tc)
	}
	wg.Wait()

	var firstErr error
	for _, err := range errs {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return results, firstErr
}

// extractPathFromArgs 从tool调用参数中提取 path 字段，用于 ScopePolicy 权限检查。
//
// 2026-09-28 P0 安全修复：原实现只识别 6 个固定 key、只看顶层、只取第一个
// 匹配——对 filePath/file/嵌套 {"request":{"path":...}}/MCP 风格
// {"input":{...}} 全部返回空，executor 层 ScopePolicy 检查被静默跳过
// （fail-open）。现改为：
//   1. 完整 JSON 树递归遍历（对象/数组/嵌套全部覆盖）；
//   2. 路径 key 集合扩充（含 camelCase 与常见简写）；
//   3. 顺带返回第二个发现路径（copy src→dst 类多路径参数的第二个目标）。
func extractPathFromArgs(args string) string {
	first, _ := extractPathsFromArgs(args)
	return first
}

// extractPathsFromArgs 递归提取参数中的全部路径字段（按发现顺序，最多返回前两个：
// 第一个用于 ScopePolicy 校验，第二个覆盖 copy/move 的 dst 场景）。
func extractPathsFromArgs(args string) (first, second string) {
	if args == "" {
		return "", ""
	}
	var root any
	if err := json.Unmarshal([]byte(args), &root); err != nil {
		return "", ""
	}
	var walk func(v any)
	walk = func(v any) {
		if first != "" && second != "" {
			return
		}
		switch t := v.(type) {
		case map[string]any:
			for k, val := range t {
				if s, ok := val.(string); ok && s != "" && isPathKey(k) {
					if first == "" {
						first = s
					} else if second == "" {
						second = s
					}
					continue
				}
				walk(val)
			}
		case []any:
			for _, item := range t {
				walk(item)
			}
		}
	}
	walk(root)
	return first, second
}

// isPathKey 判断 key 是否为常见的路径参数名（含 camelCase 与常见简写，
// 覆盖 MCP / 嵌套 / 各内置工具的参数命名差异）。
func isPathKey(key string) bool {
	switch strings.ToLower(key) {
	case "path", "file_path", "filepath", "file", "target_dir", "workdir",
		"directory", "dir", "output_path", "src", "source", "dst", "dest",
		"destination", "filename", "output", "outputfile", "output_file":
		return true
	}
	return false
}

// sensitiveKeyPatterns 预编译所有敏感字段的正则表达式（perf 优化：避免每次调用都 MustCompile）
var sensitiveKeyPatterns = func() []*regexp.Regexp {
	sensitiveKeys := []string{
		"password", "passwd", "secret", "token", "api_key", "apikey",
		"access_key", "secret_key", "authorization", "auth", "credential",
		"private_key", "session_token", "cookie",
	}
	patterns := make([]*regexp.Regexp, len(sensitiveKeys))
	for i, key := range sensitiveKeys {
		patterns[i] = regexp.MustCompile(`(?i)("` + regexp.QuoteMeta(key) + `"\s*:\s*)"[^"]*"`)
	}
	return patterns
}()

// redactSensitiveArgs 扫描 JSON 参数，将敏感字段值替换为 "***REDACTED***"
// 返回脱敏后的 JSON 字符串（截断到 256 字符）
// perf-v5 Task 20：避免 password / token / api_key 等敏感字段泄漏到日志
//
// 实现说明：使用预编译正则匹配常见 flat JSON 的 "key":"value" 模式。
// 对嵌套对象的深度不在本函数覆盖范围（只脱敏顶层 key），
// 复杂场景可改用 json.Decoder + 递归遍历。
func redactSensitiveArgs(args string) string {
	if args == "" {
		return ""
	}
	// 截断：避免极长 args 拖慢日志
	if len(args) > 1024 {
		args = args[:1024] + "...(truncated)"
	}

	redacted := args
	for _, pattern := range sensitiveKeyPatterns {
		// 用 ReplaceAllStringFunc 保留原 key 的大小写
		redacted = pattern.ReplaceAllStringFunc(redacted, func(match string) string {
			// match 形如 "PASSWORD":"hunter2" → 保留 "PASSWORD": 部分
			submatch := pattern.FindStringSubmatch(match)
			if len(submatch) >= 2 {
				return submatch[1] + `"***REDACTED***"`
			}
			return match
		})
	}
	if len(redacted) > 256 {
		return redacted[:256] + "...(truncated)"
	}
	return redacted
}
