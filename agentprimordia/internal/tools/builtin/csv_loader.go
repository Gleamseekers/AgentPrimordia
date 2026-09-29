package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/tools"
)

// CSVData 表示加载后的 CSV 数据
type CSVData struct {
	Headers []string            `json:"headers"`
	Rows    []map[string]string `json:"rows"`
	Total   int                 `json:"total"`
}

// CSVLoader 从 CSV 文件加载结构化数据
type CSVLoader struct {
	scopePolicy tools.ScopePolicy
	scopeAgent  string
}

// NewCSVLoader 创建 CSV 加载器
func NewCSVLoader() *CSVLoader {
	return &CSVLoader{}
}

// WithScopePolicy 注入权限策略
func (c *CSVLoader) WithScopePolicy(policy tools.ScopePolicy, agentID string) *CSVLoader {
	c.scopePolicy = policy
	c.scopeAgent = agentID
	return c
}

func (c *CSVLoader) Name() string { return "csv_loader" }

func (c *CSVLoader) Description() string {
	return "Load and parse CSV files into structured data with header detection, configurable delimiter, and column filtering."
}

func (c *CSVLoader) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "action": {"type": "string", "enum": ["load"], "description": "The operation to perform"},
    "path": {"type": "string", "description": "Path to the CSV file"},
    "delimiter": {"type": "string", "description": "Field delimiter (default: comma)", "default": ","},
    "has_header": {"type": "boolean", "description": "Whether the first row is a header (default: true)", "default": true},
    "columns": {"type": "array", "items": {"type": "string"}, "description": "Optional list of column names to include (filters output)"}
  },
  "required": ["action", "path"]
}`)
}

func (c *CSVLoader) Execute(ctx context.Context, args json.RawMessage) (*tools.Result, error) {
	var params struct {
		Action    string   `json:"action"`
		Path      string   `json:"path"`
		Delimiter string   `json:"delimiter"`
		HasHeader *bool    `json:"has_header"`
		Columns   []string `json:"columns"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return tools.NewErrorResult(fmt.Sprintf("invalid arguments: %v", err)), nil
	}

	if params.Action == "" {
		return tools.NewErrorResult("action is required"), nil
	}

	switch params.Action {
	case "load":
		if params.Path == "" {
			return tools.NewErrorResult("path is required"), nil
		}
		delimiter := params.Delimiter
		if delimiter == "" {
			delimiter = ","
		}
		hasHeader := true
		if params.HasHeader != nil {
			hasHeader = *params.HasHeader
		}
		return c.loadFile(params.Path, delimiter, hasHeader, params.Columns)
	default:
		return tools.NewErrorResult(fmt.Sprintf("unknown action: %s", params.Action)), nil
	}
}

// loadFile 读取并解析 CSV 文件
func (c *CSVLoader) loadFile(path, delimiter string, hasHeader bool, columns []string) (*tools.Result, error) {
	// 权限检查
	if c.scopePolicy != nil && !c.scopePolicy.Allow(c.scopeAgent, path) {
		return tools.NewErrorResult(fmt.Sprintf("access denied: %s", path)), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return tools.NewErrorResult(fmt.Sprintf("file not found: %s", path)), nil
		}
		return tools.NewErrorResult(fmt.Sprintf("read error: %v", err)), nil
	}

	content := strings.TrimSpace(string(data))
	if content == "" {
		return tools.NewErrorResult("file is empty"), nil
	}

	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return tools.NewErrorResult("file is empty"), nil
	}

	// 解析行
	var allRows [][]string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := parseCSVLine(line, delimiter)
		allRows = append(allRows, fields)
	}

	if len(allRows) == 0 {
		return tools.NewErrorResult("no data rows found"), nil
	}

	var headers []string
	var dataRows [][]string

	if hasHeader && len(allRows) > 0 {
		headers = allRows[0]
		dataRows = allRows[1:]
	} else {
		// 无标题行时生成列名 col_0, col_1, ...
		numCols := len(allRows[0])
		for i := 0; i < numCols; i++ {
			headers = append(headers, fmt.Sprintf("col_%d", i))
		}
		dataRows = allRows
	}

	// 列过滤
	columnSet := make(map[string]bool)
	if len(columns) > 0 {
		for _, col := range columns {
			columnSet[col] = true
		}
	}

	// 构建行数据
	var rows []map[string]string
	for _, rowFields := range dataRows {
		row := make(map[string]string)
		for i, header := range headers {
			if len(columnSet) > 0 && !columnSet[header] {
				continue
			}
			val := ""
			if i < len(rowFields) {
				val = rowFields[i]
			}
			row[header] = val
		}
		rows = append(rows, row)
	}

	// 如果有列过滤，也过滤 headers
	var filteredHeaders []string
	if len(columnSet) > 0 {
		for _, h := range headers {
			if columnSet[h] {
				filteredHeaders = append(filteredHeaders, h)
			}
		}
	} else {
		filteredHeaders = headers
	}

	result := CSVData{
		Headers: filteredHeaders,
		Rows:    rows,
		Total:   len(rows),
	}

	output, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("marshal error: %v", err)), nil
	}

	return tools.NewResult(string(output)), nil
}

// parseCSVLine 解析单行 CSV，支持引号包裹的字段
func parseCSVLine(line, delimiter string) []string {
	var fields []string
	var current strings.Builder
	inQuotes := false

	for i := 0; i < len(line); i++ {
		ch := line[i]
		if inQuotes {
			if ch == '"' {
				// 检查转义引号 ""
				if i+1 < len(line) && line[i+1] == '"' {
					current.WriteByte('"')
					i++ // 跳过下一个引号
				} else {
					inQuotes = false
				}
			} else {
				current.WriteByte(ch)
			}
		} else {
			if ch == '"' {
				inQuotes = true
			} else if strings.HasPrefix(line[i:], delimiter) {
				fields = append(fields, current.String())
				current.Reset()
				i += len(delimiter) - 1 // 跳过分隔符
			} else {
				current.WriteByte(ch)
			}
		}
	}
	fields = append(fields, current.String())

	return fields
}
