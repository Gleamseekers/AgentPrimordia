package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools"
)

type CalculatorTool struct{}

func NewCalculator() *CalculatorTool {
	return &CalculatorTool{}
}

func (c *CalculatorTool) Name() string {
	return "calculator"
}

func (c *CalculatorTool) Description() string {
	return "Performs basic arithmetic calculations: add, subtract, multiply, divide"
}

func (c *CalculatorTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "operation": {"type": "string", "description": "Arithmetic operation: add, subtract, multiply, divide"},
    "a": {"type": "number", "description": "First number"},
    "b": {"type": "number", "description": "Second number"}
  },
  "required": ["operation", "a", "b"]
}`)
}

func (c *CalculatorTool) Execute(ctx context.Context, args json.RawMessage) (*tools.Result, error) {
	var params map[string]json.RawMessage
	if err := json.Unmarshal(args, &params); err != nil {
		return tools.NewErrorResult(fmt.Sprintf("invalid arguments: %v", err)), nil
	}

	var op string
	if err := json.Unmarshal(params["operation"], &op); err != nil {
		return tools.NewErrorResult("operation is required"), nil
	}

	var a float64
	if err := json.Unmarshal(params["a"], &a); err != nil {
		return tools.NewErrorResult("a is required and must be a number"), nil
	}

	var b float64
	if err := json.Unmarshal(params["b"], &b); err != nil {
		return tools.NewErrorResult("b is required and must be a number"), nil
	}

	var result float64
	switch op {
	case "add":
		result = a + b
	case "subtract":
		result = a - b
	case "multiply":
		result = a * b
	case "divide":
		if b == 0 {
			return tools.NewErrorResult("division by zero"), nil
		}
		result = a / b
	default:
		return tools.NewErrorResult(fmt.Sprintf("unknown operation: %s", op)), nil
	}

	return tools.NewResult(fmt.Sprintf("%.2f", result)), nil
}

type DateTimeTool struct{}

func NewDateTime() *DateTimeTool {
	return &DateTimeTool{}
}

func (d *DateTimeTool) Name() string {
	return "datetime"
}

func (d *DateTimeTool) Description() string {
	return "Get current date and time, or convert between formats"
}

func (d *DateTimeTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "action": {"type": "string", "description": "Action to perform: now, format", "default": "now"},
    "format": {"type": "string", "description": "Date format (Go layout or preset: RFC3339, ISO8601, simple, date, time)"}
  }
}`)
}

func (d *DateTimeTool) Execute(ctx context.Context, args json.RawMessage) (*tools.Result, error) {
	var params map[string]json.RawMessage
	if err := json.Unmarshal(args, &params); err != nil {
		return tools.NewErrorResult(fmt.Sprintf("invalid arguments: %v", err)), nil
	}

	action := "now"
	if raw, ok := params["action"]; ok && len(raw) > 0 {
		if err := unmarshalRaw(raw, &action); err != nil {
			return tools.NewErrorResult(fmt.Sprintf("invalid parameter 'action': %v", err)), nil
		}
	}

	switch action {
	case "now":
		format := time.RFC3339
		if raw, ok := params["format"]; ok && len(raw) > 0 {
			var f string
			if err := unmarshalRaw(raw, &f); err != nil {
				return tools.NewErrorResult(fmt.Sprintf("invalid parameter 'format': %v", err)), nil
			}
			format = getLayout(f)
		}
		return tools.NewResult(time.Now().Format(format)), nil
	default:
		return tools.NewErrorResult(fmt.Sprintf("unknown action: %s", action)), nil
	}
}

// unmarshalRaw 从 JSON RawMessage 解析参数值
// 参数不存在（nil 或空）时使用零值，参数存在但格式错误时返回错误
func unmarshalRaw(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, target)
}

func parseNumber(v any) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case float32:
		return float64(n), nil
	case int:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case string:
		return strconv.ParseFloat(n, 64)
	default:
		return 0, fmt.Errorf("unsupported type: %T", v)
	}
}

func getLayout(name string) string {
	switch name {
	case "RFC3339":
		return time.RFC3339
	case "ISO8601":
		return "2006-01-02T15:04:05Z07:00"
	case "simple":
		return "2006-01-02 15:04:05"
	case "date":
		return "2006-01-02"
	case "time":
		return "15:04:05"
	default:
		return name
	}
}
