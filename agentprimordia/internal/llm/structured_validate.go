package llm

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/jsonutil" // perf-v6 round 8 Task 1：统一 JSON 序列化
)

// ValidationError 结构化输出验证错误
type ValidationError struct {
	Path    string
	Message string
}

func (e ValidationError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("%s: %s", e.Path, e.Message)
	}
	return e.Message
}

// ValidateAgainstSchema 验证 JSON 数据是否符合 JSON Schema 约束
// 支持核心约束：type、required、enum、minimum/maximum、minLength/maxLength、嵌套对象、数组
func ValidateAgainstSchema(data json.RawMessage, schema *SchemaDef) []ValidationError {
	if schema == nil {
		return []ValidationError{{Message: "schema must not be nil"}}
	}
	var v any
	// perf-v6 round 8 Task 1：使用 pooled reader
	if err := jsonutil.Unmarshal(data, &v); err != nil {
		return []ValidationError{{Message: fmt.Sprintf("无效 JSON: %s", err)}}
	}

	return validateValue(v, schema.Schema, "")
}

// validateValue 递归验证值
func validateValue(v any, schema map[string]any, path string) []ValidationError {
	var errs []ValidationError

	schemaType, _ := schema["type"].(string)

	switch schemaType {
	case "object":
		errs = append(errs, validateObject(v, schema, path)...)
	case "array":
		errs = append(errs, validateArray(v, schema, path)...)
	case "string", "integer", "number", "boolean":
		errs = append(errs, validateScalar(v, schema, path)...)
	default:
		if m, ok := v.(map[string]any); ok {
			errs = append(errs, validateObject(m, schema, path)...)
		}
	}

	return errs
}

// validateObject 验证对象类型
func validateObject(v any, schema map[string]any, path string) []ValidationError {
	var errs []ValidationError

	m, ok := v.(map[string]any)
	if !ok {
		return []ValidationError{{
			Path:    path,
			Message: fmt.Sprintf("期望 object 类型，实际为 %s", jsonTypeOf(v)),
		}}
	}

	if required, ok := schema["required"].([]string); ok {
		for _, key := range required {
			if _, exists := m[key]; !exists {
				p := joinPath(path, key)
				errs = append(errs, ValidationError{
					Path:    p,
					Message: "字段为必填项",
				})
			}
		}
	}

	properties, _ := schema["properties"].(map[string]any)
	for key, propSchema := range properties {
		val, exists := m[key]
		if !exists {
			continue
		}

		propMap, ok := propSchema.(map[string]any)
		if !ok {
			continue
		}

		p := joinPath(path, key)
		errs = append(errs, validateValue(val, propMap, p)...)
	}

	return errs
}

// validateArray 验证数组类型
func validateArray(v any, schema map[string]any, path string) []ValidationError {
	var errs []ValidationError

	arr, ok := v.([]any)
	if !ok {
		return []ValidationError{{
			Path:    path,
			Message: fmt.Sprintf("期望 array 类型，实际为 %s", jsonTypeOf(v)),
		}}
	}

	itemsSchema, _ := schema["items"].(map[string]any)
	if itemsSchema == nil {
		return nil
	}

	for i, item := range arr {
		p := fmt.Sprintf("%s[%d]", path, i)
		errs = append(errs, validateValue(item, itemsSchema, p)...)
	}

	return errs
}

// validateScalar 验证标量类型
func validateScalar(v any, schema map[string]any, path string) []ValidationError {
	var errs []ValidationError

	schemaType, _ := schema["type"].(string)

	if !typeMatches(v, schemaType) {
		errs = append(errs, ValidationError{
			Path:    path,
			Message: fmt.Sprintf("期望 %s 类型，实际为 %s", schemaType, jsonTypeOf(v)),
		})
		return errs
	}

	if enumVals, ok := schema["enum"].([]string); ok {
		s, _ := v.(string)
		if !containsString(enumVals, s) {
			errs = append(errs, ValidationError{
				Path:    path,
				Message: fmt.Sprintf("值 %q 不在枚举范围内 %v", s, enumVals),
			})
		}
	}

	if min, ok := toFloat(schema["minimum"]); ok {
		if num, ok := toFloat(v); ok && num < min {
			errs = append(errs, ValidationError{
				Path:    path,
				Message: fmt.Sprintf("值 %v 小于最小值 %v", num, min),
			})
		}
	}

	if max, ok := toFloat(schema["maximum"]); ok {
		if num, ok := toFloat(v); ok && num > max {
			errs = append(errs, ValidationError{
				Path:    path,
				Message: fmt.Sprintf("值 %v 大于最大值 %v", num, max),
			})
		}
	}

	if minLen, ok := schema["minLength"].(int); ok {
		if s, ok := v.(string); ok && len(s) < minLen {
			errs = append(errs, ValidationError{
				Path:    path,
				Message: fmt.Sprintf("字符串长度 %d 小于最小长度 %d", len(s), minLen),
			})
		}
	}

	if maxLen, ok := schema["maxLength"].(int); ok {
		if s, ok := v.(string); ok && len(s) > maxLen {
			errs = append(errs, ValidationError{
				Path:    path,
				Message: fmt.Sprintf("字符串长度 %d 大于最大长度 %d", len(s), maxLen),
			})
		}
	}

	return errs
}

// typeMatches 检查值是否匹配 JSON Schema 类型
func typeMatches(v any, schemaType string) bool {
	switch schemaType {
	case "string":
		_, ok := v.(string)
		return ok
	case "integer":
		switch val := v.(type) {
		case float64:
			return val == float64(int64(val))
		case int, int8, int16, int32, int64:
			return true
		}
		return false
	case "number":
		switch val := v.(type) {
		case float64, int, int8, int16, int32, int64:
			_ = val
			return true
		}
		return false
	case "boolean":
		_, ok := v.(bool)
		return ok
	}
	return true
}

// jsonTypeOf 返回值的 JSON 类型名
func jsonTypeOf(v any) string {
	switch v := v.(type) {
	case string:
		return "string"
	case float64:
		if v == float64(int64(v)) {
			return "integer"
		}
		return "number"
	case bool:
		return "boolean"
	case nil:
		return "null"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return "unknown"
	}
}

// joinPath 拼接 JSON Path
func joinPath(parent, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
}

// toFloat 将值转换为 float64
func toFloat(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case int:
		return float64(val), true
	case int8:
		return float64(val), true
	case int16:
		return float64(val), true
	case int32:
		return float64(val), true
	case int64:
		return float64(val), true
	case json.Number:
		if f, err := val.Float64(); err == nil {
			return f, true
		}
	}
	return 0, false
}

// containsString 检查字符串切片是否包含目标
func containsString(slice []string, target string) bool {
	return slices.Contains(slice, target)
}
