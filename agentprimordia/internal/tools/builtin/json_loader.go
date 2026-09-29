package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools"
)

// JSONLoader 从 JSON/YAML 文件加载结构化数据，支持简单点号路径查询和嵌套结构扁平化
type JSONLoader struct {
	scopePolicy tools.ScopePolicy
	scopeAgent  string
}

// NewJSONLoader 创建 JSON 加载器
func NewJSONLoader() *JSONLoader {
	return &JSONLoader{}
}

// WithScopePolicy 注入权限策略
func (j *JSONLoader) WithScopePolicy(policy tools.ScopePolicy, agentID string) *JSONLoader {
	j.scopePolicy = policy
	j.scopeAgent = agentID
	return j
}

func (j *JSONLoader) Name() string { return "json_loader" }

func (j *JSONLoader) Description() string {
	return "Load and parse JSON files with support for JSONPath-like queries (dot notation: users[0].name) and nested structure flattening."
}

func (j *JSONLoader) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "action": {"type": "string", "enum": ["load", "query", "flatten"], "description": "The operation to perform: load (full content), query (dot notation path), flatten (flatten nested structures)"},
    "path": {"type": "string", "description": "Path to the JSON file"},
    "query": {"type": "string", "description": "Dot-notation query path (e.g. users[0].name), used with 'query' action"},
    "flatten_prefix": {"type": "string", "description": "Prefix for flattened keys (default: empty)", "default": ""},
    "flatten_separator": {"type": "string", "description": "Separator for flattened keys (default: '.')", "default": "."}
  },
  "required": ["action", "path"]
}`)
}

func (j *JSONLoader) Execute(ctx context.Context, args json.RawMessage) (*tools.Result, error) {
	var params struct {
		Action           string `json:"action"`
		Path             string `json:"path"`
		Query            string `json:"query"`
		FlattenPrefix    string `json:"flatten_prefix"`
		FlattenSeparator string `json:"flatten_separator"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return tools.NewErrorResult(fmt.Sprintf("invalid arguments: %v", err)), nil
	}

	if params.Action == "" {
		return tools.NewErrorResult("action is required"), nil
	}
	if params.Path == "" {
		return tools.NewErrorResult("path is required"), nil
	}

	// 权限检查
	if j.scopePolicy != nil && !j.scopePolicy.Allow(j.scopeAgent, params.Path) {
		return tools.NewErrorResult(fmt.Sprintf("access denied: %s", params.Path)), nil
	}

	data, err := os.ReadFile(params.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return tools.NewErrorResult(fmt.Sprintf("file not found: %s", params.Path)), nil
		}
		return tools.NewErrorResult(fmt.Sprintf("read error: %v", err)), nil
	}

	var parsed any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return tools.NewErrorResult(fmt.Sprintf("invalid JSON: %v", err)), nil
	}

	switch params.Action {
	case "load":
		output, err := json.MarshalIndent(parsed, "", "  ")
		if err != nil {
			return tools.NewErrorResult(fmt.Sprintf("marshal error: %v", err)), nil
		}
		return tools.NewResult(string(output)), nil

	case "query":
		if params.Query == "" {
			return tools.NewErrorResult("query is required for query action"), nil
		}
		result, err := queryJSON(parsed, params.Query)
		if err != nil {
			return tools.NewErrorResult(fmt.Sprintf("query error: %v", err)), nil
		}
		output, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return tools.NewErrorResult(fmt.Sprintf("marshal error: %v", err)), nil
		}
		return tools.NewResult(string(output)), nil

	case "flatten":
		separator := params.FlattenSeparator
		if separator == "" {
			separator = "."
		}
		prefix := params.FlattenPrefix
		flattened := flattenJSON(parsed, prefix, separator)
		output, err := json.MarshalIndent(flattened, "", "  ")
		if err != nil {
			return tools.NewErrorResult(fmt.Sprintf("marshal error: %v", err)), nil
		}
		return tools.NewResult(string(output)), nil

	default:
		return tools.NewErrorResult(fmt.Sprintf("unknown action: %s", params.Action)), nil
	}
}

// arrayIndexRe 匹配数组索引访问，如 [0], [1]
var arrayIndexRe = regexp.MustCompile(`^\[(\d+)\]$`)

// queryJSON 使用简单点号路径查询 JSON 数据
// 支持路径如: "users[0].name", "data.items", "list[2]"
func queryJSON(data any, path string) (any, error) {
	parts := splitJSONPath(path)
	current := data

	for _, part := range parts {
		if current == nil {
			return nil, fmt.Errorf("path '%s': encountered nil at '%s'", path, part)
		}

		// 检查是否为数组索引访问
		if matches := arrayIndexRe.FindStringSubmatch(part); len(matches) == 2 {
			idx, err := strconv.Atoi(matches[1])
			if err != nil {
				return nil, fmt.Errorf("invalid array index: %s", part)
			}
			arr, ok := current.([]any)
			if !ok {
				return nil, fmt.Errorf("path '%s': expected array at '%s'", path, part)
			}
			if idx < 0 || idx >= len(arr) {
				return nil, fmt.Errorf("path '%s': index %d out of range (length %d)", path, idx, len(arr))
			}
			current = arr[idx]
			continue
		}

		// 对象字段访问
		obj, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("path '%s': expected object at '%s'", path, part)
		}
		val, exists := obj[part]
		if !exists {
			return nil, fmt.Errorf("path '%s': key '%s' not found", path, part)
		}
		current = val
	}

	return current, nil
}

// splitJSONPath 将点号路径拆分为各段，处理数组索引
// 例如 "users[0].name" -> ["users", "[0]", "name"]
func splitJSONPath(path string) []string {
	var parts []string
	var current strings.Builder

	for i := 0; i < len(path); i++ {
		ch := path[i]
		if ch == '.' {
			if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
		} else if ch == '[' {
			// 先保存当前段
			if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
			// 读取到 ]
			j := i + 1
			for j < len(path) && path[j] != ']' {
				j++
			}
			if j < len(path) {
				parts = append(parts, path[i:j+1]) // 包含 [ ]
				i = j
			}
		} else {
			current.WriteByte(ch)
		}
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts
}

// flattenJSON 将嵌套的 JSON 结构扁平化为单层键值对
func flattenJSON(data any, prefix, separator string) map[string]any {
	result := make(map[string]any)
	flattenRecursive(data, prefix, separator, result)
	return result
}

// flattenRecursive 递归扁平化 JSON 数据
func flattenRecursive(data any, prefix, separator string, result map[string]any) {
	switch v := data.(type) {
	case map[string]any:
		for key, val := range v {
			newKey := key
			if prefix != "" {
				newKey = prefix + separator + key
			}
			flattenRecursive(val, newKey, separator, result)
		}
	case []any:
		for i, val := range v {
			newKey := fmt.Sprintf("%s%s%d", prefix, separator, i)
			flattenRecursive(val, newKey, separator, result)
		}
	default:
		result[prefix] = v
	}
}
