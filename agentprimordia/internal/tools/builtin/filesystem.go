package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/concurrency"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools"
)

const (
	maxSearchFileSize int64 = 50 * 1024 * 1024
	maxQueryLen       int   = 500
	maxMatches        int   = 1000
)

// sensitivePatterns 敏感文件名模式（2026-09-28 P0 安全修复扩充）：
// 补入 authorized_keys / shadow / 数据库密钥库 / 其他 SSH 私钥类型，
// 匹配统一走 matchSensitivePattern（大小写不敏感，防 ".ENV" 绕过）。
var sensitivePatterns = []string{
	"*.env",
	"*.env.*",
	"*credentials*",
	"*id_rsa*",
	"*id_dsa*",
	"*id_ecdsa*",
	"*id_ed25519*",
	"authorized_keys",
	"*shadow*",
	"*.htpasswd*",
	"*.kdbx",
	"*.pem",
	"*.key",
	"*.p12",
	"*.pfx",
	"*.jks",
	"*.keystore",
	"*.secret",
	"*.token",
	"*ssh_config*",
	"*known_hosts*",
	"*.gitconfig",
	"*.git-credentials*",
	"*.npmrc",
	"*.pypirc",
	"*netrc",
}

// matchSensitivePattern 大小写不敏感地匹配敏感文件名模式。
// filepath.Match 大小写敏感，".ENV" 曾绕过 "*.env"（P0 攻击链 7）。
func matchSensitivePattern(name string) bool {
	lower := strings.ToLower(name)
	for _, pattern := range sensitivePatterns {
		if matched, _ := filepath.Match(strings.ToLower(pattern), lower); matched {
			return true
		}
	}
	return false
}

type FileSystem struct {
	rootDir      string
	scopePolicy  tools.ScopePolicy
	scopeAgent   string
	fileLock     *concurrency.FileLockManager
	maxReadSize  int64
	maxWriteSize int64
}

const (
	defaultMaxReadSize  = 4 * 1024 * 1024  // 4MB
	defaultMaxWriteSize = 10 * 1024 * 1024 // 10MB
)

func NewFileSystem(rootDir string) (*FileSystem, error) {
	abs, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("invalid root directory: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("root directory not accessible: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("root path is not a directory: %s", abs)
	}
	return &FileSystem{rootDir: abs, maxReadSize: defaultMaxReadSize, maxWriteSize: defaultMaxWriteSize}, nil
}

// WithScopePolicy 注入权限策略
func (f *FileSystem) WithScopePolicy(policy tools.ScopePolicy, agentID string) *FileSystem {
	f.scopePolicy = policy
	f.scopeAgent = agentID
	return f
}

// WithFileLock 注入文件锁管理器
func (f *FileSystem) WithFileLock(fl *concurrency.FileLockManager) *FileSystem {
	f.fileLock = fl
	return f
}

// checkScope 检查当前 Agent 是否有权限操作指定路径
func (f *FileSystem) checkScope(path string) (*tools.Result, error) {
	if f.scopePolicy == nil {
		return nil, nil
	}
	if !f.scopePolicy.Allow(f.scopeAgent, path) {
		deniedErr := tools.NewScopeDeniedError(f.scopeAgent, path)
		return tools.NewErrorResult(deniedErr.Error()), deniedErr
	}
	return nil, nil
}

// acquireLock 获取文件锁，返回释放函数
func (f *FileSystem) acquireLock(path string) (release func()) {
	if f.fileLock == nil {
		return func() {}
	}
	f.fileLock.Acquire(path)
	return func() { f.fileLock.Release(path) }
}

// openAndValidate 安全地打开文件并在打开后验证符号链接不会逃逸到根目录外
func (f *FileSystem) openAndValidate(path string, flag int, perm os.FileMode) (*os.File, error) {
	file, err := os.OpenFile(path, flag, perm)
	if err != nil {
		return nil, err
	}

	// 打开后立即验证 symlink 是否逃逸
	evalRoot, rootEvalErr := filepath.EvalSymlinks(f.rootDir)
	if rootEvalErr != nil {
		evalRoot = f.rootDir
	}
	evalRoot = filepath.Clean(evalRoot)

	evalPath, evalErr := filepath.EvalSymlinks(path)
	if evalErr == nil {
		evalPath = filepath.Clean(evalPath)
		if !strings.HasPrefix(evalPath, evalRoot+string(os.PathSeparator)) && evalPath != evalRoot {
			file.Close()
			if flag&os.O_CREATE != 0 || flag&os.O_TRUNC != 0 {
				os.Remove(path)
			}
			return nil, fmt.Errorf("access denied: symlink target is outside allowed root directory")
		}
	} else if !os.IsNotExist(evalErr) {
		// 文件已打开但 symlink 无法解析 — 可能是损坏的符号链接，access denied
		file.Close()
		if flag&os.O_CREATE != 0 || flag&os.O_TRUNC != 0 {
			os.Remove(path)
		}
		return nil, fmt.Errorf("access denied: cannot resolve path symlinks: %v", evalErr)
	}

	return file, nil
}

func (f *FileSystem) Name() string { return "filesystem" }

func (f *FileSystem) Description() string {
	return "File system tool for reading, writing, editing files and managing directories. Supports read, write, edit, list_dir, search, search_directory, and file_info operations."
}

func (f *FileSystem) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "action": {"type": "string", "enum": ["read", "write", "edit", "list_dir", "search", "search_directory", "file_info"], "description": "The operation to perform"},
    "path": {"type": "string", "description": "Relative file or directory path"},
    "content": {"type": "string", "description": "Content to write (for write action)"},
    "old_str": {"type": "string", "description": "String to replace (for edit action)"},
    "new_str": {"type": "string", "description": "Replacement string (for edit action)"},
    "query": {"type": "string", "description": "Search query (for search and search_directory actions)"},
    "include": {"type": "string", "description": "File pattern filter for search_directory (e.g. '*.go')"},
    "max_results": {"type": "number", "description": "Maximum results for search_directory (default: 50)"},
    "start_line": {"type": "number", "description": "Start line number (1-based, for read action)"},
    "end_line": {"type": "number", "description": "End line number (inclusive, for read action)"}
  },
  "required": ["action", "path"]
}`)
}

func (f *FileSystem) Execute(ctx context.Context, args json.RawMessage) (*tools.Result, error) {
	var params map[string]json.RawMessage
	if err := json.Unmarshal(args, &params); err != nil {
		return tools.NewErrorResult(fmt.Sprintf("invalid arguments: %v", err)), nil
	}

	action := ""
	if err := unmarshalRaw(params["action"], &action); err != nil {
		return tools.NewErrorResult(fmt.Sprintf("invalid parameter 'action': %v", err)), nil
	}

	rawPath := ""
	if err := unmarshalRaw(params["path"], &rawPath); err != nil {
		return tools.NewErrorResult(fmt.Sprintf("invalid parameter 'path': %v", err)), nil
	}

	cleanPath := filepath.Clean(rawPath)
	// 不再通过子串匹配拒绝包含 ".." 的文件名（如 "..gitignore"），
	// 真正的目录穿越由下面的绝对路径根目录前缀检查捕获。

	fullPath := filepath.Join(f.rootDir, cleanPath)

	absFullPath, err := filepath.Abs(fullPath)
	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("invalid path: %v", err)), nil
	}

	absRoot, err := filepath.Abs(f.rootDir)
	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("invalid root path: %v", err)), nil
	}

	if !strings.HasPrefix(absFullPath, absRoot+string(os.PathSeparator)) && absFullPath != absRoot {
		return tools.NewErrorResult("access denied: path is outside allowed root directory"), nil
	}

	// 符号链接逃逸检查：解析已存在路径的 symlink，验证解析后仍在根目录内
	evalRoot, rootEvalErr := filepath.EvalSymlinks(absRoot)
	if rootEvalErr != nil {
		evalRoot = absRoot
	}
	evalRoot = filepath.Clean(evalRoot)

	evalPath, evalErr := filepath.EvalSymlinks(absFullPath)
	if evalErr == nil {
		evalPath = filepath.Clean(evalPath)
		if !strings.HasPrefix(evalPath, evalRoot+string(os.PathSeparator)) && evalPath != evalRoot {
			return tools.NewErrorResult("access denied: symlink target is outside allowed root directory"), fmt.Errorf("access denied: symlink target is outside allowed root directory")
		}
	} else if !os.IsNotExist(evalErr) {
		// 路径存在但无法解析 symlink — 可能是损坏的符号链接或权限问题，access denied
		return tools.NewErrorResult(fmt.Sprintf("access denied: cannot resolve path symlinks: %v", evalErr)), fmt.Errorf("access denied: cannot resolve path symlinks: %v", evalErr)
	}
	// os.IsNotExist: 路径尚不存在（如写入新文件），验证父目录安全性
	if evalErr != nil && os.IsNotExist(evalErr) {
		parentPath := filepath.Dir(absFullPath)
		if evalParent, parentErr := filepath.EvalSymlinks(parentPath); parentErr == nil {
			evalParent = filepath.Clean(evalParent)
			if !strings.HasPrefix(evalParent, evalRoot+string(os.PathSeparator)) && evalParent != evalRoot {
				return tools.NewErrorResult("access denied: parent directory symlink target is outside allowed root directory"), fmt.Errorf("access denied: parent directory symlink target is outside allowed root directory")
			}
		} else if !os.IsNotExist(parentErr) {
			return tools.NewErrorResult(fmt.Sprintf("access denied: cannot resolve parent directory symlinks: %v", parentErr)), fmt.Errorf("access denied: cannot resolve parent directory symlinks: %v", parentErr)
		}
	}

	// 敏感文件保护：读/写/编辑均拒绝（大小写不敏感）
	if matchSensitivePattern(filepath.Base(cleanPath)) {
		return tools.NewErrorResult(fmt.Sprintf("access denied: sensitive file '%s' is protected", cleanPath)), nil
	}

	switch action {
	case "read":
		if errResult, err := f.checkScope(absFullPath); errResult != nil {
			return errResult, err
		}
		return f.readFile(ctx, absFullPath, params)
	case "write":
		if errResult, err := f.checkScope(absFullPath); errResult != nil {
			return errResult, err
		}
		return f.writeFile(ctx, absFullPath, params)
	case "edit":
		if errResult, err := f.checkScope(absFullPath); errResult != nil {
			return errResult, err
		}
		return f.editFile(ctx, absFullPath, params)
	case "list_dir":
		if errResult, err := f.checkScope(absFullPath); errResult != nil {
			return errResult, err
		}
		return f.listDir(absFullPath)
	case "search":
		if errResult, err := f.checkScope(absFullPath); errResult != nil {
			return errResult, err
		}
		return f.searchInFile(absFullPath, params)
	case "search_directory":
		if errResult, err := f.checkScope(absFullPath); errResult != nil {
			return errResult, err
		}
		return f.searchDirectory(ctx, absFullPath, params)
	case "file_info":
		if errResult, err := f.checkScope(absFullPath); errResult != nil {
			return errResult, err
		}
		return f.fileInfo(absFullPath)
	default:
		return tools.NewErrorResult(fmt.Sprintf("unknown action: %s", action)), nil
	}
}

func (f *FileSystem) readFile(_ context.Context, path string, params map[string]json.RawMessage) (*tools.Result, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return tools.NewErrorResult(fmt.Sprintf("file not found: %s", path)), nil
		}
		return tools.NewErrorResult(fmt.Sprintf("stat error: %v", err)), nil
	}

	if f.maxReadSize > 0 && info.Size() > f.maxReadSize {
		return tools.NewErrorResult(fmt.Sprintf("file too large: %d bytes (max %d bytes)", info.Size(), f.maxReadSize)), nil
	}

	file, err := f.openAndValidate(path, os.O_RDONLY, 0)
	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("open error: %v", err)), nil
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, f.maxReadSize+1))
	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("read error: %v", err)), nil
	}

	content := string(data)
	lines := strings.Split(content, "\n")

	startLine := 1
	endLine := len(lines)

	if raw, ok := params["start_line"]; ok && len(raw) > 0 {
		var v float64
		if err := unmarshalRaw(raw, &v); err != nil {
			return tools.NewErrorResult(fmt.Sprintf("invalid parameter 'start_line': %v", err)), nil
		}
		startLine = int(v)
	}
	if raw, ok := params["end_line"]; ok && len(raw) > 0 {
		var v float64
		if err := unmarshalRaw(raw, &v); err != nil {
			return tools.NewErrorResult(fmt.Sprintf("invalid parameter 'end_line': %v", err)), nil
		}
		endLine = int(v)
	}

	if startLine > 1 || endLine < len(lines) {
		if startLine < 1 {
			startLine = 1
		}
		if endLine > len(lines) {
			endLine = len(lines)
		}
		if startLine <= endLine {
			lines = lines[startLine-1 : endLine]
			content = strings.Join(lines, "\n")
		}
	}

	return tools.NewResult(content), nil
}

func (f *FileSystem) writeFile(_ context.Context, path string, params map[string]json.RawMessage) (*tools.Result, error) {
	release := f.acquireLock(path)
	defer release()

	var content struct {
		Value string `json:"content"`
	}
	if err := unmarshalRaw(params["content"], &content.Value); err != nil {
		return tools.NewErrorResult(fmt.Sprintf("invalid parameter 'content': %v", err)), nil
	}

	if f.maxWriteSize > 0 && int64(len(content.Value)) > f.maxWriteSize {
		return tools.NewErrorResult(fmt.Sprintf("content too large: %d bytes (max %d bytes)", len(content.Value), f.maxWriteSize)), nil
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return tools.NewErrorResult(fmt.Sprintf("cannot create directory: %v", err)), nil
	}

	file, err := f.openAndValidate(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("open error: %v", err)), nil
	}
	defer file.Close()

	if _, err := file.WriteString(content.Value); err != nil {
		return tools.NewErrorResult(fmt.Sprintf("write error: %v", err)), nil
	}
	return tools.NewResult(fmt.Sprintf("successfully wrote %d bytes to %s", len(content.Value), path)), nil
}

func (f *FileSystem) editFile(_ context.Context, path string, params map[string]json.RawMessage) (*tools.Result, error) {
	release := f.acquireLock(path)
	defer release()

	file, err := f.openAndValidate(path, os.O_RDWR, 0)
	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("open error: %v", err)), nil
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("read error: %v", err)), nil
	}

	var editParams struct {
		OldStr string `json:"old_str"`
		NewStr string `json:"new_str"`
	}
	if err := unmarshalRaw(params["old_str"], &editParams.OldStr); err != nil {
		return tools.NewErrorResult(fmt.Sprintf("invalid parameter 'old_str': %v", err)), nil
	}
	if err := unmarshalRaw(params["new_str"], &editParams.NewStr); err != nil {
		return tools.NewErrorResult(fmt.Sprintf("invalid parameter 'new_str': %v", err)), nil
	}

	if editParams.OldStr == "" {
		return tools.NewErrorResult("old_str is required for edit operation"), nil
	}

	content := string(data)
	if !strings.Contains(content, editParams.OldStr) {
		return tools.NewErrorResult(fmt.Sprintf("old_str not found in file: %s", path)), nil
	}

	count := strings.Count(content, editParams.OldStr)
	if count > 1 {
		return tools.NewErrorResult(fmt.Sprintf(
			"old_string found %d times in %s, expected exactly 1 occurrence. "+
				"Please provide more context to make the match unique.", count, path)), nil
	}

	newContent := strings.Replace(content, editParams.OldStr, editParams.NewStr, 1)
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return tools.NewErrorResult(fmt.Sprintf("seek error: %v", err)), nil
	}
	if err := file.Truncate(int64(len(newContent))); err != nil {
		return tools.NewErrorResult(fmt.Sprintf("truncate error: %v", err)), nil
	}
	if _, err := file.WriteString(newContent); err != nil {
		return tools.NewErrorResult(fmt.Sprintf("write error after edit: %v", err)), nil
	}
	return tools.NewResult(fmt.Sprintf("successfully replaced 1 occurrence in %s", path)), nil
}

func (f *FileSystem) listDir(path string) (*tools.Result, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		if os.IsNotExist(err) {
			return tools.NewErrorResult(fmt.Sprintf("directory not found: %s", path)), nil
		}
		return tools.NewErrorResult(fmt.Sprintf("list directory error: %v", err)), nil
	}

	type entryInfo struct {
		Name  string `json:"name"`
		IsDir bool   `json:"is_dir"`
		Size  int64  `json:"size"`
	}

	var result []entryInfo
	for _, e := range entries {
		info, _ := e.Info()
		result = append(result, entryInfo{
			Name:  e.Name(),
			IsDir: e.IsDir(),
			Size:  info.Size(),
		})
	}

	output, _ := json.MarshalIndent(result, "", "  ")
	return tools.NewResult(string(output)), nil
}

func (f *FileSystem) searchInFile(path string, params map[string]json.RawMessage) (*tools.Result, error) {
	// Limit file size to prevent OOM on large files
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return tools.NewErrorResult(fmt.Sprintf("file not found: %s", path)), nil
		}
		return tools.NewErrorResult(fmt.Sprintf("stat error: %v", err)), nil
	}
	if info.Size() > maxSearchFileSize {
		return tools.NewErrorResult(fmt.Sprintf("file too large for search: %d bytes (max %d)", info.Size(), maxSearchFileSize)), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("read error: %v", err)), nil
	}

	var query struct {
		Value string `json:"query"`
	}
	if err := unmarshalRaw(params["query"], &query.Value); err != nil {
		return tools.NewErrorResult(fmt.Sprintf("invalid parameter 'query': %v", err)), nil
	}

	if query.Value == "" {
		return tools.NewErrorResult("query is required for search operation"), nil
	}

	// Limit regex complexity to prevent ReDoS
	if len(query.Value) > maxQueryLen {
		return tools.NewErrorResult(fmt.Sprintf("search query too long: %d chars (max %d)", len(query.Value), maxQueryLen)), nil
	}

	content := string(data)
	lines := strings.Split(content, "\n")

	type match struct {
		LineNumber int    `json:"line_number"`
		Line       string `json:"line"`
	}

	var matches []match

	// 默认使用纯字符串匹配；仅在显式启用 regex 模式时使用正则
	useRegex := false
	var re *regexp.Regexp
	if raw, ok := params["regex"]; ok {
		var regexFlag bool
		if err := unmarshalRaw(raw, &regexFlag); err != nil {
			return tools.NewErrorResult(fmt.Sprintf("invalid parameter 'regex': %v", err)), nil
		}
		if regexFlag {
			// ReDoS 防护：限制正则复杂度，检测危险的重复量词嵌套
			if hasReDoSPattern(query.Value) {
				return tools.NewErrorResult("regex query rejected: potentially catastrophic backtracking pattern"), nil
			}
			compiled, reErr := regexp.Compile(query.Value)
			if reErr == nil {
				re = compiled
				useRegex = true
			}
		}
	}

	// Limit matches to prevent excessive results

	for i, line := range lines {
		if len(matches) >= maxMatches {
			break
		}
		if useRegex && re != nil {
			if re.MatchString(line) {
				matches = append(matches, match{LineNumber: i + 1, Line: line})
			}
		} else if strings.Contains(line, query.Value) {
			matches = append(matches, match{LineNumber: i + 1, Line: line})
		}
	}

	if len(matches) == 0 {
		return tools.NewResult(fmt.Sprintf("no matches found for '%s'", query.Value)), nil
	}

	output, _ := json.MarshalIndent(matches, "", "  ")
	return tools.NewResult(fmt.Sprintf("found %d match(es):\n%s", len(matches), string(output))), nil
}

func (f *FileSystem) fileInfo(path string) (*tools.Result, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return tools.NewErrorResult(fmt.Sprintf("file not found: %s", path)), nil
		}
		return tools.NewErrorResult(fmt.Sprintf("stat error: %v", err)), nil
	}

	fileInfo := map[string]any{
		"name":         info.Name(),
		"size":         info.Size(),
		"mode":         info.Mode().String(),
		"mod_time":     info.ModTime().Format(time.RFC3339),
		"is_directory": info.IsDir(),
	}

	if !info.IsDir() {
		ext := filepath.Ext(path)
		fileInfo["extension"] = ext
	}

	output, _ := json.MarshalIndent(fileInfo, "", "  ")
	return tools.NewResult(string(output)), nil
}

// hasReDoSPattern 检测正则表达式中可能导致灾难性回溯的模式
func hasReDoSPattern(pattern string) bool {
	// 检测嵌套重复量词，如 (a+)+, (a*)*, (a+)*, (a*)+
	reDoSPattern := regexp.MustCompile(`\([^)]*[+*][^)]*\)[+*]`)
	if reDoSPattern.MatchString(pattern) {
		return true
	}
	// 检测交替重叠重复，如 (a|a)+
	altRepeat := regexp.MustCompile(`\([^)]*\|[^)]*\)[+*]{1,2}`)
	if altRepeat.MatchString(pattern) {
		return true
	}
	return false
}

type searchDirectoryParams struct {
	Path       string `json:"path"`
	Query      string `json:"query"`
	Include    string `json:"include,omitempty"`
	MaxResults int    `json:"max_results,omitempty"`
}

func (f *FileSystem) searchDirectory(_ context.Context, dirPath string, params map[string]json.RawMessage) (*tools.Result, error) {
	var p searchDirectoryParams
	if raw, ok := params["query"]; ok && len(raw) > 0 {
		if err := unmarshalRaw(raw, &p.Query); err != nil {
			return tools.NewErrorResult(fmt.Sprintf("invalid parameter 'query': %v", err)), nil
		}
	}
	if p.Query == "" {
		return tools.NewErrorResult("query is required for search_directory operation"), nil
	}
	if raw, ok := params["include"]; ok && len(raw) > 0 {
		if err := unmarshalRaw(raw, &p.Include); err != nil {
			return tools.NewErrorResult(fmt.Sprintf("invalid parameter 'include': %v", err)), nil
		}
	}
	maxResults := 50
	if raw, ok := params["max_results"]; ok && len(raw) > 0 {
		var v float64
		if err := unmarshalRaw(raw, &v); err != nil {
			return tools.NewErrorResult(fmt.Sprintf("invalid parameter 'max_results': %v", err)), nil
		}
		if v > 0 {
			maxResults = int(v)
		}
	}

	info, err := os.Stat(dirPath)
	if err != nil {
		if os.IsNotExist(err) {
			return tools.NewErrorResult(fmt.Sprintf("directory not found: %s", dirPath)), nil
		}
		return tools.NewErrorResult(fmt.Sprintf("stat error: %v", err)), nil
	}
	if !info.IsDir() {
		return tools.NewErrorResult(fmt.Sprintf("path is not a directory: %s", dirPath)), nil
	}

	type matchEntry struct {
		File    string `json:"file"`
		Line    int    `json:"line"`
		Content string `json:"content"`
	}

	var matches []matchEntry

	_ = filepath.WalkDir(dirPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}

		if p.Include != "" {
			matched, matchErr := filepath.Match(p.Include, d.Name())
			if matchErr != nil || !matched {
				return nil
			}
		}

		if matchSensitivePattern(d.Name()) {
			return nil
		}

		fileInfo, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		if fileInfo.Size() > maxSearchFileSize {
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}

		lines := strings.Split(string(data), "\n")
		relPath, _ := filepath.Rel(dirPath, path)

		for i, line := range lines {
			if len(matches) >= maxResults {
				return fmt.Errorf("max results reached")
			}
			if strings.Contains(line, p.Query) {
				matches = append(matches, matchEntry{
					File:    relPath,
					Line:    i + 1,
					Content: line,
				})
			}
		}
		return nil
	})

	if len(matches) == 0 {
		return tools.NewResult(fmt.Sprintf("no matches found for '%s' in directory %s", p.Query, dirPath)), nil
	}

	var formatted []string
	for _, m := range matches {
		formatted = append(formatted, fmt.Sprintf("%s:%d:%s", m.File, m.Line, m.Content))
	}

	output, _ := json.MarshalIndent(matches, "", "  ")
	return tools.NewResult(fmt.Sprintf("found %d match(es):\n%s\n---\n%s", len(matches), string(output), strings.Join(formatted, "\n"))), nil
}
