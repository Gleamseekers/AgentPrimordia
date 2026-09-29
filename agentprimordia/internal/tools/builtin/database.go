package builtin

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/tools"

	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动
)

const (
	defaultMaxRows      = 1000
	defaultQueryTimeout = 30 * time.Second
	maxResultContentLen = 1 << 20 // 1MB 结果内容上限
)

// writeSQLKeywords 写操作相关的 SQL 关键字
var writeSQLKeywords = []string{
	"INSERT", "UPDATE", "DELETE", "REPLACE", "DROP", "ALTER", "CREATE", "TRUNCATE", "ATTACH", "DETACH",
}

// Database 是 SQLite 数据库tool，支持 query 和 execute 操作
type Database struct {
	db           *sql.DB
	dbPath       string
	readOnly     bool
	maxRows      int
	queryTimeout time.Duration
}

// DatabaseOption 是 Database 的可选配置
type DatabaseOption func(*Database)

// WithReadOnly 设置只读模式
func WithReadOnly(readOnly bool) DatabaseOption {
	return func(d *Database) { d.readOnly = readOnly }
}

// WithMaxRows 设置查询结果最大行数
func WithMaxRows(n int) DatabaseOption {
	return func(d *Database) {
		if n > 0 {
			d.maxRows = n
		}
	}
}

// WithQueryTimeout 设置查询超时时间
func WithQueryTimeout(dur time.Duration) DatabaseOption {
	return func(d *Database) {
		if dur > 0 {
			d.queryTimeout = dur
		}
	}
}

// NewDatabase 创建 SQLite 数据库tool
// dbPath 为数据库文件路径，传 ":memory:" 使用内存数据库
func NewDatabase(dbPath string, opts ...DatabaseOption) (*Database, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("database path is required")
	}

	d := &Database{
		dbPath:       dbPath,
		maxRows:      defaultMaxRows,
		queryTimeout: defaultQueryTimeout,
	}
	for _, opt := range opts {
		opt(d)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database error: %w", err)
	}
	// 验证连接可用
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("database ping error: %w", err)
	}
	d.db = db
	return d, nil
}

func (d *Database) Name() string { return "database" }

func (d *Database) Description() string {
	return `SQLite database tool for executing SQL queries and modifications.
Supports SELECT queries (query) and INSERT/UPDATE/DELETE operations (execute).
Features: parameterized queries, read-only mode, query timeout, result size limits.`
}

func (d *Database) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "operation": {
      "type": "string",
      "enum": ["query", "execute"],
      "description": "Operation type: 'query' for SELECT statements, 'execute' for INSERT/UPDATE/DELETE"
    },
    "query": {
      "type": "string",
      "description": "SQL statement to execute"
    },
    "params": {
      "type": "array",
      "items": {},
      "description": "Optional parameters for the SQL statement (for prepared statements with ? placeholders)"
    }
  },
  "required": ["operation", "query"]
}`)
}

func (d *Database) Execute(ctx context.Context, args json.RawMessage) (*tools.Result, error) {
	var params struct {
		Operation string `json:"operation"`
		Query     string `json:"query"`
		Params    []any  `json:"params"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return tools.NewErrorResult(fmt.Sprintf("invalid arguments: %v", err)), nil
	}

	if params.Query == "" {
		return tools.NewErrorResult("query is required"), nil
	}

	switch params.Operation {
	case "query":
		return d.doQuery(ctx, params.Query, params.Params)
	case "execute":
		return d.doExecute(ctx, params.Query, params.Params)
	default:
		return tools.NewErrorResult(fmt.Sprintf("unknown operation: %q, must be 'query' or 'execute'", params.Operation)), nil
	}
}

// doQuery 执行 SELECT 查询
func (d *Database) doQuery(ctx context.Context, queryStr string, params []any) (*tools.Result, error) {
	// 安全检查：query 操作只允许 SELECT 和 PRAGMA
	if err := validateReadOnlySQL(queryStr); err != nil {
		return tools.NewErrorResult(fmt.Sprintf("SQL safety check failed: %v", err)), nil
	}

	ctx, cancel := context.WithTimeout(ctx, d.queryTimeout)
	defer cancel()

	rows, err := d.db.QueryContext(ctx, queryStr, toInterfaces(params)...)
	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("query execution error: %v", err)), nil
	}
	defer func() { _ = rows.Close() }()

	columns, err := rows.Columns()
	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("get columns error: %v", err)), nil
	}

	results := make([]map[string]any, 0)
	rowCount := 0
	truncated := false

	for rows.Next() {
		if rowCount >= d.maxRows {
			truncated = true
			break
		}
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return tools.NewErrorResult(fmt.Sprintf("scan error: %v", err)), nil
		}
		row := make(map[string]any, len(columns))
		for i, col := range columns {
			row[col] = normalizeValue(values[i])
		}
		results = append(results, row)
		rowCount++
	}
	if err := rows.Err(); err != nil {
		return tools.NewErrorResult(fmt.Sprintf("rows iteration error: %v", err)), nil
	}

	output, _ := json.MarshalIndent(results, "", "  ")
	content := string(output)
	if len(content) > maxResultContentLen {
		content = content[:maxResultContentLen] + "\n... (truncated)"
	}

	metadata := map[string]any{
		"columns":       columns,
		"rows_count":    len(results),
		"columns_count": len(columns),
	}
	if truncated {
		metadata["truncated"] = true
		metadata["max_rows"] = d.maxRows
	}

	return &tools.Result{
		Content:  content,
		Metadata: metadata,
	}, nil
}

// doExecute 执行写操作（INSERT/UPDATE/DELETE）
func (d *Database) doExecute(ctx context.Context, queryStr string, params []any) (*tools.Result, error) {
	if d.readOnly {
		return tools.NewErrorResult("database is in read-only mode: write operations are not allowed"), nil
	}

	ctx, cancel := context.WithTimeout(ctx, d.queryTimeout)
	defer cancel()

	result, err := d.db.ExecContext(ctx, queryStr, toInterfaces(params)...)
	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("execute error: %v", err)), nil
	}

	lastID, _ := result.LastInsertId()
	affected, _ := result.RowsAffected()

	output := map[string]any{
		"success":        true,
		"last_insert_id": lastID,
		"rows_affected":  affected,
	}
	outputJSON, _ := json.MarshalIndent(output, "", "  ")

	return &tools.Result{
		Content: string(outputJSON),
		Metadata: map[string]any{
			"last_insert_id": lastID,
			"rows_affected":  affected,
		},
	}, nil
}

// Close 关闭数据库连接
func (d *Database) Close() error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

// validateReadOnlySQL 验证 SQL 语句是否安全（仅允许 SELECT / PRAGMA / WITH 等读操作）
func validateReadOnlySQL(queryStr string) error {
	upper := strings.ToUpper(strings.TrimSpace(queryStr))
	if upper == "" {
		return fmt.Errorf("empty SQL statement")
	}

	// 仅允许 SELECT、PRAGMA、EXPLAIN、WITH 开头的语句
	allowedPrefixes := []string{"SELECT", "PRAGMA", "EXPLAIN", "WITH"}
	allowed := false
	for _, prefix := range allowedPrefixes {
		if strings.HasPrefix(upper, prefix) {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("only SELECT, PRAGMA, EXPLAIN, WITH statements are allowed for query operation")
	}

	// 检查是否包含写操作关键字（防止子查询中嵌入写操作）
	for _, kw := range writeSQLKeywords {
		// 使用单词边界检查，避免误匹配列名
		if containsSQLKeyword(upper, kw) {
			// SELECT 语句中可能包含 CREATE TEMPORARY 等，但为安全起见一律禁止
			return fmt.Errorf("SQL contains forbidden keyword: %s", kw)
		}
	}
	return nil
}

// containsSQLKeyword 检查 SQL 中是否包含指定关键字（简单单词边界检测）
func containsSQLKeyword(upper, keyword string) bool {
	idx := 0
	for {
		pos := strings.Index(upper[idx:], keyword)
		if pos < 0 {
			return false
		}
		absPos := idx + pos
		// 检查前后是否为单词边界
		before := absPos == 0 || !isIdentChar(rune(upper[absPos-1]))
		after := absPos+len(keyword) >= len(upper) || !isIdentChar(rune(upper[absPos+len(keyword)]))
		if before && after {
			return true
		}
		idx = absPos + 1
	}
}

// isIdentChar 判断是否为 SQL 标识符字符
func isIdentChar(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
}

// toInterfaces 将 []any 转为 []any（确保类型兼容）
func toInterfaces(params []any) []any {
	if params == nil {
		return nil
	}
	result := make([]any, len(params))
	for i, p := range params {
		result[i] = p
	}
	return result
}

// normalizeValue 将数据库返回值转为 JSON 友好的类型
func normalizeValue(v any) any {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case []byte:
		return string(val)
	case int64:
		return val
	case float64:
		return val
	case string:
		return val
	case bool:
		return val
	case time.Time:
		return val.Format(time.RFC3339)
	default:
		return fmt.Sprintf("%v", val)
	}
}
