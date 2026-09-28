package tools

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动
)

// ===== 路径安全（CSVTool rootDir 禁锢 / 敏感文件保护，P2 安全修复）=====
//
// 背景：CSVTool 曾用 os.Open/os.Create 对任意路径读写，无 rootDir/scope/
// 敏感文件保护，是绕过 filesystem 工具全部防护的一条路径。此处仿
// internal/tools/builtin/filesystem.go 的模式补足同等级防护：
//   - rootDir 禁锢（NewCSVToolWithRoot / WithRootDir opt-in，legacy 构造不变）；
//   - 路径穿越（".." 与绝对路径越界）拒绝；
//   - 符号链接逃逸拒绝；
//   - 敏感文件模式拒绝（大小写不敏感，规则与 builtin/filesystem.go 的
//     sensitivePatterns 保持一致——该列表在包内未导出，此处同规则复刻，
//     修改时需同步两处）。

// dataToolSensitivePatterns 敏感文件名模式（与 builtin/filesystem.go 的
// sensitivePatterns 同规则；匹配大小写不敏感，防 ".ENV" 绕过）。
var dataToolSensitivePatterns = []string{
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

// matchSensitiveFileName 大小写不敏感地匹配敏感文件名模式。
func matchSensitiveFileName(name string) bool {
	lower := strings.ToLower(name)
	for _, pattern := range dataToolSensitivePatterns {
		if matched, _ := filepath.Match(strings.ToLower(pattern), lower); matched {
			return true
		}
	}
	return false
}

// isWithinRoot 判断 absPath 是否位于 absRoot 内（含自身）。
func isWithinRoot(absPath, absRoot string) bool {
	return absPath == absRoot || strings.HasPrefix(absPath, absRoot+string(os.PathSeparator))
}

// checkSymlinkEscape 校验路径（或其最近已存在父目录）解析符号链接后
// 仍在根目录内。路径不存在时（如写入新文件）校验父目录。
func checkSymlinkEscape(absPath, absRoot string) error {
	evalRoot, rootErr := filepath.EvalSymlinks(absRoot)
	if rootErr != nil {
		evalRoot = absRoot
	}
	evalRoot = filepath.Clean(evalRoot)

	evalPath, evalErr := filepath.EvalSymlinks(absPath)
	if evalErr == nil {
		evalPath = filepath.Clean(evalPath)
		if !isWithinRoot(evalPath, evalRoot) {
			return fmt.Errorf("access denied: symlink target is outside allowed root directory")
		}
		return nil
	}
	if !os.IsNotExist(evalErr) {
		return fmt.Errorf("access denied: cannot resolve path symlinks: %v", evalErr)
	}
	// 路径尚不存在：校验父目录
	parent := filepath.Dir(absPath)
	evalParent, parentErr := filepath.EvalSymlinks(parent)
	if parentErr == nil {
		if !isWithinRoot(filepath.Clean(evalParent), evalRoot) {
			return fmt.Errorf("access denied: parent directory symlink target is outside allowed root directory")
		}
	} else if !os.IsNotExist(parentErr) {
		return fmt.Errorf("access denied: cannot resolve parent directory symlinks: %v", parentErr)
	}
	return nil
}

// ===== CSV 处理tool =====

// CSVTool 是 CSV 文件处理tool
type CSVTool struct {
	name        string
	description string
	// rootDir 禁锢根目录（绝对路径）。为空表示 legacy 模式：不设路径禁锢，
	// 仅保留敏感文件保护。安全敏感场景应使用 NewCSVToolWithRoot 或
	// WithRootDir 显式禁锢。
	rootDir string
}

// NewCSVTool 创建新的 CSV tool（legacy 兼容：不设 rootDir 禁锢）。
// 安全敏感场景请使用 NewCSVToolWithRoot 或 WithRootDir 显式禁锢根目录。
func NewCSVTool() *CSVTool {
	return &CSVTool{
		name: "csv_processor",
		description: `处理 CSV 文件的tool，支持读取、解析、查询、转换和写入操作。
功能：
- 读取 CSV 文件并解析为结构化数据
- 按列名或索引筛选数据
- 执行过滤和聚合操作
- 将结果转换为 JSON 格式
- 写入新的 CSV 文件

参数：
- action (required): 操作类型 [read|query|filter|aggregate|write]
- file_path (required for read/write): CSV 文件路径
- data (required for write): 要写入的数据（JSON 数组）
- columns (optional): 指定要读取的列名列表
- filter_column (optional): 过滤的列名
- filter_value (optional): 过滤的值
- aggregate_column (optional): 聚合的列名
- aggregate_func (optional): 聚合函数 [sum|avg|min|max|count]`,
	}
}

// NewCSVToolWithRoot 创建带 rootDir 禁锢的 CSV tool（仿 filesystem.go）。
// 之后所有读写路径必须解析到 rootDir 内，越界/穿越/symlink 逃逸均被拒。
func NewCSVToolWithRoot(rootDir string) (*CSVTool, error) {
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
	return NewCSVTool().WithRootDir(abs), nil
}

// WithRootDir 设置 rootDir 禁锢（链式）。空字符串表示清除禁锢（恢复 legacy）。
func (t *CSVTool) WithRootDir(rootDir string) *CSVTool {
	if strings.TrimSpace(rootDir) == "" {
		t.rootDir = ""
		return t
	}
	if abs, err := filepath.Abs(rootDir); err == nil {
		t.rootDir = abs
	}
	return t
}

// resolveCSVPath 校验并解析 CSV 文件路径：
//  1. 敏感文件模式拒绝（大小写不敏感，无论是否配置 rootDir）；
//  2. 配置 rootDir 时：相对路径相对 rootDir 解析，绝对路径必须在 rootDir 内，
//     拒绝 ".." 穿越与 symlink 逃逸；未配置时保持 legacy 行为（原样返回）。
func (t *CSVTool) resolveCSVPath(filePath string) (string, error) {
	if strings.TrimSpace(filePath) == "" {
		return "", fmt.Errorf("parameter 'file_path' must be a non-empty string")
	}
	clean := filepath.Clean(filePath)
	if matchSensitiveFileName(filepath.Base(clean)) {
		return "", fmt.Errorf("access denied: sensitive file '%s' is protected", clean)
	}
	if t.rootDir == "" {
		// legacy 模式：不禁锢（兼容既有调用方），敏感文件保护已生效
		return filePath, nil
	}

	full := clean
	if !filepath.IsAbs(full) {
		full = filepath.Join(t.rootDir, full)
	}
	absFull, err := filepath.Abs(full)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	if !isWithinRoot(absFull, t.rootDir) {
		return "", fmt.Errorf("access denied: path is outside allowed root directory")
	}
	if err := checkSymlinkEscape(absFull, t.rootDir); err != nil {
		return "", err
	}
	return absFull, nil
}

func (t *CSVTool) Name() string        { return t.name }
func (t *CSVTool) Description() string { return t.description }

func (t *CSVTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"action": {"type": "string", "enum": ["read", "query", "filter", "aggregate", "write"]},
			"file_path": {"type": "string"},
			"data": {"type": "array"},
			"columns": {"type": "array", "items": {"type": "string"}},
			"filter_column": {"type": "string"},
			"filter_value": {"type": "string"},
			"aggregate_column": {"type": "string"},
			"aggregate_func": {"type": "string", "enum": ["sum", "avg", "min", "max", "count"]}
		},
		"required": ["action"]
	}`)
}

func (t *CSVTool) Execute(ctx context.Context, input json.RawMessage) (*Result, error) {
	var params map[string]any
	if err := json.Unmarshal(input, &params); err != nil {
		return nil, fmt.Errorf("parse parameters error: %w", err)
	}
	action, ok := params["action"].(string)
	if !ok {
		return nil, fmt.Errorf("parameter 'action' must be a string")
	}
	switch action {
	case "read":
		return t.readCSV(params)
	case "query":
		return t.readCSV(params)
	case "filter":
		return t.filterCSV(params)
	case "aggregate":
		return t.aggregateCSV(params)
	case "write":
		return t.writeCSV(params)
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}

func (t *CSVTool) readCSV(params map[string]any) (*Result, error) {
	filePath, ok := params["file_path"].(string)
	if !ok {
		return nil, fmt.Errorf("parameter 'file_path' must be a string")
	}
	// 路径 jail 校验：rootDir 禁锢 + 穿越拒绝 + 敏感文件保护
	resolvedPath, err := t.resolveCSVPath(filePath)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("open file error: %w", err)
	}
	defer func() { _ = file.Close() }()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read CSV error: %w", err)
	}

	result := make([]map[string]any, 0)
	if len(records) > 0 {
		headers := records[0]
		var selectedColumns []string
		if cols, ok := params["columns"].([]any); ok {
			for _, c := range cols {
				col, ok := c.(string)
				if !ok {
					return nil, fmt.Errorf("parameter 'columns' must be an array of strings")
				}
				selectedColumns = append(selectedColumns, col)
			}
		} else {
			selectedColumns = headers
		}
		for i := 1; i < len(records); i++ {
			row := make(map[string]any)
			for j, header := range headers {
				if containsStr(selectedColumns, header) && j < len(records[i]) {
					row[header] = parseValue(records[i][j])
				}
			}
			result = append(result, row)
		}
	}

	output, _ := json.MarshalIndent(result, "", "  ")
	colCount := 0
	if len(result) > 0 {
		colCount = len(result[0])
	}
	return &Result{
		Content: string(output),
		Metadata: map[string]any{
			"rows_count":    strconv.Itoa(len(result)),
			"columns_count": strconv.Itoa(colCount),
			"tool":          "csv_processor",
		},
	}, nil
}

func (t *CSVTool) filterCSV(params map[string]any) (*Result, error) {
	filterColumn, ok := params["filter_column"].(string)
	if !ok {
		return nil, fmt.Errorf("parameter 'filter_column' must be a string")
	}
	filterValue, ok := params["filter_value"].(string)
	if !ok {
		return nil, fmt.Errorf("parameter 'filter_value' must be a string")
	}

	dataResult, err := t.readCSV(map[string]any{"action": "read", "file_path": params["file_path"]})
	if err != nil {
		return nil, err
	}

	var allData []map[string]any
	if err := json.Unmarshal([]byte(dataResult.Content), &allData); err != nil {
		return nil, fmt.Errorf("parse filter data error: %w", err)
	}

	var filtered []map[string]any
	for _, row := range allData {
		if val, ok := row[filterColumn]; ok {
			if fmt.Sprintf("%v", val) == filterValue {
				filtered = append(filtered, row)
			}
		}
	}

	output, _ := json.MarshalIndent(filtered, "", "  ")
	return &Result{
		Content: string(output),
		Metadata: map[string]any{
			"rows_before_filter": strconv.Itoa(len(allData)),
			"rows_after_filter":  strconv.Itoa(len(filtered)),
			"filter_column":      filterColumn,
			"filter_value":       filterValue,
		},
	}, nil
}

func (t *CSVTool) aggregateCSV(params map[string]any) (*Result, error) {
	aggColumn, ok := params["aggregate_column"].(string)
	if !ok {
		return nil, fmt.Errorf("parameter 'aggregate_column' must be a string")
	}
	aggFunc, ok := params["aggregate_func"].(string)
	if !ok {
		return nil, fmt.Errorf("parameter 'aggregate_func' must be a string")
	}

	dataResult, err := t.readCSV(map[string]any{"action": "read", "file_path": params["file_path"]})
	if err != nil {
		return nil, err
	}

	var allData []map[string]any
	if err := json.Unmarshal([]byte(dataResult.Content), &allData); err != nil {
		return nil, fmt.Errorf("parse aggregate data error: %w", err)
	}

	var result float64
	count := 0

	for _, row := range allData {
		if val, ok := row[aggColumn]; ok {
			switch v := val.(type) {
			case float64:
				switch aggFunc {
				case "sum":
					result += v
				case "avg":
					result += v
				case "min":
					if count == 0 || v < result {
						result = v
					}
				case "max":
					if count == 0 || v > result {
						result = v
					}
				}
				count++
			}
		}
	}

	if aggFunc == "avg" && count > 0 {
		result /= float64(count)
	}
	if aggFunc == "count" {
		result = float64(count)
	}

	output := map[string]any{"function": aggFunc, "column": aggColumn, "value": result, "count": count}
	outputJSON, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal aggregate result: %w", err)
	}
	return &Result{Content: string(outputJSON)}, nil
}

func (t *CSVTool) writeCSV(params map[string]any) (*Result, error) {
	filePath, ok := params["file_path"].(string)
	if !ok {
		return nil, fmt.Errorf("parameter 'file_path' must be a string")
	}
	// 路径 jail 校验：rootDir 禁锢 + 穿越拒绝 + 敏感文件保护
	resolvedPath, err := t.resolveCSVPath(filePath)
	if err != nil {
		return nil, err
	}
	dataBytes, err := json.Marshal(params["data"])
	if err != nil {
		return nil, fmt.Errorf("failed to marshal CSV data: %w", err)
	}

	var data []map[string]any
	if err := json.Unmarshal(dataBytes, &data); err != nil {
		return nil, fmt.Errorf("invalid 'data' parameter: %w", err)
	}

	file, err := os.Create(resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("create file error: %w", err)
	}
	defer func() { _ = file.Close() }()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	if len(data) == 0 {
		return &Result{
			Content:  fmt.Sprintf("CSV written to %s with 0 rows", filePath),
			Metadata: map[string]any{"file": filePath, "rows": 0},
		}, nil
	}

	headers := make([]string, 0, len(data[0]))
	for key := range data[0] {
		headers = append(headers, key)
	}
	_ = writer.Write(headers)

	for _, row := range data {
		record := make([]string, len(headers))
		for i, header := range headers {
			record[i] = fmt.Sprintf("%v", row[header])
		}
		_ = writer.Write(record)
	}

	return &Result{
		Content:  fmt.Sprintf("Successfully wrote %d rows to %s", len(data), filePath),
		Metadata: map[string]any{"rows_written": strconv.Itoa(len(data)), "file_path": filePath},
	}, nil
}

// ===== JSON 处理tool =====

// JSONTool 是 JSON 数据处理tool
type JSONTool struct {
	name        string
	description string
}

// NewJSONTool 创建新的 JSON tool
func NewJSONTool() *JSONTool {
	return &JSONTool{
		name: "json_processor",
		description: `处理 JSON 数据的tool，支持解析、查询、转换和验证。
功能：解析 JSON、路径查询、数据转换、Schema 验证、对象合并`,
	}
}

func (t *JSONTool) Name() string        { return t.name }
func (t *JSONTool) Description() string { return t.description }

func (t *JSONTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"action": {"type": "string", "enum": ["parse", "query", "transform", "validate", "merge"]},
			"data": {"type": ["string", "object"]},
			"path": {"type": "string"},
			"schema": {"type": "object"}
		},
		"required": ["action", "data"]
	}`)
}

func (t *JSONTool) Execute(ctx context.Context, input json.RawMessage) (*Result, error) {
	var params map[string]any
	if err := json.Unmarshal(input, &params); err != nil {
		return nil, fmt.Errorf("parse parameters error: %w", err)
	}
	action, ok := params["action"].(string)
	if !ok {
		return nil, fmt.Errorf("parameter 'action' must be a string")
	}
	switch action {
	case "parse":
		return t.parseJSON(params)
	case "query":
		return t.queryJSON(params)
	case "transform":
		return t.transformJSON(params)
	case "validate":
		return t.validateJSON(params)
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}

func (t *JSONTool) parseJSON(params map[string]any) (*Result, error) {
	var data any
	dataStr, ok := params["data"].(string)
	if !ok {
		return nil, fmt.Errorf("parameter 'data' must be a string")
	}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return nil, fmt.Errorf("parse JSON error: %w", err)
	}
	output, _ := json.MarshalIndent(data, "", "  ")
	return &Result{Content: string(output), Metadata: map[string]any{"parsed": "true"}}, nil
}

func (t *JSONTool) queryJSON(params map[string]any) (*Result, error) {
	path, ok := params["path"].(string)
	if !ok {
		return nil, fmt.Errorf("parameter 'path' must be a string")
	}
	var data any
	dataStr, ok := params["data"].(string)
	if !ok {
		return nil, fmt.Errorf("parameter 'data' must be a string")
	}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return nil, fmt.Errorf("parse JSON error: %w", err)
	}
	result := queryByPath(data, strings.Split(path, "."))
	output, _ := json.MarshalIndent(result, "", "  ")
	return &Result{Content: string(output), Metadata: map[string]any{"path": path}}, nil
}

func (t *JSONTool) transformJSON(params map[string]any) (*Result, error) {
	var data any
	dataStr, ok := params["data"].(string)
	if !ok {
		return nil, fmt.Errorf("parameter 'data' must be a string")
	}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return nil, fmt.Errorf("parse JSON error: %w", err)
	}
	rule, ok := params["transform_rule"].(string)
	if !ok {
		return nil, fmt.Errorf("parameter 'transform_rule' must be a string")
	}
	transformed := applyTransform(data, rule)
	output, _ := json.MarshalIndent(transformed, "", "  ")
	return &Result{Content: string(output), Metadata: map[string]any{"transform_rule": rule}}, nil
}

func (t *JSONTool) validateJSON(params map[string]any) (*Result, error) {
	dataStr, ok := params["data"].(string)
	if !ok {
		return &Result{Content: `{"valid": false, "error": "parameter 'data' must be a string"}`}, nil
	}
	var data any
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return &Result{Content: fmt.Sprintf(`{"valid": false, "error": "%s"}`, err.Error())}, nil
	}
	output := map[string]any{"valid": true, "data_type": getDataType(data)}
	outputJSON, _ := json.MarshalIndent(output, "", "  ")
	return &Result{Content: string(outputJSON)}, nil
}

// ===== SQLite 处理tool =====

// dangerousSQLKeywords 危险 SQL 关键字列表
var dangerousSQLKeywords = []string{
	"DROP", "DELETE", "UPDATE", "INSERT", "ALTER", "CREATE",
	"TRUNCATE", "REPLACE", "ATTACH", "DETACH",
}

// validateSQLSafety 验证 SQL 语句安全性，仅允许 SELECT 查询
func validateSQLSafety(sql string) error {
	upper := strings.ToUpper(strings.TrimSpace(sql))
	if !strings.HasPrefix(upper, "SELECT") && !strings.HasPrefix(upper, "PRAGMA") {
		return fmt.Errorf("only SELECT and PRAGMA statements are allowed")
	}
	for _, kw := range dangerousSQLKeywords {
		if strings.Contains(upper, kw) {
			return fmt.Errorf("SQL statement contains forbidden keyword: %s", kw)
		}
	}
	return nil
}

// SQLiteTool 是 SQLite 数据库处理tool
type SQLiteTool struct {
	name   string
	desc   string
	db     *sql.DB
	dbPath string
	// queryRowLimit executeQuery 的默认行数上限（<=0 时用 defaultQueryRowLimit）。
	// 修复前 executeQuery 无行数上限，单条 SELECT 可将整库灌进上下文。
	queryRowLimit int
}

// defaultQueryRowLimit executeQuery 默认行数上限（无 LIMIT 子句时注入）。
const defaultQueryRowLimit = 1000

// limitClauseRe 匹配 SQL 中已存在的 LIMIT 子句（大小写不敏感，词边界）。
var limitClauseRe = regexp.MustCompile(`(?i)\blimit\b`)

// WithQueryRowLimit 设置 executeQuery 的默认行数上限（<=0 恢复默认值）。
// 仅影响未显式带 LIMIT 子句的 SELECT；SQL 中显式 LIMIT 与 params["limit"]
// 参数优先级更高。
func (t *SQLiteTool) WithQueryRowLimit(n int) *SQLiteTool {
	t.queryRowLimit = n
	return t
}

// effectiveRowLimit 计算本次查询的行数上限与是否可注入 LIMIT 子句。
// 优先级：params["limit"] > SQL 显式 LIMIT（尊重用户选择，不注入） > 默认值。
// 返回 limit<=0 表示不设上限。
func (t *SQLiteTool) effectiveRowLimit(params map[string]any, sqlStr string) (limit int, inject bool) {
	if v, ok := params["limit"].(float64); ok && v > 0 {
		// params 显式覆盖：SQL 无 LIMIT 时可注入，否则退化为扫描期硬截断
		return int(v), !limitClauseRe.MatchString(sqlStr)
	}
	if limitClauseRe.MatchString(sqlStr) {
		return 0, false
	}
	if t.queryRowLimit > 0 {
		return t.queryRowLimit, true
	}
	return defaultQueryRowLimit, true
}

// appendRowLimit 给无 LIMIT 子句的 SELECT 语句注入行数上限（调用方传入
// limit+1 以便判定截断）。仅对简单 SELECT（无注释、以 SELECT 开头）生效；
// 其余形态返回原句，由调用方退化为扫描期硬截断。
func appendRowLimit(sqlStr string, limit int) string {
	trimmed := strings.TrimSpace(sqlStr)
	trimmed = strings.TrimRight(trimmed, "; \t\r\n")
	upper := strings.ToUpper(trimmed)
	if !strings.HasPrefix(upper, "SELECT") {
		return sqlStr
	}
	if strings.Contains(trimmed, "--") || strings.Contains(trimmed, "/*") {
		// 含注释：注入可能产生语法错误，交回扫描期硬截断
		return sqlStr
	}
	return trimmed + fmt.Sprintf(" LIMIT %d", limit)
}

// NewSQLiteTool 创建新的 SQLite tool
func NewSQLiteTool(dbPath string) (*SQLiteTool, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database error: %w", err)
	}
	return &SQLiteTool{
		name:   "sqlite_processor",
		desc:   `SQLite 数据库处理tool，支持 SQL 查询、表管理和数据操作`,
		db:     db,
		dbPath: dbPath,
	}, nil
}

func (t *SQLiteTool) Name() string        { return t.name }
func (t *SQLiteTool) Description() string { return t.desc }

func (t *SQLiteTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"action": {"type": "string", "enum": ["query", "execute", "tables", "schema"]},
			"sql": {"type": "string"},
			"table_name": {"type": "string"},
			"limit": {"type": "number", "description": "最大返回行数（可选，覆盖默认上限；仅对 query 生效）"}
		},
		"required": ["action"]
	}`)
}

func (t *SQLiteTool) Execute(ctx context.Context, input json.RawMessage) (*Result, error) {
	var params map[string]any
	if err := json.Unmarshal(input, &params); err != nil {
		return nil, fmt.Errorf("parse parameters error: %w", err)
	}
	action, ok := params["action"].(string)
	if !ok {
		return nil, fmt.Errorf("parameter 'action' must be a string")
	}
	switch action {
	case "query":
		return t.executeQuery(ctx, params)
	case "execute":
		return t.executeSQL(ctx, params)
	case "tables":
		return t.listTables(ctx)
	case "schema":
		return t.getTableSchema(ctx, params)
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}

func (t *SQLiteTool) executeQuery(ctx context.Context, params map[string]any) (*Result, error) {
	sqlStr, ok := params["sql"].(string)
	if !ok {
		return NewErrorResult("parameter 'sql' must be a string"), nil
	}
	if err := validateSQLSafety(sqlStr); err != nil {
		return NewErrorResult(fmt.Sprintf("SQL safety check failed: %v", err)), nil
	}

	// 行数上限：默认给无 LIMIT 子句的 SELECT 注入 LIMIT（P2 修复）。
	// 注入时多读 1 行（LIMIT limit+1）用于判定是否发生截断，再裁回 limit。
	limit, inject := t.effectiveRowLimit(params, sqlStr)
	execSQL := sqlStr
	scanCap := 0
	if limit > 0 {
		if inject {
			execSQL = appendRowLimit(sqlStr, limit+1)
			scanCap = limit + 1
		} else {
			// params 覆盖显式 LIMIT 的场景：不注入，扫描期硬截断兜底
			scanCap = limit
		}
	}

	rows, err := t.db.QueryContext(ctx, execSQL)
	if err != nil {
		return nil, fmt.Errorf("query error: %w", err)
	}
	defer func() { _ = rows.Close() }()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("get columns error: %w", err)
	}

	results := make([]map[string]any, 0)
	truncated := false
	for rows.Next() {
		// 扫描期硬截断：注入 LIMIT 未生效（如含注释的语句）时的兜底，
		// 保证任何形态的查询都不会把无上限行数灌进上下文。
		if scanCap > 0 && len(results) >= scanCap {
			truncated = true
			break
		}
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("scan error: %w", err)
		}
		row := make(map[string]any)
		for i, col := range columns {
			row[col] = values[i]
		}
		results = append(results, row)
	}

	// 多读的判定行裁掉，如实标注截断，避免调用方误以为拿到全量结果
	if limit > 0 && len(results) > limit {
		results = results[:limit]
		truncated = true
	}

	output, _ := json.MarshalIndent(results, "", "  ")
	metadata := map[string]any{
		"columns_count": strconv.Itoa(len(columns)),
		"rows_count":    strconv.Itoa(len(results)),
		"tool":          "sqlite_processor",
	}
	if truncated {
		metadata["truncated"] = "true"
		metadata["row_limit"] = strconv.Itoa(limit)
	}
	return &Result{
		Content:  string(output),
		Metadata: metadata,
	}, nil
}

func (t *SQLiteTool) executeSQL(ctx context.Context, params map[string]any) (*Result, error) {
	sqlStr, ok := params["sql"].(string)
	if !ok {
		return NewErrorResult("parameter 'sql' must be a string"), nil
	}
	if err := validateSQLSafety(sqlStr); err != nil {
		return NewErrorResult(fmt.Sprintf("SQL safety check failed: %v", err)), nil
	}
	result, err := t.db.ExecContext(ctx, sqlStr)
	if err != nil {
		return nil, fmt.Errorf("execute error: %w", err)
	}
	lastInsertID, _ := result.LastInsertId()
	rowsAffected, _ := result.RowsAffected()
	output := map[string]any{"success": true, "last_insert_id": lastInsertID, "rows_affected": rowsAffected}
	outputJSON, _ := json.MarshalIndent(output, "", "  ")
	return &Result{Content: string(outputJSON)}, nil
}

func (t *SQLiteTool) listTables(ctx context.Context) (*Result, error) {
	queryResult, err := t.executeQuery(ctx, map[string]any{"sql": "SELECT name FROM sqlite_master WHERE type='table' ORDER BY name"})
	if err != nil {
		return nil, err
	}
	return queryResult, nil
}

func (t *SQLiteTool) getTableSchema(ctx context.Context, params map[string]any) (*Result, error) {
	tableName, ok := params["table_name"].(string)
	if !ok {
		return nil, fmt.Errorf("parameter 'table_name' must be a string")
	}
	// 验证表名防止 SQL 注入：仅允许字母、数字、下划线
	if !isValidTableName(tableName) {
		return nil, fmt.Errorf("invalid table name: %q", tableName)
	}
	queryResult, err := t.executeQuery(ctx, map[string]any{"sql": fmt.Sprintf("PRAGMA table_info(%q)", tableName)})
	if err != nil {
		return nil, err
	}
	return &Result{Content: queryResult.Content, Metadata: map[string]any{"table_name": tableName}}, nil
}

// isValidTableName 验证表名是否安全（仅允许字母、数字、下划线）
func isValidTableName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_') {
			return false
		}
	}
	return true
}

func (t *SQLiteTool) Close() error {
	if t.db != nil {
		return t.db.Close()
	}
	return nil
}

// ===== 辅助函数 =====

func containsStr(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func parseValue(s string) any {
	if num, err := strconv.ParseFloat(s, 64); err == nil {
		return num
	}
	if b, err := strconv.ParseBool(s); err == nil {
		return b
	}
	return s
}

func queryByPath(data any, pathParts []string) any {
	if len(pathParts) == 0 || data == nil {
		return data
	}
	key := pathParts[0]
	switch v := data.(type) {
	case map[string]any:
		if val, ok := v[key]; ok {
			return queryByPath(val, pathParts[1:])
		}
	case []any:
		if idx, err := strconv.Atoi(key); err == nil && idx >= 0 && idx < len(v) {
			return queryByPath(v[idx], pathParts[1:])
		}
	}
	return nil
}

func applyTransform(data any, rule string) any {
	switch rule {
	case "uppercase_keys":
		if m, ok := data.(map[string]any); ok {
			result := make(map[string]any)
			for k, v := range m {
				result[strings.ToUpper(k)] = v
			}
			return result
		}
	case "flatten":
		return flattenObject(data)
	}
	return data
}

func flattenObject(data any) map[string]any {
	result := make(map[string]any)
	flattenRecursive(data, "", result)
	return result
}

func flattenRecursive(data any, prefix string, result map[string]any) {
	switch v := data.(type) {
	case map[string]any:
		for k, val := range v {
			newKey := k
			if prefix != "" {
				newKey = prefix + "." + k
			}
			flattenRecursive(val, newKey, result)
		}
	default:
		if prefix != "" {
			result[prefix] = v
		}
	}
}

func getDataType(data any) string {
	switch data.(type) {
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	case nil:
		return "null"
	default:
		return "unknown"
	}
}
