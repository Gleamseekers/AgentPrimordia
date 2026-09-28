package tools

// data_tools_security_test.go — CSVTool rootDir 禁锢 / 敏感文件保护 /
// SQLiteTool 查询行数上限（P2 安全修复，TDD：先红后绿）。
//
// 背景：CSVTool 曾用 os.Open/os.Create 对任意路径读写，无 rootDir/scope/
// 敏感文件保护，是绕过 filesystem 工具全部防护的一条路径；
// SQLiteTool.executeQuery 无行数上限，单条查询可拖垮内存。
// 本文件固化修复后的安全不变式。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// executeCSV 执行 CSVTool 并返回结果内容与错误（辅助）
func executeCSV(t *testing.T, tool *CSVTool, params map[string]any) (string, error) {
	t.Helper()
	inputBytes, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal input error: %v", err)
	}
	result, err := tool.Execute(context.Background(), inputBytes)
	if err != nil {
		return "", err
	}
	if result == nil {
		return "", nil
	}
	return result.Content, nil
}

// executeSQLite 执行 SQLiteTool 并返回结果与错误（辅助）
func executeSQLite(t *testing.T, tool *SQLiteTool, params map[string]any) (*Result, error) {
	t.Helper()
	inputBytes, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal input error: %v", err)
	}
	return tool.Execute(context.Background(), inputBytes)
}

// ===== CSVTool rootDir 禁锢 =====

// TestCSVTool_RootDirJail_ReadOutsideRootDenied 验证 rootDir 外的读取被拒。
func TestCSVTool_RootDirJail_ReadOutsideRootDenied(t *testing.T) {
	rootDir := t.TempDir()
	outsideDir := t.TempDir()

	outsidePath := filepath.Join(outsideDir, "secret.csv")
	if err := os.WriteFile(outsidePath, []byte("a,b\n1,2\n"), 0644); err != nil {
		t.Fatalf("write outside file error: %v", err)
	}

	tool, err := NewCSVToolWithRoot(rootDir)
	if err != nil {
		t.Fatalf("NewCSVToolWithRoot error: %v", err)
	}

	// 绝对路径越界
	_, err = executeCSV(t, tool, map[string]any{"action": "read", "file_path": outsidePath})
	if err == nil {
		t.Error("expected read outside rootDir to be denied, got nil error")
	} else if !strings.Contains(err.Error(), "access denied") {
		t.Errorf("expected 'access denied' error, got: %v", err)
	}
}

// TestCSVTool_RootDirJail_WriteOutsideRootDenied 验证 rootDir 外的写入被拒。
func TestCSVTool_RootDirJail_WriteOutsideRootDenied(t *testing.T) {
	rootDir := t.TempDir()
	outsideDir := t.TempDir()

	tool, err := NewCSVToolWithRoot(rootDir)
	if err != nil {
		t.Fatalf("NewCSVToolWithRoot error: %v", err)
	}

	outsidePath := filepath.Join(outsideDir, "evil.csv")
	_, err = executeCSV(t, tool, map[string]any{
		"action":    "write",
		"file_path": outsidePath,
		"data":      []map[string]any{{"x": 1}},
	})
	if err == nil {
		t.Error("expected write outside rootDir to be denied, got nil error")
	} else if !strings.Contains(err.Error(), "access denied") {
		t.Errorf("expected 'access denied' error, got: %v", err)
	}
	if _, statErr := os.Stat(outsidePath); statErr == nil {
		t.Error("outside file must not be created")
	}
}

// TestCSVTool_RootDirJail_PathTraversalRejected 验证 ".." 穿越被拒。
func TestCSVTool_RootDirJail_PathTraversalRejected(t *testing.T) {
	rootDir := t.TempDir()
	outsideDir := t.TempDir()

	// 在 rootDir 旁造一个可被 "../" 命中的文件
	outsidePath := filepath.Join(outsideDir, "secret.csv")
	if err := os.WriteFile(outsidePath, []byte("a,b\n1,2\n"), 0644); err != nil {
		t.Fatalf("write outside file error: %v", err)
	}

	tool, err := NewCSVToolWithRoot(rootDir)
	if err != nil {
		t.Fatalf("NewCSVToolWithRoot error: %v", err)
	}

	// 相对路径穿越：rootDir/../<outsideDir base>/secret.csv
	traversal := filepath.Join("..", filepath.Base(outsideDir), "secret.csv")
	_, err = executeCSV(t, tool, map[string]any{"action": "read", "file_path": traversal})
	if err == nil {
		t.Error("expected '..' traversal to be denied, got nil error")
	} else if !strings.Contains(err.Error(), "access denied") {
		t.Errorf("expected 'access denied' error, got: %v", err)
	}

	// 写入穿越同样被拒
	_, err = executeCSV(t, tool, map[string]any{
		"action":    "write",
		"file_path": filepath.Join("..", filepath.Base(outsideDir), "evil.csv"),
		"data":      []map[string]any{{"x": 1}},
	})
	if err == nil {
		t.Error("expected '..' traversal write to be denied, got nil error")
	}
}

// TestCSVTool_RootDirJail_SymlinkEscapeRejected 验证指向 rootDir 外的
// 符号链接被拒（仿 filesystem.go 的 symlink 逃逸检查）。
func TestCSVTool_RootDirJail_SymlinkEscapeRejected(t *testing.T) {
	rootDir := t.TempDir()
	outsideDir := t.TempDir()

	outsidePath := filepath.Join(outsideDir, "secret.csv")
	if err := os.WriteFile(outsidePath, []byte("a,b\n1,2\n"), 0644); err != nil {
		t.Fatalf("write outside file error: %v", err)
	}
	linkPath := filepath.Join(rootDir, "link.csv")
	if err := os.Symlink(outsidePath, linkPath); err != nil {
		t.Skipf("symlink 不可用: %v", err)
	}

	tool, err := NewCSVToolWithRoot(rootDir)
	if err != nil {
		t.Fatalf("NewCSVToolWithRoot error: %v", err)
	}

	_, err = executeCSV(t, tool, map[string]any{"action": "read", "file_path": linkPath})
	if err == nil {
		t.Error("expected symlink escape to be denied, got nil error")
	} else if !strings.Contains(err.Error(), "access denied") {
		t.Errorf("expected 'access denied' error, got: %v", err)
	}
}

// TestCSVTool_RootDirJail_NormalReadWritePass 验证禁锢内的正常读写通过。
func TestCSVTool_RootDirJail_NormalReadWritePass(t *testing.T) {
	rootDir := t.TempDir()

	csvPath := filepath.Join(rootDir, "data.csv")
	if err := os.WriteFile(csvPath, []byte("name,age\nAlice,30\nBob,25\n"), 0644); err != nil {
		t.Fatalf("write csv error: %v", err)
	}

	tool, err := NewCSVToolWithRoot(rootDir)
	if err != nil {
		t.Fatalf("NewCSVToolWithRoot error: %v", err)
	}

	// 绝对路径（禁锢内）
	content, err := executeCSV(t, tool, map[string]any{"action": "read", "file_path": csvPath})
	if err != nil {
		t.Fatalf("read within rootDir error: %v", err)
	}
	if !strings.Contains(content, "Alice") {
		t.Errorf("expected 'Alice' in content, got: %s", content)
	}

	// 相对路径（相对 rootDir 解析）
	content, err = executeCSV(t, tool, map[string]any{"action": "read", "file_path": "data.csv"})
	if err != nil {
		t.Fatalf("read relative path error: %v", err)
	}
	if !strings.Contains(content, "Bob") {
		t.Errorf("expected 'Bob' in content, got: %s", content)
	}

	// 写入（相对路径）
	content, err = executeCSV(t, tool, map[string]any{
		"action":    "write",
		"file_path": "out.csv",
		"data":      []map[string]any{{"name": "X", "value": 100}},
	})
	if err != nil {
		t.Fatalf("write within rootDir error: %v", err)
	}
	if !strings.Contains(content, "Successfully wrote") {
		t.Errorf("expected success message, got: %s", content)
	}
	written, readErr := os.ReadFile(filepath.Join(rootDir, "out.csv"))
	if readErr != nil {
		t.Fatalf("read written file error: %v", readErr)
	}
	if !strings.Contains(string(written), "X") {
		t.Errorf("written file content mismatch: %s", written)
	}
}

// TestCSVTool_WithRootDir_Option 验证链式 WithRootDir 选项生效。
func TestCSVTool_WithRootDir_Option(t *testing.T) {
	rootDir := t.TempDir()
	outsideDir := t.TempDir()

	outsidePath := filepath.Join(outsideDir, "secret.csv")
	if err := os.WriteFile(outsidePath, []byte("a,b\n1,2\n"), 0644); err != nil {
		t.Fatalf("write outside file error: %v", err)
	}

	tool := NewCSVTool().WithRootDir(rootDir)

	// 禁锢后越界被拒
	if _, err := executeCSV(t, tool, map[string]any{"action": "read", "file_path": outsidePath}); err == nil {
		t.Error("expected read outside rootDir to be denied after WithRootDir")
	}

	// 禁锢内通过
	insidePath := filepath.Join(rootDir, "ok.csv")
	if err := os.WriteFile(insidePath, []byte("k,v\n1,2\n"), 0644); err != nil {
		t.Fatalf("write inside file error: %v", err)
	}
	if _, err := executeCSV(t, tool, map[string]any{"action": "read", "file_path": insidePath}); err != nil {
		t.Errorf("read within rootDir should pass: %v", err)
	}
}

// TestNewCSVToolWithRoot_InvalidRoot 验证非法 rootDir 报错。
func TestNewCSVToolWithRoot_InvalidRoot(t *testing.T) {
	if _, err := NewCSVToolWithRoot(filepath.Join(t.TempDir(), "nonexistent")); err == nil {
		t.Error("expected error for nonexistent rootDir")
	}
	filePath := filepath.Join(t.TempDir(), "a.csv")
	if err := os.WriteFile(filePath, []byte("a\n1\n"), 0644); err != nil {
		t.Fatalf("write file error: %v", err)
	}
	if _, err := NewCSVToolWithRoot(filePath); err == nil {
		t.Error("expected error when rootDir is a file")
	}
}

// ===== CSVTool 敏感文件保护 =====

// TestCSVTool_SensitiveFileRejected 验证敏感文件（大小写不敏感）被拒，
// 无论是否配置 rootDir。
func TestCSVTool_SensitiveFileRejected(t *testing.T) {
	sensitiveNames := []string{".env", "prod.ENV", "id_rsa", "server.pem", "credentials.json", ".npmrc"}

	for _, name := range sensitiveNames {
		t.Run(name, func(t *testing.T) {
			rootDir := t.TempDir()
			path := filepath.Join(rootDir, name)
			if err := os.WriteFile(path, []byte("secret\n"), 0644); err != nil {
				t.Fatalf("write sensitive file error: %v", err)
			}

			// 带 rootDir：禁锢内但仍是敏感文件 → 拒
			jailed, err := NewCSVToolWithRoot(rootDir)
			if err != nil {
				t.Fatalf("NewCSVToolWithRoot error: %v", err)
			}
			if _, err := executeCSV(t, jailed, map[string]any{"action": "read", "file_path": path}); err == nil {
				t.Errorf("expected sensitive file %q to be denied (jailed)", name)
			} else if !strings.Contains(err.Error(), "sensitive") {
				t.Errorf("expected 'sensitive' in error, got: %v", err)
			}

			// 不带 rootDir（legacy）：敏感文件保护依然生效
			legacy := NewCSVTool()
			if _, err := executeCSV(t, legacy, map[string]any{"action": "read", "file_path": path}); err == nil {
				t.Errorf("expected sensitive file %q to be denied (legacy)", name)
			}
		})
	}
}

// TestCSVTool_SensitiveFileWriteRejected 验证敏感文件写入同样被拒。
func TestCSVTool_SensitiveFileWriteRejected(t *testing.T) {
	rootDir := t.TempDir()
	tool, err := NewCSVToolWithRoot(rootDir)
	if err != nil {
		t.Fatalf("NewCSVToolWithRoot error: %v", err)
	}
	_, err = executeCSV(t, tool, map[string]any{
		"action":    "write",
		"file_path": ".env",
		"data":      []map[string]any{{"KEY": "VALUE"}},
	})
	if err == nil {
		t.Error("expected write to sensitive file to be denied")
	}
	if _, statErr := os.Stat(filepath.Join(rootDir, ".env")); statErr == nil {
		t.Error("sensitive file must not be created")
	}
}

// ===== SQLiteTool 查询行数上限 =====

// newLimitedSQLiteTool 创建带小行数上限的 SQLiteTool 并建表插 count 行。
func newLimitedSQLiteTool(t *testing.T, limit, count int) *SQLiteTool {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "limit.db")
	tool, err := NewSQLiteTool(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteTool error: %v", err)
	}
	t.Cleanup(func() { _ = tool.Close() })
	if _, err := tool.db.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`); err != nil {
		t.Fatalf("create table error: %v", err)
	}
	for i := 0; i < count; i++ {
		if _, err := tool.db.Exec(`INSERT INTO t (v) VALUES (?)`, "x"); err != nil {
			t.Fatalf("insert error: %v", err)
		}
	}
	return tool.WithQueryRowLimit(limit)
}

// TestSQLiteTool_ExecuteQuery_DefaultRowLimit 验证默认行数上限生效：
// 无 LIMIT 子句的查询被截断到默认值，并在 metadata 标注 truncated。
func TestSQLiteTool_ExecuteQuery_DefaultRowLimit(t *testing.T) {
	if defaultQueryRowLimit <= 0 {
		t.Fatalf("defaultQueryRowLimit must be positive, got %d", defaultQueryRowLimit)
	}
	tool := newLimitedSQLiteTool(t, 0, defaultQueryRowLimit+5)

	result, err := executeSQLite(t, tool, map[string]any{"action": "query", "sql": "SELECT * FROM t"})
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(result.Content), &rows); err != nil {
		t.Fatalf("unmarshal rows error: %v", err)
	}
	if len(rows) != defaultQueryRowLimit {
		t.Errorf("expected %d rows (default limit), got %d", defaultQueryRowLimit, len(rows))
	}
	if result.Metadata["truncated"] != "true" {
		t.Errorf("expected truncated=true metadata, got %v", result.Metadata["truncated"])
	}
	if result.Metadata["row_limit"] != strconv.Itoa(defaultQueryRowLimit) {
		t.Errorf("expected row_limit=%d metadata, got %v", defaultQueryRowLimit, result.Metadata["row_limit"])
	}
}

// TestSQLiteTool_ExecuteQuery_ConfiguredRowLimit 验证可配置行数上限。
func TestSQLiteTool_ExecuteQuery_ConfiguredRowLimit(t *testing.T) {
	tool := newLimitedSQLiteTool(t, 2, 10)

	result, err := executeSQLite(t, tool, map[string]any{"action": "query", "sql": "SELECT * FROM t"})
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(result.Content), &rows); err != nil {
		t.Fatalf("unmarshal rows error: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("expected 2 rows (configured limit), got %d", len(rows))
	}
	if result.Metadata["truncated"] != "true" {
		t.Error("expected truncated=true metadata")
	}
}

// TestSQLiteTool_ExecuteQuery_LimitParamOverride 验证 limit 参数覆盖默认上限。
func TestSQLiteTool_ExecuteQuery_LimitParamOverride(t *testing.T) {
	tool := newLimitedSQLiteTool(t, 1, 10)

	result, err := executeSQLite(t, tool, map[string]any{"action": "query", "sql": "SELECT * FROM t", "limit": float64(3)})
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(result.Content), &rows); err != nil {
		t.Fatalf("unmarshal rows error: %v", err)
	}
	if len(rows) != 3 {
		t.Errorf("expected 3 rows (limit param), got %d", len(rows))
	}
}

// TestSQLiteTool_ExecuteQuery_ExplicitLimitRespected 验证 SQL 中显式 LIMIT
// 被尊重（不重复注入、不强制截断）。
func TestSQLiteTool_ExecuteQuery_ExplicitLimitRespected(t *testing.T) {
	tool := newLimitedSQLiteTool(t, 2, 10)

	result, err := executeSQLite(t, tool, map[string]any{"action": "query", "sql": "SELECT * FROM t LIMIT 5"})
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(result.Content), &rows); err != nil {
		t.Fatalf("unmarshal rows error: %v", err)
	}
	if len(rows) != 5 {
		t.Errorf("expected 5 rows (explicit LIMIT), got %d", len(rows))
	}
}

// TestSQLiteTool_ExecuteQuery_NoTruncationWhenUnderLimit 验证未超限时
// 不标注 truncated。
func TestSQLiteTool_ExecuteQuery_NoTruncationWhenUnderLimit(t *testing.T) {
	tool := newLimitedSQLiteTool(t, 100, 3)

	result, err := executeSQLite(t, tool, map[string]any{"action": "query", "sql": "SELECT * FROM t"})
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(result.Content), &rows); err != nil {
		t.Fatalf("unmarshal rows error: %v", err)
	}
	if len(rows) != 3 {
		t.Errorf("expected 3 rows, got %d", len(rows))
	}
	if _, ok := result.Metadata["truncated"]; ok {
		t.Error("truncated metadata must be absent when under limit")
	}
}

// TestSQLiteTool_TablesAndSchema_StillWork 验证行数上限不影响
// tables / schema（PRAGMA）路径。
func TestSQLiteTool_TablesAndSchema_StillWork(t *testing.T) {
	tool := newLimitedSQLiteTool(t, 1, 2)

	tablesResult, err := executeSQLite(t, tool, map[string]any{"action": "tables"})
	if err != nil {
		t.Fatalf("tables error: %v", err)
	}
	if !strings.Contains(tablesResult.Content, "t") {
		t.Errorf("expected table 't' in tables list, got: %s", tablesResult.Content)
	}

	schemaResult, err := executeSQLite(t, tool, map[string]any{"action": "schema", "table_name": "t"})
	if err != nil {
		t.Fatalf("schema error: %v", err)
	}
	if !strings.Contains(schemaResult.Content, "id") {
		t.Errorf("expected column 'id' in schema, got: %s", schemaResult.Content)
	}
}
