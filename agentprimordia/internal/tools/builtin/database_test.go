package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/tools"
)

// TestDatabase_Name 测试工具名称
func TestDatabase_Name(t *testing.T) {
	db, err := NewDatabase(":memory:")
	if err != nil {
		t.Fatalf("创建数据库失败: %v", err)
	}
	defer db.Close()

	if db.Name() != "database" {
		t.Errorf("期望名称 'database', 得到 %q", db.Name())
	}
}

// TestDatabase_Description 测试工具描述
func TestDatabase_Description(t *testing.T) {
	db, err := NewDatabase(":memory:")
	if err != nil {
		t.Fatalf("创建数据库失败: %v", err)
	}
	defer db.Close()

	desc := db.Description()
	if desc == "" {
		t.Error("描述不能为空")
	}
}

// TestDatabase_Parameters 测试参数定义
func TestDatabase_Parameters(t *testing.T) {
	db, err := NewDatabase(":memory:")
	if err != nil {
		t.Fatalf("创建数据库失败: %v", err)
	}
	defer db.Close()

	params := db.Parameters()
	if params == nil {
		t.Error("参数定义不能为空")
	}

	var schema map[string]any
	if err := json.Unmarshal(params, &schema); err != nil {
		t.Errorf("参数 JSON 解析失败: %v", err)
	}
}

// TestDatabase_CreateTable 测试创建表
func TestDatabase_CreateTable(t *testing.T) {
	db, err := NewDatabase(":memory:")
	if err != nil {
		t.Fatalf("创建数据库失败: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	args := json.RawMessage(`{
		"operation": "execute",
		"query": "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, age INTEGER)"
	}`)

	result, err := db.Execute(ctx, args)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	if result.IsError {
		t.Errorf("不应返回错误: %s", result.Content)
	}
}

// TestDatabase_InsertAndQuery 测试插入和查询
func TestDatabase_InsertAndQuery(t *testing.T) {
	db, err := NewDatabase(":memory:")
	if err != nil {
		t.Fatalf("创建数据库失败: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// 创建表
	_, err = db.Execute(ctx, json.RawMessage(`{
		"operation": "execute",
		"query": "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, age INTEGER)"
	}`))
	if err != nil {
		t.Fatalf("创建表失败: %v", err)
	}

	// 插入数据
	insertArgs := json.RawMessage(`{
		"operation": "execute",
		"query": "INSERT INTO users (name, age) VALUES (?, ?)",
		"params": ["Alice", 30]
	}`)
	result, err := db.Execute(ctx, insertArgs)
	if err != nil {
		t.Fatalf("插入失败: %v", err)
	}
	if result.IsError {
		t.Errorf("插入不应返回错误: %s", result.Content)
	}

	// 验证元数据
	if result.Metadata["rows_affected"].(int64) != 1 {
		t.Errorf("期望影响 1 行, 得到 %v", result.Metadata["rows_affected"])
	}

	// 查询数据
	queryArgs := json.RawMessage(`{
		"operation": "query",
		"query": "SELECT * FROM users WHERE name = ?",
		"params": ["Alice"]
	}`)
	queryResult, err := db.Execute(ctx, queryArgs)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if queryResult.IsError {
		t.Errorf("查询不应返回错误: %s", queryResult.Content)
	}

	// 解析结果
	var rows []map[string]any
	if err := json.Unmarshal([]byte(queryResult.Content), &rows); err != nil {
		t.Fatalf("结果解析失败: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("期望 1 行结果, 得到 %d", len(rows))
	}
	if rows[0]["name"] != "Alice" {
		t.Errorf("期望 name='Alice', 得到 %v", rows[0]["name"])
	}
}

// TestDatabase_ReadOnlyMode 测试只读模式
func TestDatabase_ReadOnlyMode(t *testing.T) {
	db, err := NewDatabase(":memory:", WithReadOnly(true))
	if err != nil {
		t.Fatalf("创建数据库失败: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// 尝试写操作应该失败
	args := json.RawMessage(`{
		"operation": "execute",
		"query": "CREATE TABLE test (id INTEGER)"
	}`)
	result, err := db.Execute(ctx, args)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	if !result.IsError {
		t.Error("只读模式下写操作应该返回错误")
	}
	if result.Content == "" {
		t.Error("错误信息不能为空")
	}
}

// TestDatabase_MaxRows 测试结果行数限制
func TestDatabase_MaxRows(t *testing.T) {
	db, err := NewDatabase(":memory:", WithMaxRows(5))
	if err != nil {
		t.Fatalf("创建数据库失败: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// 创建表并插入 10 条数据
	_, err = db.Execute(ctx, json.RawMessage(`{
		"operation": "execute",
		"query": "CREATE TABLE numbers (id INTEGER)"
	}`))
	if err != nil {
		t.Fatalf("创建表失败: %v", err)
	}

	for i := 1; i <= 10; i++ {
		args := fmt.Sprintf(`{
			"operation": "execute",
			"query": "INSERT INTO numbers (id) VALUES (?)",
			"params": [%d]
		}`, i)
		_, err = db.Execute(ctx, json.RawMessage(args))
		if err != nil {
			t.Fatalf("插入失败: %v", err)
		}
	}

	// 查询应该只返回 5 行
	result, err := db.Execute(ctx, json.RawMessage(`{
		"operation": "query",
		"query": "SELECT * FROM numbers"
	}`))
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}

	if result.Metadata["truncated"] != true {
		t.Error("期望结果被截断")
	}
	if result.Metadata["max_rows"].(int) != 5 {
		t.Errorf("期望 max_rows=5, 得到 %v", result.Metadata["max_rows"])
	}
}

// TestDatabase_QueryTimeout 测试查询超时
func TestDatabase_QueryTimeout(t *testing.T) {
	db, err := NewDatabase(":memory:", WithQueryTimeout(1*time.Millisecond))
	if err != nil {
		t.Fatalf("创建数据库失败: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// 创建表
	_, err = db.Execute(ctx, json.RawMessage(`{
		"operation": "execute",
		"query": "CREATE TABLE test (id INTEGER)"
	}`))
	if err != nil {
		t.Fatalf("创建表失败: %v", err)
	}

	// 插入大量数据
	for i := 0; i < 1000; i++ {
		args := fmt.Sprintf(`{
			"operation": "execute",
			"query": "INSERT INTO test (id) VALUES (?)",
			"params": [%d]
		}`, i%10)
		_, err = db.Execute(ctx, json.RawMessage(args))
		if err != nil {
			t.Fatalf("插入失败: %v", err)
		}
	}

	// 复杂查询可能超时（不保证一定超时，但不应该崩溃）
	_, err = db.Execute(ctx, json.RawMessage(`{
		"operation": "query",
		"query": "SELECT * FROM test t1, test t2, test t3"
	}`))
	// 超时会返回错误，这是预期行为
	if err != nil {
		t.Logf("查询超时（预期行为）: %v", err)
	}
}

// TestDatabase_InvalidOperation 测试无效操作
func TestDatabase_InvalidOperation(t *testing.T) {
	db, err := NewDatabase(":memory:")
	if err != nil {
		t.Fatalf("创建数据库失败: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	args := json.RawMessage(`{
		"operation": "invalid",
		"query": "SELECT 1"
	}`)

	result, err := db.Execute(ctx, args)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	if !result.IsError {
		t.Error("无效操作应该返回错误")
	}
}

// TestDatabase_EmptyQuery 测试空查询
func TestDatabase_EmptyQuery(t *testing.T) {
	db, err := NewDatabase(":memory:")
	if err != nil {
		t.Fatalf("创建数据库失败: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	args := json.RawMessage(`{
		"operation": "query",
		"query": ""
	}`)

	result, err := db.Execute(ctx, args)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	if !result.IsError {
		t.Error("空查询应该返回错误")
	}
}

// TestDatabase_SQLInjection 测试 SQL 注入防护
func TestDatabase_SQLInjection(t *testing.T) {
	db, err := NewDatabase(":memory:")
	if err != nil {
		t.Fatalf("创建数据库失败: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// 创建表
	_, err = db.Execute(ctx, json.RawMessage(`{
		"operation": "execute",
		"query": "CREATE TABLE users (id INTEGER, name TEXT)"
	}`))
	if err != nil {
		t.Fatalf("创建表失败: %v", err)
	}

	// 尝试在 query 操作中执行 DROP（应该被阻止）
	maliciousArgs := json.RawMessage(`{
		"operation": "query",
		"query": "SELECT * FROM users; DROP TABLE users"
	}`)

	result, err := db.Execute(ctx, maliciousArgs)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	if !result.IsError {
		t.Error("包含 DROP 的查询应该返回错误")
	}
}

// TestDatabase_ParameterizedQuery 测试参数化查询
func TestDatabase_ParameterizedQuery(t *testing.T) {
	db, err := NewDatabase(":memory:")
	if err != nil {
		t.Fatalf("创建数据库失败: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// 创建表并插入数据
	_, err = db.Execute(ctx, json.RawMessage(`{
		"operation": "execute",
		"query": "CREATE TABLE products (id INTEGER PRIMARY KEY, name TEXT, price REAL)"
	}`))
	if err != nil {
		t.Fatalf("创建表失败: %v", err)
	}

	_, err = db.Execute(ctx, json.RawMessage(`{
		"operation": "execute",
		"query": "INSERT INTO products (name, price) VALUES (?, ?)",
		"params": ["Widget", 19.99]
	}`))
	if err != nil {
		t.Fatalf("插入失败: %v", err)
	}

	// 使用参数化查询
	result, err := db.Execute(ctx, json.RawMessage(`{
		"operation": "query",
		"query": "SELECT * FROM products WHERE price > ?",
		"params": [10.0]
	}`))
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if result.IsError {
		t.Errorf("查询不应返回错误: %s", result.Content)
	}

	var rows []map[string]any
	if err := json.Unmarshal([]byte(result.Content), &rows); err != nil {
		t.Fatalf("结果解析失败: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("期望 1 行结果, 得到 %d", len(rows))
	}
}

// TestDatabase_Update 测试更新操作
func TestDatabase_Update(t *testing.T) {
	db, err := NewDatabase(":memory:")
	if err != nil {
		t.Fatalf("创建数据库失败: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// 创建表并插入数据
	_, err = db.Execute(ctx, json.RawMessage(`{
		"operation": "execute",
		"query": "CREATE TABLE counters (id INTEGER, value INTEGER)"
	}`))
	if err != nil {
		t.Fatalf("创建表失败: %v", err)
	}

	_, err = db.Execute(ctx, json.RawMessage(`{
		"operation": "execute",
		"query": "INSERT INTO counters (id, value) VALUES (1, 10)"
	}`))
	if err != nil {
		t.Fatalf("插入失败: %v", err)
	}

	// 更新数据
	result, err := db.Execute(ctx, json.RawMessage(`{
		"operation": "execute",
		"query": "UPDATE counters SET value = ? WHERE id = ?",
		"params": [20, 1]
	}`))
	if err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if result.IsError {
		t.Errorf("更新不应返回错误: %s", result.Content)
	}
	if result.Metadata["rows_affected"].(int64) != 1 {
		t.Errorf("期望影响 1 行, 得到 %v", result.Metadata["rows_affected"])
	}

	// 验证更新结果
	queryResult, err := db.Execute(ctx, json.RawMessage(`{
		"operation": "query",
		"query": "SELECT value FROM counters WHERE id = 1"
	}`))
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}

	var rows []map[string]any
	if err := json.Unmarshal([]byte(queryResult.Content), &rows); err != nil {
		t.Fatalf("结果解析失败: %v", err)
	}
	if len(rows) != 1 || rows[0]["value"].(float64) != 20 {
		t.Errorf("期望 value=20, 得到 %v", rows)
	}
}

// TestDatabase_Delete 测试删除操作
func TestDatabase_Delete(t *testing.T) {
	db, err := NewDatabase(":memory:")
	if err != nil {
		t.Fatalf("创建数据库失败: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// 创建表并插入数据
	_, err = db.Execute(ctx, json.RawMessage(`{
		"operation": "execute",
		"query": "CREATE TABLE items (id INTEGER, name TEXT)"
	}`))
	if err != nil {
		t.Fatalf("创建表失败: %v", err)
	}

	_, err = db.Execute(ctx, json.RawMessage(`{
		"operation": "execute",
		"query": "INSERT INTO items (id, name) VALUES (1, 'test')"
	}`))
	if err != nil {
		t.Fatalf("插入失败: %v", err)
	}

	// 删除数据
	result, err := db.Execute(ctx, json.RawMessage(`{
		"operation": "execute",
		"query": "DELETE FROM items WHERE id = ?",
		"params": [1]
	}`))
	if err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if result.IsError {
		t.Errorf("删除不应返回错误: %s", result.Content)
	}
	if result.Metadata["rows_affected"].(int64) != 1 {
		t.Errorf("期望影响 1 行, 得到 %v", result.Metadata["rows_affected"])
	}
}

// TestDatabase_InvalidJSON 测试无效 JSON 参数
func TestDatabase_InvalidJSON(t *testing.T) {
	db, err := NewDatabase(":memory:")
	if err != nil {
		t.Fatalf("创建数据库失败: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	args := json.RawMessage(`{invalid json}`)

	result, err := db.Execute(ctx, args)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	if !result.IsError {
		t.Error("无效 JSON 应该返回错误")
	}
}

// TestDatabase_MissingOperation 测试缺少操作参数
func TestDatabase_MissingOperation(t *testing.T) {
	db, err := NewDatabase(":memory:")
	if err != nil {
		t.Fatalf("创建数据库失败: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	args := json.RawMessage(`{
		"query": "SELECT 1"
	}`)

	result, err := db.Execute(ctx, args)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	if !result.IsError {
		t.Error("缺少操作参数应该返回错误")
	}
}

// TestDatabase_PragmaQuery 测试 PRAGMA 查询
func TestDatabase_PragmaQuery(t *testing.T) {
	db, err := NewDatabase(":memory:")
	if err != nil {
		t.Fatalf("创建数据库失败: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// 创建表
	_, err = db.Execute(ctx, json.RawMessage(`{
		"operation": "execute",
		"query": "CREATE TABLE test (id INTEGER PRIMARY KEY, name TEXT)"
	}`))
	if err != nil {
		t.Fatalf("创建表失败: %v", err)
	}

	// 查询表结构
	result, err := db.Execute(ctx, json.RawMessage(`{
		"operation": "query",
		"query": "PRAGMA table_info(test)"
	}`))
	if err != nil {
		t.Fatalf("PRAGMA 查询失败: %v", err)
	}
	if result.IsError {
		t.Errorf("PRAGMA 查询不应返回错误: %s", result.Content)
	}
}

// TestDatabase_WithStatement 测试 WITH 语句
func TestDatabase_WithStatement(t *testing.T) {
	db, err := NewDatabase(":memory:")
	if err != nil {
		t.Fatalf("创建数据库失败: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// 创建表并插入数据
	_, err = db.Execute(ctx, json.RawMessage(`{
		"operation": "execute",
		"query": "CREATE TABLE numbers (id INTEGER)"
	}`))
	if err != nil {
		t.Fatalf("创建表失败: %v", err)
	}

	for i := 1; i <= 5; i++ {
		args := fmt.Sprintf(`{
			"operation": "execute",
			"query": "INSERT INTO numbers (id) VALUES (?)",
			"params": [%d]
		}`, i)
		_, err = db.Execute(ctx, json.RawMessage(args))
		if err != nil {
			t.Fatalf("插入失败: %v", err)
		}
	}

	// 使用 WITH 语句查询
	result, err := db.Execute(ctx, json.RawMessage(`{
		"operation": "query",
		"query": "WITH even_nums AS (SELECT * FROM numbers WHERE id % 2 = 0) SELECT * FROM even_nums"
	}`))
	if err != nil {
		t.Fatalf("WITH 查询失败: %v", err)
	}
	if result.IsError {
		t.Errorf("WITH 查询不应返回错误: %s", result.Content)
	}
}

// TestDatabase_NewDatabaseErrors 测试创建数据库时的错误
func TestDatabase_NewDatabaseErrors(t *testing.T) {
	// 空路径
	_, err := NewDatabase("")
	if err == nil {
		t.Error("空路径应该返回错误")
	}

	// 无效路径
	_, err = NewDatabase("/nonexistent/path/to/db.sqlite")
	if err == nil {
		t.Error("无效路径应该返回错误")
	}
}

// TestDatabase_ValidateReadOnlySQL 测试 SQL 安全验证
func TestDatabase_ValidateReadOnlySQL(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantErr bool
	}{
		{"SELECT 语句", "SELECT * FROM users", false},
		{"PRAGMA 语句", "PRAGMA table_info(users)", false},
		{"WITH 语句", "WITH cte AS (SELECT 1) SELECT * FROM cte", false},
		{"EXPLAIN 语句", "EXPLAIN SELECT * FROM users", false},
		{"INSERT 语句", "INSERT INTO users VALUES (1)", true},
		{"UPDATE 语句", "UPDATE users SET name='x'", true},
		{"DELETE 语句", "DELETE FROM users", true},
		{"DROP 语句", "DROP TABLE users", true},
		{"CREATE 语句", "CREATE TABLE test (id INT)", true},
		{"空语句", "", true},
		{"SELECT 包含 DROP", "SELECT * FROM users; DROP TABLE users", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateReadOnlySQL(tt.sql)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateReadOnlySQL(%q) error = %v, wantErr %v", tt.sql, err, tt.wantErr)
			}
		})
	}
}

// TestDatabase_InterfaceCompliance 测试接口实现
func TestDatabase_InterfaceCompliance(t *testing.T) {
	db, err := NewDatabase(":memory:")
	if err != nil {
		t.Fatalf("创建数据库失败: %v", err)
	}
	defer db.Close()

	// 验证实现了 tools.Tool 接口
	var _ tools.Tool = db
}
